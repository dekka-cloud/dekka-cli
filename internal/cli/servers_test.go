package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
)

const (
	serverID = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0010"
	keyID    = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0011"
)

func serverJSON(state, ipv4 string, busy bool) string {
	ip, op := "null", "null"
	if ipv4 != "" {
		ip = `"` + ipv4 + `"`
	}
	if busy {
		op = `{"id":"0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0012","type":"vm.provision","state":"running","progress":0.5,"phase":null}`
	}
	return `{"id":"` + serverID + `","name":"web-1","project_id":"` + projectID + `","environment_id":"` + prodEnvID + `","region_id":"gh-1",
"runtime_profile":"kvm-linux-v1","isolation":"kvm","state":"` + state + `","size_id":"server-seed-kvm-v4","pending_size_id":null,
"image_id":"kvm-ubuntu-24.04","ipv4":` + ip + `,"ipv6":null,"private_ipv4":null,"hostname":"web-1","booted_at":null,"hourly_price_minor":32.739726,
"ssh_key_fingerprints":["SHA256:abc"],"shell":true,"status_reason":null,"active_operation":` + op + `,"created_at":"2026-11-24T01:00:00Z"}`
}

var serverResource = `{"id":"` + serverID + `","project_id":"` + projectID + `","environment_id":"` + prodEnvID + `","kind":"virtual_machine","name":"web-1","slug":"web-1",
"region_id":"gh-1","spec":{},"spec_version":1,"generation":1,"status":"running","status_reason":null,
"observed":{"endpoints":[{"kind":"ssh","hostname":"102.68.83.72","port":20001}],"engine_kind":"dekka","generation":1,"health":null,"observed_at":"2026-11-24T01:01:00Z","status":"running"},
"active_operation":null,"hostname":null,"public_domain":null,"subdomain":null,"private_hostname":null,"next_runs":null,"created_at":"2026-11-24T01:00:00Z","updated_at":"2026-11-24T01:01:00Z"}`

var keysJSON = `[{"id":"` + keyID + `","name":"laptop","fingerprint":"SHA256:abc","key_type":"ssh-ed25519","comment":null,"public_key":"ssh-ed25519 AAAA laptop","created_at":"2026-11-20T10:00:00Z"}]`

// withServer serves the project, its one server and the organisation's keys.
func (f *fakeAPI) withServer(state string) *fakeAPI {
	f.withProject()
	f.json("GET /v1/projects/"+projectID+"/servers", 200, "["+serverJSON(state, "192.0.2.10", false)+"]")
	f.json("GET /v1/servers/"+serverID, 200, serverJSON(state, "192.0.2.10", false))
	f.json("GET /v1/resources/"+serverID, 200, serverResource)
	f.json("GET /v1/orgs/"+orgID+"/ssh-keys", 200, keysJSON)
	return f
}

func TestCreateWaitsForTheServerAndPrintsHowToReachIt(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.json("GET /v1/orgs/"+orgID+"/ssh-keys", 200, keysJSON)
	f.json("GET /v1/resources/"+serverID, 200, serverResource)
	f.json("POST /v1/projects/"+projectID+"/servers", 201, serverJSON("provisioning", "", true))
	var polls atomic.Int32
	f.mux.HandleFunc("GET /v1/servers/"+serverID, func(w http.ResponseWriter, _ *http.Request) {
		if polls.Add(1) < 3 {
			writeJSON(w, 200, serverJSON("provisioning", "", true))
			return
		}
		writeJSON(w, 200, serverJSON("running", "192.0.2.10", false))
	})
	h := newHarness(t, f)

	code := h.run("servers", "create", "web-1", "-p", "shop", "--size", "server-seed-kvm-v4", "--image", "kvm-ubuntu-24.04", "--ssh-key", "laptop", "--wait")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	body := decode[map[string]any](t, f.body("POST /v1/projects/"+projectID+"/servers"))
	if body["ssh_key_ids"].([]any)[0] != keyID || body["size_id"] != "server-seed-kvm-v4" || body["environment_id"] != nil {
		t.Fatalf("create sent %v", body)
	}
	contains(t, h.err.String(), "Creating web-1 in shop.", "web-1 is provisioning")
	contains(t, h.out.String(), "web-1 is running.", "Connect with: ssh -p 20001 root@102.68.83.72")
}

func TestCreateNeedsAnSSHKey(t *testing.T) {
	h := newHarness(t, newFakeAPI(t).withServer("running"))
	if code := h.run("servers", "create", "web-1", "--size", "s", "--image", "i"); code == 0 {
		t.Fatal("expected an error")
	}
	contains(t, h.err.String(), "--ssh-key", "dekka ssh-keys list")
}

