// server/codegen/ir/emit_tunnel_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// StatementTunnel is FRONT-END FURNITURE (Kemper, 2026-07-18): it emits
// no code; consumers resolve THROUGH it to the real source. These tests
// pin the transparency: zero instruction footprint, value and type
// resolution through single tunnels and chains, dangling-chain and
// cycle behaviour, and the maker-mid-build warnings that survive.
//
// Português: Túnel é peça de FRONT-END — não emite código; consumidores
// resolvem ATRAVÉS dele até a fonte real. Os testes fixam a
// transparência: pegada zero de instruções, resolução de valor e tipo
// por túnel e por cadeia, comportamento com cadeia solta e com ciclo, e
// os avisos de obra que sobrevivem.
package ir

import (
	"strings"
	"testing"

	"server/codegen/graph"
)

// tunnelNode builds a StatementTunnel shell; wire its "in" via srcDev
// (empty srcDev = dangling input). Português: Casca de túnel; srcDev
// vazio = entrada solta.
func tunnelNode(id, srcDev, srcPort string) *graph.Node {
	n := &graph.Node{ID: id, Type: "StatementTunnel",
		Properties: map[string]any{}}
	if srcDev != "" {
		n.Inputs = []graph.Port{{
			Name:      "in",
			Connected: []graph.PortRef{{DeviceID: srcDev, PortName: srcPort}},
		}}
	}
	return n
}

// constIntNode declares an int producer the way real scenes do: a
// "value" property and a typed output port (resolveInputType reads the
// port's DataType). Português: Produtor int como nas cenas reais —
// propriedade "value" e porta de saída tipada.
func constIntNode(id string, value int) *graph.Node {
	return &graph.Node{
		ID: id, Type: "StatementConstInt",
		Properties: map[string]any{"value": value},
		Outputs: []graph.Port{{
			Name: "output", DataType: "int", IsOutput: true,
		}},
	}
}

// consumerOf builds a print-int whose "value" input reads the given
// producer port. Português: Print-int lendo a porta dada.
func consumerOf(id, srcDev, srcPort string) *graph.Node {
	return &graph.Node{
		ID: id, Type: "StatementPrintInt",
		Properties: map[string]any{},
		Inputs: []graph.Port{{
			Name:      "value",
			Connected: []graph.PortRef{{DeviceID: srcDev, PortName: srcPort}},
		}},
	}
}

// tunnelEmitter mirrors newIndexEmitter: a minimal emitter over a
// hand-built graph for surgical resolution tests. Português: Emitter
// mínimo sobre grafo montado à mão, para testes cirúrgicos.
func tunnelEmitter(nodes ...*graph.Node) *emitter {
	g := &graph.Graph{
		Nodes:  make(map[string]*graph.Node, len(nodes)),
		Edges:  map[string]*graph.Edge{},
		Scopes: map[string]*graph.Scope{},
	}
	for _, n := range nodes {
		g.Nodes[n.ID] = n
	}
	return &emitter{graph: g, program: &Program{}}
}

// tunnelScene wraps nodes in a full Emit-able graph — "" is the global
// scope; the emitter reaches nodes ONLY through Scope.NodeIDs.
// Português: Grafo completo para Emit — "" é o escopo global.
func tunnelScene(nodes ...*graph.Node) *graph.Graph {
	g := &graph.Graph{
		Nodes:  make(map[string]*graph.Node, len(nodes)),
		Edges:  map[string]*graph.Edge{},
		Scopes: map[string]*graph.Scope{},
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		g.Nodes[n.ID] = n
		ids = append(ids, n.ID)
	}
	g.Scopes[""] = &graph.Scope{ID: "", NodeIDs: ids}
	return g
}

func TestTunnelResolvesThroughToSource(t *testing.T) {
	e := tunnelEmitter(
		constIntNode("const_1", 7),
		tunnelNode("tunnel_1", "const_1", "output"),
		consumerOf("print_1", "tunnel_1", "out"),
	)
	if got := e.resolveInput("print_1", "value"); got != "%const_1" {
		t.Fatalf("consumer must read THROUGH the tunnel to the source, got %q", got)
	}
}

