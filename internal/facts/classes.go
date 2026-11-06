package facts

import (
	"path"
	"regexp"
	"strings"
)

// Class is a kind of changed path that the scorer treats specially: size
// leaves out docs, generated and lock files, and the tests measure looks for
// test files. Non-code files configure the repo or hold plain data rather
// than implement, as do agent and harness config (.satelle/, .claude/ and the
// like); no measure leaves them out by default yet, the rules toggle
// does that. Run records are what records a change rather than makes it:
// logs, plan output and state snapshots.
type Class string

// The path classes. Classify returns them in this order.
const (
	ClassDocs      Class = "docs"
	ClassGenerated Class = "generated"
	ClassLock      Class = "lock"
	ClassNonCode   Class = "non_code"
	ClassRunRecord Class = "run_record"
	ClassTest      Class = "test"
)

// builtin is the one listing of riskelle's built-in path patterns, versioned
// with each release. Repo-added patterns (from .risk/rules.json) extend it;
// they never replace it. Later stories that need the built-in rules reuse this
// table rather than keep a second one; the other built-in path tables are
// builtinAreas (areas.go), whose sensitive areas are not classes and never
// score, and pairings (pairs.go), which names the file a test file tests.
//
// Patterns are globs over the whole slash-separated, repo-relative path and
// are case-sensitive: "**/" matches zero or more directories, a trailing "/**"
// everything below a directory, "*" any run of characters other than "/", and
// "?" one such character. Every other character is literal.
//
// The entries are in Class order, so Classify's result is sorted.
var builtin = []struct {
	class    Class
	patterns []string
}{
	{ClassDocs, []string{
		"**/*.md", "**/*.markdown", "**/*.mdx", "**/*.rst", "**/*.adoc",
		"**/docs/**", "**/doc/**", "**/Docs/**",
		"**/LICENSE",
	}},
	{ClassGenerated, []string{
		// Go
		"**/*.pb.go", "**/*.pb.gw.go", "**/*_generated.go", "**/zz_generated*.go", "**/*.gen.go", "**/*_gen.go",
		// JavaScript
		"**/*.min.js", "**/*.min.css", "**/*.js.map", "**/*.css.map", "**/__snapshots__/**",
		// .NET
		"**/*.Designer.cs", "**/*.g.cs", "**/*.g.i.cs", "**/*.generated.cs", "**/*ModelSnapshot.cs",
		// vendored dependencies, written by a tool
		"**/vendor/**", "**/node_modules/**",
	}},
	{ClassLock, []string{
		// Go
		"**/go.sum", "**/go.work.sum",
		// JavaScript
		"**/package-lock.json", "**/npm-shrinkwrap.json", "**/yarn.lock", "**/pnpm-lock.yaml", "**/bun.lockb",
		// .NET
		"**/packages.lock.json", "**/paket.lock",
		// Terraform
		"**/.terraform.lock.hcl",
		// any ecosystem
		"**/*.lock",
	}},
	{ClassNonCode, []string{
		// repo and editor config
		"**/.gitignore", "**/.gitattributes", "**/.editorconfig",
		// config formats
		"**/*.toml", "**/*.ini", "**/*.cfg",
		// example environment
		"**/.env.example",
		// plain data
		"**/*.csv",
		// agent and harness config: satelle, Claude Code, pi, Grok, Cursor,
		// Codex, Gemini CLI, Windsurf, Cline, Roo Code, Kiro, JetBrains Junie
		"**/.satelle/**", "**/.claude/**", "**/.pi/**", "**/.grok/**", "**/.cursor/**",
		"**/.codex/**", "**/.gemini/**", "**/.windsurf/**", "**/.clinerules/**",
		"**/.roo/**", "**/.kiro/**", "**/.junie/**",
		// GitHub Copilot; .github/workflows/** stays code
		"**/.github/copilot-instructions.md",
		"**/.github/instructions/**", "**/.github/prompts/**", "**/.github/chatmodes/**",
		// agent instruction files
		"**/CLAUDE.md", "**/AGENTS.md", "**/GEMINI.md",
		// legacy single-file rules
		"**/.cursorrules", "**/.windsurfrules", "**/.clinerules",
		// MCP server config and aider
		"**/.mcp.json", "**/.aider.conf.yml",
	}},
	{ClassRunRecord, []string{
		// logs
		"**/*.log",
		// rotated logs, *.log.[0-9]* one digit at a time
		"**/*.log.0*", "**/*.log.1*", "**/*.log.2*", "**/*.log.3*", "**/*.log.4*",
		"**/*.log.5*", "**/*.log.6*", "**/*.log.7*", "**/*.log.8*", "**/*.log.9*",
		// log directories; log/ only at the root, as **/log/** would take in
		// source packages such as internal/log
		"**/logs/**", "log/**",
		// package-manager debug logs
		"**/npm-debug.log*", "**/yarn-error.log*", "**/yarn-debug.log*", "**/pnpm-debug.log*", "**/lerna-debug.log*",
		// Terraform plan output
		"**/*.tfplan", "**/tfplan", "**/*.tfplan.json", "**/tfplan.json", "**/*.tfplan.txt", "**/tfplan.txt",
		// state snapshots
		"**/*.tfstate", "**/*.tfstate.*",
	}},
	{ClassTest, []string{
		// Go
		"**/*_test.go", "**/testdata/**",
		// JavaScript
		"**/*.test.*", "**/*.spec.*", "**/__tests__/**", "**/__mocks__/**", "**/__snapshots__/**",
		// .NET
		"**/*Tests/**", "**/*Test/**", "**/*Tests.cs", "**/*Test.cs",
		// Python
		"**/test_*.py", "**/*_test.py", "**/conftest.py",
		// Java and Kotlin; src/test/** comes from "**/test/**", and
		// src/androidTest/** and src/integrationTest/** from "**/*Test/**"
		"**/*Test.java", "**/*Tests.java", "**/*Test.kt", "**/*Tests.kt",
		// Ruby
		"**/spec/**", "**/*_spec.rb", "**/*_test.rb",
		// PHP
		"**/*Test.php",
		// Swift; *Tests/** comes from the .NET "**/*Tests/**"
		"**/*Tests.swift",
		// C and C++
		"**/*_test.c", "**/*_test.cc", "**/*_test.cpp", "**/*_unittest.cc", "**/*_unittest.cpp",
		// Scala
		"**/*Spec.scala", "**/*Suite.scala", "**/*Test.scala",
		// Elixir
		"**/*_test.exs",
		// Dart
		"**/*_test.dart",
		// Terraform
		"**/*.tftest.hcl", "**/*.tftest.json",
		// Shell
		"**/*.bats",
		// and the script test names in pairs.go's scriptTests (x-test, x_test,
		// test-x, with no extension or .sh/.bash): a glob cannot say "no
		// extension", and **/test-* would class test-utils.ts too.
		// Rust needs no pattern of its own: its integration tests are tests/**,
		// which "**/tests/**" covers, and its unit tests sit inside source files
		// (#[cfg(test)]), which a path cannot show.
		// any ecosystem
		"**/test/**", "**/tests/**",
	}},
}

