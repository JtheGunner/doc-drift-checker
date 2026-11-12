// Command doc-drift-checker reports when code changed but the README passage
// describing it did not.
package main

import (
	"os"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
)

// version is overridden at build time: -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	if err := cli.NewRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}
