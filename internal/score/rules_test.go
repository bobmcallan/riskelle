package score

import (
	"reflect"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// TestDefaultFloors pins the built-in floors: one per consequence class,
// sorted by class, each at a known level.
func TestDefaultFloors(t *testing.T) {
	d := Default()
	if want := []Floor{{facts.ConsequenceAccessWidened, High}, {facts.ConsequenceIdentityRights, Medium}, {facts.ConsequencePipelineIdentity, Medium}, {facts.ConsequenceSecretAdded, High}}; !reflect.DeepEqual(d.ConsequenceFloors, want) {
		t.Errorf("floors = %+v, want %+v", d.ConsequenceFloors, want)
	}
	for i, fl := range d.ConsequenceFloors {
		if !facts.IsConsequence(fl.Class) {
			t.Errorf("floor for unknown class %q", fl.Class)
		}
		known := false
		for _, e := range d.Levels {
			known = known || e.Level == fl.Level
		}
		if !known {
			t.Errorf("floor %q level %q is not a level", fl.Class, fl.Level)
		}
		if i > 0 && d.ConsequenceFloors[i-1].Class >= fl.Class {
			t.Error("floors are not sorted by class")
		}
	}
}

// TestDefaultShapeCap pins the built-in shape cap: Medium, a level of the
// scale, below its last edge, which is High.
func TestDefaultShapeCap(t *testing.T) {
	r := Default()
	if r.ShapeCap != Medium {
		t.Errorf("ShapeCap = %q, want Medium", r.ShapeCap)
	}
	if rank(r.ShapeCap, r.Levels) < 0 {
		t.Errorf("ShapeCap %q is not a level in %v", r.ShapeCap, r.Levels)
	}
	last := r.Levels[len(r.Levels)-1].Level
	if last != High || last == r.ShapeCap {
		t.Errorf("last edge level %q, want High above the cap %q", last, r.ShapeCap)
	}
}
