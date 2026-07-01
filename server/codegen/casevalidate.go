// /server/codegen/casevalidate.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// casevalidate.go — Cross-case validation for a single StatementCase device.
//
// This is the ONE place that decides whether a maker's set of cases is sound,
// and it is reached from both code paths that matter:
//
//   - the real pipeline: validate() in codeGen.go calls ValidateCases for
//     every StatementCase scope, so a full "Generate Code" run reports the
//     same problems; and
//   - the inspect-panel preview: the async preview task lowers the draft cases
//     and runs ValidateCases over them, so the maker sees the verdict while
//     editing, BEFORE saving.
//
// Because both callers share this function — and share ir.UseSwitchLowering
// for the switch-vs-chain decision and ir.BuildCaseCondition for the lowered
// expression — the preview and the generated code can never disagree. The
// codegen is the source of truth; this file does not re-implement its rules,
// it asks the same helpers the emitter uses.
//
// What it checks (integer selectors only — a bool selector is exhaustive and
// has nothing to collide):
//
//   1. Switch lowering (every non-default case discrete): two cases claiming
//      the same value would emit a duplicate `case` label that neither Go nor
//      C compiles → KindCaseDuplicateValue, ERROR (blocks generation).
//   2. Chain lowering (any range/comparison present, first-match-wins):
//        a. a `between` whose lower bound exceeds its upper bound is an empty
//           range that never matches → KindCaseEmptyRange, WARNING; and
//        b. a case whose every matching value is already covered by an earlier
//           case can never execute → KindCaseUnreachable, WARNING.
//
// Reachability is computed over closed integer intervals (comparisons become
// half-open intervals clamped to the int64 range). The analysis is SOUND by
// design: a case is flagged unreachable only when its whole value set is
// provably contained in the union of earlier cases, so a reachable case is
// never falsely warned about. Over-conservative gaps (a case left unflagged
// that a human might call redundant) are preferred to false positives, which
// would erode trust in the panel.
//
// Português: Validação cruzada dos cases de UM StatementCase. É o ÚNICO lugar
// que decide se o conjunto de cases é são, e é chamado tanto pelo pipeline
// real (validate() no codeGen.go) quanto pelo preview async do painel — então
// "Generate Code" e o preview nunca discordam. Não reimplementa as regras do
// codegen: usa os mesmos helpers do emitter (ir.UseSwitchLowering). Verifica,
// para selector inteiro: (1) no switch, valores discretos duplicados → rótulo
// `case` repetido que não compila → error; (2) na cadeia, `between` com mínimo
// maior que máximo (range vazio) → warning, e case totalmente coberto por um
// anterior (inalcançável) → warning. A análise de alcance usa intervalos de
// inteiros e é conservadora: só marca como inalcançável o que é provadamente
// coberto — nunca um falso positivo.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"server/codegen/diagnostics"
	"server/codegen/graph"
	"server/codegen/ir"
)

// ValidateCases returns the diagnostics for one StatementCase's ordered cases.
// scopeID is the StatementCase device ID (used as the diagnostic subject so
// the error panel can focus it); selectorType is the wire-derived selector
// type ("int", "bool", …); cases is the device's ordered case list.
//
// A nil/empty slice yields no diagnostics. A "bool" selector yields none — its
// true/false cases are exhaustive and lower to if/else with nothing to
// collide or shadow.
//
// Português: Diagnósticos dos cases de um StatementCase. scopeID é o ID do
// device (foco no painel); selectorType vem do fio; cases é a lista ordenada.
// bool → nenhum diagnóstico (true/false exaustivos).
func ValidateCases(scopeID, selectorType string, cases []graph.CaseDef) []Diagnostic {
	if selectorType == "bool" {
		return nil
	}
	if ir.UseSwitchLowering(cases) {
		return validateSwitchCases(scopeID, cases)
	}
	return validateChainCases(scopeID, cases)
}

// validateSwitchCases detects values claimed by more than one switch label.
// In the switch lowering every non-default case is discrete, so any value
// appearing in two cases — or twice inside one isAnyOf list — would emit a
// duplicate `case` label that does not compile. One error is produced per
// duplicated value, in first-seen order for deterministic output.
//
// Português: Detecta valores reivindicados por mais de um rótulo de switch.
// Um erro por valor duplicado, na ordem de primeira aparição.
func validateSwitchCases(scopeID string, cases []graph.CaseDef) []Diagnostic {
	// rec records every 1-based case position that claims a canonical value.
	type rec struct {
		key       string
		positions []int
	}
	order := make([]*rec, 0, len(cases))
	index := make(map[string]*rec, len(cases))

	for i, c := range cases {
		if c.IsDefault {
			continue
		}
		for _, raw := range c.Values {
			key := valueKey(raw)
			r, ok := index[key]
			if !ok {
				r = &rec{key: key}
				index[key] = r
				order = append(order, r)
			}
			r.positions = append(r.positions, i+1)
		}
	}

	var diags []Diagnostic
	for _, r := range order {
		if len(r.positions) < 2 {
			continue
		}
		uniq := uniqueInts(r.positions)
		var msg string
		if len(uniq) == 1 {
			// Same case lists the value more than once (e.g. isAnyOf 1, 1).
			msg = fmt.Sprintf(
				"Case #%d lists value %s more than once — a switch label cannot repeat a value.",
				uniq[0], r.key)
		} else {
			msg = fmt.Sprintf(
				"Value %s is claimed by cases %s — a switch cannot have duplicate case labels.",
				r.key, joinPositions(uniq))
		}
		diags = append(diags, Diagnostic{
			Kind:     diagnostics.KindCaseDuplicateValue,
			Severity: diagnostics.SeverityError,
			Devices:  []string{scopeID},
			Scope:    scopeID,
			Message:  msg,
		})
	}
	return diags
}

