// Command ozddns is the single binary: it runs the daemon (REST API + embedded
// web panel) and the management CLI. Both share one application layer.
package main

import (
	"os"

	"github.com/echoface/ozddns/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
