// server/codegen/ir/emit_index_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ir

import (
	"testing"

	"server/codegen/graph"
)

// newIndexEmitter wires a minimal emitter around a hand-built graph with an
// empty program. Input resolution reads node.Inputs (GetInputSources), and the
// optional-ok scan reads graph.Edges — so a reader's input ports live on its
// Inputs, while a consumer of its ok output is an Edge.
func newIndexEmitter(nodes []*graph.Node, edges []*graph.Edge) *emitter {
	g := &graph.Graph{
		Nodes:  make(map[string]*graph.Node, len(nodes)),
		Edges:  make(map[string]*graph.Edge, len(edges)),
		Scopes: map[string]*graph.Scope{},
	}
	for _, n := range nodes {
		g.Nodes[n.ID] = n
	}
	for _, ed := range edges {
		g.Edges[ed.ID] = ed
	}
	return &emitter{graph: g, program: &Program{}}
}

func idxNode(id, typ string) *graph.Node {
	return &graph.Node{ID: id, Type: typ, Properties: map[string]interface{}{}}
}

// idxReader builds a StatementIndex* node whose array/index input ports are
// connected to the given producers (via node.Inputs, which GetInputSources reads).
func idxReader(id, typ, arrDev, idxDev string) *graph.Node {
	n := idxNode(id, typ)
	n.Inputs = []graph.Port{
		{Name: "array", Connected: []graph.PortRef{{DeviceID: arrDev, PortName: "output"}}},
		{Name: "index", Connected: []graph.PortRef{{DeviceID: idxDev, PortName: "output"}}},
	}
	return n
}

func idxWire(id, fromDev, fromPort, toDev, toPort string) *graph.Edge {
	return &graph.Edge{
		ID:   id,
		From: graph.PortRef{DeviceID: fromDev, PortName: fromPort},
		To:   graph.PortRef{DeviceID: toDev, PortName: toPort},
	}
}

func firstIndexInstr(p *Program) (Instruction, bool) {
	for _, in := range p.Instructions {
		if in.Op == OpIndex {
			return in, true
		}
	}
	return Instruction{}, false
}

func TestEmitIndex_WiredInputs_NoOk(t *testing.T) {
	reader := idxReader("reader", "StatementIndexInt", "arr", "idx")
	nodes := []*graph.Node{
		idxNode("arr", "StatementConstArrayInt"),
		idxNode("idx", "StatementConstInt"),
		reader,
	}
	e := newIndexEmitter(nodes, nil) // no edge consumes ok
	e.emitIndex(reader, "int")

	inst, ok := firstIndexInstr(e.program)
	if !ok {
		t.Fatal("no OpIndex emitted for a fully-wired reader")
	}
	if inst.Dest != "reader" || inst.Type != "int" {
		t.Fatalf("Dest/Type: want reader/int, got %q/%q", inst.Dest, inst.Type)
	}
	if len(inst.Args) != 2 || inst.Args[0] != "%arr" || inst.Args[1] != "%idx" {
		t.Fatalf("Args: want [%%arr %%idx], got %v", inst.Args)
	}
	if _, has := inst.Meta["okDest"]; has {
		t.Fatalf("okDest must be absent when ok is unwired; Meta=%v", inst.Meta)
	}
}

func TestEmitIndex_OkWired_SetsCompanionRegister(t *testing.T) {
	reader := idxReader("reader", "StatementIndexInt", "arr", "idx")
	nodes := []*graph.Node{
		idxNode("arr", "StatementConstArrayInt"),
		idxNode("idx", "StatementConstInt"),
		reader,
		idxNode("sink", "StatementBool"),
	}
	edges := []*graph.Edge{idxWire("w3", "reader", "ok", "sink", "input")} // ok consumed
	e := newIndexEmitter(nodes, edges)
	e.emitIndex(reader, "int")

	inst, _ := firstIndexInstr(e.program)
	if got := inst.Meta["okDest"]; got != "reader_ok" {
		t.Fatalf("okDest: want reader_ok, got %q (Meta=%v)", got, inst.Meta)
	}
	// A consumer of the ok output must resolve to the SAME companion register.
	if got := e.resolveInput2("reader", "ok"); got != "%reader_ok" {
		t.Fatalf("resolveInput2(reader, ok): want %%reader_ok, got %q", got)
	}
}

func TestEmitIndex_UnconnectedArray_DefinesZeroValue(t *testing.T) {
	reader := idxNode("reader", "StatementIndexInt")
	reader.Inputs = []graph.Port{
		{Name: "index", Connected: []graph.PortRef{{DeviceID: "idx", PortName: "output"}}},
		// array port left unconnected
	}
	nodes := []*graph.Node{idxNode("idx", "StatementConstInt"), reader}
	e := newIndexEmitter(nodes, nil)
	e.emitIndex(reader, "int")

	if _, ok := firstIndexInstr(e.program); ok {
		t.Fatal("OpIndex must not be emitted when the array is unconnected")
	}
	defined := false
	for _, in := range e.program.Instructions {
		if in.Op == OpConst && in.Dest == "reader" {
			defined = true
		}
	}
	if !defined {
		t.Fatal("value output must be defined (OpConst) when the array is unconnected")
	}
}
