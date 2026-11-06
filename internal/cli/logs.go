package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/dekka-cloud/dekka-cli/internal/api"
	"github.com/dekka-cloud/dekka-cli/internal/sse"
)

// logLine is the shape both deployment and runtime log lines share.
type logLine struct {
	Seq    int       `json:"seq"`
	Ts     time.Time `json:"ts"`
	Stream string    `json:"stream"`
	Line   string    `json:"line"`
}

const (
	pageSize = 1000
	// maxFailures is how many failed polls or reconnects in a row end a follow.
	maxFailures = 5
)

func (a *App) logsCmd() *cobra.Command {
	var sc scope
	var deployment string
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <service>",
		Short: "Print a service's runtime logs or a deployment's build and deploy logs",
		Long: "logs prints what a running service writes to standard output and standard error. With --deployment it\n" +
			"prints that deployment's build and deploy output instead.\n\n" +
			"With --follow, runtime logs keep arriving until you press Ctrl-C, and a deployment's logs stream until it\n" +
			"finishes. Following a deployment exits with status 1 when it does not end healthy, so scripts can wait on it.\n" +
			"With --output json each line is one JSON object.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			svc, err := s.service(ctx, args[0], sc)
			if err != nil {
				return err
			}
			if deployment == "" {
				return a.runtimeLogs(ctx, s, svc.Id, follow)
			}
			id, err := uuid.Parse(deployment)
			if err != nil {
				return errors.New("--deployment takes a deployment id, which dekka status <service> lists")
			}
			r, err := s.api.GetDeploymentWithResponse(ctx, id)
			if err != nil {
				return err
			}
			dep, err := result(r.HTTPResponse, r.Body, r.JSON200)
			if err != nil {
				return err
			}
			if dep.ResourceId != svc.Id {
				return fmt.Errorf("deployment %s belongs to %s, not %s", id, dep.ResourceName, svc.Name)
			}
			if follow {
				return a.followDeployment(ctx, s, id)
			}
			return a.deploymentLogs(ctx, s, id)
		},
	}
	sc.flags(cmd)
	cmd.Flags().StringVarP(&deployment, "deployment", "d", "", "a deployment id, for its build and deploy logs")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines")
	return cmd
}

func (a *App) printLine(l logLine) {
	if a.json() {
		_ = json.NewEncoder(a.Out).Encode(l)
		return
	}
	fmt.Fprintf(a.Out, "%s  %-6s  %s\n", l.Ts.Local().Format("Jan 02 15:04:05"), l.Stream, l.Line)
}

// runtimeLogs pages through GET /v1/resources/{id}/logs, and with follow polls
// it for new lines the way the dashboard does.
func (a *App) runtimeLogs(ctx context.Context, s *session, id uuid.UUID, follow bool) error {
	after, limit, failures := 0, pageSize, 0
	interval := a.PollInterval
	if interval == 0 {
		interval = 1500 * time.Millisecond
	}
	for {
		r, err := s.api.GetResourceLogsWithResponse(ctx, id, &api.GetResourceLogsParams{After: &after, Limit: &limit})
		if ctx.Err() != nil {
			return nil
		}
		var page *api.LogPage
		if err == nil {
			page, err = result(r.HTTPResponse, r.Body, r.JSON200)
		}
		if err != nil {
			var apiErr *APIError
			transient := !errors.As(err, &apiErr) || apiErr.Status >= 500
			if !follow || !transient {
				return err
			}
			if failures++; failures >= maxFailures {
				return err
			}
		} else {
			failures = 0
			for _, l := range page.Lines {
				a.printLine(logLine{Seq: l.Seq, Ts: l.Ts, Stream: l.Stream, Line: l.Line})
			}
			after = page.NextAfter
			if len(page.Lines) == limit {
				continue
			}
			if !follow {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

// deploymentLogs pages through GET /v1/deployments/{id}/logs.
func (a *App) deploymentLogs(ctx context.Context, s *session, id uuid.UUID) error {
	after, limit := 0, pageSize
	for {
		r, err := s.api.GetDeploymentLogsWithResponse(ctx, id, &api.GetDeploymentLogsParams{After: &after, Limit: &limit})
		if err != nil {
			return err
		}
		page, err := result(r.HTTPResponse, r.Body, r.JSON200)
		if err != nil {
			return err
		}
		for _, l := range page.Lines {
			a.printLine(logLine{Seq: l.Seq, Ts: l.Ts, Stream: l.Stream, Line: l.Line})
		}
		if page.Complete || len(page.Lines) < limit {
			return nil
		}
		after = page.NextAfter
	}
}

// followDeployment reads GET /v1/deployments/{id}/logs/stream, the stream the
// dashboard uses, and reconnects from the last line it printed if the
// connection drops before the end event.
func (a *App) followDeployment(ctx context.Context, s *session, id uuid.UUID) error {
	cursor, failures := 0, 0
	wait := 3 * time.Second
	for {
		status, progressed, retry, err := a.streamOnce(ctx, s, id, &cursor)
		if ctx.Err() != nil {
			return nil
		}
		if status != "" {
			fmt.Fprintf(a.Err, "Deployment %s finished %s.\n", id, status)
			if status != string(api.DeploymentStatusHealthy) {
				return fmt.Errorf("deployment %s ended %s", id, status)
			}
			return nil
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status < 500 {
			return err
		}
		if progressed {
			failures = 0
		}
		if failures++; failures >= maxFailures {
			if err == nil {
				err = errors.New("the stream kept closing before the deployment finished")
			}
			return err
		}
		switch {
		case a.PollInterval > 0:
			wait = a.PollInterval
		case retry > 0:
			wait = retry
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// streamOnce reads one connection's events. It returns the deployment's final
// status once the end event arrives.
func (a *App) streamOnce(ctx context.Context, s *session, id uuid.UUID, cursor *int) (status string, progressed bool, retry time.Duration, err error) {
	params := &api.StreamDeploymentLogsParams{After: cursor}
	if *cursor > 0 {
		last := strconv.Itoa(*cursor)
		params.LastEventId = &last
	}
	resp, err := s.stream.StreamDeploymentLogs(ctx, id, params, func(_ context.Context, req *http.Request) error {
		req.Header.Set("Accept", "text/event-stream")
		return nil
	})
	if err != nil {
		return "", false, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", false, 0, apiError(resp, body)
	}
	events := sse.NewReader(resp.Body)
	for {
		ev, err := events.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return "", progressed, events.Retry(), err
		}
		switch ev.Type {
		case "log":
			var l logLine
			if err := json.Unmarshal([]byte(ev.Data), &l); err != nil {
				return "", progressed, events.Retry(), fmt.Errorf("the stream sent a log line that is not JSON: %w", err)
			}
			if l.Seq <= *cursor {
				continue
			}
			a.printLine(l)
			*cursor = l.Seq
			progressed = true
		case "end":
			var end struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &end); err != nil || end.Status == "" {
				return "", progressed, events.Retry(), errors.New("the stream ended without the deployment's status")
			}
			return end.Status, progressed, 0, nil
		}
	}
}
