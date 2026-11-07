package facts

import (
	"path"
	"slices"
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
// subject, and Rust unit tests sit inside the file they test. Terraform and
// Shell are in unpaired instead; a shell script test named by scriptTests
// names its script (TestSubject), though scripts still score as unpaired.
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
	if subject, ok := scriptTestSubject(base); ok {
		return subject
	}
	return base
}

// unpaired is the table of languages whose tests riskelle can see but cannot
// pair with one file (see pairings): a production file in one of them counts
// as untested only when the repo holds a test in its language. Every test
// ending builds a name builtin classes as test, and no extension here is one
// pairings pairs.
var unpaired = []struct {
	language string   // the README row label
	exts     []string // production extensions
	tests    []string // test file name endings
}{
	{scriptLanguage, []string{".sh", ".bash"}, []string{".bats"}},
	{"Terraform", []string{".tf", ".tfvars"}, []string{".tftest.hcl", ".tftest.json"}},
}

// scriptLanguage is the unpaired language of shell scripts and their tests.
const scriptLanguage = "Shell"

// scriptTests are the shell script test names, in order, first match wins: a
// test of the script stem is named prefix + stem + suffix + ext, for an ext in
// scriptExts, and tests stem + ext.
var scriptTests = []struct{ prefix, suffix string }{
	{suffix: "-test"}, {suffix: "_test"}, {prefix: "test-"},
}

// scriptExts are the extensions a script and its test share; "" is none, a
// base name with no dot.
var scriptExts = []string{"", ".sh", ".bash"}

// scriptTestSubject returns the script a shell script test's base name tests,
// in the same convention (no extension with none, .sh with .sh), and whether
// the name is a script test at all. An empty stem is not one.
func scriptTestSubject(base string) (string, bool) {
	ext := path.Ext(base)
	if !slices.Contains(scriptExts, ext) {
		return "", false
	}
	name := strings.TrimSuffix(base, ext)
	for _, m := range scriptTests {
		if len(name) > len(m.prefix)+len(m.suffix) && strings.HasPrefix(name, m.prefix) && strings.HasSuffix(name, m.suffix) {
			return name[len(m.prefix):len(name)-len(m.suffix)] + ext, true
		}
	}
	return "", false
}

// isScriptTest reports whether a base name is a shell script test name.
func isScriptTest(base string) bool {
	_, ok := scriptTestSubject(base)
	return ok
}

// LanguageOf returns the unpaired language of a changed file: the one its
// path names (UnpairedLanguage), else the one its content names (Language).
func LanguageOf(f File) string {
	if lang := UnpairedLanguage(f.Path); lang != "" {
		return lang
	}
	return f.Language
}

// UnpairedLanguage returns the unpaired language a production path is
// written in, from its extension alone, or "" when it is in none.
func UnpairedLanguage(p string) string {
	ext := path.Ext(path.Base(p))
	if ext == "" {
		return ""
	}
	for _, u := range unpaired {
		for _, e := range u.exts {
			if e == ext {
				return u.language
			}
		}
	}
	return ""
}

// TestLanguages returns the unpaired languages that have at least one test
// file among paths, sorted, distinct and non-nil. A path is a test of a
// language when Classify classes it test and its base name ends in one of the
// language's test endings or production extensions, so tests/smoke.sh counts
// for Shell. It reads only the paths, so it serves a head tree and a plan.
func TestLanguages(paths []string) []string {
	set := map[string]bool{}
	for _, p := range paths {
		base := path.Base(p)
		for _, u := range unpaired {
			if set[u.language] || !(endsInAny(base, u.tests, u.exts) || u.language == scriptLanguage && isScriptTest(base)) {
				continue
			}
			if slices.Contains(Classify(p), ClassTest) {
				set[u.language] = true
			}
		}
	}
	return sorted(set)
}

// endsInAny reports whether s ends in any of the endings of any list.
func endsInAny(s string, lists ...[]string) bool {
	for _, l := range lists {
		for _, e := range l {
			if strings.HasSuffix(s, e) {
				return true
			}
		}
	}
	return false
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
