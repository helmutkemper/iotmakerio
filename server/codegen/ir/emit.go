// server/codegen/ir/emit.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ir

// emit.go — Transforms a computation Graph into a linear IR Program.
//
// Pipeline: Graph → instance scope classification → topological sort per scope
// → BB_DECL hoisting → linear instruction emission.
//
// Key rules:
//   - Each scope is sorted independently (dependencies before dependents).
//   - Wire INTO loop: value is a snapshot (read-only in loop scope).
//   - Wire OUT OF loop: register is promoted to VAR in the parent scope.
//   - Wire internal: local to the scope, declared and used inside.
//
//   BB_DECL placement rule (var declaration):
//     If ANY method of a black-box instance is placed OUTSIDE a loop (at global
//     scope), the var declaration is hoisted to the top of main(), BEFORE the
//     loop. This guarantees the struct is visible everywhere its methods run.
//     If ALL methods of an instance are inside a loop, the var declaration goes
//     at the TOP of that loop body (first line after the opening brace).
//
//   Init → Loop ordering:
//     When a BlackBoxInit device exists in the global scope and a loop contains
//     a BlackBoxRun of the same instance, an implicit dependency edge is added:
//     Init runs before the loop starts. This is in addition to any wire
//     connections the maker drew.
//
//   executionOrder tag:
//     When nodes are not connected by wires, the executionOrder:N tag on the
//     method doc comment breaks ties in the topological sort queue. Lower N
//     runs first. Unordered nodes (N=0) run after all ordered nodes.
//
// Português:
//   Regra de posição do BB_DECL: se QUALQUER método de uma instância está
//   fora do laço (escopo global), a declaração var fica antes do laço.
//   Se TODOS os métodos estão dentro do laço, a declaração fica no topo do laço.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"server/codegen/blackbox"
	"server/codegen/diagnostics"
	"server/codegen/graph"
	"server/codegen/types"
)

// Emit takes a computation graph and produces a linear IR program,
// along with any diagnostics (scope-crossing violations, internal
// topo-sort errors, etc.). Diagnostics returned as errors block
// codegen; warnings do not.
func Emit(g *graph.Graph, bbDefs map[string]*blackbox.BlackBoxDef, variables []VariableDecl) (*Program, []diagnostics.Diagnostic) {
	e := &emitter{
		graph:              g,
		bbDefs:             bbDefs,
		variables:          variables,
		program:            &Program{},
		promoted:           make(map[string]bool),
		promotedMultiPort:  make(map[string]bool),
		emitted:            make(map[string]bool),
		bbDeclared:         make(map[string]bool),
		instanceScopeOwner: make(map[string]string),
	}
	e.synthesizeImplicitCalls()
	e.calledFunctions = map[string]bool{}
	for _, n := range g.Nodes {
		if n != nil && n.Type == graph.FunctionCallType {
			if fn, _ := n.Properties["function"].(string); fn != "" {
				e.calledFunctions[fn] = true
			}
		}
	}
	diags := e.run()
	return e.program, diags
}

type emitter struct {
	graph   *graph.Graph
	program *Program

	// bbDefs are the black-box definitions referenced by the scene, keyed
	// the same way the loader keys them: by struct name for Go devices, by
	// function name for C99 function-devices. The IR needs them to resolve
	// semantics that are not visible on the scene's wires alone — currently
	// the C99 PassThrough handle (a synthesized output port that aliases a
	// wire-type input; see resolveInput2). May be nil for scenes that use
	// only built-in primitives.
	bbDefs map[string]*blackbox.BlackBoxDef

	// variables are the user-declared project variables (GetVar/SetVar). They
	// are emitted once as zero-initialised OpVar declarations at the top of the
	// global scope (emitVariableDecls); a SetVar assigns into one and a GetVar
	// is a register alias for one. nil/empty for scenes without user variables.
	//
	// Português: Variáveis de projeto declaradas pelo usuário (GetVar/SetVar).
	// Emitidas uma vez como declarações OpVar zero-init no topo do escopo global
	// (emitVariableDecls); SetVar atribui numa, GetVar é alias de registrador.
	variables []VariableDecl

	promoted   map[string]bool // registers promoted to VAR (scope crossing out)
	emitted    map[string]bool // nodes already emitted (avoid duplicates)
	bbDeclared map[string]bool // black-box instances already declared

	// promotedMultiPort is the subset of `promoted` containing devices
	// that have more than one connected output port. They need special
	// handling: one var per port (not per device), and the producer's
	// call must use `=` instead of `:=` because the vars pre-exist.
	//
	// Single-output devices (Add, Compare, etc.) stay out of this set
	// and keep the old single-variable promotion scheme.
	//
	// Português: Subconjunto de `promoted` com devices multi-output
	// (uma var por port, chamada com `=` em vez de `:=`).
	promotedMultiPort map[string]bool

	// instanceScopeOwner maps each black-box instanceId to the scope that owns
	// its var declaration. "" = global (before the loop). Any other value = the
	// loop scope ID (var declared at loop top).
	//
	// Ownership rule: if any node of an instance has ScopeID == "" (global),
	// the instance is owned by global. Otherwise owned by its loop scope.
	instanceScopeOwner map[string]string

	// typeDiags accumulates diagnostics produced by the type-
	// compatibility pass that runs inline with emitBinOp / emitCmp.
	// Collected here rather than returned by each helper so the emit
	// call sites stay readable; consumed by run() at the end.
	//
	// Português: Diagnósticos acumulados pelo passo de compatibilidade
	// de tipos (emitBinOp / emitCmp). Consumidos no fim do run().
	typeDiags []diagnostics.Diagnostic

	// inFunctionScope guards against nested Function containers while a
	// function body is being lowered. Português: Guarda contra Function
	// aninhado durante o rebaixamento de um corpo.
	inFunctionScope bool

	// funcParamName/funcParamType — the FOURTH alias sibling (Fatia C):
	// while emitting a function body, a consumer of a PARAMETER
	// tunnel's out resolves to the parameter identifier itself (raw,
	// the folded-const channel), never piercing outward — there is no
	// outward; the caller supplies the value. Populated at emitFunction
	// entry, cleared at exit. Português: O QUARTO irmão de alias —
	// dentro do corpo, consumidor de túnel-parâmetro resolve para o
	// identificador do parâmetro (cru, canal dos consts dobrados), sem
	// atravessar — não há fora; o caller fornece o valor.
	funcParamName map[string]string
	funcParamType map[string]string

	// calledFunctions marks graphical functions that have at least one
	// instance on the stage — their "uncalled" warning is noise (the
	// call exists). Populated once at Emit entry. Português: Funções
	// gráficas com pelo menos uma instância — o aviso de "uncalled"
	// delas é ruído; populado uma vez na entrada.
	calledFunctions map[string]bool

	// convertCounter produces unique register names for CONVERT
	// instructions inserted by the type-compatibility pass. Never
	// touched by other passes — the counter is a monotonic serial
	// used only to build distinct temporaries.
	//
	// Português: Contador monotônico pra nomear registros temporários
	// emitidos por OpConvert.
	convertCounter int
}

func (e *emitter) run() []diagnostics.Diagnostic {
	var diags []diagnostics.Diagnostic

	// Step 1: Detect scope-crossing wires and promote to VAR
	e.analyzeScopeCrossings()

	// Step 1b: Classify promotions into single-port (old scheme) vs
	// multi-port (new per-port scheme). Multi-output BlackBox devices
	// consumed outside their loop used to hit a dead end here — now the
	// classifier just records the extra metadata and downstream passes
	// emit correct code.
	e.classifyScopeCrossings()

	// Step 1c: Validate control-port sources. Each loop's stop /
	// interval / condition must be produced by a device that lives
	// inside the loop (or in a descendant scope) — never in an ancestor.
	// A producer outside the loop would be evaluated once before the
	// iteration starts; the control value would never change and the
	// loop would run forever (stop=false) or not at all (stop=true).
	// The generated Go is also structurally broken: the control var
	// appears in the break expression before its assignment is emitted.
	//
	// Português: Valida as fontes de portas de controle. O produtor
	// precisa estar dentro do loop, nunca fora — senão o valor é
	// avaliado uma vez só e o loop é eterno ou nunca executa.
	// Abort only on ERROR severity — the signature-tunnel case (field
	// 2026-07-19) downgraded one branch to a WARNING, and the old
	// len>0 gate silently emitted an EMPTY program for it ("Proceeding
	// with warnings … code=0 bytes"). Warnings ride along; errors
	// still stop the press. Português: Aborta só em ERRO — o
	// rebaixamento para warning quebrou a equivalência len>0≡erro e o
	// programa saía VAZIO; warnings viajam junto, erros seguem parando
	// a prensa.
	if phaseDiags := e.validatePhaseOrders(); len(phaseDiags) > 0 {
		diags = append(diags, phaseDiags...)
		return diags
	}

	if controlDiags := e.validateControlPortSources(); len(controlDiags) > 0 {
		diags = append(diags, controlDiags...)
		hasErr := false
		for _, d := range controlDiags {
			if d.Severity == diagnostics.SeverityError {
				hasErr = true
				break
			}
		}
		if hasErr {
			return diags
		}
	}

	// Step 1d: Validate constant-collection element demands. When a
	// ConstArrayInt feeds two consumers that demand different CONCRETE
	// element types, no single declaration satisfies both (T6 decision B
	// infers the element from the consumer; slices do not convert), so
	// the scene is rejected with a clear message before any emission.
	if elemDiags := e.validateConstArrayElemConflicts(); len(elemDiags) > 0 {
		return elemDiags
	}

	// Step 2: Classify instances — determine where each BB var is declared.
	// Must run before emitScope so BB_DECL hoisting knows where to place vars.
	e.buildInstanceScopeOwners()

	// Step 2c: Emit user project-variable declarations at the very top of the
	// global scope, zero-initialised. v1 keeps every user variable global, so
	// their OpVar declarations precede all node code and stay visible to every
	// SetVar/GetVar regardless of the scope it sits in.
	//
	// Português: Emite as declarações das variáveis de projeto do usuário no
	// topo do escopo global, zero-init. v1 mantém toda variável global, então
	// suas declarações OpVar precedem o código dos nós e ficam visíveis a todo
	// SetVar/GetVar, em qualquer escopo.
	e.emitVariableDecls()

	// Step 3: Emit global scope (recursive — loops emit their contents inline)
	//
	// emitScope still returns raw strings (topo-sort internal errors).
	// These are wrapped as KindEmitterInternal diagnostics — they indicate
	// a graph the emitter cannot topologically order, usually a wire
	// cycle. Devices slice stays empty because the sorter does not track
	// which node caused the cycle.
	for _, msg := range e.emitScope("") {
		diags = append(diags, diagnostics.Diagnostic{
			Kind:     diagnostics.KindEmitterInternal,
			Severity: diagnostics.SeverityError,
			Message:  msg,
		})
	}

	// Flush type-compat diagnostics accumulated by emitBinOp / emitCmp
	// during the scope walk. These carry device IDs so the UI can
	// highlight the offending node; ordering is preserved so warnings
	// appear in the same order as the instructions that produced them.
	//
	// Português: Despeja os diagnósticos de tipo acumulados durante a
	// emissão. Carregam device IDs pra destaque na UI.
	diags = append(diags, e.typeDiags...)

	return diags
}

// =====================================================================
//  Instance scope classification
// =====================================================================

// effectiveOwnerScope walks up the scope tree from scopeID, skipping if/else
// and case scopes — which emit `{ }` branch/case blocks, NOT a var-declaration
// block — and returns the nearest enclosing scope that OWNS variable
// declarations: a loop scope, or the global scope "". A black-box instance
// used inside if/else branches (or switch cases) is therefore declared before
// that container, in the nearest loop or global ancestor, so the single shared
// instance is visible to every branch/case.
//
// Português: Sobe na árvore de escopos a partir de scopeID, pulando escopos
// if/else e case (que emitem blocos { } de branch/case, não um bloco de
// declaração de var), e retorna o escopo que DECLARA vars: um loop, ou o
// global "". Assim a instância de black-box usada dentro dos branches é
// declarada ANTES do container, visível a todos eles.
func (e *emitter) effectiveOwnerScope(scopeID string) string {
	for scopeID != "" {
		if node, ok := e.graph.Nodes[scopeID]; ok &&
			(node.Type == "StatementLoop" || node.Type == "StatementLoopDuration") {
			return scopeID // loops own their instance declarations
		}
		scope, ok := e.graph.Scopes[scopeID]
		if !ok {
			return scopeID // unknown scope — keep as-is (defensive)
		}
		scopeID = scope.ParentID // if/else, case, etc. → hoist to the parent
	}
	return ""
}

// buildInstanceScopeOwners determines the owning scope for each black-box
// instance's var declaration.
//
// Ownership rule:
//   - If ANY node of the instance is in global scope (ScopeID == ""),
//     the instance is owned by the global scope — var declared before the loop.
//   - If ALL nodes are in loop scopes, the instance is owned by the first
//     loop scope encountered — var declared at loop top.
//
// This implements the user-facing rule: "if any function runs outside the loop,
// the struct is declared outside the loop."
func (e *emitter) buildInstanceScopeOwners() {
	// Phase 1: collect all scope IDs for each instance
	instanceScopes := make(map[string]map[string]bool)
	for _, node := range e.graph.Nodes {
		if !strings.HasPrefix(node.Type, "BlackBox") {
			continue
		}
		// C99 function-devices (empty struct part) have no instance variable,
		// so they own no scope and need no BB_DECL hoisting — they are emitted
		// as BB_CALL. Skip them so they never enter instanceScopeOwner.
		if bbStructNameFromNode(node) == "" {
			continue
		}
		instanceId := e.bbInstanceId(node)
		if instanceScopes[instanceId] == nil {
			instanceScopes[instanceId] = make(map[string]bool)
		}
		instanceScopes[instanceId][e.effectiveOwnerScope(node.ScopeID)] = true
	}

	// Phase 2: assign owner based on the rule
	for instanceId, scopes := range instanceScopes {
		if scopes[""] {
			// Any node in global scope → instance is owned by global
			e.instanceScopeOwner[instanceId] = ""
		} else {
			// All nodes are in loops — pick the first scope we find.
			// In the common case (Init + Run of same component), they would be
			// in the same loop; for now we pick any scope.
			for scopeID := range scopes {
				e.instanceScopeOwner[instanceId] = scopeID
				break
			}
		}
	}
}

// inferredCollectionElem implements T6 decision B for ConstArrayInt: it
// looks at every consumer port wired to the collection's output and returns
// the CONCRETE element type they demand ("" when every consumer is the
// abstract "[]int", e.g. a Gauge). The consumer port's DataType comes from
// the scene — the WASM device registers black-box input ports with the
// authored Go type (the parser's typeString renders `values []uint16` as
// "[]uint16"), so the graph alone carries the truth and no BlackBoxDef
// lookup is needed here.
//
// Multiple consumers demanding DIFFERENT concrete elements are a maker
// error; validateConstArrayElemConflicts blocks codegen before emission,
// so this helper may simply return the first concrete candidate it finds.
//
// Português: Implementa a decisão B — retorna o tipo de elemento CONCRETO
// exigido pelos consumidores ("" se todos forem o "[]int" abstrato). O
// DataType da porta consumidora vem da cena (o WASM registra a porta da
// black-box com o tipo autoral), então o graph basta. Conflitos são
// barrados antes pela validação.
func (e *emitter) inferredCollectionElem(node *graph.Node) string {
	for _, out := range node.Outputs {
		for _, ref := range out.Connected {
			dest, ok := e.graph.Nodes[ref.DeviceID]
			if !ok {
				continue
			}
			for _, in := range dest.Inputs {
				if in.Name != ref.PortName {
					continue
				}
				elem, isColl := strings.CutPrefix(in.DataType, "[]")
				if isColl && elem != "" && elem != "int" && elem != "float" {
					return elem
				}
			}
		}
	}
	return ""
}

// validateConstArrayElemConflicts is run Step 1d: for every ConstArrayInt or
// ConstArrayFloat whose element type will be inferred from its consumers (T6
// decision B), it rejects the scene when two consumers demand DIFFERENT concrete
// element types — e.g. one black-box taking []uint16 and another taking
// []int32 wired to the same constant, or one taking []float32 and another
// []float64. There is no single declaration that satisfies both (Go slices do
// not convert), so this is a maker error the IDE must surface clearly instead
// of shipping uncompilable code. Abstract "[]int"/"[]float" consumers (Gauge &
// friends) accept anything and never conflict.
//
// Português: Passo 1d — barra a cena quando dois consumidores exigem
// elementos concretos DIFERENTES da mesma coleção (int ou float; não existe
// declaração que sirva aos dois, slice não converte). Consumidores abstratos
// ("[]int"/"[]float") aceitam qualquer coisa e nunca conflitam.
func (e *emitter) validateConstArrayElemConflicts() []diagnostics.Diagnostic {
	var diags []diagnostics.Diagnostic

	nodeIDs := make([]string, 0, len(e.graph.Nodes))
	for id := range e.graph.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	for _, id := range nodeIDs {
		node := e.graph.Nodes[id]
		if node.Type != "StatementConstArrayInt" && node.Type != "StatementConstArrayFloat" {
			continue
		}

		// Collect every DISTINCT concrete element demanded downstream,
		// remembering one demanding device per element for the message.
		demand := map[string]string{} // elem → consumer deviceID
		var order []string            // deterministic message order
		for _, out := range node.Outputs {
			for _, ref := range out.Connected {
				dest, ok := e.graph.Nodes[ref.DeviceID]
				if !ok {
					continue
				}
				for _, in := range dest.Inputs {
					if in.Name != ref.PortName {
						continue
					}
					elem, isColl := strings.CutPrefix(in.DataType, "[]")
					if !isColl || elem == "" || elem == "int" || elem == "float" {
						continue
					}
					if _, seen := demand[elem]; !seen {
						demand[elem] = ref.DeviceID
						order = append(order, elem)
					}
				}
			}
		}

		if len(demand) <= 1 {
			continue
		}

		devices := []string{node.ID}
		var parts []string
		for _, elem := range order {
			devices = append(devices, demand[elem])
			parts = append(parts, fmt.Sprintf("%s wants []%s", demand[elem], elem))
		}
		diags = append(diags, diagnostics.Diagnostic{
			Kind:     diagnostics.KindTypeMismatch,
			Severity: diagnostics.SeverityError,
			Devices:  devices,
			Message: fmt.Sprintf(
				"%s: connected devices demand different collection element types (%s) — a constant collection has a single element type, so split it into one constant per consumer",
				node.ID, strings.Join(parts, "; "),
			),
		})
	}

	return diags
}

// =====================================================================
//  Scope-crossing analysis
// =====================================================================

func (e *emitter) analyzeScopeCrossings() {
	for _, edge := range e.graph.Edges {
		// Skip control-flow ports: these wire into the loop device itself
		// (which lives in the parent scope), but they are NOT scope-crossing
		// data flows — they are consumed by the loop's own control logic.
		// Without this skip, ConstDuration inside a loop would be promoted
		// to var before the loop instead of using := inside.
		if edge.To.PortName == "stop" || edge.To.PortName == "interval" || edge.To.PortName == "condition" {
			continue
		}

		// A wire INTO a function-SIGNATURE tunnel is not an escape: the
		// return path is intra-function by construction (*out = v runs
		// inside the region), and a call-site wire into a parameter's
		// outer face passes by value at the call. The tunnel living
		// parentless at root made the ancestor test cry "escape!" and
		// promoted a file-scope twin the local then shadowed — the
		// cc -Wunused-variable voice of the shadowed-twin debt (field
		// 2026-07-19). Português: Fio PARA túnel de ASSINATURA não é
		// escape — o retorno é intra-função por construção; o túnel
		// morando na raiz fazia o teste de ancestral gritar "escape" e
		// promover o gêmeo que o local sombreava.
		if toNode, ok := e.graph.Nodes[edge.To.DeviceID]; ok && toNode != nil &&
			toNode.Type == "StatementTunnel" {
			if tp, _ := toNode.Properties["tunnelParent"].(string); tp != "" {
				if sc := e.graph.Scopes[tp]; sc != nil && sc.Function {
					continue
				}
			}
		}

		fromScope := e.graph.ScopeOf(edge.From.DeviceID)
		toScope := e.graph.ScopeOf(edge.To.DeviceID)

		if fromScope == toScope {
			continue
		}

		if e.isAncestor(toScope, fromScope) {
			e.promoted[edge.From.DeviceID] = true
		}
	}
}

