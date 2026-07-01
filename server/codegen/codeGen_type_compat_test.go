// server/codegen/codeGen_type_compat_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// codeGen_type_compat_test.go — Regression for the APDS9960 uint16 vs
// int comparison. Before this work, the emitter emitted
// `stmGreater1 := apds99600_clear > constInt0` where clear is uint16
// and constInt0 is int64 (Go refuses to compile). The new type-compat
// pass inserts an explicit CONVERT ahead of the comparison, keeping
// the concrete side (uint16) as the result and warning the maker.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/diagnostics"
)

// sceneUint16VsInt mimics the minimal shape of the failing APDS9960
// scene: one BlackBox Init, one BlackBox Run producing uint16 outputs,
// one StatementConstInt at `int`, one StatementGreaterThan comparing
// them. The loop wrapper matches the real scene. We only model the
// pieces the pipeline cares about for this regression.
const sceneUint16VsInt = `{
  "metadata": { "schemaVersion": "1.1", "camera": {"x":0,"y":0,"zoom":1}, "canvas":{"w":1024,"h":768} },
  "devices": [
    {
      "id": "i2c_1", "type": "BlackBoxInit:I2CBus", "kind": "simple",
      "properties": { "instanceId": "i2c_0", "executionOrder": 1 },
      "position": { "x": 10, "y": 50 },
      "size": { "width": 160, "height": 100 },
      "outerBBox": { "x": 10, "y": 50, "width": 160, "height": 100 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "bus", "dataType": "*machine.I2C", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 170, "y": 100 },
          "connections": [{ "wireId": "w_bus", "targetDevice": "apdsInit_1", "targetPort": "i2c" }]
        }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "apdsInit_1", "type": "BlackBoxInit:APDS9960", "kind": "simple",
      "properties": { "instanceId": "apds_0", "executionOrder": 1 },
      "position": { "x": 200, "y": 50 },
      "size": { "width": 160, "height": 100 },
      "outerBBox": { "x": 200, "y": 50, "width": 160, "height": 100 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "i2c", "dataType": "*machine.I2C", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 200, "y": 100 },
          "connections": [{ "wireId": "w_bus", "targetDevice": "i2c_1", "targetPort": "bus" }]
        }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "stmLoop_1", "type": "StatementLoop", "kind": "complex",
      "position": { "x": 50, "y": 200 },
      "size": { "width": 600, "height": 400 },
      "outerBBox": { "x": 50, "y": 200, "width": 600, "height": 400 },
      "innerBBox": { "x": 60, "y": 230, "width": 580, "height": 360 },
      "connectors": [
        {
          "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 640, "y": 590 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "gt_1", "targetPort": "output" }]
        }
      ],
      "containment": { "isContainer": true, "status": "container", "children": ["apdsRun_1", "constInt_0", "gt_1"] }
    },
    {
      "id": "apdsRun_1", "type": "BlackBoxRun:APDS9960", "kind": "simple",
      "properties": { "instanceId": "apds_0", "executionOrder": 10 },
      "position": { "x": 80, "y": 260 },
      "size": { "width": 160, "height": 157 },
      "outerBBox": { "x": 80, "y": 260, "width": 160, "height": 157 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "clear", "dataType": "uint16", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 240, "y": 315 },
          "connections": [{ "wireId": "w_clear", "targetDevice": "gt_1", "targetPort": "inputX" }]
        }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "constInt_0", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 255 },
      "position": { "x": 80, "y": 440 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 80, "y": 440, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 200, "y": 477 },
          "connections": [{ "wireId": "w_y", "targetDevice": "gt_1", "targetPort": "inputY" }]
        }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "gt_1", "type": "StatementGreaterThan", "kind": "simple",
      "position": { "x": 300, "y": 400 },
      "size": { "width": 60, "height": 78 },
      "outerBBox": { "x": 300, "y": 400, "width": 60, "height": 78 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 302, "y": 415 },
          "connections": [{ "wireId": "w_clear", "targetDevice": "apdsRun_1", "targetPort": "clear" }]
        },
        {
          "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 302, "y": 442 },
          "connections": [{ "wireId": "w_y", "targetDevice": "constInt_0", "targetPort": "output" }]
        },
        {
          "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 360, "y": 428 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "stmLoop_1", "targetPort": "stop" }]
        }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    }
  ],
  "wires": []
}`

func TestTypeCompat_Uint16VsInt_EmitsWarningAndConvert(t *testing.T) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(sceneUint16VsInt), &raw); err != nil {
		t.Fatalf("scene unmarshal: %v", err)
	}

	// Parse the APDS9960 and I2CBus blackbox definitions reused from
	// blackBox_test.go.
	apdsDef, err := blackbox.Parse([]byte(apds9960Source), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse apds9960: %v", err)
	}
	i2cDef, err := blackbox.Parse([]byte(i2cBusSource), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse i2c: %v", err)
	}

	req := Request{
		Scene:    json.RawMessage(sceneUint16VsInt),
		Language: "go",
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
			"APDS9960": apdsDef,
			"I2CBus":   i2cDef,
		},
	}
	resp := Generate(context.Background(), req)

	// (1) There should be a KindTypeLossy warning on gt_1. We iterate
	// all diagnostics looking for it — order is not asserted because
	// other benign warnings (missing-connection notes, etc.) may
	// appear in the same response.
	var gotWarn bool
	for _, d := range resp.Diagnostics {
		if d.Kind == diagnostics.KindTypeLossy && d.Severity == diagnostics.SeverityWarning {
			hasGT := false
			for _, id := range d.Devices {
				if id == "gt_1" {
					hasGT = true
					break
				}
			}
			if hasGT {
				gotWarn = true
				t.Logf("type-lossy warning: %s", d.Message)
			}
		}
	}
	if !gotWarn {
		t.Error("expected KindTypeLossy warning on gt_1, none found")
		for _, d := range resp.Diagnostics {
			t.Logf("  diag: kind=%s sev=%s devices=%v msg=%s",
				d.Kind, d.Severity, d.Devices, d.Message)
		}
	}

	// (2) No type-mismatch error (the case is lossy, not impossible).
	for _, d := range resp.Diagnostics {
		if d.Kind == diagnostics.KindTypeMismatch && d.Severity == diagnostics.SeverityError {
			t.Errorf("unexpected TypeMismatch error: %s", d.Message)
		}
	}

	// (3) The IR must contain a CONVERT instruction — the whole
	// point is that the abstract int gets promoted to uint16 on the
	// wire that feeds gt_1.inputY.
	if !strings.Contains(resp.IR, "CONVERT") {
		t.Error("expected CONVERT instruction in IR")
		t.Logf("IR:\n%s", resp.IR)
	}

	// (4) The generated Go must contain a uint16 cast somewhere. The
	// exact register naming is backend-specific, so we look for the
	// substring rather than a full match. Also, no `stmGreater1 :=
	// apds99600_clear > constInt0` form without a cast.
	if !strings.Contains(resp.Code, "uint16(") {
		t.Error("expected uint16(...) cast in generated Go code")
		t.Logf("Code:\n%s", resp.Code)
	}

	// (5) Code emission must not have been blocked — resp.Code has to
	// be non-empty.
	if resp.Code == "" {
		t.Error("expected generated code (warnings should not block emission)")
	}

	t.Logf("IR:\n%s", resp.IR)
	t.Logf("Go:\n%s", resp.Code)
}
