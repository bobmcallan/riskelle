package facts

import (
	"os"
	"reflect"
	"regexp"
	"strings"
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
		{"app.toml", []Class{ClassNonCode}},
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
		{"flow/steps.py", nil},
		{"lib/contest.py", nil},
		{"src/main/java/com/x/App.java", nil},
		{"src/main/java/com/x/Contest.java", nil},
		{"lib/app.rb", nil},
		{"src/lib.rs", nil},
		{"src/Contest.php", nil},
		{"Sources/App/App.swift", nil},
		{"src/net/socket.cc", nil},
		{"src/App.scala", nil},
		{"lib/app.ex", nil},
		{"lib/main.dart", nil},
		{"main.tf", nil},
		{"infra/main.tf", nil},
		{"infra/x.terraform.lock.hcl", nil},
		{"scripts/run.sh", nil},
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
		// Python
		{"flow/test_steps.py", test},
		{"pkg/steps_test.py", test},
		{"conftest.py", test},
		{"tests/unit/conftest.py", test},
		// Java and Kotlin
		{"src/test/java/com/x/AppTest.java", test},
		{"app/src/main/java/com/x/AppTests.java", test},
		{"src/androidTest/kotlin/x/UiTest.kt", test},
		{"lib/src/jvmMain/x/CacheTests.kt", test},
		// Ruby
		{"spec/models/user_spec.rb", test},
		{"spec/support/helpers.rb", test},
		{"lib/user_spec.rb", test},
		{"test/models/user_test.rb", test},
		// Rust
		{"tests/integration.rs", test},
		{"crates/core/tests/api.rs", test},
		// PHP
		{"src/UserTest.php", test},
		{"tests/Unit/UserTest.php", test},
		// Swift
		{"Tests/AppTests/AppTests.swift", test},
		{"AppTests/LoginTests.swift", test},
		{"Sources/App/LoginTests.swift", test},
		// C and C++
		{"src/net/socket_test.cc", test},
		{"base/strings_unittest.cc", test},
		{"lib/parse_test.c", test},
		{"lib/parse_test.cpp", test},
		{"lib/parse_unittest.cpp", test},
		// Scala
		{"src/test/scala/x/AppSpec.scala", test},
		{"core/src/main/scala/x/AppSuite.scala", test},
		{"core/x/AppTest.scala", test},
		// Elixir
		{"test/my_app_test.exs", test},
		{"apps/web/lib/web_test.exs", test},
		// Dart
		{"test/widget_test.dart", test},
		{"lib/src/parser_test.dart", test},
		// Terraform
		{".terraform.lock.hcl", lock},
		{"infra/prod/.terraform.lock.hcl", lock},
		{"modules/vpc/main.tftest.hcl", test},
		{"vpc.tftest.json", test},
		// Shell
		{"scripts/deploy.bats", test},
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

// TestClassifyNonCode covers each approved non-code format, at the repo root
// and nested, and the near misses that must stay out of the class.
func TestClassifyNonCode(t *testing.T) {
	nonCode := []Class{ClassNonCode}
	tests := []struct {
		path string
		want []Class
	}{
		{".gitignore", nonCode},
		{"web/.gitignore", nonCode},
		{".gitattributes", nonCode},
		{".editorconfig", nonCode},
		{"pyproject.toml", nonCode},
		{"config/app.toml", nonCode},
		{"config/app.ini", nonCode},
		{"setup.cfg", nonCode},
		{".env.example", nonCode},
		{"svc/.env.example", nonCode},
		{"data/rates.csv", nonCode},
		// left out
		{".env", nil},
		{"config.yaml", nil},
		{"settings.json", nil},
		{"app.toml.bak", nil},
		{"gitignore", nil},
		{"DATA.CSV", nil},
		{"Cargo.lock", []Class{ClassLock}},
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
		{"pkg/testdata/rates.csv", []Class{ClassNonCode, ClassTest}},
		{"docs/example.toml", []Class{ClassDocs, ClassNonCode}},
		{"vendor/github.com/a/b/Cargo.toml", []Class{ClassGenerated, ClassNonCode}},
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
	want := []Class{ClassDocs, ClassGenerated, ClassLock, ClassNonCode, ClassTest}
	var got []Class
	for _, b := range builtin {
		got = append(got, b.class)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("builtin classes = %v, want %v", got, want)
	}
}

// TestReadmeListsTestCatalog pins that the README's test-file catalog names
// every built-in test pattern as a code span, so the two cannot drift apart.
func TestReadmeListsTestCatalog(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range builtin {
		if b.class != ClassTest {
			continue
		}
		for _, p := range b.patterns {
			if !strings.Contains(string(readme), "`"+p+"`") {
				t.Errorf("README does not list test pattern `%s`", p)
			}
		}
	}
}
