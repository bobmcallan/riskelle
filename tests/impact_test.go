//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// impactJSON collects the facts of main..topic in dir three times, from the
// root and from a subdirectory, and returns the impact JSON after checking
// every run gave the same bytes.
func impactJSON(t *testing.T, dir, sub string) string {
	t.Helper()
	ctx := context.Background()
	var first string
	for i, from := range []string{dir, dir, filepath.Join(dir, sub)} {
		got, err := facts.Collect(ctx, from, "main", "topic")
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b, err := json.Marshal(got.Impact)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = string(b)
		} else if string(b) != first {
			t.Fatalf("run %d impact differs:\n%s\n%s", i+1, b, first)
		}
	}
	return first
}

// TestImpactOfAGoPackage changes a package imported by two packages of its own
// module and by one in a second module of the same repository. Test files,
// vendored code and uncommitted files do not count.
func TestImpactOfAGoPackage(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "go.mod", "module example.com/shop\n\ngo 1.22\n")
	writeFile(t, dir, "money/money.go", "package money\n\ntype Cents int\n")
	writeFile(t, dir, "cart/cart.go", "package cart\n\nimport \"example.com/shop/money\"\n\nvar _ money.Cents\n")
	writeFile(t, dir, "billing/billing.go", "package billing\n\nimport (\n\t\"fmt\"\n\tm \"example.com/shop/money\"\n)\n\nvar _ = fmt.Sprint(m.Cents(0))\n")
	writeFile(t, dir, "billing/billing_test.go", "package billing\n\nimport _ \"example.com/shop/money\"\n")
	writeFile(t, dir, "report/report_test.go", "package report\n\nimport _ \"example.com/shop/money\"\n")
	writeFile(t, dir, "vendor/x/x.go", "package x\n\nimport _ \"example.com/shop/money\"\n")
	writeFile(t, dir, "tools/go.mod", "module example.com/tools\n")
	writeFile(t, dir, "tools/audit/audit.go", "package audit\n\nimport _ \"example.com/shop/money\"\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	writeFile(t, dir, "money/money.go", "package money\n\ntype Cents int64\n")
	writeFile(t, dir, "cart/cart_test.go", "package cart\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "topic")

	// Not committed: must not count.
	writeFile(t, dir, "orders/orders.go", "package orders\n\nimport _ \"example.com/shop/money\"\n")

	want := `{"packages":[{"name":"example.com/shop/money","dependents":3}],"files":[]}`
	if got := impactJSON(t, dir, "money"); got != want {
		t.Errorf("impact =\n%s\nwant\n%s", got, want)
	}
}

// TestImpactOfNonGoFiles changes a JavaScript module, a stylesheet and a
// project file, and renames a doc page, each referenced from other files.
func TestImpactOfNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "web/package.json", "{}\n")
	writeFile(t, dir, "web/src/format.js", "export const f = 1\n")
	writeFile(t, dir, "web/src/app.js", "import { f } from './format'\nimport './theme.css'\n")
	writeFile(t, dir, "web/src/pages/home.js", "import { f } from '../format.js'\n")
	writeFile(t, dir, "web/src/format.test.js", "import { f } from './format'\n")
	writeFile(t, dir, "web/src/formatter.js", "import x from './formatter-core'\n") // decoy
	writeFile(t, dir, "web/src/theme.css", "body {}\n")
	writeFile(t, dir, "web/index.html", "<link href=\"./src/theme.css\">\n")
	writeFile(t, dir, "Core/Core.csproj", "<Project />\n")
	writeFile(t, dir, "Api/Api.csproj", "<ProjectReference Include=\"..\\Core\\Core.csproj\" />\n")
	writeFile(t, dir, "docs/setup.md", "Setup\n")
	writeFile(t, dir, "README.md", "See [setup](docs/setup.md) and web/src/format.js\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	writeFile(t, dir, "web/src/format.js", "export const f = 2\n")
	writeFile(t, dir, "web/src/theme.css", "body { margin: 0 }\n")
	writeFile(t, dir, "Core/Core.csproj", "<Project Sdk=\"x\" />\n")
	fixtureGit(t, dir, "mv", "docs/setup.md", "docs/install.md")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "topic")

	// Not committed: must not count.
	writeFile(t, dir, "web/src/extra.js", "import { f } from './format'\n")

	want := `{"packages":[],"files":[` +
		`{"name":"Core/Core.csproj","dependents":1},` +
		`{"name":"docs/install.md","dependents":0},` +
		`{"name":"docs/setup.md","dependents":1},` +
		`{"name":"web/src/format.js","dependents":4},` +
		`{"name":"web/src/theme.css","dependents":2}]}`
	if got := impactJSON(t, dir, "web"); got != want {
		t.Errorf("impact =\n%s\nwant\n%s", got, want)
	}
}
