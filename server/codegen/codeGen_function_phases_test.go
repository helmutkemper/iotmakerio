package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// L2 (2026-07-20): the stage's phase model IS the generated code's
// execution order, and data must flow forward. Português: O modelo de
// fases do palco É a ordem de execução; dado flui para frente.

const phasedFnHead = `{
  "version": "1.0",
  "metadata": { "language": "c" },
  "devices": [
    { "id": "fn", "type": "StatementFunction", "kind": "complex", "stage": "backend",
      "properties": { "functionName": "my_function", "phases": "%PHASES%" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 600, "height": 500 },
      "connectors": [],
      "containment": { "isContainer": true, "children": [%CHILDREN%] } },
    { "id": "zz", "type": "StatementConstInt", "kind": "simple", "stage": "backend",
      "properties": { "value": 1 },
      "position": { "x": 100, "y": 100 }, "size": { "width": 120, "height": 74 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [{ "wireId": "w1", "targetDevice": "rt1", "targetPort": "in" }] } ],
      "containment": { "isContainer": false, "parent": "fn", "status": "contained" } },
    { "id": "aa", "type": "StatementConstInt", "kind": "simple", "stage": "backend",
      "properties": { "value": 2 },
      "position": { "x": 100, "y": 200 }, "size": { "width": 120, "height": 74 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [{ "wireId": "w2", "targetDevice": "rt2", "targetPort": "in" }] } ],
      "containment": { "isContainer": false, "parent": "fn", "status": "contained" } },
    { "id": "rt1", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "out_zz", "tunnelParent": "fn", "tunnelSide": "right" },
      "position": { "x": 590, "y": 100 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "*", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "zz", "targetPort": "output" }] },
        { "port": "out", "dataType": "*", "isOutput": true, "connections": [] } ],
      "containment": { "isContainer": false, "status": "free" } },
    { "id": "rt2", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "out_aa", "tunnelParent": "fn", "tunnelSide": "right" },
      "position": { "x": 590, "y": 200 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "*", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "aa", "targetPort": "output" }] },
        { "port": "out", "dataType": "*", "isOutput": true, "connections": [] } ],
      "containment": { "isContainer": false, "status": "free" } }%EXTRADEV%
  ],
  "wires": [
    { "id": "w1", "from": { "device": "zz", "port": "output" }, "to": { "device": "rt1", "port": "in" }, "dataType": "int" },
    { "id": "w2", "from": { "device": "aa", "port": "output" }, "to": { "device": "rt2", "port": "in" }, "dataType": "int" }%EXTRAWIRE%
  ]
}`

func buildPhasedScene(phases, children, extraDev, extraWire string) string {
	s := strings.Replace(phasedFnHead, "%PHASES%", phases, 1)
	s = strings.Replace(s, "%CHILDREN%", children, 1)
	s = strings.Replace(s, "%EXTRADEV%", extraDev, 1)
	return strings.Replace(s, "%EXTRAWIRE%", extraWire, 1)
}

func TestFunctionPhaseOrderedEmission(t *testing.T) {
	// Phases REVERSE the ids' natural order: zz in phase 0, aa in
	// phase 1 — the phased emitter must honor it. Português: Fases
	// invertem a ordem natural; o emissor deve honrá-las.
	scene := buildPhasedScene(
		`[{\"id\":\"p0\",\"ids\":[\"zz\"]},{\"id\":\"p1\",\"ids\":[\"aa\"]}]`,
		`"zz", "aa"`, "", "")
	resp := Generate(context.Background(), Request{Scene: json.RawMessage(scene), Language: "c"})
	code := generatedCode(resp)
	zi := strings.Index(code, "= 1L;")
	ai := strings.Index(code, "= 2L;")
	if zi < 0 || ai < 0 || zi > ai {
		t.Fatalf("phase order not honored (zz@%d aa@%d); diags=%+v got:\n%s",
			zi, ai, resp.Diagnostics, code)
	}
}

func TestFunctionPhaseBackwardEdgeRejected(t *testing.T) {
	// aa (phase 1) feeding print pr (phase 0) breaks the forward-flow
	// law: error diagnostic and no program. Português: aa (fase 1)
	// alimentando pr (fase 0) quebra a lei — erro e sem programa.
	extraDev := `,
    { "id": "pr", "type": "StatementPrintInt", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 300, "y": 100 }, "size": { "width": 120, "height": 74 },
      "connectors": [ { "port": "value", "dataType": "int", "isOutput": false,
        "connections": [{ "wireId": "w3", "targetDevice": "aa", "targetPort": "output" }] } ],
      "containment": { "isContainer": false, "parent": "fn", "status": "contained" } }`
	extraWire := `,
    { "id": "w3", "from": { "device": "aa", "port": "output" }, "to": { "device": "pr", "port": "value" }, "dataType": "int" }`
	scene := buildPhasedScene(
		`[{\"id\":\"p0\",\"ids\":[\"zz\",\"pr\"]},{\"id\":\"p1\",\"ids\":[\"aa\"]}]`,
		`"zz", "aa", "pr"`, extraDev, extraWire)
	resp := Generate(context.Background(), Request{Scene: json.RawMessage(scene), Language: "c"})
	found := false
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Message, "later phase") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the forward-flow error; diags: %+v", resp.Diagnostics)
	}
	if strings.Contains(generatedCode(resp), "my_function") {
		t.Fatalf("backward edge must stop the press")
	}
}
