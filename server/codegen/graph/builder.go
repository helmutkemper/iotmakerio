// server/codegen/graph/builder.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package graph

// builder.go — Converts SceneJSON into a computation Graph.
//
// The builder reads the flat JSON export from the IDE's scene serializer
// and constructs a directed graph with scope (containment) information.
//
// Input types mirror scene.SceneJSON but are defined locally so the codegen
// server package has zero dependency on WASM/sprite/DOM code.
//
// Português: Converte SceneJSON em um Graph de computação.
// Os tipos de entrada espelham scene.SceneJSON mas são definidos localmente
// para que o pacote codegen do servidor não dependa de WASM/sprite/DOM.

import (
	"fmt"

	"server/codegen/diagnostics"
)

// =====================================================================
//  Input types — mirror scene.SceneJSON (pure JSON, no WASM deps)
// =====================================================================

// SceneInput is the top-level input from the IDE's JSON export.
type SceneInput struct {
	Version  string        `json:"version"`
	Metadata MetadataInput `json:"metadata"`
	Devices  []DeviceInput `json:"devices"`
	Wires    []WireInput   `json:"wires"`
}

// MetadataInput is the scene-level configuration carried alongside the
// device list. It mirrors the JSON object that the IDE serializer
// produces under the "metadata" key.
//
// TargetProfile is consumed only by the C backend; the Go backend
// ignores it. The field is optional in the JSON — older scenes that
// never knew about target profiles decode it as the empty string, and
// the C backend resolves "" to its conservative default
// (ProfileArduinoUno). This makes the addition fully backward
// compatible: existing saved scenes continue to compile without
// modification.
//
// Language captures the project's fixed compile target — "c" (C99)
// or "go". It mirrors stage_files.language on the parent row.
// Today the codegen pipeline does NOT switch on this value
// (Request.Language wins because the HTTP caller dictates which
// backend to run); Language is recorded here for diagnostics and
// for future cross-checks ("a project saved as language=c is
// requesting Go output — warn?"). When the IDE saves a scene it
// always sets this to the workspace's fixed language.
//
// Português: Configuração da cena. TargetProfile é lido só pelo
// backend C; o Go ignora. Campo opcional no JSON — ausente vira ""
// e o backend C resolve "" para o perfil arduino_uno (default).
// Language registra a linguagem fixa do projeto (espelha
// stage_files.language). Hoje serve para diagnóstico; o
// Request.Language do HTTP é quem decide qual backend roda.
type MetadataInput struct {
	Density       int         `json:"density"`
	CanvasWidth   int         `json:"canvasWidth"`
	CanvasHeight  int         `json:"canvasHeight"`
	Camera        CameraInput `json:"camera"`
	Language      string      `json:"language,omitempty"`
	TargetProfile string      `json:"targetProfile,omitempty"`
}

type CameraInput struct {
	OffsetX float64 `json:"offsetX"`
	OffsetY float64 `json:"offsetY"`
	Zoom    float64 `json:"zoom"`
}

