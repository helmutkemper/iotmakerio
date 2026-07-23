package codegen

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// Case-output MERGE (the φ, 2026-07-22): an OUTSIDE consumer fed by
// tails from BOTH branches must read one register every branch
// assigns — the field caught printf reading an uninitialized
// single-branch var (garbage output + two compiler warnings).
// Português: Consumidor EXTERNO alimentado pelas caudas dos DOIS
// ramos lê um registrador que todo ramo atribui.
const sceneCaseMerge = `{
  "metadata": { "schemaVersion": "1.1", "camera": {"x":0,"y":0,"zoom":1}, "canvas":{"w":1200,"h":800} },
  "devices": [
    { "id": "sel", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 1 },
      "position": { "x": 40, "y": 300 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 160, "y": 337 },
          "connections": [{ "wireId": "w_sel", "targetDevice": "stmCase_1", "targetPort": "selector" }] } ],
      "containment": { "isContainer": false, "status": "free" } },
    { "id": "stmCase_1", "type": "StatementCase", "kind": "complex",
      "properties": {
        "selectorType": "int",
        "selectedCase": "case_0",
        "cases": [
          { "id": "case_0", "label": "zero", "values": ["0"], "ids": ["c01","c02","add0"] },
          { "id": "case_1", "label": "one",  "values": ["1"], "ids": ["c11","c12","add1"] }
        ]
      },
      "position": { "x": 300, "y": 80 }, "size": { "width": 620, "height": 620 },
      "innerBBox": { "x": 310, "y": 120, "width": 600, "height": 560 },
      "connectors": [
        { "port": "selector", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 305, "y": 360 },
          "connections": [{ "wireId": "w_sel", "targetDevice": "sel", "targetPort": "output" }] } ],
      "containment": { "isContainer": true, "status": "container",
        "children": ["c01","c02","add0","c11","c12","add1"] } },
    { "id": "c01", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 10 },
      "position": { "x": 330, "y": 140 }, "size": { "width": 110, "height": 70 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
        "position": { "x": 440, "y": 175 },
        "connections": [{ "wireId": "w01", "targetDevice": "add0", "targetPort": "inputX" }] } ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "c02", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 20 },
      "position": { "x": 330, "y": 220 }, "size": { "width": 110, "height": 70 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
        "position": { "x": 440, "y": 255 },
        "connections": [{ "wireId": "w02", "targetDevice": "add0", "targetPort": "inputY" }] } ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "add0", "type": "StatementAdd", "kind": "simple",
      "position": { "x": 500, "y": 180 }, "size": { "width": 80, "height": 80 },
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 200 },
          "connections": [{ "wireId": "w01", "targetDevice": "c01", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 240 },
          "connections": [{ "wireId": "w02", "targetDevice": "c02", "targetPort": "output" }] },
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 580, "y": 220 },
          "connections": [{ "wireId": "w_o0", "targetDevice": "pr", "targetPort": "value" }] } ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "c11", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 30 },
      "position": { "x": 330, "y": 380 }, "size": { "width": 110, "height": 70 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
        "position": { "x": 440, "y": 415 },
        "connections": [{ "wireId": "w11", "targetDevice": "add1", "targetPort": "inputX" }] } ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "c12", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 40 },
      "position": { "x": 330, "y": 460 }, "size": { "width": 110, "height": 70 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
        "position": { "x": 440, "y": 495 },
        "connections": [{ "wireId": "w12", "targetDevice": "add1", "targetPort": "inputY" }] } ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "add1", "type": "StatementAdd", "kind": "simple",
      "position": { "x": 500, "y": 420 }, "size": { "width": 80, "height": 80 },
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 440 },
          "connections": [{ "wireId": "w11", "targetDevice": "c11", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 480 },
          "connections": [{ "wireId": "w12", "targetDevice": "c12", "targetPort": "output" }] },
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 580, "y": 460 },
          "connections": [{ "wireId": "w_o1", "targetDevice": "pr", "targetPort": "value" }] } ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "pr", "type": "StatementPrintInt", "kind": "simple", "properties": {},
      "position": { "x": 1000, "y": 300 }, "size": { "width": 120, "height": 74 },
      "connectors": [ { "port": "value", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
        "position": { "x": 1000, "y": 337 },
        "connections": [
          { "wireId": "w_o0", "targetDevice": "add0", "targetPort": "output" },
          { "wireId": "w_o1", "targetDevice": "add1", "targetPort": "output" } ] } ],
      "containment": { "isContainer": false, "status": "free" } }
  ],
  "wires": [
    { "id": "w_sel", "from": { "device": "sel",  "port": "output" }, "to": { "device": "stmCase_1", "port": "selector" }, "dataType": "int" },
    { "id": "w01",  "from": { "device": "c01",  "port": "output" }, "to": { "device": "add0", "port": "inputX" }, "dataType": "int" },
    { "id": "w02",  "from": { "device": "c02",  "port": "output" }, "to": { "device": "add0", "port": "inputY" }, "dataType": "int" },
    { "id": "w11",  "from": { "device": "c11",  "port": "output" }, "to": { "device": "add1", "port": "inputX" }, "dataType": "int" },
    { "id": "w12",  "from": { "device": "c12",  "port": "output" }, "to": { "device": "add1", "port": "inputY" }, "dataType": "int" },
    { "id": "w_o0", "from": { "device": "add0", "port": "output" }, "to": { "device": "pr", "port": "value" }, "dataType": "int" },
    { "id": "w_o1", "from": { "device": "add1", "port": "output" }, "to": { "device": "pr", "port": "value" }, "dataType": "int" }
  ]
}`

func TestCaseOutputMerge(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene: json.RawMessage(sceneCaseMerge), Language: "c",
	})
	code := generatedCode(resp)

	// Extract the printf's argument — the merge register, name-agnostic.
	m := regexp.MustCompile(`printf\("%ld\\n", \(long\)(\w+)\);`).FindStringSubmatch(code)
	if m == nil {
		t.Fatalf("printf not found; got:\n%s", code)
	}
	merged := m[1]

	// The consumer must NOT read a raw branch tail.
	if merged == "add0" || merged == "add1" {
		t.Fatalf("printf bound to a single branch tail %q — the φ is missing; got:\n%s", merged, code)
	}
	// Declared exactly once, assigned in BOTH branches (two writes).
	decl := regexp.MustCompile(`int\d+_t ` + merged + `;`)
	if len(decl.FindAllString(code, -1)) != 1 {
		t.Fatalf("merge %q must be declared once before the switch; got:\n%s", merged, code)
	}
	if strings.Contains(code, "%"+merged) {
		t.Fatalf("the %% leaked into an emitted name (field 2026-07-23); got:\n%s", code)
	}
	writes := regexp.MustCompile(`(?m)^\s+` + merged + ` = `)
	if n := len(writes.FindAllString(code, -1)); n != 2 {
		t.Fatalf("merge %q must be assigned in both branches (got %d writes); got:\n%s", merged, n, code)
	}
}
