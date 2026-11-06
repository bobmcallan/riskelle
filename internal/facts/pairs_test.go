package facts

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestTestSubject(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		// Go
		{"pkg/calc_test.go", "calc.go"},
		// Python
		{"tests/flow/test_ai_review.py", "ai_review.py"},
		{"flow/risk_gate_test.py", "risk_gate.py"},
		{"test_x_test.py", "x_test.py"},
		// .NET
		{"src/App.Tests/ServiceTests.cs", "Service.cs"},
		{"ServiceTest.cs", "Service.cs"},
		// JavaScript/TypeScript
		{"src/a.test.ts", "a.ts"},
		{"src/a.spec.tsx", "a.tsx"},
		{"a.b.test.mjs", "a.b.mjs"},
		// Java/Kotlin
		{"src/test/java/x/AppTest.java", "App.java"},
		{"AppTests.kt", "App.kt"},
		// Ruby
		{"spec/models/user_spec.rb", "user.rb"},
		{"test/user_test.rb", "user.rb"},
		// PHP
		{"tests/Unit/UserTest.php", "User.php"},
		// Swift
		{"Tests/AppTests/LoginTests.swift", "Login.swift"},
		// C/C++
		{"lib/parse_test.c", "parse.c"},
		{"src/net/socket_test.cc", "socket.cc"},
		{"base/strings_unittest.cc", "strings.cc"},
		{"lib/parse_unittest.cpp", "parse.cpp"},
		// Scala
		{"AppSpec.scala", "App.scala"},
		{"AppSuite.scala", "App.scala"},
		{"AppTest.scala", "App.scala"},
		// Elixir
		{"test/my_app_test.exs", "my_app.ex"},
		// Dart
		{"test/widget_test.dart", "widget.dart"},
		// no marker: the file of its own name
		{"src/__tests__/a.js", "a.js"},
		{"tests/api.rs", "api.rs"},
		{"modules/vpc/main.tftest.hcl", "main.tftest.hcl"},
		{"scripts/deploy.bats", "deploy.bats"},
		{"web/__snapshots__/app.test.js.snap", "app.test.js.snap"},
		// empty stem
		{"_test.go", "_test.go"},
		{"test_.py", "test_.py"},
		{"Tests.cs", "Tests.cs"},
		// case-sensitive
		{"calc_TEST.go", "calc_TEST.go"},
	}
	for _, tt := range tests {
		if got := TestSubject(tt.path); got != tt.want {
			t.Errorf("TestSubject(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestHasPairing(t *testing.T) {
	for _, ext := range []string{
		".go", ".py", ".cs", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts",
		".java", ".kt", ".rb", ".php", ".swift", ".c", ".cc", ".cpp", ".scala", ".ex", ".dart",
	} {
		if p := "pkg/a" + ext; !HasPairing(p) {
			t.Errorf("HasPairing(%q) = false, want true", p)
		}
	}
	for _, p := range []string{
		"pkg/a.json", "pkg/a.yml", "pkg/a.sh", "pkg/a.md", "pkg/a.tf", "pkg/a.rs", "pkg/a.h", "pkg/a.exs",
		"Makefile", "",
	} {
		if HasPairing(p) {
			t.Errorf("HasPairing(%q) = true, want false", p)
		}
	}
}

// TestPairingNamesAreTests pins that every pairing marker builds a name the
// test catalog classes as test, that the name pairs back to its subject, and
// that the subject is not itself classed test, so the two tables cannot drift.
func TestPairingNamesAreTests(t *testing.T) {
	for _, r := range pairings {
		for _, ext := range r.exts {
			test := "src/" + r.testName("calc", ext)
			if !slices.Contains(Classify(test), ClassTest) {
				t.Errorf("%s: %q is not classed test", r.language, test)
			}
			if got, want := TestSubject(test), "calc"+ext; got != want {
				t.Errorf("%s: TestSubject(%q) = %q, want %q", r.language, test, got, want)
			}
			if subject := "src/calc" + ext; slices.Contains(Classify(subject), ClassTest) {
				t.Errorf("%s: subject %q is classed test", r.language, subject)
			}
		}
	}
}

// TestReadmeListsPairings pins that the README's pairing table names every
// rule's example test name and every production extension as a code span.
func TestReadmeListsPairings(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)
	for _, r := range pairings {
		if name := r.testName("x", r.exts[0]); !strings.Contains(readme, "`"+name+"`") {
			t.Errorf("README does not list %s test name `%s`", r.language, name)
		}
		for _, ext := range r.exts {
			if !strings.Contains(readme, "`"+ext+"`") {
				t.Errorf("README does not list %s extension `%s`", r.language, ext)
			}
		}
	}
}
