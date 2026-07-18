// codeGen_case_test.go — Slice 1 of the StatementCase device: codegen.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// StatementCase replaces StatementIfElse. A boolean selector with true/false
// cases lowers to an if/else (reusing that pipeline); any other selector emits
// a switch. This test exercises the INT selector → switch path end to end:
//
//   - graph builder reads "selectorType":"int", the "cases" array, and the
//     "defaultCaseId" property into Scope.SelectorPort + Scope.Cases;
//   - the IR emitter (emitCase) groups the scope's children by case and emits
//     SWITCH_BEGIN / CASE_LABEL / DEFAULT_LABEL / SWITCH_END;
//   - the Go and C backends render a real switch.
//
// The scene has a const int selector and three cases — two value cases
// ("0","1" and "2") plus a default. Each case body is a self-contained
// `const + const → Add` so it emits a statement in BOTH backends without any
// black-box. (The Add result is intentionally unused; Go would reject that at
// build time, so the Go side is validated by a syntax parse, and the C side —
// where an unused local is only a warning — is validated by an actual gcc
// compile.)
//
// Português: Fatia 1 do StatementCase (codegen). Selector int → switch. A cena
// tem um const int como selector e três cases (dois de valor + default); cada
// corpo é `const + const → Add` (emite nos dois backends, sem black-box). O Go
// é validado por parse de sintaxe; o C por compilação real com gcc.

