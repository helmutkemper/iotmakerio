package blackbox

// rewrite_c_enum.go — Rewrite handlers for C99 enum type devices
// (Slice C99-6, §12.2).
//
// Two edit targets, both reusing the OpSetStructDirectives op:
//
//   - `enum.<Name>`              → icon/label of the whole enum,
//                                   written as a leading-comment
//                                   directive block above the
//                                   `typedef enum` / `enum` keyword.
//                                   Same shape as a struct's
//                                   directives (Decision 2,
//                                   2026-05-20) but WITHOUT a
//                                   `// device:` line — an enum is
//                                   not a function-group.
//
//   - `enum.<Name>.value.<V>`    → the per-enumerator label,
//                                   written as a `// label:…`
//                                   leading-comment line above the
//                                   enumerator (Decision 1A).
//
// Both are byte-level splices that reuse findLeadingCommentRange,
// indentOfLine, and renderCommentBlock, so they round-trip with the
// parser exactly like every other C99 edit.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// cEnumPath is the parsed form of an enum rewrite path. Value is
// empty for an enum-level edit and non-empty for a value-level one.
type cEnumPath struct {
	Enum  string
	Value string
}

// parseCEnumPath recognises the two enum path shapes. Returns
// (path, true) on a match, ({}, false) otherwise so the caller can
// fall through to the shared grammar.
func parseCEnumPath(s string) (cEnumPath, bool) {
	parts := strings.Split(s, ".")
	switch {
	case len(parts) == 2 && parts[0] == "enum" && parts[1] != "":
		return cEnumPath{Enum: parts[1]}, true
	case len(parts) == 4 && parts[0] == "enum" && parts[2] == "value" &&
		parts[1] != "" && parts[3] != "":
		return cEnumPath{Enum: parts[1], Value: parts[3]}, true
	}
	return cEnumPath{}, false
}

// planCEnumEdit dispatches an enum path to the enum-level or
// value-level planner. Only OpSetStructDirectives is supported —
// enums have no fields, methods, or ports.
func planCEnumEdit(source string, ep cEnumPath, e WizardEdit) (cSplicePlan, error) {
	if e.Op != OpSetStructDirectives {
		return cSplicePlan{}, fmt.Errorf(
			"enum paths only support setStructDirectives, got %q", e.Op)
	}
	if ep.Value == "" {
		return planCEnumDirectives(source, ep.Enum, e)
	}
	return planCEnumValueLabel(source, ep.Enum, ep.Value, e)
}

// planCEnumDirectives replaces the leading-comment block above an
// enum declaration with the new icon/label/comment payload. Unlike
// planCStructDirectives it never emits a `// device:` line.
func planCEnumDirectives(source, enumName string, e WizardEdit) (cSplicePlan, error) {
	var args struct {
		Label   string `json:"label"`
		Icon    string `json:"icon"`
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
	}

	declStart, ok := locateCEnum(source, enumName)
	if !ok {
		return cSplicePlan{}, fmt.Errorf("enum %q not found", enumName)
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

	newBlock := renderCommentBlock(args.Comment, directives, indent)
	return cSplicePlan{start: commentStart, end: commentEnd, newText: newBlock}, nil
}

// planCEnumValueLabel replaces the leading-comment block above a
// single enumerator with a `// label:…` line. An empty label clears
// the directive (the splice removes the existing block and inserts
// nothing), which moves the enumerator back to the "incomplete"
// state — useful when the user blanks a label.
func planCEnumValueLabel(source, enumName, valueName string, e WizardEdit) (cSplicePlan, error) {
	var args struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(e.Args, &args); err != nil {
		return cSplicePlan{}, fmt.Errorf("invalid args: %w", err)
	}

	declStart, ok := locateCEnumValue(source, enumName, valueName)
	if !ok {
		return cSplicePlan{}, fmt.Errorf("enum value %s.%s not found", enumName, valueName)
	}

	commentStart, commentEnd := findLeadingCommentRange(source, declStart)
	indent := indentOfLine(source, declStart)

	var directives []string
	if args.Label != "" {
		directives = append(directives, "label:"+args.Label+".")
	}

	newBlock := renderCommentBlock("", directives, indent)
	return cSplicePlan{start: commentStart, end: commentEnd, newText: newBlock}, nil
}

// locateCEnum returns the byte offset where the declaration of enum
// `name` begins (the `typedef` or `enum` keyword). Matches against
// the resolved Name, the tag, and the alias so a signature that
// uses the alias still resolves the right enum.
func locateCEnum(source, name string) (int, bool) {
	stripped, _ := preprocessC(source)
	for _, re := range findAllCEnums(stripped) {
		if re.Name == name || re.Tag == name || re.Alias == name {
			return re.DeclStart, true
		}
	}
	return 0, false
}

// locateCEnumValue returns the byte offset of the enumerator
// `valueName` inside enum `enumName`.
func locateCEnumValue(source, enumName, valueName string) (int, bool) {
	stripped, _ := preprocessC(source)
	for _, re := range findAllCEnums(stripped) {
		if re.Name != enumName && re.Tag != enumName && re.Alias != enumName {
			continue
		}
		for _, v := range re.Values {
			if v.Name == valueName {
				return v.DeclStart, true
			}
		}
	}
	return 0, false
}
