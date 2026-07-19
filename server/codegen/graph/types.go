// /ide/server/codegen/graph/types.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package graph

// types.go — Graph data structures for the codegen pipeline.
//
// The Graph is built from SceneJSON and represents the computation graph:
// nodes are devices, edges are wires, and scopes define containment (loops).
//
// Português: Estruturas de dados do grafo para o pipeline de codegen.
// O Graph é construído a partir do SceneJSON: nós são devices, arestas são
// wires, e escopos definem containment (loops).

// Node represents a device in the computation graph.
type Node struct {
	ID         string                 // device ID (e.g. "constInt_1")
	Type       string                 // device type (e.g. "StatementConstInt")
	Label      string                 // user-assigned label, may be empty
	Properties map[string]interface{} // device-specific properties
	ScopeID    string                 // ID of the containing scope ("" = global)
	Inputs     []Port                 // input ports with connections
	Outputs    []Port                 // output ports with connections
	// ExecutionOrder is the user-defined ordering hint (from "execution order: N" in
	// the method doc comment). Value 0 means unordered. Lower values run first within
	// a scope when no wire dependency exists between the devices.
	ExecutionOrder int

	// Y is the device's stage vertical position — the ONLY geometry the
	// graph carries. Function signatures order their parameters by it
	// (Fatia C, 2026-07-19): declaration order = top-to-bottom on the
	// stage, so the maker reorders by dragging. Português: A posição
	// vertical no palco — a única geometria do grafo. Assinaturas de
	// função ordenam parâmetros por ela: ordem de declaração = ordem
	// vertical; o maker reordena arrastando.
	Y float64
}

// Port represents a connector on a device with its connections.
type Port struct {
	Name      string    // port name (e.g. "inputA", "output", "stop")
	DataType  string    // wire data type (e.g. "int", "bool")
	IsOutput  bool      // true for output ports
	WireIDs   []string  // IDs of connected wires
	Connected []PortRef // connected endpoints
}

// PortRef identifies a specific port on a specific device.
type PortRef struct {
	DeviceID string
	PortName string
}

// Edge represents a wire between two ports.
type Edge struct {
	ID       string  // wire ID
	From     PortRef // output port (source)
	To       PortRef // input port (destination)
	DataType string  // data type carried by this wire
}

// Scope represents a containment scope (a loop or the global scope).
// FunctionCallType is the scene device type of a graphical-function
// INSTANCE (properties["function"] = the def name). Lives here so the
// merge engine (codegen) and the emitter (ir) share one truth without
// an import cycle. Português: O tipo de device de uma INSTÂNCIA de
// função gráfica; mora aqui para merge e emitter partilharem uma
// verdade sem ciclo de import.
const FunctionCallType = "StatementFunctionCall"

// FuncPort is one slot of a function's tunnel-derived signature.
// Português: Um slot da assinatura derivada de túneis.
type FuncPort struct {
	TunnelID string
	Name     string
	Type     string // "" = untyped (diagnosed at emit)
	Comment  string // maker's note — emitted as signature doc (Fatia A polish)
	Y        float64
}

