// Command jsonindent rewrites a JSON file with two-space indentation and a
// trailing newline, keeping its keys in their original order, so the
// vendored OpenAPI document diffs line by line.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: jsonindent <file>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	out.WriteByte('\n')
	return os.WriteFile(path, out.Bytes(), 0o644)
}
