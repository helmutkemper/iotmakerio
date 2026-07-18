// /server/codegen/codeGen_const_float_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// codeGen_const_float_test.go — End-to-end coverage for StatementConstFloat.
//
// Before this device had an IR emit case it fell through to the default
// "unknown device type" warning and produced no code at all. These tests
// drive a minimal scene (a float constant wired into a Gauge so the value
// is actually used) through Generate and assert the maker's precision
// survives into both backends:
//
//   - float32 → C "float" + "<v>f" literal;  Go "float32".
//   - float64 → C "double" + "<v>" literal (no suffix);  Go "float64".
//
// Precision is a per-device choice (the Inspect overlay's float32/float64
// toggle), so it is honoured verbatim regardless of the target profile —
// unlike the abstract "int"/"float", whose width the profile resolves.
// (The literal-formatting edge cases — ".0" insertion, the "f" suffix —
// are pinned directly in ansic/ident_test.go; here we only prove the
// precision token reaches the backend through the full pipeline.)
//
// Português: cobertura ponta-a-ponta do StatementConstFloat. Antes do case
// no emit, o device caía no default e não gerava nada. Aqui uma cena mínima
// (const float → Gauge, pra o valor ser usado) passa pelo Generate e os
// testes garantem que a precisão chega aos dois backends, independente do
// profile — é escolha do maker.

import (
	"context"
	"encoding/json"
	"testing"
)

// sceneConstFloat builds a scene with a single float constant (value 3.14)
// wired into a Gauge. The precision parameter sets both the device's
// "precision" property and every connector/wire dataType, mirroring what
// the IDE serialises. The Gauge only prints the wired value (emitGauge is
// type-agnostic), which keeps the constant "used" so its declaration is
// actually emitted rather than dropped as dead code.
func sceneConstFloat(precision string) string {
	return `{
  "version": "1.0",
  "metadata": { "density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
  "devices": [
    {
      "id": "constFloat_1", "type": "StatementConstFloat",
      "properties": { "value": 3.14, "precision": "` + precision + `" },
      "position": { "x": 100, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 100, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "output", "dataType": "` + precision + `", "isOutput": true, "acceptNotConnected": true, "position": { "x": 192, "y": 125 },
          "connections": [{ "wireId": "w1", "targetDevice": "gauge_1", "targetPort": "current" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "gauge_1", "type": "StatementGauge", "label": "temp",
      "position": { "x": 300, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 300, "y": 100, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "current", "dataType": "` + precision + `", "isOutput": false, "acceptNotConnected": true, "position": { "x": 308, "y": 125 },
          "connections": [{ "wireId": "w1", "targetDevice": "constFloat_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constFloat_1", "port": "output" }, "to": { "device": "gauge_1", "port": "current" }, "dataType": "` + precision + `" }
  ]
}`
}

// TestConstFloat_Float32_EndToEnd: float32 → C "float" + "3.14f", Go "float32".
func TestConstFloat_Float32_EndToEnd(t *testing.T) {
	scene := sceneConstFloat("float32")

	cResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "c",
	})
	if len(cResp.Errors) > 0 {
		t.Fatalf("C backend returned errors: %v", cResp.Errors)
	}
	mainC, ok := cResp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; got %d entries", len(cResp.Files))
	}
	t.Logf("Generated C (float32):\n%s", mainC)
	assertContains(t, mainC, "float constFloat1")
	assertContains(t, mainC, "3.14f")

	goResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
	})
	if len(goResp.Errors) > 0 {
		t.Fatalf("Go backend returned errors: %v", goResp.Errors)
	}
	t.Logf("Generated Go (float32):\n%s", goResp.Files["main.go"])
	assertContains(t, goResp.Files["main.go"], "float32")
	assertContains(t, goResp.Files["main.go"], "3.14")
}

// TestConstFloat_Float64_EndToEnd: float64 → C "double" + "3.14", Go "float64".
func TestConstFloat_Float64_EndToEnd(t *testing.T) {
	scene := sceneConstFloat("float64")

	cResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "c",
	})
	if len(cResp.Errors) > 0 {
		t.Fatalf("C backend returned errors: %v", cResp.Errors)
	}
	mainC, ok := cResp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; got %d entries", len(cResp.Files))
	}
	t.Logf("Generated C (float64):\n%s", mainC)
	assertContains(t, mainC, "double constFloat1")
	assertContains(t, mainC, "3.14")

	goResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
	})
	if len(goResp.Errors) > 0 {
		t.Fatalf("Go backend returned errors: %v", goResp.Errors)
	}
	t.Logf("Generated Go (float64):\n%s", goResp.Files["main.go"])
	assertContains(t, goResp.Files["main.go"], "float64")
	assertContains(t, goResp.Files["main.go"], "3.14")
}
