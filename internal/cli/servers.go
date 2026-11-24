package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

// settled are the states a server rests in until someone acts on it.
var settled = map[api.ServerState]bool{
	api.ServerStateRunning: true,
	api.ServerStateStopped: true,
	api.ServerStateFailed:  true,
	api.ServerStateDeleted: true,
}

func (a *App) serversCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "servers",
		Aliases: []string{"server", "vm"},
		Short:   "Work with virtual servers",
		Long: "servers creates and looks after Linux virtual servers with root over SSH. A server is billed by the hour\n" +
			"for as long as it exists, including while it's stopped. dekka servers sizes lists what you can create.",
	}
	cmd.AddCommand(
		a.serversListCmd(), a.serversSizesCmd(), a.serversCreateCmd(), a.serversShowCmd(), a.serversSSHCmd(),
		a.serverActionCmd("start", "Start a stopped server"),
		a.serverActionCmd("stop", "Stop a server, keeping its disk"),
		a.serverActionCmd("reboot", "Reboot a server"),
		a.serversResizeCmd(), a.serversRebuildCmd(), a.serversDeleteCmd(),
		a.serversSnapshotsCmd(), a.serversSnapshotCmd(), a.serversRestoreCmd(),
	)
	return cmd
}

func (a *App) sshKeysCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ssh-keys", Short: "Work with the SSH keys new servers install for root"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the organisation's SSH keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			keys, err := s.sshKeys(cmd.Context())
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(keys)
			}
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{k.Name, k.KeyType, k.Fingerprint, k.Id.String()})
			}
			return a.table([]string{"Name", "Type", "Fingerprint", "ID"}, rows)
		},
	}, &cobra.Command{
		Use:   "add <name> <public-key-file>",
		Short: "Add an SSH public key, such as ~/.ssh/id_ed25519.pub",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			public := strings.TrimSpace(string(data))
			if strings.Contains(public, "PRIVATE KEY") {
				return errors.New("that file holds a private key; pass the public key, the .pub file beside it")
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			org, err := s.org(cmd.Context())
			if err != nil {
				return err
			}
			r, err := s.api.CreateSshKeyWithResponse(cmd.Context(), org.Id, api.CreateSshKeyRequest{Name: args[0], PublicKey: public})
			if err != nil {
				return err
			}
			key, err := result(r.HTTPResponse, r.Body, r.JSON201)
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(key)
			}
			fmt.Fprintf(a.Out, "Added %s (%s).\n", key.Name, key.Fingerprint)
			return nil
		},
	})
	return cmd
}

func (a *App) serversListCmd() *cobra.Command {
	var sc scope
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List servers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			servers, err := s.servers(cmd.Context(), sc)
			if err != nil {
				return err
			}
			if a.json() {
				out := make([]api.ServerOut, 0, len(servers))
				for _, v := range servers {
					out = append(out, v.ServerOut)
				}
				return a.printJSON(out)
			}
			rows := make([][]string, 0, len(servers))
			for _, v := range servers {
				rows = append(rows, []string{v.Name, serverState(v.ServerOut), v.SizeId, str(v.Ipv4), v.where(), v.Id.String()})
			}
			return a.table([]string{"Name", "State", "Size", "IPv4", "Project", "ID"}, rows)
		},
	}
	sc.flags(cmd)
	return cmd
}

func (a *App) serversSizesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sizes",
		Short: "List the sizes and images a new server can use",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			r, err := s.api.ServerCatalogWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			catalog, err := result(r.HTTPResponse, r.Body, r.JSON200)
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(catalog)
			}
			var rows [][]string
			for _, z := range catalog.Sizes {
				if !z.Offered {
					continue
				}
				room := "yes"
				if !z.Available {
					room = "no room now"
				}
				rows = append(rows, []string{z.Id, z.Name, strconv.Itoa(z.Vcpu), mib(z.MemoryMib), fmt.Sprintf("%d GB", z.DiskGb),
					fmt.Sprintf("%s %s", z.Currency, z.MonthlyPrice), room})
			}
			if err := a.table([]string{"Size", "Name", "vCPU", "Memory", "Disk", "Per month", "Available"}, rows); err != nil {
				return err
			}
			fmt.Fprintln(a.Out)
			rows = nil
			for _, i := range catalog.Images {
				if i.Available {
					rows = append(rows, []string{i.Id, i.Name + " " + i.Version, i.RuntimeProfile})
				}
			}
			return a.table([]string{"Image", "System", "Runtime"}, rows)
		},
	}
}

