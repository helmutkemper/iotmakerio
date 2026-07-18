// server/codegen/ir/emit_datablob_cap_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The flash-asset ceiling (DataBlobMaxBytes): a data device at the cap
// emits normally; one byte over is refused with KindAssetTooLarge and
// NO instruction — the report names the device so the maker knows which
// file to shrink. Português: No teto emite; um byte acima é recusado
// com o device nomeado e sem instrução.
package ir

import (
	"strings"
	"testing"

	"server/codegen/diagnostics"
	"server/codegen/graph"
)

func dataTextNode(id, text string) *graph.Node {
	return &graph.Node{
		ID:   id,
		Type: "StatementDataText",
		Properties: map[string]any{
			"text":           text,
			"nullTerminated": "false",
		},
	}
}

func TestDataBlobAtCeilingEmits(t *testing.T) {
	n := dataTextNode("data_ok", strings.Repeat("a", DataBlobMaxBytes))
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{n.ID: n},
		Edges: map[string]*graph.Edge{},
		Scopes: map[string]*graph.Scope{
			// "" is the global scope (graph.Scope.ID doc) — the emitter
			// reaches nodes ONLY through Scope.NodeIDs. Português: "" é
			// o escopo global; o emitter só alcança nós via NodeIDs.
			"": {ID: "", NodeIDs: []string{n.ID}},
		},
	}
	prog, diags := Emit(g, nil, nil)
	for _, d := range diags {
		if d.Kind == diagnostics.KindAssetTooLarge {
			t.Fatalf("at-cap asset must pass, got: %s", d.Message)
		}
	}
	found := false
	for _, in := range prog.Instructions {
		if in.Op == OpDataBlob && in.Dest == "data_ok" {
			found = true
		}
	}
	if !found {
		t.Fatal("at-cap asset should emit its OpDataBlob")
	}
}

func TestDataBlobOverCeilingRefused(t *testing.T) {
	n := dataTextNode("data_fat", strings.Repeat("a", DataBlobMaxBytes+1))
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{n.ID: n},
		Edges: map[string]*graph.Edge{},
		Scopes: map[string]*graph.Scope{
			// "" is the global scope (graph.Scope.ID doc) — the emitter
			// reaches nodes ONLY through Scope.NodeIDs. Português: "" é
			// o escopo global; o emitter só alcança nós via NodeIDs.
			"": {ID: "", NodeIDs: []string{n.ID}},
		},
	}
	prog, diags := Emit(g, nil, nil)
	var hit *diagnostics.Diagnostic
	for i, d := range diags {
		if d.Kind == diagnostics.KindAssetTooLarge {
			hit = &diags[i]
		}
	}
	if hit == nil {
		t.Fatal("over-cap asset must raise KindAssetTooLarge")
	}
	if hit.Severity != diagnostics.SeverityError {
		t.Fatalf("severity must be Error, got %s", hit.Severity)
	}
	if len(hit.Devices) != 1 || hit.Devices[0] != "data_fat" {
		t.Fatalf("the report must name the device, got %v", hit.Devices)
	}
	for _, in := range prog.Instructions {
		if in.Op == OpDataBlob && in.Dest == "data_fat" {
			t.Fatal("over-cap asset must NOT emit an instruction")
		}
	}
}
