// server/codegen/blackbox/parser_c_files_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// The specialist shape these tests defend: a real C project split the way
// embedded C is actually written — a public header with the wire types, a
// core .c with the device functions, a util .c with shared non-static
// helpers and state. The merge must present ONE def as if the box were one
// source, and the rename machinery must cover the cross-file externals.
//
// Português: A forma real de um projeto C embarcado — header público com os
// tipos, core.c com as funções, util.c com helpers e estado compartilhados
// não-static. O merge apresenta UM def; a renomeação cobre os externos.

const mfAPIH = `// stdOut device public API.
typedef struct probe { int fd; } probe_t;
`

const mfCoreC = `#include <stdio.h>
#include "api.h"

// label:Read probe. icon:gauge.
// connection:in.
int probe_read(probe_t *p) { return util_clamp(p->fd); }
`

const mfUtilC = `#include "api.h"

int g_probe_bias = 0;

int util_clamp(int v) {
	if (v < 0) { return 0; }
	return v + g_probe_bias;
}
`

// TestParseCFiles_MergeAcrossFiles proves the multi-file walk: functions
// from every .c, wire types from the header (visible exactly once), the
// external variable captured, and the snapshot carried on the def.
func TestParseCFiles_MergeAcrossFiles(t *testing.T) {
	files := []FileEntry{
		{Path: "api.h", Content: mfAPIH},
		{Path: "core.c", Content: mfCoreC},
		{Path: "util.c", Content: mfUtilC},
	}
	def, err := ParseCFiles(files, DefaultParserLimits())
	if err != nil || def == nil {
		t.Fatalf("ParseCFiles = (%v, %v), want (def, nil)", def, err)
	}

	if len(def.Files) != 3 || def.Files[1].Path != "core.c" {
		t.Fatalf("def.Files must carry the snapshot verbatim in order, got %+v", def.Files)
	}

	fnFile := map[string]string{}
	for _, fn := range def.Functions {
		fnFile[fn.Name] = fn.SourceFile
	}
	if _, ok := fnFile["probe_read"]; !ok {
		t.Fatalf("functions from every .c must merge; got %v", fnFile)
	}
	if _, ok := fnFile["util_clamp"]; !ok {
		t.Fatalf("functions from every .c must merge; got %v", fnFile)
	}
	// Provenance: each function points at the file whose parse produced
	// it — the wizard's per-file card grouping depends on this.
	if fnFile["probe_read"] != "core.c" || fnFile["util_clamp"] != "util.c" {
		t.Fatalf("SourceFile stamping drift: %v", fnFile)
	}

	wire := 0
	for _, w := range def.WireTypes {
		if w.Name == "probe" || w.Alias == "probe_t" {
			wire++
		}
	}
	if wire != 1 {
		t.Fatalf("header wire type must surface exactly once, got %d", wire)
	}

	found := false
	for _, n := range def.ExternalNames {
		if n == "g_probe_bias" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cross-file state g_probe_bias must join ExternalNames, got %v", def.ExternalNames)
	}
}

// TestParseCFiles_DuplicateFunctionKeepsFirst pins the tolerant dedupe:
// a duplicate definition across files is the specialist's link error to
// own — the wizard keeps rendering (first wins), never crashes.
func TestParseCFiles_DuplicateFunctionKeepsFirst(t *testing.T) {
	files := []FileEntry{
		{Path: "a.c", Content: "// label:A. icon:a.\nint ping(void) { return 1; }"},
		{Path: "b.c", Content: "// label:B. icon:b.\nint ping(void) { return 2; }"},
	}
	def, err := ParseCFiles(files, DefaultParserLimits())
	if err != nil || def == nil {
		t.Fatalf("ParseCFiles = (%v, %v)", def, err)
	}
	count := 0
	var label string
	for _, fn := range def.Functions {
		if fn.Name == "ping" {
			count++
			label = fn.Label
		}
	}
	if count != 1 || label != "A" {
		t.Fatalf("duplicate function: want one entry with the FIRST label (A), got count=%d label=%q", count, label)
	}
}

