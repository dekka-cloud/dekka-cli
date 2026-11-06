package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"
)

func (a *App) printJSON(v any) error {
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// table prints rows under upper-case headers, aligned in columns.
func (a *App) table(headers []string, rows [][]string) error {
	w := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.ToUpper(strings.Join(headers, "\t")))
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	return w.Flush()
}

// fields prints label and value pairs, aligned.
func (a *App) fields(pairs [][2]string) error {
	w := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	for _, p := range pairs {
		if p[1] != "" {
			fmt.Fprintf(w, "%s\t%s\n", p[0], p[1])
		}
	}
	return w.Flush()
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func when(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}
