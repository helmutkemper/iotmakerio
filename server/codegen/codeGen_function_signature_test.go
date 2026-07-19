// server/codegen/codeGen_function_signature_test.go
//
// Fatia C offline coverage: a Function's phase-tunnels become its
// SIGNATURE — left tunnels are parameters (name = sanitized label,
// type = the wire's stamp, order = stage Y), right tunnels are returns
// (Go multi-return; C99 out-params, void function). One fabricated
// scene, three verdicts: the Go header, the C header, and the untyped
// diagnostic. Português: Cobertura da Fatia C — túneis viram a
// ASSINATURA da função: esquerda = parâmetros, direita = retornos.
package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"server/codegen/diagnostics"
)

// functionSignatureScene fabricates: myFunc containing a print (body
// consumer of the parameter) and a const (feeding the return). The
// param tunnel sits LEFT with label "sensor value"; the return tunnel
// sits RIGHT with label "doubled". wireParam=false drops the param
// tunnel's wire — the untyped-diagnostic shape. Português: Fabrica a
// cena; wireParam=false tira o fio do túnel-parâmetro (forma do
// diagnóstico sem-tipo).
func functionSignatureScene(wireParam bool) string {
	paramConns := ""
	printConns := ""
	paramWire := ""
	if wireParam {
		paramConns = `{ "wireId": "wp", "targetDevice": "printInt_1", "targetPort": "value" }`
		printConns = `{ "wireId": "wp", "targetDevice": "tunnel_p", "targetPort": "out" }`
		paramWire = `{ "id": "wp", "from": { "device": "tunnel_p", "port": "out" }, "to": { "device": "printInt_1", "port": "value" }, "dataType": "int" },`
	}
	return fmt.Sprintf(`{
  "version": "1.0",
  "metadata": { "language": "go" },
  "devices": [
    {
      "id": "fn_1", "type": "StatementFunction", "kind": "complex", "stage": "backend",
      "properties": { "functionName": "myFunc" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 420, "height": 300 },
      "connectors": [],
      "containment": { "isContainer": true, "children": ["printInt_1", "constInt_1"] }
    },
    {
      "id": "tunnel_p", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "sensor value", "tunnelParent": "fn_1", "tunnelSide": "left" },
      "position": { "x": 0, "y": 80 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "int", "isOutput": false, "connections": [] },
        { "port": "out", "dataType": "int", "isOutput": true, "connections": [%s] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "tunnel_r", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "doubled", "tunnelParent": "fn_1", "tunnelSide": "right" },
      "position": { "x": 420, "y": 120 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "wr", "targetDevice": "constInt_1", "targetPort": "output" }] },
        { "port": "out", "dataType": "int", "isOutput": true, "connections": [] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "printInt_1", "type": "StatementPrintInt", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 120, "y": 70 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false, "connections": [%s] }
      ],
      "containment": { "isContainer": false, "parent": "fn_1", "status": "contained" }
    },
    {
      "id": "constInt_1", "type": "StatementConstInt", "kind": "simple", "stage": "backend",
      "properties": { "value": 7 },
      "position": { "x": 120, "y": 170 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "wr", "targetDevice": "tunnel_r", "targetPort": "in" }] }
      ],
      "containment": { "isContainer": false, "parent": "fn_1", "status": "contained" }
    }
  ],
  "wires": [
    %s
    { "id": "wr", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "tunnel_r", "port": "in" }, "dataType": "int" }
  ]
}`, paramConns, printConns, paramWire)
}

// generatedCode returns the single generated source, wherever the
// backend put it (multi-file Files or the legacy Code mirror).
// Português: O fonte gerado, onde quer que o backend o tenha posto.
func generatedCode(resp Response) string {
	if resp.Code != "" {
		return resp.Code
	}
	for _, v := range resp.Files {
		return v
	}
	return ""
}

func TestFunctionTunnelSignature(t *testing.T) {
	t.Run("go: params and return in the header", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene:    json.RawMessage(functionSignatureScene(true)),
			Language: "go",
		})
		code := generatedCode(resp)
		// Abstract numerics: the Go target maps int → int64; and with a
		// function present, consts promote to file-scope VARs — the
		// return carries the promoted identifier. Português: Numéricos
		// abstratos (int→int64 no Go) e promoção a VAR de arquivo — o
		// return leva o identificador promovido.
		if !strings.Contains(code, "func myFunc(sensor_value int64) int64 {") {
			t.Fatalf("go header missing tunnel signature; got:\n%s", code)
		}
		if !strings.Contains(code, "return constInt1") {
			t.Fatalf("return statement missing the promoted feed; got:\n%s", code)
		}
	})

	t.Run("c99: out-param header and assignment", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene:    json.RawMessage(functionSignatureScene(true)),
			Language: "c",
		})
		code := generatedCode(resp)
		// Default profile is arduino_uno: abstract int → int32_t; the
		// const rides the file-scope VAR promotion, so the out-param
		// assignment carries the promoted identifier. Português: Profile
		// default arduino_uno (int→int32_t); o const promovido alimenta
		// o out-param.
		if !strings.Contains(code, "int32_t sensor_value") ||
			!strings.Contains(code, "int32_t *doubled") {
			t.Fatalf("c header missing signature parts; got:\n%s", code)
		}
		if !strings.Contains(code, "*doubled = constInt1;") {
			t.Fatalf("c out-param assignment missing; got:\n%s", code)
		}
		if !strings.Contains(code, "static void myFunc(") {
			t.Fatalf("c function must stay void (returns ride out-params); got:\n%s", code)
		}
	})

	t.Run("untyped parameter diagnoses", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene:    json.RawMessage(functionSignatureScene(false)),
			Language: "go",
		})
		found := false
		for _, d := range resp.Diagnostics {
			if d.Kind == diagnostics.KindFunctionSignature &&
				d.Severity == diagnostics.SeverityError &&
				strings.Contains(d.Message, "has no type") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a %s error about the untyped slot; diags: %+v",
				diagnostics.KindFunctionSignature, resp.Diagnostics)
		}
	})
}
