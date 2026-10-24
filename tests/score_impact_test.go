//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestScoreImpactCollected collects a change from a fixture repository and
// scores it with the built-in rules. The branch changes a package six other
// packages import, a config file three files refer to, and a README many
// files link to. Impact takes the widest reach (the package's 6 dependents),
// names every counted package and file with its count, and leaves the README
// out as docs.
func TestScoreImpactCollected(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "go.mod", "module example.com/shop\n\ngo 1.22\n")
	writeFile(t, dir, "money/money.go", "package money\n\ntype Cents int\n")
	for _, p := range []string{"a", "b", "c", "d", "e", "f"} {
		writeFile(t, dir, p+"/"+p+".go", fmt.Sprintf("package %s\n\nimport _ \"example.com/shop/money\"\n", p))
	}
	writeFile(t, dir, "config/settings.json", "{}\n")
	for _, p := range []string{"x", "y", "z"} {
		writeFile(t, dir, "web/"+p+".js", "load('config/settings.json')\nsee('README.md')\n")
	}
	writeFile(t, dir, "README.md", "# shop\n")
	for i := 0; i < 25; i++ {
		writeFile(t, dir, fmt.Sprintf("notes/n%02d.txt", i), "see ../README.md\n")
	}
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	writeFile(t, dir, "money/money.go", "package money\n\ntype Cents int64\n")
	writeFile(t, dir, "config/settings.json", "{\"currency\": \"EUR\"}\n")
	writeFile(t, dir, "README.md", "# shop\n\nMoney is int64.\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "topic")

	t.Chdir(t.TempDir())
	f, err := facts.Collect(context.Background(), dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	r := score.Score(f, score.Default())
	var entry *score.Point
	for i := range r.Breakdown {
		if r.Breakdown[i].Measure == score.MeasureImpact {
			entry = &r.Breakdown[i]
		}
	}
	if entry == nil {
		t.Fatalf("no impact entry in %+v", r.Breakdown)
	}
	got, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	// 6 dependents -> band 6, 4 points.
	want := `{"measure":"impact","fact":"dependents","value":6,"min":6,"points":4,` +
		`"from":["config/settings.json","example.com/shop/money"],` +
		`"excluded":["README.md"],` +
		`"dependents":[{"name":"config/settings.json","dependents":3},{"name":"example.com/shop/money","dependents":6}]}`
	if string(got) != want {
		t.Errorf("impact entry =\n%s\nwant\n%s", got, want)
	}
	sum := 0
	for _, p := range r.Breakdown {
		sum += p.Points
	}
	if r.Total != sum {
		t.Errorf("total %d, want the sum of the breakdown %d", r.Total, sum)
	}
}