func TestListShowsServers(t *testing.T) {
	h := newHarness(t, newFakeAPI(t).withServer("running"))
	if code := h.run("servers", "list"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "web-1", "running", "server-seed-kvm-v4", "192.0.2.10", "shop (production)", serverID)
}

func TestSSHRunsTheSystemSSHOrPrintsTheCommand(t *testing.T) {
	h := newHarness(t, newFakeAPI(t).withServer("running"))
	var ran []string
	h.app.RunSSH = func(args []string) error { ran = args; return nil }
	if code := h.run("servers", "ssh", "web-1"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	if !reflect.DeepEqual(ran, []string{"-p", "20001", "root@102.68.83.72"}) {
		t.Fatalf("ran ssh %v", ran)
	}
	if code := h.run("servers", "ssh", "web-1", "--print"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "ssh -p 20001 root@102.68.83.72")
}

func TestSSHExplainsAServerThatIsNotRunning(t *testing.T) {
	h := newHarness(t, newFakeAPI(t).withServer("stopped"))
	if code := h.run("servers", "ssh", "web-1"); code == 0 {
		t.Fatal("expected an error")
	}
	contains(t, h.err.String(), "web-1 is stopped")
}

func TestDeleteAsksUnlessToldYes(t *testing.T) {
	f := newFakeAPI(t).withServer("running")
	f.json("DELETE /v1/servers/"+serverID, 202, serverJSON("deleting", "192.0.2.10", true))
	h := newHarness(t, f)
	h.terminal = true
	h.input("n\n")
	if code := h.run("servers", "delete", "web-1"); code == 0 {
		t.Fatal("expected the delete to be cancelled")
	}
	contains(t, h.err.String(), "Delete web-1 in shop (production)", "delete cancelled")
	for _, c := range f.calls() {
		if c[:6] == "DELETE" {
			t.Fatalf("sent %s after being told no", c)
		}
	}
	if code := h.run("servers", "delete", "web-1", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "Deleting web-1")
	want := "DELETE /v1/servers/" + serverID + "?confirm=web-1"
	if calls := f.calls(); calls[len(calls)-1] != want {
		t.Fatalf("last call %s, want %s", calls[len(calls)-1], want)
	}
}

func TestRebuildConfirmsWithTheServersName(t *testing.T) {
	f := newFakeAPI(t).withServer("running")
	f.json("POST /v1/servers/"+serverID+"/rebuild", 202, serverJSON("provisioning", "192.0.2.10", true))
	h := newHarness(t, f)
	if code := h.run("servers", "rebuild", "web-1", "--image", "kvm-debian-12", "-y"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	body := decode[map[string]string](t, f.body("POST /v1/servers/"+serverID+"/rebuild"))
	if body["confirm"] != "web-1" || body["image_id"] != "kvm-debian-12" {
		t.Fatalf("rebuild sent %v", body)
	}
}

func TestAddingAnSSHKeyRefusesAPrivateKey(t *testing.T) {
	f := newFakeAPI(t).withServer("running")
	h := newHarness(t, f)
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, []byte("-----BEGIN OPENSSH PRIV"+"ATE KEY-----\nabc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("ssh-keys", "add", "laptop", path); code == 0 {
		t.Fatal("expected an error")
	}
	contains(t, h.err.String(), "private key", ".pub")
	for _, c := range f.calls() {
		if c[:4] == "POST" {
			t.Fatalf("sent %s", c)
		}
	}
}

func TestSSHRefusesAnAddressThatCouldBeReadAsAnOption(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.json("GET /v1/projects/"+projectID+"/servers", 200, "["+serverJSON("running", "192.0.2.10", false)+"]")
	f.json("GET /v1/resources/"+serverID, 200, `{"id":"`+serverID+`","project_id":"`+projectID+`","environment_id":"`+prodEnvID+`","kind":"virtual_machine","name":"web-1","slug":"web-1",
"region_id":"gh-1","spec":{},"spec_version":1,"generation":1,"status":"running","status_reason":null,
"observed":{"endpoints":[{"kind":"ssh","hostname":"x -oProxyCommand=touch","port":22}],"engine_kind":"dekka","generation":1,"health":null,"observed_at":"2026-11-24T01:01:00Z","status":"running"},
"active_operation":null,"hostname":null,"public_domain":null,"subdomain":null,"private_hostname":null,"next_runs":null,"created_at":"2026-11-24T01:00:00Z","updated_at":"2026-11-24T01:01:00Z"}`)
	h := newHarness(t, f)
	h.app.RunSSH = func(args []string) error { t.Fatalf("ran ssh %v", args); return nil }
	if code := h.run("servers", "ssh", "web-1"); code == 0 {
		t.Fatal("expected an error")
	}
	contains(t, h.err.String(), "won't use")
}
