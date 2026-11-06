// Package cli holds the dekka commands.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dekka-cloud/dekka-cli/internal/api"
	"github.com/dekka-cloud/dekka-cli/internal/credentials"
)

const (
	// DefaultAPIURL is the public Dekka API. Every route lives under /v1.
	DefaultAPIURL = "https://api.dekka.cloud"
	// DashboardURL is where API keys are created.
	DashboardURL = "https://dekka.cloud/app"
)

// App carries what the commands read and write, so tests can replace it.
type App struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Getenv func(string) string
	Store  credentials.Store

	// StdinIsTerminal reports whether In is an interactive terminal.
	StdinIsTerminal func() bool
	// ReadSecret reads a line from the terminal without echoing it.
	ReadSecret func() (string, error)

	Version    string
	HTTPClient *http.Client
	// PollInterval, when set, replaces the wait between runtime log polls and
	// between stream reconnects.
	PollInterval time.Duration

	output string
	in     *bufio.Reader
}

// Main runs the CLI against the process's own terminal and environment.
func Main(version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	app := &App{
		In:              os.Stdin,
		Out:             os.Stdout,
		Err:             os.Stderr,
		Getenv:          os.Getenv,
		StdinIsTerminal: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		ReadSecret: func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			return string(b), err
		},
		Version: version,
	}
	return app.Run(ctx, os.Args[1:])
}

// Run executes one command line and returns the process exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	root := a.Root()
	root.SetArgs(args)
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(a.Err, "Error:", err)
		return 1
	}
	return 0
}

// Root builds the command tree.
func (a *App) Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "dekka",
		Short: "Deploy and run applications on Dekka",
		Long: "dekka deploys and runs applications on Dekka through the public API at " + DefaultAPIURL + "/v1.\n" +
			"It signs requests with an API key, so every command passes the same permission checks as the dashboard.",
		Version:       a.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if a.output != "table" && a.output != "json" {
				return fmt.Errorf("--output must be table or json, not %q", a.output)
			}
			return nil
		},
	}
	root.PersistentFlags().StringVarP(&a.output, "output", "o", "table", "output format: table or json")
	root.AddCommand(a.loginCmd(), a.logoutCmd(), a.whoamiCmd(), a.projectsCmd(), a.appsCmd())
	return root
}

func (a *App) json() bool { return a.output == "json" }

// apiURL is DEKKA_API_URL or the public API, without a trailing /v1, since
// the generated client's paths already start with it.
func (a *App) apiURL() (string, error) {
	raw := strings.TrimSpace(a.Getenv("DEKKA_API_URL"))
	if raw == "" {
		return DefaultAPIURL, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("DEKKA_API_URL is not a URL: %q", raw)
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return "", fmt.Errorf("DEKKA_API_URL must use https, except for localhost: %q", raw)
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/v1")
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

// token is DEKKA_TOKEN or the key saved by dekka login for this API.
func (a *App) token(apiURL string) (string, credentials.Source, error) {
	if t := strings.TrimSpace(a.Getenv("DEKKA_TOKEN")); t != "" {
		return t, credentials.SourceEnv, nil
	}
	t, src, err := a.Store.Get(apiURL)
	if errors.Is(err, credentials.ErrNotFound) {
		return "", "", errors.New("you are not signed in, so run dekka login or set DEKKA_TOKEN")
	}
	if err != nil {
		return "", "", fmt.Errorf("reading the saved key: %w", err)
	}
	return t, src, nil
}

// session is a signed-in API client.
type session struct {
	api    *api.ClientWithResponses
	stream *api.Client
	url    string
	source credentials.Source
}

func (a *App) session() (*session, error) {
	apiURL, err := a.apiURL()
	if err != nil {
		return nil, err
	}
	tok, src, err := a.token(apiURL)
	if err != nil {
		return nil, err
	}
	s, err := a.newSession(apiURL, tok)
	if err != nil {
		return nil, err
	}
	s.source = src
	return s, nil
}

func (a *App) newSession(apiURL, token string) (*session, error) {
	base := a.HTTPClient
	if base == nil {
		base = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}}
	}
	timed := *base
	timed.Timeout = 60 * time.Second
	sign := api.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", fmt.Sprintf("dekka-cli/%s (%s/%s)", a.Version, runtime.GOOS, runtime.GOARCH))
		return nil
	})
	c, err := api.NewClientWithResponses(apiURL, api.WithHTTPClient(&timed), sign)
	if err != nil {
		return nil, err
	}
	stream, err := api.NewClient(apiURL, api.WithHTTPClient(base), sign)
	if err != nil {
		return nil, err
	}
	return &session{api: c, stream: stream, url: apiURL}, nil
}

// confirm asks a yes-or-no question on the terminal.
func (a *App) confirm(question string) (bool, error) {
	if !a.StdinIsTerminal() {
		return false, errors.New("this needs confirmation, so run it in a terminal or pass --yes")
	}
	fmt.Fprintf(a.Err, "%s [y/N] ", question)
	answer, err := a.readLine()
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func (a *App) readLine() (string, error) {
	if a.in == nil {
		a.in = bufio.NewReader(a.In)
	}
	line, err := a.in.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}