func (a *App) serversCreateCmd() *cobra.Command {
	var sc scope
	var size, image, hostname, userData string
	var keys []string
	var wait bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a server",
		Long: "create makes a Linux server with root over SSH, installing the SSH keys you name with --ssh-key, which\n" +
			"dekka ssh-keys list shows. It's billed by the hour from now on. With --wait it returns once the server is\n" +
			"running and prints the command that reaches it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(keys) == 0 {
				return errors.New("name at least one SSH key with --ssh-key; dekka ssh-keys list shows them")
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			projects, err := s.projects(ctx, sc)
			if err != nil {
				return err
			}
			if len(projects) != 1 {
				return errors.New("name the project to create the server in with --project")
			}
			project := projects[0]
			body := api.CreateServerRequest{Name: args[0], SizeId: size, ImageId: image}
			if env, ok := environment(project, sc.environment); !ok {
				return fmt.Errorf("no environment is called %q", sc.environment)
			} else if env != nil {
				body.EnvironmentId = &env.Id
			}
			if body.SshKeyIds, err = s.sshKeyIDs(ctx, keys); err != nil {
				return err
			}
			if hostname != "" {
				body.Hostname = &hostname
			}
			if userData != "" {
				data, err := os.ReadFile(userData) //nolint:gosec // the person names this file to send it
				if err != nil {
					return err
				}
				text := string(data)
				body.UserData = &text
			}
			r, err := s.api.CreateServerWithResponse(ctx, project.Id, body)
			if err != nil {
				return err
			}
			created, err := result(r.HTTPResponse, r.Body, r.JSON201)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.Err, "Creating %s in %s.\n", created.Name, project.Name)
			return a.finish(ctx, s, *created, wait)
		},
	}
	sc.flags(cmd)
	cmd.Flags().StringVar(&size, "size", "", "size id, from dekka servers sizes")
	cmd.Flags().StringVar(&image, "image", "", "image id, from dekka servers sizes")
	cmd.Flags().StringArrayVar(&keys, "ssh-key", nil, "SSH key name or id to install for root; repeat for more")
	cmd.Flags().StringVar(&hostname, "hostname", "", "hostname, which defaults to the name")
	cmd.Flags().StringVar(&userData, "user-data", "", "file of cloud-init user data for the first boot")
	cmd.Flags().BoolVarP(&wait, "wait", "w", false, "wait until the server is running")
	_ = cmd.MarkFlagRequired("size")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func (a *App) serversShowCmd() *cobra.Command {
	var sc scope
	cmd := &cobra.Command{
		Use:   "show <server>",
		Short: "Show a server's state, size, addresses and SSH command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			v, err := s.server(cmd.Context(), args[0], sc)
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(v.ServerOut)
			}
			ssh, _ := s.sshCommand(cmd.Context(), v.ServerOut)
			return a.fields([][2]string{
				{"Name", v.Name},
				{"State", serverState(v.ServerOut)},
				{"Size", v.SizeId},
				{"Image", v.ImageId},
				{"Project", v.where()},
				{"IPv4", str(v.Ipv4)},
				{"IPv6", str(v.Ipv6)},
				{"Private IPv4", str(v.PrivateIpv4)},
				{"SSH", ssh},
				{"Per month", fmt.Sprintf("GHS %.2f", float64(v.HourlyPriceMinor)*730/100)},
				{"Reason", str(v.StatusReason)},
				{"ID", v.Id.String()},
			})
		},
	}
	sc.flags(cmd)
	return cmd
}

func (a *App) serversSSHCmd() *cobra.Command {
	var sc scope
	var print bool
	cmd := &cobra.Command{
		Use:   "ssh <server>",
		Short: "Connect to a server over SSH, or print the command that does",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			v, err := s.server(cmd.Context(), args[0], sc)
			if err != nil {
				return err
			}
			ep, err := s.sshEndpoint(cmd.Context(), v.ServerOut)
			if err != nil {
				return err
			}
			if print || a.RunSSH == nil {
				fmt.Fprintln(a.Out, ep.command())
				return nil
			}
			return a.RunSSH(ep.args())
		},
	}
	sc.flags(cmd)
	cmd.Flags().BoolVar(&print, "print", false, "print the command instead of running it")
	return cmd
}

func (a *App) serverActionCmd(action, short string) *cobra.Command {
	var sc scope
	var wait bool
	cmd := &cobra.Command{
		Use:   action + " <server>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := s.server(ctx, args[0], sc)
			if err != nil {
				return err
			}
			r, err := s.api.RunServerActionWithResponse(ctx, v.Id, api.RunServerActionParamsAction(action))
			if err != nil {
				return err
			}
			out, err := result(r.HTTPResponse, r.Body, r.JSON202)
			if err != nil {
				return err
			}
			return a.finish(ctx, s, *out, wait)
		},
	}
	sc.flags(cmd)
	cmd.Flags().BoolVarP(&wait, "wait", "w", false, "wait until the server settles")
	return cmd
}