func (e *emitter) isAncestor(ancestor, scope string) bool {
	for scope != "" {
		s, ok := e.graph.Scopes[scope]
		if !ok {
			return false
		}
		if s.ParentID == ancestor {
			return true
		}
		scope = s.ParentID
	}
	return ancestor == ""
}

// validateScopeCrossings inspects every device that analyzeScopeCrossings
// marked for promotion and refuses the codegen request when any of them
// has more than one output port.
//
// Why:
//
//	Promotion rewrites "x := f()" (local) into "var x T; ... x = f()" so
//	the value survives across a scope boundary. The current emitter does
//	this by treating the producer as a single named register with one
//	type, which works for Add/Compare/Gauge/Const (one value out). It
//	breaks when the producer is a BlackBox method that returns multiple
//	values — e.g. apds9960.Run() → (clear, red, green, blue uint16) —
//	because we would need FOUR distinct variables declared before the
//	loop, and "=" (not ":=") inside, with each downstream consumer
//	reading its specific port. Per-port promotion is future work; until
//	that lands, we detect the pattern and emit a clear diagnostic.
//
// The message names both ends of the crossing and the loop scope in
// between, so the user can either move the downstream device inside
// the loop or restructure the data flow.
//
// Português:
//
//	Valida as promoções decididas em analyzeScopeCrossings. Se o device
//	promovido tem múltiplos ports de saída, o emitter atual não sabe
//	representá-lo (precisaria promover cada port como variável separada,
//	feature futura). Reporta erro claro explicando quais devices estão
//	envolvidos e como consertar a cena.
//
// classifyScopeCrossings looks at every device that analyzeScopeCrossings
// marked for promotion and separates them into two groups:
//
//   - Single-output devices (e.g. StatementAdd, StatementGreaterThan)
//     stay in `promoted` and use the original single-variable scheme:
//     one `var X int` before the loop, one `X = ...` inside.
//
//   - Multi-output devices (e.g. BlackBoxRun:APDS9960 with four outputs)
//     are ALSO marked in `promotedMultiPort`. Downstream passes generate
//     one `var {id}_{port} T` per connected port, and the device's call
//     site uses `=` instead of `:=`.
//
// This replaces the previous behavior where multi-output promotions
// produced an error and refused codegen. When a device is consumed
// across a loop boundary, users now get working code instead of a
// diagnostic. The tradeoff: the emitter must be consistent about
// reading promoted BB vars by `{instanceId}_{port}` everywhere.
//
// This function never returns diagnostics — promotions are always
// acceptable now. Kept as a separate pass to keep the classification
// logic readable and to leave a seam for a future validator (e.g.
// refuse when a specific port has an unrepresentable type).
//
// Português:
//
//	Classifica as promoções em single-port (comportamento antigo) ou
//	multi-port (nova promoção por port). Antes esse passo bloqueava
//	multi-port com erro; agora aceita e prepara a emissão.
func (e *emitter) classifyScopeCrossings() {
	if len(e.promoted) == 0 {
		return
	}
	for id := range e.promoted {
		node, ok := e.graph.Nodes[id]
		if !ok {
			continue
		}
		if e.connectedOutputCount(node) > 1 {
			e.promotedMultiPort[id] = true
		}
	}
}

// connectedOutputCount returns the number of output ports on `node`
// that have at least one wire attached. Unconnected outputs don't
// require a var declaration, so they don't influence whether the
// promotion is single- or multi-port.
//
// Português: Quantas portas de saída estão realmente conectadas —
// portas sem fio não precisam de var.
func (e *emitter) connectedOutputCount(node *graph.Node) int {
	n := 0
	for _, out := range node.Outputs {
		if len(out.Connected) > 0 {
			n++
		}
	}
	return n
}

// validateControlPortSources walks every loop scope and checks that
// the device wired to each control port (stop / interval / condition)
// lives inside that loop's scope — or a descendant of it, in the case
// of nested loops.
//
// The wires that are rejected:
//
//	┌────── scope="" (global) ──────┐
//	│                               │
//	│   stmGreaterThan_1 ───┐       │
//	│                       │       │
//	│  ┌── scope=stmLoop_1 ─│──┐    │
//	│  │                    ▼  │    │
//	│  │               stop of │    │
//	│  │               stmLoop_1    │
//	│  └────────────────────────┘   │
//	└───────────────────────────────┘
//
// This configuration tells the maker's intent ("evaluate the condition
// once, before the loop") but the runtime can't honor it: the loop
// would either break on the first iteration or never break at all.
// The generated Go is also structurally broken — the emitter topologically
// orders the comparison AFTER the loop that already references its
// output, producing code that Go refuses to compile.
//
// The valid inverse — a producer INSIDE the loop feeding a control port
// of that same loop (or of a nested loop) — is fine and is what makers
// actually mean when they build a stop condition from live sensor data.
//
// Português: Valida que a fonte de cada porta de controle está DENTRO
// do loop — nunca fora. Fora, a condição seria avaliada uma vez só e
// o loop rodaria infinito ou nem executaria.
func (e *emitter) validateControlPortSources() []diagnostics.Diagnostic {
	var diags []diagnostics.Diagnostic

	// Iterate scopes in a deterministic order so test output is stable.
	scopeIDs := make([]string, 0, len(e.graph.Scopes))
	for id := range e.graph.Scopes {
		if id == "" {
			continue
		}
		scopeIDs = append(scopeIDs, id)
	}
	sort.Strings(scopeIDs)

	for _, scopeID := range scopeIDs {
		scope := e.graph.Scopes[scopeID]
		loopNode, ok := e.graph.Nodes[scopeID]
		if !ok {
			continue
		}

		// This rule is LOOP-only: a loop re-evaluates its control ports every
		// iteration, so their producers must live inside the loop (a producer
		// outside is computed once and frozen). An if/else evaluates its
		// condition ONCE when control reaches the branch, so a condition source
		// outside the branch — e.g. a comparator wired into the condition port,
		// the normal way to build a branch test — is valid and must not be
		// flagged. Skip if/else scopes entirely.
		if loopNode.Type == "StatementIfElse" {
			continue
		}

		type portCheck struct {
			name string // "stop", "interval", "condition"
			ref  *graph.PortRef
		}
		checks := []portCheck{
			{"stop", scope.StopPort},
			{"interval", scope.IntervalPort},
			{"condition", scope.ConditionPort},
		}

		for _, c := range checks {
			if c.ref == nil {
				continue
			}
			producerID := c.ref.DeviceID
			producerScope := e.graph.ScopeOf(producerID)

			// Accept: producer in the loop itself or in a descendant
			// scope (nested loops are fine — their emitted block runs
			// every iteration of the outer loop).
			if producerScope == scopeID {
				continue
			}
			if e.isAncestor(scopeID, producerScope) {
				// producerScope is a descendant of scopeID → fine.
				continue
			}

			// EXCEPTION (field 2026-07-19): a FUNCTION-SIGNATURE tunnel
			// is a legitimate outside source — it is border furniture
			// of the FUNCTION and cannot be "moved inside the loop";
			// stop fed by a parameter is the embedded classic
			// while(!flag). Downgrade to a WARNING that tells the real
			// semantics: the value is a call-time constant. Português:
			// EXCEÇÃO — túnel de ASSINATURA é fonte externa legítima
			// (mobília da borda da FUNÇÃO, não movível para o loop);
			// stop por parâmetro é o clássico while(!flag). Rebaixa
			// para AVISO contando a semântica real: valor constante
			// durante a chamada.
			if pn, ok := e.graph.Nodes[producerID]; ok && pn != nil &&
				pn.Type == "StatementTunnel" {
				if tp, _ := pn.Properties["tunnelParent"].(string); tp != "" {
					if sc := e.graph.Scopes[tp]; sc != nil && sc.Function {
						diags = append(diags, diagnostics.Diagnostic{
							Kind:     diagnostics.KindLoopConstantStop,
							Severity: diagnostics.SeverityWarning,
							Devices:  []string{scopeID, producerID},
							Scope:    scopeID,
							Message: fmt.Sprintf(
								"%s: %s is fed by function parameter %q — a call-time constant, so the loop either runs forever or not at all for a given call",
								scopeID, c.name, producerID),
						})
						continue
					}
				}
			}

			// Reject: producer lives in the loop's ancestor chain or
			// in a sibling scope. Either way, from this loop's point
			// of view the producer is "outside".
			diags = append(diags, diagnostics.Diagnostic{
				Kind:     diagnostics.KindMissingConnection,
				Severity: diagnostics.SeverityError,
				Devices:  []string{scopeID, producerID},
				Scope:    scopeID,
				Message: fmt.Sprintf(
					"%s: %s port is wired to %s, which sits outside the loop — move %s inside %s so it is re-evaluated each iteration",
					scopeID, c.name, producerID, producerID, describeLoop(loopNode),
				),
			})
		}
	}

	return diags
}

// describeLoop returns a short human string naming the loop kind to
// make diagnostic messages friendlier than raw node types.
//
// Português: Retorna nome amigável do tipo de loop para diagnósticos.
func describeLoop(node *graph.Node) string {
	switch node.Type {
	case "StatementLoop":
		return "the loop"
	case "StatementLoopDuration":
		return "the duration loop"
	case "StatementIfElse":
		return "the branch"
	default:
		return node.ID
	}
}

// sortedKeys returns the keys of a string-set in lexical order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// synthesizeImplicitCalls makes an OUTER-WIRED function definition its
// own call-site (LabVIEW semantics, field 2026-07-20): wires reaching
// a signature tunnel's OUTER face from outside the function become the
// arguments/consumers of a synthetic FunctionCallType node in the
// trunk — the existing call emitter then does everything (args, zero
// fill, result vars, liveness), and the "uncalled" warning dies
// naturally because the scan below sees the synthetic call. Mapping is
// exact via FuncPort.TunnelID; outer return consumers are REWIRED to
// the synthetic node so they resolve to the call's result registers.
// Português: Definição fiada por fora vira o próprio call-site — fios
// na face EXTERNA dos túneis de assinatura viram argumentos/
// consumidores de um node de chamada sintético no tronco; o emissor de
// chamadas existente faz o resto, e o aviso "uncalled" morre porque o
// scan vê a chamada sintética. Mapeamento exato por FuncPort.TunnelID;
// consumidores externos do retorno são REFIADOS para o node sintético.
func (e *emitter) synthesizeImplicitCalls() {
	inside := func(devID, fnScope string) bool {
		s := e.graph.ScopeOf(devID)
		return s == fnScope || e.isAncestor(fnScope, s)
	}
	for fnID, scope := range e.graph.Scopes {
		if scope == nil || !scope.Function {
			continue
		}
		callID := fnID + "_selfcall"
		outer := false

		// The emitter's truth is the PORT INDEX (node.Inputs/Outputs),
		// not the Edges map (field 2026-07-21: the call's arguments
		// were invisible — "parameter not connected — passing the
		// typed zero" with the wires plainly on the stage). The
		// synthetic node is born WITH its ports, and outer consumers
		// are rewired IN THEIR OWN index. Edges-map entries ride along
		// for topo ordering. Português: A verdade do emissor é o
		// ÍNDICE DE PORTAS — o node sintético nasce COM as portas e os
		// consumidores externos são refiados no índice DELES; o mapa
		// Edges acompanha para a ordenação.
		var callInputs, callOutputs []graph.Port

		// Parameters: outer feeds become the synthetic call's inputs.
		for _, p := range scope.FuncParams {
			port := graph.Port{Name: p.Name, DataType: p.Type}
			for _, edge := range e.graph.Edges {
				if edge == nil || edge.To.DeviceID != p.TunnelID || edge.To.PortName != "in" {
					continue
				}
				if inside(edge.From.DeviceID, fnID) {
					continue
				}
				outer = true
				port.Connected = append(port.Connected, edge.From)
				key := callID + "_arg_" + p.Name + "_" + edge.From.DeviceID
				e.graph.Edges[key] = &graph.Edge{
					From: edge.From,
					To:   graph.PortRef{DeviceID: callID, PortName: p.Name},
				}
			}
			callInputs = append(callInputs, port)
		}

		// Returns: outer consumers rewire onto the synthetic call — in
		// the Edges map AND in the consumer's own port index.
		for _, r := range scope.FuncReturns {
			port := graph.Port{Name: r.Name, DataType: r.Type, IsOutput: true}
			for _, edge := range e.graph.Edges {
				if edge == nil || edge.From.DeviceID != r.TunnelID || edge.From.PortName != "out" {
					continue
				}
				if inside(edge.To.DeviceID, fnID) {
					continue
				}
				outer = true
				port.Connected = append(port.Connected, edge.To)
				if consumer, ok := e.graph.Nodes[edge.To.DeviceID]; ok && consumer != nil {
					for ci := range consumer.Inputs {
						for pi := range consumer.Inputs[ci].Connected {
							ref := &consumer.Inputs[ci].Connected[pi]
							if ref.DeviceID == r.TunnelID && ref.PortName == "out" {
								ref.DeviceID = callID
								ref.PortName = r.Name
							}
						}
					}
				}
				edge.From = graph.PortRef{DeviceID: callID, PortName: r.Name}
			}
			callOutputs = append(callOutputs, port)
		}

		if !outer {
			continue
		}
		e.graph.Nodes[callID] = &graph.Node{
			ID:   callID,
			Type: graph.FunctionCallType,
			Properties: map[string]interface{}{
				"function": scope.FunctionName,
			},
			Inputs:  callInputs,
			Outputs: callOutputs,
		}
		if root, ok := e.graph.Scopes[""]; ok && root != nil {
			root.NodeIDs = append(root.NodeIDs, callID)
		}
	}
}

// phaseOrder partitions a function scope's nodes by the "phases"
// metadata on the function node and returns them phase-by-phase (topo
// inside each). Unassigned nodes join the FIRST phase. Returns ok=false
// when the metadata is absent/unusable — caller keeps plain topo.
// Backward cross-phase wires (producer in a LATER phase than its
// consumer) are rejected with an error diagnostic: data flows forward.
// Português: Particiona os nós do escopo-função pelo metadado "phases"
// e devolve fase-a-fase (topo dentro de cada); não-atribuídos entram na
// PRIMEIRA. ok=false sem metadado — chamador mantém o topo puro. Fio
// cruzando fases para trás é erro: dado flui para frente.
// validatePhaseOrders enforces the forward-flow law for every phased
// function scope: a wire's producer phase must not exceed its
// consumer's — data flows to the same or a later phase. Runs as a
// run() pre-pass beside the stop rule. Português: Lei do fluxo para
// frente em todo escopo-função com fases; pre-pass do run() ao lado da
// regra do stop.
func (e *emitter) validatePhaseOrders() []diagnostics.Diagnostic {
	var diags []diagnostics.Diagnostic
	for scopeID, scope := range e.graph.Scopes {
		if scope == nil || !scope.Function {
			continue
		}
		phaseOf, ok := e.phaseAssignment(scopeID, scope.NodeIDs)
		if !ok {
			continue
		}
		for _, edge := range e.graph.Edges {
			fp, fok := phaseOf[edge.From.DeviceID]
			tp, tok := phaseOf[edge.To.DeviceID]
			if fok && tok && fp > tp {
				diags = append(diags, diagnostics.Diagnostic{
					Kind:     diagnostics.KindPhaseOrder,
					Severity: diagnostics.SeverityError,
					Devices:  []string{edge.From.DeviceID, edge.To.DeviceID},
					Scope:    scopeID,
					Message: fmt.Sprintf(
						"%s: %s (phase %d) feeds %s (phase %d) — data must flow to the same or a later phase",
						scopeID, edge.From.DeviceID, fp, edge.To.DeviceID, tp),
				})
			}
		}
	}
	return diags
}

// phaseAssignment maps each in-scope node to its phase index per the
// "phases" metadata; unassigned nodes map to 0. ok=false when the
// metadata is absent/unusable. Português: Mapeia nó→índice de fase;
// não-atribuídos vão para 0; ok=false sem metadado.
func (e *emitter) phaseAssignment(scopeID string, nodeIDs []string) (map[string]int, bool) {
	node, exists := e.graph.Nodes[scopeID]
	if !exists || node == nil {
		return nil, false
	}
	raw, _ := node.Properties["phases"].(string)
	if raw == "" {
		return nil, false
	}
	type phaseWire struct {
		ID  string   `json:"id"`
		IDs []string `json:"ids"`
	}
	var phases []phaseWire
	if err := json.Unmarshal([]byte(raw), &phases); err != nil || len(phases) == 0 {
		return nil, false
	}
	inScope := map[string]bool{}
	for _, id := range nodeIDs {
		inScope[id] = true
	}
	phaseOf := map[string]int{}
	for i, ph := range phases {
		for _, id := range ph.IDs {
			if inScope[id] {
				phaseOf[id] = i
			}
		}
	}
	for _, id := range nodeIDs {
		if _, assigned := phaseOf[id]; !assigned {
			phaseOf[id] = 0
		}
	}
	return phaseOf, true
}

func (e *emitter) phaseOrder(scopeID string, nodeIDs []string) ([]string, bool) {
	phaseOf, ok := e.phaseAssignment(scopeID, nodeIDs)
	if !ok {
		return nil, false
	}
	nPhases := 0
	for _, p := range phaseOf {
		if p+1 > nPhases {
			nPhases = p + 1
		}
	}
	buckets := make([][]string, nPhases)
	for _, id := range nodeIDs {
		buckets[phaseOf[id]] = append(buckets[phaseOf[id]], id)
	}

	var out []string
	for i := range buckets {
		part, errs := e.topoSort(buckets[i])
		if len(errs) > 0 {
			return nil, false
		}
		out = append(out, part...)
	}
	return out, true
}

// =====================================================================
//  Scope emission (recursive)
// =====================================================================

func (e *emitter) emitScope(scopeID string) []string {
	scope, ok := e.graph.Scopes[scopeID]
	if !ok {
		return []string{fmt.Sprintf("scope %q not found", scopeID)}
	}

	sorted, errs := e.topoSort(scope.NodeIDs)
	if len(errs) > 0 {
		return errs
	}

	// L2 (2026-07-20): a FUNCTION whose node carries the "phases"
	// metadata emits PHASE BY PHASE — topo order inside each phase,
	// phases in their declared order — so the stage's phase model IS
	// the generated code's execution order. Scenes without phases keep
	// the plain topo order untouched. Português: Função com metadado
	// "phases" emite FASE A FASE — topo dentro de cada, fases na ordem
	// declarada; cenas sem fases seguem intocadas.
	if scope.Function {
		if phased, ok := e.phaseOrder(scopeID, scope.NodeIDs); ok {
			sorted = phased
		}
	}

	if scopeID == "" {
		// Global scope: emit promoted vars, then hoist BB_DECL for global instances.
		// Both must precede the sorted nodes so all variables exist before use.
		e.emitPromotedVars()
		e.emitBBDeclsForScope("", sorted)
	}

	if scopeID != "" {
		e.program.Append(Instruction{Op: OpLoopBegin, Dest: scopeID})
		// [COMMENT] container comments land on the LOOP_BEGIN itself, so the
		// backends print them right above the loop statement.
		// Português: Comentários de container caem no próprio LOOP_BEGIN,
		// então os backends os imprimem logo acima do laço.
		e.stampScopeComment(scopeID)
		// Loop scope: hoist BB_DECL for loop-local instances at loop top,
		// immediately after LOOP_BEGIN and before any other instructions.
		e.emitBBDeclsForScope(scopeID, sorted)
	}

	for _, nodeID := range sorted {
		if e.emitted[nodeID] {
			continue
		}
		e.emitted[nodeID] = true
		e.emitNode(nodeID)
	}

	if scopeID != "" && scope.StopPort != nil {
		e.program.Append(Instruction{
			Op:   OpBreakIf,
			Args: []string{e.resolveInput2(scope.StopPort.DeviceID, scope.StopPort.PortName)},
		})
	}

	// StatementLoopDuration: emit time.Sleep(interval) at the end of each
	// loop iteration. The interval is a time.Duration register provided by
	// the connected ConstDuration (or any device outputting time.Duration).
	//
	// Português: StatementLoopDuration: emite time.Sleep(interval) no final
	// de cada iteração do loop. O intervalo é um registro time.Duration
	// fornecido pelo ConstDuration conectado.
	if scopeID != "" && scope.IntervalPort != nil {
		e.program.Append(Instruction{
			Op:   OpSleep,
			Args: []string{e.resolveInput2(scope.IntervalPort.DeviceID, scope.IntervalPort.PortName)},
		})
	}

	if scopeID != "" {
		e.program.Append(Instruction{Op: OpLoopEnd, Dest: scopeID})
	}

	return nil
}

