// /server/codegen/casevalidate_test.go

package codegen

// casevalidate_test.go — Behavioural tests for ValidateCases. Pure Go, no
// network, no SQLite: runs under the offline recipe
//   cd server && GOTOOLCHAIN=local GOFLAGS=-mod=mod GOPROXY=off \
//     go test ./codegen/ -run Cases -count=1
//
// Each test pins the kind+severity of the diagnostics and (where it matters)
// the case position named in the message, so a regression in the lowering
// decision or the interval math fails loudly.

import (
	"strings"
	"testing"

	"server/codegen/diagnostics"
	"server/codegen/graph"
)

// kindsOf collapses diagnostics to their kinds for terse assertions.
func kindsOf(ds []Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Kind
	}
	return out
}

// disc builds a discrete ("is"/"isAnyOf") case from values.
func disc(values ...string) graph.CaseDef {
	mk := "is"
	if len(values) != 1 {
		mk = "isAnyOf"
	}
	return graph.CaseDef{MatchKind: mk, Values: values}
}

func btwn(lo, hi string) graph.CaseDef {
	return graph.CaseDef{MatchKind: "between", Values: []string{lo, hi}}
}

func cmp(kind, v string) graph.CaseDef {
	return graph.CaseDef{MatchKind: kind, Values: []string{v}}
}

func deflt() graph.CaseDef { return graph.CaseDef{IsDefault: true} }

func TestCases_BoolSelector_NoDiagnostics(t *testing.T) {
	cases := []graph.CaseDef{disc("true"), disc("false")}
	if ds := ValidateCases("case_1", "bool", cases); len(ds) != 0 {
		t.Fatalf("bool selector should yield no diagnostics, got %v", kindsOf(ds))
	}
}

func TestCases_Switch_Clean(t *testing.T) {
	cases := []graph.CaseDef{disc("0"), disc("1"), disc("2"), deflt()}
	if ds := ValidateCases("case_1", "int", cases); len(ds) != 0 {
		t.Fatalf("distinct switch labels should be clean, got %v", kindsOf(ds))
	}
}

func TestCases_Switch_DuplicateAcrossCases_IsError(t *testing.T) {
	// Two cases both claim 5 → duplicate `case` label → error.
	cases := []graph.CaseDef{disc("5"), disc("7"), disc("5")}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(ds), kindsOf(ds))
	}
	if ds[0].Kind != diagnostics.KindCaseDuplicateValue {
		t.Fatalf("kind = %q, want %q", ds[0].Kind, diagnostics.KindCaseDuplicateValue)
	}
	if ds[0].Severity != diagnostics.SeverityError {
		t.Fatalf("severity = %q, want error", ds[0].Severity)
	}
	// Message should name both positions (#1 and #3) and the value.
	if !strings.Contains(ds[0].Message, "#1") || !strings.Contains(ds[0].Message, "#3") {
		t.Fatalf("message should name cases #1 and #3: %q", ds[0].Message)
	}
	if !strings.Contains(ds[0].Message, "5") {
		t.Fatalf("message should name value 5: %q", ds[0].Message)
	}
}

func TestCases_Switch_DuplicateCanonicalised(t *testing.T) {
	// "5", " 5 " and "05" are the same switch label.
	cases := []graph.CaseDef{disc("5"), disc(" 5 ")}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 || ds[0].Kind != diagnostics.KindCaseDuplicateValue {
		t.Fatalf("whitespace-variant duplicate not detected: %v", kindsOf(ds))
	}
}

func TestCases_Switch_DuplicateWithinIsAnyOf_IsError(t *testing.T) {
	// isAnyOf 1, 1 repeats a label inside a single case.
	cases := []graph.CaseDef{disc("1", "1"), disc("2")}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 || ds[0].Kind != diagnostics.KindCaseDuplicateValue {
		t.Fatalf("within-case duplicate not detected: %v", kindsOf(ds))
	}
	if !strings.Contains(ds[0].Message, "more than once") {
		t.Fatalf("expected within-case wording, got %q", ds[0].Message)
	}
}

func TestCases_Chain_EmptyBetween_IsWarning(t *testing.T) {
	// A range forces the chain lowering; between 10 and 1 is empty.
	cases := []graph.CaseDef{btwn("10", "1"), deflt()}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(ds), kindsOf(ds))
	}
	if ds[0].Kind != diagnostics.KindCaseEmptyRange || ds[0].Severity != diagnostics.SeverityWarning {
		t.Fatalf("got kind=%q sev=%q, want empty-range warning", ds[0].Kind, ds[0].Severity)
	}
}

func TestCases_Chain_UnreachableInsideEarlierRange_IsWarning(t *testing.T) {
	// between 1..10 then is 5 → 5 is already covered → unreachable.
	cases := []graph.CaseDef{btwn("1", "10"), disc("5"), deflt()}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(ds), kindsOf(ds))
	}
	if ds[0].Kind != diagnostics.KindCaseUnreachable || ds[0].Severity != diagnostics.SeverityWarning {
		t.Fatalf("got kind=%q sev=%q, want unreachable warning", ds[0].Kind, ds[0].Severity)
	}
	if !strings.Contains(ds[0].Message, "#2") {
		t.Fatalf("message should name case #2: %q", ds[0].Message)
	}
}

func TestCases_Chain_ReachableNotFlagged(t *testing.T) {
	// between 1..10 then is 20 (outside) → reachable, no diagnostic.
	cases := []graph.CaseDef{btwn("1", "10"), disc("20"), deflt()}
	if ds := ValidateCases("case_1", "int", cases); len(ds) != 0 {
		t.Fatalf("reachable case wrongly flagged: %v", kindsOf(ds))
	}
}

