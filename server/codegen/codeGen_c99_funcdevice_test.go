// /server/codegen/codeGen_c99_funcdevice_test.go
package codegen

import (
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/graph"
	"server/codegen/ir"
)

// sceneC99FuncDevices is a minimal C99 function-device chain: sht3x_create
// produces a handle on its "return" port, wired into sht3x_read's "dev"
// input. Both devices use the empty-struct discriminator "BlackBox<fn>:"
// (the colon is the last character). This is the shape Slice C99-8 produces.
const sceneC99FuncDevices = `{
  "version": "1.0",
  "devices": [
    {
      "id": "create_1",
      "type": "BlackBoxsht3x_create:",
      "properties": {},
      "connectors": [
        {
          "port": "return",
          "dataType": "sht3x_t*",
          "isOutput": true,
          "connections": [
            { "wireId": "w1", "targetDevice": "read_1", "targetPort": "dev" }
          ]
        }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" }
    },
    {
      "id": "read_1",
      "type": "BlackBoxsht3x_read:",
      "properties": {},
      "connectors": [
        {
          "port": "dev",
          "dataType": "sht3x_t*",
          "isOutput": false,
          "connections": [
            { "wireId": "w1", "targetDevice": "create_1", "targetPort": "return" }
          ]
        }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" }
    }
  ],
  "wires": [
    {
      "id": "w1",
      "from": { "device": "create_1", "port": "return" },
      "to":   { "device": "read_1",   "port": "dev" },
      "dataType": "sht3x_t*"
    }
  ]
}`

// TestIR_C99FunctionDevices_EmitBBCall is the foundation test for Fatia 4.1:
// a C99 function-device scene must lower to BB_CALL instructions and must NOT
// emit a single BB_DECL — function-devices are free function calls, not struct
// instances. Ordering must follow the wire (create before read). The test runs
// graph.Build → ir.Emit directly (no Generate) so it exercises only the IR,
// independent of the def loader and validation (Fatia 4.2).
func TestIR_C99FunctionDevices_EmitBBCall(t *testing.T) {
	var scene graph.SceneInput
	if err := json.Unmarshal([]byte(sceneC99FuncDevices), &scene); err != nil {
		t.Fatalf("unmarshal scene: %v", err)
	}

	g, buildDiags := graph.Build(scene)
	for _, d := range buildDiags {
		if d.Severity == "error" {
			t.Fatalf("graph.Build error diagnostic: %+v", d)
		}
	}

	program, emitDiags := ir.Emit(g, nil, nil)
	for _, d := range emitDiags {
		if d.Severity == "error" {
			t.Fatalf("ir.Emit error diagnostic: %+v", d)
		}
	}

	got := program.String()

	// Core contract: a free function call, never an instance declaration.
	if !strings.Contains(got, "BB_CALL") {
		t.Fatalf("expected BB_CALL in IR, got:\n%s", got)
	}
	if strings.Contains(got, "BB_DECL") {
		t.Fatalf("function-devices must not emit BB_DECL (no instance var), got:\n%s", got)
	}
	if strings.Contains(got, "BB_METHOD") || strings.Contains(got, "BB_INIT") {
		t.Fatalf("function-devices must not emit method/init opcodes, got:\n%s", got)
	}

	// Both functions are present, identified by name in Meta.
	if !strings.Contains(got, "fn=sht3x_create") {
		t.Fatalf("expected fn=sht3x_create in IR, got:\n%s", got)
	}
	if !strings.Contains(got, "fn=sht3x_read") {
		t.Fatalf("expected fn=sht3x_read in IR, got:\n%s", got)
	}

	// Ordering follows the wire: create before read.
	if iCreate, iRead := strings.Index(got, "sht3x_create"), strings.Index(got, "sht3x_read"); iCreate > iRead {
		t.Fatalf("create must be ordered before read (wire dependency), got:\n%s", got)
	}
}