// emitIfElse emits an if/else block for a StatementIfElse scope.
//
// The scope's children are split into two groups based on TrueBranchIDs and
// FalseBranchIDs. Each group is topologically sorted and emitted independently.
//
// IR output:
//
//	IF_BEGIN %condition
//	  (true branch nodes)
//	IF_ELSE
//	  (false branch nodes)
//	IF_END
//
// The Go backend detects which branches have content and emits the appropriate
// form: if/else, if-only, or if-negated.
//
// Português: Emite um bloco if/else para um escopo StatementIfElse.
// Os filhos são divididos em dois grupos pelos TrueBranchIDs e FalseBranchIDs.
func (e *emitter) emitIfElse(scopeID string) {
	scope, ok := e.graph.Scopes[scopeID]
	if !ok {
		e.program.Warn("scope %q not found for IfElse", scopeID)
		return
	}

	// Resolve condition source
	condArg := ""
	if scope.ConditionPort != nil {
		condArg = e.resolveInput2(scope.ConditionPort.DeviceID, scope.ConditionPort.PortName)
	}

	// Build sets for branch membership
	trueSet := make(map[string]bool, len(scope.TrueBranchIDs))
	for _, id := range scope.TrueBranchIDs {
		trueSet[id] = true
	}
	falseSet := make(map[string]bool, len(scope.FalseBranchIDs))
	for _, id := range scope.FalseBranchIDs {
		falseSet[id] = true
	}

	// Split scope nodes into true and false groups
	var trueNodes, falseNodes []string
	for _, nodeID := range scope.NodeIDs {
		if trueSet[nodeID] {
			trueNodes = append(trueNodes, nodeID)
		} else if falseSet[nodeID] {
			falseNodes = append(falseNodes, nodeID)
		} else {
			// Node not assigned to any branch — warn but include in true by default
			e.program.Warn("device %s inside IfElse %s but not assigned to any branch, defaulting to true", nodeID, scopeID)
			trueNodes = append(trueNodes, nodeID)
		}
	}

	// Sort each branch independently
	sortedTrue, trueErrs := e.topoSort(trueNodes)
	if len(trueErrs) > 0 {
		for _, err := range trueErrs {
			e.program.Warn("true branch sort: %s", err)
		}
	}
	sortedFalse, falseErrs := e.topoSort(falseNodes)
	if len(falseErrs) > 0 {
		for _, err := range falseErrs {
			e.program.Warn("false branch sort: %s", err)
		}
	}

	// Emit IF_BEGIN with metadata indicating which branches have content.
	// The Go backend uses this to choose between: if, if-negated, or if-else.
	hasTrueContent := len(sortedTrue) > 0
	hasFalseContent := len(sortedFalse) > 0

	e.program.Append(Instruction{
		Op:   OpIfBegin,
		Dest: scopeID,
		Args: []string{condArg},
		Meta: map[string]string{
			"hasTrue":  fmt.Sprintf("%v", hasTrueContent),
			"hasFalse": fmt.Sprintf("%v", hasFalseContent),
		},
	})

	// Emit true branch nodes
	for _, nodeID := range sortedTrue {
		if e.emitted[nodeID] {
			continue
		}
		e.emitted[nodeID] = true
		e.emitNode(nodeID)
	}

	// Emit IF_ELSE separator
	e.program.Append(Instruction{Op: OpIfElse, Dest: scopeID})

	// Emit false branch nodes
	for _, nodeID := range sortedFalse {
		if e.emitted[nodeID] {
			continue
		}
		e.emitted[nodeID] = true
		e.emitNode(nodeID)
	}

	// Emit IF_END
	e.program.Append(Instruction{Op: OpIfEnd, Dest: scopeID})
}

// emitCase emits a switch block for a StatementCase scope whose selector is
// non-boolean (boolean selectors are lowered to if/else by the builder and
// handled by emitIfElse). The scope's children are grouped by case membership
// (CaseDef.IDs); each group is topologically sorted and emitted between the
// switch labels. The default case (if any) is emitted last.
//
// IR output:
//
//	SWITCH_BEGIN %selector
//	CASE_LABEL v1 v2
//	  (case nodes)
//	CASE_LABEL v3
//	  (case nodes)
//	DEFAULT_LABEL
//	  (default nodes)
//	SWITCH_END
//
// Português: Emite um bloco switch para um escopo StatementCase com selector
// não-booleano. Os filhos são agrupados por case (CaseDef.IDs), ordenados
// topologicamente e emitidos entre os labels. O default (se houver) vai por último.
// emitSequence lowers a StatementSequence scope: N ordered phases, ALL of
// them run. It emits NO construct — phase bodies are concatenated in phase
// order, each phase topoSorted internally (Kemper's transparency law,
// 2026-07-16: "any code with a sequencer equals the same code without one;
// it only guarantees a clear order"). One diagnostic is unique to it: a wire
// flowing BACKWARD between phases (producer in a later phase than its
// consumer) is an order violation — the graph promises 0→1→2 and a
// backward wire cannot be honoured.
//
// Português: Rebaixa um StatementSequence — N fases ordenadas, TODAS rodam.
// Não emite construto: os corpos concatenam na ordem das fases, cada fase
// com topoSort interno (a lei da transparência). Diagnóstico exclusivo:
// fio PARA TRÁS entre fases (produtor em fase posterior à do consumidor)
// é violação de ordem.
// hasOutgoingWire reports whether ANY edge leaves the given device. Math
// devices have a single output port, so device-level granularity is
// exact for them; callers with multi-output devices should not use this.
// Português: Diz se ALGUM fio sai do device — exato para devices de
// saída única, como os de matemática.
func (e *emitter) hasOutgoingWire(deviceID string) bool {
	// Wires are DUAL-SOURCED (field lesson 2026-07-16, the gt_1 case):
	// scenes carry an explicit "wires" array AND per-port "connections"
	// on the consumer — real exports fill both, but connection-only
	// scenes exist (tests, old exports). Edges materialise only from the
	// array, so checking Edges alone false-flags a wired device. Scan
	// both, like resolveInput's world does. Português: Fios têm DUAS
	// fontes — o array "wires" e as "connections" por porta do
	// consumidor; Edges nasce só do array, então checar só Edges acusa
	// falso positivo. Varre as duas fontes.
	for _, edge := range e.graph.Edges {
		if edge.From.DeviceID == deviceID {
			return true
		}
	}
	for _, n := range e.graph.Nodes {
		for _, port := range n.Inputs {
			for _, ref := range port.Connected {
				if ref.DeviceID == deviceID {
					return true
				}
			}
		}
	}
	return false
}

// tunnelRealSource follows a phase-tunnel chain upstream to the first
// non-tunnel producer and returns that PortRef. ok=false when the chain
// dangles (an unwired tunnel.in) or cycles. The cycle guard is not
// paranoia: the scene file is CLIENT input — the server must never spin
// on a hostile or buggy wire loop, so a revisited tunnel warns once and
// resolves as unconnected.
//
// Português: Segue a cadeia de túneis rio acima até o primeiro produtor
// que não é túnel. ok=false quando a cadeia está solta (tunnel.in sem
// fio) ou cicla. A guarda de ciclo não é paranoia: o scene file é input
// do CLIENTE — o servidor jamais pode girar em loop num fio hostil ou
// bugado; túnel revisitado avisa uma vez e resolve como desconectado.
func (e *emitter) tunnelRealSource(node *graph.Node) (graph.PortRef, bool) {
	seen := map[string]bool{}
	cur := node
	for {
		// A function-PARAMETER tunnel is a REAL source — the caller
		// supplies its value; piercing past it walks into the void
		// (the identity-function lesson, 2026-07-19: param wired
		// straight to return resolved "no feed"). Stop here and let
		// the fourth-alias resolution answer with the parameter name.
		// Português: Túnel-PARÂMETRO é fonte REAL — o caller fornece;
		// atravessá-lo caminha para o vazio (lição da função
		// identidade). Para aqui; o quarto irmão responde com o nome.
		if _, isParam := e.funcParamName[cur.ID]; isParam {
			return graph.PortRef{DeviceID: cur.ID, PortName: "out"}, true
		}
		if seen[cur.ID] {
			e.program.Warn("%s: tunnel cycle — treated as unconnected", cur.ID)
			return graph.PortRef{}, false
		}
		seen[cur.ID] = true
		srcs := e.graph.GetInputSources(cur.ID, "in")
		if len(srcs) == 0 {
			return graph.PortRef{}, false
		}
		next, ok := e.graph.Nodes[srcs[0].DeviceID]
		if !ok || next.Type != "StatementTunnel" {
			// An unknown node id falls through here on purpose:
			// resolveInput2 renders it as "%<id>" — the same contract a
			// direct wire to it would have. Português: Id desconhecido
			// cai aqui de propósito — mesmo contrato de um fio direto.
			return srcs[0], true
		}
		cur = next
	}
}

// skipTunnel emits NOTHING for a phase-tunnel. The tunnel is FRONT-END
// FURNITURE (Kemper, 2026-07-18: "eu vejo túnel como sendo uma peça de
// front end... não vejo o túnel como algo que vá para o código"): pure
// wire routing across a Sequence phase border. Consumers resolve
// THROUGH it to the real source (resolveInput2 / resolveInputType), so
// the generated program reads as if the wire never stopped — no
// synthetic variable, nothing to mis-order. This supersedes the
// 2026-07-16 "honest line" design (tunnel_1 = <source>): being
// parentless border furniture, those lines fell OUTSIDE every phase and
// were appended AFTER their consumers — use-before-declaration, field
// 2026-07-18. The maker-mid-build warnings survive; only the code went
// silent.
//
// Português: Túnel não emite NADA — é peça de FRONT-END (Kemper,
// 2026-07-18), puro roteamento de fio pela borda de fase. Consumidores
// resolvem ATRAVÉS dele até a fonte real; o programa gerado lê como se
// o fio nunca tivesse parado — sem variável sintética, nada para
// desordenar. Supersede a "linha honesta" de 2026-07-16: móvel de borda
// não tem fase, então aquelas linhas caíam DEPOIS dos consumidores —
// uso-antes-da-declaração, campo 2026-07-18. Os avisos de obra
// continuam; só o código silenciou.
// emitFunctionCall lowers a graphical-function instance into a real
// call: inputs resolve per the signature's parameter order (an
// unconnected input warns and passes the typed zero — the binop
// policy); each return gets a destination register named
// <instance>_<return>, with liveness flags so the Go backend can use
// "_" for unconsumed returns while C99 declares scratch out-params.
// Português: Rebaixa uma instância em chamada real — entradas resolvem
// na ordem dos parâmetros (desconectada avisa e passa o zero tipado);
// cada retorno ganha registrador <instância>_<retorno> com bandeira de
// vivacidade (Go usa "_" nos não-consumidos; C99 declara rascunhos).
func (e *emitter) emitFunctionCall(node *graph.Node) {
	fnName, _ := node.Properties["function"].(string)
	var scope *graph.Scope
	for _, sc := range e.graph.Scopes {
		if sc != nil && sc.Function && sc.FunctionName == fnName {
			scope = sc
			break
		}
	}
	if scope == nil {
		e.program.Warn("%s: graphical function %q not found — call skipped", node.ID, fnName)
		return
	}
	args := make([]string, 0, len(scope.FuncParams))
	for _, p := range scope.FuncParams {
		// resolveInput2 takes the SOURCE (the misread that cost the
		// identity test): look the source up first. Português:
		// resolveInput2 recebe a FONTE — buscá-la primeiro.
		v := ""
		if srcs := e.graph.GetInputSources(node.ID, p.Name); len(srcs) > 0 {
			v = e.resolveInput2(srcs[0].DeviceID, srcs[0].PortName)
		}
		if v == "" {
			e.program.Warn("%s.%s: parameter not connected — passing the typed zero", node.ID, p.Name)
			v = zeroLiteralFor(p.Type)
		}
		args = append(args, v)
	}
	dests := make([]string, 0, len(scope.FuncReturns))
	types := make([]string, 0, len(scope.FuncReturns))
	live := make([]string, 0, len(scope.FuncReturns))
	for _, r := range scope.FuncReturns {
		dests = append(dests, node.ID+"_"+r.Name)
		types = append(types, r.Type)
		alive := "0"
		for _, edge := range e.graph.Edges {
			if edge != nil && edge.From.DeviceID == node.ID && edge.From.PortName == r.Name {
				alive = "1"
				break
			}
		}
		live = append(live, alive)
	}
	e.program.Append(Instruction{Op: OpCall, Args: args, Meta: map[string]string{
		"fn":    fnName,
		"dests": strings.Join(dests, ","),
		"types": strings.Join(types, ","),
		"live":  strings.Join(live, ","),
	}})
}

// zeroLiteralFor — the typed zero for an unconnected call input.
// Português: O zero tipado para entrada de chamada desconectada.
func zeroLiteralFor(typ string) string {
	switch typ {
	case "float":
		return "0.0"
	case "bool":
		return "false"
	case "string":
		return "\"\""
	default:
		return "0"
	}
}

func (e *emitter) skipTunnel(node *graph.Node) {
	// SIGNATURE tunnels (parent scope is a Function) are exempt from
	// the plumbing warnings: their OUTER faces — the parameter's .in
	// mouth, the return's .out — belong to the CALLER, unwired by
	// design while call sites don't exist (field 2026-07-19: a fully
	// healthy my_function warned "not connected" on both, read as
	// ghosts). The INNER problems that matter are already ERRORS in
	// the Fatia C validation: untyped slot, feedless return.
	// Português: Túneis de ASSINATURA são isentos dos avisos de
	// encanamento — as faces EXTERNAS pertencem ao CALLER, sem fio por
	// desenho enquanto call sites não existem. Os problemas INTERNOS
	// que importam já são ERROS na validação da Fatia C.
	if parent, _ := node.Properties["tunnelParent"].(string); parent != "" {
		if sc := e.graph.Scopes[parent]; sc != nil && sc.Function {
			return
		}
	}
	if len(e.graph.GetInputSources(node.ID, "in")) == 0 {
		e.program.Warn("%s.in: not connected — consumers of this tunnel read nothing", node.ID)
	}
	if !e.hasOutgoingWire(node.ID) {
		e.program.Warn("%s.out: not connected — the tunnel leads nowhere yet", node.ID)
	}
}

// funcNameRe validates a Function container's name: emitted VERBATIM
// into both targets (doctrine 1), so it must be a C identifier.
// Português: O nome sai verbatim — precisa ser identificador C.
// encodePorts flattens a signature group into Meta form: "n1:t1,n2:t2".
// Português: Achata um grupo da assinatura na forma do Meta.
func encodePorts(ports []graph.FuncPort) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, p.Name+":"+p.Type)
	}
	return strings.Join(parts, ",")
}

// encodePortDocs JSON-encodes the ports' comments, aligned by index;
// "" when none has a comment. Português: Comentários das portas em
// JSON, alinhados por índice; "" quando nenhum tem.
func encodePortDocs(ports []graph.FuncPort) string {
	any := false
	docs := make([]string, len(ports))
	for i, p := range ports {
		docs[i] = p.Comment
		if p.Comment != "" {
			any = true
		}
	}
	if !any {
		return ""
	}
	b, _ := json.Marshal(docs)
	return string(b)
}

// ValidFunctionName reports whether name is a legal C identifier — the
// single truth the emitter enforces, exported so the My Items save
// path judges by the same law. Português: Se o nome é identificador C
// legal — a verdade única do emitter, exportada para o save de My
// Items julgar pela mesma lei.
func ValidFunctionName(name string) bool { return funcNameRe.MatchString(name) }

var funcNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// emitFunction lowers a StatementFunction scope: the body between
// OpFuncBegin/OpFuncEnd is lifted OUT of main by each backend into
// `func <name>()` / `static void <name>(void)` — the linkage axis's new
// emit muscle (ARDUINO_TARGET §3, slice 2). On this slice nothing calls
// the function; a WARNING says so, loudly, by the portability-tiers
// rule. Values crossing the boundary ride the existing VAR promotion —
// the backends place VAR declarations at FILE scope whenever a program
// contains functions, so both sides can see them.
//
// Português: Rebaixa StatementFunction — o corpo entre FuncBegin/End é
// içado do main pelos backends para uma função nomeada. Nesta fatia
// ninguém a chama (warning barulhento). Cruzamentos viajam pela promoção
// a VAR existente; com funções no programa, os backends declaram VARs em
// escopo de arquivo.
func (e *emitter) emitFunction(scopeID string) {
	scope, ok := e.graph.Scopes[scopeID]
	if !ok || !scope.Function {
		e.program.Warn("function scope %s not found or not a function", scopeID)
		return
	}
	if e.inFunctionScope {
		e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
			Kind:     diagnostics.KindFunctionNested,
			Severity: diagnostics.SeverityError,
			Devices:  []string{scopeID},
			Message: fmt.Sprintf(
				"%s: a Function container cannot live inside another Function — C has no nested functions; move it to the top level",
				scopeID),
		})
		return
	}
	name := scope.FunctionName
	if !funcNameRe.MatchString(name) {
		e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
			Kind:     diagnostics.KindFunctionNameInvalid,
			Severity: diagnostics.SeverityError,
			Devices:  []string{scopeID},
			Message: fmt.Sprintf(
				"%s: function name %q is not a valid identifier (letters, digits, underscore; cannot start with a digit)",
				scopeID, name),
		})
		return
	}

	// ── Tunnel-derived signature (Fatia C, 2026-07-19) ──
	// Validate the slots before anything emits: every parameter and
	// return needs a concrete type (the chameleon's stamp), and names
	// must be unique across the whole signature. Português: Valida os
	// slots antes de emitir: todo parâmetro/retorno precisa de tipo
	// concreto, e os nomes são únicos na assinatura inteira.
	sigOK := true
	seenNames := map[string]string{}
	for _, group := range [][]graph.FuncPort{scope.FuncParams, scope.FuncReturns} {
		for _, p := range group {
			if p.Type == "" {
				e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
					Kind:     diagnostics.KindFunctionSignature,
					Severity: diagnostics.SeverityError,
					Devices:  []string{p.TunnelID},
					Message: fmt.Sprintf(
						"%s: signature slot %q (tunnel %s) has no type — wire something to it so the chameleon stamps",
						name, p.Name, p.TunnelID),
				})
				sigOK = false
			}
			if other, dup := seenNames[p.Name]; dup {
				e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
					Kind:     diagnostics.KindFunctionSignature,
					Severity: diagnostics.SeverityError,
					Devices:  []string{p.TunnelID, other},
					Message: fmt.Sprintf(
						"%s: signature name %q is used by two tunnels (%s, %s) — rename one",
						name, p.Name, other, p.TunnelID),
				})
				sigOK = false
			}
			seenNames[p.Name] = p.TunnelID
		}
	}
	if !sigOK {
		return
	}

	sigMeta := map[string]string{}
	if s := encodePorts(scope.FuncParams); s != "" {
		sigMeta["params"] = s
	}
	if s := encodePorts(scope.FuncReturns); s != "" {
		sigMeta["returns"] = s
	}
	// Port docs travel as JSON arrays (free text — the n:t,n:t encoding
	// cannot carry commas/colons safely). Aligned with params/returns
	// order. Português: Docs de porta viajam como arrays JSON (texto
	// livre); alinhados com a ordem de params/returns.
	if s := encodePortDocs(scope.FuncParams); s != "" {
		sigMeta["pdoc"] = s
	}
	if s := encodePortDocs(scope.FuncReturns); s != "" {
		sigMeta["rdoc"] = s
	}

	e.program.Append(Instruction{Op: OpFuncBegin, Dest: name, Meta: sigMeta})
	e.inFunctionScope = true

	// Seat the parameters for the fourth-alias resolution.
	// Português: Assenta os parâmetros para a resolução do quarto irmão.
	e.funcParamName = map[string]string{}
	e.funcParamType = map[string]string{}
	for _, p := range scope.FuncParams {
		e.funcParamName[p.TunnelID] = p.Name
		e.funcParamType[p.TunnelID] = p.Type
	}

	sorted, errs := e.topoSort(scope.NodeIDs)
	for _, err := range errs {
		e.program.Warn("function %s sort: %s", name, err)
	}
	// L2: the function's OWN body walk honors the phase order — this
	// is the path the dispatch actually takes. Português: O walk
	// próprio do corpo honra a ordem de fases — o caminho real.
	if phased, ok := e.phaseOrder(scopeID, scope.NodeIDs); ok {
		sorted = phased
	}
	for _, nodeID := range sorted {
		if e.emitted[nodeID] {
			continue
		}
		e.emitted[nodeID] = true
		e.emitNode(nodeID)
	}

	// ── Returns: each RIGHT tunnel's feed, resolved through the
	// transparency machinery, becomes one return value — in stage-Y
	// order. A feedless return is an error: the function promises a
	// value it never computes. Português: Cada túnel da direita vira um
	// valor de retorno (ordem de Y); retorno sem alimentação é erro.
	if len(scope.FuncReturns) > 0 {
		args := make([]string, 0, len(scope.FuncReturns))
		names := make([]string, 0, len(scope.FuncReturns))
		types := make([]string, 0, len(scope.FuncReturns))
		retOK := true
		for _, r := range scope.FuncReturns {
			v := e.resolveInput2(r.TunnelID, "in")
			if v == "" {
				e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
					Kind:     diagnostics.KindFunctionSignature,
					Severity: diagnostics.SeverityError,
					Devices:  []string{r.TunnelID},
					Message: fmt.Sprintf(
						"%s: return %q (tunnel %s) has no feed — wire a value into it",
						name, r.Name, r.TunnelID),
				})
				retOK = false
				continue
			}
			args = append(args, v)
			names = append(names, r.Name)
			types = append(types, r.Type)
		}
		if retOK {
			e.program.Append(Instruction{Op: OpReturn, Args: args, Meta: map[string]string{
				"names": strings.Join(names, ","),
				"types": strings.Join(types, ","),
			}})
		}
	}

	e.funcParamName = nil
	e.funcParamType = nil
	e.inFunctionScope = false
	e.program.Append(Instruction{Op: OpFuncEnd})

	// Slice 2 is posix-only: nothing calls the function on this target.
	// Loud by decision — see KindFunctionUncalled's doctrine.
	if e.calledFunctions[name] {
		return
	}
	e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
		Kind:     diagnostics.KindFunctionUncalled,
		Severity: diagnostics.SeverityWarning,
		Devices:  []string{scopeID},
		Message: fmt.Sprintf(
			"%s: function %q has no caller on this target — its logic never runs here (it stays callable from your own code)",
			scopeID, name),
	})
}

func (e *emitter) emitSequence(scopeID string) {
	scope, ok := e.graph.Scopes[scopeID]
	if !ok || !scope.Sequence {
		e.program.Warn("sequence scope %s not found or not a sequence", scopeID)
		return
	}

	// Membership: phase index per node; strays default to phase 0 with a
	// warning (mirror of the Case policy). Português: Fase por nó; sem
	// fase declarada cai na 0 com aviso, espelhando o Case.
	phaseOf := map[string]int{}
	groups := make([][]string, len(scope.Cases))
	for pi, c := range scope.Cases {
		for _, id := range c.IDs {
			phaseOf[id] = pi
		}
	}
	for _, nodeID := range scope.NodeIDs {
		pi, assigned := phaseOf[nodeID]
		if !assigned {
			e.program.Warn(
				"device %s inside Sequence %s not assigned to any phase, defaulting to phase 0",
				nodeID, scopeID)
			pi = 0
			phaseOf[nodeID] = 0
		}
		if len(groups) == 0 {
			groups = append(groups, nil)
		}
		groups[pi] = append(groups[pi], nodeID)
	}

	// The order-violation sweep: any edge whose producer lives in a LATER
	// phase than its consumer breaks the 0→1→2 promise. Error severity —
	// the maker drew a contradiction; emission proceeds so every violation
	// is reported in one pass, but the result must not ship.
	// Português: Varredura de violação — produtor em fase posterior à do
	// consumidor quebra a promessa 0→1→2.
	for _, edge := range e.graph.Edges {
		fromPhase, fromIn := phaseOf[edge.From.DeviceID]
		toPhase, toIn := phaseOf[edge.To.DeviceID]
		if !fromIn || !toIn {
			continue // crosses the sequence boundary — hoisting's business
		}
		if fromPhase > toPhase {
			e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
				Kind:     diagnostics.KindSequenceOrderViolation,
				Severity: diagnostics.SeverityError,
				Devices:  []string{edge.From.DeviceID, edge.To.DeviceID},
				Message: fmt.Sprintf(
					"%s (phase %d) feeds %s (phase %d) — a wire cannot flow backward through a Sequence; move the producer to phase %d or earlier",
					edge.From.DeviceID, fromPhase, edge.To.DeviceID, toPhase, toPhase),
			})
		}
	}

	// Emission: phases in order, each topoSorted; no wrapper construct.
	for pi := range groups {
		sorted, errs := e.topoSort(groups[pi])
		for _, err := range errs {
			e.program.Warn("sequence phase %d sort: %s", pi, err)
		}
		for _, nodeID := range sorted {
			if e.emitted[nodeID] {
				continue
			}
			e.emitted[nodeID] = true
			e.emitNode(nodeID)
		}
	}
}

func (e *emitter) emitCase(scopeID string) {
	scope, ok := e.graph.Scopes[scopeID]
	if !ok {
		e.program.Warn("scope %q not found for Case", scopeID)
		return
	}
	if len(scope.Cases) == 0 {
		e.program.Warn("Case %s has no cases, skipping", scopeID)
		return
	}

	// Resolve selector source.
	selArg := ""
	if scope.SelectorPort != nil {
		selArg = e.resolveInput2(scope.SelectorPort.DeviceID, scope.SelectorPort.PortName)
	}

	// Map each device ID to its case index, and find the default case index.
	caseOf := make(map[string]int)
	defaultIdx := -1
	for ci, c := range scope.Cases {
		if c.IsDefault {
			defaultIdx = ci
		}
		for _, id := range c.IDs {
			caseOf[id] = ci
		}
	}

	// Group scope nodes by case, preserving scope.NodeIDs order. Unassigned
	// nodes fall into the default case if one exists, otherwise the first case.
	groups := make([][]string, len(scope.Cases))
	for _, nodeID := range scope.NodeIDs {
		ci, ok := caseOf[nodeID]
		if !ok {
			if defaultIdx >= 0 {
				ci = defaultIdx
			} else {
				ci = 0
			}
			e.program.Warn("device %s inside Case %s not assigned to any case, defaulting to case %d", nodeID, scopeID, ci)
		}
		groups[ci] = append(groups[ci], nodeID)
	}

	// emitGroup topologically sorts and emits the body nodes of one case. It
	// is shared by both lowerings below.
	emitGroup := func(ci int) {
		sorted, errs := e.topoSort(groups[ci])
		for _, err := range errs {
			e.program.Warn("case sort: %s", err)
		}
		for _, nodeID := range sorted {
			if e.emitted[nodeID] {
				continue
			}
			e.emitted[nodeID] = true
			e.emitNode(nodeID)
		}
	}

	// Choose the lowering. A switch `case` label only accepts discrete
	// constants in both Go and C, so the switch form is viable only when every
	// non-default case is discrete (is/isAnyOf). A single range or comparison
	// case (between/gt/lt/gte/lte) forces the whole Case onto the if/else-if
	// chain. The default case never affects the decision — it is the switch
	// `default:` or the chain's trailing `else` either way.
	//
	// The decision lives in UseSwitchLowering so codegen.ValidateCases reaches
	// the same verdict and can never disagree with this emitter about which
	// form the cases take.
	useSwitch := UseSwitchLowering(scope.Cases)

	if useSwitch {
		e.program.Append(Instruction{
			Op:   OpSwitchBegin,
			Dest: scopeID,
			Args: []string{selArg},
		})
		// Non-default cases in declared order.
		for ci, c := range scope.Cases {
			if c.IsDefault {
				continue
			}
			e.program.Append(Instruction{
				Op:   OpCaseLabel,
				Dest: scopeID,
				Args: c.Values,
			})
			emitGroup(ci)
		}
		// Default case last (switch `default:`), if any.
		if defaultIdx >= 0 {
			e.program.Append(Instruction{Op: OpDefaultLabel, Dest: scopeID})
			emitGroup(defaultIdx)
		}
		e.program.Append(Instruction{Op: OpSwitchEnd, Dest: scopeID})
		return
	}

	// if/else-if chain. Each COND_LABEL carries the resolved selector as
	// Args[0] and the case operands as Args[1:]; the backend assembles the
	// boolean expression via ir.BuildCaseCondition and renders the chain as
	// flat if / } else if / } else / }. Declared order is preserved and is
	// significant: ranges may overlap, so the first matching branch wins.
	// Branches that can never match (a case fully shadowed by earlier ones,
	// or an empty `between` range) are reported as diagnostics by
	// codegen.ValidateCases — the codegen is the authority on reachability,
	// not the overlay.
	e.program.Append(Instruction{Op: OpCondBegin, Dest: scopeID})
	// [COMMENT] same container rule as loops: the comment rides COND_BEGIN
	// and prints right above the if/switch.
	// Português: Mesma regra de container dos loops: o comentário viaja no
	// COND_BEGIN e imprime logo acima do if/switch.
	e.stampScopeComment(scopeID)
	for ci, c := range scope.Cases {
		if c.IsDefault {
			continue
		}
		args := make([]string, 0, len(c.Values)+1)
		args = append(args, selArg)
		args = append(args, c.Values...)
		e.program.Append(Instruction{
			Op:   OpCondLabel,
			Dest: scopeID,
			Args: args,
			Meta: map[string]string{"matchKind": c.MatchKind},
		})
		emitGroup(ci)
	}
	// Default case becomes the trailing `else`, if any.
	if defaultIdx >= 0 {
		e.program.Append(Instruction{Op: OpCondDefault, Dest: scopeID})
		emitGroup(defaultIdx)
	}
	e.program.Append(Instruction{Op: OpCondEnd, Dest: scopeID})
}

// emitBBDeclsForScope emits BB_DECL instructions for all black-box instances
// whose var declaration belongs to scopeID (as determined by buildInstanceScopeOwners).
//
// This is called BEFORE the main node loop in emitScope so that 'var X Struct'
// always appears at the top of its owning scope, regardless of where the Init
// or Run nodes fall in topological order.
//
// sorted is used to determine which instances appear in this scope and to
// preserve their relative declaration order (first-encountered wins).
func (e *emitter) emitBBDeclsForScope(scopeID string, sorted []string) {
	seen := make(map[string]bool)

	tryEmit := func(nodeID string) {
		node, ok := e.graph.Nodes[nodeID]
		if !ok {
			return
		}
		if !strings.HasPrefix(node.Type, "BlackBox") {
			return
		}
		// C99 function-devices (empty struct part) declare no instance var;
		// they are emitted as BB_CALL. (buildInstanceScopeOwners already omits
		// them, so the owner lookup below would skip them too — this explicit
		// guard documents the intent and is defence in depth.)
		if bbStructNameFromNode(node) == "" {
			return
		}
		instanceId := e.bbInstanceId(node)
		if seen[instanceId] {
			return
		}
		seen[instanceId] = true

		owner, exists := e.instanceScopeOwner[instanceId]
		if !exists || owner != scopeID {
			return
		}

		if e.bbDeclared[instanceId] {
			return
		}
		e.bbDeclared[instanceId] = true

		structName := bbStructNameFromNode(node)
		e.program.Append(Instruction{
			Op:   OpBBDecl,
			Dest: instanceId,
			Meta: map[string]string{"struct": structName},
		})
	}

	// Pass 1: instances whose nodes live directly in this scope, in topological
	// order (first-encountered wins).
	for _, nodeID := range sorted {
		tryEmit(nodeID)
	}

	// Pass 2: instances OWNED by this scope but whose nodes reside in descendant
	// if/else or case scopes. Those scopes do not declare their own vars, so
	// effectiveOwnerScope routed ownership up to this scope — but `sorted` lists
	// only THIS scope's nodes, so the descendant nodes are not in it. Scan every
	// black-box node in a stable id order to find them; the owner/declared
	// guards ensure each instance is emitted exactly once, here, before its
	// container.
	var bbNodeIDs []string
	for id, node := range e.graph.Nodes {
		if strings.HasPrefix(node.Type, "BlackBox") {
			bbNodeIDs = append(bbNodeIDs, id)
		}
	}
	sort.Strings(bbNodeIDs)
	for _, nodeID := range bbNodeIDs {
		tryEmit(nodeID)
	}
}

// bbStructNameFromNode extracts the struct name from any BlackBox node type.
//
// Node type format:
//
//	"BlackBoxInit:APDS9960"          → struct = "APDS9960"
//	"BlackBoxRun:APDS9960"           → struct = "APDS9960"
//	"BlackBoxLog:APDS9960"           → struct = "APDS9960"
//
// The struct name is everything after the first colon.
//
// Português: Extrai o nome do struct de qualquer tipo de node BlackBox.
// O nome do struct é tudo após o primeiro dois-pontos.
func bbStructNameFromNode(node *graph.Node) string {
	idx := strings.Index(node.Type, ":")
	if idx < 0 {
		return node.Type
	}
	return node.Type[idx+1:]
}

// bbMethodNameFromNode extracts the method name from a non-Init BlackBox node.
//
// "BlackBoxRun:APDS9960" → "Run"
// "BlackBoxLog:APDS9960" → "Log"
//
// Português: Extrai o nome do método de um node BlackBox não-Init.
func bbMethodNameFromNode(node *graph.Node) string {
	// Strip the "BlackBox" prefix, then take everything up to the colon.
	withoutPrefix := strings.TrimPrefix(node.Type, "BlackBox")
	idx := strings.Index(withoutPrefix, ":")
	if idx < 0 {
		return withoutPrefix
	}
	return withoutPrefix[:idx]
}

// callbackRefTypePrefix is the scene device-type prefix of a C99 callback
// REFERENCE device — the "ƒ" variant of a callback handler. The full type is
// "CallbackRef:<fn>" (e.g. "CallbackRef:displayWrite"). It is deliberately NOT
// a "BlackBox..." type: a callback reference has no struct instance, no method,
// and emits no call — it only names a function to be passed by address into a
// consumer's callback parameter. The WASM factory that creates the device must
// use this exact prefix. See the duality section of docs/CODEGEN_C99_CALLBACKS.md.
const callbackRefTypePrefix = "CallbackRef:"

// isCallbackRefNode reports whether a node is a C99 callback reference device.
func isCallbackRefNode(node *graph.Node) bool {
	return strings.HasPrefix(node.Type, callbackRefTypePrefix)
}

// callbackRefFuncName returns the referenced C function name of a callback
// reference node ("CallbackRef:displayWrite" → "displayWrite"), or "" when the
// node is not a callback reference device.
func callbackRefFuncName(node *graph.Node) string {
	if !isCallbackRefNode(node) {
		return ""
	}
	return strings.TrimPrefix(node.Type, callbackRefTypePrefix)
}

// emitPromotedVars writes the `var` declarations for every device
// flagged by analyzeScopeCrossings. The emission form depends on the
// device's port fan-out:
//
//   - Single-output device: one `var X T` where X is the device's ID
//     and T is inferred from the single output's data type. The IR
//     dest is just "%deviceID".
//
//   - Multi-output device (BlackBox methods like Run that return
//     several values, or hypothetical native devices with >1 wired
//     output): one `var {deviceID}_{portName} T` per CONNECTED output,
//     keyed off each port's own data type. The IR dest is
//     "%deviceID:portName" so the backend resolves it to the same
//     `{goIdent}_{port}` name that resolveInput2 uses at the consumer
//     side — keeping producer and consumer in sync without a side table.
//
// Unconnected ports are skipped: they wouldn't be referenced anywhere
// so a `var _` would be dead code.
//
// Português:
//
//	Emite `var` por device promovido. Single-port: uma var só.
//	Multi-port: uma var por port conectado, com nome composto
//	{deviceID}_{portName} batendo exatamente com o que o consumidor
//	resolve via resolveInput2.
func (e *emitter) emitPromotedVars() {
	ids := make([]string, 0, len(e.promoted))
	for id := range e.promoted {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		node, ok := e.graph.Nodes[id]
		if !ok {
			continue
		}

		// Constant collections are HOISTED whole instead of VAR+ASSIGN
		// promoted (T6 "içamento"): a fixed C array cannot be reassigned,
		// and the value is a compile-time literal, so the full CONST_ARRAY
		// declaration lands here in the parent scope. emitConstArray stays
		// silent for promoted collection nodes — lexical scoping makes the
		// name visible inside the loop AND at the outer consumer.
		//
		// Português: Coleções constantes são içadas inteiras (array fixo
		// em C não reatribui); a declaração completa sai aqui no escopo
		// pai e o emitter do escopo original fica mudo.
		if strings.HasPrefix(node.Type, "StatementConstArray") {
			e.program.Append(e.buildConstArrayInstruction(node))
			continue
		}

		if !e.promotedMultiPort[id] {
			// Single-port promotion: original behavior.
			dataType := e.inferOutputType(node)
			e.program.Append(Instruction{
				Op:   OpVar,
				Dest: id,
				Type: dataType,
				Args: []string{zeroValue(dataType)},
			})
			continue
		}

		// Multi-port promotion: one var per connected output port.
		// We visit outputs in a stable order so the generated Go is
		// deterministic. The dest field carries the compound form
		// "deviceID:portName" so goOperand/goIdent produce the same
		// name the consumers already generate.
		portNames := make([]string, 0, len(node.Outputs))
		portType := make(map[string]string, len(node.Outputs))
		for _, out := range node.Outputs {
			if len(out.Connected) == 0 {
				continue
			}
			portNames = append(portNames, out.Name)
			dt := out.DataType
			if dt == "" {
				dt = e.inferOutputType(node)
			}
			portType[out.Name] = dt
		}
		sort.Strings(portNames)

		// Use the BlackBox instance id when applicable so the name
		// matches the compound register "%instanceId:port" that
		// resolveInput2 emits for BlackBox producers. For native
		// devices the device id itself is the register prefix.
		prefix := id
		if strings.HasPrefix(node.Type, "BlackBox") {
			prefix = e.bbInstanceId(node)
		}

		for _, name := range portNames {
			e.program.Append(Instruction{
				Op:   OpVar,
				Dest: prefix + ":" + name,
				Type: portType[name],
				Args: []string{zeroValue(portType[name])},
			})
		}
	}
}

// =====================================================================
//  Node emission
// =====================================================================

