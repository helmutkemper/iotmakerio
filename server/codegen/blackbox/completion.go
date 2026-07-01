// server/codegen/blackbox/completion.go — Single source of truth for ⚠.
//
// Why this file exists
// ====================
//
// The wizard's main affordance is the ⚠ badge: every entity (struct,
// method, prop, port) shows one when its mandatory configuration is
// missing. Multiple places need to know which entities are incomplete:
//
//   - The card list in the wizard tab (slice 3) renders ⚠ on each row.
//   - The publish gate (slice 8) refuses to push when the set is non-empty.
//   - The draft validator (slice 3) recomputes the set on every save and
//     compares against the value posted by the client to detect tampering.
//
// Letting each consumer decide for itself would invite drift. Per
// CLAUDE_WIZARD_DESIGN.md §6.3 the rule is one function, server-side,
// run on every parse and rewrite. The client never recomputes — it
// consults the result.
//
// The function returns a sorted slice of dotted paths so the caller has
// a stable order to render and so deep-equality on []string just works
// for the validator. Internally we use a set to dedupe, then flatten.
//
// What "complete" means (per §6.2 + slice-6 addendum)
// =====================================================
//
//	struct.<n>                 — label and icon required.
//	struct.<S>.field.<F>          — label and default required, but ONLY
//	                                for native-typed fields. Non-native
//	                                fields (pointers, qualified types,
//	                                slices, channels, etc.) are inert
//	                                per §6.2 — they have no UI and no
//	                                ⚠. Disabled fields (no `prop:` tag
//	                                at all) never reach def.Props from
//	                                the parser and so do not appear here.
//	method.<S>.<M>                — label and icon required.
//	method.<S>.<M>.in|out.<n>     — label AND comment required for ALL
//	                                ports (including errors). Non-error
//	                                ports additionally require the
//	                                connection: tag.
//
// Init counts as a method for these rules. The Go fallback "if Label is
// empty, use the method name" is a runtime convenience for the IDE; the
// wizard wants explicit labels so reviewers can tell at a glance whether
// the specialist deliberately chose the displayed name.
//
// Ports
// =====
//
// Slice-6 unified the port rules. Every named port — input or output,
// regular or error — needs Label and Doc (Comment) set. Non-error
// ports additionally need a `connection:` tag (PortDef.MissingConn ==
// false). Error returns skip the connection check by IDS-spec exemption
// but DO need Label and Comment, since both surface in the IDE pin
// tooltip and in the function's godoc. Anonymous parameters (no Name)
// are skipped because no wizard path can address them.
package blackbox

import (
	"sort"
	"strings"
)

// nativePropTypes lists the Go types the wizard knows how to render
// inside the Field modal. The parser excludes non-native types from
// PropDef before they ever reach this file, so this set is exported
// only for callers that need to mirror the wizard's notion of
// "configurable type" — e.g. the Field modal in slice 4.
//
// Adding a new native type here means: (a) extend this map, (b) update
// the modal's editor in slice 4, (c) update the IDS readme.
var nativePropTypes = map[string]struct{}{
	"bool":    {},
	"byte":    {},
	"rune":    {},
	"int":     {},
	"int8":    {},
	"int16":   {},
	"int32":   {},
	"int64":   {},
	"uint":    {},
	"uint8":   {},
	"uint16":  {},
	"uint32":  {},
	"uint64":  {},
	"float32": {},
	"float64": {},
	"string":  {},
}

// IsNativePropType reports whether goType is one of the simple Go
// types the wizard exposes a Field UI for. Pointers (`*X`), slices,
// maps, channels, and qualified types (`machine.I2C`, `time.Duration`)
// are not native — fields with such types are inert in the wizard.
//
// Whitespace is trimmed before comparison so callers can pass a raw
// type string straight from the parser without prior cleanup.
func IsNativePropType(goType string) bool {
	_, ok := nativePropTypes[strings.TrimSpace(goType)]
	return ok
}

