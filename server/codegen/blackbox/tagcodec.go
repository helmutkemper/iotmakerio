// server/codegen/blackbox/tagcodec.go — Order-preserving codec for Go struct tags.
//
// Why this file exists
// ====================
//
// Slice 1 of the device wizard rewrites struct fields' tags in response
// to user input from the Field modal. The rewrite must follow two rules
// inherited from CLAUDE_WIZARD_DESIGN.md §5.1:
//
//	(1) Existing tag keys are preserved verbatim. A field arriving with
//	    `json:"x" yaml:"x"` keeps both keys, in that order, after any
//	    IDS edit.
//	(2) Only IDS-owned keys are touched (prop, default, options, range,
//	    range_min, range_max, regex, unit, encoding, bits, inputRegex,
//	    connection). User-owned keys round-trip exactly.
//
// The standard library's `reflect.StructTag` is read-only — `Get(key)`
// is the only useful operation. A round-trip codec is the missing piece;
// this file supplies it. The codec is small (≈100 lines) but earns its
// keep by being the ONE place that knows the canonical tag format —
// every other call site uses parseStructTag/emitStructTag and never has
// to touch quoting or escape rules.
//
// Tag grammar (per https://pkg.go.dev/reflect#StructTag)
// =====================================================
//
//	tag      = pair { whitespace pair }
//	pair     = key ":" quoted-string
//	key      = run of non-quote, non-colon, non-space, non-control chars
//	quoted   = `"` { any-byte | escape } `"`
//	escape   = `\\` followed by one of n,t,r,",\
//
// We tolerate space-or-tab separation between pairs (gofmt produces a
// single space, but hand-written code occasionally aligns with tabs).
//
// Quoting and escaping
// ====================
//
// Values are decoded with strconv.Unquote when read and re-encoded with
// strconv.Quote when written. This means a value containing a literal
// double-quote (rare but legal) survives the round trip via the standard
// `\"` escape. IDS values in practice never contain quotes, but baking
// proper Go-string semantics in once costs almost nothing and removes a
// whole class of corner-case bugs.
package blackbox

import (
	"fmt"
	"strconv"
	"strings"
)

// tagPair is one (key, value) entry in a struct tag, kept in original
// position order. Value is the *decoded* string; quoting and escaping
// are concerns of parseStructTag/emitStructTag, never of callers.
type tagPair struct {
	Key   string
	Value string
}

// parseStructTag parses the contents of a Go struct tag (the substring
// between the surrounding backticks) into an ordered slice of pairs.
//
// Returns an error on any malformed input — missing colon, unbalanced
// quote, invalid escape — so the caller never sees half-parsed data.
// An empty input string returns (nil, nil).
func parseStructTag(s string) ([]tagPair, error) {
	var out []tagPair
	i := 0
	for i < len(s) {
		// Skip leading whitespace between pairs.
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}

		// Read key: any run of bytes up to the next ':' or whitespace.
		// Per the reflect package's documentation, keys are restricted
		// (no spaces, no quotes, no control chars), but we do the lenient
		// thing here — let the colon do the work.
		keyStart := i
		for i < len(s) && s[i] != ':' && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		if i >= len(s) || s[i] != ':' {
			return nil, fmt.Errorf("malformed struct tag: missing colon after %q", s[keyStart:i])
		}
		key := s[keyStart:i]
		if key == "" {
			return nil, fmt.Errorf("malformed struct tag: empty key at offset %d", keyStart)
		}
		i++ // consume ':'

		// Read quoted value with proper escape handling.
		if i >= len(s) || s[i] != '"' {
			return nil, fmt.Errorf("malformed struct tag: missing quoted value for key %q", key)
		}
		valStart := i
		i++ // consume opening '"'
		for i < len(s) {
			if s[i] == '\\' && i+1 < len(s) {
				// Skip the escape sequence wholesale; strconv.Unquote
				// will validate it.
				i += 2
				continue
			}
			if s[i] == '"' {
				i++ // include closing '"' in the slice
				break
			}
			i++
		}
		raw := s[valStart:i]
		if len(raw) < 2 || raw[len(raw)-1] != '"' {
			return nil, fmt.Errorf("malformed struct tag: unterminated quoted value for key %q", key)
		}
		decoded, err := strconv.Unquote(raw)
		if err != nil {
			return nil, fmt.Errorf("malformed struct tag: invalid value for key %q: %w", key, err)
		}
		out = append(out, tagPair{Key: key, Value: decoded})
	}
	return out, nil
}

// emitStructTag renders an ordered list of pairs back to the canonical
// gofmt form: single-space separated, double-quoted values produced via
// strconv.Quote so that any embedded special character round-trips.
//
// The result does NOT include surrounding backticks; the caller wraps
// it in `…` when assigning to *ast.Field.Tag.Value.
func emitStructTag(pairs []tagPair) string {
	if len(pairs) == 0 {
		return ""
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.Key + ":" + strconv.Quote(p.Value)
	}
	return strings.Join(parts, " ")
}

// idsOwnedKeys is the set of struct-tag keys the wizard manages on
// behalf of the IoTMaker Doc Standard. Adding a key here means the
// wizard can write or remove it on a setFieldProp / disableFieldProp
// edit; removing a key here makes it user-owned and untouchable. The
// list mirrors CLAUDE_WIZARD_DESIGN.md §5.1 (2). Keep them in sync.
var idsOwnedKeys = map[string]struct{}{
	"prop":       {},
	"default":    {},
	"options":    {},
	"range":      {},
	"range_min":  {},
	"range_max":  {},
	"regex":      {},
	"unit":       {},
	"encoding":   {},
	"bits":       {},
	"inputRegex": {},
	"connection": {},
}

// upsertIDSKeys builds the new pair list by:
//
//  1. Dropping every IDS-owned key from the existing pairs.
//  2. Appending the supplied IDS pairs in the order given.
//
// Non-IDS keys are preserved in their original position. The new IDS
// keys land after every preserved non-IDS key — this yields the
// minimum-diff result for the common case of a field that already has
// `json:"x"` and is gaining a `prop:"…"` for the first time: the diff
// is a pure append.
func upsertIDSKeys(existing []tagPair, idsPairs []tagPair) []tagPair {
	out := make([]tagPair, 0, len(existing)+len(idsPairs))
	for _, p := range existing {
		if _, isIDS := idsOwnedKeys[p.Key]; !isIDS {
			out = append(out, p)
		}
	}
	out = append(out, idsPairs...)
	return out
}

// removeIDSKeys returns a copy of pairs with every IDS-owned key
// dropped. Used by the disableFieldProp operation, which strips the
// wizard's tags but keeps any user-owned keys (e.g. `json:"x"`).
func removeIDSKeys(pairs []tagPair) []tagPair {
	out := make([]tagPair, 0, len(pairs))
	for _, p := range pairs {
		if _, isIDS := idsOwnedKeys[p.Key]; isIDS {
			continue
		}
		out = append(out, p)
	}
	return out
}
