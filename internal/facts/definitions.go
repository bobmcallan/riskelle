package facts

import (
	"regexp"
	"strings"
)

// The definition_write rules. Each is matched anywhere in a logical command,
// so a command in $(...) or after "- script:" counts. It is a textual match,
// not a shell parser.
var (
	// az devops invoke with a release or build area and a write method.
	devopsInvoke = regexp.MustCompile(`\baz\s+devops\s+invoke\b`)
	invokeArea   = regexp.MustCompile(`(?:^|\s)--area[ \t=]+["']?((?i:release|build))\b`)
	invokeMethod = regexp.MustCompile(`(?:^|\s)--http-method[ \t=]+["']?((?i:put|post|patch|delete))\b`)

	// az pipelines (release) create, update or delete: the verb directly
	// after the group, so "az pipelines variable create" is not one.
	pipelinesVerb = regexp.MustCompile(`\baz\s+pipelines\s+(release\s+)?(create|update|delete)\b`)

	// curl, az rest or Invoke-RestMethod with a write method against a
	// release or build definitions URL.
	restTool   = regexp.MustCompile(`\b(curl|az\s+rest|(?i:Invoke-RestMethod))\b`)
	restURL    = regexp.MustCompile(`(?i)_apis/(release|build)/definitions`)
	restMethod = regexp.MustCompile(`(?:^|\s)(?:-[A-Za-z]*X|--request|-m|--?(?i:method))[ \t=:]*["']?((?i:put|post|patch|delete))\b`)
)

// definitionWrite returns the definition_write type of one logical command,
// or "" when it writes no release or build definition. The first rule that
// matches gives the type: "az devops invoke --area <area> --http-method
// <METHOD>", "az pipelines [release ]<verb>", or "<tool> <METHOD>
// _apis/<area>/definitions", the tool as catalogued.
func definitionWrite(cmd string) string {
	if devopsInvoke.MatchString(cmd) {
		a, m := invokeArea.FindStringSubmatch(cmd), invokeMethod.FindStringSubmatch(cmd)
		if a != nil && m != nil {
			return "az devops invoke --area " + strings.ToLower(a[1]) + " --http-method " + strings.ToUpper(m[1])
		}
	}
	if m := pipelinesVerb.FindStringSubmatch(cmd); m != nil {
		if m[1] != "" {
			return "az pipelines release " + m[2]
		}
		return "az pipelines " + m[2]
	}
	t, u, m := restTool.FindStringSubmatch(cmd), restURL.FindStringSubmatch(cmd), restMethod.FindStringSubmatch(cmd)
	if t == nil || u == nil || m == nil {
		return ""
	}
	tool := t[1]
	switch {
	case strings.EqualFold(tool, "Invoke-RestMethod"):
		tool = "Invoke-RestMethod"
	case strings.HasPrefix(tool, "az"):
		tool = "az rest"
	}
	return tool + " " + strings.ToUpper(m[1]) + " _apis/" + strings.ToLower(u[1]) + "/definitions"
}

// definitionWrites returns, for each line of content (index 0 is line 1), the
// definition_write type of the logical command the line belongs to, or "".
// A logical command is a line and each following line while the line (less a
// trailing "\r") ends in a backslash, which joins as a space; every line of a
// matching command carries its type. A full-line comment (first non-blank
// character "#") carries nothing, is never part of a command, and ends an
// open continuation.
func definitionWrites(content []byte) []string {
	lines := strings.Split(string(content), "\n")
	out := make([]string, len(lines))
	for i := 0; i < len(lines); {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			i++
			continue
		}
		var cmd strings.Builder
		j := i
		for ; j < len(lines); j++ {
			l := strings.TrimSuffix(lines[j], "\r")
			if j > i && strings.HasPrefix(strings.TrimSpace(l), "#") {
				break
			}
			cont := strings.HasSuffix(l, `\`)
			cmd.WriteString(strings.TrimSuffix(l, `\`))
			cmd.WriteString(" ")
			if !cont {
				j++
				break
			}
		}
		if typ := definitionWrite(cmd.String()); typ != "" {
			for k := i; k < j; k++ {
				out[k] = typ
			}
		}
		i = j
	}
	return out
}
