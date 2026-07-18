// /server/codegen/codeGen_variables_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/ir"
)

// sceneVariables wires ConstInt(5) → SetVarInt("counter") and
// GetVarInt("counter") → SetVarInt("mirror"). It exercises all three halves of
// the variable slice: the zero-initialised declaration, a SetVar assignment,
// and a GetVar read resolved as a register alias — mirror must take counter's
// value straight from the variable register, with no intermediate copy.
//
// Português: Liga ConstInt(5) → SetVarInt("counter") e GetVarInt("counter") →
// SetVarInt("mirror"). Exercita as três partes da fatia: declaração zero-init,
// atribuição SetVar, e leitura GetVar resolvida como alias de registrador —
// mirror toma o valor de counter direto do registrador, sem cópia.
const sceneVariables = `{
  "devices": [
    {
      "id": "const_5",
      "type": "StatementConstInt",
      "properties": { "value": 5 },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "connections": [
          { "wireId": "w1", "targetDevice": "setVar_counter", "targetPort": "value" }
        ]}
      ]
    },
    {
      "id": "setVar_counter",
      "type": "StatementSetVarInt",
      "properties": { "varName": "counter" },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false, "connections": [
          { "wireId": "w1", "targetDevice": "const_5", "targetPort": "output" }
        ]}
      ]
    },
    {
      "id": "getVar_counter",
      "type": "StatementGetVarInt",
      "properties": { "varName": "counter" },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "connections": [
          { "wireId": "w2", "targetDevice": "setVar_mirror", "targetPort": "value" }
        ]}
      ]
    },
    {
      "id": "setVar_mirror",
      "type": "StatementSetVarInt",
      "properties": { "varName": "mirror" },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false, "connections": [
          { "wireId": "w2", "targetDevice": "getVar_counter", "targetPort": "output" }
        ]}
      ]
    }
  ],
  "wires": [
    { "id": "w1", "from": {"device": "const_5", "port": "output"}, "to": {"device": "setVar_counter", "port": "value"}, "dataType": "int" },
    { "id": "w2", "from": {"device": "getVar_counter", "port": "output"}, "to": {"device": "setVar_mirror", "port": "value"}, "dataType": "int" }
  ],
  "variables": [
    { "name": "counter", "type": "int" },
    { "name": "mirror",  "type": "int" }
  ]
}`

func TestVariables_DeclareSetGetAlias(t *testing.T) {
	vars := []ir.VariableDecl{
		{Name: "counter", Type: "int"},
		{Name: "mirror", Type: "int"},
	}

	// The IR is the language-independent proof of the slice.
	resp := Generate(context.Background(), Request{
		Scene:     json.RawMessage(sceneVariables),
		Language:  "go",
		Variables: vars,
	})
	irText := resp.IR
	for _, want := range []string{
		"VAR %counter int 0",           // declaration, zero-initialised
		"VAR %mirror int 0",            // declaration, zero-initialised
		"ASSIGN %counter int %const_5", // SetVar writes counter from the const
		"ASSIGN %mirror int %counter",  // GetVar alias: mirror reads the counter register
	} {
		if !strings.Contains(irText, want) {
			t.Fatalf("IR missing %q\n--- IR ---\n%s\ndiagnostics: %+v", want, irText, resp.Diagnostics)
		}
	}

	// Go end-to-end: the backend must resolve every register to a real
	// identifier — no "%"-prefixed token may leak into the source. After the
	// emitAssign fix, the SetVar from the const reads the const's identifier and
	// the SetVar from the GetVar reads the variable directly. (C wraps logic in
	// a loop; a loopless scene emits no C, so the language-independent IR
	// assertions above are the proof that covers the C path for this slice.)
	//
	// Português: Go ponta a ponta: o backend resolve todo registrador a um
	// identificador real — nenhum token com "%" pode vazar. Depois da correção
	// no emitAssign, o SetVar do const lê o identificador do const e o SetVar do
	// GetVar lê a variável direto. (C envolve a lógica num loop; cena sem loop
	// não emite C, então as asserções de IR acima são a prova que cobre o
	// caminho C nesta fatia.)
	goResp := Generate(context.Background(), Request{
		Scene:     json.RawMessage(sceneVariables),
		Language:  "go",
		Variables: vars,
	})
	code := goResp.Files["main.go"]
	if strings.Contains(code, "%") {
		t.Fatalf("a register leaked into Go source (unresolved %%):\n%s", code)
	}
	for _, want := range []string{
		"var counter",      // zero-init declaration
		"var mirror",       // zero-init declaration
		"counter = const5", // SetVar reads the const's identifier
		"mirror = counter", // SetVar reads the variable via the GetVar alias
	} {
		if !strings.Contains(code, want) {
			t.Fatalf("Go code missing %q\n--- code ---\n%s", want, code)
		}
	}

	// C end-to-end: a user variable must be declared WITH its zero initialiser
	// (int32_t counter = 0; on the arduino_uno profile). C does not zero-
	// initialise locals, so a variable that no SetVar ever writes would
	// otherwise hold garbage — this is the emitVar "varInit" path. Wire-
	// promotion OpVars carry no marker and stay bare declarations.
	//
	// Português: C ponta a ponta: uma variável de usuário deve ser declarada COM
	// seu inicializador zero (int32_t counter = 0; no perfil arduino_uno). C não
	// zera locais, então uma variável que nenhum SetVar escreve ficaria com lixo
	// — é o caminho "varInit" do emitVar. OpVars de promoção continuam nus.
	cResp := Generate(context.Background(), Request{
		Scene:     json.RawMessage(sceneVariables),
		Language:  "c",
		Variables: vars,
	})
	// C is a multi-file backend: the source lands in resp.Files (main.c), not
	// resp.Files["main.go"] (which only single-file backends like Go populate).
	cCode := cResp.Files["main.c"]
	for name, f := range cResp.Files {
		if name != "main.c" {
			cCode += f
		}
	}
	for _, want := range []string{
		"int32_t counter = 0L;",
		"int32_t mirror = 0L;",
	} {
		if !strings.Contains(cCode, want) {
			t.Fatalf("C code missing %q\n--- C files ---\n%s\ndiagnostics: %+v", want, cCode, cResp.Diagnostics)
		}
	}

	// Complete-case demo (visible with `go test -v`): the full Go and C source
	// generated for a Const → SetVar → GetVar → SetVar scene — the whole
	// variable lifecycle (declare, write from a const, read via the alias, write
	// to a second variable) in both backends.
	t.Logf("\n========== COMPLETE CASE — Go ==========\n%s\n========== COMPLETE CASE — C (main.c) ==========\n%s", code, cCode)
}

