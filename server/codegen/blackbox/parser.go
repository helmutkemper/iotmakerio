// server/codegen/blackbox/parser.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// parser.go — Parses a Go source file into a BlackBoxDef.
//
// English:
//
//	Uses go/ast to extract the exported struct, Init() (special, optional),
//	all other exported methods (collected as Methods []NamedFuncDef),
//	prop tags, import paths, doc comments, method bodies, and manual pages.
//
//	Convention:
//	  - Exactly ONE exported struct per file.
//	  - Init() is optional. When present it has special semantics (runs first).
//	  - Any number of other methods are allowed (Run, Log, Step, Read, …).
//	  - At least one of Init or a named method must be present.
//	  - Fields with `prop:"Label" default:"val" options:"a,b,c" connection:"ROLE"` are properties.
//
//	Machine directives in doc comments (IDS tag syntax, key:value.):
//
//	  executionOrder:N  — relative execution order (positive integer).
//	  icon:name         — FontAwesome icon name (kebab-case, e.g. "gear").
//	  label:text        — Human-readable display name ([a-zA-Z0-9_\s-]+).
//
//	All three follow the same dot-terminated format and may appear on the same
//	line as each other or on separate lines. They are extracted from the doc
//	comment and stripped before storing the human-readable Doc field.
//
//	Struct-level directives appear in the type declaration doc comment:
//	  // icon:greater-than-equal. label:APDS9960.
//	  type APDS9960 struct { ... }
//
//	Method-level directives appear in the function doc comment:
//	  // executionOrder:20. icon:greater-than-equal. label:log.
//	  func (s *APDS9960) Log(...) { ... }
//
// Português:
//
//	Init() é opcional e tem semântica especial. Todos os outros métodos
//	exportados do struct são coletados em Methods []NamedFuncDef em ordem
//	de aparição no arquivo fonte. Pelo menos Init ou um método deve existir.
//
//	As diretivas icon: e label: no comentário do struct e dos métodos
//	enriquecem a visualização na IDE: ícone no cabeçalho do bloco e no
//	menu hexagonal; label como título legível do bloco.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

// Parse reads Go source code and extracts a BlackBoxDef.
//
// The limits parameter controls the structural complexity caps for this parse
// call. Pass store.GetParserLimits(userID) to apply the correct global or
// per-user limits. Pass DefaultParserLimits() in tests or contexts where the
// database is not available.
//
// Returns an error if:
//   - The source has no exported struct.
//   - Neither Init() nor any other method is present.
//   - The method count exceeds limits.MaxMethods (hard error).
//
// Returns (def, non-fatal-warning) when limits are exceeded for props/ports
// (truncation) or manual page blocks are malformed.
// The caller should log warnings and continue — the component is still usable.
func Parse(src []byte, limits ParserLimits) (*BlackBoxDef, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "blackbox.go", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	def := &BlackBoxDef{}

	// Package-level doc → device description.
	if file.Doc != nil {
		def.Doc = strings.TrimSpace(file.Doc.Text())
	}

	def.Imports = extractImports(file)

	structName, structType, structNode, structDoc := findExportedStruct(file)
	if structName == "" {
		return nil, fmt.Errorf("no exported struct found; black-box requires exactly one exported struct")
	}
	def.Name = structName
	def.Props = extractProps(structType, limits)
	def.StructCode = nodeSource(fset, src, structNode)

	// Extract struct-level visual directives (icon: and label:) from the
	// struct's own doc comment. These directives are stripped from the stored
	// doc text — they are machine metadata, not human-readable prose.
	// Note: executionOrder and menu: are not meaningful at the struct level,
	// so the extra return values are intentionally discarded.
	if structDoc != "" {
		_, _, def.StructIcon, def.StructLabel, _, _, _ = extractDocDirectives(structDoc)
		def.Interactive = extractInteractiveDirective(structDoc)
	}

	// ── Init (special, optional) ─────────────────────────────────────────────
	initMethod := findMethod(file, structName, "Init")
	if initMethod != nil {
		initDef := extractFuncDef(fset, file, initMethod, limits)
		def.Init = &initDef
	}

	// ── All other exported methods → Methods []NamedFuncDef ─────────────────
	//
	// We walk all declarations in source-file order. For each FuncDecl whose
	// receiver is the exported struct (value or pointer receiver) and whose
	// name is NOT "Init", we extract a NamedFuncDef and append it to Methods.
	//
	// Source-file order is preserved because go/ast Decls are in order of
	// appearance. This gives the specialist intuitive control: methods declared
	// first appear first in the IDE menu and run first when executionOrder is
	// equal among them.
	//
	// Safety: the loop enforces clamp(limits.MaxMethods, compiledDefaultMaxMethods). A device with more than
	// that many named methods is almost certainly not a legitimate hardware
	// driver — return a hard error rather than truncating silently.
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Recv == nil {
			continue
		}
		// Skip unexported methods (lowercase first char).
		if !ast.IsExported(funcDecl.Name.Name) {
			continue
		}
		// Skip Init — already handled above.
		if funcDecl.Name.Name == "Init" {
			continue
		}
		// Check receiver matches our struct.
		matchesStruct := false
		for _, recv := range funcDecl.Recv.List {
			if receiverTypeName(recv.Type) == structName {
				matchesStruct = true
				break
			}
		}
		if !matchesStruct {
			continue
		}

		// Enforce the method count limit before appending.
		if len(def.Methods) >= limits.MaxMethods {
			return nil, fmt.Errorf(
				"%s has more than %d exported methods (found at least %q); "+
					"reduce the number of exported methods or split into multiple devices",
				structName, limits.MaxMethods, funcDecl.Name.Name,
			)
		}

		fd := extractFuncDef(fset, file, funcDecl, limits)
		def.Methods = append(def.Methods, NamedFuncDef{
			Name:    funcDecl.Name.Name,
			FuncDef: fd,
		})
	}

	if def.Init == nil && len(def.Methods) == 0 {
		return nil, fmt.Errorf("no methods found on %s; at least one method (Init or any other exported method) is required", structName)
	}

	// ── Soft warnings for truncated props and ports ───────────────────────────
	//
	// extractProps and extractFuncDef silently truncate when limits are hit.
	// We emit soft warnings here so the specialist can see them in the UI
	// without blocking the component from being saved and used.
	//
	// Props: count the fields that are eligible to surface as props
	// in the wizard. Eligibility now matches extractProps's first-line
	// filter: exported fields with a name. Untagged exported fields
	// also count toward the cap because they too consume a row in
	// the wizard's struct card. Pre-existing behaviour counted only
	// `prop:`-tagged fields, but with the cap shared between the two
	// categories that was undercounting.
	var rawPropCount int
	if structType.Fields != nil {
		for _, f := range structType.Fields.List {
			if len(f.Names) == 0 {
				continue
			}
			if !ast.IsExported(f.Names[0].Name) {
				continue
			}
			rawPropCount++
		}
	}
	var parseWarnings []string
	if rawPropCount > clamp(limits.MaxProps, compiledDefaultMaxProps) {
		parseWarnings = append(parseWarnings, fmt.Sprintf(
			"%s: %d prop-tagged fields found but only the first %d are used (limit: %d)",
			structName, rawPropCount, clamp(limits.MaxProps, compiledDefaultMaxProps), clamp(limits.MaxProps, compiledDefaultMaxProps),
		))
	}

	// Ports: check each method for truncated inputs/outputs.
	checkPortLimits := func(methodName string, fd FuncDef, rawInputs, rawOutputs int) {
		if rawInputs > clamp(limits.MaxInputs, compiledDefaultMaxInputs) {
			parseWarnings = append(parseWarnings, fmt.Sprintf(
				"%s.%s: %d input ports found but only the first %d are used (limit: %d)",
				structName, methodName, rawInputs, clamp(limits.MaxInputs, compiledDefaultMaxInputs), clamp(limits.MaxInputs, compiledDefaultMaxInputs),
			))
		}
		if rawOutputs > clamp(limits.MaxOutputs, compiledDefaultMaxOutputs) {
			parseWarnings = append(parseWarnings, fmt.Sprintf(
				"%s.%s: %d output ports found but only the first %d are used (limit: %d)",
				structName, methodName, rawOutputs, clamp(limits.MaxOutputs, compiledDefaultMaxOutputs), clamp(limits.MaxOutputs, compiledDefaultMaxOutputs),
			))
		}
	}

	// Ports: warn for every INPUT port that is missing the connection: tag.
	//
	// Outputs (regular and error) are NEVER warned for missing connection:
	// the slice-7 rule treats outputs as always-optional. There is no
	// semantic for "this output must be wired" — the method computes the
	// value either way. See completion.go::portIncomplete for the same
	// rule on the wizard's ⚠ side.
	//
	// Missing connection: on an INPUT is not a blocking error — the
	// component still works. But the specialist should declare whether
	// each input is mandatory or optional so the IDE can show the
	// correct indicator (◉/◎) and warn makers when a required wire is
	// missing.
	//
	// Format: "ServerConfig.go: ServerConfig.Init(port int): missing connection: tag"
	// The filename "ServerConfig.go" is conventional (one struct per file).
	filename := structName + ".go"
	checkMissingConn := func(methodName string, fd FuncDef) {
		for _, p := range fd.Inputs {
			if p.MissingConn {
				parseWarnings = append(parseWarnings, fmt.Sprintf(
					"%s: %s.%s input %q (%s): missing connection: tag — add // connection: mandatory. or // connection: optional. above the parameter",
					filename, structName, methodName, p.Name, p.GoType,
				))
			}
		}
		// Outputs: no check. Slice-7 made them connection-free by design.
	}
	if def.Init != nil {
		initDecl := findMethod(file, structName, "Init")
		if initDecl != nil {
			rawIn, rawOut := countRawPorts(initDecl)
			checkPortLimits("Init", *def.Init, rawIn, rawOut)
		}
		checkMissingConn("Init", *def.Init)
	}
	for _, m := range def.Methods {
		methodDecl := findMethod(file, structName, m.Name)
		if methodDecl != nil {
			rawIn, rawOut := countRawPorts(methodDecl)
			checkPortLimits(m.Name, m.FuncDef, rawIn, rawOut)
		}
		checkMissingConn(m.Name, m.FuncDef)
	}

	def.MethodsCode = extractAllMethods(fset, src, file, structName)

	// Extract manual pages from /* */ comment blocks.
	pages, pageWarnings := parseManualBlocks(string(src), def)
	def.ManualPages = pages

	// Merge all soft warnings — prop/port truncations + manual page issues.
	allWarnings := append(parseWarnings, pageWarnings...)
	if len(allWarnings) > 0 {
		return def, fmt.Errorf("parse warnings: %s", joinWarnings(allWarnings))
	}

	return def, nil
}

