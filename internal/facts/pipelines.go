package facts

import (
	"regexp"
	"strings"
)

// pipelineDefinitions are the paths a CI/CD pipeline definition lives at:
// GitHub Actions workflows and Azure Pipelines YAML. A file there is read for
// pipeline_identity only when it also carries a pipelineSignature key.
var pipelineDefinitions = []string{
	".github/workflows/*.yml", ".github/workflows/*.yaml",
	"**/azure-pipelines*.yml", "**/azure-pipelines*.yaml",
	".azuredevops/**/*.yml", ".azuredevops/**/*.yaml",
	"**/pipelines/**/*.yml", "**/pipelines/**/*.yaml",
	"**/.pipelines/**/*.yml", "**/.pipelines/**/*.yaml",
	"**/azure-pipelines/**/*.yml", "**/azure-pipelines/**/*.yaml",
	"**/.azure-pipelines/**/*.yml", "**/.azure-pipelines/**/*.yaml",
}

// pipelineSignature are the column-0 keys of which a pipeline definition has
// at least one, and a docker-compose or Helm file none.
var pipelineSignature = []string{"jobs", "stages", "steps", "extends", "trigger", "pr", "resources", "schedules", "variables", "parameters", "on"}

var compiledPipelines = CompileGlobs(pipelineDefinitions)

// isPipeline reports whether a path is where a pipeline definition lives.
func isPipeline(p string) bool { return compiledPipelines.Match(p) }

// connectionKeys are pipeline_identity's catalog keys that name a service
// connection, as lowercase globs: keys match case-insensitively.
var connectionKeys = func() Globs {
	var ks []string
	for _, c := range builtinConsequences {
		for _, k := range c.keys {
			if strings.Contains(k, "*") {
				ks = append(ks, strings.ToLower(k))
			}
		}
	}
	return CompileGlobs(ks)
}()

// yamlKey matches a key line: indent, list dashes, the key (quoted, or plain
// with no ":" or "#") and an optional value.
var yamlKey = regexp.MustCompile(`^( *)((?:- +)*)("[^"]*"|'[^']*'|[^\s#'"][^:#]*?) *:(?: +(.*))?$`)

// blockScalar matches a value that opens a literal or folded block scalar.
var blockScalar = regexp.MustCompile(`^[|>][-+0-9]*$`)

type yamlFrame struct {
	col             int
	key, item, mark string
}

// pipelineIdentity returns, for each line of content (index 0 is line 1),
// the pipeline_identity mark it carries, or "" when none; nil when content
// has no pipelineSignature key at column 0. It walks indentation, not a YAML
// parser. A key line is marked when its key names a service connection (as
// written), is id-token: write or permissions: write-all, or is a job's
// environment: a direct key of an Azure "- deployment:" item, or
// jobs.<job_id>.environment in a GitHub workflow. Every line nested under a
// marked key carries its mark. Comment and blank lines carry none, and a
// block scalar's content is never read as keys.
func pipelineIdentity(content []byte) []string {
	lines := strings.Split(string(content), "\n")
	signed := false
	for _, l := range lines {
		if m := yamlKey.FindStringSubmatch(strings.TrimSuffix(l, "\r")); m != nil && m[1] == "" && m[2] == "" {
			for _, k := range pipelineSignature {
				signed = signed || unquote(m[3]) == k
			}
		}
	}
	if !signed {
		return nil
	}
	out := make([]string, len(lines))
	var stack []yamlFrame
	scalar := -1 // the column of the key whose block scalar is open, or -1
	top := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1].mark
	}
	pop := func(col int, keep bool) (sibling string) {
		for len(stack) > 0 && stack[len(stack)-1].col >= col {
			if f := stack[len(stack)-1]; f.col == col {
				if keep {
					return sibling
				}
				sibling = f.item
			}
			stack = stack[:len(stack)-1]
		}
		return sibling
	}
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		trimmed := strings.TrimLeft(l, " ")
		indent := len(l) - len(trimmed)
		if scalar >= 0 {
			if trimmed == "" || indent > scalar {
				if trimmed != "" {
					out[i] = top()
				}
				continue
			}
			scalar = -1
		}
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case l == "---" || l == "...":
			stack = nil
			continue
		}
		m := yamlKey.FindStringSubmatch(l)
		if m == nil {
			pop(indent, strings.HasPrefix(trimmed, "- "))
			out[i] = top()
			continue
		}
		col, key := len(m[1])+len(m[2]), unquote(m[3])
		value := m[4]
		if j := strings.Index(value, " #"); j >= 0 {
			value = value[:j]
		}
		value = unquote(strings.TrimSpace(value))
		item := pop(col, false)
		if m[2] != "" {
			item = strings.ToLower(key)
		}
		lower := strings.ToLower(key)
		mark := ""
		switch {
		case connectionKeys.Match(lower):
			mark = key
		case lower == "id-token" && value == "write":
			mark = "id-token: write"
		case lower == "permissions" && value == "write-all":
			mark = "permissions: write-all"
		case lower == "environment" && (item == "deployment" ||
			len(stack) == 2 && stack[0].key == "jobs" && stack[0].col == 0 && stack[1].item == ""):
			mark = "environment"
		}
		if mark == "" {
			mark = top()
		}
		stack = append(stack, yamlFrame{col, lower, item, mark})
		out[i] = mark
		if blockScalar.MatchString(value) {
			scalar = col
		}
	}
	return out
}

// unquote strips one pair of matching quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
