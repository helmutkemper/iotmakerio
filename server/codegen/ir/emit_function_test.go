// server/codegen/ir/emit_function_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// StatementFunction lowering (embedded slice 2): a valid name yields a
// FUNC_BEGIN/body/FUNC_END region plus the loud "uncalled" warning; an
// invalid name is refused with no region; a Function nested inside a
// Function is refused (C has no nested functions). Português: Nome
// válido = região + warning barulhento; inválido = recusa sem região;
// aninhado = recusa.
package ir

import (
	"testing"

	"server/codegen/diagnostics"
	"server/codegen/graph"
)

func functionGraph(name string) *graph.Graph {
	fn := &graph.Node{ID: "fn_1", Type: "StatementFunction",
		Properties: map[string]any{"functionName": name}}
	body := dataTextNode("body_1", "x")
	g := &graph.Graph{
		Nodes:  map[string]*graph.Node{fn.ID: fn, body.ID: body},
		Edges:  map[string]*graph.Edge{},
		Scopes: map[string]*graph.Scope{},
	}
	g.Scopes[""] = &graph.Scope{ID: "", NodeIDs: []string{fn.ID}}
	g.Scopes[fn.ID] = &graph.Scope{
		ID: fn.ID, NodeIDs: []string{body.ID},
		Function: true, FunctionName: name,
	}
	return g
}

func TestFunctionRegionAndWarning(t *testing.T) {
	prog, diags := Emit(functionGraph("blinker"), nil, nil)
	var begin, bodyIdx, end = -1, -1, -1
	for i, in := range prog.Instructions {
		switch {
		case in.Op == OpFuncBegin && in.Dest == "blinker":
			begin = i
		case in.Op == OpDataBlob && in.Dest == "body_1":
			bodyIdx = i
		case in.Op == OpFuncEnd:
			end = i
		}
	}
	if begin < 0 || bodyIdx < 0 || end < 0 || !(begin < bodyIdx && bodyIdx < end) {
		t.Fatalf("region order broken: begin=%d body=%d end=%d", begin, bodyIdx, end)
	}
	found := false
	for _, d := range diags {
		if d.Kind == diagnostics.KindFunctionUncalled &&
			d.Severity == diagnostics.SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the loud KindFunctionUncalled warning")
	}
}

func TestFunctionInvalidNameRefused(t *testing.T) {
	prog, diags := Emit(functionGraph("9lives"), nil, nil)
	hit := false
	for _, d := range diags {
		if d.Kind == diagnostics.KindFunctionNameInvalid &&
			d.Severity == diagnostics.SeverityError {
			hit = true
		}
	}
	if !hit {
		t.Fatal("invalid name must raise KindFunctionNameInvalid")
	}
	for _, in := range prog.Instructions {
		if in.Op == OpFuncBegin || in.Op == OpFuncEnd {
			t.Fatal("refused function must emit no region ops")
		}
	}
}

func TestFunctionNestedRefused(t *testing.T) {
	g := functionGraph("outer")
	inner := &graph.Node{ID: "fn_2", Type: "StatementFunction",
		Properties: map[string]any{"functionName": "inner"}}
	g.Nodes[inner.ID] = inner
	g.Scopes["fn_1"].NodeIDs = append(g.Scopes["fn_1"].NodeIDs, inner.ID)
	g.Scopes[inner.ID] = &graph.Scope{
		ID: inner.ID, ParentID: "fn_1", NodeIDs: []string{},
		Function: true, FunctionName: "inner",
	}
	_, diags := Emit(g, nil, nil)
	hit := false
	for _, d := range diags {
		if d.Kind == diagnostics.KindFunctionNested {
			hit = true
		}
	}
	if !hit {
		t.Fatal("a Function inside a Function must raise KindFunctionNested")
	}
}
