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

// ParseForLanguageFiles routes an authored file SET to the parser for the
// given programming-language token and returns the resulting BlackBoxDef.
// The set shape is the only input shape — a single-source caller passes one
// entry; there is deliberately no string-typed sibling to drift from this
// one (pre-release decision, 2026-07: no legacy surface to keep alive).
//
// Accepted tokens (case-insensitive, trimmed):
//
//	"", "go", "golang" → Go   (Parse — SINGLE file for now: multi-file Go
//	                     authoring means package-level parsing across
//	                     files and is a future slice of its own; more
//	                     than one .go file is a hard error so the wizard
//	                     says so instead of silently reading file #1)
//	"c", "c99"         → C99  (ParseCFiles — walks every file and merges;
//	                     see parser_c_files.go for the merge rules)
//
// Any other value returns an error; callers should surface it rather than
// guessing a language. Like Parse/ParseC, a non-nil def may accompany a
// non-nil error for sources with soft warnings — callers decide whether to
// treat a non-nil def as success.
//
// Português: Roteia o CONJUNTO de arquivos autorais para o parser da
// linguagem. É a única forma de entrada — chamador de fonte única passa uma
// entrada; não existe irmã string para divergir (decisão pré-release: sem
// legado). Go é um arquivo por enquanto (multiarquivo Go = parsing de
// pacote, fatia própria futura; mais de um .go é erro explícito). C caminha
// todos e funde. Token desconhecido é erro.
func ParseForLanguageFiles(language string, files []FileEntry, limits ParserLimits) (*BlackBoxDef, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "go", "golang":
		switch len(files) {
		case 0:
			// Empty set → empty def, no error — the "new project, nothing
			// typed yet" state, mirroring ParseC's empty-input contract.
			// (Go's AST parser errors on empty input; that error would be
			// noise here, not information.)
			return &BlackBoxDef{}, nil
		case 1:
			def, err := Parse([]byte(files[0].Content), limits)
			if def != nil {
				def.Files = files
				// Single-file provenance is trivial but stamped anyway:
				// one representation, one rule — consumers never need a
				// "was this multi-file?" special case.
				for i := range def.Functions {
					def.Functions[i].SourceFile = files[0].Path
				}
			}
			return def, err
		default:
			return nil, fmt.Errorf(
				"multi-file Go authoring is not supported yet (a future slice); keep a single .go file, got %d",
				len(files))
		}
	case "c", "c99":
		return ParseCFiles(files, limits)
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}
}
