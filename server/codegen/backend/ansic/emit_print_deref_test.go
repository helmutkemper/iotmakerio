// server/codegen/backend/ansic/emit_print_deref_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/ir"
)

// TestEmit_Print_Deref_C pins the C99 pointer-wire print: NULL guard
// printing "null pointer" (prefix-aware), deref otherwise — compiled with
// the real gcc when available.
//
// Português: Pina o print C99 de fio ponteiro: guarda de NULL imprimindo
// "null pointer" (com prefixo), deref caso contrário — compilado com gcc
// real quando disponível.
func TestEmit_Print_Deref_C(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "n", Type: "int", Args: []string{"7"}})
	prog.Append(ir.Instruction{
		Op: ir.OpPrint, Dest: "print1", Type: "int", Args: []string{"%p"},
		Meta: map[string]string{"prefix": "val", "format": "", "deref": "1"},
	})

	files := Emit(prog, ProfilePortable, blackbox.Naming{})
	mainC := files["main.c"]

	if !strings.Contains(mainC, "null pointer") {
		t.Fatalf("null guard text missing:\n%s", mainC)
	}
	if !strings.Contains(mainC, "== NULL") {
		t.Fatalf("NULL guard missing:\n%s", mainC)
	}
	if !strings.Contains(mainC, "(*p)") {
		t.Fatalf("deref missing:\n%s", mainC)
	}

	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("gcc not available — string assertions only")
	}
	// Inject the pointer declaration before the guard, mirroring what a
	// BB pointer output will provide in real scenes.
	// Português: Injeta a declaração do ponteiro antes da guarda,
	// espelhando o que um output ponteiro de BB fornecerá em cenas reais.
	src := strings.Replace(mainC, "if (p == NULL) {",
		"int32_t *p = &n;\n    if (p == NULL) {", 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "main.c")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(gcc, "-std=c99", "-Wall", "-Werror",
		"-o", filepath.Join(dir, "bin"), path).CombinedOutput(); err != nil {
		t.Fatalf("generated C does not compile:\n%s\n---\n%s", b, src)
	}
}
