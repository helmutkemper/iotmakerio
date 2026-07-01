// Package types implements the codegen-level type compatibility rules
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// applied to every arithmetic or comparison node in the IR. Given two
// operand types it decides whether the operation is allowed, what the
// resulting type is, and whether a cast is needed on either side.
//
// The package is pure — no dependency on IR, graph, or backend — so
// the rules can be unit-tested without a full emit pipeline.
//
// # Type categories
//
//   - Abstract numeric types: "int", "float". Marks the maker used a
//     type-less constant or a device that did not commit to a bit width.
//     Compatible with each other only when they are identical.
//   - Concrete numeric types: "uint8", "uint16", "uint32", "uint64",
//     "int8", "int16", "int32", "int64", "byte" (alias of uint8),
//     "rune" (alias of int32), "float32", "float64".
//   - Abstract non-numeric: "bool", "string". Self-compatible only.
//   - Opaque: anything else ("*machine.I2C", BlackBox-provided types,
//     slice/collection types such as "[]int", unrecognised names).
//     Self-compatible only (exact match; no element widening).
//
// # Compatibility matrix
//
// Four possible outcomes:
//
//   - CastNone: same type, or semantically identical (no code needed).
//   - CastSilent: lossless promotion. Codegen inserts the cast but does
//     not surface it to the maker.
//   - CastWarn: conversion exists but may lose range, sign, or
//     precision. Codegen inserts the cast AND emits a warning
//     diagnostic so the maker sees what happened.
//   - CastImpossible: no meaningful conversion. Codegen blocks with an
//     error diagnostic.
//
// # Rules summary
//
// The following rules apply to every pair (A, B) of operand types. The
// result is symmetric — Classify(A, B) and Classify(B, A) return the
// same triple — so callers don't have to think about which side is
// "left".
//
// ## Same type
//
// Trivial: no cast, result type is the operand type.
//
// ## Both abstract numeric, different abstract
//
//	int × float → impossible (maker ambiguity; must choose one)
//
// ## Abstract × concrete numeric
//
// The concrete wins as the result type, because once the maker wired
// a bit-sized port, that precision must be respected. A cast is
// inserted on the abstract side with a warning, because the abstract
// carries no range guarantee — it may overflow the concrete range.
//
//	int   × int32  → int32,  warn, cast abstract side
//	int   × uint16 → uint16, warn, cast abstract side
//	float × float32 → float32, warn, cast abstract side
//
// ## Concrete × concrete numeric
//
// The rule is the smallest common type that holds both sets, with a
// preference for signed when mixing signed and unsigned:
//
//   - Both unsigned, same family: widen to the larger.  uint8 × uint16
//     → uint16, silent cast for the smaller side.
//   - Both signed, same family: widen to the larger.    int8 × int16
//     → int16, silent.
//   - Mixed signed/unsigned: widen to signed one size larger than
//     the larger operand, so every possible value of both fits.
//     uint8 × int8 → int16, silent.
//     uint16 × int16 → int32, silent.
//     uint32 × int32 → int64, silent.
//     uint64 × anything signed → impossible (no signed type holds
//     the full uint64 range); fallback to the user fixing the wire.
//   - Integer × float: widen to float. Silent when the integer bit
//     width fits exactly in the float mantissa (uint8..uint16 into
//     float32; uint8..uint32 into float64), otherwise warn because
//     large integers lose precision.
//   - Float × float: widen to the wider. float32 × float64 → float64,
//     silent.
//
// Signed → unsigned castdown is NEVER silent; it is always warn or
// impossible, because negative values become garbage.
//
// ## Non-numeric kinds
//
// bool, string, and opaque types compare equal only to themselves.
// Anything else is impossible.
//
// # Rationale for picking "the concrete wins"
//
// The maker's intent is encoded in the concrete type of the wired
// port. When they drag a StatementConstInt (abstract "int") onto a
// uint16 input, the concrete port tells the story of what the
// hardware expects — so the result should stay uint16 and the
// generic int has to adapt. The warning surfaces the cast in the
// panel so makers who *intended* a wider type see it instead of
// getting silent hardware quirks on the target board (Arduino,
// TinyGo, etc.).
//
// Português: Regras de compatibilidade de tipos pra operações
// aritméticas e de comparação. Pacote puro, sem dependência de IR
// nem do grafo — fácil de testar.
//
//	Quatro resultados: CastNone, CastSilent, CastWarn, CastImpossible.
//	Concreto sempre vence abstrato. Signed → unsigned nunca é silent.
//	A decisão é simétrica: Classify(A,B) == Classify(B,A).
package types

// CastAction describes what codegen must do with the operand pair.
//
// Português: O que o codegen deve fazer com o par de operandos.
type CastAction int

