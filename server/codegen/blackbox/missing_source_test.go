// server/codegen/blackbox/missing_source_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestMissingFunctionSources pins the hybrid-def detector: a function whose
// SourceFile is absent from def.Files is reported once per file; complete
// defs report nothing. Reproduces the 2026-07-11 field case.
// Português: Pina o detector de def híbrido; reproduz o caso de campo.
func TestMissingFunctionSources(t *testing.T) {
	def := &BlackBoxDef{
		Files: []FileEntry{{Path: "portal_core.c", Content: "x"}},
		Functions: []NamedFuncDef{
			{Name: "portal_page_size", FuncDef: FuncDef{}},
			{Name: "portal_server_start", FuncDef: FuncDef{}},
		},
	}
	def.Functions[0].SourceFile = "portal_core.c"
	def.Functions[1].SourceFile = "portal_server.c"

	got := def.MissingFunctionSources()
	if len(got) != 1 || got[0] != "portal_server.c" {
		t.Fatalf("missing = %v, want [portal_server.c]", got)
	}

	def.Files = append(def.Files, FileEntry{Path: "portal_server.c", Content: "y"})
	if got := def.MissingFunctionSources(); len(got) != 0 {
		t.Fatalf("complete def reported %v", got)
	}
}
