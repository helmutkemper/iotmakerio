// server/codegen/blackbox/parser_c_func.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// parser_c_func.go — Function discovery, method classification, port parsing.
//
// English:
//
//	The third leg of the C99 parser. parser_c.go finds structs and
//	props; this file finds top-level functions, classifies them as
//	methods of one of the discovered structs (or as "extras"), and
//	parses each function's parameter list into input/output ports.
//
//	What counts as a "function" for Slice 2:
//
//	  Forward declaration:  <return-type> <name>(<params>);
//	  Definition:           <return-type> <name>(<params>) { <body> }
//
//	Both are recognised. The body of a definition is NEVER read —
//	this parser is signature-only, exactly like the Go parser is.
//	`static` and other storage-class specifiers are accepted and
//	preserved in the return-type string.
//
//	A function is classified as a method of struct S when:
//
//	  1. The function name starts with "<S>_" exactly.
//	  2. The first parameter is `struct <S>* s` (or `<S> *s` for
//	     typedef-aliased structs whose alias matches S — Slice 2's
//	     tag-vs-alias resolution).
//	  3. The remaining function name (after "<S>_") is the method
//	     name. The wizard treats "Init" specially; all others land
//	     in StructDef.Methods.
//
//	When ANY of those rules fails, the function is collected as an
//	Extra (BlackBoxDef.Extras). The wizard surfaces these so the
//	specialist can decide what to do (rename them into methods,
//	leave them as helpers, etc.) — per §2.12 the parser never
//	rejects or silently drops.
//
//	Parameter classification (the receiver is excluded from ports):
//
//	  Non-pointer parameter (int, const char*, ...) → INPUT port
//	  Pointer-to-non-const parameter (T*)            → OUTPUT port
//	  Pointer-to-const parameter (const T*)          → INPUT port
//
//	An `int` return type implicitly appends an output port named
//	`err` of type `error`, mimicking Go's `(err error)` convention.
//	A `void` return adds no implicit port.
//
//	Port directives in C99 live in `//` comments immediately above
//	the parameter (Slice 2 accepts only `//`; block comments are
//	Slice 6). The IDS vocabulary is shared with Go: `// doc:`,
//	`// label:`, `// connection:mandatory|optional`, `// range:`,
//	`// unit:`, etc. extractDocDirectives + applyPortMeta handle the
//	parsing; we just need to slice the byte ranges correctly.
//
// Português:
//
//	Descoberta de funções + classificação como método + ports.
//	Função vira método de S quando: nome começa com "S_", primeiro
//	param é `struct S* s`, restante do nome é o nome do método.
//	Senão vira "extra". Ponteiro não-const após receiver → output;
//	demais → input. Return `int` adiciona port `err` implícito.

import (
	"strings"
)

// rawCFunc is the function-discovery intermediate. Phase 7 of
// ParseC converts each into a FuncDef.
type rawCFunc struct {
	// RawName is the function identifier exactly as it appears in
	// the source (e.g. "wifi_conn_start").
	RawName string

	// ReturnType is the raw return-type string, with storage-class
	// specifiers preserved (`static int`, `bool`, `void`, etc.).
	ReturnType string

	// ParamsRaw is the raw comma-separated parameter list as
	// written between `(` and `)`. May be empty for `void` or `()`
	// signatures.
	ParamsRaw string

	// LeadingDoc is the original-source text of the contiguous
	// comment block immediately above the function declaration.
	// IDS directives have NOT been extracted yet.
	LeadingDoc string

	// DeclStart is the byte offset of the first character of the
	// declaration (which may be a storage-class specifier like
	// `static`, not the return type proper).
	DeclStart int

	// HasBody reports whether this entry came from a function
	// DEFINITION (`<R> Name(...) { ... }`) or a forward
	// DECLARATION (`<R> Name(...);`). The dedupe pass uses this
	// to prefer the definition when both forms exist in the
	// input — the definition is closer to the running code and
	// typically carries the more informative leading comment.
	HasBody bool
}

