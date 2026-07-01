// server/codegen/blackbox/rewrite_test.go — Tests for the wizard rewrite engine.
//
// Coverage goals (per CLAUDE_WIZARD_DESIGN.md §5):
//
//   - One end-to-end test per operation (setStructDirectives, setFieldProp,
//     disableFieldProp, setMethodDirectives, setPortConnection).
//   - The two preservation invariants get dedicated tests:
//   - a non-IDS struct tag (json:) survives a setFieldProp edit,
//   - user-written godoc prose survives a setStructDirectives edit.
//   - Tag-codec round trip via the public Rewrite path (no need to test
//     the codec in isolation; if it broke, every other test would fail).
//   - The single-line param-list expansion that setPortConnection
//     triggers on the example APDS9960 source.
//   - A no-op call returns the same source modulo gofmt.
//
// The tests use string literals for the input and expected output. We
// compare normalised forms (TrimSpace) to avoid line-ending pedantry,
// and assert specific substrings only when whole-file comparison would
// be over-specified (e.g. the body of an unrelated method).
package blackbox

import (
	"encoding/json"
	"strings"
	"testing"
)

// =============================================================================
//  Helpers
// =============================================================================

// rewriteOK runs Rewrite and t.Fatals on error. Used everywhere that
// we expect success — keeps the test bodies focused on the comparison.
func rewriteOK(t *testing.T, source string, edits []WizardEdit) string {
	t.Helper()
	out, err := Rewrite(source, edits)
	if err != nil {
		t.Fatalf("Rewrite returned error: %v\n--- source ---\n%s", err, source)
	}
	return out
}

// edit is a tiny convenience to build a WizardEdit from typed args.
// json.Marshal converts the args to RawMessage so the constructor stays
// short in the test bodies.
func edit(t *testing.T, op, path string, args any) WizardEdit {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling args for %s: %v", op, err)
	}
	return WizardEdit{Op: op, Path: path, Args: raw}
}

// mustContain asserts that s contains all of subs. The error message
// shows s when the assertion fails — long but unambiguous.
func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("output does not contain %q\n--- output ---\n%s", sub, s)
		}
	}
}

// mustNotContain asserts that s contains none of subs.
func mustNotContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Errorf("output unexpectedly contains %q\n--- output ---\n%s", sub, s)
		}
	}
}

// =============================================================================
//  Sample source files
// =============================================================================

// minimalDevice is the smallest valid black-box used by most tests.
// It has one struct, one prop-eligible field, one Init method.
const minimalDevice = `package mydevice

type Sensor struct {
	Gain byte
}

func (s *Sensor) Init() (err error) {
	return nil
}
`

// preservedTagsDevice has fields with non-IDS struct tags. The wizard
// must round-trip those tags untouched while editing the IDS-owned
// keys. This is the test for CLAUDE_WIZARD_DESIGN.md §5.1 (2).
const preservedTagsDevice = `package mydevice

type Sensor struct {
	ID    string ` + "`json:\"id\" yaml:\"id\"`" + `
	Gain  byte   ` + "`json:\"gain\"`" + `
}

func (s *Sensor) Init() (err error) {
	return nil
}
`

// proseDevice has user-written godoc prose on the struct. Editing
// directives must not lose the prose. Tests §5.1 (1).
const proseDevice = `package mydevice

// Sensor reads ambient light through an APDS-9960 module.
//
// The driver was written for TinyGo and tested on a Raspberry Pi Pico.
type Sensor struct {
	Gain byte
}

func (s *Sensor) Init() (err error) {
	return nil
}
`

// inlineParamsDevice mirrors the APDS9960 example in the design doc:
// the Init method has its parameters and results on a single line,
// which the wizard auto-expands when a port edit targets one of them.
const inlineParamsDevice = `package mydevice

type Sensor struct {
	Gain byte
}

func (s *Sensor) Init(i2c int) (err error) {
	return nil
}
`

// =============================================================================
//  setStructDirectives
// =============================================================================

func TestRewrite_setStructDirectives_addsLabelAndIcon(t *testing.T) {
	out := rewriteOK(t, minimalDevice, []WizardEdit{
		edit(t, OpSetStructDirectives, "struct.Sensor", map[string]string{
			"label": "Color Sensor",
			"icon":  "eye",
		}),
	})
	mustContain(t, out,
		"// label:Color Sensor.",
		"// icon:eye.",
		"type Sensor struct {",
	)
}

