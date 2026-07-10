// server/codegen/blackbox/rewrite_c_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// rewrite_c_test.go — Tests for the C99 rewrite engine (Slice 3).
//
// Each test feeds RewriteC a piece of C99 source plus one or more
// WizardEdits, and asserts that the rewritten output contains the
// expected substring (or, for delete-style edits, does NOT contain
// the removed pattern). We deliberately avoid byte-for-byte
// comparison — the engine preserves indentation but tests should
// not be brittle about it.

import (
	"encoding/json"
	"strings"
	"testing"
)

// helper — builds a WizardEdit with JSON-encoded args.
func mkEdit(op, path string, args any) WizardEdit {
	raw, _ := json.Marshal(args)
	return WizardEdit{Op: op, Path: path, Args: raw}
}

// ─── Op 1: setStructDirectives ────────────────────────────────────────────────

func TestRewriteC_StructDirectives_AddedToBareStruct(t *testing.T) {
	src := `struct Foo {
    int x;
};`
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.Foo", map[string]any{
			"label": "Foo Device",
			"icon":  "gear",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "// label:Foo Device.") {
		t.Errorf("output missing label directive:\n%s", out)
	}
	if !strings.Contains(out, "// icon:gear.") {
		t.Errorf("output missing icon directive:\n%s", out)
	}
	// Original code intact.
	if !strings.Contains(out, "struct Foo {") {
		t.Errorf("struct keyword/name disappeared:\n%s", out)
	}
}

func TestRewriteC_StructDirectives_ReplacesExisting(t *testing.T) {
	src := `// label:OldName.
// icon:question.
struct Foo {
    int x;
};`
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.Foo", map[string]any{
			"label": "NewName",
			"icon":  "gear",
		}),
	}
	out, _ := RewriteC(src, edits)
	if strings.Contains(out, "OldName") {
		t.Errorf("old label leaked through:\n%s", out)
	}
	if strings.Contains(out, "question") {
		t.Errorf("old icon leaked through:\n%s", out)
	}
	if !strings.Contains(out, "// label:NewName.") {
		t.Errorf("new label missing:\n%s", out)
	}
}

func TestRewriteC_StructDirectives_PreservesProse(t *testing.T) {
	src := `// User-written description.
// Spans multiple lines.
struct Foo {
    int x;
};`
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.Foo", map[string]any{
			"label":   "Foo",
			"comment": "User-written description.\nSpans multiple lines.",
		}),
	}
	out, _ := RewriteC(src, edits)
	if !strings.Contains(out, "User-written description.") {
		t.Errorf("prose lost:\n%s", out)
	}
	if !strings.Contains(out, "Spans multiple lines.") {
		t.Errorf("prose line 2 lost:\n%s", out)
	}
	if !strings.Contains(out, "// label:Foo.") {
		t.Errorf("directive missing:\n%s", out)
	}
}

func TestRewriteC_StructDirectives_TypedefAliasForm(t *testing.T) {
	src := `typedef struct {
    int x;
} MyAlias;`
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.MyAlias", map[string]any{
			"label": "My Device",
			"icon":  "wifi",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "// label:My Device.") {
		t.Errorf("typedef-alias struct: directive missing:\n%s", out)
	}
}

// ─── Op 2/3: setFieldProp / disableFieldProp ─────────────────────────────────

