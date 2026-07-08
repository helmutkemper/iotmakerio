// server/codegen/blackbox/parser_c_external_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"reflect"
	"testing"
)

// TestExtractCExternalVars_Shapes walks the declaration shapes embedded C
// actually uses. Each case documents WHY it is (or is not) an external
// symbol — the table is the scanner's contract in executable form.
//
// Português: As formas de declaração que o C embarcado realmente usa; cada
// caso documenta POR QUE é (ou não) símbolo externo. A tabela é o contrato
// do scanner em forma executável.
func TestExtractCExternalVars_Shapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"plain int", "int g_state;", []string{"g_state"}},
		{"with initializer", "int g_bias = 42;", []string{"g_bias"}},
		{"pointer", "char *g_buf;", []string{"g_buf"}},
		{"array suffix stripped", "int g_table[16];", []string{"g_table"}},
		{"multi-declarator", "int a, b = 2, c[4];", []string{"a", "b", "c"}},
		{"qualified", "volatile unsigned long g_ticks;", []string{"g_ticks"}},
		{"struct instance after body", "struct cfg { int x; } g_cfg;", []string{"g_cfg"}},
		{"fn-pointer variable", "int (*g_handler)(int);", []string{"g_handler"}},

		// Filtered shapes — each is NOT an external definition here.
		{"static is internal linkage", "static int hidden;", nil},
		{"extern is someone else's", "extern int elsewhere;", nil},
		{"typedef declares a type", "typedef unsigned char byte_t;", nil},
		{"function definition", "int run(void) { return 0; }", nil},
		{"prototype", "int run(void);", nil},
		{"preprocessor only", "#include <stdio.h>\n#define X 1\n", nil},
		{"bare struct decl", "struct fwd;", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stripped, _ := preprocessC(tc.src)
			got := extractCExternalVars(stripped)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractCExternalVars(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

// TestExtractCExternalVars_BodySkipping proves the depth walk: names inside
// function bodies are locals — invisible at link time, invisible here —
// while the file-scope variable AFTER a body is still seen (the span
// continues past a closing brace).
func TestExtractCExternalVars_BodySkipping(t *testing.T) {
	src := `
int outer_before;
void run(void) {
	int local_inside = 1;
	(void)local_inside;
}
int outer_after;
`
	stripped, _ := preprocessC(src)
	got := extractCExternalVars(stripped)
	want := []string{"outer_before", "outer_after"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body skipping: got %v, want %v", got, want)
	}
}

// TestExtractCExternalVars_LineCommentsDoNotContaminate replays the two
// live-caught failures: (1) a file-header comment whose prose contains
// '(' must not swallow the global that follows the #includes; (2) a
// directive comment ABOVE a typedef must not defeat the typedef filter
// and leak the alias as a fake variable.
//
// Português: Reencena as duas falhas pegas ao vivo: prosa com '(' no
// cabeçalho não pode engolir o global após os includes; comentário de
// diretiva acima de typedef não pode furar o filtro e vazar o alias.
func TestExtractCExternalVars_LineCommentsDoNotContaminate(t *testing.T) {
	src := `// probe_core.c — the device functions (see probe_api.h for details,
// and probe_filter.c for helpers). Prose with (parentheses) and, commas.

#include <stdlib.h>
#include "probe_api.h"

// A directive-style comment right above the declaration.
int g_probe_count = 0;

// label:Sampling mode.
// icon:wave-square.
typedef enum {
	// label:Fast.
	MODE_FAST = 0,
} probe_mode_t;
`
	stripped, _ := preprocessC(src)
	got := extractCExternalVars(stripped)
	want := []string{"g_probe_count"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("comment contamination: got %v, want %v", got, want)
	}
}