func (a *App) serversResizeCmd() *cobra.Command {
	var sc scope
	var size string
	var wait bool
	cmd := &cobra.Command{
		Use:   "resize <server>",
		Short: "Move a stopped server to another size",
		Long: "resize moves a stopped server to a size whose disk is at least as large, since disks grow but never\n" +
			"shrink. Stop it first with dekka servers stop, and start it again once the resize is done.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := s.server(ctx, args[0], sc)
			if err != nil {
				return err
			}
			r, err := s.api.ResizeServerWithResponse(ctx, v.Id, api.ResizeServerRequest{SizeId: size})
			if err != nil {
				return err
			}
			out, err := result(r.HTTPResponse, r.Body, r.JSON202)
			if err != nil {
				return err
			}
			return a.finish(ctx, s, *out, wait)
		},
	}
	sc.flags(cmd)
	cmd.Flags().StringVar(&size, "size", "", "the new size id, from dekka servers sizes")
	cmd.Flags().BoolVarP(&wait, "wait", "w", false, "wait until the resize is done")
	_ = cmd.MarkFlagRequired("size")
	return cmd
}

func (a *App) serversRebuildCmd() *cobra.Command {
	var sc scope
	var image string
	var yes, wait bool
	cmd := &cobra.Command{
		Use:   "rebuild <server>",
		Short: "Reinstall a server from an image, wiping its disk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := s.server(ctx, args[0], sc)
			if err != nil {
				return err
			}
			if err := a.check(yes, fmt.Sprintf("Rebuild %s in %s from %s? Its disk and everything on it is wiped; snapshots stay.",
				v.Name, v.where(), image), "rebuild cancelled"); err != nil {
				return err
			}
			r, err := s.api.RebuildServerWithResponse(ctx, v.Id, api.RebuildServerRequest{ImageId: image, Confirm: v.Name})
			if err != nil {
				return err
			}
			out, err := result(r.HTTPResponse, r.Body, r.JSON202)
			if err != nil {
				return err
			}
			return a.finish(ctx, s, *out, wait)
		},
	}
	sc.flags(cmd)
	cmd.Flags().StringVar(&image, "image", "", "image id, from dekka servers sizes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "rebuild without asking")
	cmd.Flags().BoolVarP(&wait, "wait", "w", false, "wait until the server is running again")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func (a *App) serversDeleteCmd() *cobra.Command {
	var sc scope
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <server>",
		Short: "Delete a server with its disk and snapshots",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := s.server(ctx, args[0], sc)
			if err != nil {
				return err
			}
			if err := a.check(yes, fmt.Sprintf("Delete %s in %s, with its disk and snapshots? This can't be undone.",
				v.Name, v.where()), "delete cancelled"); err != nil {
				return err
			}
			r, err := s.api.DeleteServerWithResponse(ctx, v.Id, &api.DeleteServerParams{Confirm: &v.Name})
			if err != nil {
				return err
			}
			out, err := result(r.HTTPResponse, r.Body, r.JSON202)
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(out)
			}
			fmt.Fprintf(a.Out, "Deleting %s. Billing stops at the end of this hour.\n", out.Name)
			return nil
		},
	}
	sc.flags(cmd)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete without asking")
	return cmd
}

func (a *App) serversSnapshotsCmd() *cobra.Command {
	var sc scope
	cmd := &cobra.Command{
		Use:   "snapshots <server>",
		Short: "List a server's disk snapshots",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			v, err := s.server(cmd.Context(), args[0], sc)
			if err != nil {
				return err
			}
			r, err := s.api.ListServerSnapshotsWithResponse(cmd.Context(), v.Id)
			if err != nil {
				return err
			}
			list, err := result(r.HTTPResponse, r.Body, r.JSON200)
			if err != nil {
				return err
			}
			return a.snapshots(*list)
		},
	}
	sc.flags(cmd)
	return cmd
}

func (a *App) serversSnapshotCmd() *cobra.Command {
	var sc scope
	cmd := &cobra.Command{
		Use:   "snapshot <server> <name>",
		Short: "Take a snapshot of a server's disk",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			v, err := s.server(cmd.Context(), args[0], sc)
			if err != nil {
				return err
			}
			r, err := s.api.CreateServerSnapshotWithResponse(cmd.Context(), v.Id, api.CreateServerSnapshotJSONRequestBody{Name: args[1]})
			if err != nil {
				return err
			}
			list, err := result(r.HTTPResponse, r.Body, r.JSON202)
			if err != nil {
				return err
			}
			return a.snapshots(*list)
		},
	}
	sc.flags(cmd)
	return cmd
}

