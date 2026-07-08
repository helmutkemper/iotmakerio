// /server/codegen/codeGen_c99_instanceid_test.go
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

// sensorOptSrcC99 has a constructor with OPTIONAL, defaultless inputs (bus,
// addr) and an out-param reader — the shape that produced two bugs from a real
// scene: an empty `create()` call and a handle variable whose name diverged
// between producer and consumer.
const sensorOptSrcC99 = `
typedef struct sensor sensor_t;
struct sensor { int fd; };

// label:Create.
// return:Handle.
sensor_t *sensor_create(
    // label:Bus.
    // connection:optional.
    int bus,
    // label:Addr.
    // connection:optional.
    int addr
) { sensor_t *d = 0; return d; }

// label:Read.
// return:Status.
int sensor_read(
    // label:Dev.
    // connection:mandatory.
    sensor_t *dev,
    // label:Temp.
    // direction:out.
    // connection:optional.
    float *temperature
) { return 0; }

// label:Destroy.
// handle:consume.
void sensor_destroy(
    // label:Dev.
    // connection:mandatory.
    sensor_t *dev
) { }
`

func sensorOptDefs(t *testing.T) map[string]*blackbox.BlackBoxDef {
	t.Helper()
	def, err := blackbox.ParseC([]byte(sensorOptSrcC99), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	def.Files = []blackbox.FileEntry{{Path: "dev.c", Content: sensorOptSrcC99}}
	return map[string]*blackbox.BlackBoxDef{
		"sensor_create":  def,
		"sensor_read":    def,
		"sensor_destroy": def,
	}
}

// sceneC99InstanceId gives sensor_create_1 an instanceId property that DIFFERS
// from its node id (as the IDE does), and leaves the optional bus/addr unwired.
const sceneC99InstanceId = `{
  "version": "1.0",
  "devices": [
    { "id": "sensor_create_1", "type": "BlackBoxsensor_create:", "properties": { "instanceId": "c99fn0" },
      "connectors": [
        { "port": "bus",  "dataType": "int", "isOutput": false, "connections": [] },
        { "port": "addr", "dataType": "int", "isOutput": false, "connections": [] },
        { "port": "return", "dataType": "sensor_t *", "isOutput": true,
          "connections": [ { "wireId": "wc", "targetDevice": "sensor_read_1", "targetPort": "dev" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },

    { "id": "sensor_read_1", "type": "BlackBoxsensor_read:", "properties": { "instanceId": "c99fn1" },
      "connectors": [
        { "port": "dev", "dataType": "sensor_t *", "isOutput": false,
          "connections": [ { "wireId": "wc", "targetDevice": "sensor_create_1", "targetPort": "return" } ] },
        { "port": "dev_out", "dataType": "sensor_t *", "isOutput": true,
          "connections": [ { "wireId": "wp", "targetDevice": "sensor_destroy_1", "targetPort": "dev" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } },

    { "id": "sensor_destroy_1", "type": "BlackBoxsensor_destroy:", "properties": { "instanceId": "c99fn2" },
      "connectors": [
        { "port": "dev", "dataType": "sensor_t *", "isOutput": false,
          "connections": [ { "wireId": "wp", "targetDevice": "sensor_read_1", "targetPort": "dev_out" } ] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" } }
  ],
  "wires": [
    { "id": "wc", "from": { "device": "sensor_create_1", "port": "return" }, "to": { "device": "sensor_read_1", "port": "dev" }, "dataType": "sensor_t *" },
    { "id": "wp", "from": { "device": "sensor_read_1", "port": "dev_out" }, "to": { "device": "sensor_destroy_1", "port": "dev" }, "dataType": "sensor_t *" }
  ]
}`

// TestEmitC_C99_InstanceIdAndOptionalInputs locks in two fixes:
//   - the handle variable is keyed by node.ID, so producer and consumers agree
//     even when the IDE assigns a different instanceId (the var must not carry
//     the "c99fn*" instance ids);
//   - optional unwired inputs (bus, addr) get default literals, so the call is
//     well-formed rather than an empty sensor_create().
func TestEmitC_C99_InstanceIdAndOptionalInputs(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99InstanceId),
		Language:     "c",
		BlackBoxDefs: sensorOptDefs(t),
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

	cap := regexp.MustCompile(`(\w+)\s*=\s*sensor_create\(`).FindStringSubmatch(body)
	if cap == nil {
		t.Fatalf("expected the handle to be captured, got:\n%s", mainC)
	}
	handle := cap[1]

	// Bug 1: producer and consumers must reference the SAME variable, and it
	// must be derived from node.ID — never the instanceId.
	if strings.Contains(handle, "c99fn") {
		t.Fatalf("handle var %q must not be keyed by instanceId, got:\n%s", handle, mainC)
	}
	if !strings.Contains(body, "sensor_read("+handle) {
		t.Fatalf("read must read the producer's handle %q, got:\n%s", handle, mainC)
	}
	if !strings.Contains(body, "sensor_destroy("+handle+")") {
		t.Fatalf("destroy must read the producer's handle %q, got:\n%s", handle, mainC)
	}

	// Bug 2: the constructor call must carry arguments (defaults for the
	// optional unwired inputs), not be an empty sensor_create().
	createCall := regexp.MustCompile(`sensor_create\([^)]*\)`).FindString(body)
	if createCall == "sensor_create()" {
		t.Fatalf("constructor call must pass defaults for unwired optional inputs, got %q\n%s", createCall, mainC)
	}
}
