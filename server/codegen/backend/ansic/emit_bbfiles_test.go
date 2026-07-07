// server/codegen/backend/ansic/emit_bbfiles_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// End-to-end tests of the multi-file C output: a program whose scene uses a
// black-box WITH a database id must come out as main.c + Makefile +
// bb_<id>/{.c,.h}, with every authored name prefixed on the main.c side and
// renamed inside the bb unit; a def WITHOUT an id must keep the single-file
// inline behaviour byte-for-byte. Also pins the three licensing regimes
// (generated files stamped, authored bb .c NOT stamped, Makefile stamped in
// `#` dialect) and the arg protocols "@" (handler reference), "=" (defaults,
// enum constants prefixed / string literals untouched) and "(cast)".
//
// Português: Testes fim-a-fim da saída C multiarquivo — chaves do map,
// main.c com include + chamada prefixada, preâmbulo do bb_<id>.c, os três
// regimes de carimbo de licença, Makefile explícito, fallback sem id e os
// protocolos de argumento "@", "=" e "(cast)".

package ansic

import (
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/ir"
)

// bbFixtureID is a realistic 32-hex database id (cryptoauth.NewID shape).
const bbFixtureID = "3f9a2b1c3f9a2b1c3f9a2b1c3f9a2b1c"

// bbFixtureDef mirrors the blackbox package's surface fixture: two functions
// (the source cross-calls internally), a wire type, an enum and a callback
// typedef — every surface category the emitter must prefix.
func bbFixtureDef(id string) *blackbox.BlackBoxDef {
	return &blackbox.BlackBoxDef{
		ID:     id,
		CodeID: "47",
		RawSource: `#include <stdio.h>

typedef struct sht3x { int fd; } sht3x_t;

typedef enum { MODE_FAST = 0, MODE_SLOW = 1 } sht3x_mode_t;

typedef void (*sht3x_alert_cb_t)(float value);

sht3x_t *sht3x_create(int bus) {
    (void)bus;
    return 0;
}

void sht3x_log(sht3x_t *dev, sht3x_mode_t mode, sht3x_alert_cb_t cb) {
    (void)dev; (void)mode; (void)cb;
    sht3x_create(0);
}
`,
		Functions: []blackbox.NamedFuncDef{
			{Name: "sht3x_create", FuncDef: blackbox.FuncDef{
				CReturnType: "sht3x_t *",
				CParams:     "int bus",
				Outputs:     []blackbox.PortDef{{Name: "return", GoType: "sht3x_t *"}},
			}},
			{Name: "sht3x_log", FuncDef: blackbox.FuncDef{
				CReturnType: "void",
				CParams:     "sht3x_t *dev, sht3x_mode_t mode, sht3x_alert_cb_t cb",
			}},
		},
		WireTypes: []blackbox.StructDef{{Name: "sht3x", Alias: "sht3x_t"}},
		Enums: []blackbox.EnumDef{{
			Name: "sht3x_mode_t",
			Values: []blackbox.EnumValueDef{
				{Name: "MODE_FAST", Value: 0},
				{Name: "MODE_SLOW", Value: 1},
			},
		}},
		CallbackTypes: []blackbox.CallbackTypeDef{{
			Name: "sht3x_alert_cb_t", ReturnType: "void", Params: "float value",
		}},
		Author: &blackbox.AuthorInfo{Username: "specialist"},
	}
}

// bbFixtureProgram wires one create + one log call through the fixture def:
//
//	%n1:return = sht3x_create((int)%c1)              — return wired, cast
//	sht3x_log(%n1:return, =MODE_FAST, @alert_handler) — enum default + handler
//
// The handler's own def (a SECOND black-box) proves the "@" reference is
// prefixed with the HANDLER's id, not the callee's.
func bbFixtureProgram() *ir.Program {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "c1", Type: "int", Args: []string{"1"}})
	prog.Append(ir.Instruction{
		Op:   ir.OpBBCall,
		Dest: "n1",
		Args: []string{"(int)%c1"},
		Meta: map[string]string{"fn": "sht3x_create", "connectedOutputs": "return"},
	})
	prog.Append(ir.Instruction{
		Op:   ir.OpBBCall,
		Dest: "n2",
		Args: []string{"%n1:return", "=MODE_FAST", "@alert_handler"},
		Meta: map[string]string{"fn": "sht3x_log", "connectedOutputs": ""},
	})

	handlerDef := &blackbox.BlackBoxDef{
		ID:        "beefbeefbeefbeefbeefbeefbeefbeef",
		CodeID:    "9",
		RawSource: "void alert_handler(float value) { (void)value; }\n",
		Functions: []blackbox.NamedFuncDef{
			{Name: "alert_handler", FuncDef: blackbox.FuncDef{
				CReturnType: "void", CParams: "float value",
				HandlerType: "sht3x_alert_cb_t",
			}},
		},
	}

	def := bbFixtureDef(bbFixtureID)
	prog.BlackBoxDefs = map[string]*blackbox.BlackBoxDef{
		"sht3x_create":  def,
		"sht3x_log":     def, // both functions share ONE def → one folder
		"alert_handler": handlerDef,
	}
	return prog
}

