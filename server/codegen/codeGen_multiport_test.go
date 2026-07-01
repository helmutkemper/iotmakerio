// server/codegen/codeGen_multiport_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// apdsMultiPortSource is a minimal BlackBox definition used only by the
// multi-port scope-crossing test. It declares Run (4 outputs) and Log (4
// inputs) so the scene below can wire them to each other, which is the
// shape needed to trigger the emitter's validator. We keep it local to
// this file to avoid coupling with the richer fixture used by
// blackBox_test.go.
//
// Português: Fonte BlackBox mínima só pra esse teste. Tem Run com 4
// saídas e Log com 4 entradas — o suficiente pra disparar o validador
// de scope-crossing.
const apdsMultiPortSource = `package bb

// APDS9960 is a color sensor.
//
// icon:sun. label:APDS9960.
type APDS9960 struct {
	gain byte ` + "`" + `prop:"Gain" default:"0"` + "`" + `
}

// Run reads the four colour channels.
//
// executionOrder:10. icon:sun. label:read.
//
// Returns
//   clear: total light.  connection:optional.
//   red:   red channel.  connection:optional.
//   green: green channel.  connection:optional.
//   blue:  blue channel.  connection:optional.
func (s *APDS9960) Run() (clear, red, green, blue uint16) {
	return 0, 0, 0, 0
}

// Log prints the four colour channels.
//
// executionOrder:20. icon:usb. label:log.
//
// Params
//   clear: total light.  connection:mandatory.
//   red:   red channel.  connection:mandatory.
//   green: green channel.  connection:mandatory.
//   blue:  blue channel.  connection:mandatory.
func (s *APDS9960) Log(clear, red, green, blue uint16) {
	_ = clear
}
`

// sceneMultiPortScopeCross reproduces a user-reported scene in which a
// BlackBoxRun:APDS9960 sits inside a Loop and a BlackBoxLog:APDS9960 sits
// outside the Loop. Four wires connect Run's outputs (clear/red/green/
// blue, all uint16) to Log's inputs. Because Log lives in the Loop's
// parent scope, the Run node is promoted — but it has four output ports,
// and the current emitter cannot yet represent per-port promotion. The
// old behavior was to emit broken Go code (four := vars inside the for
// loop, then a Log call outside referencing names that do not exist in
// that scope). The correct behavior is to refuse codegen with a clear
// diagnostic that names both devices, the loop, and suggests the fix.
//
// When per-port promotion lands, this test should be updated to assert
// the happy-path output instead.
//
// Português: Cena que a IDE exporta quando o usuário coloca um
// BlackBoxLog fora de um Loop mas o conecta a um BlackBoxRun dentro. O
// emitter atual não sabe promover device multi-output, então deve
// bloquear codegen com mensagem clara em vez de gerar código quebrado.
const sceneMultiPortScopeCross = `{
  "version": "1.0",
  "metadata": {
    "density": 1, "canvasWidth": 1200, "canvasHeight": 800,
    "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 }
  },
  "devices": [
    {
      "id": "stmLoop_1", "type": "StatementLoop", "kind": "complex",
      "position": { "x": 100, "y": 200 },
      "size": { "width": 500, "height": 300 },
      "outerBBox": { "x": 100, "y": 200, "width": 500, "height": 300 },
      "innerBBox": { "x": 120, "y": 220, "width": 460, "height": 260 },
      "connectors": [
        {
          "port": "stop", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
          "position": { "x": 540, "y": 480 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "bool_0", "targetPort": "output" }]
        }
      ],
      "containment": {
        "isContainer": true,
        "children": ["apds9960Run_1", "bool_0"],
        "status": "container"
      }
    },
    {
      "id": "apds9960Run_1", "type": "BlackBoxRun:APDS9960", "kind": "simple",
      "label": "apds9960Run_1",
      "properties": { "executionOrder": 10, "instanceId": "apds9960_0" },
      "position": { "x": 150, "y": 240 },
      "size": { "width": 160, "height": 157 },
      "outerBBox": { "x": 150, "y": 240, "width": 160, "height": 157 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "clear", "dataType": "uint16", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 302, "y": 295 },
          "connections": [{ "wireId": "w3", "targetDevice": "apds9960Log_1", "targetPort": "clear" }]
        },
        {
          "port": "red", "dataType": "uint16", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 302, "y": 315 },
          "connections": [{ "wireId": "w4", "targetDevice": "apds9960Log_1", "targetPort": "red" }]
        },
        {
          "port": "green", "dataType": "uint16", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 302, "y": 335 },
          "connections": [{ "wireId": "w5", "targetDevice": "apds9960Log_1", "targetPort": "green" }]
        },
        {
          "port": "blue", "dataType": "uint16", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 302, "y": 355 },
          "connections": [{ "wireId": "w6", "targetDevice": "apds9960Log_1", "targetPort": "blue" }]
        }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    },
    {
      "id": "apds9960Log_1", "type": "BlackBoxLog:APDS9960", "kind": "simple",
      "label": "apds9960Log_1",
      "properties": { "executionOrder": 20, "instanceId": "apds9960_0" },
      "position": { "x": 720, "y": 240 },
      "size": { "width": 160, "height": 157 },
      "outerBBox": { "x": 720, "y": 240, "width": 160, "height": 157 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "clear", "dataType": "uint16", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 728, "y": 295 },
          "connections": [{ "wireId": "w3", "targetDevice": "apds9960Run_1", "targetPort": "clear" }]
        },
        {
          "port": "red", "dataType": "uint16", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 728, "y": 315 },
          "connections": [{ "wireId": "w4", "targetDevice": "apds9960Run_1", "targetPort": "red" }]
        },
        {
          "port": "green", "dataType": "uint16", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 728, "y": 335 },
          "connections": [{ "wireId": "w5", "targetDevice": "apds9960Run_1", "targetPort": "green" }]
        },
        {
          "port": "blue", "dataType": "uint16", "isOutput": false, "acceptNotConnected": false,
          "position": { "x": 728, "y": 355 },
          "connections": [{ "wireId": "w6", "targetDevice": "apds9960Run_1", "targetPort": "blue" }]
        }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "bool_0", "type": "StatementBool", "kind": "simple",
      "properties": { "value": false },
      "position": { "x": 250, "y": 430 },
      "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 250, "y": 430, "width": 120, "height": 74 },
      "innerBBox": null,
      "connectors": [
        {
          "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 362, "y": 458 },
          "connections": [{ "wireId": "w_stop", "targetDevice": "stmLoop_1", "targetPort": "stop" }]
        }
      ],
      "containment": { "isContainer": false, "parent": "stmLoop_1", "status": "contained" }
    }
  ],
  "wires": [
    { "id": "w3", "from": { "device": "apds9960Run_1", "port": "clear" }, "to": { "device": "apds9960Log_1", "port": "clear" }, "dataType": "uint16" },
    { "id": "w4", "from": { "device": "apds9960Run_1", "port": "red" }, "to": { "device": "apds9960Log_1", "port": "red" }, "dataType": "uint16" },
    { "id": "w5", "from": { "device": "apds9960Run_1", "port": "green" }, "to": { "device": "apds9960Log_1", "port": "green" }, "dataType": "uint16" },
    { "id": "w6", "from": { "device": "apds9960Run_1", "port": "blue" }, "to": { "device": "apds9960Log_1", "port": "blue" }, "dataType": "uint16" },
    { "id": "w_stop", "from": { "device": "bool_0", "port": "output" }, "to": { "device": "stmLoop_1", "port": "stop" }, "dataType": "bool" }
  ]
}`

