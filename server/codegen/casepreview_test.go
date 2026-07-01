// /server/codegen/casepreview_test.go

package codegen

// casepreview_test.go — Golden tests for PreviewCase. Pure Go, offline:
//   cd server && GOTOOLCHAIN=local GOFLAGS=-mod=mod GOPROXY=off \
//     go test ./codegen/ -run Preview -count=1
//
// The goldens are full-string and pin the scaffold byte for byte against what
// backend/golang/emit.go and backend/ansic/emit.go emit (comma-joined vs
// one-per-line `case`, the C `{ … break; }` block, parenthesised C
// conditions, default rendered last). Each body echoes the case's inspector
// label as a "// <label>" comment (or "// ..." when the case has no label).

import (
	"strings"
	"testing"

	"server/codegen/diagnostics"
	"server/codegen/graph"
)

func eqCode(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("preview mismatch\n--- got ---\n%s\n--- want ---\n%s\n-----------", got, want)
	}
}

// Shared case set for the switch goldens: two value cases (one multi-value)
// and a default, each with an inspector label.
func switchCases() []graph.CaseDef {
	return []graph.CaseDef{
		{Label: "zero", MatchKind: "is", Values: []string{"0"}},
		{Label: "low pair", MatchKind: "isAnyOf", Values: []string{"1", "2"}},
		{Label: "fallback", IsDefault: true},
	}
}

func TestPreview_Go_Switch(t *testing.T) {
	code, _ := PreviewCase("case_1", "go", "int", switchCases())
	want := "switch selector {\n" +
		"case 0:\n" +
		"\t// zero\n" +
		"case 1, 2:\n" +
		"\t// low pair\n" +
		"default:\n" +
		"\t// fallback\n" +
		"}"
	eqCode(t, code, want)
}

func TestPreview_C_Switch(t *testing.T) {
	code, _ := PreviewCase("case_1", "c", "int", switchCases())
	want := "switch (selector) {\n" +
		"case 0:\n" +
		"{\n" +
		"\t// zero\n" +
		"\tbreak;\n" +
		"}\n" +
		"case 1:\n" +
		"case 2:\n" +
		"{\n" +
		"\t// low pair\n" +
		"\tbreak;\n" +
		"}\n" +
		"default:\n" +
		"{\n" +
		"\t// fallback\n" +
		"\tbreak;\n" +
		"}\n" +
		"}"
	eqCode(t, code, want)
}

// Shared case set for the chain goldens: a range and a comparison force the
// if/else-if lowering, plus a default.
func chainCases() []graph.CaseDef {
	return []graph.CaseDef{
		{Label: "teens", MatchKind: "between", Values: []string{"1", "10"}},
		{Label: "big", MatchKind: "gt", Values: []string{"100"}},
		{Label: "rest", IsDefault: true},
	}
}

func TestPreview_Go_Chain(t *testing.T) {
	code, _ := PreviewCase("case_1", "go", "int", chainCases())
	want := "if selector >= 1 && selector <= 10 {\n" +
		"\t// teens\n" +
		"} else if selector > 100 {\n" +
		"\t// big\n" +
		"} else {\n" +
		"\t// rest\n" +
		"}"
	eqCode(t, code, want)
}

func TestPreview_C_Chain(t *testing.T) {
	code, _ := PreviewCase("case_1", "c", "int", chainCases())
	want := "if (selector >= 1 && selector <= 10) {\n" +
		"\t// teens\n" +
		"} else if (selector > 100) {\n" +
		"\t// big\n" +
		"} else {\n" +
		"\t// rest\n" +
		"}"
	eqCode(t, code, want)
}

// Default declared in the MIDDLE of the slice must still render last, matching
// emitCase (which emits non-default cases first, then the default). Each body
// still carries its own label.
func TestPreview_DefaultRendersLast(t *testing.T) {
	cases := []graph.CaseDef{
		{Label: "other", IsDefault: true},
		{Label: "five", MatchKind: "is", Values: []string{"5"}},
	}
	code, _ := PreviewCase("case_1", "go", "int", cases)
	want := "switch selector {\n" +
		"case 5:\n" +
		"\t// five\n" +
		"default:\n" +
		"\t// other\n" +
		"}"
	eqCode(t, code, want)
}

// A case with no inspector label falls back to the neutral ellipsis, so the
// snippet never shows an empty "// " line.
func TestPreview_EmptyLabelFallsBackToEllipsis(t *testing.T) {
	cases := []graph.CaseDef{
		{MatchKind: "is", Values: []string{"7"}},
	}
	code, _ := PreviewCase("case_1", "go", "int", cases)
	want := "switch selector {\n" +
		"case 7:\n" +
		"\t// ...\n" +
		"}"
	eqCode(t, code, want)
}

// A label is user text; a stray newline must not break out of the // comment.
func TestPreview_LabelIsFlattenedToOneLine(t *testing.T) {
	cases := []graph.CaseDef{
		{Label: "line one\nline two", MatchKind: "is", Values: []string{"1"}},
	}
	code, _ := PreviewCase("case_1", "go", "int", cases)
	want := "switch selector {\n" +
		"case 1:\n" +
		"\t// line one line two\n" +
		"}"
	eqCode(t, code, want)
}

// An empty case list previews as an empty switch (UseSwitchLowering treats "no
// non-default cases" as the switch path). Must not panic or emit a dangling
// else.
func TestPreview_EmptyCases(t *testing.T) {
	code, _ := PreviewCase("case_1", "go", "int", nil)
	eqCode(t, code, "switch selector {\n}")
}

// Unknown languages preview as Go (mirrors Generate's "" → Go default).
func TestPreview_UnknownLanguageFallsBackToGo(t *testing.T) {
	code, _ := PreviewCase("case_1", "python", "int", switchCases())
	if !strings.HasPrefix(code, "switch selector {") {
		t.Fatalf("unknown language should preview as Go, got:\n%s", code)
	}
}

// PreviewCase returns the SAME cross-case diagnostics as the pipeline: a
// duplicate switch value is still an error here, alongside the rendered code.
func TestPreview_CarriesDiagnostics(t *testing.T) {
	cases := []graph.CaseDef{
		{Label: "a", MatchKind: "is", Values: []string{"5"}},
		{Label: "b", MatchKind: "is", Values: []string{"5"}},
	}
	code, diags := PreviewCase("case_7", "go", "int", cases)
	if code == "" {
		t.Fatalf("expected rendered code even when diagnostics are present")
	}
	var found bool
	for _, d := range diags {
		if d.Kind == diagnostics.KindCaseDuplicateValue && d.Severity == diagnostics.SeverityError {
			found = true
			if d.Scope != "case_7" {
				t.Fatalf("diagnostic scope = %q, want case_7", d.Scope)
			}
		}
	}
	if !found {
		t.Fatalf("expected duplicate-value error in preview diagnostics, got %d diags", len(diags))
	}
}

// A bool selector previews its structure but carries no cross-case
// diagnostics (true/false are exhaustive).
func TestPreview_BoolSelectorNoDiagnostics(t *testing.T) {
	cases := []graph.CaseDef{
		{Label: "yes", MatchKind: "is", Values: []string{"true"}},
		{Label: "no", MatchKind: "is", Values: []string{"false"}},
	}
	_, diags := PreviewCase("case_1", "go", "bool", cases)
	if len(diags) != 0 {
		t.Fatalf("bool selector should carry no diagnostics, got %d", len(diags))
	}
}
