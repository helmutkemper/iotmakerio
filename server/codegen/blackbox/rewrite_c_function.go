// server/codegen/blackbox/rewrite_c_function.go — Rewrite handlers for standalone function
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// devices (Slice C99-8, "one device per function").
//
// Paths (intercepted in RewriteC before the shared parsePath, like
// enum paths):
//
//   function.<name>                  → icon/label/comment of the
//                                       function device, written as a
//                                       leading-comment directive
//                                       block above the function.
//   function.<name>.<in|out>.<port>  → label/connection of a
//                                       parameter port. The synthetic
//                                       `return` output has no source
//                                       position, so it is NOT
//                                       editable through this path.
//
// All reuse the existing function/parameter locators and the shared
// comment-block renderer, so edits round-trip with the parser.

package blackbox

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// cFunctionPath is the parsed form of a function-device rewrite path.
type cFunctionPath struct {
	Func string
	Dir  string // "" for the device header; "in"/"out" for a port
	Port string
}

// parseCFunctionPath recognises the two function path shapes.
func parseCFunctionPath(s string) (cFunctionPath, bool) {
	parts := strings.Split(s, ".")
	switch {
	case len(parts) == 2 && parts[0] == "function" && parts[1] != "":
		return cFunctionPath{Func: parts[1]}, true
	case len(parts) == 4 && parts[0] == "function" &&
		(parts[2] == "in" || parts[2] == "out") &&
		parts[1] != "" && parts[3] != "":
		return cFunctionPath{Func: parts[1], Dir: parts[2], Port: parts[3]}, true
	}
	return cFunctionPath{}, false
}

// planCFunctionEdit dispatches a function path to the device-level or
// port-level planner.
func planCFunctionEdit(source string, fp cFunctionPath, e WizardEdit) (cSplicePlan, error) {
	if fp.Dir == "" {
		// Device header: icon/label/comment.
		if e.Op != OpSetStructDirectives && e.Op != OpSetMethodDirectives {
			return cSplicePlan{}, fmt.Errorf(
				"function device path supports setStructDirectives/setMethodDirectives, got %q", e.Op)
		}
		return planCFunctionDirectives(source, fp.Func, e)
	}
	// Port: connection/label.
	if e.Op != OpSetPortConnection {
		return cSplicePlan{}, fmt.Errorf(
			"function port path supports setPortConnection, got %q", e.Op)
	}
	if fp.Port == "return" {
		// The synthetic return port HAS a designed label mechanism — the
		// `return:<label>.` directive in the FUNCTION's leading comment
		// (parser: extractReturnLabelDirective) — but this path used to
		// reject with "not editable", so the natural gesture (clicking
		// the return row and typing a label) silently lost the value on
		// re-open (field report 2026-07-08). It now MERGES: the existing
		// leading block survives verbatim (label/icon/prose/callback —
		// this writer owns ONLY the return: segment), unlike the
		// function modal's whole-block rebuild, which prefills and
		// re-emits everything. Both writers converge on the same
		// directive. Connection is meaningless on outputs and the port
		// has no doc slot — both args are ignored, mirroring the Go
		// router's silent-drop stance.
		//
		// Português: O port sintético de retorno TEM mecanismo de label
		// (`return:<label>.` no comentário da função), mas este caminho
		// rejeitava — o gesto natural perdia o valor. Agora faz MERGE: o
		// bloco existente sobrevive verbatim; este writer é dono SÓ do
		// segmento return:. Connection/doc são ignorados.
		return planCReturnLabel(source, fp.Func, e)
	}
	return planCFunctionPortConnection(source, fp, e)
}

