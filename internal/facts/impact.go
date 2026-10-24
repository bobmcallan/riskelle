package facts

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
)

// CollectImpact counts, for the changed paths, how many places in the tree at
// commit depend on them.
//
// A changed non-test .go file under a go.mod (nearest ancestor, with a module
// directive) and outside vendor, testdata and dot or underscore directories
// names its Go package; the package's dependents are the other directories
// whose non-test .go files import it. Every other changed path, except
// _test.go files, is a file; its dependents are the other text files at commit
// that refer to it (see references). Only the git objects of commit are read,
// never the working tree, so uncommitted files cannot change the counts.
func CollectImpact(ctx context.Context, repo, commit string, paths []string) (Impact, error) {
	if len(paths) == 0 {
		return emptyImpact(), nil
	}
	out, err := git(ctx, repo, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return Impact{}, err
	}
	var mods, gos, others []blob
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return Impact{}, fmt.Errorf("facts: malformed ls-tree record %q", rec)
		}
		if fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			continue // symlinks and gitlinks are not files
		}
		b := blob{path: p, sha: fields[2]}
		switch {
		case path.Base(p) == "go.mod":
			mods = append(mods, b)
		case strings.HasSuffix(p, ".go"):
			gos = append(gos, b)
		default:
			others = append(others, b)
		}
	}

	s := newScan(paths)
	// Modules first, so the changed paths split into packages and files; the
	// non-Go blobs are read only when some changed file needs its referrers.
	if err := catBlobs(ctx, repo, mods, s.add); err != nil {
		return Impact{}, err
	}
	rest := gos
	if len(s.changedFiles()) > 0 {
		rest = append(rest, others...)
	}
	if err := catBlobs(ctx, repo, rest, s.add); err != nil {
		return Impact{}, err
	}
	return s.result(), nil
}

func emptyImpact() Impact {
	return Impact{Packages: []Dependency{}, Files: []Dependency{}}
}

type blob struct{ path, sha string }

// catBlobs streams the contents of blobs through one `git cat-file --batch`,
// calling fn for each in order. --batch applies no filters or textconv, so no
// config changes the bytes.
func catBlobs(ctx context.Context, repo string, blobs []blob, fn func(path string, content []byte)) error {
	if len(blobs) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("facts: git cat-file: %w", err)
	}
	go func() {
		w := bufio.NewWriter(stdin)
		for _, b := range blobs {
			if _, err := w.WriteString(b.sha + "\n"); err != nil {
				break
			}
		}
		w.Flush()
		stdin.Close()
	}()

	r := bufio.NewReader(stdout)
	readErr := func() error {
		for _, b := range blobs {
			header, err := r.ReadString('\n')
			if err != nil {
				return fmt.Errorf("facts: git cat-file: reading %s: %w", b.path, err)
			}
			f := strings.Fields(header)
			if len(f) != 3 || f[0] != b.sha || f[1] != "blob" {
				return fmt.Errorf("facts: git cat-file: unexpected header %q for %s", strings.TrimSpace(header), b.path)
			}
			size, err := strconv.Atoi(f[2])
			if err != nil || size < 0 {
				return fmt.Errorf("facts: git cat-file: bad size in %q", strings.TrimSpace(header))
			}
			content := make([]byte, size+1) // the content and its trailing newline
			if _, err := io.ReadFull(r, content); err != nil {
				return fmt.Errorf("facts: git cat-file: reading %s: %w", b.path, err)
			}
			fn(b.path, content[:size])
		}
		return nil
	}()
	if readErr != nil {
		cancel()
		_ = cmd.Wait()
		return readErr
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("facts: git cat-file: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// scan accumulates, one file at a time, what impact needs from a tree: each
// module's path, each package directory's imports, and the referrers of each
// changed path. It is pure, so it is tested without git.
type scan struct {
	changed   []string                   // changed paths, sorted, without _test.go files
	modules   map[string]string          // go.mod directory -> module path
	imports   map[string]map[string]bool // package directory -> import paths
	referrers map[string]map[string]bool // changed path -> files referring to it
	forms     map[string][]form          // referrer directory -> reference forms
}

// form is one way a file in some directory would write a changed path.
type form struct {
	target string
	text   []byte
}

func newScan(paths []string) *scan {
	set := map[string]bool{}
	for _, p := range paths {
		if !strings.HasSuffix(p, "_test.go") {
			set[p] = true
		}
	}
	return &scan{
		changed:   sorted(set),
		modules:   map[string]string{},
		imports:   map[string]map[string]bool{},
		referrers: map[string]map[string]bool{},
		forms:     map[string][]form{},
	}
}

// add takes one regular file of the tree, by repo-relative path.
func (s *scan) add(p string, content []byte) {
	dir := path.Dir(p)
	if path.Base(p) == "go.mod" {
		if m := modulePath(content); m != "" {
			s.modules[dir] = m
		}
	}
	if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") && buildable(dir) {
		set := s.imports[dir]
		if set == nil {
			set = map[string]bool{}
			s.imports[dir] = set
		}
		for _, imp := range goImports(p, content) {
			set[imp] = true
		}
	}
	if isText(content) {
		for _, target := range references(s.formsFrom(dir), p, content) {
			set := s.referrers[target]
			if set == nil {
				set = map[string]bool{}
				s.referrers[target] = set
			}
			set[p] = true
		}
	}
}

func (s *scan) formsFrom(dir string) []form {
	if fs, ok := s.forms[dir]; ok {
		return fs
	}
	var fs []form
	for _, x := range s.changed {
		for _, t := range referenceForms(dir, x) {
			fs = append(fs, form{target: x, text: []byte(t)})
		}
	}
	s.forms[dir] = fs
	return fs
}

// pkgOf returns the import path and directory of the Go package a changed
// path belongs to, or "" when it is not a buildable package file under a
// module.
func (s *scan) pkgOf(p string) (importPath, dir string) {
	if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
		return "", ""
	}
	dir = path.Dir(p)
	if !buildable(dir) {
		return "", ""
	}
	for d := dir; ; d = path.Dir(d) {
		if m, ok := s.modules[d]; ok {
			if d == dir {
				return m, dir
			}
			rel := dir
			if d != "." {
				rel = strings.TrimPrefix(dir, d+"/")
			}
			return m + "/" + rel, dir
		}
		if d == "." || d == "/" {
			return "", ""
		}
	}
}

