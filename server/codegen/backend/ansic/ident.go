// /server/codegen/backend/ansic/ident.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

// ident.go — Pure string helpers used by the C emitter.
//
// This file holds the small, side-effect-free functions that translate
// the IR's textual conventions (device IDs, register references, type
// names, literal values) into their C99 counterparts. Each function is
// pure, deterministic, and depends only on the standard "strings"
// package plus the local profile.go. No IR, no graph, no fmt — those
// belong to emit.go.
//
// The functions are intentionally close in shape to their Go-backend
// counterparts (goIdent, goOperand, goTypeName, goLiteral) so anyone
// who already knows the Go side can read the C side without rediscovery.
// The two divergence points are documented inline where they occur:
//
//   - cTypeName consults a TargetProfile (the Go backend hardcodes its
//     widening to int64/float64; in C, "int" can mean int32_t or
//     int64_t depending on the alvo).
//
//   - cLiteral applies a per-profile suffix and, when emitting a
//     float without a decimal point, inserts ".0" so the literal is
//     syntactically a float in C. ("3f" is invalid C; "3.0f" is.)
//
// Português:
//
//	Helpers puros de string usados pelo emitter de C. São funções
//	determinísticas, sem efeitos colaterais, e dependem só do pacote
//	"strings" e do profile.go local. Mantêm forma próxima dos análogos
//	do backend Go pra que quem conhece um lado leia o outro de
//	imediato. Os pontos de divergência (cTypeName e cLiteral usam o
//	perfil) estão documentados no local em cada função.

import "strings"

// =====================================================================
//  Identifier helpers
// =====================================================================

// cIdent converts an IR device ID into a valid C identifier.
//
// The transformation is a direct mirror of goIdent: each '_' that
// immediately precedes a digit is stripped, joining the suffix to the
// base name. Examples:
//
//	"constInt_1" → "constInt1"
//	"add_2"      → "add2"
//	"loop_3"     → "loop3"
//	"i2cBus_42"  → "i2cBus42"
//
// An empty input collapses to "v" — a placeholder that compiles but
// is obvious in the output, matching the Go backend's defensive
// behaviour. This case is never expected to fire in practice (every
// device in the IoTMaker IDE has a non-empty ID by construction),
// but the guard avoids a silent "= 0" landing in the emitted C if
// the IR ever passes through an empty Dest.
//
// What this function does NOT do:
//
//   - It does not escape C reserved words. A device in the IoTMaker
//     IDE literally named "int" or "if" would round-trip into broken
//     C. In practice device IDs are mechanically generated as
//     "<type>_<n>" (e.g. "constInt_1") and never collide with
//     keywords, so this case is parked as a Phase 2 hardening. If it
//     ever bites, the fix is to append a constant prefix (like "v_")
//     to the result before returning.
//
//   - It does not check that the result starts with a letter or
//     underscore (the C identifier rule). Stripping "_" before a
//     digit could theoretically expose a leading digit (e.g. an ID
//     of the form "_42foo" would become "42foo"), which is invalid
//     C. The IoTMaker IDE does not produce such IDs today, but Phase
//     2 hardening should add a guard here as well.
//
// Português:
//
//	Converte um ID de device do IR em um identificador C válido. A
//	transformação é espelho exata do goIdent: remove '_' imediatamente
//	antes de dígito. ID vazio cai em "v" (defensivo, não esperado em
//	cenas reais). Não escapa palavras-chave do C nem checa se começa
//	com letra — endurecimento pra Fase 2.
func cIdent(id string) string {
	var sb strings.Builder
	sb.Grow(len(id))
	for i := 0; i < len(id); i++ {
		if id[i] == '_' && i+1 < len(id) && id[i+1] >= '0' && id[i+1] <= '9' {
			continue
		}
		sb.WriteByte(id[i])
	}
	result := sb.String()
	if result == "" {
		return "v"
	}
	return result
}

