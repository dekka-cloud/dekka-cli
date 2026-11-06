// Package sse reads a server-sent event stream as the HTML specification
// describes it, which is the format the Dekka API streams deployment logs in.
package sse

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// Event is one dispatched event. Type is "message" when the stream named none.
type Event struct {
	Type string
	ID   string
	Data string
}

// Reader yields events from a stream.
type Reader struct {
	scan    *bufio.Scanner
	lastID  string
	retry   time.Duration
	hasData bool
}

// NewReader reads events from r. Lines can be up to 1 MiB long.
func NewReader(r io.Reader) *Reader {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 64*1024), 1<<20)
	return &Reader{scan: scan}
}

// Retry is the reconnection delay the server last asked for, or zero.
func (r *Reader) Retry() time.Duration { return r.retry }

// Next returns the next event, or io.EOF when the stream ends cleanly.
// An event that was still being read when the stream ended is dropped, as
// the specification requires.
func (r *Reader) Next() (Event, error) {
	var typ string
	var data strings.Builder
	r.hasData = false
	for r.scan.Scan() {
		line := r.scan.Text()
		if line == "" {
			if !r.hasData {
				typ = ""
				continue
			}
			if typ == "" {
				typ = "message"
			}
			return Event{Type: typ, ID: r.lastID, Data: strings.TrimSuffix(data.String(), "\n")}, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			typ = value
		case "data":
			data.WriteString(value)
			data.WriteByte('\n')
			r.hasData = true
		case "id":
			if !strings.ContainsRune(value, 0) {
				r.lastID = value
			}
		case "retry":
			if ms, err := strconv.Atoi(value); err == nil && ms >= 0 {
				r.retry = time.Duration(ms) * time.Millisecond
			}
		}
	}
	if err := r.scan.Err(); err != nil {
		return Event{}, err
	}
	return Event{}, io.EOF
}