package codegen

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sceneCaseIntSwitch = `{
  "metadata": { "schemaVersion": "1.1", "camera": {"x":0,"y":0,"zoom":1}, "canvas":{"w":1200,"h":800} },
  "devices": [
    {
      "id": "constInt_sel", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 1 },
      "position": { "x": 40, "y": 300 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 40, "y": 300, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 160, "y": 337 },
          "connections": [{ "wireId": "w_sel", "targetDevice": "stmCase_1", "targetPort": "selector" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "stmCase_1", "type": "StatementCase", "kind": "complex",
      "properties": {
        "selectorType": "int",
        "selectedCase": "case_a",
        "cases": [
          { "id": "case_a",   "label": "a",     "values": ["0","1"], "ids": ["const_a1","const_a2","add_a","print_a"] },
          { "id": "case_b",   "label": "b",     "values": ["2"],     "ids": ["const_b1","const_b2","add_b","print_b"] },
          { "id": "case_def", "label": "other", "values": [],        "ids": ["const_d1","const_d2","add_def","print_def"] }
        ],
        "defaultCaseId": "case_def"
      },
      "position": { "x": 300, "y": 80 }, "size": { "width": 620, "height": 620 },
      "outerBBox": { "x": 300, "y": 80, "width": 620, "height": 620 },
      "innerBBox": { "x": 310, "y": 120, "width": 600, "height": 560 },
      "connectors": [
        { "port": "selector", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 305, "y": 360 },
          "connections": [{ "wireId": "w_sel", "targetDevice": "constInt_sel", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": true, "status": "container",
        "children": ["const_a1","const_a2","add_a","print_a","const_b1","const_b2","add_b","print_b","const_d1","const_d2","add_def","print_def"] }
    },

    { "id": "const_a1", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 10 },
      "position": { "x": 330, "y": 140 }, "size": { "width": 110, "height": 70 },
      "outerBBox": { "x": 330, "y": 140, "width": 110, "height": 70 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 440, "y": 175 },
          "connections": [{ "wireId": "w_a1", "targetDevice": "add_a", "targetPort": "inputX" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "const_a2", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 20 },
      "position": { "x": 330, "y": 220 }, "size": { "width": 110, "height": 70 },
      "outerBBox": { "x": 330, "y": 220, "width": 110, "height": 70 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 440, "y": 255 },
          "connections": [{ "wireId": "w_a2", "targetDevice": "add_a", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "add_a", "type": "StatementAdd", "kind": "simple",
      "position": { "x": 500, "y": 180 }, "size": { "width": 80, "height": 80 },
      "outerBBox": { "x": 500, "y": 180, "width": 80, "height": 80 }, "innerBBox": null,
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 200 },
          "connections": [{ "wireId": "w_a1", "targetDevice": "const_a1", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 240 },
          "connections": [{ "wireId": "w_a2", "targetDevice": "const_a2", "targetPort": "output" }] },
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 580, "y": 220 }, "connections": [] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },

    { "id": "print_a", "type": "StatementPrintInt", "kind": "simple",
      "position": { "x": 0, "y": 0 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 0, "y": 0 },
          "connections": [{ "wireId": "w_pa", "targetDevice": "add_a", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "const_b1", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 5 },
      "position": { "x": 330, "y": 320 }, "size": { "width": 110, "height": 70 },
      "outerBBox": { "x": 330, "y": 320, "width": 110, "height": 70 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 440, "y": 355 },
          "connections": [{ "wireId": "w_b1", "targetDevice": "add_b", "targetPort": "inputX" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "const_b2", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 6 },
      "position": { "x": 330, "y": 400 }, "size": { "width": 110, "height": 70 },
      "outerBBox": { "x": 330, "y": 400, "width": 110, "height": 70 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 440, "y": 435 },
          "connections": [{ "wireId": "w_b2", "targetDevice": "add_b", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "add_b", "type": "StatementAdd", "kind": "simple",
      "position": { "x": 500, "y": 360 }, "size": { "width": 80, "height": 80 },
      "outerBBox": { "x": 500, "y": 360, "width": 80, "height": 80 }, "innerBBox": null,
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 380 },
          "connections": [{ "wireId": "w_b1", "targetDevice": "const_b1", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 420 },
          "connections": [{ "wireId": "w_b2", "targetDevice": "const_b2", "targetPort": "output" }] },
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 580, "y": 400 }, "connections": [] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },

    { "id": "print_b", "type": "StatementPrintInt", "kind": "simple",
      "position": { "x": 0, "y": 0 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 0, "y": 0 },
          "connections": [{ "wireId": "w_pb", "targetDevice": "add_b", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "const_d1", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 7 },
      "position": { "x": 330, "y": 500 }, "size": { "width": 110, "height": 70 },
      "outerBBox": { "x": 330, "y": 500, "width": 110, "height": 70 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 440, "y": 535 },
          "connections": [{ "wireId": "w_d1", "targetDevice": "add_def", "targetPort": "inputX" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "const_d2", "type": "StatementConstInt", "kind": "simple", "properties": { "value": 8 },
      "position": { "x": 330, "y": 580 }, "size": { "width": 110, "height": 70 },
      "outerBBox": { "x": 330, "y": 580, "width": 110, "height": 70 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 440, "y": 615 },
          "connections": [{ "wireId": "w_d2", "targetDevice": "add_def", "targetPort": "inputY" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "add_def", "type": "StatementAdd", "kind": "simple",
      "position": { "x": 500, "y": 540 }, "size": { "width": 80, "height": 80 },
      "outerBBox": { "x": 500, "y": 540, "width": 80, "height": 80 }, "innerBBox": null,
      "connectors": [
        { "port": "inputX", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 560 },
          "connections": [{ "wireId": "w_d1", "targetDevice": "const_d1", "targetPort": "output" }] },
        { "port": "inputY", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 502, "y": 600 },
          "connections": [{ "wireId": "w_d2", "targetDevice": "const_d2", "targetPort": "output" }] },
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 580, "y": 580 }, "connections": [] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } },
    { "id": "print_def", "type": "StatementPrintInt", "kind": "simple",
      "position": { "x": 0, "y": 0 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 0, "y": 0 },
          "connections": [{ "wireId": "w_pd", "targetDevice": "add_def", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "child", "parent": "stmCase_1" } }
  ],
  "wires": [
    { "id": "w_sel", "from": { "device": "constInt_sel", "port": "output" }, "to": { "device": "stmCase_1", "port": "selector" }, "dataType": "int" },
    { "id": "w_a1",  "from": { "device": "const_a1", "port": "output" }, "to": { "device": "add_a", "port": "inputX" }, "dataType": "int" },
    { "id": "w_a2",  "from": { "device": "const_a2", "port": "output" }, "to": { "device": "add_a", "port": "inputY" }, "dataType": "int" },
    { "id": "w_b1",  "from": { "device": "const_b1", "port": "output" }, "to": { "device": "add_b", "port": "inputX" }, "dataType": "int" },
    { "id": "w_b2",  "from": { "device": "const_b2", "port": "output" }, "to": { "device": "add_b", "port": "inputY" }, "dataType": "int" },
    { "id": "w_d1",  "from": { "device": "const_d1", "port": "output" }, "to": { "device": "add_def", "port": "inputX" }, "dataType": "int" },
    { "id": "w_d2",  "from": { "device": "const_d2", "port": "output" }, "to": { "device": "add_def", "port": "inputY" }, "dataType": "int" },
    { "id": "w_pa",  "from": { "device": "add_a",   "port": "output" }, "to": { "device": "print_a",   "port": "value" }, "dataType": "int" },
    { "id": "w_pb",  "from": { "device": "add_b",   "port": "output" }, "to": { "device": "print_b",   "port": "value" }, "dataType": "int" },
    { "id": "w_pd",  "from": { "device": "add_def", "port": "output" }, "to": { "device": "print_def", "port": "value" }, "dataType": "int" }
  ]
}`

