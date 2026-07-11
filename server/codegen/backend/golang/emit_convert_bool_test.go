// server/codegen/backend/golang/emit_convert_bool_test.go
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

// TestEmit_Convert_BoolToInt_Go pins the bool→numeric rendering: Go has no
// direct conversion, so the backend must emit the 0/1 temp pattern — and
// the result must ACTUALLY compile (the bug this pins: `int64(flag)`).
//
// Português: Pina a renderização bool→numérico: Go não tem conversão
// direta, então o backend emite o padrão temp 0/1 — e o resultado precisa
// COMPILAR de verdade (o bug que isto pina: `int64(flag)`).
func TestEmit_Convert_BoolToInt_Go(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "flag", Type: "bool", Args: []string{"true"}})
	prog.Append(ir.Instruction{
		Op: ir.OpConvert, Dest: "conv_0", Type: "int", Args: []string{"%flag"},
		Meta: map[string]string{"srcType": "bool"},
	})
	prog.Append(ir.Instruction{Op: ir.OpAdd, Dest: "sum", Type: "int", Args: []string{"%conv_0", "10"}})
	prog.Append(printInst("int", "%sum", "sum", ""))

	out := Emit(prog)
	if strings.Contains(out, "int64(flag)") {
		t.Fatalf("direct cast of a bool survived:\n%s", out)
	}
	if !strings.Contains(out, "if flag {") {
		t.Fatalf("0/1 temp pattern missing:\n%s", out)
	}

	// The decisive check: the generated program must compile.
	// Português: A prova decisiva: o programa gerado precisa compilar.
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "bin"), path)
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GO111MODULE=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go does not compile:\n%s", b)
	}
}

// TestEmit_Convert_NumericKeepsCast pins that non-bool sources keep the
// plain cast rendering.
// Português: Pina que origens não-bool mantêm o cast simples.
func TestEmit_Convert_NumericKeepsCast(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "n", Type: "int", Args: []string{"7"}})
	prog.Append(ir.Instruction{
		Op: ir.OpConvert, Dest: "conv_0", Type: "float", Args: []string{"%n"},
		Meta: map[string]string{"srcType": "int"},
	})
	out := Emit(prog)
	if !strings.Contains(out, "float64(n)") {
		t.Fatalf("plain numeric cast missing:\n%s", out)
	}
}
