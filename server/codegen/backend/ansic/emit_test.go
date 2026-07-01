// /server/codegen/backend/ansic/emit_test.go

package ansic

// emit_test.go — Unit tests for the C99 emitter at the instruction level.
//
// These tests drive the emitter directly with synthetic ir.Program
// values rather than going through the full Generate pipeline. The
// purpose is to exercise one or two opcodes at a time in isolation,
// asserting the exact C text emitted. This complements the
// pipeline-level tests in codeGen_c_test.go which cover the routing
// and end-to-end behaviour.
//
// Synthetic IR pattern:
//
//   prog := &ir.Program{}
//   prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "x", Type: "int", Args: []string{"42"}})
//   files := Emit(prog, ProfileArduinoUno)
//   assertContains(t, files["main.c"], "int32_t x = 42L;")
//
// This pattern is the closest the C backend has to a unit test —
// each opcode emitter (emitConst, emitVar, emitAssign, and later
// emitBinOp etc.) can be exercised in isolation with full control
// over the input.
//
// Português:
//
//	Testes unitários do emitter no nível das instruções. Usam
//	ir.Program montados à mão pra exercitar um opcode de cada vez,
//	com asserts em substrings do main.c gerado. Complementa os
//	testes do pipeline em codeGen_c_test.go.

import (
	"strings"
	"testing"

	"server/codegen/ir"
)

// =====================================================================
//  OpConst
// =====================================================================

// TestEmit_Const_IntArduinoUno is the smoke test: a single int
// constant emitted under the default profile. Asserts the declaration
// form, the C type, the literal suffix, and the absence of the Go-
// style cast syntax.
func TestEmit_Const_IntArduinoUno(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "x",
		Type: "int",
		Args: []string{"42"},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "int32_t x = 42L;")
	assertNotContains(t, main, "int64_t")  // wrong width for arduino_uno
	assertNotContains(t, main, "int32_t(") // Go-style cast must not leak
}

// TestEmit_Const_IntPiLinux confirms that switching profiles flips
// both the type and the literal suffix.
func TestEmit_Const_IntPiLinux(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "x",
		Type: "int",
		Args: []string{"42"},
	})

	main := emitMain(prog, ProfilePiLinux)

	assertContains(t, main, "int64_t x = 42LL;")
	assertNotContains(t, main, "int32_t")
}

// TestEmit_Const_FloatPickUpSuffix exercises the float branch of
// cLiteral — specifically the "f" suffix that keeps the literal as
// float on AVR rather than letting it promote to double.
func TestEmit_Const_FloatPickUpSuffix(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "pi",
		Type: "float",
		Args: []string{"3.14"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "float pi = 3.14f;")

	mainPi := emitMain(prog, ProfilePiLinux)
	assertContains(t, mainPi, "double pi = 3.14;") // empty FloatSuffix
}

// TestEmit_Const_FloatBareInteger covers the "3" → "3.0f" gotcha.
// Without the ".0" insertion the C lexer would refuse "3f" outright.
func TestEmit_Const_FloatBareInteger(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "n",
		Type: "float",
		Args: []string{"3"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "float n = 3.0f;")
}

// TestEmit_Const_Bool covers the bool branch — both true/false are
// valid C99 literals via <stdbool.h>.
func TestEmit_Const_Bool(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "flag",
		Type: "bool",
		Args: []string{"true"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "bool flag = true;")
}

// TestEmit_Const_NegativeInteger — the minus sign is part of the
// emitted literal and survives suffixing.
func TestEmit_Const_NegativeInteger(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "n",
		Type: "int",
		Args: []string{"-42"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "int32_t n = -42L;")
}

// TestEmit_Const_NameNormalisation makes sure the device-ID style
// names produced by the IoTMaker IDE (with underscore-then-digit
// suffix) are normalised the same way the Go backend normalises them.
func TestEmit_Const_NameNormalisation(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "constInt_1",
		Type: "int",
		Args: []string{"10"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "int32_t constInt1 = 10L;")
	assertNotContains(t, main, "constInt_1") // underscore stripped
}

// =====================================================================
//  OpVar
// =====================================================================

// TestEmit_Var_IntArduinoUno emits an uninitialised declaration —
// the form used when the IR promotes a register out of a loop body
// to outer scope.
func TestEmit_Var_IntArduinoUno(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpVar,
		Dest: "y",
		Type: "int",
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "int32_t y;")
	assertNotContains(t, main, "int32_t y =") // no initialiser
}

// TestEmit_Var_FloatPiLinux confirms the profile shapes both type
// and the absence of suffix-related decisions (Var has no value to
// suffix).
func TestEmit_Var_FloatPiLinux(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpVar,
		Dest: "z",
		Type: "float",
	})

	main := emitMain(prog, ProfilePiLinux)
	assertContains(t, main, "double z;")
}

// =====================================================================
//  OpAssign
// =====================================================================

// TestEmit_Assign_AfterVar exercises the canonical pairing: a Var
// declaration followed by an Assign that fills it. The Assign must
// NOT redeclare the type.
func TestEmit_Assign_AfterVar(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpVar,
		Dest: "y",
		Type: "int",
	})
	prog.Append(ir.Instruction{
		Op:   ir.OpAssign,
		Dest: "y",
		Type: "int",
		Args: []string{"42"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "int32_t y;")
	assertContains(t, main, "y = 42L;")

	// Critical: the second line MUST NOT be "int32_t y = 42L;" —
	// that would be a duplicate declaration in C.
	if strings.Count(main, "int32_t y") != 1 {
		t.Errorf("expected exactly one 'int32_t y' declaration in main.c, got:\n%s", main)
	}
}

// =====================================================================
//  Arithmetic (OpAdd / OpSub / OpMul / OpDiv)
// =====================================================================

// TestEmit_BinOp_AddIntFirstUse exercises the canonical case: two
// constants followed by an addition. The Add is the first occurrence
// of its dest, so the type prefix must appear.
func TestEmit_BinOp_AddIntFirstUse(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "a", Type: "int", Args: []string{"10"}})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "b", Type: "int", Args: []string{"20"}})
	prog.Append(ir.Instruction{Op: ir.OpAdd, Dest: "sum", Type: "int", Args: []string{"%a", "%b"}})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "int32_t a = 10L;")
	assertContains(t, main, "int32_t b = 20L;")
	assertContains(t, main, "int32_t sum = a + b;")
}