// TestMultiPortScopeCrossEmitsPerPortVars asserts that when a BlackBox
// device with multiple output ports is consumed across a loop scope
// boundary, Generate emits correct Go using per-port promotion:
//
//   - One `var {instance}_{port} T` declaration before the loop, for
//     each CONNECTED output port.
//   - Inside the loop the producer call uses `=` (not `:=`) since the
//     vars already exist.
//   - The consumer after the loop references those same vars.
//
// This locks in the Layer-2 feature (per-port scope promotion) and
// replaces the earlier Layer-1 test that asserted a blocking error —
// the old behavior was "refuse" and the new behavior is "emit".
//
// Português: Valida a promoção por porta (Camada 2) — var antes do
// loop, `=` dentro, consumidor fora lê as mesmas vars.
func TestMultiPortScopeCrossEmitsPerPortVars(t *testing.T) {
	def, err := blackbox.Parse([]byte(apdsMultiPortSource), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse APDS9960 source: %v", err)
	}

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneMultiPortScopeCross),
		Language: "go",
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
			"APDS9960": def,
		},
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("expected no errors, got: %v", resp.Errors)
	}
	if resp.Code == "" {
		t.Fatalf("expected Go code, got empty")
	}

	t.Log("=== IR ===")
	t.Log(resp.IR)
	t.Log("=== Go ===")
	t.Log(resp.Code)

	code := resp.Code

	// ── Four var declarations before the loop, one per connected port.
	// The instance id in the scene is "apds9960_0", which goIdent
	// collapses to "apds99600". Tolerate either form defensively.
	for _, port := range []string{"clear", "red", "green", "blue"} {
		patternA := "var apds99600_" + port + " uint16"
		patternB := "var apds9960_0_" + port + " uint16"
		if !strings.Contains(code, patternA) && !strings.Contains(code, patternB) {
			t.Errorf("expected `var ..._%s uint16` before the loop, not found\n  Code:\n%s",
				port, code)
		}
	}

	// ── Inside the loop, the producer call uses `=` not `:=`.
	// Example: "apds99600_clear, apds99600_red, apds99600_green, apds99600_blue = apds99600.Run()"
	// We locate the `for {` and look for a `=` assignment before the
	// closing brace that matches this shape.
	forIdx := strings.Index(code, "for {")
	if forIdx < 0 {
		t.Fatalf("expected a for-loop in generated code\n%s", code)
	}
	loopBody := code[forIdx:]
	// The call must appear before any bare `:= apds99600.Run()`. We
	// assert positively that the `=` form exists and negatively that
	// the `:=` form does NOT — either would compile individually, but
	// `:=` would clash with the hoisted vars.
	if !strings.Contains(loopBody, "= apds99600.Run()") {
		t.Errorf("expected `= apds99600.Run()` inside the loop\n%s", code)
	}
	if strings.Contains(loopBody, ":= apds99600.Run()") {
		t.Errorf("unexpected `:= apds99600.Run()` — vars were hoisted, must use `=`\n%s", code)
	}

	// ── After the loop, the Log call references the hoisted vars.
	// Find the position of `LOOP_END`/closing brace; easier to assert
	// that the Log call appears AFTER the first `for {` and uses the
	// same names as the hoisted vars.
	//
	// Since T6 ("cast escalar"), every argument at a BB call site is
	// wrapped in a conversion to the authored parameter's Go type —
	// here `uint16(apds99600_clear)`. The cast is idempotent (the
	// source already IS uint16; identity conversion is legal Go), and
	// applying it unconditionally is what frees the emitter from
	// tracking source register types. The hoisted-name pairing this
	// test pins is unchanged.
	if !strings.Contains(code, ".Log(uint16(apds99600_clear)") && !strings.Contains(code, ".Log(uint16(apds9960_0_clear)") {
		t.Errorf("expected Log call with cast hoisted var names after the loop\n%s", code)
	}
}
