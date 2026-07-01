// /server/codegen/blackbox/parser_dispatch.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// parser_dispatch.go — Single language dispatch point for the black-box parser.
//
// English:
//
//	ParseForLanguage is the one place that maps a programming-language token
//	to its concrete parser (Parse for Go, ParseC for C99). Every caller that
//	holds a project's source plus its language — the wizard parse endpoint,
//	the project listing endpoint, the rewrite re-parse — routes through here
//	instead of writing its own switch. That keeps the language matrix in a
//	single location: adding Arduino or Python later is "write ParseX, add one
//	case here", and no caller needs to change.
//
//	Token matching is case-insensitive and trims whitespace. The accepted
//	spellings intentionally cover both the stage token space ("go") and the
//	programming_languages table id space ("golang"), because callers pull the
//	language from different storage layers that do not yet share a single
//	token vocabulary. An unknown token is a hard error — a silent fallback to
//	Go is exactly the bug this function exists to prevent (a C99 source parsed
//	as Go fails and the device silently disappears from the catalog).
//
// Português:
//
//	ParseForLanguage é o único lugar que mapeia o token de linguagem ao
//	parser concreto (Parse para Go, ParseC para C99). Todo chamador que tem
//	a fonte + a linguagem passa por aqui em vez de escrever o próprio switch.
//	Adicionar Arduino/Python depois é "escrever ParseX + um case aqui".
//	Token desconhecido é erro — o fallback silencioso pra Go é justamente o
//	bug que esta função evita.

import (
	"fmt"
	"strings"
)

// ParseForLanguage routes src to the parser for the given programming-language
// token and returns the resulting BlackBoxDef.
//
// Accepted tokens (case-insensitive, trimmed):
//
//	"", "go", "golang" → Go   (Parse)
//	"c", "c99"         → C99  (ParseC)
//
// Any other value returns an error; callers should surface it rather than
// guessing a language. Like Parse/ParseC, a non-nil def may accompany a
// non-nil error for sources with soft warnings — callers decide whether to
// treat a non-nil def as success.
//
// Português: Roteia src para o parser da linguagem dada. Tokens aceitos acima;
// qualquer outro vira erro. def != nil com err != nil é possível (avisos
// leves) — o chamador decide.
func ParseForLanguage(language string, src []byte, limits ParserLimits) (*BlackBoxDef, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "go", "golang":
		return Parse(src, limits)
	case "c", "c99":
		return ParseC(src, limits)
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}
}
