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
