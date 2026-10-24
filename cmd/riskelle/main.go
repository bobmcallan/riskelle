// Command riskelle scores the risk of a code change.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/rules"
	"github.com/bobmcallan/riskelle/internal/version"
)

const usage = `usage: riskelle <command>

commands:
  score [--format json|markdown] --base <ref> --head <ref>
            score the change from the merge base of base and head to head
  estimate [--format json|markdown] <plan.json>
            score a planned change from a plan file (format: README.md, Plan file)
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
	case "score":
		return scoreCmd(args[1:], stdout, stderr)
	case "estimate":
		return estimateCmd(args[1:], stdout, stderr)
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

// flags parses a command's flags, with --format shared by every command that
// prints a report. It returns the format, or ok false after printing the
// usage error.
func flags(fs *flag.FlagSet, args []string, stderr io.Writer) (format string, ok bool) {
	fs.SetOutput(io.Discard)
	fs.StringVar(&format, "format", report.Formats[0], "")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "riskelle: %s: %v\n%s", fs.Name(), err, usage)
		return "", false
	}
	if !slices.Contains(report.Formats, format) {
		fmt.Fprintf(stderr, "riskelle: %s: unknown format %q (want %s)\n%s",
			fs.Name(), format, strings.Join(report.Formats, " or "), usage)
		return "", false
	}
	return format, true
}

// scoreCmd scores the real diff, from the merge base of base and head to
// head, of the repository in the current directory, with the rules read at
// the resolved base (the target branch tip) by rules.Score.
func scoreCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	base := fs.String("base", "", "")
	head := fs.String("head", "", "")
	format, ok := flags(fs, args, stderr)
	if !ok {
		return 2
	}
	if *base == "" || *head == "" {
		fmt.Fprintf(stderr, "riskelle: score needs --base and --head\n%s", usage)
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "riskelle: score takes no arguments\n%s", usage)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "riskelle: score: %v\n", err)
		return 1
	}
	ctx := context.Background()
	f, err := facts.Collect(ctx, ".", *base, *head)
	if err != nil {
		return fail(err)
	}
	res, err := rules.Score(ctx, ".", f)
	if err != nil {
		return fail(err)
	}
	r := report.New(report.KindScore, res)
	r.Base, r.Head = f.Base, f.Head
	if err := report.Write(stdout, format, r); err != nil {
		return fail(err)
	}
	return 0
}

// estimateCmd scores the plan file, for the repository in the current
// directory, the way a real diff is scored: rules.Score, with the rules read
// at the plan's target branch.
func estimateCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("estimate", flag.ContinueOnError)
	format, ok := flags(fs, args, stderr)
	if !ok {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "riskelle: estimate takes one plan file\n%s", usage)
		return 2
	}
	planPath := fs.Arg(0)
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
	r := report.New(report.KindEstimate, res)
	r.Plan = planPath
	if err := report.Write(stdout, format, r); err != nil {
		return fail(err)
	}
	return 0
}
