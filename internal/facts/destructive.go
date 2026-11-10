package facts

import (
	"regexp"
	"strings"
)

// migrationCode is where an EF Core migration lives: a C# file under a
// Migrations directory, case-sensitive like the migrations area (areas.go).
var migrationCode = CompileGlobs([]string{"**/Migrations/**/*.cs"})

// isMigrationCode reports whether a path is an EF Core migration.
func isMigrationCode(p string) bool { return migrationCode.Match(p) }

// isSQL reports whether a path is a SQL file.
func isSQL(p string) bool { return strings.HasSuffix(p, ".sql") }

// readsDestructive reports whether destructive_migration reads a path.
func readsDestructive(p string) bool { return isMigrationCode(p) || isSQL(p) }

var (
	// builderCall matches a destructive migrationBuilder call; the method
	// name is the operation.
	builderCall = regexp.MustCompile(`\bmigrationBuilder\s*\.\s*(DeleteData|DropColumn|DropSchema|DropTable|RenameColumn|RenameTable)\b`)
	sqlDrop     = regexp.MustCompile(`(?i)\bDROP\s+(TABLE|COLUMN|SCHEMA|DATABASE|VIEW)\b`)
	sqlDelete   = regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`)
	// sqlTruncate is TRUNCATE not followed by "(": Math.Truncate(x) and
	// MySQL's TRUNCATE(x, d) are not statements.
	sqlTruncate = regexp.MustCompile(`(?i)\bTRUNCATE\b\s*(?:[^\s(]|$)`)
	sqlAlter    = regexp.MustCompile(`(?i)\bALTER\s+TABLE\b`)
	sqlDropWord = regexp.MustCompile(`(?i)\bDROP\b`)
	downHeader  = regexp.MustCompile(`\bvoid\s+Down\s*\(`)
)

// destructiveOperations returns, for each line of content (index 0 is line
// 1), the destructive operations it carries, or nil when none. p decides how
// it is read: a migration's (isMigrationCode) "//" comments, builder calls
// and Down method, and in both kinds "/* */" and "--" comments and the SQL
// statements. It is a line walk, not a C# or SQL parser: strings are not
// tracked, so quoted SQL is read as SQL and a comment marker in a string
// starts a comment.
func destructiveOperations(p string, content []byte) [][]string {
	cs := isMigrationCode(p)
	raw := strings.Split(string(content), "\n")
	lines := blankComments(raw, cs)
	out := make([][]string, len(lines))
	down, downIndent, alter := false, 0, false
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		trimmed := strings.TrimSpace(l)
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		if cs {
			// Down is the rollback: scaffolding writes a drop there for every
			// additive migration, so its lines carry nothing.
			if down {
				if trimmed == "}" && indent <= downIndent {
					down = false
				}
				continue
			}
			if downHeader.MatchString(l) {
				if !strings.HasSuffix(trimmed, "}") && !strings.HasSuffix(trimmed, ";") {
					down, downIndent = true, indent
				}
				alter = false
				continue
			}
		}
		var ops []string
		add := func(op string) {
			for _, o := range ops {
				if o == op {
					return
				}
			}
			ops = append(ops, op)
		}
		if cs {
			for _, m := range builderCall.FindAllStringSubmatch(l, -1) {
				add(m[1])
			}
		}
		for _, m := range sqlDrop.FindAllStringSubmatch(l, -1) {
			add("DROP " + strings.ToUpper(m[1]))
		}
		if sqlDelete.MatchString(l) {
			add("DELETE FROM")
		}
		if sqlTruncate.MatchString(l) {
			add("TRUNCATE")
		}
		// An ALTER TABLE statement runs to its ";" or a blank line; a DROP
		// in it, on any of its lines, marks that line. A comment line is not
		// a blank line.
		if strings.TrimSpace(raw[i]) == "" {
			alter = false
		}
		segs := strings.Split(l, ";")
		for j, s := range segs {
			text, open := s, alter
			if loc := sqlAlter.FindStringIndex(s); loc != nil {
				text, open = s[loc[1]:], true
			}
			if open && sqlDropWord.MatchString(text) {
				add("ALTER TABLE DROP")
			}
			alter = open && j == len(segs)-1
		}
		out[i] = ops
	}
	return out
}

// blankComments returns lines with their comments replaced by spaces: "/* */"
// (carried across lines) and "--" to the end of the line, and in C# (cs)
// "//" to the end of the line too.
func blankComments(lines []string, cs bool) []string {
	out := make([]string, len(lines))
	block := false
	for i, l := range lines {
		b := []byte(l)
		for j := 0; j < len(b); j++ {
			if block {
				if b[j] == '*' && j+1 < len(b) && b[j+1] == '/' {
					b[j], b[j+1] = ' ', ' '
					j++
					block = false
				} else if b[j] != '\t' {
					b[j] = ' '
				}
				continue
			}
			if j+1 >= len(b) {
				break
			}
			two := string(b[j : j+2])
			switch {
			case two == "/*":
				b[j], b[j+1] = ' ', ' '
				j++
				block = true
			case two == "--" || cs && two == "//":
				for k := j; k < len(b); k++ {
					if b[k] != '\t' {
						b[k] = ' '
					}
				}
				j = len(b)
			}
		}
		out[i] = string(b)
	}
	return out
}
