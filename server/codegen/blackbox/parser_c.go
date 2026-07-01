package blackbox

// parser_c.go — C99 source → BlackBoxDef.
//
// English:
//
//	Slice 2 of C99 device support. This parser is the IDS GENERATOR's
//	entry point — see docs/claude_c99_device_support.md §2.12.
//	The contract changed since Slice 1:
//
//	  - Permissive on input. Source with zero IDS annotations is
//	    valid; the wizard will help the user add them. The parser
//	    never rejects for "no struct" or "no methods" — it returns
//	    whatever it found, empty or not.
//
//	  - Collects EVERY struct in the file (Slice 1 took only the
//	    first). The BlackBoxDef.Structs []StructDef field carries
//	    them all; legacy single-struct fields mirror Structs[0] for
//	    SPA back-compat.
//
//	  - Accepts all three C99 struct forms:
//
//	      struct Tag { ... };                       Name = Tag
//	      typedef struct Tag { ... } Alias;         Name = Tag    (tag wins)
//	      typedef struct      { ... } Alias;        Name = Alias  (only available)
//
//	  - Collects EVERY top-level function. A function whose name
//	    starts with `<StructName>_` AND whose first parameter is
//	    the expected receiver `struct <StructName>* s` becomes that
//	    struct's method (Init or Methods). Other functions are
//	    surfaced in BlackBoxDef.Extras so the wizard can offer to
//	    convert them later.
//
//	File split:
//	  - parser_c_lex.go — byte-level helpers (preprocess, lookups).
//	  - parser_c.go     — structural pass (this file): find structs,
//	                       extract directives + props, assemble def.
//	  - parser_c_func.go — function-signature parser and method
//	                       classifier (parameter grammar + port
//	                       directives).
//
// Português:
//
//	Slice 2 do C99. Parser permissivo — aceita código sem IDS,
//	coleta todos os structs e funções. Três formas de struct.
//	Multi-struct. Métodos classificados por convenção
//	`<Struct>_<Method>` com receiver obrigatório.

import (
	"strings"
)

