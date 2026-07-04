// /server/codegen/codeGen.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// codegen.go — Entry point for the code generation pipeline.
//
// SceneJSON (from IDE export) → Graph → IR → Backend → Source Code
//
// Black-box support: Request includes BlackBoxDefs which are passed through
// to the IR Program. The Go backend uses them to emit struct definitions,
// imports, and method bodies.
//
// Multi-language support:
//
// The pipeline is language-agnostic up to Step 5. Step 5 dispatches on
// req.Language to a concrete backend package. Each backend reads the
// same ir.Program and writes its own target source.
//
// Português: Ponto de entrada para o pipeline de geração de código.
// Suporte a black-box: Request inclui BlackBoxDefs que são passados para
// o Program IR. O backend Go usa para emitir structs, imports e métodos.
// O pipeline é agnóstico de linguagem até a Etapa 5, que despacha para o
// backend concreto de acordo com req.Language.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"server/codegen/backend/ansic"
	"server/codegen/backend/golang"
	"server/codegen/blackbox"
	"server/codegen/diagnostics"
	"server/codegen/graph"
	"server/codegen/ir"
	"server/codegen/target"
)

// Diagnostic is re-exported from the diagnostics subpackage so external
// callers (HTTP handlers, WASM client) import a single codegen.Diagnostic
// type instead of reaching across subpackages.
//
// Português: Reexportado do subpacote diagnostics pra callers externos
// verem um único codegen.Diagnostic.
type Diagnostic = diagnostics.Diagnostic

// Request is the input to the code generation pipeline.
type Request struct {
	Scene    json.RawMessage `json:"scene"`    // SceneJSON from the IDE
	Language string          `json:"language"` // target language: "go", "c", "python"

	// BlackBoxDefs are the black-box definitions used by the scene.
	// Key is the struct name (e.g. "APDS9960").
	// Loaded from the server's black-box registry.
	//
	// Português: Definições de black-box usadas pela cena.
	// Chave é o nome do struct. Carregadas do registro de black-box do servidor.
	BlackBoxDefs map[string]*blackbox.BlackBoxDef `json:"-"` // not from JSON; set by handler

	// Variables are the user-declared project variables (GetVar/SetVar
	// devices). The handler loads them from the project_variables table by
	// project_id and sets them here; they are not part of the scene JSON. The
	// IR emits one zero-initialised declaration per variable.
	//
	// Português: Variáveis de projeto declaradas pelo usuário (devices
	// GetVar/SetVar). O handler as carrega da tabela project_variables por
	// project_id; não vêm do JSON da cena. O IR emite uma declaração zero-init
	// por variável.
	Variables []ir.VariableDecl `json:"-"` // not from JSON; set by handler
}

// Response is the output of the code generation pipeline.
//
// Two output shapes coexist by design:
//
//   - Single-file backends (Go) populate Code with the entire generated
//     source and leave Files nil. The client treats Code as a single
//     file named after the target's convention (main.go).
//
//   - Multi-file backends (C, and any future backend that needs a
//     header plus an implementation file) populate Files with a map
//     of relative path to content. The client zips each entry into
//     the download. Code may be left empty or used for a short
//     human-readable summary.
//
// Clients decide by checking Files first: if non-nil and non-empty, use
// it; otherwise fall back to Code wrapped as a single-entry zip. This
// keeps the legacy single-file path byte-for-byte unchanged.
//
// The legacy Errors and Warnings slices are string mirrors of the
// Message field of each Diagnostic, preserved so existing clients that
// parse only those fields keep working. New clients should consume
// Diagnostics instead, which carries device IDs and scope so the IDE
// can highlight the right nodes on the canvas.
//
// Português: Errors e Warnings são espelhos textuais dos Diagnostics
// pra manter compat. Clientes novos devem ler Diagnostics que tem
// device IDs e scope para realce no canvas.
// Code carrega arquivo único (Go). Files carrega múltiplos arquivos (C).
// Cliente decide pela presença de Files; ausência cai no caminho legado.
type Response struct {
	Code        string            `json:"code"`            // generated source code (single-file backends)
	Files       map[string]string `json:"files,omitempty"` // generated source files (multi-file backends)
	IR          string            `json:"ir,omitempty"`    // IR text (for debug)
	Errors      []string          `json:"errors"`          // fatal error messages (legacy mirror)
	Warnings    []string          `json:"warnings"`        // non-fatal warning messages (legacy mirror)
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
}

