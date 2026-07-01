// server/codegen/blackbox/parser_c_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// parser_c_test.go — C99 parser tests (Slice C99-8 contract).
//
// Model: C99 is the source of truth.
//   - `static` functions are dropped (file-private).
//   - .h declaration + .c definition dedupe (definition preferred).
//   - A struct not referenced by any public function AND without
//     methods is internal → dropped (filter suppressed when there
//     are zero public functions).
//   - Every public function NOT consumed as a struct method becomes
//     its OWN device — one device per function — in def.Functions.
//     No prefix grouping, no "runs first" (C99 has neither).
//   - A non-void return is a typed output named "return"; void → no
//     output. No error concept.

import (
	"strings"
	"testing"
)

// ─── static filter ────────────────────────────────────────────────────────────

func TestParseC_StaticFunctionsIgnored(t *testing.T) {
	src := `static void helper(void) { }
void public_api(int x);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 1 {
		t.Fatalf("want 1 function device; got %v", fnSummary(def))
	}
	if def.Functions[0].Name != "public_api" {
		t.Errorf("want device 'public_api'; got %q", def.Functions[0].Name)
	}
}

func TestParseC_StaticInlineIgnored(t *testing.T) {
	src := `static inline void helper(void) { return; }
void api(void);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 1 || def.Functions[0].Name != "api" {
		t.Errorf("want exactly 1 function device (api); got %v", fnSummary(def))
	}
}

// ─── dedupe declaration/definition ────────────────────────────────────────────

func TestParseC_HeaderAndCDefinitionDedupe(t *testing.T) {
	src := `
void display_init(void);
void display_write(int c, const char *t);
void display_clear(void);

void display_init(void) { return; }
void display_write(int c, const char *t) { return; }
void display_clear(void) { return; }
`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 3 {
		t.Errorf("want 3 function devices (dedupe h/c); got %v", fnSummary(def))
	}
}

// ─── internal struct filter ───────────────────────────────────────────────────

func TestParseC_InternalStructFiltered(t *testing.T) {
	src := `typedef struct {
    int count;
} internal_state_t;

static void increment(internal_state_t *s) { s->count++; }

void do_work(int n);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	for _, sd := range def.Structs {
		if sd.Name == "internal_state_t" {
			t.Errorf("internal_state_t should be filtered: %v", structSummary(def))
		}
	}
}

func TestParseC_StructUsedInPublicSignatureSurfaces(t *testing.T) {
	src := `typedef struct { int x; } config_t;
void apply_config(const config_t *cfg);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	// Decision (b): a struct referenced by a public function is a
	// WIRE-TYPE, not a device. It must surface in WireTypes, and must
	// NOT appear as a device in Structs.
	found := false
	for _, sd := range def.WireTypes {
		if sd.Name == "config_t" {
			found = true
		}
	}
	if !found {
		t.Errorf("config_t should surface as a wire-type: wireTypes=%v structs=%v",
			wireTypeNames(def), structSummary(def))
	}
	if len(def.Structs) != 0 {
		t.Errorf("no struct should be a device under model (b): %v", structSummary(def))
	}
}

func TestParseC_StructOnlyFileSurfaces(t *testing.T) {
	src := `struct Foo { int x; };`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	// A struct-only file (no public functions) still surfaces the
	// struct — as a wire-type now, not a device.
	if len(def.WireTypes) != 1 || def.WireTypes[0].Name != "Foo" {
		t.Errorf("Foo must surface as a wire-type (struct-only file): wireTypes=%v",
			wireTypeNames(def))
	}
}

