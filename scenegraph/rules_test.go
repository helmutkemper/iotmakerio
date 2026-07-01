// /ide/scenegraph/rules_test.go

package scenegraph

import (
	"testing"
)

// rules_test.go — exhaustive tests for the pure rule functions.
//
// English:
//
//	Layout of this file:
//	  1. Helper constructors — rect, simple, complex
//	  2. ContainsRect / IntersectsRect boundary tests
//	  3. classifyPair tests, organised by (Kind of A, Kind of B)
//	  4. findParent tests (single container, nested, excluded)
//	  5. findConflicts tests (symmetric detection, multi-peer)
//	  6. conflictsEqual tests
//
//	The naming convention is Test<Function>_<Scenario>. Each scenario
//	describes one specific spatial configuration, not a range.

// =====================================================================
//  Helper constructors
// =====================================================================

func rect(x, y, w, h float64) Rect {
	return Rect{X: x, Y: y, W: w, H: h}
}

// simple builds a non-container candidate (Simple).
func simple(id string, x, y, w, h float64) candidate {
	return candidate{
		ID:    id,
		Kind:  KindSimple,
		Outer: rect(x, y, w, h),
	}
}

// fitting builds a Fitting candidate — geometrically identical to simple.
func fitting(id string, x, y, w, h float64) candidate {
	return candidate{
		ID:    id,
		Kind:  KindFitting,
		Outer: rect(x, y, w, h),
	}
}

// complex4 builds a Complex candidate with a given outer and an inner
// inset by the given padding on all four sides.
func complex4(id string, x, y, w, h, pad float64) candidate {
	outer := rect(x, y, w, h)
	inner := rect(x+pad, y+pad, w-2*pad, h-2*pad)
	return candidate{
		ID:    id,
		Kind:  KindComplex,
		Outer: outer,
		Inner: &inner,
	}
}

// =====================================================================
//  ContainsRect / IntersectsRect boundary tests
// =====================================================================

func TestContainsRect_FullyInside(t *testing.T) {
	outer := rect(0, 0, 100, 100)
	inner := rect(10, 10, 50, 50)
	if !outer.ContainsRect(inner) {
		t.Fatal("expected inner fully inside outer")
	}
}

func TestContainsRect_EdgeTouchCountsAsInside(t *testing.T) {
	outer := rect(0, 0, 100, 100)
	// Touching top-left exactly.
	inner := rect(0, 0, 50, 50)
	if !outer.ContainsRect(inner) {
		t.Fatal("edge-touching child must count as contained")
	}
	// Touching right edge.
	inner = rect(50, 0, 50, 100)
	if !outer.ContainsRect(inner) {
		t.Fatal("right-edge touching child must count as contained")
	}
}

func TestContainsRect_PartiallyOutside(t *testing.T) {
	outer := rect(0, 0, 100, 100)
	// 1 pixel over the right edge.
	inner := rect(50, 0, 51, 50)
	if outer.ContainsRect(inner) {
		t.Fatal("child extending past right edge must not be contained")
	}
}

func TestIntersectsRect_Touching(t *testing.T) {
	a := rect(0, 0, 50, 50)
	b := rect(50, 0, 50, 50) // shares the right edge
	if a.IntersectsRect(b) {
		t.Fatal("edge-touching rectangles must not report intersection")
	}
}

func TestIntersectsRect_OnePixelOverlap(t *testing.T) {
	a := rect(0, 0, 50, 50)
	b := rect(49, 0, 50, 50)
	if !a.IntersectsRect(b) {
		t.Fatal("one-pixel overlap must report intersection")
	}
}

// =====================================================================
//  classifyPair — Simple × Simple
// =====================================================================

func TestClassify_SimpleSimple_Apart(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	b := simple("b", 200, 200, 50, 50)
	if _, bad := classifyPair(a, b); bad {
		t.Fatal("devices far apart must be legal")
	}
}