// ParseC reads C99 source and returns a fully populated BlackBoxDef.
//
// Slice 2 contract (see file-level doc and design doc §2.12, §7):
//
//   - Empty input → empty BlackBoxDef (no error).
//
//   - Source with code but no struct → BlackBoxDef with Imports
//     populated, Structs empty, possibly Extras populated. No error.
//
//   - Source with N structs → BlackBoxDef.Structs has N entries.
//     Legacy first-struct fields mirror Structs[0].
//
//   - Source with extra functions (not matching any struct) →
//     BlackBoxDef.Extras populated. No error.
//
//   - `limits.MaxProps` truncates each struct's prop list.
//     `limits.MaxMethods` caps method count PER STRUCT. Hard
//     errors are reserved for the structurally impossible
//     (unterminated braces); soft truncation is silent.
func ParseC(src []byte, limits ParserLimits) (*BlackBoxDef, error) {
	s := string(src)

	def := &BlackBoxDef{}

	// ── Phase 1: preprocess ──────────────────────────────────────────────
	// Strip strings/chars/block comments so brace matching is safe.
	stripped, blockComments := preprocessC(s)

	// ── Phase 2: package doc ─────────────────────────────────────────────
	def.Doc = extractCPackageDoc(s)

	// ── Phase 3: includes ────────────────────────────────────────────────
	def.Imports = extractCIncludes(stripped)

	// ── Phase 4: find all structs ────────────────────────────────────────
	rawStructs, err := findAllCStructs(stripped)
	if err != nil {
		return nil, err
	}

	// Back-fill aliases (and the doc anchor) declared in separate
	// forward typedefs. Two cases:
	//   1. The body exists in this source — attach the alias and the
	//      forward typedef's offset to the matching struct so the
	//      wire-type's doc/label/icon is read from above the typedef
	//      (the public interface), not from above the body.
	//   2. The body does NOT exist here (a purely opaque handle whose
	//      `struct Tag { ... }` lives in the .c) — synthesise a
	//      body-less rawCStruct anchored on the forward typedef so the
	//      handle still surfaces as a wire-type.
	if fwd := forwardTypedefStructs(stripped); len(fwd) > 0 {
		seen := make(map[string]bool, len(rawStructs))
		for i := range rawStructs {
			seen[rawStructs[i].Tag] = true
			if info, ok := fwd[rawStructs[i].Tag]; ok {
				if rawStructs[i].Alias == "" {
					rawStructs[i].Alias = info.Alias
				}
				rawStructs[i].TypedefDeclStart = info.DeclStart
			}
		}
		for tag, info := range fwd {
			if seen[tag] {
				continue // body present — already handled above
			}
			// Body-less opaque handle: anchor everything on the typedef.
			rawStructs = append(rawStructs, rawCStruct{
				Name:             tag,
				Tag:              tag,
				Alias:            info.Alias,
				TypedefDeclStart: info.DeclStart,
				DeclStart:        info.DeclStart,
				BodyStart:        0,
				BodyEnd:          0,
				DeclEnd:          info.DeclStart,
			})
		}
	}

	// ── Phase 5: find all PUBLIC functions ───────────────────────────────
	// findAllCFunctions already filters out `static` functions and
	// dedupes (.h declaration + .c definition → one entry). What
	// returns here is the parser's view of the "public API" of the
	// source.
	rawFuncs := findAllCFunctions(s, stripped, blockComments)

	// ── Phase 7: build struct entries (NO method classification) ─────────
	// Decision (b), 2026-05-25: C99 has no methods. We still parse each
	// struct's props/doc/icon/label here because a referenced struct
	// becomes a WIRE-TYPE (Phase 8), but we no longer fold any function
	// into a struct as its method — every public function is a
	// standalone device-function (Phase 9), and what used to be the
	// `struct X *` receiver is just an ordinary input port.
	type structEntry struct {
		raw rawCStruct
		sd  StructDef
	}
	entries := make([]structEntry, 0, len(rawStructs))
	for i := range rawStructs {
		rs := &rawStructs[i]
		sd := StructDef{Name: rs.Name, Alias: rs.Alias}
		// Doc/label/icon anchor: above the forward typedef when one
		// exists (the public interface, where an opaque handle is
		// documented), otherwise above the struct body. The rewrite
		// writes directives at the same anchor.
		docAnchor := rs.DeclStart
		if rs.TypedefDeclStart >= 0 {
			docAnchor = rs.TypedefDeclStart
		}
		leadingDoc := collectLeadingComments(s, docAnchor, blockComments)
		if leadingDoc != "" {
			cleaned, _, icon, label, _, _, _ := extractDocDirectives(leadingDoc)
			sd.Icon = icon
			sd.Label = label
			sd.Interactive = extractInteractiveDirective(leadingDoc)
			sd.Doc = strings.TrimSpace(cleaned)
		}
		// Body field walk → props. A body-less opaque handle (body in
		// the .c) has no fields to walk.
		if rs.BodyEnd > rs.BodyStart {
			body := s[rs.BodyStart:rs.BodyEnd]
			sd.Props = extractCProps(body, limits)
			sd.StructCode = strings.TrimSpace(s[rs.DeclStart:rs.DeclEnd])
		}
		entries = append(entries, structEntry{raw: *rs, sd: sd})
	}

	// ── Phase 8: classify referenced structs as wire-types ───────────────
	// A struct is surfaced as a WIRE-TYPE when a public function
	// references it in a parameter or return type. Structs referenced
	// only by `static` helpers (or not at all) are internal
	// implementation detail and stay hidden — the same gate that
	// applies to enums and functions.
	//
	// No struct becomes an executable device anymore (decision (b)), so
	// def.Structs stays empty on the C99 path; the Go path still uses
	// Structs[]/Methods[] for real methods.
	hasPublicFuncs := len(rawFuncs) > 0
	referenced := referencedTypesInPublicAPI(rawFuncs)
	for ei := range entries {
		e := &entries[ei]
		// Match the struct against the referenced set by tag, alias OR
		// resolved name — public signatures normally use the alias
		// (`sht3x_t *`), while the struct's resolved Name is the tag.
		if hasPublicFuncs &&
			!referenced[e.raw.Tag] &&
			!referenced[e.raw.Alias] &&
			!referenced[e.raw.Name] {
			continue // internal — not surfaced
		}
		def.WireTypes = append(def.WireTypes, e.sd)
	}

	// ── Phase 9: every public function is its own device ─────────────────
	// Decision (b): there is no method classification, so EVERY public
	// function becomes a standalone device-function. funcDefFromRaw is
	// called with structName "" so it skips no receiver — a leading
	// `struct X *` parameter becomes an ordinary input port like any
	// other. C99 is the source of truth: a function has parameters and
	// a return type, and that is the whole device.
	//
	// Source order is preserved so the SPA renders devices in the
	// order they appear in the file.
	for fi := range rawFuncs {
		fn := &rawFuncs[fi]
		def.Functions = append(def.Functions, NamedFuncDef{
			Name:    fn.RawName,
			FuncDef: *funcDefFromRaw(fn, limits),
		})
	}

	// ── Phase 9.6: callback types + handler reference ports ──────────────
	// Function-pointer typedefs (`typedef void (*sht3x_alert_cb_t)(...);`)
	// are the visual model's callback types. Record them, then flag every
	// port whose type is one of them with PortDef.CallbackType — on an input
	// this marks a callback PARAMETER (a reference the maker wires from a
	// handler, e.g. `cb` in sht3x_set_alert); on the single output of a
	// `// callback:<type>.` device it marks the handler reference (already set
	// by funcDefFromRaw, so this also covers it when the typedef is visible in
	// the same source). Matching by name is the v1 rule — the C compiler
	// catches any signature divergence. The Go path produces neither callback
	// types nor handlers, so this is a no-op there.
	def.CallbackTypes = functionPointerTypedefs(stripped)
	if len(def.CallbackTypes) > 0 {
		isCallbackType := make(map[string]bool, len(def.CallbackTypes))
		for _, c := range def.CallbackTypes {
			isCallbackType[c.Name] = true
		}
		flag := func(ports []PortDef) {
			for pi := range ports {
				if ports[pi].CallbackType != "" {
					continue // already set (handler reference output)
				}
				if isCallbackType[strings.TrimSpace(ports[pi].GoType)] {
					ports[pi].CallbackType = strings.TrimSpace(ports[pi].GoType)
				}
			}
		}
		for fi := range def.Functions {
			flag(def.Functions[fi].Inputs)
			flag(def.Functions[fi].Outputs)
			// Which callback typedefs can THIS function be a handler of? Match
			// the function's RAW signature (rawFuncs is index-aligned with
			// def.Functions, built in the loop above) against each typedef. The
			// wizard offers exactly this set and disables its dropdown when it
			// is empty, so a signature-incompatible handler cannot be authored.
			raw := &rawFuncs[fi]
			var compat []string
			for _, ct := range def.CallbackTypes {
				if callbackSignatureMatch(raw.ReturnType, raw.ParamsRaw, ct) {
					compat = append(compat, ct.Name)
				}
			}
			def.Functions[fi].CompatibleCallbacks = compat
		}
	}

	// ── Phase 9.5: enum type devices ─────────────────────────────────────
	// Find every enum, keep only those referenced in a public
	// function signature (same gate as internal structs), and attach
	// the per-enumerator labels from leading comments. Enums are
	// independent of Structs[] — they live in def.Enums.
	//
	// The `referenced` set was computed in Phase 8. We check BOTH the
	// enum's tag and its typedef alias against it, because public
	// signatures normally use the alias (`display_color_t`).
	if hasPublicFuncs {
		for _, re := range findAllCEnums(stripped) {
			if !referenced[re.Tag] && !referenced[re.Alias] && !referenced[re.Name] {
				continue // internal enum — not surfaced
			}
			ed := EnumDef{
				Name:     re.Name,
				EnumCode: strings.TrimSpace(s[re.DeclStart:re.DeclEnd]),
			}
			// Enum-level icon/label/doc from the leading comment
			// block, reusing the struct directive extractor.
			if leadingDoc := collectLeadingComments(s, re.DeclStart, blockComments); leadingDoc != "" {
				cleaned, _, icon, label, _, _, _ := extractDocDirectives(leadingDoc)
				ed.Icon = icon
				ed.Label = label
				ed.Doc = strings.TrimSpace(cleaned)
			}
			// Per-enumerator labels: the `// label:…` directive lives
			// in the leading comment block immediately above each
			// enumerator (Decision 1A).
			for _, rv := range re.Values {
				evd := EnumValueDef{
					Name:       rv.Name,
					Value:      rv.Value,
					ValueIsRaw: rv.ValueIsRaw,
					RawValue:   rv.RawValue,
				}
				if lc := collectLeadingComments(s, rv.DeclStart, blockComments); lc != "" {
					_, _, _, label, _, _, _ := extractDocDirectives(lc)
					evd.Label = label
				}
				ed.Values = append(ed.Values, evd)
			}
			def.Enums = append(def.Enums, ed)
		}
	}

	// ── Phase 10: legacy single-struct mirror ────────────────────────────
	// Until the SPA fully migrates off the legacy single-struct
	// fields, mirror Structs[0] into them so older code paths
	// continue rendering.
	if len(def.Structs) > 0 {
		first := &def.Structs[0]
		def.Name = first.Name
		def.StructIcon = first.Icon
		def.StructLabel = first.Label
		def.Interactive = first.Interactive
		def.Init = first.Init
		def.Methods = first.Methods
		def.Props = first.Props
		def.StructCode = first.StructCode
		if def.Doc == "" {
			def.Doc = first.Doc
		}
	}

	return def, nil
}