// TestEmit_BinOp_AllFourOperators sweeps the four arithmetic opcodes
// and asserts the correct C operator appears for each. The test does
// not chain the ops; each one is exercised on a fresh program so
// failures pinpoint the broken mapping.
func TestEmit_BinOp_AllFourOperators(t *testing.T) {
	cases := []struct {
		op       ir.Op
		operator string // expected C operator
	}{
		{ir.OpAdd, "+"},
		{ir.OpSub, "-"},
		{ir.OpMul, "*"},
		{ir.OpDiv, "/"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.op), func(t *testing.T) {
			prog := &ir.Program{}
			prog.Append(ir.Instruction{
				Op:   tc.op,
				Dest: "result",
				Type: "int",
				Args: []string{"%a", "%b"},
			})

			main := emitMain(prog, ProfileArduinoUno)
			expected := "int32_t result = a " + tc.operator + " b;"
			assertContains(t, main, expected)
		})
	}
}

// TestEmit_BinOp_ReuseDoesNotRedeclare is the C-specific guard: when
// the dest of a binary op has already been declared (typically by a
// preceding OpVar that promoted it to outer scope), the emitter MUST
// produce a plain assignment, NOT a duplicate type declaration.
//
// In Go this would only trigger a compiler warning about "no new
// variables on left side of :=", and goEmitter handles it via the
// same declared map. In C, redeclaring a variable is a hard error
// ("error: redefinition of 'sum'"). This test pins the behaviour
// firmly.
func TestEmit_BinOp_ReuseDoesNotRedeclare(t *testing.T) {
	prog := &ir.Program{}

	// Declare "sum" as an uninitialised variable — the typical
	// shape the IR emitter produces for a value that crosses a
	// scope boundary.
	prog.Append(ir.Instruction{Op: ir.OpVar, Dest: "sum", Type: "int"})

	// Now compute into it.
	prog.Append(ir.Instruction{Op: ir.OpAdd, Dest: "sum", Type: "int", Args: []string{"%a", "%b"}})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "int32_t sum;") // from OpVar
	assertContains(t, main, "sum = a + b;") // from OpAdd (no type!)
	assertNotContains(t, main, "int32_t sum = a + b;")

	// Belt and suspenders: count occurrences of the declaration.
	// More than one is a regression.
	if strings.Count(main, "int32_t sum") != 1 {
		t.Errorf("expected exactly one 'int32_t sum' in output, got:\n%s", main)
	}
}

// TestEmit_BinOp_FloatProfilePropagates verifies that arithmetic on
// floats picks up the FloatType from the profile, and that the
// operand references stay clean (no suffix leakage from cLiteral —
// register refs go through cOperand, not cLiteral).
func TestEmit_BinOp_FloatProfilePropagates(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpMul, Dest: "area", Type: "float", Args: []string{"%w", "%h"}})

	mainAr := emitMain(prog, ProfileArduinoUno)
	assertContains(t, mainAr, "float area = w * h;")

	mainPi := emitMain(prog, ProfilePiLinux)
	assertContains(t, mainPi, "double area = w * h;")
}

// TestEmit_BinOp_OperandNormalisation makes sure the canvas-style
// identifiers ("constInt_1", "add_2") flow cleanly through cOperand
// — the underscore-before-digit stripping must apply to operand refs
// just as it applies to dest names.
func TestEmit_BinOp_OperandNormalisation(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpAdd,
		Dest: "add_1",
		Type: "int",
		Args: []string{"%constInt_1", "%constInt_3"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "int32_t add1 = constInt1 + constInt3;")

	// Stripped form only — raw underscore-digit must not leak.
	assertNotContains(t, main, "constInt_1")
	assertNotContains(t, main, "constInt_3")
	assertNotContains(t, main, "add_1")
}

// =====================================================================
//  Conversions (OpConvert)
// =====================================================================

// TestEmit_Convert_IntToFloatArduinoUno is the canonical isolated
// case: a single OpConvert that widens an int to a float on the
// default profile.
func TestEmit_Convert_IntToFloatArduinoUno(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConvert,
		Dest: "a_conv",
		Type: "float",
		Args: []string{"%a"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "float a_conv = (float)a;")
	assertNotContains(t, main, "float(a)") // not C++ syntax
}

// TestEmit_Convert_FloatToInt is the opposite direction — narrowing
// a float to an int. The C compiler will truncate; the IR emitter's
// type-compat pass attaches a "lossy" warning to the program when
// this happens but does not block emission.
func TestEmit_Convert_FloatToInt(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConvert,
		Dest: "x_trunc",
		Type: "int",
		Args: []string{"%x"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "int32_t x_trunc = (int32_t)x;")
}

// TestEmit_Convert_RespectsProfile asserts the target type flows
// from the profile, just like every other emitter that takes a
// type. Same OpConvert under different profiles produces different
// concrete C types.
func TestEmit_Convert_RespectsProfile(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConvert,
		Dest: "y_widened",
		Type: "float",
		Args: []string{"%y"},
	})

	mainAr := emitMain(prog, ProfileArduinoUno)
	assertContains(t, mainAr, "float y_widened = (float)y;")

	mainPi := emitMain(prog, ProfilePiLinux)
	assertContains(t, mainPi, "double y_widened = (double)y;")
}

