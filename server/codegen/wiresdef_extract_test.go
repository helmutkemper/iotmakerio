// server/codegen/wiresdef_extract_test.go
//
// P3 coverage: ExtractFunctionDef captures a Function's subtree as a
// wires-origin def, rejects invalid signatures before anything could
// be stored, and — the golden loop — the captured def round-trips
// through the P2 engine into a real call. Português: Cobertura P3 — o
// extrator captura o subtree como def, rejeita assinatura inválida
// antes de qualquer gravação, e a def capturada fecha o ciclo de ouro
// virando chamada real pelo motor P2.
package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/graph"
)

func TestExtractFunctionDef(t *testing.T) {
	t.Run("captures the subtree as a wires def", func(t *testing.T) {
		def, diags := ExtractFunctionDef(
			[]byte(functionSignatureScene(true)), "fn_1")
		if def == nil {
			t.Fatalf("extraction failed: %+v", diags)
		}
		if def.Name != "myFunc" || def.Origin != "wires" {
			t.Fatalf("def identity wrong: %+v", def)
		}
		var sub graph.SceneInput
		if err := json.Unmarshal(def.Scene, &sub); err != nil {
			t.Fatalf("captured scene unreadable: %v", err)
		}
		if len(sub.Devices) != 5 || len(sub.Wires) != 2 {
			t.Fatalf("subtree wrong: devices=%d wires=%d", len(sub.Devices), len(sub.Wires))
		}
	})

	t.Run("untyped slot is refused before the shelf", func(t *testing.T) {
		def, diags := ExtractFunctionDef(
			[]byte(functionSignatureScene(false)), "fn_1")
		if def != nil {
			t.Fatalf("invalid signature must not extract")
		}
		found := false
		for _, d := range diags {
			if strings.Contains(d.Message, "has no type") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected the untyped-slot error; got: %+v", diags)
		}
	})

	t.Run("golden loop: captured def becomes a real call", func(t *testing.T) {
		def, diags := ExtractFunctionDef(
			[]byte(functionSignatureScene(true)), "fn_1")
		if def == nil {
			t.Fatalf("extraction failed: %+v", diags)
		}
		caller := `{
  "version": "1.0",
  "metadata": { "language": "go" },
  "devices": [
    {
      "id": "c1", "type": "StatementConstInt", "kind": "simple", "stage": "backend",
      "properties": { "value": 2 },
      "position": { "x": 0, "y": 0 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w1", "targetDevice": "k1", "targetPort": "sensor_value" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "k1", "type": "StatementFunctionCall", "kind": "simple", "stage": "backend",
      "properties": { "function": "myFunc" },
      "position": { "x": 200, "y": 0 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "sensor_value", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "c1", "targetPort": "output" }] },
        { "port": "doubled", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w2", "targetDevice": "p1", "targetPort": "value" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "p1", "type": "StatementPrintInt", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 400, "y": 0 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "k1", "targetPort": "doubled" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "c1", "port": "output" }, "to": { "device": "k1", "port": "sensor_value" }, "dataType": "int" },
    { "id": "w2", "from": { "device": "k1", "port": "doubled" }, "to": { "device": "p1", "port": "value" }, "dataType": "int" }
  ]
}`
		resp := Generate(context.Background(), Request{
			Scene:    json.RawMessage(caller),
			Language: "go",
			BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
				"myFunc": def,
			},
		})
		code := generatedCode(resp)
		if !strings.Contains(code, "func myFunc(sensor_value int64) int64 {") {
			t.Fatalf("golden loop: expanded header missing; got:\n%s", code)
		}
		if !strings.Contains(code, ":= myFunc(") {
			t.Fatalf("golden loop: call missing; got:\n%s", code)
		}
	})
}