// Regression: text inside `//` line comments must never be discovered
// as a declaration. preprocessC keeps line comments verbatim so IDS
// directives can be read from them, but the scanners (function, struct,
// enum, forward typedef) must skip their prose. This bug surfaced after
// editing a wire-type: the rewrite rewrote the handle's doc as `//`
// lines, and "Created by sht3x_create();" became a phantom function.
func TestParseC_DeclarationsInLineCommentsAreIgnored(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		fns   int
		wts   int
		enums int
	}{
		{
			name: "function call in comment",
			src:  "#include <stdint.h>\n// Created by sht3x_create();\ntypedef struct sht3x sht3x_t;\nstruct sht3x { int fd; };\nesp_err_t sht3x_read(sht3x_t *dev);",
			fns:  1, wts: 1, enums: 0,
		},
		{
			name: "struct in comment",
			src:  "// see struct Ghost { int x; } in the docs.\nstruct Real { int y; };\nvoid f(struct Real *r);",
			fns:  1, wts: 1, enums: 0,
		},
		{
			name: "enum in comment",
			src:  "// example: typedef enum { A, B } Ghost_t;\ntypedef enum { X = 0 } Real_t;\nvoid f(Real_t v);",
			fns:  1, wts: 0, enums: 1,
		},
		{
			name: "forward typedef in comment",
			src:  "// produced by: typedef struct ghost ghost_t;\ntypedef struct real real_t;\nstruct real { int z; };\nvoid f(real_t *r);",
			fns:  1, wts: 1, enums: 0,
		},
	}
	for _, tc := range cases {
		def, _ := ParseC([]byte(tc.src), DefaultParserLimits())
		if len(def.Functions) != tc.fns {
			t.Errorf("%s: functions=%d, want %d (%v)", tc.name, len(def.Functions), tc.fns, fnSummary(def))
		}
		if len(def.WireTypes) != tc.wts {
			t.Errorf("%s: wireTypes=%d, want %d (%v)", tc.name, len(def.WireTypes), tc.wts, wireTypeNames(def))
		}
		if len(def.Enums) != tc.enums {
			t.Errorf("%s: enums=%d, want %d", tc.name, len(def.Enums), tc.enums)
		}
	}
}

func wireTypeNames(def *BlackBoxDef) []string {
	var out []string
	for _, wt := range def.WireTypes {
		out = append(out, wt.Name)
	}
	return out
}

// An opaque handle whose alias is declared in a separate forward
// typedef (`typedef struct sht3x sht3x_t;`) while its body lives
// elsewhere (`struct sht3x { ... };`) must surface as a wire-type when
// a public signature references it by the alias.
// The handle's documentation lives above its forward typedef (the
// public interface). The wire-type's doc must come from there, not from
// the struct body (which carries no comment, and may be in the .c).
func TestParseC_OpaqueHandleDocFromForwardTypedef(t *testing.T) {
	src := `/*
 * Opaque handle to one sensor instance.
 */
typedef struct sht3x sht3x_t;
struct sht3x { int fd; };
esp_err_t sht3x_read(sht3x_t *dev);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.WireTypes) != 1 {
		t.Fatalf("want 1 wire-type; got %d", len(def.WireTypes))
	}
	if def.WireTypes[0].Doc != "Opaque handle to one sensor instance." {
		t.Errorf("doc should come from the forward typedef; got %q", def.WireTypes[0].Doc)
	}
}

// A purely opaque handle whose body lives in the .c (absent here)
// still surfaces as a wire-type, anchored on its forward typedef.
func TestParseC_BodylessOpaqueHandleSurfaces(t *testing.T) {
	src := `/*
 * Opaque handle (body hidden in the .c).
 */
typedef struct sht3x sht3x_t;
esp_err_t sht3x_read(sht3x_t *dev);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.WireTypes) != 1 {
		t.Fatalf("body-less opaque handle should surface; got %d", len(def.WireTypes))
	}
	wt := def.WireTypes[0]
	if wt.Name != "sht3x" || wt.Alias != "sht3x_t" {
		t.Errorf("want name=sht3x alias=sht3x_t; got name=%q alias=%q", wt.Name, wt.Alias)
	}
	if wt.Doc != "Opaque handle (body hidden in the .c)." {
		t.Errorf("doc should come from the typedef; got %q", wt.Doc)
	}
}

func TestParseC_OpaqueHandleForwardTypedefSurfaces(t *testing.T) {
	src := `typedef struct sht3x sht3x_t;
struct sht3x { int fd; };
esp_err_t sht3x_read(sht3x_t *dev);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.WireTypes) != 1 {
		t.Fatalf("opaque handle should surface as a wire-type; got %v", wireTypeNames(def))
	}
	wt := def.WireTypes[0]
	if wt.Name != "sht3x" || wt.Alias != "sht3x_t" {
		t.Errorf("want name=sht3x alias=sht3x_t; got name=%q alias=%q", wt.Name, wt.Alias)
	}
}

// A struct that no public signature references stays hidden — it is an
// internal implementation detail, not a wire-type.
func TestParseC_InternalStructStaysHidden(t *testing.T) {
	src := `struct hidden { int x; };
esp_err_t f(int v);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.WireTypes) != 0 {
		t.Errorf("unreferenced struct must stay hidden; got %v", wireTypeNames(def))
	}
}

// ─── one device per function ────────────────────────────────────────────────────

