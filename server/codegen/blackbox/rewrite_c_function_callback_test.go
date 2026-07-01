// server/codegen/blackbox/rewrite_c_function_callback_test.go
package blackbox

import (
	"strings"
	"testing"
)

// TestRewriteC_FunctionCallbackDirective verifies the wizard's callback
// dropdown writes and clears `callback:<type>.` through the SAME function
// planner that writes label/icon/executionOrder — additively, preserving the
// other directives (the whole comment block is rebuilt from args). It then
// re-parses the rewritten source to confirm the function actually became a
// handler (the wizard → parser round-trip the maker relies on).
func TestRewriteC_FunctionCallbackDirective(t *testing.T) {
	src := "" +
		"typedef void (*display_write_fn)(const char *text);\n" +
		"\n" +
		"// label:displayWrite.\n" +
		"// icon:display.\n" +
		"void displayWrite(const char *text) {\n" +
		"    (void)text;\n" +
		"}\n"

	// Mark as a handler of display_write_fn, carrying label/icon/comment the
	// way the function card always does.
	out, err := RewriteC(src, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.displayWrite", map[string]any{
			"label":    "displayWrite",
			"icon":     "display",
			"comment":  "Writes one line to the display.",
			"callback": "display_write_fn",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (set): %v", err)
	}
	if !strings.Contains(out, "callback:display_write_fn.") {
		t.Fatalf("want callback:display_write_fn. in output, got:\n%s", out)
	}
	if !strings.Contains(out, "label:displayWrite.") {
		t.Fatalf("label must be preserved alongside callback, got:\n%s", out)
	}
	if !strings.Contains(out, "icon:display.") {
		t.Fatalf("icon must be preserved alongside callback, got:\n%s", out)
	}

	// Round-trip: the rewritten source must parse into a callback handler. A
	// bare `callback:T.` is the default "both" mode, so the parsed def stays the
	// pure callable (keeps its `text` input) and records HandlerType +
	// CallbackMode — but it is NOT given a `callback` output: the reference is a
	// separate CallbackRef:<fn> device the IDE synthesizes. (The wizard's
	// both-vs-ref checkbox, which writes `:both`/`:ref`, is a later slice.)
	def, err := ParseC([]byte(out), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC after set: %v", err)
	}
	var fn *NamedFuncDef
	for i := range def.Functions {
		if def.Functions[i].Name == "displayWrite" {
			fn = &def.Functions[i]
		}
	}
	if fn == nil {
		t.Fatalf("displayWrite not found after rewrite")
	}
	if fn.FuncDef.HandlerType != "display_write_fn" {
		t.Fatalf("want HandlerType display_write_fn, got %q", fn.FuncDef.HandlerType)
	}
	if fn.FuncDef.CallbackMode != CallbackModeBoth {
		t.Fatalf("a bare callback:T. defaults to both mode, got CallbackMode %q", fn.FuncDef.CallbackMode)
	}
	if len(fn.FuncDef.Inputs) != 1 {
		t.Fatalf("both-mode handler keeps its parameters, want 1 input (text), got %d", len(fn.FuncDef.Inputs))
	}
	// displayWrite is void, so the pure-callable def has no output ports — and
	// never a `callback` pin (that lives on the separate reference device).
	if len(fn.FuncDef.Outputs) != 0 {
		t.Fatalf("handler def must carry no callback pin, want 0 outputs, got %+v", fn.FuncDef.Outputs)
	}
	for _, o := range fn.FuncDef.Outputs {
		if o.Name == "callback" || o.CallbackType != "" {
			t.Fatalf("def must not carry a callback output (got %+v) — the reference is a separate device", o)
		}
	}

	// Clear: omit callback (the dropdown's "— Not a callback handler —") →
	// directive removed; the other directives survive.
	out2, err := RewriteC(out, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.displayWrite", map[string]any{
			"label":   "displayWrite",
			"icon":    "display",
			"comment": "Writes one line to the display.",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (clear): %v", err)
	}
	if strings.Contains(out2, "callback:") {
		t.Fatalf("callback directive must be removed when omitted, got:\n%s", out2)
	}
	if !strings.Contains(out2, "label:displayWrite.") {
		t.Fatalf("label must survive the clear, got:\n%s", out2)
	}

	// And the cleared source is no longer a handler — its `text` input returns.
	def2, err := ParseC([]byte(out2), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC after clear: %v", err)
	}
	for i := range def2.Functions {
		if def2.Functions[i].Name != "displayWrite" {
			continue
		}
		if def2.Functions[i].FuncDef.HandlerType != "" {
			t.Fatalf("clearing callback must drop HandlerType, got %q", def2.Functions[i].FuncDef.HandlerType)
		}
		if len(def2.Functions[i].FuncDef.Inputs) != 1 {
			t.Fatalf("cleared function must regain its text input, got %d inputs", len(def2.Functions[i].FuncDef.Inputs))
		}
	}
}

// TestRewriteC_FunctionCallbackRefMode verifies the wizard's "Also generate the
// normal function block" checkbox: when UNCHECKED it sends callbackMode "ref",
// and the planner must write the explicit `:ref` third segment so the parser
// records CallbackMode "ref". (Checked / "both" stays a bare `callback:T.`,
// covered by the test above.)
func TestRewriteC_FunctionCallbackRefMode(t *testing.T) {
	src := "" +
		"typedef void (*display_write_fn)(const char *text);\n" +
		"\n" +
		"void displayWrite(const char *text) {\n" +
		"    (void)text;\n" +
		"}\n"

	out, err := RewriteC(src, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.displayWrite", map[string]any{
			"label":        "displayWrite",
			"callback":     "display_write_fn",
			"callbackMode": "ref",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (ref): %v", err)
	}
	if !strings.Contains(out, "callback:display_write_fn:ref.") {
		t.Fatalf("want callback:display_write_fn:ref. in output, got:\n%s", out)
	}

	def, err := ParseC([]byte(out), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC after ref set: %v", err)
	}
	var fn *NamedFuncDef
	for i := range def.Functions {
		if def.Functions[i].Name == "displayWrite" {
			fn = &def.Functions[i]
		}
	}
	if fn == nil {
		t.Fatalf("displayWrite not found after rewrite")
	}
	if fn.FuncDef.HandlerType != "display_write_fn" {
		t.Fatalf("want HandlerType display_write_fn, got %q", fn.FuncDef.HandlerType)
	}
	if fn.FuncDef.CallbackMode != CallbackModeRef {
		t.Fatalf("unchecked checkbox must yield CallbackMode ref, got %q", fn.FuncDef.CallbackMode)
	}
	// Even in ref mode the parsed def stays the pure callable (params kept, no
	// callback pin) — ref is an IDE offering decision, not a def-shape change.
	if len(fn.FuncDef.Inputs) != 1 {
		t.Fatalf("ref-mode def keeps its parameters, want 1 input, got %d", len(fn.FuncDef.Inputs))
	}
	for _, o := range fn.FuncDef.Outputs {
		if o.Name == "callback" || o.CallbackType != "" {
			t.Fatalf("def must not carry a callback output (got %+v) — the reference is a separate device", o)
		}
	}
}
