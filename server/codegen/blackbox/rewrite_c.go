// server/codegen/blackbox/rewrite_c.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// rewrite_c.go — Apply WizardEdits to C99 source.
//
// English:
//
//	Slice 3 of C99 device support. This file is the C99 counterpart
//	of rewrite.go (Go). Same 5 operations, same WizardEdit wire
//	format, same path grammar — the difference is HOW the edits are
//	written into the source.
//
//	Go uses go/ast + AST mutation for tag edits and byte-level
//	splicing for doc edits, then go/format normalises the output.
//	C99 has no AST tooling and no canonical formatter, so EVERY
//	edit is a byte-level splice. The engine:
//
//	  1. Plans each edit independently: locates the target's byte
//	     range and the byte range of its current leading-comment
//	     block (the "splice range"), then renders the new comment
//	     block.
//	  2. Sorts plans descending by start offset.
//	  3. Applies them right-to-left so earlier offsets stay valid
//	     as later splices shrink or grow the buffer.
//
//	What's preserved verbatim:
//	  - Function bodies — never read, never written.
//	  - #includes — never inserted, removed, or reordered.
//	  - Code outside of the edited target's leading-comment range.
//	  - User-written prose lines in the leading comment (anything
//	    that is NOT an IDS directive line is carried through).
//	  - Existing indentation: the engine reads the indent of the
//	    target's line and applies it to every inserted comment
//	    line. The user's tab-vs-space preference round-trips.
//
//	What changes on every edit:
//	  - IDS directive lines in the target's leading comment
//	    (`// label:…`, `// icon:…`, `// prop:…`, etc.) are
//	    completely replaced by the edit's new content. Pre-existing
//	    `connection:` directives on output ports are dropped per
//	    rewrite.go's "organic cleanup" rule (slice-7).
//
//	What's NOT in Slice 3 (future work):
//	  - Block-comment field directives (Slice 6).
//	  - Multi-name field splitting (C does not allow multi-name
//	    declarations the way Go does, so this is moot).
//	  - Formatter-driven whitespace normalisation. C99 has no
//	    `go/format` equivalent; the engine preserves whatever
//	    indentation and whitespace the user wrote.
//
// Português:
//
//	Slice 3 do C99 — motor de reescrita. Mesmas 5 ops do Go (Op…
//	constants), mesma path grammar, mesmo WizardEdit shape. O wire
//	format JSON é idêntico — o SPA não precisa saber qual motor
//	está rodando. Todas as edições são byte-level splice (C99 não
//	tem go/ast). Pré-existente prose é preservada; só as linhas
//	de diretiva são substituídas.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// RewriteC applies edits to a C99 source string and returns the
// rewritten file. On any error the original source is returned
// unchanged so the caller can surface a message without losing the
// user's in-progress work.
//
// RewriteC is safe to call concurrently — it shares no mutable
// state. Each call uses its own scratch storage.
func RewriteC(source string, edits []WizardEdit) (string, error) {
	// Empty edits is a valid no-op. Unlike Go, we don't format —
	// C99 has no canonical formatter — so we return the source
	// verbatim.
	if len(edits) == 0 {
		return source, nil
	}

	// Phase 0: plan every edit independently. Each plan carries
	// the source byte range to replace and the new text to put in
	// its place. Errors abort the whole rewrite — partial
	// application would leave the source in an unpredictable
	// intermediate state.
	plans := make([]cSplicePlan, 0, len(edits))
	for i, e := range edits {
		// Enum paths (`enum.<Name>` and `enum.<Name>.value.<V>`)
		// are C99-specific and unknown to the shared parsePath
		// grammar in rewrite.go. Intercept them here so the Go
		// router stays untouched. Everything else falls through to
		// the shared grammar + planCEdit.
		if ep, ok := parseCEnumPath(e.Path); ok {
			plan, err := planCEnumEdit(source, ep, e)
			if err != nil {
				return source, fmt.Errorf("edit %d (%s): %w", i, e.Op, err)
			}
			plans = append(plans, plan)
			continue
		}
		// Callback-type paths (`callbacktype.<Name>`, §12.3) — same
		// interception doctrine as the enum paths above: C99-specific,
		// unknown to the shared grammar, the Go router stays untouched.
		if name, ok := parseCCallbackTypePath(e.Path); ok {
			plan, err := planCCallbackTypeEdit(source, name, e)
			if err != nil {
				return source, fmt.Errorf("edit %d (%s): %w", i, e.Op, err)
			}
			plans = append(plans, plan)
			continue
		}
		// Standalone function-device paths (Slice C99-8) are also
		// C99-specific and unknown to the shared grammar.
		if fp, ok := parseCFunctionPath(e.Path); ok {
			plan, err := planCFunctionEdit(source, fp, e)
			if err != nil {
				return source, fmt.Errorf("edit %d (%s): %w", i, e.Op, err)
			}
			plans = append(plans, plan)
			continue
		}
		p, perr := parsePath(e.Path)
		if perr != nil {
			return source, fmt.Errorf("edit %d: %w", i, perr)
		}
		plan, err := planCEdit(source, p, e)
		if err != nil {
			return source, fmt.Errorf("edit %d (%s): %w", i, e.Op, err)
		}
		plans = append(plans, plan)
	}

	// Phase 1: sort descending by start offset so we can splice
	// right-to-left without invalidating earlier ranges.
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].start > plans[j].start
	})

	// Phase 2: apply splices.
	out := source
	for _, p := range plans {
		out = out[:p.start] + p.newText + out[p.end:]
	}

	return out, nil
}

