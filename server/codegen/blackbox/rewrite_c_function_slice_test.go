// rewrite_c_function_slice_test.go — regression for the 2026-06-20 report:
// editing a collection input port in the Wizard dropped the structural
// `slice:<len>.` directive, silently un-collapsing the (pointer, length)
// pair. The port reverted from []string to `const char **` and the length
// parameter (values_len) reappeared as its own unconfigured pin.
//
// The fix re-emits `slice:` from the parsed PortDef.SliceLenName on every
// port rewrite, so an edit to the port — or to a sibling — leaves it intact.

package blackbox

import (
	"strings"
	"testing"
)

const sliceSortStringSrc = "" +
	"// label:Sorters.\n" +
	"\n" +
	"#include <stdint.h>\n" +
	"#include <stddef.h>\n" +
	"\n" +
	"// sort_string sorts a constant string collection ascending and prints it.\n" +
	"//\n" +
	"// executionOrder:4. icon:sort. label:sort string.\n" +
	"void sort_string(\n" +
	"    // string table to sort.  slice:values_len.  connection:mandatory.\n" +
	"    const char **values,\n" +
	"    size_t values_len) {\n" +
	"    (void)values; (void)values_len;\n" +
	"}\n"

// assertOneStringCollection parses src and checks sort_string still exposes a
// SINGLE input port `values` of type []string (i.e. the slice pair is still
// collapsed and values_len is not a separate pin).
func assertOneStringCollection(t *testing.T, src, stage string) {
	t.Helper()
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("[%s] ParseC: %v\nsource:\n%s", stage, err, src)
	}
	var fn *NamedFuncDef
	for i := range def.Functions {
		if def.Functions[i].Name == "sort_string" {
			fn = &def.Functions[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("[%s] sort_string not found", stage)
	}
	if len(fn.Inputs) != 1 {
		var got []string
		for _, p := range fn.Inputs {
			got = append(got, p.Name+":"+p.GoType)
		}
		t.Fatalf("[%s] expected ONE collapsed input port, got %d: %v\nsource:\n%s",
			stage, len(fn.Inputs), got, src)
	}
	if fn.Inputs[0].Name != "values" || fn.Inputs[0].GoType != "[]string" {
		t.Fatalf("[%s] expected values:[]string, got %s:%s",
			stage, fn.Inputs[0].Name, fn.Inputs[0].GoType)
	}
}

func TestRewriteC_FunctionPortEdit_PreservesSliceDirective(t *testing.T) {
	// Baseline: the authored source already collapses to one []string port.
	assertOneStringCollection(t, sliceSortStringSrc, "baseline")

	// The user opened the `values` port popup and saved it unchanged
	// (label "values", mandatory, the same comment) — exactly the steps in
	// the report.
	edit := mkEdit(OpSetPortConnection, "function.sort_string.in.values", map[string]any{
		"connection": "mandatory",
		"label":      "values",
		"comment":    "string table to sort",
	})

	out, err := RewriteC(sliceSortStringSrc, []WizardEdit{edit})
	if err != nil {
		t.Fatalf("RewriteC: %v", err)
	}
	if !strings.Contains(out, "slice:values_len.") {
		t.Fatalf("slice:values_len. was dropped by the port edit; source:\n%s", out)
	}
	// The pair must still be collapsed after the edit.
	assertOneStringCollection(t, out, "after first edit")

	// Round-trip: editing again (now the directive lives on its own
	// `// slice:values_len.` line, the multi-line form) must STILL keep it.
	out2, err := RewriteC(out, []WizardEdit{edit})
	if err != nil {
		t.Fatalf("RewriteC (second edit): %v", err)
	}
	if !strings.Contains(out2, "slice:values_len.") {
		t.Fatalf("slice:values_len. was dropped on the second edit; source:\n%s", out2)
	}
	assertOneStringCollection(t, out2, "after second edit")
}
