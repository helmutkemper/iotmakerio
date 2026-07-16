// server/codegen/blackbox/rewrite_c_function_editorcfg_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

const editorCfgSrc = "" +
	"// label:Config.\n" +
	"\n" +
	"#include <stdint.h>\n" +
	"\n" +
	"// label:App config.\n" +
	"void app_config(\n" +
	"    // The maker's YAML.\n" +
	"    // connection:mandatory.\n" +
	"    // lang:yaml.\n" +
	"    // dict:config_dict.json.\n" +
	"    // slice:n.\n" +
	"    const uint8_t *cfg,\n" +
	"    unsigned long n) {\n" +
	"    (void)cfg; (void)n;\n" +
	"}\n"

// TestRewriteC_FunctionPortEdit_PreservesEditorConfig: a modal round-trip
// must NOT strip the Phase B `lang:` + `dict:` directives — the wizard has
// no fields for them, so the rewrite preserves them from the parsed port
// (field report 2026-07-13: one modal pass silently muted the maker's
// Monaco and the demo dictionary vanished without a trace).
// Português: Uma ida ao modal NÃO pode apagar `lang:` + `dict:` — o
// wizard não tem campos para elas, então o rewrite as preserva da porta
// parseada (report de 2026-07-13: uma passada emudeceu o Monaco do maker).
func TestRewriteC_FunctionPortEdit_PreservesEditorConfig(t *testing.T) {
	edit := mkEdit(OpSetPortConnection, "function.app_config.in.cfg", map[string]any{
		"connection": "mandatory",
		"label":      "cfg",
		"comment":    "Edited by the modal.",
	})
	out, err := RewriteC(editorCfgSrc, []WizardEdit{edit})
	if err != nil {
		t.Fatalf("RewriteC: %v", err)
	}
	for _, want := range []string{"lang:yaml.", "dict:config_dict.json."} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s was dropped by the port edit; source:\n%s", want, out)
		}
	}

	// The parsed port must still carry the config after the round-trip.
	def, perr := ParseC([]byte(out), DefaultParserLimits())
	if perr != nil {
		t.Fatalf("ParseC after rewrite: %v", perr)
	}
	p := def.Functions[0].Inputs[0]
	if p.EditorLang != "yaml" || p.EditorDict != "config_dict.json" {
		t.Fatalf("port config lost after rewrite: lang=%q dict=%q\nsource:\n%s",
			p.EditorLang, p.EditorDict, out)
	}

	// Second pass: the re-emitted own-line form must survive again.
	out2, err := RewriteC(out, []WizardEdit{edit})
	if err != nil {
		t.Fatalf("RewriteC (2nd): %v", err)
	}
	if !strings.Contains(out2, "lang:yaml.") || !strings.Contains(out2, "dict:config_dict.json.") {
		t.Fatalf("editor config dropped on the SECOND pass; source:\n%s", out2)
	}
}

// TestRewriteC_PortModal_TriState: the complete modal's knobs — SET a new
// editor config where none existed, CHANGE the language, CLEAR the
// dictionary — while an absent field keeps preserving (covered by the
// test above). One test per verb of the contract.
// Português: Os botões do modal completo — GRAVA config nova, TROCA a
// linguagem, LIMPA o dicionário — enquanto campo ausente segue
// preservando. Um teste por verbo do contrato.
func TestRewriteC_PortModal_TriState(t *testing.T) {
	bare := "" +
		"// label:Config.\n" +
		"\n" +
		"#include <stdint.h>\n" +
		"\n" +
		"// label:App config.\n" +
		"void app_config(\n" +
		"    // The maker's YAML.\n" +
		"    // slice:n.\n" +
		"    const uint8_t *cfg,\n" +
		"    unsigned long n) {\n" +
		"    (void)cfg; (void)n;\n" +
		"}\n"

	// SET: the modal picks yaml + a dictionary on a port that had none.
	set := mkEdit(OpSetPortConnection, "function.app_config.in.cfg", map[string]any{
		"connection": "mandatory",
		"lang":       "yaml",
		"dict":       "config_dict.json",
	})
	out, err := RewriteC(bare, []WizardEdit{set})
	if err != nil {
		t.Fatalf("RewriteC set: %v", err)
	}
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	p := def.Functions[0].Inputs[0]
	if p.EditorLang != "yaml" || p.EditorDict != "config_dict.json" {
		t.Fatalf("SET failed: lang=%q dict=%q\n%s", p.EditorLang, p.EditorDict, out)
	}
	if p.SliceLenName != "n" {
		t.Fatalf("SET dropped the untouched slice pairing: %q\n%s", p.SliceLenName, out)
	}

	// CHANGE lang + CLEAR dict in one edit.
	change := mkEdit(OpSetPortConnection, "function.app_config.in.cfg", map[string]any{
		"connection": "mandatory",
		"lang":       "json",
		"dict":       "",
	})
	out2, err := RewriteC(out, []WizardEdit{change})
	if err != nil {
		t.Fatalf("RewriteC change: %v", err)
	}
	def2, _ := ParseC([]byte(out2), DefaultParserLimits())
	p2 := def2.Functions[0].Inputs[0]
	if p2.EditorLang != "json" {
		t.Fatalf("CHANGE failed: lang=%q\n%s", p2.EditorLang, out2)
	}
	if p2.EditorDict != "" || strings.Contains(out2, "dict:") {
		t.Fatalf("CLEAR failed: dict=%q\n%s", p2.EditorDict, out2)
	}

	// CLEAR the slice pairing: the pair un-collapses back to two pins.
	unslice := mkEdit(OpSetPortConnection, "function.app_config.in.cfg", map[string]any{
		"connection": "mandatory",
		"slice":      "",
	})
	out3, err := RewriteC(out2, []WizardEdit{unslice})
	if err != nil {
		t.Fatalf("RewriteC unslice: %v", err)
	}
	def3, _ := ParseC([]byte(out3), DefaultParserLimits())
	if n := len(def3.Functions[0].Inputs); n != 2 {
		t.Fatalf("un-collapse failed: %d input(s)\n%s", n, out3)
	}
}
