// server/codegen/blackbox/parser_c_files.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "fmt"

// Multi-file C parsing.
//
// English:
//
//	A specialist's device project is a SET of files (api.h, core.c,
//	util.c). ParseCFiles is the entry point for that shape: it runs the
//	single-file ParseC over each entry and MERGES the partial defs into
//	one. The single-file parser stays the primitive — its phases (strip,
//	structs, functions, enums, callbacks) are per-translation-unit by
//	nature, and includes are NOT resolved (the parser never expands
//	`#include "api.h"`, so parsing api.h separately is what makes its
//	types visible, once, without duplication).
//
//	Merge rules — keep-first, in file order (tab order):
//
//	  Doc            first non-empty package doc wins (the first file is
//	                 the specialist's "front page" by convention).
//	  Imports        union, first-seen order, deduplicated.
//	  Functions      deduplicated by NAME; a DEFINITION upgrades a bare
//	                 PROTOTYPE in place (header-first is the standard C
//	                 layout, and annotations live on the definition),
//	                 keeping the position of first sighting. Among two
//	                 definitions, first wins — a true duplicate is a C
//	                 link error the specialist owns; the tolerant stance
//	                 keeps rendering and the maker's compiler tells the
//	                 rest of the story.
//	  WireTypes      deduplicated by Name (tag).
//	  Enums          deduplicated by Name; anonymous enums always append
//	                 (their constants are the identity, not the name).
//	  CallbackTypes  deduplicated by Name.
//	  ExternalNames  union, first-seen order (see parser_c_external.go).
//	  Files          the input set, verbatim — the def carries its own
//	                 source of truth for the emitter.
//
// Português:
//
//	O projeto do especialista é um CONJUNTO de arquivos. ParseCFiles roda
//	o ParseC de arquivo único sobre cada entrada e FUNDE os defs parciais.
//	O parser de um arquivo continua sendo a primitiva — as fases são por
//	unidade de tradução, e includes NÃO são resolvidos (parsear o api.h
//	separadamente é o que torna seus tipos visíveis, uma vez, sem
//	duplicação). Regras do merge: keep-first na ordem dos arquivos (= das
//	abas); duplicata real de definição entre arquivos é erro de link do C
//	que pertence ao especialista — a postura tolerante mantém a primeira e
//	o compilador do maker conta o resto.

// ParseCFiles parses every authored file and merges the results into one
// BlackBoxDef carrying the full snapshot (def.Files). A hard parse error
// (structurally impossible input, e.g. unterminated braces) aborts with the
// offending PATH wrapped in, so the wizard can point at the right tab.
// An empty input yields an empty def, mirroring ParseC's contract.
//
// Português: Parseia cada arquivo e funde os resultados em um def com o
// snapshot completo. Erro duro aborta com o CAMINHO do arquivo no erro,
// para o wizard apontar a aba certa. Entrada vazia → def vazio.
func ParseCFiles(files []FileEntry, limits ParserLimits) (*BlackBoxDef, error) {
	merged := &BlackBoxDef{Files: files}
	if len(files) == 0 {
		return merged, nil
	}

	// seenFn maps a function name to its index in merged.Functions so a
	// later DEFINITION can upgrade an earlier PROTOTYPE in place — the
	// standard C layout (header first, .c after) must not cost the
	// function its annotations. Position stays at first sighting: the
	// header declares the box's reading order; the .c supplies the meat.
	//
	// Português: Nome → índice em merged.Functions, para uma DEFINIÇÃO
	// posterior promover um PROTÓTIPO anterior no lugar — o layout
	// padrão do C (header primeiro) não pode custar as anotações. A
	// posição fica na primeira aparição: o header dita a ordem de
	// leitura; o .c traz a carne.
	seenFn := make(map[string]int)
	seenWire := make(map[string]bool)
	seenEnum := make(map[string]bool)
	seenCb := make(map[string]bool)
	seenImport := make(map[string]bool)
	seenExt := make(map[string]bool)

	for _, f := range files {
		part, err := ParseC([]byte(f.Content), limits)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Path, err)
		}

		// Provenance: every function this file's parse produced belongs
		// to this file. The stamp travels through the merge naturally —
		// a definition upgrading a prototype REPLACES the entry, so the
		// merged function points at the definition's file, exactly where
		// its annotations live.
		//
		// Português: Proveniência: toda função que o parse deste arquivo
		// produziu pertence a ele. O carimbo atravessa o merge sozinho —
		// a definição SUBSTITUI a entrada do protótipo, então a função
		// fundida aponta para o arquivo da definição, onde moram as
		// anotações.
		for i := range part.Functions {
			part.Functions[i].SourceFile = f.Path
		}

		if merged.Doc == "" {
			merged.Doc = part.Doc
		}
		for _, imp := range part.Imports {
			if !seenImport[imp] {
				seenImport[imp] = true
				merged.Imports = append(merged.Imports, imp)
			}
		}
		for _, fn := range part.Functions {
			if idx, dup := seenFn[fn.Name]; dup {
				// Same name seen again. A definition upgrades a bare
				// prototype (annotations live on the definition); every
				// other duplicate keeps the first — a true double
				// definition is the specialist's link error to own, and
				// the tolerant stance renders SOMETHING while the
				// maker's compiler tells the rest of the story.
				if fn.HasBody && !merged.Functions[idx].HasBody {
					merged.Functions[idx] = fn
				}
				continue
			}
			seenFn[fn.Name] = len(merged.Functions)
			merged.Functions = append(merged.Functions, fn)
		}
		for _, wt := range part.WireTypes {
			if !seenWire[wt.Name] {
				seenWire[wt.Name] = true
				merged.WireTypes = append(merged.WireTypes, wt)
			}
		}
		for _, en := range part.Enums {
			// Anonymous enums have no name identity — their constants are
			// what matters, and two anonymous enums are two enums.
			if en.Name == "" || !seenEnum[en.Name] {
				if en.Name != "" {
					seenEnum[en.Name] = true
				}
				merged.Enums = append(merged.Enums, en)
			}
		}
		for _, cb := range part.CallbackTypes {
			if !seenCb[cb.Name] {
				seenCb[cb.Name] = true
				merged.CallbackTypes = append(merged.CallbackTypes, cb)
			}
		}
		for _, name := range part.ExternalNames {
			if !seenExt[name] {
				seenExt[name] = true
				merged.ExternalNames = append(merged.ExternalNames, name)
			}
		}
	}
	return merged, nil
}
