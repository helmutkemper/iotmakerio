// /server/codegen/backend/ansic/emit_const_array_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

// emit_const_array_test.go — Unit tests for emitConstArray, the OpConstArray
// translator (the StatementConstArray{Int,Float,String} device's collection literal).
//
// Same synthetic-IR pattern as emit_test.go: an ir.Program is built by hand
// with a single CONST_ARRAY instruction, emitted through emitMain, and the
// exact C text is asserted. This is the offline proof demanded by Task 4 of
// docs/claude_const_array_plan.md — the whole collection codegen path is
// proven from a raw IR instruction before any WASM device exists.
//
// Coverage:
//
//   - int elements under two profiles (type AND per-element suffix flip:
//     int32_t/L on arduino_uno, int64_t/LL on pi_linux)
//   - the `const size_t <dest>_len = N;` companion (plan decision 3 —
//     survives pointer decay at call sites)
//   - the gated `#include <stddef.h>` (present with an array, absent
//     without one)
//   - float32 elements (decimal-point normalisation + unconditional `f`)
//   - string elements (pre-quoted by the IR, `const char*` element type)
//   - the ZERO-LENGTH stance: `{}` is not valid C99, so an empty
//     collection emits a one-slot zeroed array with `_len = 0`
//   - dest identifier normalisation (`constArray_1` → `constArray1`),
//     matching what consumers reference via cOperand
//
// Português: Testes unitários do emitConstArray com IR sintético, no mesmo
// padrão do emit_test.go. Prova offline exigida pela Task 4 do plano —
// o caminho inteiro do codegen de coleção é provado a partir de uma
// instrução IR crua, antes de existir qualquer device WASM.

import (
	"testing"

	"server/codegen/ir"
)

// =====================================================================
//  OpConstArray — int elements, profile behaviour
// =====================================================================

// TestEmit_ConstArray_IntArduinoUno is the Task 4 acceptance shape: three
// int elements under the default profile. Asserts the fixed-array
// declaration (profile type + per-element suffix), the size_t length
// companion, and the gated stddef include.
func TestEmit_ConstArray_IntArduinoUno(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "constArray_1",
		Type: "int",
		Args: []string{"1", "2", "3"},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "int32_t constArray1[] = {1L, 2L, 3L};")
	assertContains(t, main, "const size_t constArray1_len = 3;")
	assertContains(t, main, "#include <stddef.h>")
	assertNotContains(t, main, "int64_t") // wrong width for arduino_uno
	assertNotContains(t, main, "malloc")  // fixed-size literal — never heap
}

// TestEmit_ConstArray_IntPiLinux confirms that switching profiles flips
// both the element type and every per-element literal suffix.
func TestEmit_ConstArray_IntPiLinux(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "constArray_1",
		Type: "int",
		Args: []string{"1", "2", "3"},
	})

	main := emitMain(prog, ProfilePiLinux)

	assertContains(t, main, "int64_t constArray1[] = {1LL, 2LL, 3LL};")
	assertContains(t, main, "const size_t constArray1_len = 3;")
	assertNotContains(t, main, "int32_t")
}

// =====================================================================
//  Gated <stddef.h>
// =====================================================================

// TestEmit_ConstArray_StddefGated proves the honest-artefact stance: a
// program WITHOUT any collection must not drag in <stddef.h>.
func TestEmit_ConstArray_StddefGated(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "x",
		Type: "int",
		Args: []string{"42"},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertNotContains(t, main, "stddef.h")
}

// =====================================================================
//  Element types beyond int (the formatter is already parametric —
//  Task 8 only widens the device's select)
// =====================================================================

// TestEmit_ConstArray_Float32 exercises the concrete single-precision
// path: decimal-point normalisation plus the unconditional `f` suffix on
// EACH element, byte-identical to the same value emitted as a scalar.
func TestEmit_ConstArray_Float32(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "f_1",
		Type: "float32",
		Args: []string{"0.5", "1.5", "3"},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "float f1[] = {0.5f, 1.5f, 3.0f};")
	assertContains(t, main, "const size_t f1_len = 3;")
}

// TestEmit_ConstArray_String: the IR delivers elements pre-quoted
// (emitConstArray/formatArrayElement contract), and cTypeName maps the
// element type to `const char*` — so each slot is a C string literal.
func TestEmit_ConstArray_String(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "s_1",
		Type: "string",
		Args: []string{`"alpha"`, `"beta"`},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, `const char* s1[] = {"alpha", "beta"};`)
	assertContains(t, main, "const size_t s1_len = 2;")
}

// =====================================================================
//  Zero-length stance
// =====================================================================

// TestEmit_ConstArray_Empty locks the documented zero-length stance: `{}`
// is not valid C99 (§6.7.8 requires at least one initializer), so an empty
// collection becomes a one-slot zeroed array whose `_len` carries the real
// logical size (0). The artefact always compiles; consumers iterating
// `_len` elements never touch the dummy slot.
func TestEmit_ConstArray_Empty(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConstArray,
		Dest: "e_1",
		Type: "int",
		Args: []string{},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "int32_t e1[1] = {0};")
	assertContains(t, main, "const size_t e1_len = 0;")
	assertNotContains(t, main, "= {};") // the invalid-C99 form must never appear
}
