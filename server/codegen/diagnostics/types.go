// Package diagnostics defines the structured issue type shared across
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// the codegen pipeline (builder, IR emitter, validator, backends).
//
// Why a separate package:
//
//	graph/ and ir/ both need to emit structured issues, and both are
//	imported by the top-level codegen package. Putting the type in
//	codegen would create an import cycle. Putting it here lets every
//	layer produce the same shape, and the top-level codegen package
//	re-exports it via a type alias so external callers see a single
//	`codegen.Diagnostic` name.
//
// Português:
//
//	Tipo compartilhado entre builder, emitter e validador do pipeline
//	de codegen. Em package próprio pra evitar ciclo de import — ele é
//	importado por todas as camadas.
package diagnostics

// Severity levels. Errors block code generation; warnings do not.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Known diagnostic kinds. The string values are stable API — the IDE's
// UI binds severity styling and message translations to them. Add new
// kinds here rather than inventing free-form strings at call sites so
// the UI can be exhaustive about rendering.
const (
	KindSceneParse          = "scene_parse"
	KindGeometric           = "geometric"
	KindScopeCrossingMulti  = "scope_crossing_multi_output"
	KindMissingConnection   = "missing_connection"
	KindMissingStop         = "missing_stop"
	KindMissingInterval     = "missing_interval"
	KindMissingCondition    = "missing_condition"
	KindBlackBoxDefMissing  = "blackbox_def_missing"
	KindUnsupportedLanguage = "unsupported_language"
	// KindBlackBoxFilesInvalid marks an authored device file set that
	// cannot ship: hostile path, missing .c, authored main(), collision
	// with the generated header, or a multi-file def without identity.
	// Rules in codegen/bbfiles_validate.go.
	KindBlackBoxFilesInvalid = "blackbox_files_invalid"
	KindEmitterInternal      = "emitter_internal"
	// KindPhaseOrder marks a phase-tunnel wired against the sequence's
	// temporal order (feed outside the birth phase, or a consumer at or
	// before it) — the C99 §7 rule, server-side truth. Português: Túnel
	// de fase ligado contra a ordem temporal do sequence (feed fora da
	// fase natal, ou consumo nela/antes dela) — a regra §7 no servidor.
	KindPhaseOrder = "phase_order"
	// KindFunctionSignature marks a tunnel-derived signature problem
	// (Fatia C): an untyped parameter/return slot, a duplicate name, or
	// a return tunnel with no feed. Português: Problema na assinatura
	// derivada de túneis — slot sem tipo, nome duplicado, retorno sem
	// alimentação.
	KindFunctionSignature = "function_signature"

	// KindCancelled — codegen was cancelled by the caller (typically
	// because the user closed the IDE tab or clicked Cancel on the
	// progress overlay). Error severity because the response is
	// incomplete and must not be treated as a successful generation,
	// but the message itself is informational — the worker that
	// observed ctx.Err() != nil reports failure via its own channel.
	//
	// Português: Codegen cancelado pelo chamador. Severity error porque
	// o response está incompleto. A mensagem em si é informativa — o
	// worker reporta a falha pelo Redis independentemente.
	KindCancelled = "cancelled"

	// KindTypeMismatch — two operand types cannot be combined under
	// any conversion the codegen knows about. Always error severity.
	// Example: "bool" × "int32", or "*machine.I2C" reaching an Add
	// node (the wire layer should block this, so hitting it usually
	// means a scene saved before a wire-policy change).
	//
	// Português: Tipos incompatíveis — nenhuma conversão conhecida
	// combina os dois. Sempre error.
	KindTypeMismatch = "type_mismatch"

	// KindTypeLossy — a conversion exists and is inserted so the code
	// compiles, but it may lose range, sign, or precision. Warning
	// severity. Example: int × uint16 (abstract int may overflow
	// uint16), int64 × float32 (>24-bit int may round), signed →
	// unsigned downcast.
	//
	// Português: Conversão existe e é inserida (código compila) mas
	// pode perder range, sinal ou precisão. Warning.
	KindTypeLossy = "type_lossy"

	// KindCaseDuplicateValue — two cases of a StatementCase that lowers to
	// a switch claim the same discrete value, which would emit a duplicate
	// `case` label that neither Go nor C will compile. Always error
	// severity. Only raised for the switch lowering; in the if/else-if
	// lowering the same situation surfaces as an unreachable branch
	// (KindCaseUnreachable, warning) because first-match-wins makes the
	// later branch merely dead, not malformed.
	//
	// Português: Dois cases (em lowering de switch) reivindicam o mesmo
	// valor discreto → rótulo `case` duplicado que não compila. Sempre
	// error. Só no switch; na cadeia, a mesma colisão vira ramo
	// inalcançável (warning).
	KindCaseDuplicateValue = "case_duplicate_value"

	// KindCaseEmptyRange — a `between` case whose lower bound exceeds its
	// upper bound (e.g. between 10 and 1). The generated condition compiles
	// but can never be true, so the branch is dead. Warning severity: the
	// code still generates.
	//
	// Português: Um `between` com limite inferior maior que o superior
	// (ex.: between 10 e 1). Compila mas nunca é verdadeiro — ramo morto.
	// Warning.
	KindCaseEmptyRange = "case_empty_range"

	// KindCaseUnreachable — a case in the if/else-if lowering whose every
	// matching value is already claimed by an earlier case (first-match-
	// wins), so its branch can never execute. Warning severity: the code
	// still generates; the branch is simply dead.
	//
	// Português: Um case (na cadeia if/else-if) cujos valores já são
	// cobertos por um case anterior (first-match-wins) — o ramo nunca
	// executa. Warning.
	KindCaseUnreachable = "case_unreachable"

	// KindAssetTooLarge — a Data · File / Data · Text device carries more
	// bytes than the flash-asset ceiling (ir.DataBlobMaxBytes, 2 MB). The
	// blob is NOT emitted: on the small targets these arrays exist for,
	// oversize assets brick the build at link time with a far worse
	// message; refusing early with the device named is the kind path.
	// Error severity — generation proceeds so every oversize device is
	// reported in one pass, but the result must not ship.
	//
	// Português: Um device Data · File / Data · Text carrega mais bytes
	// que o teto de asset de flash (2 MB). O blob NÃO é emitido — nos
	// alvos pequenos para os quais esses arrays existem, um asset
	// gigante quebra o build no link com mensagem bem pior; recusar cedo
	// nomeando o device é o caminho gentil. Severidade Error.
	KindAssetTooLarge = "asset_too_large"

	// KindSequenceOrderViolation — a wire inside a StatementSequence flows
	// BACKWARD: its producer sits in a later phase than its consumer. The
	// Sequence's whole promise is 0→1→2; a backward wire is a drawn
	// contradiction. Error severity; emission proceeds so every violation
	// is reported in one pass, but the result must not ship.
	//
	// Português: Fio PARA TRÁS dentro de um Sequence — produtor em fase
	// posterior à do consumidor. A promessa do device é 0→1→2; fio para
	// trás é contradição desenhada. Severidade Error.
	KindSequenceOrderViolation = "sequence_order_violation"

	// KindMathOutputUnwired — a math or comparison device's output feeds
	// nothing. An unwired math output becomes a leaf assignment nothing
	// reads: C tolerates it (a warning), but Go REJECTS unused locals at
	// build time — so the generated program would not compile. Decision
	// 2026-06-30 (§7.5): validation error BEFORE generation, enforced in
	// the ir (language-agnostic — both backends inherit it, the parity
	// rule). No instruction is emitted for the offender.
	//
	// Português: Saída de math/comparação sem fio — vira atribuição-folha
	// que nada lê; C tolera, Go REJEITA locals não usados. Erro de
	// validação ANTES da geração, imposto no ir (as duas linguagens
	// herdam). O infrator não emite instrução.
	KindMathOutputUnwired = "math_output_unwired"

	// KindFunctionNameInvalid — a Function container's name is empty or
	// not a valid C identifier. The name is emitted VERBATIM into both
	// targets (doctrine 1: prefix-exempt), so there is no sanitizer to
	// hide behind: the maker owns the name, the validator owns the
	// refusal. Error; the region is not emitted. Português: Nome vazio ou
	// inválido como identificador C — emitido verbatim, então recusa
	// clara em vez de sanitização escondida.
	KindFunctionNameInvalid = "function_name_invalid"

	// KindFunctionUncalled — a Function region emitted on a target where
	// nothing calls it (posix in slice 2). WARNING, deliberately loud
	// (upgraded from info by the portability-tiers rule): a PC test must
	// not silently diverge from the embedded behaviour; the body remains
	// manually testable from a harness. Português: Função sem chamador
	// neste alvo — warning barulhento por decisão (tiers): teste no PC
	// não pode divergir em silêncio.
	KindFunctionUncalled = "function_uncalled"

	// KindFunctionNested — a Function container inside another Function's
	// scope. C has no nested functions; v1 refuses with the offender
	// named. Português: Function dentro de Function — C não tem funções
	// aninhadas; v1 recusa nomeando.
	KindFunctionNested = "function_nested"
)