// addDiagnostic appends a diagnostic to the response and mirrors its
// message into the legacy Errors/Warnings slices so old clients keep
// working until they migrate to reading Diagnostics directly.
func (r *Response) addDiagnostic(d Diagnostic) {
	r.Diagnostics = append(r.Diagnostics, d)
	if d.Error() {
		r.Errors = append(r.Errors, d.Message)
	} else {
		r.Warnings = append(r.Warnings, d.Message)
	}
}

// addDiagnostics is the bulk variant.
func (r *Response) addDiagnostics(ds []Diagnostic) {
	for _, d := range ds {
		r.addDiagnostic(d)
	}
}

// hasErrorSeverity reports whether the slice contains at least one
// diagnostic with error severity. Used to decide whether a pipeline
// step's output should block the rest of codegen — warnings are
// additive and must not prevent emission of the code.
//
// Português: True se houver pelo menos um diagnóstico com severity
// error. Warnings são aditivos e não devem bloquear.
func hasErrorSeverity(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == diagnostics.SeverityError {
			return true
		}
	}
	return false
}

// Generate runs the full codegen pipeline.
//
// The pipeline is pure CPU and has no IO — no network, no disk, no
// channels — so cancellation is observed only between steps, not
// inside them. A caller that wants finer-grained cancellation would
// need to refactor the individual sub-packages (graph, ir, backend)
// to accept context themselves; the four checkpoints below cover
// >95% of the wall-clock budget for any scene we have measured
// because parsing and backend emission are quick, while build/
// validate/emit-ir together account for the bulk of the time.
//
// Português:
//
//	Pipeline é puramente CPU sem IO. Cancelamento é observado entre
//	as 5 etapas, não dentro delas. Para granularidade fina dentro de
//	graph/ir/backend, esses sub-pacotes precisariam aceitar ctx também.
//	Os 4 checkpoints cobrem >95% do tempo de wall-clock medido.
func Generate(ctx context.Context, req Request) Response {
	resp := Response{
		Errors:   make([]string, 0),
		Warnings: make([]string, 0),
	}

	// Step 1: Parse scene JSON
	var scene graph.SceneInput
	if err := json.Unmarshal(req.Scene, &scene); err != nil {
		resp.addDiagnostic(Diagnostic{
			Kind:     diagnostics.KindSceneParse,
			Severity: diagnostics.SeverityError,
			Message:  fmt.Sprintf("invalid scene JSON: %v", err),
		})
		return resp
	}

	// Step 1b: Project variables (Path A — embedded in the scene).
	//
	// Codegen is a stateless scene→code step: the live /codegen route carries
	// only :language and no project_id, so the scene must be the COMPLETE input
	// — exactly as it already is for devices, wires and black-boxes. The IDE
	// embeds the project's variable declarations as a top-level "variables"
	// array (loaded from the project_variables table, which IS project-scoped
	// on the IDE side). We read them here. A caller that set req.Variables
	// explicitly (direct API, tests) wins; we only fall back to the scene.
	// graph.SceneInput ignores the "variables" key, so this re-read is the one
	// place that sees it.
	//
	// Português: Variáveis de projeto (Caminho A — embutidas na cena). O codegen
	// é um passo cena→código sem estado: a rota /codegen tem só :language, sem
	// project_id, então a cena é a entrada COMPLETA — como já é para devices,
	// fios e black-boxes. A IDE embute as declarações como um array "variables"
	// no topo (carregado da tabela project_variables, que é project-scoped do
	// lado da IDE). Lemos aqui. Quem setou req.Variables explicitamente (API
	// direta, testes) vence; só caímos na cena como fallback.
	if len(req.Variables) == 0 {
		var sv struct {
			Variables []ir.VariableDecl `json:"variables"`
		}
		if json.Unmarshal(req.Scene, &sv) == nil {
			req.Variables = sv.Variables
		}
	}

	// Checkpoint 1: bail out if the caller cancelled while the task
	// sat queued. Catches the common case where many users submit and
	// then click Cancel — the worker observes the cancellation before
	// burning CPU on graph.Build.
	if cancelled(ctx, &resp) {
		return resp
	}

	// Step 2: Build graph
	//
	// buildDiags currently only reports geometric conflicts — devices
	// that overlap each other, straddle a container boundary, or pierce
	// a container's outer border. Any of these violations means the
	// intended control flow is ambiguous, so we refuse to emit code:
	// the user must fix the canvas first.
	//
	// Português: Erros do builder são violações geométricas e bloqueiam
	// a geração — o canvas precisa ser corrigido antes de gerar código.
	g, buildDiags := graph.Build(scene)
	if len(buildDiags) > 0 {
		resp.addDiagnostics(buildDiags)
		return resp
	}

	// Checkpoint 2: graph.Build can be O(n²) on dense scenes; check
	// cancellation before validating.
	if cancelled(ctx, &resp) {
		return resp
	}

	// Step 3: Validate (basic checks + black-box checks)
	valDiags := validate(g, req.BlackBoxDefs)
	if len(valDiags) > 0 {
		resp.addDiagnostics(valDiags)
		return resp
	}

	// Checkpoint 3: validate iterates every node and every black-box
	// method input. On scenes with many BlackBoxRun nodes the cost is
	// linear in nodes × inputs; checking ctx here keeps the budget tight
	// before ir.Emit, which is typically the most expensive stage.
	if cancelled(ctx, &resp) {
		return resp
	}

	// Step 4: Emit IR
	program, emitDiags := ir.Emit(g, req.BlackBoxDefs, req.Variables)
	resp.addDiagnostics(emitDiags)
	// Only errors block codegen; warnings (for example the type-compat
	// pass's "lossy conversion inserted" advisories) are additive and
	// we must keep going to emit the actual code.
	if hasErrorSeverity(emitDiags) {
		return resp
	}

	// Checkpoint 4: ir.Emit performs topological ordering and scope
	// crossing analysis; it is the heaviest stage in the pipeline.
	// Checking ctx here lets us skip the backend altogether on a
	// cancellation that arrived during IR emission.
	if cancelled(ctx, &resp) {
		return resp
	}

	// Step 4b: Attach black-box definitions to the program
	program.BlackBoxDefs = req.BlackBoxDefs

	resp.IR = program.String()
	// Pass through backend warnings as non-blocking diagnostics.
	for _, w := range program.Warnings {
		resp.addDiagnostic(Diagnostic{
			Kind:     diagnostics.KindEmitterInternal,
			Severity: diagnostics.SeverityWarning,
			Message:  w,
		})
	}

	// Step 5: Backend
	//
	// Each branch consumes the same ir.Program and writes its own
	// target output. Branches are mutually exclusive: at most one of
	// resp.Code and resp.Files is populated per call. The empty string
	// language defaults to Go for backward compatibility with the
	// earliest clients that did not send the field.
	//
	// Português: cada caso consome o mesmo IR e escreve para o alvo
	// dele. Code (Go) e Files (C) são mutuamente exclusivos por call.
	// Language vazio cai em Go por compat com clientes antigos.
	switch req.Language {
	case "go", "":
		resp.Code = golang.Emit(program)
	case "c":
		// Pick the C type profile and the string-buffer size for this
		// generation. Two paths:
		//
		//   1. PRESET path (the normal, board-first one, Arduino-style — pick a
		//      board, not an architecture). Taken when a TARGET is selected
		//      (Metadata.Target) AND, crucially, as the DEFAULT when nothing is
		//      selected: ResolveTarget maps an empty or unknown id to the
		//      conservative Arduino UNO target, so an unconfigured scene
		//      generates EXACTLY the Arduino UNO preset — its type profile AND
		//      its RAM-sized buffer (64 bytes) — not a looser fallback. This is
		//      what makes "open with Arduino UNO" consistent: the implicit
		//      default equals what an explicit "Arduino UNO" pick produces.
		//   2. ADVANCED / custom path. Taken only when NO target is selected but
		//      a profile is named directly (Metadata.TargetProfile) — picking
		//      type widths without a preset. The RAM-sized buffer is a preset
		//      feature, so this path leaves StringBufferSize unset and the C
		//      backend uses its own conservative default.
		//
		// Português: Escolhe o profile de tipos C e o tamanho do buffer. Dois
		// caminhos: (1) PRESET (board-first, estilo Arduino) — quando um TARGET é
		// escolhido E, importante, como DEFAULT quando nada é escolhido:
		// ResolveTarget mapeia id vazio/desconhecido para o target conservador
		// Arduino UNO, então uma cena não configurada gera EXATAMENTE o preset
		// Arduino UNO (profile + buffer de 64), não um fallback mais frouxo — é o
		// que torna "abrir com Arduino UNO" consistente; (2) AVANÇADO/custom — só
		// quando não há target mas há um profile nomeado direto (TargetProfile),
		// sem preset, buffer no default do backend C.
		var profile ansic.TargetProfile
		if scene.Metadata.Target == "" && scene.Metadata.TargetProfile != "" {
			profile = ansic.ResolveProfile(scene.Metadata.TargetProfile)
		} else {
			t := target.ResolveTarget(scene.Metadata.Target)
			profile = ansic.ResolveProfile(t.ProfileName)
			program.StringBufferSize = t.StringBufferSize
		}
		// Buffer override: a positive Metadata.StringBufferSize (set from a
		// board's advanced panel, already in bytes) replaces whatever the paths
		// above resolved — the board's default, or the C backend's own fallback
		// on the direct-profile path. Zero leaves the resolved size untouched, so
		// the common case (no override) is unaffected.
		//
		// Português: Override do buffer: um Metadata.StringBufferSize positivo
		// (do painel avançado de uma placa, já em bytes) substitui o que os
		// caminhos acima resolveram — o default da placa, ou o fallback do
		// backend C no caminho de profile direto. Zero não mexe.
		if scene.Metadata.StringBufferSize > 0 {
			program.StringBufferSize = scene.Metadata.StringBufferSize
		}
		resp.Files = ansic.Emit(program, profile)
	default:
		resp.addDiagnostic(Diagnostic{
			Kind:     diagnostics.KindUnsupportedLanguage,
			Severity: diagnostics.SeverityError,
			Message:  fmt.Sprintf("unsupported language: %q", req.Language),
		})
	}

	return resp
}