// =====================================================================
//  Import extraction
// =====================================================================

func extractImports(file *ast.File) []string {
	var imports []string
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		imports = append(imports, path)
	}
	return imports
}

// =====================================================================
//  Struct finding
// =====================================================================

// findExportedStruct returns the first exported struct in the file, along
// with its AST StructType, the containing GenDecl node, and the raw doc
// comment text (used to extract struct-level icon: and label: directives).
func findExportedStruct(file *ast.File) (name string, st *ast.StructType, node ast.Node, doc string) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			n := typeSpec.Name.Name
			if ast.IsExported(n) {
				// Doc comment priority: GenDecl.Doc, then TypeSpec.Doc.
				// For single-spec type declarations (the normal case), the
				// comment directly above `type Foo struct` is on genDecl.Doc.
				var docText string
				if genDecl.Doc != nil {
					docText = strings.TrimSpace(genDecl.Doc.Text())
				} else if typeSpec.Doc != nil {
					docText = strings.TrimSpace(typeSpec.Doc.Text())
				}
				return n, structType, genDecl, docText
			}
		}
	}
	return "", nil, nil, ""
}

// =====================================================================
//  Prop tag extraction
// =====================================================================

// extractProps walks the struct fields and emits a PropDef for every
// field that the wizard should display. Two categories qualify:
//
//  1. Fields with a `prop:"Label"` struct tag — the explicit case.
//     Untagged=false, all directive-derived fields populated from
//     the tag (Label, Default, Options, Connection). The type can
//     be anything the specialist asks for (native or not) because
//     the tag is an opt-in promise that the field makes sense as
//     a property.
//
//  2. Exported NATIVE fields without a `prop:"..."` tag — the
//     discovery case. The wizard surfaces these so the user
//     (specialist) can either promote them to props or accept them
//     as internal structure. Untagged=true. The wizard renders the
//     row with a ⚠ and saving the modal adds the missing tag.
//
//     Discovery is gated on `isNativeGoType(goType)`. A non-native
//     untagged field — e.g. `I2C *machine.I2C`, `Buf []byte`,
//     `M map[string]int` — is internal state populated by wires or
//     methods, never by the inspect form (you cannot type a pointer
//     into a text input). Skipping those silently keeps Properties
//     clean of blank rows that would otherwise sit above the real
//     props.
//
// Unexported fields are filtered out entirely. Go convention treats
// lowercase fields as internal state, and the wizard respects that.
//
// The MaxProps cap counts BOTH categories together — going over the
// cap means the device is too complex for the wizard to render
// usefully, regardless of which category is bloating it.
//
// Field doc comments (godoc) are captured for the tagged case via
// field.Doc (the AST attaches them when the comment immediately
// precedes the field) and stripped of any IDS machine directives
// before being assigned to PropDef.Doc.
func extractProps(structType *ast.StructType, limits ParserLimits) []PropDef {
	var props []PropDef
	if structType.Fields == nil {
		return props
	}
	cap := clamp(limits.MaxProps, compiledDefaultMaxProps)
	for _, field := range structType.Fields.List {
		if len(props) >= cap {
			break
		}
		// Skip embedded/anonymous fields — they have no name, so no
		// path can ever address them. The pre-existing behaviour.
		if len(field.Names) == 0 {
			continue
		}
		fieldName := field.Names[0].Name

		// Skip unexported fields by Go convention. ast.IsExported
		// returns true when the first letter is uppercase. Internal
		// state (i2c handles, mutex fields, cached buffers) lives
		// here and the wizard pretends it doesn't exist.
		if !ast.IsExported(fieldName) {
			continue
		}

		goType := typeString(field.Type)
		isNative := isNativeGoType(goType)

		// Capture leading godoc, stripped of any IDS directives. When
		// the field has no leading comment field.Doc is nil, which
		// stringifies to "" — fine.
		fieldDoc := ""
		if field.Doc != nil {
			fieldDoc = strings.TrimSpace(stripDocDirectives(field.Doc.Text()))
		}

		// Untagged path: emit a discovery PropDef when (and only when)
		// the type is native.
		//
		// Why the native gate: discovery exists to nudge the specialist
		// when they forgot to tag a configurable property
		// (`Gain byte` → ⚠ in the wizard). It does not — and cannot —
		// apply to non-native types: a *machine.I2C, []byte, or
		// map[string]int has no sensible representation in a text
		// input. Those fields are internal state populated by wires
		// (Init writes s.I2C = i2c) or methods, not by the user
		// editing the inspect form.
		//
		// Before this gate, untagged exported non-native fields
		// rendered as a blank row above the real props in the
		// Properties form (the empty Label produced an empty
		// `<label>` cell). The screenshot in the wizard bug report
		// at /docs/tasks/CLAUDE_KNOWN_ISSUES.md captured the
		// classic case: APDS9960 has `I2C *machine.I2C` and a blank
		// row appeared above Gain/ATime.
		//
		// If a future specialist genuinely wants to expose a
		// non-native prop (e.g. a struct config blob), they must add
		// `prop:"..."` explicitly — the tagged path below honours any
		// type the specialist asks for, native or not, because the
		// `prop:` tag is an opt-in promise that the type makes sense
		// in this context.
		if field.Tag == nil {
			if !isNative {
				continue
			}
			props = append(props, PropDef{
				FieldName:  fieldName,
				GoType:     goType,
				Label:      "",
				Doc:        fieldDoc,
				Untagged:   true,
				NativeType: isNative,
			})
			continue
		}

		rawTag := strings.Trim(field.Tag.Value, "`")
		tag := reflect.StructTag(rawTag)

		propLabel := tag.Get("prop")
		if propLabel == "" {
			// The field has SOME tag but not `prop:"..."`. Treat as
			// untagged for wizard purposes — the spec only cares
			// about prop. (A field with `json:"x"` is still
			// untagged from IDS's point of view.)
			//
			// Same native gate as the no-tag branch above: a non-
			// native field with a non-prop tag (e.g. an i2c pointer
			// carrying `json:"i2c"` for serialisation) is internal
			// state, not a property to surface.
			if !isNative {
				continue
			}
			props = append(props, PropDef{
				FieldName:  fieldName,
				GoType:     goType,
				Label:      "",
				Doc:        fieldDoc,
				Untagged:   true,
				NativeType: isNative,
			})
			continue
		}

		// Tagged path: full PropDef built from the tag.
		//
		// Container/KeyType/ValueType are populated for composite
		// types (map[K]V, []T) here so the WASM client does not
		// have to re-parse the goType string. The renderer only
		// uses these when Slice 2.2 / 2.4 land; until then they
		// travel on the wire ignored. Native scalars produce an
		// empty Container, which the renderer treats exactly as
		// before this slice.
		container, keyType, valueType, nativeKey, nativeValue := analyseGoType(goType)

		prop := PropDef{
			FieldName:   fieldName,
			GoType:      goType,
			Label:       propLabel,
			Default:     tag.Get("default"),
			Connection:  tag.Get("connection"),
			Doc:         fieldDoc,
			Untagged:    false,
			NativeType:  isNative,
			Container:   container,
			KeyType:     keyType,
			ValueType:   valueType,
			NativeKey:   nativeKey,
			NativeValue: nativeValue,
		}

		if opts := tag.Get("options"); opts != "" {
			prop.Options = strings.Split(opts, ",")
			for i := range prop.Options {
				prop.Options[i] = strings.TrimSpace(prop.Options[i])
			}
		}

		props = append(props, prop)
	}
	return props
}