func TestRewrite_setStructDirectives_preservesUserProse(t *testing.T) {
	// The hard test: existing prose must survive. Engine appends our
	// directives after a blank godoc line; original lines stay verbatim.
	out := rewriteOK(t, proseDevice, []WizardEdit{
		edit(t, OpSetStructDirectives, "struct.Sensor", map[string]string{
			"label":   "Color Sensor",
			"icon":    "eye",
			"comment": "Sensor reads ambient light through an APDS-9960 module.\n\nThe driver was written for TinyGo and tested on a Raspberry Pi Pico.",
		}),
	})
	mustContain(t, out,
		"// Sensor reads ambient light through an APDS-9960 module.",
		"// The driver was written for TinyGo and tested on a Raspberry Pi Pico.",
		"// label:Color Sensor.",
		"// icon:eye.",
	)
}

// =============================================================================
//  setFieldProp
// =============================================================================

func TestRewrite_setFieldProp_addsPropTag(t *testing.T) {
	out := rewriteOK(t, minimalDevice, []WizardEdit{
		edit(t, OpSetFieldProp, "struct.Sensor.field.Gain", map[string]any{
			"label":   "ADC Gain",
			"default": "1",
			"format":  "options",
			"formatArgs": map[string]any{
				"values": []string{"0", "1", "2", "3"},
			},
		}),
	})
	mustContain(t, out, "`prop:\"ADC Gain\" default:\"1\" options:\"0,1,2,3\"`")
}

func TestRewrite_setFieldProp_rangeMinMax(t *testing.T) {
	out := rewriteOK(t, minimalDevice, []WizardEdit{
		edit(t, OpSetFieldProp, "struct.Sensor.field.Gain", map[string]any{
			"label":   "Integration Time",
			"default": "255",
			"format":  "range_min_max",
			"formatArgs": map[string]any{
				"min": 0,
				"max": 255,
			},
		}),
	})
	mustContain(t, out, "`prop:\"Integration Time\" default:\"255\" range:\"0..255\"`")
}

func TestRewrite_setFieldProp_preservesNonIDSTags(t *testing.T) {
	// Round-trip invariant: json: and yaml: keys must be untouched.
	out := rewriteOK(t, preservedTagsDevice, []WizardEdit{
		edit(t, OpSetFieldProp, "struct.Sensor.field.Gain", map[string]any{
			"label":   "ADC Gain",
			"default": "1",
		}),
	})
	mustContain(t, out, "json:\"id\"", "yaml:\"id\"", "json:\"gain\"", "prop:\"ADC Gain\"")
	// The Gain tag should now be `json:"gain" prop:"ADC Gain" default:"1"`
	// (json: first, IDS keys appended after).
	mustContain(t, out, "`json:\"gain\" prop:\"ADC Gain\" default:\"1\"`")
}

// =============================================================================
//  disableFieldProp
// =============================================================================

func TestRewrite_disableFieldProp_stripsIDSKeys(t *testing.T) {
	// Field has both an IDS prop and a user-owned json: tag. Disabling
	// drops the prop but keeps json:.
	src := `package mydevice

type Sensor struct {
	Gain byte ` + "`json:\"gain\" prop:\"ADC Gain\" default:\"1\" options:\"0,1,2,3\"`" + `
}

func (s *Sensor) Init() (err error) {
	return nil
}
`
	out := rewriteOK(t, src, []WizardEdit{
		edit(t, OpDisableFieldProp, "struct.Sensor.field.Gain", map[string]any{}),
	})
	mustContain(t, out, "`json:\"gain\"`")
	mustNotContain(t, out, "prop:", "options:", "default:")
}

func TestRewrite_disableFieldProp_dropsTagWhenAllIDS(t *testing.T) {
	// When the field's tag has only IDS keys, disabling clears the tag
	// entirely (no empty backticks left over).
	src := `package mydevice

type Sensor struct {
	Gain byte ` + "`prop:\"ADC Gain\" default:\"1\"`" + `
}

func (s *Sensor) Init() (err error) {
	return nil
}
`
	out := rewriteOK(t, src, []WizardEdit{
		edit(t, OpDisableFieldProp, "struct.Sensor.field.Gain", map[string]any{}),
	})
	mustNotContain(t, out, "prop:", "default:", "``")
	mustContain(t, out, "Gain byte")
}

// =============================================================================
//  setMethodDirectives
// =============================================================================

func TestRewrite_setMethodDirectives_addsAllDirectives(t *testing.T) {
	zero := 0
	out := rewriteOK(t, minimalDevice, []WizardEdit{
		edit(t, OpSetMethodDirectives, "method.Sensor.Init", map[string]any{
			"label":          "initialize",
			"icon":           "play",
			"executionOrder": &zero,
			"comment":        "Init configures the sensor.",
		}),
	})
	mustContain(t, out,
		"// Init configures the sensor.",
		"// label:initialize.",
		"// icon:play.",
		"// executionOrder:0.",
	)
}

// =============================================================================
//  setPortConnection
// =============================================================================

