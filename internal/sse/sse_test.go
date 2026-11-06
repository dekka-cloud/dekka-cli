package sse

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReader(t *testing.T) {
	stream := "retry: 3000\n\n" +
		": heartbeat\n\n" +
		"id: 1\nevent: log\ndata: {\"seq\":1}\n\n" +
		"data: first\ndata: second\n\n" +
		"event: end\ndata: {\"status\":\"healthy\"}\n\n" +
		"event: log\ndata: cut off"
	r := NewReader(strings.NewReader(stream))

	want := []Event{
		{Type: "log", ID: "1", Data: `{"seq":1}`},
		{Type: "message", ID: "1", Data: "first\nsecond"},
		{Type: "end", ID: "1", Data: `{"status":"healthy"}`},
	}
	for i, w := range want {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		if got != w {
			t.Fatalf("event %d = %+v, want %+v", i, got, w)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("the unterminated event was dispatched: %v", err)
	}
	if r.Retry() != 3*time.Second {
		t.Fatalf("Retry = %v", r.Retry())
	}
}