// isNativeGoType is a private wrapper around completion.IsNativePropType
// to avoid an import cycle. extractProps lives in parser.go (package
// blackbox); IsNativePropType lives in completion.go (same package),
// so the wrapper is a one-liner — but giving it a local name makes
// the intent obvious at the call site without crossing files
// repeatedly.
func isNativeGoType(goType string) bool {
	return IsNativePropType(goType)
}

// stripDocDirectives strips IDS machine directives (label:, icon:,
// connection:, menu:, order:) from a doc comment, returning just
// the human-readable prose. Reused by extractProps to populate
// PropDef.Doc with clean text.
//
// Implementation note: this duplicates the directive list used by
// extractDocDirectives elsewhere in the file, but on a per-line
// "drop if matches" basis rather than the full directive-extraction
// pass. We don't need the directive values here, only the cleaned
// remainder.
func stripDocDirectives(doc string) string {
	if doc == "" {
		return ""
	}
	lines := strings.Split(doc, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if isIDSDirectiveLine(trimmed) {
			continue
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

// isIDSDirectiveLine returns true for strings that look like
// "directive:value." — the IDS machine-directive shape.
func isIDSDirectiveLine(s string) bool {
	// Cheapest test first — every directive ends with a period.
	if !strings.HasSuffix(s, ".") {
		return false
	}
	// Match `name:rest.` where name is a known directive prefix.
	for _, prefix := range []string{"label:", "icon:", "connection:", "menu:", "order:", "interactive:", "default:", "options:", "unit:", "format:"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// =====================================================================
//  Method finding and signature extraction
// =====================================================================

// findMethod finds a specific named method on structName. Used only for Init.
func findMethod(file *ast.File, structName, methodName string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Recv == nil || funcDecl.Name.Name != methodName {
			continue
		}
		for _, recv := range funcDecl.Recv.List {
			if receiverTypeName(recv.Type) == structName {
				return funcDecl
			}
		}
	}
	return nil
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// countRawPorts counts the raw number of input parameters and return values
// in a function declaration without applying any limits. Used to detect
// truncation and emit soft warnings.
func countRawPorts(funcDecl *ast.FuncDecl) (inputs, outputs int) {
	if funcDecl.Type.Params != nil {
		for _, field := range funcDecl.Type.Params.List {
			n := len(field.Names)
			if n == 0 {
				n = 1 // unnamed parameter counts as one port
			}
			inputs += n
		}
	}
	if funcDecl.Type.Results != nil {
		for _, field := range funcDecl.Type.Results.List {
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			outputs += n
		}
	}
	return inputs, outputs
}

// extractFuncDef extracts port definitions and all machine directives from a
// method's doc comment.
//
// Machine directives extracted (and stripped from the human-readable Doc):
//   - executionOrder:N  — relative execution order (positive integer).
//   - icon:name         — FontAwesome icon name (kebab-case).
//   - label:text        — Human-readable display name.
//   - menu:col,row      — Explicit hex-menu position offset from Back center.
//
// All directives follow the IDS tag format (key:value.) and may appear
// on the same line as each other or on separate lines.
//
// Port metadata is extracted from the comment block directly above each
// parameter or return value (field.Doc) or inline at the end of the line
// (field.Comment). Both locations use the same IDS tag syntax:
//
//	// doc: Human-readable description.  connection: mandatory.  unit: lux.
//
// extractFuncDef extracts port definitions and all machine directives from a
// method's doc comment.
//
// Machine directives extracted (and stripped from the human-readable Doc):
//   - executionOrder:N  — relative execution order (positive integer).
//   - icon:name         — FontAwesome icon name (kebab-case).
//   - label:text        — Human-readable display name.
//   - menu:col,row      — Explicit hex-menu position offset from Back center.
//
// Port metadata — two supported formats, both fully valid Go doc comments:
//
// Format 1 — inline field comment (preferred for new code):
//
//	func (s *Sensor) Run(
//	    // doc: I2C bus. connection: mandatory. unit: i2c_bus.
//	    i2c *machine.I2C,
//	) (
//	    // doc: Lux value. connection: optional. unit: lux.
//	    lux uint16, err error,
//	)
//
// Format 2 — Params/Returns sections in the method doc comment (standard godoc
// style, compatible with go doc and pkg.go.dev):
//
//	// Run reads sensor data.
//	//
//	// Params
//	//
//	//	i2c: I2C bus.  connection:mandatory.  unit:i2c_bus.
//	//
//	// Returns
//	//
//	//	lux: lux value.  connection:optional.  unit:lux.
//	//	err: error.      connection:optional.
//	func (s *Sensor) Run(i2c *machine.I2C) (lux uint16, err error)
//
// The two formats can coexist in the same file. Format 1 (field.Doc) takes
// priority for a given port; Format 2 is used as fallback when no field
// comment is present.
//
// Entries in Params/Returns are matched by POSITION (declaration order), not
// by name. The name before the colon is for human readability only and is not
// used for matching. This means the format works even for unnamed parameters.
func extractFuncDef(fset *token.FileSet, file *ast.File, funcDecl *ast.FuncDecl, limits ParserLimits) FuncDef {
	def := FuncDef{}

	// Parse machine directives from the method doc comment.
	// Also extract any Params/Returns sections for use as positional fallback.
	var paramsDoc []portMeta  // positional: index = parameter position
	var returnsDoc []portMeta // positional: index = return value position

	if funcDecl.Doc != nil {
		rawDoc := strings.TrimSpace(funcDecl.Doc.Text())
		def.Doc, def.ExecutionOrder, def.Icon, def.Label,
			def.MenuCol, def.MenuRow, def.MenuPosSet = extractDocDirectives(rawDoc)
		paramsDoc, returnsDoc = extractParamsReturnsSections(def.Doc)
	}

	if funcDecl.Type.Params != nil {
		inputIdx := 0 // tracks the flat port position across all fields
		for _, field := range funcDecl.Type.Params.List {
			if len(def.Inputs) >= clamp(limits.MaxInputs, compiledDefaultMaxInputs) {
				break
			}
			goType := typeString(field.Type)

			// Format 1: inline comment on this field (field.Doc / field.Comment).
			// funcDecl is passed so the helper can reject the function's
			// own godoc when the signature is single-line — see the long
			// comment on extractPortMeta.
			inlineMeta := extractPortMeta(field, file, fset, funcDecl)

			names := field.Names
			if len(names) == 0 {
				// Unnamed parameter — derive a display name from the type.
				meta := inlineMeta
				if inputIdx < len(paramsDoc) {
					meta = mergePortMeta(meta, paramsDoc[inputIdx])
				}
				p := PortDef{
					Name:   strings.ToLower(baseTypeName(goType)),
					GoType: goType,
				}
				applyPortMeta(&p, meta)
				def.Inputs = append(def.Inputs, p)
				inputIdx++
				continue
			}

			for _, name := range names {
				if len(def.Inputs) >= clamp(limits.MaxInputs, compiledDefaultMaxInputs) {
					break
				}
				// Use inline meta if present; fall back to Params section by position.
				meta := inlineMeta
				if inputIdx < len(paramsDoc) {
					meta = mergePortMeta(meta, paramsDoc[inputIdx])
				}
				p := PortDef{Name: name.Name, GoType: goType}
				applyPortMeta(&p, meta)
				def.Inputs = append(def.Inputs, p)
				inputIdx++
			}
		}
	}

	if funcDecl.Type.Results != nil {
		unnamedIdx := 0
		outputIdx := 0 // tracks the flat port position across all result fields
		for _, field := range funcDecl.Type.Results.List {
			if len(def.Outputs) >= clamp(limits.MaxOutputs, compiledDefaultMaxOutputs) {
				break
			}
			goType := typeString(field.Type)
			isErr := goType == "error"

			// Same funcDecl pass-through reasoning as the input loop above.
			inlineMeta := extractPortMeta(field, file, fset, funcDecl)

			if len(field.Names) > 0 {
				for _, name := range field.Names {
					if len(def.Outputs) >= clamp(limits.MaxOutputs, compiledDefaultMaxOutputs) {
						break
					}
					meta := inlineMeta
					if outputIdx < len(returnsDoc) {
						meta = mergePortMeta(meta, returnsDoc[outputIdx])
					}
					p := PortDef{Name: name.Name, GoType: goType, IsError: isErr}
					applyPortMeta(&p, meta)
					def.Outputs = append(def.Outputs, p)
					outputIdx++
				}
			} else {
				portName := "out"
				if isErr {
					portName = "err"
				} else {
					unnamedIdx++
					portName = fmt.Sprintf("out%d", unnamedIdx)
				}
				meta := inlineMeta
				if outputIdx < len(returnsDoc) {
					meta = mergePortMeta(meta, returnsDoc[outputIdx])
				}
				p := PortDef{Name: portName, GoType: goType, IsError: isErr}
				applyPortMeta(&p, meta)
				def.Outputs = append(def.Outputs, p)
				outputIdx++
			}
		}
	}

	return def
}

// extractParamsReturnsSections parses the Params / Returns sections of a
// function's doc comment and returns two positional slices of portMeta,
// one per parameter and one per return value.
//
// Why whitespace-agnostic detection:
//
//	go/ast strips the `//` prefix AND the first space from every doc-comment
//	line before delivering the text. After strip, lines that the human wrote
//	as `//   i2c: I2C bus.  connection:mandatory.` arrive here as
//	`  i2c: I2C bus.  connection:mandatory.` (2 leading spaces), while other
//	formatters produce tab indentation or no indentation at all. We do NOT
//	rely on leading whitespace — we decide line role purely by content:
//
//	  1. Headers: a line whose trimmed+lowercased content is exactly
//	     "params" / "parameters" / "returns" / "return" opens that section.
//	  2. Entries: once in a section, any line containing ":" before the
//	     first space is a list entry. Prose sentences never start with
//	     a single identifier followed by ":" before a space.
//	  3. Anything else (blank line, prose without a leading label) is
//	     skipped but does NOT close the section — real docs contain stray
//	     blank lines between entries.
//
// This matches the documented flag format "key:value." (key + colon +
// value + period), with multiple flags allowed on the same line.
//
// Português:
//
//	Extrai as seções Params/Returns dos comentários da função. Independente
//	de indentação — go/ast já normaliza o whitespace. Detecta cabeçalhos
//	pelo conteúdo exato (params/returns) e entradas pela presença de um
//	rótulo `nome:` no início. Formato dos flags: chave:valor.
func extractParamsReturnsSections(doc string) (params []portMeta, returns []portMeta) {
	const (
		sNone    = 0
		sParams  = 1
		sReturns = 2
	)
	section := sNone

	for _, raw := range strings.Split(doc, "\n") {
		line := strings.TrimSpace(raw)

		// Empty line — stays in current section (docs commonly have blank
		// separators between entries).
		if line == "" {
			continue
		}

		// Header detection — exact, lowercased, single-word line.
		// Must not contain any ':' so "returns: foo" (a malformed entry)
		// doesn't get eaten as a header.
		lower := strings.ToLower(line)
		if !strings.Contains(line, ":") {
			switch lower {
			case "params", "parameters":
				section = sParams
				continue
			case "returns", "return":
				section = sReturns
				continue
			default:
				// Prose line outside any recognizable role. Leave section
				// as-is and skip — we don't want a stray prose paragraph
				// inside "Params" to silently close the section.
				continue
			}
		}

		// If we're not inside a Params/Returns section, colon-bearing lines
		// (like field comments elsewhere in the doc) are not our concern.
		if section == sNone {
			continue
		}

		// List entry detection: the token before the first colon must be a
		// bare identifier — letters, digits, or underscores, nothing else.
		// This distinguishes entries from prose that happens to contain a
		// colon.
		//
		// Accepted examples:
		//   i2c: I2C bus.  connection:mandatory.          ← portname entry
		//   connection:mandatory.                         ← bare IDS flag
		//   clear: total light.  range:0..65535.          ← portname entry
		//
		// Rejected examples:
		//   "This function does X."                       ← no colon
		//   "e.g.: a colon inside prose"                  ← "e.g" has a dot
		//   "See section: foo"                            ← space before colon
		colonIdx := strings.Index(line, ":")
		if colonIdx <= 0 {
			continue
		}
		head := line[:colonIdx]
		if !isIdentifier(head) {
			continue
		}

		// Strip the leading "portname:" label — it exists for human readability
		// only. After stripping, we parse the rest as a normal IDS tag string.
		//
		// Guard: do NOT strip the label when it is itself a known IDS tag key
		// (e.g. "connection:mandatory." must not become "mandatory." — the tag
		// would be lost). The `head` variable already holds the label.
		rest := line
		label := strings.ToLower(head)
		isIDSTag := label == "doc" || label == "connection" ||
			label == "range" || label == "rangemin" || label == "rangemax" ||
			label == "unit" || label == "encoding" || label == "default" ||
			label == "bits"
		if !isIDSTag {
			rest = strings.TrimSpace(line[colonIdx+1:])
		}

		meta := parsePortMetaString(rest)

		switch section {
		case sParams:
			params = append(params, meta)
		case sReturns:
			returns = append(returns, meta)
		}
	}

	return params, returns
}

// isIdentifier reports whether s is a non-empty run of characters drawn
// from the identifier alphabet: ASCII letters, digits, and underscore.
// Used by extractParamsReturnsSections to decide whether the token
// before a colon looks like a port name or a flag key (accepted) versus
// prose (rejected).
//
// Não exige Unicode — nomes de portas em Go BlackBox são sempre ASCII.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

// portMeta holds the IDS tags extracted from a parameter's comment block.
// It is a temporary struct used only inside extractPortMeta / applyPortMeta
// to avoid passing too many return values.
type portMeta struct {
	doc         string
	label       string // label: directive — wizard-set display name
	connection  string // "mandatory" | "optional" | "" (absent)
	missingConn bool   // true when the connection: tag was entirely absent
	rangeVal    string
	rangeMin    string
	rangeMax    string
	unit        string
	encoding    string
	defaultVal  string
	bits        string
}

// extractPortMeta reads the IDS comment tags from an ast.Field's Doc and
// Comment groups. Both sources use the same tag syntax so they are merged.
//
// IDS port tags (case-insensitive, dot-terminated):
//
//	doc: text           — human-readable description (prose, no dot terminator needed)
//	label: text         — wizard-set human-readable port name
//	connection: mandatory | optional
//	range: min..max     — e.g. "0..255" or "0.0..1.0"
//	rangeMin: N
//	rangeMax: N
//	unit: label         — physical unit label
//	encoding: label     — data encoding label
//	default: value      — unwired default
//	bits: N             — significant bit count
//
// Tags are extracted from Go field-level comment groups:
//   - field.Doc     — comment block (// lines) directly above the parameter
//   - field.Comment — inline comment (// at end of the same line)
//
// Important: Go's parser does NOT populate field.Doc for fields nested
// inside parameter or result lists (parser.ParseComments leaves those
// in file.Comments as floating groups). When `file` and `fset` are
// non-nil and field.Doc is nil, we fall back to scanning file.Comments
// for the group whose last line sits directly above the field. This
// keeps round-tripping intact: a port whose godoc was set by the
// rewrite engine reads back the same way on the next parse.
//
// funcDecl is the enclosing function declaration. It is used to reject
// false matches where the function's own godoc happens to sit one line
// above the parameter list because the signature is on a single line:
//
//	// Run reads the four RGBC colour channels.            ← godoc
//	// ...
//	//	red:   red channel                                  ← godoc continues
//	func (s *X) Run() (clear, red, green, blue uint16) {    ← field line
//
// Without the funcDecl guard, the godoc above is mis-identified as the
// "leading comment" of the result field on the next line, and every
// port in that field inherits the entire method godoc as its own doc.
// Pass nil to disable the guard (used by callers that are scanning
// outside of a function context).
func extractPortMeta(field *ast.Field, file *ast.File, fset *token.FileSet, funcDecl *ast.FuncDecl) portMeta {
	var raw []string
	docGroup := field.Doc
	if docGroup == nil && file != nil && fset != nil {
		docGroup = findLeadingPortCommentInParser(file, fset, field, funcDecl)
	}
	if docGroup != nil {
		raw = append(raw, strings.TrimSpace(docGroup.Text()))
	}
	if field.Comment != nil {
		raw = append(raw, strings.TrimSpace(field.Comment.Text()))
	}

	combined := strings.Join(raw, " ")
	if combined == "" {
		return portMeta{missingConn: true}
	}

	return parsePortMetaString(combined)
}

// findLeadingPortCommentInParser is the parser-side mirror of
// findLeadingPortComment in rewrite.go. We don't share the helper
// because the rewrite package does not import the parser file
// (and pulling them into a shared util would scatter the logic).
// Both implementations follow the same rule: the comment group's
// last line ends on the line directly above the field's first line,
// AND the comment group is not the function's own godoc.
//
// The funcDecl guard exists to reject the case where a single-line
// function signature places its parameter list on the line directly
// after the method's godoc — see the long comment on extractPortMeta
// for the failure mode this rules out. Pass nil to disable the
// guard.
func findLeadingPortCommentInParser(file *ast.File, fset *token.FileSet, field *ast.Field, funcDecl *ast.FuncDecl) *ast.CommentGroup {
	fieldLine := fset.Position(field.Pos()).Line

	// When the enclosing function is known, any comment that ends at
	// or before the function's own first line cannot be a port's
	// leading comment — it is the method godoc (or a comment on
	// some unrelated earlier declaration). Reject everything in
	// that range up front.
	var rejectAtOrBeforeLine int
	if funcDecl != nil {
		rejectAtOrBeforeLine = fset.Position(funcDecl.Pos()).Line
	}

	var best *ast.CommentGroup
	for _, cg := range file.Comments {
		if cg.End() > field.Pos() {
			continue
		}
		// Is this the function's own godoc? It would end on the
		// line immediately above the func keyword. When the
		// signature is single-line, that's also one line above
		// the field — exactly the false-positive we need to
		// suppress.
		if rejectAtOrBeforeLine > 0 {
			if fset.Position(cg.End()).Line < rejectAtOrBeforeLine {
				continue
			}
		}
		if fset.Position(cg.End()).Line == fieldLine-1 {
			best = cg
		}
	}
	return best
}

// parsePortMetaString parses a raw comment string into a portMeta struct.
// It is extracted as a pure function so it can be tested without an AST.
//
// The comment may contain multiple tags separated by dots or newlines.
// Unrecognised content is accumulated into the doc field.
func parsePortMetaString(raw string) portMeta {
	var m portMeta
	var docParts []string
	connectionSeen := false

	// Normalise: replace newlines with spaces, then split on ".".
	// Each segment is either a "key: value" tag or prose text.
	normalised := strings.ReplaceAll(raw, "\n", " ")
	for _, seg := range strings.Split(normalised, ".") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		lower := strings.ToLower(seg)

		switch {
		case strings.HasPrefix(lower, "doc:"):
			val := strings.TrimSpace(seg[len("doc:"):])
			if val != "" {
				docParts = append(docParts, val)
			}
		case strings.HasPrefix(lower, "label:"):
			// Wizard-set human-readable port name. The rewrite engine
			// writes this with `setPortConnection { label: "..." }`.
			// Parsing it here closes the loop so the modal can read
			// the same value back on the next parse.
			m.label = strings.TrimSpace(seg[len("label:"):])
		case strings.HasPrefix(lower, "connection:"):
			connectionSeen = true
			val := strings.TrimSpace(strings.ToLower(seg[len("connection:"):]))
			if val == "mandatory" {
				m.connection = "mandatory"
			} else {
				m.connection = "optional"
			}
		case strings.HasPrefix(lower, "range:"):
			m.rangeVal = strings.TrimSpace(seg[len("range:"):])
		case strings.HasPrefix(lower, "rangemin:"):
			m.rangeMin = strings.TrimSpace(seg[len("rangemin:"):])
		case strings.HasPrefix(lower, "rangemax:"):
			m.rangeMax = strings.TrimSpace(seg[len("rangemax:"):])
		case strings.HasPrefix(lower, "unit:"):
			m.unit = strings.TrimSpace(seg[len("unit:"):])
		case strings.HasPrefix(lower, "encoding:"):
			m.encoding = strings.TrimSpace(seg[len("encoding:"):])
		case strings.HasPrefix(lower, "default:"):
			m.defaultVal = strings.TrimSpace(seg[len("default:"):])
		case strings.HasPrefix(lower, "bits:"):
			m.bits = strings.TrimSpace(seg[len("bits:"):])
		default:
			// Prose — add to doc if non-trivial.
			if seg != "" {
				docParts = append(docParts, seg)
			}
		}
	}

	m.doc = strings.TrimSpace(strings.Join(docParts, ". "))

	// MissingConn is true only when the connection: tag was completely absent.
	// A port with connection:optional does NOT set MissingConn.
	if !connectionSeen {
		m.missingConn = true
		m.connection = "optional"
	}

	return m
}

// mergePortMeta combines two portMeta values, preferring `primary`
// for any field it has set and falling back to `fallback` otherwise.
// Used to layer inline-comment metadata over the positional
// Params/Returns sections in the method's godoc — both sources can
// supply data for the same port, and an empty inline value should
// not erase a populated fallback (and vice versa).
//
// "Set" means non-zero string for string fields; for connection
// specifically we look at `connectionSeen` style — we treat
// fallback's connection as authoritative only when the primary did
// not see a connection: tag at all (primary.missingConn == true).
func mergePortMeta(primary, fallback portMeta) portMeta {
	out := primary
	if out.doc == "" {
		out.doc = fallback.doc
	}
	if out.label == "" {
		out.label = fallback.label
	}
	// Connection: only inherit from fallback when primary was silent.
	// "Silent" = primary.missingConn == true (the inline comment
	// block had no connection: directive). When primary did declare
	// a connection (mandatory or optional), it wins.
	if primary.missingConn && !fallback.missingConn {
		out.connection = fallback.connection
		out.missingConn = false
	}
	if out.rangeVal == "" {
		out.rangeVal = fallback.rangeVal
	}
	if out.rangeMin == "" {
		out.rangeMin = fallback.rangeMin
	}
	if out.rangeMax == "" {
		out.rangeMax = fallback.rangeMax
	}
	if out.unit == "" {
		out.unit = fallback.unit
	}
	if out.encoding == "" {
		out.encoding = fallback.encoding
	}
	if out.defaultVal == "" {
		out.defaultVal = fallback.defaultVal
	}
	if out.bits == "" {
		out.bits = fallback.bits
	}
	return out
}

// applyPortMeta copies the fields from a portMeta into a PortDef.
func applyPortMeta(p *PortDef, m portMeta) {
	p.Doc = m.doc
	p.Label = m.label
	p.Connection = m.connection
	p.MissingConn = m.missingConn
	p.Range = m.rangeVal
	p.RangeMin = m.rangeMin
	p.RangeMax = m.rangeMax
	p.Unit = m.unit
	p.Encoding = m.encoding
	p.Default = m.defaultVal
	p.Bits = m.bits
}

// =====================================================================
//  Machine directive extraction
// =====================================================================

// extractDocDirectives scans the doc comment for all machine directives and
// returns a clean human-readable doc string with those directives removed.
//
// Extracted directives:
//   - executionOrder:N  — positive integer (0 = not set).
//   - icon:name         — FontAwesome icon name (kebab-case).
//   - label:text        — Human-readable display name.
//   - menu:col,row      — Signed integer pair for explicit hex-menu position.
//
// The function processes the doc comment line by line. A line that contains
// ONLY machine directives (no human-readable prose after tag extraction) is
// dropped entirely from the clean doc output.
//
// Directives may appear on the same line:
//
//	// executionOrder:20. icon:greater-than-equal. label:log. menu:-1,-1.
//
// Or on separate lines:
//
//	// icon:greater-than-equal.
//	// menu:-1,-1.
//
// Canonical format (key:value.):
//
//	executionOrder:10    — preferred (camelCase, no spaces)
//	execution order: 10 — deprecated but still accepted (backward compat)
//
// Português: Extrai diretivas de máquina do comentário de doc. Linhas que
// contenham apenas diretivas são removidas do texto legível.
// menu:col,row. permite ao especialista fixar a posição do item no menu.
func extractDocDirectives(doc string) (cleanDoc string, order int, icon, label string, menuCol, menuRow int, menuPosSet bool) {
	var kept []string

	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)

		// Detect whether this line contains any machine directive.
		// We use a case-insensitive check on the lowercased line.
		lower := strings.ToLower(trimmed)

		containsDirective := strings.Contains(lower, "executionorder:") ||
			strings.Contains(lower, "execution order:") ||
			strings.Contains(lower, "icon:") ||
			strings.Contains(lower, "label:") ||
			strings.Contains(lower, "menu:")

		if !containsDirective {
			kept = append(kept, line)
			continue
		}

		// Split the line on "." to extract individual tag segments.
		// Each segment may be "key:value" or prose text.
		var proseSegments []string

		for _, segment := range strings.Split(trimmed, ".") {
			seg := strings.TrimSpace(segment)
			if seg == "" {
				continue
			}
			segLower := strings.ToLower(seg)

			// Try to extract known directives from this segment.
			if extracted := tryExtractDirective(segLower, seg, &order, &icon, &label, &menuCol, &menuRow, &menuPosSet); extracted {
				continue
			}

			// No directive matched — keep this segment as prose.
			proseSegments = append(proseSegments, seg)
		}

		// If some prose remains after extracting directives, keep the line
		// (rebuilt from its prose segments). Otherwise drop it entirely.
		if len(proseSegments) > 0 {
			kept = append(kept, strings.Join(proseSegments, ". ")+".")
		}
	}

	cleanDoc = strings.TrimSpace(strings.Join(kept, "\n"))
	return cleanDoc, order, icon, label, menuCol, menuRow, menuPosSet
}

// extractInteractiveDirective scans a struct doc comment for the interactive:
// IDS tag and returns its value (e.g. "rp2040" from "interactive:rp2040.").
//
// The value must be a single token with no spaces. It is stored as-is
// (lowercased by the dot-split parser) and must match a filename in the
// server's static/interactive/ directory without the .svg extension.
//
// The feature is not limited to hardware boards — any SVG that follows the
// IoTMaker dual-mode convention can be referenced here.
//
// Returns "" when the directive is absent or malformed.
func extractInteractiveDirective(doc string) string {
	for _, line := range strings.Split(doc, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		for _, seg := range strings.Split(lower, ".") {
			seg = strings.TrimSpace(seg)
			if strings.HasPrefix(seg, "interactive:") {
				val := strings.TrimSpace(seg[len("interactive:"):])
				if val != "" && !strings.Contains(val, " ") {
					return val
				}
			}
		}
	}
	return ""
}

// from a dot-split segment. Returns true if the segment was a directive and
// the output pointers were updated. Returns false if the segment is prose.
//
// segLower is the lowercase version of seg (avoids repeated ToLower calls).
// order, icon, label, menuCol, menuRow, menuPosSet are updated in-place on
// a successful match.
func tryExtractDirective(segLower, seg string, order *int, icon, label *string, menuCol, menuRow *int, menuPosSet *bool) bool {
	// ── executionOrder:N ──────────────────────────────────────────────────
	if strings.HasPrefix(segLower, "executionorder:") {
		val := strings.TrimSpace(seg[len("executionorder:"):])
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			*order = n
		}
		return true
	}
	// Deprecated: "execution order: N" (with spaces and colon after space)
	if strings.HasPrefix(segLower, "execution order:") {
		val := strings.TrimSpace(seg[len("execution order:"):])
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			*order = n
		}
		return true
	}

	// ── icon:name ─────────────────────────────────────────────────────────
	// Icon names are kebab-case FontAwesome names (e.g. "greater-than-equal").
	// We take everything after "icon:" as the name (trimmed). The name must
	// not be empty and must not contain spaces (kebab-case only).
	if strings.HasPrefix(segLower, "icon:") {
		val := strings.TrimSpace(segLower[len("icon:"):])
		if val != "" && !strings.Contains(val, " ") {
			*icon = val
		}
		return true
	}

	// ── label:text ────────────────────────────────────────────────────────
	// Labels are human-readable names matching [a-zA-Z0-9_\s-]+.
	// We preserve the original case of the value (not lowercased).
	if strings.HasPrefix(segLower, "label:") {
		// Use the original (non-lowercased) segment to preserve label casing.
		colonIdx := strings.Index(seg, ":")
		if colonIdx >= 0 {
			val := strings.TrimSpace(seg[colonIdx+1:])
			if val != "" {
				*label = val
			}
		}
		return true
	}

	// ── menu:col,row ──────────────────────────────────────────────────────
	// Explicit hex-menu position for this method, expressed as a signed
	// (col, row) offset from the Back button center.
	//
	// Format: menu:col,row.   e.g. menu:-1,-1. or menu:0,2.
	//
	// Both col and row are signed integers. (0,0) is reserved for Back and
	// should not be used. When this directive is absent, the IDE auto-places
	// the item using the radial layout engine.
	//
	// Português: Posição explícita do item no menu hexagonal, como offset
	// (col,linha) do botão Back. Quando ausente, o layout radial automático
	// é aplicado. (0,0) é reservado para Back.
	if strings.HasPrefix(segLower, "menu:") {
		val := strings.TrimSpace(seg[len("menu:"):])
		commaIdx := strings.Index(val, ",")
		if commaIdx > 0 && commaIdx < len(val)-1 {
			colStr := strings.TrimSpace(val[:commaIdx])
			rowStr := strings.TrimSpace(val[commaIdx+1:])
			col, colErr := strconv.Atoi(colStr)
			row, rowErr := strconv.Atoi(rowStr)
			if colErr == nil && rowErr == nil {
				*menuCol = col
				*menuRow = row
				*menuPosSet = true
			}
		}
		return true
	}

	return false
}

