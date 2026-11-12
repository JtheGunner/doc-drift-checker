// Command doc-drift-checker reports when code changed but the README passage
// describing it did not.
package main

import (
	"fmt"
	"os"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
)

// version is overridden at build time: -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	err := cli.NewRootCmd(version).Execute()
	if err != nil && err.Error() != "" {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	os.Exit(cli.ExitCode(err))
}