// planCFunctionDirectives writes icon/label/comment above a function
// device. Mirrors planCMethodDirectives but keyed by the bare
// function name (no struct receiver).
func planCFunctionDirectives(source, fnName string, e WizardEdit) (cSplicePlan, error) {
	var args struct {
		Label          string `json:"label"`
		Icon           string `json:"icon"`
		ExecutionOrder *int   `json:"executionOrder"`
		Comment        string `json:"comment"`
		ReturnLabel    string `json:"returnLabel"`
		Callback       string `json:"callback"`
		CallbackMode   string `json:"callbackMode"`
		MinTarget      string `json:"minTarget"`
		NoDevice       bool   `json:"noDevice"`
	}
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
	}

	declStart, ok := locateCFunction(source, fnName)
	if !ok {
		return cSplicePlan{}, fmt.Errorf("function %q not found", fnName)
	}

	commentStart, commentEnd := findLeadingCommentRange(source, declStart)
	indent := indentOfLine(source, declStart)

	var directives []string
	if args.Label != "" {
		directives = append(directives, "label:"+args.Label+".")
	}
	if args.Icon != "" {
		directives = append(directives, "icon:"+args.Icon+".")
	}
	// callback marks the function as a CALLBACK HANDLER of the named
	// function-pointer type — the wizard's dropdown writes it, the human never
	// types it. The function stays a normal callable (it keeps its parameters);
	// the parser records HandlerType, and the IDE offers a SEPARATE callback
	// reference device (CallbackRef:<fn>). The optional `:<mode>` third segment
	// decides which device variants the IDE offers: "both" (default) → callable
	// + reference; "ref" → reference only. The wizard's "Also generate the
	// normal function block" checkbox drives it (checked → both, unchecked →
	// ref). A bare `callback:<type>.` already means both, so we only append
	// `:ref` explicitly and keep the source clean. Omitting callback (the
	// dropdown's "— Not a callback handler —") clears it. See
	// docs/CODEGEN_C99_CALLBACKS.md.
	// Português: `callback:T[:mode].` marca a função como handler do tipo T —
	// escrito pelo dropdown, nunca digitado. A função continua chamável; o modo
	// (both/ref) decide as variantes de device. Omitir limpa a diretiva.
	if args.Callback != "" {
		directive := "callback:" + args.Callback
		if strings.EqualFold(args.CallbackMode, "ref") {
			directive += ":ref"
		}
		directives = append(directives, directive+".")
	}
	// executionOrder mirrors the Go method planner (rewrite.go,
	// OpSetMethodDirectives): the whole directive block is rebuilt from args,
	// so a nil pointer (field cleared in the wizard) simply omits the
	// directive — i.e. clears it — while a non-nil value writes
	// `executionOrder:N.`, the relative run order the codegen honours after
	// wires. Kept identical to Go so the C99 and Go wizards behave the same.
	if args.ExecutionOrder != nil {
		directives = append(directives, "executionOrder:"+strconv.Itoa(*args.ExecutionOrder)+".")
	}
	// min-target declares the smallest hardware class the function's code
	// runs on (avr | mcu32 | posix — see target_class.go). Chosen by the
	// wizard's dropdown; omitting it (the "— any board —" option) clears
	// the directive, since the whole block is rebuilt from args. The
	// planner writes whatever it is told — an invalid value survives to
	// the export validator, which names it with the valid classes.
	// Português: min-target declara a menor classe de hardware onde o
	// código roda. Escolhido no dropdown do wizard; omitir (opção "— any
	// board —") limpa a diretiva, pois o bloco é reconstruído dos args. O
	// planner escreve o que recebe — valor inválido sobrevive até o
	// validador de export, que o nomeia com as classes válidas.
	if args.MinTarget != "" {
		directives = append(directives, "min-target:"+args.MinTarget+".")
	}
	// device:false — the wizard checkbox's opt-out for a public helper;
	// omitted (unchecked) simply clears it, block rebuilt from args.
	// Português: O opt-out do checkbox; desmarcado limpa (bloco
	// reconstruído dos args).
	if args.NoDevice {
		directives = append(directives, "device:false.")
	}
	// C99 return-value label. The synthetic `return` output has no
	// source position, so its human label rides in the function's
	// leading comment as a `return:<label>.` directive — written by
	// the SAME planner that writes the device's label/icon, so the
	// two never overwrite each other.
	if args.ReturnLabel != "" {
		directives = append(directives, "return:"+args.ReturnLabel+".")
	}

	newBlock := renderCommentBlock(args.Comment, directives, indent)
	return cSplicePlan{start: commentStart, end: commentEnd, newText: newBlock}, nil
}