// =====================================================================
//  Source extraction
// =====================================================================

func extractAllMethods(fset *token.FileSet, src []byte, file *ast.File, structName string) string {
	var parts []string
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Recv == nil {
			continue
		}
		for _, recv := range funcDecl.Recv.List {
			if receiverTypeName(recv.Type) == structName {
				parts = append(parts, nodeSourceWithDoc(fset, src, funcDecl))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func nodeSourceWithDoc(fset *token.FileSet, src []byte, funcDecl *ast.FuncDecl) string {
	end := fset.Position(funcDecl.End()).Offset
	start := fset.Position(funcDecl.Pos()).Offset
	if funcDecl.Doc != nil {
		docStart := fset.Position(funcDecl.Doc.Pos()).Offset
		if docStart >= 0 && docStart < start {
			start = docStart
		}
	}
	if start >= 0 && end <= len(src) && start < end {
		return string(src[start:end])
	}
	var buf strings.Builder
	printer.Fprint(&buf, fset, funcDecl)
	return buf.String()
}

func nodeSource(fset *token.FileSet, src []byte, node ast.Node) string {
	start := fset.Position(node.Pos()).Offset
	end := fset.Position(node.End()).Offset
	if start >= 0 && end <= len(src) && start < end {
		return string(src[start:end])
	}
	var buf strings.Builder
	printer.Fprint(&buf, fset, node)
	return buf.String()
}

// =====================================================================
//  Type helpers
// =====================================================================

func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeString(t.Elt)
		}
		return "[...]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	default:
		return "interface{}"
	}
}

