// /server/codegen/codeGen_const_array_test.go

package codegen

// codeGen_const_array_test.go — End-to-end coverage for the three constant
// collection devices: StatementConstArrayInt / Float / String.
//
// These are the SCENE-level tests of the const-array plan: the per-op
// formatting is pinned in backend/ansic/emit_const_array_test.go and
// backend/golang/emit_const_array_test.go from a hand-built ir.Program;
// here a minimal scene (collection const wired into a Gauge so the value is
// actually used) goes through Generate end to end — scene JSON → graph → IR
// (ir.emitConstArray parses the Inspect text) → backend.
//
// What each test pins:
//
//   - Int:     "1, 2, 3" (comma-separated) → Go []int64 slice literal (the
//     abstract int widens, Task 3); C fixed array + `_len = 3`.
//   - Float:   precision is a per-device choice (the Inspect select) and is
//     honoured verbatim in both backends — float32 → C "float" + "f"
//     suffix; float64 → C "double", bare literals (mirrors the scalar
//     StatementConstFloat tests).
//   - String:  THE LINE-SPLIT RULE (plan decision C). The values text is a
//     TEXTAREA, one element per line — a comma is legitimate CONTENT
//     ("hello, world"), so the emitter splits string collections on
//     newlines. The test feeds two lines, the first containing a comma,
//     and asserts the result is exactly TWO elements with the comma
//     preserved inside the first.
//
// Português: cobertura ponta-a-ponta dos três devices de coleção constante.
// Uma cena mínima (coleção → Gauge) passa pelo Generate e os testes fixam:
// o alargamento do int abstrato (int64 no Go), a precisão do float honrada
// verbatim nos dois backends, e a regra de quebra POR LINHA do array de
// string — vírgula é conteúdo, não separador.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// sceneConstArray builds a scene with one constant-collection device wired
// into a Gauge. deviceType selects the sibling (StatementConstArrayInt /
// Float / String); elementType and values mirror what the WASM device
// exports in GetProperties (values is the RAW Inspect text — CSV for
// int/float, one element per line for string); wireType is the collection
// port/wire dataType ("[]int", "[]float32", ...). The Gauge only prints the
// wired value (emitGauge is type-agnostic), which keeps the collection
// "used" so its declaration is actually emitted rather than dropped.
func sceneConstArray(deviceType, elementType, values, wireType string) string {
	propsJSON, _ := json.Marshal(map[string]interface{}{
		"values":      values,
		"elementType": elementType,
	})
	return `{
  "version": "1.0",
  "metadata": { "density": 1, "canvasWidth": 1200, "canvasHeight": 800, "camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 } },
  "devices": [
    {
      "id": "` + idForDeviceType(deviceType) + `_1", "type": "` + deviceType + `",
      "properties": ` + string(propsJSON) + `,
      "position": { "x": 100, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 100, "y": 100, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "output", "dataType": "` + wireType + `", "isOutput": true, "acceptNotConnected": false, "position": { "x": 192, "y": 125 },
          "connections": [{ "wireId": "w1", "targetDevice": "gauge_1", "targetPort": "current" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "gauge_1", "type": "StatementGauge", "label": "view",
      "position": { "x": 300, "y": 100 }, "size": { "width": 100, "height": 50 },
      "outerBBox": { "x": 300, "y": 100, "width": 100, "height": 50 },
      "overlapPolicy": { "allowAbove": false, "allowBelow": true, "allowPartial": false },
      "connectors": [
        { "port": "current", "dataType": "` + wireType + `", "isOutput": false, "acceptNotConnected": true, "position": { "x": 308, "y": 125 },
          "connections": [{ "wireId": "w1", "targetDevice": "` + idForDeviceType(deviceType) + `_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "` + idForDeviceType(deviceType) + `_1", "port": "output" }, "to": { "device": "gauge_1", "port": "current" }, "dataType": "` + wireType + `" }
  ]
}`
}

// idForDeviceType mirrors the WASM id bases (rulesSequentialId) so the test
// scene looks exactly like a saved one: StatementConstArrayInt →
// "constArrayInt", ...String → "constArrayStr".
func idForDeviceType(deviceType string) string {
	switch deviceType {
	case "StatementConstArrayInt":
		return "constArrayInt"
	case "StatementConstArrayFloat":
		return "constArrayFloat"
	case "StatementConstArrayString":
		return "constArrayStr"
	}
	return "constArray"
}

