// server/codegen/blackbox/rewrite.go — AST rewrite engine for the device wizard.
//
// Why this file exists
// ====================
//
// The Field, Struct, Method and Port modals on the wizard tab produce
// typed edit instructions instead of raw source. Each modal save sends a
// list of WizardEdit values to /api/v1/blackbox/wizard/rewrite; this
// file is the engine that turns those edits into a rewritten Go source.
//
// The engine guarantees, in order of importance (per CLAUDE_WIZARD_DESIGN.md §5.1):
//
//  1. User-written prose in godoc comments is preserved.
//  2. User-owned struct tag keys (json:, yaml:, …) round-trip exactly.
//  3. Function bodies are never read or written.
//  4. Imports are never inserted, reordered, or removed.
//  5. The output is gofmt-valid; if gofmt would reject it, the rewrite
//     fails and the original source is returned unchanged.
//
// Two-phase implementation
// ========================
//
// The five operations split cleanly into "tag edits" (modify a
// `*ast.Field.Tag`) and "doc edits" (modify a `*ast.CommentGroup`
// attached to a struct, method, field, or port). Tag edits are safe to
// apply directly on the AST: the printer round-trips tags verbatim with
// no positional surprises. Doc edits, on the other hand, are brittle to
// apply on the AST — go/printer reads comments by token position, so
// adding or replacing a `*ast.CommentGroup` requires juggling synthetic
// `token.Pos` values and praying the printer associates them with the
// right declaration.
//
// We sidestep that fight by doing comment edits as **byte-level
// splices** on the gofmt'd intermediate source. The pipeline:
//
//  1. Parse the input source.
//  2. Apply every tag edit by AST mutation.
//  3. Print the AST → intermediate source (gofmt'd).
//  4. Re-parse the intermediate source so positions are accurate.
//  5. For each doc edit, compute the byte range to replace using the
//     re-parsed AST, then apply replacements right-to-left so earlier
//     offsets stay valid as later splices shrink or grow the buffer.
//  6. Run format.Source on the final result to normalise whitespace.
//
// Step 6 catches any indentation drift introduced by step 5 and
// guarantees a gofmt-clean output, so the user pasting the same code
// into the editor twice can never see a stale-formatting diff.
//
// Single-line param/result lists
// ==============================
//
// IDS port directives live in line comments above each parameter:
//
//	func (s *Sensor) Run(
//	    // doc: I2C bus instance.
//	    // connection: mandatory.
//	    i2c machine.I2C,
//	) error
//
// A parameter on the same line as the opening `(` has no comment line
// of its own to attach a directive block to. When a port edit targets
// such a parameter, the engine first auto-expands that specific
// FieldList to the multi-line form. The expansion is purely textual,
// preserves the parameter type expressions verbatim via go/format on
// each AST type node, and is gofmt-stable (gofmt does not collapse
// multi-line param lists with trailing commas).
//
// Single-line normalisation runs only when needed: a method that has
// only struct-level edits keeps its inline param list. The wizard's
// own UI never asks for port edits without there already being a
// motivating need to give the param its own comment line, so this
// behaviour is exactly what the user expects.
package blackbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// =============================================================================
//  Public API
// =============================================================================

// WizardEdit is a single typed mutation request from the wizard client.
// The shape mirrors CLAUDE_WIZARD_DESIGN.md §5.2; new ops are added by
// extending the switch in Rewrite plus this comment.
type WizardEdit struct {
	// Op identifies the operation: setStructDirectives, setFieldProp,
	// disableFieldProp, setMethodDirectives, or setPortConnection.
	Op string `json:"op"`

	// Path is the dotted address of the target node, per the grammar
	// in §3 of the design doc:
	//
	//	struct.<n>
	//	struct.<S>.field.<F>
	//	method.<S>.<M>
	//	method.<S>.<M>.in.<n>     |  method.<S>.<M>.out.<n>
	//
	Path string `json:"path"`

	// Args is the operation-specific payload. Each handler defines its
	// own anonymous struct and unmarshals on demand. Unknown fields are
	// ignored (forward compatibility with newer clients).
	Args json.RawMessage `json:"args"`
}

// Op constants — exported because slice-3 client code on the server
// side (the draft validator) needs to enumerate valid ops without
// hard-coding strings.
const (
	OpSetStructDirectives = "setStructDirectives"
	OpSetFieldProp        = "setFieldProp"
	OpDisableFieldProp    = "disableFieldProp"
	OpSetMethodDirectives = "setMethodDirectives"
	OpSetPortConnection   = "setPortConnection"
)

