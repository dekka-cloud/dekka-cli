package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/dekka-cloud/dekka-cli/internal/credentials"
)

const (
	testToken = "dk_test_secret"
	orgID     = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0001"
	projectID = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0002"
	prodEnvID = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0003"
	prevEnvID = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0004"
	apiID     = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0005"
	dbID      = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0006"
	depID     = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0007"
	newDepID  = "0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0008"
)

var meJSON = `{"user":{"id":"0b8f9a52-6f43-4c39-9a3a-2a1d1c7e0009","email":"ama@example.com","name":"Ama Mensah","email_verified":true,"avatar_url":null,"github_login":null},
"organizations":[{"id":"` + orgID + `","slug":"acme","name":"Acme","role":"developer"}],
"features":{},"engine":{"kind":"dekka"},"has_passkey":false,"sign_in_methods":["email"]}`

var projectJSON = `{"id":"` + projectID + `","organization_id":"` + orgID + `","name":"shop","slug":"shop","description":null,
"region_id":"gh-1","service_count":2,"status":"active","last_deployment":null,"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-01T10:00:00Z",
"environments":[{"id":"` + prodEnvID + `","project_id":"` + projectID + `","name":"production","slug":"production","is_default":true,"kind":"production","ephemeral":false,"status":"active","forked_from_id":null,"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-01T10:00:00Z"},
{"id":"` + prevEnvID + `","project_id":"` + projectID + `","name":"staging","slug":"staging","is_default":false,"kind":"standard","ephemeral":false,"status":"active","forked_from_id":null,"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-01T10:00:00Z"}]}`

func resourceJSON(id, env, kind, name, status, hostname string) string {
	host := "null"
	if hostname != "" {
		host = `"` + hostname + `"`
	}
	return `{"id":"` + id + `","project_id":"` + projectID + `","environment_id":"` + env + `","kind":"` + kind + `","name":"` + name + `","slug":"` + name + `",
"region_id":"gh-1","spec":{},"spec_version":1,"generation":1,"status":"` + status + `","status_reason":null,"observed":null,"active_operation":null,
"hostname":` + host + `,"public_domain":null,"subdomain":null,"private_hostname":null,"next_runs":null,"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-01T10:00:00Z"}`
}

var apiResource = resourceJSON(apiID, prodEnvID, "application", "api", "running", "api-shop.apps.dekka.cloud")
var resourcesJSON = "[" + apiResource + "," + resourceJSON(dbID, prodEnvID, "postgres", "db", "running", "") + "]"

func deploymentJSON(id, status, trigger string) string {
	return `{"id":"` + id + `","project_id":"` + projectID + `","resource_id":"` + apiID + `","resource_name":"api","status":"` + status + `","trigger":"` + trigger + `",
"branch":"main","commit_sha":"4f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a39","commit_message":"Fix checkout","commit_author":"Ama","image_ref":null,"rollback_of_id":null,
"config_sources":null,"effective_spec":null,"cancel_reason":null,"error":null,"build_seconds":null,"operation_id":null,"active_operation":null,
"created_by_user_id":null,"created_at":"2026-11-05T09:30:00Z","updated_at":"2026-11-05T09:30:00Z","started_at":null,"finished_at":null}`
}

// fakeAPI is a stand-in for the Dekka API that records what it was asked.
type fakeAPI struct {
	*httptest.Server
	mux *http.ServeMux

	mu       sync.Mutex
	requests []string
	bodies   map[string]string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{mux: http.NewServeMux(), bodies: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		f.bodies[r.Method+" "+r.URL.Path] = string(body)
		f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "dekka-cli/") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			writeJSON(w, http.StatusUnauthorized, `{"error":{"code":"invalid_api_key","message":"The API key is invalid, expired or revoked.","request_id":"req_1"}}`)
			return
		}
		f.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

// json answers pattern with a fixed body.
func (f *fakeAPI) json(pattern string, status int, body string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, status, body) })
}

func (f *fakeAPI) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fakeAPI) called(req string) bool {
	for _, r := range f.calls() {
		if r == req {
			return true
		}
	}
	return false
}

func (f *fakeAPI) body(req string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[req]
}

// withProject serves the organisation's one project and its resources.
func (f *fakeAPI) withProject() *fakeAPI {
	f.json("GET /v1/me", 200, meJSON)
	f.json("GET /v1/orgs/"+orgID+"/projects", 200, "["+projectJSON+"]")
	f.json("GET /v1/projects/"+projectID, 200, projectJSON)
	f.json("GET /v1/projects/"+projectID+"/resources", 200, resourcesJSON)
	f.json("GET /v1/resources/"+apiID, 200, apiResource)
	return f
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

type harness struct {
	app      *App
	out, err *bytes.Buffer
	env      map[string]string
	terminal bool
}

// newHarness runs commands against f with DEKKA_TOKEN set, unless the test
// removes it, and a mock keychain.
func newHarness(t *testing.T, f *fakeAPI) *harness {
	keyring.MockInit()
	h := &harness{out: &bytes.Buffer{}, err: &bytes.Buffer{}, env: map[string]string{
		"DEKKA_API_URL": f.URL + "/v1",
		"DEKKA_TOKEN":   testToken,
	}}
	h.app = &App{
		In:              strings.NewReader(""),
		Out:             h.out,
		Err:             h.err,
		Getenv:          func(k string) string { return h.env[k] },
		Store:           credentials.Store{Dir: t.TempDir()},
		StdinIsTerminal: func() bool { return h.terminal },
		ReadSecret:      func() (string, error) { return "", io.EOF },
		Version:         "test",
		PollInterval:    1,
	}
	return h
}

func (h *harness) run(args ...string) int {
	return h.runContext(context.Background(), args...)
}

func (h *harness) runContext(ctx context.Context, args ...string) int {
	h.out.Reset()
	h.err.Reset()
	return h.app.Run(ctx, args)
}

func (h *harness) input(s string) { h.app.In = strings.NewReader(s); h.app.in = nil }

func contains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output does not contain %q:\n%s", w, got)
		}
	}
}

func decode[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, s)
	}
	return v
}
