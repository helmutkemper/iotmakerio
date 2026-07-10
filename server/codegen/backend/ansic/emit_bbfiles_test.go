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
		Files: []blackbox.FileEntry{{Path: "dev.c", Content: `#include <stdio.h>

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
`}},
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
		ID:     "beefbeefbeefbeefbeefbeefbeefbeef",
		CodeID: "9",
		Files:  []blackbox.FileEntry{{Path: "dev.c", Content: "void alert_handler(float value) { (void)value; }\n"}},
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
		"iotm_47/dev.c", "iotm_47/iotm_47.h",
		"iotm_9/dev.c", "iotm_9/iotm_9.h",
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

	bbC := files["iotm_47/dev.c"]
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
		"iotm_47/dev.o: iotm_47/dev.c iotm_47/iotm_47.h",
		// main.o depends on every bb header, listed in full-id order (units
		// sort by database id, so 3f9a… precedes beef… → 47 before 9).
		"main.o: main.c iotm_47/iotm_47.h iotm_9/iotm_9.h",
		// Convenience targets (owner request, 2026-07): a bare `make` still
		// builds; `run` executes after building when stale; `buildandrun`
		// is the explicit spelling of the same graph.
		"all: build",
		"build: $(TARGET)",
		"run: $(TARGET)\n\t./$(TARGET)",
		"buildandrun: build run",
		"clean:",
		".PHONY: all build run buildandrun clean",
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

// TestEmit_SingleFile_Makefile pins the 2026-07 contract change: the Makefile
// ships even when no black-box folder exists (owner request — the single-file
// zip must carry build/run targets). main.c stays buildable with a bare
// `gcc main.c`; the Makefile only adds the convenience vocabulary on top —
// build, run (dependency-built, so a beginner never sees "No such file") and
// buildandrun — compiling exactly the files that shipped and nothing else.
//
// Português: Pina a mudança de contrato de 2026-07: o Makefile viaja mesmo
// sem pasta de black-box (pedido do dono — o zip single-file precisa dos
// alvos build/run). O main.c segue compilável com `gcc main.c` puro; o
// Makefile só adiciona o vocabulário de conveniência — build, run (compila
// antes, por dependência) e buildandrun — compilando exatamente os arquivos
// que viajaram e nada mais.
func TestEmit_SingleFile_Makefile(t *testing.T) {
	t.Run("no runtime — main.o alone", func(t *testing.T) {
		mk, ok := Emit(&ir.Program{}, ProfilePortable, blackbox.Naming{})["Makefile"]
		if !ok {
			t.Fatal("single-file export must ship a Makefile")
		}
		for _, want := range []string{
			"# Generated by IoTMaker",
			"OBJS = main.o\n",
			"all: build",
			"build: $(TARGET)",
			"run: $(TARGET)\n\t./$(TARGET)",
			"buildandrun: build run",
			"main.o: main.c\n",
			".PHONY: all build run buildandrun clean",
		} {
			if !strings.Contains(mk, want) {
				t.Errorf("single-file Makefile misses %q\nMakefile:\n%s", want, mk)
			}
		}
		// No black-box leftovers, no C-style comments (breaks make).
		// Português: Sem sobras de black-box, sem comentários C (quebra o make).
		for _, banned := range []string{"iotm_", "// Generated", "/*"} {
			if strings.Contains(mk, banned) {
				t.Errorf("single-file Makefile must not contain %q:\n%s", banned, mk)
			}
		}
	})

	t.Run("with runtime — stub compiles, main.o depends on the header", func(t *testing.T) {
		prog := &ir.Program{}
		prog.Append(ir.Instruction{
			Op:   ir.OpSleep,
			Args: []string{"%constDuration_1"},
		})
		mk := Emit(prog, ProfilePortable, blackbox.Naming{})["Makefile"]
		for _, want := range []string{
			"OBJS = main.o \\\n       iotmaker_runtime_stub.o",
			"main.o: main.c iotmaker_runtime.h",
			"iotmaker_runtime_stub.o: iotmaker_runtime_stub.c",
		} {
			if !strings.Contains(mk, want) {
				t.Errorf("runtime Makefile misses %q\nMakefile:\n%s", want, mk)
			}
		}
	})
}

