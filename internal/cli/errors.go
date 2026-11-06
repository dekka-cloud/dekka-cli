package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dekka-cloud/dekka-cli/internal/api"
)

// APIError is an error the API answered with.
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	// Hint is what to do next, when the CLI knows.
	Hint string
}

func (e *APIError) Error() string {
	if e.Hint != "" {
		return e.detail() + ". " + e.Hint
	}
	return e.detail()
}

func (e *APIError) detail() string {
	msg := strings.TrimSuffix(e.Message, ".")
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	switch {
	case e.Code != "" && e.RequestID != "":
		return fmt.Sprintf("%s (%s, request %s)", msg, e.Code, e.RequestID)
	case e.Code != "":
		return fmt.Sprintf("%s (%s)", msg, e.Code)
	}
	return fmt.Sprintf("%s (HTTP %d)", msg, e.Status)
}

func apiError(resp *http.Response, body []byte) error {
	e := &APIError{Status: resp.StatusCode}
	var env api.ErrorEnvelope
	if json.Unmarshal(body, &env) == nil {
		e.Code, e.Message, e.RequestID = env.Error.Code, env.Error.Message, env.Error.RequestId
	}
	if resp.StatusCode == http.StatusUnauthorized {
		e.Hint = "Run dekka login with a new key, or check DEKKA_TOKEN."
	}
	return e
}

// result returns the parsed 2xx body, or the API's error.
func result[T any](resp *http.Response, body []byte, parsed *T) (*T, error) {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && parsed != nil {
		return parsed, nil
	}
	return nil, apiError(resp, body)
}

func isStatus(err error, status int) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == status
}