func TestClassify_SimpleSimple_Touching(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	b := simple("b", 50, 0, 50, 50) // shares right edge of a
	if _, bad := classifyPair(a, b); bad {
		t.Fatal("edge-touching devices must not conflict")
	}
}

func TestClassify_SimpleSimple_Overlap(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	b := simple("b", 25, 25, 50, 50)
	k, bad := classifyPair(a, b)
	if !bad || k != ConflictOverlap {
		t.Fatalf("expected ConflictOverlap, got (%v, %v)", k, bad)
	}
}

func TestClassify_SimpleSimple_Identical(t *testing.T) {
	a := simple("a", 10, 10, 50, 50)
	b := simple("b", 10, 10, 50, 50)
	k, bad := classifyPair(a, b)
	if !bad || k != ConflictOverlap {
		t.Fatalf("identical rects must be ConflictOverlap, got (%v, %v)", k, bad)
	}
}

// =====================================================================
//  classifyPair — Simple × Complex
// =====================================================================

func TestClassify_SimpleInsideComplexInner(t *testing.T) {
	// A is fully inside B's inner area → valid child, no conflict.
	a := simple("a", 50, 50, 30, 30)
	b := complex4("b", 20, 20, 200, 200, 20) // inner = (40,40) → (200,200)
	if _, bad := classifyPair(a, b); bad {
		t.Fatal("Simple fully inside Complex inner must be legal")
	}
}

func TestClassify_SimpleStraddlesComplexInner(t *testing.T) {
	// A crosses B's inner edge: outer intersects inner but is not
	// fully inside inner.
	b := complex4("b", 0, 0, 200, 200, 20) // inner = (20,20)→(200,200)
	a := simple("a", 10, 50, 40, 40)       // outer = (10,50)→(50,90)
	// a.Outer intersects b.Inner (x 20..50, y 50..90) → yes
	// a.Outer fully inside b.Inner? b.Inner.X = 20, a.Outer.X = 10 → no
	k, bad := classifyPair(a, b)
	if !bad || k != ConflictStraddle {
		t.Fatalf("expected ConflictStraddle, got (%v, %v)", k, bad)
	}
}

func TestClassify_SimpleInComplexFrame_Pierced(t *testing.T) {
	// A is fully inside B's outer but fully outside B's inner — lodged
	// in the "frame" between borders 1 and 3.
	b := complex4("b", 0, 0, 200, 200, 20)
	// Place A in the top-left frame strip: x in [0..20), y in [0..20).
	a := simple("a", 5, 5, 10, 10) // wholly in the frame
	// a.Outer ⊂ b.Outer, a.Outer ∩ b.Inner = ∅
	k, bad := classifyPair(a, b)
	if !bad || k != ConflictPiercedOuter {
		t.Fatalf("expected ConflictPiercedOuter, got (%v, %v)", k, bad)
	}
}

func TestClassify_SimpleFullyOutsideComplex(t *testing.T) {
	a := simple("a", 300, 300, 30, 30)
	b := complex4("b", 0, 0, 200, 200, 20)
	if _, bad := classifyPair(a, b); bad {
		t.Fatal("Simple fully outside Complex must be legal")
	}
}

func TestClassify_SimpleOverlapComplexOuterOnly(t *testing.T) {
	// A crosses B's border 1 but not B's border 3 — in other words, A is
	// half outside B and half in B's frame. That is PiercedOuter only if
	// the portion inside B's outer doesn't touch B.Inner. If it DOES touch
	// B.Inner, it's Straddle. Let's test both.
	b := complex4("b", 0, 0, 200, 200, 20)
	// A straddling border 1 of B on the left side, NOT reaching B.Inner:
	// A spans x -10..15, y 50..80. Inside B.Outer: x 0..15 (in frame).
	// B.Inner starts at x=20, so A does NOT touch inner.
	a := simple("a", -10, 50, 25, 30)
	k, bad := classifyPair(a, b)
	if !bad {
		t.Fatalf("expected conflict, got none (k=%v)", k)
	}
	if k != ConflictPiercedOuter {
		t.Fatalf("expected ConflictPiercedOuter, got %v", k)
	}
}

