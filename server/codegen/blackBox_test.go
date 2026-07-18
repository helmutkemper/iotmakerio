// server/codegen/blackBox_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// blackBox_test.go — Integration tests for the black-box parser and code generator.
//
// English:
//
//	Tests the full pipeline from Go source → BlackBoxDef (via blackbox.Parse)
//	through to code generation (via codegen.Generate).
//
//	The test source fixtures use simple, deterministic structs so the generated
//	output can be checked with exact string assertions.
//
// New fields tested here:
//
//	StructIcon, StructLabel — declared in the struct doc comment.
//	Method.Icon, Method.Label — declared in the method doc comment.
//	Both are verified to be extracted correctly and to NOT appear in the
//	human-readable Doc field (they are machine directives, not prose).

import (
	"context"
	"encoding/json"
	"fmt"
	"server/codegen/blackbox"
	"testing"
)

// =====================================================================
//  Source fixtures
// =====================================================================

const apds9960Source = `package bb

import "machine"

// APDS9960 is a color/proximity sensor connected via I2C.
//
// icon:greater-than-equal. label:APDS9960.
type APDS9960 struct {
	I2C   *machine.I2C
	Gain  byte   ` + "`" + `prop:"Gain"             default:"0"   options:"0,1,2,3"` + "`" + `
	ATime byte   ` + "`" + `prop:"Integration Time" default:"255"` + "`" + `
}

// Init configures the sensor on the I2C bus.
//
// icon:gear. label:setup.
//
// Params
//   i2c: I2C bus reference.  connection:mandatory.
//
// Returns
//   err: init error.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) {
	s.I2C = i2c
	s.I2C.WriteRegister(0x39, 0x80, []byte{0x01})
	s.I2C.WriteRegister(0x39, 0x81, []byte{s.ATime})
	s.I2C.WriteRegister(0x39, 0x8F, []byte{s.Gain})
	s.I2C.WriteRegister(0x39, 0x80, []byte{0x03})
	return nil
}

// Run reads the four RGBC colour channels.
//
// executionOrder:20. icon:greater-than-equal. label:read colours.
//
// Returns
//   clear: total light.  range:0..65535.  connection:optional.
//   red:   red channel.  range:0..65535.  connection:optional.
//   green: green channel.  range:0..65535.  connection:optional.
//   blue:  blue channel.  range:0..65535.  connection:optional.
func (s *APDS9960) Run() (clear, red, green, blue uint16) {
	data := make([]byte, 8)
	s.I2C.ReadRegister(0x39, 0x94, data)
	clear = uint16(data[0]) | uint16(data[1])<<8
	red   = uint16(data[2]) | uint16(data[3])<<8
	green = uint16(data[4]) | uint16(data[5])<<8
	blue  = uint16(data[6]) | uint16(data[7])<<8
	return
}
`

const i2cBusSource = `package bb

import (
	"machine"
	"time"
)

// I2CBus initialises a TinyGo I2C bus.
//
// icon:gear. label:I2C bus.
type I2CBus struct {
	Bus  *machine.I2C
	Sda  string ` + "`" + `prop:"SDA Pin"   default:"GP4" options:"GP0,GP1,GP2,GP3,GP4,GP5"` + "`" + `
	Scl  string ` + "`" + `prop:"SCL Pin"   default:"GP5" options:"GP0,GP1,GP2,GP3,GP4,GP5"` + "`" + `
	Freq string ` + "`" + `prop:"Frequency" default:"400000" options:"100000,400000,1000000"` + "`" + `
}

// Init configures and returns the I2C bus.
//
// icon:gear. label:init bus.
//
// Returns
//   bus: configured I2C bus.  connection:mandatory.
//   err: configuration error.  connection:optional.
func (s *I2CBus) Init() (bus *machine.I2C, err error) {
	s.Bus = machine.I2C0
	s.Bus.Configure(machine.I2CConfig{
		Frequency: 400000,
		SDA:       machine.GP4,
		SCL:       machine.GP5,
	})
	time.Sleep(100 * time.Millisecond)
	return s.Bus, nil
}
`

// =====================================================================
//  Parser tests
// =====================================================================

