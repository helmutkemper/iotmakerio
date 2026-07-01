// /server/codegen/blackbox/parser_c_passthrough_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// shtC is the CORRECTED sht3x.c fixture (LabVIEW resource-chain v2):
//   - the `sht3x_t *dev` handle carries NO `direction:out.` (it is a
//     wire-type input; its pass-through is synthesized, not declared);
//   - `temperature` and `humidity` are real results → `direction:out.`;
//   - `sht3x_destroy` is the destructor → `handle:consume.`.
//
// Kept in sync with /mnt/user-data/outputs/sht3x.c (the project fixture).
const shtC = `
#include "esp_err.h"
#include "driver/i2c.h"
#include <stdint.h>
#include <stddef.h>

// label:sht3x.
// icon:42-group.
typedef struct sht3x sht3x_t;

// label:Repeatability.
// icon:wave-square.
typedef enum {
    // label:Low.
    SHT3X_REPEATABILITY_LOW    = 0,
    // label:Medium.
    SHT3X_REPEATABILITY_MEDIUM = 1,
    // label:High.
    SHT3X_REPEATABILITY_HIGH   = 2,
} sht3x_repeatability_t;

typedef void (*sht3x_alert_cb_t)(float temperature_c, void *user_data);

typedef struct {
    i2c_port_t bus;
    uint8_t    addr;
    int        clock_stretch_ms;
} sht3x_bus_config_t;

struct sht3x {
    sht3x_bus_config_t    cfg;
    sht3x_repeatability_t rep;
    sht3x_alert_cb_t      alert_cb;
    void                 *alert_user_data;
};

// label:Create sensor.
// icon:plus.
// return:Sensor.
sht3x_t *sht3x_create(
    // doc:bus.
    // label:bus.
    // connection:mandatory.
    i2c_port_t bus,
    // doc:addr.
    // label:addr.
    // connection:mandatory.
    uint8_t addr
)
{
    sht3x_t *dev = (sht3x_t *) calloc(1, sizeof(sht3x_t));
    if (!dev) return NULL;
    return dev;
}

// label:Read.
// icon:temperature-half.
// return:Status.
esp_err_t sht3x_read(
    // doc:dev.
    // label:dev.
    // connection:mandatory.
    sht3x_t *dev,
    // doc:Temperature in degrees Celsius.
    // label:temperature.
    // direction:out.
    // connection:mandatory.
    float *temperature,
    // doc:Relative humidity, 0-100 %.
    // label:Humidity.
    // direction:out.
    // connection:mandatory.
    float *humidity
)
{
    return ESP_OK;
}

// label:Set repeatability.
// icon:sliders.
// return:Error.
esp_err_t sht3x_set_repeatability(
    // doc:dev.
    // label:dev.
    // connection:mandatory.
    sht3x_t *dev,
    // doc:Level.
    // label:level.
    // connection:mandatory.
    sht3x_repeatability_t level
)
{
    return ESP_OK;
}

// label:On alert.
// icon:bell.
// return:Error.
esp_err_t sht3x_set_alert(
    // doc:dev.
    // label:dev.
    // connection:mandatory.
    sht3x_t *dev,
    // doc:threshold.
    // label:threshold.
    // connection:mandatory.
    float threshold_c,
    // doc:Alert callback.
    // label:cb.
    // connection:mandatory.
    sht3x_alert_cb_t cb,
    // doc:user_data.
    // label:user_data.
    // connection:mandatory.
    void *user_data
)
{
    return ESP_OK;
}

// label:Destroy.
// icon:trash.
// handle:consume.
void sht3x_destroy(
    // doc:dev.
    // label:dev.
    // connection:mandatory.
    sht3x_t *dev
)
{
}

// label:Version.
// icon:tag.
// return:Version.
const char *sht3x_version(void)
{
    return "1.0.0";
}
`