// generateBoth runs the scene through both backends, failing the test on any
// backend error, and returns (cCode, goCode).
func generateBoth(t *testing.T, scene string) (string, string) {
	t.Helper()

	cResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "c",
	})
	if len(cResp.Errors) > 0 {
		t.Fatalf("C backend returned errors: %v", cResp.Errors)
	}
	mainC, ok := cResp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; got %d entries", len(cResp.Files))
	}

	goResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
	})
	if len(goResp.Errors) > 0 {
		t.Fatalf("Go backend returned errors: %v", goResp.Errors)
	}

	return mainC, goResp.Code
}

// TestConstArrayInt_EndToEnd: "1, 2, 3" → Go []int64 slice literal; C fixed
// array with the explicit `_len = 3` companion.
func TestConstArrayInt_EndToEnd(t *testing.T) {
	scene := sceneConstArray("StatementConstArrayInt", "int", "1, 2, 3", "[]int")
	mainC, goCode := generateBoth(t, scene)

	t.Logf("Generated C (int):\n%s", mainC)
	// Default profile is arduino_uno: abstract int → int32_t with the
	// profile's "L" literal suffix (pinned per-element in
	// ansic/ident_test.go; here we prove it through the full pipeline).
	assertContains(t, mainC, "int32_t constArrayInt1[] = {1L, 2L, 3L};")
	assertContains(t, mainC, "const size_t constArrayInt1_len = 3;")

	t.Logf("Generated Go (int):\n%s", goCode)
	assertContains(t, goCode, "[]int64{1, 2, 3}")
}

// TestConstArrayFloat_Float32_EndToEnd: precision float32 → Go []float32;
// C "float" with per-element "f" suffix.
func TestConstArrayFloat_Float32_EndToEnd(t *testing.T) {
	scene := sceneConstArray("StatementConstArrayFloat", "float32", "0.5, 1.5", "[]float32")
	mainC, goCode := generateBoth(t, scene)

	t.Logf("Generated C (float32):\n%s", mainC)
	assertContains(t, mainC, "float constArrayFloat1[]")
	assertContains(t, mainC, "0.5f")
	assertContains(t, mainC, "constArrayFloat1_len = 2")

	t.Logf("Generated Go (float32):\n%s", goCode)
	assertContains(t, goCode, "[]float32{0.5, 1.5}")
}

// TestConstArrayFloat_Float64_EndToEnd: precision float64 → Go []float64;
// C "double" with BARE decimal literals (no suffix).
func TestConstArrayFloat_Float64_EndToEnd(t *testing.T) {
	scene := sceneConstArray("StatementConstArrayFloat", "float64", "0.5, 1.5", "[]float64")
	mainC, goCode := generateBoth(t, scene)

	t.Logf("Generated C (float64):\n%s", mainC)
	assertContains(t, mainC, "double constArrayFloat1[]")
	assertContains(t, mainC, "constArrayFloat1_len = 2")
	if strings.Contains(mainC, "0.5f") {
		t.Errorf("float64 C literals must NOT carry the f suffix; got:\n%s", mainC)
	}

	t.Logf("Generated Go (float64):\n%s", goCode)
	assertContains(t, goCode, "[]float64{0.5, 1.5}")
}

// TestConstArrayString_EndToEnd_LineSplit: THE decision-C pin. Two lines,
// the first containing a comma — must become exactly TWO quoted elements
// with the comma preserved inside the first, in both backends.
func TestConstArrayString_EndToEnd_LineSplit(t *testing.T) {
	// The values go through json.Marshal in the scene helper, so a REAL Go
	// newline ("\n" in an interpreted string) is what produces a real
	// multiline property after the scene round-trip — exactly what the
	// WASM textarea exports. (A raw-string `\n` would arrive as a literal
	// backslash-n and the line split would see ONE element.)
	scene := sceneConstArray("StatementConstArrayString", "string", "hello, world\nred", "[]string")
	mainC, goCode := generateBoth(t, scene)

	t.Logf("Generated C (string):\n%s", mainC)
	assertContains(t, mainC, `const char* constArrayStr1[] = {"hello, world", "red"};`)
	assertContains(t, mainC, "const size_t constArrayStr1_len = 2;")

	t.Logf("Generated Go (string):\n%s", goCode)
	assertContains(t, goCode, `[]string{"hello, world", "red"}`)
}
