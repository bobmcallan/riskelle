package facts

import (
	"context"
	"fmt"
	"strings"
)

// ReadFileAt returns the contents of the repo-relative path p in the tree of
// commit, reading only the repository: never the working tree, no filters. A
// path absent there gives found false; one that is not a regular file is an
// error.
func ReadFileAt(ctx context.Context, repo, commit, p string) (data []byte, found bool, err error) {
	sha, err := resolve(ctx, repo, commit)
	if err != nil {
		return nil, false, err
	}
	out, err := git(ctx, repo, "ls-tree", "-z", "--full-tree", sha, "--", p)
	if err != nil || len(out) == 0 {
		return nil, false, err
	}
	meta, _, _ := strings.Cut(string(out), "\t") // "<mode> <type> <oid>\t<path>\x00"
	fields := strings.Fields(meta)
	if len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return nil, false, fmt.Errorf("facts: %s at %s is not a regular file", p, sha)
	}
	data, err = git(ctx, repo, "cat-file", "blob", fields[2])
	return data, err == nil, err
}
