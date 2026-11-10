package facts

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const migPath = "src/App/Migrations/20240102_Change.cs"

// destructiveCases are the lines destructive_migration reads, each with the
// "line:operation" marks the criteria expect.
var destructiveCases = []struct {
	name, path string
	lines      []string
	want       []string
}{
	{"each builder call", migPath, []string{
		`protected override void Up(MigrationBuilder migrationBuilder)`,
		`{`,
		`    migrationBuilder.DropColumn(name: "Legacy", table: "Users");`,
		`    migrationBuilder.DropTable(name: "Old");`,
		`    migrationBuilder.DropSchema(name: "audit");`,
		`    migrationBuilder.RenameTable(name: "A", newName: "B");`,
		`    migrationBuilder.RenameColumn(name: "a", table: "T", newName: "b");`,
		`    migrationBuilder.DeleteData(table: "T", keyColumn: "Id", keyValue: 1);`,
		`    migrationBuilder . DropTable("Spaced");`,
		`}`,
	}, []string{"3:DropColumn", "4:DropTable", "5:DropSchema", "6:RenameTable", "7:RenameColumn", "8:DeleteData", "9:DropTable"}},
	{"multi-line call marks only its line", migPath, []string{
		`migrationBuilder.DropColumn(`,
		`    name: "Legacy",`,
		`    table: "Users");`,
	}, []string{"1:DropColumn"}},
	{"Sql with a destructive statement", migPath, []string{
		`migrationBuilder.Sql("DROP VIEW v");`,
		`migrationBuilder.Sql(@"`,
		`    UPDATE t SET a = 1;`,
		`    DELETE FROM sessions WHERE expired = 1;`,
		`");`,
	}, []string{"1:DROP VIEW", "4:DELETE FROM"}},
	{"each SQL statement, any case and spacing", "db/a.sql", []string{
		`DROP TABLE a;`,
		`drop   column b;`,
		`Drop Schema s;`,
		`DROP DATABASE d;`,
		`drop view v;`,
		`TRUNCATE TABLE t;`,
		`truncate t;`,
		`delete  from t;`,
		`ALTER TABLE t DROP CONSTRAINT c;`,
		`TRUNCATE`,
	}, []string{"1:DROP TABLE", "2:DROP COLUMN", "3:DROP SCHEMA", "4:DROP DATABASE", "5:DROP VIEW",
		"6:TRUNCATE", "7:TRUNCATE", "8:DELETE FROM", "9:ALTER TABLE DROP", "10:TRUNCATE"}},
	{"ALTER TABLE DROP COLUMN reports both", "db/a.sql", []string{
		`ALTER TABLE t DROP COLUMN c;`,
	}, []string{"1:ALTER TABLE DROP", "1:DROP COLUMN"}},
	{"ALTER TABLE across lines", "db/a.sql", []string{
		`ALTER TABLE t`,
		`  -- the old constraint`,
		`  DROP CONSTRAINT c;`,
		`ALTER TABLE u`,
		`  ADD x int`,
		`  DROP CONSTRAINT d`,
		``,
		`DROP INDEX i`,
	}, []string{"3:ALTER TABLE DROP", "6:ALTER TABLE DROP"}},
	{"ALTER TABLE ends at a blank line", "db/a.sql", []string{
		`ALTER TABLE t ADD c int`,
		``,
		`DROP INDEX i`,
	}, nil},
	{"helper after Down is read", migPath, []string{
		`    protected override void Down(MigrationBuilder migrationBuilder)`,
		`    {`,
		`        migrationBuilder.DropTable(name: "New");`,
		`        if (x) {`,
		`        }`,
		`        migrationBuilder.DropColumn(name: "C", table: "T");`,
		`    }`,
		`    private static void Helper(MigrationBuilder migrationBuilder)`,
		`    {`,
		`        migrationBuilder.DropTable(name: "Gone");`,
		`    }`,
	}, []string{"10:DropTable"}},
	{"one-line Down does not swallow what follows", migPath, []string{
		`    protected override void Down(MigrationBuilder migrationBuilder) => migrationBuilder.DropTable("New");`,
		`    protected override void Up(MigrationBuilder migrationBuilder)`,
		`    {`,
		`        migrationBuilder.DropTable("Gone");`,
		`    }`,
		`    void Down(MigrationBuilder migrationBuilder) { migrationBuilder.DropTable("X"); }`,
		`    migrationBuilder.DropColumn("A", "B");`,
	}, []string{"4:DropTable", "7:DropColumn"}},
	{"positive control: a trailing comment", "db/a.sql", []string{
		`DROP TABLE a; -- gone`,
	}, []string{"1:DROP TABLE"}},

	// AC2: these carry nothing.
	{"scaffolded AddColumn migration", migPath, []string{
		`public partial class AddNote : Migration`,
		`{`,
		`    protected override void Up(MigrationBuilder migrationBuilder)`,
		`    {`,
		`        migrationBuilder.AddColumn<string>(`,
		`            name: "Note",`,
		`            table: "Users",`,
		`            nullable: true);`,
		`    }`,
		``,
		`    protected override void Down(MigrationBuilder migrationBuilder)`,
		`    {`,
		`        migrationBuilder.DropColumn(`,
		`            name: "Note",`,
		`            table: "Users");`,
		`    }`,
		`}`,
	}, nil},
	{"CreateTable, DropIndex, DropForeignKey", migPath, []string{
		`migrationBuilder.CreateTable(name: "T", columns: table => new { });`,
		`migrationBuilder.DropIndex(name: "IX_T", table: "T");`,
		`migrationBuilder.DropForeignKey(name: "FK_T", table: "T");`,
	}, nil},
	{"commented-out builder calls", migPath, []string{
		`// migrationBuilder.DropTable(name: "Old");`,
		`/* migrationBuilder.DropColumn(name: "A", table: "T"); */`,
		`/*`,
		`migrationBuilder.DropTable(name: "Old");`,
		`*/`,
		`x = 1; // DROP TABLE t`,
	}, nil},
	{"another builder and Math.Truncate", migPath, []string{
		`builder.DropTable(name: "Old");`,
		`var y = Math.Truncate(x);`,
	}, nil},
	{"non-destructive SQL", "db/a.sql", []string{
		`CREATE TABLE a (id int);`,
		`ALTER TABLE t ADD c int;`,
		`DROP INDEX i;`,
		`DROP PROCEDURE p;`,
		`SELECT TRUNCATE(x, 2);`,
		`SELECT truncate (x, 2);`,
	}, nil},
	{"SQL comments", "db/a.sql", []string{
		`-- DROP TABLE a`,
		`/* DROP TABLE a */`,
		`/*`,
		`DROP TABLE a;`,
		`TRUNCATE b;`,
		`*/`,
		`SELECT 1; -- DROP TABLE a`,
	}, nil},
}

