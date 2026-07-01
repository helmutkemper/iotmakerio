// /server/codegen/codeGen_c99_chain_test.go
package codegen

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// sceneC99AddChain chains two add() function-devices: add_1(a,b) feeds its
// "return" into add_2's "a". This exercises the connected-return path — the
// first call must capture its result in a variable, and the second call must
// read that same variable.
//
//	constInt_a ─┐
//	            ├─ add_1 ─return─┐
//	constInt_b ─┘               ├─ add_2  (return unwired)
//	            constInt_c ──────┘
const sceneC99AddChain = `{
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
    { "id": "constInt_c", "type": "StatementConstInt", "properties": { "value": 4 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [ { "wireId": "wc", "targetDevice": "add_2", "targetPort": "b" } ] } ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },
    { "id": "add_1", "type": "BlackBoxadd:", "properties": {},
      "connectors": [
        { "port": "a", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wa", "targetDevice": "constInt_a", "targetPort": "output" } ] },
        { "port": "b", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wb", "targetDevice": "constInt_b", "targetPort": "output" } ] },
        { "port": "return", "dataType": "int", "isOutput": true,
          "connections": [ { "wireId": "wr", "targetDevice": "add_2", "targetPort": "a" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },
    { "id": "add_2", "type": "BlackBoxadd:", "properties": {},
      "connectors": [
        { "port": "a", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wr", "targetDevice": "add_1", "targetPort": "return" } ] },
        { "port": "b", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wc", "targetDevice": "constInt_c", "targetPort": "output" } ] },
        { "port": "return", "dataType": "int", "isOutput": true, "connections": [] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } }
  ],
  "wires": [
    { "id": "wa", "from": { "device": "constInt_a", "port": "output" }, "to": { "device": "add_1", "port": "a" }, "dataType": "int" },
    { "id": "wb", "from": { "device": "constInt_b", "port": "output" }, "to": { "device": "add_1", "port": "b" }, "dataType": "int" },
    { "id": "wr", "from": { "device": "add_1", "port": "return" }, "to": { "device": "add_2", "port": "a" }, "dataType": "int" },
    { "id": "wc", "from": { "device": "constInt_c", "port": "output" }, "to": { "device": "add_2", "port": "b" }, "dataType": "int" }
  ]
}`

// TestEmitC_C99FunctionDevice_ConnectedReturn verifies that a function-device
// whose "return" is wired captures the result in a variable, and a downstream
// consumer reads that same variable. This is the shape a sensor read() needs.
func TestEmitC_C99FunctionDevice_ConnectedReturn(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99AddChain),
		Language:     "c",
		BlackBoxDefs: c99AddDefsWithSource(),
	})

	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; diagnostics=%+v", resp.Diagnostics)
	}

	idx := strings.Index(mainC, "int main(void)")
	if idx < 0 {
		t.Fatalf("main.c has no main(); got:\n%s", mainC)
	}
	body := mainC[idx:]

	// add_1's return is wired → it must be declared into a variable:
	//   <type> <var> = add(...);
	capture := regexp.MustCompile(`(?m)^\s*\w+\s+(\w+)\s*=\s*add\(`).FindStringSubmatch(body)
	if capture == nil {
		t.Fatalf("expected a captured return ('<type> <var> = add(...)') in main(), got:\n%s", mainC)
	}
	retVar := capture[1]

	// add_2 reads add_1's captured variable as an argument. With the type-seam
	// fix, scalar args are cast to the parameter's authored type, so the read
	// appears as "(int)<var>" — not a bare "<var>". Assert the variable is both
	// declared and consumed (≥2 occurrences) and that the scalar arg is cast.
	if strings.Count(body, retVar) < 2 {
		t.Fatalf("expected %q to be declared and then consumed downstream, got:\n%s", retVar, mainC)
	}
	if !strings.Contains(body, "(int)"+retVar) {
		t.Fatalf("expected the downstream scalar arg cast to its authored type, e.g. (int)%s, got:\n%s", retVar, mainC)
	}
}