type DeviceInput struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Label       string                 `json:"label,omitempty"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	Position    PointInput             `json:"position"`
	Size        SizeInput              `json:"size"`
	OuterBBox   RectInput              `json:"outerBBox"`
	InnerBBox   *RectInput             `json:"innerBBox"`
	Connectors  []ConnectorInput       `json:"connectors"`
	Containment ContainmentInput       `json:"containment"`
}

// ConflictInput mirrors the IDE's ConflictJSON: one entry per spatial
// conflict detected by the scenegraph, describing which peer the device
// is in conflict with and what kind of geometric violation it is
// ("overlap", "straddle", "pierced_outer").
type ConflictInput struct {
	With string `json:"with"`
	Kind string `json:"kind"`
}

type PointInput struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type SizeInput struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type RectInput struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type ConnectorInput struct {
	Port               string               `json:"port"`
	DataType           string               `json:"dataType"`
	IsOutput           bool                 `json:"isOutput"`
	AcceptNotConnected bool                 `json:"acceptNotConnected"`
	Position           PointInput           `json:"position"`
	Connections        []ConnectionRefInput `json:"connections"`
}

type ConnectionRefInput struct {
	WireID       string `json:"wireId"`
	TargetDevice string `json:"targetDevice"`
	TargetPort   string `json:"targetPort"`
}

type ContainmentInput struct {
	IsContainer bool            `json:"isContainer"`
	Children    []string        `json:"children,omitempty"`
	Overlapping []string        `json:"overlapping,omitempty"`
	Parent      string          `json:"parent,omitempty"`
	Status      string          `json:"status"`
	Conflicts   []ConflictInput `json:"conflicts,omitempty"`
}

type WireInput struct {
	ID       string        `json:"id"`
	From     EndpointInput `json:"from"`
	To       EndpointInput `json:"to"`
	DataType string        `json:"dataType"`
}

type EndpointInput struct {
	Device string `json:"device"`
	Port   string `json:"port"`
}

// =====================================================================
//  Builder
// =====================================================================

// Build constructs a Graph from a SceneInput.
// Returns the graph and a list of diagnostics (empty if valid).
func Build(scene SceneInput) (*Graph, []diagnostics.Diagnostic) {
	g := &Graph{
		Nodes:  make(map[string]*Node, len(scene.Devices)),
		Edges:  make(map[string]*Edge, len(scene.Wires)),
		Scopes: make(map[string]*Scope),
	}

	var diags []diagnostics.Diagnostic

	// Create global scope
	g.Scopes[""] = &Scope{ID: "", ParentID: "", NodeIDs: make([]string, 0)}

	// Pass 1: create nodes and scopes from devices
	for _, dev := range scene.Devices {
		node := &Node{
			ID:         dev.ID,
			Type:       dev.Type,
			Label:      dev.Label,
			Properties: dev.Properties,
			Inputs:     make([]Port, 0),
			Outputs:    make([]Port, 0),
		}

		// Extract execution order from device properties.
		// The WASM device stores it as "executionOrder" (int or float64 after JSON decode).
		if v, ok := dev.Properties["executionOrder"]; ok {
			switch n := v.(type) {
			case float64:
				node.ExecutionOrder = int(n)
			case int:
				node.ExecutionOrder = n
			}
		}

		// Build port list from connectors
		for _, conn := range dev.Connectors {
			port := Port{
				Name:      conn.Port,
				DataType:  conn.DataType,
				IsOutput:  conn.IsOutput,
				WireIDs:   make([]string, 0, len(conn.Connections)),
				Connected: make([]PortRef, 0, len(conn.Connections)),
			}
			for _, ref := range conn.Connections {
				port.WireIDs = append(port.WireIDs, ref.WireID)
				port.Connected = append(port.Connected, PortRef{
					DeviceID: ref.TargetDevice,
					PortName: ref.TargetPort,
				})
			}
			if conn.IsOutput {
				node.Outputs = append(node.Outputs, port)
			} else {
				node.Inputs = append(node.Inputs, port)
			}
		}

		// Containers become scopes
		if dev.Containment.IsContainer {
			scope := &Scope{
				ID:      dev.ID,
				NodeIDs: make([]string, 0, len(dev.Containment.Children)),
			}
			g.Scopes[dev.ID] = scope
		}

		g.Nodes[dev.ID] = node
	}

	// Pass 2: assign nodes to scopes using containment.
	//
	// Every device lives in exactly one enclosing scope. We decide which
	// scope purely by looking at Containment.Parent:
	//   - Parent != ""  → the device lives inside that parent's scope.
	//   - Parent == ""  → the device lives in the global scope "".
	//
	// The Containment.IsContainer flag is orthogonal: it tells us whether
	// the device ALSO opens a new scope of its own. A Loop inside another
	// Loop is both a node in the outer Loop's scope AND the owner of its
	// own inner scope. The earlier code missed this and left nested
	// containers unattached, which produced output Go with the inner
	// `for { }` silently dropped.
	//
	// Error-status devices are still registered in their parent scope so
	// downstream reporting knows where they live; the "error" check below
	// surfaces the diagnostic to the user.
	//
	// Português: Todo device vive em exatamente um escopo, definido pelo
	// seu Parent. IsContainer é ortogonal — diz se ele abre um escopo
	// próprio. Um Loop dentro de outro Loop é ao mesmo tempo nó do escopo
	// externo e dono do escopo interno.
	for _, dev := range scene.Devices {
		node := g.Nodes[dev.ID]

		parentID := dev.Containment.Parent
		node.ScopeID = parentID

		if parentID == "" {
			g.Scopes[""].NodeIDs = append(g.Scopes[""].NodeIDs, dev.ID)
		} else if scope, ok := g.Scopes[parentID]; ok {
			scope.NodeIDs = append(scope.NodeIDs, dev.ID)
		}

		// Thread nested-container scope relationships so downstream
		// passes can walk upward through the scope tree.
		if dev.Containment.IsContainer {
			if innerScope, ok := g.Scopes[dev.ID]; ok {
				innerScope.ParentID = parentID
			}
		}

		// Surface geometric errors as codegen diagnostics. Each entry in
		// the device's Containment.Conflicts slice produces a dedicated
		// Diagnostic with the producer and the peer in Devices so the UI
		// can highlight both on the canvas. Devices with Status=="error"
		// but no Conflicts slice (older scenes that only set the flag)
		// still get a generic diagnostic so nothing is silently accepted.
		if dev.Containment.Status == "error" {
			if len(dev.Containment.Conflicts) == 0 {
				diags = append(diags, diagnostics.Diagnostic{
					Kind:     diagnostics.KindGeometric,
					Severity: diagnostics.SeverityError,
					Devices:  []string{dev.ID},
					Message:  fmt.Sprintf("%s: spatial conflict (details unavailable)", dev.ID),
				})
			} else {
				for _, c := range dev.Containment.Conflicts {
					diags = append(diags, diagnostics.Diagnostic{
						Kind:     diagnostics.KindGeometric,
						Severity: diagnostics.SeverityError,
						Devices:  []string{dev.ID, c.With},
						Message:  fmt.Sprintf("%s: %s with %s", dev.ID, c.Kind, c.With),
					})
				}
			}
		}
	}

	// Pass 3: create edges from wires
	for _, w := range scene.Wires {
		edge := &Edge{
			ID:       w.ID,
			From:     PortRef{DeviceID: w.From.Device, PortName: w.From.Port},
			To:       PortRef{DeviceID: w.To.Device, PortName: w.To.Port},
			DataType: w.DataType,
		}
		g.Edges[edge.ID] = edge
	}

	// Pass 4: resolve loop stop ports and interval ports
	for scopeID, scope := range g.Scopes {
		if scopeID == "" {
			continue // global scope has no stop/interval port
		}
		// The scope device is a loop — find its "stop" or "interval" input
		loopNode, ok := g.Nodes[scopeID]
		if !ok {
			continue
		}
		// selectorRef captures the StatementCase "selector" input (if any),
		// resolved after the loop into ConditionPort (boolean → if/else) or
		// SelectorPort (other → switch).
		//
		// Português: Captura a porta "selector" do StatementCase (se houver),
		// resolvida após o loop em ConditionPort (bool) ou SelectorPort (switch).
		var selectorRef *PortRef
		for _, input := range loopNode.Inputs {
			if input.Name == "stop" && len(input.Connected) > 0 {
				ref := input.Connected[0] // max 1 connection
				scope.StopPort = &PortRef{
					DeviceID: ref.DeviceID,
					PortName: ref.PortName,
				}
			}
			// StatementLoopDuration uses an "interval" port instead of "stop".
			// The interval port carries a time.Duration value for time.Sleep().
			//
			// Português: StatementLoopDuration usa porta "interval" em vez de "stop".
			// A porta interval carrega um time.Duration para time.Sleep().
			if input.Name == "interval" && len(input.Connected) > 0 {
				ref := input.Connected[0]
				scope.IntervalPort = &PortRef{
					DeviceID: ref.DeviceID,
					PortName: ref.PortName,
				}
			}

			// StatementIfElse uses a "condition" port (bool → if/else branching).
			//
			// Português: StatementIfElse usa porta "condition" (bool → if/else).
			if input.Name == "condition" && len(input.Connected) > 0 {
				ref := input.Connected[0]
				scope.ConditionPort = &PortRef{
					DeviceID: ref.DeviceID,
					PortName: ref.PortName,
				}
			}

			// StatementCase uses a "selector" port (typed value → switch/case).
			//
			// Português: StatementCase usa porta "selector" (valor tipado → switch).
			if input.Name == "selector" && len(input.Connected) > 0 {
				ref := input.Connected[0]
				selectorRef = &PortRef{
					DeviceID: ref.DeviceID,
					PortName: ref.PortName,
				}
			}
		}

		// Parse branch IDs from StatementIfElse properties.
		// The WASM device stores trueBranchIDs and falseBranchIDs as arrays
		// in the properties map. These are used by the emitter to split the
		// scope's nodes into two groups for if/else code generation.
		//
		// Português: Lê os IDs dos branches das properties do StatementIfElse.
		if loopNode.Type == "StatementIfElse" && loopNode.Properties != nil {
			scope.TrueBranchIDs = extractStringSlice(loopNode.Properties, "trueBranchIDs")
			scope.FalseBranchIDs = extractStringSlice(loopNode.Properties, "falseBranchIDs")
		}

		// StatementCase: a boolean selector with true/false cases lowers to an
		// if/else scope (reusing that pipeline); any other selector becomes a
		// switch scope (SelectorPort + Cases).
		//
		// Português: StatementCase — selector booleano com cases true/false vira
		// escopo if/else; qualquer outro vira switch (SelectorPort + Cases).
		if loopNode.Type == "StatementCase" && loopNode.Properties != nil {
			selType, _ := loopNode.Properties["selectorType"].(string)
			cases := extractCases(loopNode.Properties)
			if selType == "bool" {
				scope.ConditionPort = selectorRef
				for _, c := range cases {
					if caseMatchesValue(c, "true") {
						scope.TrueBranchIDs = c.IDs
					} else if caseMatchesValue(c, "false") {
						scope.FalseBranchIDs = c.IDs
					}
				}
			} else {
				scope.SelectorPort = selectorRef
				scope.Cases = cases
			}
		}
	}

	return g, diags
}

// =====================================================================
//  Graph query helpers
// =====================================================================

// GetInputSources returns the PortRefs that feed into a node's input port.
func (g *Graph) GetInputSources(nodeID, portName string) []PortRef {
	node, ok := g.Nodes[nodeID]
	if !ok {
		return nil
	}
	for _, p := range node.Inputs {
		if p.Name == portName {
			return p.Connected
		}
	}
	return nil
}

// GetOutputTargets returns the PortRefs that a node's output port feeds.
func (g *Graph) GetOutputTargets(nodeID, portName string) []PortRef {
	node, ok := g.Nodes[nodeID]
	if !ok {
		return nil
	}
	for _, p := range node.Outputs {
		if p.Name == portName {
			return p.Connected
		}
	}
	return nil
}

// ScopeOf returns the scope ID for a node.
func (g *Graph) ScopeOf(nodeID string) string {
	if node, ok := g.Nodes[nodeID]; ok {
		return node.ScopeID
	}
	return ""
}

// extractStringSlice reads a []interface{} property value as []string.
// JSON arrays decode to []interface{} with string elements — this helper
// safely converts them. Returns nil if the key is missing or not an array.
//
// Português: Lê um valor de property []interface{} como []string.
func extractStringSlice(props map[string]interface{}, key string) []string {
	raw, ok := props[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// toStringSlice converts a JSON-decoded value (expected []interface{} of
// strings) into []string. Non-string elements are skipped.
//
// Português: Converte um valor JSON ([]interface{} de strings) em []string.
func toStringSlice(raw interface{}) []string {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// extractCases parses the "cases" array from a StatementCase device's
// properties into []CaseDef. Each entry is an object with "values" and "ids"
// (arrays of strings) and an "id"; the device's "defaultCaseId" property (or a
// per-case "isDefault" flag) marks which case is the switch default.
//
// Português: Lê o array "cases" das properties do StatementCase para []CaseDef.
func extractCases(props map[string]interface{}) []CaseDef {
	raw, ok := props["cases"].([]interface{})
	if !ok {
		return nil
	}
	defaultID, _ := props["defaultCaseId"].(string)
	var out []CaseDef
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		c := CaseDef{
			Values: toStringSlice(m["values"]),
			IDs:    toStringSlice(m["ids"]),
		}
		// matchKind drives the switch-vs-if/else-if lowering. Legacy scenes
		// (saved before the field existed) omit it; infer a discrete kind from
		// the value count so they keep generating exactly the switch they did
		// before — one value is "is", several is "isAnyOf".
		c.MatchKind, _ = m["matchKind"].(string)
		if c.MatchKind == "" {
			if len(c.Values) > 1 {
				c.MatchKind = "isAnyOf"
			} else {
				c.MatchKind = "is"
			}
		}
		if id, _ := m["id"].(string); id != "" && id == defaultID {
			c.IsDefault = true
		}
		if b, ok := m["isDefault"].(bool); ok && b {
			c.IsDefault = true
		}
		out = append(out, c)
	}
	return out
}

// caseMatchesValue reports whether a case lists the given literal value.
//
// Português: Diz se um case contém o valor literal dado.
func caseMatchesValue(c CaseDef, v string) bool {
	for _, x := range c.Values {
		if x == v {
			return true
		}
	}
	return false
}