// planCFunctionPortConnection rewrites a parameter port's directives.
//
// C parameters are often declared inline (`f(int a, char *b)`), where
// there is no per-parameter line to anchor a comment on — writing a
// comment "above the parameter" would land above the whole function
// and be misread as the device's directives (the inline-signature
// bug). To match Go (the standard), we EXPAND the whole signature to
// multi-line, one parameter per line, with each parameter's
// directives in a comment block above it:
//
//	void display_write(
//	    // doc:the text colour.
//	    // label:Pen colour.
//	    // connection:mandatory.
//	    display_color_t color,
//	    // doc:the message.
//	    const char *text)
//
// Every parameter is re-parsed and re-emitted in the canonical
// directive form (`doc:`/`label:`/`connection:`, each terminated by
// `.`), so existing directives on OTHER parameters are preserved and
// the directive/prose split is unambiguous to the parser (the prose
// always ends in `.`, so it never swallows the next directive — the
// label-leak bug).
func planCFunctionPortConnection(source string, fp cFunctionPath, e WizardEdit) (cSplicePlan, error) {
	var args struct {
		Connection string `json:"connection"`
		Label      string `json:"label"`
		Comment    string `json:"comment"`
		// Direction is "out" when the specialist ticked the output
		// checkbox, "in" (or empty) otherwise. Only meaningful on a
		// mutable pointer; the SPA only offers the checkbox there.
		Direction string `json:"direction"`
		// Slice / Lang / Dict are the COMPLETE port modal's knobs
		// (wizard UI plan, 2026-07-13): the collection pairing
		// (`slice:<len>.`), the maker-editor language (`lang:`) and the
		// completion dictionary (`dict:`). TRI-STATE by pointer — the
		// contract every modal field follows from now on:
		//   absent (nil)  → PRESERVE what the source already has
		//                   (old clients keep working untouched);
		//   ""            → CLEAR the directive;
		//   value         → WRITE it.
		// Português: Os botões do modal COMPLETO da porta. TRI-STATE por
		// ponteiro: ausente = PRESERVA o que o fonte já tem (clientes
		// antigos seguem intactos); "" = LIMPA; valor = GRAVA.
		Slice *string `json:"slice"`
		Lang  *string `json:"lang"`
		Dict  *string `json:"dict"`
	}
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
	}

	stripped, blockComments := preprocessC(source)
	rawFuncs := findAllCFunctions(source, stripped, blockComments)
	var fn *rawCFunc
	for i := range rawFuncs {
		if rawFuncs[i].RawName == fp.Func {
			fn = &rawFuncs[i]
			break
		}
	}
	if fn == nil {
		return cSplicePlan{}, fmt.Errorf("function %q not found", fp.Func)
	}

	paramsStart := findParamsStart(source, fn.DeclStart)
	if paramsStart < 0 {
		return cSplicePlan{}, fmt.Errorf("could not locate parameter list of %q", fp.Func)
	}
	closeParen := findMatchingCloseParen(source, paramsStart-1)
	if closeParen < 0 {
		return cSplicePlan{}, fmt.Errorf("unbalanced parameter list of %q", fp.Func)
	}

	tokens := splitParams(source[paramsStart:closeParen])
	if len(tokens) == 0 {
		return cSplicePlan{}, fmt.Errorf("function %q has no parameters", fp.Func)
	}

	// Locate the target parameter by NAME. Parameter names are unique
	// within a signature, and the direction may be CHANGING in this very
	// edit (the output checkbox), so we must not match on direction.
	targetIdx := -1
	for i, tok := range tokens {
		name, _ := splitCFieldNameType(tok.text)
		if name == fp.Port {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return cSplicePlan{}, fmt.Errorf("port %s.%s not found", fp.Func, fp.Port)
	}

	baseIndent := indentOfLine(source, fn.DeclStart)
	paramIndent := baseIndent + "    "

	var b strings.Builder
	for i, tok := range tokens {
		pp := portFromParamToken(tok)
		_, typ := splitCFieldNameType(tok.text)

		// Directive values. For the target parameter they come from the
		// edit. For every OTHER parameter we re-emit only what was
		// already there: if it had no leading comment, we leave it bare
		// (so editing one param doesn't sprinkle default labels onto its
		// untouched siblings).
		var doc, label, connection string
		// nil = preserve from the parsed port; set = the modal's word is
		// final (possibly ""=clear). Português: nil = preserva da porta
		// parseada; setado = a palavra do modal é final ("" = limpa).
		var sliceOverride, langOverride, dictOverride *string
		isOutput := false
		emit := true
		if i == targetIdx {
			doc = args.Comment
			label = args.Label
			// Direction is only how the pin is shown. A parameter is
			// still a parameter, so its connection (mandatory/optional)
			// is independent of whether it's drawn as an input or an
			// output — we keep it either way.
			isOutput = args.Direction == "out" && canBeOutput(typ)
			connection = args.Connection
			// Complete-modal knobs: apply the tri-state NOW so the
			// preservation blocks below see the final intent. Português:
			// Aplica o tri-state AGORA; os blocos de preservação abaixo
			// enxergam a intenção final.
			if args.Slice != nil {
				sliceOverride = args.Slice
			}
			if args.Lang != nil {
				langOverride = args.Lang
			}
			if args.Dict != nil {
				dictOverride = args.Dict
			}
		} else if strings.TrimSpace(tok.leadingDoc) == "" {
			emit = false
		} else if pp != nil {
			doc = pp.portDef.Doc
			label = pp.portDef.Label
			isOutput = pp.isOutput
			if !pp.portDef.MissingConn {
				connection = pp.portDef.Connection
			}
		}

		var dirs []string
		if emit {
			if doc != "" {
				dirs = append(dirs, "doc:"+doc+".")
			}
			if label != "" {
				dirs = append(dirs, "label:"+label+".")
			}
			// direction and connection are orthogonal: a parameter may
			// be an output AND still carry a mandatory/optional choice.
			if isOutput {
				dirs = append(dirs, "direction:out.")
			}
			if connection != "" {
				dirs = append(dirs, "connection:"+connection+".")
			}
			// Preserve the structural `slice:<len>.` directive. The wizard's
			// port editor only manages doc/label/direction/connection, so it
			// is not regenerated from the edit above — and a port rewrite that
			// dropped it would silently un-collapse the (pointer, length)
			// collection: the port reverts from []T back to the raw pointer
			// (e.g. []string → const char **) and the length parameter
			// reappears as its own unconfigured pin. The length-companion name
			// comes from the parsed PortDef, populated from the ORIGINAL
			// comment, so the directive survives an edit to THIS port or to any
			// sibling parameter. (The parser strips `// ` before reading, so
			// re-emitting it on its own line round-trips cleanly.)
			sliceVal := ""
			if pp != nil {
				sliceVal = pp.portDef.SliceLenName
			}
			if sliceOverride != nil {
				sliceVal = *sliceOverride
			}
			if sliceVal != "" {
				dirs = append(dirs, "slice:"+sliceVal+".")
			}
			// Preserve the Phase B editor config (`lang:` + `dict:`) the
			// same way: the wizard's port editor has NO fields for them,
			// so they are never regenerated from the edit — and a rewrite
			// that dropped them would silently mute the maker's Monaco
			// (2026-07-13 field report: one modal round-trip stripped
			// both and the demo dictionary vanished without a trace).
			// They ride the parsed PortDef and survive an edit to THIS
			// port or to any sibling.
			// Português: Preserva a config de editor da Fase B do mesmo
			// jeito: o editor de portas do wizard NÃO tem campos para
			// elas, então nunca são regeneradas do edit — e um rewrite
			// que as derrubasse emudeceria o Monaco do maker em silêncio
			// (report de campo de 2026-07-13: uma ida ao modal apagou as
			// duas e o dicionário sumiu sem rastro).
			langVal, dictVal := "", ""
			if pp != nil {
				langVal, dictVal = pp.portDef.EditorLang, pp.portDef.EditorDict
			}
			if langOverride != nil {
				langVal = *langOverride
			}
			if dictOverride != nil {
				dictVal = *dictOverride
			}
			if langVal != "" {
				dirs = append(dirs, "lang:"+langVal+".")
			}
			if dictVal != "" {
				dirs = append(dirs, "dict:"+dictVal+".")
			}
		}

		b.WriteString("\n")
		for _, d := range dirs {
			b.WriteString(paramIndent)
			b.WriteString("// ")
			b.WriteString(d)
			b.WriteByte('\n')
		}
		b.WriteString(paramIndent)
		b.WriteString(strings.TrimSpace(tok.text))
		if i < len(tokens)-1 {
			b.WriteByte(',')
		}
	}
	b.WriteByte('\n')
	b.WriteString(baseIndent)

	return cSplicePlan{start: paramsStart, end: closeParen, newText: b.String()}, nil
}

