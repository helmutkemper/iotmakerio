package types

// compat_test.go — Unit tests for every documented case in the
// compatibility table. Organised by the section headings in the
// package doc so reviewers can cross-reference rule ↔ test.
//
// Each subtest names the (A, B) pair explicitly. Assertions cover:
//
//   - Action is the expected outcome kind.
//   - Result carries the promoted type (empty when impossible).
//   - CastA / CastB are set only on the side that needs conversion.
//
// Symmetric behaviour (Classify(A,B) == Classify(B,A) semantically)
// is checked separately so a rule that accidentally breaks symmetry
// fails loudly.

import "testing"

func TestClassify_IdenticalTypes(t *testing.T) {
	cases := []string{"int", "float", "uint16", "int64", "bool", "string", "*machine.I2C"}
	for _, ty := range cases {
		got := Classify(ty, ty)
		if got.Action != CastNone {
			t.Errorf("%s × %s: Action=%s, want none", ty, ty, got.Action)
		}
		if got.Result != ty {
			t.Errorf("%s × %s: Result=%q, want %q", ty, ty, got.Result, ty)
		}
		if got.CastA != "" || got.CastB != "" {
			t.Errorf("%s × %s: unexpected cast (%q, %q)", ty, ty, got.CastA, got.CastB)
		}
	}
}

func TestClassify_SliceCollections(t *testing.T) {
	// Pins the v1 collection rule. A slice type like "[]int" is opaque
	// to the numeric machinery, so it combines only with an identical
	// slice (CastNone, no cast on either side) — exactly what the
	// StatementConstArray{Int,Float,String} feature needs when wiring a []int const into a
	// []int parameter. Any non-identical pair (different element type,
	// or slice × scalar) is impossible: v1 does no element widening
	// across collections.

	// Compatible: identical slice types resolve with no cast.
	same := []string{"[]int", "[]float32", "[]float64", "[]bool", "[]string", "[]uint8"}
	for _, ty := range same {
		got := Classify(ty, ty)
		if got.Action != CastNone {
			t.Errorf("%s × %s: Action=%s, want none", ty, ty, got.Action)
		}
		if got.Result != ty {
			t.Errorf("%s × %s: Result=%q, want %q", ty, ty, got.Result, ty)
		}
		if got.CastA != "" || got.CastB != "" {
			t.Errorf("%s × %s: unexpected cast (%q, %q)", ty, ty, got.CastA, got.CastB)
		}
	}

	// Impossible: slice × scalar, and slice × differently-typed slice.
	// Both orders are checked so a symmetry break fails loudly.
	impossible := [][2]string{
		{"[]int", "int"},
		{"int", "[]int"},
		{"[]int", "[]float32"},
		{"[]float32", "[]int"},
		{"[]int", "[]int64"},
		{"[]int", "float"},
	}
	for _, c := range impossible {
		got := Classify(c[0], c[1])
		if got.Action != CastImpossible {
			t.Errorf("%s × %s: Action=%s, want impossible", c[0], c[1], got.Action)
		}
		if got.Result != "" {
			t.Errorf("%s × %s: Result=%q, want empty", c[0], c[1], got.Result)
		}
	}
}

func TestClassify_AbstractVsAbstract(t *testing.T) {
	// Different abstract kinds are impossible — maker must commit.
	got := Classify("int", "float")
	if got.Action != CastImpossible {
		t.Errorf("int × float: Action=%s, want impossible", got.Action)
	}
}