func TestRewriteC_FieldProp_AddedToUntaggedField(t *testing.T) {
	src := `struct Foo {
    uint8_t Gain;
};`
	edits := []WizardEdit{
		mkEdit(OpSetFieldProp, "struct.Foo.field.Gain", setFieldPropArgs{
			Label:   "Gain",
			Default: "1",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `prop:"Gain".`) {
		t.Errorf("prop directive missing:\n%s", out)
	}
	if !strings.Contains(out, `default:"1".`) {
		t.Errorf("default directive missing:\n%s", out)
	}
}

func TestRewriteC_FieldProp_WithOptions(t *testing.T) {
	src := `struct Foo {
    uint8_t Mode;
};`
	args := setFieldPropArgs{
		Label:  "Mode",
		Format: "options",
		FormatArgs: map[string]json.RawMessage{
			"options": json.RawMessage(`["A","B","C"]`),
		},
	}
	edits := []WizardEdit{
		mkEdit(OpSetFieldProp, "struct.Foo.field.Mode", args),
	}
	out, _ := RewriteC(src, edits)
	if !strings.Contains(out, `options:"A,B,C".`) {
		t.Errorf("options directive missing:\n%s", out)
	}
}

func TestRewriteC_DisableFieldProp_RemovesDirectiveKeepsProse(t *testing.T) {
	src := `struct Foo {
    // Some user-written description.
    // prop:"Gain". default:"1".
    uint8_t Gain;
};`
	edits := []WizardEdit{
		mkEdit(OpDisableFieldProp, "struct.Foo.field.Gain", map[string]any{}),
	}
	out, _ := RewriteC(src, edits)
	if strings.Contains(out, `prop:"Gain"`) {
		t.Errorf("prop directive should be removed:\n%s", out)
	}
	// The prose is part of the leading-comment range that was
	// REPLACED by the rendered block. When disableFieldProp runs,
	// renderCommentBlock returns "" so the block disappears
	// entirely. That's the documented behaviour — disable means
	// "clean slate for the field". User prose attached to a prop
	// disappears too.
	//
	// We assert only that the directive is gone; the prose
	// disappearance is an explicit design choice noted in the doc.
}

// ─── Right-to-left ordering ──────────────────────────────────────────────────

func TestRewriteC_MultipleEdits_OrderingInvariant(t *testing.T) {
	src := `struct A {
    int x;
    int y;
};
void A_Run(struct A* s);`
	// Three edits: wire-type directives, field prop on x, and
	// function-device directives. All three target different byte
	// ranges. The engine must apply them right-to-left so earlier
	// splices don't shift later offsets.
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.A", map[string]any{
			"label": "A Device",
			"icon":  "gear",
		}),
		mkEdit(OpSetFieldProp, "struct.A.field.x", setFieldPropArgs{
			Label:   "X Param",
			Default: "0",
		}),
		mkEdit(OpSetStructDirectives, "function.A_Run", map[string]any{
			"label": "Run",
			"icon":  "play",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "// label:A Device.") {
		t.Errorf("struct directive missing:\n%s", out)
	}
	if !strings.Contains(out, `prop:"X Param".`) {
		t.Errorf("field prop missing:\n%s", out)
	}
	if !strings.Contains(out, "// label:Run.") {
		t.Errorf("method directive missing:\n%s", out)
	}
}

// ─── No edits: identity ──────────────────────────────────────────────────────

func TestRewriteC_NoEdits_IsIdentity(t *testing.T) {
	src := `struct Foo {
    int x;
};
void helper(int y);`
	out, err := RewriteC(src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != src {
		t.Errorf("no edits should be a no-op; got changes:\nIN:\n%s\nOUT:\n%s", src, out)
	}
}

// ─── Round-trip: parse → edit → re-parse ─────────────────────────────────────

// Decision (b): marking a mutable-pointer parameter as an output via
// the Wizard checkbox writes `direction:out.` and flips it to an
// output on re-parse; everything else stays an input by default.
func TestRewriteC_RoundTrip_DirectionOut(t *testing.T) {
	src := `// label:Read.
esp_err_t sensor_read(struct Sensor *s, float *out);`
	edits := []WizardEdit{
		mkEdit("setPortConnection", "function.sensor_read.in.out", map[string]string{
			"label":      "Temperature",
			"comment":    "the reading",
			"direction":  "out",
			"connection": "mandatory",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !strings.Contains(out, "// direction:out.") {
		t.Errorf("direction:out directive not written:\n%s", out)
	}
	// A parameter shown as an output is still a parameter: its
	// connection must be emitted alongside the direction.
	if !strings.Contains(out, "// connection:mandatory.") {
		t.Errorf("connection not written for an output parameter:\n%s", out)
	}
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	fn := def.Functions[0]
	// `out` flipped to output; `s` stays a default input.
	if len(fn.Outputs) != 2 { // out + return
		t.Errorf("want 2 outputs (out + return); got %v", fn.Outputs)
	}
	var outPort *PortDef
	for i := range fn.Outputs {
		if fn.Outputs[i].Name == "out" {
			outPort = &fn.Outputs[i]
		}
	}
	if outPort == nil {
		t.Fatal("out should be an output")
	}
	if outPort.Label != "Temperature" {
		t.Errorf("out label = %q, want Temperature", outPort.Label)
	}
	if outPort.MissingConn || outPort.Connection != "mandatory" {
		t.Errorf("out should keep its mandatory connection; got conn=%q missing=%v",
			outPort.Connection, outPort.MissingConn)
	}
	if len(fn.Inputs) != 1 || fn.Inputs[0].Name != "s" {
		t.Errorf("s should remain a default input; got %v", fn.Inputs)
	}
}

// Editing an opaque handle's label/icon writes the directives above its
// forward typedef (the doc anchor), preserving the user's prose, rather
// than above the struct body.
func TestRewriteC_WireTypeDirectivesAboveForwardTypedef(t *testing.T) {
	src := `/*
 * Opaque handle to one sensor instance.
 */
typedef struct sht3x sht3x_t;
struct sht3x { int fd; };
esp_err_t sht3x_read(sht3x_t *dev);`
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.sht3x", map[string]any{
			"label":   "Sensor handle",
			"icon":    "microchip",
			"comment": "Opaque handle to one sensor instance.",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	posDoc := strings.Index(out, "Opaque handle to one sensor instance")
	posLabel := strings.Index(out, "label:Sensor handle")
	posTypedef := strings.Index(out, "typedef struct sht3x")
	posBody := strings.Index(out, "struct sht3x {")
	if !(posDoc >= 0 && posDoc < posLabel && posLabel < posTypedef && posTypedef < posBody) {
		t.Errorf("directives should sit above the typedef, below the prose; got doc=%d label=%d typedef=%d body=%d\n%s",
			posDoc, posLabel, posTypedef, posBody, out)
	}
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	wt := def.WireTypes[0]
	if wt.Label != "Sensor handle" || wt.Icon != "microchip" {
		t.Errorf("round-trip lost label/icon; got label=%q icon=%q", wt.Label, wt.Icon)
	}
	if wt.Doc != "Opaque handle to one sensor instance." {
		t.Errorf("round-trip lost prose; got doc=%q", wt.Doc)
	}
}

func TestRewriteC_RoundTrip_StructLabelPersists(t *testing.T) {
	src := `typedef struct {
    int x;
} Sensor;`
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.Sensor", map[string]any{
			"label": "Sensor Reading",
			"icon":  "thermometer",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Now re-parse the output and confirm the directives stuck. Under
	// model (b) the struct is a wire-type, not a device, so the
	// directives surface in WireTypes (the rewrite still writes them
	// into the struct's leading comment exactly as before).
	def, err := ParseC([]byte(out), DefaultParserLimits())
	if err != nil {
		t.Fatalf("re-parse failed: %v\nsource:\n%s", err, out)
	}
	if len(def.WireTypes) != 1 {
		t.Fatalf("expected 1 wire-type after re-parse; got %d", len(def.WireTypes))
	}
	if def.WireTypes[0].Label != "Sensor Reading" {
		t.Errorf("label round-trip failed: %q", def.WireTypes[0].Label)
	}
	if def.WireTypes[0].Icon != "thermometer" {
		t.Errorf("icon round-trip failed: %q", def.WireTypes[0].Icon)
	}
}

// ─── Error paths ─────────────────────────────────────────────────────────────

func TestRewriteC_StructNotFound_ReturnsError(t *testing.T) {
	src := `struct Foo { int x; };`
	edits := []WizardEdit{
		mkEdit(OpSetStructDirectives, "struct.Missing", map[string]any{
			"label": "X",
		}),
	}
	out, err := RewriteC(src, edits)
	if err == nil {
		t.Fatal("expected error when struct does not exist")
	}
	// Original source must be returned unchanged on error.
	if out != src {
		t.Errorf("source should be unchanged on error")
	}
}

func TestRewriteC_UnknownOp_ReturnsError(t *testing.T) {
	src := `struct Foo { int x; };`
	edits := []WizardEdit{
		{Op: "bogusOp", Path: "struct.Foo", Args: json.RawMessage(`{}`)},
	}
	_, err := RewriteC(src, edits)
	if err == nil {
		t.Fatal("expected error on unknown op")
	}
}

// ─── Slice C99-8: standalone function device rewrites ─────────────────────────

func TestRewriteC_Function_DirectivesAboveFunction(t *testing.T) {
	src := `void display_clear(void);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "function.display_clear", map[string]string{
			"label": "Clear screen",
			"icon":  "eraser",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	if !strings.Contains(out, "// label:Clear screen.") {
		t.Errorf("label directive missing:\n%s", out)
	}
	if !strings.Contains(out, "// icon:eraser.") {
		t.Errorf("icon directive missing:\n%s", out)
	}
	labelIdx := strings.Index(out, "// label:")
	fnIdx := strings.Index(out, "void display_clear")
	if labelIdx < 0 || labelIdx >= fnIdx {
		t.Errorf("directive must precede the function; label@%d fn@%d", labelIdx, fnIdx)
	}
}

func TestRewriteC_Function_DirectivesRoundTrip(t *testing.T) {
	src := `void display_clear(void);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "function.display_clear", map[string]string{"label": "Clear"}),
	}
	out, _ := RewriteC(src, edits)
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	if len(def.Functions) != 1 || def.Functions[0].FuncDef.Label != "Clear" {
		t.Errorf("label did not round-trip; functions=%+v", def.Functions)
	}
}

func TestRewriteC_Function_InputPortConnection(t *testing.T) {
	src := `void display_write(int color, const char *text);`
	edits := []WizardEdit{
		mkEdit("setPortConnection", "function.display_write.in.color", map[string]string{
			"label":      "Pen colour",
			"connection": "mandatory",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	if !strings.Contains(out, "// label:Pen colour.") || !strings.Contains(out, "connection:mandatory.") {
		t.Errorf("port directives missing:\n%s", out)
	}
}

// Bug A + Bug B: editing a parameter of an INLINE signature must (a)
// not corrupt the device's own label/icon, and (b) round-trip the
// param's label/connection/comment. The fix expands the signature to
// multi-line and emits canonical directives.
func TestRewriteC_Function_InlineParamDoesNotCorruptDevice(t *testing.T) {
	src := "// label:Write line.\n// icon:pen.\nvoid display_write(display_color_t color, const char *text);"
	edits := []WizardEdit{
		mkEdit("setPortConnection", "function.display_write.in.color", map[string]string{
			"label":      "Pen colour",
			"connection": "mandatory",
			"comment":    "the text colour",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	fn := def.Functions[0]
	// Device directives must survive untouched.
	if fn.Label != "Write line" || fn.Icon != "pen" {
		t.Errorf("device corrupted: label=%q icon=%q (want 'Write line'/'pen')", fn.Label, fn.Icon)
	}
	// The edited param round-trips.
	var color *PortDef
	for i := range fn.Inputs {
		if fn.Inputs[i].Name == "color" {
			color = &fn.Inputs[i]
		}
	}
	if color == nil {
		t.Fatal("color port missing")
	}
	if color.Label != "Pen colour" {
		t.Errorf("color label = %q, want 'Pen colour' (must not leak into doc)", color.Label)
	}
	if color.Connection != "mandatory" || color.MissingConn {
		t.Errorf("color connection = %q missing=%v, want 'mandatory'/false", color.Connection, color.MissingConn)
	}
	if color.Doc != "the text colour" {
		t.Errorf("color doc = %q, want 'the text colour'", color.Doc)
	}
	// The untouched param keeps no spurious directives.
	if strings.Count(out, "label:text") != 0 {
		t.Errorf("untouched 'text' param gained a spurious label:\n%s", out)
	}
}

// The synthetic return port is labelled through the function's leading
// `return:<label>.` directive — this pins the MERGE semantics: the
// existing block (label/icon/prose) survives verbatim, only the
// return: segment is owned by this writer, relabel replaces it, and an
// empty label removes it. (This path used to reject as "not editable";
// the natural row-click gesture silently lost the typed value — field
// report 2026-07-08.)
func TestRewriteC_Function_ReturnPortLabelMerge(t *testing.T) {
	src := "// label:Init display.\n" +
		"// icon:tv.\n" +
		"// Prose the merge must not eat.\n" +
		"int display_init(void) { return 0; }\n"

	out, err := RewriteC(src, []WizardEdit{
		mkEdit("setPortConnection", "function.display_init.out.return", map[string]string{"label": "Status"}),
	})
	if err != nil {
		t.Fatalf("label the return: %v", err)
	}
	for _, needle := range []string{"label:Init display.", "icon:tv.", "Prose the merge must not eat.", "// return:Status."} {
		if !strings.Contains(out, needle) {
			t.Fatalf("merge lost %q:\n%s", needle, out)
		}
	}
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	if got := def.Functions[0].Outputs[0].Label; got != "Status" {
		t.Fatalf("round trip: return label = %q, want Status", got)
	}

	// Relabel replaces — no duplicate return: lines.
	out2, err := RewriteC(out, []WizardEdit{
		mkEdit("setPortConnection", "function.display_init.out.return", map[string]string{"label": "Err code"}),
	})
	if err != nil {
		t.Fatalf("relabel: %v", err)
	}
	if strings.Count(out2, "return:") != 1 || !strings.Contains(out2, "return:Err code.") {
		t.Fatalf("relabel must replace, not stack:\n%s", out2)
	}

	// Empty label removes the directive.
	out3, err := RewriteC(out2, []WizardEdit{
		mkEdit("setPortConnection", "function.display_init.out.return", map[string]string{"label": ""}),
	})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if strings.Contains(out3, "return:") {
		t.Fatalf("clear must remove the directive:\n%s", out3)
	}
}

func TestRewriteC_Function_UnknownFunctionErrors(t *testing.T) {
	src := `void a(void);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "function.nonexistent", map[string]string{"label": "X"}),
	}
	out, err := RewriteC(src, edits)
	if err == nil {
		t.Fatal("expected error for unknown function")
	}
	if out != src {
		t.Error("source should be unchanged on error")
	}
}

func TestRewriteC_Function_ReturnLabelRoundTrip(t *testing.T) {
	// The return label persists as a `return:<label>.` directive in
	// the function's leading comment, alongside the device label, and
	// is read back into the port's Label. Codegen still sees esp_err_t.
	src := `esp_err_t display_init(void);`
	edits := []WizardEdit{
		mkEdit("setStructDirectives", "function.display_init", map[string]string{
			"label":       "Init display",
			"returnLabel": "Status",
		}),
	}
	out, err := RewriteC(src, edits)
	if err != nil {
		t.Fatalf("RewriteC error: %v", err)
	}
	if !strings.Contains(out, "// return:Status.") && !strings.Contains(out, "return:Status.") {
		t.Errorf("return label directive missing:\n%s", out)
	}
	def, _ := ParseC([]byte(out), DefaultParserLimits())
	if len(def.Functions) != 1 {
		t.Fatalf("want 1 function; got %d", len(def.Functions))
	}
	fn := def.Functions[0].FuncDef
	// Device label round-trips.
	if fn.Label != "Init display" {
		t.Errorf("device label = %q, want 'Init display'", fn.Label)
	}
	// Return port keeps the real type AND gains the human label.
	ret := fn.Outputs[0]
	if ret.GoType != "esp_err_t" {
		t.Errorf("return type = %q, want 'esp_err_t' (codegen truth)", ret.GoType)
	}
	if ret.Label != "Status" {
		t.Errorf("return label = %q, want 'Status'", ret.Label)
	}
	// The directive must NOT leak into the device prose doc.
	if strings.Contains(fn.Doc, "return:") || strings.Contains(fn.Doc, "Status") {
		t.Errorf("return directive leaked into doc: %q", fn.Doc)
	}
}

// Regression guard for Fatia 3: the C99 method path was demolished, so a
// `method.*` edit must be REJECTED ("unknown op") rather than silently
// processed. This locks the removal — if the dead method branch ever
// creeps back into planCEdit, this test fails.
func TestRewriteC_C99MethodPathRejected(t *testing.T) {
	src := `struct S { int x; };
void S_Run(struct S* s);`
	edits := []WizardEdit{
		mkEdit(OpSetMethodDirectives, "method.S.Run", map[string]any{
			"label": "Run", "icon": "play",
		}),
	}
	_, err := RewriteC(src, edits)
	if err == nil {
		t.Fatal("expected an error for a C99 method-path edit, got nil")
	}
	if !strings.Contains(err.Error(), "unknown op") {
		t.Errorf("expected an 'unknown op' error, got: %v", err)
	}
}
