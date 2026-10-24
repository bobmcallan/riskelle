package facts

import (
	"regexp"
	"strings"
)

// Class is a kind of changed path that the scorer treats specially: size
// leaves out docs, generated and lock files, and the tests measure looks for
// test files.
type Class string

// The path classes. Classify returns them in this order.
const (
	ClassDocs      Class = "docs"
	ClassGenerated Class = "generated"
	ClassLock      Class = "lock"
	ClassTest      Class = "test"
)

// builtin is the one listing of riskelle's built-in path patterns, versioned
// with each release. Repo-added patterns (from .risk/rules.json) extend it;
// they never replace it. Later stories that need the built-in rules reuse this
// table rather than keep a second one.
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
		// any ecosystem
		"**/*.lock",
	}},
	{ClassTest, []string{
		// Go
		"**/*_test.go", "**/testdata/**",
		// JavaScript
		"**/*.test.*", "**/*.spec.*", "**/__tests__/**", "**/__mocks__/**", "**/__snapshots__/**",
		// .NET
		"**/*Tests/**", "**/*Test/**", "**/*Tests.cs", "**/*Test.cs",
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
