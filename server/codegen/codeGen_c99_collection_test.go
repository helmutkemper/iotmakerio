// /server/codegen/codeGen_c99_collection_test.go

package codegen

// codeGen_c99_collection_test.go — Task 7 end to end: an IDE constant
// collection feeding a C99 function-device through the `slice:` directive.
//
// The def comes from the REAL C parser over the authored source (not a
// hand-built struct), so the whole chain is exercised:
//
//	ParseC: (const uint16_t* values, size_t values_len) + `slice:values_len.`
//	        → ONE input port "[]uint16" carrying the length's signature slot
//	graph:  the port's DataType reaches the scene/graph verbatim
//	IR:     inferredCollectionElem (T6 decision B) types the declaration
//	        from that consumer port; buildBBCallArgs rebuilds the pair —
//	        array register + "#" length-companion protocol
//	C:      cTypeName "uint16" → uint16_t (T7 fixed-width translation);
//	        the call lands as f(constArrayInt1, constArrayInt1_len, …),
//	        the `_len` symbol the const array emits precisely because it
//	        survives pointer decay (plan decision 3)
//
// The scalar gain rides along to pin the C-side authored cast:
// `(uint16_t)constInt1`.
//
// Português: T7 ponta-a-ponta — coleção constante do IDE alimentando uma
// função C99 via diretiva `slice:`. O def vem do parser C real; a
// declaração é inferida do consumidor (decisão B), a chamada reconstrói o
// par (array, _len) e o gain escalar fixa o cast autoral do lado C.

import (
	"context"
	"encoding/json"
	"testing"

	"server/codegen/blackbox"
)

// mixerC99Source mirrors the parser test fixture — authored C with the
// slice directive on the pointer parameter.
const mixerC99Source = `// Mixer blends a fixed table of levels scaled by a gain.
// label:Mixer.

#include <stdint.h>
#include <stddef.h>

// blend the table.
void mixer_run(
    // level table.  slice:values_len.  connection:mandatory.
    const uint16_t* values,
    size_t values_len,
    // scale factor.  connection:mandatory.
    uint16_t gain) {
    (void)values; (void)values_len; (void)gain;
}
`

func mixerC99Defs(t *testing.T) map[string]*blackbox.BlackBoxDef {
	t.Helper()
	def, err := blackbox.ParseC([]byte(mixerC99Source), blackbox.DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC failed: %v", err)
	}
	// The store fills RawSource when loading defs for a scene so the C
	// backend can inline the implementation; mirror that here.
	def.RawSource = mixerC99Source
	return map[string]*blackbox.BlackBoxDef{"mixer_run": def}
}

func TestC99Collection_SliceCallEndToEnd(t *testing.T) {
	scene := `{
  "version": "1.0",
  "devices": [
    {
      "id": "constArrayInt_1", "type": "StatementConstArrayInt",
      "properties": { "values": "1, 2, 3", "elementType": "int" },
      "connectors": [
        { "port": "output", "dataType": "[]int", "isOutput": true, "acceptNotConnected": false,
          "connections": [{ "wireId": "w1", "targetDevice": "mixer_1", "targetPort": "values" }] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" }
    },
    {
      "id": "constInt_1", "type": "StatementConstInt", "properties": { "value": 7 },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w2", "targetDevice": "mixer_1", "targetPort": "gain" }] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" }
    },
    {
      "id": "mixer_1", "type": "BlackBoxmixer_run:", "properties": {},
      "connectors": [
        { "port": "values", "dataType": "[]uint16", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "constArrayInt_1", "targetPort": "output" }] },
        { "port": "gain", "dataType": "uint16_t", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "constInt_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "children": [], "parent": "", "status": "free" }
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constArrayInt_1", "port": "output" }, "to": { "device": "mixer_1", "port": "values" }, "dataType": "[]int" },
    { "id": "w2", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "mixer_1", "port": "gain" }, "dataType": "int" }
  ]
}`

	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(scene),
		Language:     "c",
		BlackBoxDefs: mixerC99Defs(t),
	})
	if len(resp.Errors) > 0 {
		t.Fatalf("Errors: %v", resp.Errors)
	}
	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; diagnostics=%+v", resp.Diagnostics)
	}
	t.Logf("Generated C:\n%s", mainC)

	// T6 decision B through the C path: the consumer port "[]uint16"
	// types the declaration — and T7's fixed-width translation renders
	// it as the stdint name. uint16 elements take no literal suffix
	// (they are already ints by C99 §6.4.4.1).
	assertContains(t, mainC, "uint16_t constArrayInt1[] = {1, 2, 3};")
	assertContains(t, mainC, "const size_t constArrayInt1_len = 3;")

	// The `slice:` pair rebuilt at the call: array (decays to pointer),
	// then the `_len` companion, then the cast scalar.
	assertContains(t, mainC, "mixer_run(constArrayInt1, constArrayInt1_len, (uint16_t)constInt1);")
}
