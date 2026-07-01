// server/codegen/blackbox/parser_c_callback_test.go
package blackbox

import (
	"strings"
	"testing"
)

// Sub-fatia 5.1 (callback handlers, Opção C). These tests pin the PARSER
// contract that the IR/backend (5.2/5.3) and the WASM UI will build on:
//
//   - a function-pointer typedef is recorded as a callback type;
//   - a parameter of that type (e.g. `cb` in sht3x_set_alert) is flagged with
//     PortDef.CallbackType, while a plain `void *user_data` is NOT;
//   - a `// callback:<type>:<mode>.` function: "both" (the default, including a
//     bare `// callback:<type>.`) keeps its parameters as inputs AND gains one
//     `callback` reference output; "ref" exposes only the reference output and
//     no maker-wired inputs (params supplied by the caller at runtime).

// TestParseC_Callback_TypeAndPort: the typedef becomes a callback type and the
// matching parameter is flagged; the trailing void* context is left plain.
func TestParseC_Callback_TypeAndPort(t *testing.T) {
	const src = `
typedef void (*sht3x_alert_cb_t)(float temperature_c, void *user_data);

// label:On alert.
// return:Error.
int sht3x_set_alert(
	// label:dev.
	sht3x_t *dev,
	// label:threshold.
	float threshold_c,
	// label:cb.
	sht3x_alert_cb_t cb,
	// label:user_data.
	void *user_data
) { return 0; }
`
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC error: %v", err)
	}

	if len(def.CallbackTypes) != 1 {
		t.Fatalf("expected 1 callback type, got %d: %+v", len(def.CallbackTypes), def.CallbackTypes)
	}
	ct := def.CallbackTypes[0]
	if ct.Name != "sht3x_alert_cb_t" {
		t.Errorf("callback type name = %q, want sht3x_alert_cb_t", ct.Name)
	}
	if ct.ReturnType != "void" {
		t.Errorf("callback return type = %q, want void", ct.ReturnType)
	}
	if !strings.Contains(ct.Params, "temperature_c") || !strings.Contains(ct.Params, "user_data") {
		t.Errorf("callback params = %q, want temperature_c and user_data", ct.Params)
	}

	fn := findFunc(t, def, "sht3x_set_alert")
	if fn.HandlerType != "" {
		t.Errorf("sht3x_set_alert.HandlerType = %q, want empty (not a handler)", fn.HandlerType)
	}

	cb, ok := portByName(fn.Inputs, "cb")
	if !ok {
		t.Fatalf("cb input not found in %v", inputNames(fn))
	}
	if cb.CallbackType != "sht3x_alert_cb_t" {
		t.Errorf("cb.CallbackType = %q, want sht3x_alert_cb_t", cb.CallbackType)
	}
	if cb.GoType != "sht3x_alert_cb_t" {
		t.Errorf("cb.GoType = %q, want sht3x_alert_cb_t", cb.GoType)
	}

	ud, ok := portByName(fn.Inputs, "user_data")
	if !ok {
		t.Fatalf("user_data input not found in %v", inputNames(fn))
	}
	if ud.CallbackType != "" {
		t.Errorf("user_data.CallbackType = %q, want empty (plain void*)", ud.CallbackType)
	}
}

// TestParseC_Callback_Handler: a bare `callback:T.` defaults to "both" mode.
// The parsed def is the pure CALLABLE function — it keeps its parameters as
// inputs and is NOT given a `callback` output (the reference is a separate
// device). Only the metadata (HandlerType, CallbackMode) records the directive.
func TestParseC_Callback_Handler(t *testing.T) {
	const src = `
typedef void (*sht3x_alert_cb_t)(float temperature_c, void *user_data);

// label:On high temp.
// callback:sht3x_alert_cb_t.
void on_high_temp(float temperature_c, void *user_data) {
	(void)temperature_c;
	(void)user_data;
}
`
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC error: %v", err)
	}

	fn := findFunc(t, def, "on_high_temp")
	if fn.HandlerType != "sht3x_alert_cb_t" {
		t.Errorf("HandlerType = %q, want sht3x_alert_cb_t", fn.HandlerType)
	}
	if fn.CallbackMode != CallbackModeBoth {
		t.Errorf("CallbackMode = %q, want both (bare callback:T. default)", fn.CallbackMode)
	}
	// The def keeps the parameters as inputs (it is the callable function).
	if len(fn.Inputs) != 2 {
		t.Errorf("handler def inputs = %d (%v), want 2 (params kept)", len(fn.Inputs), inputNames(fn))
	}
	// Pure callable: void return means no output ports, and the def carries no
	// `callback` pin (the reference is a separate CallbackRef:<fn> device).
	if len(fn.Outputs) != 0 {
		t.Errorf("handler def outputs = %d (%+v), want 0 (void return, no callback pin)", len(fn.Outputs), fn.Outputs)
	}
	assertNoCallbackOutputOnDef(t, fn)
	if strings.Contains(strings.ToLower(fn.Doc), "callback:") {
		t.Errorf("handler directive leaked into Doc: %q", fn.Doc)
	}
	if fn.Label != "On high temp" {
		t.Errorf("Label = %q, want On high temp (directive extraction must not disturb other directives)", fn.Label)
	}
}

