package facts

import (
	"regexp"
	"strings"
)

// The definition_update and definition_write rules. Each is matched anywhere in a logical command,
// so a command in $(...) or after "- script:" counts. It is a textual match,
// not a shell parser.
var (
	// az devops invoke with a release or build area and a write method.
	devopsInvoke = regexp.MustCompile(`\baz\s+devops\s+invoke\b`)
	invokeArea   = regexp.MustCompile(`(?:^|\s)--area[ \t=]+["']?((?i:release|build))\b`)
	invokeMethod = regexp.MustCompile(`(?:^|\s)--http-method[ \t=]+["']?((?i:put|post|patch|delete))\b`)
	// the definitions resource itself: not --resource-group, not another
	// resource (approvals, releases) and not a longer name.
	invokeResource = regexp.MustCompile(`(?:^|\s)--resource[ \t=]+["']?(?i:definitions)["']?(?:\s|$)`)

	// az pipelines (release) create, update or delete: the verb directly
	// after the group, so "az pipelines variable create" is not one.
	pipelinesVerb = regexp.MustCompile(`\baz\s+pipelines\s+(release\s+)?(create|update|delete)\b`)

	// curl, az rest or Invoke-RestMethod with a write method against a
	// release or build definitions URL.
	restTool   = regexp.MustCompile(`\b(curl|az\s+rest|(?i:Invoke-RestMethod))\b`)
	restURL    = regexp.MustCompile(`(?i)_apis/(release|build)/definitions`)
	restMethod = regexp.MustCompile(`(?:^|\s)(?:-[A-Za-z]*X|--request|-m|--?(?i:method))[ \t=:]*["']?((?i:put|post|patch|delete))\b`)
)

// definitionCmd is the class and type of a logical command that writes a
// release or build definition; the zero value when it writes none.
type definitionCmd struct{ class, typ string }

// updates reports whether a write method replaces or amends what it names.
func updates(method string) bool {
	return strings.EqualFold(method, "PUT") || strings.EqualFold(method, "PATCH")
}

// definitionWrite returns the class and type of one logical command, or the
// zero value when it writes no release or build definition. The first rule
// that matches gives the type: "az devops invoke --area <area> --http-method
// <METHOD>", "az pipelines [release ]<verb>", or "<tool> <METHOD>
// _apis/<area>/definitions", the tool as catalogued.
//
// The class is definition_update when the command only updates an existing
// definition, which the service keeps every revision of: an invoke PUT or
// PATCH whose --resource is definitions, "az pipelines update", or a PUT or
// PATCH to a definitions URL. Every other write is definition_write: a POST
// or DELETE, an invoke on another resource or none named, and the create,
// delete and release verbs.
func definitionWrite(cmd string) definitionCmd {
	class := func(update bool) string {
		if update {
			return ConsequenceDefinitionUpdate
		}
		return ConsequenceDefinitionWrite
	}
	if devopsInvoke.MatchString(cmd) {
		a, m := invokeArea.FindStringSubmatch(cmd), invokeMethod.FindStringSubmatch(cmd)
		if a != nil && m != nil {
			return definitionCmd{class(updates(m[1]) && invokeResource.MatchString(cmd)),
				"az devops invoke --area " + strings.ToLower(a[1]) + " --http-method " + strings.ToUpper(m[1])}
		}
	}
	if m := pipelinesVerb.FindStringSubmatch(cmd); m != nil {
		if m[1] != "" {
			return definitionCmd{ConsequenceDefinitionWrite, "az pipelines release " + m[2]}
		}
		return definitionCmd{class(m[2] == "update"), "az pipelines " + m[2]}
	}
	t, u, m := restTool.FindStringSubmatch(cmd), restURL.FindStringSubmatch(cmd), restMethod.FindStringSubmatch(cmd)
	if t == nil || u == nil || m == nil {
		return definitionCmd{}
	}
	tool := t[1]
	switch {
	case strings.EqualFold(tool, "Invoke-RestMethod"):
		tool = "Invoke-RestMethod"
	case strings.HasPrefix(tool, "az"):
		tool = "az rest"
	}
	return definitionCmd{class(updates(m[1])), tool + " " + strings.ToUpper(m[1]) + " _apis/" + strings.ToLower(u[1]) + "/definitions"}
}

// definitionWrites returns, for each line of content (index 0 is line 1), the
// class (definition_update or definition_write) and type of the logical
// command the line belongs to, or the zero value.
// A logical command is a line and each following line while the line (less a
// trailing "\r") ends in a backslash, which joins as a space; every line of a
// matching command carries its class and type. A full-line comment (first non-blank
// character "#") carries nothing, is never part of a command, and ends an
// open continuation.
func definitionWrites(content []byte) []definitionCmd {
	lines := strings.Split(string(content), "\n")
	out := make([]definitionCmd, len(lines))
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
		if c := definitionWrite(cmd.String()); c.typ != "" {
			for k := i; k < j; k++ {
				out[k] = c
			}
		}
		i = j
	}
	return out
}