// findAllCFunctions walks the source and returns every top-level
// function declaration or definition. Order matches source order
// so downstream classification preserves method-listing order.
//
// Slice C99-5 filtering rules (per the design doc §7 Slice 5):
//
//   - Functions qualified with `static` at file scope are IGNORED
//     entirely. In C99 `static` means "private to this translation
//     unit" — the function is not part of the public API, so it
//     does not represent a device or method that the wizard should
//     expose. There is no override: C99 is the source of truth.
//
//   - Forward declarations (header file) and definitions (.c file)
//     of the SAME function are deduplicated: the parser keeps a
//     single rawCFunc per (name, parameter-types) pair, preferring
//     the one with a body (the definition) so the leading-comment
//     block is the most informative. Without this, a typical
//     paste of `module.h + module.c` was double-counting every
//     public function.
//
//   - `static inline` is treated the same as `static` (still
//     ignored). The inline qualifier doesn't change the linkage.
//
//   - The bare `inline` qualifier (no `static`) is NOT a static
//     specifier and the function still appears. C99 inline-without-
//     static is rare in modern code but valid.
//
// The implementation is a state-machine over `stripped`:
//
//  1. Scan for `(` outside of any brace.
//  2. Walk backwards from `(` over identifier chars to recover the
//     function name.
//  3. Walk further back over whitespace + identifier chars to
//     recover the return-type tokens. Stop at `;`, `}`, or start
//     of file — those terminate the previous declaration.
//  4. matchParen to find the `)` and capture the parameter list.
//  5. After the `)`, the next non-whitespace byte is either `;`
//     (forward declaration), `{` (definition body — we skip it),
//     or something else (which means we mis-identified — skip).
//  6. Collect leading comments via collectLeadingComments on the
//     ORIGINAL source.
//  7. Check the return-type string for the `static` qualifier;
//     skip the entry when present.
//  8. Build a signature key (name + parameter-type list) and look
//     it up in a dedupe map. On collision, keep the entry whose
//     return-type span carries no body indicator (i.e. the
//     definition, which has a leading-comment block closer to the
//     code that actually runs).
//
// Tricky cases handled:
//
//   - Function bodies (`{ ... }`) are skipped via matchBrace so
//     local declarations inside a body don't pollute the top-level
//     list. We do NOT walk into bodies for any reason.
//   - Function-pointer-typed PARAMETERS contain extra `()` but the
//     matchParen call from step 4 handles nested parens correctly.
//   - Function-pointer-typed return values (`int (*f(void))(int)`)
//     are extremely rare in embedded code and Slice 2 silently
//     skips them — they would mis-classify the function name.
//   - Calls inside top-level initialisers (`int x = foo();`)
//     never trigger step 2 because we only fire on `(` and walk
//     back; the `=` before `foo` causes step 3 to bail.
func findAllCFunctions(src, stripped string, blockComments map[int]string) []rawCFunc {
	// Phase A — collect every signature we can find, including
	// duplicates. Phase B dedupes.
	var collected []rawCFunc

	i := 0
	for i < len(stripped) {
		c := stripped[i]

		// Skip line comments entirely. preprocessC masks block
		// comments but leaves `//` line comments verbatim (so IDS
		// directives can be read from them via the source). Their
		// PROSE, however, may contain text that looks like a
		// declaration — e.g. a doc line "Created by sht3x_create();"
		// — which must NOT be discovered as a function. The leading
		// directives above a real function live on their own `//`
		// lines and are picked up separately; skipping them here only
		// drops comment text, never a real signature (which is never
		// inside a `//`).
		if c == '/' && i+1 < len(stripped) && stripped[i+1] == '/' {
			nl := strings.IndexByte(stripped[i:], '\n')
			if nl < 0 {
				break
			}
			i += nl + 1
			continue
		}

		// Skip whole brace blocks (function bodies, struct bodies,
		// enum bodies, anything brace-delimited). We do not parse
		// anything inside them.
		if c == '{' {
			end, ok := matchBrace(stripped, i)
			if !ok {
				return dedupeCFunctions(collected)
			}
			i = end + 1
			continue
		}

		// We use `(` as the anchor for function discovery.
		if c != '(' {
			i++
			continue
		}

		// Walk backwards to find the function name (identifier
		// immediately preceding `(`). Skip whitespace first.
		nameEnd := i - 1
		for nameEnd >= 0 && isSpace(stripped[nameEnd]) {
			nameEnd--
		}
		if nameEnd < 0 || !isIdentByte(stripped[nameEnd]) {
			i++
			continue
		}
		nameStart := nameEnd
		for nameStart > 0 && isIdentByte(stripped[nameStart-1]) {
			nameStart--
		}
		funcName := stripped[nameStart : nameEnd+1]

		// Reject keywords. `if`, `while`, `for`, `switch`, `return`,
		// `sizeof` — none of these are function declarations.
		if isCKeyword(funcName) {
			i++
			continue
		}

		// Walk further back to determine the start of the
		// declaration. We stop at the first `;`, `}`, or start of
		// file. The span between (stopBoundary+1) and (nameStart-1)
		// is the return-type tokens.
		stopBoundary := nameStart - 1
		for stopBoundary >= 0 {
			b := stripped[stopBoundary]
			// `;`, `}`, `{` mark a declaration boundary — but only when
			// they are real code. The same characters appearing inside a
			// `//` doc-comment (e.g. "Append text; wrap to width." or
			// "Use {color} per line.") must NOT be treated as a boundary,
			// or the return-type span swallows the rest of the comment
			// (and the function's `void` return is mis-read as a typed
			// one, conjuring a phantom `return` port). preprocessC masks
			// block comments but leaves line comments in `stripped`, so we
			// guard against them explicitly here.
			if (b == ';' || b == '}' || b == '{') && !isInsideLineComment(stripped, stopBoundary) {
				break
			}
			stopBoundary--
		}
		declStart := stopBoundary + 1
		// Trim leading whitespace and line-comments. Line comments
		// don't end at `;` or `}` so the previous loop walked past
		// them; we now skip forward over any whitespace OR `//…\n`
		// runs so declStart lands on the first real token of the
		// declaration (the storage class or return type). Without
		// this skip, declStart points at the start of the file (or
		// at the line of a previous comment block), and the leading-
		// comment collector misattributes those comments to the
		// function. Block comments (`/* … */`) were already
		// stripped by preprocessC so the only marker we look for
		// here is `//`.
		for declStart < nameStart {
			if isSpace(stripped[declStart]) {
				declStart++
				continue
			}
			// Line comment: skip to end-of-line.
			if declStart+1 < nameStart && stripped[declStart] == '/' && stripped[declStart+1] == '/' {
				eol := strings.Index(stripped[declStart:], "\n")
				if eol < 0 {
					declStart = nameStart
					break
				}
				declStart += eol + 1
				continue
			}
			// Preprocessor directive (#include, #define, ...): skip to
			// end-of-line. A return type never contains `#`, but a system
			// header sits between the file's leading comment and the first
			// function; without skipping it the entire `#include ...` line
			// (plus any comment after it) is swept into the return-type span,
			// which is then not exactly "void" and spawns a phantom "return"
			// output port. Mirrors the line-comment skip above.
			//
			// Português: Diretiva de preprocessador (#include, #define...) —
			// pula até o fim da linha. Um tipo de retorno nunca contém `#`, mas
			// um header do sistema fica entre o comentário do topo e a primeira
			// função; sem pular, a linha `#include ...` inteira é engolida no
			// tipo de retorno, que deixa de ser exatamente "void" e cria uma
			// saída "return" fantasma.
			if stripped[declStart] == '#' {
				eol := strings.Index(stripped[declStart:], "\n")
				if eol < 0 {
					declStart = nameStart
					break
				}
				declStart += eol + 1
				continue
			}
			break
		}
		// If the return-type span is empty, this is not a
		// function declaration (no type before the name).
		if declStart >= nameStart {
			i++
			continue
		}
		returnType := strings.TrimSpace(stripped[declStart:nameStart])

		// Sanity: the return-type span must contain at least one
		// identifier character. If it's all punctuation, skip.
		if !containsIdent(returnType) {
			i++
			continue
		}

		// Reject return-type spans that contain `=`, which would
		// mean we're looking at `int x = foo(...)` not a declaration.
		if strings.Contains(returnType, "=") {
			i++
			continue
		}

		// Slice 5: skip `static` functions entirely. They are
		// private-to-translation-unit per C99 §6.2.2/3 and never
		// represent a wizard-visible device or method.
		if isStaticReturnType(returnType) {
			i++
			continue
		}

		// matchParen to find the closing `)`.
		parenEnd, ok := matchParen(stripped, i)
		if !ok {
			break
		}
		paramsRaw := src[i+1 : parenEnd]

		// After the `)`, expect `;` or `{`. Anything else means
		// we mis-identified; skip the `(` and continue.
		after := skipSpaces(stripped, parenEnd+1)
		if after >= len(stripped) {
			break
		}
		switch stripped[after] {
		case ';':
			// Forward declaration.
		case '{':
			// Definition — we'll skip its body via the outer loop
			// re-entering the `{` branch on next iteration.
		default:
			i = parenEnd + 1
			continue
		}

		// Collect leading comments from the original source.
		leadingDoc := collectLeadingComments(src, declStart, blockComments)

		collected = append(collected, rawCFunc{
			RawName:    funcName,
			ReturnType: returnType,
			ParamsRaw:  paramsRaw,
			LeadingDoc: leadingDoc,
			DeclStart:  declStart,
			HasBody:    stripped[after] == '{',
		})

		// Continue past this declaration. We let the next iteration
		// see the `;` or `{`; the outer loop's brace-skip will
		// handle a definition body.
		i = parenEnd + 1
	}

	return dedupeCFunctions(collected)
}

// isInsideLineComment reports whether the byte at `pos` sits inside a
// `//` line comment — i.e. a `//` marker appears earlier on the same
// line. Used by the declaration-boundary scan so that `;`/`{`/`}`
// characters written in prose inside a doc-comment are not mistaken
// for code boundaries. (preprocessC masks block comments but keeps
// line comments verbatim in `stripped`, so this check is needed.)
func isInsideLineComment(s string, pos int) bool {
	lineStart := pos
	for lineStart > 0 && s[lineStart-1] != '\n' {
		lineStart--
	}
	return strings.Contains(s[lineStart:pos], "//")
}

// isStaticReturnType reports whether the return-type token sequence
// begins with a `static` qualifier (including the `static inline`
// combination). Used by findAllCFunctions to skip
// translation-unit-private functions before they become rawCFunc
// entries.
//
// Token-based matching protects against false positives in identifiers
// like `static_assert` (a keyword in C11, but conservatively also
// avoided here in case of macro expansion games).
func isStaticReturnType(returnType string) bool {
	for _, tok := range strings.Fields(returnType) {
		if tok == "static" {
			return true
		}
	}
	return false
}