// TestEmit_MultiFile_FilesAndMainC pins the output shape: the four kinds of
// keys, main.c including (not inlining) the boxes and calling prefixed names,
// with authored type names prefixed in declarations and casts.
func TestEmit_MultiFile_FilesAndMainC(t *testing.T) {
	files := Emit(bbFixtureProgram(), ProfilePortable, blackbox.Naming{})

	for _, key := range []string{
		"main.c", "Makefile",
		"iotm_47/iotm_47.c", "iotm_47/iotm_47.h",
		"iotm_9/iotm_9.c", "iotm_9/iotm_9.h",
	} {
		if _, ok := files[key]; !ok {
			t.Fatalf("output misses key %q; got keys: %v", key, keysOf(files))
		}
	}

	mainC := files["main.c"]
	for _, want := range []string{
		"#include \"iotm_47/iotm_47.h\"",
		// Return capture: prefixed type, prefixed callee, prefixed cast.
		"iotm_47_sht3x_t *",
		"iotm_47_sht3x_create((int)",
		// Second call: enum default and handler reference, each under the
		// right owner's code — the handler lives in box 9, not 47.
		"iotm_47_sht3x_log(",
		"iotm_47_MODE_FAST",
		"iotm_9_alert_handler",
	} {
		if !strings.Contains(mainC, want) {
			t.Errorf("main.c misses %q\nmain.c:\n%s", want, mainC)
		}
	}
	// The authored implementation must NOT be inlined anymore.
	for _, banned := range []string{
		"authored device sources (inlined verbatim)",
		"typedef struct sht3x { int fd; }",
	} {
		if strings.Contains(mainC, banned) {
			t.Errorf("main.c still inlines authored source (%q present):\n%s", banned, mainC)
		}
	}
}

// TestEmit_MultiFile_BBSourceAndLicensing pins the three licensing regimes
// and the bb unit's anatomy: preamble (attribution + own-header include +
// rename defines + marker) above the VERBATIM source; exception header on the
// generated .h but NOT on the authored .c; `#`-dialect exception on the
// Makefile.
func TestEmit_MultiFile_BBSourceAndLicensing(t *testing.T) {
	files := Emit(bbFixtureProgram(), ProfilePortable, blackbox.Naming{})

	bbC := files["iotm_47/iotm_47.c"]
	for _, want := range []string{
		"// authored by specialist",
		"#define sht3x_create iotm_47_sht3x_create",
		"authored source below (verbatim)",
		"typedef struct sht3x { int fd; } sht3x_t;", // untouched authored text
		// Unit-local declaration check (unprefixed; defines rename it).
		"generated declaration check",
		"void sht3x_log(sht3x_t *dev, sht3x_mode_t mode, sht3x_alert_cb_t cb);",
	} {
		if !strings.Contains(bbC, want) {
			t.Errorf("bb .c misses %q\nfile:\n%s", want, bbC)
		}
	}
	if strings.Contains(bbC, "Generated by IoTMaker") {
		t.Errorf("bb .c must NOT carry the Generated Code Exception stamp (authored file):\n%s", bbC)
	}
	if strings.Contains(bbC, "#include \"iotm_47.h\"") {
		t.Errorf("bb .c must not include its own header (type redefinition — see Preamble):\n%s", bbC)
	}

	bbH := files["iotm_47/iotm_47.h"]
	if !strings.Contains(bbH, "Generated by IoTMaker") {
		t.Errorf("bb .h is fully generated and must carry the exception stamp:\n%s", bbH)
	}

	mk := files["Makefile"]
	if !strings.Contains(mk, "# Generated by IoTMaker") {
		t.Errorf("Makefile must carry the exception in `#` dialect:\n%s", mk)
	}
	if strings.Contains(mk, "// Generated") || strings.Contains(mk, "/*") {
		t.Errorf("Makefile must not contain C-style comments (breaks make):\n%s", mk)
	}
}