func (e *emitter) emitNode(nodeID string) {
	node, ok := e.graph.Nodes[nodeID]
	if !ok {
		return
	}

	// [COMMENT] the maker's Inspect comment rides the node's FIRST emitted
	// instruction as Meta["comment"]; the backends prefix it as `// ` lines.
	// A deferred stamp survives the early returns inside the dispatch, and
	// nodes that emit nothing (callback references) naturally skip.
	// Português: O comentário do Inspect do maker viaja na PRIMEIRA
	// instrução emitida do node como Meta["comment"]; os backends o
	// prefixam como linhas `// `. O defer sobrevive aos returns dentro do
	// dispatch, e nodes que não emitem nada (referências de callback) pulam
	// naturalmente.
	before := len(e.program.Instructions)
	defer e.stampNodeComment(node, before)

	switch {
	case isCallbackRefNode(node):
		// C99 callback REFERENCE device (the "ƒ" variant of a callback handler):
		// referenced, never executed, so it emits no IR. Its address (the
		// function name) is passed into the consumer's callback parameter,
		// resolved by callbackHandlerName when the consumer's BB_CALL is built;
		// the function body is inlined by the ANSI C backend from the CALLABLE
		// variant's def. See the duality section of docs/CODEGEN_C99_CALLBACKS.md.
		return
	case strings.HasPrefix(node.Type, "BlackBox") && bbStructNameFromNode(node) == "":
		// C99 standalone function-device: the scene type is "BlackBox<fn>:"
		// with an empty struct part. This is a free function call, not a
		// struct method — emitted as BB_CALL, never BB_DECL/BB_METHOD. Must
		// be checked BEFORE the Init/method cases so a C99 function literally
		// named "Init" ("BlackBoxInit:") is still treated as a free function.
		// See docs/CODEGEN_C99_STAGE.md §4.2.
		e.emitBlackBoxCall(node)
	case strings.HasPrefix(node.Type, "BlackBoxInit:"):
		e.emitBlackBoxInit(node)
	case strings.HasPrefix(node.Type, "BlackBox") && !strings.HasPrefix(node.Type, "BlackBoxInit:"):
		// Any BlackBox node that is not Init is a generic named method call.
		// This covers BlackBoxRun:X, BlackBoxLog:X, BlackBoxStep:X, etc.
		// The method name is extracted from the node type at emit time.
		//
		// Português: Qualquer node BlackBox que não é Init é uma chamada de
		// método genérico nomeado (Run, Log, Step, etc.).
		e.emitBlackBoxMethod(node)
	case node.Type == "StatementConstInt":
		e.emitConst(node, "int")
	case node.Type == "StatementConstFloat":
		e.emitConstFloat(node)
	case node.Type == "StatementConstString":
		e.emitConstString(node)
	case node.Type == "StatementConstArrayInt",
		node.Type == "StatementConstArrayFloat",
		node.Type == "StatementConstArrayString":
		// Three sibling devices, one emitter: each device exports its own
		// fixed "elementType" property (int / float32|float64 / string), so
		// emitConstArray stays fully parametric. Mirrors the scalar const
		// family (ConstInt / ConstFloat / ConstString are separate devices).
		e.emitConstArray(node)
	case node.Type == "StatementDataFile",
		node.Type == "StatementDataText":
		// Maker-data devices: one embedded byte array per INSTANCE — see
		// emitDataBlob. Português: Devices de dados do maker: um array de
		// bytes embutido por INSTÂNCIA.
		e.emitDataBlob(node)
	case node.Type == "StatementBool":
		e.emitConstBool(node)
	case node.Type == "StatementGetVarInt",
		node.Type == "StatementGetVarFloat",
		node.Type == "StatementGetVarString":
		// A GetVar emits no instruction: its output is a register alias for
		// the project variable it names (resolved in resolveInput2), so a
		// consumer reads the variable directly with no intermediate copy.
		// Português: GetVar não emite instrução — sua saída é alias do
		// registrador da variável que nomeia (resolvido em resolveInput2);
		// o consumidor lê a variável direto, sem cópia.
	case node.Type == "StatementSetVarInt":
		e.emitSetVar(node, "int")
	case node.Type == "StatementSetVarFloat":
		e.emitSetVar(node, "float")
	case node.Type == "StatementSetVarString":
		e.emitSetVar(node, "string")
	case node.Type == "StatementAdd":
		e.emitBinOp(node, OpAdd)
	case node.Type == "StatementSub":
		e.emitBinOp(node, OpSub)
	case node.Type == "StatementMul":
		e.emitBinOp(node, OpMul)
	case node.Type == "StatementDiv":
		e.emitBinOp(node, OpDiv)
	case node.Type == "StatementIndexInt":
		e.emitIndex(node, "int")
	case node.Type == "StatementIndexFloat":
		e.emitIndex(node, "float")
	case node.Type == "StatementIndexString":
		e.emitIndex(node, "string")
	case node.Type == "StatementPrintInt":
		e.emitPrint(node, "int")
	case node.Type == "StatementPrintFloat":
		e.emitPrint(node, "float")
	case node.Type == "StatementPrintString":
		e.emitPrint(node, "string")
	case node.Type == "StatementPrintBool":
		e.emitPrint(node, "bool")
	case node.Type == "StatementPrintByte":
		e.emitPrint(node, "byte")
	case node.Type == "StatementPrintByteArray":
		e.emitPrint(node, "[]byte")
	case node.Type == "StatementEqualTo":
		e.emitCmp(node, OpCmpEQ)
	case node.Type == "StatementNotEqualTo":
		e.emitCmp(node, OpCmpNE)
	case node.Type == "StatementLessThan":
		e.emitCmp(node, OpCmpLT)
	case node.Type == "StatementLessThanOrEqualTo":
		e.emitCmp(node, OpCmpLE)
	case node.Type == "StatementGreaterThan":
		e.emitCmp(node, OpCmpGT)
	case node.Type == "StatementGreaterThanOrEqualTo":
		e.emitCmp(node, OpCmpGE)
	case node.Type == "StatementLoop":
		e.emitScope(node.ID)
	case node.Type == "StatementLoopDuration":
		e.emitScope(node.ID)
	case node.Type == "StatementSequence":
		e.emitSequence(node.ID)
	case node.Type == "StatementFunction":
		e.emitFunction(node.ID)
	case node.Type == graph.FunctionCallType:
		e.emitFunctionCall(node)
	case node.Type == "StatementTunnel":
		e.skipTunnel(node)
	case node.Type == "StatementIfElse":
		e.emitIfElse(node.ID)
	case node.Type == "StatementCase":
		// A boolean StatementCase was lowered to an if/else scope by the
		// builder (ConditionPort set); reuse emitIfElse. Any other selector
		// emits a switch.
		//
		// Português: StatementCase booleano foi rebaixado a if/else pelo
		// builder (ConditionPort definido); reusa emitIfElse. Os demais viram switch.
		if sc := e.graph.Scopes[node.ID]; sc != nil && sc.ConditionPort != nil {
			e.emitIfElse(node.ID)
		} else {
			e.emitCase(node.ID)
		}
	case node.Type == "StatementConstDuration":
		e.emitConst(node, "time.Duration")
	case node.Type == "StatementGauge":
		e.emitGauge(node)
	default:
		e.program.Warn("unknown device type: %s (id: %s)", node.Type, node.ID)
	}
}

// =====================================================================
//  Black-box emission
// =====================================================================

// emitBlackBoxInit emits BB_PROP + BB_INIT for a BlackBoxInit device.
//
// Note: BB_DECL is NOT emitted here. It is always pre-hoisted to the top
// of the owning scope by emitBBDeclsForScope(), called before the node loop
// in emitScope(). This ensures 'var X Struct' always precedes any use of X,
// regardless of topological order within the scope.
//
// Connected outputs:
//
//	Only output ports that have at least one downstream connection are
//	captured as named variables in the generated code. Unconnected outputs
//	(including optional error returns that the maker chose not to wire) are
//	emitted as the blank identifier '_'. When ALL outputs are unconnected,
//	the Init() call is emitted as a bare statement with no LHS at all.
//
//	This information is encoded in the BB_INIT instruction as:
//	  Meta["connectedOutputs"] = "portName1,portName2,..."
//	The Go backend reads this to decide per-port whether to use a name or '_'.
//
// Português:
//
//	Apenas portas de saída com pelo menos uma conexão downstream são capturadas
//	como variáveis nomeadas. Saídas não conectadas (incluindo erros opcionais)
//	são emitidas como o identificador em branco '_'. Quando TODAS as saídas
//	estão desconectadas, a chamada Init() é emitida sem LHS algum.
func (e *emitter) emitBlackBoxInit(node *graph.Node) {
	structName := strings.TrimPrefix(node.Type, "BlackBoxInit:")
	instanceId := e.bbInstanceId(node)

	// Build the set of input port names covered by a wire.
	// When a wire feeds into an input port, the wire value is passed as an
	// argument to Init() — emitting BB_PROP for the matching field would set
	// the struct field to the Inspect panel default BEFORE Init() runs, and
	// if Init() reads s.FieldName instead of the parameter, the prop wins over
	// the wire. Skipping the prop guarantees the wire value is the only source.
	//
	// Matching is case-insensitive: prop "Port" matches connector "port".
	//
	// Português:
	//   Quando um fio está conectado a um conector de entrada, o BB_PROP do
	//   campo correspondente é pulado. O fio passa o valor como argumento de
	//   Init() e tem prioridade sobre o valor do painel Inspect.
	wiredInputPorts := make(map[string]bool, len(node.Inputs))
	for _, input := range node.Inputs {
		if len(input.Connected) > 0 {
			wiredInputPorts[strings.ToLower(input.Name)] = true
		}
	}

	// BB_PROP for each property value set in the Inspect panel.
	// Sorted for deterministic output.
	// Fields whose connector is wired are skipped — the wire value takes priority.
	if propsRaw, ok := node.Properties["props"]; ok {
		if props, ok := propsRaw.(map[string]interface{}); ok {
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, field := range keys {
				// Wire connected to the matching port — skip this prop.
				// The wire value will be passed as an Init() argument instead.
				if wiredInputPorts[strings.ToLower(field)] {
					continue
				}
				val := fmt.Sprintf("%v", props[field])
				e.program.Append(Instruction{
					Op:   OpBBProp,
					Dest: instanceId,
					Args: []string{field, val},
					Meta: map[string]string{"struct": structName},
				})
			}
		}
	}

	// BB_INIT: call Init() with wired inputs.
	var args []string
	for _, input := range node.Inputs {
		if len(input.Connected) > 0 {
			src := input.Connected[0]
			args = append(args, e.resolveInput2(src.DeviceID, src.PortName))
		}
	}

	// Collect the names of output ports that are wired to at least one
	// downstream consumer. The Go backend uses this set to decide whether
	// to emit a named variable or '_' for each return value.
	// Sorted so the Meta string is deterministic across runs.
	//
	// Português: Coleta os nomes das portas de saída conectadas. O backend
	// Go usa este conjunto para decidir nome ou '_' por valor de retorno.
	var connectedOutputNames []string
	for _, out := range node.Outputs {
		if len(out.Connected) > 0 {
			connectedOutputNames = append(connectedOutputNames, out.Name)
		}
	}
	sort.Strings(connectedOutputNames)

	e.program.Append(Instruction{
		Op:   OpBBInit,
		Dest: instanceId,
		Args: args,
		Meta: map[string]string{
			"struct":           structName,
			"nodeId":           node.ID,
			"connectedOutputs": strings.Join(connectedOutputNames, ","),
		},
	})
}

// emitBlackBoxMethod emits BB_METHOD for any non-Init black-box method.
//
// This is the generic replacement for the old emitBlackBoxRun. It handles
// Run, Log, Step, Read, Write, or any other exported method on the struct.
//
// The method name is extracted from the node type:
//
//	"BlackBoxRun:APDS9960" → method = "Run"
//	"BlackBoxLog:APDS9960" → method = "Log"
//
// Like emitBlackBoxInit, BB_DECL is NOT emitted here — it is pre-hoisted by
// emitBBDeclsForScope(). This is true even for pure-method devices (no Init).
//
// Connected outputs follow the same rule as emitBlackBoxInit: only wired
// output ports are captured as named variables; unwired ports become '_'.
// When ALL outputs are unwired, the call is emitted with no LHS.
//
// Português:
//
//	Substituto genérico do antigo emitBlackBoxRun. O nome do método é
//	extraído do tipo do node: "BlackBoxLog:X" → method="Log".
func (e *emitter) emitBlackBoxMethod(node *graph.Node) {
	methodName := bbMethodNameFromNode(node)
	structName := bbStructNameFromNode(node)
	instanceId := e.bbInstanceId(node)

	var args []string
	for _, input := range node.Inputs {
		if len(input.Connected) > 0 {
			src := input.Connected[0]
			args = append(args, e.resolveInput2(src.DeviceID, src.PortName))
		}
	}

	// Collect connected output port names (same logic as emitBlackBoxInit).
	// Português: Coleta nomes das portas de saída conectadas.
	var connectedOutputNames []string
	for _, out := range node.Outputs {
		if len(out.Connected) > 0 {
			connectedOutputNames = append(connectedOutputNames, out.Name)
		}
	}
	sort.Strings(connectedOutputNames)

	meta := map[string]string{
		"struct":           structName,
		"method":           methodName,
		"nodeId":           node.ID,
		"connectedOutputs": strings.Join(connectedOutputNames, ","),
	}

	// When the device has been promoted per-port, the output variables
	// were declared before the loop by emitPromotedVars. The backend
	// must use `=` (not `:=`) so Go doesn't try to redeclare them.
	// Português: Device promovido multi-port → backend usa `=` em vez
	// de `:=` para não redeclarar as vars.
	if e.promotedMultiPort[node.ID] {
		meta["reassign"] = "true"
	}

	e.program.Append(Instruction{
		Op:   OpBBMethod,
		Dest: instanceId,
		Args: args,
		Meta: meta,
	})
}

// emitBlackBoxCall emits BB_CALL for a C99 standalone function-device.
//
// Unlike emitBlackBoxMethod, a function-device has no struct instance and no
// receiver: the scene type is "BlackBox<fn>:" with an empty struct part, so
// there is no `var inst Struct` to hoist (BB_DECL is never emitted for it) and
// no Init/Run pairing. The handle a C99 function returns flows on an ordinary
// wire like any other output, so the composite output registers use the node's
// own ID (%nodeId:portName) — Dest is the node ID, not a shared instance.
//
// Inputs are gathered in port order; connected output port names ride in
// Meta["connectedOutputs"] so the ANSI C backend knows which return/out-params
// are wired (and which PassThrough alias to resolve). The Go backend ignores
// BB_CALL. See docs/CODEGEN_C99_STAGE.md §4 and §6.
//
// Português: Emite BB_CALL para um function-device C99 — função livre, sem
// instância nem receptor. Nenhum BB_DECL é hoisted; o handle flui por fio
// normal, então os registros de saída usam o ID do node (%nodeId:porta). O
// backend C traduz para `ret = fn(args)`; o backend Go ignora.
func (e *emitter) emitBlackBoxCall(node *graph.Node) {
	fnName := bbMethodNameFromNode(node)

	// Note: a callback REFERENCE node (the "ƒ" variant) never reaches here — it
	// is skipped in emitNode. A CALLABLE function node always emits its call,
	// even when its def is a callback handler (HandlerType set): the handler-ness
	// is surfaced by the SEPARATE reference device, not by this callable node, so
	// there is no per-handler skip here anymore. See docs/CODEGEN_C99_CALLBACKS.md.

	args := e.buildBBCallArgs(node, fnName)

	// Connected output ports — same logic as emitBlackBoxMethod. The backend
	// uses these to decide which outputs get a named variable.
	// Português: Portas de saída conectadas, mesma lógica do método.
	var connectedOutputNames []string
	for _, out := range node.Outputs {
		if len(out.Connected) > 0 {
			connectedOutputNames = append(connectedOutputNames, out.Name)
		}
	}
	sort.Strings(connectedOutputNames)

	meta := map[string]string{
		"fn":               fnName,
		"nodeId":           node.ID,
		"connectedOutputs": strings.Join(connectedOutputNames, ","),
	}
	// A function-device whose outputs cross a loop scope is promoted to a
	// pre-declared var, same as a multi-port method; the backend then uses
	// `=` instead of a fresh declaration.
	// Português: Function-device que cruza escopo é promovido a var; backend
	// usa `=` em vez de declarar de novo.
	if e.promotedMultiPort[node.ID] {
		meta["reassign"] = "true"
	}

	e.program.Append(Instruction{
		Op:   OpBBCall,
		Dest: node.ID, // no shared instance — composite outputs are %nodeId:port
		Args: args,
		Meta: meta,
	})
}

// buildBBCallArgs builds the argument list for a C99 function-device call.
//
// When the function has out-params — mutable-pointer outputs the function
// writes (`read(dev, float *temperature, float *humidity)`) — the call must
// pass EVERY parameter in source order: inputs as their resolved register,
// out-params by address (a "&"-prefixed register the backend turns into
// `&var`). Args are therefore ordered by the recorded ParamIndex.
//
// When there are no out-params the simpler inputs-only path (in node order,
// connected inputs only) is used, preserving the original behaviour for
// ordinary devices (Add, create, destroy, …). Falls back to inputs-only when
// the def is unavailable (e.g. bbDefs nil, as in IR-only unit tests).
//
// Português: Monta os argumentos da chamada. Com out-params, passa todos os
// parâmetros na ordem do código (ParamIndex): input vira registro resolvido,
// out-param vira "&registro". Sem out-params, usa o caminho antigo (só inputs).
func (e *emitter) buildBBCallArgs(node *graph.Node, fnName string) []string {
	fn := e.funcDef(fnName)
	if fn == nil {
		// No def (e.g. IR-only unit tests): inputs in node order, connected
		// only — the original pre-def behaviour.
		var args []string
		for _, input := range node.Inputs {
			if len(input.Connected) > 0 {
				src := input.Connected[0]
				args = append(args, e.resolveInput2(src.DeviceID, src.PortName))
			}
		}
		return args
	}

	type orderedArg struct {
		idx int
		val string
	}
	var collected []orderedArg
	for _, in := range fn.Inputs {
		val := e.resolveNodeInput(node, in.Name)
		switch {
		case in.CallbackType != "":
			// Callback parameter: the wire carries a REFERENCE to a handler
			// device, not a value. Resolve it to the handler's C function
			// name and ship it under the dedicated "@" marker — a callback
			// reference BY FUNCTION NAME, distinct from the "=" verbatim
			// literal it used to ride on. The distinction matters for the
			// multi-file C output: the handler's definition lives in some
			// black-box unit under a PREFIXED symbol, and only the backend
			// (which owns C naming) can look the function up in the defs and
			// prefix it; a plain "=" literal would be emitted verbatim and
			// dangle. The IR stays language-neutral — "@" names the function,
			// the backend decides how that name is spelled in the target.
			// This is the LabVIEW static-VI-reference idiom; the generated
			// call is `consumer(P<id>_handlerName)` (or the bare name in the
			// single-file fallback). An unwired callback parameter falls back
			// to its default (NULL) through the ordinary "=" path, so the
			// call stays well-formed.
			// Português: Parâmetro de callback carrega uma REFERÊNCIA a um
			// handler — viaja como "@nome" (marcador próprio, não "="): no
			// modelo multiarquivo o handler existe sob símbolo PREFIXADO, e
			// só o backend (dono do naming C) resolve o prefixo. O IR segue
			// neutro. Sem fio → default (NULL) pelo caminho "=" normal.
			if name := e.callbackHandlerName(node, in.Name); name != "" {
				val = "@" + name
			} else {
				val = "=" + bbInputDefault(in)
			}
		case in.SliceLenName != "":
			// Collection parameter (C99 `slice:` directive — const-array
			// plan Task 7): the def collapsed (pointer, length) into this
			// ONE "[]T" port, so the call must rebuild BOTH arguments. The
			// array argument is the resolved register (the fixed array
			// decays to a pointer at the call); the length argument is the
			// register's `_len` companion — the explicit symbol the const
			// array emits precisely so it survives pointer decay (plan
			// decision 3 paying its dividend). "#" is the backend protocol
			// for "append _len to the resolved operand". Each argument
			// lands at its own recorded ParamIndex, so the pair need not
			// be adjacent in the C signature. Unwired (optional) →
			// NULL + 0, the well-formed empty collection.
			//
			// Português: Parâmetro de coleção (diretiva `slice:`) — a
			// chamada reconstrói o par: o array (decai a ponteiro) e o
			// companion `_len` (protocolo "#"). Sem fio → NULL + 0.
			if val == "" {
				collected = append(collected, orderedArg{idx: in.ParamIndex, val: "=NULL"})
				collected = append(collected, orderedArg{idx: in.SliceLenIndex, val: "=0"})
			} else {
				collected = append(collected, orderedArg{idx: in.ParamIndex, val: val})
				collected = append(collected, orderedArg{idx: in.SliceLenIndex, val: "#" + val})
			}
			continue
		case val == "":
			// Unwired input: pass its default so the call is well-formed —
			// every C parameter needs a value. "=" marks a literal the backend
			// emits verbatim. A mandatory unwired input is already a validation
			// error; this covers optional inputs.
			val = "=" + bbInputDefault(in)
		case !strings.Contains(in.GoType, "*"):
			// Scalar input: cast to the parameter's authored C type so the call
			// matches the signature even when the producer uses the profile's
			// wider representation (e.g. an int32_t const into an `int` param).
			// "(type)register" — the backend renders the cast and the operand.
			// Pointers (handles / wire-types) carry their exact type already, so
			// they are not cast.
			val = "(" + in.GoType + ")" + val
		}
		collected = append(collected, orderedArg{idx: in.ParamIndex, val: val})
	}
	for _, out := range fn.Outputs {
		if out.Name == "return" || out.PassThrough {
			continue // the C return is the LHS, not an arg; PassThrough is an alias
		}
		// Out-param: passed by address. The backend declares a var for this
		// register and takes its address; downstream consumers resolve the
		// same "%nodeId:portName" register to that var.
		collected = append(collected, orderedArg{idx: out.ParamIndex, val: "&%" + node.ID + ":" + out.Name})
	}
	sort.Slice(collected, func(i, j int) bool { return collected[i].idx < collected[j].idx })

	args := make([]string, 0, len(collected))
	for _, c := range collected {
		args = append(args, c.val)
	}
	return args
}