// A `;`, `{` or `}` written in prose inside a function's doc-comment
// must not be mistaken for a declaration boundary. If it were, the
// return-type span would swallow the rest of the comment and a `void`
// function would sprout a phantom typed `return` port.
func TestParseC_DocBoundaryCharsDoNotCorruptReturn(t *testing.T) {
	src := "esp_err_t display_init(void) { return ESP_OK; }\n" +
		"// Append text; wrap to width. Use {color} per line.\n" +
		"// label:displayWrite.\n" +
		"// icon:pen-clip.\n" +
		"void display_write(display_color_t color, const char *text) { return; }"
	def, _ := ParseC([]byte(src), DefaultParserLimits())

	var dw *NamedFuncDef
	for i := range def.Functions {
		if def.Functions[i].Name == "display_write" {
			dw = &def.Functions[i]
		}
	}
	if dw == nil {
		t.Fatal("display_write not found")
	}
	if len(dw.Outputs) != 0 {
		t.Errorf("void display_write has %d outputs, want 0 (no phantom return); got %+v",
			len(dw.Outputs), dw.Outputs)
	}
	if dw.Label != "displayWrite" || dw.Icon != "pen-clip" {
		t.Errorf("device directives corrupted: label=%q icon=%q", dw.Label, dw.Icon)
	}
}

func TestParseC_OneDevicePerFunction(t *testing.T) {
	src := `void display_init(void);
void display_write(int c, const char *t);
void display_clear(void);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 3 {
		t.Fatalf("want 3 function devices; got %v", fnSummary(def))
	}
	names := []string{def.Functions[0].Name, def.Functions[1].Name, def.Functions[2].Name}
	want := []string{"display_init", "display_write", "display_clear"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("device[%d] = %q, want %q (full function name, no grouping)", i, names[i], want[i])
		}
	}
	// No struct named "display" should be synthesised.
	for _, sd := range def.Structs {
		if sd.Name == "display" {
			t.Error("no 'display' group device should exist — one device per function")
		}
	}
}

func TestParseC_FunctionDevicePortsFromSignature(t *testing.T) {
	src := `void display_write(int color, const char *text);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 1 {
		t.Fatalf("want 1 device; got %v", fnSummary(def))
	}
	fn := def.Functions[0].FuncDef
	if len(fn.Inputs) != 2 {
		t.Errorf("want 2 inputs (color, text); got %d", len(fn.Inputs))
	}
	if len(fn.Outputs) != 0 {
		t.Errorf("void return → no output; got %v", fn.Outputs)
	}
}

func TestParseC_FunctionDeviceReturnIsTypedOutput(t *testing.T) {
	src := `esp_err_t display_init(void);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	fn := def.Functions[0].FuncDef
	if len(fn.Outputs) != 1 {
		t.Fatalf("want 1 output (return); got %v", fn.Outputs)
	}
	if fn.Outputs[0].Name != "return" || fn.Outputs[0].GoType != "esp_err_t" {
		t.Errorf("want return·esp_err_t; got %s·%s", fn.Outputs[0].Name, fn.Outputs[0].GoType)
	}
	if fn.Outputs[0].IsError {
		t.Error("C99 has no error concept — IsError must not be set")
	}
}

func TestParseC_NoInitSlotForStandaloneFunctions(t *testing.T) {
	// `display_init` is just a function. C99 has no "runs first",
	// so it is a plain device named display_init — no Init slot.
	src := `void display_init(void);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 1 || def.Functions[0].Name != "display_init" {
		t.Fatalf("want device 'display_init'; got %v", fnSummary(def))
	}
}

func TestParseC_SingleFunctionDevice(t *testing.T) {
	src := `void mainloop(void);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 1 || def.Functions[0].Name != "mainloop" {
		t.Errorf("want device 'mainloop'; got %v", fnSummary(def))
	}
}

func TestParseC_FunctionDeviceSourceOrderPreserved(t *testing.T) {
	src := `void wifi_a(void);
void net_x(void);
void wifi_b(void);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	want := []string{"wifi_a", "net_x", "wifi_b"}
	if len(def.Functions) != 3 {
		t.Fatalf("want 3 devices in source order; got %v", fnSummary(def))
	}
	for i := range want {
		if def.Functions[i].Name != want[i] {
			t.Errorf("order broken at %d: %v", i, fnSummary(def))
		}
	}
}

// ─── struct methods coexist with function devices ──────────────────────────────

