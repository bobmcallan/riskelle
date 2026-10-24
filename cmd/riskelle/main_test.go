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