// cOperand converts an IR operand string into C code.
//
// The IR distinguishes two operand forms:
//
//   - A literal value, used for constants embedded directly in
//     instructions (currently never; constants always go through
//     CONST → register, but the form is preserved for symmetry with
//     the Go backend). Literals are returned unchanged.
//
//   - A register reference, prefixed by "%". Two sub-forms exist:
//
//     Simple:   "%add_1"          → "add1"
//     Compound: "%i2cBus_1:bus"   → "i2cBus1_bus"
//
//     The compound form names a specific output port on a black-box
//     instance — currently produced by Phase 2 opcodes (BB_INIT,
//     BB_METHOD) that have multiple output ports. Phase 1 never
//     emits this form, but the function handles it for parity with
//     goOperand so when Phase 2 lands the call sites need no change.
//
// The join character "_" in compound form is the same one the Go
// backend uses, so a port "bus" on instance "i2cBus_1" becomes the
// same C identifier as the Go identifier — useful when reading both
// outputs side by side during black-box debugging.
//
// Português:
//
//	Converte um operando do IR em código C. "%name" vira o nome
//	limpo, "%inst:port" vira "inst_port" (forma composta usada pelos
//	opcodes de black-box na Fase 2). Literais passam direto.
func cOperand(arg string) string {
	if !strings.HasPrefix(arg, "%") {
		// Literal — pass through unchanged. Examples in practice:
		// quoted strings from OpOutput's channel arg, numeric values
		// that bypassed the OpConst → register path.
		return arg
	}

	ref := arg[1:] // drop the leading '%'

	// Compound: instanceId:portName.
	if idx := strings.Index(ref, ":"); idx >= 0 {
		return cIdent(ref[:idx]) + "_" + ref[idx+1:]
	}

	return cIdent(ref)
}

// =====================================================================
//  Type helpers
// =====================================================================

// cTypeName maps an abstract IR type name to the concrete C type
// dictated by the current TargetProfile.
//
// The IR carries three abstract type names that exist regardless of
// target: "int", "float", "bool". Each maps to a different concrete
// C type per profile (e.g. arduino_uno uses int32_t/float/bool;
// pi_linux uses int64_t/double/bool). The mapping table lives in
// profile.go.
//
// Two further cases:
//
//   - "string" maps to "const char*". C99 has no string type; the
//     closest semantic match for an immutable text value is a
//     pointer to const char. Phase 1 does not exercise this path
//     (no StatementConstString opcode is in scope) but it is
//     supported for forward compatibility.
//
//   - An empty IR type defaults to profile.IntType. The Go backend
//     defaults to int64 in the same situation; this is a defensive
//     fallback so emitters that forgot to set inst.Type still produce
//     compilable code rather than "int  x;".
//
// Anything else — typically a Go-side concrete type that came through
// a black-box port (e.g. "uint16", "byte", "*machine.I2C") — passes
// through unchanged. Phase 2 will need a dedicated translation pass
// for these because Go's "uint16" is C's "uint16_t" (via
// <stdint.h>), and "*machine.I2C" has no direct C equivalent at all.
// That translation is intentionally not in this function: it would
// require knowledge of the black-box parser's type vocabulary which
// belongs to a future Phase 2 helper.
//
// Português:
//
//	Mapeia tipo abstrato do IR para tipo C concreto, usando o
//	perfil. "int"/"float"/"bool" são resolvidos via perfil. "string"
//	vira "const char*". Tipo vazio cai no IntType (defensivo). Outros
//	passam direto — Fase 2 vai precisar traduzir tipos vindos de
//	BlackBox (uint16 → uint16_t, etc.) num passo separado.
func cTypeName(irType string, profile TargetProfile) string {
	switch irType {
	case "int":
		return profile.IntType
	case "float":
		return profile.FloatType
	case "float32":
		// Concrete single precision chosen on the device — standard C
		// "float" in every profile. Unlike the abstract "float" above,
		// the maker's precision choice is honoured verbatim rather than
		// deferred to the profile.
		return "float"
	case "float64":
		// Concrete double precision — standard C "double" in every
		// profile. (On targets where "double" is 32-bit, e.g. AVR, that
		// is the platform's definition of double, not a codegen choice.)
		return "double"
	case "bool":
		return profile.BoolType
	case "string":
		return "const char*"
	case "time.Duration":
		// Durations are always nanoseconds in 64-bit precision —
		// the IR carries them as Go's time.Duration alias. We map
		// to int64_t in every profile, including arduino_uno where
		// the default IntType is int32_t. One second is 10^9 ns,
		// which overflows int32_t; using int64_t unconditionally
		// avoids silent overflow on the most common case
		// (LoopDuration with second-scale cadence).
		return "int64_t"
	case "int8":
		return "int8_t"
	case "int16":
		return "int16_t"
	case "int32":
		return "int32_t"
	case "int64":
		return "int64_t"
	case "uint8", "byte":
		// IDE/Go "byte" is an alias of uint8 — one C face for both.
		return "uint8_t"
	case "uint16":
		return "uint16_t"
	case "uint32":
		return "uint32_t"
	case "uint64":
		return "uint64_t"
	case "":
		// Defensive default — same spirit as goTypeName's "int64".
		return profile.IntType
	default:
		// Remaining pass-through territory: pointer/struct tokens from
		// black-box parsers (*machine.I2C, sht3x_t*, …) keep their
		// authored spelling. The FIXED-WIDTH integer tokens used to live
		// here as Phase-2 debt; they are now mapped above (T7 of the
		// const-array plan) — <stdint.h> is one of the foundational
		// includes, so the names are always available. Platform-width
		// tokens ("uint", "int" arrive abstract) never reach this point
		// from collection inference: the C parser only collapses
		// fixed-width element pointers (cPointerElemToIDE).
		return irType
	}
}

