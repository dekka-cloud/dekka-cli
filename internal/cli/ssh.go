package cli

import (
	"os"
	"os/exec"
)

// runSSH hands this terminal to the system's ssh until it exits.
func runSSH(args []string) error {
	// The arguments are a port number and root@ a validated host, so ssh can't read them as options.
	cmd := exec.Command("ssh", args...) //nolint:gosec // see above
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
