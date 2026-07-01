// server/codegen/blackbox/extract_props_test.go — Tests for the
// "discovery" path of extractProps and its native-type gate.
//
// Background:
//
// Devices like APDS9960 carry wire-input fields such as
//
//	I2C *machine.I2C
//
// — exported (capital I) but with no `prop:` tag, populated by the
// method `Init(i2c *machine.I2C)` doing `s.I2C = i2c`. Before the
// native-gate fix, the parser's discovery path emitted a PropDef for
// every untagged exported field regardless of type. The wizard then
// rendered an empty row above the real props (Label: "" → empty
// `<label>` cell).
//
// The fix: untagged exported fields qualify for the discovery path
// only when the type is native (byte, int, string, bool, …). A
// non-native untagged field — pointer, slice, map, qualified type
// — is treated as internal state and skipped.
//
// These tests pin down that behaviour:
//
//   - Tagged path is unchanged: `prop:` survives any type.
//   - Untagged native (no tag at all): emitted as discovery PropDef.
//   - Untagged non-native (no tag at all): skipped.
//   - Untagged with non-prop tag (e.g. json:"…") on non-native:
//     skipped, even though the field has SOME tag.
//   - End-to-end Parse on the original APDS9960 example — now exposes
//     exactly Gain and ATime; no blank row.
package blackbox

import (
	"strings"
	"testing"
)

// findExtractProp returns the PropDef for the field named fieldName,
// or nil if no prop with that field name exists. Helper kept local
// to this test file (with a unique prefix) because completion_test.go
// in the same package may add its own helpers later.
func findExtractProp(props []PropDef, fieldName string) *PropDef {
	for i := range props {
		if props[i].FieldName == fieldName {
			return &props[i]
		}
	}
	return nil
}

// ─── Untagged native: discovery path emits a PropDef ──────────────────────────