// ComputeIncomplete returns the sorted list of dotted paths that are
// not yet fully configured in def. The result is the wizard's single
// source of truth for ⚠ rendering and the publish-gate check.
//
// The function is pure and concurrency-safe. A nil def returns an empty
// (non-nil) slice so callers can range over the result without a nil
// guard. JSON marshalling produces "[]" for the empty case, never
// "null" — the slice is allocated explicitly.
func ComputeIncomplete(def *BlackBoxDef) []string {
	if def == nil {
		return []string{}
	}
	set := map[string]struct{}{}

	// Struct-level: a Go device-struct needs both label and icon.
	// Guarded by a non-empty Name: in the C99 device-per-function
	// model there is NO primary struct (Name is empty, structs become
	// wire-types), so emitting "struct." there would be a spurious
	// path with no card — and would wedge the publish gate. A Go
	// BlackBox always has a named struct, so this guard never changes
	// Go behaviour.
	if def.Name != "" && (def.StructLabel == "" || def.StructIcon == "") {
		set["struct."+def.Name] = struct{}{}
	}

	// Field-level: each prop the parser exposed needs evaluation in
	// one of three categories — see PropDef docs for the full
	// taxonomy. The wizard rendering rules:
	//
	//   - Untagged & non-native (e.g. *machine.I2C without a tag):
	//     inert row. NEVER incomplete; the wizard does not generate
	//     UI for non-native types and the field's intent is up to
	//     the specialist to express in code, not in the wizard.
	//
	//   - Untagged & native (e.g. ATime byte without a tag):
	//     ALWAYS incomplete — the field is exposed but the user has
	//     not yet decided whether it's a configurable prop. Saving
	//     the modal adds the prop:"..." tag and brings the field
	//     into the tagged category.
	//
	//   - Tagged (e.g. Gain byte `prop:"ADC Gain" default:"0"`):
	//     incomplete iff Label is empty. Default is intentionally
	//     optional — the specialist may rely on Go's zero value
	//     and/or want `if x == nil` to be true for slice/map
	//     props. Non-native tagged props are an authoring oddity
	//     (the spec discourages them) but treated the same way —
	//     if the author tagged it, they meant for it to be
	//     configurable.
	//
	// Anonymous (no name) fields are filtered out by extractProps and
	// never reach here, but a defensive check stays correct.
	for _, p := range def.Props {
		if p.FieldName == "" {
			continue
		}
		path := "struct." + def.Name + ".field." + p.FieldName

		if p.Untagged {
			if p.NativeType {
				// Untagged native — incomplete by definition.
				set[path] = struct{}{}
			}
			// Untagged non-native — inert, never incomplete.
			continue
		}

		// Tagged — needs label. Default is intentionally optional:
		// omitting it lets Go's zero value drive the field at
		// runtime, which is a valid design choice (e.g. user code
		// may want `if x == nil` to be true for an unconfigured
		// slice/map prop). The wizard's ⚠ marker is reserved for
		// things that actually break code generation or block
		// runtime use; a missing default is neither.
		if p.Label == "" {
			set[path] = struct{}{}
		}
	}

	// Method-level: Init plus every named method.
	if def.Init != nil {
		addMethodIncomplete(set, def.Name, "Init", def.Init)
	}
	for i := range def.Methods {
		// Indexing rather than ranging by value avoids copying the
		// FuncDef once per iteration; the slice is small in practice
		// but the cost-free habit is worth keeping.
		m := &def.Methods[i]
		addMethodIncomplete(set, def.Name, m.Name, &m.FuncDef)
	}

	// ─── C99 device-per-function model ──────────────────────────────────────
	//
	// Additive and disjoint from the Go path above: the Go parser never
	// populates Functions/Enums/WireTypes, and the C99 parser never
	// populates Methods/Init/Props or a struct Name. These three loops
	// mirror the SPA's client-side rules (_functionDeviceIncomplete,
	// _enumDeviceIncomplete, _wireTypeIncomplete) so the publish gate
	// and the wizard ⚠ rows agree on what "incomplete" means.

	// Function devices: each needs a label AND an icon, and each port
	// must satisfy the C99 port rule (see portIncompleteC99). Paths
	// match the SPA's: function.<n> for the device, function.<n>.in|out.<p>
	// for ports.
	for i := range def.Functions {
		fn := &def.Functions[i]
		base := "function." + fn.Name
		if fn.Label == "" || fn.Icon == "" {
			set[base] = struct{}{}
		}
		for j := range fn.Inputs {
			p := &fn.Inputs[j]
			if portIncompleteC99(p) {
				set[base+".in."+p.Name] = struct{}{}
			}
		}
		for j := range fn.Outputs {
			p := &fn.Outputs[j]
			// Synthetic callback reference output: a handler's `callback`
			// pin (produced by `// callback:<type>.`) carries CallbackType
			// and has NO backing parameter in the source, so it is not
			// editable — it has no label/comment/connection of its own to
			// complete and can never be satisfied. Exempt it from the
			// incomplete set, mirroring the client's _functionPortMissing
			// and the `return` exemption inside portIncompleteC99. (A
			// callback INPUT also carries CallbackType but IS a real
			// parameter; it is handled in the input loop above and stays
			// subject to the label/comment/connection rules.)
			if p.CallbackType != "" {
				continue
			}
			if portIncompleteC99(p) {
				set[base+".out."+p.Name] = struct{}{}
			}
		}
	}

	// Enums: incomplete iff any value lacks a label. The enum's own
	// label/icon are optional (the maker only ever picks among the
	// values), mirroring the SPA's _enumDeviceIncomplete.
	for i := range def.Enums {
		e := &def.Enums[i]
		for j := range e.Values {
			v := &e.Values[j]
			if v.Name != "" && v.Label == "" {
				set["enum."+e.Name+".value."+v.Name] = struct{}{}
			}
		}
	}

	// Wire-types (opaque handles carried on a wire): need a label AND
	// an icon — the same visual-identity bar as a device. They have no
	// ports, so that is the whole check.
	for i := range def.WireTypes {
		wt := &def.WireTypes[i]
		if wt.Label == "" || wt.Icon == "" {
			set["wiretype."+wt.Name] = struct{}{}
		}
	}

	// Flatten to a sorted slice. sort.Strings does the lex order; this
	// matters for deep-equality comparisons in the draft validator and
	// for the rendering order of ⚠ rows in the wizard tab.
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// addMethodIncomplete writes the method's own incompleteness paths and
// every port path that fails the wizard's port-completeness rules
// into `set`. Split out so Init and the named methods share the same
// logic without duplicating the loop body.
//
// Per-port rules (slice 6+ semantics, refined again in slice-7
// addendum: outputs are always optional connection-wise):
//
//   - Every named port needs Label.
//   - Every named port needs Doc (Comment).
//   - INPUTS additionally need a `connection:` tag (i.e.
//     MissingConn must be false). The wizard forces the specialist
//     to declare optional/mandatory deliberately.
//   - OUTPUTS (regular and error) skip the connection check
//     entirely. There is no semantic for "this output must be
//     wired" — the method computes the value either way and the
//     caller is free to ignore it. Mandatory on outputs would
//     make wiring decisions affect control flow, which is wrong.
//     Error returns were already exempt; this slice extends that
//     exemption to all outputs.
//
// "Anonymous" ports (no Name) are skipped because the wizard cannot
// surface a path to address them.
func addMethodIncomplete(set map[string]struct{}, structName, methodName string, fd *FuncDef) {
	methodPath := "method." + structName + "." + methodName
	if fd.Label == "" || fd.Icon == "" {
		set[methodPath] = struct{}{}
	}
	for i := range fd.Inputs {
		port := &fd.Inputs[i]
		if portIncomplete(port, "in") {
			set[methodPath+".in."+port.Name] = struct{}{}
		}
	}
	for i := range fd.Outputs {
		port := &fd.Outputs[i]
		if portIncomplete(port, "out") {
			set[methodPath+".out."+port.Name] = struct{}{}
		}
	}
}

// portIncomplete reports whether a port is incomplete under the
// uniform rules described in addMethodIncomplete. The `dir`
// argument selects between input ("in") and output ("out") rules:
// outputs never require a connection: tag.
//
// Anonymous ports (empty Name) are never reported because no
// wizard path can address them.
func portIncomplete(p *PortDef, dir string) bool {
	if p.Name == "" {
		return false
	}
	if p.Label == "" {
		return true
	}
	if p.Doc == "" {
		return true
	}
	// Connection: applies to inputs only. Outputs (regular or
	// error) skip the check — there is no "mandatory output"
	// semantic. See the addMethodIncomplete docstring for the
	// reasoning.
	if dir == "in" && p.MissingConn {
		return true
	}
	return false
}

// portIncompleteC99 reports whether a C99 function-device port is
// incomplete. It mirrors the SPA's _functionPortMissing and differs
// from the Go rule (portIncomplete) in one key way: EVERY parameter —
// input OR output — needs a connection choice, because direction is
// only how the pin is drawn; in the generated call the parameter still
// takes an argument either way. The single exception is the synthetic
// `return`, which is the return VALUE (not a parameter): it needs only
// a label, never a comment or connection.
//
// Anonymous ports (empty Name) are never reported — no wizard path can
// address them.
func portIncompleteC99(p *PortDef) bool {
	if p.Name == "" {
		return false
	}
	if p.Name == "return" {
		return p.Label == ""
	}
	if p.Label == "" {
		return true
	}
	if p.Doc == "" {
		return true
	}
	return p.MissingConn
}
