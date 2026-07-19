// server/codegen/codeGen_phase_tunnel_validation_test.go
//
// Offline coverage for the C99 §7 rule (phase_tunnel_validation.go):
// feed in the birth phase, consumers strictly after it. One scene
// skeleton, six wirings — the healthy one is the control.
// Português: Cobertura offline da regra §7: feed na fase natal,
// consumidores estritamente depois. Um esqueleto de cena, seis
// ligações — a saudável é o controle.
package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"server/codegen/diagnostics"
)

// phaseTunnelScene builds a three-phase sequence (c0, c1, c2) with one
// tunnel (natal parameterized), a const feeder and a print consumer
// whose phase memberships the caller chooses. An empty phase drops the
// device from every phase — the "outside the sequence" shape.
// Português: Sequence de três fases com um túnel (natal parametrizado),
// um const alimentador e um print consumidor com fases à escolha. Fase
// vazia = device fora de toda fase — a forma "fora do sequence".
func phaseTunnelScene(natal, feederPhase, consumerPhase string) string {
	member := func(phase, dev string) string {
		if phase == "" {
			return ""
		}
		return dev
	}
	ids := map[string][]string{"c0": {}, "c1": {}, "c2": {}}
	if d := member(feederPhase, "constInt_1"); d != "" {
		ids[feederPhase] = append(ids[feederPhase], d)
	}
	if d := member(consumerPhase, "printInt_1"); d != "" {
		ids[consumerPhase] = append(ids[consumerPhase], d)
	}
	quote := func(list []string) string {
		var q []string
		for _, s := range list {
			q = append(q, fmt.Sprintf("%q", s))
		}
		return strings.Join(q, ", ")
	}
	// Containment mirrors what the exporter always writes — the graph
	// builder births scopes from it (Pass 1/2), not from the cases.
	// Português: O containment espelha o que o exportador sempre grava —
	// o builder nasce os escopos dele, não dos cases.
	var children []string
	contain := func(phase string) string {
		if phase == "" {
			return `{ "isContainer": false, "status": "free" }`
		}
		return `{ "isContainer": false, "parent": "seq_1", "status": "contained" }`
	}
	if feederPhase != "" {
		children = append(children, "constInt_1")
	}
	if consumerPhase != "" {
		children = append(children, "printInt_1")
	}
	feederContain := contain(feederPhase)
	consumerContain := contain(consumerPhase)
	return fmt.Sprintf(`{
  "version": "1.0",
  "metadata": { "language": "go" },
  "devices": [
    {
      "id": "seq_1", "type": "StatementSequence", "kind": "complex", "stage": "backend",
      "properties": {
        "cases": [
          { "id": "seq_1_c0", "ids": [%s], "label": "phase 0", "matchKind": "is", "values": ["0"] },
          { "id": "seq_1_c1", "ids": [%s], "label": "phase 1", "matchKind": "is", "values": ["1"] },
          { "id": "seq_1_c2", "ids": [%s], "label": "phase 2", "matchKind": "is", "values": ["2"] }
        ],
        "selectedCase": "seq_1_c0"
      },
      "position": { "x": 0, "y": 0 }, "size": { "width": 400, "height": 300 },
      "connectors": [],
      "containment": { "isContainer": true, "children": [%s] }
    },
    {
      "id": "tunnel_1", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "tunnel_1", "tunnelNatal": %q, "tunnelParent": "seq_1", "tunnelSide": "right" },
      "position": { "x": 400, "y": 60 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "constInt_1", "targetPort": "output" }] },
        { "port": "out", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w2", "targetDevice": "printInt_1", "targetPort": "value" }] }
      ]
    },
    {
      "id": "constInt_1", "type": "StatementConstInt", "kind": "simple", "stage": "backend",
      "properties": { "value": 7 },
      "position": { "x": 40, "y": 40 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "w1", "targetDevice": "tunnel_1", "targetPort": "in" }] }
      ],
      "containment": %s
    },
    {
      "id": "printInt_1", "type": "StatementPrintInt", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 40, "y": 160 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "tunnel_1", "targetPort": "out" }] }
      ],
      "containment": %s
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "constInt_1", "port": "output" }, "to": { "device": "tunnel_1", "port": "in" }, "dataType": "int" },
    { "id": "w2", "from": { "device": "tunnel_1", "port": "out" }, "to": { "device": "printInt_1", "port": "value" }, "dataType": "int" }
  ]
}`, quote(ids["c0"]), quote(ids["c1"]), quote(ids["c2"]), quote(children), natal,
		feederContain, consumerContain)
}

// phaseOrderDiags filters the §7 family out of a response.
func phaseOrderDiags(resp Response) []Diagnostic {
	var out []Diagnostic
	for _, d := range resp.Diagnostics {
		if d.Kind == diagnostics.KindPhaseOrder {
			out = append(out, d)
		}
	}
	return out
}

func TestPhaseTunnelValidation(t *testing.T) {
	cases := []struct {
		name          string
		natal         string
		feederPhase   string // "" = outside every phase
		consumerPhase string
		wantSeverity  string // "" = expect NO §7 diagnostics
		wantSubstring string
	}{
		{
			name:  "healthy order is silent",
			natal: "seq_1_c1", feederPhase: "c1", consumerPhase: "c2",
			wantSeverity: "",
		},
		{
			name:  "consumer in the birth phase errors",
			natal: "seq_1_c1", feederPhase: "c1", consumerPhase: "c1",
			wantSeverity:  string(diagnostics.SeverityError),
			wantSubstring: "at or before the tunnel's birth phase",
		},
		{
			name:  "consumer before the birth phase errors",
			natal: "seq_1_c1", feederPhase: "c1", consumerPhase: "c0",
			wantSeverity:  string(diagnostics.SeverityError),
			wantSubstring: "at or before the tunnel's birth phase",
		},
		{
			name:  "feed from a later phase errors",
			natal: "seq_1_c1", feederPhase: "c2", consumerPhase: "c2",
			wantSeverity:  string(diagnostics.SeverityError),
			wantSubstring: "must land in the tunnel's birth phase",
		},
		{
			name:  "consumer outside the sequence warns",
			natal: "seq_1_c1", feederPhase: "c1", consumerPhase: "",
			wantSeverity:  string(diagnostics.SeverityWarning),
			wantSubstring: "outside sequence",
		},
		{
			name:  "unknown birth phase warns and skips",
			natal: "seq_1_ghost", feederPhase: "c1", consumerPhase: "c2",
			wantSeverity:  string(diagnostics.SeverityWarning),
			wantSubstring: "not found in sequence",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scene := phaseTunnelScene(tc.natal, tc.feederPhase, tc.consumerPhase)
			resp := Generate(context.Background(), Request{
				Scene:    json.RawMessage(scene),
				Language: "go",
			})
			got := phaseOrderDiags(resp)
			for _, d := range got {
				t.Logf("§7 diag [%s] %s", d.Severity, d.Message)
			}
			if tc.wantSeverity == "" {
				if len(got) != 0 {
					t.Fatalf("healthy scene produced %d §7 diagnostic(s)", len(got))
				}
				return
			}
			found := false
			for _, d := range got {
				if string(d.Severity) == tc.wantSeverity &&
					strings.Contains(d.Message, tc.wantSubstring) {
					found = true
				}
			}
			if !found {
				t.Fatalf("wanted a %s diagnostic containing %q; got %d §7 diagnostic(s)",
					tc.wantSeverity, tc.wantSubstring, len(got))
			}
		})
	}
}