// TestParseCFiles_HardErrorNamesThePath: an unterminated brace in the
// second tab must abort with the PATH in the error so the wizard can
// focus the offending tab.
func TestParseCFiles_HardErrorNamesThePath(t *testing.T) {
	files := []FileEntry{
		{Path: "ok.c", Content: "int fine(void) { return 0; }"},
		{Path: "broken.c", Content: "typedef struct broken { int x;"},
	}
	def, err := ParseCFiles(files, DefaultParserLimits())
	if def != nil || err == nil {
		t.Fatalf("ParseCFiles(broken) = (%v, %v), want (nil, error)", def, err)
	}
	if !strings.Contains(err.Error(), "broken.c") {
		t.Fatalf("hard error must carry the file path, got %q", err)
	}
}

// TestCSurface_ExternalNames_RenamedNotExposed pins the rename-all,
// expose-some contract at the surface layer: the external variable gets a
// rename define in the preamble, and the generated header never mentions
// it — the header is the IDS contract, not the box's internals.
func TestCSurface_ExternalNames_RenamedNotExposed(t *testing.T) {
	def := &BlackBoxDef{
		ID:     "3f9a2b1c3f9a2b1c3f9a2b1c3f9a2b1c",
		CodeID: "47",
		Files: []FileEntry{
			{Path: "core.c", Content: mfCoreC},
			{Path: "util.c", Content: mfUtilC},
		},
		Functions: []NamedFuncDef{
			{Name: "probe_read", FuncDef: FuncDef{Label: "Read"}},
			{Name: "util_clamp", FuncDef: FuncDef{Label: "Clamp"}},
		},
		ExternalNames: []string{"g_probe_bias"},
	}
	s := NewCSurface(def, Naming{})
	if s == nil {
		t.Fatal("NewCSurface = nil for an identified def")
	}
	pre := s.Preamble()
	if !strings.Contains(pre, "#define g_probe_bias iotm_47_g_probe_bias") {
		t.Fatalf("preamble must rename the external variable; got:\n%s", pre)
	}
	if h := s.Header(); strings.Contains(h, "g_probe_bias") {
		t.Fatalf("generated header must NOT expose internals; got:\n%s", h)
	}
}

// TestParseCFiles_DefinitionUpgradesPrototype pins the header-first rule:
// the STANDARD C layout declares in api.h and defines in core.c, so the
// natural tab order sees the bare prototype BEFORE the annotated
// definition. The merge must upgrade in place — same position (the header
// dictates reading order), definition's annotations (the .c supplies the
// meat) — and the mirror order (definition first, prototype later) must
// not downgrade.
//
// Português: O layout padrão do C declara no api.h e define no core.c —
// a ordem natural das abas vê o protótipo cru ANTES da definição anotada.
// O merge promove no lugar (posição do header, anotações do .c); a ordem
// espelhada não pode rebaixar.
func TestParseCFiles_DefinitionUpgradesPrototype(t *testing.T) {
	header := FileEntry{Path: "api.h", Content: "int probe_read(int fd);\n"}
	core := FileEntry{Path: "core.c", Content: `
// label:Read probe.
// icon:gauge.
int probe_read(int fd) { return fd; }
`}

	for _, tc := range []struct {
		name  string
		files []FileEntry
	}{
		{"header first (standard layout)", []FileEntry{header, core}},
		{"definition first (mirror order)", []FileEntry{core, header}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def, err := ParseCFiles(tc.files, DefaultParserLimits())
			if err != nil || def == nil {
				t.Fatalf("ParseCFiles = (%v, %v)", def, err)
			}
			var got *NamedFuncDef
			count := 0
			for i := range def.Functions {
				if def.Functions[i].Name == "probe_read" {
					got = &def.Functions[i]
					count++
				}
			}
			if count != 1 {
				t.Fatalf("probe_read must merge to ONE entry, got %d", count)
			}
			if !got.HasBody {
				t.Fatalf("merged entry must be the DEFINITION (HasBody), got prototype")
			}
			if got.Label != "Read probe" {
				t.Fatalf("merged entry must carry the definition's annotations; label = %q", got.Label)
			}
			if got.SourceFile != "core.c" {
				t.Fatalf("merged entry must point at the DEFINITION's file; got %q", got.SourceFile)
			}
		})
	}
}
