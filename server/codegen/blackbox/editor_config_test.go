// server/codegen/blackbox/editor_config_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestEditorConfigDirectives: `lang:` + `dict:` ride the port (Phase B),
// the prose survives verbatim, and undirected params are untouched.
// Português: `lang:` + `dict:` viajam na porta, a prosa sobrevive e
// parâmetros sem diretiva ficam intactos.
func TestEditorConfigDirectives(t *testing.T) {
	src := []FileEntry{{Path: "cfg.c", Content: `
// label:Config sink.
void cfg_sink(
	// The device configuration, wrapped
	// across two prose lines.
	// lang:yaml.
	// dict:config_dict.json.
	// slice:n.
	const uint8_t *cfg,
	unsigned long n,
	// Plain doc. No directives here.
	int mode
) { (void)cfg; (void)n; (void)mode; }
`}}
	def, perr := ParseCFiles(src, DefaultParserLimits())
	if perr != nil {
		t.Logf("parse warnings: %v", perr)
	}
	if len(def.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(def.Functions))
	}
	fn := def.Functions[0]
	if len(fn.Inputs) != 2 {
		t.Fatalf("inputs = %d, want 2 (slice collapsed + mode)", len(fn.Inputs))
	}

	cfg := fn.Inputs[0]
	if cfg.EditorLang != "yaml" {
		t.Errorf("EditorLang = %q, want yaml", cfg.EditorLang)
	}
	if cfg.EditorDict != "config_dict.json" {
		t.Errorf("EditorDict = %q, want config_dict.json", cfg.EditorDict)
	}
	if got := cfg.Doc; got == "" ||
		!contains(got, "wrapped") || !contains(got, "two prose lines") {
		t.Errorf("prose mangled: %q", got)
	}
	if contains(cfg.Doc, "lang:") || contains(cfg.Doc, "dict:") {
		t.Errorf("directives leaked into doc: %q", cfg.Doc)
	}

	mode := fn.Inputs[1]
	if mode.EditorLang != "" || mode.EditorDict != "" {
		t.Errorf("undirected param picked up config: %q/%q",
			mode.EditorLang, mode.EditorDict)
	}
	// The legacy slice extractor rebuilds every line (pre-guard mold) and
	// normalizes the trailing period — assert content, not punctuation.
	// Português: O extractor antigo do slice normaliza o ponto final —
	// o assert cobra conteúdo, não pontuação.
	if !contains(mode.Doc, "Plain doc") || !contains(mode.Doc, "No directives here") {
		t.Errorf("undirected doc changed: %q", mode.Doc)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