// TestEmit_Convert_OperandNormalisation makes sure the underscore-
// before-digit stripping applies to both source and destination
// identifiers, mirroring cIdent/cOperand behaviour throughout the
// backend. Note that "_conv" survives the normalisation because
// cIdent only strips underscores that precede digits — the 'c' in
// "_conv" is a letter, so the underscore is kept.
func TestEmit_Convert_OperandNormalisation(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConvert,
		Dest: "constInt_1_conv",
		Type: "float",
		Args: []string{"%constInt_1"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	// "constInt_1_conv" → "constInt1_conv": only the '_' before '1'
	// is stripped; the '_' before 'conv' is kept (letter, not digit).
	assertContains(t, main, "float constInt1_conv = (float)constInt1;")
}

// TestEmit_Convert_ChainedWithArithmetic reproduces the realistic
// pattern: a Const(int), a Const(float), a Convert that widens the
// int, then an Add that consumes the converted register together
// with the original float. This is exactly what the IR emitter
// produces for a scene that mixes int and float on the same Add
// device. The test asserts the full sequence appears in order.
func TestEmit_Convert_ChainedWithArithmetic(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "a", Type: "int", Args: []string{"10"}})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "b", Type: "float", Args: []string{"3.14"}})
	prog.Append(ir.Instruction{Op: ir.OpConvert, Dest: "a_conv", Type: "float", Args: []string{"%a"}})
	prog.Append(ir.Instruction{Op: ir.OpAdd, Dest: "sum", Type: "float", Args: []string{"%a_conv", "%b"}})

	main := emitMain(prog, ProfileArduinoUno)

	// All four lines must appear, in this order.
	assertContains(t, main, "int32_t a = 10L;")
	assertContains(t, main, "float b = 3.14f;")
	assertContains(t, main, "float a_conv = (float)a;")
	assertContains(t, main, "float sum = a_conv + b;")

	// Order check: each line must come AFTER its predecessor in
	// the output. A reversed Convert/Add ordering would mean the
	// Add references a_conv before it has been declared, which
	// the C compiler would reject — better catch the order error
	// in our test than in someone's toolchain.
	idxA := strings.Index(main, "int32_t a = 10L;")
	idxB := strings.Index(main, "float b = 3.14f;")
	idxConv := strings.Index(main, "float a_conv = (float)a;")
	idxSum := strings.Index(main, "float sum = a_conv + b;")

	if !(idxA < idxB && idxB < idxConv && idxConv < idxSum) {
		t.Errorf("instructions appear out of order in main.c:\n%s", main)
	}
}

// =====================================================================
//  Comparisons (OpCmpEQ / OpCmpNE / OpCmpLT / OpCmpGT / OpCmpLE / OpCmpGE)
// =====================================================================

// TestEmit_Compare_AllSixOperators sweeps every comparison opcode and
// asserts the right C operator and bool result type appear. Each
// case runs on a fresh program so failures pinpoint the broken
// mapping without cross-contamination.
func TestEmit_Compare_AllSixOperators(t *testing.T) {
	cases := []struct {
		op       ir.Op
		operator string // expected C operator
	}{
		{ir.OpCmpEQ, "=="},
		{ir.OpCmpNE, "!="},
		{ir.OpCmpLT, "<"},
		{ir.OpCmpGT, ">"},
		{ir.OpCmpLE, "<="},
		{ir.OpCmpGE, ">="},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.op), func(t *testing.T) {
			prog := &ir.Program{}
			prog.Append(ir.Instruction{
				Op:   tc.op,
				Dest: "result",
				Type: "bool",
				Args: []string{"%a", "%b"},
			})

			main := emitMain(prog, ProfileArduinoUno)
			expected := "bool result = a " + tc.operator + " b;"
			assertContains(t, main, expected)
		})
	}
}

// TestEmit_Compare_ResultTypeFromProfile pins the contract that the
// result type comes from cTypeName("bool", profile), which is "bool"
// in every shipped profile via <stdbool.h>. A future C89-only profile
// would have BoolType="int" — and this test would force that change
// to be deliberate rather than accidental.
func TestEmit_Compare_ResultTypeFromProfile(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpCmpGT,
		Dest: "isHot",
		Type: "bool",
		Args: []string{"%temp", "%threshold"},
	})

	// Every shipped profile must emit "bool" — see profile.go's
	// TestProfile_BoolIsAlwaysBoolForC99 in profile_test.go.
	for _, p := range ListProfiles() {
		main := emitMain(prog, p)
		expected := "bool isHot = temp > threshold;"
		if !strings.Contains(main, expected) {
			t.Errorf("profile %s: missing %q in:\n%s", p.Name, expected, main)
		}
	}
}

// TestEmit_Compare_ReuseDoesNotRedeclare is the C-specific guard
// against duplicate type declarations on second use of the same
// dest. Same shape as the equivalent test for emitBinOp.
func TestEmit_Compare_ReuseDoesNotRedeclare(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpVar, Dest: "flag", Type: "bool"})
	prog.Append(ir.Instruction{
		Op:   ir.OpCmpEQ,
		Dest: "flag",
		Type: "bool",
		Args: []string{"%a", "%b"},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "bool flag;")     // from OpVar
	assertContains(t, main, "flag = a == b;") // from OpCmpEQ (no type!)
	assertNotContains(t, main, "bool flag = a == b;")

	if strings.Count(main, "bool flag") != 1 {
		t.Errorf("expected exactly one 'bool flag' declaration, got:\n%s", main)
	}
}

// TestEmit_Compare_OperandNormalisation confirms cOperand applies to
// both sides of the comparison — the same canvas-style ID normalising
// that arithmetic uses.
func TestEmit_Compare_OperandNormalisation(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpCmpGT,
		Dest: "compare_1",
		Type: "bool",
		Args: []string{"%add_1", "%constInt_5"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "bool compare1 = add1 > constInt5;")

	// Raw underscore-digit forms must not appear anywhere.
	assertNotContains(t, main, "compare_1")
	assertNotContains(t, main, "add_1")
	assertNotContains(t, main, "constInt_5")
}

