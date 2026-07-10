// server/codegen/backend/ansic/emit_comment_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

import (
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/ir"
)

// TestEmit_Comment_C pins the C99 rendering of a stamped device comment:
// `// ` lines (valid C99) immediately above the node's statement.
//
// Português: Pina a renderização C99 de um comentário carimbado: linhas
// `// ` (C99 válido) imediatamente acima do statement do node.
func TestEmit_Comment_C(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpConst, Dest: "c1", Type: "int", Args: []string{"5"},
		Meta: map[string]string{"comment": "hello\nworld"},
	})

	files := Emit(prog, ProfilePortable, blackbox.Naming{})
	mainC := files["main.c"]

	hi := strings.Index(mainC, "// hello\n")
	wi := strings.Index(mainC, "// world\n")
	ci := strings.Index(mainC, "c1")
	if hi == -1 || wi == -1 {
		t.Fatalf("comment lines missing:\n%s", mainC)
	}
	if !(hi < wi && wi < ci) {
		t.Fatalf("comment lines out of order (hello=%d world=%d const=%d):\n%s", hi, wi, ci, mainC)
	}
}
