// Command dekka is the Dekka command line.
package main

import (
	"os"

	"github.com/dekka-cloud/dekka-cli/internal/cli"
)

// version is set by the release build.
var version = "dev"

func main() {
	os.Exit(cli.Main(version))
}
