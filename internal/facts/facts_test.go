package facts

import (
	"reflect"
	"testing"
)

func TestDirectories(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{"none", nil, []string{}},
		{"root file", []string{"README.md"}, []string{"."}},
		{"distinct and sorted", []string{"b/x.go", "a/y.go", "b/z.go", "a/c/d.go"}, []string{"a", "a/c", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Directories(tt.paths); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Directories(%q) = %q, want %q", tt.paths, got, tt.want)
			}
		})
	}
}

func TestProjects(t *testing.T) {
	tests := []struct {
		name         string
		paths, roots []string
		want         []string
	}{
		{"no roots", []string{"a/b.go"}, nil, []string{}},
		{"nearest ancestor wins", []string{"web/src/x.js", "main.go"}, []string{".", "web"}, []string{".", "web"}},
		{"under no root", []string{"docs/a.md"}, []string{"app"}, []string{}},
		{"manifest itself", []string{"app/go.mod"}, []string{"app"}, []string{"app"}},
		{"deep path", []string{"svc/a/b/c.cs"}, []string{"svc"}, []string{"svc"}},
		{"prefix is not an ancestor", []string{"application/x.go"}, []string{"app"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Projects(tt.paths, tt.roots); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Projects(%q, %q) = %q, want %q", tt.paths, tt.roots, got, tt.want)
			}
		})
	}
}

func TestIsManifest(t *testing.T) {
	for name, want := range map[string]bool{
		"go.mod":            true,
		"package.json":      true,
		"Svc.csproj":        true,
		"go.sum":            false,
		"App.csproj.user":   false,
		"package-lock.json": false,
		"main.go":           false,
	} {
		if got := IsManifest(name); got != want {
			t.Errorf("IsManifest(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestParseNumstat(t *testing.T) {
	in := "2\t1\tapp/main.go\x00" +
		"1\t0\t\x00docs/old.md\x00docs/guide/new.md\x00" +
		"-\t-\tlogo.png\x00"
	got, err := parseNumstat([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []File{
		{Path: "app/main.go", Added: 2, Removed: 1},
		{Path: "docs/guide/new.md", OldPath: "docs/old.md", Added: 1},
		{Path: "logo.png", Binary: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNumstat = %+v, want %+v", got, want)
	}

	if got, err := parseNumstat(nil); err != nil || len(got) != 0 || got == nil {
		t.Errorf("parseNumstat(empty) = %#v, %v; want empty non-nil, nil", got, err)
	}

	for _, bad := range []string{
		"garbage\x00",
		"x\t1\ta.go\x00",
		"1\t0\t\x00only-old\x00",
	} {
		if _, err := parseNumstat([]byte(bad)); err == nil {
			t.Errorf("parseNumstat(%q) = nil error, want an error", bad)
		}
	}
}