func TestClassify_AbstractVsConcrete(t *testing.T) {
	// Concrete always wins. Abstract side gets a warning cast.
	cases := []struct {
		a, b   string
		result string
		castA  string
		castB  string
	}{
		{"int", "int32", "int32", "int32", ""},
		{"int", "uint16", "uint16", "uint16", ""},
		{"int", "int64", "int64", "int64", ""},
		{"int32", "int", "int32", "", "int32"},
		{"uint16", "int", "uint16", "", "uint16"},
		{"float", "float32", "float32", "float32", ""},
		{"float64", "float", "float64", "", "float64"},
	}
	for _, c := range cases {
		got := Classify(c.a, c.b)
		if got.Action != CastWarn {
			t.Errorf("%s × %s: Action=%s, want warn", c.a, c.b, got.Action)
		}
		if got.Result != c.result {
			t.Errorf("%s × %s: Result=%q, want %q", c.a, c.b, got.Result, c.result)
		}
		if got.CastA != c.castA {
			t.Errorf("%s × %s: CastA=%q, want %q", c.a, c.b, got.CastA, c.castA)
		}
		if got.CastB != c.castB {
			t.Errorf("%s × %s: CastB=%q, want %q", c.a, c.b, got.CastB, c.castB)
		}
	}
}

func TestClassify_ConcreteInt_SameSignedness(t *testing.T) {
	// Widen to larger; smaller side casts; silent.
	cases := []struct {
		a, b, result string
		castSide     rune // 'A', 'B', or '0' when identical
	}{
		{"uint8", "uint16", "uint16", 'A'},
		{"uint16", "uint8", "uint16", 'B'},
		{"int8", "int32", "int32", 'A'},
		{"int64", "int8", "int64", 'B'},
	}
	for _, c := range cases {
		got := Classify(c.a, c.b)
		if got.Action != CastSilent {
			t.Errorf("%s × %s: Action=%s, want silent", c.a, c.b, got.Action)
		}
		if got.Result != c.result {
			t.Errorf("%s × %s: Result=%q, want %q", c.a, c.b, got.Result, c.result)
		}
		if c.castSide == 'A' && got.CastA != c.result {
			t.Errorf("%s × %s: expected cast on A side", c.a, c.b)
		}
		if c.castSide == 'B' && got.CastB != c.result {
			t.Errorf("%s × %s: expected cast on B side", c.a, c.b)
		}
	}
}

func TestClassify_ConcreteInt_MixedSignedness(t *testing.T) {
	// Promote to signed one size larger than the larger operand.
	// Both sides end up cast to the promoted type; silent.
	cases := []struct {
		a, b, result string
	}{
		{"uint8", "int8", "int16"},
		{"int8", "uint8", "int16"},
		{"uint16", "int16", "int32"},
		{"uint32", "int32", "int64"},
		{"int16", "uint8", "int32"}, // max(16,8)*2 = 32
	}
	for _, c := range cases {
		got := Classify(c.a, c.b)
		if got.Action != CastSilent {
			t.Errorf("%s × %s: Action=%s, want silent", c.a, c.b, got.Action)
		}
		if got.Result != c.result {
			t.Errorf("%s × %s: Result=%q, want %q", c.a, c.b, got.Result, c.result)
		}
		if got.CastA != c.result || got.CastB != c.result {
			t.Errorf("%s × %s: expected both sides cast to %q, got (%q, %q)",
				c.a, c.b, c.result, got.CastA, got.CastB)
		}
	}
}

func TestClassify_Uint64_MixedSignedImpossible(t *testing.T) {
	// No signed type holds the full uint64 range.
	for _, other := range []string{"int8", "int16", "int32", "int64"} {
		got := Classify("uint64", other)
		if got.Action != CastImpossible {
			t.Errorf("uint64 × %s: Action=%s, want impossible", other, got.Action)
		}
		got = Classify(other, "uint64")
		if got.Action != CastImpossible {
			t.Errorf("%s × uint64: Action=%s, want impossible", other, got.Action)
		}
	}
}

