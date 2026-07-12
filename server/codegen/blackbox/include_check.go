// server/codegen/blackbox/include_check.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

// Export-time resolution check for LOCAL includes in authored C files.
//
// A quoted include must resolve to something the exported box folder will
// actually contain: another authored file, or the generated companion
// header of an attached asset (the unified-asset naming contract). Angle
// includes (<stdio.h>) are the platform's problem and are ignored.
//
// The check exists because the failure mode without it is the worst kind:
// a one-letter tab-name typo ("porta_api.h" holding portal_api.h content)
// or a forgotten asset exports SILENTLY and only explodes minutes later in
// the maker's compiler, far from the IDE (field report 2026-07-11). The
// message therefore names the file, the line, the include, and — when a
// near-miss exists — the suggestion.
//
// Português: Checagem de resolução, no export, dos includes LOCAIS dos
// arquivos C autorais. Include entre aspas precisa resolver para algo que a
// pasta exportada da caixa vai realmente conter: outro arquivo autoral, ou
// o header-companheiro gerado de um asset anexado (contrato de nomes do
// modelo unificado). Includes de ângulo (<stdio.h>) são problema da
// plataforma e são ignorados. A checagem existe porque a falha sem ela é da
// pior espécie: um typo de uma letra no nome da aba ("porta_api.h" com
// conteúdo de portal_api.h) ou um asset esquecido exporta EM SILÊNCIO e só
// explode minutos depois no compilador do maker, longe da IDE (report de
// campo 2026-07-11). Por isso a mensagem nomeia arquivo, linha, include e —
// quando há quase-acerto — a sugestão.

package blackbox

import (
	"fmt"
	"regexp"
	"strings"
)

// IncludeIssue describes one unresolved local include.
// Português: Descreve um include local não resolvido.
type IncludeIssue struct {
	File       string // authored file containing the include | arquivo autoral
	Line       int    // 1-based line of the #include | linha (base 1)
	Include    string // the quoted path as written | o caminho como escrito
	Suggestion string // nearest candidate, "" when none | candidato próximo
}

// Message renders the issue as one maker-facing line.
// Português: Renderiza o problema em uma linha para o maker.
func (i IncludeIssue) Message() string {
	msg := fmt.Sprintf("%s:%d: #include %q does not match any file of this device",
		i.File, i.Line, i.Include)
	if i.Suggestion != "" {
		msg += fmt.Sprintf(" — did you mean %q?", i.Suggestion)
	}
	return msg
}

// MissingFunctionSources returns, for every parsed function of the def,
// the ones whose SourceFile is NOT among def.Files — the signature of a
// HYBRID def: functions captured at parse time riding a stored version
// that never received the file (the maker parsed, dragged and exported
// without saving). Exporting such a def links a call to a body that never
// ships (field report 2026-07-11: portal_server_start referenced by main,
// portal_server.c absent from the box). One entry per offending file.
//
// Português: Retorna, das funções parseadas do def, as cujo SourceFile
// NÃO está em def.Files — a assinatura de um def HÍBRIDO: funções
// capturadas no parse montadas numa versão salva que nunca recebeu o
// arquivo (o maker parseou, arrastou e exportou sem salvar). Exportar
// isso linka uma chamada a um corpo que nunca embarca (report de campo
// 2026-07-11). Uma entrada por arquivo faltante.
func (d *BlackBoxDef) MissingFunctionSources() []string {
	if d == nil || len(d.Functions) == 0 {
		return nil
	}
	have := make(map[string]bool, len(d.Files))
	for _, f := range d.Files {
		have[f.Path] = true
	}
	seen := map[string]bool{}
	var missing []string
	for _, fn := range d.Functions {
		sf := fn.SourceFile
		if sf == "" || have[sf] || seen[sf] {
			continue
		}
		seen[sf] = true
		missing = append(missing, sf)
	}
	return missing
}

var quotedIncludeRe = regexp.MustCompile(`^\s*#\s*include\s+"([^"]+)"`)

// MissingLocalIncludes scans every authored C file of the def and returns
// the quoted includes that will NOT resolve inside the exported box folder.
// Resolvable targets are: (a) any authored file path (def.Files), (b) the
// generated companion header of any attached asset (AssetHeaderPath over
// def.Assets). Order of issues follows file order, then line order.
//
// Português: Varre todo arquivo C autoral do def e retorna os includes
// entre aspas que NÃO resolverão dentro da pasta exportada da caixa. Alvos
// resolvíveis: (a) qualquer caminho de arquivo autoral (def.Files), (b) o
// header-companheiro gerado de qualquer asset anexado (AssetHeaderPath
// sobre def.Assets). A ordem segue arquivo, depois linha.
func (d *BlackBoxDef) MissingLocalIncludes() []IncludeIssue {
	if d == nil || len(d.Files) == 0 {
		return nil
	}

	resolvable := make(map[string]bool, len(d.Files)+len(d.Assets))
	candidates := make([]string, 0, len(d.Files)+len(d.Assets))
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || resolvable[p] {
			return
		}
		resolvable[p] = true
		candidates = append(candidates, p)
	}
	for _, f := range d.Files {
		add(f.Path)
	}
	for _, a := range d.Assets {
		add(AssetHeaderPath(a.Path))
	}

	var issues []IncludeIssue
	for _, f := range d.Files {
		lower := strings.ToLower(f.Path)
		if !strings.HasSuffix(lower, ".c") && !strings.HasSuffix(lower, ".h") {
			continue
		}
		for n, line := range strings.Split(f.Content, "\n") {
			m := quotedIncludeRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			inc := m[1]
			if resolvable[inc] {
				continue
			}
			issues = append(issues, IncludeIssue{
				File:       f.Path,
				Line:       n + 1,
				Include:    inc,
				Suggestion: nearestPath(inc, candidates),
			})
		}
	}
	return issues
}

// nearestPath returns the candidate closest to want by edit distance, but
// only when the distance is small relative to the name (a one- or
// two-letter slip), so the suggestion never turns into a wild guess.
//
// Português: Retorna o candidato mais próximo por distância de edição, mas
// só quando a distância é pequena em relação ao nome (escorregão de uma ou
// duas letras) — a sugestão nunca vira chute selvagem.
func nearestPath(want string, candidates []string) string {
	best, bestDist := "", 1<<30
	for _, c := range candidates {
		d := editDistance(want, c)
		if d < bestDist {
			best, bestDist = c, d
		}
	}
	limit := len(want) / 4
	if limit < 2 {
		limit = 2
	}
	if best != "" && bestDist <= limit {
		return best
	}
	return ""
}

// editDistance is a plain Levenshtein — the inputs are file names, so the
// quadratic cost is irrelevant.
// Português: Levenshtein simples — entradas são nomes de arquivo, custo
// quadrático é irrelevante.
func editDistance(a, b string) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			m := prev[j] + 1
			if cur[j-1]+1 < m {
				m = cur[j-1] + 1
			}
			if prev[j-1]+cost < m {
				m = prev[j-1] + cost
			}
			cur[j] = m
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}
