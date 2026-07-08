// /server/codegen/codeGen_c99_passthrough_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// sensorSrcC99 is a minimal C99 resource device: a create/read/destroy trio
// over a wire-type handle (sensor_t*). sensor_read republishes the handle via
// the synthesized "dev_out" pass-through; sensor_destroy consumes it
// (handle:consume → no pass-through). This is the LabVIEW refnum chain.
const sensorSrcC99 = `
// label:Sensor.
typedef struct sensor sensor_t;

struct sensor {
    int fd;
    int addr;
};

// label:Create.
// return:Handle.
sensor_t *sensor_create(
    // doc:I2C address.
    // label:Addr.
    // connection:mandatory.
    int addr
)
{
    sensor_t *dev = 0;
    return dev;
}

// label:Read.
// return:Value.
int sensor_read(
    // doc:Sensor handle.
    // label:Dev.
    // connection:mandatory.
    sensor_t *dev
)
{
    return 0;
}

// label:Destroy.
// handle:consume.
void sensor_destroy(
    // doc:Sensor handle.
    // label:Dev.
    // connection:mandatory.
    sensor_t *dev
)
{
}
`

// sensorChainDefs parses sensorSrcC99 and keys the resulting def by every
// function name the scene references — exactly what the loader does — and
// carries the verbatim source for inlining.
func sensorChainDefs(t *testing.T) map[string]*blackbox.BlackBoxDef {
	t.Helper()
	def, err := blackbox.ParseC([]byte(sensorSrcC99), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	def.Files = []blackbox.FileEntry{{Path: "dev.c", Content: sensorSrcC99}}
	return map[string]*blackbox.BlackBoxDef{
		"sensor_create":  def,
		"sensor_read":    def,
		"sensor_destroy": def,
	}
}

// sceneC99SensorChain wires the resource chain:
//
//	constInt_addr ─ sensor_create ─return─ sensor_read ─dev_out─ sensor_destroy
//
// The handle is created once, read once, then destroyed. The destroy block is
// fed by sensor_read's pass-through ("dev_out"), NOT by sensor_create directly
// — so a correct build must resolve dev_out back to the original handle.
const sceneC99SensorChain = `{
  "version": "1.0",
  "devices": [
    { "id": "constInt_addr", "type": "StatementConstInt", "properties": { "value": 68 },
      "connectors": [ { "port": "output", "dataType": "int", "isOutput": true,
        "connections": [ { "wireId": "wa", "targetDevice": "sensor_create_1", "targetPort": "addr" } ] } ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },

    { "id": "sensor_create_1", "type": "BlackBoxsensor_create:", "properties": {},
      "connectors": [
        { "port": "addr", "dataType": "int", "isOutput": false,
          "connections": [ { "wireId": "wa", "targetDevice": "constInt_addr", "targetPort": "output" } ] },
        { "port": "return", "dataType": "sensor_t *", "isOutput": true,
          "connections": [ { "wireId": "wc", "targetDevice": "sensor_read_1", "targetPort": "dev" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },

    { "id": "sensor_read_1", "type": "BlackBoxsensor_read:", "properties": {},
      "connectors": [
        { "port": "dev", "dataType": "sensor_t *", "isOutput": false,
          "connections": [ { "wireId": "wc", "targetDevice": "sensor_create_1", "targetPort": "return" } ] },
        { "port": "return", "dataType": "int", "isOutput": true, "connections": [] },
        { "port": "dev_out", "dataType": "sensor_t *", "isOutput": true,
          "connections": [ { "wireId": "wp", "targetDevice": "sensor_destroy_1", "targetPort": "dev" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },

    { "id": "sensor_destroy_1", "type": "BlackBoxsensor_destroy:", "properties": {},
      "connectors": [
        { "port": "dev", "dataType": "sensor_t *", "isOutput": false,
          "connections": [ { "wireId": "wp", "targetDevice": "sensor_read_1", "targetPort": "dev_out" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } }
  ],
  "wires": [
    { "id": "wa", "from": { "device": "constInt_addr", "port": "output" }, "to": { "device": "sensor_create_1", "port": "addr" }, "dataType": "int" },
    { "id": "wc", "from": { "device": "sensor_create_1", "port": "return" }, "to": { "device": "sensor_read_1", "port": "dev" }, "dataType": "sensor_t *" },
    { "id": "wp", "from": { "device": "sensor_read_1", "port": "dev_out" }, "to": { "device": "sensor_destroy_1", "port": "dev" }, "dataType": "sensor_t *" }
  ]
}`

// TestEmitC_C99SensorChain_PassThrough verifies the resource chain: the handle
// is captured once at create, read uses it, and destroy receives THE SAME
// handle variable — even though destroy is wired to sensor_read's pass-through
// output, not to create directly. Without pass-through resolution destroy would
// reference an undeclared "dev_out" register and the C would not compile.
func TestEmitC_C99SensorChain_PassThrough(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99SensorChain),
		Language:     "c",
		BlackBoxDefs: sensorChainDefs(t),
	})

	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; diagnostics=%+v", resp.Diagnostics)
	}

	idx := strings.Index(mainC, "int main(void)")
	if idx < 0 {
		t.Fatalf("main.c has no main(); got:\n%s", mainC)
	}
	body := mainC[idx:]

	// create captures the handle into a variable.
	cap := regexp.MustCompile(`(\w+)\s*=\s*sensor_create\(`).FindStringSubmatch(body)
	if cap == nil {
		t.Fatalf("expected the handle to be captured ('<type> <var> = sensor_create(...)'), got:\n%s", mainC)
	}
	handle := cap[1]

	// read uses the captured handle.
	if !strings.Contains(body, "sensor_read("+handle+")") {
		t.Fatalf("expected sensor_read(%s), got:\n%s", handle, mainC)
	}

	// destroy receives the SAME handle (pass-through resolved back to create),
	// NOT a separate/undeclared dev_out variable.
	if !strings.Contains(body, "sensor_destroy("+handle+")") {
		t.Fatalf("expected sensor_destroy(%s) via pass-through resolution, got:\n%s", handle, mainC)
	}
	if strings.Contains(body, "dev_out") {
		t.Fatalf("pass-through 'dev_out' must not appear as a variable in main(), got:\n%s", mainC)
	}
}
