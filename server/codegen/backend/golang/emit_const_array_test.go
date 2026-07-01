// /server/codegen/backend/golang/emit_const_array_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package golang

// emit_const_array_test.go — Unit tests for emitConstArray, the OpConstArray
// translator (the StatementConstArray{Int,Float,String} device's collection literal) on the Go
// backend.
//
// Synthetic-IR pattern, the Go counterpart of
// backend/ansic/emit_const_array_test.go: an ir.Program is built by hand
// with a CONST_ARRAY instruction, emitted through Emit, and the exact Go
// text is asserted. Together the two files are the offline proof demanded
// by Task 4 of docs/claude_const_array_plan.md.
//
// Coverage:
//
//   - int elements — the abstract "int" widens to int64 via goTypeName,
//     consistent with every scalar this backend emits (see emitConstArray's
//     Task 6 note about authored []int parameters)
//   - float32 / string elements (the formatter is already parametric;
//     Task 8 only widens the device's select)
//   - the zero-length literal (`[]int64{}` is valid Go, len() == 0)
//   - dest identifier normalisation (`constArray_1` → `constArray1`),
//     matching what consumers reference via goOperand
//
// Português: Testes unitários do emitConstArray no backend Go, com IR
// sintético — o par do arquivo equivalente no backend C. Juntos são a
// prova offline exigida pela Task 4 do plano.

import (
	"strings"
	"testing"

	"server/codegen/ir"
)

// assertContains / assertNotContains mirror the helpers of the same name in
// backend/ansic/emit_test.go (different package — no collision), so the two
// const-array test files read identically.
func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q, got:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q, got:\n%s", needle, haystack)
	}
}

// =====================================================================
//  OpConstArray — element types
// =====================================================================

// TestEmit_ConstArray_Int is the Task 4 acceptance shape on the Go side:
// three int elements. The abstract "int" renders as int64 — the same
// widening goTypeName applies to every scalar const/var in the file.
func TestEmit_ConstArray_Int(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "constArray_1",
		Type: "int",
		Args: []string{"1", "2", "3"},
	})

	out := Emit(prog)

	assertContains(t, out, "constArray1 := []int64{1, 2, 3}")
	assertNotContains(t, out, "[]int{") // the abstract int never renders bare
}

// TestEmit_ConstArray_Float32: a concrete element type passes through
// goTypeName verbatim, and the IR's plain-decimal elements are joined as-is.
func TestEmit_ConstArray_Float32(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "f_1",
		Type: "float32",
		Args: []string{"0.5", "1.5"},
	})

	out := Emit(prog)

	assertContains(t, out, "f1 := []float32{0.5, 1.5}")
}

// TestEmit_ConstArray_String: elements arrive pre-quoted from the IR
// (formatArrayElement's strconv.Quote — the emitConstString contract).
func TestEmit_ConstArray_String(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "s_1",
		Type: "string",
		Args: []string{`"alpha"`, `"beta"`},
	})

	out := Emit(prog)

	assertContains(t, out, `s1 := []string{"alpha", "beta"}`)
}

// =====================================================================
//  Zero-length literal
// =====================================================================

// TestEmit_ConstArray_Empty: unlike C (see the ansic counterpart's
// zero-length stance), an empty Go slice literal is simply valid — len()
// is 0 and no dummy slot is needed. The IR has already attached the
// authoring warning.
func TestEmit_ConstArray_Empty(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "e_1",
		Type: "int",
		Args: []string{},
	})

	out := Emit(prog)

	assertContains(t, out, "e1 := []int64{}")
}
