// server/codegen/codeGen_c99_conststring_test.go
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

// sceneC99ConstString wires a StatementConstString ("Hello") into the
// `const char*` input of a C99 function device (displayWrite). This is the
// "a string I can connect to displayWrite" scenario: before the
// StatementConstString case existed in emitNode, the emitter warned
// "unknown device type" and dropped the value. Now it lowers to a string
// OpConst, which the C backend renders as a `const char*` literal.
const sceneC99ConstString = `{
  "version": "1.0",
  "devices": [
    { "id": "constStr_0", "type": "StatementConstString", "properties": { "value": "Hello" },
      "connectors": [ { "port": "output", "dataType": "string", "isOutput": true,
        "connections": [ { "wireId": "w1", "targetDevice": "displayWrite_1", "targetPort": "text" } ] } ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },
    { "id": "displayWrite_1", "type": "BlackBoxdisplayWrite:", "properties": {},
      "connectors": [
        { "port": "text", "dataType": "const char*", "isOutput": false,
          "connections": [ { "wireId": "w1", "targetDevice": "constStr_0", "targetPort": "output" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constStr_0", "port": "output" }, "to": { "device": "displayWrite_1", "port": "text" }, "dataType": "const char*" }
  ]
}`

// c99DisplayWriteDefs is the def for the displayWrite C99 function device, with
// the authored source on Files so the backend can inline the body.
func c99DisplayWriteDefs() map[string]*blackbox.BlackBoxDef {
	const src = "" +
		"// write text to the display.\n" +
		"// label:WRITE.\n" +
		"void displayWrite(const char *text) {\n" +
		"    (void)text;\n" +
		"}\n"
	def := &blackbox.BlackBoxDef{
		Files: []blackbox.FileEntry{{Path: "dev.c", Content: src}},
		Functions: []blackbox.NamedFuncDef{
			{
				Name: "displayWrite",
				FuncDef: blackbox.FuncDef{
					Inputs:  []blackbox.PortDef{{Name: "text", GoType: "const char*", Connection: "mandatory"}},
					Outputs: nil,
				},
			},
		},
	}
	return map[string]*blackbox.BlackBoxDef{"displayWrite": def}
}

// TestEmitC_C99ConstStringIntoFunction verifies the StatementConstString case:
// the node is emitted (no "unknown device type" diagnostic) and its value
// reaches the C output as a quoted string literal feeding the function call.
func TestEmitC_C99ConstStringIntoFunction(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99ConstString),
		Language:     "c",
		BlackBoxDefs: c99DisplayWriteDefs(),
	})

	// The case means the emitter no longer warns and drops the node.
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Message, "unknown device type") {
			t.Fatalf("StatementConstString still unhandled: %+v", d)
		}
	}

	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; diagnostics=%+v", resp.Diagnostics)
	}

	// The string literal reaches the C output, quoted (the backend maps the
	// "string" IR type to const char* and passes the already-quoted value).
	if !strings.Contains(mainC, `"Hello"`) {
		t.Fatalf("expected the quoted literal \"Hello\" in main.c, got:\n%s", mainC)
	}
	// ...and the function is called with it (directly inlined or via the
	// declared const variable — either way the call is present).
	if !strings.Contains(mainC, "displayWrite(") {
		t.Fatalf("expected displayWrite(...) call in main.c, got:\n%s", mainC)
	}
}