func TestParseC_EveryFunctionIsADeviceUnderModelB(t *testing.T) {
	// Decision (b), 2026-05-25: C99 has no methods. A function with a
	// `struct X *` receiver is NOT folded into a struct — it is a plain
	// device-function whose receiver is an ordinary input port, exactly
	// like a function without a receiver. The struct surfaces as a
	// wire-type, never as a device.
	src := `typedef struct { int x; } Sensor;
void Sensor_Read(struct Sensor* s, int *out);

void util_clamp(int *val);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())

	// No struct devices and no methods anywhere.
	if len(def.Structs) != 0 || len(def.Methods) != 0 {
		t.Errorf("model (b): no struct devices/methods; got %v", structSummary(def))
	}
	// Both functions are standalone device-functions, in source order.
	wantFns := []string{"Sensor_Read", "util_clamp"}
	if len(def.Functions) != len(wantFns) {
		t.Fatalf("want 2 function devices; got %v", fnSummary(def))
	}
	for i, w := range wantFns {
		if def.Functions[i].Name != w {
			t.Errorf("function[%d] = %q, want %q", i, def.Functions[i].Name, w)
		}
	}
	// Sensor_Read's receiver `s` is an ordinary INPUT port (default
	// direction); `out` is also an input (no direction:out directive).
	read := def.Functions[0]
	if len(read.Inputs) != 2 || len(read.Outputs) != 0 {
		t.Errorf("Sensor_Read: receiver+out should be inputs (default); got in=%d out=%d",
			len(read.Inputs), len(read.Outputs))
	}
	// Sensor surfaces as a wire-type.
	found := false
	for _, wt := range def.WireTypes {
		if wt.Name == "Sensor" {
			found = true
		}
	}
	if !found {
		t.Errorf("Sensor should surface as a wire-type; got %v", wireTypeNames(def))
	}
}

// ─── display.h + display.c smoke test ─────────────────────────────────────────

func TestParseC_DisplaySmokeTest(t *testing.T) {
	src := `
typedef enum {
    DISPLAY_COLOR_WHITE = 0,
    DISPLAY_COLOR_RED   = 1,
} display_color_t;

esp_err_t display_init(void);
void display_write(display_color_t c, const char *t);
void display_clear(void);

typedef struct {
    char text[22];
    display_color_t color;
} display_line_t;

static void color_to_rgb(display_color_t c) { return; }
static void redraw_all(void) { return; }

esp_err_t display_init(void) { return 0; }
void display_write(display_color_t c, const char *t) { return; }
void display_clear(void) { return; }
`
	def, _ := ParseC([]byte(src), DefaultParserLimits())

	// Three function devices, full names, source order.
	if len(def.Functions) != 3 {
		t.Fatalf("want 3 function devices; got %v", fnSummary(def))
	}
	want := []string{"display_init", "display_write", "display_clear"}
	for i := range want {
		if def.Functions[i].Name != want[i] {
			t.Errorf("device[%d] = %q, want %q", i, def.Functions[i].Name, want[i])
		}
	}
	// display_init returns esp_err_t.
	if got := def.Functions[0].FuncDef.Outputs; len(got) != 1 || got[0].GoType != "esp_err_t" {
		t.Errorf("display_init should return esp_err_t output; got %v", got)
	}
	// display_line_t internal → filtered.
	for _, sd := range def.Structs {
		if sd.Name == "display_line_t" {
			t.Error("display_line_t should be filtered as internal")
		}
	}
	// display_color_t enum surfaces (used in display_write).
	foundEnum := false
	for _, e := range def.Enums {
		if e.Name == "display_color_t" {
			foundEnum = true
		}
	}
	if !foundEnum {
		t.Error("display_color_t enum should surface")
	}
}

// ─── carry-forward ──────────────────────────────────────────────────────────────

func TestParseC_EmptySource(t *testing.T) {
	def, err := ParseC([]byte(""), DefaultParserLimits())
	if err != nil {
		t.Fatalf("must accept empty: %v", err)
	}
	if def == nil || len(def.Structs) != 0 || len(def.Functions) != 0 {
		t.Errorf("empty input → empty; got %v / %v", structSummary(def), fnSummary(def))
	}
}

func TestParseC_TypedefTagWins(t *testing.T) {
	src := `typedef struct Inner { int x; } Outer;`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.WireTypes) == 0 || def.WireTypes[0].Name != "Inner" {
		t.Errorf("tag should win; got %v", wireTypeNames(def))
	}
}

func TestParseC_TypedefAnonAliasWins(t *testing.T) {
	src := `typedef struct { int x; } OnlyAlias;`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.WireTypes) == 0 || def.WireTypes[0].Name != "OnlyAlias" {
		t.Errorf("alias should win when no tag; got %v", wireTypeNames(def))
	}
}

// A comma inside a port's leading comment must not be mistaken for a
// parameter separator, or the parameter it documents gets dropped.
func TestParseC_CommaInPortDocDoesNotSplitParams(t *testing.T) {
	src := "esp_err_t f(\n" +
		"    float *temperature,\n" +
		"    // doc:Relative humidity, 0-100 %.\n" +
		"    // direction:out.\n" +
		"    float *humidity);"
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	fn := def.Functions[0]
	var hum *PortDef
	for i := range fn.Outputs {
		if fn.Outputs[i].Name == "humidity" {
			hum = &fn.Outputs[i]
		}
	}
	if hum == nil {
		t.Fatalf("humidity dropped (comma in its doc split the params); got in=%v out=%v",
			fn.Inputs, fn.Outputs)
	}
	if hum.Doc != "Relative humidity, 0-100 %" {
		t.Errorf("humidity doc = %q", hum.Doc)
	}
}

func TestParseC_FunctionPortsDefaultToInput(t *testing.T) {
	// Decision (b): every parameter is an input by default, regardless
	// of pointer shape. Without a `direction:out.` directive, even a
	// mutable pointer like `uint16_t *out` is an input. The function is
	// a standalone device (no method).
	src := `typedef struct { int x; } S;
void S_Read(struct S* s, const uint8_t* in, uint16_t* out, int channel);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions) != 1 {
		t.Fatalf("S_Read should be a device-function; got %v", fnSummary(def))
	}
	fn := def.Functions[0]
	if len(fn.Inputs) != 4 || len(fn.Outputs) != 0 {
		t.Errorf("all params default to input; got in=%d out=%d", len(fn.Inputs), len(fn.Outputs))
	}
}

