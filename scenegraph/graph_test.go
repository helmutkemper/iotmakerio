// /ide/scenegraph/graph_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package scenegraph

import (
	"testing"
)

// graph_test.go — integration tests for the live Graph.
//
// English:
//
//	These tests drive the Graph through realistic lifecycles (register,
//	drag, resize, unregister) using a fake DeviceRef that mirrors
//	geometry mutations in-memory. They verify the observable contract:
//
//	  - conflicts fire the observer at the right moments
//	  - parent assignment follows the geometry
//	  - children move rigidly with their parent
//	  - resize respects floor and ceiling
//	  - removing a container re-parents its children
//
//	The fake device lets tests set the outer/inner rectangles directly
//	and observes MoveBy calls, so we do not depend on the sprite/WASM
//	layer at all. This is what makes the core engine CI-testable.

// =====================================================================
//  Fake DeviceRef for testing
// =====================================================================

// fakeDevice is a pure in-memory implementation of DeviceRef. Tests
// mutate its fields directly to simulate the user moving/resizing a
// device with the mouse; MoveBy is called by the Graph during drag-end
// and observes the translation.
type fakeDevice struct {
	id    string
	kind  Kind
	outer Rect
	inner *Rect

	// moveCount and lastDelta are populated by MoveBy to let tests
	// assert on rigid-translation behaviour.
	moveCount int
	lastDelta [2]float64
}

func newSimpleDevice(id string, x, y, w, h float64) *fakeDevice {
	return &fakeDevice{id: id, kind: KindSimple, outer: rect(x, y, w, h)}
}

func newComplexDevice(id string, x, y, w, h, pad float64) *fakeDevice {
	outer := rect(x, y, w, h)
	inner := rect(x+pad, y+pad, w-2*pad, h-2*pad)
	return &fakeDevice{id: id, kind: KindComplex, outer: outer, inner: &inner}
}

func (f *fakeDevice) ID() string       { return f.id }
func (f *fakeDevice) Kind() Kind       { return f.kind }
func (f *fakeDevice) OuterRect() Rect  { return f.outer }
func (f *fakeDevice) InnerRect() *Rect { return f.inner }
func (f *fakeDevice) MoveBy(dx, dy float64) {
	f.outer.X += dx
	f.outer.Y += dy
	if f.inner != nil {
		f.inner.X += dx
		f.inner.Y += dy
	}
	f.moveCount++
	f.lastDelta = [2]float64{dx, dy}
}

// setOuter simulates the sprite layer updating this device's position
// and size after the user drags the mouse. Also shifts the inner by
// the same amount so the padding remains constant (matches real device
// behaviour).
func (f *fakeDevice) setOuter(newOuter Rect) {
	if f.inner != nil {
		dx := newOuter.X - f.outer.X
		dy := newOuter.Y - f.outer.Y
		// Preserve the inner's relative offset and size.
		pad := f.outer.X - f.inner.X // negative if inner is inside
		_ = pad
		// Simpler: recompute inner keeping the same four-side padding.
		leftPad := f.inner.X - f.outer.X
		topPad := f.inner.Y - f.outer.Y
		rightPad := (f.outer.X + f.outer.W) - (f.inner.X + f.inner.W)
		botPad := (f.outer.Y + f.outer.H) - (f.inner.Y + f.inner.H)
		f.inner.X = newOuter.X + leftPad
		f.inner.Y = newOuter.Y + topPad
		f.inner.W = newOuter.W - leftPad - rightPad
		f.inner.H = newOuter.H - topPad - botPad
		_ = dx
		_ = dy
	}
	f.outer = newOuter
}

// =====================================================================
//  Recording observer
// =====================================================================

type recordedConflicts struct {
	deviceID  string
	conflicts []Conflict
}
type recordedParent struct {
	deviceID, oldParent, newParent string
}

type recorder struct {
	conflictEvents []recordedConflicts
	parentEvents   []recordedParent
}

func (r *recorder) OnConflictsChanged(id string, cs []Conflict) {
	r.conflictEvents = append(r.conflictEvents, recordedConflicts{id, cs})
}
func (r *recorder) OnParentChanged(id, old, new string) {
	r.parentEvents = append(r.parentEvents, recordedParent{id, old, new})
}

// lastParentFor returns the most recent OnParentChanged event for the
// given device, or nil if none.
func (r *recorder) lastParentFor(deviceID string) *recordedParent {
	for i := len(r.parentEvents) - 1; i >= 0; i-- {
		if r.parentEvents[i].deviceID == deviceID {
			return &r.parentEvents[i]
		}
	}
	return nil
}

