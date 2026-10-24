package facts

import (
	"reflect"
	"testing"
)

func TestDegreeOf(t *testing.T) {
	tests := []struct {
		name  string
		files []File
		want  Degree
	}{
		{"none", nil, Degree{}},
		{"modified existing file", []File{{Path: "a.go", Added: 2, Removed: 1}},
			Degree{Existing: Lines{Added: 2, Removed: 1}}},
		{"new file", []File{{Path: "b.go", Added: 5, New: true}},
			Degree{New: Lines{Added: 5}}},
		{"deleted file", []File{{Path: "c.go", Removed: 7}},
			Degree{Existing: Lines{Removed: 7}}},
		{"renamed with edits", []File{{Path: "d/e.go", OldPath: "e.go", Added: 1, Removed: 2}},
			Degree{Existing: Lines{Added: 1, Removed: 2}}},
		{"binary files add nothing", []File{{Path: "x.png", Binary: true}, {Path: "y.bin", Binary: true, New: true}},
			Degree{}},
		{"mixed", []File{
			{Path: "a.go", Added: 2, Removed: 1},
			{Path: "b.go", Added: 5, New: true},
			{Path: "c.go", Removed: 7},
		}, Degree{Existing: Lines{Added: 2, Removed: 8}, New: Lines{Added: 5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DegreeOf(tt.files); got != tt.want {
				t.Errorf("DegreeOf(%+v) = %+v, want %+v", tt.files, got, tt.want)
			}
		})
	}
}

func TestMarkNew(t *testing.T) {
	files := []File{
		{Path: "kept.go", Added: 1},               // modified
		{Path: "made.go", Added: 3},               // absent at base
		{Path: "gone.go", Removed: 4},             // deleted: in the base tree
		{Path: "moved/to.go", OldPath: "from.go"}, // rename: old path existed
		{Path: "dir", Added: 1},                   // a directory at base, a file at head
	}
	atBase := map[string]bool{"kept.go": true, "gone.go": true, "from.go": true, "dir/inner.go": true}
	markNew(files, atBase)
	var got []bool
	for _, f := range files {
		got = append(got, f.New)
	}
	if want := []bool{false, true, false, false, true}; !reflect.DeepEqual(got, want) {
		t.Errorf("New flags = %v, want %v", got, want)
	}
}