// =====================================================================
//  Literal helpers
// =====================================================================

// cLiteral wraps a raw IR value in the appropriate C literal form,
// applying the per-type suffix dictated by the TargetProfile.
//
// Behaviour per IR type:
//
//   - int (and empty type): the IntSuffix is appended verbatim.
//     "10" with profile arduino_uno → "10L".
//     "10" with profile pi_linux    → "10LL".
//     A leading minus is fine: the C grammar treats "-10L" as the
//     unary minus operator applied to the literal "10L", which is
//     what we want.
//
//   - float: the value gets the FloatSuffix appended. If the value
//     has no decimal point and no exponent (e.g. "3" instead of
//     "3.14" or "1e10"), ".0" is inserted before the suffix because
//     "3f" is not a valid C literal — the lexer parses "3" as an int
//     and refuses to attach the float suffix. "3.0f" works.
//     "3" with profile arduino_uno  → "3.0f".
//     "3.14" with profile pi_linux  → "3.14"   (empty FloatSuffix).
//     "1e10" with profile arduino_uno → "1e10f" (exponent counts as
//     "looks like float").
//
//   - bool, string, and unknown types: the value is returned
//     unchanged. C99's <stdbool.h> defines "true" and "false" as
//     valid bool literals, and string literals already arrive
//     quoted from the IR (`"hello"` → `"hello"`). For unknown types
//     the safe default is identity — the same posture goLiteral
//     takes.
//
// Why the suffix matters in practice:
//
// On 8-bit AVR (arduino_uno profile) a bare integer literal like
// "10" is interpreted as a 16-bit int by the avr-gcc compiler, which
// silently overflows for values above 32767. Appending "L" forces a
// 32-bit long interpretation that matches the declared int32_t type
// of the surrounding variable.
//
// Similarly, a bare float literal like "3.14" is a double in standard
// C, and AVR has no native double precision — the compiler emits
// software-emulated double arithmetic that is dramatically slower
// than float. Appending "f" keeps the literal as a 32-bit float so
// the multiplication or addition that follows uses cheap float
// instructions instead of expensive double ones.
//
// Português:
//
//	Envolve um valor cru do IR no formato literal apropriado para C,
//	com sufixo por perfil. Int recebe sufixo direto. Float recebe
//	".0" extra quando não tem ponto nem expoente (porque "3f" não é
//	literal C válido). Bool e string passam direto. Os sufixos são
//	críticos em AVR: sem "L" os inteiros podem ficar em 16 bits e
//	estourar; sem "f" os floats viram doubles emulados em software,
//	dramaticamente mais lentos.
func cLiteral(irType, val string, profile TargetProfile) string {
	switch irType {
	case "int", "":
		return val + profile.IntSuffix

	case "time.Duration":
		// Durations are always int64_t nanoseconds — see cTypeName's
		// time.Duration case for rationale. The LL suffix is
		// unconditional here, regardless of the profile's IntSuffix,
		// because the type itself was widened to int64_t. Using the
		// profile's "L" suffix would mismatch the int64_t declaration
		// on arduino_uno and trigger a "narrowing conversion" warning
		// in strict compilers.
		return val + "LL"

	case "float":
		out := val
		if !looksLikeFloat(out) {
			// "3" → "3.0" so the suffix can attach. Without this
			// the C lexer would refuse "3f" — the unsuffixed token
			// "3" is locked in as an int literal.
			out = out + ".0"
		}
		return out + profile.FloatSuffix

	case "float32":
		// Concrete single precision. Ensure a decimal point, then the
		// unconditional "f" suffix so the literal is a float (not a
		// double the compiler must narrow). Profile-independent — the
		// precision was the maker's explicit choice, not the target's.
		out := val
		if !looksLikeFloat(out) {
			out = out + ".0"
		}
		return out + "f"

	case "float64":
		// Concrete double precision. Ensure a decimal point; no suffix,
		// because an unsuffixed C floating literal is already a double.
		out := val
		if !looksLikeFloat(out) {
			out = out + ".0"
		}
		return out

	case "int8", "int16", "uint8", "uint16", "byte":
		// Narrow fixed-width integers need no suffix: a C99 decimal
		// literal is typed as the FIRST of int → long → long long that
		// fits (§6.4.4.1), so any value of these ranges is already an
		// int, and initialising the narrower _t type from it is a plain
		// (lossless) conversion.
		return val

	case "int32":
		// Mirrors the arduino_uno profile's IntSuffix for its int32_t:
		// "L" guarantees the literal is at least 32 bits even where int
		// is 16 (AVR), with no narrowing surprise elsewhere.
		return val + "L"

	case "int64":
		return val + "LL"

	case "uint32":
		// C99 decimal literals never become unsigned on their own
		// (§6.4.4.1 walks the SIGNED ladder) — the U keeps the constant
		// in unsigned arithmetic from the first token; L widens it past
		// 16-bit-int targets.
		return val + "UL"

	case "uint64":
		// Values above INT64_MAX fit NO signed type — without ULL the
		// literal is a constraint violation, not merely a warning.
		return val + "ULL"

	case "bool":
		// IR emits "true" / "false" verbatim — both are valid in
		// C99 via <stdbool.h>. Pass through.
		return val

	case "string":
		// IR carries the quoted form already (e.g. `"hello"`).
		// Pass through; the value is already a valid C string
		// literal.
		return val

	default:
		// Unknown type (likely a Phase 2 black-box-derived type).
		// Safest behaviour is identity, matching goLiteral.
		return val
	}
}

