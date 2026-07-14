// server/codegen/blackbox/parser_dispatch_assets_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestDispatchRecordsAssets: the dispatch choke point that filters
// non-source files away from the walkers RECORDS them on def.Assets —
// the Phase B dictionary lives or dies by this (field diagnosis
// 2026-07-13: the demo dictionary vanished before the parser and the
// resolver searched an emptier world).
// Português: O choke point do dispatch que filtra não-fonte REGISTRA os
// filtrados em def.Assets — o dicionário da Fase B vive ou morre disto.
func TestDispatchRecordsAssets(t *testing.T) {
	def, _ := ParseForLanguageFiles("c", []FileEntry{
		{Path: "dev.c", Content: `
// label:Sink.
void sink(
	// lang:yaml.
	// dict:cfg.json.
	// slice:n.
	const uint8_t *d,
	unsigned long n
) { (void)d; (void)n; }
`},
		{Path: "cfg.json", Content: `[{"label":"a","insert":"a: $1","doc":"A."}]`},
	}, DefaultParserLimits())

	if len(def.Files) != 1 {
		t.Fatalf("Files = %d, want 1 (source only)", len(def.Files))
	}
	if len(def.Assets) != 1 || def.Assets[0].Path != "cfg.json" {
		t.Fatalf("Assets = %+v, want the filtered cfg.json", def.Assets)
	}
	p := def.Functions[0].Inputs[0]
	if p.EditorLang != "yaml" || p.EditorDict != "cfg.json" {
		t.Fatalf("port config lost: lang=%q dict=%q", p.EditorLang, p.EditorDict)
	}
}
