package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

// serviceKinds are the resources that run code and can be deployed.
var serviceKinds = map[api.ResourceKind]bool{
	api.Application:  true,
	api.Worker:       true,
	api.ScheduledJob: true,
}

const projectTokenHint = "a project token reaches only its own project, so name it with --project"

// scope narrows name lookups to a project and environment.
type scope struct {
	project     string
	environment string
}

func (sc *scope) flags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&sc.project, "project", "p", "", "project name, slug or id")
	cmd.Flags().StringVarP(&sc.environment, "environment", "e", "", "environment name or slug")
}

// projects returns the project named by --project, or every project in the
// organisation.
func (s *session) projects(ctx context.Context, sc scope) ([]api.ProjectOut, error) {
	if id, err := uuid.Parse(sc.project); err == nil {
		r, err := s.api.GetProjectWithResponse(ctx, id)
		if err != nil {
			return nil, err
		}
		p, err := result(r.HTTPResponse, r.Body, r.JSON200)
		if err != nil {
			return nil, err
		}
		return []api.ProjectOut{*p}, nil
	}
	org, err := s.org(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.api.ListProjectsWithResponse(ctx, org.Id)
	if err != nil {
		return nil, err
	}
	all, err := result(r.HTTPResponse, r.Body, r.JSON200)
	if isStatus(err, http.StatusNotFound) {
		return nil, fmt.Errorf("this key cannot list the organisation's projects, and %s id", projectTokenHint)
	}
	if err != nil {
		return nil, err
	}
	if sc.project == "" {
		return *all, nil
	}
	for _, p := range *all {
		if p.Name == sc.project || p.Slug == sc.project {
			return []api.ProjectOut{p}, nil
		}
	}
	return nil, fmt.Errorf("no project is called %q", sc.project)
}

// environment finds the environment named by --environment in p, or returns
// nil for every environment.
func environment(p api.ProjectOut, name string) (*api.EnvironmentOut, bool) {
	if name == "" {
		return nil, true
	}
	for i, e := range p.Environments {
		if e.Name == name || e.Slug == name || e.Id.String() == name {
			return &p.Environments[i], true
		}
	}
	return nil, false
}

func envName(p api.ProjectOut, id uuid.UUID) string {
	for _, e := range p.Environments {
		if e.Id == id {
			return e.Name
		}
	}
	return id.String()
}

// placed is a resource with the project it lives in.
type placed struct {
	api.ResourceOut
	project api.ProjectOut
}

func (r placed) where() string {
	return fmt.Sprintf("%s (%s)", r.project.Name, envName(r.project, r.EnvironmentId))
}

// resources lists the resources in scope.
func (s *session) resources(ctx context.Context, sc scope) ([]placed, error) {
	projects, err := s.projects(ctx, sc)
	if err != nil {
		return nil, err
	}
	var out []placed
	found := false
	for _, p := range projects {
		env, ok := environment(p, sc.environment)
		if !ok {
			continue
		}
		found = true
		params := &api.ListResourcesParams{}
		if env != nil {
			params.EnvironmentId = &env.Id
		}
		r, err := s.api.ListResourcesWithResponse(ctx, p.Id, params)
		if err != nil {
			return nil, err
		}
		list, err := result(r.HTTPResponse, r.Body, r.JSON200)
		if err != nil {
			return nil, err
		}
		for _, res := range *list {
			out = append(out, placed{ResourceOut: res, project: p})
		}
	}
	if !found {
		return nil, fmt.Errorf("no environment is called %q", sc.environment)
	}
	return out, nil
}

// service finds one resource by id, or by name or slug within the scope.
func (s *session) service(ctx context.Context, ref string, sc scope) (placed, error) {
	if id, err := uuid.Parse(ref); err == nil {
		r, err := s.api.GetResourceWithResponse(ctx, id)
		if err != nil {
			return placed{}, err
		}
		res, err := result(r.HTTPResponse, r.Body, r.JSON200)
		if err != nil {
			return placed{}, err
		}
		pr, err := s.api.GetProjectWithResponse(ctx, res.ProjectId)
		if err != nil {
			return placed{}, err
		}
		p, err := result(pr.HTTPResponse, pr.Body, pr.JSON200)
		if err != nil {
			return placed{}, err
		}
		return placed{ResourceOut: *res, project: *p}, nil
	}
	all, err := s.resources(ctx, sc)
	if err != nil {
		return placed{}, err
	}
	var matches []placed
	for _, r := range all {
		if r.Name == ref || r.Slug == ref {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return placed{}, fmt.Errorf("no service is called %q", ref)
	case 1:
		return matches[0], nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q names more than one service, so pass its id or narrow it with --project and --environment:", ref)
	for _, m := range matches {
		fmt.Fprintf(&b, "\n  %s in %s, %s", m.Name, m.where(), m.Id)
	}
	return placed{}, errors.New(b.String())
}
