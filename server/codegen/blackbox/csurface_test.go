// server/codegen/blackbox/csurface_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Tests for the multi-file public surface (csurface.go): what enters the
// renameable set, how identifier prefixing behaves around literals, the
// anti-hijack property of the rename defines, and the shape of the generated
// header and source preamble.
//
// Português: Testes da superfície pública multiarquivo — conjunto renomeável,
// prefixamento em volta de literais, propriedade anti-sequestro dos #define e
// forma do header gerado e do preâmbulo.

package blackbox

import (
	"strings"
	"testing"
)

// surfaceFixtureDef builds a def exercising every surface category at once:
// two functions (one cross-calls the other in RawSource), a wire type with
// tag+alias, an enum with a computed and a raw value, and a callback typedef
// whose parameters name the wire type.
func surfaceFixtureDef() *BlackBoxDef {
	return &BlackBoxDef{
		ID:     "3f9a2b1c3f9a2b1c3f9a2b1c3f9a2b1c",
		CodeID: "47",
		RawSource: `#include <stdio.h>

typedef struct sht3x { int fd; } sht3x_t;

typedef enum { MODE_FAST = 0, MODE_SLOW = (1 << 2) } sht3x_mode_t;

typedef void (*sht3x_alert_cb_t)(sht3x_t *dev, float value);

sht3x_t *sht3x_create(int bus) {
    (void)bus;
    return 0;
}

void sht3x_log(sht3x_t *dev, sht3x_mode_t mode) {
    (void)mode;
    printf("MODE_FAST as text stays untouched\n");
    sht3x_create(0); /* internal cross-call: renamed by the same defines */
    (void)dev;
}
`,
		Functions: []NamedFuncDef{
			{Name: "sht3x_create", FuncDef: FuncDef{
				CReturnType: "sht3x_t *",
				CParams:     "int bus",
				Outputs:     []PortDef{{Name: "return", GoType: "sht3x_t *"}},
			}},
			{Name: "sht3x_log", FuncDef: FuncDef{
				CReturnType: "void",
				CParams:     "sht3x_t *dev, sht3x_mode_t mode",
			}},
		},
		WireTypes: []StructDef{{Name: "sht3x", Alias: "sht3x_t"}},
		Enums: []EnumDef{{
			Name: "sht3x_mode_t",
			Values: []EnumValueDef{
				{Name: "MODE_FAST", Value: 0},
				{Name: "MODE_SLOW", ValueIsRaw: true, RawValue: "(1 << 2)"},
			},
		}},
		CallbackTypes: []CallbackTypeDef{{
			Name:       "sht3x_alert_cb_t",
			ReturnType: "void",
			Params:     "sht3x_t *dev, float value",
		}},
		Author: &AuthorInfo{Username: "specialist"},
	}
}

// TestNewCSurface_RequiresID pins the fallback contract: no database id → no
// surface → the emitter routes the def through the single-file inline path
// instead of inventing an identity.
func TestNewCSurface_RequiresID(t *testing.T) {
	if s := NewCSurface(nil, Naming{}); s != nil {
		t.Fatalf("NewCSurface(nil, Naming{}) = %v, want nil", s)
	}
	def := surfaceFixtureDef()
	def.ID = ""
	if s := NewCSurface(def, Naming{}); s != nil {
		t.Fatalf("NewCSurface(def with empty ID) = %v, want nil", s)
	}
	if s := NewCSurface(surfaceFixtureDef(), Naming{}); s == nil {
		t.Fatalf("NewCSurface(def with ID) = nil, want surface")
	}
}

