// server/codegen/codeGen_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// sceneLinear: two ConstInt → Add → Gauge (no loop)
//
//	constInt_1 (10) ─→ add_1.inputX ─→ gauge_1.current
//	constInt_3 (20) ─→ add_1.inputY
const sceneLinear = `{
  "version": "1.0",
  "metadata": { "density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
  "devices": [
    {
      "id": "constInt_1", "type": "StatementConstInt", "properties": { "value": 10 },
      "position": { "x": 100, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 100, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true, "position": { "x": 192, "y": 125 },
          "connections": [{ "wireId": "w1", "targetDevice": "add_1", "targetPort": "inputX" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "constInt_3", "type": "StatementConstInt", "properties": { "value": 20 },
      "position": { "x": 100, "y": 200 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 200, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true, "position": { "x": 192, "y": 225 },
          "connections": [{ "wireId": "w2", "targetDevice": "add_1", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "add_1", "type": "StatementAdd",
      "position": { "x": 300, "y": 150 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 300, "y": 150, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false, "position": { "x": 308, "y": 162 },
          "connections": [{ "wireId": "w1", "targetDevice": "constInt_1", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false, "position": { "x": 308, "y": 187 },
          "connections": [{ "wireId": "w2", "targetDevice": "constInt_3", "targetPort": "output" }] },
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true, "position": { "x": 392, "y": 175 },
          "connections": [{ "wireId": "w3", "targetDevice": "gauge_1", "targetPort": "current" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "gauge_1", "type": "StatementGauge", "label": "total",
      "position": { "x": 500, "y": 150 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 500, "y": 150, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "current", "dataType": "int", "isOutput": false, "acceptNotConnected": true, "position": { "x": 508, "y": 175 },
          "connections": [{ "wireId": "w3", "targetDevice": "add_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "add_1", "port": "inputX" }, "dataType": "int" },
    { "id": "w2", "from": { "device": "constInt_3", "port": "output" }, "to": { "device": "add_1", "port": "inputY" }, "dataType": "int" },
    { "id": "w3", "from": { "device": "add_1", "port": "output" }, "to": { "device": "gauge_1", "port": "current" }, "dataType": "int" }
  ]
}`