func (a *App) serversRestoreCmd() *cobra.Command {
	var sc scope
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <server> <snapshot-id>",
		Short: "Put a stopped server's disk back to a snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[1])
			if err != nil {
				return errors.New("pass the snapshot's id, which dekka servers snapshots lists")
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := s.server(ctx, args[0], sc)
			if err != nil {
				return err
			}
			if err := a.check(yes, fmt.Sprintf("Put %s's disk back to snapshot %d? Everything written since is lost.", v.Name, id),
				"restore cancelled"); err != nil {
				return err
			}
			r, err := s.api.RevertServerSnapshotWithResponse(ctx, v.Id, id, api.RevertServerSnapshotJSONRequestBody{Confirm: v.Name})
			if err != nil {
				return err
			}
			list, err := result(r.HTTPResponse, r.Body, r.JSON202)
			if err != nil {
				return err
			}
			return a.snapshots(*list)
		},
	}
	sc.flags(cmd)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "restore without asking")
	return cmd
}

// check asks before a destructive change, unless yes was passed.
func (a *App) check(yes bool, question, cancelled string) error {
	if yes {
		return nil
	}
	ok, err := a.confirm(question)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New(cancelled)
	}
	return nil
}

func (a *App) snapshots(list api.SnapshotsOut) error {
	if a.json() {
		return a.printJSON(list)
	}
	rows := make([][]string, 0, len(list.Snapshots))
	for _, n := range list.Snapshots {
		mark := ""
		if n.Current {
			mark = "current"
		}
		rows = append(rows, []string{strconv.Itoa(n.Id), n.Name, when(n.CreatedAt), mib(n.SizeMib), mark})
	}
	return a.table([]string{"ID", "Name", "Taken", "Size", ""}, rows)
}

// finish prints a server after a change, waiting for it to settle first when asked.
func (a *App) finish(ctx context.Context, s *session, v api.ServerOut, wait bool) error {
	if wait {
		var err error
		if v, err = a.waitFor(ctx, s, v); err != nil {
			return err
		}
	}
	if a.json() {
		return a.printJSON(v)
	}
	fmt.Fprintf(a.Out, "%s is %s.\n", v.Name, serverState(v))
	if v.State == api.ServerStateRunning && !busy(v) {
		if ssh, err := s.sshCommand(ctx, v); err == nil {
			fmt.Fprintf(a.Out, "Connect with: %s\n", ssh)
		}
	}
	return nil
}

func (a *App) waitFor(ctx context.Context, s *session, v api.ServerOut) (api.ServerOut, error) {
	interval := a.PollInterval
	if interval == 0 {
		interval = 2 * time.Second
	}
	last := ""
	for {
		if settled[v.State] && !busy(v) {
			return v, nil
		}
		if state := serverState(v); state != last {
			fmt.Fprintf(a.Err, "%s is %s…\n", v.Name, state)
			last = state
		}
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		case <-time.After(interval):
		}
		r, err := s.api.GetServerWithResponse(ctx, v.Id)
		if err != nil {
			return v, err
		}
		next, err := result(r.HTTPResponse, r.Body, r.JSON200)
		if err != nil {
			return v, err
		}
		v = *next
	}
}

func busy(v api.ServerOut) bool {
	return v.ActiveOperation != nil
}

func serverState(v api.ServerOut) string {
	if v.PendingSizeId != nil {
		return fmt.Sprintf("%s, resizing to %s", v.State, *v.PendingSizeId)
	}
	return string(v.State)
}

func mib(n int) string {
	if n%1024 == 0 {
		return fmt.Sprintf("%d GB", n/1024)
	}
	return fmt.Sprintf("%d MB", n)
}

// placedServer is a server with the project it belongs to.
type placedServer struct {
	api.ServerOut
	project api.ProjectOut
}

func (v placedServer) where() string {
	return fmt.Sprintf("%s (%s)", v.project.Name, envName(v.project, v.EnvironmentId))
}

