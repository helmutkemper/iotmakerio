package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Field repro 2026-07-19: function { param tunnel → loop.stop } emitted
// an EMPTY main.c. Português: Repro do campo — função com loop cujo
// stop vem do parâmetro gerou main.c vazio.
const fnLoopScene = `{
  "version": "1.0",
  "metadata": { "language": "c", "target": "pi_linux" },
  "devices": [
    { "id": "fn", "type": "StatementFunction", "kind": "complex", "stage": "backend",
      "properties": { "functionName": "my_function" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 600, "height": 500 },
      "connectors": [],
      "containment": { "isContainer": true, "children": ["lp"] } },
    { "id": "lp", "type": "StatementLoop", "kind": "complex", "stage": "backend",
      "position": { "x": 100, "y": 100 }, "size": { "width": 300, "height": 250 },
      "connectors": [
        { "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
          "connections": [{ "wireId": "w1", "targetDevice": "tp", "targetPort": "out" }] }
      ],
      "containment": { "isContainer": true, "parent": "fn", "children": [], "status": "contained" } },
    { "id": "tp", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "stop_flag", "tunnelParent": "fn", "tunnelSide": "left" },
      "position": { "x": 0, "y": 200 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "*", "isOutput": false, "connections": [] },
        { "port": "out", "dataType": "*", "isOutput": true,
          "connections": [{ "wireId": "w1", "targetDevice": "lp", "targetPort": "stop" }] }
      ],
      "containment": { "isContainer": false, "status": "free" } }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "tp", "port": "out" }, "to": { "device": "lp", "port": "stop" }, "dataType": "bool" }
  ]
}`

func TestFunctionLoopStopParam(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene: json.RawMessage(fnLoopScene), Language: "c",
	})
	code := generatedCode(resp)
	if !strings.Contains(code, "static void my_function(") {
		t.Fatalf("function missing from output; got:\n%s", code)
	}
	if !strings.Contains(code, "while") {
		t.Fatalf("loop body missing; got:\n%s", code)
	}
	// The downgraded stop warning must ride along without emptying
	// the program. Português: O warning rebaixado viaja sem esvaziar
	// o programa.
	found := false
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Message, "call-time constant") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the call-time-constant warning; got %+v", resp.Diagnostics)
	}
}
