// server/codegen/ir/emit_sequence_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// StatementSequence lowering: N ordered phases, ALL run, no construct —
// pure emission order (the transparency law). Covered here: (1) phases
// emit in 0→1→2 order regardless of executionOrder noise across phases;
// (2) a backward wire between phases raises KindSequenceOrderViolation
// naming both devices; (3) a forward wire between phases is legal.
// Português: Fases emitem em ordem; fio para trás = diagnóstico nomeando
// os dois devices; fio para frente é legal.
package ir

import (
	"testing"

	"server/codegen/diagnostics"
	"server/codegen/graph"
)

func seqGraph(phases [][]string, extraEdges map[string]*graph.Edge) *graph.Graph {
	seq := &graph.Node{ID: "seq_1", Type: "StatementSequence",
		Properties: map[string]any{}}
	g := &graph.Graph{
		Nodes:  map[string]*graph.Node{seq.ID: seq},
		Edges:  map[string]*graph.Edge{},
		Scopes: map[string]*graph.Scope{},
	}
	var cases []graph.CaseDef
	var members []string
	for _, ids := range phases {
		cases = append(cases, graph.CaseDef{IDs: ids})
		for _, id := range ids {
			// dataTextNode: the unconditional-emit probe proven by the
			// blob-ceiling tests — OpDataBlob with Dest = id, no wires
			// needed. Português: Sonda de emissão incondicional já
			// provada pelos testes do teto.
			g.Nodes[id] = dataTextNode(id, "x")
			members = append(members, id)
		}
	}
	g.Scopes[""] = &graph.Scope{ID: "", NodeIDs: []string{seq.ID}}
	g.Scopes[seq.ID] = &graph.Scope{
		ID: seq.ID, NodeIDs: members,
		Sequence: true, Cases: cases,
	}
	for k, e := range extraEdges {
		g.Edges[k] = e
	}
	return g
}

func emittedOrder(prog *Program, ids ...string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	for _, in := range prog.Instructions {
		if want[in.Dest] {
			out = append(out, in.Dest)
		}
	}
	return out
}

func TestSequencePhasesEmitInOrder(t *testing.T) {
	g := seqGraph([][]string{{"p0"}, {"p1"}, {"p2"}}, nil)
	prog, diags := Emit(g, nil, nil)
	for _, d := range diags {
		if d.Severity == diagnostics.SeverityError {
			t.Fatalf("unexpected error: %s", d.Message)
		}
	}
	got := emittedOrder(prog, "p0", "p1", "p2")
	if len(got) != 3 || got[0] != "p0" || got[1] != "p1" || got[2] != "p2" {
		t.Fatalf("phase order broken: %v", got)
	}
}

func TestSequenceBackwardWireIsViolation(t *testing.T) {
	edges := map[string]*graph.Edge{
		"e1": {
			From: graph.PortRef{DeviceID: "p2", PortName: "out"},
			To:   graph.PortRef{DeviceID: "p0", PortName: "in"},
		},
	}
	g := seqGraph([][]string{{"p0"}, {"p1"}, {"p2"}}, edges)
	_, diags := Emit(g, nil, nil)
	var hit *diagnostics.Diagnostic
	for i, d := range diags {
		if d.Kind == diagnostics.KindSequenceOrderViolation {
			hit = &diags[i]
		}
	}
	if hit == nil {
		t.Fatal("backward wire must raise KindSequenceOrderViolation")
	}
	if len(hit.Devices) != 2 || hit.Devices[0] != "p2" || hit.Devices[1] != "p0" {
		t.Fatalf("violation must name producer then consumer, got %v", hit.Devices)
	}
}

func TestSequenceForwardWireIsLegal(t *testing.T) {
	edges := map[string]*graph.Edge{
		"e1": {
			From: graph.PortRef{DeviceID: "p0", PortName: "out"},
			To:   graph.PortRef{DeviceID: "p2", PortName: "in"},
		},
	}
	g := seqGraph([][]string{{"p0"}, {"p1"}, {"p2"}}, edges)
	_, diags := Emit(g, nil, nil)
	for _, d := range diags {
		if d.Kind == diagnostics.KindSequenceOrderViolation {
			t.Fatalf("forward wire wrongly flagged: %s", d.Message)
		}
	}
}
