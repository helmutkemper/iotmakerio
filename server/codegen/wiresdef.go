// server/codegen/wiresdef.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// wiresdef.go — the WIRES-ORIGIN black-box merge engine (Kemper
// 2026-07-19, the "my_function becomes a device" arc).
//
// English:
//
//	A graphical function saved to My Items is a black-box whose body is
//	a SUB-SCENE (BlackBoxDef.Origin == "wires", .Scene == the captured
//	SceneJSON). At codegen time this engine EXPANDS every referenced
//	wires-def into the main scene BEFORE the graph is built: the sub-
//	scene's Function container, tunnels, members and internal wires are
//	injected with a collision-proof id prefix, and the existing Fatia C
//	pipeline then does everything it already knows — derives the
//	signature from the tunnels, validates it, emits the function in
//	both backends. Instances on the stage (StatementFunctionCall
//	devices) lower to real calls in the IR (see emit.go's OpCall path).
//
//	Injection is TRANSITIVE (a wires function may call other wires
//	functions — B4) with a CYCLE GUARD (direct or indirect recursion is
//	an error: neither target has a stack story for maker recursion in
//	v1) and a NAME-COLLISION GUARD (a def whose function name matches a
//	function already authored in the main scene is an error — one name,
//	one meaning).
//
// Português:
//
//	Uma função gráfica salva em My Items é um black-box cujo corpo é
//	uma SUB-CENA. Na geração, este motor EXPANDE cada def-de-fios
//	referenciada para dentro da cena principal ANTES do grafo nascer:
//	o container Function, túneis, membros e fios internos entram com um
//	prefixo de id à prova de colisão, e o pipeline da Fatia C faz o que
//	já sabe — deriva a assinatura dos túneis, valida, emite a função
//	nos dois backends. Instâncias no palco (StatementFunctionCall)
//	viram chamadas reais no IR.
//
//	A injeção é TRANSITIVA (função chama função — B4) com GUARDA DE
//	CICLO (recursão direta ou indireta é erro no v1) e GUARDA DE
//	COLISÃO DE NOME (def cujo nome bate com função já desenhada na cena
//	principal é erro — um nome, um significado).

import (
	"encoding/json"
	"fmt"
	"strings"

	"server/codegen/blackbox"
	"server/codegen/diagnostics"
	"server/codegen/graph"
	"server/codegen/ir"
)

// calledFunctionName extracts the referenced def name from an instance
// device, "" when absent. Português: Extrai o nome da def referenciada
// por uma instância; "" quando ausente.
func calledFunctionName(dev graph.DeviceInput) string {
	if dev.Type != graph.FunctionCallType || dev.Properties == nil {
		return ""
	}
	name, _ := dev.Properties["function"].(string)
	return strings.TrimSpace(name)
}

