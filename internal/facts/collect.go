package facts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Collect gathers the facts of the change from base to head in the git
// repository at repo. The change is merge-base(base, head)..head: what a pull
// request from head into base shows, without trunk commits made after the
// branch point. Collect reads only the repository: no clock, no network.
func Collect(ctx context.Context, repo, base, head string) (Facts, error) {
	baseSHA, err := resolve(ctx, repo, base)
	if err != nil {
		return Facts{}, err
	}
	headSHA, err := resolve(ctx, repo, head)
	if err != nil {
		return Facts{}, err
	}
	mb, err := git(ctx, repo, "merge-base", baseSHA, headSHA)
	if err != nil {
		return Facts{}, fmt.Errorf("facts: no merge base between %q and %q: %w", base, head, err)
	}
	mergeBase := strings.TrimSpace(string(mb))

	// Every setting that changes the diff is pinned by flag, so user or repo
	// config cannot change the facts; attributes come from the head commit,
	// not the working tree.
	out, err := git(ctx, repo, "--attr-source="+headSHA, "diff", "--numstat", "-z",
		"--no-color", "--no-ext-diff", "--no-textconv", "--no-relative",
		"--ignore-submodules=none", "--diff-algorithm=myers", "-M50%", "-l1000",
		mergeBase, headSHA)
	if err != nil {
		return Facts{}, err
	}
	files, err := parseNumstat(out)
	if err != nil {
		return Facts{}, err
	}
	// A file is classified by its head path; a rename's old path is not.
	for i := range files {
		files[i].Classes = Classify(files[i].Path)
	}

	roots := map[string]bool{}
	for _, commit := range []string{mergeBase, headSHA} {
		rs, err := manifestRoots(ctx, repo, commit)
		if err != nil {
			return Facts{}, err
		}
		for _, r := range rs {
			roots[r] = true
		}
	}

	var touched []string
	for _, f := range files {
		touched = append(touched, f.Path)
		if f.OldPath != "" {
			touched = append(touched, f.OldPath)
		}
	}
	return Facts{
		Base:        baseSHA,
		MergeBase:   mergeBase,
		Head:        headSHA,
		Files:       files,
		Directories: Directories(touched),
		Projects:    Projects(touched, sorted(roots)),
	}, nil
}

// resolve turns a ref into the full SHA of the commit it names.
func resolve(ctx context.Context, repo, ref string) (string, error) {
	out, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("facts: cannot resolve %q to a commit: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// manifestRoots lists the directories holding a project manifest at commit.
func manifestRoots(ctx context.Context, repo, commit string) ([]string, error) {
	out, err := git(ctx, repo, "ls-tree", "-r", "-z", "--name-only", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" && IsManifest(path.Base(p)) {
			roots = append(roots, path.Dir(p))
		}
	}
	return roots, nil
}

// parseNumstat parses `git diff --numstat -z` output into files sorted by path.
// A normal record is "A\tR\tpath\0"; a rename is "A\tR\t\0old\0new\0"; a binary
// file has "-" for both counts.
func parseNumstat(out []byte) ([]File, error) {
	files := []File{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if rec == "" && i == len(fields)-1 {
			break
		}
		parts := strings.SplitN(rec, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("facts: malformed numstat record %q", rec)
		}
		var f File
		if parts[0] == "-" && parts[1] == "-" {
			f.Binary = true
		} else {
			a, errA := strconv.Atoi(parts[0])
			r, errR := strconv.Atoi(parts[1])
			if errA != nil || errR != nil || a < 0 || r < 0 {
				return nil, fmt.Errorf("facts: malformed numstat counts in %q", rec)
			}
			f.Added, f.Removed = a, r
		}
		if parts[2] != "" {
			f.Path = parts[2]
		} else {
			if i+2 >= len(fields) || fields[i+1] == "" || fields[i+2] == "" {
				return nil, fmt.Errorf("facts: truncated numstat rename record %q", rec)
			}
			f.OldPath, f.Path = fields[i+1], fields[i+2]
			i += 2
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// git runs one git command in repo and returns its stdout.
func git(ctx context.Context, repo string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		sub := args[0]
		if strings.HasPrefix(sub, "--") && len(args) > 1 {
			sub = args[1]
		}
		return nil, fmt.Errorf("facts: git %s: %w: %s", sub, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