// TestEmit_Compare_ChainedAfterArithmetic — the realistic shape: an
// Add followed by a comparison of the result against a constant.
// This is exactly what sceneLoop's body produces, minus the loop
// scaffolding (which Task 9 handles). The test asserts both lines
// appear in the right order.
func TestEmit_Compare_ChainedAfterArithmetic(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpAdd, Dest: "sum", Type: "int",
		Args: []string{"%a", "%b"},
	})
	prog.Append(ir.Instruction{
		Op: ir.OpCmpGT, Dest: "overLimit", Type: "bool",
		Args: []string{"%sum", "%limit"},
	})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "int32_t sum = a + b;")
	assertContains(t, main, "bool overLimit = sum > limit;")

	idxAdd := strings.Index(main, "int32_t sum = a + b;")
	idxCmp := strings.Index(main, "bool overLimit = sum > limit;")
	if idxAdd >= idxCmp {
		t.Errorf("comparison should come AFTER its operand's arithmetic:\n%s", main)
	}
}

// =====================================================================
//  Loop control flow (OpLoopBegin / OpBreakIf / OpLoopEnd)
// =====================================================================

// TestEmit_Loop_BeginAndEndIndent confirms the indent dance: the
// LoopBegin bumps indent, LoopEnd pops it. With an empty body the
// generated C contains "while (1) {" and "}" at consistent
// indentation levels.
func TestEmit_Loop_BeginAndEndIndent(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpLoopBegin, Dest: "loop_1"})
	prog.Append(ir.Instruction{Op: ir.OpLoopEnd, Dest: "loop_1"})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "    while (1) {")
	assertContains(t, main, "    }")

	// The opening and closing braces must appear with the same
	// indent (main-body level == 4 spaces).
	openIdx := strings.Index(main, "    while (1) {")
	closeIdx := strings.Index(main, "    }")
	if openIdx < 0 || closeIdx < 0 {
		t.Fatalf("expected both while and closing brace; got:\n%s", main)
	}
	if openIdx >= closeIdx {
		t.Errorf("while must come before its closing brace; got openIdx=%d closeIdx=%d", openIdx, closeIdx)
	}
}

// TestEmit_Loop_BodyIsIndented exercises the indent increment that
// LoopBegin applies. An OpConst inside the loop must sit at indent
// level 2 (8 spaces) — one for the main body, one for inside the
// while.
func TestEmit_Loop_BodyIsIndented(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpLoopBegin, Dest: "loop_1"})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "x", Type: "int", Args: []string{"10"}})
	prog.Append(ir.Instruction{Op: ir.OpLoopEnd, Dest: "loop_1"})

	main := emitMain(prog, ProfileArduinoUno)
	// Inside the while, the Const sits at 8 spaces. Anchoring with
	// '\n' guards against false positives — without the anchor,
	// the 4-space form would be a prefix substring of the 8-space
	// form and assertNotContains below would always fire.
	assertContains(t, main, "\n        int32_t x = 10L;")
	// And NOT at 4 spaces (which would be outside the loop).
	assertNotContains(t, main, "\n    int32_t x = 10L;")
}

// TestEmit_BreakIf_Shape pins the three-line form of the conditional
// break. The "if" sits at the loop-body indent; "break;" sits one
// level deeper; the closing brace returns to the loop-body level.
func TestEmit_BreakIf_Shape(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpLoopBegin, Dest: "loop_1"})
	prog.Append(ir.Instruction{Op: ir.OpBreakIf, Args: []string{"%done"}})
	prog.Append(ir.Instruction{Op: ir.OpLoopEnd, Dest: "loop_1"})

	main := emitMain(prog, ProfileArduinoUno)

	// All three required lines, with the right indents.
	assertContains(t, main, "        if (done) {") // 8 spaces (inside while)
	assertContains(t, main, "            break;")  // 12 spaces (inside if)
	// Closing brace of the if at 8 spaces is harder to assert
	// cleanly because LoopEnd's closing brace also lives at 4
	// spaces — both close braces appear in main. So we assert by
	// looking for the pattern "break;\n        }" instead.
	if !strings.Contains(main, "            break;\n        }") {
		t.Errorf("expected 'break;' followed by '}' at loop-body indent; got:\n%s", main)
	}
}

// TestEmit_BreakIf_ParenthesesAroundCondition is the C-specific
// guard. Go writes "if cond {"; C requires "if (cond) {". A
// regression that copied the Go form would silently produce broken C.
func TestEmit_BreakIf_ParenthesesAroundCondition(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpBreakIf, Args: []string{"%cond"}})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "if (cond) {")  // C form
	assertNotContains(t, main, "if cond {") // Go form must not appear
}

// TestEmit_Loop_NestedIndentation covers loop-in-loop. Each
// LoopBegin bumps indent by one, each LoopEnd pops by one — and the
// outermost LoopEnd returns to the main body's level. A const inside
// the inner loop should sit at 12 spaces (3 levels: main + outer +
// inner).
func TestEmit_Loop_NestedIndentation(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpLoopBegin, Dest: "outer"})
	prog.Append(ir.Instruction{Op: ir.OpLoopBegin, Dest: "inner"})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "x", Type: "int", Args: []string{"1"}})
	prog.Append(ir.Instruction{Op: ir.OpLoopEnd, Dest: "inner"})
	prog.Append(ir.Instruction{Op: ir.OpLoopEnd, Dest: "outer"})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "    while (1) {")             // outer at level 1
	assertContains(t, main, "        while (1) {")         // inner at level 2
	assertContains(t, main, "            int32_t x = 1L;") // const at level 3
}

