package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func lines(from, to int, stream string) string {
	var parts []string
	for i := from; i <= to; i++ {
		parts = append(parts, fmt.Sprintf(`{"seq":%d,"ts":"2026-11-05T09:30:%02dZ","stream":"%s","line":"line %d"}`, i, i%60, stream, i))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestRuntimeLogsPageUntilTheEnd(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.mux.HandleFunc("GET /v1/resources/"+apiID+"/logs", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("after") {
		case "0":
			// A full page means there may be more.
			var parts []string
			for i := 1; i <= pageSize; i++ {
				parts = append(parts, fmt.Sprintf(`{"seq":%d,"ts":"2026-11-05T09:30:00Z","stream":"stdout","line":"line %d"}`, i, i))
			}
			writeJSON(w, 200, fmt.Sprintf(`{"lines":[%s],"next_after":%d}`, strings.Join(parts, ","), pageSize))
		default:
			writeJSON(w, 200, fmt.Sprintf(`{"lines":%s,"next_after":%d}`, lines(pageSize+1, pageSize+2, "stderr"), pageSize+2))
		}
	})
	h := newHarness(t, f)
	if code := h.run("logs", "api"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	out := h.out.String()
	if n := strings.Count(out, "\n"); n != pageSize+2 {
		t.Fatalf("printed %d lines", n)
	}
	contains(t, out, "stdout  line 1\n", "stderr  line 1002\n")
}

func TestRuntimeLogsFollowPollsForNewLines(t *testing.T) {
	f := newFakeAPI(t).withProject()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var polls atomic.Int32
	f.mux.HandleFunc("GET /v1/resources/"+apiID+"/logs", func(w http.ResponseWriter, r *http.Request) {
		switch polls.Add(1) {
		case 1:
			writeJSON(w, 200, `{"lines":`+lines(1, 2, "stdout")+`,"next_after":2}`)
		case 2:
			writeJSON(w, 503, `{"error":{"code":"engine_unavailable","message":"The engine did not return logs.","request_id":"r"}}`)
		case 3:
			if r.URL.Query().Get("after") != "2" {
				t.Errorf("follow asked from after=%s", r.URL.Query().Get("after"))
			}
			writeJSON(w, 200, `{"lines":`+lines(3, 3, "stdout")+`,"next_after":3}`)
		default:
			cancel()
			writeJSON(w, 200, `{"lines":[],"next_after":3}`)
		}
	})
	h := newHarness(t, f)
	if code := h.runContext(ctx, "logs", "api", "--follow", "-o", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	got := strings.Split(strings.TrimSpace(h.out.String()), "\n")
	if len(got) != 3 {
		t.Fatalf("printed %q", got)
	}
	if l := decode[logLine](t, got[2]); l.Seq != 3 || l.Line != "line 3" {
		t.Fatalf("last line = %+v", l)
	}
}

func TestDeploymentLogs(t *testing.T) {
	f := newFakeAPI(t).withProject()
	f.json("GET /v1/deployments/"+depID, 200, deploymentJSON(depID, "healthy", "push"))
	f.json("GET /v1/deployments/"+depID+"/logs", 200, `{"lines":`+lines(1, 3, "build")+`,"next_after":3,"complete":true}`)
	h := newHarness(t, f)
	if code := h.run("logs", "api", "--deployment", depID); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	contains(t, h.out.String(), "build   line 1", "build   line 3")
	if !f.called("GET /v1/deployments/" + depID + "/logs?after=0&limit=1000") {
		t.Fatalf("calls: %v", f.calls())
	}
}

func TestDeploymentLogsMustBelongToTheService(t *testing.T) {
	f := newFakeAPI(t).withProject()
	other := strings.Replace(deploymentJSON(depID, "healthy", "push"), `"resource_id":"`+apiID, `"resource_id":"`+dbID, 1)
	f.json("GET /v1/deployments/"+depID, 200, other)
	h := newHarness(t, f)
	if code := h.run("logs", "api", "--deployment", depID); code == 0 {
		t.Fatal("printed another service's deployment")
	}
}

// streamAPI serves the deployment log stream, dropping the first connection
// after two lines so the CLI has to resume.
func streamAPI(t *testing.T, final string) (*fakeAPI, *atomic.Int32) {
	f := newFakeAPI(t).withProject()
	f.json("GET /v1/deployments/"+depID, 200, deploymentJSON(depID, "building", "push"))
	var conns atomic.Int32
	f.mux.HandleFunc("GET /v1/deployments/"+depID+"/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(seq int) {
			fmt.Fprintf(w, "id: %d\nevent: log\ndata: {\"seq\":%d,\"ts\":\"2026-11-05T09:30:00Z\",\"stream\":\"build\",\"line\":\"step %d\"}\n\n", seq, seq, seq)
		}
		switch conns.Add(1) {
		case 1:
			if r.URL.Query().Get("after") != "0" {
				t.Errorf("first connection asked from after=%s", r.URL.Query().Get("after"))
			}
			fmt.Fprint(w, "retry: 3000\n\n: heartbeat\n\n")
			event(1)
			event(2)
		default:
			if r.Header.Get("Last-Event-ID") != "2" || r.URL.Query().Get("after") != "2" {
				t.Errorf("reconnect resumed from Last-Event-ID %q, after=%s", r.Header.Get("Last-Event-ID"), r.URL.Query().Get("after"))
			}
			event(2) // a repeat the CLI must not print twice
			event(3)
			fmt.Fprintf(w, "event: end\ndata: {\"status\":\"%s\"}\n\n", final)
		}
	})
	return f, &conns
}

func TestFollowDeploymentStreamsAndResumes(t *testing.T) {
	f, conns := streamAPI(t, "healthy")
	h := newHarness(t, f)
	if code := h.run("logs", "api", "-d", depID, "-f"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	if conns.Load() != 2 {
		t.Fatalf("connected %d times", conns.Load())
	}
	out := h.out.String()
	if strings.Count(out, "step 2") != 1 {
		t.Fatalf("a resumed line was printed twice:\n%s", out)
	}
	contains(t, out, "step 1", "step 3")
	contains(t, h.err.String(), "finished healthy")
}

func TestFollowDeploymentFailsWhenTheDeploymentFails(t *testing.T) {
	f, _ := streamAPI(t, "failed")
	h := newHarness(t, f)
	if code := h.run("logs", "api", "-d", depID, "-f"); code != 1 {
		t.Fatalf("exit %d for a failed deployment", code)
	}
	contains(t, h.err.String(), "ended failed")
}
