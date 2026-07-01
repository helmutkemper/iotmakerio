// /server/codegen/blackbox/parser_c_callback_compat_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"testing"
)

// TestParseC_Callback_CompatibleCallbacks verifies the per-function
// CompatibleCallbacks list: a function is offered a callback type ONLY when
// its C signature matches that typedef (return type + parameter types). This
// is what lets the wizard disable the "Callback handler" dropdown for
// functions that cannot be a handler (displayInit, setDisplay, wifiConnect)
// and offer display_write_fn only for the function that actually matches it
// (displayWrite).
func TestParseC_Callback_CompatibleCallbacks(t *testing.T) {
	src := "" +
		"typedef void (*display_write_fn)(const char *text);\n" +
		"\n" +
		"int displayInit(void) { return 0; }\n" +
		"\n" +
		"void displayWrite(const char *text) { (void)text; }\n" +
		"\n" +
		"void setDisplay(display_write_fn writer) { (void)writer; }\n" +
		"\n" +
		"bool wifiConnect(void) { return true; }\n"

	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}

	want := map[string][]string{
		"displayInit":  nil,                  // int(void) — no match
		"displayWrite": {"display_write_fn"}, // void(const char*) — matches
		"setDisplay":   nil,                  // takes the callback, is not one
		"wifiConnect":  nil,                  // bool(void) — no match
	}

	got := map[string][]string{}
	for i := range def.Functions {
		got[def.Functions[i].Name] = def.Functions[i].CompatibleCallbacks
	}

	for name, exp := range want {
		g := got[name]
		if len(exp) != len(g) {
			t.Fatalf("%s: want CompatibleCallbacks %v, got %v", name, exp, g)
		}
		for i := range exp {
			if exp[i] != g[i] {
				t.Fatalf("%s: want CompatibleCallbacks %v, got %v", name, exp, g)
			}
		}
	}
}

// TestNormaliseCType_SpacingInvariant guards the spacing canonicalisation the
// signature match relies on — pointer spacing must not change the result.
func TestNormaliseCType_SpacingInvariant(t *testing.T) {
	cases := [][2]string{
		{"const char *", "const char*"},
		{"const char*", "const char*"},
		{"const   char  *", "const char*"},
		{"char **", "char**"},
		{"int", "int"},
		{"void *", "void*"},
	}
	for _, c := range cases {
		if got := normaliseCType(c[0]); got != c[1] {
			t.Fatalf("normaliseCType(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}