// lastConflictsFor returns the most recent OnConflictsChanged event for
// the given device, or nil if none.
func (r *recorder) lastConflictsFor(deviceID string) *recordedConflicts {
	for i := len(r.conflictEvents) - 1; i >= 0; i-- {
		if r.conflictEvents[i].deviceID == deviceID {
			return &r.conflictEvents[i]
		}
	}
	return nil
}

// =====================================================================
//  Register / Unregister
// =====================================================================

func TestRegister_AssignsInitialParent(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	loop := newComplexDevice("loop", 0, 0, 400, 400, 20)
	child := newSimpleDevice("child", 50, 50, 30, 30)

	g.Register(loop)
	g.Register(child)

	if got := g.ParentOf("child"); got != "loop" {
		t.Fatalf("expected parent loop, got %q", got)
	}
	if kids := g.ChildrenOf("loop"); len(kids) != 1 || kids[0] != "child" {
		t.Fatalf("expected loop's children = [child], got %v", kids)
	}

	pe := rec.lastParentFor("child")
	if pe == nil || pe.newParent != "loop" || pe.oldParent != "" {
		t.Fatalf("expected parent event child: ''→'loop', got %+v", pe)
	}
}

func TestUnregister_ReparentsChildrenToGrandparent(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	outer := newComplexDevice("outer", 0, 0, 600, 600, 20)
	middle := newComplexDevice("middle", 50, 50, 300, 300, 20)
	child := newSimpleDevice("child", 100, 100, 30, 30)

	g.Register(outer)
	g.Register(middle)
	g.Register(child)

	if got := g.ParentOf("child"); got != "middle" {
		t.Fatalf("pre-condition: expected middle, got %q", got)
	}

	// Remove middle — child should become a child of outer.
	g.Unregister("middle")

	if got := g.ParentOf("child"); got != "outer" {
		t.Fatalf("expected re-parent to outer, got %q", got)
	}
	pe := rec.lastParentFor("child")
	if pe == nil || pe.oldParent != "middle" || pe.newParent != "outer" {
		t.Fatalf("expected parent event child: 'middle'→'outer', got %+v", pe)
	}
}

func TestUnregister_RootifiesChildrenWhenNoGrandparent(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	loop := newComplexDevice("loop", 0, 0, 400, 400, 20)
	child := newSimpleDevice("child", 50, 50, 30, 30)

	g.Register(loop)
	g.Register(child)
	g.Unregister("loop")

	if got := g.ParentOf("child"); got != "" {
		t.Fatalf("expected child at root, got parent %q", got)
	}
	pe := rec.lastParentFor("child")
	if pe == nil || pe.oldParent != "loop" || pe.newParent != "" {
		t.Fatalf("expected parent event child: 'loop'→'', got %+v", pe)
	}
}

func TestUnregister_ClearsConflictsOnPeers(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	a := newSimpleDevice("a", 0, 0, 50, 50)
	b := newSimpleDevice("b", 25, 25, 50, 50) // overlaps a

	g.Register(a)
	g.Register(b)

	// On registering b, both a and b should have been notified of a
	// conflict against the other.
	if ce := rec.lastConflictsFor("a"); ce == nil || len(ce.conflicts) == 0 {
		t.Fatalf("expected a to have conflict with b, got %+v", ce)
	}

	// Remove b — a's conflict should clear.
	g.Unregister("b")
	ce := rec.lastConflictsFor("a")
	if ce == nil || len(ce.conflicts) != 0 {
		t.Fatalf("expected a to be clean after b removed, got %+v", ce)
	}
}

// =====================================================================
//  Drag lifecycle
// =====================================================================

func TestDrag_SimpleIntoComplex_FiresParentChange(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	loop := newComplexDevice("loop", 0, 0, 400, 400, 20)
	child := newSimpleDevice("child", 600, 600, 30, 30) // far from loop

	g.Register(loop)
	g.Register(child)

	if got := g.ParentOf("child"); got != "" {
		t.Fatalf("pre-condition: child should be root, got %q", got)
	}

	// Simulate the user dragging child onto the loop.
	g.BeginDrag("child")
	child.setOuter(rect(50, 50, 30, 30)) // now inside loop.Inner
	g.UpdateDrag("child")
	g.EndDrag("child", -550, -550)

	if got := g.ParentOf("child"); got != "loop" {
		t.Fatalf("expected new parent loop, got %q", got)
	}
	pe := rec.lastParentFor("child")
	if pe == nil || pe.newParent != "loop" {
		t.Fatalf("expected parent event to 'loop', got %+v", pe)
	}
}