// TestVariables_EmbeddedInScene proves Path A: when the caller does NOT set
// req.Variables, the declarations are read from the scene's top-level
// "variables" array. This is the real IDE flow — codegen has no project_id, so
// the scene is the complete input. The IR must be identical to the explicit-
// injection test above.
//
// Português: Prova o Caminho A — sem req.Variables setado, as declarações vêm
// do array "variables" no topo da cena (o fluxo real da IDE, codegen sem
// project_id). O IR tem que ser idêntico ao teste de injeção explícita acima.
func TestVariables_EmbeddedInScene(t *testing.T) {
	// No Variables in the Request — they must come from the scene itself.
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneVariables),
		Language: "go",
	})
	irText := resp.IR
	for _, want := range []string{
		"VAR %counter int 0",
		"VAR %mirror int 0",
		"ASSIGN %counter int %const_5",
		"ASSIGN %mirror int %counter",
	} {
		if !strings.Contains(irText, want) {
			t.Fatalf("IR missing %q (scene-embedded variables not picked up?)\n--- IR ---\n%s\ndiagnostics: %+v",
				want, irText, resp.Diagnostics)
		}
	}
}

// sceneVariablesFloat mirrors sceneVariables with float types: ConstFloat(0) →
// SetVarFloat("temp") and GetVarFloat("temp") → SetVarFloat("mirrorF"). It
// proves the float branch of the variable slice end-to-end.
const sceneVariablesFloat = `{
  "devices": [
    {
      "id": "constF_0",
      "type": "StatementConstFloat",
      "properties": { "value": 0.0 },
      "connectors": [
        { "port": "output", "dataType": "float", "isOutput": true, "connections": [
          { "wireId": "wf1", "targetDevice": "setVarF_temp", "targetPort": "value" }
        ]}
      ]
    },
    {
      "id": "setVarF_temp",
      "type": "StatementSetVarFloat",
      "properties": { "varName": "temp" },
      "connectors": [
        { "port": "value", "dataType": "float", "isOutput": false, "connections": [
          { "wireId": "wf1", "targetDevice": "constF_0", "targetPort": "output" }
        ]}
      ]
    },
    {
      "id": "getVarF_temp",
      "type": "StatementGetVarFloat",
      "properties": { "varName": "temp" },
      "connectors": [
        { "port": "output", "dataType": "float", "isOutput": true, "connections": [
          { "wireId": "wf2", "targetDevice": "setVarF_mirror", "targetPort": "value" }
        ]}
      ]
    },
    {
      "id": "setVarF_mirror",
      "type": "StatementSetVarFloat",
      "properties": { "varName": "mirrorF" },
      "connectors": [
        { "port": "value", "dataType": "float", "isOutput": false, "connections": [
          { "wireId": "wf2", "targetDevice": "getVarF_temp", "targetPort": "output" }
        ]}
      ]
    }
  ],
  "wires": [
    { "id": "wf1", "from": {"device": "constF_0", "port": "output"}, "to": {"device": "setVarF_temp", "port": "value"}, "dataType": "float" },
    { "id": "wf2", "from": {"device": "getVarF_temp", "port": "output"}, "to": {"device": "setVarF_mirror", "port": "value"}, "dataType": "float" }
  ],
  "variables": [
    { "name": "temp",    "type": "float" },
    { "name": "mirrorF", "type": "float" }
  ]
}`