// validateChainCases reports empty `between` ranges and cases made unreachable
// by earlier ones under first-match-wins. Coverage accumulates in declared
// order across non-default cases; the trailing default is the catch-all and is
// excluded (it always matches whatever is left).
//
// Português: Reporta `between` vazio e cases inalcançáveis por um anterior
// (first-match-wins). A cobertura acumula na ordem declarada; o default
// (catch-all) é excluído.
func validateChainCases(scopeID string, cases []graph.CaseDef) []Diagnostic {
	var diags []Diagnostic
	var coverage []interval // union of every earlier non-default case

	for i, c := range cases {
		if c.IsDefault {
			continue
		}
		pos := i + 1
		ivls, emptyRange := caseIntervals(c)

		if emptyRange {
			diags = append(diags, Diagnostic{
				Kind:     diagnostics.KindCaseEmptyRange,
				Severity: diagnostics.SeverityWarning,
				Devices:  []string{scopeID},
				Scope:    scopeID,
				Message: fmt.Sprintf(
					"Case #%d (%s) is an empty range and never matches.",
					pos, describeCase(c)),
			})
			// An empty case covers nothing and is reported by the more
			// specific empty-range diagnostic; do not also flag it as
			// unreachable, and do not add it to the coverage.
			continue
		}

		// Unreachable only when the case actually has values to match AND all
		// of them already fall inside the accumulated coverage.
		if len(ivls) > 0 && coversAll(coverage, ivls) {
			diags = append(diags, Diagnostic{
				Kind:     diagnostics.KindCaseUnreachable,
				Severity: diagnostics.SeverityWarning,
				Devices:  []string{scopeID},
				Scope:    scopeID,
				Message: fmt.Sprintf(
					"Case #%d (%s) can never match — earlier cases already cover all its values.",
					pos, describeCase(c)),
			})
		}

		coverage = mergeIntervals(append(coverage, ivls...))
	}
	return diags
}

// ── interval model ───────────────────────────────────────────────────────

// interval is a closed integer interval [lo, hi] with lo <= hi. Comparison
// matchKinds become half-open ranges clamped to the int64 limits.
type interval struct{ lo, hi int64 }

const (
	minVal = math.MinInt64
	maxVal = math.MaxInt64
)

// caseIntervals returns the closed integer intervals a non-default case
// matches, and whether it is an empty `between` range (lo > hi). A malformed
// case (missing or unparseable operands) contributes no intervals and is not
// reported here — per-row well-formedness is the overlay's job and the
// emitter's BuildCaseCondition has its own safe fallbacks; double-reporting it
// would only add noise.
//
// Português: Intervalos fechados que um case não-default casa, e se é um
// `between` vazio (lo > hi). Case malformado não contribui intervalos e não é
// reportado aqui (a boa-formação por linha é do overlay).
func caseIntervals(c graph.CaseDef) (ivls []interval, emptyRange bool) {
	switch c.MatchKind {
	case "between":
		if len(c.Values) >= 2 {
			lo, okLo := parseVal(c.Values[0])
			hi, okHi := parseVal(c.Values[1])
			if okLo && okHi {
				if lo > hi {
					return nil, true
				}
				return []interval{{lo, hi}}, false
			}
		}
		return nil, false
	case "gt", "gte", "lt", "lte":
		if len(c.Values) >= 1 {
			if v, ok := parseVal(c.Values[0]); ok {
				return thresholdInterval(c.MatchKind, v), false
			}
		}
		return nil, false
	default: // "is", "isAnyOf", "" → discrete equality over each value
		for _, raw := range c.Values {
			if v, ok := parseVal(raw); ok {
				ivls = append(ivls, interval{v, v})
			}
		}
		return ivls, false
	}
}

// thresholdInterval maps a comparison matchKind to its half-open interval,
// guarding the int64 boundary so `> maxInt` and `< minInt` correctly yield the
// empty set rather than wrapping.
//
// Português: Mapeia um matchKind de comparação para seu intervalo semiaberto,
// protegendo o limite do int64 (> max e < min viram conjunto vazio).
func thresholdInterval(matchKind string, v int64) []interval {
	switch matchKind {
	case "gt":
		if v == maxVal {
			return nil
		}
		return []interval{{v + 1, maxVal}}
	case "gte":
		return []interval{{v, maxVal}}
	case "lt":
		if v == minVal {
			return nil
		}
		return []interval{{minVal, v - 1}}
	case "lte":
		return []interval{{minVal, v}}
	}
	return nil
}