func TestDestructiveOperations(t *testing.T) {
	for _, tt := range destructiveCases {
		got := marks(destructiveOperations(tt.path, []byte(strings.Join(tt.lines, "\n"))))
		want := append([]string(nil), tt.want...)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", tt.name, got, want)
		}
	}
}

// marks flattens per-line operations into sorted "line:operation" strings.
func marks(ops [][]string) []string {
	var out []string
	for i, l := range ops {
		for _, op := range l {
			out = append(out, fmt.Sprintf("%d:%s", i+1, op))
		}
	}
	sort.Strings(out)
	return out
}

// TestDestructiveCatalog pins the catalog's operations to those the cases
// emit, and the paths the class reads.
func TestDestructiveCatalog(t *testing.T) {
	emitted := map[string]bool{}
	for _, tt := range destructiveCases {
		for _, l := range destructiveOperations(tt.path, []byte(strings.Join(tt.lines, "\n"))) {
			for _, op := range l {
				emitted[op] = true
			}
		}
	}
	var catalog []string
	for _, c := range builtinConsequences {
		if c.class == ConsequenceDestructiveMigration {
			catalog = append([]string(nil), c.operations...)
		}
	}
	sort.Strings(catalog)
	if got := sorted(emitted); !reflect.DeepEqual(got, catalog) {
		t.Errorf("emitted operations %v, catalog %v", got, catalog)
	}
	if !IsConsequence(ConsequenceDestructiveMigration) {
		t.Error("IsConsequence(destructive_migration) is false")
	}
	for p, want := range map[string]bool{
		"Migrations/A.cs":              true,
		"src/Data/Migrations/Sub/A.cs": true,
		"a.sql":                        true,
		"db/migrations/001.sql":        true,
		"src/migrations/a.cs":          false,
		"Migrations/a.txt":             false,
		"a.SQL":                        false,
		"src/Data/Drop.cs":             false,
	} {
		if got := readsDestructive(p); got != want {
			t.Errorf("readsDestructive(%q) = %v, want %v", p, got, want)
		}
		if got := readable(p); got != want {
			t.Errorf("readable(%q) = %v, want %v", p, got, want)
		}
	}
}
