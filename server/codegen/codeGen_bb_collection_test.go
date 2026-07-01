// /server/codegen/codeGen_bb_collection_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// codeGen_bb_collection_test.go — End-to-end coverage for Task 6 of the
// const-array plan: IDE constants feeding AUTHORED Go black-box parameters.
//
// Three decisions are pinned here (Kemper, 2026-06-10 — see
// docs/claude_const_array_plan.md §2 / T6):
//
//   - DECISION B — the collection element type FLOWS FROM THE CONSUMER.
//     A ConstArrayInt wired into an authored `values []uint16` parameter
//     must DECLARE []uint16{…}, never the default []int64: Go slices have
//     no implicit conversion, so any other declaration cannot compile.
//     The IR reads the consumer port's DataType straight from the graph
//     (the WASM registers black-box input ports with the authored type).
//
//   - CAST ESCALAR — every scalar argument at a BB call site is wrapped
//     in a conversion to the authored parameter type:
//     `mixer1.Run(…, uint16(constInt1))`. Unconditional and idempotent
//     (identity conversion is legal Go), so the emitter never tracks the
//     source register's type.
//
//   - IÇAMENTO — a constant collection that crosses its scope outward is
//     hoisted WHOLE to the parent scope (a fixed C array cannot be
//     reassigned, so the scalar VAR+ASSIGN promotion scheme does not
//     apply). The declaration must appear BEFORE the loop; no zero-value
//     `var` is emitted.
//
// The conflict path is also pinned: two consumers demanding DIFFERENT
// concrete elements from the same constant is a maker error that blocks
// codegen with a clear message (run Step 1d).
//
// The black-box defs come from the REAL parser over fixture sources, so
// the `[]uint16` → "[]uint16" port-type path (typeString on
// ast.ArrayType) is exercised too — not hand-built defs.
//
// Português: Cobertura ponta-a-ponta da T6 — constantes do IDE
// alimentando parâmetros autorais de black-box Go. Fixa a decisão B (tipo
// do elemento flui do consumidor), o cast escalar no call site, o
// içamento da coleção inteira e o erro de fan-out conflitante. Os defs
// vêm do parser real sobre fontes fixture.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// mixerSource is an authored Go black-box whose Run takes a CONCRETE
// integer collection plus a concrete scalar — the exact shape decision B
// and the scalar cast exist for.
const mixerSource = `package bb

// Mixer blends a fixed table of levels scaled by a gain.
//
// icon:gear. label:Mixer.
type Mixer struct{}

// Run consumes the level table.
//
// Params
//   values: level table.  connection:mandatory.
//   gain:   scale factor. connection:mandatory.
func (m *Mixer) Run(values []uint16, gain uint16) {
	_ = values
	_ = gain
}
`

// filterSource is a second authored black-box demanding a DIFFERENT
// concrete collection element — used by the fan-out conflict test.
const filterSource = `package bb

// Filter consumes signed 32-bit samples.
//
// icon:gear. label:Filter.
type Filter struct{}

// Run consumes the sample table.
//
// Params
//   values: sample table.  connection:mandatory.
func (f *Filter) Run(values []int32) {
	_ = values
}
`

// parseDef parses a fixture source through the real black-box parser,
// failing the test on any parse error.
func parseDef(t *testing.T, src string) *blackbox.BlackBoxDef {
	t.Helper()
	def, err := blackbox.Parse([]byte(src), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("fixture parse failed: %v", err)
	}
	return def
}