// =====================================================================
//  Internal helpers
// =====================================================================

// looksLikeFloat reports whether a numeric string is syntactically a
// floating-point literal in C — i.e. whether the C lexer would tokenise
// it as a float without needing additional decoration.
//
// The rule: a literal is "float-shaped" if it contains a decimal point
// or an exponent marker ('e' or 'E'). Examples:
//
//	"3.14"   → true  (decimal point)
//	"1e10"   → true  (exponent)
//	"1.5e10" → true  (both)
//	"3"      → false (looks like an int)
//	"-42"    → false (the sign is not part of the literal in C; the
//	                  unary minus is a separate operator)
//
// This function is used by cLiteral to decide whether to insert ".0"
// before appending the float suffix. It is intentionally permissive:
// it does not validate the rest of the syntax. If the IR emits a
// malformed value like "3.14.15" it will still pass through unchanged
// and the C compiler will surface the syntax error — the IR is the
// upstream guarantor of well-formedness, not this helper.
//
// Português:
//
//	Reporta se a string numérica já tem forma de float pra o lexer
//	do C — ou seja, se tem '.' ou 'e'/'E'. Usado por cLiteral pra
//	decidir se precisa adicionar ".0" antes do sufixo. Não valida
//	o resto da sintaxe; confia que o IR não emite valores malformados.
func looksLikeFloat(s string) bool {
	return strings.ContainsAny(s, ".eE")
}