// findFunc returns the NamedFuncDef with the given name, or fails.
func findFunc(t *testing.T, def *BlackBoxDef, name string) NamedFuncDef {
	t.Helper()
	for _, fn := range def.Functions {
		if fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %q not found in def.Functions (have %d funcs)", name, len(def.Functions))
	return NamedFuncDef{}
}

// portByName returns the first port with the given name from a slice.
func portByName(ports []PortDef, name string) (PortDef, bool) {
	for _, p := range ports {
		if p.Name == name {
			return p, true
		}
	}
	return PortDef{}, false
}

// TestParseC_PassthroughSynthesis is the Fatia-1 acceptance gate. It runs the
// corrected sht3x.c through ParseC and asserts that the wire-type, the
// handle:consume flag, and the synthesized pass-through outputs match the
// shape documented in docs/c99_ide_integration.md §4.
func TestParseC_PassthroughSynthesis(t *testing.T) {
	def, err := ParseC([]byte(shtC), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC error: %v", err)
	}

	// One wire-type: the opaque handle sht3x (alias sht3x_t).
	if len(def.WireTypes) != 1 {
		t.Fatalf("want 1 wire-type, got %d", len(def.WireTypes))
	}
	if w := def.WireTypes[0]; w.Name != "sht3x" || w.Alias != "sht3x_t" {
		t.Fatalf("wire-type = {Name:%q Alias:%q}, want {sht3x, sht3x_t}", w.Name, w.Alias)
	}

	// Six device-functions, no methods/init/struct (decision b).
	if len(def.Functions) != 6 {
		t.Fatalf("want 6 functions, got %d", len(def.Functions))
	}
	if def.Name != "" || def.Init != nil || len(def.Methods) != 0 {
		t.Fatalf("C99 model expects no struct/init/methods; got Name=%q init=%v methods=%d",
			def.Name, def.Init != nil, len(def.Methods))
	}

	// ── handle:consume only on the destructor ────────────────────────────
	for _, fn := range def.Functions {
		want := fn.Name == "sht3x_destroy"
		if fn.ConsumesHandle != want {
			t.Errorf("%s.ConsumesHandle = %v, want %v", fn.Name, fn.ConsumesHandle, want)
		}
	}

	// ── sht3x_create: handle is the RETURN output; no pass-through ────────
	create := findFunc(t, def, "sht3x_create")
	if len(create.Inputs) != 2 { // bus, addr
		t.Errorf("create inputs = %d, want 2", len(create.Inputs))
	}
	createOut := def.FunctionSynthesizedOutputs(create.FuncDef)
	if ret, ok := portByName(createOut, "return"); !ok || ret.GoType != "sht3x_t *" {
		t.Errorf("create return = %+v (ok=%v), want sht3x_t *", ret, ok)
	}
	for _, p := range createOut {
		if p.PassThrough {
			t.Errorf("create must have no pass-through (handle is the return), got %q", p.Name)
		}
	}

	// ── sht3x_read: dev is an INPUT; temp/humidity are outputs; dev_out
	//    is synthesized as a pass-through ──────────────────────────────────
	read := findFunc(t, def, "sht3x_read")
	if _, ok := portByName(read.Inputs, "dev"); !ok {
		t.Errorf("read must take dev as an INPUT; inputs=%v", read.Inputs)
	}
	if p, ok := portByName(read.Inputs, "dev"); ok && p.GoType != "sht3x_t *" {
		t.Errorf("read.dev type = %q, want sht3x_t *", p.GoType)
	}
	// temperature & humidity were marked direction:out → parsed outputs.
	for _, name := range []string{"temperature", "humidity"} {
		if _, ok := portByName(read.Outputs, name); !ok {
			t.Errorf("read.%s should be a parsed OUTPUT (direction:out.); outputs=%v", name, read.Outputs)
		}
		if _, ok := portByName(read.Inputs, name); ok {
			t.Errorf("read.%s should NOT be an input", name)
		}
	}
	readOut := def.FunctionSynthesizedOutputs(read.FuncDef)
	devOut, ok := portByName(readOut, "dev_out")
	if !ok {
		t.Fatalf("read should synthesize a dev_out pass-through; synthesized=%v", readOut)
	}
	if !devOut.PassThrough {
		t.Errorf("read.dev_out.PassThrough = false, want true")
	}
	if devOut.GoType != "sht3x_t *" {
		t.Errorf("read.dev_out type = %q, want sht3x_t *", devOut.GoType)
	}
	if devOut.Connection != "optional" || devOut.MissingConn {
		t.Errorf("read.dev_out should be optional/non-missing, got conn=%q missing=%v",
			devOut.Connection, devOut.MissingConn)
	}
	// The synthesized list must still carry the real outputs (return + results).
	for _, name := range []string{"return", "temperature", "humidity"} {
		if _, ok := portByName(readOut, name); !ok {
			t.Errorf("read synthesized outputs missing real output %q; got %v", name, readOut)
		}
	}

	// ── set_repeatability / set_alert: dev_out synthesized ────────────────
	for _, name := range []string{"sht3x_set_repeatability", "sht3x_set_alert"} {
		fn := findFunc(t, def, name)
		if _, ok := portByName(fn.Inputs, "dev"); !ok {
			t.Errorf("%s must take dev as an input", name)
		}
		out := def.FunctionSynthesizedOutputs(fn.FuncDef)
		if dp, ok := portByName(out, "dev_out"); !ok || !dp.PassThrough {
			t.Errorf("%s should synthesize a dev_out pass-through; got %v", name, out)
		}
	}

	// ── sht3x_destroy: consumes the handle → NO pass-through ──────────────
	destroy := findFunc(t, def, "sht3x_destroy")
	if _, ok := portByName(destroy.Inputs, "dev"); !ok {
		t.Errorf("destroy must take dev as an INPUT; inputs=%v", destroy.Inputs)
	}
	destroyOut := def.FunctionSynthesizedOutputs(destroy.FuncDef)
	for _, p := range destroyOut {
		if p.PassThrough {
			t.Errorf("destroy must NOT synthesize a pass-through (handle:consume.), got %q", p.Name)
		}
	}
	if len(destroyOut) != 0 { // void return, no results
		t.Errorf("destroy should have no outputs, got %v", destroyOut)
	}

	// ── sht3x_version: standalone, no handle, no pass-through ─────────────
	version := findFunc(t, def, "sht3x_version")
	if len(version.Inputs) != 0 {
		t.Errorf("version inputs = %d, want 0", len(version.Inputs))
	}
	versionOut := def.FunctionSynthesizedOutputs(version.FuncDef)
	if ret, ok := portByName(versionOut, "return"); !ok || ret.GoType != "const char *" {
		t.Errorf("version return = %+v (ok=%v), want const char *", ret, ok)
	}
	for _, p := range versionOut {
		if p.PassThrough {
			t.Errorf("version must have no pass-through, got %q", p.Name)
		}
	}
}