// referencedTypesInPublicAPI returns the set of struct/typedef
// names that appear in the parameter types or return type of ANY
// public function. Used by Phase 8 to filter out structs that are
// only used internally (which the wizard should not surface as
// devices per §7 Slice 5).
//
// The matching is text-based: we tokenise each parameter declaration
// and return type, stripping pointer/const/volatile decorators, and
// collect every identifier-shaped token. False positives from
// macros or unusual typedefs are acceptable — they err on the side
// of EXPOSING a struct (less aggressive filtering) rather than
// hiding one the user actually needed.
func referencedTypesInPublicAPI(funcs []rawCFunc) map[string]bool {
	out := make(map[string]bool)
	collect := func(s string) {
		for _, tok := range tokeniseTypeString(s) {
			out[tok] = true
		}
	}
	for _, fn := range funcs {
		collect(fn.ReturnType)
		for _, p := range splitParams(fn.ParamsRaw) {
			_, typ := splitCFieldNameType(p.text)
			collect(typ)
		}
	}
	return out
}

// tokeniseTypeString returns the identifier-shaped tokens inside a
// C type string, dropping pointer/array decorators and known
// qualifiers. `const uint8_t* foo` → ["uint8_t"]; `struct Sensor*`
// → ["Sensor"] (the leading `struct` keyword is dropped to match
// how struct names are stored elsewhere in the parser).
func tokeniseTypeString(s string) []string {
	// Replace non-ident punctuation with spaces, then split.
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isIdentByte(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte(' ')
		}
	}
	var out []string
	for _, tok := range strings.Fields(b.String()) {
		switch tok {
		case "const", "volatile", "restrict", "register",
			"signed", "unsigned", "void", "struct", "union", "enum",
			"static", "extern", "inline", "auto":
			continue
		}
		// Skip pure integer literals (array sizes).
		if isAllDigits(tok) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// isAllDigits reports whether s consists entirely of decimal digits.
// Used by tokeniseTypeString to drop array-size literals.
func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ─── Phase 4: struct discovery (3 forms) ───────────────────────────────────────

// rawCStruct carries byte-range information about a single struct
// declaration. Phase 5 of ParseC converts each into a StructDef.
type rawCStruct struct {
	// Name is the identifier the parser settled on per the
	// tag-vs-alias rule (see file-level doc).
	Name string

	// Tag is the struct tag (`struct Tag { ... }`), "" when the
	// struct is anonymous. Kept separately from Name so Phase 8 can
	// match a public-API reference against the tag OR the alias.
	Tag string

	// Alias is the typedef alias (`typedef struct ... Alias;`), ""
	// when there is no typedef. For opaque handles whose alias is
	// declared in a separate forward typedef, this is back-filled
	// from forwardTypedefStructs.
	Alias string

	// TypedefDeclStart is the byte offset of the `typedef` keyword of a
	// SEPARATE forward typedef (`typedef struct Tag Alias;`) when one
	// exists, else -1. For an opaque handle the specialist documents
	// the handle above this forward typedef (the public interface), so
	// the doc/label/icon anchor is here, not above the body. When the
	// body is absent entirely (declared in the .c), DeclStart itself
	// points here and BodyStart==BodyEnd.
	TypedefDeclStart int

	// DeclStart is the byte offset of the first byte of the
	// declaration (the `struct` keyword for the bare form, or
	// `typedef` for typedef'd forms). Used to find the leading
	// comment block.
	DeclStart int

	// BodyStart and BodyEnd are byte offsets bracketing the
	// struct's body, EXCLUDING the braces themselves. The slice
	// `src[BodyStart:BodyEnd]` is the field list as written in the
	// original source.
	BodyStart int
	BodyEnd   int

	// DeclEnd is the byte offset just past the terminating `;`.
	// Used to compute StructCode.
	DeclEnd int
}

// findAllCStructs walks the stripped source and returns one
// rawCStruct per struct declaration. Three forms supported (see
// file-level doc).
//
// Unterminated braces produce a hard error (the rest of the source
// is structurally undecidable). Other irregularities are silently
// skipped — Slice 2 prefers to surface what it can over rejecting
// the whole file.
func findAllCStructs(stripped string) ([]rawCStruct, error) {
	var out []rawCStruct
	const tk = "typedef"

	i := 0
	for i < len(stripped) {
		// Locate the next `struct` keyword that is not part of an
		// identifier.
		if !hasWordAt(stripped, i, "struct") {
			i++
			continue
		}
		if i > 0 && isIdentByte(stripped[i-1]) {
			i++
			continue
		}
		// `struct` appearing inside a `//` line comment is prose, not a
		// declaration (e.g. a doc line "see struct Foo for details").
		// preprocessC leaves line comments verbatim for directive
		// reading, so we skip them here.
		if isInsideLineComment(stripped, i) {
			i++
			continue
		}

		// Detect optional `typedef` prefix (with whitespace between).
		structStart := i
		declStart := structStart
		isTypedef := false
		back := i - 1
		for back >= 0 && isSpace(stripped[back]) {
			back--
		}
		if back >= len(tk)-1 {
			startOfTk := back - len(tk) + 1
			if startOfTk >= 0 && stripped[startOfTk:back+1] == tk &&
				(startOfTk == 0 || !isIdentByte(stripped[startOfTk-1])) {
				isTypedef = true
				declStart = startOfTk
			}
		}

		// Skip past the `struct` keyword.
		j := structStart + len("struct")
		j = skipSpaces(stripped, j)
		if j >= len(stripped) {
			break
		}

		// Optional tag identifier.
		tag, j2 := readIdent(stripped, j)
		j = j2
		j = skipSpaces(stripped, j)

		if j >= len(stripped) {
			break
		}

		// At this point we expect `{`, `;`, or a variable-decl
		// pattern. Handle each.
		switch stripped[j] {
		case ';':
			// Forward declaration — no body.
			i = j + 1
			continue
		case '{':
			// Definition — fall through to body handling.
		default:
			// Likely `struct Tag varName;` or similar. Skip and
			// continue scanning past this `struct` keyword.
			i = j
			continue
		}

		// matchBrace to find the closing `}`.
		braceStart := j
		braceEnd, ok := matchBrace(stripped, braceStart)
		if !ok {
			return nil, &parserError{
				msg: "unterminated struct body (missing closing brace)",
			}
		}

		// For typedef forms, read the alias identifier after `}`
		// (possibly past pointer decorators).
		var alias string
		afterBrace := skipSpaces(stripped, braceEnd+1)
		if isTypedef {
			for afterBrace < len(stripped) &&
				(stripped[afterBrace] == '*' || isSpace(stripped[afterBrace])) {
				afterBrace++
			}
			alias, afterBrace = readIdent(stripped, afterBrace)
		}

		// Find the terminating `;`.
		stmtEnd, ok := findStatementEnd(stripped, afterBrace)
		if !ok {
			return nil, &parserError{
				msg: "unterminated struct declaration (missing ;)",
			}
		}

		// Resolve the name. Tag wins; alias is fallback for
		// anonymous typedef forms.
		name := tag
		if name == "" && isTypedef {
			name = alias
		}
		// Anonymous structs without a typedef alias have no usable
		// name. Slice 2 silently skips them (the wizard cannot
		// address them by name).
		if name == "" {
			i = stmtEnd + 1
			continue
		}

		out = append(out, rawCStruct{
			Name:             name,
			Tag:              tag,
			Alias:            alias,
			TypedefDeclStart: -1,
			DeclStart:        declStart,
			BodyStart:        braceStart + 1,
			BodyEnd:          braceEnd,
			DeclEnd:          stmtEnd + 1,
		})

		i = stmtEnd + 1
	}

	return out, nil
}

// forwardTypedefInfo records a forward-declaration typedef's alias and
// the byte offset of its `typedef` keyword. The offset lets the parser
// read the wire-type's doc/label/icon from the comment ABOVE the
// typedef (the public interface, where specialists document an opaque
// handle) and lets the rewrite write directives there too.
type forwardTypedefInfo struct {
	Alias     string
	DeclStart int
}

// forwardTypedefStructs scans for forward-declaration typedefs of the
// form `typedef struct TAG ALIAS;` (no body) and returns a tag→info
// map. Opaque-handle headers declare the alias separately from the
// struct body — the typedef forward-declares the tag and names the
// alias, while `struct TAG { ... }` (the body) carries no alias, and
// often lives in the .c file. findAllCStructs only recognises forms
// with a `{ }` body, so the alias (and the doc anchor) from the forward
// typedef is recovered here and matched onto the struct by tag.
func forwardTypedefStructs(stripped string) map[string]forwardTypedefInfo {
	out := make(map[string]forwardTypedefInfo)
	const kw = "typedef"
	i := 0
	for {
		rel := strings.Index(stripped[i:], kw)
		if rel < 0 {
			break
		}
		p := i + rel
		i = p + len(kw)
		// Word boundary around `typedef`.
		if p > 0 && isIdentByte(stripped[p-1]) {
			continue
		}
		if i < len(stripped) && isIdentByte(stripped[i]) {
			continue
		}
		// A `typedef` inside a `//` line comment is prose, not a
		// declaration (e.g. "produced by: typedef struct x y;").
		if isInsideLineComment(stripped, p) {
			continue
		}
		j := skipSpaces(stripped, i)
		if !strings.HasPrefix(stripped[j:], "struct") {
			continue
		}
		j += len("struct")
		if j < len(stripped) && isIdentByte(stripped[j]) {
			continue // `structXYZ`, not the keyword
		}
		j = skipSpaces(stripped, j)
		tag, j2 := readIdent(stripped, j)
		if tag == "" {
			continue
		}
		j = skipSpaces(stripped, j2)
		// A `{` here means this is a definition, not a forward typedef.
		if j < len(stripped) && stripped[j] == '{' {
			continue
		}
		alias, j3 := readIdent(stripped, j)
		if alias == "" {
			continue
		}
		j = skipSpaces(stripped, j3)
		if j < len(stripped) && stripped[j] == ';' {
			// First forward typedef for a tag wins.
			if _, seen := out[tag]; !seen {
				out[tag] = forwardTypedefInfo{Alias: alias, DeclStart: p}
			}
		}
	}
	return out
}

// functionPointerTypedefs scans for C99 function-pointer typedefs of the
// canonical shape `typedef <ret> (*<name>)(<params>);` and returns one
// CallbackTypeDef per match. These are the visual model's "callback types"
// (see BlackBoxDef.CallbackTypes): a reference to such a type is satisfied by
// wiring a handler device, not by a computed value. Other typedef forms
// (struct / enum / plain aliases) do not fit the shape and are skipped here —
// they are handled by forwardTypedefStructs, findAllCEnums, etc.
//
// The scan mirrors forwardTypedefStructs: find each `typedef` keyword at a
// word boundary and outside line comments, then match the function-pointer
// shape token by token. The return type is whatever precedes the first '(';
// a '{' or ';' before any '(' means a non-function-pointer typedef, skipped.
func functionPointerTypedefs(stripped string) []CallbackTypeDef {
	var out []CallbackTypeDef
	const kw = "typedef"
	i := 0
	for {
		rel := strings.Index(stripped[i:], kw)
		if rel < 0 {
			break
		}
		p := i + rel
		i = p + len(kw)
		// Word boundary around `typedef`.
		if p > 0 && isIdentByte(stripped[p-1]) {
			continue
		}
		if i < len(stripped) && isIdentByte(stripped[i]) {
			continue
		}
		// A `typedef` inside a `//` line comment is prose, not a declaration.
		if isInsideLineComment(stripped, p) {
			continue
		}

		// Return type runs up to the first '(' — which, for a function-pointer
		// typedef, opens the "(*name)" group. A '{' or ';' first means some
		// other typedef form.
		j := skipSpaces(stripped, i)
		retStart := j
		k := j
		for k < len(stripped) && stripped[k] != '(' && stripped[k] != '{' && stripped[k] != ';' {
			k++
		}
		if k >= len(stripped) || stripped[k] != '(' {
			continue
		}
		ret := strings.TrimSpace(stripped[retStart:k])
		if ret == "" {
			continue
		}

		// "(" then optional spaces then "*".
		k = skipSpaces(stripped, k+1)
		if k >= len(stripped) || stripped[k] != '*' {
			continue
		}
		k = skipSpaces(stripped, k+1)
		name, k2 := readIdent(stripped, k)
		if name == "" {
			continue
		}
		// ")" closing the "(*name)" group.
		k = skipSpaces(stripped, k2)
		if k >= len(stripped) || stripped[k] != ')' {
			continue
		}
		// "(" opening the parameter list.
		k = skipSpaces(stripped, k+1)
		if k >= len(stripped) || stripped[k] != '(' {
			continue
		}
		// Capture the parameter list to its matching ')'.
		depth := 0
		paramStart := k + 1
		paramEnd := -1
		for ; k < len(stripped); k++ {
			switch stripped[k] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					paramEnd = k
				}
			}
			if paramEnd >= 0 {
				break
			}
		}
		if paramEnd < 0 {
			continue
		}
		params := strings.TrimSpace(stripped[paramStart:paramEnd])
		if params == "void" {
			params = ""
		}
		// Trailing ';'.
		k = skipSpaces(stripped, paramEnd+1)
		if k >= len(stripped) || stripped[k] != ';' {
			continue
		}

		// First typedef for a name wins (mirrors forwardTypedefStructs).
		dup := false
		for _, c := range out {
			if c.Name == name {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, CallbackTypeDef{Name: name, ReturnType: ret, Params: params})
		}
	}
	return out
}