// mergeWiresDefs expands every wires-origin def referenced by the scene
// (transitively) into the scene itself. Returns diagnostics; any error
// severity means the caller must stop before building the graph.
// Português: Expande toda def-de-fios referenciada (transitivamente)
// para dentro da cena. Qualquer erro nos diagnósticos exige parada
// antes do grafo.
func mergeWiresDefs(scene *graph.SceneInput, defs map[string]*blackbox.BlackBoxDef) []Diagnostic {
	var diags []Diagnostic

	// The main scene's own function names — the collision guard's left
	// side. Português: Nomes de função da cena principal — o lado
	// esquerdo da guarda de colisão.
	normalizeWiresInstances(scene, defs)

	mainFunctions := map[string]bool{}
	for _, dev := range scene.Devices {
		if dev.Type == "StatementFunction" {
			if n, _ := dev.Properties["functionName"].(string); n != "" {
				mainFunctions[n] = true
			}
		}
	}

	injected := map[string]bool{}

	var inject func(name string, path []string) bool
	inject = func(name string, path []string) bool {
		if injected[name] {
			return true
		}
		for _, p := range path {
			if p == name {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindFunctionCycle,
					Severity: diagnostics.SeverityError,
					Message: fmt.Sprintf(
						"graphical function %q calls itself (cycle: %s → %s) — recursion between drawn functions is not supported",
						name, strings.Join(path, " → "), name),
				})
				return false
			}
		}
		def := findWiresDef(defs, name)
		if def == nil {
			diags = append(diags, Diagnostic{
				Kind:     diagnostics.KindFunctionSignature,
				Severity: diagnostics.SeverityError,
				Message: fmt.Sprintf(
					"graphical function %q is used on the stage but its definition was not found in My Items",
					name),
			})
			return false
		}
		if mainFunctions[name] {
			diags = append(diags, Diagnostic{
				Kind:     diagnostics.KindFunctionSignature,
				Severity: diagnostics.SeverityError,
				Message: fmt.Sprintf(
					"graphical function %q collides with a function of the same name drawn in this scene — one name, one meaning; rename one of them",
					name),
			})
			return false
		}

		var sub graph.SceneInput
		if err := json.Unmarshal(def.Scene, &sub); err != nil {
			diags = append(diags, Diagnostic{
				Kind:     diagnostics.KindFunctionSignature,
				Severity: diagnostics.SeverityError,
				Message: fmt.Sprintf(
					"graphical function %q has an unreadable body: %v", name, err),
			})
			return false
		}

		// Recurse FIRST into the defs this def calls (B4 transitivity),
		// carrying the path for the cycle guard. Português: Recorre
		// PRIMEIRO nas defs que esta def chama, carregando o caminho.
		for _, dev := range sub.Devices {
			if callee := calledFunctionName(dev); callee != "" {
				if !inject(callee, append(path, name)) {
					return false
				}
			}
		}

		remapSceneIDs(&sub, "fx"+sanitizeDefPrefix(name)+"_")
		scene.Devices = append(scene.Devices, sub.Devices...)
		scene.Wires = append(scene.Wires, sub.Wires...)
		injected[name] = true
		return true
	}

	for _, dev := range scene.Devices {
		if callee := calledFunctionName(dev); callee != "" {
			inject(callee, nil)
		}
	}
	return diags
}