// dedupeCFunctions removes duplicate (name, signature) pairs from a
// raw function list. When a function is declared in a header and
// defined in a .c file, both forms reach the parser; we want a
// single entry in the output.
//
// The rule is: if two entries share both name and parameter-type
// signature, keep the one with HasBody=true (the definition).
// Because we walk the source in order, the definition usually
// appears AFTER the declaration; the dedupe loop scans linearly
// and replaces the earlier entry when a body-carrying duplicate
// shows up later.
//
// Signature comparison uses the type tokens of each parameter,
// ignoring parameter names: `int x` and `int y` have identical
// signatures. This matches C99 semantics — the linker resolves
// declarations to definitions by name + parameter types only.
//
// When two declarations (both without body) collide, the FIRST is
// kept; subsequent duplicates are dropped. This matters for header
// files that include other header files transitively — the same
// declaration may appear multiple times in the concatenated input.
func dedupeCFunctions(fns []rawCFunc) []rawCFunc {
	// keyOf builds the lookup key: `name(param-types)`.
	keyOf := func(fn rawCFunc) string {
		paramTokens := splitParams(fn.ParamsRaw)
		types := make([]string, 0, len(paramTokens))
		for _, pt := range paramTokens {
			_, typ := splitCFieldNameType(pt.text)
			types = append(types, strings.TrimSpace(typ))
		}
		return fn.RawName + "(" + strings.Join(types, ",") + ")"
	}

	seenAt := make(map[string]int, len(fns))
	out := make([]rawCFunc, 0, len(fns))
	for _, fn := range fns {
		key := keyOf(fn)
		idx, exists := seenAt[key]
		if !exists {
			seenAt[key] = len(out)
			out = append(out, fn)
			continue
		}
		// Duplicate: prefer the entry with a body. Replace the
		// existing entry when the new one is the definition.
		if fn.HasBody && !out[idx].HasBody {
			// The definition wins (it's closest to the running code),
			// but C idiom documents a function on its .h PROTOTYPE —
			// so the definition usually has no leading comment while
			// the prototype carries the doc block. Preserve the
			// prototype's leading doc when the definition lacks one,
			// otherwise the device's description is lost on dedupe.
			if strings.TrimSpace(fn.LeadingDoc) == "" &&
				strings.TrimSpace(out[idx].LeadingDoc) != "" {
				fn.LeadingDoc = out[idx].LeadingDoc
			}
			out[idx] = fn
		}
	}
	return out
}

// isCKeyword reports whether s is a C99 reserved word that could
// look like a function call. Used to avoid mis-identifying
// `if (...)` as a function named `if`.
func isCKeyword(s string) bool {
	switch s {
	case "if", "else", "while", "for", "switch", "case", "default",
		"return", "break", "continue", "do", "goto",
		"sizeof", "typeof", "_Alignof", "alignof",
		"static_assert", "_Static_assert",
		"struct", "union", "enum", "typedef",
		"const", "volatile", "restrict", "register",
		"static", "extern", "auto", "inline",
		"signed", "unsigned":
		return true
	}
	return false
}

// containsIdent reports whether s contains at least one identifier
// byte. Used to reject empty/punctuation-only return-type spans.
func containsIdent(s string) bool {
	for i := 0; i < len(s); i++ {
		if isIdentByte(s[i]) {
			return true
		}
	}
	return false
}

// ─── FuncDef construction ──────────────────────────────────────────────────────

// funcDefFromRaw builds a FuncDef from a rawCFunc. In the C99 model
// every public function is a standalone device-function — there is no
// receiver to skip; every parameter is a real port.
func funcDefFromRaw(fn *rawCFunc, limits ParserLimits) *FuncDef {
	fd := &FuncDef{}

	// Preserve the authored C signature verbatim. The multi-file C output
	// needs it to compose the black-box's generated header prototype
	// (`<CReturnType> P<id>_<name>(<CParams>);` — see csurface.go): the port
	// lists alone cannot rebuild it, because they deliberately transform the
	// signature (slice pairs collapse into one "[]T" port, out-params split
	// into Outputs, pass-throughs are synthesized). The prototype must match
	// the SOURCE — the definition shipped in bb_<id>.c is the authored one —
	// so verbatim text is the only faithful carrier. Same stance as
	// CallbackTypeDef, which already carries ReturnType/Params verbatim.
	//
	// Português: Preserva a assinatura C autoral verbatim. O header gerado da
	// saída multiarquivo precisa dela para compor o protótipo — as portas não
	// reconstroem a assinatura (slice colapsa, out-params viram Outputs). O
	// protótipo tem que casar com o FONTE embarcado em bb_<id>.c, então texto
	// verbatim é o único portador fiel. Mesma postura do CallbackTypeDef.
	fd.CReturnType = strings.TrimSpace(fn.ReturnType)
	fd.CParams = strings.TrimSpace(fn.ParamsRaw)

	// Extract IDS directives from the leading doc.
	returnLabel := ""
	handlerType := ""
	handlerMode := ""
	if fn.LeadingDoc != "" {
		cleaned, order, icon, label, menuCol, menuRow, menuPosSet := extractDocDirectives(fn.LeadingDoc)
		// C99: a return value has no name, so a `return:<label>.`
		// directive in the leading comment lets the specialist give
		// the synthetic `return` output a human label. This is for
		// the IDE only — codegen still uses the real return type.
		// extractDocDirectives doesn't know this directive, so it
		// survives into `cleaned`; we pull it out here so it doesn't
		// leak into the device's prose doc.
		cleaned, returnLabel = extractReturnLabelDirective(cleaned)
		// C99: `// handle:consume.` marks the destructor — the function
		// that consumes the wire-type handle and ends the resource chain
		// (no pass-through republish). Pull it out so it never leaks into
		// the device's prose doc; the flag drives FunctionSynthesizedOutputs.
		cleaned, fd.ConsumesHandle = extractConsumesHandleDirective(cleaned)
		// C99: `// min-target:<class>.` — the minimum hardware class; see
		// target_class.go. Pulled out so it never leaks into the prose doc.
		// Português: A classe mínima de hardware; extraída para não vazar
		// na prosa.
		cleaned, fd.MinTarget = extractMinTargetDirective(cleaned)
		// C99: `// device:false.` — public helper, not a device.
		// Português: Helper público, não vira device.
		cleaned, fd.NoDevice = extractDeviceDirective(cleaned)
		// C99: `// callback:<callbackType>.` marks a callback handler — a
		// function referenced (not called) and passed by address into a
		// callback parameter of that type. Pull it out so it never leaks into
		// the device's prose doc; the type drives the handler branch below.
		cleaned, handlerType, handlerMode = extractCallbackDirective(cleaned)
		fd.Doc = strings.TrimSpace(cleaned)
		fd.ExecutionOrder = order
		fd.Icon = icon
		fd.Label = label
		fd.MenuCol = menuCol
		fd.MenuRow = menuRow
		fd.MenuPosSet = menuPosSet
	}
	fd.HandlerType = handlerType
	// Set the callback mode (only "ref" is reference-only; anything else — the
	// empty value of a bare `// callback:<type>.`, "both", or a typo — defaults
	// to "both"). The mode is METADATA that drives the IDE: it decides whether
	// the CALLABLE device variant is offered. The CALLBACK reference is a
	// SEPARATE dedicated device the IDE synthesizes from the function name +
	// HandlerType, so the parsed def stays the pure callable — it keeps its
	// parameters as inputs and is NEVER given a `callback` output here (no
	// hybrid block). Codegen routes by the node's device type, not by
	// HandlerType. See the duality section of docs/CODEGEN_C99_CALLBACKS.md.
	if fd.HandlerType != "" {
		if handlerMode == CallbackModeRef {
			fd.CallbackMode = CallbackModeRef
		} else {
			fd.CallbackMode = CallbackModeBoth
		}
	}

	// Parse parameters. We split at top-level commas, then classify
	// each piece. Every parameter is a port (no receiver to skip).
	paramTokens := splitParams(fn.ParamsRaw)

	for i := 0; i < len(paramTokens); i++ {
		t := paramTokens[i]
		port := portFromParamToken(t)
		if port == nil {
			continue
		}
		// Record the parameter's position so codegen can rebuild the call in
		// source order (inputs and out-params interleave in the signature).
		port.portDef.ParamIndex = i
		// Per-method port cap. ParserLimits has separate Input
		// and Output caps; apply each to the matching slice.
		if port.isOutput {
			fd.Outputs = append(fd.Outputs, port.portDef)
			if limits.MaxOutputs > 0 && len(fd.Outputs) >= limits.MaxOutputs {
				break
			}
		} else {
			fd.Inputs = append(fd.Inputs, port.portDef)
			if limits.MaxInputs > 0 && len(fd.Inputs) >= limits.MaxInputs {
				break
			}
		}
	}

	// Collapse `slice:`-paired parameters: (pointer, length) → ONE
	// collection input port typed "[]T". Must run after the loop above
	// so the length parameter — wherever it sits in the signature — has
	// been seen and carries its ParamIndex.
	// Português: Colapsa pares (ponteiro, tamanho) marcados com `slice:`
	// numa única porta de coleção "[]T".
	collapseSliceParams(fd)

	// Return value → a typed output port.
	//
	// C99 is the source of truth here: a function has a return type,
	// and that is all. There is no `error` type, no automatic error
	// propagation, and no "runtime-handled" concept — those were Go
	// notions that do not belong in C99 (corrected 2026-05-25). So a
	// non-void return becomes ONE output port whose type is the
	// return type, verbatim (`esp_err_t` stays `esp_err_t`, not
	// `error`). Whether the caller treats that value as an error
	// code (checking `ESP_OK`, branching, logging) is the maker's
	// logic in the graph — the parser does not presume it.
	//
	// The return value has no name in C99 (unlike parameters), so we
	// synthesise the port name "return". The specialist can attach a
	// label like any other port. `void` yields no output port.
	if !isVoidReturn(fn.ReturnType) {
		// [PTR] A scalar-pointer return (`int32_t *get_buffer()`) becomes a
		// POINTER WIRE with the abstract family token: int8..uint64 → "int*",
		// float/double → "float*", bool → "bool*", uint8 stays in the int
		// family. `char *` keeps the existing VALUE convention (a C string
		// IS char*, so it stays "string" — no star). Non-scalar pointers
		// (struct handles) keep the verbatim type: the resource-chain idiom
		// is untouched. The debug family dereferences these wires; nothing
		// else accepts them (AllowedTypes intersection is the gate).
		// Português: Retorno ponteiro-escalar vira FIO PONTEIRO com o token
		// abstrato da família: int8..uint64 → "int*", float/double →
		// "float*", bool → "bool*". `char *` mantém a convenção de VALOR
		// (string C É char*, então fica "string" — sem estrela). Ponteiros
		// não-escalares (handles de struct) mantêm o tipo verbatim: o
		// idioma resource-chain fica intocado. A família debug dereferencia
		// esses fios; nada mais os aceita (a interseção de AllowedTypes é o
		// portão).
		retType := normaliseReturnType(fn.ReturnType)
		wireType := ""
		if elem := cPointerElemToIDE(retType); elem != "" && elem != "string" {
			// GoType stays the AUTHORED C type (the return capture is
			// declared with it — `int32_t *ret = f();`); only the WIRE
			// speaks the family token.
			// Português: GoType fica o tipo C AUTORAL (a captura de
			// retorno é declarada com ele); só o FIO fala o token de
			// família.
			wireType = cPointerFamilyToken(elem)
		}
		fd.Outputs = append(fd.Outputs, PortDef{
			Name:        "return",
			GoType:      retType,
			WireType:    wireType,
			Label:       returnLabel,
			Connection:  "optional",
			MissingConn: false,
		})
	}

	return fd
}

