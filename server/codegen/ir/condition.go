// server/codegen/ir/condition.go
package ir

import (
	"strings"

	"server/codegen/graph"
)

// BuildCaseCondition assembles the boolean expression for a single
// StatementCase branch in the if/else-if lowering (the COND_LABEL opcode).
//
// The relational and logical operators it produces (==, >=, <=, >, <, &&, ||)
// are byte-for-byte identical in Go and C, so one builder serves both
// backends. Each backend is responsible only for (a) resolving sel to its own
// variable spelling before calling this, and (b) wrapping the returned
// expression in its own conditional syntax — Go `if <expr> {` versus C
// `if (<expr>) {`.
//
// operands are pre-formatted integer literals whose meaning depends on
// matchKind:
//
//   - "is"               equality with a single value     → sel == v
//   - "isAnyOf"          equality with any listed value    → sel == v1 || sel == v2 …
//   - "between"          inclusive range [lo, hi]           → sel >= lo && sel <= hi
//   - "gt"/"lt"/"gte"/"lte"  a single threshold             → sel > v, sel < v, …
//
// Robustness: a between with fewer than two operands, a threshold with no
// operand, or an unknown/empty matchKind all fall back to discrete equality
// over whatever operands are present. An empty operand list yields the literal
// "false" (a branch that can never match) rather than malformed code, so a
// corrupt scene still compiles. None of these fallbacks should occur for a
// scene produced by the IDE — the overlay's validation guarantees well-formed
// operands — but the IR must never emit code that fails to compile.
//
// Português: Monta a expressão booleana de um ramo do StatementCase no
// lowering if/else-if. Os operadores (== >= <= > < && ||) são idênticos em Go
// e C, então um único builder serve aos dois backends; cada backend só resolve
// sel para o nome de variável dele e embrulha o resultado na sintaxe própria
// (Go `if <expr> {`, C `if (<expr>) {`). Os fallbacks cobrem cenas corrompidas
// para nunca gerar código que não compila.
func BuildCaseCondition(sel, matchKind string, operands []string) string {
	switch matchKind {
	case "between":
		if len(operands) >= 2 {
			return sel + " >= " + operands[0] + " && " + sel + " <= " + operands[1]
		}
		// malformed — fall through to discrete equality below
	case "gt", "lt", "gte", "lte":
		if len(operands) >= 1 {
			return sel + " " + relationalOperator(matchKind) + " " + operands[0]
		}
		// malformed — fall through to discrete equality below
	}

	// "is", "isAnyOf", "", or any malformed case above: equality, OR-ed over
	// every operand. A single operand collapses to a plain `sel == v`.
	if len(operands) == 0 {
		return "false"
	}
	parts := make([]string, len(operands))
	for i, v := range operands {
		parts[i] = sel + " == " + v
	}
	return strings.Join(parts, " || ")
}

// relationalOperator maps a threshold matchKind to its operator. It is only
// reached for the four threshold kinds; any other input returns "==" as a safe
// default so the result is still valid code.
//
// Português: Mapeia um matchKind de limiar para o operador. Só é chamada para
// os quatro tipos de limiar; qualquer outro valor retorna "==" como padrão
// seguro.
func relationalOperator(matchKind string) string {
	switch matchKind {
	case "gt":
		return ">"
	case "lt":
		return "<"
	case "gte":
		return ">="
	case "lte":
		return "<="
	}
	return "=="
}

// DiscreteMatchKind reports whether a case's matchKind can be expressed as a
// switch `case` label (a list of discrete constants) rather than requiring the
// if/else-if chain. Only "is" and "isAnyOf" qualify; the empty string is
// treated as discrete because legacy scenes (predating matchKind) carried only
// discrete value lists and extractCases backfills them to "is"/"isAnyOf".
//
// emitCase uses this across all non-default cases to choose between the
// SWITCH_* lowering and the COND_* lowering.
//
// Português: Diz se o matchKind de um case pode virar um rótulo `case` de
// switch (constantes discretas) em vez de exigir a cadeia if/else-if. Só "is"
// e "isAnyOf" qualificam; string vazia conta como discreta por
// retrocompatibilidade com cenas antigas.
func DiscreteMatchKind(matchKind string) bool {
	return matchKind == "is" || matchKind == "isAnyOf" || matchKind == ""
}

// UseSwitchLowering reports whether a StatementCase whose ordered, non-default
// cases are `cases` lowers to a switch statement (true) rather than an
// if/else-if chain (false).
//
// A switch `case` label accepts only discrete constant lists in both Go and C,
// so the switch form is viable only when EVERY non-default case is discrete
// (is/isAnyOf — see DiscreteMatchKind). A single range or comparison case
// (between/gt/lt/gte/lte) forces the whole Case onto the if/else-if chain. The
// default case never affects the decision — it is the switch `default:` or the
// chain's trailing `else` either way.
//
// This is the SINGLE authority for the switch-vs-chain decision. emitCase uses
// it to pick the lowering, and codegen.ValidateCases uses it to choose the
// severity of a colliding value: a duplicate switch label does not compile (an
// error), whereas the same collision in a chain is merely an unreachable
// branch (a warning). Keeping one function guarantees the emitter and the
// validator can never disagree about which form a given set of cases takes —
// which is the whole point of validating against the codegen rather than a
// re-implementation of its rules.
//
// Português: Diz se um StatementCase rebaixa para switch (true) ou para a
// cadeia if/else-if (false). Só é switch quando TODO case não-default é
// discreto; um único range/comparação força a cadeia. É a autoridade ÚNICA
// dessa decisão — usada pelo emitCase (escolher o lowering) e pelo
// ValidateCases (escolher a severidade da colisão: rótulo duplicado de switch
// não compila → error; a mesma colisão na cadeia é ramo inalcançável →
// warning). Uma função só garante que emitter e validador nunca discordem.
func UseSwitchLowering(cases []graph.CaseDef) bool {
	for _, c := range cases {
		if c.IsDefault {
			continue
		}
		if !DiscreteMatchKind(c.MatchKind) {
			return false
		}
	}
	return true
}