// TestEmit_Loop_PromotedVarBeforeWhile exercises the canonical IR
// shape for sceneLoop: a register declared with OpVar at outer
// scope, then assigned inside the loop body. The C output must have:
//
//   - int32_t add1;          at level 1 (main body)
//   - while (1) {            at level 1
//   - add1 = ...;            at level 2 (inside loop)
//
// And critically, no "int32_t add1 = ..." inside the loop (which
// would be a duplicate declaration). The declared map from Task 5
// handles this; this test pins the integration with the loop.
func TestEmit_Loop_PromotedVarBeforeWhile(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpVar, Dest: "add_1", Type: "int"})
	prog.Append(ir.Instruction{Op: ir.OpLoopBegin, Dest: "stmLoop_1"})
	prog.Append(ir.Instruction{Op: ir.OpAdd, Dest: "add_1", Type: "int",
		Args: []string{"%a", "%b"}})
	prog.Append(ir.Instruction{Op: ir.OpLoopEnd, Dest: "stmLoop_1"})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "    int32_t add1;")       // declared outside
	assertContains(t, main, "    while (1) {")         // loop header
	assertContains(t, main, "        add1 = a + b;")   // assignment inside, no type
	assertNotContains(t, main, "int32_t add1 = a + b") // must NOT redeclare

	// Order check: the declaration must come BEFORE the while.
	declIdx := strings.Index(main, "    int32_t add1;")
	whileIdx := strings.Index(main, "    while (1) {")
	if declIdx >= whileIdx {
		t.Errorf("OpVar must emit before LoopBegin; got declIdx=%d whileIdx=%d in:\n%s",
			declIdx, whileIdx, main)
	}
}

// =====================================================================
//  If/Else (OpIfBegin / OpIfElse / OpIfEnd) — 4 forms
// =====================================================================

// TestEmit_IfElse_BothBranches is the canonical case: an if with a
// non-empty else. Form: "if (cond) { ... } else { ... }".
func TestEmit_IfElse_BothBranches(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpIfBegin, Args: []string{"%cond"},
		Meta: map[string]string{"hasTrue": "true", "hasFalse": "true"},
	})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "t", Type: "int", Args: []string{"1"}})
	prog.Append(ir.Instruction{Op: ir.OpIfElse})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "f", Type: "int", Args: []string{"0"}})
	prog.Append(ir.Instruction{Op: ir.OpIfEnd})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "if (cond) {")
	assertContains(t, main, "} else {")
	// Both consts must appear, one in each branch.
	assertContains(t, main, "int32_t t = 1L;")
	assertContains(t, main, "int32_t f = 0L;")

	// Order: open before else before close.
	openIdx := strings.Index(main, "if (cond) {")
	elseIdx := strings.Index(main, "} else {")
	tIdx := strings.Index(main, "int32_t t = 1L;")
	fIdx := strings.Index(main, "int32_t f = 0L;")

	if !(openIdx < tIdx && tIdx < elseIdx && elseIdx < fIdx) {
		t.Errorf("instructions out of order in main.c:\n%s", main)
	}
}

// TestEmit_IfElse_OnlyTrueBranch is the no-else case. The IR omits
// the false branch (hasFalse=false). Output form: a single
// "if (cond) { ... }" with no "} else {" separator.
func TestEmit_IfElse_OnlyTrueBranch(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpIfBegin, Args: []string{"%cond"},
		Meta: map[string]string{"hasTrue": "true", "hasFalse": "false"},
	})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "t", Type: "int", Args: []string{"1"}})
	prog.Append(ir.Instruction{Op: ir.OpIfElse})
	// no false branch content
	prog.Append(ir.Instruction{Op: ir.OpIfEnd})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "if (cond) {")
	assertContains(t, main, "int32_t t = 1L;")
	// No else separator must appear.
	assertNotContains(t, main, "} else {")
	// And no negation (this is positive form).
	assertNotContains(t, main, "if (!cond)")
}

// TestEmit_IfElse_OnlyFalseBranch_Negated is the optimisation case:
// when only the false branch has content, the backend inverts the
// condition rather than emitting an empty true block. Output form:
// "if (!cond) { ... }". The IfElse separator is a no-op.
func TestEmit_IfElse_OnlyFalseBranch_Negated(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpIfBegin, Args: []string{"%cond"},
		Meta: map[string]string{"hasTrue": "false", "hasFalse": "true"},
	})
	// no true branch content
	prog.Append(ir.Instruction{Op: ir.OpIfElse})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "f", Type: "int", Args: []string{"0"}})
	prog.Append(ir.Instruction{Op: ir.OpIfEnd})

	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "if (!cond) {")
	assertContains(t, main, "int32_t f = 0L;")
	// No "} else {" — the negated form has no else.
	assertNotContains(t, main, "} else {")
	// And no positive "if (cond)" — it must be negated.
	assertNotContains(t, main, "if (cond) {")
}

// TestEmit_IfElse_BothBranchesEmpty pins the "comment-only" form for
// the theoretical case where the IR emits IF_BEGIN with both
// hasTrue=false and hasFalse=false. In practice the IR's
// scope-resolution pass elides such blocks, but the case is
// preserved for parity with the Go backend. Documented inline.
func TestEmit_IfElse_BothBranchesEmpty(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpIfBegin, Args: []string{"%cond"},
		Meta: map[string]string{"hasTrue": "false", "hasFalse": "false"},
	})
	prog.Append(ir.Instruction{Op: ir.OpIfElse})
	prog.Append(ir.Instruction{Op: ir.OpIfEnd})

	main := emitMain(prog, ProfileArduinoUno)

	// The comment must appear. The C compiler ignores it, so the
	// output remains valid even with the trailing "}" that
	// emitIfEnd unconditionally emits — see emitIfEnd's doc.
	assertContains(t, main, "/* empty if/else on cond */")
	// No actual "if (cond) {" — emitIfBegin's default branch skips
	// the block opener.
	assertNotContains(t, main, "if (cond) {")
	assertNotContains(t, main, "if (!cond) {")
}