// findMatchingCloseParen returns the byte offset of the `)` that
// matches the `(` at openParen, accounting for nested parens (e.g.
// function-pointer parameter types). Returns -1 if unbalanced.
func findMatchingCloseParen(source string, openParen int) int {
	if openParen < 0 || openParen >= len(source) || source[openParen] != '(' {
		return -1
	}
	depth := 0
	for i := openParen; i < len(source); i++ {
		switch source[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// locateCFunction returns the byte offset where function `name`
// begins. Standalone functions have no receiver, so any function of
// that exact name matches (first occurrence — declaration or
// definition, consistent with locateCMethod).
func locateCFunction(source, name string) (int, bool) {
	stripped, blockComments := preprocessC(source)
	for _, fn := range findAllCFunctions(source, stripped, blockComments) {
		if fn.RawName == name {
			return fn.DeclStart, true
		}
	}
	return 0, false
}

// planCReturnLabel writes (or removes) the `return:<label>.` directive
// in a function's leading comment, PRESERVING every other line of the
// block verbatim — see the routing note above for why this writer merges
// where the function modal rebuilds. An empty label removes the
// directive (the return row shows its default again).
//
// Português: Escreve (ou remove) o `return:<label>.` no comentário
// líder, preservando todo o resto verbatim. Label vazio remove.
func planCReturnLabel(source, fnName string, e WizardEdit) (cSplicePlan, error) {
	var args struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
	}

	declStart, ok := locateCFunction(source, fnName)
	if !ok {
		return cSplicePlan{}, fmt.Errorf("function %q not found", fnName)
	}
	commentStart, commentEnd := findLeadingCommentRange(source, declStart)
	indent := indentOfLine(source, declStart)

	existing := source[commentStart:commentEnd]
	var kept []string
	for _, line := range strings.Split(existing, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Drop ONLY a `// return:…` directive line; everything else —
		// label/icon lines, prose, /* */ blocks — passes through
		// verbatim, indentation included.
		if body, isLine := strings.CutPrefix(trimmed, "//"); isLine {
			if strings.HasPrefix(strings.TrimSpace(body), "return:") {
				continue
			}
		}
		kept = append(kept, line)
	}
	if strings.TrimSpace(args.Label) != "" {
		kept = append(kept, indent+"// return:"+strings.TrimSpace(args.Label)+".")
	}

	newBlock := ""
	if len(kept) > 0 {
		newBlock = strings.Join(kept, "\n") + "\n"
	}
	return cSplicePlan{start: commentStart, end: commentEnd, newText: newBlock}, nil
}