// changedFiles returns the changed paths that are files rather than packages.
func (s *scan) changedFiles() []string {
	var files []string
	for _, p := range s.changed {
		if ip, _ := s.pkgOf(p); ip == "" {
			files = append(files, p)
		}
	}
	return files
}

// result resolves the accumulated tree into the impact, sorted by name.
func (s *scan) result() Impact {
	imp := emptyImpact()
	pkgs := map[string]string{} // import path -> directory
	for _, p := range s.changed {
		if ip, dir := s.pkgOf(p); ip != "" {
			pkgs[ip] = dir
		}
	}
	for ip, dir := range pkgs {
		n := 0
		for d, set := range s.imports {
			if d != dir && set[ip] {
				n++
			}
		}
		imp.Packages = append(imp.Packages, Dependency{Name: ip, Dependents: n})
	}
	for _, p := range s.changedFiles() {
		imp.Files = append(imp.Files, Dependency{Name: p, Dependents: len(s.referrers[p])})
	}
	sort.Slice(imp.Packages, func(i, j int) bool { return imp.Packages[i].Name < imp.Packages[j].Name })
	sort.Slice(imp.Files, func(i, j int) bool { return imp.Files[i].Name < imp.Files[j].Name })
	return imp
}

// buildable reports whether the go tool builds packages in directory dir: no
// segment is vendor or testdata, or starts with "." or "_".
func buildable(dir string) bool {
	if dir == "." {
		return true
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == "vendor" || seg == "testdata" || strings.HasPrefix(seg, ".") || strings.HasPrefix(seg, "_") {
			return false
		}
	}
	return true
}

// modulePath returns the module path a go.mod declares, or "" when it has no
// module directive.
func modulePath(gomod []byte) string {
	for _, line := range strings.Split(string(gomod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "module" {
			if m, err := strconv.Unquote(f[1]); err == nil {
				return m
			}
			return f[1]
		}
	}
	return ""
}

// goImports returns the import paths of a Go source file. Build constraints
// are ignored. A file that does not parse contributes whatever imports the
// parser recovered.
func goImports(name string, src []byte) []string {
	f, _ := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if f == nil {
		return nil
	}
	var out []string
	for _, spec := range f.Imports {
		if p, err := strconv.Unquote(spec.Path.Value); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// isText uses git's own heuristic: no NUL byte in the first 8000 bytes.
func isText(content []byte) bool {
	if len(content) > 8000 {
		content = content[:8000]
	}
	return bytes.IndexByte(content, 0) < 0
}

// referenceForms lists how a file in directory dir would write changed path
// x: its relative path ("./r", or "../r" when it climbs), the same without
// its extension, and its repository path when that has a directory.
func referenceForms(dir, x string) []string {
	r := relPath(dir, x)
	if !strings.HasPrefix(r, "../") {
		r = "./" + r
	}
	forms := []string{r}
	if ext := path.Ext(x); ext != "" && ext != path.Base(x) {
		forms = append(forms, strings.TrimSuffix(r, ext))
	}
	if strings.Contains(x, "/") {
		forms = append(forms, x)
	}
	return forms
}

// relPath returns target relative to directory dir; both are slash-separated
// and repo-relative, with the root as ".".
func relPath(dir, target string) string {
	var from []string
	if dir != "." {
		from = strings.Split(dir, "/")
	}
	to := strings.Split(target, "/")
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	return strings.Repeat("../", len(from)-i) + strings.Join(to[i:], "/")
}

// references returns the changed paths that content, the file at p, refers
// to. Backslashes count as slashes. A form matches only at a boundary: the
// byte before is not one of [A-Za-z0-9_.-] and the byte after is not one of
// [A-Za-z0-9_./-], so "./util" matches none of "../util", "./util.css" and
// "./util/x". A file never refers to itself.
func references(forms []form, p string, content []byte) []string {
	if len(forms) == 0 {
		return nil
	}
	text := bytes.ReplaceAll(content, []byte{'\\'}, []byte{'/'})
	seen := map[string]bool{}
	var out []string
	for _, f := range forms {
		if f.target == p || seen[f.target] {
			continue
		}
		if containsAtBoundary(text, f.text) {
			seen[f.target] = true
			out = append(out, f.target)
		}
	}
	return out
}

func containsAtBoundary(text, form []byte) bool {
	for off := 0; off < len(text); {
		i := bytes.Index(text[off:], form)
		if i < 0 {
			return false
		}
		start, end := off+i, off+i+len(form)
		before := start == 0 || !isNameByte(text[start-1], false)
		after := end == len(text) || !isNameByte(text[end], true)
		if before && after {
			return true
		}
		off = start + 1
	}
	return false
}

// isNameByte reports whether c can continue a path name; slash counts only
// after a match.
func isNameByte(c byte, slash bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_' || c == '.' || c == '-':
		return true
	case c == '/':
		return slash
	}
	return false
}
