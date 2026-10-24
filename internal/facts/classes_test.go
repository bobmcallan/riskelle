package facts

import (
	"reflect"
	"regexp"
	"testing"
)

func TestGlob(t *testing.T) {
	tests := []struct {
		glob, path string
		want       bool
	}{
		{"**/x.md", "x.md", true},
		{"**/x.md", "a/b/x.md", true},
		{"**/x.md", "ax.md", false},
		{"*.md", "x.md", true},
		{"*.md", "a/x.md", false},
		{"a/*", "a/b", true},
		{"a/*", "a/b/c", false},
		{"?.go", "a.go", true},
		{"?.go", "ab.go", false},
		{"a?b", "a/b", false},
		{"**/vendor/**", "vendor/a", true},
		{"**/vendor/**", "x/vendor/a/b", true},
		{"**/vendor/**", "vendors/a", false},
		{"**/vendor/**", "vendor", false},
		{"*.min.js", "app.min.js", true},
		{"*.min.js", "axminxjs", false},
		{"a+(b).txt", "a+(b).txt", true},
		{"a+(b).txt", "aa(b).txt", false},
	}
	for _, tt := range tests {
		re := regexp.MustCompile(globRegexp(tt.glob))
		if got := re.MatchString(tt.path); got != tt.want {
			t.Errorf("glob %q on %q = %v, want %v", tt.glob, tt.path, got, tt.want)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		path string
		want []Class
	}{
		// one plain example per class
		{"README.md", []Class{ClassDocs}},
		{"api/x.pb.go", []Class{ClassGenerated}},
		{"go.sum", []Class{ClassLock}},
		{"main_test.go", []Class{ClassTest}},
		// paths in no class
		{"main.go", nil},
		{"src/App/Program.cs", nil},
		{"web/src/index.js", nil},
		{"go.mod", nil},
		{"package.json", nil},
		{"cmd/latest/main.go", nil},
		{"amd/x.go", nil},
		{"src/Contest.cs", nil},
		{"", nil},
	}
	for _, tt := range tests {
		if got := Classify(tt.path); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Classify(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestClassifyConventions(t *testing.T) {
	docs := []Class{ClassDocs}
	gen := []Class{ClassGenerated}
	lock := []Class{ClassLock}
	test := []Class{ClassTest}
	tests := []struct {
		path string
		want []Class
	}{
		// Go
		{"go.sum", lock},
		{"api/v1/svc.pb.go", gen},
		{"pkg/x_test.go", test},
		{"pkg/testdata/in.json", test},
		{"vendor/github.com/a/b/c.go", gen},
		// JavaScript
		{"package-lock.json", lock},
		{"web/yarn.lock", lock},
		{"web/pnpm-lock.yaml", lock},
		{"web/dist/app.min.js", gen},
		{"src/app.test.ts", test},
		{"src/app.spec.js", test},
		{"src/__tests__/a.js", test},
		// .NET
		{"src/App/packages.lock.json", lock},
		{"src/App/Form1.Designer.cs", gen},
		{"src/App/Migrations/AppModelSnapshot.cs", gen},
		{"src/App.Tests/ServiceTests.cs", test},
		{"src/App.UnitTests/Foo.cs", test},
		// docs, any ecosystem
		{"docs/setup.txt", docs},
		{"LICENSE", docs},
	}
	for _, tt := range tests {
		if got := Classify(tt.path); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Classify(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestClassifyMultipleClasses(t *testing.T) {
	tests := []struct {
		path string
		want []Class
	}{
		{"pkg/testdata/golden.md", []Class{ClassDocs, ClassTest}},
		{"web/src/__snapshots__/app.test.js.snap", []Class{ClassGenerated, ClassTest}},
		{"vendor/github.com/a/b/README.md", []Class{ClassDocs, ClassGenerated}},
		// matches two docs patterns, carries the class once
		{"docs/guide/intro.md", []Class{ClassDocs}},
	}
	for _, tt := range tests {
		if got := Classify(tt.path); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Classify(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestBuiltinOrder pins that the table lists each class once, in Class order,
// which is what keeps Classify's result sorted.
func TestBuiltinOrder(t *testing.T) {
	want := []Class{ClassDocs, ClassGenerated, ClassLock, ClassTest}
	var got []Class
	for _, b := range builtin {
		got = append(got, b.class)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("builtin classes = %v, want %v", got, want)
	}
}