// Rewrite applies edits to source and returns the rewritten Go file.
// On any error the original source is returned unchanged so the caller
// can surface a message to the user without losing their in-progress
// work.
//
// Rewrite is safe to call concurrently — it shares no mutable state.
// Each call uses its own token.FileSet and AST.
func Rewrite(source string, edits []WizardEdit) (string, error) {
	// No edits → still gofmt the source so downstream consumers see a
	// canonical shape. Same as a normal save in the editor.
	if len(edits) == 0 {
		out, err := format.Source([]byte(source))
		if err != nil {
			return source, fmt.Errorf("source does not parse: %w", err)
		}
		return string(out), nil
	}

	// ── Phase 0: parse and classify edits ────────────────────────────────
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "blackbox.go", source, parser.ParseComments)
	if err != nil {
		return source, fmt.Errorf("parse error: %w", err)
	}

	type tagJob struct {
		path wizardPath
		edit WizardEdit
	}
	var tagJobs []tagJob
	var docJobs []docEdit

	for i, e := range edits {
		p, perr := parsePath(e.Path)
		if perr != nil {
			return source, fmt.Errorf("edit %d: %w", i, perr)
		}
		switch e.Op {
		case OpSetFieldProp:
			if p.Kind != pathStructField {
				return source, fmt.Errorf("edit %d (%s): path must be struct.<S>.field.<F>", i, e.Op)
			}
			tagJobs = append(tagJobs, tagJob{path: p, edit: e})
			// setFieldProp also carries an optional `comment` arg that
			// rewrites the field's godoc; planFieldDocFromProp builds
			// that doc edit when comment is non-empty.
			if d, ok, derr := planFieldDocFromProp(p, e); derr != nil {
				return source, fmt.Errorf("edit %d (%s): %w", i, e.Op, derr)
			} else if ok {
				docJobs = append(docJobs, d)
			}
		case OpDisableFieldProp:
			if p.Kind != pathStructField {
				return source, fmt.Errorf("edit %d (%s): path must be struct.<S>.field.<F>", i, e.Op)
			}
			tagJobs = append(tagJobs, tagJob{path: p, edit: e})
		case OpSetStructDirectives, OpSetMethodDirectives, OpSetPortConnection:
			d, derr := planDocEdit(p, e)
			if derr != nil {
				return source, fmt.Errorf("edit %d (%s): %w", i, e.Op, derr)
			}
			docJobs = append(docJobs, d)
		default:
			return source, fmt.Errorf("edit %d: unknown op %q", i, e.Op)
		}
	}

	// ── Phase 1: tag edits via AST mutation ──────────────────────────────
	for _, j := range tagJobs {
		if err := applyTagEdit(file, j.path, j.edit); err != nil {
			return source, fmt.Errorf("tag edit %s: %w", j.edit.Op, err)
		}
	}

	// ── Phase 1b: split multi-name port declarations ─────────────────────
	//
	// A method like `func (s *S) Run() (clear, red, green, blue uint16)`
	// has all four outputs in a single *ast.Field with four Names. Phase 2
	// writes a doc comment block keyed to ONE name (the one the wizard
	// edited), but the splice replaces the leading-comment range above
	// the entire field — meaning all four names share the same metadata.
	// On re-parse, the parser dutifully assigns the same Label / Doc /
	// Connection to each name and the user sees `red`, `green`, `blue`
	// all renamed to whatever they wrote for `clear`.
	//
	// Fix: before printing the AST, expand any multi-name field that a
	// setPortConnection edit targets into separate single-name fields.
	// gofmt prints them on separate lines, each with its own leading
	// comment slot, and Phase 2's findPort lookup matches a specific
	// name without affecting siblings.
	//
	// This expands ONLY fields actually referenced by an edit — leaving
	// untouched lists alone keeps the source stable for users who never
	// edit those ports through the wizard.
	for _, j := range docJobs {
		if j.Path.Kind != pathMethodPort {
			continue
		}
		if err := splitMultiNamePort(file, j.Path); err != nil {
			return source, fmt.Errorf("split port method.%s.%s.%s.%s: %w",
				j.Path.Struct, j.Path.Method, j.Path.Dir, j.Path.Port, err)
		}
	}

	// Print the AST after tag mutations. format.Node already runs the
	// gofmt formatter, so the output is canonical Go.
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return source, fmt.Errorf("emit after tag edits: %w", err)
	}
	intermediate := buf.String()

	// ── Phase 2: doc edits via byte-level splices ────────────────────────
	if len(docJobs) > 0 {
		out, err := applyDocEdits(intermediate, docJobs)
		if err != nil {
			return source, fmt.Errorf("doc edit: %w", err)
		}
		intermediate = out
	}

	// ── Phase 3: final format pass ───────────────────────────────────────
	final, err := format.Source([]byte(intermediate))
	if err != nil {
		// This should never happen — phase 1 produced gofmt-valid Go
		// and phase 2's splices are designed to keep it gofmt-valid.
		// If we get here, return the un-formatted intermediate plus an
		// error so the user can see what went wrong.
		return source, fmt.Errorf("post-format error: %w", err)
	}
	return string(final), nil
}

// =============================================================================
//  Path parsing
// =============================================================================

type pathKind int

const (
	pathStruct      pathKind = iota // struct.<n>
	pathStructField                 // struct.<S>.field.<F>
	pathMethod                      // method.<S>.<M>
	pathMethodPort                  // method.<S>.<M>.in.<n> or .out.<n>
)

type wizardPath struct {
	Kind   pathKind
	Struct string // always populated
	Field  string // pathStructField only
	Method string // pathMethod, pathMethodPort
	Dir    string // pathMethodPort only: "in" or "out"
	Port   string // pathMethodPort only
}