const (
	// CastNone: the two operands are already the same type or
	// otherwise require no conversion. Result type equals the operand
	// type. No cast instruction is emitted.
	CastNone CastAction = iota

	// CastSilent: conversion exists and is lossless. Cast is inserted
	// by the emitter; no diagnostic is surfaced to the maker.
	CastSilent

	// CastWarn: conversion exists but is not lossless. Cast is still
	// inserted — the code compiles — but the maker gets a warning
	// diagnostic naming the operands so the risk is visible.
	CastWarn

	// CastImpossible: no meaningful conversion. The emitter blocks
	// with an error diagnostic; no cast is inserted.
	CastImpossible
)

// String returns a short human label for logs and tests.
func (a CastAction) String() string {
	switch a {
	case CastNone:
		return "none"
	case CastSilent:
		return "silent"
	case CastWarn:
		return "warn"
	case CastImpossible:
		return "impossible"
	default:
		return "unknown"
	}
}

// Classification is the full result of analysing one pair of operand
// types. When Action is CastImpossible, Result is empty and the two
// CastX fields are zero — the caller must abort the operation.
//
// CastA/CastB carry the concrete type each operand must be converted
// to (empty string when no cast is needed on that side). They are
// always the same value as Result when non-empty — both sides end up
// at the promoted type — but keeping them split makes the emitter's
// loop trivial: for each operand, if CastX is non-empty, emit a
// CONVERT instruction to CastX before using the operand.
//
// Português: Resultado completo da análise. CastA/CastB são o tipo
// alvo pra cada lado (vazio = sem cast). Action=Impossible => aborta.
type Classification struct {
	Action CastAction
	Result string // resulting type of the operation (empty when impossible)
	CastA  string // target type for operand A ("" = no cast needed)
	CastB  string // target type for operand B ("" = no cast needed)
}

// Classify decides how to combine operand types a and b for a binary
// arithmetic or comparison operation. The function is symmetric: the
// returned Result, CastA/CastB semantics follow the input argument
// order, so Classify(A,B).CastA always refers to A and CastB to B.
//
// Português: Classifica o par (a, b) para uma operação binária.
func Classify(a, b string) Classification {
	// Identical types: trivial path. Any non-numeric, opaque, or
	// unknown type also enters here if both sides happen to match.
	if a == b {
		return Classification{Action: CastNone, Result: a}
	}

	// Non-numeric kinds only combine with themselves (handled above),
	// so any remaining pair containing bool/string is impossible.
	if isNonNumericKind(a) || isNonNumericKind(b) {
		return Classification{Action: CastImpossible}
	}

	aNumeric := isNumeric(a)
	bNumeric := isNumeric(b)
	if !aNumeric || !bNumeric {
		// At least one side is an opaque type (pointer, BlackBox
		// struct, slice/collection like "[]int", unknown name). The
		// wire layer should have prevented this, but the codegen still
		// rejects defensively.
		//
		// This branch is also what defines the v1 collection rule:
		// opaque types only ever match themselves via the a==b check
		// above, so "[]int" × "[]int" is CastNone (no cast), while
		// "[]int" × int or "[]int" × "[]float32" land here as
		// impossible. Element-aware widening across collections, if it
		// is ever wanted, would branch out before this point.
		return Classification{Action: CastImpossible}
	}

	// Both numeric, different types. Resolve per category mix.
	aAbs, bAbs := isAbstract(a), isAbstract(b)
	switch {
	case aAbs && bAbs:
		// Different abstracts (e.g. int × float). Ambiguous intent.
		return Classification{Action: CastImpossible}

	case aAbs && !bAbs:
		// Concrete wins. Warn because the abstract carries no width
		// guarantee and may overflow the concrete range.
		return Classification{
			Action: CastWarn,
			Result: b,
			CastA:  b,
		}

	case !aAbs && bAbs:
		return Classification{
			Action: CastWarn,
			Result: a,
			CastB:  a,
		}
	}

	// Both concrete. Pick the smallest common type, then decide
	// whether casts are lossless. Helpers below do the width/sign
	// reasoning and set the verdict.
	return classifyConcretePair(a, b)
}

// classifyConcretePair is the hot path: both operands are concrete
// numeric. Delegates to the appropriate family pair.
func classifyConcretePair(a, b string) Classification {
	aIsFloat := isFloat(a)
	bIsFloat := isFloat(b)

	switch {
	case aIsFloat && bIsFloat:
		return classifyFloatFloat(a, b)
	case aIsFloat && !bIsFloat:
		return classifyIntFloat(b, a /*floatIsB=*/, true)
	case !aIsFloat && bIsFloat:
		return classifyIntFloat(a, b /*floatIsB=*/, false)
	default:
		return classifyIntInt(a, b)
	}
}

// classifyFloatFloat: both float. Widen to the wider, silent cast.
func classifyFloatFloat(a, b string) Classification {
	if floatBits(a) >= floatBits(b) {
		return Classification{Action: CastSilent, Result: a, CastB: a}
	}
	return Classification{Action: CastSilent, Result: b, CastA: b}
}