// servers lists the servers in scope.
func (s *session) servers(ctx context.Context, sc scope) ([]placedServer, error) {
	projects, err := s.projects(ctx, sc)
	if err != nil {
		return nil, err
	}
	var out []placedServer
	found := false
	for _, p := range projects {
		env, ok := environment(p, sc.environment)
		if !ok {
			continue
		}
		found = true
		r, err := s.api.ListServersWithResponse(ctx, p.Id)
		if err != nil {
			return nil, err
		}
		list, err := result(r.HTTPResponse, r.Body, r.JSON200)
		if err != nil {
			return nil, err
		}
		for _, v := range *list {
			if env == nil || v.EnvironmentId == env.Id {
				out = append(out, placedServer{ServerOut: v, project: p})
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("no environment is called %q", sc.environment)
	}
	return out, nil
}

// server finds one server by id, or by name within the scope.
func (s *session) server(ctx context.Context, ref string, sc scope) (placedServer, error) {
	if id, err := uuid.Parse(ref); err == nil {
		r, err := s.api.GetServerWithResponse(ctx, id)
		if err != nil {
			return placedServer{}, err
		}
		v, err := result(r.HTTPResponse, r.Body, r.JSON200)
		if err != nil {
			return placedServer{}, err
		}
		pr, err := s.api.GetProjectWithResponse(ctx, v.ProjectId)
		if err != nil {
			return placedServer{}, err
		}
		p, err := result(pr.HTTPResponse, pr.Body, pr.JSON200)
		if err != nil {
			return placedServer{}, err
		}
		return placedServer{ServerOut: *v, project: *p}, nil
	}
	all, err := s.servers(ctx, sc)
	if err != nil {
		return placedServer{}, err
	}
	var matches []placedServer
	for _, v := range all {
		if v.Name == ref {
			matches = append(matches, v)
		}
	}
	switch len(matches) {
	case 0:
		return placedServer{}, fmt.Errorf("no server is called %q", ref)
	case 1:
		return matches[0], nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q names more than one server, so pass its id or narrow it with --project and --environment:", ref)
	for _, m := range matches {
		fmt.Fprintf(&b, "\n  %s in %s, %s", m.Name, m.where(), m.Id)
	}
	return placedServer{}, errors.New(b.String())
}

// sshHost is what an SSH endpoint's hostname may be: a name or an IP address, never
// anything ssh could read as an option.
var sshHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]*$`)

// endpoint is where a server answers SSH.
type endpoint struct {
	host string
	port int
}

func (e endpoint) args() []string {
	if e.port == 22 {
		return []string{"root@" + e.host}
	}
	return []string{"-p", strconv.Itoa(e.port), "root@" + e.host}
}

func (e endpoint) command() string {
	return "ssh " + strings.Join(e.args(), " ")
}

// sshEndpoint is where a running server answers SSH, as the engine observes it.
func (s *session) sshEndpoint(ctx context.Context, v api.ServerOut) (endpoint, error) {
	if v.State != api.ServerStateRunning {
		return endpoint{}, fmt.Errorf("%s is %s, so there is nothing to connect to yet", v.Name, v.State)
	}
	r, err := s.api.GetResourceWithResponse(ctx, v.Id)
	if err != nil {
		return endpoint{}, err
	}
	res, err := result(r.HTTPResponse, r.Body, r.JSON200)
	if err != nil {
		return endpoint{}, err
	}
	if res.Observed != nil {
		for _, e := range res.Observed.Endpoints {
			if e.Kind != "ssh" {
				continue
			}
			if !sshHost.MatchString(e.Hostname) || e.Port < 0 || e.Port > 65535 {
				return endpoint{}, fmt.Errorf("%s reported an SSH address the CLI won't use: %q port %d", v.Name, e.Hostname, e.Port)
			}
			port := e.Port
			if port == 0 {
				port = 22
			}
			return endpoint{host: e.Hostname, port: port}, nil
		}
	}
	return endpoint{}, fmt.Errorf("%s hasn't reported where it answers SSH yet; try again in a moment", v.Name)
}

// sshCommand is the command that reaches a running server.
func (s *session) sshCommand(ctx context.Context, v api.ServerOut) (string, error) {
	ep, err := s.sshEndpoint(ctx, v)
	if err != nil {
		return "", err
	}
	return ep.command(), nil
}

func (s *session) sshKeys(ctx context.Context) ([]api.SshKeyOut, error) {
	org, err := s.org(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.api.ListSshKeysWithResponse(ctx, org.Id)
	if err != nil {
		return nil, err
	}
	keys, err := result(r.HTTPResponse, r.Body, r.JSON200)
	if err != nil {
		return nil, err
	}
	return *keys, nil
}

// sshKeyIDs turns key names or ids into ids.
func (s *session) sshKeyIDs(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	keys, err := s.sshKeys(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(refs))
	for _, ref := range refs {
		found := false
		for _, k := range keys {
			if k.Name == ref || k.Id.String() == ref {
				ids = append(ids, k.Id)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no SSH key is called %q; dekka ssh-keys list shows them", ref)
		}
	}
	return ids, nil
}