// cSplicePlan describes a single byte-range replacement.
type cSplicePlan struct {
	// start and end bracket the bytes in the ORIGINAL source that
	// the splice replaces. The half-open interval [start, end) is
	// removed; newText is inserted at start.
	start int
	end   int

	// newText is the literal byte sequence that replaces the
	// [start, end) range. May be empty (deletion) or longer than
	// the original range (insertion).
	newText string
}

// planCEdit dispatches to the per-op planning function. Returns
// the splice plan ready for the right-to-left application loop.
func planCEdit(source string, p wizardPath, e WizardEdit) (cSplicePlan, error) {
	switch e.Op {
	case OpSetStructDirectives:
		if p.Kind != pathStruct {
			return cSplicePlan{}, fmt.Errorf("path must be struct.<n>")
		}
		return planCStructDirectives(source, p, e)
	case OpSetFieldProp:
		if p.Kind != pathStructField {
			return cSplicePlan{}, fmt.Errorf("path must be struct.<S>.field.<F>")
		}
		return planCFieldProp(source, p, e, false)
	case OpDisableFieldProp:
		if p.Kind != pathStructField {
			return cSplicePlan{}, fmt.Errorf("path must be struct.<S>.field.<F>")
		}
		return planCFieldProp(source, p, e, true)
	}
	return cSplicePlan{}, fmt.Errorf("unknown op %q", e.Op)
}

// ─── Op 1: setStructDirectives ────────────────────────────────────────────────

// planCStructDirectives builds a splice plan for replacing the
// leading-comment block above a struct declaration (or above the
// first function of a function-group device) with the new
// label/icon/comment payload.
//
// Args shape (matches the Go implementation):
//
//	{ "label": "Foo", "icon": "gear", "comment": "User prose..." }
//
// Empty fields are simply omitted from the rendered block.
//
// Function-group devices (Slice C99-5): the target has no struct
// in the source. The engine writes a `// device:<name>.` line as
// the first directive so the parser re-recognises the block on
// next load. The anchor for the splice is the first function of
// the group (or an existing `// device:` directive if present).
func planCStructDirectives(source string, p wizardPath, e WizardEdit) (cSplicePlan, error) {
	var args struct {
		Label   string `json:"label"`
		Icon    string `json:"icon"`
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
	}

	// Locate the target. May be a real struct, an existing
	// `// device:` directive, or a function-group anchor.
	declStart, ok := locateCStruct(source, p.Struct)
	if !ok {
		return cSplicePlan{}, fmt.Errorf("struct %q not found", p.Struct)
	}

	// Determine whether the target is a function-group device. The
	// rule: there is NO real struct of this name in the source AND no
	// forward typedef for it (a body-less opaque handle is a wire-type,
	// not a function-group). We re-run discovery because locateCStruct
	// doesn't surface that distinction in its return.
	stripped, _ := preprocessC(source)
	realStructs, _ := findAllCStructs(stripped)
	fwd := forwardTypedefStructs(stripped)
	isFunctionGroup := true
	for _, rs := range realStructs {
		if rs.Name == p.Struct {
			isFunctionGroup = false
			break
		}
	}
	if _, ok := fwd[p.Struct]; ok {
		isFunctionGroup = false
	}

	// Range of existing leading comments.
	commentStart, commentEnd := findLeadingCommentRange(source, declStart)
	indent := indentOfLine(source, declStart)

	var directives []string
	if isFunctionGroup {
		// The `// device:<name>.` line is the parser's signal that
		// the leading-comment block belongs to a function-group
		// device. Without it, the directives would be attributed
		// to the function below as method-level metadata.
		directives = append(directives, "device:"+p.Struct+".")
	}
	if args.Label != "" {
		directives = append(directives, "label:"+args.Label+".")
	}
	if args.Icon != "" {
		directives = append(directives, "icon:"+args.Icon+".")
	}

	newBlock := renderCommentBlock(args.Comment, directives, indent)

	return cSplicePlan{
		start:   commentStart,
		end:     commentEnd,
		newText: newBlock,
	}, nil
}