// TestCSurface_PrefixIdentifiers covers the lexical rules: surface
// identifiers are renamed wherever they appear as WHOLE tokens; substrings,
// foreign identifiers and string/char literal contents pass through.
func TestCSurface_PrefixIdentifiers(t *testing.T) {
	s := NewCSurface(surfaceFixtureDef(), Naming{})
	cases := []struct{ in, want string }{
		// Type expressions — the main.c-side use (casts, declared types).
		{"sht3x_t *", "iotm_47_sht3x_t" + " *"},
		{"(sht3x_mode_t)", "(" + "iotm_47_sht3x_mode_t" + ")"},
		{"const sht3x_t *dev", "const " + "iotm_47_sht3x_t" + " *dev"},
		// Enum-constant default — the "=" literal use.
		{"MODE_FAST", "iotm_47_MODE_FAST"},
		// String/char literals are opaque: contents never renamed.
		{`"MODE_FAST"`, `"MODE_FAST"`},
		{`printf("MODE_FAST %d", MODE_FAST)`,
			`printf("MODE_FAST %d", ` + "iotm_47_MODE_FAST" + `)`},
		{`'"'`, `'"'`},
		{`"esc \" MODE_FAST"`, `"esc \" MODE_FAST"`},
		// Whole-token matching: no substring or foreign renames.
		{"sht3x_take", "sht3x_take"},
		{"my_sht3x_t", "my_sht3x_t"},
		{"uint8_t", "uint8_t"},
	}
	for _, c := range cases {
		if got := s.PrefixIdentifiers(c.in); got != c.want {
			t.Errorf("PrefixIdentifiers(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// nil receiver is a documented no-op (the inline-fallback branch).
	var nilS *CSurface
	if got := nilS.PrefixIdentifiers("sht3x_t"); got != "sht3x_t" {
		t.Errorf("nil surface PrefixIdentifiers = %q, want passthrough", got)
	}
}

// TestCSurface_RenameDefines_AntiHijack pins the security property through
// the preprocessor mechanism: the prefix is UNCONDITIONAL, so a source that
// declares a function already spelled like a victim's prefixed symbol gets
// its own id stamped ON TOP — the define maps the fake name to a
// double-prefixed one, and nothing this unit exports can collide with (or
// impersonate) the victim's real symbol.
func TestCSurface_RenameDefines_AntiHijack(t *testing.T) {
	def := surfaceFixtureDef()
	victim := "iotm_9999_read"
	def.Functions = append(def.Functions, NamedFuncDef{
		Name:    victim,
		FuncDef: FuncDef{CReturnType: "int", CParams: ""},
	})
	s := NewCSurface(def, Naming{})
	defines := s.RenameDefines()

	want := "#define " + victim + " iotm_47_" + victim
	if !strings.Contains(defines, want) {
		t.Fatalf("rename defines miss the anti-hijack double prefix:\nwant line %q\ngot:\n%s", want, defines)
	}
	// Every ordinary surface name gets exactly one define too.
	for _, name := range []string{"sht3x_create", "sht3x_log", "sht3x",
		"sht3x_t", "sht3x_mode_t", "MODE_FAST", "MODE_SLOW", "sht3x_alert_cb_t"} {
		if !strings.Contains(defines, "#define "+name+" iotm_47_"+name+"\n") {
			t.Errorf("rename defines miss %q:\n%s", name, defines)
		}
	}
}

// TestCSurface_Header checks the generated bb_<id>.h piece by piece: guard,
// opaque wire-type typedef (no members), complete enum with pinned computed
// values and prefixed raw expressions, callback typedef, and prototypes
// composed from the authored verbatim signature with surface identifiers
// prefixed.
func TestCSurface_Header(t *testing.T) {
	s := NewCSurface(surfaceFixtureDef(), Naming{})
	h := s.Header()

	guard := "IOTM_47_H"
	for _, want := range []string{
		"#ifndef " + guard,
		"#define " + guard,
		"#endif /* " + guard + " */",
		// Opaque handle: forward typedef, tag and alias both prefixed.
		"typedef struct " + "iotm_47_sht3x" + " " + "iotm_47_sht3x_t" + ";",
		// Enum: single prefixed name as BOTH tag and typedef; computed value
		// pinned explicitly; raw expression verbatim (nothing to prefix here).
		"typedef enum " + "iotm_47_sht3x_mode_t" + " {",
		"iotm_47_MODE_FAST" + " = 0,",
		"iotm_47_MODE_SLOW" + " = (1 << 2),",
		"} " + "iotm_47_sht3x_mode_t" + ";",
		// Callback typedef with prefixed parameter types.
		"typedef void (*" + "iotm_47_sht3x_alert_cb_t" + ")(" +
			"iotm_47_sht3x_t" + " *dev, float value);",
		// Prototypes: authored signature verbatim, surface names prefixed.
		"iotm_47_sht3x_t" + " * " + "iotm_47_sht3x_create" + "(int bus);",
		"void " + "iotm_47_sht3x_log" + "(" +
			"iotm_47_sht3x_t" + " *dev, " + "iotm_47_sht3x_mode_t" + " mode);",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("header misses %q\nheader:\n%s", want, h)
		}
	}
	if strings.Contains(h, "int fd;") {
		t.Errorf("header leaked wire-type MEMBERS — handles must stay opaque:\n%s", h)
	}
}

// TestCSurface_Header_BlankReturnFallsBackToInt covers a def whose stored
// parsed_json predates CReturnType: the prototype degrades to a compilable
// "int" instead of emitting a blank type — same stance as the backend's
// cOutputType profile fallback.
func TestCSurface_Header_BlankReturnFallsBackToInt(t *testing.T) {
	def := surfaceFixtureDef()
	def.Functions = []NamedFuncDef{{Name: "legacy_fn", FuncDef: FuncDef{}}}
	def.WireTypes, def.Enums, def.CallbackTypes = nil, nil, nil
	h := NewCSurface(def, Naming{}).Header()
	want := "int " + "iotm_47_legacy_fn" + "();"
	if !strings.Contains(h, want) {
		t.Fatalf("header misses the int fallback prototype %q:\n%s", want, h)
	}
}

// TestCSurface_Preamble checks the generated top of bb_<id>.c: author
// attribution, the licensing note (and the ABSENCE of the Generated Code
// Exception wording — the verbatim source below the marker is the author's,
// not IoTMaker's to relicense), the own-header include, the rename defines
// and the verbatim marker.
func TestCSurface_Preamble(t *testing.T) {
	s := NewCSurface(surfaceFixtureDef(), Naming{})
	p := s.Preamble()

	for _, want := range []string{
		"// authored by specialist\n",
		"remains under its author's own license",
		"#define sht3x_create " + "iotm_47_sht3x_create",
		"authored source below (verbatim)",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("preamble misses %q\npreamble:\n%s", want, p)
		}
	}
	if strings.Contains(p, "Generated Code Exception\n") &&
		strings.Contains(p, "You may license it as you choose") {
		t.Errorf("preamble must NOT stamp the Generated Code Exception over authored source:\n%s", p)
	}
	// The unit must NOT include its own header: the renamed source re-defines
	// the surface types, and typedef redefinition is a C99 hard error (the
	// gcc smoke test that motivated the postamble design).
	if strings.Contains(p, "#include \"iotm_47.h\"") {
		t.Errorf("preamble must not include the unit's own header (type redefinition):\n%s", p)
	}
}

// TestCSurface_Postamble checks the unit-local declaration check appended
// after the verbatim source: one UNPREFIXED re-declaration per parsed
// function (the still-active rename defines prefix them at preprocess time),
// and the skip rule for legacy defs with no parsed signature.
func TestCSurface_Postamble(t *testing.T) {
	s := NewCSurface(surfaceFixtureDef(), Naming{})
	p := s.Postamble()

	for _, want := range []string{
		"generated declaration check",
		"sht3x_t * sht3x_create(int bus);",
		"void sht3x_log(sht3x_t *dev, sht3x_mode_t mode);",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("postamble misses %q\npostamble:\n%s", want, p)
		}
	}
	if strings.Contains(p, "iotm_47_sht3x_create") {
		t.Errorf("postamble lines must be UNPREFIXED (the defines rename them):\n%s", p)
	}

	// Legacy def: no parsed signature → no check line, no fabricated error.
	legacy := surfaceFixtureDef()
	legacy.Functions = []NamedFuncDef{{Name: "legacy_fn", FuncDef: FuncDef{}}}
	if got := NewCSurface(legacy, Naming{}).Postamble(); got != "" {
		t.Errorf("legacy def (blank CReturnType) must produce an empty postamble, got:\n%s", got)
	}
}

