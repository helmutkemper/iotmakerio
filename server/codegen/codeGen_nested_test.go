package codegen

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// sceneNestedLoops reproduces a real scene exported from the IDE in which
// one StatementLoop (stmLoop_3) sits inside another (stmLoop_1), each with
// its own Bool(false) wired to its stop port. Before the builder was fixed
// to treat `isContainer` and `parent` as orthogonal facts, the inner Loop
// was silently dropped from codegen because the IDE tagged it with
// status="container" while the builder only added children to their
// parent's scope when status=="contained". The emitted Go file therefore
// had exactly ONE `for { }` instead of the expected two.
//
// This test locks that behavior so the regression cannot return. It asserts
// that both IR `LOOP_BEGIN` / `LOOP_END` pairs are present, that they
// properly straddle the inner ones, and that the Go output contains a
// `for {}` nested inside another `for {}`.
//
// The JSON below is copied verbatim from the user's export — keeping the
// real data (and its precise field ordering / omitted policy struct)
// documents the exact contract between the IDE and the codegen server.
//
// Português: Regressão pro bug do loop aninhado — valida que Loop dentro
// de Loop gera dois `for`s aninhados no Go final.
const sceneNestedLoops = `{
  "version": "1.0",
  "metadata": {
    "density": 1,
    "canvasWidth": 1214,
    "canvasHeight": 896,
    "camera": { "offsetX": -142.38, "offsetY": -105.09, "zoom": 0.81 }
  },
  "devices": [
    {
      "id": "stmLoop_1", "type": "StatementLoop", "kind": "complex",
      "position": { "x": 132.89, "y": 150.61 },
      "size": { "width": 999, "height": 670.3 },
      "outerBBox": { "x": 132.89, "y": 150.61, "width": 999, "height": 670.3 },
      "innerBBox": { "x": 152.89, "y": 170.61, "width": 959, "height": 630.3 },
      "connectors": [
        {
          "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
          "position": { "x": 1074.89, "y": 778.91 },
          "connections": [{ "wireId": "wire_2", "targetDevice": "bool_2", "targetPort": "output" }]
        }
      ],
      "containment": {
        "isContainer": true,
        "children": ["stmLoop_3", "bool_2"],
        "status": "container"
      }
    },
    {
      "id": "stmLoop_3", "type": "StatementLoop", "kind": "complex",
      "position": { "x": 432, "y": 322.16 },
      "size": { "width": 400.5, "height": 298.78 },
      "outerBBox": { "x": 432, "y": 322.16, "width": 400.5, "height": 298.78 },
      "innerBBox": { "x": 452, "y": 342.16, "width": 360.5, "height": 258.78 },
      "connectors": [
        {
          "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
          "position": { "x": 775.5, "y": 578.94 },
          "connections": [{ "wireId": "wire_1", "targetDevice": "bool_1", "targetPort": "output" }]
        }
      ],
      "containment": {
        "isContainer": true,
        "children": ["bool_1"],
        "parent": "stmLoop_1",
        "status": "contained"
      }
    },
    {
      "id": "bool_1", "type": "StatementBool", "kind": "simple",
      "properties": { "label": "", "value": false },
      "position": { "x": 619.52, "y": 496.51 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 619.52, "y": 496.51, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 731.52, "y": 524.51 },
          "connections": [{ "wireId": "wire_1", "targetDevice": "stmLoop_3", "targetPort": "stop" }]
        }
      ],
      "containment": {
        "isContainer": false,
        "parent": "stmLoop_3",
        "status": "contained"
      }
    },
    {
      "id": "bool_2", "type": "StatementBool", "kind": "simple",
      "properties": { "label": "", "value": false },
      "position": { "x": 932.11, "y": 704.2 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 932.11, "y": 704.2, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 1044.11, "y": 732.2 },
          "connections": [{ "wireId": "wire_2", "targetDevice": "stmLoop_1", "targetPort": "stop" }]
        }
      ],
      "containment": {
        "isContainer": false,
        "parent": "stmLoop_1",
        "status": "contained"
      }
    }
  ],
  "wires": [
    { "id": "wire_1", "from": { "device": "bool_1", "port": "output" }, "to": { "device": "stmLoop_3", "port": "stop" }, "dataType": "bool" },
    { "id": "wire_2", "from": { "device": "bool_2", "port": "output" }, "to": { "device": "stmLoop_1", "port": "stop" }, "dataType": "bool" }
  ]
}`