// funcDef returns the FuncDef for a C99 function-device by name, or nil when
// bbDefs is absent or the function is not present.
func (e *emitter) funcDef(fnName string) *blackbox.FuncDef {
	def := e.bbDefs[fnName]
	if def == nil {
		return nil
	}
	for i := range def.Functions {
		if def.Functions[i].Name == fnName {
			return &def.Functions[i].FuncDef
		}
	}
	return nil
}

// hasOutParams reports whether a function writes any out-param — a parsed
// output that is neither the C return nor a synthesized pass-through.
// bbInputDefault returns the literal an unwired input contributes to the call.
// The port's `default:` directive wins; absent that, "0" is a universal zero —
// valid for integers, floats (0 → 0.0) and pointers (NULL) in C. A real value
// still requires the maker to wire the input (or the specialist to set a
// meaningful default).
func bbInputDefault(in blackbox.PortDef) string {
	if in.Default != "" {
		return in.Default
	}
	return "0"
}

// resolveNodeInput resolves a node's input port (by name) to its source
// register, or "" when the port is unwired (a missing mandatory connection is
// surfaced by validate(); optional unwired inputs are a pre-existing concern).
func (e *emitter) resolveNodeInput(node *graph.Node, portName string) string {
	for _, in := range node.Inputs {
		if in.Name == portName && len(in.Connected) > 0 {
			src := in.Connected[0]
			return e.resolveInput2(src.DeviceID, src.PortName)
		}
	}
	return ""
}

// callbackHandlerName returns the C function name of the callback-handler
// device wired into the named callback input port of `node`, or "" when the
// port is unwired or its source is not a handler. The handler's name IS the
// C symbol passed by reference into the consumer's callback parameter (the
// wire-ƒ resolves to a function name, not a register — see buildBBCallArgs).
//
// It reads the source node straight off the graph (e.graph.Nodes), which is
// keyed by node id, then confirms via the loaded def that the source carries
// a HandlerType. The non-handler / unwired cases return "" so the caller can
// fall back to the parameter's default (NULL), keeping the emitted call well-
// formed even before the maker has wired anything.
//
// Português: Devolve o nome da função-handler ligada à porta de callback de
// `node` (ou "" se a porta estiver sem fio ou a origem não for um handler).
// Esse nome é o símbolo C passado por referência ao parâmetro de callback do
// consumidor. Lê o node de origem direto do grafo e confirma pelo def que ele
// é um handler.
func (e *emitter) callbackHandlerName(node *graph.Node, portName string) string {
	for _, in := range node.Inputs {
		if in.Name != portName || len(in.Connected) == 0 {
			continue
		}
		src, ok := e.graph.Nodes[in.Connected[0].DeviceID]
		if !ok || src == nil {
			return ""
		}
		// The wire-ƒ source must be a callback REFERENCE device
		// ("CallbackRef:<fn>"); its referenced function name is the C symbol
		// passed by address. A callable function node is never the source of a
		// callback wire (it has no callback output). Confirm via the loaded def
		// that the function is indeed a handler before resolving.
		fnName := callbackRefFuncName(src)
		if fnName == "" {
			return ""
		}
		if fd := e.funcDef(fnName); fd != nil && fd.HandlerType != "" {
			return fnName
		}
		return ""
	}
	return ""
}

// bbInstanceId extracts the shared instance ID from node properties.
func (e *emitter) bbInstanceId(node *graph.Node) string {
	if id, ok := node.Properties["instanceId"]; ok {
		if s, ok := id.(string); ok && s != "" {
			return s
		}
	}
	return node.ID
}

// =====================================================================
//  Native device emission
// =====================================================================

// emitVariableDecls emits a zero-initialised OpVar declaration for every user
// project variable, at the top of the program. The variable name is its
// register; the value lives in that single register for the whole program (v1
// is global scope). Reused machinery: OpVar is the same opcode the scope-
// crossing promotion uses, and zeroValue supplies the per-type initialiser.
//
// Português: Emite uma declaração OpVar zero-init para cada variável de
// projeto do usuário, no topo do programa. O nome da variável é seu
// registrador; o valor vive nesse único registrador o programa todo (v1 é
// escopo global). Reusa OpVar (mesmo opcode da promoção) e zeroValue.
func (e *emitter) emitVariableDecls() {
	for _, v := range e.variables {
		if v.Name == "" {
			continue
		}
		// The "varInit" marker tells the C backend this OpVar is a user
		// variable that must be EXPLICITLY zero-initialised at its declaration
		// (int32_t counter = 0;). Wire-promotion OpVars look identical (same
		// OpVar + zero arg) but are assigned from inside the scope right after,
		// so the C backend leaves them bare — only the marker distinguishes the
		// two. The Go backend ignores Meta and zero-values var declarations
		// natively, so this is a no-op there.
		//
		// Português: O marcador "varInit" diz ao backend C que este OpVar é uma
		// variável de usuário que precisa ser zero-inicializada explicitamente
		// na declaração. OpVars de promoção de fio são idênticos mas atribuídos
		// logo depois; só o marcador os distingue. O backend Go ignora Meta e
		// zera declarações nativamente.
		e.program.Append(Instruction{
			Op:   OpVar,
			Dest: v.Name,
			Type: v.Type,
			Args: []string{zeroValue(v.Type)},
			Meta: map[string]string{"varInit": "1"},
		})
	}
}

// emitSetVar lowers a SetVar device to an assignment into the project variable
// named by its "varName" property: OpAssign %<varName> <type> %<input>. SetVar
// is a pure sink — a value input and no output — so it adds exactly one
// instruction and nothing reads back from it. The variable register is the
// variable name, declared once by emitVariableDecls. An unnamed target or an
// unwired value is skipped here; both are surfaced as separate diagnostics.
//
// Português: Lowering de um SetVar para atribuição na variável de projeto
// nomeada por "varName": OpAssign %<varName> <type> %<input>. SetVar é sink
// puro — entrada de valor, sem saída — então adiciona exatamente uma instrução
// e nada lê de volta. Alvo sem nome ou valor sem fio são pulados aqui (cada um
// é reportado por outro passo).
func (e *emitter) emitSetVar(node *graph.Node, dataType string) {
	name, _ := node.Properties["varName"].(string)
	if name == "" {
		return
	}
	src := e.resolveInput(node.ID, "value")
	if src == "" {
		return
	}
	e.program.Append(Instruction{
		Op:   OpAssign,
		Dest: name,
		Type: dataType,
		Args: []string{src},
	})
}

// getVarName reports whether node is a GetVar device and, if so, the project
// variable it reads. Used by resolveInput2 to alias a GetVar consumer straight
// to the variable's register, so a GetVar produces no instruction of its own.
//
// Português: Diz se node é um GetVar e, se for, a variável de projeto que ele
// lê. Usado por resolveInput2 pra fazer o consumidor de um GetVar apontar
// direto pro registrador da variável, sem o GetVar emitir instrução própria.
func getVarName(node *graph.Node) (string, bool) {
	switch node.Type {
	case "StatementGetVarInt", "StatementGetVarFloat", "StatementGetVarString":
		if v, ok := node.Properties["varName"].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

func (e *emitter) emitConst(node *graph.Node, dataType string) {
	val := zeroValue(dataType)
	if v, ok := node.Properties["value"]; ok {
		val = fmt.Sprintf("%v", v)
	}
	op := OpConst
	if e.promoted[node.ID] {
		op = OpAssign
	}
	e.program.Append(Instruction{Op: op, Dest: node.ID, Type: dataType, Args: []string{val}})
}

// emitConstFloat emits a float constant (StatementConstFloat). Unlike the
// abstract "int" of emitConst — whose width the target profile resolves —
// a float constant carries the maker's explicit precision ("float32" or
// "float64") as the IR type, because precision is a per-device choice
// (float32 for embedded targets with hardware single precision; float64
// otherwise). The backend maps that concrete token to the right type:
// float/double in C, float32/float64 in Go.
//
// The value is formatted with strconv.FormatFloat in 'f' (plain decimal)
// form so it never reaches the backend in exponent notation (e.g.
// "1e-05"), which the C literal builder cannot turn into a valid float
// literal. A missing "value" yields the type's zero via zeroValue.
//
// Português: Emite uma constante float. Diferente do "int" abstrato (cuja
// largura o profile resolve), a precisão (float32/float64) é escolha do
// device e vai como tipo do IR — o backend mapeia pro tipo certo
// (float/double em C, float32/float64 em Go). Valor em decimal puro, sem
// notação exponencial, pra que o literal C seja sempre válido.
func (e *emitter) emitConstFloat(node *graph.Node) {
	dataType := "float64"
	if p, ok := node.Properties["precision"].(string); ok && p != "" {
		dataType = p
	}
	val := zeroValue(dataType)
	if v, ok := node.Properties["value"]; ok {
		if f, isFloat := v.(float64); isFloat {
			// JSON numbers decode to float64; 'f' form avoids exponents.
			val = strconv.FormatFloat(f, 'f', -1, 64)
		} else {
			val = fmt.Sprintf("%v", v)
		}
	}
	op := OpConst
	if e.promoted[node.ID] {
		op = OpAssign
	}
	e.program.Append(Instruction{Op: op, Dest: node.ID, Type: dataType, Args: []string{val}})
}

// emitConstString emits a string constant (StatementConstString). Unlike the
// numeric emitConst, the value must reach the IR ALREADY QUOTED: both backends
// pass a "string" const value through verbatim (goLiteral and cLiteral assume
// it is a valid quoted literal), while cTypeName maps "string" to C's
// `const char*`. strconv.Quote applies Go-style escaping, which matches C
// string-literal escaping for the characters a maker can type (\n, \t, \", \\).
// The stored scene value is raw (e.g. Hello), so we quote it here → `"Hello"`.
func (e *emitter) emitConstString(node *graph.Node) {
	val := ""
	if v, ok := node.Properties["value"]; ok {
		val = fmt.Sprintf("%v", v)
	}
	op := OpConst
	if e.promoted[node.ID] {
		op = OpAssign
	}
	e.program.Append(Instruction{Op: op, Dest: node.ID, Type: "string", Args: []string{strconv.Quote(val)}})
}

func (e *emitter) emitConstBool(node *graph.Node) {
	val := "false"
	if v, ok := node.Properties["value"]; ok {
		if b, isBool := v.(bool); isBool && b {
			val = "true"
		}
	}
	op := OpConst
	if e.promoted[node.ID] {
		op = OpAssign
	}
	e.program.Append(Instruction{Op: op, Dest: node.ID, Type: "bool", Args: []string{val}})
}

// emitConstArray / buildConstArrayInstruction emit a constant fixed-size collection literal — the shared
// emitter behind the three sibling devices StatementConstArrayInt /
// StatementConstArrayFloat / StatementConstArrayString — as a single
// CONST_ARRAY instruction whose Type is the BARE element type and whose Args
// are the formatted element literals; the collection length is len(Args).
// See OpConstArray in types.go for the backend translation (Go slice
// literal; C fixed array + `_len` companion).
//
// Properties read:
//
//	elementType — bare scalar element type, exported FIXED by each device:
//	              "int" (ConstArrayInt), "float32"/"float64" (ConstArrayFloat,
//	              per its precision select), "string" (ConstArrayString).
//	              Defaults to "int" if absent.
//	values      — the element list. Two shapes are accepted:
//	                a) a JSON array ([]interface{} of numbers/bools/strings);
//	                b) a single string as typed in the Inspect field — the
//	                   separator depends on the element type (commas for
//	                   numeric/bool, NEWLINES for string; see the parsing
//	                   switch below). Blank tokens are skipped, so trailing
//	                   separators are harmless.
//
// Element formatting mirrors the sibling const emitters — see
// formatArrayElement.
//
// Authoring-error defenses (warnings, not hard errors — the device's
// AcceptNotConnected:false port covers the UNWIRED case before codegen;
// these cover the rest):
//
//   - an empty/missing value list warns AND still emits the (empty)
//     instruction: Go compiles `[]int{}`, while the C backend must take its
//     own stance when Task 3 lands (an empty initializer list is not valid
//     C99).
//   - a scope-crossing PROMOTION (e.promoted) is not representable for a
//     collection yet: the OpVar+OpAssign promotion scheme carries a single
//     value (and a C fixed array cannot be reassigned at all), so the
//     device is emitted in place with a warning. Proper collection
//     promotion — hoisting the whole declaration — is deliberately
//     deferred to the first end-to-end slice (Task 6 design); see
//     docs/claude_const_array_plan.md.
//
// Português: Emite a coleção constante (StatementConstArray{Int,Float,String}) como uma única
// instrução CONST_ARRAY — Type é o tipo do ELEMENTO, Args são os literais já
// formatados (decimal puro para números, strings pré-citadas). Aceita values
// como array JSON ou string CSV do campo do Inspect. Lista vazia e promoção
// entre escopos geram warning (promoção de coleção fica para a Task 6).
// emitDataBlob turns a Data · File / Data · Text device into ONE
// DATA_BLOB instruction. The payload is decoded here from the scene
// properties into raw bytes and re-packed as base64 in Meta — the IR stays
// language-neutral, and the backends never touch scene shapes:
//
//   - File: properties["file"] is the FieldFile StoreName JSON
//     {"name","dataUrl"}; the bytes are the dataUrl's base64 section.
//   - Text: properties["text"] as UTF-8; properties["nullTerminated"]
//     "true" (the device default) APPENDS a trailing NUL so C consumers
//     can treat the pointer as a string — the logical length in
//     Meta["lenNoNul"] NEVER counts it (decision 2026-07-12).
//
// An empty payload (no file picked / empty editor) gets an authoring
// warning and an empty blob — the backend's zero-length stance keeps the
// artefact compiling, mirroring the const-array precedent.
//
// Português: Transforma um device Data · File / Text em UMA instrução
// DATA_BLOB. O payload é decodificado AQUI das propriedades da cena para
// bytes crus e reempacotado em base64 no Meta — o IR fica neutro e os
// backends nunca tocam formas de cena. Text com null-terminated ANEXA o
// NUL mas o tamanho lógico NUNCA o conta. Payload vazio ganha warning
// autoral e blob vazio — a postura de tamanho-zero do backend mantém o
// artefato compilando.
func (e *emitter) emitDataBlob(node *graph.Node) {
	var data []byte
	var lenNoNul int
	kind := "text"
	sourceName := ""

	if node.Type == "StatementDataFile" {
		kind = "file"
		if raw, ok := node.Properties["file"].(string); ok && raw != "" {
			var v struct {
				Name    string `json:"name"`
				DataURL string `json:"dataUrl"`
			}
			if json.Unmarshal([]byte(raw), &v) == nil {
				sourceName = v.Name
				if comma := strings.Index(v.DataURL, ","); comma >= 0 {
					if b, err := base64.StdEncoding.DecodeString(v.DataURL[comma+1:]); err == nil {
						data = b
					}
				}
			}
		}
		lenNoNul = len(data)
	} else {
		if raw, ok := node.Properties["text"].(string); ok {
			data = []byte(raw)
		}
		lenNoNul = len(data)
		if nt, ok := node.Properties["nullTerminated"].(string); !ok || nt == "true" {
			data = append(data, 0)
		}
	}

	if lenNoNul == 0 {
		e.program.Warn("data device %s has no content — emitting an empty blob", node.ID)
	}

	// The flash-asset ceiling (field rule 2026-07-16): a distracted maker
	// can wire a 200 MB video into a Data · File; turning that into a C
	// byte array would produce a source file no target could swallow.
	// Refuse HERE — the one place every data path (file upload, text,
	// legacy DataURL) converges — with the device named, and emit no
	// instruction. Português: O teto do asset de flash — recusa AQUI, o
	// único ponto onde todos os caminhos de dado convergem, nomeando o
	// device, sem emitir instrução.
	if len(data) > DataBlobMaxBytes {
		e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
			Kind:     diagnostics.KindAssetTooLarge,
			Severity: diagnostics.SeverityError,
			Devices:  []string{node.ID},
			Message: fmt.Sprintf(
				"%s: asset is %d bytes — the flash-asset ceiling is %d (2 MB); shrink the file or serve it from storage instead of embedding it",
				node.ID, len(data), DataBlobMaxBytes,
			),
		})
		return
	}

	e.program.Append(Instruction{
		Op:   OpDataBlob,
		Dest: node.ID,
		Type: "uint8",
		Meta: map[string]string{
			"base64":     base64.StdEncoding.EncodeToString(data),
			"lenNoNul":   strconv.Itoa(lenNoNul),
			"kind":       kind,
			"sourceName": sourceName,
		},
	})
}

func (e *emitter) emitConstArray(node *graph.Node) {
	if e.promoted[node.ID] {
		// HOISTED (T6 "içamento"): a constant collection that crosses a
		// scope boundary outward is not VAR+ASSIGN-promoted like scalars —
		// a fixed C array cannot be reassigned, and the value is a
		// compile-time literal anyway. emitPromotedVars already emitted
		// the FULL declaration in the parent scope; lexical scoping makes
		// the name visible both here and at the outer consumer, so the
		// in-scope emitter stays silent.
		//
		// Português: Coleção constante que cruza o escopo pra fora é
		// IÇADA inteira pelo emitPromotedVars (array fixo em C não
		// reatribui); aqui não se emite nada — o escopo léxico resolve.
		return
	}
	e.program.Append(e.buildConstArrayInstruction(node))
}