// ExtractFunctionDef captures ONE graphical function out of a full
// scene and packages it as a wires-origin BlackBoxDef ready for My
// Items (P3): the Function container, its transitive children, its
// tunnels (tunnelParent) and every wire whose both ends live in the
// captured set. The signature is validated HERE — before anything is
// stored — by the same rules the emitter enforces (valid identifier
// name, every slot typed, no duplicate names), so a broken function
// never reaches the shelf. Português: Captura UMA função gráfica da
// cena completa e a empacota como def de origem-fios pronta para My
// Items: o container, filhos transitivos, túneis e fios internos. A
// assinatura é validada AQUI — antes de qualquer gravação — pelas
// mesmas regras do emitter, para função quebrada nunca chegar à
// prateleira.
func ExtractFunctionDef(sceneJSON []byte, functionID string) (*blackbox.BlackBoxDef, []Diagnostic) {
	var scene graph.SceneInput
	if err := json.Unmarshal(sceneJSON, &scene); err != nil {
		return nil, []Diagnostic{{
			Kind: diagnostics.KindFunctionSignature, Severity: diagnostics.SeverityError,
			Message: fmt.Sprintf("scene unreadable: %v", err),
		}}
	}

	byID := map[string]*graph.DeviceInput{}
	for i := range scene.Devices {
		byID[scene.Devices[i].ID] = &scene.Devices[i]
	}
	fn := byID[functionID]
	if fn == nil || fn.Type != "StatementFunction" {
		return nil, []Diagnostic{{
			Kind: diagnostics.KindFunctionSignature, Severity: diagnostics.SeverityError,
			Message: fmt.Sprintf("device %q is not a Function container", functionID),
		}}
	}
	fnName, _ := fn.Properties["functionName"].(string)

	// The captured set: the container, its transitive children, and its
	// tunnels. Português: O conjunto capturado — container, filhos
	// transitivos e túneis.
	keep := map[string]bool{functionID: true}
	var walk func(id string)
	walk = func(id string) {
		d := byID[id]
		if d == nil {
			return
		}
		for _, c := range d.Containment.Children {
			if !keep[c] {
				keep[c] = true
				walk(c)
			}
		}
	}
	walk(functionID)
	for i := range scene.Devices {
		d := &scene.Devices[i]
		if tp, _ := d.Properties["tunnelParent"].(string); tp == functionID {
			keep[d.ID] = true
		}
	}

	sub := graph.SceneInput{Version: scene.Version, Metadata: scene.Metadata}
	for i := range scene.Devices {
		if keep[scene.Devices[i].ID] {
			sub.Devices = append(sub.Devices, scene.Devices[i])
		}
	}
	for _, w := range scene.Wires {
		if keep[w.From.Device] && keep[w.To.Device] {
			sub.Wires = append(sub.Wires, w)
		}
	}

	// Validation: build the sub-graph and judge the derived signature
	// with the emitter's own rules. Português: Constrói o sub-grafo e
	// julga a assinatura com as regras do próprio emitter.
	var diags []Diagnostic
	if !ValidFunctionName(fnName) {
		diags = append(diags, Diagnostic{
			Kind: diagnostics.KindFunctionNameInvalid, Severity: diagnostics.SeverityError,
			Devices: []string{functionID},
			Message: fmt.Sprintf("function name %q is not a valid identifier", fnName),
		})
	}
	g, _ := graph.Build(sub)
	scope := g.Scopes[functionID]
	if scope == nil || !scope.Function {
		diags = append(diags, Diagnostic{
			Kind: diagnostics.KindFunctionSignature, Severity: diagnostics.SeverityError,
			Devices: []string{functionID},
			Message: "captured scene did not produce a function scope",
		})
	} else {
		seen := map[string]string{}
		for _, group := range [][]graph.FuncPort{scope.FuncParams, scope.FuncReturns} {
			for _, p := range group {
				if p.Type == "" {
					diags = append(diags, Diagnostic{
						Kind: diagnostics.KindFunctionSignature, Severity: diagnostics.SeverityError,
						Devices: []string{p.TunnelID},
						Message: fmt.Sprintf("signature slot %q has no type — wire something to it before saving", p.Name),
					})
				}
				if other, dup := seen[p.Name]; dup {
					diags = append(diags, Diagnostic{
						Kind: diagnostics.KindFunctionSignature, Severity: diagnostics.SeverityError,
						Devices: []string{p.TunnelID, other},
						Message: fmt.Sprintf("signature name %q is used by two tunnels — rename one before saving", p.Name),
					})
				}
				seen[p.Name] = p.TunnelID
			}
		}
	}
	if hasErrorSeverity(diags) {
		return nil, diags
	}

	subJSON, err := json.Marshal(sub)
	if err != nil {
		return nil, []Diagnostic{{
			Kind: diagnostics.KindFunctionSignature, Severity: diagnostics.SeverityError,
			Message: fmt.Sprintf("captured scene could not be serialised: %v", err),
		}}
	}
	return &blackbox.BlackBoxDef{
		Name:   fnName,
		Origin: "wires",
		Scene:  subJSON,
	}, diags
}

// WiresSignature derives a wires def's signature (parameter and
// return slots, in rail order) straight from its stored sub-scene —
// the same collector truth the emitter uses. The list endpoint calls
// this to synthesise the menu-facing function entry (P4). Português:
// Deriva a assinatura da def direto da sub-cena — a mesma verdade do
// coletor; o endpoint de listagem sintetiza a entrada de menu com ela.
func WiresSignature(def *blackbox.BlackBoxDef) (params, returns []graph.FuncPort, err error) {
	if def == nil || def.Origin != "wires" || len(def.Scene) == 0 {
		return nil, nil, fmt.Errorf("not a wires def")
	}
	var sub graph.SceneInput
	if e := json.Unmarshal(def.Scene, &sub); e != nil {
		return nil, nil, e
	}
	g, _ := graph.Build(sub)
	for _, sc := range g.Scopes {
		if sc != nil && sc.Function {
			return sc.FuncParams, sc.FuncReturns, nil
		}
	}
	return nil, nil, fmt.Errorf("no function scope in def scene")
}