// fixedWidthPointerElem is the STRICT pointee mapping: only C types whose
// bit-width is part of the authored contract (stdint fixed-width, float,
// double). The slice: collapse uses it exclusively — a collection's element
// width defines an array ABI, so platform-width elements (plain int, long)
// stay invalid there (pinned by TestParseC_SliceDirective_InvalidDropped),
// while still being perfectly fine as VALUE/probe wire tokens.
// Português: Mapeamento ESTRITO do apontado: só tipos C cuja largura faz
// parte do contrato autoral (stdint, float, double). O colapso de slice: o
// usa exclusivamente — a largura do elemento define ABI de array, então
// elementos de largura-de-plataforma (int puro, long) seguem inválidos lá
// (pinado pelo TestParseC_SliceDirective_InvalidDropped), continuando
// perfeitamente válidos como tokens de fio de VALOR/sonda.
func fixedWidthPointerElem(t string) string {
	switch t {
	case "int8_t":
		return "int8"
	case "int16_t":
		return "int16"
	case "int32_t":
		return "int32"
	case "int64_t":
		return "int64"
	case "uint8_t":
		return "uint8"
	case "uint16_t":
		return "uint16"
	case "uint32_t":
		return "uint32"
	case "uint64_t":
		return "uint64"
	case "float":
		return "float32"
	case "double":
		return "float64"
	}
	return ""
}

// cPointerElemFixedWidthOf normalises a pointer C type exactly like
// cPointerElemToIDE (const strip, star count, char** string case) but maps
// the pointee through the STRICT fixed-width table only.
// Português: Normaliza um tipo C de ponteiro exatamente como o
// cPointerElemToIDE (const, contagem de estrelas, caso char**) mas mapeia o
// apontado só pela tabela ESTRITA fixed-width.
func cPointerElemFixedWidthOf(cType string) string {
	t := strings.TrimSpace(cType)
	t = strings.TrimPrefix(t, "const ")
	t = strings.TrimSpace(t)
	stars := 0
	for strings.HasSuffix(t, "*") {
		t = strings.TrimSpace(strings.TrimSuffix(t, "*"))
		stars++
	}
	if stars == 2 && (t == "char" || t == "const char") {
		return "string"
	}
	if stars != 1 {
		return ""
	}
	return fixedWidthPointerElem(t)
}

// cPointerFamilyToken maps a pointer's IDE element type to the abstract
// pointer-wire token of its FAMILY. Width is irrelevant on the wire — the
// only consumers are the debug devices, which widen on print anyway — so
// every integer width collapses to "int*". "string" means the pointer was
// `char *`: that is a C string by convention and travels as a VALUE.
// Português: Mapeia o tipo-elemento IDE de um ponteiro para o token
// abstrato de fio da FAMÍLIA. Largura é irrelevante no fio — os únicos
// consumidores são os devices de debug, que alargam no print — então toda
// largura inteira colapsa para "int*". "string" significa que o ponteiro
// era `char *`: string C por convenção, viaja como VALOR.
func cPointerFamilyToken(elem string) string {
	switch elem {
	case "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "int":
		return "int*"
	case "float", "float32", "float64":
		return "float*"
	case "bool":
		return "bool*"
	case "byte":
		return "byte*"
	case "string":
		return "string"
	default:
		return elem + "*"
	}
}

// isVoidReturn reports whether a return-type string is `void`
// (ignoring storage-class specifiers and surrounding whitespace).
// A `void*` return is NOT void — it returns a pointer — so the
// pointer check matters.
func isVoidReturn(ret string) bool {
	t := strings.TrimSpace(ret)
	for _, q := range []string{"static ", "extern ", "inline "} {
		t = strings.TrimPrefix(t, q)
	}
	t = strings.TrimSpace(t)
	return t == "void"
}

// normaliseReturnType trims storage-class specifiers from a return
// type so the output port carries the bare type the maker cares
// about (`esp_err_t`, `int`, `char *`, `struct Foo *`). It does NOT
// collapse or reinterpret the type — C99 is the source of truth, so
// `esp_err_t` stays `esp_err_t`.
func normaliseReturnType(ret string) string {
	t := strings.TrimSpace(ret)
	for _, q := range []string{"static ", "extern ", "inline "} {
		t = strings.TrimPrefix(t, q)
	}
	return strings.TrimSpace(t)
}