func TestDrag_OverSimple_ConflictRaisedThenClearedOnMoveAway(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	a := newSimpleDevice("a", 0, 0, 50, 50)
	b := newSimpleDevice("b", 500, 500, 50, 50)

	g.Register(a)
	g.Register(b)

	// Drag a onto b.
	g.BeginDrag("a")
	a.setOuter(rect(490, 490, 50, 50)) // overlaps b
	g.UpdateDrag("a")

	ce := rec.lastConflictsFor("a")
	if ce == nil || len(ce.conflicts) != 1 || ce.conflicts[0].With != "b" {
		t.Fatalf("expected conflict with b, got %+v", ce)
	}

	// Move away again.
	a.setOuter(rect(0, 0, 50, 50))
	g.UpdateDrag("a")

	ce = rec.lastConflictsFor("a")
	if ce == nil || len(ce.conflicts) != 0 {
		t.Fatalf("expected no conflicts after moving away, got %+v", ce)
	}

	// EndDrag with no conflicts: no parent change (both still root).
	g.EndDrag("a", 0, 0)
	if got := g.ParentOf("a"); got != "" {
		t.Fatalf("expected root, got parent %q", got)
	}
}

func TestDrag_ContainerMovesAllDescendants(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	outer := newComplexDevice("outer", 0, 0, 600, 600, 20)
	middle := newComplexDevice("middle", 50, 50, 300, 300, 20)
	leaf := newSimpleDevice("leaf", 100, 100, 30, 30)

	g.Register(outer)
	g.Register(middle)
	g.Register(leaf)

	// Drag outer by (100, 50). middle and leaf must travel with it.
	g.BeginDrag("outer")
	outer.setOuter(rect(100, 50, 600, 600))
	g.UpdateDrag("outer")
	g.EndDrag("outer", 100, 50)

	if middle.moveCount != 1 {
		t.Fatalf("middle should have been moved once, got %d", middle.moveCount)
	}
	if middle.lastDelta != [2]float64{100, 50} {
		t.Fatalf("middle delta wrong: %v", middle.lastDelta)
	}
	if leaf.moveCount != 1 {
		t.Fatalf("leaf should have been moved once, got %d", leaf.moveCount)
	}
	if leaf.lastDelta != [2]float64{100, 50} {
		t.Fatalf("leaf delta wrong: %v", leaf.lastDelta)
	}

	// Parent-child structure must be preserved.
	if g.ParentOf("middle") != "outer" {
		t.Fatal("middle should still be child of outer")
	}
	if g.ParentOf("leaf") != "middle" {
		t.Fatal("leaf should still be child of middle")
	}
}

func TestDrag_ConflictOnEndPreservesOldParent(t *testing.T) {
	g := NewGraph()
	rec := &recorder{}
	g.SetObserver(rec)

	loop := newComplexDevice("loop", 0, 0, 400, 400, 20)
	obstacle := newSimpleDevice("obstacle", 600, 50, 30, 30)
	child := newSimpleDevice("child", 50, 50, 30, 30) // starts inside loop

	g.Register(loop)
	g.Register(obstacle)
	g.Register(child)

	if g.ParentOf("child") != "loop" {
		t.Fatal("pre-condition: child should be in loop")
	}

	// Drag child onto obstacle — ends with a conflict.
	g.BeginDrag("child")
	child.setOuter(rect(610, 60, 30, 30)) // overlaps obstacle
	g.UpdateDrag("child")
	g.EndDrag("child", 560, 10)

	// Despite ending outside the loop's inner, the conflict prevents
	// parent reassignment. Parent stays "loop".
	if got := g.ParentOf("child"); got != "loop" {
		t.Fatalf("conflict must preserve old parent, got %q", got)
	}
}

// =====================================================================
//  Resize lifecycle
// =====================================================================

func TestResize_FloorBlocksShrinkPastChild(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	loop := newComplexDevice("loop", 0, 0, 400, 400, 20)
	child := newSimpleDevice("child", 50, 50, 100, 100)

	g.Register(loop)
	g.Register(child)

	g.BeginResize("loop")
	// Check that resizeFloor was cached around the child.
	node := g.nodes["loop"]
	if node.resizeFloor == nil {
		t.Fatal("expected resizeFloor cached")
	}
	if *node.resizeFloor != (Rect{50, 50, 100, 100}) {
		t.Fatalf("expected floor = child.Outer, got %+v", *node.resizeFloor)
	}
	g.EndResize("loop")
	if node.resizeFloor != nil {
		t.Fatal("expected resizeFloor cleared after EndResize")
	}
}