// ─── Op 2/3: setFieldProp / disableFieldProp ──────────────────────────────────

// planCFieldProp builds a splice plan for adding, modifying, or
// removing the `// prop:` directive above a struct field.
//
// Args shape (matches the Go implementation):
//
//	{ "label": "Gain", "default": "1", "format": "options",
//	  "formatArgs": {"options": ["0","1","2","3"]},
//	  "unit": "dB", "comment": "User prose..." }
//
// When `disable` is true, all IDS directives are stripped from the
// leading comment, leaving prose intact (if any).
func planCFieldProp(source string, p wizardPath, e WizardEdit, disable bool) (cSplicePlan, error) {
	declStart, ok := locateCField(source, p.Struct, p.Field)
	if !ok {
		return cSplicePlan{}, fmt.Errorf("field %s.%s not found", p.Struct, p.Field)
	}

	commentStart, commentEnd := findLeadingCommentRange(source, declStart)
	indent := indentOfLine(source, declStart)

	var directives []string
	var comment string

	if !disable {
		var args setFieldPropArgs
		if err := json.Unmarshal(e.Args, &args); err != nil {
			return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
		}
		if strings.TrimSpace(args.Label) == "" {
			return cSplicePlan{}, fmt.Errorf("label is required for setFieldProp")
		}
		comment = args.Comment
		directives = buildCPropDirectives(args)
	}
	// disable=true → directives stays nil; only prose survives, and
	// only if the user previously typed any.

	newBlock := renderCommentBlock(comment, directives, indent)

	return cSplicePlan{
		start:   commentStart,
		end:     commentEnd,
		newText: newBlock,
	}, nil
}

// buildCPropDirectives constructs the IDS directive line(s) for a
// `setFieldProp` edit, given the args from the wizard's Field modal.
//
// The C99 form is a single line:
//
//	prop:"Gain". default:"1". options:"0,1,2,3". unit:"dB".
//
// We emit it as ONE composite directive (not multiple) so re-parse
// + re-emit round-trips identically and so the rendered block is
// compact (one `// prop:…` line above the field, not four).
func buildCPropDirectives(args setFieldPropArgs) []string {
	var parts []string
	parts = append(parts, `prop:"`+args.Label+`".`)
	if args.Default != "" {
		parts = append(parts, `default:"`+args.Default+`".`)
	}

	switch args.Format {
	case "options":
		if raw, ok := args.FormatArgs["options"]; ok {
			var opts []string
			if err := json.Unmarshal(raw, &opts); err == nil && len(opts) > 0 {
				parts = append(parts, `options:"`+strings.Join(opts, ",")+`".`)
			}
		}
	case "range_min_max":
		minS, maxS := extractRangeArg(args.FormatArgs, "min"), extractRangeArg(args.FormatArgs, "max")
		if minS != "" && maxS != "" {
			parts = append(parts, `range:"`+minS+".."+maxS+`".`)
		}
	case "range_min":
		if v := extractRangeArg(args.FormatArgs, "min"); v != "" {
			parts = append(parts, `rangeMin:"`+v+`".`)
		}
	case "range_max":
		if v := extractRangeArg(args.FormatArgs, "max"); v != "" {
			parts = append(parts, `rangeMax:"`+v+`".`)
		}
	case "regex":
		if raw, ok := args.FormatArgs["pattern"]; ok {
			var pat string
			if err := json.Unmarshal(raw, &pat); err == nil && pat != "" {
				parts = append(parts, `regex:"`+pat+`".`)
			}
		}
	}

	if args.Unit != "" {
		parts = append(parts, `unit:"`+args.Unit+`".`)
	}

	// One composite line — easier to scan in the source.
	return []string{strings.Join(parts, " ")}
}