// parsePath converts a dotted path string into a wizardPath. Valid
// shapes are listed on WizardEdit.Path. Anything else returns an error
// — there is no fallback or "best effort" interpretation.
func parsePath(s string) (wizardPath, error) {
	parts := strings.Split(s, ".")
	switch {
	case len(parts) == 2 && parts[0] == "struct":
		if parts[1] == "" {
			return wizardPath{}, fmt.Errorf("empty struct name in path %q", s)
		}
		return wizardPath{Kind: pathStruct, Struct: parts[1]}, nil
	case len(parts) == 4 && parts[0] == "struct" && parts[2] == "field":
		if parts[1] == "" || parts[3] == "" {
			return wizardPath{}, fmt.Errorf("empty struct or field name in path %q", s)
		}
		return wizardPath{Kind: pathStructField, Struct: parts[1], Field: parts[3]}, nil
	case len(parts) == 3 && parts[0] == "method":
		if parts[1] == "" || parts[2] == "" {
			return wizardPath{}, fmt.Errorf("empty struct or method name in path %q", s)
		}
		return wizardPath{Kind: pathMethod, Struct: parts[1], Method: parts[2]}, nil
	case len(parts) == 5 && parts[0] == "method" && (parts[3] == "in" || parts[3] == "out"):
		if parts[1] == "" || parts[2] == "" || parts[4] == "" {
			return wizardPath{}, fmt.Errorf("empty segment in port path %q", s)
		}
		return wizardPath{
			Kind:   pathMethodPort,
			Struct: parts[1],
			Method: parts[2],
			Dir:    parts[3],
			Port:   parts[4],
		}, nil
	}
	return wizardPath{}, fmt.Errorf("malformed path %q", s)
}

// =============================================================================
//  AST node lookup
// =============================================================================

func findStructDecl(file *ast.File, name string) (*ast.GenDecl, *ast.StructType, error) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return nil, nil, fmt.Errorf("type %q is not a struct", name)
			}
			return gd, st, nil
		}
	}
	return nil, nil, fmt.Errorf("struct %q not found", name)
}

func findMethodDecl(file *ast.File, structName, methodName string) (*ast.FuncDecl, error) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Name.Name != methodName {
			continue
		}
		if len(fd.Recv.List) == 0 {
			continue
		}
		if receiverTypeName(fd.Recv.List[0].Type) == structName {
			return fd, nil
		}
	}
	return nil, fmt.Errorf("method %s.%s not found", structName, methodName)
}

// findStructField looks up a single-name field by name. Returns the
// field along with the index in the struct's field list — the index is
// useful when the caller needs to splice neighbouring entries.
func findStructField(st *ast.StructType, name string) (*ast.Field, int, error) {
	if st.Fields == nil {
		return nil, -1, fmt.Errorf("struct has no fields")
	}
	for fi, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == name {
				return f, fi, nil
			}
		}
	}
	return nil, -1, fmt.Errorf("field %q not found", name)
}

// findPort looks up a parameter or return value by name on a method's
// signature. Anonymous params (no name) cannot be addressed and return
// an error.
func findPort(fd *ast.FuncDecl, dir, name string) (*ast.FieldList, *ast.Field, int, error) {
	var list *ast.FieldList
	switch dir {
	case "in":
		list = fd.Type.Params
	case "out":
		list = fd.Type.Results
	default:
		return nil, nil, -1, fmt.Errorf("unknown port direction %q", dir)
	}
	if list == nil {
		return nil, nil, -1, fmt.Errorf("method has no %s ports", dir)
	}
	for fi, f := range list.List {
		for _, n := range f.Names {
			if n.Name == name {
				return list, f, fi, nil
			}
		}
	}
	return nil, nil, -1, fmt.Errorf("port %q not found in %s ports", name, dir)
}

// splitMultiNamePort expands a multi-name parameter or result field
// in the method targeted by `p` so that the named port is in its own
// single-name *ast.Field. After this runs, findPort(p.Port) returns
// a Field with exactly one Name and the doc-splice can replace its
// leading comment without affecting siblings.
//
// No-op when the target field already has exactly one name (the
// common case; only ports declared as `a, b, c uint16` need splitting).
//
// Returns an error only when the method or the named port can't be
// located — these are programmer errors at this point because the
// wizard already validated the path before submitting the edit.
func splitMultiNamePort(file *ast.File, p wizardPath) error {
	fd, err := findMethodDecl(file, p.Struct, p.Method)
	if err != nil {
		return err
	}
	list, field, idx, err := findPort(fd, p.Dir, p.Port)
	if err != nil {
		return err
	}
	if len(field.Names) <= 1 {
		// Already isolated — nothing to do.
		return nil
	}

	// Build replacement fields, one per name. Preserve the type
	// expression by sharing the same *ast.Expr (gofmt re-prints it
	// per line; sharing is safe because we don't mutate it).
	splits := make([]*ast.Field, 0, len(field.Names))
	for ni, name := range field.Names {
		nf := &ast.Field{
			Names: []*ast.Ident{{Name: name.Name}},
			Type:  field.Type,
		}
		// Carry the doc only on the first split-out field. The
		// original group-level Doc, if any, was the source's
		// "// Returns" header or similar prose — keeping it on
		// the first port preserves the user's intent without
		// duplicating prose four times.
		if ni == 0 {
			nf.Doc = field.Doc
		}
		// Tag fields aren't valid on parameters/results in Go, so
		// we don't copy field.Tag (it would always be nil here).
		splits = append(splits, nf)
	}

	// Splice the new fields into the parameter/result list at the
	// original position.
	tail := append([]*ast.Field{}, list.List[idx+1:]...)
	list.List = append(list.List[:idx], splits...)
	list.List = append(list.List, tail...)

	return nil
}