// callbackSignatureMatch reports whether a function whose raw return type is
// retRaw and raw parameter list is paramsRaw has the SAME C signature as the
// callback typedef ct — same return type and the same ordered parameter types,
// parameter names ignored. This is exactly the condition under which the
// function may be passed by reference into a `ct` callback parameter: the
// generated `consumer(fn)` is well-typed only when the signatures match. It
// drives FuncDef.CompatibleCallbacks (which the wizard offers / disables on),
// so a signature-incompatible handler can never be authored through the UI.
//
// Português: Diz se a assinatura C da função (tipo de retorno + tipos dos
// parâmetros, nomes ignorados) é igual à do typedef de callback — condição
// para a função poder ser handler desse callback.
func callbackSignatureMatch(retRaw, paramsRaw string, ct CallbackTypeDef) bool {
	if normaliseCType(normaliseReturnType(retRaw)) != normaliseCType(normaliseReturnType(ct.ReturnType)) {
		return false
	}
	return cParamTypesEqual(paramsRaw, ct.Params)
}

// cParamTypesEqual compares two raw C parameter lists by their ordered
// sequence of parameter TYPES (names ignored). An empty list and a lone
// `void` both mean "no parameters".
func cParamTypesEqual(aRaw, bRaw string) bool {
	a := cParamTypeList(aRaw)
	b := cParamTypeList(bRaw)
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

// cParamTypeList splits a raw C parameter list and returns the normalised
// type of each parameter, in order. A `void` sentinel or an empty list yields
// no entries. Parameter names are stripped; for unnamed parameters
// (`const char *`) the whole token is the type.
func cParamTypeList(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "void" {
		return nil
	}
	var out []string
	for _, tok := range splitParams(raw) {
		out = append(out, cParamType(tok.text))
	}
	return out
}

// cParamType returns the normalised type of a single raw parameter token,
// stripping the parameter name when present. splitCFieldNameType handles the
// named case (`const char *text` → `const char *`); when it declines (an
// unnamed parameter such as `const char *`, or a bare `int`), the whole token
// is the type.
func cParamType(tokenText string) string {
	d := strings.TrimSpace(tokenText)
	if name, typ := splitCFieldNameType(d); name != "" && typ != "" {
		return normaliseCType(typ)
	}
	return normaliseCType(d)
}

// normaliseCType canonicalises a C type string for comparison: it collapses
// whitespace runs to single spaces and removes spacing around `*`, so
// `const char *`, `const char*` and `const   char  *` all compare equal, and
// `char **` equals `char**`. It does NOT reinterpret the type — only spacing
// is canonicalised, both sides identically.
func normaliseCType(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	t = strings.ReplaceAll(t, " *", "*")
	t = strings.ReplaceAll(t, "* ", "*")
	return t
}

// extractReturnLabelDirective pulls a `return:<label>.` segment out
// of the (already directive-cleaned) doc text and returns the doc
// with that segment removed plus the label ("" when absent).
//
// C99 gives a return value no name, so the specialist labels the
// synthetic `return` output through this directive in the function's
// leading comment. It is purely a human label — codegen uses the
// real return type. We strip it here so it never leaks into the
// device's prose doc. The matching mirrors extractDocDirectives:
// each line is split on ".", and a segment beginning with "return:"
// is consumed.
func extractReturnLabelDirective(doc string) (cleaned, label string) {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		// A line with no `return:` directive is kept VERBATIM, so prose that
		// wraps across several lines survives intact. Splitting such a line on
		// "." and rejoining with a per-line "." would insert a spurious period
		// mid-sentence — e.g. "...followed by a" + "newline." becomes
		// "...followed by a." + "newline." — which then shows up in the
		// generated main.c (the authored source is inlined for the reader).
		// This mirrors the verbatim stance extractDocDirectives already takes.
		//
		// Português: Linha sem diretivo `return:` é mantida VERBATIM, para
		// prosa que quebra entre linhas sobreviver intacta. Dividir por "." e
		// rejuntar com "." por linha inseriria um ponto espúrio no meio da
		// frase, que acabaria aparecendo no main.c gerado (a fonte autoral é
		// embutida para o leitor). Espelha a postura da extractDocDirectives.
		if !strings.Contains(strings.ToLower(line), "return:") {
			prose = append(prose, line)
			continue
		}
		var keptSegs []string
		for _, segment := range strings.Split(line, ".") {
			seg := strings.TrimSpace(segment)
			if seg == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(seg), "return:") {
				v := strings.TrimSpace(seg[len("return:"):])
				if v != "" {
					label = v
				}
				continue // drop the directive segment
			}
			keptSegs = append(keptSegs, seg)
		}
		if len(keptSegs) > 0 {
			prose = append(prose, strings.Join(keptSegs, ". ")+".")
		}
	}
	return strings.TrimSpace(strings.Join(prose, "\n")), label
}

// extractConsumesHandleDirective pulls a `handle:consume.` segment out of
// a function's leading-comment text and returns the doc with that segment
// removed plus whether it was present. Mirrors extractReturnLabelDirective's
// line/`.`-segment scan so it composes with the rest of the directive
// parsing. `handle:consume.` marks the destructor — the C99 device-function
// that consumes its wire-type handle and ends the resource chain, so no
// pass-through output is synthesized for it (see FunctionSynthesizedOutputs).
// extractMinTargetDirective pulls `min-target:<class>.` from a leading
// comment — the specialist's declaration of the smallest hardware class
// the function runs on (see target_class.go). The raw token is kept even
// when unknown: the export validator turns a typo into a clear diagnostic
// listing the valid classes, which beats silently guessing here.
// Português: Extrai `min-target:<classe>.` do comentário de cabeçalho — a
// declaração do especialista da menor classe de hardware onde a função
// roda (ver target_class.go). O token cru é mantido mesmo desconhecido: o
// validador de export transforma typo em diagnóstico claro listando as
// classes válidas, melhor que chutar aqui.
// extractDeviceDirective pulls `device:<v>.` from a leading comment. The
// only meaningful value is "false" (case-insensitive) — the specialist's
// opt-out from device generation for a public helper. Any other value is
// ignored on purpose: "device:true" is the default and needs no tag.
// Português: Extrai `device:<v>.`. O único valor com significado é
// "false" — o opt-out do especialista para helper público. Qualquer outro
// valor é ignorado de propósito: "device:true" é o default.
func extractDeviceDirective(doc string) (cleaned string, noDevice bool) {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		low := strings.ToLower(line)
		if !strings.Contains(low, "device:") {
			prose = append(prose, line)
			continue
		}
		var keptSegs []string
		for _, seg := range strings.Split(line, ".") {
			s := strings.TrimSpace(seg)
			if v, ok := strings.CutPrefix(strings.ToLower(s), "device:"); ok {
				if strings.TrimSpace(v) == "false" {
					noDevice = true
				}
				continue
			}
			if s != "" {
				keptSegs = append(keptSegs, s)
			}
		}
		if len(keptSegs) > 0 {
			prose = append(prose, strings.Join(keptSegs, ". ")+".")
		}
	}
	return strings.Join(prose, "\n"), noDevice
}

func extractMinTargetDirective(doc string) (cleaned string, minTarget string) {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		low := strings.ToLower(line)
		if !strings.Contains(low, "min-target:") {
			prose = append(prose, line)
			continue
		}
		var keptSegs []string
		for _, seg := range strings.Split(line, ".") {
			s := strings.TrimSpace(seg)
			if v, ok := strings.CutPrefix(strings.ToLower(s), "min-target:"); ok {
				minTarget = strings.TrimSpace(v)
				continue
			}
			if s != "" {
				keptSegs = append(keptSegs, s)
			}
		}
		if len(keptSegs) > 0 {
			prose = append(prose, strings.Join(keptSegs, ". ")+".")
		}
	}
	return strings.Join(prose, "\n"), minTarget
}

