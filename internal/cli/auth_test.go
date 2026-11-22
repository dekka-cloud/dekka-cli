package cli

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
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

func TestLoginWithKeyOnATerminalPointsToTheDashboard(t *testing.T) {
	f := newFakeAPI(t).withProject()
	h := newHarness(t, f)
	delete(h.env, "DEKKA_TOKEN")
	h.terminal = true
	h.app.ReadSecret = func() (string, error) { return testToken, nil }

	if code := h.run("login", "--with-key"); code != 0 {
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

const startedJSON = `{"device_code":"device-secret","user_code":"BCDF-GHJK","verification_url":"https://dekka.cloud/cli?code=BCDF-GHJK","expires_in":600,"interval":3}`

// browserSignIn serves a sign-in that is approved after the CLI has asked
// `pending` times, and returns how many times it was asked.
func browserSignIn(f *fakeAPI, pending int32, then func(http.ResponseWriter)) *atomic.Int32 {
	asks := &atomic.Int32{}
	f.json("POST /v1/cli/logins", 201, startedJSON)
	f.mux.HandleFunc("POST /v1/cli/logins/token", func(w http.ResponseWriter, _ *http.Request) {
		if asks.Add(1) <= pending {
			writeJSON(w, 400, `{"error":{"code":"authorization_pending","message":"Waiting for the sign-in to be approved in the dashboard.","request_id":"req_2"}}`)
			return
		}
		then(w)
	})
	return asks
}

func TestLoginOnATerminalSignsInThroughTheBrowser(t *testing.T) {
	f := newFakeAPI(t).withProject()
	asks := browserSignIn(f, 2, func(w http.ResponseWriter) {
		writeJSON(w, 200, `{"key":"`+testToken+`","organization":{"id":"`+orgID+`","name":"Acme","slug":"acme"}}`)
	})
	h := newHarness(t, f)
	delete(h.env, "DEKKA_TOKEN")
	h.terminal = true
	var opened string
	h.app.OpenBrowser = func(link string) error { opened = link; return nil }
	h.app.Hostname = func() (string, error) { return "ama-laptop", nil }

	if code := h.run("login"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	if opened != "https://dekka.cloud/cli?code=BCDF-GHJK" {
		t.Fatalf("opened %q", opened)
	}
	contains(t, h.err.String(), "Your code is BCDF-GHJK.", "Your browser is opening https://dekka.cloud/cli?code=BCDF-GHJK", "Waiting for your approval.")
	contains(t, f.body("POST /v1/cli/logins"), `"device_name":"ama-laptop"`)
	contains(t, f.body("POST /v1/cli/logins/token"), `"device_code":"device-secret"`)
	if asks.Load() != 3 {
		t.Fatalf("asked for the key %d times", asks.Load())
	}
	contains(t, h.out.String(), "Signed in as Ama Mensah (ama@example.com) in Acme.")
	if key, _, err := h.app.Store.Get(f.URL); err != nil || key != testToken {
		t.Fatalf("saved key = %q (%v)", key, err)
	}
	// The device code is the CLI's secret, and the key is never printed.
	for _, secret := range []string{"device-secret", testToken} {
		if strings.Contains(h.out.String()+h.err.String(), secret) {
			t.Fatalf("%q was printed", secret)
		}
	}
}

func TestLoginPrintsTheLinkWhereThereIsNoBrowser(t *testing.T) {
	for name, arrange := range map[string]func(*harness) []string{
		"no browser on this machine": func(h *harness) []string { h.app.OpenBrowser = nil; return []string{"login"} },
		"the browser fails to open": func(h *harness) []string {
			h.app.OpenBrowser = func(string) error { return errors.New("no display") }
			return []string{"login"}
		},
		"--no-browser": func(h *harness) []string {
			h.app.OpenBrowser = func(string) error { t.Error("the browser was opened"); return nil }
			return []string{"login", "--no-browser"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeAPI(t).withProject()
			browserSignIn(f, 0, func(w http.ResponseWriter) {
				writeJSON(w, 200, `{"key":"`+testToken+`","organization":{"id":"`+orgID+`","name":"Acme","slug":"acme"}}`)
			})
			h := newHarness(t, f)
			delete(h.env, "DEKKA_TOKEN")
			h.terminal = true
			if code := h.run(arrange(h)...); code != 0 {
				t.Fatalf("exit %d: %s", code, h.err)
			}
			contains(t, h.err.String(), "Open https://dekka.cloud/cli?code=BCDF-GHJK in a browser and approve this sign-in if it shows the same code.")
		})
	}
}

func TestLoginStopsWhenTheSignInIsRefusedOrExpires(t *testing.T) {
	for code, want := range map[string]string{
		"access_denied": "the sign-in was refused in the dashboard",
		"expired_token": "the code expired before the sign-in was approved, so run dekka login again",
	} {
		t.Run(code, func(t *testing.T) {
			f := newFakeAPI(t).withProject()
			browserSignIn(f, 1, func(w http.ResponseWriter) {
				writeJSON(w, 400, `{"error":{"code":"`+code+`","message":"Over.","request_id":"req_3"}}`)
			})
			h := newHarness(t, f)
			delete(h.env, "DEKKA_TOKEN")
			h.terminal = true
			if exit := h.run("login"); exit == 0 {
				t.Fatal("login succeeded")
			}
			contains(t, h.err.String(), want)
			if _, _, err := h.app.Store.Get(f.URL); !errors.Is(err, credentials.ErrNotFound) {
				t.Fatalf("a key was saved: %v", err)
			}
		})
	}
}

func TestLoginRefusesASignInLinkThatIsNotAWebAddress(t *testing.T) {
	f := newFakeAPI(t)
	f.json("POST /v1/cli/logins", 201, `{"device_code":"d","user_code":"BCDF-GHJK","verification_url":"file:///etc/passwd","expires_in":600,"interval":3}`)
	h := newHarness(t, f)
	delete(h.env, "DEKKA_TOKEN")
	h.terminal = true
	h.app.OpenBrowser = func(string) error { t.Error("the browser was opened"); return nil }
	if code := h.run("login"); code == 0 {
		t.Fatal("login opened a link that is not a web address")
	}
	contains(t, h.err.String(), "not a web address")
}
