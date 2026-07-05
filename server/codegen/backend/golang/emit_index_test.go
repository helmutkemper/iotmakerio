// server/codegen/backend/golang/emit_index_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package golang

import (
	"testing"

	"server/codegen/ir"
)

// buildIndexProgram assembles a minimal program: a constant array, an index
// constant, and an OpIndex reading the element. okWired controls whether the ok
// output is present.
func buildIndexProgram(okWired bool) *ir.Program {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConstArray, Dest: "arr", Type: "int", Args: []string{"10", "20", "30"}})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "idx", Type: "int", Args: []string{"1"}})
	inst := ir.Instruction{Op: ir.OpIndex, Dest: "val", Type: "int", Args: []string{"%arr", "%idx"}}
	if okWired {
		inst.Meta = map[string]string{"okDest": "ok"}
	}
	prog.Append(inst)
	return prog
}

func TestEmit_Index_WithOk(t *testing.T) {
	// A bounds-checked read with the ok output wired. The guard, the int() match
	// to len(), and Go's native len() must all be present, and the value is
	// declared (so it holds its zero value when out of range).
	out := Emit(buildIndexProgram(true))
	assertContains(t, out, "var val") // value declared → holds its zero when out of range
	assertContains(t, out, ">= 0 &&") // a negative index is out of range
	assertContains(t, out, "int(")    // index matched to len()'s int type
	assertContains(t, out, "len(")    // Go's native length
}

func TestEmit_Index_NoOk_StillGuarded(t *testing.T) {
	// Without the ok output the access is still bounds-checked (the guard is
	// inlined into the if). This proves the unwired path is safe, not raw.
	out := Emit(buildIndexProgram(false))
	assertContains(t, out, "var val")
	assertContains(t, out, ">= 0 &&")
	assertContains(t, out, "len(")
}
