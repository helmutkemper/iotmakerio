// scenegraph/graph_hidden_test.go
package scenegraph

import "testing"

// TestSetHidden_ExcludesFromConflicts verifies that a hidden device takes part
// in no conflict: hiding one of two overlapping devices clears the peer's
// conflict (and the hidden one's own), the observer is re-notified so the
// warning mark clears, and showing it again restores the real conflict. This
// is the scenegraph half of the IfElse branch fix — a device in the inactive
// branch must not collide with a device in the active branch.
func TestSetHidden_ExcludesFromConflicts(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	a := newSimpleDevice("a", 0, 0, 50, 50)
	b := newSimpleDevice("b", 25, 25, 50, 50) // overlaps a
	g.Register(a)
	g.Register(b)

	// Sanity: overlapping → a conflicts with b.
	if got := g.FindConflicts("a"); len(got) == 0 {
		t.Fatalf("expected a to conflict with b before hiding, got %+v", got)
	}

	// Hide b → it leaves every spatial query.
	g.SetHidden("b", true)
	if got := g.FindConflicts("a"); len(got) != 0 {
		t.Errorf("a should be clean while b is hidden, got %+v", got)
	}
	if got := g.FindConflicts("b"); len(got) != 0 {
		t.Errorf("b should report no conflict while hidden, got %+v", got)
	}
	// The observer was re-notified, so a's warning mark clears.
	if ce := rec.lastConflictsFor("a"); ce == nil || len(ce.conflicts) != 0 {
		t.Errorf("expected a's conflicts re-notified as empty after b hidden, got %+v", ce)
	}

	// Show b again → the real conflict returns.
	g.SetHidden("b", false)
	if got := g.FindConflicts("a"); len(got) == 0 {
		t.Errorf("a should conflict with b again after b is shown, got %+v", got)
	}

	// Toggling an unknown id or a no-op state must not panic or change anything.
	g.SetHidden("does-not-exist", true)
	g.SetHidden("b", false) // already shown — no-op
	if got := g.FindConflicts("a"); len(got) == 0 {
		t.Errorf("a should still conflict with b after no-op toggles, got %+v", got)
	}
}
