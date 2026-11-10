package facts

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The two classes a definition write carries.
const (
	upd = ConsequenceDefinitionUpdate
	wr  = ConsequenceDefinitionWrite
)

// written returns the 1-based line numbers definitionWrites marks, each with
// its class and type, as "nn:class type".
func written(content string) []string {
	var out []string
	for i, w := range definitionWrites([]byte(content)) {
		if w.typ != "" {
			out = append(out, fmt.Sprintf("%02d:%s %s", i+1, w.class, w.typ))
		}
	}
	return out
}

// every marks lines from..to with class and typ.
func every(from, to int, class, typ string) []string {
	var out []string
	for n := from; n <= to; n++ {
		out = append(out, fmt.Sprintf("%02d:%s %s", n, class, typ))
	}
	return out
}

// releasePut is a continued az devops invoke PUT of a release definition, as
// a ticket script writes it.
const releasePut = "az devops invoke \\\n" +
	"  --area release \\\n" +
	"  --resource definitions \\\n" +
	"  --route-parameters project=platform definitionId=8 \\\n" +
	"  --org \"$org\" \\\n" +
	"  --http-method PUT \\\n" +
	"  --in-file release-8.new.json \\\n" +
	"  --api-version 7.1\n"

func TestDefinitionWrites(t *testing.T) {
	const invokePut = "az devops invoke --area release --http-method PUT"
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		// AC1: az devops invoke
		{"continued release PUT", releasePut, every(1, 8, upd, invokePut)},
		{"continued release GET", strings.Replace(releasePut, "PUT", "GET", 1), nil},
		{"no http method", strings.Replace(releasePut, "  --http-method PUT \\\n", "", 1), nil},
		{"motivating case", "az devops invoke --org \"$org\" --area release --resource definitions \\\n" +
			"  --route-parameters project=\"$project\" --http-method PUT \\\n" +
			"  --in-file \"$out/intended.json\" --api-version 7.1 -o json > \"$out/put.json\"\n",
			every(1, 3, upd, invokePut)},
		{"motivating GET", "az devops invoke --org \"$org\" --area release --resource definitions \\\n" +
			"  --route-parameters project=\"$project\" definitionId=8 -o json > \"$out/before.json\"\n", nil},
		{"build POST with equals", "az devops invoke --area=build --resource definitions --http-method=post\n",
			every(1, 1, wr, "az devops invoke --area build --http-method POST")},
		{"quoted values", "az devops invoke --area 'release' --http-method \"DELETE\"\n",
			every(1, 1, wr, "az devops invoke --area release --http-method DELETE")},
		{"git area", "az devops invoke --area git --resource refs --http-method POST\n", nil},
		{"crlf continuation", "az devops invoke \\\r\n  --area release \\\r\n  --http-method PATCH\r\n",
			every(1, 3, wr, "az devops invoke --area release --http-method PATCH")},
		{"only the write command", "echo start\n" + releasePut + "echo done\n",
			every(2, 9, upd, invokePut)},
		{"in command substitution", "out=$(az devops invoke --area release --http-method PUT --in-file x)\n",
			every(1, 1, wr, invokePut)},
		{"resource with equals, PATCH", "az devops invoke --area build --resource=definitions --http-method PATCH\n",
			every(1, 1, upd, "az devops invoke --area build --http-method PATCH")},
		{"quoted resource", "az devops invoke --area release --resource \"definitions\" --http-method PUT\n",
			every(1, 1, upd, invokePut)},
		{"another resource", strings.Replace(strings.Replace(releasePut, "definitions", "approvals", 1), "PUT", "PATCH", 1),
			every(1, 8, wr, "az devops invoke --area release --http-method PATCH")},
		{"no resource", strings.Replace(releasePut, "  --resource definitions \\\n", "", 1), every(1, 7, wr, invokePut)},
		{"resource group, no resource", strings.Replace(releasePut, "--resource definitions", "--resource-group rg", 1),
			every(1, 8, wr, invokePut)},
		{"a longer resource name", strings.Replace(releasePut, "--resource definitions", "--resource definitions/x", 1),
			every(1, 8, wr, invokePut)},

		// AC2: az pipelines
		{"pipelines update", "az pipelines update --id 12 --branch main\n", every(1, 1, upd, "az pipelines update")},
		{"pipelines create", "az pipelines create --name x --yml-path a.yml\n", every(1, 1, wr, "az pipelines create")},
		{"pipelines delete", "az pipelines delete --id 12 --yes\n", every(1, 1, wr, "az pipelines delete")},
		{"pipelines release create", "az pipelines release create --definition-id 8\n", every(1, 1, wr, "az pipelines release create")},
		{"pipelines release update", "az pipelines release update --release-id 3\n", every(1, 1, wr, "az pipelines release update")},
		{"pipelines release delete", "az pipelines release delete --id 3\n", every(1, 1, wr, "az pipelines release delete")},
		{"yaml script step", "- script: az pipelines update --id 12\n", every(1, 1, upd, "az pipelines update")},
		{"pipelines list", "az pipelines list -o table\n", nil},
		{"pipelines show", "az pipelines show --id 12\n", nil},
		{"pipelines run", "az pipelines run --id 12\n", nil},
		{"pipelines variable create", "az pipelines variable create --pipeline-id 12 --name x\n", nil},
		{"pipelines release list", "az pipelines release list\n", nil},

		// AC2: REST writes
		{"curl PUT build", "curl -X PUT -H 'Content-Type: application/json' \\\n  -d @def.json \\\n  \"$url/_apis/build/definitions/12?api-version=7.1\"\n",
			every(1, 3, upd, "curl PUT _apis/build/definitions")},
		{"curl -sSX PATCH release", "curl -sSX PATCH \"$vsrm/_apis/release/definitions?api-version=7.1\" -d @d.json\n",
			every(1, 1, upd, "curl PATCH _apis/release/definitions")},
		{"curl --request DELETE", "curl --request DELETE https://dev.azure.com/o/p/_apis/build/definitions/3\n",
			every(1, 1, wr, "curl DELETE _apis/build/definitions")},
		{"curl -XPOST", "curl -XPOST https://dev.azure.com/o/p/_apis/build/definitions\n",
			every(1, 1, wr, "curl POST _apis/build/definitions")},
		{"curl GET", "curl -X GET https://dev.azure.com/o/p/_apis/build/definitions/3\n", nil},
		{"curl no method", "curl https://dev.azure.com/o/p/_apis/build/definitions/3\n", nil},
		{"curl implied POST", "curl -d @d.json https://dev.azure.com/o/p/_apis/build/definitions\n", nil},
		{"curl PUT elsewhere", "curl -X PUT https://dev.azure.com/o/p/_apis/git/repositories/r\n", nil},
		{"Invoke-RestMethod Put", "Invoke-RestMethod -Method Put -Uri \"$base/_apis/release/definitions?api-version=7.1\" -Body $b\n",
			every(1, 1, upd, "Invoke-RestMethod PUT _apis/release/definitions")},
		{"Invoke-RestMethod Delete", "Invoke-RestMethod -Method Delete -Uri \"$base/_apis/release/definitions/8\"\n",
			every(1, 1, wr, "Invoke-RestMethod DELETE _apis/release/definitions")},
		{"Invoke-RestMethod Get", "Invoke-RestMethod -Method Get -Uri \"$base/_apis/release/definitions/8\"\n", nil},
		{"az rest put", "az rest --method put --url \"$org/p/_apis/build/definitions/3?api-version=7.1\" --body @d.json\n",
			every(1, 1, upd, "az rest PUT _apis/build/definitions")},
		{"az rest post", "az rest --method post --url \"$org/p/_apis/build/definitions?api-version=7.1\" --body @d.json\n",
			every(1, 1, wr, "az rest POST _apis/build/definitions")},
		{"az rest get", "az rest --method get --url \"$org/p/_apis/build/definitions/3\"\n", nil},

		// AC2: comments
		{"commented write", "# az pipelines update --id 12\n", nil},
		{"indented comment", "  # curl -X PUT $u/_apis/build/definitions\n", nil},
		{"comment ends continuation", "az devops invoke \\\n  --area release \\\n# note\n  --http-method PUT\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := written(tt.content); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	// Every type the rules give starts with a command catalogued under the
	// class it is reported with.
	commands := map[string][]string{}
	for _, c := range builtinConsequences {
		commands[c.class] = c.commands
	}
	for _, tt := range tests {
		for _, w := range definitionWrites([]byte(tt.content)) {
			if w.typ == "" {
				continue
			}
			known := false
			for _, c := range commands[w.class] {
				known = known || strings.HasPrefix(w.typ, c+" ")
			}
			if !known {
				t.Errorf("%s type %q starts with no catalogued command %q", w.class, w.typ, commands[w.class])
			}
		}
	}
}