func TestExtractProps_UntaggedNative_Discovered(t *testing.T) {
	src := `
package blackbox

// label:Foo.
// icon:gear.
type Foo struct {
    Gain byte
}

// label:Init.
// icon:sun.
func (f *Foo) Init() (
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
	p := findExtractProp(def.Props, "Gain")
	if p == nil {
		t.Fatalf("expected discovery PropDef for Gain, got props: %+v", def.Props)
	}
	if !p.Untagged {
		t.Errorf("Gain should be Untagged=true (discovery), got %+v", *p)
	}
	if !p.NativeType {
		t.Errorf("Gain (byte) should be NativeType=true, got %+v", *p)
	}
}

// ─── Untagged non-native: skipped (the bug fix) ───────────────────────────────

func TestExtractProps_UntaggedNonNative_Skipped(t *testing.T) {
	src := `
package blackbox

// label:APDS9960.
// icon:microchip.
type APDS9960 struct {
    I2C *machine.I2C
    Gain byte ` + "`prop:\"Gain\" default:\"1\"`" + `
}

// label:Init.
// icon:sun.
func (s *APDS9960) Init(
    // doc:I2C bus.
    // connection:mandatory.
    // label:i2c.
    i2c *machine.I2C,
) (
    // doc:err.
    // label:err.
    err error,
) {
    s.I2C = i2c
    return nil
}
`
	def, err := Parse([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p := findExtractProp(def.Props, "I2C"); p != nil {
		t.Errorf("I2C is non-native and untagged — must NOT appear as a prop; got %+v", *p)
	}
	if p := findExtractProp(def.Props, "Gain"); p == nil {
		t.Errorf("Gain should still be present as a tagged prop; got: %+v", def.Props)
	}
	// Sanity: the only prop should be Gain. A blank row in the
	// inspect form would mean an extra PropDef sneaking in.
	if got, want := len(def.Props), 1; got != want {
		t.Errorf("expected exactly 1 prop (Gain), got %d: %+v", got, def.Props)
	}
}

// ─── Untagged with non-prop tag: same native gate applies ────────────────────

func TestExtractProps_NonPropTagOnNonNative_Skipped(t *testing.T) {
	src := `
package blackbox

// label:Bar.
// icon:gear.
type Bar struct {
    I2C *machine.I2C ` + "`json:\"i2c\"`" + `
    Pulses byte ` + "`prop:\"Pulses\" default:\"0\"`" + `
}

// label:Init.
// icon:sun.
func (b *Bar) Init() (
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
	if p := findExtractProp(def.Props, "I2C"); p != nil {
		t.Errorf("I2C with json: tag but no prop: tag, non-native — must be skipped; got %+v", *p)
	}
}

// ─── Untagged with non-prop tag on native: still discovered ──────────────────

func TestExtractProps_NonPropTagOnNative_Discovered(t *testing.T) {
	src := `
package blackbox

// label:Baz.
// icon:gear.
type Baz struct {
    Speed int ` + "`json:\"speed\"`" + `
}

// label:Init.
// icon:sun.
func (b *Baz) Init() (
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
		t.Fatalf("Speed (native int with json: tag, no prop:) should still be discovered; got: %+v", def.Props)
	}
	if !p.Untagged {
		t.Errorf("Speed should be Untagged=true (the json: tag is not a prop: tag), got %+v", *p)
	}
}

// ─── Tagged non-native: still emitted, the specialist's choice rules ─────────

func TestExtractProps_TaggedNonNative_Emitted(t *testing.T) {
	// Specialists may eventually want to expose a non-native prop
	// (e.g. a configuration map). The renderer cannot handle it yet,
	// but the parser must not silently drop the tag — that would
	// hide the specialist's explicit opt-in. A future slice will
	// give the renderer the matching UI; the parser is already
	// correct as-is.
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
		t.Fatalf("tagged map prop must be emitted regardless of native gate; got: %+v", def.Props)
	}
	if p.Label != "Headers" {
		t.Errorf("Label = %q, want %q", p.Label, "Headers")
	}
	if p.Untagged {
		t.Errorf("Headers has prop: tag — Untagged should be false")
	}
	if p.NativeType {
		t.Errorf("map[string]string is not a native type")
	}
	if !strings.HasPrefix(p.GoType, "map[") {
		t.Errorf("GoType = %q, want a map[...] prefix", p.GoType)
	}
}

// ─── End-to-end: the real-world bug from the wizard screenshot ───────────────

func TestExtractProps_APDS9960_NoBlankRow(t *testing.T) {
	// The exact source the user pasted in the bug report. Before the
	// fix, Properties showed a blank row (I2C) above Gain and ATime;
	// after the fix, exactly Gain and ATime survive.
	src := `
package blackbox

// APDS9960 is a colour, proximity, and gesture sensor.
//
// label:APDS9960.
// icon:microchip.
type APDS9960 struct {
    I2C *machine.I2C
    // ADC gain
    Gain byte ` + "`prop:\"Gain\" default:\"1\" range:\"0..5\"`" + `
    // Integration time
    ATime byte ` + "`prop:\"ATime\" default:\"200\" range:\"0..255\"`" + `
}

// label:Init.
// icon:sun.
func (s *APDS9960) Init(
    // doc:I2C bus.
    // connection:mandatory.
    // label:i2c.
    i2c *machine.I2C,
) (
    // doc:err.
    // label:err.
    err error,
) {
    s.I2C = i2c
    return nil
}
`
	def, err := Parse([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, want := len(def.Props), 2; got != want {
		t.Fatalf("expected exactly 2 props (Gain, ATime); got %d: %+v", got, def.Props)
	}
	for _, name := range []string{"Gain", "ATime"} {
		if findExtractProp(def.Props, name) == nil {
			t.Errorf("missing expected prop %q in %+v", name, def.Props)
		}
	}
	if findExtractProp(def.Props, "I2C") != nil {
		t.Errorf("I2C must NOT be a prop")
	}
}
