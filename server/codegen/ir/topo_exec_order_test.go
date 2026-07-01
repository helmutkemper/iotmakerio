// server/codegen/ir/topo_exec_order_test.go
package ir

import (
	"testing"

	"server/codegen/graph"
)

// These tests pin the execution-order ordering rules directly at topoSort,
// where the fix lives. They build a graph by hand (no black-box defs / no emit
// pass needed — the ordering is independent of those) and assert the order
// topoSort returns. See docs/claude_exec_order_propagation.md.

// newTopoEmitter wires a minimal emitter around a hand-built graph. topoSort
// only reads e.graph; the BlackBoxInit / cross-scope branches are not exercised
// by these in-scope, non-Init node sets.
func newTopoEmitter(nodes []*graph.Node, edges []*graph.Edge) *emitter {
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
	return &emitter{graph: g}
}

func tNode(id, typ string, order int) *graph.Node {
	return &graph.Node{ID: id, Type: typ, ExecutionOrder: order, Properties: map[string]interface{}{}}
}

// tWire connects from.output → to.a (a plain data port — never a control port,
// so topoSort counts it for ordering).
func tWire(id, from, to string) *graph.Edge {
	return &graph.Edge{
		ID:       id,
		From:     graph.PortRef{DeviceID: from, PortName: "output"},
		To:       graph.PortRef{DeviceID: to, PortName: "a"},
		DataType: "[]int",
	}
}

