// server/codegen/blackbox/rewrite_c_callbacktype.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// rewrite_c_callbacktype.go — Rewrite handler for C99 callback-type
// cards (§12.3): `callbacktype.<Name>` sets the icon/label/comment
// block above a function-pointer typedef.
//
// English:
//
//	A callback type's wizard card exists for one purpose — visual
//	identity (icon + label); the signature is fixed by the typedef. So
//	the whole rewrite surface is a single path shape,
//	`callbacktype.<Name>`, handled exactly like the enum-level
//	directives in rewrite_c_enum.go: replace the leading-comment block
//	above the declaration, never emit a `// device:` line (a callback
//	type is a contract, not a function-group). Intercepted inside
//	RewriteC before the shared parsePath, so the Go router stays
//	untouched — same doctrine as the enum paths.
//
// Português:
//
//	O card do callback type existe para uma coisa só — identidade
//	visual; a assinatura é do typedef. A superfície de rewrite é um
//	caminho único, `callbacktype.<Name>`, tratado exatamente como o
//	nível-de-enum do rewrite_c_enum.go: substitui o bloco de comentário
//	líder acima da declaração, nunca emite `// device:` (callback type
//	é contrato, não function-group). Interceptado dentro do RewriteC
//	antes do parsePath compartilhado — mesma doutrina dos enums.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseCCallbackTypePath recognises `callbacktype.<Name>` and nothing
// deeper — the card has no rows, so no sub-paths exist by design.
//
// Português: Reconhece `callbacktype.<Nome>` e nada mais fundo — o card
// não tem linhas, então sub-caminhos não existem por design.
func parseCCallbackTypePath(s string) (string, bool) {
	const prefix = "callbacktype."
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(s, prefix)
	if name == "" || strings.Contains(name, ".") {
		return "", false
	}
	return name, true
}

// planCCallbackTypeEdit replaces the leading-comment block above the
// function-pointer typedef with the new icon/label/comment payload —
// the exact mirror of planCEnumDirectives. An all-empty payload clears
// the block, which flips the card back to "incomplete"
// (completion.go's callbacktype rule).
//
// Português: Substitui o bloco líder acima do typedef — espelho exato
// do planCEnumDirectives. Payload vazio limpa o bloco e o card volta a
// "incompleto".
func planCCallbackTypeEdit(source, name string, e WizardEdit) (cSplicePlan, error) {
	if e.Op != OpSetStructDirectives {
		return cSplicePlan{}, fmt.Errorf(
			"callbacktype paths only support setStructDirectives, got %q", e.Op)
	}
	var args struct {
		Label   string `json:"label"`
		Icon    string `json:"icon"`
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
	}

	declStart, ok := locateCCallbackTypedef(source, name)
	if !ok {
		return cSplicePlan{}, fmt.Errorf("callback type %q not found", name)
	}

	commentStart, commentEnd := findLeadingCommentRange(source, declStart)
	indent := indentOfLine(source, declStart)

	var directives []string
	if args.Label != "" {
		directives = append(directives, "label:"+args.Label+".")
	}
	if args.Icon != "" {
		directives = append(directives, "icon:"+args.Icon+".")
	}

	newBlock := renderCommentBlock(args.Comment, directives, indent)
	return cSplicePlan{start: commentStart, end: commentEnd, newText: newBlock}, nil
}

// locateCCallbackTypedef finds the byte offset of the `typedef` keyword
// that declares the function-pointer type `name`. It reuses the
// parser's own extractor (functionPointerTypedefs stamps DeclStart), so
// locate and parse can never disagree about which typedef a name means
// — the same single-source-of-truth instinct behind the enum locator.
//
// Português: Acha o offset do `typedef` que declara o tipo. Reusa o
// extrator do parser (DeclStart), então localizar e parsear nunca
// discordam sobre qual typedef um nome significa.
func locateCCallbackTypedef(source, name string) (int, bool) {
	stripped, _ := preprocessC(source)
	for _, ct := range functionPointerTypedefs(stripped) {
		if ct.Name == name {
			return ct.DeclStart, true
		}
	}
	return 0, false
}
