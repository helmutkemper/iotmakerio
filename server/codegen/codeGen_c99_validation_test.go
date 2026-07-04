// /server/codegen/codeGen_c99_validation_test.go
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

// c99AddDefs builds the BlackBoxDefs map the way store.LoadBlackBoxDefsForScene
// now does for a C99 source: the def has no struct name and is keyed by the
// function name ("add"), not a struct name. The "add" function declares two
// mandatory int inputs and one return output.
func c99AddDefs() map[string]*blackbox.BlackBoxDef {
	def := &blackbox.BlackBoxDef{
		Functions: []blackbox.NamedFuncDef{
			{
				Name: "add",
				FuncDef: blackbox.FuncDef{
					Inputs: []blackbox.PortDef{
						{Name: "a", GoType: "int", Connection: "mandatory"},
						{Name: "b", GoType: "int", Connection: "mandatory"},
					},
					Outputs: []blackbox.PortDef{{Name: "return", GoType: "int"}},
				},
			},
		},
	}
	return map[string]*blackbox.BlackBoxDef{"add": def}
}

// sceneC99Add wires two ConstInt outputs into the "a" and "b" inputs of an
// "add" function-device (type "BlackBoxadd:", empty struct part).
const sceneC99Add = `{
  "version": "1.0",
  "devices": [
    { "id": "constInt_a", "type": "StatementConstInt", "properties": { "value": 2 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [ { "wireId": "wa", "targetDevice": "add_1", "targetPort": "a" } ] } ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },
    { "id": "constInt_b", "type": "StatementConstInt", "properties": { "value": 3 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [ { "wireId": "wb", "targetDevice": "add_1", "targetPort": "b" } ] } ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },
    { "id": "add_1", "type": "BlackBoxadd:", "properties": {},
      "connectors": [
        { "port": "a", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wa", "targetDevice": "constInt_a", "targetPort": "output" } ] },
        { "port": "b", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wb", "targetDevice": "constInt_b", "targetPort": "output" } ] },
        { "port": "return", "dataType": "int", "isOutput": true, "connections": [] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } }
  ],
  "wires": [
    { "id": "wa", "from": { "device": "constInt_a", "port": "output" }, "to": { "device": "add_1", "port": "a" }, "dataType": "int" },
    { "id": "wb", "from": { "device": "constInt_b", "port": "output" }, "to": { "device": "add_1", "port": "b" }, "dataType": "int" }
  ]
}`

// sceneC99AddMissingB is the same scene with input "b" left unconnected — the
// mandatory-input check must flag it.
const sceneC99AddMissingB = `{
  "version": "1.0",
  "devices": [
    { "id": "constInt_a", "type": "StatementConstInt", "properties": { "value": 2 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [ { "wireId": "wa", "targetDevice": "add_1", "targetPort": "a" } ] } ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },
    { "id": "add_1", "type": "BlackBoxadd:", "properties": {},
      "connectors": [
        { "port": "a", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wa", "targetDevice": "constInt_a", "targetPort": "output" } ] },
        { "port": "b", "dataType": "int", "isOutput": false, "connections": [] },
        { "port": "return", "dataType": "int", "isOutput": true, "connections": [] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } }
  ],
  "wires": [
    { "id": "wa", "from": { "device": "constInt_a", "port": "output" }, "to": { "device": "add_1", "port": "a" }, "dataType": "int" }
  ]
}`

func hasDiagContaining(diags []Diagnostic, substr string) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

// TestValidate_C99FunctionDevice_Present is the fix for the "black-box
// definition \"\" not found" error: with the def keyed by function name, a
// fully-wired function-device scene validates cleanly and lowers to BB_CALL.
func TestValidate_C99FunctionDevice_Present(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99Add),
		Language:     "c",
		BlackBoxDefs: c99AddDefs(),
	})

	for _, d := range resp.Diagnostics {
		if d.Severity == "error" {
			t.Fatalf("unexpected error diagnostic: %+v", d)
		}
	}
	if hasDiagContaining(resp.Diagnostics, "not found") {
		t.Fatalf("function-device def should resolve by function name; got: %+v", resp.Diagnostics)
	}
	// validate() passed, so the pipeline reached IR emission.
	if !strings.Contains(resp.IR, "BB_CALL") || !strings.Contains(resp.IR, "fn=add") {
		t.Fatalf("expected BB_CALL fn=add in IR, got:\n%s", resp.IR)
	}
}

// TestValidate_C99FunctionDevice_Missing checks the corrected diagnostic when
// no def is supplied: it must name the function, not an empty struct.
func TestValidate_C99FunctionDevice_Missing(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99Add),
		Language:     "c",
		BlackBoxDefs: nil, // no defs loaded
	})

	if !hasDiagContaining(resp.Diagnostics, `function-device "add" not found`) {
		t.Fatalf("expected 'function-device \"add\" not found' diagnostic, got: %+v", resp.Diagnostics)
	}
}

// TestValidate_C99FunctionDevice_MandatoryInput checks that an unconnected
// mandatory input is reported (and blocks codegen).
func TestValidate_C99FunctionDevice_MandatoryInput(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99AddMissingB),
		Language:     "c",
		BlackBoxDefs: c99AddDefs(),
	})

	if !hasDiagContaining(resp.Diagnostics, "add_1.b") {
		t.Fatalf("expected a 'not connected' diagnostic for add_1.b, got: %+v", resp.Diagnostics)
	}
}

// sceneStringAdd is a single StatementAdd in string mode (concatenation) with
// no inputs wired. It exists only to exercise the C-target string-concat guard;
// the unwired inputs also produce missing-connection diagnostics, which the
// test ignores — it asserts only on the string-concat diagnostic.
//
// Português: Um único StatementAdd em modo string (concatenação) sem entradas
// ligadas. Existe só pra exercitar o guard de concat no alvo C; as entradas
// soltas também geram diagnósticos de conexão, que o teste ignora — ele afere
// só o diagnóstico de concatenação.
const sceneStringAdd = `{
  "devices": [
    {
      "id": "add_1",
      "type": "StatementAdd",
      "properties": { "dataType": "string" },
      "connectors": [
        { "port": "inputX", "dataType": "string", "isOutput": false, "connections": [] },
        { "port": "inputY", "dataType": "string", "isOutput": false, "connections": [] },
        { "port": "output", "dataType": "string", "isOutput": true, "connections": [] }
      ]
    }
  ],
  "wires": []
}`

// TestValidate_C99_StringConcatAllowed proves the C target no longer blocks a
// string-mode Add: the C backend now lowers string concatenation to a bounded
// snprintf copy (see ansic.emitStringConcat), so the old blocking diagnostic
// must be gone on both targets.
//
// Português: Prova que o alvo C não bloqueia mais um Add em modo string: o
// backend C agora faz o lowering da concatenação para uma cópia limitada com
// snprintf (ver ansic.emitStringConcat), então o antigo diagnóstico bloqueante
// deve ter sumido nos dois alvos.
func TestValidate_C99_StringConcatAllowed(t *testing.T) {
	const blockMsg = "string concatenation is not supported"

	respC := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneStringAdd),
		Language: "c",
	})
	if hasDiagContaining(respC.Diagnostics, blockMsg) {
		t.Fatalf("C target must no longer block string concatenation; got: %+v", respC.Diagnostics)
	}

	respGo := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneStringAdd),
		Language: "go",
	})
	if hasDiagContaining(respGo.Diagnostics, blockMsg) {
		t.Fatalf("Go target must not block string concatenation; got: %+v", respGo.Diagnostics)
	}
}