// TestCSurface_FullIDFallback pins the other half of the CodeIdent contract:
// a def whose loader never stitched a code number composes every name from
// its FULL database id, in the same family — long but correct, never
// invented. This is what legacy blobs and pre-migration rows produce.
func TestCSurface_FullIDFallback(t *testing.T) {
	def := surfaceFixtureDef()
	def.CodeID = "" // no stitched number → CodeIdent falls back to ID
	s := NewCSurface(def, Naming{})
	if s == nil {
		t.Fatal("def with ID but no CodeID must still get a surface (fallback)")
	}
	if got, want := s.Code(), def.ID; got != want {
		t.Fatalf("Code() = %q, want the full id %q", got, want)
	}
	wantSym := "iotm_" + def.ID + "_sht3x_create"
	if defines := s.RenameDefines(); !strings.Contains(defines, wantSym) {
		t.Errorf("fallback defines miss %q:\n%s", wantSym, defines)
	}
	if h := s.Header(); !strings.Contains(h, "IOTM_"+strings.ToUpper(def.ID)+"_H") {
		t.Errorf("fallback guard missing from header:\n%s", h)
	}
}

// TestCSurface_CustomRadical pins that a maker-configured radical moves the
// surface's whole output — defines, header guard and prototypes alike.
func TestCSurface_CustomRadical(t *testing.T) {
	s := NewCSurface(surfaceFixtureDef(), NewNaming("acme_"))
	if defines := s.RenameDefines(); !strings.Contains(defines, "#define sht3x_create acme_47_sht3x_create") {
		t.Errorf("custom radical missing from defines:\n%s", defines)
	}
	h := s.Header()
	for _, want := range []string{"ACME_47_H", "acme_47_sht3x_create", "acme_47_sht3x_mode_t"} {
		if !strings.Contains(h, want) {
			t.Errorf("custom radical header misses %q:\n%s", want, h)
		}
	}
}
