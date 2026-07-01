// server/codegen/backend/golang/emit_slice_literal_test.go — Tests
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// for encodeSliceLiteral, the helper that turns JSON-encoded slice
// values from the wizard's row-builder UI into Go slice literals at
// code-emission time.
//
// Coverage:
//
//   - []string with escape characters
//   - []int / []byte / []float64
//   - []bool
//   - empty slice (input "" or "[]") — emits zero literal not nil
//   - error cases: malformed JSON, fractional in []int
//   - order preservation — unlike the map encoder, no sorting
//
// goLiteralForMapValue is reused for per-element formatting; its
// contract is exercised end-to-end through these tests.

package golang

import (
	"strings"
	"testing"
)

func TestEncodeSliceLiteral_Strings(t *testing.T) {
	got, err := encodeSliceLiteral("[]string", `["a","b","c"]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `[]string{"a", "b", "c"}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeSliceLiteral_Ints(t *testing.T) {
	got, err := encodeSliceLiteral("[]int", `[1,2,3]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `[]int{1, 2, 3}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeSliceLiteral_Bytes(t *testing.T) {
	got, err := encodeSliceLiteral("[]byte", `[10,20,255]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `[]byte{10, 20, 255}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeSliceLiteral_Bools(t *testing.T) {
	got, err := encodeSliceLiteral("[]bool", `[true,false,true]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `[]bool{true, false, true}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeSliceLiteral_Floats(t *testing.T) {
	got, err := encodeSliceLiteral("[]float64", `[1.5,2.25,3.0]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// json.Number passes through as-is, so 3.0 stays 3.0; that
	// remains valid Go.
	want := `[]float64{1.5, 2.25, 3.0}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeSliceLiteral_Empty(t *testing.T) {
	got, err := encodeSliceLiteral("[]string", `[]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != `[]string{}` {
		t.Errorf("got %q want empty literal", got)
	}
	got2, err := encodeSliceLiteral("[]int", ``)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got2 != `[]int{}` {
		t.Errorf("got %q want empty literal", got2)
	}
}

func TestEncodeSliceLiteral_StringEscapes(t *testing.T) {
	got, err := encodeSliceLiteral("[]string", `["a\"b","c\\d"]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, `"a\"b"`) || !strings.Contains(got, `"c\\d"`) {
		t.Errorf("escapes not preserved: %q", got)
	}
}

func TestEncodeSliceLiteral_OrderPreserved(t *testing.T) {
	// Unlike the map encoder, slice elements MUST stay in the
	// order the user typed them. Reverse-sorted input must NOT
	// be re-sorted ascending.
	got, err := encodeSliceLiteral("[]int", `[5,3,1,4,2]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != `[]int{5, 3, 1, 4, 2}` {
		t.Errorf("order not preserved: %q", got)
	}
}

func TestEncodeSliceLiteral_NonIntegerInIntField(t *testing.T) {
	_, err := encodeSliceLiteral("[]int", `[1, 2.5, 3]`)
	if err == nil {
		t.Errorf("expected error for fractional value in []int")
	}
}

func TestEncodeSliceLiteral_MalformedJSON(t *testing.T) {
	_, err := encodeSliceLiteral("[]string", `[not json`)
	if err == nil {
		t.Errorf("expected error for malformed JSON")
	}
}

func TestEncodeSliceLiteral_NotASlice(t *testing.T) {
	// goType missing the "[]" prefix.
	_, err := encodeSliceLiteral("string", `["a"]`)
	if err == nil {
		t.Errorf("expected error for non-slice goType")
	}
}