// TestEmit_IfElse_ParenthesesAroundCondition is the C-specific
// guard. Just like emitBreakIf, a regression that copied the Go form
// ("if cond {") would silently produce broken C.
func TestEmit_IfElse_ParenthesesAroundCondition(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpIfBegin, Args: []string{"%cond"},
		Meta: map[string]string{"hasTrue": "true", "hasFalse": "false"},
	})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "x", Type: "int", Args: []string{"1"}})
	prog.Append(ir.Instruction{Op: ir.OpIfElse})
	prog.Append(ir.Instruction{Op: ir.OpIfEnd})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "if (cond) {")  // C form
	assertNotContains(t, main, "if cond {") // Go form must not appear
}

// TestEmit_IfElse_Nested exercises ifStack push/pop. An outer if
// with both branches; the true branch contains a nested if with
// only-true-branch. The output must have correctly aligned and
// matched braces at three indent levels.
func TestEmit_IfElse_Nested(t *testing.T) {
	prog := &ir.Program{}

	// outer if
	prog.Append(ir.Instruction{
		Op: ir.OpIfBegin, Args: []string{"%outer"},
		Meta: map[string]string{"hasTrue": "true", "hasFalse": "true"},
	})
	//   inner if (true-only, in outer's true branch)
	prog.Append(ir.Instruction{
		Op: ir.OpIfBegin, Args: []string{"%inner"},
		Meta: map[string]string{"hasTrue": "true", "hasFalse": "false"},
	})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "t", Type: "int", Args: []string{"1"}})
	prog.Append(ir.Instruction{Op: ir.OpIfElse})
	prog.Append(ir.Instruction{Op: ir.OpIfEnd})
	// end inner
	prog.Append(ir.Instruction{Op: ir.OpIfElse})
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "f", Type: "int", Args: []string{"0"}})
	prog.Append(ir.Instruction{Op: ir.OpIfEnd})
	// end outer

	main := emitMain(prog, ProfileArduinoUno)

	// Outer at level 1 (4 spaces); inner at level 2 (8 spaces);
	// const inside inner at level 3 (12 spaces).
	assertContains(t, main, "\n    if (outer) {")
	assertContains(t, main, "\n        if (inner) {")
	assertContains(t, main, "\n            int32_t t = 1L;")
	// The inner is true-only, so no inner "} else {".
	// But the OUTER has both branches → exactly one "} else {".
	if strings.Count(main, "} else {") != 1 {
		t.Errorf("expected exactly one '} else {' (outer's separator); got:\n%s", main)
	}
}

// =====================================================================
//  Sleep (OpSleep) and the runtime files
// =====================================================================

// TestEmit_Sleep_BasicCall is the smoke test for emitSleep: a single
// OpSleep with one register operand produces a call to the runtime
// hook. The argument flows through cOperand the same way the other
// emitters use it.
func TestEmit_Sleep_BasicCall(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpSleep,
		Args: []string{"%constDuration_1"},
	})

	main := emitMain(prog, ProfileArduinoUno)

	// Note the underscore-stripping: "%constDuration_1" → "constDuration1".
	assertContains(t, main, "iotmaker_sleep_ns(constDuration1);")
}

// TestEmit_Sleep_WithConstDuration covers the realistic shape: a
// Const of type time.Duration declared first, then a Sleep that
// consumes it. The duration is int64_t with LL suffix regardless of
// the profile (durations always use 64-bit precision).
func TestEmit_Sleep_WithConstDuration(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op: ir.OpConst, Dest: "constDuration_1", Type: "time.Duration",
		Args: []string{"1000000000"}, // 1 second in ns
	})
	prog.Append(ir.Instruction{
		Op:   ir.OpSleep,
		Args: []string{"%constDuration_1"},
	})

	for _, p := range ListProfiles() {
		p := p
		t.Run(p.Name, func(t *testing.T) {
			main := emitMain(prog, p)

			// time.Duration is unconditionally int64_t with LL
			// suffix, regardless of profile. This is the whole
			// point of treating duration specially in cTypeName
			// and cLiteral.
			assertContains(t, main, "int64_t constDuration1 = 1000000000LL;")
			assertContains(t, main, "iotmaker_sleep_ns(constDuration1);")
		})
	}
}

// TestEmit_Sleep_InsideLoop is the canonical LoopDuration shape: the
// loop body ends with the sleep call before LoopEnd closes the
// while. The Sleep must therefore sit at the loop body's indent
// level (8 spaces).
func TestEmit_Sleep_InsideLoop(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "constDuration_1",
		Type: "time.Duration", Args: []string{"500000000"}})
	prog.Append(ir.Instruction{Op: ir.OpLoopBegin, Dest: "loop_1"})
	prog.Append(ir.Instruction{Op: ir.OpSleep, Args: []string{"%constDuration_1"}})
	prog.Append(ir.Instruction{Op: ir.OpLoopEnd, Dest: "loop_1"})

	main := emitMain(prog, ProfileArduinoUno)

	// At 8 spaces (inside the while).
	assertContains(t, main, "\n        iotmaker_sleep_ns(constDuration1);")
}