// TestEmit_MultiFile_Makefile pins the build-system decisions: explicit rules
// only (no pattern rules, no wildcards), objects INSIDE the bb folders (the
// self-attributing linker tripwire), main.o depending on every bb header, and
// overridable CC/CFLAGS/TARGET.
func TestEmit_MultiFile_Makefile(t *testing.T) {
	mk := Emit(bbFixtureProgram(), ProfilePortable, blackbox.Naming{})["Makefile"]

	for _, want := range []string{
		"CC      ?= cc",
		"CFLAGS  ?= -std=c99 -Wall -Wextra -O2",
		"iotm_47/iotm_47.o: iotm_47/iotm_47.c iotm_47/iotm_47.h",
		// main.o depends on every bb header, listed in full-id order (units
		// sort by database id, so 3f9a… precedes beef… → 47 before 9).
		"main.o: main.c iotm_47/iotm_47.h iotm_9/iotm_9.h",
		"clean:",
		".PHONY: all clean",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("Makefile misses %q\nMakefile:\n%s", want, mk)
		}
	}
	for _, banned := range []string{"%.o", "$(wildcard", ".c.o:"} {
		if strings.Contains(mk, banned) {
			t.Errorf("Makefile must use explicit rules only; found %q:\n%s", banned, mk)
		}
	}
}

// TestEmit_InlineFallback_NoID pins the other half of the BlackBoxDef.ID
// contract: a def that never touched the database keeps the single-file
// behaviour — source inlined verbatim under bare names, no folder, no
// Makefile — because the emitter must never invent an identity.
func TestEmit_InlineFallback_NoID(t *testing.T) {
	prog := bbFixtureProgram()
	for _, def := range prog.BlackBoxDefs {
		def.ID = ""     // strip every identity → all boxes fall back
		def.CodeID = "" // and every stitched number, for good measure
	}
	files := Emit(prog, ProfilePortable, blackbox.Naming{})

	if _, ok := files["Makefile"]; ok {
		t.Fatalf("fallback must not emit a Makefile; got keys: %v", keysOf(files))
	}
	for key := range files {
		if strings.Contains(key, "/") {
			t.Fatalf("fallback must not emit black-box folders; got key %q", key)
		}
	}
	mainC := files["main.c"]
	for _, want := range []string{
		"authored device sources (inlined verbatim)",
		"typedef struct sht3x { int fd; } sht3x_t;",
		"sht3x_create((int)", // bare name, no prefix
		"sht3x_log(",
		"MODE_FAST",      // bare enum default
		"alert_handler)", // bare handler reference via "@" → bare fallback
	} {
		if !strings.Contains(mainC, want) {
			t.Errorf("fallback main.c misses %q\nmain.c:\n%s", want, mainC)
		}
	}
	if strings.Contains(mainC, "iotm_") {
		t.Errorf("fallback main.c must not contain prefixed symbols:\n%s", mainC)
	}
}

// TestEmit_MultiFile_StringDefaultUntouched guards the "=" protocol's literal
// rule: a string default containing an enum constant's spelling must pass
// through verbatim — only bare identifier tokens are renamed.
func TestEmit_MultiFile_StringDefaultUntouched(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpBBCall,
		Dest: "n1",
		Args: []string{`="MODE_FAST"`},
		Meta: map[string]string{"fn": "sht3x_log", "connectedOutputs": ""},
	})
	def := bbFixtureDef(bbFixtureID)
	prog.BlackBoxDefs = map[string]*blackbox.BlackBoxDef{"sht3x_log": def}

	mainC := Emit(prog, ProfilePortable, blackbox.Naming{})["main.c"]
	want := `iotm_47_sht3x_log("MODE_FAST")`
	if !strings.Contains(mainC, want) {
		t.Fatalf("string default was altered; want %q in:\n%s", want, mainC)
	}
}

// keysOf lists a file map's keys for failure messages.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
