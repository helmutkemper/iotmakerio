// server/codegen/codeGen_c99_callback_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// Fatia 5.2/5.3 — callback wire-ƒ codegen (the LabVIEW static-VI-reference
// idiom). A `// callback:<type>.` handler is wired into a consumer's callback
// parameter. The generated main.c must:
//
//   - pass the handler BY NAME into the consumer call: `setDisplay(displayWrite)`;
//   - NOT emit a call to the handler itself (it is referenced, never executed);
//   - inline both authored bodies (and the typedef) ahead of main so the file
//     compiles on its own.
//
// At the IR level: exactly one BB_CALL (fn=setDisplay); none for the handler.

// c99CallbackDefs parses the authored source the way
// store.LoadBlackBoxDefsForScene does (ParseCFiles → def.Files carries the snapshot → key every
// function name to the same def), so the test exercises the real parser
// contract (HandlerType, CallbackMode, and the consumer input's
// port.CallbackType) rather than a hand-built def that could drift from the
// parser. Note the handler's def is the pure callable (its `text` parameter is
// kept); the `callback` reference is a separate device, not a def output.
func c99CallbackDefs(t *testing.T) map[string]*blackbox.BlackBoxDef {
	t.Helper()
	const src = "" +
		"// Minimal display/wifi callback proof.\n" +
		"typedef void (*display_write_fn)(const char *text);\n" +
		"\n" +
		"// label:displayWrite.\n" +
		"// icon:display.\n" +
		"// callback:display_write_fn.\n" +
		"void displayWrite(const char *text) {\n" +
		"    (void)text;\n" +
		"}\n" +
		"\n" +
		"// label:setDisplay.\n" +
		"// icon:kitchen-set.\n" +
		"void setDisplay(display_write_fn writer) {\n" +
		"    (void)writer;\n" +
		"}\n"

	def, err := blackbox.ParseC([]byte(src), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	def.Files = []blackbox.FileEntry{{Path: "dev.c", Content: src}}

	defs := make(map[string]*blackbox.BlackBoxDef, len(def.Functions))
	for i := range def.Functions {
		defs[def.Functions[i].Name] = def
	}
	return defs
}

// sceneC99Callback: a callback REFERENCE device (CallbackRef:displayWrite —
// the "ƒ" variant) whose `callback` output is wired into the consumer
// (setDisplay) `writer` input. The reference device is a dedicated, non-BlackBox
// node that names the function to pass by address; the normal callable
// displayWrite function is a separate device (not placed here). Mirrors the
// connector/wire shape exported by the WASM stage (see sceneC99Add).
const sceneC99Callback = `{
  "version": "1.0",
  "devices": [
    { "id": "writer_1", "type": "CallbackRef:displayWrite", "properties": {},
      "connectors": [
        { "port": "callback", "dataType": "display_write_fn", "isOutput": true,
          "connections": [ { "wireId": "wcb", "targetDevice": "setdisp_1", "targetPort": "Writer" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },
    { "id": "setdisp_1", "type": "BlackBoxsetDisplay:", "properties": {},
      "connectors": [
        { "port": "writer", "dataType": "display_write_fn", "isOutput": false,
          "connections": [ { "wireId": "wcb", "targetDevice": "writer_1", "targetPort": "callback" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } }
  ],
  "wires": [
    { "id": "wcb", "from": { "device": "writer_1", "port": "callback" }, "to": { "device": "setdisp_1", "port": "writer" }, "dataType": "display_write_fn" }
  ]
}`

func TestEmitC_C99Callback_PassByReference(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99Callback),
		Language:     "c",
		BlackBoxDefs: c99CallbackDefs(t),
	})

	for _, d := range resp.Diagnostics {
		if d.Severity == "error" {
			t.Fatalf("unexpected error diagnostic: %+v", d)
		}
	}

	// IR: one BB_CALL for the consumer, none for the handler.
	if !strings.Contains(resp.IR, "fn=setDisplay") {
		t.Fatalf("expected BB_CALL fn=setDisplay in IR, got:\n%s", resp.IR)
	}
	if strings.Contains(resp.IR, "fn=displayWrite") {
		t.Fatalf("handler must not emit a BB_CALL, but IR contains fn=displayWrite:\n%s", resp.IR)
	}

	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; diagnostics=%+v", resp.Diagnostics)
	}

	// The consumer call passes the handler by name (its address).
	if !strings.Contains(mainC, "setDisplay(displayWrite)") {
		t.Fatalf("expected setDisplay(displayWrite) in main.c, got:\n%s", mainC)
	}

	// Split at main(): the handler's DEFINITION may (must) appear in the
	// preamble, but the handler must NOT be CALLED inside the body.
	idx := strings.Index(mainC, "int main(void)")
	if idx < 0 {
		t.Fatalf("main.c has no main(); got:\n%s", mainC)
	}
	preamble, body := mainC[:idx], mainC[idx:]

	if strings.Contains(body, "displayWrite(") {
		t.Fatalf("handler must not be called inside main(); body was:\n%s", body)
	}

	// Both authored bodies (and the typedef) are inlined ahead of main so the
	// reference resolves and the file compiles standalone.
	if !strings.Contains(preamble, "typedef void (*display_write_fn)(const char *text);") {
		t.Fatalf("typedef not inlined ahead of main(); preamble:\n%s", preamble)
	}
	if !strings.Contains(preamble, "void displayWrite(const char *text)") {
		t.Fatalf("displayWrite definition not inlined; preamble:\n%s", preamble)
	}
	if !strings.Contains(preamble, "void setDisplay(display_write_fn writer)") {
		t.Fatalf("setDisplay definition not inlined; preamble:\n%s", preamble)
	}
}