func TestParseC_DirectionOutMarksMutablePointerAsOutput(t *testing.T) {
	// With an explicit `direction:out.` on a mutable pointer, that
	// parameter becomes an output.
	src := `typedef struct { int x; } S;
void S_Read(
    struct S* s,
    // direction:out.
    uint16_t* out);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	fn := def.Functions[0]
	if len(fn.Outputs) != 1 || fn.Outputs[0].Name != "out" {
		t.Errorf("out should be an output via direction:out; got out=%v", fn.Outputs)
	}
	if len(fn.Inputs) != 1 || fn.Inputs[0].Name != "s" {
		t.Errorf("s should remain an input; got in=%v", fn.Inputs)
	}
}

// ─── return-value rules (Slice C99-7, still valid) ──────────────────────────────

func TestParseC_Return_NonVoidYieldsTypedOutput(t *testing.T) {
	src := `typedef struct { int x; } S;
esp_err_t S_Foo(struct S* s);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	m := def.Functions[0].FuncDef
	if len(m.Outputs) != 1 || m.Outputs[0].Name != "return" || m.Outputs[0].GoType != "esp_err_t" {
		t.Fatalf("want return·esp_err_t output; got %v", m.Outputs)
	}
	if m.Outputs[0].IsError {
		t.Error("IsError must never be set in C99")
	}
}

func TestParseC_Return_VoidYieldsNoOutput(t *testing.T) {
	src := `typedef struct { int x; } S;
void S_Bar(struct S* s);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Functions[0].FuncDef.Outputs) != 0 {
		t.Errorf("void return → no output")
	}
}

func TestParseC_Return_PointerIsAnOutput(t *testing.T) {
	src := `char * top_name(void);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	out := def.Functions[0].FuncDef.Outputs
	if len(out) != 1 || out[0].IsError {
		t.Errorf("pointer return should be a non-error output; got %v", out)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func structSummary(def *BlackBoxDef) string {
	if def == nil {
		return "<nil>"
	}
	var parts []string
	for _, s := range def.Structs {
		parts = append(parts, s.Name+"("+intStr(len(s.Methods))+"m)")
	}
	return "structs[" + strings.Join(parts, " ") + "]"
}

func fnSummary(def *BlackBoxDef) string {
	if def == nil {
		return "<nil>"
	}
	var parts []string
	for _, f := range def.Functions {
		parts = append(parts, f.Name)
	}
	return "functions[" + strings.Join(parts, " ") + "]"
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	d := []byte{}
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