func TestTunnelChainCollapses(t *testing.T) {
	e := tunnelEmitter(
		constIntNode("const_1", 7),
		tunnelNode("tunnel_1", "const_1", "output"),
		tunnelNode("tunnel_2", "tunnel_1", "out"),
		consumerOf("print_1", "tunnel_2", "out"),
	)
	if got := e.resolveInput("print_1", "value"); got != "%const_1" {
		t.Fatalf("a tunnel chain must collapse to the real source, got %q", got)
	}
}

func TestTunnelTypeResolvesThroughToSource(t *testing.T) {
	e := tunnelEmitter(
		constIntNode("const_1", 7),
		tunnelNode("tunnel_1", "const_1", "output"),
		consumerOf("print_1", "tunnel_1", "out"),
	)
	if got := e.resolveInputType("print_1", "value"); got != "int" {
		t.Fatalf("type must flow through the tunnel to the producer's port, got %q", got)
	}
}

func TestTunnelDanglingChainResolvesUnconnected(t *testing.T) {
	e := tunnelEmitter(
		tunnelNode("tunnel_1", "", ""),
		consumerOf("print_1", "tunnel_1", "out"),
	)
	if got := e.resolveInput("print_1", "value"); got != "" {
		t.Fatalf("a dangling tunnel must resolve as unconnected, got %q", got)
	}
}

func TestTunnelCycleResolvesUnconnected(t *testing.T) {
	// A hostile or buggy scene can wire tunnels into a loop; the guard
	// must terminate (this test would hang without it), warn, and
	// resolve as unconnected. Português: Cena hostil pode fechar ciclo;
	// a guarda precisa terminar, avisar e resolver como desconectado.
	e := tunnelEmitter(
		tunnelNode("tunnel_1", "tunnel_2", "out"),
		tunnelNode("tunnel_2", "tunnel_1", "out"),
		consumerOf("print_1", "tunnel_1", "out"),
	)
	if got := e.resolveInput("print_1", "value"); got != "" {
		t.Fatalf("a tunnel cycle must resolve as unconnected, got %q", got)
	}
	found := false
	for _, w := range e.program.Warnings {
		if strings.Contains(w, "cycle") {
			found = true
		}
	}
	if !found {
		t.Fatal("a tunnel cycle must warn")
	}
}

func TestTunnelEmitsNothing(t *testing.T) {
	prog, _ := Emit(tunnelScene(
		constIntNode("const_1", 7),
		tunnelNode("tunnel_1", "const_1", "output"),
		consumerOf("print_1", "tunnel_1", "out"),
	), nil, nil)

	for _, in := range prog.Instructions {
		if in.Dest == "tunnel_1" {
			t.Fatalf("tunnel must have ZERO instruction footprint, got %+v", in)
		}
	}
	sawPrint := false
	for _, in := range prog.Instructions {
		if in.Op == OpPrint && in.Dest == "print_1" {
			sawPrint = true
			if len(in.Args) == 0 || in.Args[0] != "%const_1" {
				t.Fatalf("print must read the source register directly, got %v", in.Args)
			}
		}
	}
	if !sawPrint {
		t.Fatal("expected the consumer print to be emitted")
	}
	for _, w := range prog.Warnings {
		if strings.Contains(w, "tunnel_1") {
			t.Fatalf("a fully-wired tunnel must not warn, got %q", w)
		}
	}
}

func TestTunnelWarningsSurvive(t *testing.T) {
	// Dangling OUT: wired in, nobody reads it. Português: Saída solta.
	prog, _ := Emit(tunnelScene(
		constIntNode("const_1", 7),
		tunnelNode("tunnel_1", "const_1", "output"),
	), nil, nil)
	found := false
	for _, w := range prog.Warnings {
		if strings.Contains(w, "leads nowhere") {
			found = true
		}
	}
	if !found {
		t.Fatal("a tunnel with a dangling out must warn")
	}

	// Dangling IN: a consumer reads a tunnel nobody feeds. The tunnel
	// names itself; the consumer's own policy also speaks. Português:
	// Entrada solta — o túnel se nomeia; a política do consumidor fala.
	prog, _ = Emit(tunnelScene(
		tunnelNode("tunnel_1", "", ""),
		consumerOf("print_1", "tunnel_1", "out"),
	), nil, nil)
	found = false
	for _, w := range prog.Warnings {
		if strings.Contains(w, "read nothing") {
			found = true
		}
	}
	if !found {
		t.Fatal("a tunnel with a dangling in must warn")
	}
}