// TestBBCollection_InferenceAndScalarCast: the central T6 scene.
// ConstArrayInt("1, 2, 3") → Mixer.values ([]uint16 authored) and
// ConstInt(7) → Mixer.gain (uint16 authored), all at global scope.
func TestBBCollection_InferenceAndScalarCast(t *testing.T) {
	scene := `{
  "version": "1.0",
  "metadata": { "density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
  "devices": [
    {
      "id": "constArrayInt_1", "type": "StatementConstArrayInt",
      "properties": { "values": "1, 2, 3", "elementType": "int" },
      "position": { "x": 100, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 100, "width": 100, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "output", "dataType": "[]int", "isOutput": true, "acceptNotConnected": false,
          "connections": [{ "wireId": "w1", "targetDevice": "mixer_1_run", "targetPort": "values" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "constInt_1", "type": "StatementConstInt",
      "properties": { "value": 7 },
      "position": { "x": 100, "y": 200 }, "size": { "width": 80, "height": 50 },
      "outerBBox": { "x": 100, "y": 200, "width": 80, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w2", "targetDevice": "mixer_1_run", "targetPort": "gain" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "mixer_1_run", "type": "BlackBoxRun:Mixer",
      "properties": { "instanceId": "mixer_1" },
      "position": { "x": 320, "y": 120 }, "size": { "width": 140, "height": 80 },
      "outerBBox": { "x": 320, "y": 120, "width": 140, "height": 80 }, "overlapPolicy": {},
      "connectors": [
        { "port": "values", "dataType": "[]uint16", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "constArrayInt_1", "targetPort": "output" }] },
        { "port": "gain", "dataType": "uint16", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "constInt_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constArrayInt_1", "port": "output" }, "to": { "device": "mixer_1_run", "port": "values" }, "dataType": "[]int" },
    { "id": "w2", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "mixer_1_run", "port": "gain" }, "dataType": "int" }
  ]
}`

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
			"Mixer": parseDef(t, mixerSource),
		},
	})
	if len(resp.Errors) > 0 {
		t.Fatalf("Errors: %v", resp.Errors)
	}
	t.Logf("Generated Go:\n%s", resp.Code)

	// DECISION B: the declaration carries the CONSUMER's element type —
	// []uint16, not the []int64 default.
	assertContains(t, resp.Code, "constArrayInt1 := []uint16{1, 2, 3}")
	if strings.Contains(resp.Code, "[]int64{1, 2, 3}") {
		t.Errorf("declaration must follow the consumer's []uint16, found []int64:\n%s", resp.Code)
	}

	// CAST ESCALAR: the scalar argument is cast to the authored uint16;
	// the collection argument passes uncast (its type already matches by
	// inference — and slices have no conversion anyway).
	assertContains(t, resp.Code, "mixer1.Run(constArrayInt1, uint16(constInt1))")
}

// TestBBCollection_FanOutConflict: one constant, two consumers demanding
// DIFFERENT concrete elements ([]uint16 vs []int32) — codegen must refuse
// with a clear message instead of shipping an uncompilable declaration.
func TestBBCollection_FanOutConflict(t *testing.T) {
	scene := `{
  "version": "1.0",
  "metadata": { "density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
  "devices": [
    {
      "id": "constArrayInt_1", "type": "StatementConstArrayInt",
      "properties": { "values": "1, 2, 3", "elementType": "int" },
      "position": { "x": 100, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 100, "width": 100, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "output", "dataType": "[]int", "isOutput": true, "acceptNotConnected": false,
          "connections": [
            { "wireId": "w1", "targetDevice": "mixer_1_run", "targetPort": "values" },
            { "wireId": "w2", "targetDevice": "filter_1_run", "targetPort": "values" }
          ] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "constInt_1", "type": "StatementConstInt", "properties": { "value": 7 },
      "position": { "x": 100, "y": 200 }, "size": { "width": 80, "height": 50 },
      "outerBBox": { "x": 100, "y": 200, "width": 80, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w3", "targetDevice": "mixer_1_run", "targetPort": "gain" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "mixer_1_run", "type": "BlackBoxRun:Mixer",
      "properties": { "instanceId": "mixer_1" },
      "position": { "x": 320, "y": 80 }, "size": { "width": 140, "height": 80 },
      "outerBBox": { "x": 320, "y": 80, "width": 140, "height": 80 }, "overlapPolicy": {},
      "connectors": [
        { "port": "values", "dataType": "[]uint16", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "constArrayInt_1", "targetPort": "output" }] },
        { "port": "gain", "dataType": "uint16", "isOutput": false,
          "connections": [{ "wireId": "w3", "targetDevice": "constInt_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "filter_1_run", "type": "BlackBoxRun:Filter",
      "properties": { "instanceId": "filter_1" },
      "position": { "x": 320, "y": 220 }, "size": { "width": 140, "height": 80 },
      "outerBBox": { "x": 320, "y": 220, "width": 140, "height": 80 }, "overlapPolicy": {},
      "connectors": [
        { "port": "values", "dataType": "[]int32", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "constArrayInt_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constArrayInt_1", "port": "output" }, "to": { "device": "mixer_1_run", "port": "values" }, "dataType": "[]int" },
    { "id": "w2", "from": { "device": "constArrayInt_1", "port": "output" }, "to": { "device": "filter_1_run", "port": "values" }, "dataType": "[]int" },
    { "id": "w3", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "mixer_1_run", "port": "gain" }, "dataType": "int" }
  ]
}`

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
			"Mixer":  parseDef(t, mixerSource),
			"Filter": parseDef(t, filterSource),
		},
	})
	if len(resp.Errors) == 0 {
		t.Fatalf("expected a blocking error for conflicting collection element demands, got none\ncode:\n%s", resp.Code)
	}
	joined := strings.Join(resp.Errors, " | ")
	if !strings.Contains(joined, "different collection element types") {
		t.Errorf("error message should explain the element-type conflict, got: %s", joined)
	}
	t.Logf("conflict error (as expected): %s", joined)
}

