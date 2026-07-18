// server/codegen/backend/golang/emit_function_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The Go side of slice 2: a FUNC region lifts out of main into
// `func <name>()`, and — because the program contains a function — the
// OpVar declaration routes to PACKAGE scope so both sides see it.
// Português: Região vira func nomeada acima do main; OpVar declara em
// escopo de pacote quando há funções.
package golang

import (
	"strings"
	"testing"

	"server/codegen/ir"
)

func TestFunctionLiftAndPackageVar(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpVar, Dest: "shared", Type: "int",
		Args: []string{"0"}})
	prog.Append(ir.Instruction{Op: ir.OpFuncBegin, Dest: "blinker"})
	prog.Append(ir.Instruction{Op: ir.OpAssign, Dest: "shared", Type: "int",
		Args: []string{"1"}})
	prog.Append(ir.Instruction{Op: ir.OpFuncEnd})

	out := Emit(prog)

	must := func(needle string) int {
		i := strings.Index(out, needle)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", needle, out)
		}
		return i
	}
	varIdx := must("var shared int")
	fnIdx := must("func blinker() {")
	asgIdx := must("shared = 1")
	mainIdx := must("func main() {")

	if !(varIdx < fnIdx && fnIdx < asgIdx && asgIdx < mainIdx) {
		t.Fatalf("layout order broken: var=%d func=%d assign=%d main=%d\n%s",
			varIdx, fnIdx, asgIdx, mainIdx, out)
	}
	if strings.Contains(out[mainIdx:], "shared = 1") {
		t.Fatalf("assignment leaked into main:\n%s", out)
	}
}