func extractConsumesHandleDirective(doc string) (cleaned string, consume bool) {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		// Lines without a `handle:` directive are kept verbatim, so wrapped
		// prose is not corrupted by a per-line "." (see
		// extractReturnLabelDirective for the full rationale).
		if !strings.Contains(strings.ToLower(line), "handle:") {
			prose = append(prose, line)
			continue
		}
		var keptSegs []string
		for _, segment := range strings.Split(line, ".") {
			seg := strings.TrimSpace(segment)
			if seg == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(seg), "handle:") {
				v := strings.ToLower(strings.TrimSpace(seg[len("handle:"):]))
				if v == "consume" {
					consume = true
				}
				continue // drop the directive segment either way
			}
			keptSegs = append(keptSegs, seg)
		}
		if len(keptSegs) > 0 {
			prose = append(prose, strings.Join(keptSegs, ". ")+".")
		}
	}
	return strings.TrimSpace(strings.Join(prose, "\n")), consume
}

// extractCallbackDirective pulls a `callback:<callbackType>[:<mode>].` segment
// out of a function's leading-comment text and returns the doc with that
// segment removed, plus the callback type name and the optional mode (both
// empty when absent). Mirrors
// extractConsumesHandleDirective's line/`.`-segment scan so it composes with
// the rest of the directive parsing. `callback:T.` marks the function as a
// CALLBACK HANDLER for the function-pointer typedef T: it is never called in
// the flow, only referenced — its address is passed to a callback parameter
// of type T (the LabVIEW static VI reference idiom). The optional `<mode>`
// ("both" default, "ref" reference-only) refines how the handler is exposed.
// The type and mode drive funcDefFromRaw's handler branch, FuncDef.HandlerType,
// and FuncDef.CallbackMode.
//
// `callback:` is deliberately a different word from the destructor's
// `handle:consume.` — they share no prefix, so neither extractor can claim
// the other's directive and a specialist won't confuse the two when reading.
func extractCallbackDirective(doc string) (cleaned string, callbackType string, callbackMode string) {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		// Lines without a `callback:` directive are kept verbatim, so wrapped
		// prose is not corrupted by a per-line "." (see
		// extractReturnLabelDirective for the full rationale).
		if !strings.Contains(strings.ToLower(line), "callback:") {
			prose = append(prose, line)
			continue
		}
		var keptSegs []string
		for _, segment := range strings.Split(line, ".") {
			seg := strings.TrimSpace(segment)
			if seg == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(seg), "callback:") {
				v := strings.TrimSpace(seg[len("callback:"):])
				// v is "<type>" or "<type>:<mode>"; split on the first ":".
				// The mode is returned verbatim (lower-cased); funcDefFromRaw
				// normalizes anything other than "ref" to "both".
				if i := strings.IndexByte(v, ':'); i >= 0 {
					callbackType = strings.TrimSpace(v[:i])
					callbackMode = strings.ToLower(strings.TrimSpace(v[i+1:]))
				} else if v != "" {
					callbackType = v
				}
				continue // drop the directive segment either way
			}
			keptSegs = append(keptSegs, seg)
		}
		if len(keptSegs) > 0 {
			prose = append(prose, strings.Join(keptSegs, ". ")+".")
		}
	}
	return strings.TrimSpace(strings.Join(prose, "\n")), callbackType, callbackMode
}

type paramToken struct {
	text       string
	leadingDoc string
}

// splitParams breaks the raw parameter list at top-level commas
// (respecting nested parens for function-pointer parameters). For
// each piece, it splits leading `//` comment lines from the
// declaration text. Leading whitespace + newlines between params
// are preserved in the text portion to keep type strings intact.
//
// `void` (the empty-param marker) returns an empty slice — the
// function has no inputs and no outputs.
func splitParams(raw string) []paramToken {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "void" {
		return nil
	}

	// Split on top-level commas. Commas (and parens) inside `//` or
	// `/* */` comments must be ignored — a port's leading comment may
	// contain prose like "0-100 %, typical" whose comma is NOT a
	// parameter separator. We skip over comment spans while scanning.
	var pieces []string
	depth := 0
	start := 0
	for i := 0; i < len(raw); i++ {
		// Skip a line comment to end-of-line.
		if raw[i] == '/' && i+1 < len(raw) && raw[i+1] == '/' {
			j := strings.IndexByte(raw[i:], '\n')
			if j < 0 {
				i = len(raw)
			} else {
				i += j // loop's i++ steps past the '\n'
			}
			continue
		}
		// Skip a block comment to its close.
		if raw[i] == '/' && i+1 < len(raw) && raw[i+1] == '*' {
			j := strings.Index(raw[i+2:], "*/")
			if j < 0 {
				i = len(raw)
			} else {
				i += 2 + j + 1 // land on the closing '/'
			}
			continue
		}
		switch raw[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				pieces = append(pieces, raw[start:i])
				start = i + 1
			}
		}
	}
	pieces = append(pieces, raw[start:])

	out := make([]paramToken, 0, len(pieces))
	for _, p := range pieces {
		// Each piece may have leading // comments. Split them off.
		commentLines, decl := splitFieldChunk(p + ";")
		decl = strings.TrimSpace(strings.TrimSuffix(decl, ";"))
		if decl == "" {
			continue
		}
		out = append(out, paramToken{
			text:       decl,
			leadingDoc: strings.Join(commentLines, "\n"),
		})
	}
	return out
}

// portFromParamToken converts a paramToken into a port. Returns nil
// for empty / unparseable tokens (Slice 2 skips function-pointer
// params and other oddities silently).
func portFromParamToken(t paramToken) *paramPort {
	name, typ := splitCFieldNameType(t.text)
	if name == "" {
		return nil
	}

	port := PortDef{
		Name:        name,
		GoType:      typ,
		Connection:  "",
		MissingConn: true, // default; flipped below if `connection:` present
		Label:       name,
	}

	// Direction is EXPLICIT, not inferred. Decision (b), 2026-05-25:
	// every C99 parameter is an input by default — passing a pointer is
	// still passing a value (the address), so "the function can write
	// through it" is not, by itself, an output. A parameter becomes an
	// output only when the specialist marks it with a `direction:out.`
	// directive (surfaced as a checkbox in the Wizard). That directive
	// is meaningless on a value or a const pointer — the function has no
	// way to hand data back through those — so it only takes effect on a
	// mutable pointer (canBeOutput).
	wantOut := false
	doc := t.leadingDoc
	if doc != "" {
		// Pull `direction:out.` out first so it never leaks into the
		// port's prose doc, then `slice:<lenParam>.` (the collection
		// pairing directive — const-array plan Task 7), then parse the
		// rest of the IDS vocabulary. The slice pairing itself is
		// resolved later by collapseSliceParams, once every parameter
		// of the function is known.
		doc, wantOut = extractDirectionDirective(doc)
		var sliceLen string
		doc, sliceLen = extractSliceDirective(doc)
		port.SliceLenName = sliceLen
		meta := parsePortMetaString(doc)
		applyPortMeta(&port, meta)
	}

	isOutput := wantOut && canBeOutput(typ)

	// [PTR] A scalar-pointer INPUT (`const int32_t *data`, or a mutable
	// pointer the specialist did NOT mark direction:out) exposes the
	// abstract pointer-family token on the WIRE while GoType stays the
	// authored C type (declarations and call-site casts read GoType — see
	// the WireType field doc). Three guards: outputs are handled by the
	// out-param split; slice-paired pointers must keep collapsing into a
	// collection (collapseSliceParams reads the authored type later); and
	// `char *` keeps the C-string VALUE convention.
	// Português: Uma ENTRADA ponteiro-escalar (`const int32_t *data`, ou
	// ponteiro mutável que o especialista NÃO marcou direction:out) expõe
	// o token abstrato da família no FIO enquanto GoType fica o tipo C
	// autoral (declarações e casts leem GoType — ver doc do campo
	// WireType). Três guardas: outputs vão pelo split de out-param;
	// ponteiros pareados por slice: precisam continuar colapsando em
	// coleção; e `char *` mantém a convenção de VALOR de string C.
	if port.SliceLenName == "" {
		if elem := cPointerElemToIDE(typ); elem != "" && elem != "string" {
			if isOutput {
				// [PTR] An out-param IS a value source on the stage: the
				// pointer is a calling convention, not wire semantics —
				// the wire carries the VALUE token (field report
				// 2026-07-11: out-params exposed the verbatim C type and
				// connected to nothing). Declarations keep the authored
				// type via cDerefType.
				// Português: Out-param É fonte de valor no stage: o
				// ponteiro é convenção de chamada, não semântica de fio —
				// o fio carrega o token de VALOR (report 2026-07-11:
				// out-params expunham o tipo C verbatim e não conectavam
				// em nada). Declarações mantêm o tipo autoral via
				// cDerefType.
				port.WireType = elem
			} else {
				port.WireType = cPointerFamilyToken(elem)
			}
		}
	}

	return &paramPort{
		portDef:  port,
		isOutput: isOutput,
	}
}