type compiledClass struct {
	class    Class
	patterns []*regexp.Regexp
}

// compiled holds builtin compiled once; a malformed built-in panics at init.
var compiled = func() []compiledClass {
	out := make([]compiledClass, 0, len(builtin))
	for _, b := range builtin {
		c := compiledClass{class: b.class}
		for _, p := range b.patterns {
			c.patterns = append(c.patterns, regexp.MustCompile(globRegexp(p)))
		}
		out = append(out, c)
	}
	return out
}()

// Classify returns the classes of a slash-separated, repo-relative path from
// the built-in patterns, in Class order and each at most once. A path in no
// class gives nil. It reads only the path, so it serves planned paths too.
func Classify(p string) []Class {
	var classes []Class
	for _, c := range compiled {
		for _, re := range c.patterns {
			if re.MatchString(p) {
				classes = append(classes, c.class)
				break
			}
		}
	}
	// ClassTest is last in Class order, so appending keeps the order.
	if (len(classes) == 0 || classes[len(classes)-1] != ClassTest) && isScriptTest(path.Base(p)) {
		classes = append(classes, ClassTest)
	}
	return classes
}

// globRegexp translates a glob (syntax as documented on builtin) into an
// anchored regular expression.
func globRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 3
		case glob[i:] == "/**":
			b.WriteString("/.*")
			i += 3
		case glob[i] == '*':
			b.WriteString("[^/]*")
			i++
		case glob[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
			i++
		}
	}
	b.WriteString("$")
	return b.String()
}

// Globs are repo-added patterns in the builtin syntax, compiled once.
type Globs []*regexp.Regexp

// CompileGlobs compiles patterns; globRegexp quotes every literal, so none fails.
func CompileGlobs(patterns []string) Globs {
	g := make(Globs, 0, len(patterns))
	for _, p := range patterns {
		g = append(g, regexp.MustCompile(globRegexp(p)))
	}
	return g
}

// Match reports whether p matches any of the globs.
func (g Globs) Match(p string) bool {
	for _, re := range g {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}
