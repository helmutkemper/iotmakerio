// devices/comment.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package devices

import "strings"

// CommentPrefix turns the maker's Inspect comment into `// ` source lines
// (one per comment line, trailing newline), ready to prefix a device's
// Code Preview. Returns "" for an empty/blank comment so previews can
// concatenate it unconditionally.
//
// This mirrors what the REAL pipeline does: the IR stamps the comment on the
// node's first instruction (Meta["comment"]) and the backends prefix it the
// same way — the preview and the generated code stay in visual agreement.
//
// Português: Converte o comentário do Inspect do maker em linhas `// ` (uma
// por linha do comentário, com quebra final), prontas para prefixar o Code
// Preview de um device. Retorna "" para comentário vazio, então os previews
// concatenam sem condicional. Espelha o pipeline REAL: o IR carimba o
// comentário na primeira instrução do node (Meta["comment"]) e os backends
// prefixam do mesmo jeito — preview e código gerado ficam visualmente de
// acordo.
func CommentPrefix(comment string) string {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return ""
	}
	var sb strings.Builder
	for _, line := range strings.Split(comment, "\n") {
		sb.WriteString("// ")
		sb.WriteString(strings.TrimRight(line, " \t"))
		sb.WriteString("\n")
	}
	return sb.String()
}