// cancelled reports whether ctx has been cancelled and, when it has,
// appends a Cancelled diagnostic to resp so the response carries a
// trace of why it is incomplete. Returns true when the caller should
// short-circuit the pipeline.
//
// The diagnostic is informational only — the worker that runs Generate
// observes ctx.Err() independently and publishes failure via Redis,
// so callers that look at resp alone still get to know "this is not
// a complete generation" from the diagnostic alone.
//
// Português:
//
//	Sinaliza cancelamento e marca o response com um diagnóstico
//	informativo. A falha em si é reportada via Redis pelo worker.
func cancelled(ctx context.Context, resp *Response) bool {
	if ctx.Err() == nil {
		return false
	}
	resp.addDiagnostic(Diagnostic{
		Kind:     diagnostics.KindCancelled,
		Severity: diagnostics.SeverityError,
		Message:  "codegen cancelled by caller",
	})
	return true
}

// =====================================================================
//  Validation
// =====================================================================

func validate(g *graph.Graph, bbDefs map[string]*blackbox.BlackBoxDef) []Diagnostic {
	var diags []Diagnostic

	// Helper: emit a missing-connection diagnostic for a single port.
	missingConn := func(deviceID, msg string) Diagnostic {
		return Diagnostic{
			Kind:     diagnostics.KindMissingConnection,
			Severity: diagnostics.SeverityError,
			Devices:  []string{deviceID},
			Message:  msg,
		}
	}

	// Check that loops have the correct control port connected.
	// StatementLoop requires a "stop" condition (bool → break).
	// StatementLoopDuration requires an "interval" port (time.Duration → sleep).
	// StatementIfElse requires a "condition" port (bool → branch).
	//
	// Português: Verifica que loops têm a porta de controle correta conectada.
	for scopeID, scope := range g.Scopes {
		if scopeID == "" {
			continue
		}
		loopNode, ok := g.Nodes[scopeID]
		if !ok {
			continue
		}
		switch loopNode.Type {
		case "StatementLoop":
			if scope.StopPort == nil {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindMissingStop,
					Severity: diagnostics.SeverityError,
					Devices:  []string{scopeID},
					Scope:    scopeID,
					Message:  fmt.Sprintf("%s: no stop condition connected", scopeID),
				})
			}
		case "StatementLoopDuration":
			if scope.IntervalPort == nil {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindMissingInterval,
					Severity: diagnostics.SeverityError,
					Devices:  []string{scopeID},
					Scope:    scopeID,
					Message:  fmt.Sprintf("%s: no interval (time.Duration) connected — connect a ConstDuration device", scopeID),
				})
			}
		case "StatementIfElse":
			if scope.ConditionPort == nil {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindMissingCondition,
					Severity: diagnostics.SeverityError,
					Devices:  []string{scopeID},
					Scope:    scopeID,
					Message:  fmt.Sprintf("%s: no condition (bool) connected — connect a Bool or comparison device", scopeID),
				})
			}
		case "StatementCase":
			// A boolean StatementCase lowers to an if/else scope (ConditionPort);
			// any other selector populates SelectorPort. Either way the selector
			// input must be connected.
			//
			// Português: StatementCase booleano vira escopo if/else (ConditionPort);
			// os demais populam SelectorPort. De qualquer forma o selector é obrigatório.
			if scope.ConditionPort == nil && scope.SelectorPort == nil {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindMissingCondition,
					Severity: diagnostics.SeverityError,
					Devices:  []string{scopeID},
					Scope:    scopeID,
					Message:  fmt.Sprintf("%s: no selector connected — connect a value to the case selector", scopeID),
				})
			}

			// Cross-case soundness: duplicate switch labels (error — the
			// generated switch would not compile), empty `between` ranges and
			// unreachable branches (warnings — the code still generates). This
			// is the SAME authority the inspect panel's preview calls, so a
			// generated scene and the panel can never disagree about a case
			// conflict. The selector type only needs to tell bool (exhaustive,
			// nothing to validate) apart from any integer selector; a bool
			// StatementCase lowers to if/else and carries no switch Cases.
			//
			// Português: Solidez entre cases — rótulos duplicados (error, não
			// compila), `between` vazio e ramos inalcançáveis (warning). Mesma
			// autoridade do preview do painel, então cena gerada e painel nunca
			// discordam. selectorType só distingue bool (sem nada a validar) de
			// um selector inteiro.
			selectorType := "int"
			if scope.ConditionPort != nil {
				selectorType = "bool"
			}
			diags = append(diags, ValidateCases(scopeID, selectorType, scope.Cases)...)
		}
	}

	// Check required inputs are connected
	for _, node := range g.Nodes {
		switch {
		case strings.HasPrefix(node.Type, "BlackBoxInit:"):
			structName := strings.TrimPrefix(node.Type, "BlackBoxInit:")
			if bbDefs == nil || bbDefs[structName] == nil {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindBlackBoxDefMissing,
					Severity: diagnostics.SeverityError,
					Devices:  []string{node.ID},
					Message:  fmt.Sprintf("%s: black-box definition %q not found", node.ID, structName),
				})
				continue
			}
			def := bbDefs[structName]
			if def.Init == nil {
				continue
			}
			for _, input := range def.Init.Inputs {
				if len(g.GetInputSources(node.ID, input.Name)) == 0 {
					diags = append(diags, missingConn(node.ID,
						fmt.Sprintf("%s.%s: not connected", node.ID, input.Name)))
				}
			}

		case strings.HasPrefix(node.Type, "BlackBox") && !strings.HasPrefix(node.Type, "BlackBoxInit:"):
			colonIdx := strings.Index(node.Type, ":")
			if colonIdx < 0 {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindBlackBoxDefMissing,
					Severity: diagnostics.SeverityError,
					Devices:  []string{node.ID},
					Message:  fmt.Sprintf("%s: malformed BlackBox node type %q (missing colon)", node.ID, node.Type),
				})
				continue
			}
			structName := node.Type[colonIdx+1:]
			methodName := strings.TrimPrefix(node.Type[:colonIdx], "BlackBox")

			// C99 function-device: empty struct part ("BlackBox<fn>:"). Its
			// def is keyed by function name (store.LoadBlackBoxDefsForScene),
			// not struct name, and there is no Init/instance to verify. Look
			// it up by function name and validate only the mandatory inputs
			// declared on the function's signature. See
			// docs/CODEGEN_C99_STAGE.md §5.
			if structName == "" {
				def := bbDefs[methodName]
				if bbDefs == nil || def == nil {
					diags = append(diags, Diagnostic{
						Kind:     diagnostics.KindBlackBoxDefMissing,
						Severity: diagnostics.SeverityError,
						Devices:  []string{node.ID},
						Message:  fmt.Sprintf("%s: function-device %q not found", node.ID, methodName),
					})
					continue
				}
				for i := range def.Functions {
					if def.Functions[i].Name != methodName {
						continue
					}
					for _, input := range def.Functions[i].Inputs {
						if input.Connection == "mandatory" &&
							len(g.GetInputSources(node.ID, input.Name)) == 0 {
							diags = append(diags, missingConn(node.ID,
								fmt.Sprintf("%s.%s: not connected", node.ID, input.Name)))
						}
					}
					break
				}
				continue
			}

			if bbDefs == nil || bbDefs[structName] == nil {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindBlackBoxDefMissing,
					Severity: diagnostics.SeverityError,
					Devices:  []string{node.ID},
					Message:  fmt.Sprintf("%s: black-box definition %q not found", node.ID, structName),
				})
				continue
			}
			def := bbDefs[structName]

			if def.HasInit() {
				nodeInstanceId := ""
				if id, ok := node.Properties["instanceId"]; ok {
					if s, ok := id.(string); ok {
						nodeInstanceId = s
					}
				}
				if nodeInstanceId == "" {
					nodeInstanceId = node.ID
				}

				initFound := false
				for _, otherNode := range g.Nodes {
					if !strings.HasPrefix(otherNode.Type, "BlackBoxInit:") {
						continue
					}
					otherInstanceId := ""
					if id, ok := otherNode.Properties["instanceId"]; ok {
						if s, ok := id.(string); ok {
							otherInstanceId = s
						}
					}
					if otherInstanceId == "" {
						otherInstanceId = otherNode.ID
					}
					if otherInstanceId == nodeInstanceId {
						initFound = true
						break
					}
				}
				if !initFound {
					diags = append(diags, Diagnostic{
						Kind:     diagnostics.KindBlackBoxDefMissing,
						Severity: diagnostics.SeverityError,
						Devices:  []string{node.ID},
						Message: fmt.Sprintf(
							"%s: component %q has an Init() method — place a %s Init block in the workspace and wire it before generating code",
							node.ID, structName, structName,
						),
					})
				}
			}

			method := def.GetMethod(methodName)
			if method != nil {
				for _, input := range method.Inputs {
					if len(g.GetInputSources(node.ID, input.Name)) == 0 {
						diags = append(diags, missingConn(node.ID,
							fmt.Sprintf("%s.%s: not connected", node.ID, input.Name)))
					}
				}
			}

		case node.Type == "StatementAdd" || node.Type == "StatementSub" ||
			node.Type == "StatementMul" || node.Type == "StatementDiv" ||
			node.Type == "StatementEqualTo" || node.Type == "StatementNotEqualTo" ||
			node.Type == "StatementLessThan" || node.Type == "StatementLessThanOrEqualTo" ||
			node.Type == "StatementGreaterThan" || node.Type == "StatementGreaterThanOrEqualTo":
			if len(g.GetInputSources(node.ID, "inputX")) == 0 {
				diags = append(diags, missingConn(node.ID,
					fmt.Sprintf("%s.inputX: not connected", node.ID)))
			}
			if len(g.GetInputSources(node.ID, "inputY")) == 0 {
				diags = append(diags, missingConn(node.ID,
					fmt.Sprintf("%s.inputY: not connected", node.ID)))
			}

		case node.Type == "StatementCompare":
			if len(g.GetInputSources(node.ID, "inputA")) == 0 {
				diags = append(diags, missingConn(node.ID,
					fmt.Sprintf("%s.inputA: not connected", node.ID)))
			}
			if len(g.GetInputSources(node.ID, "inputB")) == 0 {
				diags = append(diags, missingConn(node.ID,
					fmt.Sprintf("%s.inputB: not connected", node.ID)))
			}

		case node.Type == "StatementGauge":
			if len(g.GetInputSources(node.ID, "current")) == 0 {
				diags = append(diags, missingConn(node.ID,
					fmt.Sprintf("%s.current: not connected", node.ID)))
			}
		}
	}

	return diags
}
