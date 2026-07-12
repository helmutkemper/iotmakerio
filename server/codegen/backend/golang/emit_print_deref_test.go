// server/codegen/backend/golang/emit_print_deref_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package golang

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"server/codegen/ir"
)

// TestEmit_Print_Deref_Go pins the pointer-wire print: Meta["deref"] makes
// the backend read through the pointer with a nil guard that prints
// "null pointer" (prefix-aware) — and the generated program must COMPILE.
//
// Português: Pina o print de fio ponteiro: Meta["deref"] faz o backend ler
// através do ponteiro com guarda de nil imprimindo "null pointer" (com
// prefixo) — e o programa gerado precisa COMPILAR.
func TestEmit_Print_Deref_Go(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "n", Type: "int", Args: []string{"7"}})
	prog.Append(ir.Instruction{
		Op: ir.OpPrint, Dest: "print1", Type: "int", Args: []string{"%p"},
		Meta: map[string]string{"prefix": "val", "format": "", "deref": "1"},
	})

	out := Emit(prog)
	// The IR has no pointer-producing opcode (BB outputs will be the real
	// source); the compile proof injects the pointer declaration right
	// before the guard the backend emitted.
	// Português: O IR não tem opcode que produza ponteiro (os outputs de
	// BB serão a fonte real); a prova de compilação injeta a declaração
	// do ponteiro logo antes da guarda que o backend emitiu.
	out = strings.Replace(out, "if p != nil {", "p := &n\n\tif p != nil {", 1)
	if !strings.Contains(out, "null pointer") {
		t.Fatalf("null guard text missing:\n%s", out)
	}
	if !strings.Contains(out, "!= nil") {
		t.Fatalf("nil guard missing:\n%s", out)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "bin"), path)
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GO111MODULE=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go does not compile:\n%s\n---\n%s", b, out)
	}
}