// TestParseC_Callback_HandlerRefMode: `callback:T:ref.` records CallbackMode
// "ref". At the PARSER level the def is identical to "both" (params kept, no
// callback pin); the difference is metadata only. "ref" means the IDE offers
// ONLY the callback reference device (no callable variant) — a device-level
// decision, not a port-shape change on the def.
func TestParseC_Callback_HandlerRefMode(t *testing.T) {
	const src = `
typedef void (*sht3x_alert_cb_t)(float temperature_c, void *user_data);

// label:On high temp.
// callback:sht3x_alert_cb_t:ref.
void on_high_temp(float temperature_c, void *user_data) {
	(void)temperature_c;
	(void)user_data;
}
`
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC error: %v", err)
	}

	fn := findFunc(t, def, "on_high_temp")
	if fn.HandlerType != "sht3x_alert_cb_t" {
		t.Errorf("HandlerType = %q, want sht3x_alert_cb_t", fn.HandlerType)
	}
	if fn.CallbackMode != CallbackModeRef {
		t.Errorf("CallbackMode = %q, want ref", fn.CallbackMode)
	}
	// Same def shape as "both": params kept, no callback pin on the def.
	if len(fn.Inputs) != 2 {
		t.Errorf("ref-mode handler def inputs = %d (%v), want 2 (params kept on the def)", len(fn.Inputs), inputNames(fn))
	}
	assertNoCallbackOutputOnDef(t, fn)
}

// TestParseC_Callback_HandlerBothExplicit: `callback:T:both.` is identical to a
// bare `callback:T.` — CallbackMode "both", params kept, no callback pin.
func TestParseC_Callback_HandlerBothExplicit(t *testing.T) {
	const src = `
typedef void (*evt_cb_t)(int code);

// callback:evt_cb_t:both.
void on_event(int code) { (void)code; }
`
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC error: %v", err)
	}
	fn := findFunc(t, def, "on_event")
	if fn.HandlerType != "evt_cb_t" || fn.CallbackMode != CallbackModeBoth {
		t.Errorf("HandlerType=%q CallbackMode=%q, want evt_cb_t/both", fn.HandlerType, fn.CallbackMode)
	}
	if len(fn.Inputs) != 1 {
		t.Errorf("both-mode handler def inputs = %d (%v), want 1 (code kept)", len(fn.Inputs), inputNames(fn))
	}
	assertNoCallbackOutputOnDef(t, fn)
}

// assertNoCallbackOutputOnDef fails if the def carries a `callback` reference
// output. Under the duality the reference is a SEPARATE device (CallbackRef:<fn>)
// the IDE synthesizes — never an output on the function's own parsed def.
func assertNoCallbackOutputOnDef(t *testing.T, fn NamedFuncDef) {
	t.Helper()
	for _, o := range fn.Outputs {
		if o.Name == "callback" || o.CallbackType != "" {
			t.Errorf("def must not carry a callback output (got %+v) — the reference is a separate device", o)
		}
	}
}

// TestParseC_Callback_HandlerVsConsume keeps the two function-level directives
// independent: `callback:<type>.` sets the handler, `handle:consume.` sets the
// destructor flag — neither claims the other (they share no prefix by design).
func TestParseC_Callback_HandlerVsConsume(t *testing.T) {
	const src = `
typedef void (*evt_cb_t)(int code);

// callback:evt_cb_t.
void on_event(int code) { (void)code; }

// handle:consume.
void teardown(int *resource) { (void)resource; }
`
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC error: %v", err)
	}

	h := findFunc(t, def, "on_event")
	if h.HandlerType != "evt_cb_t" {
		t.Errorf("on_event.HandlerType = %q, want evt_cb_t", h.HandlerType)
	}
	if h.ConsumesHandle {
		t.Errorf("on_event.ConsumesHandle = true, want false (callback: is not handle:)")
	}

	d := findFunc(t, def, "teardown")
	if !d.ConsumesHandle {
		t.Errorf("teardown.ConsumesHandle = false, want true")
	}
	if d.HandlerType != "" {
		t.Errorf("teardown.HandlerType = %q, want empty", d.HandlerType)
	}
}

// inputNames lists input port names for failure messages.
func inputNames(fn NamedFuncDef) []string {
	out := make([]string, 0, len(fn.Inputs))
	for _, p := range fn.Inputs {
		out = append(out, p.Name)
	}
	return out
}