func baseTypeName(goType string) string {
	goType = strings.TrimPrefix(goType, "*")
	if idx := strings.LastIndex(goType, "."); idx >= 0 {
		return goType[idx+1:]
	}
	return goType
}

// =====================================================================
//  Manual page extraction from /* */ blocks
// =====================================================================

// parseManualBlocks scans the raw Go source for /* */ comment blocks and
// extracts manual pages.
//
// The showIn: tag now accepts any method name in addition to the reserved
// values "init" and "both":
//
//	showIn:init  → Init block only
//	showIn:run   → BlackBoxRun block only
//	showIn:log   → BlackBoxLog block only
//	showIn:both  → all blocks of this component (default)
//
// The matching is case-insensitive: "showIn:Log" matches the "Log" method.
func parseManualBlocks(src string, def *BlackBoxDef) ([]ManualPage, []string) {
	var pages []ManualPage
	var warnings []string

	blocks := extractBlockComments(src)

	for _, block := range blocks {
		page, warn, skip := parseOneManualBlock(block)
		if skip {
			continue
		}
		if warn != "" {
			warnings = append(warnings, warn)
			continue
		}
		pages = append(pages, page)
	}

	return pages, warnings
}

func parseOneManualBlock(body string) (page ManualPage, warning string, skip bool) {
	lines := strings.Split(body, "\n")

	page.Language = "en"
	page.ShowIn = ManualShowBoth
	var nameFound bool
	var markdownStart int = -1

	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		lower := strings.ToLower(line)

		if lower == "```markdown" {
			markdownStart = i + 1
			break
		}

		if strings.HasPrefix(lower, "manualname:") {
			val := extractTagValue(lower, "manualname:")
			if val == "" {
				return ManualPage{}, "manualName: tag has empty value", false
			}
			page.Name = val
			nameFound = true
			continue
		}
		if strings.HasPrefix(lower, "language:") {
			val := extractTagValue(lower, "language:")
			if val != "" {
				page.Language = val
			}
			continue
		}
		if strings.HasPrefix(lower, "showin:") {
			val := extractTagValue(lower, "showin:")
			// Accept any non-empty value — "init", "both", or any method name.
			// The IDE uses this verbatim to match against method names.
			if val != "" {
				page.ShowIn = ManualShowIn(val)
			}
			continue
		}
	}

	if !nameFound {
		return ManualPage{}, "", true
	}

	if markdownStart < 0 {
		return ManualPage{}, fmt.Sprintf("manual page %q has no ```markdown section", page.Name), false
	}

	contentLines := lines[markdownStart:]

	closeFence := -1
	for i := len(contentLines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(contentLines[i])
		if trimmed == "" {
			continue
		}
		if trimmed == "```" {
			closeFence = i
		}
		break
	}

	if closeFence < 0 {
		return ManualPage{}, fmt.Sprintf(
			"manual page %q: closing ``` not found immediately before */", page.Name), false
	}

	raw := strings.Join(contentLines[:closeFence], "\n")
	page.Content = strings.TrimSpace(raw)

	if page.Content == "" {
		return ManualPage{}, fmt.Sprintf("manual page %q has empty markdown content", page.Name), false
	}

	return page, "", false
}

func extractBlockComments(src string) []string {
	var blocks []string
	i := 0
	for i < len(src)-1 {
		if src[i] == '"' || src[i] == '`' {
			quote := rune(src[i])
			i++
			for i < len(src) {
				if rune(src[i]) == quote && src[i-1] != '\\' {
					i++
					break
				}
				i++
			}
			continue
		}
		if src[i] == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if src[i] == '/' && i+1 < len(src) && src[i+1] == '*' {
			start := i + 2
			i += 2
			for i < len(src)-1 {
				if src[i] == '*' && src[i+1] == '/' {
					blocks = append(blocks, src[start:i])
					i += 2
					break
				}
				i++
			}
			continue
		}
		i++
	}
	return blocks
}

func extractTagValue(lower, prefix string) string {
	val := strings.TrimSpace(lower[len(prefix):])
	val = strings.TrimSuffix(val, ".")
	return strings.TrimSpace(val)
}

func joinWarnings(ws []string) string {
	return strings.Join(ws, "; ")
}
