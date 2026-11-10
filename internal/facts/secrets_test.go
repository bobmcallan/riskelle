package facts

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Every secret-shaped sample is built at runtime from parts, so no complete
// token sits in this file and riskelle's own source carries no secret.
var (
	b36   = strings.Repeat("A", 36)
	keyHd = "-----BEGIN " // the private key header, split in two
	keyTl = "PRIVATE KEY-----"
)

// TestSecretRules pins each rule: a full-length sample matches on line 1
// alone, and a sample short of the rule, a placeholder or a lookalike gives
// nil. Rule names are sorted and distinct.
func TestSecretRules(t *testing.T) {
	for _, tt := range []struct{ rule, line string }{
		{"aws_access_key_id", "key = " + "AK" + "IA" + strings.Repeat("Z", 16)},
		{"aws_access_key_id", "AS" + "IA" + strings.Repeat("7", 16)},
		{"azure_storage_account_key", "DefaultEndpointsProtocol=https;Account" + "Key=" + strings.Repeat("a", 86) + "==;"},
		{"github_token", "token: " + "gh" + "p_" + b36},
		{"github_token", "gh" + "o_" + b36},
		{"github_token", "gh" + "s_" + b36},
		{"github_token", "gh" + "u_" + b36},
		{"github_token", "github" + "_pat_" + strings.Repeat("B", 22) + "_" + strings.Repeat("c", 59)},
		{"google_api_key", "AI" + "za" + strings.Repeat("x", 35)},
		{"private_key", keyHd + keyTl},
		{"private_key", keyHd + "RSA " + keyTl},
		{"private_key", keyHd + "OPENSSH " + keyTl},
		{"private_key", keyHd + "PGP " + keyTl[:len(keyTl)-5] + " BLOCK-----"},
		{"slack_token", "xo" + "xb-" + "1234567890-" + strings.Repeat("q", 24)},
	} {
		if got, want := secretLines([]byte(tt.line)), map[string][]int{tt.rule: {1}}; !reflect.DeepEqual(got, want) {
			t.Errorf("rule %s: secretLines = %v, want %v", tt.rule, got, want)
		}
	}
	for _, line := range []string{
		"AK" + "IA" + strings.Repeat("Z", 15),            // one short
		"AK" + "IA" + strings.Repeat("Z", 17),            // a 17th trailing character
		"X" + "AK" + "IA" + strings.Repeat("Z", 16),      // a leading letter
		"Account" + "Key=" + strings.Repeat("a", 85),     // one short
		"gh" + "p_" + strings.Repeat("A", 35),            // one short
		"gh" + "p_xxx",                                   // a placeholder
		"github" + "_pat_" + strings.Repeat("B", 81),     // one short
		"AI" + "za" + strings.Repeat("x", 34),            // one short
		"xo" + "xb-your-token-here",                      // no numeric id
		"xo" + "xb-1234567890-" + strings.Repeat("q", 9), // one short
		keyHd + "PUBLIC KEY-----",                        // a public key
		keyHd + "CERTIFICATE-----",                       // a certificate
		"-----BEGIN ... " + keyTl,                        // the story's own shorthand
		"",
	} {
		if got := secretLines([]byte(line)); got != nil {
			t.Errorf("secretLines(%d-char line) = %v, want nil", len(line), got)
		}
	}
	var names []string
	for _, r := range secretRules {
		names = append(names, r.name)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("rules not sorted: %v", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Errorf("rule %s twice", names[i])
		}
	}
}

// TestSecretLines pins line numbering over multi-line content: two rules on
// different lines, one line ending in CR, a rule matching twice.
func TestSecretLines(t *testing.T) {
	content := strings.Join([]string{
		"a = 1",                        // 1
		"tok = " + "gh" + "p_" + b36,   // 2
		keyHd + "EC " + keyTl + "\r",   // 3
		"body",                         // 4
		"again = " + "gh" + "s_" + b36, // 5
		"",                             // 6
	}, "\n")
	want := map[string][]int{"github_token": {2, 5}, "private_key": {3}}
	if got := secretLines([]byte(content)); !reflect.DeepEqual(got, want) {
		t.Errorf("secretLines = %v, want %v", got, want)
	}
}

// TestOwnSourceCarriesNoSecret pins that riskelle practises the rule: the
// rules, their tests and the README match none of them.
func TestOwnSourceCarriesNoSecret(t *testing.T) {
	for _, p := range []string{"secrets.go", "secrets_test.go", "../../tests/secret_added_test.go", "../../README.md"} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := secretLines(data); got != nil {
			t.Errorf("%s matches secret rules %v", p, got)
		}
	}
}