// classifyIntFloat: one int, one float. Result is float. Cast is
// silent when the int fits exactly in the float mantissa, otherwise
// warn because large integers may round.
func classifyIntFloat(intT, floatT string, floatIsB bool) Classification {
	bits := intBits(intT)
	action := CastSilent
	// Mantissa bits: float32 = 24, float64 = 53. Integers wider than
	// the mantissa may not be representable exactly.
	limit := 53
	if floatT == "float32" {
		limit = 24
	}
	if bits > limit {
		action = CastWarn
	}
	result := Classification{Action: action, Result: floatT}
	if floatIsB {
		result.CastA = floatT // a is int, needs cast
	} else {
		result.CastB = floatT // b is int, needs cast
	}
	return result
}

// classifyIntInt: both concrete integers. Three sub-cases drive the
// decision: same signedness (widen), mixed (widen to signed holding
// both), uint64 × signed (impossible).
func classifyIntInt(a, b string) Classification {
	aSigned := isSignedInt(a)
	bSigned := isSignedInt(b)
	aBits := intBits(a)
	bBits := intBits(b)

	// Same signedness: widen to larger, silent. Covers uint8×uint16,
	// int8×int16, etc.
	if aSigned == bSigned {
		if aBits >= bBits {
			return Classification{Action: CastSilent, Result: a, CastB: a}
		}
		return Classification{Action: CastSilent, Result: b, CastA: b}
	}

	// Mixed signedness. Result must hold full range of both.
	// Strategy: take max(bits) and go one step up in the signed family.
	// uint8×int8   → int16
	// uint16×int16 → int32
	// uint32×int32 → int64
	// uint64×anything signed → impossible (no signed holds uint64).
	maxBits := aBits
	if bBits > maxBits {
		maxBits = bBits
	}

	// Special rule: uint64 × signed has no home in the signed family.
	if (a == "uint64" || b == "uint64") && (aSigned != bSigned) {
		return Classification{Action: CastImpossible}
	}

	promoted := signedTypeOfWidth(maxBits * 2)
	if promoted == "" {
		return Classification{Action: CastImpossible}
	}
	// Silent when both sides can fit; since the promotion was chosen
	// precisely to fit both, it's always silent for the allowed cases.
	return Classification{
		Action: CastSilent,
		Result: promoted,
		CastA:  promoted,
		CastB:  promoted,
	}
}

// =====================================================================
//  Type introspection
// =====================================================================

// isAbstract returns true when the type name is one of the maker-level
// abstract markers produced by type-less devices (e.g. StatementConstInt
// emits "int", not "int64").
func isAbstract(t string) bool {
	return t == "int" || t == "float"
}

// isNonNumericKind returns true for the two abstract non-numeric
// categories the codegen is aware of.
func isNonNumericKind(t string) bool {
	return t == "bool" || t == "string"
}

// isNumeric reports whether t is recognised as numeric — abstract or
// concrete. Unknown names (pointers, BlackBox structs) return false.
func isNumeric(t string) bool {
	if isAbstract(t) {
		return true
	}
	if _, ok := intWidths[t]; ok {
		return true
	}
	if _, ok := floatWidths[t]; ok {
		return true
	}
	return false
}

// isFloat returns true when t is a concrete float type.
func isFloat(t string) bool {
	_, ok := floatWidths[t]
	return ok
}

// isSignedInt returns true when t is a concrete signed integer.
func isSignedInt(t string) bool {
	w, ok := intWidths[t]
	return ok && w.signed
}

// intBits and floatBits return the bit widths of a concrete type, or
// 0 when unknown.
func intBits(t string) int {
	if w, ok := intWidths[t]; ok {
		return w.bits
	}
	return 0
}

func floatBits(t string) int {
	if b, ok := floatWidths[t]; ok {
		return b
	}
	return 0
}

// signedTypeOfWidth returns the canonical signed integer type of the
// requested bit width (8, 16, 32, 64). Returns "" for widths that do
// not map to a Go signed type.
func signedTypeOfWidth(bits int) string {
	switch bits {
	case 8:
		return "int8"
	case 16:
		return "int16"
	case 32:
		return "int32"
	case 64:
		return "int64"
	}
	return ""
}

// intWidth carries bit size and signedness for a concrete integer.
type intWidth struct {
	bits   int
	signed bool
}

// intWidths enumerates every concrete integer type the codegen
// understands. "byte" is an alias of uint8, "rune" of int32.
var intWidths = map[string]intWidth{
	"int8":   {8, true},
	"int16":  {16, true},
	"int32":  {32, true},
	"int64":  {64, true},
	"uint8":  {8, false},
	"uint16": {16, false},
	"uint32": {32, false},
	"uint64": {64, false},
	"byte":   {8, false},
	"rune":   {32, true},
}

// floatWidths enumerates every concrete float type the codegen
// understands, mapped to their significand-relevant bit width.
var floatWidths = map[string]int{
	"float32": 32,
	"float64": 64,
}
