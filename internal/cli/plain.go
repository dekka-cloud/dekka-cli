package cli

import (
	"io"
	"unicode/utf8"
)

// plainWriter drops terminal control characters from what the CLI prints.
// Names, log lines and messages come from the API, and some of them were
// written by other people in the organisation, so an escape sequence among
// them must not reach the terminal, where it could rewrite earlier lines or
// retitle the window. Newlines and tabs pass through; the CLI prints no
// control sequences of its own.
type plainWriter struct{ w io.Writer }

func plain(w io.Writer) io.Writer {
	if _, ok := w.(plainWriter); ok || w == nil {
		return w
	}
	return plainWriter{w}
}

func (p plainWriter) Write(b []byte) (int, error) {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		switch {
		case r == '\n' || r == '\t':
			out = append(out, b[i])
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
		case r == utf8.RuneError && size == 1:
			out = utf8.AppendRune(out, utf8.RuneError)
		default:
			out = append(out, b[i:i+size]...)
		}
		i += size
	}
	if _, err := p.w.Write(out); err != nil {
		return 0, err
	}
	return len(b), nil
}
