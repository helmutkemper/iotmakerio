// server/codegen/bbfiles_validate_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/diagnostics"
)

// Each rule of validateBlackBoxFiles exists because a specific downstream
// failure is worse than a 4xx: a zip-slip on the maker's machine (R1), an
// un-includable flattened main.c (R2), a linker hunting definitions a
// header promised (R3), a duplicate main() (R4), the generated header
// silently overwritten by an authored file (R5). The table walks one
// violation per rule plus the happy path.
//
// Português: Cada regra evita uma falha pior que um 4xx lá na frente:
// zip-slip no unzip do maker, main.c achatado sem include local, linker
// caçando definição prometida, main() duplicado, header gerado sobrescrito.
func TestValidateBlackBoxFiles_Rules(t *testing.T) {
	naming := blackbox.NewNaming("")
	id := "3f9a2b1c3f9a2b1c3f9a2b1c3f9a2b1c"

	mk := func(def *blackbox.BlackBoxDef) map[string]*blackbox.BlackBoxDef {
		return map[string]*blackbox.BlackBoxDef{"fn": def}
	}

	cases := []struct {
		name    string
		defs    map[string]*blackbox.BlackBoxDef
		wantSub string // "" → expect NO diagnostics
	}{
		{
			name: "happy path: header + two .c, identified",
			defs: mk(&blackbox.BlackBoxDef{ID: id, CodeID: "47", Files: []blackbox.FileEntry{
				{Path: "api.h", Content: "typedef int probe_t;"},
				{Path: "core.c", Content: "int a(void){return 0;}"},
				{Path: "util.c", Content: "int b(void){return 0;}"},
			}}),
			wantSub: "",
		},
		{
			name: "R1 hostile path",
			defs: mk(&blackbox.BlackBoxDef{ID: id, CodeID: "47", Files: []blackbox.FileEntry{
				{Path: "../evil.c", Content: "int a(void){return 0;}"},
			}}),
			wantSub: "not a plain relative path",
		},
		{
			name: "R2 multi-file without identity",
			defs: mk(&blackbox.BlackBoxDef{Files: []blackbox.FileEntry{
				{Path: "a.c", Content: "int a(void){return 0;}"},
				{Path: "b.c", Content: "int b(void){return 0;}"},
			}}),
			wantSub: "without a database identity",
		},
		{
			name: "R3 identified box with headers only",
			defs: mk(&blackbox.BlackBoxDef{ID: id, CodeID: "47", Files: []blackbox.FileEntry{
				{Path: "api.h", Content: "typedef int probe_t;"},
			}}),
			wantSub: "ships no .c",
		},
		{
			name: "R4 authored main()",
			defs: mk(&blackbox.BlackBoxDef{ID: id, CodeID: "47",
				Files:     []blackbox.FileEntry{{Path: "dev.c", Content: "int main(void){return 0;}"}},
				Functions: []blackbox.NamedFuncDef{{Name: "main"}},
			}),
			wantSub: "must not define main()",
		},
		{
			name: "R5 collision with the generated header",
			defs: mk(&blackbox.BlackBoxDef{ID: id, CodeID: "47", Files: []blackbox.FileEntry{
				{Path: "iotm_47.h", Content: "// impostor"},
				{Path: "core.c", Content: "int a(void){return 0;}"},
			}}),
			wantSub: "collides with the generated header",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateBlackBoxFiles(tc.defs, naming)
			if tc.wantSub == "" {
				if len(diags) != 0 {
					t.Fatalf("want no diagnostics, got %+v", diags)
				}
				return
			}
			if len(diags) == 0 {
				t.Fatalf("want a diagnostic containing %q, got none", tc.wantSub)
			}
			found := false
			for _, d := range diags {
				if d.Kind != diagnostics.KindBlackBoxFilesInvalid {
					t.Fatalf("diagnostic kind = %q, want %q", d.Kind, diagnostics.KindBlackBoxFilesInvalid)
				}
				if strings.Contains(d.Message, tc.wantSub) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no diagnostic contains %q; got %+v", tc.wantSub, diags)
			}
		})
	}
}

// TestValidateBlackBoxFiles_SharedDefValidatedOnce: several map keys share
// one *def (one source, many devices) — a violation must be reported once,
// not once per function name.
func TestValidateBlackBoxFiles_SharedDefValidatedOnce(t *testing.T) {
	def := &blackbox.BlackBoxDef{ID: "3f9a2b1c", CodeID: "47", Files: []blackbox.FileEntry{
		{Path: "../evil.c", Content: "int a(void){return 0;}"},
	}}
	defs := map[string]*blackbox.BlackBoxDef{"a": def, "b": def, "c": def}
	diags := validateBlackBoxFiles(defs, blackbox.NewNaming(""))
	if len(diags) != 1 {
		t.Fatalf("shared def must be validated once: got %d diagnostics", len(diags))
	}
}