// =============================================================================
//  Phase 1 — tag edits (AST mutation)
// =============================================================================

func applyTagEdit(file *ast.File, p wizardPath, e WizardEdit) error {
	_, st, err := findStructDecl(file, p.Struct)
	if err != nil {
		return err
	}
	field, _, err := findStructField(st, p.Field)
	if err != nil {
		return err
	}

	// Multi-name fields (`a, b, c int`) are split first so the tag we
	// write applies only to the named field. Each split-out field is a
	// fresh *ast.Field in the struct's field list.
	if len(field.Names) > 1 {
		field = splitMultiNameField(st, field, p.Field)
		if field == nil {
			return fmt.Errorf("could not isolate field %q", p.Field)
		}
	}

	// Read the existing tag (if any) and parse it through the codec.
	var existing []tagPair
	if field.Tag != nil {
		raw := strings.Trim(field.Tag.Value, "`")
		existing, err = parseStructTag(raw)
		if err != nil {
			return fmt.Errorf("existing tag for %q: %w", p.Field, err)
		}
	}

	var merged []tagPair
	switch e.Op {
	case OpSetFieldProp:
		var args setFieldPropArgs
		if err := json.Unmarshal(e.Args, &args); err != nil {
			return fmt.Errorf("invalid args: %w", err)
		}
		if strings.TrimSpace(args.Label) == "" {
			return fmt.Errorf("label is required for setFieldProp")
		}
		idsPairs, err := buildPropPairs(args)
		if err != nil {
			return err
		}
		merged = upsertIDSKeys(existing, idsPairs)
	case OpDisableFieldProp:
		merged = removeIDSKeys(existing)
	default:
		return fmt.Errorf("applyTagEdit called with non-tag op %q", e.Op)
	}

	if len(merged) == 0 {
		field.Tag = nil
		return nil
	}
	field.Tag = &ast.BasicLit{
		Kind:  token.STRING,
		Value: "`" + emitStructTag(merged) + "`",
	}
	return nil
}

