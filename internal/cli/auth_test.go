package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/dekka-cloud/dekka-cli/internal/credentials"
)

func TestLoginReadsTheKeyFromStdinAndSavesItInTheKeychain(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	delete(h.env, "DEKKA_TOKEN")
	h.input(testToken + "\n")

	if code := h.run("login"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "Signed in as Ama Mensah (ama@example.com) in Acme.")
	key, src, err := h.app.Store.Get(f.URL)
	if err != nil || key != testToken || src != credentials.SourceKeychain {
		t.Fatalf("saved key = %q from %q (%v)", key, src, err)
	}
	if !f.called("GET /v1/me") {
		t.Fatal("login did not verify the key")
	}
}

func TestLoginOnATerminalPointsToTheDashboard(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	delete(h.env, "DEKKA_TOKEN")
	h.terminal = true
	h.app.ReadSecret = func() (string, error) { return testToken, nil }

	if code := h.run("login"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.err.String(), "https://dekka.cloud/app", "Settings, API keys")
}

func TestLoginRefusesAKeyTheAPIRejects(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	delete(h.env, "DEKKA_TOKEN")

	if code := h.run("login", "--token", "dk_wrong_key"); code == 0 {
		t.Fatal("login accepted a rejected key")
	}
	contains(t, h.err.String(), "The API key is invalid, expired or revoked", "invalid_api_key", "req_1")
	if _, _, err := h.app.Store.Get(f.URL); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("a rejected key was saved: %v", err)
	}
}

func TestLoginRefusesSomethingThatIsNotAKey(t *testing.T) {
	f := newFakeAPI(t)
	h := newHarness(t, f)
	if code := h.run("login", "--token", "ghp_abc"); code == 0 {
		t.Fatal("login accepted a non-Dekka token")
	}
	if len(f.calls()) != 0 {
		t.Fatalf("the token was sent to the API: %v", f.calls())
	}
}

func TestLoginExplainsAGatewayOnlyKey(t *testing.T) {
	f := newFakeAPI(t)
	f.json("GET /v1/me", 404, `{"error":{"code":"not_found","message":"Not found.","request_id":"req_2"}}`)
	h := newHarness(t, f)
	if code := h.run("login", "--token", testToken); code == 0 {
		t.Fatal("login accepted a gateway-only key")
	}
	contains(t, h.err.String(), "only reaches the AI gateway")
}

func TestLoginFallsBackToAPrivateFile(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	keyring.MockInitWithError(errors.New("no secret service"))

	if code := h.run("login", "--token", testToken); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	path, _ := h.app.Store.Path()
	contains(t, h.err.String(), "keychain is unavailable (no secret service)", path, "DEKKA_TOKEN is set")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials file: %v, %v", info, err)
	}
}

func TestCommandsUseTheSavedKeyAndLogoutRemovesIt(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	delete(h.env, "DEKKA_TOKEN")
	if code := h.run("whoami"); code == 0 || !strings.Contains(h.err.String(), "dekka login") {
		t.Fatalf("whoami without a key: exit %d, %s", code, h.err)
	}
	if _, err := h.app.Store.Set(f.URL, testToken); err != nil {
		t.Fatal(err)
	}
	if code := h.run("whoami"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "the system keychain")

	if code := h.run("logout"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "Removed the saved key for "+f.URL)
	if code := h.run("logout"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "No key was saved")
}

func TestWhoami(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	if code := h.run("whoami"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "Ama Mensah <ama@example.com>", "Acme (acme), where you are a developer", f.URL, "DEKKA_TOKEN")

	if code := h.run("whoami", "-o", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	got := decode[whoami](t, h.out.String())
	if got.User.Email != "ama@example.com" || got.Organizations[0].Slug != "acme" || got.APIURL != f.URL {
		t.Fatalf("whoami json = %+v", got)
	}
}

func TestAPIURL(t *testing.T) {
	for raw, want := range map[string]string{
		"":                            DefaultAPIURL,
		"https://api.dekka.cloud/v1":  DefaultAPIURL,
		"https://api.dekka.cloud/v1/": DefaultAPIURL,
		"http://localhost:8000":       "http://localhost:8000",
	} {
		a := &App{Getenv: func(string) string { return raw }}
		got, err := a.apiURL()
		if err != nil || got != want {
			t.Errorf("apiURL(%q) = %q, %v", raw, got, err)
		}
	}
	a := &App{Getenv: func(string) string { return "http://api.example.com" }}
	if _, err := a.apiURL(); err == nil {
		t.Error("a plain-HTTP API URL on another host was accepted")
	}
}

func TestUnknownOutputFormat(t *testing.T) {
	h := newHarness(t, newFakeAPI(t))
	if code := h.run("whoami", "-o", "yaml"); code == 0 {
		t.Fatal("yaml was accepted")
	}
}