// parserError carries a structural-error message. We use a private
// type rather than fmt.Errorf so the handler can identify
// "malformed C input" vs other internal Go errors if it ever needs
// to.
type parserError struct {
	msg string
}

func (e *parserError) Error() string { return e.msg }

// ─── Phase 5b: prop extraction ─────────────────────────────────────────────────

// extractCProps walks the struct body, identifies field
// declarations, and surfaces each as a PropDef. Slice 2 changes
// (per §2.12):
//
//   - Every named field becomes a PropDef row, NOT just those with
//     `// prop:` directives. This mirrors the Go parser's behaviour
//     of surfacing exported untagged fields so the wizard can
//     promote them.
//
//   - Fields with `// prop:` directives → Untagged=false, all tag
//     fields populated.
//
//   - Fields without `// prop:` directives → Untagged=true. Label
//     defaults to the field name; the wizard's Field modal
//     prompts the user to fill in the rest.
//
//   - NativeType is set per the §5.1 list. Pointers, arrays, and
//     non-scalar types are NativeType=false; the wizard shows them
//     as inert rows for native, clickable rows for non-native (the
//     specialist's choice expressed in code).
//
// Limits: limits.MaxProps caps the per-struct prop count. Excess
// fields are silently dropped (the wizard surfaces this via the
// soft-warning channel in a future slice; for now, dropping is
// fine because hitting MaxProps means the source is well past the
// reasonable per-struct cap).
func extractCProps(body string, limits ParserLimits) []PropDef {
	var props []PropDef

	clean, _ := preprocessC(body)

	pos := 0
	for pos < len(clean) {
		// Find the next `;` at depth 0.
		semi, ok := findStatementEnd(clean, pos)
		if !ok {
			break
		}

		// The original-source chunk (with comments intact) spans
		// from `pos` to `semi`. Within it, leading // comments are
		// the field's directives; the rest is the declaration.
		chunk := body[pos:semi]
		commentLines, declLine := splitFieldChunk(chunk)
		declLine = strings.TrimSpace(declLine)
		if declLine == "" {
			pos = semi + 1
			continue
		}

		// Extract field name + type from the declaration.
		fieldName, cType := splitCFieldNameType(declLine)
		if fieldName == "" {
			pos = semi + 1
			continue
		}

		// Look for a `// prop:` directive in the leading comments.
		// When present, parse its key:"value" triplets; when
		// absent, the field surfaces as an untagged native or
		// non-native row.
		doc := strings.Join(commentLines, "\n")
		propTag := findIDSPropTag(doc)

		// Strip IDS directives from the human-readable doc text.
		cleanDoc := stripIDSPropTag(doc)
		cleanDoc, _, _, _, _, _, _ = extractDocDirectives(cleanDoc)
		cleanDoc = strings.TrimSpace(cleanDoc)

		p := PropDef{
			FieldName:  fieldName,
			GoType:     cType,
			Doc:        cleanDoc,
			NativeType: isNativeCType(cType),
		}

		if propTag != "" {
			label, def, options, connection := parseIDSPropTag(propTag)
			if label == "" {
				label = fieldName
			}
			p.Label = label
			p.Default = def
			p.Options = options
			p.Connection = connection
			p.Untagged = false
		} else {
			// Untagged exposure: matches the Go parser's behaviour
			// for an exported field without a prop tag. The wizard
			// surfaces a ⚠ when NativeType=true and offers the
			// Field modal to fill in label/default/connection.
			p.Label = fieldName
			p.Untagged = true
		}

		props = append(props, p)

		if limits.MaxProps > 0 && len(props) >= limits.MaxProps {
			break
		}
		pos = semi + 1
	}

	return props
}

