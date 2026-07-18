// server/codegen/backend/ansic/emit_function_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The C side of slice 2: the FUNC region lifts into
// `static void <name>(void)` above main, and OpVar routes to a file-scope
// `static` declaration. Português: Região vira static void acima do main;
// OpVar vira static de arquivo.
package ansic

import (
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/ir"
)

func TestFunctionLiftAndStaticVar(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpVar, Dest: "shared", Type: "int",
		Args: []string{"0"}})
	prog.Append(ir.Instruction{Op: ir.OpFuncBegin, Dest: "blinker"})
	prog.Append(ir.Instruction{Op: ir.OpAssign, Dest: "shared", Type: "int",
		Args: []string{"1"}})
	prog.Append(ir.Instruction{Op: ir.OpFuncEnd})

	files := Emit(prog, ProfilePortable, blackbox.Naming{})
	out := files["main.c"]

	must := func(needle string) int {
		i := strings.Index(out, needle)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", needle, out)
		}
		return i
	}
	varIdx := must("static ")
	_ = must("shared;")
	fnIdx := must("static void blinker(void) {")
	asgIdx := must("shared = 1L;")
	mainIdx := must("int main(void) {")

	if !(varIdx < fnIdx && fnIdx < asgIdx && asgIdx < mainIdx) {
		t.Fatalf("layout order broken: var=%d func=%d assign=%d main=%d\n%s",
			varIdx, fnIdx, asgIdx, mainIdx, out)
	}
	if strings.Contains(out[mainIdx:], "shared = 1L;") {
		t.Fatalf("assignment leaked into main:\n%s", out)
	}
}