// TestBBCollection_HoistAcrossScope: the constant lives INSIDE the loop
// but its consumer (a Gauge) sits OUTSIDE — the whole declaration must be
// hoisted before the loop, with no zero-value `var` + reassignment.
func TestBBCollection_HoistAcrossScope(t *testing.T) {
	scene := `{
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
        { "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
          "connections": [{ "wireId": "w_stop", "targetDevice": "compare_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": true, "children": ["constArrayInt_1", "constInt_a", "constInt_b", "compare_1"], "status": "container" }
    },
    {
      "id": "constArrayInt_1", "type": "StatementConstArrayInt",
      "properties": { "values": "1, 2, 3", "elementType": "int" },
      "position": { "x": 100, "y": 120 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 120, "width": 100, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "output", "dataType": "[]int", "isOutput": true, "acceptNotConnected": false,
          "connections": [{ "wireId": "w1", "targetDevice": "gauge_1", "targetPort": "current" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "constInt_a", "type": "StatementConstInt", "properties": { "value": 1 },
      "position": { "x": 100, "y": 200 }, "size": { "width": 80, "height": 50 },
      "outerBBox": { "x": 100, "y": 200, "width": 80, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w2", "targetDevice": "compare_1", "targetPort": "inputX" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "constInt_b", "type": "StatementConstInt", "properties": { "value": 1 },
      "position": { "x": 100, "y": 280 }, "size": { "width": 80, "height": 50 },
      "outerBBox": { "x": 100, "y": 280, "width": 80, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w3", "targetDevice": "compare_1", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "compare_1", "type": "StatementEqualTo",
      "position": { "x": 300, "y": 240 }, "size": { "width": 80, "height": 60 },
      "outerBBox": { "x": 300, "y": 240, "width": 80, "height": 60 }, "overlapPolicy": {},
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "constInt_a", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "w3", "targetDevice": "constInt_b", "targetPort": "output" }] },
        { "port": "output", "dataType": "bool", "isOutput": true,
          "connections": [{ "wireId": "w_stop", "targetDevice": "stmLoop_1", "targetPort": "stop" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "gauge_1", "type": "StatementGauge", "label": "view",
      "position": { "x": 700, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 700, "y": 100, "width": 100, "height": 50 }, "overlapPolicy": {},
      "connectors": [
        { "port": "current", "dataType": "[]int", "isOutput": false, "acceptNotConnected": true,
          "connections": [{ "wireId": "w1", "targetDevice": "constArrayInt_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1",     "from": { "device": "constArrayInt_1", "port": "output" }, "to": { "device": "gauge_1",   "port": "current" }, "dataType": "[]int" },
    { "id": "w2",     "from": { "device": "constInt_a",      "port": "output" }, "to": { "device": "compare_1", "port": "inputX" },  "dataType": "int" },
    { "id": "w3",     "from": { "device": "constInt_b",      "port": "output" }, "to": { "device": "compare_1", "port": "inputY" },  "dataType": "int" },
    { "id": "w_stop", "from": { "device": "compare_1",       "port": "output" }, "to": { "device": "stmLoop_1", "port": "stop" },    "dataType": "bool" }
  ]
}`

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
	})
	if len(resp.Errors) > 0 {
		t.Fatalf("Errors: %v", resp.Errors)
	}
	t.Logf("Generated Go:\n%s", resp.Code)

	code := resp.Code

	// The WHOLE declaration is hoisted: it must appear BEFORE the loop.
	declIdx := strings.Index(code, "constArrayInt1 := []int64{1, 2, 3}")
	loopIdx := strings.Index(code, "for {")
	if declIdx < 0 {
		t.Fatalf("hoisted declaration not found:\n%s", code)
	}
	if loopIdx < 0 {
		t.Fatalf("loop not found:\n%s", code)
	}
	if declIdx > loopIdx {
		t.Errorf("declaration must be hoisted BEFORE the loop (decl at %d, loop at %d):\n%s", declIdx, loopIdx, code)
	}

	// No scalar-style promotion artifacts: no zero-value `var` for the
	// collection, and the declaration appears exactly once.
	if strings.Contains(code, "var constArrayInt1") {
		t.Errorf("collection must be hoisted whole, not VAR+ASSIGN promoted:\n%s", code)
	}
	if strings.Count(code, "constArrayInt1 := ") != 1 {
		t.Errorf("hoisted declaration must appear exactly once:\n%s", code)
	}

	// The outer consumer still reads it after the loop (the Gauge prints
	// "label, value", so the hoisted name appears as the second argument).
	printIdx := strings.LastIndex(code, `fmt.Println("view", constArrayInt1)`)
	if printIdx < 0 || printIdx < loopIdx {
		t.Errorf("outer Gauge must read the hoisted name after the loop:\n%s", code)
	}
}
