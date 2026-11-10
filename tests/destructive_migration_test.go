//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// efInit is a scaffolded EF Core migration: CreateTable in Up, its DropTable
// in Down.
const efInit = `using Microsoft.EntityFrameworkCore.Migrations;

namespace App.Migrations
{
    public partial class Init : Migration
    {
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.CreateTable(
                name: "Users",
                columns: table => new
                {
                    Id = table.Column<int>(nullable: false),
                    Legacy = table.Column<string>(nullable: true)
                });
        }

        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "Users");
        }
    }
}
`

// efDropLegacy drops a column in Up and adds it back in Down.
const efDropLegacy = `using Microsoft.EntityFrameworkCore.Migrations;

namespace App.Migrations
{
    public partial class DropLegacy : Migration
    {
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropColumn(
                name: "Legacy",
                table: "Users");
        }

        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<string>(
                name: "Legacy",
                table: "Users",
                nullable: true);
        }
    }
}
`

// efAddNote is a scaffolded additive migration: AddColumn in Up, its
// DropColumn in Down.
const efAddNote = `using Microsoft.EntityFrameworkCore.Migrations;

namespace App.Migrations
{
    public partial class AddNote : Migration
    {
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<string>(
                name: "Note",
                table: "Users",
                nullable: true);
        }

        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropColumn(
                name: "Note",
                table: "Users");
        }
    }
}
`

const sqlInitHead = "CREATE TABLE users (id int PRIMARY KEY);\n"
const sqlInitDrop = "DROP TABLE IF EXISTS legacy;\n"

// TestDestructiveMigrationFloorsHigh drives the built binary: an added
// DropColumn in an EF Core migration and an added DROP TABLE in a .sql file
// each carry destructive_migration, floored High and typed by the operation,
// and fire a trigger; an additive migration, CREATE TABLE, commented-out
// DROPs and a removed DROP line carry nothing. estimate with a plan naming
// the class is High; naming none is Low.
func TestDestructiveMigrationFloorsHigh(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	base := map[string]string{
		"src/App/Migrations/20240101_Init.cs": efInit,
		"db/migrations/001_init.sql":          sqlInitHead + sqlInitDrop,
	}
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	high := func(t *testing.T, topic map[string]string, path, op string) string {
		t.Helper()
		dir := rulesFixture(t, base, topic)
		got := decodeReport(t, run(t, dir))
		if got.Level != score.High || got.ShapeLevel != score.Low || !reflect.DeepEqual(got.FlooredBy, []string{"destructive_migration"}) {
			t.Errorf("level %s shape %s floored_by %v, want High, Low, [destructive_migration]", got.Level, got.ShapeLevel, got.FlooredBy)
		}
		want := `[{"class":"destructive_migration","paths":["` + path + `"],"types":["` + op + `"],"floor":"High"}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != want {
			t.Errorf("consequences = %s, want %s", b, want)
		}
		want = `[{"class":"destructive_migration","paths":["` + path + `"],"types":["` + op + `"],"why":"class"}]`
		if b, _ := json.Marshal(got.Triggers); string(b) != want {
			t.Errorf("triggers = %s, want %s", b, want)
		}
		md := run(t, dir, "--format", "markdown")
		if w := "- destructive_migration, floor High: paths `" + path + "`; types `" + op + "`"; !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
		return dir
	}

	t.Run("DropColumn in a migration", func(t *testing.T) {
		high(t, map[string]string{"src/App/Migrations/20240102_DropLegacy.cs": efDropLegacy},
			"src/App/Migrations/20240102_DropLegacy.cs", "DropColumn")
	})

	t.Run("DROP TABLE in a .sql file", func(t *testing.T) {
		dir := high(t, map[string]string{"db/migrations/002_drop.sql": "-- retire the audit log\nDROP TABLE audit_log;\n"},
			"db/migrations/002_drop.sql", "DROP TABLE")

		for _, tt := range []struct {
			name, consequences string
			level              score.Level
		}{
			{"plan naming the class", `,"consequences":["destructive_migration"]`, score.High},
			{"plan naming none", ``, score.Low},
		} {
			plan := filepath.Join(t.TempDir(), "plan.json")
			if err := os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"db/migrations/002_drop.sql","added":2`+tt.consequences+`}]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out, stderr := runEstimate(t, bin, dir, plan)
			if code != 0 {
				t.Fatalf("%s: estimate exit %d: %s", tt.name, code, stderr)
			}
			est := decodeReport(t, out)
			if est.Level != tt.level {
				t.Errorf("%s: estimate level %s, want %s", tt.name, est.Level, tt.level)
			}
			if tt.level == score.High {
				want := `[{"class":"destructive_migration","paths":["db/migrations/002_drop.sql"],"types":[],"floor":"High"}]`
				if b, _ := json.Marshal(est.Consequences); string(b) != want {
					t.Errorf("%s: consequences = %s, want %s", tt.name, b, want)
				}
			} else if len(est.Consequences) != 0 {
				t.Errorf("%s: consequences = %+v, want none", tt.name, est.Consequences)
			}
		}
	})

	t.Run("carries nothing", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{
			"src/App/Migrations/20240102_AddNote.cs": efAddNote,
			"db/migrations/002_notes.sql":            "CREATE TABLE notes (id int);\n-- DROP TABLE notes;\n/* DROP TABLE notes; */\n",
			"db/migrations/001_init.sql":             sqlInitHead,
		})
		out := run(t, dir)
		got := decodeReport(t, out)
		if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) || !strings.Contains(out, `"floored_by": []`) || !strings.Contains(out, `"triggers": []`) {
			t.Errorf("level %s, want Low with no consequence, floor or trigger:\n%s", got.Level, out)
		}
		if md := run(t, dir, "--format", "markdown"); strings.Contains(md, report.FloorHeading) || strings.Contains(md, "destructive_migration") {
			t.Errorf("markdown names a consequence:\n%s", md)
		}
	})
}