// sceneLoop: Loop containing ConstInt(10) + ConstInt(20) → Add,
// Compare(add > 100) → stop port. Gauge outside reads add result.
//
//	stmLoop_1 (container)
//	├── constInt_1 (10) ─→ add_1.inputX
//	├── constInt_3 (20) ─→ add_1.inputY
//	├── add_1 ─→ compare_1.inputX
//	├── constInt_5 (100) ─→ compare_1.inputY
//	└── compare_1 (GT) ─→ stmLoop_1.stop
//	add_1 ─────────────→ gauge_1.current  (wire crosses scope)
const sceneLoop = `{
  "version": "1.0",
  "metadata": { "density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
  "devices": [
    {
      "id": "stmLoop_1", "type": "StatementLoop",
      "position": { "x": 50, "y": 50 }, "size": { "width": 500, "height": 400 },
      "outerBBox": { "x": 50, "y": 50, "width": 500, "height": 400 },
      "innerBBox": { "x": 70, "y": 90, "width": 460, "height": 340 },
      "overlapPolicy": { "allowAbove": true, "allowBelow": false, "allowPartial": false },
      "connectors": [
        { "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true, "position": { "x": 493, "y": 408 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "compare_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": true, "children": ["constInt_1", "constInt_3", "add_1", "constInt_5", "compare_1"], "status": "container" }
    },
    {
      "id": "constInt_1", "type": "StatementConstInt", "properties": { "value": 10 },
      "position": { "x": 100, "y": 120 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 120, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true, "position": { "x": 192, "y": 145 },
          "connections": [{ "wireId": "w1", "targetDevice": "add_1", "targetPort": "inputX" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "constInt_3", "type": "StatementConstInt", "properties": { "value": 20 },
      "position": { "x": 100, "y": 200 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 200, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true, "position": { "x": 192, "y": 225 },
          "connections": [{ "wireId": "w2", "targetDevice": "add_1", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "add_1", "type": "StatementAdd",
      "position": { "x": 280, "y": 160 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 280, "y": 160, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false, "position": { "x": 288, "y": 172 },
          "connections": [{ "wireId": "w1", "targetDevice": "constInt_1", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false, "position": { "x": 288, "y": 197 },
          "connections": [{ "wireId": "w2", "targetDevice": "constInt_3", "targetPort": "output" }] },
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true, "position": { "x": 372, "y": 185 },
          "connections": [
            { "wireId": "w3", "targetDevice": "compare_1", "targetPort": "inputX" },
            { "wireId": "w_out", "targetDevice": "gauge_1", "targetPort": "current" }
          ] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "constInt_5", "type": "StatementConstInt", "properties": { "value": 100 },
      "position": { "x": 280, "y": 260 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 280, "y": 260, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true, "position": { "x": 372, "y": 285 },
          "connections": [{ "wireId": "w4", "targetDevice": "compare_1", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "compare_1", "type": "StatementGreaterThan",
      "position": { "x": 420, "y": 200 }, "size": { "width": 100, "height": 60 },
      "outerBBox": { "x": 420, "y": 200, "width": 100, "height": 60 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false, "position": { "x": 428, "y": 215 },
          "connections": [{ "wireId": "w3", "targetDevice": "add_1", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false, "position": { "x": 428, "y": 245 },
          "connections": [{ "wireId": "w4", "targetDevice": "constInt_5", "targetPort": "output" }] },
        { "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true, "position": { "x": 512, "y": 230 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "stmLoop_1", "targetPort": "stop" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "gauge_1", "type": "StatementGauge", "label": "total",
      "position": { "x": 600, "y": 200 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 600, "y": 200, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "current", "dataType": "int", "isOutput": false, "acceptNotConnected": true, "position": { "x": 608, "y": 225 },
          "connections": [{ "wireId": "w_out", "targetDevice": "add_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "add_1", "port": "inputX" }, "dataType": "int" },
    { "id": "w2", "from": { "device": "constInt_3", "port": "output" }, "to": { "device": "add_1", "port": "inputY" }, "dataType": "int" },
    { "id": "w3", "from": { "device": "add_1", "port": "output" }, "to": { "device": "compare_1", "port": "inputX" }, "dataType": "int" },
    { "id": "w4", "from": { "device": "constInt_5", "port": "output" }, "to": { "device": "compare_1", "port": "inputY" }, "dataType": "int" },
    { "id": "w_stop", "from": { "device": "compare_1", "port": "output" }, "to": { "device": "stmLoop_1", "port": "stop" }, "dataType": "bool" },
    { "id": "w_out", "from": { "device": "add_1", "port": "output" }, "to": { "device": "gauge_1", "port": "current" }, "dataType": "int" }
  ]
}`

// =====================================================================
//  Test: linear scene (no loop)
// =====================================================================

func TestLinear(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLinear),
		Language: "go",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", resp.Errors)
	}

	t.Log("=== IR ===")
	t.Log(resp.IR)
	t.Log("=== Go ===")
	t.Log(resp.Files["main.go"])

	// IR checks
	assertContains(t, resp.IR, "CONST %constInt_1 int 10")
	assertContains(t, resp.IR, "CONST %constInt_3 int 20")
	assertContains(t, resp.IR, "ADD %add_1 int %constInt_1 %constInt_3")
	assertContains(t, resp.IR, "OUTPUT %gauge_1")

	// Go code checks
	assertContains(t, resp.Files["main.go"], "package main")
	assertContains(t, resp.Files["main.go"], "constInt1 := int64(10)")
	assertContains(t, resp.Files["main.go"], "constInt3 := int64(20)")
	assertContains(t, resp.Files["main.go"], "add1 := constInt1 + constInt3")
	assertContains(t, resp.Files["main.go"], `fmt.Println("total"`)
}

// =====================================================================
//  Test: loop with scope crossing
// =====================================================================

