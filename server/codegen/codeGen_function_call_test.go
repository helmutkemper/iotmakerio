// server/codegen/codeGen_function_call_test.go
//
// P2 engine coverage (the "my_function becomes a device" arc): a
// wires-origin black-box def expands into the scene, its instance
// lowers to a real call in both backends, the uncalled warning dies,
// and recursion between drawn functions is refused. The identity
// function (param tunnel wired straight into the return tunnel) is the
// minimal body that exercises the whole chain. Português: Cobertura do
// motor P2 — def de origem-fios expande, instância vira chamada real
// nos dois alvos, o uncalled morre, e recursão é recusada. A função
// identidade é o corpo mínimo que exercita a cadeia inteira.
package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/diagnostics"
)

// identityDefScene: my_ident(x int) int { return x } drawn in wires —
// the param tunnel's out feeds the return tunnel's in directly.
// Português: A identidade desenhada — out do parâmetro alimenta o in
// do retorno diretamente.
const identityDefScene = `{
  "version": "1.0",
  "metadata": { "language": "go" },
  "devices": [
    {
      "id": "fn", "type": "StatementFunction", "kind": "complex", "stage": "backend",
      "properties": { "functionName": "my_ident" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 300, "height": 200 },
      "connectors": [],
      "containment": { "isContainer": true, "children": [] }
    },
    {
      "id": "tp", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "x", "tunnelParent": "fn", "tunnelSide": "left" },
      "position": { "x": 0, "y": 60 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "int", "isOutput": false, "connections": [] },
        { "port": "out", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w1", "targetDevice": "tr", "targetPort": "in" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "tr", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "y", "tunnelParent": "fn", "tunnelSide": "right" },
      "position": { "x": 300, "y": 60 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "tp", "targetPort": "out" }] },
        { "port": "out", "dataType": "int", "isOutput": true, "connections": [] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "tp", "port": "out" }, "to": { "device": "tr", "port": "in" }, "dataType": "int" }
  ]
}`

// callerScene: constInt 7 → instance.x; instance.y → printInt.
// Português: A cena chamadora — const 7 na entrada, print na saída.
const callerScene = `{
  "version": "1.0",
  "metadata": { "language": "go" },
  "devices": [
    {
      "id": "constInt_1", "type": "StatementConstInt", "kind": "simple", "stage": "backend",
      "properties": { "value": 7 },
      "position": { "x": 0, "y": 0 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "wa", "targetDevice": "call_1", "targetPort": "x" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "call_1", "type": "StatementFunctionCall", "kind": "simple", "stage": "backend",
      "properties": { "function": "my_ident" },
      "position": { "x": 200, "y": 0 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "x", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "wa", "targetDevice": "constInt_1", "targetPort": "output" }] },
        { "port": "y", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "wb", "targetDevice": "printInt_1", "targetPort": "value" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "printInt_1", "type": "StatementPrintInt", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 400, "y": 0 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "wb", "targetDevice": "call_1", "targetPort": "y" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "wa", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "call_1", "port": "x" }, "dataType": "int" },
    { "id": "wb", "from": { "device": "call_1", "port": "y" }, "to": { "device": "printInt_1", "port": "value" }, "dataType": "int" }
  ]
}`

func identityDefs() map[string]*blackbox.BlackBoxDef {
	return map[string]*blackbox.BlackBoxDef{
		"my_ident": {
			Name:   "my_ident",
			Origin: "wires",
			Scene:  json.RawMessage(identityDefScene),
		},
	}
}

func TestGraphicalFunctionCall(t *testing.T) {
	t.Run("go: expansion, real call, silenced uncalled", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene:        json.RawMessage(callerScene),
			Language:     "go",
			BlackBoxDefs: identityDefs(),
		})
		code := generatedCode(resp)
		if !strings.Contains(code, "func my_ident(x int64) int64 {") {
			t.Fatalf("expanded function header missing; got:\n%s", code)
		}
		if !strings.Contains(code, "return x") {
			t.Fatalf("identity body missing; got:\n%s", code)
		}
		if !strings.Contains(code, ":= my_ident(") {
			t.Fatalf("call binding missing; got:\n%s", code)
		}
		for _, d := range resp.Diagnostics {
			if d.Kind == diagnostics.KindFunctionUncalled {
				t.Fatalf("uncalled must be silenced when an instance exists: %s", d.Message)
			}
		}
	})

	t.Run("c99: out-param call with declared destination", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene:        json.RawMessage(callerScene),
			Language:     "c",
			BlackBoxDefs: identityDefs(),
		})
		code := generatedCode(resp)
		if !strings.Contains(code, "static void my_ident(int32_t x, int32_t *y)") {
			t.Fatalf("c function header missing; got:\n%s", code)
		}
		if !strings.Contains(code, "*y = x;") {
			t.Fatalf("c identity body missing; got:\n%s", code)
		}
		// cIdent strips underscores: call_1_y renders as call1_y.
		// Português: cIdent come underscores.
		// No shadowed file-scope twin for intra-function values (the
		// cc -Wunused-variable regression, 2026-07-19). Português: Sem
		// gêmeo de arquivo sombreado para valores intra-função.
		if strings.Contains(code, "Shared state") {
			t.Fatalf("intra-function value wrongly promoted to file scope; got:\n%s", code)
		}
		if !strings.Contains(code, "int32_t call1_y;") ||
			!strings.Contains(code, "my_ident(") ||
			!strings.Contains(code, "&call1_y") {
			t.Fatalf("c call site wrong; got:\n%s", code)
		}
	})

	t.Run("recursion between drawn functions is refused", func(t *testing.T) {
		// Build the self-calling def honestly: parse, append the
		// instance, re-marshal. Português: Constrói a def
		// auto-chamadora honestamente — parse, injeta, re-serializa.
		var sc map[string]interface{}
		if err := json.Unmarshal([]byte(identityDefScene), &sc); err != nil {
			t.Fatalf("fixture parse: %v", err)
		}
		devs := sc["devices"].([]interface{})
		devs = append(devs, map[string]interface{}{
			"id": "inner", "type": "StatementFunctionCall", "kind": "simple", "stage": "backend",
			"properties":  map[string]interface{}{"function": "my_ident"},
			"position":    map[string]interface{}{"x": 120, "y": 60},
			"size":        map[string]interface{}{"width": 120, "height": 74},
			"connectors":  []interface{}{},
			"containment": map[string]interface{}{"isContainer": false, "parent": "fn", "status": "contained"},
		})
		sc["devices"] = devs
		selfScene, err := json.Marshal(sc)
		if err != nil {
			t.Fatalf("fixture marshal: %v", err)
		}
		defs := map[string]*blackbox.BlackBoxDef{
			"my_ident": {Name: "my_ident", Origin: "wires", Scene: json.RawMessage(selfScene)},
		}
		resp := Generate(context.Background(), Request{
			Scene:        json.RawMessage(callerScene),
			Language:     "go",
			BlackBoxDefs: defs,
		})
		found := false
		for _, d := range resp.Diagnostics {
			if d.Kind == diagnostics.KindFunctionCycle && d.Severity == diagnostics.SeverityError {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a %s error; diags: %+v", diagnostics.KindFunctionCycle, resp.Diagnostics)
		}
	})
}