// extractRangeArg pulls a string-or-number range arg out of
// FormatArgs. The SPA's modal stores numbers as JSON numbers; we
// re-stringify so the rendered directive looks the same regardless
// of input type.
func extractRangeArg(args map[string]json.RawMessage, key string) string {
	raw, ok := args[key]
	if !ok {
		return ""
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Then number.
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return ""
}

// ─── Locators ──────────────────────────────────────────────────────────────────

// locateCStruct returns the byte offset where the declaration of
// struct `name` begins in the source. For an opaque handle it prefers
// the forward-typedef anchor (where the handle is documented) over the
// body. Used to place directives above a wire-type's declaration.
//
// The function NEVER returns the offset of a `// device:` directive
// line — directives are leading-comment metadata, not declarations.
// findLeadingCommentRange will reach backwards from the returned
// offset and pick up an existing directive block as the "leading
// comments" of the anchor, which is the correct behaviour for
// in-place updates.
//
// Returns (0, false) when name matches nothing.
func locateCStruct(source, name string) (int, bool) {
	stripped, _ := preprocessC(source)

	// Path 1: real struct. Prefer the forward-typedef anchor when one
	// exists, so directives land above the public interface (where an
	// opaque handle is documented) — matching the parser's doc anchor.
	rawStructs, err := findAllCStructs(stripped)
	fwd := forwardTypedefStructs(stripped)
	if err == nil {
		for _, rs := range rawStructs {
			if rs.Name == name {
				if info, ok := fwd[rs.Tag]; ok {
					return info.DeclStart, true
				}
				return rs.DeclStart, true
			}
		}
		// Body-less opaque handle: findAllCStructs didn't return it
		// (no `{ }` body in this source) — anchor on the forward
		// typedef directly.
		if info, ok := fwd[name]; ok {
			return info.DeclStart, true
		}
	}

	return 0, false
}

// locateDeviceDirective scans the source for the line
// `// device:<name>.` and returns the byte offset of the FIRST
// character of that line (start of indentation). Used by code
// that needs to know whether a `// device:` block already exists
// without needing the directive's content.
func locateDeviceDirective(source, name string) (int, bool) {
	needle := "// device:" + name + "."
	idx := 0
	for idx < len(source) {
		found := strings.Index(source[idx:], needle)
		if found < 0 {
			return 0, false
		}
		absolute := idx + found
		lineStart := absolute
		for lineStart > 0 && source[lineStart-1] != '\n' {
			lineStart--
		}
		ok := true
		for k := lineStart; k < absolute; k++ {
			if source[k] != ' ' && source[k] != '\t' {
				ok = false
				break
			}
		}
		if ok {
			return lineStart, true
		}
		idx = absolute + len(needle)
	}
	return 0, false
}

// locateCField returns the byte offset where the declaration of
// `fieldName` begins inside the body of `structName`. The offset is
// in the ORIGINAL source; use it to walk back for leading comments.
func locateCField(source, structName, fieldName string) (int, bool) {
	stripped, _ := preprocessC(source)
	rawStructs, err := findAllCStructs(stripped)
	if err != nil {
		return 0, false
	}
	for _, rs := range rawStructs {
		if rs.Name != structName {
			continue
		}
		// Walk the body looking for the field by name.
		body := source[rs.BodyStart:rs.BodyEnd]
		off, ok := findFieldDeclOffsetInBody(body, fieldName)
		if !ok {
			return 0, false
		}
		return rs.BodyStart + off, true
	}
	return 0, false
}

// findFieldDeclOffsetInBody scans a struct body for a field whose
// name matches `fieldName` and returns the offset of the first byte
// of its declaration (relative to body start). Returns (0, false)
// when not found.
//
// The scan splits the body at `;`-at-depth-0, processes each chunk
// with the same `splitFieldChunk` + `splitCFieldNameType` helpers
// the parser uses, and returns the offset of the FIRST byte AFTER
// the leading comments (i.e. the first byte of the actual
// declaration).
func findFieldDeclOffsetInBody(body, fieldName string) (int, bool) {
	clean, _ := preprocessC(body)
	pos := 0
	for pos < len(clean) {
		semi, ok := findStatementEnd(clean, pos)
		if !ok {
			break
		}
		chunk := body[pos:semi]
		commentLines, decl := splitFieldChunk(chunk)
		decl = strings.TrimSpace(decl)
		if decl == "" {
			pos = semi + 1
			continue
		}
		name, _ := splitCFieldNameType(decl)
		if name == fieldName {
			// Found. The declaration starts AFTER any leading
			// comment lines. Walk through `chunk` line-by-line to
			// find the first non-`//`/non-blank line — that's the
			// declaration line.
			lineIdx := 0
			cur := pos
			for _, line := range strings.Split(chunk, "\n") {
				t := strings.TrimSpace(line)
				if t == "" || strings.HasPrefix(t, "//") {
					cur += len(line) + 1 // +1 for the newline we split on
					lineIdx++
					continue
				}
				return cur, true
			}
			_ = commentLines
			_ = lineIdx
			return pos, true
		}
		pos = semi + 1
	}
	return 0, false
}

// findParamsStart returns the byte offset just AFTER the `(` of
// the function declaration that starts at `declStart`. Returns -1
// when the `(` is not found within a reasonable distance.
func findParamsStart(source string, declStart int) int {
	for i := declStart; i < len(source) && i < declStart+512; i++ {
		if source[i] == '(' {
			return i + 1
		}
	}
	return -1
}

// ─── Leading-comment range + rendering ─────────────────────────────────────────

// findLeadingCommentRange returns the byte range [start, end) in
// the source that holds the contiguous leading comment block above
// `targetByteOffset`. When no leading comments exist, both values
// equal the start of the target's line (so the splice becomes a
// pure insertion above the target).
//
// "Contiguous" means: walking backwards line by line, every line is
// either blank (one blank line is tolerated WITHIN the block as
// separator between prose and directives), a `//` line, or the end
// of a `/* */` block. The first non-comment non-blank line
// terminates the streak.
//
// The returned `start` always points at the FIRST byte of a line
// (i.e. the byte right after the previous '\n' or 0). This makes
// the splice clean — replacing a whole-line range never partially
// overwrites another line.
func findLeadingCommentRange(source string, targetByteOffset int) (int, int) {
	// Snap to the start of the target's line.
	end := targetByteOffset
	for end > 0 && source[end-1] != '\n' {
		end--
	}
	// `end` now points at the start of the target line — also our
	// no-comments-found fallback start.
	start := end

	cur := end
	for cur > 0 {
		// Previous line.
		prevLineEnd := cur - 1 // '\n'
		prevLineStart := prevLineEnd
		for prevLineStart > 0 && source[prevLineStart-1] != '\n' {
			prevLineStart--
		}
		lineText := source[prevLineStart:prevLineEnd]
		trimmed := strings.TrimSpace(lineText)

		if trimmed == "" {
			break
		}

		if strings.HasPrefix(trimmed, "//") {
			start = prevLineStart
			cur = prevLineStart
			continue
		}

		// Could be the end of a /* */ block. Walk back to the
		// matching /* to absorb the whole block.
		if strings.HasSuffix(trimmed, "*/") {
			// Find the line containing the matching /*.
			openOff := strings.LastIndex(source[:prevLineEnd], "/*")
			if openOff < 0 || openOff < prevLineStart-1 && prevLineStart > 0 {
				// Single-line /* … */ on prevLine.
				if strings.HasPrefix(trimmed, "/*") {
					start = prevLineStart
					cur = prevLineStart
					continue
				}
			}
			if openOff >= 0 {
				openLineStart := openOff
				for openLineStart > 0 && source[openLineStart-1] != '\n' {
					openLineStart--
				}
				start = openLineStart
				cur = openLineStart
				continue
			}
			break
		}

		break
	}

	return start, end
}

// indentOfLine returns the leading whitespace of the line that
// contains `byteOffset`. Used to keep inserted comment lines
// aligned with the declaration they sit above.
func indentOfLine(source string, byteOffset int) string {
	lineStart := byteOffset
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	end := lineStart
	for end < len(source) && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	return source[lineStart:end]
}

// renderCommentBlock builds the byte sequence that replaces an
// existing leading-comment range. Each output line gets the indent
// prefix, then `// `, then the content. The block ends with a
// newline so the next line (the declaration itself) starts cleanly.
//
// Layout:
//
//	<indent>// <prose line 1>
//	<indent>// <prose line 2>
//	<indent>//
//	<indent>// <directive 1>
//	<indent>// <directive 2>
//
// When `prose` is empty, the separator line is omitted. When the
// resulting block is empty (no prose, no directives), the function
// returns the empty string — letting the splice REMOVE the
// existing leading comments entirely. That matches the
// `disableFieldProp` semantics (remove the prop annotation but
// don't introduce blank comment lines just to have a leading
// block).
func renderCommentBlock(prose string, directives []string, indent string) string {
	prose = strings.TrimSpace(prose)
	if prose == "" && len(directives) == 0 {
		return ""
	}

	var b strings.Builder
	if prose != "" {
		for _, line := range strings.Split(prose, "\n") {
			b.WriteString(indent)
			b.WriteString("// ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if prose != "" && len(directives) > 0 {
		// Separator line between prose and directives.
		b.WriteString(indent)
		b.WriteString("//\n")
	}
	for _, d := range directives {
		b.WriteString(indent)
		b.WriteString("// ")
		b.WriteString(d)
		b.WriteByte('\n')
	}
	return b.String()
}