// TestEmit_RuntimeFiles_ConditionalOnSleep pins the contract that
// runtime files (iotmaker_runtime.h, iotmaker_runtime_stub.c) only
// ship when the generated body actually calls something from the
// runtime. Today that gating is driven by emitSleep setting
// e.usesRuntime; the moment a different emitter ever uses a
// runtime symbol, it must set the same flag (the field's doc
// comment in cEmitter spells this out).
//
// Two sub-cases:
//
//   - Empty IR (no Sleep) — the project is a single self-contained
//     main.c the maker can build with `gcc main.c`. No header, no
//     stub, no dead dependency.
//
//   - IR with OpSleep — header and stub ride along so the project
//     builds end-to-end on a POSIX host with `gcc *.c`. The maker
//     swaps the stub for a target-specific implementation when
//     deploying to embedded.
//
// Splitting the test into named sub-tests makes a future failure
// point at exactly one case rather than at a generic "Emit returned
// the wrong files" line.
func TestEmit_RuntimeFiles_ConditionalOnSleep(t *testing.T) {
	t.Run("empty IR — main.c alone, no runtime files", func(t *testing.T) {
		files := Emit(&ir.Program{}, ProfileArduinoUno)

		if _, ok := files["main.c"]; !ok {
			t.Error("expected main.c in Files")
		}
		if _, ok := files["iotmaker_runtime.h"]; ok {
			t.Error("did not expect iotmaker_runtime.h for a scene without Sleep")
		}
		if _, ok := files["iotmaker_runtime_stub.c"]; ok {
			t.Error("did not expect iotmaker_runtime_stub.c for a scene without Sleep")
		}
		if len(files) != 1 {
			t.Errorf("expected exactly 1 file (main.c); got %d", len(files))
		}
	})

	t.Run("IR with OpSleep — main.c + header + stub", func(t *testing.T) {
		prog := &ir.Program{}
		prog.Append(ir.Instruction{
			Op:   ir.OpSleep,
			Args: []string{"%constDuration_1"},
		})
		files := Emit(prog, ProfileArduinoUno)

		if _, ok := files["main.c"]; !ok {
			t.Error("expected main.c in Files")
		}
		if _, ok := files["iotmaker_runtime.h"]; !ok {
			t.Error("expected iotmaker_runtime.h in Files when Sleep is used")
		}
		if _, ok := files["iotmaker_runtime_stub.c"]; !ok {
			t.Error("expected iotmaker_runtime_stub.c in Files when Sleep is used")
		}
		if len(files) != 3 {
			t.Errorf("expected exactly 3 files; got %d", len(files))
		}
	})
}

// TestEmit_RuntimeHeader_Contents asserts the runtime header carries
// the expected declarations and guards. These are the contract the
// maker reads when writing a per-target stub, so we pin them. The
// IR carries one OpSleep so the header actually ships — empty IR
// would skip the runtime files entirely under the conditional-
// shipping contract.
func TestEmit_RuntimeHeader_Contents(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpSleep,
		Args: []string{"%constDuration_1"},
	})
	files := Emit(prog, ProfileArduinoUno)
	header := files["iotmaker_runtime.h"]
	if header == "" {
		t.Fatal("expected iotmaker_runtime.h to be present when Sleep is used")
	}

	// Header guard prevents double-inclusion.
	assertContains(t, header, "#ifndef IOTMAKER_RUNTIME_H")
	assertContains(t, header, "#define IOTMAKER_RUNTIME_H")
	assertContains(t, header, "#endif")

	// Stdint and stdbool because the contract uses int64_t and bool.
	assertContains(t, header, "#include <stdint.h>")
	assertContains(t, header, "#include <stdbool.h>")

	// The hook declaration the program calls.
	assertContains(t, header, "void iotmaker_sleep_ns(int64_t ns);")
}

// TestEmit_RuntimeStub_Contents pins the stub implementation: a
// POSIX nanosleep call wrapped in the iotmaker_sleep_ns signature.
// The maker is expected to replace this file for embedded targets,
// but the default must compile and run on any Unix host. As with
// the header test above, the IR carries one OpSleep so the stub
// actually ships.
func TestEmit_RuntimeStub_Contents(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpSleep,
		Args: []string{"%constDuration_1"},
	})
	files := Emit(prog, ProfileArduinoUno)
	stub := files["iotmaker_runtime_stub.c"]
	if stub == "" {
		t.Fatal("expected iotmaker_runtime_stub.c to be present when Sleep is used")
	}

	assertContains(t, stub, "#include \"iotmaker_runtime.h\"")
	assertContains(t, stub, "#include <time.h>")
	assertContains(t, stub, "void iotmaker_sleep_ns(int64_t ns)")
	assertContains(t, stub, "nanosleep(&ts, NULL);")
}

// =====================================================================
//  Composite scenario — the example from the design plan
// =====================================================================

// TestEmit_Composite_ConstVarAssign reproduces the exact example
// from docs/CODEGEN_ANSI_C.md Task 5: a Const, a Var, and an Assign
// chained together. This is the canonical "does the whole flow work"
// test for the foundational opcodes.
//
// Expected C (arduino_uno):
//
//	int32_t x = 42L;
//	int32_t y;
//	y = x;          ← Note: this case uses Args[0]="x" as a LITERAL.
//	                  The current OpAssign emitter does not interpret
//	                  args as register references. If the IR's
//	                  semantics for OpAssign change to use register
//	                  refs in the future, both this test and the
//	                  emitter need to update together.
//
// The third line is what the Go backend's emitAssign produces too —
// see goEmitter.emitAssign at backend/golang/emit.go.
func TestEmit_Composite_ConstVarAssign_ArduinoUno(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "x", Type: "int", Args: []string{"42"}})
	prog.Append(ir.Instruction{Op: ir.OpVar, Dest: "y", Type: "int"})
	prog.Append(ir.Instruction{Op: ir.OpAssign, Dest: "y", Type: "int", Args: []string{"x"}})

	main := emitMain(prog, ProfileArduinoUno)

	// All three lines must appear, in this order.
	assertContains(t, main, "int32_t x = 42L;")
	assertContains(t, main, "int32_t y;")
	assertContains(t, main, "y = xL;")
	// Note the suffixed "xL" above: cLiteral appends IntSuffix to
	// any "int"-typed value, regardless of whether the text is a
	// number or an identifier. This faithfully mirrors the Go
	// backend's goLiteral, which is also dumb-pass-through. Real
	// register-to-register assignments would use OpVar + arithmetic,
	// not OpAssign. See the function doc above.
}