// normalizeWiresInstances rewrites method-block nodes that reference a
// WIRES def into pure StatementFunctionCall nodes BEFORE the graph is
// built (P4): the menu creates instances through the shared
// StatementBlackBoxMethod sprite, whose scene Type is
// "BlackBox<fn>:<def>" — for a wires def that node must lower through
// the P2 call engine, not the C99 black-box path. Ports stay as
// serialised (the method block's pin names ARE the parameter names by
// design). Português: Reescreve nodes de method-block que referenciam
// def de FIOS em StatementFunctionCall puros ANTES do grafo — o menu
// cria instâncias pelo sprite compartilhado; para def de fios o node
// rebaixa pelo motor P2, não pelo caminho C99. Portas ficam como
// serializadas (os pinos SÃO os nomes dos parâmetros).
func normalizeWiresInstances(scene *graph.SceneInput, defs map[string]*blackbox.BlackBoxDef) {
	for i := range scene.Devices {
		d := &scene.Devices[i]
		if !strings.HasPrefix(d.Type, "BlackBox") {
			continue
		}
		colon := strings.LastIndex(d.Type, ":")
		if colon < 0 {
			continue
		}
		defName := d.Type[colon+1:]
		if findWiresDef(defs, defName) == nil {
			continue
		}
		d.Type = graph.FunctionCallType
		if d.Properties == nil {
			d.Properties = map[string]interface{}{}
		}
		d.Properties["function"] = defName
	}
}

// findWiresDef locates a wires-origin def by function name.
// Português: Localiza uma def de origem-fios pelo nome.
func findWiresDef(defs map[string]*blackbox.BlackBoxDef, name string) *blackbox.BlackBoxDef {
	for _, d := range defs {
		if d != nil && d.Origin == "wires" && d.Name == name && len(d.Scene) > 0 {
			return d
		}
	}
	return nil
}

// ValidFunctionName — the ir package's law, re-exported here so the
// handler layer needs only the codegen import. Português: A lei do
// pacote ir, re-exportada para o handler importar só codegen.
func ValidFunctionName(name string) bool { return ir.ValidFunctionName(name) }

// sanitizeDefPrefix makes the def name safe as an id fragment.
// Português: Torna o nome seguro como fragmento de id.
func sanitizeDefPrefix(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		ok := r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')
		if !ok {
			r = '_'
		}
		out = append(out, r)
	}
	return string(out)
}

// remapSceneIDs prefixes every device id in the sub-scene and rewrites
// every reference to it: containment (parent/children/overlapping),
// connector connections, wire endpoints, and the tunnels' tunnelParent
// property. The prefix is collision-proof against the main scene and
// against other defs. Português: Prefixa todo id de device da sub-cena
// e reescreve toda referência: containment, conexões de conector,
// pontas de fio e o tunnelParent dos túneis.
func remapSceneIDs(sc *graph.SceneInput, prefix string) {
	old := map[string]bool{}
	for i := range sc.Devices {
		old[sc.Devices[i].ID] = true
	}
	ren := func(id string) string {
		if id != "" && old[id] {
			return prefix + id
		}
		return id
	}
	for i := range sc.Devices {
		d := &sc.Devices[i]
		d.ID = ren(d.ID)
		d.Containment.Parent = ren(d.Containment.Parent)
		for j := range d.Containment.Children {
			d.Containment.Children[j] = ren(d.Containment.Children[j])
		}
		for j := range d.Containment.Overlapping {
			d.Containment.Overlapping[j] = ren(d.Containment.Overlapping[j])
		}
		for j := range d.Connectors {
			for k := range d.Connectors[j].Connections {
				c := &d.Connectors[j].Connections[k]
				c.TargetDevice = ren(c.TargetDevice)
			}
		}
		if d.Properties != nil {
			if tp, _ := d.Properties["tunnelParent"].(string); tp != "" {
				d.Properties["tunnelParent"] = ren(tp)
			}
		}
	}
	for i := range sc.Wires {
		sc.Wires[i].From.Device = ren(sc.Wires[i].From.Device)
		sc.Wires[i].To.Device = ren(sc.Wires[i].To.Device)
	}
}