type Scope struct {
	ID       string   // scope ID (device ID for loops, "" for global)
	ParentID string   // parent scope ID ("" for top-level)
	NodeIDs  []string // device IDs directly in this scope (not nested)

	// Sequence marks a StatementSequence scope: Cases holds the ORDERED
	// phases (values "0","1","2"…) and ALL of them run, in order — no
	// selector, no construct, pure emission order (the device is
	// semantically transparent by design: "any code with a sequencer
	// equals the same code without one; it only guarantees a clear
	// order" — Kemper, 2026-07-16). Português: Marca escopo de
	// StatementSequence — Cases carrega as FASES ordenadas e TODAS
	// rodam, em ordem; sem seletor, sem construto, pura ordem de
	// emissão.
	Sequence bool

	// Function marks a StatementFunction scope: the body emits into a
	// separate NAMED function (FunctionName), lifted above main — the
	// "made for the unforeseen" device of the embedded family. On posix
	// nothing calls it (a loud warning says so; the body stays manually
	// testable from a harness — the portability-tiers rule). Português:
	// Escopo de StatementFunction — o corpo vira função NOMEADA acima do
	// main; no posix ninguém a chama (warning barulhento).
	Function     bool
	FunctionName string

	// FuncParams / FuncReturns are the function's SIGNATURE, derived
	// from its phase-tunnels (Fatia C): LEFT-side tunnels are
	// parameters, RIGHT-side are returns (the F2 normal convention —
	// in-left/out-right; the Sequence's inversion is its own). Ordered
	// by stage Y. Name = the tunnel's label, identifier-sanitized;
	// Type = the wire-stamped concrete type. Português: A ASSINATURA da
	// função, derivada dos túneis: lado ESQUERDO = parâmetros, DIREITO
	// = retornos (convenção normal F2). Ordenados por Y. Nome = label
	// sanitizado; tipo = o carimbo concreto do fio.
	FuncParams  []FuncPort
	FuncReturns []FuncPort
	StopPort    *PortRef // for loops: the device+port connected to the stop input

	// IntervalPort is set for StatementLoopDuration scopes.
	// Points to the device+port providing the time.Duration value for
	// the time.Sleep() call emitted at the end of each loop iteration.
	//
	// Mutually exclusive with StopPort in practice: StatementLoop uses
	// StopPort (bool → break), StatementLoopDuration uses IntervalPort
	// (time.Duration → sleep).
	//
	// Português: Definido para escopos StatementLoopDuration. Aponta para o
	// device+port que fornece o time.Duration para o time.Sleep().
	IntervalPort *PortRef // for duration loops: the device+port for sleep interval

	// ConditionPort is set for StatementIfElse scopes.
	// Points to the device+port providing the bool value for the if condition.
	//
	// Português: Definido para escopos StatementIfElse. Aponta para o
	// device+port que fornece o bool para a condição do if.
	ConditionPort *PortRef

	// TrueBranchIDs and FalseBranchIDs hold the device IDs assigned to each
	// branch of a StatementIfElse scope. Populated from the device's properties
	// during graph building. The emitter uses these to split the scope's nodes
	// into two groups for if/else code generation.
	//
	// Português: IDs dos devices em cada branch do StatementIfElse. Populados
	// a partir das properties do device durante a construção do grafo.
	TrueBranchIDs  []string
	FalseBranchIDs []string

	// SelectorPort is set for StatementCase scopes that lower to a switch
	// (non-boolean selector). It points to the device+port providing the
	// value that selects which case runs. A StatementCase with a BOOLEAN
	// selector and true/false cases is lowered to an if/else scope instead
	// (ConditionPort + TrueBranchIDs/FalseBranchIDs), reusing that pipeline.
	//
	// Português: Definido para escopos StatementCase que viram switch (selector
	// não-booleano). Aponta para o device+port que fornece o valor seletor. Um
	// StatementCase com selector booleano vira escopo if/else (ConditionPort).
	SelectorPort *PortRef

	// Cases holds the ordered cases of a StatementCase scope (switch form).
	// Each case carries the literal values it matches and the device IDs
	// assigned to it; at most one case is the default (switch `default:`).
	// Populated from the device's properties during graph building.
	//
	// Português: Cases ordenados de um escopo StatementCase (forma switch).
	// Cada case tem os valores que casa e os IDs dos devices; no máximo um é o
	// default. Populado a partir das properties do device.
	Cases []CaseDef
}

// CaseDef is one branch of a StatementCase scope.
//
// A case matches when the selector satisfies a condition described by
// MatchKind over Values. When every non-default case uses a discrete kind
// ("is"/"isAnyOf"), the scope lowers to a switch; if any case uses a range or
// comparison kind, the whole scope lowers to an if/else-if chain instead (see
// ir.BuildCaseCondition and the COND_* opcodes).
//
// Português: Um ramo (case) de um escopo StatementCase. Um case casa quando o
// selector satisfaz a condição descrita por MatchKind sobre Values. Se todos
// os cases não-default forem discretos ("is"/"isAnyOf"), o escopo vira switch;
// se algum usar range ou comparação, o escopo inteiro vira uma cadeia
// if/else-if.
type CaseDef struct {
	// ID is the case's stable identifier ("stmCase_1_c2") as serialized by
	// the frontend since the family's birth. Phase-tunnel validation
	// resolves tunnelNatal against it; the collapsible-function work will
	// too. Português: Identificador estável do case, como o frontend
	// serializa desde o nascimento. A validação de túnel de fase resolve o
	// tunnelNatal por ele.
	ID string

	// Label is the case's display name from the inspector (e.g. "condição 0").
	// It carries NO execution meaning and the real emitters ignore it — only
	// the inspect-panel preview (PreviewCase / caseBodyComment) uses it, to
	// mark which branch is which inside the structural snippet. extractCases
	// need not populate it: for the preview it arrives straight from the
	// inspect form through the worker.
	//
	// Português: Nome de exibição do case vindo do inspetor ("condição 0").
	// Sem significado de execução; os emissores reais o ignoram. Só o preview
	// do painel o usa, para marcar qual ramo é qual no snippet.
	Label string

	// MatchKind selects how Values is interpreted:
	//   - "is"      : exactly Values[0]                 → switch-compatible
	//   - "isAnyOf" : any of Values                     → switch-compatible
	//   - "between" : inclusive range [Values[0],[1]]   → forces if/else-if
	//   - "gt"/"lt"/"gte"/"lte": threshold Values[0]    → forces if/else-if
	// Empty means a legacy scene predating the field; extractCases backfills
	// it to "is" or "isAnyOf" from the length of Values, so the rest of the
	// pipeline never sees "".
	MatchKind string
	Values    []string // operands; their meaning depends on MatchKind (above)
	IDs       []string // device IDs assigned to this case
	IsDefault bool     // the fallback case (switch `default:` / chain `else`); at most one
}

// Graph is the complete computation graph built from a scene.
type Graph struct {
	Nodes  map[string]*Node  // all nodes by ID
	Edges  map[string]*Edge  // all edges by wire ID
	Scopes map[string]*Scope // all scopes by ID ("" = global)
}