// TestEmit_InlineFallback_NoID pins the other half of the BlackBoxDef.ID
// contract: a def that never touched the database keeps the single-file
// IDENTITY behaviour — source inlined verbatim under bare names, no folder —
// because the emitter must never invent an identity. The Makefile is the one
// deliberate exception since 2026-07: it ships in every mode (owner request),
// but in fallback it must compile main.c alone — no bb folders, no prefixed
// objects.
//
// Português: Pina a outra metade do contrato do ID: def sem banco mantém a
// IDENTIDADE single-file — fonte inline com nomes puros, sem pasta. O
// Makefile é a exceção deliberada desde 2026-07 (pedido do dono): viaja em
// todo modo, mas no fallback compila só o main.c — sem pastas, sem objetos
// prefixados.
func TestEmit_InlineFallback_NoID(t *testing.T) {
	prog := bbFixtureProgram()
	for _, def := range prog.BlackBoxDefs {
		def.ID = ""     // strip every identity → all boxes fall back
		def.CodeID = "" // and every stitched number, for good measure
	}
	files := Emit(prog, ProfilePortable, blackbox.Naming{})

	mk, ok := files["Makefile"]
	if !ok {
		t.Fatalf("Makefile must ship in every mode; got keys: %v", keysOf(files))
	}
	if strings.Contains(mk, "iotm_") {
		t.Errorf("fallback Makefile must compile main.c alone — no bb folders/objects:\n%s", mk)
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

// TestEmit_MultiFile_AuthoredSet is the multi-file heart of the emitter:
// one box shipping a header plus two translation units under a subfolder,
// with a cross-file non-static helper. It pins the four multi-file
// promises at once:
//
//   - every authored file lands under the box folder with its AUTHORED
//     name and relative path (util/helpers.c stays nested);
//   - each .c gets the SAME preamble (rename defines — including the
//     ExternalNames entry) and the SAME postamble (compatible
//     redeclaration per translation unit is legal C99, so every unit
//     carries the cross-check);
//   - the authored .h ships VERBATIM — no preamble, no stamp: it is
//     renamed at inclusion time by the including .c's defines;
//   - the Makefile compiles one object per authored .c, nested paths
//     included, each depending on the generated header.
//
// Português: O coração multiarquivo: header + duas unidades (uma em
// subpasta) com helper não-static entre arquivos. Fixa as quatro
// promessas: nome autoral preservado; MESMO preâmbulo/posâmbulo em cada
// .c (redeclaração compatível por unidade é C99 legal); .h verbatim
// (renomeia na inclusão); Makefile com um objeto por .c.
func TestEmit_MultiFile_AuthoredSet(t *testing.T) {
	def := &blackbox.BlackBoxDef{
		ID:     bbFixtureID,
		CodeID: "47",
		Files: []blackbox.FileEntry{
			{Path: "api.h", Content: "typedef struct probe { int fd; } probe_t;\n"},
			{Path: "core.c", Content: "#include \"api.h\"\nint probe_read(probe_t *p) { return util_clamp(p->fd); }\n"},
			{Path: "util/helpers.c", Content: "int g_probe_bias = 0;\nint util_clamp(int v) { return v + g_probe_bias; }\n"},
		},
		Functions: []blackbox.NamedFuncDef{
			{Name: "probe_read", FuncDef: blackbox.FuncDef{
				CReturnType: "int",
				CParams:     "probe_t *p",
				Outputs:     []blackbox.PortDef{{Name: "return", GoType: "int"}},
			}},
			{Name: "util_clamp", FuncDef: blackbox.FuncDef{
				CReturnType: "int",
				CParams:     "int v",
				Outputs:     []blackbox.PortDef{{Name: "return", GoType: "int"}},
			}},
		},
		WireTypes:     []blackbox.StructDef{{Name: "probe", Alias: "probe_t"}},
		ExternalNames: []string{"g_probe_bias"},
	}
	prog := &ir.Program{
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
			"probe_read": def,
			"util_clamp": def,
		},
	}
	files := Emit(prog, ProfilePortable, blackbox.NewNaming(""))

	for _, key := range []string{
		"iotm_47/iotm_47.h", "iotm_47/api.h", "iotm_47/core.c", "iotm_47/util/helpers.c",
	} {
		if _, ok := files[key]; !ok {
			t.Fatalf("output misses %q; got keys: %v", key, keysOf(files))
		}
	}

	// The authored header is byte-identical: verbatim means verbatim.
	if files["iotm_47/api.h"] != def.Files[0].Content {
		t.Fatalf("authored .h must ship verbatim; got:\n%s", files["iotm_47/api.h"])
	}

	// Both .c units carry the same rename define for the cross-file
	// external variable, and both end with the postamble's cross-check.
	for _, cKey := range []string{"iotm_47/core.c", "iotm_47/util/helpers.c"} {
		unit := files[cKey]
		if !strings.Contains(unit, "#define g_probe_bias iotm_47_g_probe_bias") {
			t.Fatalf("%s misses the external-variable rename define", cKey)
		}
		if !strings.Contains(unit, "#define util_clamp iotm_47_util_clamp") {
			t.Fatalf("%s misses the helper rename define", cKey)
		}
		if !strings.Contains(unit, "generated declaration check") {
			t.Fatalf("%s misses the postamble cross-check", cKey)
		}
	}

	mk := files["Makefile"]
	for _, want := range []string{
		"iotm_47/core.o: iotm_47/core.c iotm_47/iotm_47.h",
		"iotm_47/util/helpers.o: iotm_47/util/helpers.c iotm_47/iotm_47.h",
	} {
		if !strings.Contains(mk, want) {
			t.Fatalf("Makefile misses %q; got:\n%s", want, mk)
		}
	}
}

// The maker-side half of the unified asset model, pinned: a box whose
// def carries assets ships them DECODED into its folder, each with its
// generated companion header beside it — so the specialist's
// hand-written #includes resolve in the maker's build (field gap
// 2026-07-08: the export had the includes but not the headers; gcc
// died on file-not-found).
//
// Português: A metade maker do modelo de assets, fixada: caixa com
// assets no def os embarca DECODIFICADOS na pasta, cada um com seu
// header companheiro — os #includes manuais do especialista resolvem
// no build do maker.
func TestBBFiles_AssetsShipWithCompanionHeaders(t *testing.T) {
	def := &blackbox.BlackBoxDef{
		ID:     "abc123",
		CodeID: "7",
		Files: []blackbox.FileEntry{
			{Path: "core.c", Content: "// label:X.\nvoid probe_go(void) {}\n"},
		},
		Assets: []blackbox.AssetEntry{
			{Path: "templates/portal.html", Content: "<html>hi</html>"},
			// "R0lGODlh" = base64 of "GIF89a" — the binary lane.
			{Path: "logo.gif", Content: "R0lGODlh", Encoding: "base64"},
		},
		Functions: []blackbox.NamedFuncDef{{Name: "probe_go", FuncDef: blackbox.FuncDef{}}},
	}
	surf := blackbox.NewCSurface(def, blackbox.NewNaming(""))
	out := map[string]string{}
	(&cEmitter{naming: blackbox.NewNaming("")}).bbFiles([]*blackbox.CSurface{surf}, out)

	dir := blackbox.NewNaming("").SourceDir(surf.Code())
	if got := out[dir+"/templates/portal.html"]; got != "<html>hi</html>" {
		t.Errorf("text asset must ship verbatim; got %q", got)
	}
	if got := out[dir+"/logo.gif"]; got != "GIF89a" {
		t.Errorf("binary asset must ship DECODED; got %q", got)
	}
	hdr := out[dir+"/templates/portal_html_data.h"]
	if !strings.Contains(hdr, "asset_templates_portal_html[] = {") ||
		!strings.Contains(hdr, "asset_templates_portal_html_len = 15;") {
		t.Errorf("companion header missing/incomplete:\n%s", hdr)
	}
	if gif := out[dir+"/logo_gif_data.h"]; !strings.Contains(gif, "0x47,") {
		t.Errorf("gif header must carry the decoded bytes ('G'=0x47):\n%s", gif)
	}
}
