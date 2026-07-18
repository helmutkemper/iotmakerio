// server/codegen/ir/emit_math_unwired_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// §7.5 (decision 2026-06-30, shipped 2026-07-16): a math device whose
// output feeds nothing is a validation ERROR — the leaf assignment would
// not compile on the Go backend. Enforced in the ir so BOTH languages
// inherit it. Português: Saída de math sem fio = erro; imposto no ir,
// as duas linguagens herdam.
package ir

import (
	"testing"

	"server/codegen/diagnostics"
	"server/codegen/graph"
)

func mathGraph(withConsumer bool) *graph.Graph {
	add := &graph.Node{ID: "add_1", Type: "StatementAdd",
		Properties: map[string]any{}}
	g := &graph.Graph{
		Nodes:  map[string]*graph.Node{add.ID: add},
		Edges:  map[string]*graph.Edge{},
		Scopes: map[string]*graph.Scope{},
	}
	members := []string{add.ID}
	if withConsumer {
		sink := dataTextNode("sink_1", "x")
		g.Nodes[sink.ID] = sink
		g.Edges["w1"] = &graph.Edge{
			From: graph.PortRef{DeviceID: "add_1", PortName: "output"},
			To:   graph.PortRef{DeviceID: "sink_1", PortName: "value"},
		}
		members = append(members, sink.ID)
	}
	g.Scopes[""] = &graph.Scope{ID: "", NodeIDs: members}
	return g
}

func TestMathUnwiredOutputIsError(t *testing.T) {
	prog, diags := Emit(mathGraph(false), nil, nil)
	var hit *diagnostics.Diagnostic
	for i, d := range diags {
		if d.Kind == diagnostics.KindMathOutputUnwired {
			hit = &diags[i]
		}
	}
	if hit == nil {
		t.Fatal("unwired math output must raise KindMathOutputUnwired")
	}
	if hit.Severity != diagnostics.SeverityError {
		t.Fatalf("severity must be Error, got %s", hit.Severity)
	}
	if len(hit.Devices) != 1 || hit.Devices[0] != "add_1" {
		t.Fatalf("must name the offender, got %v", hit.Devices)
	}
	for _, in := range prog.Instructions {
		if in.Op == OpAdd {
			t.Fatal("offender must NOT emit an instruction")
		}
	}
}

func TestMathWiredOutputIsClean(t *testing.T) {
	_, diags := Emit(mathGraph(true), nil, nil)
	for _, d := range diags {
		if d.Kind == diagnostics.KindMathOutputUnwired {
			t.Fatalf("wired math wrongly flagged: %s", d.Message)
		}
	}
}