func TestParseAPDS9960(t *testing.T) {
	def, err := blackbox.Parse([]byte(apds9960Source), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// ── Name ──────────────────────────────────────────────────────────────
	if def.Name != "APDS9960" {
		t.Errorf("Name: got %q, want %q", def.Name, "APDS9960")
	}

	// ── Struct-level visual directives ────────────────────────────────────
	if def.StructIcon != "greater-than-equal" {
		t.Errorf("StructIcon: got %q, want %q", def.StructIcon, "greater-than-equal")
	}
	if def.StructLabel != "APDS9960" {
		t.Errorf("StructLabel: got %q, want %q", def.StructLabel, "APDS9960")
	}

	// ── Imports ───────────────────────────────────────────────────────────
	if len(def.Imports) != 1 || def.Imports[0] != "machine" {
		t.Errorf("Imports: got %v, want [machine]", def.Imports)
	}

	// ── Props ─────────────────────────────────────────────────────────────
	if len(def.Props) != 2 {
		t.Fatalf("Props count: got %d, want 2", len(def.Props))
	}
	if def.Props[0].Label != "Gain" {
		t.Errorf("Props[0].Label: got %q, want %q", def.Props[0].Label, "Gain")
	}
	if len(def.Props[0].Options) != 4 {
		t.Errorf("Props[0].Options: got %v, want 4 items", def.Props[0].Options)
	}
	if def.Props[1].Default != "255" {
		t.Errorf("Props[1].Default: got %q, want %q", def.Props[1].Default, "255")
	}

	// ── Init ──────────────────────────────────────────────────────────────
	if def.Init == nil {
		t.Fatal("Init is nil")
	}
	if len(def.Init.Inputs) != 1 {
		t.Fatalf("Init.Inputs count: got %d, want 1", len(def.Init.Inputs))
	}
	if def.Init.Inputs[0].Name != "i2c" {
		t.Errorf("Init.Inputs[0].Name: got %q, want %q", def.Init.Inputs[0].Name, "i2c")
	}
	if def.Init.Inputs[0].GoType != "*machine.I2C" {
		t.Errorf("Init.Inputs[0].GoType: got %q, want %q", def.Init.Inputs[0].GoType, "*machine.I2C")
	}
	if len(def.Init.Outputs) != 1 || !def.Init.Outputs[0].IsError {
		t.Errorf("Init.Outputs: got %+v, want 1 error output", def.Init.Outputs)
	}

	// Init method-level visual directives
	if def.Init.Icon != "gear" {
		t.Errorf("Init.Icon: got %q, want %q", def.Init.Icon, "gear")
	}
	if def.Init.Label != "setup" {
		t.Errorf("Init.Label: got %q, want %q", def.Init.Label, "setup")
	}
	// Machine directive must NOT appear in the human-readable Doc
	if containsStr(def.Init.Doc, "icon:") || containsStr(def.Init.Doc, "label:") {
		t.Errorf("Init.Doc must not contain machine directives, got: %q", def.Init.Doc)
	}

	// ── Methods (Run is the first named method) ───────────────────────────
	if len(def.Methods) != 1 {
		t.Fatalf("Methods count: got %d, want 1", len(def.Methods))
	}
	runMethod := def.Methods[0]
	if runMethod.Name != "Run" {
		t.Errorf("Methods[0].Name: got %q, want %q", runMethod.Name, "Run")
	}
	if len(runMethod.Inputs) != 0 {
		t.Errorf("Run.Inputs: got %d, want 0", len(runMethod.Inputs))
	}
	if len(runMethod.Outputs) != 4 {
		t.Fatalf("Run.Outputs: got %d, want 4", len(runMethod.Outputs))
	}
	wantNames := []string{"clear", "red", "green", "blue"}
	for i, want := range wantNames {
		if runMethod.Outputs[i].Name != want {
			t.Errorf("Run.Outputs[%d].Name: got %q, want %q", i, runMethod.Outputs[i].Name, want)
		}
		if runMethod.Outputs[i].GoType != "uint16" {
			t.Errorf("Run.Outputs[%d].GoType: got %q, want %q", i, runMethod.Outputs[i].GoType, "uint16")
		}
	}

	// Run method-level visual directives
	if runMethod.Icon != "greater-than-equal" {
		t.Errorf("Run.Icon: got %q, want %q", runMethod.Icon, "greater-than-equal")
	}
	if runMethod.Label != "read colours" {
		t.Errorf("Run.Label: got %q, want %q", runMethod.Label, "read colours")
	}
	if runMethod.ExecutionOrder != 20 {
		t.Errorf("Run.ExecutionOrder: got %d, want 20", runMethod.ExecutionOrder)
	}
	// Machine directives must NOT appear in the human-readable Doc
	if containsStr(runMethod.Doc, "icon:") || containsStr(runMethod.Doc, "label:") || containsStr(runMethod.Doc, "executionOrder:") {
		t.Errorf("Run.Doc must not contain machine directives, got: %q", runMethod.Doc)
	}

	// ── StructCode / MethodsCode ──────────────────────────────────────────
	if def.StructCode == "" {
		t.Error("StructCode is empty")
	}
	if def.MethodsCode == "" {
		t.Error("MethodsCode is empty")
	}

	t.Logf("StructIcon: %s  StructLabel: %s", def.StructIcon, def.StructLabel)
	t.Logf("Init.Icon: %s  Init.Label: %s", def.Init.Icon, def.Init.Label)
	t.Logf("Run.Icon: %s  Run.Label: %s  Run.ExecutionOrder: %d", runMethod.Icon, runMethod.Label, runMethod.ExecutionOrder)
	t.Logf("StructCode:\n%s", def.StructCode)
	t.Logf("MethodsCode:\n%s", def.MethodsCode)
}

func TestParseI2CBus(t *testing.T) {
	def, err := blackbox.Parse([]byte(i2cBusSource), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if def.Name != "I2CBus" {
		t.Errorf("Name: got %q, want %q", def.Name, "I2CBus")
	}
	if len(def.Imports) != 2 {
		t.Errorf("Imports: got %v, want [machine, time]", def.Imports)
	}

	// Struct-level directives
	if def.StructIcon != "gear" {
		t.Errorf("StructIcon: got %q, want %q", def.StructIcon, "gear")
	}
	if def.StructLabel != "I2C bus" {
		t.Errorf("StructLabel: got %q, want %q", def.StructLabel, "I2C bus")
	}

	// Props: sda, scl, freq
	if len(def.Props) != 3 {
		t.Fatalf("Props count: got %d, want 3", len(def.Props))
	}
	if len(def.Props[0].Options) < 6 {
		t.Errorf("Props[0].Options: got %v, want at least 6 GPIO pins", def.Props[0].Options)
	}

	// Init: no inputs, 2 outputs (bus + error)
	if def.Init == nil {
		t.Fatal("Init is nil")
	}
	if len(def.Init.Inputs) != 0 {
		t.Errorf("Init.Inputs: got %d, want 0", len(def.Init.Inputs))
	}
	if len(def.Init.Outputs) != 2 {
		t.Fatalf("Init.Outputs: got %d, want 2", len(def.Init.Outputs))
	}
	if def.Init.Outputs[0].Name != "bus" {
		t.Errorf("Init.Outputs[0].Name: got %q, want %q", def.Init.Outputs[0].Name, "bus")
	}
	if !def.Init.Outputs[1].IsError {
		t.Errorf("Init.Outputs[1].IsError: got false, want true")
	}

	// No named methods (I2CBus only has Init)
	if len(def.Methods) != 0 {
		t.Errorf("Methods count: got %d, want 0 (I2CBus has no named methods)", len(def.Methods))
	}
}

// =====================================================================
//  Full pipeline test: I2C Bus → APDS-9960 → Gauge
// =====================================================================

func TestBlackBoxPipeline(t *testing.T) {
	// Parse both black-boxes
	i2cDef, err := blackbox.Parse([]byte(i2cBusSource), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("Parse I2CBus: %v", err)
	}
	apdsDef, err := blackbox.Parse([]byte(apds9960Source), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("Parse APDS9960: %v", err)
	}

	// Scene: I2CBus.Init → APDS9960.Init → Loop { APDS9960.Run → Gauge(clear) → EqualTo(10) → stop }
	sceneJSON := `{
		"version": "1.0",
		"metadata": {"density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": {"offsetX": 0, "offsetY": 0, "zoom": 1}},
		"devices": [
			{
				"id": "i2cBus_1_init", "type": "BlackBoxInit:I2CBus",
				"properties": {
					"instanceId": "i2cBus_1",
					"props": {"sda": "GP4", "scl": "GP5", "freq": "400000"}
				},
				"position": {"x": 100, "y": 100}, "size": {"width": 120, "height": 60},
				"outerBBox": {"x": 100, "y": 100, "width": 120, "height": 60}, "overlapPolicy": {},
				"connectors": [
					{"port": "bus", "dataType": "*machine.I2C", "isOutput": true, "connections": [
						{"wireId": "w1", "targetDevice": "apds9960_1_init", "targetPort": "i2c"}
					]},
					{"port": "err", "dataType": "error", "isOutput": true, "connections": []}
				],
				"containment": {"isContainer": false, "status": "free"}
			},
			{
				"id": "apds9960_1_init", "type": "BlackBoxInit:APDS9960",
				"properties": {
					"instanceId": "apds9960_1",
					"props": {"gain": "0", "atime": "255"}
				},
				"position": {"x": 300, "y": 100}, "size": {"width": 120, "height": 60},
				"outerBBox": {"x": 300, "y": 100, "width": 120, "height": 60}, "overlapPolicy": {},
				"connectors": [
					{"port": "i2c", "dataType": "*machine.I2C", "isOutput": false, "connections": [
						{"wireId": "w1", "targetDevice": "i2cBus_1_init", "targetPort": "bus"}
					]},
					{"port": "err", "dataType": "error", "isOutput": true, "connections": []}
				],
				"containment": {"isContainer": false, "status": "free"}
			},
			{
				"id": "stmLoop_1", "type": "StatementLoop",
				"position": {"x": 50, "y": 200}, "size": {"width": 600, "height": 300},
				"outerBBox": {"x": 50, "y": 200, "width": 600, "height": 300},
				"innerBBox": {"x": 55, "y": 205, "width": 590, "height": 290},
				"overlapPolicy": {"allowAbove": true},
				"connectors": [
					{"port": "stop", "dataType": "bool", "isOutput": false, "connections": [
						{"wireId": "w_stop", "targetDevice": "stmEqualTo_1", "targetPort": "output"}
					]}
				],
				"containment": {"isContainer": true, "children": ["apds9960_1_run", "constInt_1", "stmEqualTo_1", "gauge_1"], "status": "free"}
			},
			{
				"id": "apds9960_1_run", "type": "BlackBoxRun:APDS9960",
				"properties": {"instanceId": "apds9960_1"},
				"position": {"x": 100, "y": 250}, "size": {"width": 140, "height": 80},
				"outerBBox": {"x": 100, "y": 250, "width": 140, "height": 80}, "overlapPolicy": {},
				"connectors": [
					{"port": "clear", "dataType": "uint16", "isOutput": true, "connections": [
						{"wireId": "w2", "targetDevice": "gauge_1", "targetPort": "current"}
					]},
					{"port": "red",   "dataType": "uint16", "isOutput": true, "connections": []},
					{"port": "green", "dataType": "uint16", "isOutput": true, "connections": []},
					{"port": "blue",  "dataType": "uint16", "isOutput": true, "connections": []}
				],
				"containment": {"isContainer": false, "parent": "stmLoop_1", "status": "contained"}
			},
			{
				"id": "constInt_1", "type": "StatementConstInt",
				"properties": {"value": 10},
				"position": {"x": 100, "y": 350}, "size": {"width": 80, "height": 60},
				"outerBBox": {"x": 100, "y": 350, "width": 80, "height": 60}, "overlapPolicy": {},
				"connectors": [
					{"port": "output", "dataType": "int", "isOutput": true, "connections": [
						{"wireId": "w3", "targetDevice": "stmEqualTo_1", "targetPort": "inputY"}
					]}
				],
				"containment": {"isContainer": false, "parent": "stmLoop_1", "status": "contained"}
			},
			{
				"id": "stmEqualTo_1", "type": "StatementEqualTo",
				"position": {"x": 300, "y": 350}, "size": {"width": 80, "height": 60},
				"outerBBox": {"x": 300, "y": 350, "width": 80, "height": 60}, "overlapPolicy": {},
				"connectors": [
					{"port": "inputX", "dataType": "uint16", "isOutput": false, "connections": [
						{"wireId": "w4", "targetDevice": "apds9960_1_run", "targetPort": "clear"}
					]},
					{"port": "inputY", "dataType": "int", "isOutput": false, "connections": [
						{"wireId": "w3", "targetDevice": "constInt_1", "targetPort": "output"}
					]},
					{"port": "output", "dataType": "bool", "isOutput": true, "connections": [
						{"wireId": "w_stop", "targetDevice": "stmLoop_1", "targetPort": "stop"}
					]}
				],
				"containment": {"isContainer": false, "parent": "stmLoop_1", "status": "contained"}
			},
			{
				"id": "gauge_1", "type": "StatementGauge",
				"label": "clear",
				"position": {"x": 400, "y": 250}, "size": {"width": 100, "height": 60},
				"outerBBox": {"x": 400, "y": 250, "width": 100, "height": 60}, "overlapPolicy": {},
				"connectors": [
					{"port": "current", "dataType": "uint16", "isOutput": false, "connections": [
						{"wireId": "w2", "targetDevice": "apds9960_1_run", "targetPort": "clear"}
					]}
				],
				"containment": {"isContainer": false, "parent": "stmLoop_1", "status": "contained"}
			}
		],
		"wires": [
			{"id": "w1",     "from": {"device": "i2cBus_1_init",   "port": "bus"},    "to": {"device": "apds9960_1_init", "port": "i2c"},     "dataType": "*machine.I2C"},
			{"id": "w2",     "from": {"device": "apds9960_1_run",  "port": "clear"},  "to": {"device": "gauge_1",         "port": "current"}, "dataType": "uint16"},
			{"id": "w3",     "from": {"device": "constInt_1",      "port": "output"}, "to": {"device": "stmEqualTo_1",    "port": "inputY"},  "dataType": "int"},
			{"id": "w4",     "from": {"device": "apds9960_1_run",  "port": "clear"},  "to": {"device": "stmEqualTo_1",    "port": "inputX"},  "dataType": "uint16"},
			{"id": "w_stop", "from": {"device": "stmEqualTo_1",    "port": "output"}, "to": {"device": "stmLoop_1",       "port": "stop"},    "dataType": "bool"}
		]
	}`

	req := Request{
		Scene:    json.RawMessage(sceneJSON),
		Language: "go",
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
			"I2CBus":   i2cDef,
			"APDS9960": apdsDef,
		},
	}

	resp := Generate(context.Background(), req)

	if len(resp.Errors) > 0 {
		t.Fatalf("Errors: %v", resp.Errors)
	}
	if len(resp.Warnings) > 0 {
		t.Logf("Warnings: %v", resp.Warnings)
	}

	t.Logf("=== IR ===\n%s", resp.IR)
	t.Logf("=== Generated Go Code ===\n%s", resp.Files["main.go"])

	// Basic assertions on generated code
	code := resp.Files["main.go"]
	assertContainsB(t, code, "package main")
	assertContainsB(t, code, "var i2cBus1 I2CBus")
	assertContainsB(t, code, "var apds99601 APDS9960")
	assertContainsB(t, code, "i2cBus1.Init()")
	assertContainsB(t, code, "apds99601.Init(")
	assertContainsB(t, code, "apds99601.Run()")
	assertContainsB(t, code, "for {")
	assertContainsB(t, code, "break")
	assertContainsB(t, code, "fmt.Println")
}

// =====================================================================
//  Helpers
// =====================================================================

func assertContainsB(t *testing.T, code, substr string) {
	t.Helper()
	if !containsStr(code, substr) {
		t.Errorf("Generated code missing %q\n---code---\n%s", substr, code)
	}
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Quick JSON print for debug
func jsonPretty(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func init() {
	_ = fmt.Sprintf // suppress unused import
	_ = jsonPretty  // suppress unused
}
