// server/codegen/blackbox/rewrite_c_enum_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// rewrite_c_enum_test.go — Tests for enum rewrite ops (Slice C99-6).

import (
	"strings"
	"testing"
)

// ─── enum-level directives ──────────────────────────────────────────────────────

func TestRewriteC_Enum_DirectivesAddedAboveEnum(t *testing.T) {
	src := `typedef enum {
    A = 0,
    B = 1,
} color_t;
void use(color_t c);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "enum.color_t", map[string]string{
			"label": "Color",
			"icon":  "palette",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	if !strings.Contains(out, "// label:Color.") {
		t.Errorf("enum label directive missing:\n%s", out)
	}
	if !strings.Contains(out, "// icon:palette.") {
		t.Errorf("enum icon directive missing:\n%s", out)
	}
	// No // device: line — enums are not function-groups.
	if strings.Contains(out, "// device:") {
		t.Errorf("enum should NOT get a // device: line:\n%s", out)
	}
	// Directive must precede the typedef.
	labelIdx := strings.Index(out, "// label:Color.")
	typedefIdx := strings.Index(out, "typedef enum")
	if labelIdx < 0 || labelIdx >= typedefIdx {
		t.Errorf("label must precede typedef; label@%d typedef@%d", labelIdx, typedefIdx)
	}
}

func TestRewriteC_Enum_DirectivesRoundTrip(t *testing.T) {
	src := `typedef enum {
    A = 0,
} color_t;
void use(color_t c);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "enum.color_t", map[string]string{"label": "Color"}),
	}
	out, _ := RewriteC(src, edits)
	// Re-parse and confirm the label round-trips.
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	if len(def.Enums) != 1 || def.Enums[0].Label != "Color" {
		t.Errorf("label did not round-trip; enums=%+v", def.Enums)
	}
}

// ─── per-value labels ────────────────────────────────────────────────────────────

func TestRewriteC_Enum_ValueLabelAddedAboveEnumerator(t *testing.T) {
	src := `typedef enum {
    DISPLAY_COLOR_WHITE = 0,
    DISPLAY_COLOR_RED   = 1,
} display_color_t;
void use(display_color_t c);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "enum.display_color_t.value.DISPLAY_COLOR_WHITE",
			map[string]string{"label": "White"}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	if !strings.Contains(out, "// label:White.") {
		t.Errorf("value label missing:\n%s", out)
	}
	// The label must sit immediately above its enumerator.
	labelIdx := strings.Index(out, "// label:White.")
	enumIdx := strings.Index(out, "DISPLAY_COLOR_WHITE")
	redIdx := strings.Index(out, "DISPLAY_COLOR_RED")
	if labelIdx < 0 || labelIdx >= enumIdx {
		t.Errorf("label must precede WHITE; label@%d white@%d", labelIdx, enumIdx)
	}
	// And it must be BELOW nothing relevant / ABOVE the RED line's start.
	if labelIdx >= redIdx {
		t.Errorf("label landed in the wrong place (after RED)")
	}
}

func TestRewriteC_Enum_ValueLabelRoundTrip(t *testing.T) {
	src := `typedef enum {
    A = 0,
    B = 1,
} e_t;
void use(e_t v);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "enum.e_t.value.A", map[string]string{"label": "Alpha"}),
		mkEdit("setStructDirectives", "enum.e_t.value.B", map[string]string{"label": "Bravo"}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	labels := map[string]string{}
	for _, v := range def.Enums[0].Values {
		labels[v.Name] = v.Label
	}
	if labels["A"] != "Alpha" || labels["B"] != "Bravo" {
		t.Errorf("value labels did not round-trip: %v", labels)
	}
}

func TestRewriteC_Enum_ValueLabelReplaceInPlace(t *testing.T) {
	// A value that already has a label gets a NEW one — old must go.
	src := `typedef enum {
    // label:OldName.
    A = 0,
} e_t;
void use(e_t v);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "enum.e_t.value.A", map[string]string{"label": "NewName"}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	if strings.Contains(out, "OldName") {
		t.Errorf("old label leaked:\n%s", out)
	}
	if !strings.Contains(out, "// label:NewName.") {
		t.Errorf("new label missing:\n%s", out)
	}
	if strings.Count(out, "// label:") != 1 {
		t.Errorf("should be exactly one label line:\n%s", out)
	}
}

// ─── path validation ─────────────────────────────────────────────────────────────

func TestRewriteC_Enum_UnknownEnumErrors(t *testing.T) {
	src := `typedef enum { A } e_t;
void use(e_t v);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "enum.nonexistent", map[string]string{"label": "X"}),
	}
	out, err := RewriteC(src, edits)
	if err == nil {
		t.Fatal("expected error for unknown enum")
	}
	if out != src {
		t.Error("source should be unchanged on error")
	}
}

func TestRewriteC_Enum_WrongOpErrors(t *testing.T) {
	src := `typedef enum { A } e_t;
void use(e_t v);`
	edits := []WizardEdit{
		mkEdit("setFieldProp", "enum.e_t", map[string]string{"label": "X"}),
	}
	_, err := RewriteC(src, edits)
	if err == nil {
		t.Fatal("expected error: enum paths only support setStructDirectives")
	}
}
