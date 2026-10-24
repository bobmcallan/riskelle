package facts

import (
	"context"
	"reflect"
	"testing"
)

// scanTree feeds files to a scan in the order given, as CollectImpact does with
// the blobs of a commit.
func scanTree(changed []string, files [][2]string) Impact {
	s := newScan(changed)
	for _, f := range files {
		s.add(f[0], []byte(f[1]))
	}
	return s.result()
}

func goFile(pkg string, imports ...string) string {
	src := "package " + pkg + "\n\n"
	for _, imp := range imports {
		src += "import _ \"" + imp + "\"\n"
	}
	return src
}

func TestScanGoPackages(t *testing.T) {
	const lib = "example.com/m/lib"
	base := [][2]string{
		{"go.mod", "module example.com/m // the root module\n\ngo 1.22\n"},
		{"lib/lib.go", goFile("lib")},
		{"lib/x_test.go", goFile("lib_test", lib)},                            // own test: not counted
		{"a/a.go", goFile("a", lib, "fmt")},                                   // counted
		{"b/b.go", goFile("b", lib)},                                          // counted
		{"b/b2.go", goFile("b", lib)},                                         // same package: counted once
		{"c/c_test.go", goFile("c", lib)},                                     // test only: not counted
		{"vendor/v/v.go", goFile("v", lib)},                                   // vendor: not counted
		{"testdata/t/t.go", goFile("t", lib)},                                 // testdata: not counted
		{".hidden/h.go", goFile("h", lib)},                                    // dot dir: not counted
		{"broken/x.go", "package broken\n\nimport \"" + lib + "\"\n\nfunc {"}, // recovered import: counted
	}
	nested := append(append([][2]string{}, base...),
		[2]string{"tools/go.mod", "module \"example.com/tools\"\n"},
		[2]string{"tools/cmd/main.go", goFile("main", lib)},
	)

	tests := []struct {
		name    string
		changed []string
		files   [][2]string
		want    Impact
	}{
		{
			name:    "importing packages, excluding tests, vendor, testdata and own dir",
			changed: []string{"lib/lib.go"},
			files:   base,
			want:    Impact{Packages: []Dependency{{lib, 3}}, Files: []Dependency{}},
		},
		{
			name:    "an importer in a nested module of the same repo counts",
			changed: []string{"lib/lib.go"},
			files:   nested,
			want:    Impact{Packages: []Dependency{{lib, 4}}, Files: []Dependency{}},
		},
		{
			name:    "a package in a nested module is named by that module",
			changed: []string{"tools/cmd/main.go"},
			files:   nested,
			want:    Impact{Packages: []Dependency{{"example.com/tools/cmd", 0}}, Files: []Dependency{}},
		},
		{
			name:    "the root package is the module path",
			changed: []string{"main.go"},
			files:   append(append([][2]string{}, base...), [2]string{"main.go", goFile("main")}),
			want:    Impact{Packages: []Dependency{{"example.com/m", 0}}, Files: []Dependency{}},
		},
		{
			name:    "two files of one package list it once",
			changed: []string{"b/b.go", "b/b2.go"},
			files:   base,
			want:    Impact{Packages: []Dependency{{"example.com/m/b", 0}}, Files: []Dependency{}},
		},
		{
			name:    "a test-only change lists nothing",
			changed: []string{"lib/lib_test.go"},
			files:   base,
			want:    Impact{Packages: []Dependency{}, Files: []Dependency{}},
		},
		{
			name:    "a .go file under no module is a file",
			changed: []string{"script/gen.go"},
			files:   [][2]string{{"script/gen.go", goFile("main")}},
			want:    Impact{Packages: []Dependency{}, Files: []Dependency{{"script/gen.go", 0}}},
		},
		{
			name:    "a .go file in vendor is a file",
			changed: []string{"vendor/v/v.go"},
			files:   base,
			want:    Impact{Packages: []Dependency{}, Files: []Dependency{{"vendor/v/v.go", 0}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scanTree(tt.changed, tt.files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("impact = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestModulePath(t *testing.T) {
	tests := map[string]string{
		"module example.com/m\n":                 "example.com/m",
		"// header\nmodule example.com/m // x\n": "example.com/m",
		"module \"example.com/q\"\n\ngo 1.22\n":  "example.com/q",
		"go 1.22\n":                              "",
		"modulex example.com/m\n":                "",
	}
	for in, want := range tests {
		if got := modulePath([]byte(in)); got != want {
			t.Errorf("modulePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelPath(t *testing.T) {
	tests := []struct{ dir, target, want string }{
		{".", "a.js", "a.js"},
		{".", "web/a.js", "web/a.js"},
		{"web", "web/src/a.js", "src/a.js"},
		{"web/src", "web/src/a.js", "a.js"},
		{"web/lib", "web/src/a.js", "../src/a.js"},
		{"x/y", "a.js", "../../a.js"},
		{"web", "webx/a.js", "../webx/a.js"},
	}
	for _, tt := range tests {
		if got := relPath(tt.dir, tt.target); got != tt.want {
			t.Errorf("relPath(%q, %q) = %q, want %q", tt.dir, tt.target, got, tt.want)
		}
	}
}

func TestScanFileReferences(t *testing.T) {
	const util = "web/src/util.js"
	files := [][2]string{
		{util, "export const u = './util'\n"},                           // itself: not counted
		{"web/src/app.js", "import u from './util'\n"},                  // counted
		{"web/index.html", "<script src=\"./src/util.js\"></script>\n"}, // counted
		{"web/lib/x.js", "const u = require('../src/util')\n"},          // counted
		{"README.md", "See /web/src/util.js for v\n"},                   // counted: repo path
		{"web/src/style.js", "import './util.css'\n"},                   // other file
		{"web/src/h.js", "import './util/helpers'\n"},                   // a directory below
		{"web/src/up.js", "require('../util')\n"},                       // web/util, not this
		{"notes.txt", "the util module\n"},                              // bare name
		{"web/src/blob.dat", "\x00./util\n"},                            // binary
	}
	got := scanTree([]string{util}, files)
	want := Impact{Packages: []Dependency{}, Files: []Dependency{{util, 4}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("impact = %+v, want %+v", got, want)
	}

	// A project file referring with backslashes.
	got = scanTree([]string{"Lib/Lib.csproj"}, [][2]string{
		{"App/App.csproj", "<ProjectReference Include=\"..\\Lib\\Lib.csproj\" />\n"},
		{"Lib/Lib.csproj", "<Project />\n"},
	})
	want = Impact{Packages: []Dependency{}, Files: []Dependency{{"Lib/Lib.csproj", 1}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("csproj impact = %+v, want %+v", got, want)
	}

	// A rename lists both paths; referrers of the old path still count.
	got = scanTree([]string{"docs/new.md", "docs/old.md"}, [][2]string{
		{"docs/new.md", "text\n"},
		{"index.md", "[a](./docs/old.md) and [b](docs/old.md)\n"},
	})
	want = Impact{Packages: []Dependency{}, Files: []Dependency{{"docs/new.md", 0}, {"docs/old.md", 1}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rename impact = %+v, want %+v", got, want)
	}
}

// newImpactFixture is a repository with a root Go module whose package lib
// is imported by packages a and b, and a web app whose util.js is referenced
// by two files; branch feature changes lib and util.js.
func newImpactFixture(t *testing.T) string {
	t.Helper()
	f := newFixture(t) // a repo with git configured; its content is replaced below
	dir := f.dir
	runGit(t, dir, "switch", "--quiet", "--orphan", "impact")
	runGit(t, dir, "rm", "-r", "--quiet", "--cached", "--ignore-unmatch", ".")
	write(t, dir, "go.mod", "module example.com/m\n")
	write(t, dir, "lib/lib.go", "package lib\n\nconst V = 1\n")
	write(t, dir, "a/a.go", goFile("a", "example.com/m/lib"))
	write(t, dir, "b/b.go", goFile("b", "example.com/m/lib", "fmt"))
	write(t, dir, "web/src/util.js", "export const v = 1\n")
	write(t, dir, "web/src/app.js", "import { v } from './util'\n")
	write(t, dir, "web/index.html", "<script src=\"./src/util.js\"></script>\n")
	write(t, dir, "web/src/style.js", "import './util.css'\n")
	runGit(t, dir, "add", "go.mod", "lib", "a", "b", "web")
	runGit(t, dir, "commit", "--quiet", "-m", "impact base")
	runGit(t, dir, "switch", "--quiet", "-c", "impact-feature")
	write(t, dir, "lib/lib.go", "package lib\n\nconst V = 2\n")
	write(t, dir, "web/src/util.js", "export const v = 2\n")
	runGit(t, dir, "add", "lib", "web")
	runGit(t, dir, "commit", "--quiet", "-m", "change lib and util")
	return dir
}

func TestCollectImpact(t *testing.T) {
	dir := newImpactFixture(t)
	ctx := context.Background()
	want := Impact{
		Packages: []Dependency{{"example.com/m/lib", 2}},
		Files:    []Dependency{{"web/src/util.js", 2}},
	}
	first, err := Collect(ctx, dir, "impact", "impact-feature")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !reflect.DeepEqual(first.Impact, want) {
		t.Errorf("Impact = %+v, want %+v", first.Impact, want)
	}

	second, err := Collect(ctx, dir, "impact", "impact-feature")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second collection differs:\n%+v\n%+v", first, second)
	}

	// Uncommitted importers and referrers, and config, must not count.
	write(t, dir, "c/extra.go", goFile("c", "example.com/m/lib"))
	write(t, dir, "web/other.js", "require('./src/util')\n")
	runGit(t, dir, "config", "core.autocrlf", "true")
	third, err := Collect(ctx, dir, "impact", "impact-feature")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, third) {
		t.Errorf("collection after worktree and config changes differs:\n%+v\n%+v", first, third)
	}
}

func TestCollectImpactNoPaths(t *testing.T) {
	got, err := CollectImpact(context.Background(), t.TempDir(), "HEAD", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := emptyImpact(); !reflect.DeepEqual(got, want) {
		t.Errorf("CollectImpact(no paths) = %+v, want %+v", got, want)
	}
}
