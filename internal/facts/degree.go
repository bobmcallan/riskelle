package facts

// Lines is a count of added and removed lines.
type Lines struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// Degree splits a change's lines between files that existed at the merge
// base and files the change creates. A deleted file's lines are removed
// existing lines. Binary files carry no line counts and add nothing.
type Degree struct {
	Existing Lines `json:"existing"`
	New      Lines `json:"new"`
}

// DegreeOf sums files' line counts into existing and new, by each file's New
// marker.
func DegreeOf(files []File) Degree {
	var d Degree
	for _, f := range files {
		side := &d.Existing
		if f.New {
			side = &d.New
		}
		side.Added += f.Added
		side.Removed += f.Removed
	}
	return d
}

// markNew sets New on every file absent from the merge-base tree, given as
// the set of paths it holds. A rename's old path existed at base, so a rename
// is never new; a deletion is in the base tree, so it is never new either.
func markNew(files []File, atBase map[string]bool) {
	for i := range files {
		files[i].New = files[i].OldPath == "" && !atBase[files[i].Path]
	}
}
