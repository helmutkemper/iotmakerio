// codeGen_bb_ifelse_test.go — Regression test for the 2026-06-18 session:
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// a black-box instance used in BOTH branches of an if/else had its variable
// declaration dropped, so the generated Go called `test0.SortInt(...)` in
// both branches while `var test0 Test` was never emitted ("the device does
// not appear in the generated code").
//
// Root cause: buildInstanceScopeOwners assigned the instance's declaration to
// the if/else scope, but an if/else emits as two separate blocks — there is no
// single body where a var visible to both branches can live, so the BB_DECL
// was lost. The fix hoists ownership up to the nearest enclosing loop or
// global, placing the declaration BEFORE the if/else.
//
// Português: Teste de regressão — uma instância de black-box usada nos DOIS
// branches do if/else tinha sua declaração descartada (chamava
// test0.SortInt(...) nos dois lados, mas `var test0 Test` nunca era emitido).
// O fix iça a posse da declaração pro loop/global mais próximo, antes do if.

package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// sortIntBBSource is a minimal black-box: a struct Test with a single method
// SortInt(a []int64). It mirrors the user's "BlackBoxSortInt:Test" component
// (method SortInt, struct Test) so the scene below can instantiate it.
const sortIntBBSource = `package bb

// Test sorts slices of integers in place.
//
// icon:sort. label:Test.
type Test struct {
}

// SortInt sorts the given slice ascending.
//
// executionOrder:4. icon:sort. label:sort.
//
// Params
//   a: the slice to sort.  connection:mandatory.
func (s *Test) SortInt(a []int64) {
	_ = a
}
`

// sceneBBInBothBranches mirrors the reported scene: a comparator feeds the
// if/else condition; the true branch holds constArrayInt_0 {1,2,3} →
// testSortInt_1, the false branch holds constArrayInt_1 {4,5,6} →
// testSortInt_2. BOTH black-box devices share instanceId "test_0", so they
// resolve to a single instance `test0` used in both branches.
const sceneBBInBothBranches = `{
  "version": "1.0",
  "metadata": { "density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
  "devices": [
    {
      "id": "stmIfElse_1", "type": "StatementIfElse", "kind": "complex",
      "properties": {
        "selectedBranch": "true",
        "trueBranchIDs": ["constArrayInt_0", "testSortInt_1"],
        "falseBranchIDs": ["constArrayInt_1", "testSortInt_2"]
      },
      "position": { "x": 300, "y": 100 }, "size": { "width": 700, "height": 540 },
      "outerBBox": { "x": 300, "y": 100, "width": 700, "height": 540 },
      "innerBBox": { "x": 320, "y": 140, "width": 660, "height": 480 },
      "connectors": [
        { "port": "condition", "dataType": "bool", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 305, "y": 360 },
          "connections": [{ "wireId": "w_cond", "targetDevice": "stmEqualTo_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": true, "status": "container",
        "children": ["constArrayInt_0", "testSortInt_1", "constArrayInt_1", "testSortInt_2"] }
    },
    {
      "id": "constInt_0", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 10 },
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
      "properties": { "value": 20 },
      "position": { "x": 30, "y": 250 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 30, "y": 250, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 150, "y": 287 },
          "connections": [{ "wireId": "w_y", "targetDevice": "stmEqualTo_1", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "stmEqualTo_1", "type": "StatementEqualTo", "kind": "simple",
      "position": { "x": 190, "y": 200 }, "size": { "width": 60, "height": 78 },
      "outerBBox": { "x": 190, "y": 200, "width": 60, "height": 78 }, "innerBBox": null,
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 192, "y": 215 },
          "connections": [{ "wireId": "w_x", "targetDevice": "constInt_0", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 192, "y": 242 },
          "connections": [{ "wireId": "w_y", "targetDevice": "constInt_1", "targetPort": "output" }] },
        { "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 238, "y": 228 },
          "connections": [{ "wireId": "w_cond", "targetDevice": "stmIfElse_1", "targetPort": "condition" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "constArrayInt_0", "type": "StatementConstArrayInt", "kind": "simple",
      "properties": { "elementType": "int", "values": "1,2,3" },
      "position": { "x": 360, "y": 220 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 360, "y": 220, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "[]int", "isOutput": true, "acceptNotConnected": false,
          "position": { "x": 472, "y": 248 },
          "connections": [{ "wireId": "w_a0", "targetDevice": "testSortInt_1", "targetPort": "a" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmIfElse_1", "status": "contained" }
    },
    {
      "id": "testSortInt_1", "type": "BlackBoxSortInt:Test", "kind": "simple",
      "label": "testSortInt_1",
      "properties": { "executionOrder": 4, "instanceId": "test_0" },
      "position": { "x": 560, "y": 200 }, "size": { "width": 160, "height": 106 },
      "outerBBox": { "x": 560, "y": 200, "width": 160, "height": 106 }, "innerBBox": null,
      "connectors": [
        { "port": "a", "dataType": "[]int64", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 568, "y": 255 },
          "connections": [{ "wireId": "w_a0", "targetDevice": "constArrayInt_0", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmIfElse_1", "status": "contained" }
    },
    {
      "id": "constArrayInt_1", "type": "StatementConstArrayInt", "kind": "simple",
      "properties": { "elementType": "int", "values": "4,5,6" },
      "position": { "x": 360, "y": 420 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 360, "y": 420, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "[]int", "isOutput": true, "acceptNotConnected": false,
          "position": { "x": 472, "y": 448 },
          "connections": [{ "wireId": "w_a1", "targetDevice": "testSortInt_2", "targetPort": "a" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmIfElse_1", "status": "contained" }
    },
    {
      "id": "testSortInt_2", "type": "BlackBoxSortInt:Test", "kind": "simple",
      "label": "testSortInt_2",
      "properties": { "executionOrder": 4, "instanceId": "test_0" },
      "position": { "x": 560, "y": 400 }, "size": { "width": 160, "height": 106 },
      "outerBBox": { "x": 560, "y": 400, "width": 160, "height": 106 }, "innerBBox": null,
      "connectors": [
        { "port": "a", "dataType": "[]int64", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 568, "y": 455 },
          "connections": [{ "wireId": "w_a1", "targetDevice": "constArrayInt_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "parent": "stmIfElse_1", "status": "contained" }
    }
  ],
  "wires": [
    { "id": "w_cond", "from": { "device": "stmEqualTo_1", "port": "output" }, "to": { "device": "stmIfElse_1", "port": "condition" }, "dataType": "bool" },
    { "id": "w_x", "from": { "device": "constInt_0", "port": "output" }, "to": { "device": "stmEqualTo_1", "port": "inputX" }, "dataType": "int" },
    { "id": "w_y", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "stmEqualTo_1", "port": "inputY" }, "dataType": "int" },
    { "id": "w_a0", "from": { "device": "constArrayInt_0", "port": "output" }, "to": { "device": "testSortInt_1", "port": "a" }, "dataType": "[]int64" },
    { "id": "w_a1", "from": { "device": "constArrayInt_1", "port": "output" }, "to": { "device": "testSortInt_2", "port": "a" }, "dataType": "[]int64" }
  ]
}`

