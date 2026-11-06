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

// treeBlobs lists the regular-file blobs (modes 100644 and 100755) in the
// tree of commit; symlinks and gitlinks are not files and are left out.
func treeBlobs(ctx context.Context, repo, commit string) ([]blob, error) {
	out, err := git(ctx, repo, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	var blobs []blob
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("facts: malformed ls-tree record %q", rec)
		}
		if fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			continue
		}
		blobs = append(blobs, blob{path: p, sha: fields[2]})
	}
	return blobs, nil
}
