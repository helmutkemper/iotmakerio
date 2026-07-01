// /server/codegen/casepreview.go

package codegen

// casepreview.go — Source-code preview for a single StatementCase device.
//
// PreviewCase is the preview half of the same authority validate() uses. It
// shares the two functions that decide everything semantic about a Case:
//
//   - ir.UseSwitchLowering  — switch vs if/else-if (so the previewed shape
//     matches what emitCase emits); and
//   - ir.BuildCaseCondition — the boolean expression of each chain branch
//     (so `between 1,10` previews as the exact `selector >= 1 && selector <=
//     10` the backends generate).
//
// What PreviewCase adds on top is only the *scaffold*: the `switch`/`case`/
// `default` and `if`/`else if`/`else` keywords, braces and (for C) `break;`.
// That scaffold is duplicated from the two backend emitters by necessity —
// the backends render a whole standalone program, and their per-construct
// renderers are unexported, so an isolated snippet cannot call them. The
// duplication is kept minimal and faithful: renderCasePreview mirrors
// backend/golang/emit.go (emitSwitchBegin/emitCaseLabel/…/emitCondLabel) and
// backend/ansic/emit.go line for line. Anything that could actually drift —
// the conditions and the lowering choice — comes from the shared ir helpers,
// not from here. If a backend ever changes its scaffold (it renders stable
// language keywords, so this is unlikely), the goldens in casepreview_test.go
// fail and point here.
//
// The bodies are placeholders: the maker is previewing the case STRUCTURE
// (which values route to which branch), not the blocks inside each branch.
// The selector is rendered as the identifier `selector` because the preview
// has no wire from which to resolve the real variable name.
//
// Português: Preview do código de UM StatementCase. Usa as MESMAS funções do
// pipeline real para tudo que é semântico — ir.UseSwitchLowering (switch vs
// cadeia) e ir.BuildCaseCondition (a condição de cada ramo) — de modo que a
// forma do preview bate com o que o emitCase gera. O que o PreviewCase
// acrescenta é só o scaffold (palavras-chave/chaves/`break;`), espelhado dos
// dois backends linha a linha porque os renderizadores deles são privados e
// produzem um programa inteiro, não um trecho. O que poderia divergir
// (condições, escolha do lowering) vem dos helpers compartilhados. Corpos são
// placeholders; o seletor é o identificador `selector` (não há fio no preview).

import (
	"fmt"
	"strings"

	"server/codegen/graph"
	"server/codegen/ir"
)

const (
	// previewSelector is the identifier used in place of the wire-resolved
	// selector variable, which does not exist in the preview context.
	previewSelector = "selector"

	// previewScopeID is the diagnostic scope used when the caller supplies
	// no device ID (e.g. unit tests).
	previewScopeID = "preview"
)

// PreviewCase renders the source a StatementCase lowers to, in the target
// language, plus the cross-case diagnostics. scopeID is the StatementCase
// device ID (carried on the diagnostics so the panel can attribute them);
// language selects the syntax ("c"/"c99"/"ansic" → C, anything else → Go);
// selectorType distinguishes bool (no cross-case diagnostics) from an integer
// selector; cases is the device's ordered case list.
//
// The returned code is a snippet (not a standalone program) so it drops
// straight into the inspect panel's preview tab.
//
// Português: Renderiza o código que um StatementCase gera, no idioma alvo,
// mais os diagnósticos cruzados. É um trecho (não programa completo) para a
// aba de preview do painel.
func PreviewCase(scopeID, language, selectorType string, cases []graph.CaseDef) (code string, diags []Diagnostic) {
	if scopeID == "" {
		scopeID = previewScopeID
	}
	diags = ValidateCases(scopeID, selectorType, cases)
	code = renderCasePreview(cases, isCLanguage(language))
	return code, diags
}

// isCLanguage reports whether the language identifier selects the C backend.
// Mirrors the dispatch in Generate ("c"), and also accepts the friendlier
// "c99"/"ansic" spellings so a caller cannot accidentally preview C code as
// Go over a naming mismatch.
//
// Português: Diz se o identificador de linguagem seleciona o backend C.
func isCLanguage(language string) bool {
	switch language {
	case "c", "c99", "ansic":
		return true
	default:
		return false
	}
}