// TestCaseIntSwitchGo asserts the Go backend renders a switch for an int-selector
// StatementCase: the selector value, two value cases ("0","1" and "2") and a
// default, each with a body. The generated source is parsed (syntax check)
// rather than built, because the unused Add result would fail `go build` for a
// reason unrelated to the switch.
func TestCaseIntSwitchGo(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneCaseIntSwitch),
		Language: "go",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors generating Go:\n%s", strings.Join(resp.Errors, "\n"))
	}

	for _, want := range []string{
		"switch ",    // switch on the selector
		"case 0, 1:", // first value case, both values on one label
		"case 2:",    // second value case
		"default:",   // the optional default case
	} {
		if !strings.Contains(resp.Files["main.go"], want) {
			t.Errorf("Go switch missing %q in:\n%s", want, resp.Files["main.go"])
		}
	}

	// All three case bodies must emit (an Add per case).
	if got := strings.Count(resp.Files["main.go"], " + "); got < 3 {
		t.Errorf("expected at least 3 additions (one per case body), got %d in:\n%s", got, resp.Files["main.go"])
	}

	// Syntactic validity (avoids the unused-variable build error).
	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", resp.Files["main.go"], parser.AllErrors); err != nil {
		t.Errorf("generated Go does not parse: %v\n%s", err, resp.Files["main.go"])
	}

	if t.Failed() {
		t.Logf("Go code:\n%s", resp.Files["main.go"])
	}
}

// TestCaseIntSwitchC asserts the C backend renders a switch for the same scene
// and that the generated C actually compiles with gcc. The C backend returns a
// file map (resp.Files), not resp.Files["main.go"]; for native-only scenes that is a single
// main.c. Each case body is wrapped in a brace block, so the per-case
// declarations are valid C99 and the unit links to a runnable binary.
func TestCaseIntSwitchC(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneCaseIntSwitch),
		Language: "c",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors generating C:\n%s", strings.Join(resp.Errors, "\n"))
	}
	if len(resp.Files) == 0 {
		t.Fatalf("C backend produced no files")
	}

	// Combine all generated file contents for structure assertions.
	var combined strings.Builder
	var names []string
	for name, content := range resp.Files {
		names = append(names, name)
		combined.WriteString("/* === " + name + " === */\n")
		combined.WriteString(content)
		combined.WriteString("\n")
	}
	code := combined.String()

	for _, want := range []string{
		"switch (", // C parenthesises the selector
		"case 0:",  // first value case: one label per value
		"case 1:",
		"case 2:",
		"default:",
		"break;", // each case body is terminated to avoid fallthrough
	} {
		if !strings.Contains(code, want) {
			t.Errorf("C switch missing %q in:\n%s", want, code)
		}
	}

	// Compile with gcc. Write every generated file to a temp dir (so headers
	// resolve) and compile the .c translation units together. Skip (don't
	// fail) if gcc is unavailable in the environment.
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skipf("gcc not available, skipping compile check: %v", err)
	}
	dir := t.TempDir()
	var cFiles []string
	for name, content := range resp.Files {
		p := filepath.Join(dir, name)
		if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr != nil {
			t.Fatalf("mkdir for %s: %v", name, mkErr)
		}
		if wErr := os.WriteFile(p, []byte(content), 0o644); wErr != nil {
			t.Fatalf("write %s: %v", name, wErr)
		}
		if strings.HasSuffix(name, ".c") {
			cFiles = append(cFiles, p)
		}
	}
	if len(cFiles) == 0 {
		t.Fatalf("no .c translation unit among generated files: %v", names)
	}
	bin := filepath.Join(dir, "case_gen")
	args := append([]string{"-std=c99", "-I", dir, "-o", bin}, cFiles...)
	out, err := exec.Command(gcc, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("generated C failed to compile: %v\n%s\n--- files ---\n%s", err, out, code)
	}

	if t.Failed() {
		t.Logf("C files:\n%s", code)
	}
}
