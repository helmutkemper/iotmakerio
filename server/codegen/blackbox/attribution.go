// server/codegen/blackbox/attribution.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"sort"
	"strings"
)

// AuthorLine returns a single "//" comment line attributing one black-box's
// inlined code to its author, terminated by a newline so it sits on its own
// line, e.g.
//
//	// authored by amy (https://github.com/amy/sensor-lib)
//
// It returns "" when the def has no author (the maker's own code, or a
// component with no provenance), so callers can prepend it unconditionally. It
// is placed directly on the author's generated code — the struct/methods the Go
// backend inlines, or the verbatim source the C99 backend inlines — so a reader
// scanning the file sees, right there, whose code each block is. This is the
// per-component counterpart to AttributionManifest's top-of-file summary. Valid
// in both Go and C99.
//
// Português: Retorna uma linha de comentário "//" atribuindo o código inlinado
// de uma black-box ao seu autor, com quebra de linha para ficar sozinha. Vazia
// quando o def não tem autor, para o chamador prepender sem condicional. Fica
// direto no código gerado do autor (struct/métodos inlinados pelo backend Go,
// ou o source verbatim inlinado pelo backend C99), então quem lê o arquivo vê,
// ali, de quem é cada bloco. É a contraparte por-componente do resumo de topo
// do AttributionManifest.
func AuthorLine(def *BlackBoxDef) string {
	if def == nil || def.Author == nil {
		return ""
	}
	a := def.Author
	switch {
	case a.Username != "" && a.URL != "":
		return "// authored by " + a.Username + " (" + a.URL + ")\n"
	case a.Username != "":
		return "// authored by " + a.Username + "\n"
	case a.URL != "":
		return "// authored by " + a.URL + "\n"
	default:
		return ""
	}
}

// AttributionManifest builds the contributor block appended to a generated
// file's header: a deduplicated, deterministically-ordered list of the authors
// whose black-boxes contributed code to the file, each with their GitHub
// username and source URL. It returns "" when no def carries attribution (the
// maker used only their own code, or the components had no provenance), so the
// caller can append it unconditionally.
//
// The output uses "//" line comments, which are valid in both Go and C99, so a
// single implementation serves both backends.
//
// Determinism matters: the same scene must always produce byte-identical output
// (for response caching and golden tests), so authors are SORTED rather than
// emitted in Go map-iteration order.
//
// Português: Monta o bloco de contribuidores anexado ao header do arquivo
// gerado: a lista deduplicada e ordenada dos autores cujas black-boxes
// contribuíram código, cada um com username + URL do GitHub. Retorna "" quando
// nenhum def tem atribuição, para o chamador anexar sem condicional. Usa
// comentários "//", válidos em Go e C99, então uma implementação serve os dois
// backends. Ordenação determinística: a mesma cena sempre gera saída
// byte-a-byte idêntica (a ordem de iteração de map em Go não é determinística).
func AttributionManifest(defs map[string]*BlackBoxDef) string {
	// Deduplicate by the (username, url) pair. A struct key keeps both parts
	// distinct, so two repositories from the same user — or two users with the
	// same repository name — remain separate entries.
	type key struct{ username, url string }
	seen := make(map[key]bool)
	var authors []key

	for _, def := range defs {
		if def == nil || def.Author == nil {
			continue
		}
		a := def.Author
		if a.Username == "" && a.URL == "" {
			continue
		}
		k := key{a.Username, a.URL}
		if seen[k] {
			continue
		}
		seen[k] = true
		authors = append(authors, k)
	}

	if len(authors) == 0 {
		return ""
	}

	sort.Slice(authors, func(i, j int) bool {
		if authors[i].username != authors[j].username {
			return authors[i].username < authors[j].username
		}
		return authors[i].url < authors[j].url
	})

	var b strings.Builder
	b.WriteString("//\n")
	b.WriteString("// Portions of this file are generated from components authored by others.\n")
	b.WriteString("// Their authorship is preserved below; you hold the right to use, modify,\n")
	b.WriteString("// and distribute the generated result (see the notice above). Contributors:\n")
	b.WriteString("//\n")
	for _, a := range authors {
		switch {
		case a.username != "" && a.url != "":
			b.WriteString("//   - " + a.username + " (" + a.url + ")\n")
		case a.username != "":
			b.WriteString("//   - " + a.username + "\n")
		default:
			b.WriteString("//   - " + a.url + "\n")
		}
	}
	return b.String()
}
