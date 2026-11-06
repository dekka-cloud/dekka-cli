package cli

import (
	"testing"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

func TestDeployByName(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.json("POST /v1/resources/"+apiID+"/deployments", 201, deploymentJSON(newDepID, "queued", "manual"))
	h := newHarness(t, f)

	if code := h.run("deploy", "api", "--branch", "release", "--commit", "4f2a9c1"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	body := decode[map[string]string](t, f.body("POST /v1/resources/"+apiID+"/deployments"))
	if body["branch"] != "release" || body["commit_sha"] != "4f2a9c1" {
		t.Fatalf("deploy sent %v", body)
	}
	contains(t, h.out.String(), "Started deployment "+newDepID+" of api in shop (production).", "dekka logs "+apiID+" --deployment "+newDepID+" --follow")
}

func TestDeploySendsNoBranchUnlessAsked(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.json("POST /v1/resources/"+apiID+"/deployments", 201, deploymentJSON(newDepID, "queued", "manual"))
	h := newHarness(t, f)
	if code := h.run("deploy", apiID, "-o", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	if got := f.body("POST /v1/resources/" + apiID + "/deployments"); got != "{}" {
		t.Fatalf("deploy sent %s", got)
	}
	if got := decode[api.DeploymentOut](t, h.out.String()); got.Id.String() != newDepID {
		t.Fatalf("deploy json = %+v", got)
	}
}

func TestDeployReportsTheAPIError(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.json("POST /v1/resources/"+apiID+"/deployments", 403, `{"error":{"code":"forbidden","message":"Your role cannot deploy here.","request_id":"req_9"}}`)
	h := newHarness(t, f)
	if code := h.run("deploy", "api"); code == 0 {
		t.Fatal("expected an error")
	}
	contains(t, h.err.String(), "Your role cannot deploy here (forbidden, request req_9)")
}

func TestServiceNamesThatRepeatAcrossEnvironmentsAreRefused(t *testing.T) {
	f := newFakeAPI(t)
	f.json("GET /v1/me", 200, meJSON)
	f.json("GET /v1/orgs/"+orgID+"/projects", 200, "["+projectJSON+"]")
	other := "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0010"
	f.json("GET /v1/projects/"+projectID+"/resources", 200,
		"["+apiResource+","+resourceJSON(other, prevEnvID, "application", "api", "running", "")+"]")
	h := newHarness(t, f)
	if code := h.run("deploy", "api"); code == 0 {
		t.Fatal("an ambiguous name was deployed")
	}
	contains(t, h.err.String(), "more than one service", "shop (production), "+apiID, "shop (staging), "+other)
	for _, c := range f.calls() {
		if c[:4] == "POST" {
			t.Fatalf("ambiguous deploy reached the API: %s", c)
		}
	}
}

func TestStatus(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.json("GET /v1/resources/"+apiID+"/deployments", 200, "["+deploymentJSON(newDepID, "healthy", "manual")+","+deploymentJSON(depID, "healthy", "push")+"]")
	h := newHarness(t, f)
	if code := h.run("status", "api"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "Service", "api", "shop (production)", "running", "https://api-shop.apps.dekka.cloud", "DEPLOYMENT", newDepID, "main at 4f2a9c1")
	if !f.called("GET /v1/resources/" + apiID + "/deployments?limit=5") {
		t.Fatalf("calls: %v", f.calls())
	}

	if code := h.run("status", "api", "-o", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	got := decode[serviceStatus](t, h.out.String())
	if got.Service.Name != "api" || len(got.Deployments) != 2 {
		t.Fatalf("status json = %+v", got)
	}
}

func rollbackAPI(t *testing.T) *fakeAPI {
	f := newFakeAPI(t).withProject()
	f.json("GET /v1/deployments/"+depID, 200, deploymentJSON(depID, "healthy", "push"))
	f.json("POST /v1/deployments/"+depID+"/rollback", 201, deploymentJSON(newDepID, "queued", "rollback"))
	return f
}

func TestRollbackAsksNamingTheServiceAndDeployment(t *testing.T) {
	f := rollbackAPI(t)
	h := newHarness(t, f)
	h.terminal = true
	h.input("n\n")
	if code := h.run("rollback", depID); code == 0 {
		t.Fatal("a declined rollback succeeded")
	}
	contains(t, h.err.String(), "Roll back api in shop (production) to deployment "+depID, "main at 4f2a9c1", "current variables", "rollback cancelled")
	if f.called("POST /v1/deployments/" + depID + "/rollback") {
		t.Fatal("a declined rollback reached the API")
	}

	h.input("y\n")
	if code := h.run("rollback", depID); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "Started deployment "+newDepID+" of api, rolling back to "+depID)
}

func TestRollbackNeedsYesWithoutATerminal(t *testing.T) {
	f := rollbackAPI(t)
	h := newHarness(t, f)
	if code := h.run("rollback", depID); code == 0 {
		t.Fatal("rolled back without confirmation")
	}
	contains(t, h.err.String(), "--yes")
	if f.called("POST /v1/deployments/" + depID + "/rollback") {
		t.Fatal("an unconfirmed rollback reached the API")
	}

	if code := h.run("rollback", depID, "--yes", "-o", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	if got := decode[api.DeploymentOut](t, h.out.String()); got.Trigger != "rollback" {
		t.Fatalf("rollback json = %+v", got)
	}
}

func TestRollbackTakesADeploymentID(t *testing.T) {
	h := newHarness(t, newFakeAPI(t))
	if code := h.run("rollback", "api", "--yes"); code == 0 {
		t.Fatal("a service name was taken as a deployment")
	}
	contains(t, h.err.String(), "deployment's id")
}
