// codeGen_case_cond_test.go — StatementCase codegen, the if/else-if lowering.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// A switch `case` label only accepts discrete constants in both Go and C, so a
// StatementCase whose int selector uses any range or comparison case
// (between/gt/lt/gte/lte) cannot be a switch. emitCase detects this and lowers
// the WHOLE Case to a flat if/else-if chain via the COND_* opcodes; the two
// backends render `if … } else if … } else …` and resolve the boolean
// expression through the shared ir.BuildCaseCondition helper.
//
// This test reuses the proven int-switch scene (sceneCaseIntSwitch) and swaps
// only its "cases" block so that case_a is a range (between 0 and 10) and
// case_b is a threshold (greater than 100). Everything else — the const-int
// selector, the per-case const+const→Add bodies, the wiring and containment —
// is identical, which keeps the test focused on the lowering decision. The
// backward-compatible path (no matchKind → discrete → switch) stays covered by
// the original TestCaseIntSwitch* tests, whose scene carries no matchKind.
//
// Português: Um rótulo `case` de switch só aceita constantes discretas em Go e
// C, então um StatementCase com selector int usando range/comparação não pode
// ser switch. O emitCase detecta isso e baixa o Case inteiro para uma cadeia
// if/else-if (opcodes COND_*); os backends renderizam `if … } else if … } else`
// e montam a condição via ir.BuildCaseCondition. O teste reusa a cena de switch
// já provada e troca só o bloco "cases" (case_a vira range 0..10, case_b vira
// limiar > 100). A retrocompatibilidade (sem matchKind → discreto → switch)
// segue coberta pelos testes TestCaseIntSwitch* originais.

package codegen

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// oldCasesBlock is the exact "cases" array of sceneCaseIntSwitch. It must match
// that source verbatim (whitespace included); if the original scene is ever
// reformatted, the replacement silently no-ops and the guard at the top of each
// test fails loudly rather than testing the wrong scene.
const oldCasesBlock = `        "cases": [
          { "id": "case_a",   "label": "a",     "values": ["0","1"], "ids": ["const_a1","const_a2","add_a"] },
          { "id": "case_b",   "label": "b",     "values": ["2"],     "ids": ["const_b1","const_b2","add_b"] },
          { "id": "case_def", "label": "other", "values": [],        "ids": ["const_d1","const_d2","add_def"] }
        ],`

// newCasesBlock forces the if/else-if lowering: a range case and a threshold
// case alongside the default. case_a matches 0..10 inclusive, case_b matches
// anything greater than 100, and case_def is the catch-all.
const newCasesBlock = `        "cases": [
          { "id": "case_a",   "label": "low",   "matchKind": "between", "values": ["0","10"], "ids": ["const_a1","const_a2","add_a"] },
          { "id": "case_b",   "label": "high",  "matchKind": "gt",      "values": ["100"],    "ids": ["const_b1","const_b2","add_b"] },
          { "id": "case_def", "label": "other", "matchKind": "is",      "values": [],         "ids": ["const_d1","const_d2","add_def"] }
        ],`

// sceneCaseIntCond is sceneCaseIntSwitch with the cases block above swapped in.
var sceneCaseIntCond = strings.Replace(sceneCaseIntSwitch, oldCasesBlock, newCasesBlock, 1)

// indexOrder asserts that each needle appears in s and in the given order. It
// returns false (after logging) on the first needle that is missing or out of
// order, so callers can fail the test with full context.
func indexOrder(t *testing.T, s string, needles ...string) bool {
	t.Helper()
	prev := -1
	for _, n := range needles {
		i := strings.Index(s, n)
		if i < 0 {
			t.Errorf("expected %q to appear", n)
			return false
		}
		if i < prev {
			t.Errorf("expected %q to appear after the previous marker (order violated)", n)
			return false
		}
		prev = i
	}
	return true
}