// TestVariables_FloatZeroInit proves the float branch of the variable slice: a
// float variable is declared with its zero initialiser formatted for C as
// "0.0f" (the cLiteral float case + the emitVar varInit path) — the GetVarFloat
// device's codegen contract.
//
// Português: Prova o ramo float da fatia — variável float declarada com o
// inicializador zero formatado em C como "0.0f".
func TestVariables_FloatZeroInit(t *testing.T) {
	vars := []ir.VariableDecl{
		{Name: "temp", Type: "float"},
		{Name: "mirrorF", Type: "float"},
	}
	cResp := Generate(context.Background(), Request{
		Scene:     json.RawMessage(sceneVariablesFloat),
		Language:  "c",
		Variables: vars,
	})
	cCode := cResp.Files["main.c"]
	for name, f := range cResp.Files {
		if name != "main.c" {
			cCode += f
		}
	}
	for _, want := range []string{
		"float temp = 0.0f;",
		"float mirrorF = 0.0f;",
	} {
		if !strings.Contains(cCode, want) {
			t.Fatalf("C float var not zero-initialised: missing %q\n--- C ---\n%s\ndiag: %+v", want, cCode, cResp.Diagnostics)
		}
	}
}

// TestVariables_StringZeroInit closes the Get/Set family: a ConstString feeds a
// SetVarString, a GetVarString reads it back, and a second SetVarString copies
// it. Confirms string variables zero-initialise and assign correctly in both
// backends.
func TestVariables_StringZeroInit(t *testing.T) {
	const sceneVariablesString = `{
  "devices": [
    { "id": "constStr_0", "type": "StatementConstString", "properties": { "value": "hi" },
      "connectors": [ { "port": "output", "dataType": "string", "isOutput": true, "connections": [
        { "wireId": "w1", "targetDevice": "setVarStr_0", "targetPort": "value" } ] } ] },
    { "id": "setVarStr_0", "type": "StatementSetVarString", "properties": { "varName": "msg" },
      "connectors": [ { "port": "value", "dataType": "string", "isOutput": false, "connections": [
        { "wireId": "w1", "targetDevice": "constStr_0", "targetPort": "output" } ] } ] },
    { "id": "getVarStr_0", "type": "StatementGetVarString", "properties": { "varName": "msg" },
      "connectors": [ { "port": "output", "dataType": "string", "isOutput": true, "connections": [
        { "wireId": "w2", "targetDevice": "setVarStr_1", "targetPort": "value" } ] } ] },
    { "id": "setVarStr_1", "type": "StatementSetVarString", "properties": { "varName": "copy" },
      "connectors": [ { "port": "value", "dataType": "string", "isOutput": false, "connections": [
        { "wireId": "w2", "targetDevice": "getVarStr_0", "targetPort": "output" } ] } ] }
  ],
  "wires": [
    { "id": "w1", "from": {"device": "constStr_0", "port": "output"}, "to": {"device": "setVarStr_0", "port": "value"}, "dataType": "string" },
    { "id": "w2", "from": {"device": "getVarStr_0", "port": "output"}, "to": {"device": "setVarStr_1", "port": "value"}, "dataType": "string" }
  ],
  "variables": [ { "name": "msg", "type": "string" }, { "name": "copy", "type": "string" } ]
}`
	goResp := Generate(context.Background(), Request{Scene: json.RawMessage(sceneVariablesString), Language: "go"})
	code := goResp.Files["main.go"]
	for _, want := range []string{"var msg string", "var copy string", `"hi"`, "copy = msg"} {
		if !strings.Contains(code, want) {
			t.Fatalf("Go string lifecycle missing %q\n%s", want, code)
		}
	}
	cResp := Generate(context.Background(), Request{Scene: json.RawMessage(sceneVariablesString), Language: "c"})
	cCode := cResp.Files["main.c"]
	t.Logf("\n=== string Go ===\n%s\n=== string C ===\n%s", code, cCode)
}
