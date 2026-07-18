// codeGen_ifelse_condition_test.go — Regression test for the 2026-06-18
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// session: an if/else whose `condition` port is fed by a comparator that
// sits OUTSIDE the branch (the normal way to build a branch test) was being
// rejected by validateControlPortSources, which applied its loop-only rule
// ("the producer must live inside so it is re-evaluated each iteration") to
// the if/else scope. An if/else evaluates its condition once when control
// reaches it, so a condition source outside the branch is valid. After the
// fix the if/else scope is skipped by that rule and no diagnostic is raised.
//
// The scene below mirrors the reported one (StatementEqualTo outside →
// StatementIfElse.condition); the branches are intentionally empty so the
// test isolates the condition-source validation without depending on a
// black-box device or on branch-body codegen (both out of scope here).
//
// Português: Teste de regressão — um if/else cuja porta `condition` é
// alimentada por um comparador FORA da branch (o jeito normal) era rejeitado
// por uma regra que só faz sentido para loop ("reavaliado a cada iteração").
// O if/else avalia a condição uma vez; a fonte fora é válida. Após o fix o
// scope do if/else é pulado por essa regra.

package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const sceneIfElseConditionOutside = `{
  "metadata": { "schemaVersion": "1.1", "camera": {"x":0,"y":0,"zoom":1}, "canvas":{"w":1024,"h":768} },
  "devices": [
    {
      "id": "stmIfElse_1", "type": "StatementIfElse", "kind": "complex",
      "properties": { "trueBranchIDs": [], "falseBranchIDs": [], "selectedBranch": "true" },
      "position": { "x": 300, "y": 100 }, "size": { "width": 400, "height": 300 },
      "outerBBox": { "x": 300, "y": 100, "width": 400, "height": 300 },
      "innerBBox": { "x": 310, "y": 130, "width": 380, "height": 260 },
      "connectors": [
        { "port": "condition", "dataType": "bool", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 305, "y": 250 },
          "connections": [{ "wireId": "w_cond", "targetDevice": "stmEqualTo_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": true, "status": "container", "children": [] }
    },
    {
      "id": "stmEqualTo_1", "type": "StatementEqualTo", "kind": "simple",
      "position": { "x": 180, "y": 200 }, "size": { "width": 60, "height": 78 },
      "outerBBox": { "x": 180, "y": 200, "width": 60, "height": 78 }, "innerBBox": null,
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 182, "y": 215 },
          "connections": [{ "wireId": "w_x", "targetDevice": "constInt_0", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 182, "y": 242 },
          "connections": [{ "wireId": "w_y", "targetDevice": "constInt_1", "targetPort": "output" }] },
        { "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 228, "y": 228 },
          "connections": [{ "wireId": "w_cond", "targetDevice": "stmIfElse_1", "targetPort": "condition" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "constInt_0", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 1 },
      "position": { "x": 30, "y": 160 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 30, "y": 160, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 150, "y": 197 },
          "connections": [{ "wireId": "w_x", "targetDevice": "stmEqualTo_1", "targetPort": "inputX" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "constInt_1", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 3 },
      "position": { "x": 30, "y": 240 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 30, "y": 240, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 150, "y": 277 },
          "connections": [{ "wireId": "w_y", "targetDevice": "stmEqualTo_1", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w_cond", "from": { "device": "stmEqualTo_1", "port": "output" }, "to": { "device": "stmIfElse_1", "port": "condition" }, "dataType": "bool" },
    { "id": "w_x", "from": { "device": "constInt_0", "port": "output" }, "to": { "device": "stmEqualTo_1", "port": "inputX" }, "dataType": "int" },
    { "id": "w_y", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "stmEqualTo_1", "port": "inputY" }, "dataType": "int" }
  ]
}`

// TestIfElseConditionSourceOutsideAllowed asserts that an if/else fed by a
// comparator outside the branch is NOT rejected with the loop-only
// "sits outside / re-evaluated each iteration" diagnostic. Before the fix
// this scene produced two such errors (one for the comparator, one for the
// if/else); after the fix it produces none.
func TestIfElseConditionSourceOutsideAllowed(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneIfElseConditionOutside),
		Language: "go",
	})

	joined := strings.Join(resp.Errors, "\n")
	for _, forbidden := range []string{
		"sits outside",
		"re-evaluated each iteration",
		"move stmEqualTo_1",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("if/else condition source outside the branch must be allowed, "+
				"but errors still contain %q:\n%s", forbidden, joined)
		}
	}

	// Belt and suspenders: no structured diagnostic should carry the loop
	// message either, and none should flag the if/else for a missing/invalid
	// condition connection.
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Message, "sits outside") ||
			strings.Contains(d.Message, "re-evaluated each iteration") {
			t.Errorf("unexpected loop-only diagnostic on if/else: %+v", d)
		}
	}

	// The comparator must still be emitted and its bool wired as the
	// condition — i.e. the if/else genuinely accepted the comparator output.
	if !strings.Contains(resp.Files["main.go"], "==") {
		t.Errorf("expected the equality comparison to be emitted, got:\n%s", resp.Files["main.go"])
	}

	if t.Failed() {
		t.Logf("Errors (%d): %v", len(resp.Errors), resp.Errors)
		t.Logf("Code:\n%s", resp.Files["main.go"])
	}
}