// splitMultiNameField replaces a multi-name field `a, b, c int` in the
// struct's field list with three separate fields. Returns the freshly
// minted *ast.Field for the requested name, or nil when the name is
// not found in the original list.
//
// The split preserves the type expression and tag (if any), but copies
// nothing from Doc/Comment because those associate with the field
// declaration as a whole, not with each name. The original Doc lives on
// the first split-out field; subsequent ones get nil Doc.
func splitMultiNameField(st *ast.StructType, field *ast.Field, target string) *ast.Field {
	idx := -1
	for i, f := range st.Fields.List {
		if f == field {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}

	splits := make([]*ast.Field, 0, len(field.Names))
	for ni, name := range field.Names {
		nf := &ast.Field{
			Names: []*ast.Ident{{Name: name.Name}},
			Type:  field.Type,
		}
		if field.Tag != nil {
			// Same tag on every split — the IDS edit will overwrite the
			// target's prop key but leave neighbours untouched (they
			// keep the shared json: / yaml: / etc.).
			tagCopy := *field.Tag
			nf.Tag = &tagCopy
		}
		// Carry the doc only on the first one to avoid duplicating prose.
		if ni == 0 {
			nf.Doc = field.Doc
		}
		splits = append(splits, nf)
	}

	// Replace the original entry with the splits, preserving order.
	tail := append([]*ast.Field{}, st.Fields.List[idx+1:]...)
	st.Fields.List = append(st.Fields.List[:idx], splits...)
	st.Fields.List = append(st.Fields.List, tail...)

	// Find and return the target.
	for _, f := range splits {
		if len(f.Names) == 1 && f.Names[0].Name == target {
			return f
		}
	}
	return nil
}

// setFieldPropArgs is the JSON body of the setFieldProp operation.
type setFieldPropArgs struct {
	// Label — UI display name; goes into prop:"…". Required.
	Label string `json:"label"`

	// Default — initial value for the prop. Optional.
	Default string `json:"default"`

	// Format — restriction kind: "options", "range_min_max", "range_min",
	// "range_max", "regex", or empty for no constraint.
	Format string `json:"format"`

	// FormatArgs — operation-specific data per Format. See buildPropPairs.
	FormatArgs map[string]json.RawMessage `json:"formatArgs"`

	// Unit — unit label shown in the inspector. Optional.
	Unit string `json:"unit"`

	// Comment — user prose godoc on the field. Handled as a doc edit,
	// not part of the tag — see planFieldDocFromProp.
	Comment string `json:"comment"`
}

// buildPropPairs converts the modal's Format dropdown selection into
// IDS-owned tag pairs. The mapping is the table in
// CLAUDE_WIZARD_DESIGN.md §5.3.
//
// The pair order is fixed: prop, default, options/range/regex/unit. This
// gives stable, reviewable diffs in version control.
func buildPropPairs(args setFieldPropArgs) ([]tagPair, error) {
	var out []tagPair
	out = append(out, tagPair{Key: "prop", Value: args.Label})
	if args.Default != "" {
		out = append(out, tagPair{Key: "default", Value: args.Default})
	}
	switch args.Format {
	case "":
		// No constraint — fine.
	case "options":
		raw, ok := args.FormatArgs["values"]
		if !ok {
			return nil, fmt.Errorf("format=options requires formatArgs.values")
		}
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("formatArgs.values must be a string array: %w", err)
		}
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
		}
		out = append(out, tagPair{Key: "options", Value: strings.Join(values, ",")})
	case "range_min_max":
		min, errMin := jsonNumberToString(args.FormatArgs["min"])
		max, errMax := jsonNumberToString(args.FormatArgs["max"])
		if errMin != nil || errMax != nil {
			return nil, fmt.Errorf("format=range_min_max requires numeric min and max")
		}
		out = append(out, tagPair{Key: "range", Value: min + ".." + max})
	case "range_min":
		min, err := jsonNumberToString(args.FormatArgs["min"])
		if err != nil {
			return nil, fmt.Errorf("format=range_min requires numeric min: %w", err)
		}
		out = append(out, tagPair{Key: "range_min", Value: min})
	case "range_max":
		max, err := jsonNumberToString(args.FormatArgs["max"])
		if err != nil {
			return nil, fmt.Errorf("format=range_max requires numeric max: %w", err)
		}
		out = append(out, tagPair{Key: "range_max", Value: max})
	case "regex":
		raw, ok := args.FormatArgs["pattern"]
		if !ok {
			return nil, fmt.Errorf("format=regex requires formatArgs.pattern")
		}
		var pat string
		if err := json.Unmarshal(raw, &pat); err != nil {
			return nil, fmt.Errorf("formatArgs.pattern must be a string: %w", err)
		}
		if pat != "" {
			out = append(out, tagPair{Key: "inputRegex", Value: pat})
		}
	default:
		return nil, fmt.Errorf("unknown format %q", args.Format)
	}
	if args.Unit != "" {
		out = append(out, tagPair{Key: "unit", Value: args.Unit})
	}
	return out, nil
}

// jsonNumberToString accepts a JSON number (int or float) and returns
// its string form without trailing zeros for integer-valued floats —
// "5" instead of "5.000000" — so the IDS tag stays compact.
func jsonNumberToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("missing number")
	}
	// Try int first to keep "5" as "5", not "5.0".
	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return strconv.FormatInt(i, 10), nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return "", fmt.Errorf("not a number: %w", err)
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

// =============================================================================
//  Phase 2 — doc edits (byte-level splices on the post-tag source)
// =============================================================================

// docEdit is the planned shape of a comment-block edit. The path
// identifies the target node (struct decl, method decl, struct field,
// or port). Comment is the user prose; Directives are pre-formatted
// IDS lines like "label:Foo." (without the "// " prefix).
type docEdit struct {
	Op         string
	Path       wizardPath
	Comment    string
	Directives []string
}