// TestCaseIntCondChainGo asserts the Go backend lowers a range/comparison Case
// to a flat if/else-if chain (never a switch), with the branch conditions in
// declared order so the first match wins.
func TestCaseIntCondChainGo(t *testing.T) {
	if !strings.Contains(sceneCaseIntCond, `"matchKind": "between"`) {
		t.Fatal("scene replacement failed — did sceneCaseIntSwitch's cases block change? oldCasesBlock no longer matches")
	}

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneCaseIntCond),
		Language: "go",
	})
	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors generating Go:\n%s", strings.Join(resp.Errors, "\n"))
	}

	// A range/comparison Case must NOT be a switch.
	if strings.Contains(resp.Code, "switch") {
		t.Errorf("expected an if/else-if chain, but found a switch in:\n%s", resp.Code)
	}

	// The chain shape and both branch conditions.
	for _, want := range []string{
		">= 0 && ", // case_a: between 0 and 10 → sel >= 0 && sel <= 10
		"<= 10",
		"> 100",      // case_b: greater than 100 → sel > 100
		"} else if ", // second branch
		"} else {",   // default branch
	} {
		if !strings.Contains(resp.Code, want) {
			t.Errorf("Go chain missing %q in:\n%s", want, resp.Code)
		}
	}

	// Declared order is preserved (between before gt before default), because
	// overlapping ranges resolve first-match-wins.
	indexOrder(t, resp.Code, ">= 0 && ", "> 100", "} else {")

	// All three case bodies still emit (an Add per case).
	if got := strings.Count(resp.Code, " + "); got < 3 {
		t.Errorf("expected at least 3 additions (one per case body), got %d in:\n%s", got, resp.Code)
	}

	// Syntactic validity (the unused Add result rules out a full build).
	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", resp.Code, parser.AllErrors); err != nil {
		t.Errorf("generated Go does not parse: %v\n%s", err, resp.Code)
	}

	if t.Failed() {
		t.Logf("Go code:\n%s", resp.Code)
	}
}

// TestCaseIntCondChainC asserts the C backend lowers the same scene to a
// parenthesised if/else-if chain (never a switch) and that the generated C
// actually compiles with gcc.
func TestCaseIntCondChainC(t *testing.T) {
	if !strings.Contains(sceneCaseIntCond, `"matchKind": "between"`) {
		t.Fatal("scene replacement failed — did sceneCaseIntSwitch's cases block change? oldCasesBlock no longer matches")
	}

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneCaseIntCond),
		Language: "c",
	})
	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors generating C:\n%s", strings.Join(resp.Errors, "\n"))
	}
	if len(resp.Files) == 0 {
		t.Fatalf("C backend produced no files")
	}

	var combined strings.Builder
	var names []string
	for name, content := range resp.Files {
		names = append(names, name)
		combined.WriteString("/* === " + name + " === */\n")
		combined.WriteString(content)
		combined.WriteString("\n")
	}
	code := combined.String()

	if strings.Contains(code, "switch") {
		t.Errorf("expected an if/else-if chain, but found a switch in:\n%s", code)
	}
	for _, want := range []string{
		"if (",        // C parenthesises conditions
		">= 0 && ",    // between lower bound + conjunction
		"<= 10",       // between upper bound
		"> 100",       // gt threshold
		"} else if (", // second branch
		"} else {",    // default branch
	} {
		if !strings.Contains(code, want) {
			t.Errorf("C chain missing %q in:\n%s", want, code)
		}
	}
	indexOrder(t, code, ">= 0 && ", "> 100", "} else {")

	// Compile with gcc (skip, don't fail, if gcc is unavailable).
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skipf("gcc not available, skipping compile check: %v", err)
	}
	dir := t.TempDir()
	var cFiles []string
	for name, content := range resp.Files {
		p := filepath.Join(dir, name)
		if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr != nil {
			t.Fatalf("mkdir for %s: %v", name, mkErr)
		}
		if wErr := os.WriteFile(p, []byte(content), 0o644); wErr != nil {
			t.Fatalf("write %s: %v", name, wErr)
		}
		if strings.HasSuffix(name, ".c") {
			cFiles = append(cFiles, p)
		}
	}
	if len(cFiles) == 0 {
		t.Fatalf("no .c translation unit among generated files: %v", names)
	}
	bin := filepath.Join(dir, "case_cond_gen")
	args := append([]string{"-std=c99", "-I", dir, "-o", bin}, cFiles...)
	out, err := exec.Command(gcc, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("generated C failed to compile: %v\n%s\n--- files ---\n%s", err, out, code)
	}

	if t.Failed() {
		t.Logf("C files:\n%s", code)
	}
}
