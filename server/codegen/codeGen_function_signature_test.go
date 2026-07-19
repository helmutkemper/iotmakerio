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
      "properties": { "functionName": "myFunc", "comment": "select the color" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 420, "height": 300 },
      "connectors": [],
      "containment": { "isContainer": true, "children": ["printInt_1", "constInt_1"] }
    },
    {
      "id": "tunnel_p", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "sensor value", "tunnelParent": "fn_1", "tunnelSide": "left", "comment": "the color index\n0: red\n1: green" },
      "position": { "x": 0, "y": 80 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "int", "isOutput": false, "connections": [] },
        { "port": "out", "dataType": "int", "isOutput": true, "connections": [%s] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "tunnel_r", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "doubled", "tunnelParent": "fn_1", "tunnelSide": "right", "comment": "true when found" },
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

// generatedCode returns ALL generated sources concatenated — map
// iteration order is nondeterministic (a lesson paid in the ritual:
// the first version returned ONE random file and once picked the
// Makefile), and the asserts are substring searches, so the
// concatenation is order-proof. Português: TODOS os fontes gerados
// concatenados — ordem de mapa é não-determinística (a primeira versão
// devolvia UM arquivo aleatório e um dia pegou o Makefile); os asserts
// são busca de substring, então a concatenação é à prova de ordem.
func generatedCode(resp Response) string {
	var sb strings.Builder
	sb.WriteString(resp.Code)
	for _, v := range resp.Files {
		sb.WriteString(v)
		sb.WriteString("\n")
	}
	return sb.String()
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
		// Signature docs (field 2026-07-19): the function's comment and
		// the port notes render ABOVE the lifted header, exactly once —
		// never inside main. Português: Docs da assinatura acima do
		// cabeçalho, exatamente uma vez — nunca dentro do main.
		if strings.Count(code, "// select the color") != 1 {
			t.Fatalf("function comment must appear exactly once; got:\n%s", code)
		}
		if strings.Index(code, "// select the color") > strings.Index(code, "func myFunc(") {
			t.Fatalf("function comment must sit above the header")
		}
		if !strings.Contains(code, "// sensor_value: the color index") ||
			!strings.Contains(code, "//   0: red") ||
			!strings.Contains(code, "//   1: green") ||
			!strings.Contains(code, "// doubled: true when found") {
			t.Fatalf("port docs missing or continuation unprefixed; got:\n%s", code)
		}
		if strings.Contains(code, "\n0: red") {
			t.Fatalf("bare continuation line escaped the comment; got:\n%s", code)
		}
		// The outer faces belong to the caller — a healthy signature
		// must produce ZERO tunnel plumbing warnings (field 2026-07-19).
		// Português: Faces externas são do caller — assinatura saudável
		// produz ZERO avisos de encanamento de túnel.
		for _, d := range resp.Diagnostics {
			if strings.Contains(d.Message, "not connected") &&
				strings.Contains(d.Message, "tunnel") {
				t.Fatalf("signature tunnel got a plumbing warning: %s", d.Message)
			}
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
		if strings.Count(code, "// select the color") != 1 ||
			!strings.Contains(code, "// sensor_value: the color index") ||
			!strings.Contains(code, "// doubled: true when found") {
			t.Fatalf("c signature docs wrong; got:\n%s", code)
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