func planDocEdit(p wizardPath, e WizardEdit) (docEdit, error) {
	d := docEdit{Op: e.Op, Path: p}
	switch e.Op {
	case OpSetStructDirectives:
		if p.Kind != pathStruct {
			return d, fmt.Errorf("path must be struct.<n>")
		}
		var args struct {
			Label   string `json:"label"`
			Icon    string `json:"icon"`
			Comment string `json:"comment"`
		}
		if err := json.Unmarshal(e.Args, &args); err != nil {
			return d, fmt.Errorf("invalid args: %w", err)
		}
		d.Comment = args.Comment
		if args.Label != "" {
			d.Directives = append(d.Directives, "label:"+args.Label+".")
		}
		if args.Icon != "" {
			d.Directives = append(d.Directives, "icon:"+args.Icon+".")
		}
	case OpSetMethodDirectives:
		if p.Kind != pathMethod {
			return d, fmt.Errorf("path must be method.<S>.<M>")
		}
		var args struct {
			Label          string `json:"label"`
			Icon           string `json:"icon"`
			ExecutionOrder *int   `json:"executionOrder"`
			Comment        string `json:"comment"`
		}
		if err := json.Unmarshal(e.Args, &args); err != nil {
			return d, fmt.Errorf("invalid args: %w", err)
		}
		d.Comment = args.Comment
		if args.Label != "" {
			d.Directives = append(d.Directives, "label:"+args.Label+".")
		}
		if args.Icon != "" {
			d.Directives = append(d.Directives, "icon:"+args.Icon+".")
		}
		if args.ExecutionOrder != nil {
			d.Directives = append(d.Directives, "executionOrder:"+strconv.Itoa(*args.ExecutionOrder)+".")
		}
	case OpSetPortConnection:
		if p.Kind != pathMethodPort {
			return d, fmt.Errorf("path must be method.<S>.<M>.{in|out}.<n>")
		}
		var args struct {
			Connection string `json:"connection"`
			Label      string `json:"label"`
			Comment    string `json:"comment"`
		}
		if err := json.Unmarshal(e.Args, &args); err != nil {
			return d, fmt.Errorf("invalid args: %w", err)
		}
		if args.Connection != "" && args.Connection != "optional" && args.Connection != "mandatory" {
			return d, fmt.Errorf("connection must be \"optional\" or \"mandatory\"")
		}
		// Port godoc is a single comment block where every line is
		// either a `key:value.` directive or prose. The rewrite engine
		// emits the user's Comment as a `doc:` directive (rather than
		// raw prose) so that on re-parse the splitter cleanly
		// separates it from the other directives — without that the
		// `\n` between the prose and the next directive collapses to
		// a space and the next-segment-with-no-trailing-`.` rule
		// merges the two into one segment.
		//
		// Empty Comment means "remove the doc:" — we just skip the
		// emission and the field's prose disappears on the next save.
		if strings.TrimSpace(args.Comment) != "" {
			d.Directives = append(d.Directives, "doc:"+strings.TrimSpace(args.Comment)+".")
		}
		// connection: is for INPUTS only (slice-7 rule). Outputs —
		// regular or error — are always wiring-optional. We drop
		// the connection arg silently when dir=="out". Users can
		// still send it (older clients, manual API calls) but it
		// has no effect on the source.
		//
		// Side effect: any pre-existing `connection:` directive in
		// the source disappears on next save because the splice
		// REPLACES the entire leading-comment block. This is the
		// "organic cleanup" path discussed with Kemper — no
		// migration, no global cleanup, just self-healing as the
		// user touches each output.
		if p.Dir == "in" && args.Connection != "" {
			d.Directives = append(d.Directives, "connection:"+args.Connection+".")
		}
		if args.Label != "" {
			d.Directives = append(d.Directives, "label:"+args.Label+".")
		}
	default:
		return d, fmt.Errorf("planDocEdit called with non-doc op %q", e.Op)
	}
	return d, nil
}

// planFieldDocFromProp synthesises a doc edit on the field's godoc
// comment when setFieldProp carries a non-empty `comment` arg. Returns
// (docEdit, true, nil) when there is something to do, or (_, false, nil)
// when the prop edit has no comment to apply.
//
// The field's godoc carries only user prose — IDS data lives in the
// tag, not the doc — so Directives stays nil here.
func planFieldDocFromProp(p wizardPath, e WizardEdit) (docEdit, bool, error) {
	var args setFieldPropArgs
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return docEdit{}, false, fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(args.Comment) == "" {
		return docEdit{}, false, nil
	}
	return docEdit{
		Op:      "setFieldDoc",
		Path:    p,
		Comment: args.Comment,
	}, true, nil
}

// applyDocEdits is phase 2: splice doc-comment text into the
// gofmt-printed intermediate source. Operates on the source as a byte
// slice, applying replacements right-to-left so positions computed
// before the loop stay valid throughout.
func applyDocEdits(source string, edits []docEdit) (string, error) {
	src := []byte(source)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "blackbox.go", src, parser.ParseComments)
	if err != nil {
		return source, fmt.Errorf("re-parse for doc edits: %w", err)
	}

	var splices []splice

	// Walk edits, also performing single-line param-list normalisation
	// when a port edit demands it. Normalisation produces its own
	// splice and rewrites the FieldList opening/closing bytes; we then
	// re-parse to get fresh positions for the per-port splice.
	//
	// Doing this inside the same loop, with a re-parse after every
	// normalisation, is simpler than tracking offset deltas manually.
	for _, e := range edits {
		// If this is a port edit on a single-line param/result list,
		// expand the list to multi-line first. The expansion may shift
		// every subsequent byte offset, so we serialise: expand, re-
		// parse, plan the splice, then continue.
		if e.Path.Kind == pathMethodPort {
			expanded, didExpand, err := expandSingleLinePortList(src, fset, file, e.Path)
			if err != nil {
				return source, err
			}
			if didExpand {
				// Apply queued splices first so their offsets reset
				// against the same source the expansion happened on.
				if len(splices) > 0 {
					expanded = applySplicesRTL(expanded, splices)
					splices = splices[:0]
				}
				src = expanded
				fset = token.NewFileSet()
				file, err = parser.ParseFile(fset, "blackbox.go", src, parser.ParseComments)
				if err != nil {
					return source, fmt.Errorf("re-parse after param-list expansion: %w", err)
				}
			}
		}

		startOff, endOff, indent, err := docEditTargetRange(string(src), fset, file, e)
		if err != nil {
			return source, err
		}
		text := buildCommentBlock(e.Comment, e.Directives, indent)
		splices = append(splices, splice{start: startOff, end: endOff, replacement: text})
	}

	out := applySplicesRTL(src, splices)
	return string(out), nil
}