// buildConstArrayInstruction builds the CONST_ARRAY instruction for one of
// the three collection devices. Shared by the two call sites: the normal
// in-scope path (emitConstArray) and the hoisting path (emitPromotedVars,
// when the collection crosses its scope outward).
func (e *emitter) buildConstArrayInstruction(node *graph.Node) Instruction {
	elemType := "int"
	if t, ok := node.Properties["elementType"].(string); ok && t != "" {
		elemType = t
	}

	// T6 decision B — the element type FLOWS FROM THE CONSUMER. The abstract
	// "int" (ConstArrayInt) and "float" (ConstArrayFloat) are inferable: when
	// the collection feeds an authored black-box parameter with a CONCRETE
	// element (a Go method taking []uint16, a C sensor taking []float32, whose
	// input port carries that type in the scene), the declaration is rendered
	// in that element type so the generated call compiles — Go slices have no
	// implicit conversion, so declaring []float64 against a []float32 parameter
	// would never build. The String sibling is concrete and honours the maker's
	// value verbatim. With no concrete consumer the abstract element survives
	// and goTypeName/cTypeName widen it per backend (int→int64, float→float64 in
	// Go; the target profile picks the width in C). Conflicting concrete
	// consumers are rejected earlier by validateConstArrayElemConflicts (run
	// Step 1d), so at most one candidate survives to this point. Decision 5
	// (AcceptNotConnected: false) guarantees a consumer always exists.
	//
	// Português: Decisão B da T6 — o tipo do elemento FLUI DO CONSUMIDOR.
	// "int" e "float" abstratos são inferíveis; consumidor autoral concreto
	// (ex: []uint16, []float32) define o tipo da declaração. Sem consumidor
	// concreto, o elemento abstrato sobrevive e goTypeName/cTypeName decidem a
	// largura por backend. Conflitos de fan-out são barrados antes, na validação.
	if elemType == "int" || elemType == "float" {
		if inferred := e.inferredCollectionElem(node); inferred != "" {
			elemType = inferred
		}
	}

	var elems []string
	switch raw := node.Properties["values"].(type) {
	case []interface{}:
		// Shape (a): a JSON array — one literal per entry.
		elems = make([]string, 0, len(raw))
		for _, v := range raw {
			elems = append(elems, formatArrayElement(v, elemType))
		}
	case string:
		// Shape (b): the Inspect text field. The separator is decided by
		// the ELEMENT TYPE: string collections split on NEWLINES (one
		// element per line — a comma is legitimate content inside a
		// string, e.g. "hello, world", so CSV would corrupt it), while
		// numeric/bool collections split on commas ("1, 2, 3" — a newline
		// is never legitimate inside a numeric token). Blank entries are
		// skipped, so trailing separators are harmless.
		//
		// Português: O separador depende do TIPO do elemento — string
		// quebra por LINHA (vírgula é conteúdo legítimo de texto),
		// numéricos/bool quebram por vírgula. Entradas em branco são
		// ignoradas.
		sep := ","
		if elemType == "string" {
			sep = "\n"
		}
		for _, tok := range strings.Split(raw, sep) {
			tok = strings.TrimSpace(tok) // also drops the \r of CRLF input
			if tok == "" {
				continue
			}
			elems = append(elems, formatArrayElement(tok, elemType))
		}
	case nil:
		// Missing property — handled by the empty-list warning below.
	default:
		e.program.Warn("const array %s: unsupported values shape %T", node.ID, raw)
	}

	if len(elems) == 0 {
		e.program.Warn("const array %s has no values — fill the Values field in Inspect", node.ID)
	}

	return Instruction{Op: OpConstArray, Dest: node.ID, Type: elemType, Args: elems}
}

// formatArrayElement renders one collection element as the textual literal
// the backends expect, given the collection's bare element type.
//
// JSON numbers arrive as float64 and are rendered in plain decimal 'f' form,
// never exponent notation, for the same reason as emitConstFloat — the C
// literal builder cannot consume "1e-05". An integral float64 prints with no
// decimal point ("3"), so the same path serves int elements. A string token
// holding a number is normalized through the same float path when the element
// type is numeric, so a maker-typed "0.00001" can never reach a backend in
// exponent form either. For a "string" element type the value is pre-quoted
// with strconv.Quote, matching emitConstString's contract (backends pass
// string literals through verbatim). Anything else passes through as written
// — the target compiler catches garbage, the same permissive stance
// emitConst takes for scalars.
//
// Width note: formatting always uses bitSize 64; the per-element TYPE SUFFIX
// (e.g. C's `1.5f` for float32) is backend business — ansic's cLiteral
// already applies it per element type, so the IR stays suffix-free.
//
// Português: Formata um elemento da coleção como literal textual. Números em
// decimal puro (nunca exponencial), elementos string pré-citados com
// strconv.Quote, o resto passa como veio (o compilador alvo pega lixo). O
// sufixo de tipo (ex: `f` do float32 em C) é responsabilidade do backend.
func formatArrayElement(v interface{}, elemType string) string {
	if elemType == "string" {
		return strconv.Quote(fmt.Sprintf("%v", v))
	}
	switch val := v.(type) {
	case float64:
		// JSON numbers decode to float64; 'f' form avoids exponents.
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case string:
		if isNumericElemType(elemType) {
			if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
				return strconv.FormatFloat(f, 'f', -1, 64)
			}
		}
		return strings.TrimSpace(val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// isNumericElemType reports whether the bare collection element type is a
// numeric scalar the formatter should normalize into plain decimal form.
// Local on purpose: the types package's isNumeric is unexported and also
// recognizes the ABSTRACT markers ("int"/"float" as wire categories), which
// is a different concern from "this literal token is a number to format".
//
// Português: Diz se o tipo do elemento é um escalar numérico que o
// formatador deve normalizar para decimal puro. Local de propósito — o
// isNumeric do package types é unexported e trata também os marcadores
// abstratos de fio, que é outra preocupação.
func isNumericElemType(elemType string) bool {
	switch elemType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float", "float32", "float64":
		return true
	}
	return false
}

func (e *emitter) emitCmp(node *graph.Node, op Op) {
	// §7.5 (decision 2026-06-30, shipped 2026-07-16): an unwired output
	// here is a validation ERROR — the leaf assignment would not even
	// compile on the Go backend. Checked in the ir so both languages
	// inherit the rule (parity by construction). Português: Saída sem
	// fio = erro de validação; checado no ir, as duas linguagens herdam.
	if !e.hasOutgoingWire(node.ID) {
		e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
			Kind:     diagnostics.KindMathOutputUnwired,
			Severity: diagnostics.SeverityError,
			Devices:  []string{node.ID},
			Message: fmt.Sprintf(
				"%s: output is not connected — wire it to a consumer or remove the device (an unread result does not compile on the Go target)",
				node.ID),
		})
		return
	}

	argA := e.resolveInput(node.ID, "inputX")
	argB := e.resolveInput(node.ID, "inputY")
	if argA == "" {
		e.program.Warn("%s.inputX: not connected", node.ID)
		argA = "0"
	}
	if argB == "" {
		e.program.Warn("%s.inputY: not connected", node.ID)
		argB = "0"
	}

	// Type-compat pass. Inserts CONVERT instructions when a cast is
	// needed and emits a diagnostic for warn/impossible verdicts. When
	// impossible, we skip the comparison entirely — no point emitting
	// code that will break the Go compiler downstream.
	typeA := e.resolveInputType(node.ID, "inputX")
	typeB := e.resolveInputType(node.ID, "inputY")
	if typeA != "" && typeB != "" {
		verdict := types.Classify(typeA, typeB)
		switch verdict.Action {
		case types.CastImpossible:
			e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
				Kind:     diagnostics.KindTypeMismatch,
				Severity: diagnostics.SeverityError,
				Devices:  []string{node.ID},
				Message: fmt.Sprintf(
					"%s: cannot compare %s with %s — no conversion known between these types",
					node.ID, typeA, typeB,
				),
			})
			return
		case types.CastWarn:
			e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
				Kind:     diagnostics.KindTypeLossy,
				Severity: diagnostics.SeverityWarning,
				Devices:  []string{node.ID},
				Message: fmt.Sprintf(
					"%s: comparing %s with %s — conversion to %s may lose range, sign or precision",
					node.ID, typeA, typeB, verdict.Result,
				),
			})
		}
		if verdict.CastA != "" {
			argA = e.emitConvert(argA, typeA, verdict.CastA)
		}
		if verdict.CastB != "" {
			argB = e.emitConvert(argB, typeB, verdict.CastB)
		}
	}

	e.program.Append(Instruction{Op: op, Dest: node.ID, Type: "bool", Args: []string{argA, argB}})
}

func (e *emitter) emitBinOp(node *graph.Node, op Op) {
	// §7.5 (decision 2026-06-30, shipped 2026-07-16): an unwired output
	// here is a validation ERROR — the leaf assignment would not even
	// compile on the Go backend. Checked in the ir so both languages
	// inherit the rule (parity by construction). Português: Saída sem
	// fio = erro de validação; checado no ir, as duas linguagens herdam.
	if !e.hasOutgoingWire(node.ID) {
		e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
			Kind:     diagnostics.KindMathOutputUnwired,
			Severity: diagnostics.SeverityError,
			Devices:  []string{node.ID},
			Message: fmt.Sprintf(
				"%s: output is not connected — wire it to a consumer or remove the device (an unread result does not compile on the Go target)",
				node.ID),
		})
		return
	}

	argA := e.resolveInput(node.ID, "inputX")
	argB := e.resolveInput(node.ID, "inputY")
	dataType := e.inferOutputType(node)
	if argA == "" {
		e.program.Warn("%s.inputX: not connected", node.ID)
		argA = zeroValue(dataType)
	}
	if argB == "" {
		e.program.Warn("%s.inputY: not connected", node.ID)
		argB = zeroValue(dataType)
	}

	// Type-compat pass. Same structure as emitCmp, except the
	// promoted type also becomes the result type of the operation
	// (overrides whatever inferOutputType guessed from the node's
	// declared port).
	typeA := e.resolveInputType(node.ID, "inputX")
	typeB := e.resolveInputType(node.ID, "inputY")
	if typeA != "" && typeB != "" {
		verdict := types.Classify(typeA, typeB)
		switch verdict.Action {
		case types.CastImpossible:
			e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
				Kind:     diagnostics.KindTypeMismatch,
				Severity: diagnostics.SeverityError,
				Devices:  []string{node.ID},
				Message: fmt.Sprintf(
					"%s: cannot combine %s with %s — no conversion known between these types",
					node.ID, typeA, typeB,
				),
			})
			return
		case types.CastWarn:
			e.typeDiags = append(e.typeDiags, diagnostics.Diagnostic{
				Kind:     diagnostics.KindTypeLossy,
				Severity: diagnostics.SeverityWarning,
				Devices:  []string{node.ID},
				Message: fmt.Sprintf(
					"%s: combining %s with %s — conversion to %s may lose range, sign or precision",
					node.ID, typeA, typeB, verdict.Result,
				),
			})
		}
		if verdict.CastA != "" {
			argA = e.emitConvert(argA, typeA, verdict.CastA)
		}
		if verdict.CastB != "" {
			argB = e.emitConvert(argB, typeB, verdict.CastB)
		}
		if verdict.Result != "" {
			dataType = verdict.Result
		}
	}

	e.program.Append(Instruction{Op: op, Dest: node.ID, Type: dataType, Args: []string{argA, argB}})
}

// emitIndex translates a StatementIndex{Int,Float,String} node into an OpIndex
// instruction — the safe, bounds-checked array reader. It has two inputs (the
// "array" and "index" ports) and two outputs: the primary "value" (which uses
// the single-register scheme, Dest = node.ID, like any other device) and an
// OPTIONAL "ok" bool. The ok output becomes a synthesized companion register
// "{id}_ok" — matching what resolveInput2 hands a consumer of that port — but
// ONLY when a wire actually consumes it; unwired, no ok register is produced and
// the backends inline the bounds check with no dead variable.
//
// When either input is unconnected there is nothing to read, so emitIndex warns
// and still DEFINES the outputs at their zero (value = the type's zero, ok =
// false) so downstream consumers compile — a missing wire is an authoring
// warning, never broken output.
//
// Português: Traduz um nó StatementIndex{Int,Float,String} em uma instrução
// OpIndex — o leitor de array seguro e checado. Duas entradas (portas "array" e
// "index") e duas saídas: o "value" primário (esquema single-register, Dest =
// node.ID) e um "ok" bool OPCIONAL. O "ok" vira o companheiro sintetizado
// "{id}_ok" — o mesmo que resolveInput2 entrega a um consumidor dessa porta —
// mas SÓ quando uma aresta o consome; sem consumo, nenhum registrador ok é
// produzido e os backends inlinam a checagem sem variável morta. Entrada
// desconectada: avisa e ainda DEFINE as saídas no zero para o código a jusante
// compilar — fio faltando é aviso de autoria, nunca saída quebrada.
func (e *emitter) emitIndex(node *graph.Node, elemType string) {
	array := e.resolveInput(node.ID, "array")
	index := e.resolveInput(node.ID, "index")

	// The ok output is emitted only when a wire consumes it.
	okConnected := false
	for _, edge := range e.graph.Edges {
		if edge.From.DeviceID == node.ID && edge.From.PortName == "ok" {
			okConnected = true
			break
		}
	}

	// Nothing to index if an input is missing: warn, and define the outputs at
	// their zero so consumers still compile.
	if array == "" || index == "" {
		if array == "" {
			e.program.Warn("%s.array: not connected", node.ID)
		}
		if index == "" {
			e.program.Warn("%s.index: not connected", node.ID)
		}
		e.program.Append(Instruction{Op: OpConst, Dest: node.ID, Type: elemType, Args: []string{zeroValue(elemType)}})
		if okConnected {
			e.program.Append(Instruction{Op: OpConst, Dest: node.ID + "_ok", Type: "bool", Args: []string{"false"}})
		}
		return
	}

	var meta map[string]string
	if okConnected {
		meta = map[string]string{"okDest": node.ID + "_ok"}
	}
	e.program.Append(Instruction{
		Op:   OpIndex,
		Dest: node.ID,
		Type: elemType,
		Args: []string{array, index},
		Meta: meta,
	})
}

func (e *emitter) emitGauge(node *graph.Node) {
	src := e.resolveInput(node.ID, "current")
	if src == "" {
		e.program.Warn("%s.current: not connected", node.ID)
		return
	}
	channel := node.Label
	if channel == "" {
		channel = node.ID
	}
	e.program.Append(Instruction{
		Op:   OpOutput,
		Dest: node.ID,
		Args: []string{src, fmt.Sprintf("%q", channel)},
		Meta: map[string]string{"channel": channel},
	})
}

// emitPrint emits OpPrint for one StatementPrint{...} sink. The single input
// port is "value"; the device carries two free properties — "prefix" (text
// printed before the value) and "format" (the per-type variant; see OpPrint's
// table in types.go). Both travel as Meta so the two backends read one shape.
// An unconnected input is a warning, not an error: the maker parked the
// device, the scene still generates.
//
// Português: Emite OpPrint para um sink StatementPrint{...}. A porta única é
// "value"; o device leva duas propriedades — "prefix" (texto antes do valor)
// e "format" (a variante por tipo; tabela no OpPrint em types.go). As duas
// vão em Meta para os dois backends lerem uma forma só. Entrada desconectada
// é aviso, não erro: o maker estacionou o device, a cena ainda gera.
func (e *emitter) emitPrint(node *graph.Node, dataType string) {
	src := e.resolveInput(node.ID, "value")
	if src == "" {
		e.program.Warn("%s.value: not connected", node.ID)
		return
	}
	prefix, _ := node.Properties["prefix"].(string)
	format, _ := node.Properties["format"].(string)
	meta := map[string]string{"prefix": prefix, "format": format}
	// [PTR] The debug family is the universal probe: it also accepts a
	// pointer to its scalar type. The frontend stamps the RESOLVED wire
	// type at connection time as valueType; a trailing '*' means the
	// backends must dereference — with a null guard that prints
	// "null pointer" (a null is debug INFORMATION, not an error).
	// Português: A família debug é a lupa universal: aceita também um
	// ponteiro para seu tipo escalar. O frontend carimba o tipo RESOLVIDO
	// do fio na conexão como valueType; '*' no fim significa que os
	// backends devem dereferenciar — com guarda de nulo imprimindo
	// "null pointer" (nulo é INFORMAÇÃO de debug, não erro).
	if vt, _ := node.Properties["valueType"].(string); strings.HasSuffix(vt, "*") {
		meta["deref"] = "1"
	}
	e.program.Append(Instruction{
		Op:   OpPrint,
		Dest: node.ID,
		Type: dataType,
		Args: []string{src},
		Meta: meta,
	})
}

// =====================================================================
//  Topological sort with execution-order and Init→Loop implicit edges
// =====================================================================

