// server/codegen/blackbox/type_shape_test.go — Tests for the
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// analyseGoType helper that decomposes Go type strings into the
// (container, keyType, valueType, nativeKey, nativeValue) tuple
// that PropDef carries on the wire.
//
// Coverage:
//
//   - Scalar (native): empty container, empty K/V, native flags false
//   - Scalar (non-native qualified type): same as above
//   - map[string]string: container=map, both natives true
//   - map[string]int / float64 / bool: same
//   - map with non-native value: NativeValue=false
//   - map with non-native key: NativeKey=false
//   - nested map[string]map[string]int: handled by depth tracker
//   - []int / []string: container=slice, NativeValue true
//   - []*foo.Bar: container=slice, NativeValue=false
//   - [...]int (fixed array): NOT recognised as slice — empty container
//   - "interface{}", "func() error", "chan int": empty container

package blackbox

import "testing"

type analyseExp struct {
	in          string
	container   string
	keyType     string
	valueType   string
	nativeKey   bool
	nativeValue bool
}

func TestAnalyseGoType(t *testing.T) {
	cases := []analyseExp{
		// Scalars — container empty, K/V empty, both flags false.
		{"", "", "", "", false, false},
		{"byte", "", "", "", false, false},
		{"int", "", "", "", false, false},
		{"string", "", "", "", false, false},
		{"*machine.I2C", "", "", "", false, false},
		{"machine.Pin", "", "", "", false, false},
		{"interface{}", "", "", "", false, false},
		{"chan int", "", "", "", false, false},

		// Map with native scalar K and V.
		{"map[string]string", "map", "string", "string", true, true},
		{"map[string]int", "map", "string", "int", true, true},
		{"map[string]int64", "map", "string", "int64", true, true},
		{"map[string]bool", "map", "string", "bool", true, true},
		{"map[string]float64", "map", "string", "float64", true, true},
		{"map[string]byte", "map", "string", "byte", true, true},
		{"map[int]string", "map", "int", "string", true, true},

		// Map where the value is not native — Slice 2.2 will refuse
		// to render this, but the parser must still emit the shape.
		{"map[string]*machine.I2C", "map", "string", "*machine.I2C", true, false},
		{"map[string]machine.Pin", "map", "string", "machine.Pin", true, false},

		// Map where the key is not native (rare, possible in Go).
		// Slice 2.2 refuses, but the shape is honest.
		{"map[*foo.Bar]string", "map", "*foo.Bar", "string", false, true},

		// Nested map: outer is map, K=string, V="map[string]int" (a
		// composite that the renderer can still ignore — the shape
		// nesting is the renderer's problem, not the parser's).
		{"map[string]map[string]int", "map", "string", "map[string]int", true, false},

		// Slice — container=slice, KeyType always empty.
		{"[]int", "slice", "", "int", false, true},
		{"[]string", "slice", "", "string", false, true},
		{"[]byte", "slice", "", "byte", false, true},

		// Slice of non-native: ValueType set, NativeValue false.
		{"[]*foo.Bar", "slice", "", "*foo.Bar", false, false},
		{"[]machine.Pin", "slice", "", "machine.Pin", false, false},

		// Fixed-size array NOT recognised as slice — typeString()
		// produces "[...]T" for arrays. We treat it as scalar so
		// the renderer leaves it inert.
		{"[...]int", "", "", "", false, false},

		// Whitespace tolerance.
		{"  string  ", "", "", "", false, false},
		{"  map[string]int  ", "map", "string", "int", true, true},
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			container, keyType, valueType, nk, nv := analyseGoType(c.in)
			if container != c.container || keyType != c.keyType || valueType != c.valueType ||
				nk != c.nativeKey || nv != c.nativeValue {
				t.Errorf("analyseGoType(%q) = (%q, %q, %q, %v, %v); want (%q, %q, %q, %v, %v)",
					c.in,
					container, keyType, valueType, nk, nv,
					c.container, c.keyType, c.valueType, c.nativeKey, c.nativeValue)
			}
		})
	}
}

// ─── End-to-end via Parse: PropDef carries the new fields ────────────────────

func TestExtractProps_TaggedMap_PopulatesContainer(t *testing.T) {
	src := `
package blackbox

// label:Cfg.
// icon:gear.
type Cfg struct {
    Headers map[string]string ` + "`prop:\"Headers\"`" + `
}

// label:Init.
// icon:sun.
func (c *Cfg) Init() (
    // doc:err.
    // label:err.
    err error,
) {
    return nil
}
`
	def, err := Parse([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := findExtractProp(def.Props, "Headers")
	if p == nil {
		t.Fatalf("expected PropDef for Headers, got: %+v", def.Props)
	}
	if p.Container != "map" {
		t.Errorf("Container = %q, want %q", p.Container, "map")
	}
	if p.KeyType != "string" {
		t.Errorf("KeyType = %q, want %q", p.KeyType, "string")
	}
	if p.ValueType != "string" {
		t.Errorf("ValueType = %q, want %q", p.ValueType, "string")
	}
	if !p.NativeKey {
		t.Errorf("NativeKey should be true for string")
	}
	if !p.NativeValue {
		t.Errorf("NativeValue should be true for string")
	}
}

func TestExtractProps_TaggedSlice_PopulatesContainer(t *testing.T) {
	src := `
package blackbox

// label:Cfg.
// icon:gear.
type Cfg struct {
    Pins []int ` + "`prop:\"Pins\"`" + `
}

// label:Init.
// icon:sun.
func (c *Cfg) Init() (
    // doc:err.
    // label:err.
    err error,
) {
    return nil
}
`
	def, err := Parse([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := findExtractProp(def.Props, "Pins")
	if p == nil {
		t.Fatalf("expected PropDef for Pins, got: %+v", def.Props)
	}
	if p.Container != "slice" {
		t.Errorf("Container = %q, want %q", p.Container, "slice")
	}
	if p.KeyType != "" {
		t.Errorf("KeyType should be empty for slice, got %q", p.KeyType)
	}
	if p.ValueType != "int" {
		t.Errorf("ValueType = %q, want %q", p.ValueType, "int")
	}
	if !p.NativeValue {
		t.Errorf("NativeValue should be true for int")
	}
}

func TestExtractProps_NativeScalar_NoContainer(t *testing.T) {
	// Sanity: a plain scalar prop must NOT populate Container.
	src := `
package blackbox

// label:Cfg.
// icon:gear.
type Cfg struct {
    Speed int ` + "`prop:\"Speed\" default:\"100\"`" + `
}

// label:Init.
// icon:sun.
func (c *Cfg) Init() (
    // doc:err.
    // label:err.
    err error,
) {
    return nil
}
`
	def, err := Parse([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := findExtractProp(def.Props, "Speed")
	if p == nil {
		t.Fatalf("expected PropDef for Speed, got: %+v", def.Props)
	}
	if p.Container != "" {
		t.Errorf("scalar must have empty Container, got %q", p.Container)
	}
	if p.KeyType != "" || p.ValueType != "" {
		t.Errorf("scalar must have empty KeyType/ValueType, got %q/%q", p.KeyType, p.ValueType)
	}
	if p.NativeKey || p.NativeValue {
		t.Errorf("scalar must have NativeKey/NativeValue=false, got %v/%v", p.NativeKey, p.NativeValue)
	}
}