// TestNestedLoops asserts that a Loop-inside-Loop scene emits two nested
// `for` blocks in the generated Go source, with each Bool's break living
// in the correct scope.
//
// Regression guard: before the builder treated `parent` as the sole
// criterion for scope membership, the inner Loop was dropped and only
// ONE `for` appeared in the output.
func TestNestedLoops(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneNestedLoops),
		Language: "go",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected codegen errors: %v", resp.Errors)
	}

	t.Log("=== IR ===")
	t.Log(resp.IR)
	t.Log("=== Go ===")
	t.Log(resp.Code)

	// ── IR: both loops must open and close ────────────────────────────
	assertContains(t, resp.IR, "LOOP_BEGIN %stmLoop_1")
	assertContains(t, resp.IR, "LOOP_BEGIN %stmLoop_3")
	assertContains(t, resp.IR, "LOOP_END %stmLoop_1")
	assertContains(t, resp.IR, "LOOP_END %stmLoop_3")

	// ── IR: structural ordering — outer straddles inner ───────────────
	// Expected shape (relative positions, other ops may sit between):
	//   LOOP_BEGIN %stmLoop_1
	//     LOOP_BEGIN %stmLoop_3
	//       ...
	//       BREAK_IF %bool_1
	//     LOOP_END %stmLoop_3
	//     BREAK_IF %bool_2
	//   LOOP_END %stmLoop_1
	outerBegin := strings.Index(resp.IR, "LOOP_BEGIN %stmLoop_1")
	innerBegin := strings.Index(resp.IR, "LOOP_BEGIN %stmLoop_3")
	innerEnd := strings.Index(resp.IR, "LOOP_END %stmLoop_3")
	outerEnd := strings.Index(resp.IR, "LOOP_END %stmLoop_1")

	switch {
	case outerBegin < 0 || innerBegin < 0 || innerEnd < 0 || outerEnd < 0:
		t.Fatalf("missing a LOOP marker; positions: outerBegin=%d innerBegin=%d innerEnd=%d outerEnd=%d",
			outerBegin, innerBegin, innerEnd, outerEnd)
	case !(outerBegin < innerBegin && innerBegin < innerEnd && innerEnd < outerEnd):
		t.Errorf("loops are not properly nested in IR\n"+
			"  expected: outerBegin < innerBegin < innerEnd < outerEnd\n"+
			"  got:      %d < %d < %d < %d (values shown)\n"+
			"  IR:\n%s", outerBegin, innerBegin, innerEnd, outerEnd, resp.IR)
	}

	// ── IR: each BREAK_IF is tied to the correct Bool ─────────────────
	// bool_1 breaks the INNER loop (stmLoop_3); bool_2 breaks the OUTER.
	assertContains(t, resp.IR, "BREAK_IF %bool_1")
	assertContains(t, resp.IR, "BREAK_IF %bool_2")

	bool1Break := strings.Index(resp.IR, "BREAK_IF %bool_1")
	bool2Break := strings.Index(resp.IR, "BREAK_IF %bool_2")

	// bool_1's break must land inside the inner loop body
	if !(innerBegin < bool1Break && bool1Break < innerEnd) {
		t.Errorf("BREAK_IF %%bool_1 should sit inside stmLoop_3 (between %d and %d), got position %d",
			innerBegin, innerEnd, bool1Break)
	}
	// bool_2's break must land in the outer loop but OUTSIDE the inner
	if !(innerEnd < bool2Break && bool2Break < outerEnd) {
		t.Errorf("BREAK_IF %%bool_2 should sit in stmLoop_1 after stmLoop_3 ends (between %d and %d), got position %d",
			innerEnd, outerEnd, bool2Break)
	}

	// ── Go output: the critical structural assertion ──────────────────
	// Two `for {` openings must exist in the generated Go source.
	forCount := strings.Count(resp.Code, "for {")
	if forCount < 2 {
		t.Errorf("expected at least 2 `for {` blocks in generated Go, got %d\n  Go:\n%s",
			forCount, resp.Code)
	}

	// `for {` must appear nested: a second `for {` occurs before the
	// first closing `}`. The quickest portable check is a regex that
	// allows arbitrary interior content without another top-level close.
	nestedPattern := regexp.MustCompile(`(?s)for \{[^}]*for \{`)
	if !nestedPattern.MatchString(resp.Code) {
		t.Errorf("expected inner `for {` nested inside outer `for {`\n  Go:\n%s", resp.Code)
	}

	// Both Bools must be declared and each must feed its own `if ... break`.
	// The emitter currently writes `bool1 := bool(false)` (explicit cast);
	// older snapshots wrote `bool1 := false`. Accept either form by only
	// checking the assignment prefix.
	assertContains(t, resp.Code, "bool1 := ")
	assertContains(t, resp.Code, "bool2 := ")
	assertContains(t, resp.Code, "if bool1 {")
	assertContains(t, resp.Code, "if bool2 {")

	// There must be two `break` statements (one per loop).
	breakCount := strings.Count(resp.Code, "break")
	if breakCount < 2 {
		t.Errorf("expected 2 `break` statements (one per loop), got %d\n  Go:\n%s",
			breakCount, resp.Code)
	}
}