// =====================================================================
//  classifyPair — Complex × Complex
// =====================================================================

func TestClassify_ComplexInsideComplex(t *testing.T) {
	// Loop inside IfElse. The inner Complex is fully inside the outer's
	// inner rectangle.
	outer := complex4("outer", 0, 0, 400, 400, 30) // inner 30→400
	inner := complex4("inner", 50, 50, 100, 100, 10)
	if _, bad := classifyPair(inner, outer); bad {
		t.Fatal("Complex fully inside Complex inner must be legal")
	}
}

func TestClassify_ComplexLateralOverlap(t *testing.T) {
	// Two loops side by side, overlapping by 20 pixels. Neither contains
	// the other. Must be ConflictOverlap (they cannot be "half in" each
	// other, so it's not Straddle).
	a := complex4("a", 0, 0, 200, 200, 20)
	b := complex4("b", 180, 0, 200, 200, 20)
	k, bad := classifyPair(a, b)
	if !bad {
		t.Fatal("laterally overlapping Complexes must conflict")
	}
	// From A's perspective, B is not containing A → straddle or overlap.
	// B.Outer intersects A.Outer, B.Inner might or might not intersect A.
	// If B.Inner touches A, we get Straddle. Otherwise Overlap.
	// Check what it actually is: B.Inner = (200,20)→(380,200).
	// A.Outer = (0,0)→(200,200). Intersect: x=200 is edge → no.
	// So B.Inner does NOT intersect A.Outer → it's PiercedOuter from
	// A's point of view (A is partly in B's outer, not in B's inner).
	if k != ConflictPiercedOuter {
		t.Fatalf("expected ConflictPiercedOuter (A in B's frame), got %v", k)
	}
}

func TestClassify_ComplexStraddlingParentInner(t *testing.T) {
	// Outer Loop, inner Loop that pokes out through the outer's border 3.
	outer := complex4("outer", 0, 0, 200, 200, 20) // inner (20,20)→(200,200)
	inner := complex4("inner", 10, 50, 100, 100, 10)
	// inner.Outer = (10,50)→(110,150). outer.Inner starts at (20,20).
	// inner.Outer intersects outer.Inner (x 20..110, y 50..150) AND is
	// not contained (inner.Outer.X=10 < outer.Inner.X=20) → Straddle.
	k, bad := classifyPair(inner, outer)
	if !bad || k != ConflictStraddle {
		t.Fatalf("expected ConflictStraddle, got (%v, %v)", k, bad)
	}
}

// =====================================================================
//  findParent
// =====================================================================

func TestFindParent_NoContainers(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	b := simple("b", 100, 0, 50, 50)
	if got := findParent(a.Outer, "a", []candidate{a, b}); got != "" {
		t.Fatalf("expected no parent, got %q", got)
	}
}

func TestFindParent_SingleContainer(t *testing.T) {
	loop := complex4("loop", 0, 0, 400, 400, 20)
	child := simple("child", 50, 50, 30, 30)
	cands := []candidate{loop, child}
	if got := findParent(child.Outer, "child", cands); got != "loop" {
		t.Fatalf("expected parent loop, got %q", got)
	}
}

func TestFindParent_NestedReturnsInnermost(t *testing.T) {
	outer := complex4("outer", 0, 0, 400, 400, 20) // inner (20,20)→(400,400)
	middle := complex4("middle", 50, 50, 200, 200, 20)
	child := simple("child", 100, 100, 20, 20)
	cands := []candidate{outer, middle, child}
	if got := findParent(child.Outer, "child", cands); got != "middle" {
		t.Fatalf("expected parent middle (innermost), got %q", got)
	}
}