// topoSort sorts nodeIDs within a scope by dependency (Kahn's algorithm) with
// two additional features:
//
//  1. Implicit ordering edges for black-box instances:
//
//     a) Init→Loop: when a BlackBoxInit node and a StatementLoop both appear
//     in the same scope, and the loop contains a BlackBoxRun of the same
//     instance, an implicit dependency edge Init→Loop is added. This
//     guarantees Init() runs before the loop body, even when the maker did
//     not draw a wire between them.
//
//     b) Init→Run (same scope): when a BlackBoxInit and a BlackBoxRun of the
//     same instance both appear in the same scope (most commonly the global
//     scope, but also the same loop body), an implicit dependency edge
//     Init→Run is added. This guarantees Run() never executes before Init(),
//     even when Run has no downstream wire connections and would otherwise
//     sort first due to zero in-degree.
//
//     Together (a) and (b) cover all layout combinations:
//     Init (global) + Run (global)       ← rule (b)
//     Init (global) + Run (inside loop)  ← rule (a)
//     Init (loop)   + Run (same loop)    ← rule (b)
//
//  2. executionOrder tie-breaking:
//     When multiple nodes are ready simultaneously (same in-degree), they are
//     ordered by executionOrder ascending (lower = first). Nodes without an
//     executionOrder (value 0) are placed after all ordered nodes. Among nodes
//     with identical effective order, IDs are sorted lexicographically for
//     deterministic output.
//
// Português:
//
//	Dois conjuntos de arestas implícitas para instâncias black-box:
//	  (a) Init→Loop: Init roda antes do loop que contém o Run correspondente.
//	  (b) Init→Run mesmo escopo: Run nunca executa antes de Init, mesmo quando
//	      Run não tem conexões downstream e teria in-degree zero.
//	Juntas cobrem todos os layouts possíveis de Init e Run.
func (e *emitter) topoSort(nodeIDs []string) ([]string, []string) {
	idSet := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		idSet[id] = true
	}

	inDegree := make(map[string]int, len(nodeIDs))
	dependents := make(map[string][]string)
	added := make(map[[2]string]bool)

	for _, id := range nodeIDs {
		inDegree[id] = 0
	}

	// Wire-based edges
	for _, edge := range e.graph.Edges {
		// Skip control-flow ports — same reasoning as analyzeScopeCrossings.
		if edge.To.PortName == "stop" || edge.To.PortName == "interval" || edge.To.PortName == "condition" {
			continue
		}

		fromID := edge.From.DeviceID
		toID := edge.To.DeviceID

		if idSet[fromID] && idSet[toID] {
			key := [2]string{fromID, toID}
			if !added[key] {
				inDegree[toID]++
				dependents[fromID] = append(dependents[fromID], toID)
				added[key] = true
			}
			continue
		}

		if !idSet[fromID] && idSet[toID] {
			containerID := e.findContainerInScope(fromID, idSet)
			if containerID != "" && containerID != toID {
				key := [2]string{containerID, toID}
				if !added[key] {
					inDegree[toID]++
					dependents[containerID] = append(dependents[containerID], toID)
					added[key] = true
				}
			}
		}
	}

	// Implicit Init→Loop edges.
	// Ensures Init() always runs before the loop that contains the matching Run().
	for _, initID := range nodeIDs {
		initNode := e.graph.Nodes[initID]
		if !strings.HasPrefix(initNode.Type, "BlackBoxInit:") {
			continue
		}
		// A C99 function-device named "Init" has type "BlackBoxInit:" with an
		// empty struct part — it is a free function, not a Go Init method.
		// Function-devices have no Init/Run pairing and order purely by wires,
		// so they take part in no implicit Init→Loop edge.
		if bbStructNameFromNode(initNode) == "" {
			continue
		}
		instanceId := e.bbInstanceId(initNode)
		for _, loopID := range nodeIDs {
			loopNode := e.graph.Nodes[loopID]
			if loopNode.Type != "StatementLoop" && loopNode.Type != "StatementLoopDuration" && loopNode.Type != "StatementIfElse" {
				continue
			}
			if e.loopContainsBBMethod(loopID, instanceId) {
				key := [2]string{initID, loopID}
				if !added[key] {
					inDegree[loopID]++
					dependents[initID] = append(dependents[initID], loopID)
					added[key] = true
				}
			}
		}
	}

	// Implicit Init→Method direct edges (same scope).
	//
	// When a BlackBoxInit and any non-Init method of the same instance both live
	// in the same scope, the method must always execute after Init. Without this
	// edge the topological sort places the method first whenever it has no other
	// dependencies (e.g. its outputs are not connected downstream).
	//
	// This generalises the old Init→Run rule to cover all named methods:
	// Run, Log, Step, Read, Write, etc.
	//
	// Together with the Init→Loop rule above, all layout combinations are covered:
	//   Init (global) + Method (global)       ← this rule
	//   Init (global) + Method (inside loop)  ← Init→Loop rule above
	//   Init (loop)   + Method (same loop)    ← this rule
	//
	// Português:
	//
	//   Garante que qualquer método não-Init sempre executa após Init quando
	//   ambos estão no mesmo escopo, mesmo sem fio conectando-os.
	for _, initID := range nodeIDs {
		initNode := e.graph.Nodes[initID]
		if !strings.HasPrefix(initNode.Type, "BlackBoxInit:") {
			continue
		}
		// Same as the Init→Loop rule: a C99 function-device named "Init"
		// ("BlackBoxInit:" with empty struct) is a free function, not an Init
		// anchor. Skip it.
		if bbStructNameFromNode(initNode) == "" {
			continue
		}
		instanceId := e.bbInstanceId(initNode)
		for _, methodID := range nodeIDs {
			methodNode := e.graph.Nodes[methodID]
			// Match any BlackBox node that is NOT Init.
			if !strings.HasPrefix(methodNode.Type, "BlackBox") {
				continue
			}
			if strings.HasPrefix(methodNode.Type, "BlackBoxInit:") {
				continue
			}
			if e.bbInstanceId(methodNode) != instanceId {
				continue
			}
			key := [2]string{initID, methodID}
			if !added[key] {
				inDegree[methodID]++
				dependents[initID] = append(dependents[initID], methodID)
				added[key] = true
			}
		}
	}

	// effectiveOrder propagates ExecutionOrder backwards through the dependency
	// graph: an unordered node (ExecutionOrder == 0) inherits the MINIMUM
	// effective order of the nodes it feeds (its dependents). This makes an
	// unordered producer — e.g. a const array — sort to exactly where its
	// ordered consumer (the method it feeds) needs to run, instead of clumping
	// at the bottom by the sentinel. Memoized via effMemo; the pre-seeded
	// sentinel also guards against cycles.
	//
	// Português: Propaga ExecutionOrder para trás. Um nó sem ordem
	// (ExecutionOrder == 0) herda a ordem mínima dos nós que ele alimenta, então
	// um produtor sem ordem (ex.: const array) ordena junto do consumidor
	// ordenado que ele alimenta. Memoizado; o sentinel pré-semeado evita ciclos.
	effMemo := make(map[string]int)
	var effectiveOrder func(id string) int
	effectiveOrder = func(id string) int {
		if v, ok := effMemo[id]; ok {
			return v
		}
		effMemo[id] = 1<<31 - 1 // cycle guard / default sentinel
		if node, ok := e.graph.Nodes[id]; ok && node.ExecutionOrder > 0 {
			effMemo[id] = node.ExecutionOrder
			return node.ExecutionOrder
		}
		best := 1<<31 - 1
		for _, dep := range dependents[id] {
			if eo := effectiveOrder(dep); eo < best {
				best = eo
			}
		}
		effMemo[id] = best
		return best
	}

	// nodeOrder returns the sort key for queue insertion. It uses the effective
	// (back-propagated) ExecutionOrder so unordered producers sort next to the
	// ordered consumers they feed; nodes with no ordered consumer fall to the
	// sentinel group and run last, broken ties lexically by id.
	nodeOrder := func(id string) (group int, lexID string) {
		return effectiveOrder(id), id
	}

	// insertSorted inserts id into queue maintaining dual sort order.
	insertSorted := func(queue []string, id string) []string {
		g1, l1 := nodeOrder(id)
		pos := len(queue)
		for i, existing := range queue {
			g2, l2 := nodeOrder(existing)
			if g1 < g2 || (g1 == g2 && l1 < l2) {
				pos = i
				break
			}
		}
		queue = append(queue, "")
		copy(queue[pos+1:], queue[pos:])
		queue[pos] = id
		return queue
	}

	var queue []string
	for _, id := range nodeIDs {
		if inDegree[id] == 0 {
			queue = insertSorted(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		for _, dep := range dependents[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = insertSorted(queue, dep)
			}
		}
	}

	if len(sorted) != len(nodeIDs) {
		return sorted, []string{fmt.Sprintf("cycle detected in scope (sorted %d of %d nodes)", len(sorted), len(nodeIDs))}
	}

	return sorted, nil
}

// loopContainsBBMethod returns true if the given loop scope contains a non-Init
// BlackBox node (any method: Run, Log, Step, …) with the matching instanceId.
//
// Previously called loopContainsBBRun — renamed and generalised to cover all
// non-Init methods now that a component may have N named methods.
//
// Português: Retorna true se o escopo do loop contém qualquer node BlackBox
// não-Init (qualquer método: Run, Log, Step…) com o instanceId correspondente.
func (e *emitter) loopContainsBBMethod(loopID, instanceId string) bool {
	scope, ok := e.graph.Scopes[loopID]
	if !ok {
		return false
	}
	for _, nodeID := range scope.NodeIDs {
		node, ok := e.graph.Nodes[nodeID]
		if !ok {
			continue
		}
		// Match any BlackBox node that is NOT Init.
		if !strings.HasPrefix(node.Type, "BlackBox") {
			continue
		}
		if strings.HasPrefix(node.Type, "BlackBoxInit:") {
			continue
		}
		if e.bbInstanceId(node) == instanceId {
			return true
		}
	}
	return false
}

func (e *emitter) findContainerInScope(nodeID string, scopeSet map[string]bool) string {
	current := e.graph.ScopeOf(nodeID)
	for current != "" {
		if scopeSet[current] {
			return current
		}
		if s, ok := e.graph.Scopes[current]; ok {
			current = s.ParentID
		} else {
			return ""
		}
	}
	return ""
}

// =====================================================================
//  Input resolution
// =====================================================================

func (e *emitter) resolveInput(nodeID, portName string) string {
	sources := e.graph.GetInputSources(nodeID, portName)
	if len(sources) == 0 {
		return ""
	}
	return e.resolveInput2(sources[0].DeviceID, sources[0].PortName)
}

func (e *emitter) resolveInput2(deviceID, portName string) string {
	srcNode, ok := e.graph.Nodes[deviceID]
	if !ok {
		return "%" + deviceID
	}

	// A GetVar is a register alias: it produces no value of its own — its
	// output IS the project variable's register. Resolve a consumer straight
	// to that register so no intermediate copy is emitted.
	// Português: GetVar é alias de registrador: sua saída É o registrador da
	// variável de projeto. Resolve o consumidor direto pra esse registrador,
	// sem cópia intermediária.
	if vn, ok := getVarName(srcNode); ok {
		return "%" + vn
	}

	// A StatementTunnel is FRONT-END FURNITURE (Kemper, 2026-07-18) —
	// the third alias sibling, next to GetVar above and the C99
	// PassThrough below: it produces no value of its own. A consumer of
	// tunnel.out reads whatever feeds tunnel.in, chains collapsing via
	// tunnelRealSource. A dangling chain resolves "" so the CONSUMER's
	// own unconnected policy speaks (binops warn and pass 0, prints warn
	// and skip) — while skipTunnel names the tunnel itself in a warning.
	// Português: Túnel é peça de FRONT-END — terceiro irmão de alias,
	// junto do GetVar acima e do PassThrough abaixo: não produz valor.
	// Quem lê tunnel.out lê o que alimenta tunnel.in (cadeias colapsam
	// via tunnelRealSource). Cadeia solta resolve "" — a política do
	// próprio consumidor fala, e o skipTunnel nomeia o túnel no aviso.
	// A graphical-function instance's output IS the call's destination
	// register (<instance>_<return>) — declared by the backend at the
	// OpCall site. Português: A saída de uma instância É o registrador
	// de destino da chamada.
	if srcNode.Type == graph.FunctionCallType {
		return "%" + srcNode.ID + "_" + portName
	}

	if srcNode.Type == "StatementTunnel" {
		// FOURTH alias sibling (Fatia C): a function PARAMETER tunnel
		// resolves to the parameter identifier itself — raw, the
		// folded-const channel — and never pierces outward: the caller
		// supplies the value. Português: QUARTO irmão de alias — túnel
		// de PARÂMETRO resolve para o identificador do parâmetro (cru),
		// sem atravessar: o caller fornece o valor.
		if pname, ok := e.funcParamName[srcNode.ID]; ok {
			return pname
		}
		real, wired := e.tunnelRealSource(srcNode)
		if !wired {
			return ""
		}
		return e.resolveInput2(real.DeviceID, real.PortName)
	}

	// Any BlackBox node (Init or any named method) uses the compound register
	// form "%instanceId:portName" so the backend can resolve it to a named
	// Go variable (e.g. "i2cBus1_bus").
	//
	// Português: Qualquer node BlackBox usa a forma composta de registro.
	if strings.HasPrefix(srcNode.Type, "BlackBox") {
		// C99 PassThrough: a synthesized "<handle>_out" output republishes
		// the function's wire-type handle input unchanged — it is an alias,
		// not a produced value. A consumer of it must therefore read whatever
		// feeds the handle input, so we follow the wire one hop back. Without
		// this the consumer would reference an undeclared "<node>:<handle>_out"
		// and the generated C would not compile.
		// Português: PassThrough é alias do handle de entrada; segue o fio uma
		// vez pra trás em vez de referenciar um registro nunca declarado.
		if handleInput, ok := e.passThroughInput(srcNode, portName); ok {
			if srcs := e.graph.GetInputSources(srcNode.ID, handleInput); len(srcs) > 0 {
				return e.resolveInput2(srcs[0].DeviceID, srcs[0].PortName)
			}
			// Handle input unwired — fall through to the composite form; the
			// missing mandatory connection is surfaced by validate().
		}

		// C99 function-devices have NO shared instance: each call is
		// standalone, so the composite register is keyed by node.ID — exactly
		// what emitBlackBoxCall uses for Dest and for the out-param registers.
		// Go struct methods, by contrast, share an instanceId across all of an
		// instance's method nodes, so those resolve through bbInstanceId. Using
		// bbInstanceId for a function-device would mis-key the consumer when the
		// IDE assigns it an instanceId that differs from node.ID (the producer
		// would declare "<nodeId>_return" while the consumer read
		// "<instanceId>_return").
		if bbStructNameFromNode(srcNode) == "" {
			return "%" + srcNode.ID + ":" + portName
		}
		instanceId := e.bbInstanceId(srcNode)
		return "%" + instanceId + ":" + portName
	}

	// The array-index reader (StatementIndex*) has two outputs. Its "value"
	// output falls through to the single-register default below (%deviceID); its
	// "ok" output is a synthesized companion register, matching the {id}_ok
	// register emitIndex declares for it.
	//
	// Português: O leitor de índice (StatementIndex*) tem duas saídas. O "value"
	// cai no default single-register abaixo (%deviceID); o "ok" é o companheiro
	// sintetizado, casando com o registrador {id}_ok que o emitIndex declara.
	if strings.HasPrefix(srcNode.Type, "StatementIndex") && portName == "ok" {
		return "%" + deviceID + "_ok"
	}

	return "%" + deviceID
}

// passThroughInput reports whether (node, portName) is a C99 PassThrough output
// — a synthesized "<handle>_out" port that republishes a wire-type input — and
// if so returns the name of the aliased input. Only C99 function-devices
// (empty struct part) synthesize pass-throughs, and only the def knows which
// ports they are, so this consults FunctionSynthesizedOutputs (the authoritative
// source, same one the IDE uses). Returns ok=false when bbDefs is absent, the
// node is not a C99 function-device, or the port is a real output.
//
// Português: Diz se (node, portName) é uma saída PassThrough C99 sintetizada
// ("<handle>_out") e, se for, devolve o input wire-type que ela espelha. Só
// function-devices C99 sintetizam; a fonte autoritativa é o def via
// FunctionSynthesizedOutputs.
func (e *emitter) passThroughInput(node *graph.Node, portName string) (string, bool) {
	if e.bbDefs == nil || bbStructNameFromNode(node) != "" {
		return "", false
	}
	def := e.bbDefs[bbMethodNameFromNode(node)]
	if def == nil {
		return "", false
	}
	fnName := bbMethodNameFromNode(node)
	for _, fn := range def.Functions {
		if fn.Name != fnName {
			continue
		}
		for _, out := range def.FunctionSynthesizedOutputs(fn.FuncDef) {
			if !out.PassThrough || out.Name != portName {
				continue
			}
			// Synthesized name is "<inputName>_out" — recover the input it
			// republishes by matching against the function's inputs.
			for _, in := range fn.Inputs {
				if in.Name+"_out" == out.Name {
					return in.Name, true
				}
			}
			return strings.TrimSuffix(out.Name, "_out"), true
		}
		break
	}
	return "", false
}

// resolveInputType returns the concrete or abstract type of whatever
// is wired into (nodeID, portName). Reads the DataType declared on the
// producer's output port. Returns "" if the input is unconnected or
// the producer doesn't declare a type — callers treat this as "skip
// type checking" rather than failing loudly, because missing-connection
// is already a separate diagnostic surfaced by validate() in codeGen.go.
//
// Português: Retorna o tipo do que alimenta (nodeID, portName). Lê o
// DataType da porta de saída do produtor. "" = sem conexão ou produtor
// sem tipo declarado — nesse caso o chamador pula a verificação de
// tipos (a conexão faltante já é reportada por outro passo).
func (e *emitter) resolveInputType(nodeID, portName string) string {
	sources := e.graph.GetInputSources(nodeID, portName)
	if len(sources) == 0 {
		return ""
	}
	src := sources[0]
	srcNode, ok := e.graph.Nodes[src.DeviceID]
	if !ok {
		return ""
	}
	// See resolveInput2's tunnel branch: the tunnel is front-end
	// furniture, so the TYPE also flows through it to the real
	// producer's declared port. Without this hop, type checking would
	// read the shell instead of the source. Português: O TIPO também
	// atravessa o túnel até a porta declarada do produtor real — sem
	// este pulo, a checagem leria a casca em vez da fonte.
	if srcNode.Type == graph.FunctionCallType {
		if fn, _ := srcNode.Properties["function"].(string); fn != "" {
			for _, sc := range e.graph.Scopes {
				if sc != nil && sc.Function && sc.FunctionName == fn {
					for _, r := range sc.FuncReturns {
						if r.Name == portName {
							return r.Type
						}
					}
				}
			}
		}
		return ""
	}

	if srcNode.Type == "StatementTunnel" {
		// FOURTH alias sibling, type side (Fatia C): a parameter
		// tunnel's type is the signature slot's type — no piercing.
		// Português: Quarto irmão, lado do tipo — túnel de parâmetro
		// responde o tipo do slot da assinatura, sem atravessar.
		if ptype, ok := e.funcParamType[srcNode.ID]; ok {
			return ptype
		}
		real, wired := e.tunnelRealSource(srcNode)
		if !wired {
			return ""
		}
		src = real
		srcNode, ok = e.graph.Nodes[src.DeviceID]
		if !ok {
			return ""
		}
	}
	for _, p := range srcNode.Outputs {
		if p.Name == src.PortName {
			return p.DataType
		}
	}
	return ""
}

// emitConvert appends an OpConvert instruction that produces a new
// temporary register holding `src` converted to `targetType`, and
// returns the name of that temporary so the caller can use it as an
// operand downstream. The temp name uses a monotonic counter so
// repeated casts in the same scope never collide.
//
// Português: Insere OpConvert e retorna o nome do registro temporário.
// Usa contador monotônico para nomes distintos.
func (e *emitter) emitConvert(src, srcType, targetType string) string {
	dest := fmt.Sprintf("conv_%d", e.convertCounter)
	e.convertCounter++
	e.program.Append(Instruction{
		Op:   OpConvert,
		Dest: dest,
		Type: targetType,
		Args: []string{src},
		// [BOOL→INT] the SOURCE type rides Meta so backends whose language
		// has no direct cast for a pair can pick a different rendering —
		// Go cannot `int64(someBool)` and needs the 0/1 temp pattern.
		// Português: O tipo de ORIGEM viaja no Meta para backends cuja
		// linguagem não tem cast direto para um par escolherem outra
		// renderização — Go não faz `int64(someBool)` e precisa do padrão
		// temp 0/1.
		Meta: map[string]string{"srcType": srcType},
	})
	return "%" + dest
}

// =====================================================================
//  Type helpers
// =====================================================================

func (e *emitter) inferOutputType(node *graph.Node) string {
	for _, p := range node.Outputs {
		if p.DataType != "" {
			return p.DataType
		}
	}
	for _, p := range node.Inputs {
		if p.DataType != "" {
			return p.DataType
		}
	}
	return "int"
}

func zeroValue(dataType string) string {
	switch dataType {
	case "bool":
		return "false"
	case "float", "float32", "float64":
		return "0.0"
	case "string":
		return `""`
	default:
		return "0"
	}
}

// stampNodeComment attaches the node's Inspect comment to the instruction at
// index `before` — the first one this node emitted. Deferred from emitNode:
// runs after the dispatch, whatever path it took. Skips silently when the
// node has no comment or emitted nothing.
//
// Português: Anexa o comentário do Inspect do node à instrução no índice
// `before` — a primeira que este node emitiu. Deferido do emitNode: roda
// após o dispatch, qualquer que seja o caminho. Sai em silêncio quando o
// node não tem comentário ou não emitiu nada.
func (e *emitter) stampNodeComment(node *graph.Node, before int) {
	if len(e.program.Instructions) <= before {
		return
	}
	e.stampCommentAt(node, before)
}

// stampScopeComment attaches a container node's comment to the instruction
// just appended (LOOP_BEGIN / COND_BEGIN). Containers don't flow through
// emitNode — the scope walkers emit their begin/end frames — so they get
// this dedicated stamp right after the begin Append.
//
// Português: Anexa o comentário de um node container à instrução recém
// anexada (LOOP_BEGIN / COND_BEGIN). Containers não passam pelo emitNode —
// os walkers de escopo emitem seus frames — então ganham este carimbo
// dedicado logo após o Append do begin.
func (e *emitter) stampScopeComment(scopeID string) {
	node, ok := e.graph.Nodes[scopeID]
	if !ok {
		return
	}
	n := len(e.program.Instructions)
	if n == 0 {
		return
	}
	e.stampCommentAt(node, n-1)
}

// stampCommentAt is the shared core: reads Properties["comment"], trims it,
// and writes Meta["comment"] on the instruction at idx. Meta is the IR's
// documented "extra metadata" bag — no Instruction struct change needed.
//
// Português: Núcleo compartilhado: lê Properties["comment"], apara e grava
// Meta["comment"] na instrução em idx. Meta é a bolsa documentada de
// "metadados extras" do IR — sem mudança na struct Instruction.
func (e *emitter) stampCommentAt(node *graph.Node, idx int) {
	if node == nil || node.Properties == nil {
		return
	}
	c, _ := node.Properties["comment"].(string)
	c = strings.TrimSpace(c)
	if c == "" {
		return
	}
	inst := &e.program.Instructions[idx]
	if inst.Meta == nil {
		inst.Meta = map[string]string{}
	}
	inst.Meta["comment"] = c
}