// splice is a single byte-range replacement scheduled by phase 2.
type splice struct {
	start, end  int
	replacement string
}

// applySplicesRTL applies a list of splices to src, sorted right-to-left.
// The right-to-left order keeps every splice's `start` offset valid even
// when earlier splices change the buffer length. No overlap detection is
// done — the caller is responsible for ensuring ranges don't overlap.
// (In our pipeline they cannot: each edit targets a distinct AST node.)
func applySplicesRTL(src []byte, splices []splice) []byte {
	// Copy so we can sort without mutating the caller's slice.
	work := append([]splice(nil), splices...)
	sort.Slice(work, func(i, j int) bool { return work[i].start > work[j].start })
	for _, s := range work {
		next := make([]byte, 0, len(src)+len(s.replacement)-(s.end-s.start))
		next = append(next, src[:s.start]...)
		next = append(next, []byte(s.replacement)...)
		next = append(next, src[s.end:]...)
		src = next
	}
	return src
}

// docEditTargetRange returns the byte range to replace and the indent
// to apply for one doc edit. The range is [startOff, endOff): when the
// node already has a doc comment, it is the range of that comment plus
// the trailing newline; when it doesn't, both offsets equal the
// declaration's start position so the splice becomes a pure insertion.
func docEditTargetRange(source string, fset *token.FileSet, file *ast.File, e docEdit) (int, int, string, error) {
	switch e.Path.Kind {
	case pathStruct:
		gd, _, err := findStructDecl(file, e.Path.Struct)
		if err != nil {
			return 0, 0, "", err
		}
		startPos := gd.TokPos
		endPos := startPos
		if gd.Doc != nil {
			startPos = gd.Doc.Pos()
		}
		return fset.Position(startPos).Offset, fset.Position(endPos).Offset, "", nil
	case pathMethod:
		fd, err := findMethodDecl(file, e.Path.Struct, e.Path.Method)
		if err != nil {
			return 0, 0, "", err
		}
		startPos := fd.Type.Func
		if startPos == token.NoPos {
			startPos = fd.Pos()
		}
		endPos := startPos
		if fd.Doc != nil {
			startPos = fd.Doc.Pos()
		}
		return fset.Position(startPos).Offset, fset.Position(endPos).Offset, "", nil
	case pathStructField:
		_, st, err := findStructDecl(file, e.Path.Struct)
		if err != nil {
			return 0, 0, "", err
		}
		field, _, err := findStructField(st, e.Path.Field)
		if err != nil {
			return 0, 0, "", err
		}
		startPos := field.Pos()
		endPos := startPos
		if field.Doc != nil {
			startPos = field.Doc.Pos()
		}
		indent := leadingIndent(source, fset.Position(field.Pos()).Offset)
		return fset.Position(startPos).Offset, fset.Position(endPos).Offset, indent, nil
	case pathMethodPort:
		fd, err := findMethodDecl(file, e.Path.Struct, e.Path.Method)
		if err != nil {
			return 0, 0, "", err
		}
		_, field, _, err := findPort(fd, e.Path.Dir, e.Path.Port)
		if err != nil {
			return 0, 0, "", err
		}
		// Locate the comment block (if any) immediately above the
		// parameter line. We can't rely on field.Doc here: Go's
		// parser does NOT associate leading comments to fields when
		// they are nested inside a parameter or result list — those
		// comments end up in file.Comments instead, unattached.
		//
		// findLeadingPortComment walks file.Comments to find a
		// CommentGroup whose last line ends on the line directly
		// above the parameter. That group, if found, is the range
		// to replace; otherwise both offsets equal field.Pos() and
		// the splice becomes a pure insertion.
		startPos := field.Pos()
		endPos := field.Pos()
		if cg := findLeadingPortComment(file, fset, field); cg != nil {
			startPos = cg.Pos()
		}
		indent := leadingIndent(source, fset.Position(field.Pos()).Offset)
		return fset.Position(startPos).Offset, fset.Position(endPos).Offset, indent, nil
	}
	return 0, 0, "", fmt.Errorf("unsupported doc-edit path kind")
}

// findLeadingPortComment locates the CommentGroup that visually sits
// immediately above a parameter or result field, if any. Returns nil
// when the field has no leading comment.
//
// Why this exists: Go's parser only attaches a CommentGroup to
// ast.Field.Doc when the field is at the top level of a struct
// definition. Comments inside `func (...) (...) { ... }` parameter
// or result lists are kept in file.Comments as floating groups. To
// edit them via splice we need to find them ourselves.
//
// "Immediately above" means: the comment group's last line ends on
// the line directly preceding the field's first line. We tolerate
// any amount of horizontal whitespace but not blank lines between
// the comment and the field — a blank line means the comment
// belongs to something earlier, not to this parameter.
func findLeadingPortComment(file *ast.File, fset *token.FileSet, field *ast.Field) *ast.CommentGroup {
	fieldLine := fset.Position(field.Pos()).Line
	var best *ast.CommentGroup
	for _, cg := range file.Comments {
		if cg.End() > field.Pos() {
			// Comment starts after the field — can't be its leading
			// doc. (file.Comments is sorted by position, but we
			// iterate fully to keep the logic obvious; the cost is
			// negligible since file.Comments is short.)
			continue
		}
		// The CommentGroup's last line:
		lastLine := fset.Position(cg.End()).Line
		// Comment whose final line is exactly one line above the
		// field is the one we want. If multiple groups satisfy,
		// take the last (closest) — gofmt-formatted code rarely
		// has that case, but the rule is unambiguous.
		if lastLine == fieldLine-1 {
			best = cg
		}
	}
	return best
}