func TestFindParent_ExcludesSelf(t *testing.T) {
	// A Complex does not become its own parent just because its inner
	// contains its own outer (it doesn't, but let's guard it anyway).
	loop := complex4("loop", 0, 0, 400, 400, 20)
	cands := []candidate{loop}
	if got := findParent(loop.Outer, "loop", cands); got != "" {
		t.Fatalf("expected no parent (self excluded), got %q", got)
	}
}

func TestFindParent_OnlyContainsWhenFullyInside(t *testing.T) {
	loop := complex4("loop", 0, 0, 400, 400, 20)
	child := simple("child", 10, 10, 30, 30) // in the FRAME, not the inner
	cands := []candidate{loop, child}
	// child.Outer overlaps loop.Outer but NOT loop.Inner, so it should
	// not be considered a child.
	if got := findParent(child.Outer, "child", cands); got != "" {
		t.Fatalf("expected no parent (pierced, not contained), got %q", got)
	}
}

// =====================================================================
//  findConflicts
// =====================================================================

func TestFindConflicts_CleanScene(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	b := simple("b", 100, 0, 50, 50)
	if c := findConflicts("a", []candidate{a, b}); len(c) != 0 {
		t.Fatalf("expected no conflicts, got %v", c)
	}
}

func TestFindConflicts_SimpleOverlap(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	b := simple("b", 25, 25, 50, 50)
	cs := findConflicts("a", []candidate{a, b})
	if len(cs) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %v", len(cs), cs)
	}
	if cs[0].With != "b" || cs[0].Kind != ConflictOverlap {
		t.Fatalf("unexpected conflict: %+v", cs[0])
	}
}

func TestFindConflicts_StraddlingDetectedFromBothSides(t *testing.T) {
	// The Simple straddles the Loop's border 3. findConflicts should
	// report it from both the Simple's and the Loop's perspectives.
	loop := complex4("loop", 0, 0, 200, 200, 20) // inner (20,20)→(200,200)
	child := simple("child", 10, 50, 40, 40)     // straddles inner

	cs := findConflicts("child", []candidate{loop, child})
	if len(cs) != 1 || cs[0].With != "loop" || cs[0].Kind != ConflictStraddle {
		t.Fatalf("child should see straddle with loop, got %+v", cs)
	}

	cs = findConflicts("loop", []candidate{loop, child})
	if len(cs) != 1 || cs[0].With != "child" || cs[0].Kind != ConflictStraddle {
		t.Fatalf("loop should see straddle with child, got %+v", cs)
	}
}

func TestFindConflicts_MultiplePeers(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	b := simple("b", 25, 0, 50, 50)    // overlaps a
	c := simple("c", 40, 40, 50, 50)   // overlaps a
	d := simple("d", 500, 500, 50, 50) // far away, no conflict
	cs := findConflicts("a", []candidate{a, b, c, d})
	if len(cs) != 2 {
		t.Fatalf("expected 2 conflicts, got %d: %+v", len(cs), cs)
	}
	ids := map[string]bool{cs[0].With: true, cs[1].With: true}
	if !ids["b"] || !ids["c"] {
		t.Fatalf("expected conflicts with b and c, got %+v", cs)
	}
}

func TestFindConflicts_UnknownSubject(t *testing.T) {
	a := simple("a", 0, 0, 50, 50)
	if cs := findConflicts("ghost", []candidate{a}); cs != nil {
		t.Fatalf("expected nil for unknown subject, got %v", cs)
	}
}

// =====================================================================
//  Nested cases
// =====================================================================