// Diagnostic is a single structured issue produced anywhere in the
// codegen pipeline. The UI uses Devices and Scope to highlight affected
// nodes on the canvas and to pan/zoom when the user clicks a row in the
// error panel; Message carries the human-readable text.
//
// Português:
//
//	Problema estruturado gerado em qualquer etapa do codegen. A UI usa
//	Devices e Scope pra destacar nós no canvas e dar pan/zoom quando o
//	usuário clica na linha; Message é o texto pro humano.
type Diagnostic struct {
	// Kind is a machine-readable category. One of the Kind* constants
	// above. The UI dispatches on this to choose an icon, a colour, and
	// a "Learn more" link.
	Kind string `json:"kind"`

	// Severity is "error" or "warning". Errors block codegen; warnings
	// do not. When both are present, codegen still completes.
	Severity string `json:"severity"`

	// Devices lists the device IDs involved in the issue. For a
	// geometric overlap this is both peers; for scope crossing it is
	// the producer and every consumer; for a missing connection it is
	// the single device whose port is unwired.
	//
	// Order matters: the first entry is the primary subject (the one
	// the UI should focus when the diagnostic is clicked).
	Devices []string `json:"devices,omitempty"`

	// Scope is the container device ID whose scope the diagnostic
	// relates to — typically a Loop or IfElse. Empty string means
	// the global scope. Empty means not applicable.
	Scope string `json:"scope,omitempty"`

	// Message is the human-readable explanation. Should name the
	// devices and tell the user what to change; the UI shows it
	// verbatim, so keep it actionable.
	Message string `json:"message"`
}

// Error reports whether the diagnostic is a blocking error.
func (d Diagnostic) Error() bool { return d.Severity == SeverityError }

// HasErrors returns true if any diagnostic in the slice is an error.
func HasErrors(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Error() {
			return true
		}
	}
	return false
}

// Messages returns the Message field of every diagnostic, in order.
// Convenience for code that still needs a plain []string (legacy
// Response.Errors compatibility, test assertions, logging).
func Messages(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Message)
	}
	return out
}

// ErrorMessages is like Messages but filters to severity=="error".
func ErrorMessages(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		if d.Error() {
			out = append(out, d.Message)
		}
	}
	return out
}

// WarningMessages is like Messages but filters to severity=="warning".
func WarningMessages(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		if d.Severity == SeverityWarning {
			out = append(out, d.Message)
		}
	}
	return out
}