// TestEmit_Composite_ConstVarAssign_PiLinux verifies the same flow
// produces a structurally identical file with the alternate profile,
// proving the per-target type/suffix decisions thread cleanly
// through every opcode.
func TestEmit_Composite_ConstVarAssign_PiLinux(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{Op: ir.OpConst, Dest: "x", Type: "int", Args: []string{"42"}})
	prog.Append(ir.Instruction{Op: ir.OpVar, Dest: "y", Type: "int"})

	main := emitMain(prog, ProfilePiLinux)

	assertContains(t, main, "int64_t x = 42LL;")
	assertContains(t, main, "int64_t y;")
}

// =====================================================================
//  Wrapping (main, includes, header)
// =====================================================================

// TestEmit_Wrapping_StructuralAssertions checks the file skeleton
// produced when the IR is empty — no opcodes to translate. The
// result must still be valid C: comment header, foundational
// includes, empty main with a return. The runtime include is NOT
// expected here: an empty IR makes no runtime calls, so under the
// conditional-shipping contract the project compiles standalone
// without iotmaker_runtime.h.
func TestEmit_Wrapping_StructuralAssertions(t *testing.T) {
	prog := &ir.Program{}
	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "/* main.c — generated by IoTMaker codegen.")
	assertContains(t, main, "Target profile: arduino_uno")
	assertContains(t, main, "Integer type: int32_t")
	assertContains(t, main, "Float type:   float")
	assertContains(t, main, "#include <stdint.h>")
	assertContains(t, main, "#include <stdbool.h>")
	assertContains(t, main, "int main(void) {")
	assertContains(t, main, "    return 0;")
	assertContains(t, main, "}")

	// The runtime include is gated on usesRuntime — empty IR has
	// no Sleep call, so the line should not appear. Asserting
	// absence (rather than asserting just the two stdlib headers)
	// is the test that fails loudly if the conditional ever
	// regresses to always-emit.
	if strings.Contains(main, "#include \"iotmaker_runtime.h\"") {
		t.Errorf("did not expect runtime include for empty IR; got:\n%s", main)
	}
}

// TestEmit_Wrapping_RuntimeInclude_WhenSleep pins the include side
// of the conditional-shipping contract: as soon as the body calls a
// runtime symbol, main.c regains the `#include "iotmaker_runtime.h"`
// line. The header is project-local so it comes last; the order
// keeps system headers above project-local ones, which matches the
// convention every C codebase enforces.
func TestEmit_Wrapping_RuntimeInclude_WhenSleep(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpSleep,
		Args: []string{"%constDuration_1"},
	})
	main := emitMain(prog, ProfileArduinoUno)

	assertContains(t, main, "#include <stdint.h>")
	assertContains(t, main, "#include <stdbool.h>")
	assertContains(t, main, "#include \"iotmaker_runtime.h\"")

	// Include order: stdint before stdbool before iotmaker_runtime.h.
	// stdint comes first as the most foundational; the project-local
	// header comes last so the system includes are not affected by
	// any macros the runtime header may eventually define.
	stdintPos := strings.Index(main, "#include <stdint.h>")
	stdboolPos := strings.Index(main, "#include <stdbool.h>")
	runtimePos := strings.Index(main, "#include \"iotmaker_runtime.h\"")
	if !(stdintPos < stdboolPos && stdboolPos < runtimePos) {
		t.Errorf("expected <stdint.h> < <stdbool.h> < \"iotmaker_runtime.h\"; got positions %d, %d, %d",
			stdintPos, stdboolPos, runtimePos)
	}
}

// TestEmit_Wrapping_BodyIndentation asserts the body is indented by
// four spaces — the convention chosen for C output. A regression
// here (e.g. accidentally emitting tabs) is easy to introduce when
// borrowing patterns from the Go backend.
func TestEmit_Wrapping_BodyIndentation(t *testing.T) {
	prog := &ir.Program{}
	prog.Append(ir.Instruction{
		Op:   ir.OpConst,
		Dest: "x",
		Type: "int",
		Args: []string{"42"},
	})

	main := emitMain(prog, ProfileArduinoUno)
	assertContains(t, main, "    int32_t x = 42L;")  // 4-space indent
	assertNotContains(t, main, "\tint32_t x = 42L;") // no tab indent
}

// TestEmit_Wrapping_EmptyProgramStillValid pins the contract that an
// empty IR produces a compilable main.c with just a return.
func TestEmit_Wrapping_EmptyProgramStillValid(t *testing.T) {
	prog := &ir.Program{}
	main := emitMain(prog, ProfileArduinoUno)

	// Both must be present; nothing else in the body.
	assertContains(t, main, "int main(void) {")
	assertContains(t, main, "    return 0;")

	// Sanity: the body between { and return must contain ONLY the
	// return statement (no stray emitter output).
	mainStart := strings.Index(main, "int main(void) {")
	if mainStart < 0 {
		t.Fatalf("could not find main entry in:\n%s", main)
	}
	body := main[mainStart+len("int main(void) {"):]
	body = body[:strings.Index(body, "}")]
	trimmed := strings.TrimSpace(body)
	if trimmed != "return 0;" {
		t.Errorf("expected empty-program body to be only 'return 0;', got: %q", trimmed)
	}
}

// =====================================================================
//  Test helpers
// =====================================================================

// emitMain is a tiny convenience: drive Emit, pull out main.c. Every
// test in this file uses it. Keeping the boilerplate centralised
// makes adding new tests trivial.
func emitMain(prog *ir.Program, profile TargetProfile) string {
	files := Emit(prog, profile)
	return files["main.c"]
}

// assertContains and assertNotContains mirror the helpers in the
// parent package's codeGen_c_test.go. We re-declare them locally
// because the two test files live in different Go packages and the
// helpers there are unexported. Keeping signatures identical means
// either set can be moved between files without rewriting calls.
func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected to contain %q\n  got: %s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected NOT to contain %q\n  got: %s", needle, haystack)
	}
}
