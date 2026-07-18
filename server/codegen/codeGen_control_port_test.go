// server/codegen/codeGen_control_port_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// codeGen_control_port_test.go — Regression test for the pattern
// demonstrated in the 2026-04-18 session: a StatementGreaterThan lives
// at the global scope but its output is wired to a Loop.stop port.
// Before the fix, the emitter silently produced Go that used
// stmGreater1 before it was declared, and emitted an assignment in the
// loop body that had no effect. Now the emitter refuses with a
// structured diagnostic that names the loop and the outside producer
// so the user can fix the scene.
//
// Português: Teste de regressão — produtor fora do loop alimentando a
// porta stop antes gerava Go quebrado; agora é rejeitado com mensagem
// clara apontando o loop e o produtor externo.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// sceneControlPortOutside describes the minimum scene to reproduce
// the bug: a Loop, a StatementBool driving the loop's stop port, and
// a StatementGreaterThan that tries to live at the global scope while
// also feeding the stop port. The comparison has only a constant input
// and no live producer, so the control value is frozen — exactly the
// anti-pattern we want to catch.
const sceneControlPortOutside = `{
  "metadata": { "schemaVersion": "1.1", "camera": {"x":0,"y":0,"zoom":1}, "canvas":{"w":1024,"h":768} },
  "devices": [
    {
      "id": "stmLoop_1", "type": "StatementLoop", "kind": "complex",
      "position": { "x": 100, "y": 100 },
      "size": { "width": 400, "height": 300 },
      "outerBBox": { "x": 100, "y": 100, "width": 400, "height": 300 },
      "innerBBox": { "x": 110, "y": 130, "width": 380, "height": 260 },
      "connectors": [
        {
          "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 490, "y": 390 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "gt_1", "targetPort": "output" }]
        }
      ],
      "containment": { "isContainer": true, "status": "container", "children": [] }
    },
    {
      "id": "constInt_0", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 100 },
      "position": { "x": 600, "y": 200 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 600, "y": 200, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 720, "y": 237 },
          "connections": [{ "wireId": "w_x", "targetDevice": "gt_1", "targetPort": "inputX" }]
        }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "constInt_1", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 10 },
      "position": { "x": 600, "y": 300 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 600, "y": 300, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 720, "y": 337 },
          "connections": [{ "wireId": "w_y", "targetDevice": "gt_1", "targetPort": "inputY" }]
        }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "gt_1", "type": "StatementGreaterThan", "kind": "simple",
      "position": { "x": 750, "y": 250 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 750, "y": 250, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 750, "y": 270 },
          "connections": [{ "wireId": "w_x", "targetDevice": "constInt_0", "targetPort": "output" }]
        },
        {
          "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 750, "y": 290 },
          "connections": [{ "wireId": "w_y", "targetDevice": "constInt_1", "targetPort": "output" }]
        },
        {
          "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 870, "y": 280 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "stmLoop_1", "targetPort": "stop" }]
        }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w_x", "from": { "device": "constInt_0", "port": "output" }, "to": { "device": "gt_1", "port": "inputX" }, "dataType": "int" },
    { "id": "w_y", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "gt_1", "port": "inputY" }, "dataType": "int" },
    { "id": "w_stop", "from": { "device": "gt_1", "port": "output" }, "to": { "device": "stmLoop_1", "port": "stop" }, "dataType": "bool" }
  ]
}`

// TestControlPortProducerOutsideLoop asserts that the emitter refuses
// a scene where the producer of a loop's stop port sits outside the
// loop, and that the diagnostic names both the loop and the producer
// so the user can find them on the canvas.
func TestControlPortProducerOutsideLoop(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneControlPortOutside),
		Language: "go",
	})

	if len(resp.Errors) == 0 {
		t.Fatalf("expected an error about the outside producer, got none\n  Code:\n%s", resp.Files["main.go"])
	}
	if resp.Files["main.go"] != "" {
		t.Errorf("expected empty Code when control port source is invalid, got:\n%s", resp.Files["main.go"])
	}

	joined := strings.Join(resp.Errors, "\n")
	for _, mustMention := range []string{"stmLoop_1", "gt_1", "stop"} {
		if !strings.Contains(joined, mustMention) {
			t.Errorf("expected error to mention %q, got:\n%s", mustMention, joined)
		}
	}

	// The structured Diagnostics must also carry the device IDs so the
	// UI can highlight them both. Checking the first diagnostic is
	// enough because the current scene produces exactly one.
	if len(resp.Diagnostics) == 0 {
		t.Fatalf("expected at least one structured Diagnostic")
	}
	d := resp.Diagnostics[0]
	if d.Severity != "error" {
		t.Errorf("expected severity=error, got %q", d.Severity)
	}
	devs := strings.Join(d.Devices, ",")
	for _, want := range []string{"stmLoop_1", "gt_1"} {
		if !strings.Contains(devs, want) {
			t.Errorf("expected Devices to include %q, got %v", want, d.Devices)
		}
	}

	t.Logf("Error (as expected): %s", resp.Errors[0])
}