// mergeIntervals returns the normalized union of in: sorted by lower bound,
// with overlapping or adjacent intervals merged. Adjacency (next.lo == cur.hi
// + 1) merges too, so {[1,5],[6,9]} becomes {[1,9]} — important because a case
// covering [1,5] followed by one covering [6,9] together cover [1,9], which
// can render a later [3,7] unreachable.
//
// Português: União normalizada de in (ordenada e com sobrepostos/adjacentes
// fundidos). Adjacência também funde, pois [1,5]+[6,9] cobrem [1,9].
func mergeIntervals(in []interval) []interval {
	if len(in) == 0 {
		return nil
	}
	cp := make([]interval, len(in))
	copy(cp, in)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].lo != cp[j].lo {
			return cp[i].lo < cp[j].lo
		}
		return cp[i].hi < cp[j].hi
	})
	out := []interval{cp[0]}
	for _, cur := range cp[1:] {
		last := &out[len(out)-1]
		// last.hi == maxVal already extends to the right edge, so anything
		// starting at or after it merges; otherwise test lo <= hi+1 without
		// overflowing past maxVal.
		if last.hi == maxVal || cur.lo <= last.hi+1 {
			if cur.hi > last.hi {
				last.hi = cur.hi
			}
		} else {
			out = append(out, cur)
		}
	}
	return out
}

// coversAll reports whether every interval in xs lies entirely within the
// union of coverage.
//
// Português: Diz se todo intervalo de xs está inteiramente dentro da união de
// coverage.
func coversAll(coverage []interval, xs []interval) bool {
	merged := mergeIntervals(coverage)
	for _, x := range xs {
		if !covered(merged, x) {
			return false
		}
	}
	return true
}

// covered reports whether x is contained in a single interval of merged (which
// is already normalized, so a value spanning two non-adjacent intervals is
// correctly reported as not covered).
//
// Português: Diz se x cabe num único intervalo de merged (já normalizado).
func covered(merged []interval, x interval) bool {
	for _, m := range merged {
		if m.lo <= x.lo && x.hi <= m.hi {
			return true
		}
	}
	return false
}

// ── small helpers ────────────────────────────────────────────────────────

// parseVal parses a trimmed base-10 int64, reporting success. Non-integer
// operands fail and are treated as "no interval" by callers.
//
// Português: Faz parse de int64 base-10 aparado; não-inteiros falham.
func parseVal(raw string) (int64, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// valueKey canonicalises a discrete operand for duplicate detection so that
// "5", " 5 ", "+5" and "05" collapse to one key. Non-integer operands fall
// back to their trimmed text.
//
// Português: Canoniza um operando discreto para detectar duplicatas ("5",
// " 5 ", "+5", "05" → mesma chave); não-inteiros usam o texto aparado.
func valueKey(raw string) string {
	if v, ok := parseVal(raw); ok {
		return strconv.FormatInt(v, 10)
	}
	return strings.TrimSpace(raw)
}

// uniqueInts returns the sorted, de-duplicated members of in.
//
// Português: Membros únicos e ordenados de in.
func uniqueInts(in []int) []int {
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

// joinPositions renders case positions as "#1, #2 and #4" for messages.
//
// Português: Formata posições de case como "#1, #2 and #4".
func joinPositions(positions []int) string {
	parts := make([]string, len(positions))
	for i, p := range positions {
		parts[i] = fmt.Sprintf("#%d", p)
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// describeCase renders a case's match expression in words for diagnostic
// messages (e.g. "is 5", "between 1 and 10", "≥ 3").
//
// Português: Descreve a expressão de match de um case por extenso para as
// mensagens (ex.: "is 5", "between 1 and 10", "≥ 3").
func describeCase(c graph.CaseDef) string {
	switch c.MatchKind {
	case "between":
		if len(c.Values) >= 2 {
			return "between " + strings.TrimSpace(c.Values[0]) + " and " + strings.TrimSpace(c.Values[1])
		}
		return "between"
	case "gt":
		return "> " + firstValue(c.Values)
	case "lt":
		return "< " + firstValue(c.Values)
	case "gte":
		return "\u2265 " + firstValue(c.Values) // ≥
	case "lte":
		return "\u2264 " + firstValue(c.Values) // ≤
	case "isAnyOf":
		return "is any of " + strings.Join(trimAll(c.Values), ", ")
	default: // "is", ""
		if len(c.Values) == 1 {
			return "is " + strings.TrimSpace(c.Values[0])
		}
		return "is any of " + strings.Join(trimAll(c.Values), ", ")
	}
}

// firstValue returns the trimmed first operand, or "?" when absent.
//
// Português: Primeiro operando aparado, ou "?" se ausente.
func firstValue(values []string) string {
	if len(values) == 0 {
		return "?"
	}
	return strings.TrimSpace(values[0])
}

// trimAll trims every operand for display.
//
// Português: Apara todos os operandos para exibição.
func trimAll(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.TrimSpace(v)
	}
	return out
}
