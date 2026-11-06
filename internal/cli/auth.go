package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dekka-cloud/dekka-cli/internal/api"
	"github.com/dekka-cloud/dekka-cli/internal/credentials"
)

func (a *App) loginCmd() *cobra.Command {
	var token string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save an API key for the commands to use",
		Long: "login reads an API key from the terminal, standard input or --token, checks it with the API, and saves it\n" +
			"in the system keychain. Create the key in the dashboard at " + DashboardURL + " under Settings, API keys.\n\n" +
			"Passing the key on standard input keeps it out of your shell history:\n\n" +
			"  dekka login < key.txt",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			apiURL, err := a.apiURL()
			if err != nil {
				return err
			}
			key := strings.TrimSpace(token)
			if key == "" {
				if key, err = a.readKey(); err != nil {
					return err
				}
			}
			if !strings.HasPrefix(key, "dk_") {
				return errors.New("dekka API keys start with dk_, and this one does not")
			}
			s, err := a.newSession(apiURL, key)
			if err != nil {
				return err
			}
			me, err := s.me(cmd.Context())
			if isStatus(err, http.StatusNotFound) {
				return errors.New("this key only reaches the AI gateway, so create one with the read or write scope for the CLI")
			}
			if err != nil {
				return fmt.Errorf("the API did not accept this key: %w", err)
			}
			saved, err := a.Store.Set(apiURL, key)
			if err != nil {
				return fmt.Errorf("saving the key: %w", err)
			}
			if saved.Source == credentials.SourceFile {
				path, _ := a.Store.Path()
				fmt.Fprintf(a.Err, "The system keychain is unavailable (%v), so the key is saved in %s, which only your user can read.\n", saved.KeychainErr, path)
			}
			if a.Getenv("DEKKA_TOKEN") != "" {
				fmt.Fprintln(a.Err, "DEKKA_TOKEN is set in this shell, and it takes precedence over the saved key.")
			}
			if a.json() {
				return a.printJSON(whoami{User: me.User, Organizations: me.Organizations, APIURL: apiURL, KeySource: string(saved.Source)})
			}
			fmt.Fprintf(a.Out, "Signed in as %s (%s)%s.\n", me.User.Name, me.User.Email, orgSuffix(me))
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "the API key, instead of reading it from standard input")
	return cmd
}

func (a *App) readKey() (string, error) {
	if a.StdinIsTerminal() {
		fmt.Fprintf(a.Err, "Create an API key in the dashboard at %s under Settings, API keys, and paste it here.\nAPI key: ", DashboardURL)
		key, err := a.ReadSecret()
		fmt.Fprintln(a.Err)
		return strings.TrimSpace(key), err
	}
	key, err := a.readLine()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("no key on standard input, so create one at %s and pass it with --token or on standard input", DashboardURL)
	}
	return strings.TrimSpace(key), nil
}

func orgSuffix(me *api.MeResponse) string {
	if len(me.Organizations) == 0 {
		return ""
	}
	return " in " + me.Organizations[0].Name
}

func (a *App) logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the saved API key",
		Long: "logout removes the key that dekka login saved for this API. The key itself stays valid until you revoke it\n" +
			"in the dashboard under Settings, API keys.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			apiURL, err := a.apiURL()
			if err != nil {
				return err
			}
			removed, err := a.Store.Delete(apiURL)
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(a.Out, "Removed the saved key for %s.\n", apiURL)
			} else {
				fmt.Fprintf(a.Out, "No key was saved for %s.\n", apiURL)
			}
			if a.Getenv("DEKKA_TOKEN") != "" {
				fmt.Fprintln(a.Err, "DEKKA_TOKEN is still set in this shell, so commands keep using it.")
			}
			return nil
		},
	}
}

type whoami struct {
	User          api.UserOut          `json:"user"`
	Organizations []api.MeOrganization `json:"organizations"`
	APIURL        string               `json:"api_url"`
	KeySource     string               `json:"key_source"`
}

func (a *App) whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show who the API key acts for",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			me, err := s.me(cmd.Context())
			if err != nil {
				return err
			}
			if a.json() {
				return a.printJSON(whoami{User: me.User, Organizations: me.Organizations, APIURL: s.url, KeySource: string(s.source)})
			}
			pairs := [][2]string{{"User", fmt.Sprintf("%s <%s>", me.User.Name, me.User.Email)}}
			for _, o := range me.Organizations {
				pairs = append(pairs, [2]string{"Organisation", fmt.Sprintf("%s (%s), where you are %s", o.Name, o.Slug, article(string(o.Role)))})
			}
			pairs = append(pairs, [2]string{"API", s.url}, [2]string{"Key from", string(s.source)})
			return a.fields(pairs)
		},
	}
}

func article(role string) string {
	if strings.ContainsAny(role[:1], "aeiou") {
		return "an " + role
	}
	return "a " + role
}

func (s *session) me(ctx context.Context) (*api.MeResponse, error) {
	r, err := s.api.GetMeWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	return result(r.HTTPResponse, r.Body, r.JSON200)
}

// org is the organisation the key belongs to. An API key reaches exactly one.
func (s *session) org(ctx context.Context) (api.MeOrganization, error) {
	me, err := s.me(ctx)
	if err != nil {
		return api.MeOrganization{}, err
	}
	if len(me.Organizations) == 0 {
		return api.MeOrganization{}, errors.New("this key belongs to no organisation")
	}
	return me.Organizations[0], nil
}
