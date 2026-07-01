// server/codegen/codeGen_conflict_test.go
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

// sceneConflict places a StatementBool straddling the outer Loop's border
// — the Bool's outerBBox intersects the Loop's outerBBox but is not fully
// contained within the Loop's innerBBox, so the IDE's scenegraph classifies
// it as "pierced_outer" and exports Status="error".
//
// The fixture mirrors what the WASM IDE writes on disk when the user
// drops a device half-on / half-off a container's edge. The test asserts
// that codegen refuses to emit code and surfaces a proper Errors entry
// instead of silently generating a broken program.
//
// Português: Teste de regressão — se o JSON traz Status="error" num
// device (conflito espacial), o codegen precisa BLOQUEAR a geração,
// não apenas avisar. Violação geométrica significa fluxo ambíguo.
const sceneConflict = `{
  "version": "1.0",
  "metadata": {
    "density": 1, "canvasWidth": 1200, "canvasHeight": 800,
    "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 }
  },
  "devices": [
    {
      "id": "stmLoop_1", "type": "StatementLoop", "kind": "complex",
      "position": { "x": 100, "y": 100 },
      "size": { "width": 400, "height": 300 },
      "outerBBox": { "x": 100, "y": 100, "width": 400, "height": 300 },
      "innerBBox": { "x": 120, "y": 120, "width": 360, "height": 260 },
      "connectors": [
        {
          "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
          "position": { "x": 440, "y": 380 },
          "connections": [{ "wireId": "wire_1", "targetDevice": "bool_1", "targetPort": "output" }]
        }
      ],
      "containment": { "isContainer": true, "children": [], "status": "container" }
    },
    {
      "id": "bool_1", "type": "StatementBool", "kind": "simple",
      "properties": { "value": false },
      "position": { "x": 460, "y": 330 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 460, "y": 330, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 572, "y": 358 },
          "connections": [{ "wireId": "wire_1", "targetDevice": "stmLoop_1", "targetPort": "stop" }]
        }
      ],
      "containment": {
        "isContainer": false,
        "status": "error",
      "conflicts": [
        { "with": "stmLoop_1", "kind": "pierced_outer" }
      ]
    }
    }
  ],
  "wires": [
    { "id": "wire_1", "from": { "device": "bool_1", "port": "output" }, "to": { "device": "stmLoop_1", "port": "stop" }, "dataType": "bool" }
  ]
}`

// TestConflictBlocksCodegen asserts that when the scene JSON contains any
// device with Containment.Status="error", Generate refuses to emit code
// and surfaces a descriptive error naming the offending device, the kind
// of conflict, and the peer.
func TestConflictBlocksCodegen(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneConflict),
		Language: "go",
	})

	// Must produce Errors, not just Warnings.
	if len(resp.Errors) == 0 {
		t.Fatalf("expected at least one error, got none\n"+
			"  Warnings: %v\n  IR: %q\n  Code: %q",
			resp.Warnings, resp.IR, resp.Code)
	}

	// Must NOT emit any Go code — a broken canvas should never produce
	// a .go file, even a syntactically-valid one.
	if resp.Code != "" {
		t.Errorf("expected empty Code when conflict present, got:\n%s", resp.Code)
	}

	// The error message must identify the offending device and the kind
	// of conflict. Check for both pieces so a future refactor doesn't
	// silently drop either.
	joined := strings.Join(resp.Errors, "\n")
	if !strings.Contains(joined, "bool_1") {
		t.Errorf("expected error to mention offending device bool_1, got:\n%s", joined)
	}
	if !strings.Contains(joined, "pierced_outer") {
		t.Errorf("expected error to mention conflict kind pierced_outer, got:\n%s", joined)
	}
	if !strings.Contains(joined, "stmLoop_1") {
		t.Errorf("expected error to mention conflicting peer stmLoop_1, got:\n%s", joined)
	}

	t.Logf("Errors (as expected):\n  %s", strings.Join(resp.Errors, "\n  "))
}
