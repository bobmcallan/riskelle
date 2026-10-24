package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring; empty means stdout must be empty
		wantStderr string // substring; empty means stderr must be empty
	}{
		{"no arguments", nil, 2, "", "usage"},
		{"help", []string{"help"}, 0, "usage", ""},
		{"-h", []string{"-h"}, 0, "usage", ""},
		{"--help", []string{"--help"}, 0, "usage", ""},
		{"unknown command", []string{"bogus"}, 2, "", `unknown command "bogus"`},
		{"version with arguments", []string{"version", "extra"}, 2, "", "takes no arguments"},
		{"estimate without a plan", []string{"estimate"}, 2, "", "usage"},
		{"estimate with two plans", []string{"estimate", "a.json", "b.json"}, 2, "", "one plan file"},
		{"estimate with a missing plan", []string{"estimate", "no-such-plan.json"}, 1, "", "no-such-plan.json"},
		{"estimate with an unknown format", []string{"estimate", "--format", "xml", "a.json"}, 2, "", `unknown format "xml"`},
		{"estimate with a format and no plan", []string{"estimate", "--format", "markdown"}, 2, "", "one plan file"},
		{"estimate with an unknown flag", []string{"estimate", "--bogus", "a.json"}, 2, "", "-bogus"},
		{"score without refs", []string{"score"}, 2, "", "needs --base and --head"},
		{"score without head", []string{"score", "--base", "main"}, 2, "", "needs --base and --head"},
		{"score without base", []string{"score", "--head", "main"}, 2, "", "needs --base and --head"},
		{"score with an argument", []string{"score", "--base", "main", "--head", "x", "extra"}, 2, "", "takes no arguments"},
		{"score with an unknown format", []string{"score", "--format", "xml", "--base", "a", "--head", "b"}, 2, "", `unknown format "xml"`},
		{"score with an unknown flag", []string{"score", "--bogus"}, 2, "", "-bogus"},
		{"score with a flag missing its value", []string{"score", "--base"}, 2, "", "-base"},
		{"score with an unresolvable ref", []string{"score", "--base", "no-such-ref-xyz", "--head", "HEAD"}, 1, "", "no-such-ref-xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			check(t, "stdout", stdout.String(), tt.wantStdout)
			check(t, "stderr", stderr.String(), tt.wantStderr)
		})
	}
}

func check(t *testing.T, stream, got, want string) {
	t.Helper()
	if want == "" {
		if got != "" {
			t.Errorf("%s = %q, want empty", stream, got)
		}
		return
	}
	if !strings.Contains(got, want) {
		t.Errorf("%s = %q, want it to contain %q", stream, got, want)
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !regexp.MustCompile(`^riskelle \S+.*\n$`).MatchString(stdout.String()) {
		t.Errorf("stdout = %q, want \"riskelle <version>\\n\"", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}