// extractDirectionDirective pulls a `direction:out.` (or
// `direction:in.`) segment out of a port's leading-comment text and
// returns the doc with that segment removed plus whether "out" was
// requested. Mirrors extractReturnLabelDirective's line/`.`-segment
// scan so it composes with the rest of the directive parsing.
func extractDirectionDirective(doc string) (cleaned string, out bool) {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		var keptSegs []string
		for _, segment := range strings.Split(line, ".") {
			seg := strings.TrimSpace(segment)
			if seg == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(seg), "direction:") {
				v := strings.ToLower(strings.TrimSpace(seg[len("direction:"):]))
				if v == "out" {
					out = true
				}
				continue // drop the directive segment either way
			}
			keptSegs = append(keptSegs, seg)
		}
		if len(keptSegs) > 0 {
			prose = append(prose, strings.Join(keptSegs, ". ")+".")
		}
	}
	return strings.TrimSpace(strings.Join(prose, "\n")), out
}

// paramPort wraps a PortDef with the direction the parser resolved
// (from the explicit `direction:` directive, defaulting to input).
type paramPort struct {
	portDef  PortDef
	isOutput bool
}

// extractSliceDirective pulls a `slice:<lenParamName>.` segment out of a
// port's leading-comment text and returns the doc with that segment removed
// plus the named length parameter (empty when absent). Mirrors
// extractDirectionDirective's line/`.`-segment scan so it composes with the
// rest of the directive parsing.
//
// IDS authoring (const-array plan Task 7): the directive lives on the
// POINTER parameter and names its length companion —
//
//	void mixer_run(
//	    // level table.  slice:values_len.  connection:mandatory.
//	    const uint16_t* values,
//	    size_t values_len,
//	    uint16_t gain);
//
// NOTE — deliberate divergence from the plan's early sketch
// (`slice: values values_len`): every per-parameter IDS directive
// (`direction:`, `connection:`, `callback:`) lives in the parameter's own
// comment, where naming the pointer again would be redundant. One token,
// same grammar as its siblings.
//
// Português: Extrai `slice:<nomeDoParamDeTamanho>.` do comentário do
// parâmetro PONTEIRO. Diverge do rascunho do plano de propósito: as
// diretivas por-parâmetro vivem no comentário do próprio parâmetro —
// repetir o nome do ponteiro seria redundante.
func extractSliceDirective(doc string) (cleaned string, lenName string) {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		var keptSegs []string
		for _, segment := range strings.Split(line, ".") {
			seg := strings.TrimSpace(segment)
			if seg == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(seg), "slice:") {
				lenName = strings.TrimSpace(seg[len("slice:"):])
				continue // drop the directive segment either way
			}
			keptSegs = append(keptSegs, seg)
		}
		if len(keptSegs) > 0 {
			prose = append(prose, strings.Join(keptSegs, ". ")+".")
		}
	}
	cleaned = strings.Join(prose, "\n")
	return cleaned, lenName
}

// collapseSliceParams resolves every `slice:` directive of a function: for
// each input port that named a length companion, the (pointer, length) pair
// becomes ONE collection port —
//
//	GoType:        "[]T" (cPointerElemToIDE on the pointer's C type)
//	SliceLenIndex: the length parameter's position in the C signature
//
// — and the length parameter is REMOVED from the port list (it is consumed
// by the collection; the maker never sees it). Codegen later rebuilds the
// pair at the call site: `f(constArray1, constArray1_len, …)`.
//
// TOLERANT on authoring mistakes, matching the parser's house stance (cf.
// the callback-mode typo default): when the named length parameter does not
// exist, is not an integral type, or the directive sits on a parameter whose
// type is not an eligible element pointer, the directive is DROPPED — the
// two parameters stay ordinary scalar ports, nothing breaks, and the
// specialist sees the un-collapsed shape in the wizard. (A soft-warning
// channel is future parser work; see the dropping note in parser_c.go.)
//
// ELIGIBLE ELEMENT TYPES are fixed-width by design: embedded C must not
// guess platform-dependent widths, so `int*` / `size_t*` collections are
// rejected here — the specialist writes `int32_t*` and gets "[]int32".
//
// Português: Resolve as diretivas `slice:` — o par (ponteiro, tamanho)
// vira UMA porta de coleção "[]T" e o parâmetro de tamanho some da lista.
// Tolerante a erro de autoria: diretiva inválida é descartada e as portas
// ficam como estavam. Elementos exigem largura fixa (int32_t, não int).
func collapseSliceParams(fd *FuncDef) {
	// Two phases on purpose: resolving and REMOVING in the same range
	// loop would index past the shrunk slice (range captures the
	// original length). Phase 1 resolves every directive and marks the
	// consumed length parameters; phase 2 rebuilds Inputs without them.
	consumed := map[int]bool{} // positions in fd.Inputs to drop

	for i := range fd.Inputs {
		lenName := fd.Inputs[i].SliceLenName
		if lenName == "" {
			continue
		}

		// STRICT: collections demand fixed-width elements (see
		// fixedWidthPointerElem) — platform-width pointees stay invalid
		// here even though they are valid probe-wire tokens.
		// Português: ESTRITO: coleções exigem elementos fixed-width —
		// apontados de largura-de-plataforma seguem inválidos aqui mesmo
		// sendo tokens de fio de sonda válidos.
		elem := cPointerElemFixedWidthOf(fd.Inputs[i].GoType)
		lenPos := -1
		for j := range fd.Inputs {
			if j != i && !consumed[j] && fd.Inputs[j].Name == lenName {
				lenPos = j
				break
			}
		}

		if elem == "" || lenPos < 0 || !isIntegralCType(fd.Inputs[lenPos].GoType) {
			// Invalid pairing — drop the directive, keep both ports.
			fd.Inputs[i].SliceLenName = ""
			continue
		}

		fd.Inputs[i].GoType = "[]" + elem
		fd.Inputs[i].SliceLenIndex = fd.Inputs[lenPos].ParamIndex
		consumed[lenPos] = true
	}

	if len(consumed) == 0 {
		return
	}
	kept := fd.Inputs[:0]
	for j := range fd.Inputs {
		if !consumed[j] {
			kept = append(kept, fd.Inputs[j])
		}
	}
	fd.Inputs = kept
}