func TestRewrite_setPortConnection_expandsSingleLineParams(t *testing.T) {
	out := rewriteOK(t, inlineParamsDevice, []WizardEdit{
		edit(t, OpSetPortConnection, "method.Sensor.Init.in.i2c", map[string]string{
			"connection": "mandatory",
		}),
	})
	mustContain(t, out, "// connection:mandatory.")
	// The Init signature should now have its param on its own line.
	if !strings.Contains(out, "Init(\n") {
		t.Errorf("expected Init param list to expand to multi-line; got:\n%s", out)
	}
	if !strings.Contains(out, "i2c int,") {
		t.Errorf("expected expanded param to keep `i2c int,` form; got:\n%s", out)
	}
}

func TestRewrite_setPortConnection_onMultilineParams_noExpansion(t *testing.T) {
	// A method that already has a multi-line param list should not be
	// rewritten beyond the doc comment insertion.
	src := `package mydevice

type Sensor struct {
	Gain byte
}

func (s *Sensor) Init(
	i2c int,
) (err error) {
	return nil
}
`
	out := rewriteOK(t, src, []WizardEdit{
		edit(t, OpSetPortConnection, "method.Sensor.Init.in.i2c", map[string]string{
			"connection": "optional",
		}),
	})
	mustContain(t, out, "// connection:optional.", "i2c int,")
}

// =============================================================================
//  Combined / no-op behaviour
// =============================================================================

func TestRewrite_noEdits_returnsGofmtSource(t *testing.T) {
	// An ill-formatted but valid input should come back gofmt'd.
	src := "package mydevice\n\n\n\ntype Sensor struct {\n\tGain byte\n}\n\nfunc (s *Sensor) Init() (err error) { return nil }\n"
	out := rewriteOK(t, src, nil)
	if !strings.Contains(out, "func (s *Sensor) Init() (err error)") {
		t.Errorf("expected gofmt-canonical form, got:\n%s", out)
	}
}

func TestRewrite_invalidSource_returnsErrorAndKeepsSource(t *testing.T) {
	src := "package mydevice\nfunc oops("
	out, err := Rewrite(src, []WizardEdit{
		edit(t, OpSetStructDirectives, "struct.Sensor", map[string]string{"label": "x"}),
	})
	if err == nil {
		t.Fatal("expected error for invalid source")
	}
	if out != src {
		t.Errorf("on error, output should equal input verbatim; got:\n%s", out)
	}
}

func TestRewrite_unknownOp(t *testing.T) {
	_, err := Rewrite(minimalDevice, []WizardEdit{
		{Op: "definitelyNotARealOp", Path: "struct.Sensor"},
	})
	if err == nil {
		t.Fatal("expected error for unknown op")
	}
}

// =============================================================================
//  Path-parser unit coverage
// =============================================================================

func TestParsePath(t *testing.T) {
	cases := []struct {
		in       string
		wantKind pathKind
		wantErr  bool
	}{
		{"struct.X", pathStruct, false},
		{"struct.X.field.Y", pathStructField, false},
		{"method.X.Y", pathMethod, false},
		{"method.X.Y.in.z", pathMethodPort, false},
		{"method.X.Y.out.err", pathMethodPort, false},
		{"struct.", 0, true},
		{"struct", 0, true},
		{"method.X.Y.sideways.z", 0, true},
		{"method.X.Y.in", 0, true},
		{"random.text", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		p, err := parsePath(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parsePath(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && p.Kind != c.wantKind {
			t.Errorf("parsePath(%q): kind=%v, want=%v", c.in, p.Kind, c.wantKind)
		}
	}
}

// =============================================================================
//  Tag-codec round trip via the public engine
// =============================================================================

func TestTagCodec_roundTripViaRewrite(t *testing.T) {
	// Field tag with json: + odd whitespace + escapes round-trips clean.
	// We deliberately use TWO spaces between the json: and xml: pairs in
	// Name's tag to confirm that the engine never even *reads* tags it
	// is not editing — Name's bytes survive the rewrite verbatim.
	src := `package mydevice

type Sensor struct {
	Name string ` + "`json:\"my_name,omitempty\"  xml:\"name\"`" + `
	Gain byte
}

func (s *Sensor) Init() (err error) {
	return nil
}
`
	// Add a prop to Gain — Name's tag should be untouched. We assert
	// the literal two-space form to prove the bytes are unchanged.
	out := rewriteOK(t, src, []WizardEdit{
		edit(t, OpSetFieldProp, "struct.Sensor.field.Gain", map[string]any{
			"label":   "ADC Gain",
			"default": "1",
		}),
	})
	mustContain(t, out, "`json:\"my_name,omitempty\"  xml:\"name\"`")
	// gofmt aligns the two field tags to the longer of the two type
	// columns; the exact column count is gofmt's business, so we check
	// only that the prop tag was emitted in the canonical single-space
	// codec form.
	mustContain(t, out, "`prop:\"ADC Gain\" default:\"1\"`")
}
