// /server/codegen/codeGen_c99_outparam_test.go
package codegen

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// sensorOutSrcC99 mirrors sht3x_read: the reader takes the handle and writes two
// results through out-params (float *temperature, float *humidity, marked
// direction:out). It also exercises the pass-through (dev_out) and consume.
const sensorOutSrcC99 = `
typedef struct sensor sensor_t;
struct sensor { int fd; };

// label:Create.
// return:Handle.
sensor_t *sensor_create(
    // label:Addr.
    // connection:mandatory.
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
    // connection:mandatory.
    float *temperature,
    // label:Hum.
    // direction:out.
    // connection:mandatory.
    float *humidity
) { return 0; }

// label:Destroy.
// handle:consume.
void sensor_destroy(
    // label:Dev.
    // connection:mandatory.
    sensor_t *dev
) { }
`

func sensorOutDefs(t *testing.T) map[string]*blackbox.BlackBoxDef {
	t.Helper()
	def, err := blackbox.ParseC([]byte(sensorOutSrcC99), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	def.RawSource = sensorOutSrcC99
	return map[string]*blackbox.BlackBoxDef{
		"sensor_create":  def,
		"sensor_read":    def,
		"sensor_destroy": def,
	}
}

// sceneC99OutParam wires create → read → destroy. The out-params on read
// (temperature, humidity) are not wired downstream — they are still emitted
// because the function writes to them; the IR sources them from the def.
const sceneC99OutParam = `{
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

// TestEmitC_C99OutParams verifies that out-params are passed by address in
// parameter order — sensor_read(handle, &temperature, &humidity) — with the
// out-param variables declared (dereferenced value type), while pass-through
// and consume still work on the same chain.
func TestEmitC_C99OutParams(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99OutParam),
		Language:     "c",
		BlackBoxDefs: sensorOutDefs(t),
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

	// The read call passes the handle, then two out-params by address.
	readCall := regexp.MustCompile(`sensor_read\([^;]*\)`).FindString(body)
	if readCall == "" {
		t.Fatalf("no sensor_read(...) call found, got:\n%s", mainC)
	}
	if !strings.HasPrefix(readCall, "sensor_read("+handle) {
		t.Fatalf("read call must take the handle first: %q\n%s", readCall, mainC)
	}
	if strings.Count(readCall, "&") != 2 {
		t.Fatalf("read call must pass exactly two out-params by address: %q\n%s", readCall, mainC)
	}

	// The two out-param value vars are declared as float (deref of float*).
	if strings.Count(body, "float ") < 2 {
		t.Fatalf("expected two 'float' out-param declarations, got:\n%s", mainC)
	}

	// Pass-through + consume still hold: destroy receives the original handle.
	if !strings.Contains(body, "sensor_destroy("+handle+")") {
		t.Fatalf("expected sensor_destroy(%s) via pass-through, got:\n%s", handle, mainC)
	}
}
