package facts

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestReadPlan(t *testing.T) {
	valid := `{"target": "main", "branch_age_days": 2, "files": [
		{"path": "a/b.go", "added": 10},
		{"path": "c.go", "added": 1, "removed": 3}]}`
	got, err := ReadPlan(strings.NewReader(valid))
	if err != nil {
		t.Fatalf("ReadPlan(valid): %v", err)
	}
	want := Plan{Target: "main", BranchAgeDays: 2, Files: []PlannedFile{
		{Path: "a/b.go", Added: 10}, {Path: "c.go", Added: 1, Removed: 3},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadPlan(valid) = %+v, want %+v", got, want)
	}

	tests := []struct {
		name, plan string
		want       []string // every one must appear in the error
	}{
		{"empty file", "  \n", []string{"empty plan file"}},
		{"not JSON", "{", []string{"not valid JSON"}},
		{"unknown field", `{"target":"main","files":[{"path":"a","added":1}],"foo":1}`, []string{"not valid JSON", `unknown field "foo"`}},
		{"trailing data", `{"target":"main","files":[{"path":"a","added":1}]} {}`, []string{"data after the plan object"}},
		{"no target", `{"files":[{"path":"a","added":1}]}`, []string{`missing "target"`}},
		{"negative age", `{"target":"main","branch_age_days":-1,"files":[{"path":"a","added":1}]}`, []string{`"branch_age_days" is negative`}},
		{"no files", `{"target":"main"}`, []string{`missing "files"`}},
		{"empty files", `{"target":"main","files":[]}`, []string{`"files" is empty`}},
		{"no path", `{"target":"main","files":[{"added":1}]}`, []string{`files[0]: missing "path"`}},
		{"unclean paths", `{"target":"main","files":[{"path":"/a","added":1},{"path":"a\\b","added":1},{"path":"a/./b","added":1},{"path":"../a","added":1},{"path":".","added":1}]}`,
			[]string{`files[0] "/a": not a clean`, `files[1] "a\\b": not a clean`, `files[2] "a/./b": not a clean`, `files[3] "../a": not a clean`, `files[4] ".": not a clean`}},
		{"duplicate", `{"target":"main","files":[{"path":"a","added":1},{"path":"a","removed":1}]}`, []string{`files[1] "a": duplicate path`}},
		{"no counts", `{"target":"main","files":[{"path":"a"}]}`, []string{`files[0] "a": missing "added" or "removed"`}},
		{"negative added", `{"target":"main","files":[{"path":"a","added":-1}]}`, []string{`files[0] "a": "added" is negative`}},
		{"negative removed", `{"target":"main","files":[{"path":"a","removed":-1}]}`, []string{`files[0] "a": "removed" is negative`}},
		{"two problems", `{"files":[{"path":"a"}]}`, []string{`missing "target"`, `files[0] "a": missing "added" or "removed"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadPlan(strings.NewReader(tt.plan))
			if err == nil {
				t.Fatalf("ReadPlan(%s) accepted the plan", tt.plan)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q, want it to contain %q", err, w)
				}
			}
		})
	}
}

func TestPredict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	plan := Plan{Target: "main", BranchAgeDays: 3, Files: []PlannedFile{
		{Path: "web/src/new.test.js", Added: 1},
		{Path: "app/main.go", Added: 2, Removed: 1},
		{Path: "cli/run.go", Added: 3},
		{Path: "svc/Program.cs", Removed: 3},
		{Path: "lib/package.json", Added: 1},
		{Path: "lib/index.js", Added: 5},
	}}
	want := Facts{
		Base: f.tip,
		Files: []File{
			{Path: "app/main.go", Added: 2, Removed: 1},
			{Path: "cli/run.go", Added: 3, New: true},
			{Path: "lib/index.js", Added: 5, New: true},
			{Path: "lib/package.json", Added: 1, New: true},
			{Path: "svc/Program.cs", Removed: 3},
			{Path: "web/src/new.test.js", Added: 1, New: true, Classes: []Class{ClassTest}},
		},
		Directories: []string{"app", "cli", "lib", "svc", "web/src"},
		Projects:    []string{"app", "lib", "svc", "web"},
		Degree:      Degree{Existing: Lines{Added: 2, Removed: 4}, New: Lines{Added: 10}},
		Impact: Impact{
			Packages: []Dependency{{"app", 0}},
			Files: []Dependency{
				{"cli/run.go", 0}, {"lib/index.js", 0}, {"lib/package.json", 0},
				{"svc/Program.cs", 0}, {"web/src/new.test.js", 0},
			},
		},
		BranchAgeDays: 3,
	}
	// The order files are listed in cannot change the facts.
	rev := Plan{Target: plan.Target, BranchAgeDays: plan.BranchAgeDays, Files: slices.Clone(plan.Files)}
	slices.Reverse(rev.Files)
	for _, p := range []Plan{plan, rev} {
		got, err := Predict(ctx, f.dir, p)
		if err != nil {
			t.Fatalf("Predict: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Predict(%v) =\n%+v\nwant\n%+v", p.Files, got, want)
		}
	}

	if _, err := Predict(ctx, f.dir, Plan{Target: "nope", Files: plan.Files}); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("Predict(target nope) error = %v, want it to name \"nope\"", err)
	}
}
