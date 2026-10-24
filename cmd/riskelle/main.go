// Command riskelle scores the risk of a code change.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/rules"
	"github.com/bobmcallan/riskelle/internal/score"
	"github.com/bobmcallan/riskelle/internal/version"
)

const usage = `usage: riskelle <command>

commands:
  estimate  score a planned change from a plan file (format: README.md, Plan file)
  version   print the riskelle version
  help      print this help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command and returns the process exit code: 0 on success,
// 1 when the command fails, 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "estimate":
		if len(args) != 2 {
			fmt.Fprintf(stderr, "riskelle: estimate takes one plan file\n%s", usage)
			return 2
		}
		return estimate(args[1], stdout, stderr)
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

// estimateReport is what estimate prints: the scored result, marked as an
// estimate of the named plan file.
type estimateReport struct {
	Kind string `json:"kind"` // always "estimate"
	Plan string `json:"plan"` // the plan file as given on the command line
	score.Result
}

// estimate scores the plan file at planPath, for the repository in the
// current directory, the way a real diff is scored: rules.Score, with the
// rules read at the plan's target branch.
func estimate(planPath string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "riskelle: estimate: %s: %v\n", planPath, err)
		return 1
	}
	file, err := os.Open(planPath)
	if err != nil {
		return fail(err)
	}
	plan, err := facts.ReadPlan(file)
	file.Close()
	if err != nil {
		return fail(err)
	}
	ctx := context.Background()
	f, err := facts.Predict(ctx, ".", plan)
	if err != nil {
		return fail(err)
	}
	res, err := rules.Score(ctx, ".", f)
	if err != nil {
		return fail(err)
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(estimateReport{Kind: "estimate", Plan: planPath, Result: res}); err != nil {
		return fail(err)
	}
	return 0
}