// cPointerElemToIDE maps an eligible C collection-pointer type to the IDE's
// bare element token: `const uint16_t*` → "uint16", `float *` → "float32",
// `const char**` → "string". Returns "" when the type is not an eligible
// element pointer — platform-width integers (`int*`, `size_t*`) are
// deliberately ineligible (fixed-width only; see collapseSliceParams), and
// a bare `char*`/`const char*` is a STRING SCALAR, not a collection (a
// string collection is char**).
//
// Português: Mapeia o ponteiro C elegível para o token de elemento do IDE.
// Larguras dependentes de plataforma são inelegíveis; `char*` é string
// escalar — coleção de string é `char**`.
func cPointerElemToIDE(cType string) string {
	t := strings.TrimSpace(cType)
	t = strings.TrimPrefix(t, "const ")
	t = strings.TrimSpace(t)

	stars := 0
	for strings.HasSuffix(t, "*") {
		t = strings.TrimSpace(strings.TrimSuffix(t, "*"))
		stars++
	}
	// A string collection is a pointer-to-pointer of char.
	if stars == 2 && (t == "char" || t == "const char") {
		return "string"
	}
	if stars != 1 {
		return ""
	}

	if elem := fixedWidthPointerElem(t); elem != "" {
		return elem
	}

	switch t {
	// [K&R] Plain C integer types. Their bit-width belongs to the PLATFORM
	// (the target profile), so they map to wire-vocabulary tokens only —
	// never to a declaration: out-param temps and return captures are
	// always declared with the AUTHORED type (cDerefType / GoType), which
	// keeps the ABI honest on every target. Field report 2026-07-11: an
	// `int *size_bytes` out-param exposed the verbatim "int *" on the
	// wire and connected to nothing.
	// Português: Tipos inteiros do C puro. A largura pertence à PLATAFORMA
	// (o target profile), então eles mapeiam para tokens de vocabulário de
	// fio apenas — nunca para declaração: temps de out-param e capturas de
	// retorno são sempre declarados com o tipo AUTORAL (cDerefType /
	// GoType), o que mantém o ABI honesto em todo target. Report de campo
	// 2026-07-11: um out-param `int *size_bytes` expunha "int *" verbatim
	// no fio e não conectava em nada.
	case "int", "signed", "signed int":
		return "int"
	case "long", "long int", "signed long", "signed long int":
		// Abstract on purpose: long is 32-bit on AVR, 64 elsewhere.
		// Português: Abstrato de propósito: long é 32 no AVR, 64 fora.
		return "int"
	case "long long", "long long int", "signed long long", "signed long long int":
		return "int64"
	case "short", "short int", "signed short", "signed short int":
		return "int16"
	case "unsigned char":
		return "uint8"
	case "unsigned short", "unsigned short int":
		return "uint16"
	case "unsigned", "unsigned int", "unsigned long", "unsigned long int":
		return "uint32"
	case "unsigned long long", "unsigned long long int":
		return "uint64"
	case "bool", "_Bool":
		return "bool"
	}
	// `char *` (one star) deliberately falls through to "": a single-star
	// char pointer is a C STRING and is handled by the string-value
	// convention elsewhere — mapping it here would collide with it.
	// Português: `char *` (uma estrela) cai de propósito no "": ponteiro
	// simples de char é STRING C, tratada pela convenção de valor em outro
	// lugar — mapear aqui colidiria com ela.
	return ""
}

// isIntegralCType reports whether a C type can serve as a collection LENGTH
// parameter. Any integral works — the value is only consumed (the codegen
// passes the const array's `_len` companion, a size_t, and C converts
// implicitly) — so the platform-width types are fine HERE, unlike element
// types.
//
// Português: Aceita qualquer integral como parâmetro de TAMANHO — o valor é
// só consumido, então largura de plataforma é aceitável aqui.
func isIntegralCType(cType string) bool {
	switch strings.TrimSpace(cType) {
	case "size_t", "int", "unsigned", "unsigned int", "long", "unsigned long",
		"long long", "unsigned long long", "short", "unsigned short",
		"int8_t", "int16_t", "int32_t", "int64_t",
		"uint8_t", "uint16_t", "uint32_t", "uint64_t":
		return true
	}
	return false
}

// canBeOutput reports whether a C99 parameter type is capable of
// carrying a value back to the caller — i.e. it is a mutable (non-
// const) pointer to something other than an opaque handle. This does
// NOT decide direction on its own (every parameter defaults to input);
// it only gates whether a `direction:out.` directive is allowed to
// take effect, and drives whether the Wizard offers the "output"
// checkbox at all.
//
//	int               → no  (value; can't hand data back)
//	const char *      → no  (pointer-to-const; can't write)
//	uint16_t *        → yes (mutable pointer)
//	void *            → no  (opaque handle the caller passes in)
//	const uint16_t *  → no
func canBeOutput(typ string) bool {
	t := strings.TrimSpace(typ)
	// Pointer present?
	if !strings.Contains(t, "*") {
		return false
	}
	tokens := strings.Fields(t)
	// `const` anywhere makes it incapable of being an output.
	for _, tok := range tokens {
		if tok == "const" {
			return false
		}
	}
	// `void*` is an opaque caller-supplied handle, not a destination.
	if strings.HasPrefix(t, "void") || strings.Contains(t, "void*") || strings.Contains(t, "void *") {
		return false
	}
	return true
}

// wireTypeNameForParam reports whether a C99 parameter type is a POINTER to
// one of def's wire-types and, if so, returns that wire-type's canonical
// Name (its tag). The match reuses tokeniseTypeString — the same normaliser
// Phase 8 uses to classify wire-types — so `sht3x_t *` (alias) and
// `struct sht3x *` (tag) both resolve to the "sht3x" wire-type. A non-pointer
// returns false: a wire-type passed by value is not a flowing handle.
func (def *BlackBoxDef) wireTypeNameForParam(typ string) (name string, ok bool) {
	if !strings.Contains(typ, "*") {
		return "", false
	}
	toks := tokeniseTypeString(typ)
	for _, w := range def.WireTypes {
		for _, t := range toks {
			if t == w.Name || (w.Alias != "" && t == w.Alias) {
				return w.Name, true
			}
		}
	}
	return "", false
}

// FunctionSynthesizedOutputs returns the EFFECTIVE output ports for a C99
// device-function as the stage should render them: the parsed outputs (the
// real C return, plus any `T*` result the function writes that the specialist
// marked `direction:out.`) followed by the SYNTHESIZED handle pass-through.
//
// The pass-through is the republished copy of the function's wire-type handle
// input, so the maker can chain resource blocks in series — the LabVIEW
// refnum idiom. Rules (docs/c99_ide_integration.md §2, §5.2):
//
//   - It is derived from the input being a WIRE-TYPE pointer, NOT from any
//     directive. ParseC stays faithful; this method does the presentation-
//     layer synthesis on demand, so the parsed def never carries a fake port.
//   - The destructor (fn.ConsumesHandle, from `// handle:consume.`) gets NO
//     pass-through — it consumes the handle and ends the chain.
//   - At most ONE wire-type carries a pass-through. With two or more DISTINCT
//     wire-types in the signature, none is synthesized (the maker wires each
//     as a plain input); revisit when a real multi-wire-type case appears.
//
// The synthesized port is named `<input>_out` (e.g. `dev` → `dev_out`),
// shares the input's GoType and Label, is flagged PassThrough, and is
// connection-optional — chaining onward is the maker's choice, so it raises
// no missing-wire ⚠. Codegen must treat PassThrough ports as the same handle
// variable, never as a return value.
func (def *BlackBoxDef) FunctionSynthesizedOutputs(fn FuncDef) []PortDef {
	out := make([]PortDef, len(fn.Outputs))
	copy(out, fn.Outputs)

	if fn.ConsumesHandle {
		return out
	}

	// Collect the first input port per DISTINCT wire-type, in source order.
	firstPerType := make(map[string]PortDef)
	var order []string
	for _, in := range fn.Inputs {
		wn, ok := def.wireTypeNameForParam(in.GoType)
		if !ok {
			continue
		}
		if _, seen := firstPerType[wn]; !seen {
			firstPerType[wn] = in
			order = append(order, wn)
		}
	}

	// Exactly one distinct wire-type → synthesize its pass-through.
	// Zero, or two-or-more distinct → none.
	if len(order) != 1 {
		return out
	}

	in := firstPerType[order[0]]
	out = append(out, PortDef{
		Name:        in.Name + "_out",
		GoType:      in.GoType,
		Label:       in.Label,
		Connection:  "optional",
		MissingConn: false,
		PassThrough: true,
	})
	return out
}
