// server/codegen/blackbox/type_shape.go — Decompose a Go type string
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// into the (container, keyType, valueType, nativeKey, nativeValue)
// tuple that PropDef carries on the wire.
//
// Why this lives in its own file:
//
// extractProps is already long. The Go-type-string parsing logic is
// useful in isolation (tests can hammer it without setting up an
// AST), and putting it next to IsNativePropType (completion.go)
// would couple two unrelated concerns in the same file. The two
// callers — extractProps for tagged paths, future code for slices —
// reach into here.
//
// The grammar this file recognises is intentionally narrow:
//
//   <native-type>           → Container=""
//   map[<token>]<typeStr>   → Container="map"
//   []<typeStr>             → Container="slice"
//   anything else           → Container="" (treated as opaque scalar)
//
// "anything else" includes pointers, channels, qualified types
// (machine.I2C), generic instantiations (List[int]), arrays with
// fixed size, function types, interface{}, etc. The renderer leaves
// those inert; the parser still emits the PropDef when the field
// has a `prop:` tag, so the specialist's explicit choice survives.
//
// Regex is avoided on purpose — Go type strings have a very simple
// prefix grammar (`map[`, `[]`) and a hand-rolled parser is faster,
// allocation-free, and easier to audit.

package blackbox

import "strings"

// analyseGoType inspects a Go type string and returns the shape
// fields used by PropDef.
//
//	container   — "" for scalars, "map", or "slice"
//	keyType     — the K of map[K]V, empty otherwise
//	valueType   — the V of map[K]V, or T of []T, empty for scalars
//	nativeKey   — IsNativePropType(keyType) — false when empty
//	nativeValue — IsNativePropType(valueType) — false when empty
//
// All returned strings are trimmed of surrounding whitespace.
func analyseGoType(goType string) (container, keyType, valueType string, nativeKey, nativeValue bool) {
	t := strings.TrimSpace(goType)

	// Map prefix: "map[K]V" — find the matching ']' starting from
	// position 4. Bracket depth tracking handles nested maps
	// (map[string]map[string]int) — the outermost K parsing stops
	// at the depth-0 ']'. Today the renderer only supports flat
	// map[K]V, but parsing is honest so a nested type produces a
	// valid Container/KeyType/ValueType tuple even if the renderer
	// later refuses to draw it.
	if strings.HasPrefix(t, "map[") {
		depth := 1
		closeIdx := -1
		for i := 4; i < len(t) && closeIdx < 0; i++ {
			switch t[i] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					closeIdx = i
				}
			}
		}
		if closeIdx > 4 && closeIdx < len(t)-1 {
			keyType = strings.TrimSpace(t[4:closeIdx])
			valueType = strings.TrimSpace(t[closeIdx+1:])
			container = "map"
			nativeKey = IsNativePropType(keyType)
			nativeValue = IsNativePropType(valueType)
			return
		}
		// Malformed `map[...` without a closing bracket — fall
		// through to the scalar branch. Should never happen for
		// type strings produced by typeString() (they come from
		// the AST and are always balanced), but defensive.
	}

	// Slice prefix: "[]T". Distinguished from a fixed-size array
	// "[N]T" — the latter starts with a digit after the bracket.
	if strings.HasPrefix(t, "[]") {
		valueType = strings.TrimSpace(t[2:])
		if valueType != "" {
			container = "slice"
			nativeValue = IsNativePropType(valueType)
			return
		}
	}

	// Anything else is "scalar" from the renderer's point of view.
	// IsNativePropType(t) decides whether the renderer will draw an
	// input or leave the row inert; that check is done by the
	// existing NativeType field on PropDef, not here.
	return "", "", "", false, false
}