// renderCasePreview renders the switch or if/else-if scaffold for cases. isC
// selects C syntax (parenthesised conditions/selector, one `case` line per
// value, a `{ … break; }` block per case body) over Go syntax (bare
// condition/selector, comma-joined `case` values, no break). The lowering
// choice and the branch conditions come from the shared ir helpers; only the
// scaffold lives here. Non-default cases render in declared order and the
// default renders last, exactly as emitCase orders them.
//
// Português: Renderiza o scaffold switch ou if/else-if. isC escolhe a sintaxe
// C (condições/seletor entre parênteses, um `case` por valor, bloco
// `{ … break; }` por corpo) vs. Go (condição/seletor nu, valores com vírgula,
// sem break). A escolha do lowering e as condições vêm dos helpers do ir; só o
// scaffold mora aqui. Não-default na ordem declarada e default por último,
// como o emitCase ordena.
func renderCasePreview(cases []graph.CaseDef, isC bool) string {
	var b strings.Builder
	indent := 0
	write := func(format string, args ...interface{}) {
		for i := 0; i < indent; i++ {
			b.WriteByte('\t')
		}
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}
	body := func(c graph.CaseDef) { write("%s", caseBodyComment(c)) }

	if ir.UseSwitchLowering(cases) {
		if isC {
			write("switch (%s) {", previewSelector)
		} else {
			write("switch %s {", previewSelector)
		}

		emitCase := func(c graph.CaseDef, isDefault bool) {
			if isC {
				// C: one `case <v>:` per value, then a brace block whose body
				// ends with `break;` — matching backend/ansic emitCaseLabel.
				if isDefault {
					write("default:")
				} else {
					for _, v := range c.Values {
						write("case %s:", strings.TrimSpace(v))
					}
				}
				write("{")
				indent++
				body(c)
				write("break;")
				indent--
				write("}")
				return
			}
			// Go: a single comma-joined `case` (or `default`), body indented —
			// matching backend/golang emitCaseLabel.
			if isDefault {
				write("default:")
			} else {
				write("case %s:", joinTrimmed(c.Values, ", "))
			}
			indent++
			body(c)
			indent--
		}

		for _, c := range cases {
			if !c.IsDefault {
				emitCase(c, false)
			}
		}
		for _, c := range cases {
			if c.IsDefault {
				emitCase(c, true)
				break
			}
		}
		write("}")
		return strings.TrimRight(b.String(), "\n")
	}

	// if/else-if chain. The shape is identical in Go and C; only C
	// parenthesises the condition. A Case reaches this lowering only with at
	// least one non-default branch (a range/comparison forces it), so the
	// leading `if` is always emitted before any `else`.
	cond := func(c graph.CaseDef) string {
		expr := ir.BuildCaseCondition(previewSelector, c.MatchKind, c.Values)
		if isC {
			return "(" + expr + ")"
		}
		return expr
	}
	first := true
	for _, c := range cases {
		if c.IsDefault {
			continue
		}
		if first {
			write("if %s {", cond(c))
			first = false
		} else {
			write("} else if %s {", cond(c))
		}
		indent++
		body(c)
		indent--
	}
	for _, c := range cases {
		if c.IsDefault {
			write("} else {")
			indent++
			body(c)
			indent--
			break
		}
	}
	write("}")
	return strings.TrimRight(b.String(), "\n")
}

// caseBodyComment is the placeholder shown in place of a case's real blocks.
// The preview shows the decision STRUCTURE (which values route to which
// branch), not the blocks inside each branch, so each body is a single comment
// line. It echoes the case's inspector label (e.g. "// condição 0") so the
// maker can line up each branch in the snippet with the row that produced it;
// when a case has no label it falls back to a neutral ellipsis.
//
// The label is user text, so it is flattened to a single line — a stray
// newline would otherwise break out of the // comment in the rendered snippet.
//
// Português: Placeholder no lugar dos blocos reais. O preview mostra a
// ESTRUTURA, não os blocos — então o corpo é uma linha de comentário com o
// LABEL do case do inspetor ("// condição 0"), para o maker casar cada ramo do
// snippet com a linha que o gerou; sem label, cai numa reticência neutra. O
// label é texto do usuário, então é achatado para uma única linha.
func caseBodyComment(c graph.CaseDef) string {
	label := strings.TrimSpace(c.Label)
	label = strings.ReplaceAll(label, "\r", " ")
	label = strings.ReplaceAll(label, "\n", " ")
	if label == "" {
		return "// ..."
	}
	return "// " + label
}

// joinTrimmed trims each value and joins them with sep (used for Go's
// comma-separated `case` label).
//
// Português: Apara cada valor e junta com sep (rótulo `case` do Go).
func joinTrimmed(values []string, sep string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strings.TrimSpace(v)
	}
	return strings.Join(parts, sep)
}