// TestBlackBoxInstanceDeclaredBeforeIfElse asserts that a black-box instance
// used in both branches of an if/else is DECLARED (once) before the if/else,
// not dropped. Before the fix the declaration was lost and the generated code
// referenced `test0` without declaring it.
func TestBlackBoxInstanceDeclaredBeforeIfElse(t *testing.T) {
	def, err := blackbox.Parse([]byte(sortIntBBSource), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse Test source: %v", err)
	}

	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneBBInBothBranches),
		Language:     "go",
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{"Test": def},
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("expected no errors, got: %v\nCode:\n%s", resp.Errors, resp.Files["main.go"])
	}
	if resp.Files["main.go"] == "" {
		t.Fatalf("expected Go code, got empty")
	}

	code := resp.Files["main.go"]

	// goIdent collapses "test_0" → "test0"; tolerate either form defensively.
	declA := "var test0 Test"
	declB := "var test_0 Test"
	declIdx := strings.Index(code, declA)
	if declIdx < 0 {
		declIdx = strings.Index(code, declB)
	}
	if declIdx < 0 {
		t.Fatalf("black-box instance is never declared — expected `%s` before the if/else.\nCode:\n%s",
			declA, code)
	}

	// The declaration must sit BEFORE the if/else (so it is visible to both
	// branches), not inside one branch.
	ifIdx := strings.Index(code, "if ")
	if ifIdx < 0 {
		t.Fatalf("expected an if statement in generated code\n%s", code)
	}
	if declIdx > ifIdx {
		t.Errorf("instance declaration must come BEFORE the if/else (visible to both branches), "+
			"but it appears after the `if`.\nCode:\n%s", code)
	}

	// Both branches must call the method on the shared instance.
	callA := "test0.SortInt("
	callB := "test_0.SortInt("
	calls := strings.Count(code, callA) + strings.Count(code, callB)
	if calls < 2 {
		t.Errorf("expected the method called in both branches (>=2 calls), got %d\nCode:\n%s",
			calls, code)
	}
}