func TestClassify_IntFloatMix(t *testing.T) {
	// Integers fitting in the mantissa → silent; otherwise warn.
	cases := []struct {
		a, b   string
		action CastAction
		result string
	}{
		{"int8", "float32", CastSilent, "float32"},
		{"uint16", "float32", CastSilent, "float32"}, // 16 bits fit in float32's 24-bit mantissa
		{"int32", "float32", CastWarn, "float32"},    // 32 > 24
		{"int64", "float32", CastWarn, "float32"},    // 64 > 24
		{"int8", "float64", CastSilent, "float64"},
		{"int32", "float64", CastSilent, "float64"}, // 32 fits in 53
		{"int64", "float64", CastWarn, "float64"},   // 64 > 53
		{"float32", "int32", CastWarn, "float32"},   // symmetry
	}
	for _, c := range cases {
		got := Classify(c.a, c.b)
		if got.Action != c.action {
			t.Errorf("%s × %s: Action=%s, want %s", c.a, c.b, got.Action, c.action)
		}
		if got.Result != c.result {
			t.Errorf("%s × %s: Result=%q, want %q", c.a, c.b, got.Result, c.result)
		}
	}
}

func TestClassify_FloatFloat(t *testing.T) {
	got := Classify("float32", "float64")
	if got.Action != CastSilent || got.Result != "float64" {
		t.Errorf("float32 × float64: got %+v, want silent/float64", got)
	}
	if got.CastA != "float64" {
		t.Errorf("float32 × float64: expected CastA=float64, got %q", got.CastA)
	}
}

func TestClassify_BoolStringOpaque_OnlySelf(t *testing.T) {
	// Non-numeric kinds compare only to themselves; everything else
	// is impossible.
	for _, kind := range []string{"bool", "string", "*machine.I2C", "SomeBlackBoxStruct"} {
		for _, other := range []string{"int", "int32", "float64", "bool", "string"} {
			if kind == other {
				continue // identical case already covered
			}
			got := Classify(kind, other)
			if got.Action != CastImpossible {
				t.Errorf("%s × %s: Action=%s, want impossible", kind, other, got.Action)
			}
		}
	}
}

func TestClassify_AliasesByteRune(t *testing.T) {
	// byte is uint8, rune is int32 — they participate in the same
	// rules as their aliases.
	got := Classify("byte", "uint16")
	if got.Action != CastSilent || got.Result != "uint16" {
		t.Errorf("byte × uint16: got %+v, want silent/uint16", got)
	}
	got = Classify("rune", "int64")
	if got.Action != CastSilent || got.Result != "int64" {
		t.Errorf("rune × int64: got %+v, want silent/int64", got)
	}
}

func TestClassify_Symmetry(t *testing.T) {
	// Classify(A,B) must produce a result equivalent to Classify(B,A)
	// after swapping CastA/CastB. Checked on a representative sample
	// covering each rule family.
	pairs := [][2]string{
		{"int", "uint16"},
		{"uint8", "int32"},
		{"int64", "float32"},
		{"uint64", "int32"},
		{"bool", "string"},
		{"float", "float32"},
	}
	for _, p := range pairs {
		ab := Classify(p[0], p[1])
		ba := Classify(p[1], p[0])
		if ab.Action != ba.Action || ab.Result != ba.Result {
			t.Errorf("asymmetry (%s,%s)=%+v vs (%s,%s)=%+v", p[0], p[1], ab, p[1], p[0], ba)
			continue
		}
		if ab.CastA != ba.CastB || ab.CastB != ba.CastA {
			t.Errorf("cast sides not mirrored for (%s,%s): ab=%+v ba=%+v", p[0], p[1], ab, ba)
		}
	}
}

// The APDS9960 scenario from the 2026-04-18 session: uint16 × int.
// This is the case that exposed the type mismatch bug in the first
// place — the generated Go had `apds99600_clear > constInt0` with
// mismatched types, and we want to be sure the new rules classify
// it as warn and promote to uint16.
func TestClassify_APDS9960Regression(t *testing.T) {
	got := Classify("uint16", "int")
	if got.Action != CastWarn {
		t.Errorf("uint16 × int: Action=%s, want warn", got.Action)
	}
	if got.Result != "uint16" {
		t.Errorf("uint16 × int: Result=%q, want uint16", got.Result)
	}
	if got.CastB != "uint16" {
		t.Errorf("uint16 × int: CastB=%q, want uint16 (abstract side)", got.CastB)
	}
	if got.CastA != "" {
		t.Errorf("uint16 × int: CastA should be empty, got %q", got.CastA)
	}
}
