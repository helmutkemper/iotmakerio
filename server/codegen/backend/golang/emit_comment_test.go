// server/codegen/backend/golang/emit_comment_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package golang

import (
	"strings"
	"testing"

	"server/codegen/ir"
)

// TestEmit_Comment_Go pins the Go rendering of a stamped device comment:
// each comment line becomes a `// ` line immediately above the node's
// statement, trailing whitespace stripped.
//
// Português: Pina a renderização Go de um comentário carimbado: cada linha
// vira uma linha `// ` imediatamente acima do statement do node, espaços à
// direita removidos.
func TestEmit_Comment_Go(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpConst, Dest: "c1", Type: "int", Args: []string{"5"},
		Meta: map[string]string{"comment": "hello\nworld  "},
	})

	out := Emit(prog)

	hi := strings.Index(out, "// hello\n")
	wi := strings.Index(out, "// world\n")
	ci := strings.Index(out, "c1")
	if hi == -1 || wi == -1 {
		t.Fatalf("comment lines missing:\n%s", out)
	}
	if !(hi < wi && wi < ci) {
		t.Fatalf("comment lines out of order (hello=%d world=%d const=%d):\n%s", hi, wi, ci, out)
	}
	if strings.Contains(out, "world  ") {
		t.Fatalf("trailing whitespace survived:\n%s", out)
	}
}

// TestEmit_NoComment_Go pins that instructions without the stamp emit no
// stray comment lines.
//
// Português: Pina que instruções sem carimbo não emitem linhas de
// comentário perdidas.
func TestEmit_NoComment_Go(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "c1", Type: "int", Args: []string{"5"}})

	out := Emit(prog)
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") && strings.Contains(trimmed, "c1") {
			t.Fatalf("unexpected comment line: %q", line)
		}
	}
}
