package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Implicit call (2026-07-20): an OUTER-WIRED definition is its own
// call-site — LabVIEW semantics. Português: Definição fiada por fora é
// o próprio call-site.
const selfcallScene = `{
  "version": "1.0",
  "metadata": { "language": "c" },
  "devices": [
    { "id": "fn", "type": "StatementFunction", "kind": "complex", "stage": "backend",
      "properties": { "functionName": "my_function" },
      "position": { "x": 200, "y": 100 }, "size": { "width": 400, "height": 300 },
      "connectors": [],
      "containment": { "isContainer": true, "children": [] } },
    { "id": "pt", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "x_in", "tunnelParent": "fn", "tunnelSide": "left" },
      "position": { "x": 191, "y": 200 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "*", "isOutput": false,
          "connections": [{ "wireId": "wo1", "targetDevice": "cext", "targetPort": "output" }] },
        { "port": "out", "dataType": "*", "isOutput": true,
          "connections": [{ "wireId": "wi1", "targetDevice": "rt", "targetPort": "in" }] } ],
      "containment": { "isContainer": false, "status": "free" } },
    { "id": "rt", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "y_out", "tunnelParent": "fn", "tunnelSide": "right" },
      "position": { "x": 591, "y": 200 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "*", "isOutput": false,
          "connections": [{ "wireId": "wi1", "targetDevice": "pt", "targetPort": "out" }] },
        { "port": "out", "dataType": "*", "isOutput": true,
          "connections": [{ "wireId": "wo2", "targetDevice": "pr", "targetPort": "value" }] } ],
      "containment": { "isContainer": false, "status": "free" } },
    { "id": "cext", "type": "StatementConstInt", "kind": "simple", "stage": "backend",
      "properties": { "value": 7 },
      "position": { "x": 20, "y": 200 }, "size": { "width": 120, "height": 74 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [{ "wireId": "wo1", "targetDevice": "pt", "targetPort": "in" }] } ],
      "containment": { "isContainer": false, "status": "free" } },
    { "id": "pr", "type": "StatementPrintInt", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 700, "y": 200 }, "size": { "width": 120, "height": 74 },
      "connectors": [ { "port": "value", "dataType": "int", "isOutput": false,
        "connections": [{ "wireId": "wo2", "targetDevice": "rt", "targetPort": "out" }] } ],
      "containment": { "isContainer": false, "status": "free" } }
  ],
  "wires": [
    { "id": "wo1", "from": { "device": "cext", "port": "output" }, "to": { "device": "pt", "port": "in" }, "dataType": "int" },
    { "id": "wi1", "from": { "device": "pt", "port": "out" }, "to": { "device": "rt", "port": "in" }, "dataType": "int" },
    { "id": "wo2", "from": { "device": "rt", "port": "out" }, "to": { "device": "pr", "port": "value" }, "dataType": "int" }
  ]
}`

func TestFunctionImplicitSelfCall(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene: json.RawMessage(selfcallScene), Language: "c",
	})
	code := generatedCode(resp)
	// The call must carry the REAL argument (the outer const's
	// register), and the print must consume the call's result var —
	// the weak substrings let the port-index bug slip (2026-07-21).
	// Português: A chamada deve levar o argumento REAL e o print
	// consumir a variável do call — asserts fracos deixaram o bug
	// do índice passar.
	if !strings.Contains(code, "my_function(cext") {
		t.Fatalf("implicit call must carry the outer argument; got:\n%s", code)
	}
	callVar := "fn_selfcall_y_out"
	pi := strings.Index(code, "printf")
	if pi < 0 || !strings.Contains(code[pi:], callVar) {
		t.Fatalf("print must consume the call's result var %q; got:\n%s", callVar, code)
	}
	if !strings.Contains(code, "printf") {
		t.Fatalf("outer consumer lost; got:\n%s", code)
	}
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Message, "no caller") {
			t.Fatalf("uncalled warning must die for outer-wired defs; diags: %+v",
				resp.Diagnostics)
		}
	}
}