// splitFieldChunk separates the leading // comments from the field
// declaration inside a `;`-terminated chunk.
//
// Slice 2 still recognises ONLY `//` for field comments. Block
// comments above fields are a Slice 6 follow-up.
func splitFieldChunk(chunk string) (commentLines []string, decl string) {
	lines := strings.Split(chunk, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "//") {
			break
		}
		commentLines = append(commentLines, strings.TrimSpace(strings.TrimPrefix(t, "//")))
		i++
	}
	decl = strings.Join(lines[i:], " ")
	return commentLines, decl
}

// splitCFieldNameType separates the field name from the type string
// in a C declaration. Examples:
//
//	const uint8_t* foo     → name=foo, type="const uint8_t*"
//	int bar[16]            → name=bar, type="int [16]"
//	struct Inner inner     → name=inner, type="struct Inner"
//	const char** tags      → name=tags, type="const char**"
//
// Returns ("", "") for shapes Slice 2 cannot parse:
//
//	void (*on_event)(int)  — function pointer (Slice 6)
//	uint8_t flags : 4      — bit-field      (Slice 6)
func splitCFieldNameType(decl string) (name, typ string) {
	d := strings.TrimSpace(decl)
	if d == "" {
		return "", ""
	}
	// Function pointers contain parentheses; skip for Slice 2.
	if strings.ContainsAny(d, "()") {
		return "", ""
	}
	// Bit-fields contain `:`; skip for Slice 2.
	if strings.Contains(d, ":") {
		return "", ""
	}
	// Strip array suffix (`[N]`) and remember it as part of the
	// type string. `int foo[16]` → name=foo, type="int [16]".
	arraySuffix := ""
	if br := strings.Index(d, "["); br >= 0 {
		arraySuffix = strings.TrimSpace(d[br:])
		d = strings.TrimSpace(d[:br])
	}
	// The name is the LAST identifier token.
	end := len(d)
	for end > 0 && isIdentByte(d[end-1]) {
		end--
	}
	if end == len(d) {
		return "", ""
	}
	name = d[end:]
	typ = strings.TrimSpace(d[:end])
	if arraySuffix != "" {
		typ = strings.TrimSpace(typ + " " + arraySuffix)
	}
	return name, typ
}

