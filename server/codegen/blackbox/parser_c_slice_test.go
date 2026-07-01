// /server/codegen/blackbox/parser_c_slice_test.go

package blackbox

// parser_c_slice_test.go — the `slice:<lenParam>.` directive (const-array
// plan Task 7): a C99 pointer parameter and its length companion collapse
// into ONE collection input port typed "[]T".
//
// Pinned here:
//   - The happy path: (const uint16_t* values, size_t values_len) +
//     `slice:values_len.` → one "[]uint16" port carrying the length
//     parameter's name and signature position; the length parameter
//     itself vanishes from the port list.
//   - The house's TOLERANT stance on authoring mistakes (cf. the
//     callback-mode typo default): a directive naming a missing length
//     parameter, or sitting on a platform-width element pointer
//     (`int*` — embedded C must not guess widths), is DROPPED and both
//     parameters stay ordinary scalar ports. Nothing breaks; the
//     un-collapsed shape is visible in the wizard.
//   - String collections: `const char** names` → "[]string" (a single
//     `char*` is a string SCALAR, so the collection is pointer-to-pointer).
//
// Português: Testes da diretiva `slice:` — o par (ponteiro, tamanho) vira
// UMA porta de coleção "[]T"; diretiva inválida é descartada (postura
// tolerante da casa); coleção de string é `char**`.

import (
	"strings"
	"testing"
)

const mixerCSource = `// Mixer blends a fixed table of levels scaled by a gain.
// label:Mixer.

#include <stdint.h>
#include <stddef.h>

// blend the table.
//
// Params
void mixer_run(
    // level table.  slice:values_len.  connection:mandatory.
    const uint16_t* values,
    size_t values_len,
    // scale factor.  connection:mandatory.
    uint16_t gain) {
    (void)values; (void)values_len; (void)gain;
}
`

func TestParseC_SliceDirective_Collapses(t *testing.T) {
	def, err := ParseC([]byte(mixerCSource), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC failed: %v", err)
	}
	if len(def.Functions) != 1 {
		t.Fatalf("Functions: got %d, want 1", len(def.Functions))
	}
	fn := def.Functions[0].FuncDef

	if len(fn.Inputs) != 2 {
		t.Fatalf("Inputs after collapse: got %d (%+v), want 2", len(fn.Inputs), fn.Inputs)
	}

	values := fn.Inputs[0]
	if values.Name != "values" {
		t.Fatalf("Inputs[0].Name: got %q, want %q", values.Name, "values")
	}
	if values.GoType != "[]uint16" {
		t.Errorf("collapsed GoType: got %q, want %q", values.GoType, "[]uint16")
	}
	if values.SliceLenName != "values_len" {
		t.Errorf("SliceLenName: got %q, want %q", values.SliceLenName, "values_len")
	}
	if values.ParamIndex != 0 || values.SliceLenIndex != 1 {
		t.Errorf("indices: ParamIndex=%d SliceLenIndex=%d, want 0 and 1",
			values.ParamIndex, values.SliceLenIndex)
	}

	gain := fn.Inputs[1]
	if gain.Name != "gain" || gain.ParamIndex != 2 {
		t.Errorf("gain: got name=%q idx=%d, want gain/2", gain.Name, gain.ParamIndex)
	}

	// The length parameter must NOT survive as a port.
	for _, in := range fn.Inputs {
		if in.Name == "values_len" {
			t.Errorf("length parameter leaked as a port: %+v", in)
		}
	}

	// The directive never leaks into the human-readable port doc.
	if got := values.Doc; got != "" && strings.Contains(got, "slice:") {
		t.Errorf("directive leaked into port doc: %q", got)
	}
}

func TestParseC_SliceDirective_InvalidDropped(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// Length parameter does not exist → drop, keep both ports.
			name: "missing length param",
			src: `void f(
    // slice:nope_len.
    const uint16_t* values,
    size_t values_len) {}`,
		},
		{
			// Platform-width element (`int*`) is ineligible by design —
			// embedded C must not guess widths.
			name: "platform-width element",
			src: `void f(
    // slice:n.
    const int* values,
    size_t n) {}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def, err := ParseC([]byte(c.src), DefaultParserLimits())
			if err != nil {
				t.Fatalf("ParseC failed: %v", err)
			}
			fn := def.Functions[0].FuncDef
			if len(fn.Inputs) != 2 {
				t.Fatalf("Inputs: got %d (%+v), want 2 (uncollapsed)", len(fn.Inputs), fn.Inputs)
			}
			if fn.Inputs[0].SliceLenName != "" {
				t.Errorf("invalid directive must be dropped, SliceLenName=%q",
					fn.Inputs[0].SliceLenName)
			}
			if got := fn.Inputs[0].GoType; len(got) > 1 && got[:2] == "[]" {
				t.Errorf("invalid directive must not collapse the type, got %q", got)
			}
		})
	}
}

func TestParseC_SliceDirective_StringCollection(t *testing.T) {
	src := `void print_all(
    // names to print.  slice:count.
    const char** names,
    size_t count) {}`

	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC failed: %v", err)
	}
	fn := def.Functions[0].FuncDef
	if len(fn.Inputs) != 1 {
		t.Fatalf("Inputs: got %d (%+v), want 1", len(fn.Inputs), fn.Inputs)
	}
	if got := fn.Inputs[0].GoType; got != "[]string" {
		t.Errorf("char** collection: got %q, want %q", got, "[]string")
	}
}
