package facts

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// secretRule is one high-precision pattern for a secret written into a file:
// its name, which is the secret_added consequence's type, and a pattern that
// matches within one line.
type secretRule struct {
	name string
	re   *regexp.Regexp
}

// secretRules are the built-in secret rules, sorted by name and compiled
// once. Each length is the real token's, so a shorter placeholder does not
// match; no pattern matches its own source text.
var secretRules = []secretRule{
	{"aws_access_key_id", regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"azure_storage_account_key", regexp.MustCompile(`AccountKey=[A-Za-z0-9+/]{86,}`)},
	{"github_token", regexp.MustCompile(`\b(?:gh[opsu]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{82,})`)},
	{"google_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
	{"private_key", regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`)},
	{"slack_token", regexp.MustCompile(`\bxox[abprs]-[0-9]+-[0-9A-Za-z-]{10,}`)},
}

// secretLines returns, for each secret rule that matches a line of content,
// the 1-based numbers of the lines it matches; nil when none does. Only
// whether a line matches is kept, never the matched text.
func secretLines(content []byte) map[string][]int {
	var out map[string][]int
	for i, l := range strings.Split(string(content), "\n") {
		for _, r := range secretRules {
			if r.re.MatchString(l) {
				if out == nil {
					out = map[string][]int{}
				}
				out[r.name] = append(out[r.name], i+1)
			}
		}
	}
	return out
}

// collectSecrets calls hit(secret_added, path, rule) for each secret rule
// that matches a line added at head of a changed text file. A binary file, a
// file with no added line, a removed line and an untouched line carry
// nothing. Head blobs are read first, in one batch, and only a file with a
// match is diffed for its added lines.
func collectSecrets(ctx context.Context, repo, mergeBase, head string, files []File, hit func(class, p, typ string)) error {
	byPath := map[string]File{}
	for _, f := range files {
		if !f.Binary && f.Added > 0 {
			byPath[f.Path] = f
		}
	}
	if len(byPath) == 0 {
		return nil
	}
	all, err := treeBlobs(ctx, repo, head)
	if err != nil {
		return err
	}
	var blobs []blob
	for _, b := range all {
		if _, ok := byPath[b.path]; ok {
			blobs = append(blobs, b)
		}
	}
	matched := map[string]map[string][]int{}
	err = catBlobs(ctx, repo, blobs, func(p string, content []byte) {
		if m := secretLines(content); m != nil {
			matched[p] = m
		}
	})
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(matched))
	for p := range matched {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		_, added, err := changedLines(ctx, repo, mergeBase, head, byPath[p])
		if err != nil {
			return err
		}
		isAdded := map[int]bool{}
		for _, n := range added {
			isAdded[n] = true
		}
		for rule, lines := range matched[p] {
			for _, n := range lines {
				if isAdded[n] {
					hit(ConsequenceSecretAdded, p, rule)
					break
				}
			}
		}
	}
	return nil
}