func TestCases_Chain_AdjacentRangesShadowMiddle(t *testing.T) {
	// [1,5] and [6,9] together cover [1,9]; a later [3,7] is unreachable.
	cases := []graph.CaseDef{btwn("1", "5"), btwn("6", "9"), btwn("3", "7"), deflt()}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 || ds[0].Kind != diagnostics.KindCaseUnreachable {
		t.Fatalf("adjacent-range shadowing not detected: %v", kindsOf(ds))
	}
	if !strings.Contains(ds[0].Message, "#3") {
		t.Fatalf("message should name case #3: %q", ds[0].Message)
	}
}

func TestCases_Chain_ComparisonCoversThreshold(t *testing.T) {
	// gte 0 covers every non-negative; a later is 5 is unreachable.
	cases := []graph.CaseDef{cmp("gte", "0"), disc("5"), deflt()}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 || ds[0].Kind != diagnostics.KindCaseUnreachable {
		t.Fatalf("comparison coverage not detected: %v", kindsOf(ds))
	}
}

func TestCases_Chain_ComparisonReachableBelowThreshold(t *testing.T) {
	// gt 10 does NOT cover 5; is 5 stays reachable.
	cases := []graph.CaseDef{cmp("gt", "10"), disc("5"), deflt()}
	if ds := ValidateCases("case_1", "int", cases); len(ds) != 0 {
		t.Fatalf("case below threshold wrongly flagged: %v", kindsOf(ds))
	}
}

func TestCases_Chain_GapBetweenRangesKeepsReachable(t *testing.T) {
	// [1,5] and [10,15] leave a gap; is 7 is reachable (not covered).
	cases := []graph.CaseDef{btwn("1", "5"), btwn("10", "15"), disc("7"), deflt()}
	if ds := ValidateCases("case_1", "int", cases); len(ds) != 0 {
		t.Fatalf("case in the gap wrongly flagged: %v", kindsOf(ds))
	}
}

func TestCases_Chain_DuplicateBecomesUnreachable(t *testing.T) {
	// is 5, between 1..10, is 5 → chain lowering (range present); the second
	// `is 5` is unreachable (the first matches first). Verifies a duplicate
	// surfaces as a warning in the chain, NOT a duplicate-value error.
	cases := []graph.CaseDef{disc("5"), btwn("1", "10"), disc("5"), deflt()}
	ds := ValidateCases("case_1", "int", cases)
	if len(ds) != 1 || ds[0].Kind != diagnostics.KindCaseUnreachable {
		t.Fatalf("chain duplicate should be unreachable warning, got %v", kindsOf(ds))
	}
	if ds[0].Severity != diagnostics.SeverityWarning {
		t.Fatalf("severity = %q, want warning", ds[0].Severity)
	}
}

func TestCases_DiagnosticCarriesScopeAndDevice(t *testing.T) {
	cases := []graph.CaseDef{disc("5"), disc("5")}
	ds := ValidateCases("myCase_7", "int", cases)
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(ds))
	}
	if ds[0].Scope != "myCase_7" {
		t.Fatalf("scope = %q, want myCase_7", ds[0].Scope)
	}
	if len(ds[0].Devices) != 1 || ds[0].Devices[0] != "myCase_7" {
		t.Fatalf("devices = %v, want [myCase_7]", ds[0].Devices)
	}
}

func TestCases_MalformedOperandsDoNotPanicOrFalseError(t *testing.T) {
	// Non-integer operands are the overlay's per-row concern; ValidateCases
	// must neither panic nor invent a cross-case error from them.
	cases := []graph.CaseDef{disc("abc"), btwn("x", "y"), cmp("gt", "")}
	if ds := ValidateCases("case_1", "int", cases); len(ds) != 0 {
		t.Fatalf("malformed operands should not produce cross-case diagnostics: %v", kindsOf(ds))
	}
}

// TestCases_WiredIntoValidate proves the pipeline's validate() actually calls
// ValidateCases for a StatementCase scope — i.e. a full "Generate Code" run
// surfaces the same conflicts the inspect panel does. A minimal graph (one
// StatementCase node + its scope) with two cases claiming the same value must
// yield the duplicate-value error from validate(), not just from a direct
// ValidateCases call.
//
// Português: Prova que o validate() do pipeline chama o ValidateCases para um
// scope de StatementCase — uma geração real reporta os mesmos conflitos do
// painel. Grafo mínimo com dois cases no mesmo valor → erro de duplicata.
func TestCases_WiredIntoValidate(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"stmCase_1": {ID: "stmCase_1", Type: "StatementCase"},
		},
		Scopes: map[string]*graph.Scope{
			"stmCase_1": {
				ID: "stmCase_1",
				// Non-nil selector so the "no selector connected" check
				// does not fire and muddy the assertion.
				SelectorPort: &graph.PortRef{DeviceID: "constInt_sel", PortName: "output"},
				Cases:        []graph.CaseDef{disc("5"), disc("5")},
			},
		},
	}

	diags := validate(g, nil)

	var found *Diagnostic
	for i := range diags {
		if diags[i].Kind == diagnostics.KindCaseDuplicateValue {
			found = &diags[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("validate() did not surface the duplicate-value diagnostic; got kinds %v", kindsOf(diags))
	}
	if found.Severity != diagnostics.SeverityError {
		t.Fatalf("duplicate-value severity = %q, want error", found.Severity)
	}
	if found.Scope != "stmCase_1" {
		t.Fatalf("scope = %q, want stmCase_1", found.Scope)
	}
}
