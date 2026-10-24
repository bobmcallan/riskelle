// Command riskelle scores the risk of a code change.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/bobmcallan/riskelle/internal/version"
)

const usage = `usage: riskelle <command>

commands:
  version   print the riskelle version
  help      print this help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command and returns the process exit code: 0 on success,
// 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		if len(args) > 1 {
			fmt.Fprintf(stderr, "riskelle: version takes no arguments\n%s", usage)
			return 2
		}
		fmt.Fprintf(stdout, "riskelle %s\n", version.Get())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "riskelle: unknown command %q\n%s", args[0], usage)
		return 2
	}
}