// findIDSPropTag scans a doc string for a line starting with
// `prop:` and returns it verbatim. Returns "" when absent.
func findIDSPropTag(doc string) string {
	for _, line := range strings.Split(doc, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "prop:") {
			return t
		}
	}
	return ""
}

// parseIDSPropTag breaks an IDS prop directive line into its
// label / default / options / connection components. The grammar:
//
//	prop:"Label". default:"value". options:"a,b". connection:"ROLE".
func parseIDSPropTag(line string) (label, defaultVal string, options []string, connection string) {
	i := 0
	for i < len(line) {
		colonQ := strings.Index(line[i:], `:"`)
		if colonQ < 0 {
			break
		}
		keyEnd := i + colonQ
		keyStart := keyEnd
		for keyStart > 0 && (isIdentByte(line[keyStart-1]) || line[keyStart-1] == '_') {
			keyStart--
		}
		key := line[keyStart:keyEnd]
		valStart := keyEnd + 2
		closeQ := strings.Index(line[valStart:], `"`)
		if closeQ < 0 {
			break
		}
		valEnd := valStart + closeQ
		val := line[valStart:valEnd]

		switch strings.ToLower(strings.TrimSpace(key)) {
		case "prop":
			label = val
		case "default":
			defaultVal = val
		case "options":
			for _, opt := range strings.Split(val, ",") {
				if t := strings.TrimSpace(opt); t != "" {
					options = append(options, t)
				}
			}
		case "connection":
			connection = val
		}

		i = valEnd + 1
		if i < len(line) && line[i] == '.' {
			i++
		}
	}
	return label, defaultVal, options, connection
}

// stripIDSPropTag removes any `prop:` line from the doc so the Doc
// field stores only human-readable prose.
func stripIDSPropTag(doc string) string {
	var out []string
	for _, line := range strings.Split(doc, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "prop:") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// isNativeCType reports whether the given C type string is one of
// the scalar types the wizard knows how to render an input UI for.
// Matches the Slice 1 list (see design doc §5.1). Pointers, arrays,
// and qualified types are NOT native; the wizard renders them as
// inert rows.
func isNativeCType(typ string) bool {
	t := strings.TrimSpace(typ)
	for _, q := range []string{"const ", "volatile ", "static ", "register ", "signed ", "unsigned "} {
		t = strings.TrimPrefix(t, q)
	}
	if strings.ContainsAny(t, "*[]") {
		return false
	}
	switch t {
	case "_Bool", "bool",
		"char",
		"short", "int", "long", "long long",
		"int8_t", "int16_t", "int32_t", "int64_t",
		"uint8_t", "uint16_t", "uint32_t", "uint64_t",
		"size_t", "ssize_t",
		"float", "double":
		return true
	}
	return false
}