// buildCommentBlock formats the comment lines + IDS directives into a
// gofmt-shaped block, ending with a newline plus the indent of the
// following node so the spliced text lines up correctly. The replaced
// range always ends at the declaration's first character (column zero
// of its line), so we restore that indent ourselves at the end.
//
// Empty comment AND empty directives produce an empty string, which
// removes any pre-existing doc when the range covers it.
func buildCommentBlock(comment string, directives []string, indent string) string {
	var lines []string
	if strings.TrimSpace(comment) != "" {
		for _, l := range strings.Split(comment, "\n") {
			lines = append(lines, indent+"// "+l)
		}
		if len(directives) > 0 {
			lines = append(lines, indent+"//")
		}
	}
	for _, d := range directives {
		lines = append(lines, indent+"// "+d)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n" + indent
}

// leadingIndent returns the whitespace at the beginning of the line
// containing offset. Used to make sure inserted comments line up with
// the field they document.
func leadingIndent(source string, offset int) string {
	if offset > len(source) {
		offset = len(source)
	}
	start := offset
	for start > 0 && source[start-1] != '\n' {
		start--
	}
	end := start
	for end < offset && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	return source[start:end]
}

// =============================================================================
//  Single-line param-list expansion
// =============================================================================

// expandSingleLinePortList expands a method's param or result list to
// multi-line form when the targeted port lives on the same line as the
// list's opening `(`. Returns (newSource, true, nil) when an expansion
// happened, or (origSource, false, nil) when no expansion was needed.
//
// The expansion is purely textual but uses the AST to read each field's
// type expression — this guarantees we re-emit the type verbatim
// (including stars, generics, and qualified names) without re-implementing
// Go's expression grammar.
func expandSingleLinePortList(src []byte, fset *token.FileSet, file *ast.File, p wizardPath) ([]byte, bool, error) {
	fd, err := findMethodDecl(file, p.Struct, p.Method)
	if err != nil {
		return src, false, err
	}
	var list *ast.FieldList
	switch p.Dir {
	case "in":
		list = fd.Type.Params
	case "out":
		list = fd.Type.Results
	}
	if list == nil || list.Opening == token.NoPos {
		return src, false, nil
	}
	openLine := fset.Position(list.Opening).Line
	closeLine := fset.Position(list.Closing).Line
	if openLine != closeLine {
		// Already multi-line.
		return src, false, nil
	}

	// Build the multi-line replacement.
	openOff := fset.Position(list.Opening).Offset
	closeOff := fset.Position(list.Closing).Offset
	// The indent for each parameter line is one tab past whatever
	// indent the `func` keyword has. Methods are always at column 1, so
	// the resulting indent is one tab.
	funcIndent := leadingIndent(string(src), fset.Position(fd.Pos()).Offset)
	paramIndent := funcIndent + "\t"

	var sb strings.Builder
	sb.WriteString("(\n")
	for _, f := range list.List {
		sb.WriteString(paramIndent)
		if len(f.Names) > 0 {
			names := make([]string, len(f.Names))
			for i, n := range f.Names {
				names[i] = n.Name
			}
			sb.WriteString(strings.Join(names, ", "))
			sb.WriteString(" ")
		}
		typeStr, err := formatTypeNode(fset, f.Type)
		if err != nil {
			return src, false, fmt.Errorf("emit type for port: %w", err)
		}
		sb.WriteString(typeStr)
		if f.Tag != nil {
			sb.WriteString(" ")
			sb.WriteString(f.Tag.Value)
		}
		sb.WriteString(",\n")
	}
	sb.WriteString(funcIndent)

	// Replace the byte range [openOff, closeOff) — closeOff is the byte
	// where ')' sits, and we keep that ')'.
	out := make([]byte, 0, len(src)+sb.Len()-(closeOff-openOff))
	out = append(out, src[:openOff]...)
	out = append(out, []byte(sb.String())...)
	out = append(out, src[closeOff:]...)
	return out, true, nil
}

// formatTypeNode prints a type expression as Go source via go/format,
// preserving qualifiers (`machine.I2C`), pointers (`*machine.I2C`),
// arrays, generics, and so on without re-implementing the printer.
func formatTypeNode(fset *token.FileSet, expr ast.Expr) (string, error) {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, expr); err != nil {
		return "", err
	}
	return buf.String(), nil
}