// subseq returns the elements of sorted that are in want, preserving order.
func subseq(sorted []string, want ...string) []string {
	set := make(map[string]bool, len(want))
	for _, w := range want {
		set[w] = true
	}
	var out []string
	for _, id := range sorted {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

func equalSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOf(sorted []string, id string) int {
	for i, s := range sorted {
		if s == id {
			return i
		}
	}
	return -1
}

// TestTopoSort_LooseDevicesOrderByExecutionOrder is the regression test for the
// reported bug (the "Test" scene): four independent sort methods, each fed by
// its own const array, with NO wire between the methods. ExecutionOrder is the
// only ordering signal among them, so the methods must run 1,2,3,4.
//
// The const source IDs are chosen to sort lexically in the REVERSE of
// ExecutionOrder. Before the fix, the methods trailed their sources and came
// out 4,3,2,1; the fix makes the sources inherit their method's order, so the
// methods order by ExecutionOrder and each const lands just before its method.
func TestTopoSort_LooseDevicesOrderByExecutionOrder(t *testing.T) {
	nodes := []*graph.Node{
		// source IDs ca_a < ca_b < ca_c < ca_d sort opposite to the methods'
		// ExecutionOrder, so a passing result cannot be coincidence.
		tNode("ca_d", "StatementConstArrayString", 0), tNode("m_str", "BlackBoxSortString:Test", 1),
		tNode("ca_c", "StatementConstArrayFloat", 0), tNode("m_f64", "BlackBoxSortFloat64:Test", 2),
		tNode("ca_b", "StatementConstArrayFloat", 0), tNode("m_f32", "BlackBoxSortFloat32:Test", 3),
		tNode("ca_a", "StatementConstArrayInt", 0), tNode("m_int", "BlackBoxSortInt:Test", 4),
	}
	edges := []*graph.Edge{
		tWire("w1", "ca_d", "m_str"),
		tWire("w2", "ca_c", "m_f64"),
		tWire("w3", "ca_b", "m_f32"),
		tWire("w4", "ca_a", "m_int"),
	}
	e := newTopoEmitter(nodes, edges)

	sorted, errs := e.topoSort([]string{
		"ca_d", "m_str", "ca_c", "m_f64", "ca_b", "m_f32", "ca_a", "m_int",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected topoSort errors: %v", errs)
	}

	// Methods must run in ExecutionOrder ascending.
	gotMethods := subseq(sorted, "m_str", "m_f64", "m_f32", "m_int")
	wantMethods := []string{"m_str", "m_f64", "m_f32", "m_int"}
	if !equalSeq(gotMethods, wantMethods) {
		t.Errorf("method order = %v, want %v (full sorted: %v)", gotMethods, wantMethods, sorted)
	}

	// Clean placement: each const sits immediately before the method it feeds
	// (just-in-time, not clumped at the top).
	pairs := [][2]string{{"ca_d", "m_str"}, {"ca_c", "m_f64"}, {"ca_b", "m_f32"}, {"ca_a", "m_int"}}
	for _, p := range pairs {
		src, dev := indexOf(sorted, p[0]), indexOf(sorted, p[1])
		if src+1 != dev {
			t.Errorf("const %s should sit immediately before %s, got positions %d and %d (sorted: %v)",
				p[0], p[1], src, dev, sorted)
		}
	}
}

// TestTopoSort_ChainOrderedByWireNotExecutionOrder pins the precedence rule:
// when devices are chained by wires, the data dependency wins and the
// ExecutionOrder values are moot. device1→device2→device3 with ExecutionOrder
// 5,1,3 must still emit device1, device2, device3.
func TestTopoSort_ChainOrderedByWireNotExecutionOrder(t *testing.T) {
	nodes := []*graph.Node{
		tNode("c1", "StatementConstArrayInt", 0), tNode("ds1", "StatementConstArrayInt", 0),
		tNode("device1", "BlackBoxStep1:Chain", 5),
		tNode("c2", "StatementConstArrayInt", 0), tNode("device2", "BlackBoxStep2:Chain", 1),
		tNode("c3", "StatementConstArrayInt", 0), tNode("device3", "BlackBoxStep3:Chain", 3),
	}
	edges := []*graph.Edge{
		tWire("w1", "c1", "device1"),
		tWire("w2", "ds1", "device1"),
		tWire("w3", "device1", "device2"), // chain: device1 → device2
		tWire("w4", "c2", "device2"),
		tWire("w5", "device2", "device3"), // chain: device2 → device3
		tWire("w6", "c3", "device3"),
	}
	e := newTopoEmitter(nodes, edges)

	sorted, errs := e.topoSort([]string{
		"c1", "ds1", "device1", "c2", "device2", "c3", "device3",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected topoSort errors: %v", errs)
	}

	gotDevices := subseq(sorted, "device1", "device2", "device3")
	wantDevices := []string{"device1", "device2", "device3"}
	if !equalSeq(gotDevices, wantDevices) {
		t.Errorf("chained device order = %v, want %v (wire must win over ExecutionOrder; full sorted: %v)",
			gotDevices, wantDevices, sorted)
	}

	// Each device's own const inputs must precede it (data dependency).
	for _, p := range [][2]string{{"c1", "device1"}, {"ds1", "device1"}, {"c2", "device2"}, {"c3", "device3"}} {
		if indexOf(sorted, p[0]) > indexOf(sorted, p[1]) {
			t.Errorf("const %s must precede %s (sorted: %v)", p[0], p[1], sorted)
		}
	}
}

// TestTopoSort_OrderZeroRunsLast confirms the sentinel: a device with
// ExecutionOrder 0 (unordered) and no ordered consumer runs after ordered peers.
func TestTopoSort_OrderZeroRunsLast(t *testing.T) {
	nodes := []*graph.Node{
		tNode("ca_x", "StatementConstArrayInt", 0), tNode("m_ordered", "BlackBoxA:T", 2),
		tNode("ca_y", "StatementConstArrayInt", 0), tNode("m_unordered", "BlackBoxB:T", 0),
	}
	edges := []*graph.Edge{
		tWire("w1", "ca_x", "m_ordered"),
		tWire("w2", "ca_y", "m_unordered"),
	}
	e := newTopoEmitter(nodes, edges)

	sorted, errs := e.topoSort([]string{"ca_x", "m_ordered", "ca_y", "m_unordered"})
	if len(errs) != 0 {
		t.Fatalf("unexpected topoSort errors: %v", errs)
	}
	if indexOf(sorted, "m_ordered") > indexOf(sorted, "m_unordered") {
		t.Errorf("ordered device (ExecutionOrder 2) should run before the unordered one (0); sorted: %v", sorted)
	}
}