func TestLoop(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLoop),
		Language: "go",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", resp.Errors)
	}

	t.Log("=== IR ===")
	t.Log(resp.IR)
	t.Log("=== Go ===")
	t.Log(resp.Files["main.go"])

	// IR: add_1 crosses scope → must be promoted to VAR
	// compare_1 does NOT cross scope (only connects to stop port)
	assertContains(t, resp.IR, "VAR %add_1 int 0")
	assertNotContains(t, resp.IR, "VAR %compare_1") // stop port doesn't cross scope
	assertContains(t, resp.IR, "LOOP_BEGIN %stmLoop_1")
	assertContains(t, resp.IR, "ADD %add_1 int %constInt_1 %constInt_3")
	assertContains(t, resp.IR, "CMP_GT %compare_1 bool %add_1 %constInt_5")
	assertContains(t, resp.IR, "BREAK_IF %compare_1")
	assertContains(t, resp.IR, "LOOP_END %stmLoop_1")
	assertContains(t, resp.IR, "OUTPUT %gauge_1")

	// IR: OUTPUT must come AFTER LOOP_END (gauge reads add_1 which is inside loop)
	loopEndPos := strings.Index(resp.IR, "LOOP_END")
	outputPos := strings.Index(resp.IR, "OUTPUT")
	if outputPos < loopEndPos {
		t.Errorf("OUTPUT should come after LOOP_END\n  IR: %s", resp.IR)
	}

	// Go code: var before loop, for{}, break, output after loop
	assertContains(t, resp.Files["main.go"], "var add1 int64")
	assertNotContains(t, resp.Files["main.go"], "var compare1") // not promoted
	assertContains(t, resp.Files["main.go"], "for {")
	assertContains(t, resp.Files["main.go"], "add1 = constInt1 + constInt3")
	assertContains(t, resp.Files["main.go"], "compare1 := add1 > constInt5") // := not =
	assertContains(t, resp.Files["main.go"], "if compare1 {")
	assertContains(t, resp.Files["main.go"], "break")
	assertContains(t, resp.Files["main.go"], `fmt.Println("total"`)
}

// =====================================================================
//  Test: validation errors
// =====================================================================

func TestValidationErrors(t *testing.T) {
	// Loop without stop condition
	scene := `{
		"version": "1.0",
		"metadata": { "density": 1, "canvasWidth": 800, "canvasHeight": 600, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
		"devices": [
			{
				"id": "stmLoop_1", "type": "StatementLoop",
				"position": { "x": 50, "y": 50 }, "size": { "width": 300, "height": 200 },
				"outerBBox": { "x": 50, "y": 50, "width": 300, "height": 200 },
				"innerBBox": { "x": 70, "y": 90, "width": 260, "height": 140 },
				"overlapPolicy": { "allowAbove": true, "allowBelow": false, "allowPartial": false },
				"connectors": [
					{ "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
					  "position": { "x": 293, "y": 208 }, "connections": [] }
				],
				"containment": { "isContainer": true, "children": [], "status": "container" }
			}
		],
		"wires": []
	}`

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
	})

	if len(resp.Errors) == 0 {
		t.Fatal("expected validation error for loop without stop condition")
	}

	found := false
	for _, err := range resp.Errors {
		if strings.Contains(err, "no stop condition") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'no stop condition' error, got: %v", resp.Errors)
	}
	t.Logf("Validation errors (expected): %v", resp.Errors)
}

// =====================================================================
//  Test: invalid JSON
// =====================================================================

func TestInvalidJSON(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(`{invalid`),
		Language: "go",
	})

	if len(resp.Errors) == 0 {
		t.Fatal("expected error for invalid JSON")
	}
	assertContains(t, resp.Errors[0], "invalid scene JSON")
}

// =====================================================================
//  Test: unsupported language
// =====================================================================

func TestUnsupportedLanguage(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLinear),
		Language: "rust",
	})

	if len(resp.Errors) == 0 {
		t.Fatal("expected error for unsupported language")
	}
	assertContains(t, resp.Errors[0], "unsupported language")
}

// =====================================================================
//  Helper
// =====================================================================

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected to contain %q\n  got: %s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected NOT to contain %q\n  got: %s", needle, haystack)
	}
}
