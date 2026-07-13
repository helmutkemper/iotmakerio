// server/codegen/blackbox/rewrite_c_function_mintarget_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// TestRewriteC_FunctionMinTarget pins the min-target directive through the
// wizard's rewrite: set writes it, echo preserves it, omission clears it —
// the exact wipe scenario of the field report (a hand-typed tag dying on
// an unrelated modal save).
// Português: Pina o min-target no rewrite do wizard: set escreve, eco
// preserva, omissão limpa — o cenário exato do report (tag digitada
// morrendo num save de modal alheio).
func TestRewriteC_FunctionMinTarget(t *testing.T) {
	src := "" +
		"// label:Portal server.\n" +
		"// icon:server.\n" +
		"// min-target:posix.\n" +
		"void portal_server_start(int port) {\n" +
		"}\n"

	// Echo (the modal always sends the current value): tag survives an
	// unrelated label edit.
	out, err := RewriteC(src, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.portal_server_start", map[string]any{
			"label":     "Portal server v2",
			"icon":      "server",
			"minTarget": "posix",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (echo): %v", err)
	}
	if !strings.Contains(out, "min-target:posix.") {
		t.Fatalf("echoed min-target must survive, got:\n%s", out)
	}
	if !strings.Contains(out, "label:Portal server v2.") {
		t.Fatalf("label edit lost, got:\n%s", out)
	}

	// Clear: the "— any board —" option omits the field → directive gone.
	out2, err := RewriteC(out, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.portal_server_start", map[string]any{
			"label": "Portal server v2",
			"icon":  "server",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (clear): %v", err)
	}
	if strings.Contains(out2, "min-target:") {
		t.Fatalf("omission must clear the directive, got:\n%s", out2)
	}

	// Set on a bare function: dropdown pick writes the tag.
	bare := "void tiny(void) {\n}\n"
	out3, err := RewriteC(bare, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.tiny", map[string]any{
			"label":     "Tiny",
			"minTarget": "mcu32",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (set): %v", err)
	}
	if !strings.Contains(out3, "min-target:mcu32.") {
		t.Fatalf("set must write the directive, got:\n%s", out3)
	}
}

// TestRewriteC_FunctionNoDevice pins the helper opt-out through the wizard:
// checked writes `device:false.`, unchecked (omitted) clears it, and the
// parser round-trips the flag without leaking into the prose.
// Português: Pina o opt-out de helper: marcado escreve `device:false.`,
// desmarcado limpa, e o parser faz o round-trip sem vazar na prosa.
func TestRewriteC_FunctionNoDevice(t *testing.T) {
	bare := "int clamp_index(int i) {\n    return i;\n}\n"
	out, err := RewriteC(bare, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.clamp_index", map[string]any{
			"label":    "Clamp",
			"noDevice": true,
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (set): %v", err)
	}
	if !strings.Contains(out, "device:false.") {
		t.Fatalf("checkbox must write device:false., got:\n%s", out)
	}

	def, perr := ParseC([]byte(out), DefaultParserLimits())
	if perr != nil {
		t.Logf("parse warnings: %v", perr)
	}
	var fn *NamedFuncDef
	for i := range def.Functions {
		if def.Functions[i].Name == "clamp_index" {
			fn = &def.Functions[i]
		}
	}
	if fn == nil || !fn.NoDevice {
		t.Fatalf("parser must set NoDevice, got %+v", fn)
	}
	if strings.Contains(fn.Doc, "device") {
		t.Fatalf("directive leaked into prose: %q", fn.Doc)
	}

	out2, err := RewriteC(out, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.clamp_index", map[string]any{
			"label": "Clamp",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (clear): %v", err)
	}
	if strings.Contains(out2, "device:false.") {
		t.Fatalf("unchecked must clear the tag, got:\n%s", out2)
	}
}
