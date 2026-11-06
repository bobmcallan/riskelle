package facts

import (
	"path"
	"strings"
)

// pairing is one naming convention that ties a test file to the production
// file it tests. For a stem s and a production extension e, the test file is
// named prefix + s + suffix + (testExt, or e when testExt is empty) and the
// file it tests is s + e.
type pairing struct {
	language string   // the README row label
	exts     []string // production extensions this rule pairs, e.g. ".go"
	prefix   string   // test-name prefix before the stem, e.g. "test_"
	suffix   string   // test-name suffix after the stem, before the extension, e.g. "_test"
	testExt  string   // the test file's extension when it differs from the production one ("" = same)
}

// pairings is riskelle's built-in table of test pairing conventions, beside
// the class catalog (builtin, classes.go), and versioned with each release.
// Every marker builds a name that builtin classes as test. The table is
// ordered: within a language the longer marker comes first, so the first
// match is the most specific. Terraform, Shell and Rust have no rule: a
// Terraform test exercises a module directory, a .bats file has no fixed
// subject, and Rust unit tests sit inside the file they test.
var pairings = []pairing{
	{language: "Go", exts: []string{".go"}, suffix: "_test"},
	{language: "Python", exts: []string{".py"}, prefix: "test_"},
	{language: "Python", exts: []string{".py"}, suffix: "_test"},
	{language: ".NET", exts: []string{".cs"}, suffix: "Tests"},
	{language: ".NET", exts: []string{".cs"}, suffix: "Test"},
	{language: "JavaScript/TypeScript", exts: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"}, suffix: ".test"},
	{language: "JavaScript/TypeScript", exts: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"}, suffix: ".spec"},
	{language: "Java/Kotlin", exts: []string{".java", ".kt"}, suffix: "Tests"},
	{language: "Java/Kotlin", exts: []string{".java", ".kt"}, suffix: "Test"},
	{language: "Ruby", exts: []string{".rb"}, suffix: "_spec"},
	{language: "Ruby", exts: []string{".rb"}, suffix: "_test"},
	{language: "PHP", exts: []string{".php"}, suffix: "Test"},
	{language: "Swift", exts: []string{".swift"}, suffix: "Tests"},
	{language: "C/C++", exts: []string{".cc", ".cpp"}, suffix: "_unittest"},
	{language: "C/C++", exts: []string{".c", ".cc", ".cpp"}, suffix: "_test"},
	{language: "Scala", exts: []string{".scala"}, suffix: "Spec"},
	{language: "Scala", exts: []string{".scala"}, suffix: "Suite"},
	{language: "Scala", exts: []string{".scala"}, suffix: "Test"},
	{language: "Elixir", exts: []string{".ex"}, suffix: "_test", testExt: ".exs"},
	{language: "Dart", exts: []string{".dart"}, suffix: "_test"},
}

// testName returns the test file name the rule gives a stem and production
// extension.
func (r pairing) testName(stem, ext string) string {
	te := r.testExt
	if te == "" {
		te = ext
	}
	return r.prefix + stem + r.suffix + te
}

// TestSubject returns the base name of the production file a test file tests,
// from its slash-separated path alone. It ignores directories, is
// case-sensitive, and takes the first rule in pairings whose marker matches
// with a non-empty stem. A test file with no marker (__tests__/a.js,
// tests/api.rs, a file in a *.Tests project) pairs with the file of its own
// name, so the result is that base name. It reads only the path, so it serves
// planned paths too.
func TestSubject(p string) string {
	base := path.Base(p)
	for _, r := range pairings {
		for _, ext := range r.exts {
			end := r.testName("", ext)[len(r.prefix):]
			if len(base) <= len(r.prefix)+len(end) {
				continue
			}
			if strings.HasPrefix(base, r.prefix) && strings.HasSuffix(base, end) {
				return base[len(r.prefix):len(base)-len(end)] + ext
			}
		}
	}
	return base
}

// HasPairing reports whether a path's extension is a production extension
// some rule in pairings pairs, telling code with a test naming convention
// from config, manifests and scripts. It reads only the path.
func HasPairing(p string) bool {
	ext := path.Ext(path.Base(p))
	if ext == "" {
		return false
	}
	for _, r := range pairings {
		for _, e := range r.exts {
			if e == ext {
				return true
			}
		}
	}
	return false
}
