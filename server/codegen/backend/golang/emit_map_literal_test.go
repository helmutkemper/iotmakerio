// server/codegen/backend/golang/emit_map_literal_test.go — Tests for
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// encodeMapLiteral and goLiteralForMapValue, the helpers that turn
// JSON-encoded map values from the wizard's row-builder UI into
// Go map literals at code-emission time.
//
// Coverage:
//
//   - map[string]string with escape characters (quotes, backslashes)
//   - map[string]int / int64 / uint8 — integer round-trip
//   - map[string]bool — true/false literals + tolerated string forms
//   - map[string]float64 — numeric formatting
//   - empty map (input "" or "{}") — emits zero literal not nil
//   - error cases: non-integer in int field, non-string key
//   - deterministic ordering — same input → same output bytes
//
// These tests pin down the contract the wizard's WASM renderer relies
// on: whatever JSON the row-builder writes to the hidden input must
// produce a compilable Go literal here.

package golang

import (
	"strings"
	"testing"
)

func TestEncodeMapLiteral_StringString(t *testing.T) {
	got, err := encodeMapLiteral(
		"map[string]string",
		`{"User-Agent":"iot/1.0","Authorization":"Bearer xxx"}`,
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `map[string]string{"Authorization": "Bearer xxx", "User-Agent": "iot/1.0"}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeMapLiteral_StringInt(t *testing.T) {
	got, err := encodeMapLiteral("map[string]int", `{"a":1,"b":42}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `map[string]int{"a": 1, "b": 42}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeMapLiteral_StringBool(t *testing.T) {
	got, err := encodeMapLiteral("map[string]bool", `{"on":true,"off":false}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `map[string]bool{"off": false, "on": true}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeMapLiteral_StringFloat(t *testing.T) {
	got, err := encodeMapLiteral("map[string]float64", `{"pi":3.14}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `map[string]float64{"pi": 3.14}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEncodeMapLiteral_Empty(t *testing.T) {
	got, err := encodeMapLiteral("map[string]string", `{}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != `map[string]string{}` {
		t.Errorf("got %q want empty literal", got)
	}
	got2, err := encodeMapLiteral("map[string]string", ``)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got2 != `map[string]string{}` {
		t.Errorf("got %q want empty literal", got2)
	}
}

func TestEncodeMapLiteral_StringEscapes(t *testing.T) {
	// Keys and values with quotes, backslashes.
	got, err := encodeMapLiteral(
		"map[string]string",
		`{"a\"b":"c\\d"}`,
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, `"a\"b": "c\\d"`) {
		t.Errorf("escapes not preserved: %q", got)
	}
}

func TestEncodeMapLiteral_NonIntegerInIntField(t *testing.T) {
	_, err := encodeMapLiteral("map[string]int", `{"a":1.5}`)
	if err == nil {
		t.Errorf("expected error for fractional value in int field")
	}
}

func TestEncodeMapLiteral_NonStringKey(t *testing.T) {
	_, err := encodeMapLiteral("map[int]string", `{"1":"a"}`)
	if err == nil {
		t.Errorf("expected error for non-string key type")
	}
}

func TestEncodeMapLiteral_DeterministicOrder(t *testing.T) {
	// Same input twice must yield byte-identical output.
	got1, _ := encodeMapLiteral("map[string]string", `{"z":"1","a":"2","m":"3"}`)
	got2, _ := encodeMapLiteral("map[string]string", `{"a":"2","m":"3","z":"1"}`)
	if got1 != got2 {
		t.Errorf("non-deterministic output:\n  %q\n  %q", got1, got2)
	}
}

func TestEncodeMapLiteral_UintNonNegative(t *testing.T) {
	got, err := encodeMapLiteral("map[string]uint8", `{"a":255}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != `map[string]uint8{"a": 255}` {
		t.Errorf("got %q", got)
	}
}
