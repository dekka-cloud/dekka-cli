package cli

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

func (a *App) projectsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "projects", Short: "Work with projects"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the organisation's projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			projects, err := s.projects(cmd.Context(), scope{})
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(projects)
			}
			rows := make([][]string, 0, len(projects))
			for _, p := range projects {
				envs := make([]string, 0, len(p.Environments))
				for _, e := range p.Environments {
					envs = append(envs, e.Name)
				}
				rows = append(rows, []string{p.Name, p.RegionId, strings.Join(envs, ", "), strconv.Itoa(p.ServiceCount), p.Id.String()})
			}
			return a.table([]string{"Name", "Region", "Environments", "Services", "ID"}, rows)
		},
	})
	return cmd
}

func (a *App) appsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "apps", Aliases: []string{"services"}, Short: "Work with applications, workers and cron jobs"}
	var sc scope
	list := &cobra.Command{
		Use:   "list",
		Short: "List applications, workers and cron jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			all, err := s.resources(cmd.Context(), sc)
			if err != nil {
				return err
			}
			var apps []placed
			for _, r := range all {
				if serviceKinds[r.Kind] {
					apps = append(apps, r)
				}
			}
			if a.json() {
				out := make([]api.ResourceOut, 0, len(apps))
				for _, r := range apps {
					out = append(out, r.ResourceOut)
				}
				return a.printJSON(out)
			}
			rows := make([][]string, 0, len(apps))
			for _, r := range apps {
				rows = append(rows, []string{r.Name, kindName(r.Kind), string(r.Status), r.where(), address(r.ResourceOut), r.Id.String()})
			}
			return a.table([]string{"Name", "Kind", "Status", "Project", "URL", "ID"}, rows)
		},
	}
	sc.flags(list)
	cmd.AddCommand(list)
	return cmd
}

func kindName(k api.ResourceKind) string {
	if k == api.ScheduledJob {
		return "cron job"
	}
	return string(k)
}

func address(r api.ResourceOut) string {
	if h := str(r.Hostname); h != "" {
		return "https://" + h
	}
	return ""
}
