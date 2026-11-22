package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

// browserLogin signs this machine in through the dashboard and returns the
// API key the person's approval made for it.
//
// The API gives the CLI two codes. The device code stays here and collects
// the key. The short code is printed, and the person checks it against the
// one the dashboard shows before they approve, so a link sent by someone
// else cannot sign that person's terminal in.
func (a *App) browserLogin(ctx context.Context, apiURL string, open bool) (string, error) {
	s, err := a.newSession(apiURL, "")
	if err != nil {
		return "", err
	}
	body := api.StartCliLoginJSONRequestBody{}
	if a.Hostname != nil {
		if name, err := a.Hostname(); err == nil && name != "" {
			body.DeviceName = &name
		}
	}
	r, err := s.api.StartCliLoginWithResponse(ctx, body)
	if err != nil {
		return "", err
	}
	started, err := result(r.HTTPResponse, r.Body, r.JSON201)
	if err != nil {
		return "", fmt.Errorf("starting the sign-in: %w", err)
	}
	link, err := url.Parse(started.VerificationUrl)
	if err != nil || (link.Scheme != "https" && link.Scheme != "http") || link.Host == "" {
		return "", fmt.Errorf("the API sent a sign-in link that is not a web address: %q", started.VerificationUrl)
	}

	fmt.Fprintf(a.Err, "Your code is %s.\n", started.UserCode)
	if open && a.OpenBrowser != nil && a.OpenBrowser(link.String()) == nil {
		fmt.Fprintf(a.Err, "Your browser is opening %s, where you approve this sign-in if it shows the same code.\n", link)
	} else {
		fmt.Fprintf(a.Err, "Open %s in a browser and approve this sign-in if it shows the same code.\n", link)
	}
	fmt.Fprintln(a.Err, "Waiting for your approval.")

	wait := time.Duration(started.Interval) * time.Second
	if a.PollInterval > 0 {
		wait = a.PollInterval
	}
	if wait <= 0 {
		wait = 3 * time.Second
	}
	deadline := time.Now().Add(time.Duration(started.ExpiresIn) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return "", errors.New("the sign-in was cancelled before it was approved")
		case <-time.After(wait):
		}
		c, err := s.api.CollectCliLoginWithResponse(ctx, api.CollectCliLoginJSONRequestBody{DeviceCode: started.DeviceCode})
		if err != nil {
			// A dropped connection is not an answer, so ask again until the code runs out.
			if ctx.Err() == nil && time.Now().Before(deadline) {
				continue
			}
			return "", err
		}
		got, err := result(c.HTTPResponse, c.Body, c.JSON200)
		if err == nil {
			return got.Key, nil
		}
		var e *APIError
		if !errors.As(err, &e) {
			return "", err
		}
		switch e.Code {
		case "authorization_pending":
			if time.Now().After(deadline) {
				return "", errors.New("the code expired before the sign-in was approved, so run dekka login again")
			}
		case "access_denied":
			return "", errors.New("the sign-in was refused in the dashboard")
		case "expired_token":
			return "", errors.New("the code expired before the sign-in was approved, so run dekka login again")
		default:
			return "", fmt.Errorf("collecting the key: %w", err)
		}
	}
}

// browserOpener returns how to open a link on this machine, or nil where a
// browser would open somewhere the person is not looking: over SSH, or on a
// Linux machine with no display.
func browserOpener(getenv func(string) string) func(string) error {
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return nil
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
			return nil
		}
		name = "xdg-open"
	}
	if _, err := exec.LookPath(name); err != nil {
		return nil
	}
	return func(link string) error {
		cmd := exec.Command(name, append(args, link)...) //nolint:gosec // the link is checked to be a web address, and no shell runs
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
}
