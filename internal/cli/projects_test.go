package cli

import (
	"strings"
	"testing"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

func TestProjectsList(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	if code := h.run("projects", "list"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "NAME", "shop", "gh-accra", "production, staging", projectID)

	if code := h.run("projects", "list", "--output", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	got := decode[[]api.ProjectOut](t, h.out.String())
	if len(got) != 1 || got[0].Name != "shop" {
		t.Fatalf("projects json = %+v", got)
	}
}

func TestProjectsListWithAProjectToken(t *testing.T) {
	f := newFakeAPI(t)
	f.json("GET /v1/me", 200, meJSON)
	f.json("GET /v1/orgs/"+orgID+"/projects", 404, `{"error":{"code":"not_found","message":"Not found.","request_id":"r"}}`)
	h := newHarness(t, f)
	if code := h.run("projects", "list"); code == 0 {
		t.Fatal("expected an error")
	}
	contains(t, h.err.String(), "--project")
}

func TestAppsListShowsOnlyServices(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	if code := h.run("apps", "list"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "api", "application", "running", "shop (production)", "https://api-shop.apps.dekka.cloud")
	if got := h.out.String(); strings.Contains(got, "postgres") {
		t.Errorf("a database was listed as an app:\n%s", got)
	}

	if code := h.run("apps", "list", "-o", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	got := decode[[]api.ResourceOut](t, h.out.String())
	if len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("apps json = %+v", got)
	}
}

func TestAppsListNarrowsToAProjectAndEnvironment(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	if code := h.run("apps", "list", "--project", projectID, "--environment", "staging"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	if !f.called("GET /v1/projects/" + projectID + "/resources?environment_id=" + prevEnvID) {
		t.Fatalf("resources were not filtered by environment: %v", f.calls())
	}
	if f.called("GET /v1/orgs/" + orgID + "/projects") {
		t.Fatal("a project id still listed the organisation's projects, which a project token cannot do")
	}
	if code := h.run("apps", "list", "--project", "nope"); code == 0 {
		t.Fatal("an unknown project was accepted")
	}
	contains(t, h.err.String(), `no project is called "nope"`)
}