func TestFindConflicts_NestedChain_Clean(t *testing.T) {
	// Loop → IfElse → Bool, all properly nested. No conflicts anywhere.
	loop := complex4("loop", 0, 0, 600, 600, 20)       // inner 20→600
	ifelse := complex4("ifelse", 50, 50, 400, 400, 20) // outer fits in loop.Inner; inner 70→450
	bool1 := simple("bool", 100, 100, 40, 40)          // fits in ifelse.Inner
	cands := []candidate{loop, ifelse, bool1}

	for _, id := range []string{"loop", "ifelse", "bool"} {
		cs := findConflicts(id, cands)
		if len(cs) != 0 {
			t.Fatalf("%s should be clean, got %+v", id, cs)
		}
	}
}

func TestFindConflicts_ChildStraddlesInnerContainer(t *testing.T) {
	// The innermost child straddles only the innermost container. The
	// outer container sees no conflict (the child is still inside the
	// outer's inner rect — the conflict is specifically with the middle).
	loop := complex4("loop", 0, 0, 600, 600, 20)
	ifelse := complex4("ifelse", 50, 50, 400, 400, 20) // inner 70→450
	bool1 := simple("bool", 60, 100, 30, 30)           // straddles ifelse.Inner
	cands := []candidate{loop, ifelse, bool1}

	// bool must conflict with ifelse.
	cs := findConflicts("bool", cands)
	foundIfElse := false
	foundLoop := false
	for _, c := range cs {
		if c.With == "ifelse" {
			foundIfElse = true
		}
		if c.With == "loop" {
			foundLoop = true
		}
	}
	if !foundIfElse {
		t.Fatal("bool must conflict with ifelse")
	}
	if foundLoop {
		t.Fatal("bool must NOT conflict with loop (still inside loop's inner)")
	}
}

// =====================================================================
//  conflictsEqual
// =====================================================================

func TestConflictsEqual_Empty(t *testing.T) {
	if !conflictsEqual(nil, nil) {
		t.Fatal("nil == nil")
	}
	if !conflictsEqual(nil, []Conflict{}) {
		t.Fatal("nil == empty slice")
	}
}

func TestConflictsEqual_SameOrder(t *testing.T) {
	a := []Conflict{{With: "x", Kind: ConflictOverlap}}
	b := []Conflict{{With: "x", Kind: ConflictOverlap}}
	if !conflictsEqual(a, b) {
		t.Fatal("identical lists must be equal")
	}
}

func TestConflictsEqual_DifferentOrder(t *testing.T) {
	a := []Conflict{
		{With: "x", Kind: ConflictOverlap},
		{With: "y", Kind: ConflictStraddle},
	}
	b := []Conflict{
		{With: "y", Kind: ConflictStraddle},
		{With: "x", Kind: ConflictOverlap},
	}
	if !conflictsEqual(a, b) {
		t.Fatal("order-independence broken")
	}
}

func TestConflictsEqual_Different(t *testing.T) {
	a := []Conflict{{With: "x", Kind: ConflictOverlap}}
	b := []Conflict{{With: "x", Kind: ConflictStraddle}}
	if conflictsEqual(a, b) {
		t.Fatal("different kinds must not match")
	}
	b = []Conflict{{With: "y", Kind: ConflictOverlap}}
	if conflictsEqual(a, b) {
		t.Fatal("different peer IDs must not match")
	}
}

// =====================================================================
//  Fitting × anything — same rules as Simple
// =====================================================================

func TestClassify_FittingSameAsSimple(t *testing.T) {
	// Fitting should behave identically to Simple in all spatial tests.
	// Overlap with another Fitting: expect ConflictOverlap.
	a := fitting("a", 0, 0, 50, 50)
	b := fitting("b", 25, 25, 50, 50)
	k, bad := classifyPair(a, b)
	if !bad || k != ConflictOverlap {
		t.Fatalf("expected ConflictOverlap, got (%v, %v)", k, bad)
	}

	// Fitting inside a Complex inner — no conflict.
	cbox := complex4("c", 0, 0, 200, 200, 20)
	f := fitting("f", 50, 50, 30, 30)
	if _, bad := classifyPair(f, cbox); bad {
		t.Fatal("Fitting inside Complex inner must be legal")
	}
}