func TestResize_CeilingCachedWhenNested(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	outer := newComplexDevice("outer", 0, 0, 600, 600, 20) // inner 20→600
	inner := newComplexDevice("inner", 50, 50, 300, 300, 20)

	g.Register(outer)
	g.Register(inner)

	if g.ParentOf("inner") != "outer" {
		t.Fatal("pre-condition: inner should be child of outer")
	}

	g.BeginResize("inner")
	node := g.nodes["inner"]
	if node.resizeCeiling == nil {
		t.Fatal("expected resizeCeiling cached")
	}
	expected := Rect{20, 20, 560, 560}
	if *node.resizeCeiling != expected {
		t.Fatalf("expected ceiling = outer.Inner, got %+v", *node.resizeCeiling)
	}
	g.EndResize("inner")
}

// =====================================================================
//  Query methods
// =====================================================================

func TestDescendants_DepthFirst(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	outer := newComplexDevice("outer", 0, 0, 600, 600, 20)
	middle := newComplexDevice("middle", 50, 50, 300, 300, 20)
	leaf1 := newSimpleDevice("leaf1", 100, 100, 30, 30)
	leaf2 := newSimpleDevice("leaf2", 150, 150, 30, 30)

	g.Register(outer)
	g.Register(middle)
	g.Register(leaf1)
	g.Register(leaf2)

	ds := g.Descendants("outer")
	// Expect [middle, leaf1, leaf2] in depth-first order.
	if len(ds) != 3 {
		t.Fatalf("expected 3 descendants, got %d: %v", len(ds), ds)
	}
	if ds[0] != "middle" {
		t.Fatalf("expected middle first, got %v", ds)
	}
	// leaf1 and leaf2 both under middle; order matches registration.
	set := map[string]bool{ds[1]: true, ds[2]: true}
	if !set["leaf1"] || !set["leaf2"] {
		t.Fatalf("expected leaf1 and leaf2 as siblings, got %v", ds)
	}
}

func TestSnapshot_IncludesConflicts(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	a := newSimpleDevice("a", 0, 0, 50, 50)
	b := newSimpleDevice("b", 25, 25, 50, 50)

	g.Register(a)
	g.Register(b)

	snap := g.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 views, got %d", len(snap))
	}
	var viewA NodeView
	for _, v := range snap {
		if v.ID == "a" {
			viewA = v
		}
	}
	if len(viewA.Conflicts) != 1 || viewA.Conflicts[0].With != "b" {
		t.Fatalf("expected a's snapshot to show conflict with b, got %+v", viewA)
	}
}

func TestChildrenBounds_UnionOfChildren(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	loop := newComplexDevice("loop", 0, 0, 400, 400, 20)
	c1 := newSimpleDevice("c1", 50, 50, 30, 30)   // outer 50,50→80,80
	c2 := newSimpleDevice("c2", 200, 150, 40, 40) // outer 200,150→240,190

	g.Register(loop)
	g.Register(c1)
	g.Register(c2)

	b := g.ChildrenBounds("loop")
	if b == nil {
		t.Fatal("expected non-nil bounds")
	}
	// Expected union: x 50..240, y 50..190 → W=190, H=140
	expected := Rect{X: 50, Y: 50, W: 190, H: 140}
	if *b != expected {
		t.Fatalf("expected bounds %+v, got %+v", expected, *b)
	}
}

func TestChildrenBounds_NilWhenNoChildren(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})
	loop := newComplexDevice("loop", 0, 0, 400, 400, 20)
	g.Register(loop)

	if b := g.ChildrenBounds("loop"); b != nil {
		t.Fatalf("expected nil, got %+v", b)
	}
}

func TestParentInnerRect_ReturnsParentInner(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	outer := newComplexDevice("outer", 0, 0, 600, 600, 20)
	child := newSimpleDevice("child", 50, 50, 30, 30)

	g.Register(outer)
	g.Register(child)

	ceiling := g.ParentInnerRect("child")
	if ceiling == nil {
		t.Fatal("expected non-nil ceiling")
	}
	expected := Rect{20, 20, 560, 560}
	if *ceiling != expected {
		t.Fatalf("expected %+v, got %+v", expected, *ceiling)
	}
}

func TestParentInnerRect_NilForRoot(t *testing.T) {
	g := NewGraph()
	g.SetObserver(NoopObserver{})

	leaf := newSimpleDevice("leaf", 0, 0, 50, 50)
	g.Register(leaf)

	if c := g.ParentInnerRect("leaf"); c != nil {
		t.Fatalf("expected nil for root device, got %+v", c)
	}
}
