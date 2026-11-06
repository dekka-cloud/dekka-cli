package cli

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

func (a *App) deployCmd() *cobra.Command {
	var sc scope
	var branch, commit string
	cmd := &cobra.Command{
		Use:   "deploy <service>",
		Short: "Deploy a service",
		Long: "deploy starts a deployment of a service, named by its name, slug or id. A service built from GitHub\n" +
			"deploys the head of its configured branch unless --branch or --commit says otherwise.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			svc, err := s.service(cmd.Context(), args[0], sc)
			if err != nil {
				return err
			}
			body := api.CreateDeploymentRequest{}
			if branch != "" {
				body.Branch = &branch
			}
			if commit != "" {
				body.CommitSha = &commit
			}
			r, err := s.api.CreateDeploymentWithResponse(cmd.Context(), svc.Id, body)
			if err != nil {
				return err
			}
			dep, err := result(r.HTTPResponse, r.Body, r.JSON201)
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(dep)
			}
			fmt.Fprintf(a.Out, "Started deployment %s of %s in %s.\n", dep.Id, svc.Name, svc.where())
			fmt.Fprintf(a.Out, "Follow it with: dekka logs %s --deployment %s --follow\n", svc.Id, dep.Id)
			return nil
		},
	}
	sc.flags(cmd)
	cmd.Flags().StringVar(&branch, "branch", "", "the branch to deploy")
	cmd.Flags().StringVar(&commit, "commit", "", "the commit to deploy, as a full or abbreviated SHA")
	return cmd
}

type serviceStatus struct {
	Service     api.ResourceOut     `json:"service"`
	Deployments []api.DeploymentOut `json:"deployments"`
}

func (a *App) statusCmd() *cobra.Command {
	var sc scope
	cmd := &cobra.Command{
		Use:   "status <service>",
		Short: "Show a service's status and its recent deployments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			svc, err := s.service(cmd.Context(), args[0], sc)
			if err != nil {
				return err
			}
			limit := 5
			r, err := s.api.ListResourceDeploymentsWithResponse(cmd.Context(), svc.Id, &api.ListResourceDeploymentsParams{Limit: &limit})
			if err != nil {
				return err
			}
			deps, err := result(r.HTTPResponse, r.Body, r.JSON200)
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(serviceStatus{Service: svc.ResourceOut, Deployments: *deps})
			}
			status := string(svc.Status)
			if reason := str(svc.StatusReason); reason != "" {
				status += ", " + reason
			}
			pairs := [][2]string{
				{"Service", svc.Name},
				{"Kind", kindName(svc.Kind)},
				{"Project", svc.where()},
				{"Status", status},
				{"URL", address(svc.ResourceOut)},
			}
			if op := svc.ActiveOperation; op != nil {
				pairs = append(pairs, [2]string{"Operation", fmt.Sprintf("%s is %s at %.0f%%", op.Type, op.State, op.Progress*100)})
			}
			pairs = append(pairs, [2]string{"ID", svc.Id.String()})
			if err := a.fields(pairs); err != nil {
				return err
			}
			if len(*deps) == 0 {
				fmt.Fprintln(a.Out, "\nIt has not been deployed yet.")
				return nil
			}
			fmt.Fprintln(a.Out)
			rows := make([][]string, 0, len(*deps))
			for _, d := range *deps {
				rows = append(rows, []string{d.Id.String(), string(d.Status), string(d.Trigger), source(d), when(d.CreatedAt)})
			}
			return a.table([]string{"Deployment", "Status", "Trigger", "Source", "Created"}, rows)
		},
	}
	sc.flags(cmd)
	return cmd
}

// source describes what a deployment ran: a branch and commit, or an image.
func source(d api.DeploymentOut) string {
	branch, sha := str(d.Branch), short(str(d.CommitSha))
	switch {
	case branch != "" && sha != "":
		return branch + " at " + sha
	case branch != "":
		return branch
	case sha != "":
		return sha
	}
	return str(d.ImageRef)
}

func (a *App) rollbackCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rollback <deployment>",
		Short: "Deploy an earlier deployment's image again",
		Long: "rollback deploys the image an earlier healthy deployment ran, with the settings it ran with and the\n" +
			"service's current variables. It names the service and deployment and asks before it starts, unless you\n" +
			"pass --yes. dekka status <service> lists recent deployments and their ids.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := uuid.Parse(args[0])
			if err != nil {
				return errors.New("pass the deployment's id, which dekka status <service> lists")
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			dr, err := s.api.GetDeploymentWithResponse(ctx, id)
			if err != nil {
				return err
			}
			target, err := result(dr.HTTPResponse, dr.Body, dr.JSON200)
			if err != nil {
				return err
			}
			svc, err := s.service(ctx, target.ResourceId.String(), scope{})
			if err != nil {
				return err
			}
			if !yes {
				what := source(*target)
				if what != "" {
					what = ", which ran " + what
				}
				ok, err := a.confirm(fmt.Sprintf("Roll back %s in %s to deployment %s from %s%s? The service keeps its current variables.",
					svc.Name, svc.where(), target.Id, when(target.CreatedAt), what))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("rollback cancelled")
				}
			}
			r, err := s.api.RollbackDeploymentWithResponse(ctx, id)
			if err != nil {
				return err
			}
			dep, err := result(r.HTTPResponse, r.Body, r.JSON201)
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(dep)
			}
			fmt.Fprintf(a.Out, "Started deployment %s of %s, rolling back to %s.\n", dep.Id, svc.Name, target.Id)
			fmt.Fprintf(a.Out, "Follow it with: dekka logs %s --deployment %s --follow\n", svc.Id, dep.Id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "roll back without asking")
	return cmd
}
