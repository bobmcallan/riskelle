package facts

import (
	"bytes"
	"context"
	"path"
	"regexp"
)

// resourceHeader matches a resource block header at column 0, capturing its
// type and name labels; quotes are optional, as in blockHeader.
var resourceHeader = regexp.MustCompile(`^resource[ \t]+"?([A-Za-z][A-Za-z0-9_-]*)"?[ \t]+"?([A-Za-z_][A-Za-z0-9_-]*)"?`)

// goneHeader matches a moved or removed block header at column 0.
var goneHeader = regexp.MustCompile(`^(moved|removed)[ \t]*\{`)

// goneFrom matches a "from = type.name" line naming a whole resource, with
// nothing after it but an optional comment: an instance ("type.name[0]") or a
// module address names something else.
var goneFrom = regexp.MustCompile(`^[ \t]*from[ \t]*=[ \t]*([A-Za-z][A-Za-z0-9_-]*\.[A-Za-z_][A-Za-z0-9_-]*)[ \t\r]*(?:(?:#|//).*)?$`)

// removalConsequencesOf returns the classes a resource block of type typ
// carries when its address leaves its directory.
func removalConsequencesOf(typ string) []string {
	var out []string
	for _, c := range compiledConsequences {
		if c.removed.Match(typ) {
			out = append(out, c.class)
		}
	}
	return out
}

// tfAddresses returns the resource addresses (type.name) a .tf file declares,
// with their types, and the addresses it names in the from of a moved or
// removed block. Data blocks declare none.
func tfAddresses(content []byte) (declared map[string]string, gone map[string]bool) {
	declared, gone = map[string]string{}, map[string]bool{}
	text := bytes.Split(content, []byte("\n"))
	for i, l := range terraformBlocks(content) {
		if l.address != "" {
			declared[l.address] = l.block
		}
		if l.gone != "" && l.attr == "from" {
			if m := goneFrom.FindSubmatch(text[i]); m != nil {
				gone[string(m[1])] = true
			}
		}
	}
	return declared, gone
}

// removedBlocks records the removal classes of each catalogued resource block
// whose address type.name is declared in a .tf file of directory D at the
// merge base, and that no .tf file of D at head declares or names in the from
// of a moved or removed block, with the merge-base path of the file that
// declared it and its type. Only the directories of the merge-base paths of
// changed .tf files are read: an address can leave a directory only through
// one of them.
func removedBlocks(ctx context.Context, repo, mergeBase, head string, files []File, record func(class, p, typ string)) error {
	dirs := map[string]bool{}
	for _, f := range files {
		p := f.Path
		if f.OldPath != "" {
			p = f.OldPath
		}
		if !f.Binary && isTerraform(p) {
			dirs[path.Dir(p)] = true
		}
	}
	if len(dirs) == 0 {
		return nil
	}
	read := func(commit string, fn func(p string, declared map[string]string, gone map[string]bool)) error {
		all, err := treeBlobs(ctx, repo, commit)
		if err != nil {
			return err
		}
		var blobs []blob
		for _, b := range all {
			if isTerraform(b.path) && dirs[path.Dir(b.path)] {
				blobs = append(blobs, b)
			}
		}
		return catBlobs(ctx, repo, blobs, func(p string, content []byte) {
			declared, gone := tfAddresses(bytes.TrimSuffix(content, []byte("\n")))
			fn(p, declared, gone)
		})
	}
	kept := map[string]bool{} // dir NUL address, at head
	err := read(head, func(p string, declared map[string]string, gone map[string]bool) {
		for a := range declared {
			kept[path.Dir(p)+"\x00"+a] = true
		}
		for a := range gone {
			kept[path.Dir(p)+"\x00"+a] = true
		}
	})
	if err != nil {
		return err
	}
	return read(mergeBase, func(p string, declared map[string]string, _ map[string]bool) {
		for a, typ := range declared {
			if kept[path.Dir(p)+"\x00"+a] {
				continue
			}
			for _, class := range removalConsequencesOf(typ) {
				record(class, p, typ)
			}
		}
	})
}
