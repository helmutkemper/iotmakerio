// server/codegen/blackbox/completion_test.go — Tests for ComputeIncomplete.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// One test per rule from CLAUDE_WIZARD_DESIGN.md §6.2 + slice-6
// addendum:
//
//   - struct needs label + icon
//   - prop needs label (Default is optional — see completion.go)
//   - method (Init or named) needs label + icon
//   - port needs label + doc (every named port, including errors)
//   - non-error port additionally needs connection: tag set
//   - error ports skip the connection: check (IDS-spec exemption)
//   - anonymous ports (no name) are skipped — no addressable path
//   - sort order is stable across runs
//   - native-type filter (smoke-test of the IsNativePropType helper)
//
// Plus an end-to-end test that parses real Go source and checks the
// computed set against an expected slice — this is the only test that
// exercises ComputeIncomplete + Parse together, catching any drift in
// how the parser populates BlackBoxDef.
package blackbox

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// =============================================================================
//  Helpers
// =============================================================================

// equalSorted reports whether two string slices are equal as sorted
// sequences. The fixtures already pass sorted values for `want`, but
// using equalSorted instead of reflect.DeepEqual makes the tests
// resilient to future changes in ComputeIncomplete's emission order.
func equalSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a2 := append([]string(nil), a...)
	b2 := append([]string(nil), b...)
	sort.Strings(a2)
	sort.Strings(b2)
	return reflect.DeepEqual(a2, b2)
}

// =============================================================================
//  IsNativePropType — smoke test of the public helper
// =============================================================================

func TestIsNativePropType(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"int", true},
		{"int32", true},
		{"uint8", true},
		{"byte", true},
		{"rune", true},
		{"float64", true},
		{"string", true},
		{"bool", true},
		{"  string  ", true}, // whitespace trim
		{"machine.I2C", false},
		{"*machine.I2C", false},
		{"[]byte", false},
		{"map[string]int", false},
		{"chan int", false},
		{"interface{}", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsNativePropType(c.in); got != c.want {
			t.Errorf("IsNativePropType(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// =============================================================================
//  Struct rule
// =============================================================================

func TestComputeIncomplete_struct(t *testing.T) {
	cases := []struct {
		name string
		def  BlackBoxDef
		want []string
	}{
		{
			name: "missing both label and icon",
			def:  BlackBoxDef{Name: "Sensor"},
			want: []string{"struct.Sensor"},
		},
		{
			name: "missing icon only",
			def:  BlackBoxDef{Name: "Sensor", StructLabel: "Color Sensor"},
			want: []string{"struct.Sensor"},
		},
		{
			name: "missing label only",
			def:  BlackBoxDef{Name: "Sensor", StructIcon: "eye"},
			want: []string{"struct.Sensor"},
		},
		{
			name: "complete",
			def:  BlackBoxDef{Name: "Sensor", StructLabel: "Color Sensor", StructIcon: "eye"},
			want: []string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeIncomplete(&c.def)
			if !equalSorted(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// =============================================================================
//  Prop rule
// =============================================================================

func TestComputeIncomplete_prop(t *testing.T) {
	// Use a struct that is itself complete so the prop rule is the
	// only thing under test.
	//
	// Each prop fixture sets `Untagged` and `NativeType` explicitly
	// because the new ComputeIncomplete logic (slice 6+ field
	// surfacing) branches on both. Tests cover the three categories:
	//   - tagged native: incomplete iff Label is empty (Default is
	//                    optional — see completion.go for rationale)
	//   - untagged native: ALWAYS incomplete (the user has not
	//                      decided whether to promote to a prop)
	//   - untagged non-native: NEVER incomplete (inert)
	complete := BlackBoxDef{Name: "Sensor", StructLabel: "S", StructIcon: "eye"}

	cases := []struct {
		name  string
		props []PropDef
		want  []string
	}{
		{
			name:  "tagged native, label empty (default also empty)",
			props: []PropDef{{FieldName: "Gain", GoType: "byte", NativeType: true}},
			want:  []string{"struct.Sensor.field.Gain"},
		},
		{
			name: "tagged native, label set, default empty — complete (default is optional)",
			// Default is intentionally optional. A specialist who
			// tagged a field but did not provide a default is
			// signalling "use Go's zero value", which is a valid
			// configuration. No ⚠.
			props: []PropDef{{FieldName: "Gain", GoType: "byte", Label: "ADC Gain", NativeType: true}},
			want:  []string{},
		},
		{
			name:  "tagged native, label empty, default set",
			props: []PropDef{{FieldName: "Gain", GoType: "byte", Default: "1", NativeType: true}},
			want:  []string{"struct.Sensor.field.Gain"},
		},
		{
			name:  "tagged native, complete",
			props: []PropDef{{FieldName: "Gain", GoType: "byte", Label: "ADC Gain", Default: "1", NativeType: true}},
			want:  []string{},
		},
		{
			name: "multiple props, only label-less one is incomplete",
			props: []PropDef{
				{FieldName: "Gain", GoType: "byte", Label: "ADC Gain", Default: "1", NativeType: true},
				// Mode has label but no default — that's fine now.
				{FieldName: "Mode", GoType: "string", Label: "Mode", NativeType: true},
				// Threshold has neither — incomplete because of label.
				{FieldName: "Threshold", GoType: "int", NativeType: true},
			},
			want: []string{"struct.Sensor.field.Threshold"},
		},
		{
			name: "untagged non-native is inert (no warning)",
			props: []PropDef{
				// *machine.I2C — non-native, untagged. Should NOT
				// appear in incomplete[].
				{FieldName: "I2C", GoType: "*machine.I2C", Untagged: true, NativeType: false},
			},
			want: []string{},
		},
		{
			name: "untagged native is always incomplete",
			props: []PropDef{
				// ATime byte without a tag — needs the user to
				// either promote it to a prop or remove it.
				{FieldName: "ATime", GoType: "byte", Untagged: true, NativeType: true},
			},
			want: []string{"struct.Sensor.field.ATime"},
		},
		{
			name: "mixed categories",
			props: []PropDef{
				// I2C: untagged non-native, inert
				{FieldName: "I2C", GoType: "*machine.I2C", Untagged: true, NativeType: false},
				// Gain: tagged native, complete
				{FieldName: "Gain", GoType: "byte", Label: "ADC Gain", Default: "0", NativeType: true},
				// ATime: untagged native, ⚠
				{FieldName: "ATime", GoType: "byte", Untagged: true, NativeType: true},
			},
			want: []string{"struct.Sensor.field.ATime"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := complete
			d.Props = c.props
			got := ComputeIncomplete(&d)
			if !equalSorted(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// =============================================================================
//  Method rule (covers both Init and named methods)
// =============================================================================

func TestComputeIncomplete_method(t *testing.T) {
	complete := BlackBoxDef{Name: "Sensor", StructLabel: "S", StructIcon: "eye"}

	t.Run("Init missing both", func(t *testing.T) {
		d := complete
		d.Init = &FuncDef{}
		got := ComputeIncomplete(&d)
		want := []string{"method.Sensor.Init"}
		if !equalSorted(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("Init complete", func(t *testing.T) {
		d := complete
		d.Init = &FuncDef{Label: "init", Icon: "play"}
		got := ComputeIncomplete(&d)
		if len(got) != 0 {
			t.Errorf("got %v, want []", got)
		}
	})

	t.Run("named method missing icon", func(t *testing.T) {
		d := complete
		d.Methods = []NamedFuncDef{
			{Name: "Run", FuncDef: FuncDef{Label: "run"}},
		}
		got := ComputeIncomplete(&d)
		want := []string{"method.Sensor.Run"}
		if !equalSorted(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("Init incomplete plus named complete", func(t *testing.T) {
		d := complete
		d.Init = &FuncDef{} // no label, no icon
		d.Methods = []NamedFuncDef{
			{Name: "Run", FuncDef: FuncDef{Label: "run", Icon: "play"}},
		}
		got := ComputeIncomplete(&d)
		want := []string{"method.Sensor.Init"}
		if !equalSorted(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// =============================================================================
//  Port rule + skip conditions
// =============================================================================

func TestComputeIncomplete_port(t *testing.T) {
	// A method that is itself complete; only the port rule is varied.
	completeMethod := FuncDef{Label: "init", Icon: "play"}
	base := BlackBoxDef{
		Name: "Sensor", StructLabel: "S", StructIcon: "eye",
		Init: &completeMethod,
	}

	// Helper: a fully-complete port (label + doc + connection) so tests
	// can assert just one missing field at a time. Modify the returned
	// value to break exactly the property under test.
	completePort := func(name string) PortDef {
		return PortDef{
			Name:        name,
			GoType:      "uint16",
			Label:       "Lux",
			Doc:         "Total light intensity",
			Connection:  "optional",
			MissingConn: false,
		}
	}

	t.Run("input port with MissingConn", func(t *testing.T) {
		d := base
		mcopy := completeMethod
		port := completePort("i2c")
		port.GoType = "machine.I2C"
		port.MissingConn = true
		port.Connection = ""
		mcopy.Inputs = []PortDef{port}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		want := []string{"method.Sensor.Init.in.i2c"}
		if !equalSorted(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("input port fully complete", func(t *testing.T) {
		d := base
		mcopy := completeMethod
		port := completePort("i2c")
		port.GoType = "machine.I2C"
		port.Connection = "mandatory"
		mcopy.Inputs = []PortDef{port}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		if len(got) != 0 {
			t.Errorf("got %v, want []", got)
		}
	})

	t.Run("named output port with MissingConn is complete (slice-7 rule)", func(t *testing.T) {
		// Slice-7 rule: outputs are always optional connection-wise.
		// MissingConn=true on an output (regular or error) is not a
		// completeness problem — only inputs require connection: to
		// be set.
		d := base
		mcopy := completeMethod
		port := completePort("lux")
		port.MissingConn = true
		port.Connection = ""
		mcopy.Outputs = []PortDef{port}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		if len(got) != 0 {
			t.Errorf("got %v, want []", got)
		}
	})

	t.Run("input vs output asymmetry on MissingConn", func(t *testing.T) {
		// Same fixture twice — once as an input, once as an output.
		// Rule: input with MissingConn is incomplete; output isn't.
		mkPort := func() PortDef {
			p := completePort("port")
			p.MissingConn = true
			p.Connection = ""
			return p
		}
		// As input → incomplete.
		dIn := base
		min := completeMethod
		min.Inputs = []PortDef{mkPort()}
		dIn.Init = &min
		gotIn := ComputeIncomplete(&dIn)
		wantIn := []string{"method.Sensor.Init.in.port"}
		if !equalSorted(gotIn, wantIn) {
			t.Errorf("input case: got %v, want %v", gotIn, wantIn)
		}
		// As output → complete.
		dOut := base
		mout := completeMethod
		mout.Outputs = []PortDef{mkPort()}
		dOut.Init = &mout
		gotOut := ComputeIncomplete(&dOut)
		if len(gotOut) != 0 {
			t.Errorf("output case: got %v, want []", gotOut)
		}
	})

	t.Run("port without label is incomplete", func(t *testing.T) {
		// Slice-6 rule: every named port needs Label. A port that
		// has connection: set but no Label is still incomplete.
		d := base
		mcopy := completeMethod
		port := completePort("lux")
		port.Label = ""
		mcopy.Outputs = []PortDef{port}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		want := []string{"method.Sensor.Init.out.lux"}
		if !equalSorted(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("port without doc is incomplete", func(t *testing.T) {
		// Slice-6 rule: every named port needs Doc (Comment).
		d := base
		mcopy := completeMethod
		port := completePort("lux")
		port.Doc = ""
		mcopy.Outputs = []PortDef{port}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		want := []string{"method.Sensor.Init.out.lux"}
		if !equalSorted(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("error return needs label and doc", func(t *testing.T) {
		// Slice-6 rule: errors are EXEMPT from the connection check
		// but still need Label and Doc set, because both surface in
		// the IDE pin tooltip and in the function's godoc. An error
		// with neither should be incomplete.
		d := base
		mcopy := completeMethod
		mcopy.Outputs = []PortDef{
			{Name: "err", GoType: "error", IsError: true, MissingConn: true},
		}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		want := []string{"method.Sensor.Init.out.err"}
		if !equalSorted(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("error return with label and doc is complete", func(t *testing.T) {
		// MissingConn=true on errors no longer matters (IDS spec
		// exempts them). With Label and Doc set they are complete.
		d := base
		mcopy := completeMethod
		mcopy.Outputs = []PortDef{
			{
				Name: "err", GoType: "error", IsError: true,
				MissingConn: true, // explicitly true to prove it's ignored
				Label:       "Init failure",
				Doc:         "Returned when the sensor doesn't respond on the I2C bus",
			},
		}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		if len(got) != 0 {
			t.Errorf("got %v, want []", got)
		}
	})

	t.Run("anonymous port is skipped", func(t *testing.T) {
		// Empty name → no addressable path → must be skipped even if
		// the port has every other completeness problem (no label,
		// no doc, MissingConn).
		d := base
		mcopy := completeMethod
		mcopy.Inputs = []PortDef{
			{Name: "", GoType: "int", MissingConn: true},
		}
		d.Init = &mcopy
		got := ComputeIncomplete(&d)
		if len(got) != 0 {
			t.Errorf("got %v, want []", got)
		}
	})
}

// =============================================================================
//  Sort and nil-safety
// =============================================================================

func TestComputeIncomplete_sortedOutput(t *testing.T) {
	// Build a def that hits multiple incomplete paths in a non-sorted
	// natural emission order, to make sure ComputeIncomplete sorts.
	d := &BlackBoxDef{
		Name: "Sensor", // struct.Sensor will be incomplete
		Methods: []NamedFuncDef{
			{Name: "Zeta", FuncDef: FuncDef{}},  // method.Sensor.Zeta
			{Name: "Alpha", FuncDef: FuncDef{}}, // method.Sensor.Alpha
		},
		Props: []PropDef{
			{FieldName: "Mode", GoType: "string"}, // struct.Sensor.field.Mode
			{FieldName: "Gain", GoType: "byte"},   // struct.Sensor.field.Gain
		},
	}
	got := ComputeIncomplete(d)
	want := []string{
		"method.Sensor.Alpha",
		"method.Sensor.Zeta",
		"struct.Sensor",
		"struct.Sensor.field.Gain",
		"struct.Sensor.field.Mode",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected sorted output, got %v", got)
	}
}

func TestComputeIncomplete_nilDef(t *testing.T) {
	got := ComputeIncomplete(nil)
	if got == nil {
		t.Fatal("ComputeIncomplete(nil) must return non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ComputeIncomplete(nil) = %v, want []", got)
	}
}

// =============================================================================
//  Integration with Parse — does the real parser populate the def in a way
//  that ComputeIncomplete can interpret correctly?
// =============================================================================

func TestComputeIncomplete_endToEndViaParse(t *testing.T) {
	// A device that is intentionally half-configured: the struct has
	// an icon but no label, the field has a prop but no default, the
	// Init method has neither label nor icon, and the i2c port has no
	// connection.
	src := `package mydevice

// icon:eye.
type Sensor struct {
	Gain byte ` + "`prop:\"ADC Gain\"`" + `
}

func (s *Sensor) Init(i2c int) (err error) {
	return nil
}
`
	// Parse may return a non-nil err for soft warnings (e.g. missing
	// `connection:` tags) while still returning a valid def. The wizard
	// endpoint deliberately discards those soft warnings — the incomplete
	// set computed below is the canonical signal for the same conditions.
	def, err := Parse([]byte(src), DefaultParserLimits())
	if def == nil {
		t.Fatalf("Parse: hard error (def is nil): %v", err)
	}
	got := ComputeIncomplete(def)

	// We expect:
	//   struct.Sensor                — missing label
	//   method.Sensor.Init           — no label, no icon
	//   method.Sensor.Init.in.i2c    — no connection, no label, no doc
	//   method.Sensor.Init.out.err   — error port has no Label/Doc.
	//                                  Slice-6 rule: errors skip the
	//                                  connection check but still need
	//                                  Label and Doc to surface in the
	//                                  IDE pin tooltip and godoc.
	//
	// We deliberately DO NOT expect struct.Sensor.field.Gain.
	// The field has a prop:"ADC Gain" tag (Label is non-empty) and
	// no default — that's a valid configuration: the specialist is
	// signalling "use Go's zero value". Default is optional; only
	// Label is required for tagged props.
	wantContains := []string{
		"struct.Sensor",
		"method.Sensor.Init",
		"method.Sensor.Init.in.i2c",
		"method.Sensor.Init.out.err",
	}
	for _, w := range wantContains {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in incomplete set; got %v", w, got)
		}
	}
	// And explicitly assert Gain is NOT incomplete — it has a
	// label and that is enough.
	for _, g := range got {
		if g == "struct.Sensor.field.Gain" {
			t.Errorf("did not expect struct.Sensor.field.Gain in incomplete set "+
				"(Default is optional when Label is set); got %v", got)
		}
	}
}

// ─── C99 device-per-function model ──────────────────────────────────────────

// In the C99 model the def has no primary struct (empty Name): no
// spurious "struct." path may be emitted, and Functions/Enums/WireTypes
// drive incompleteness instead. These mirror the SPA's client-side rules.

func TestComputeIncomplete_c99_noSpuriousStruct(t *testing.T) {
	def := &BlackBoxDef{
		WireTypes: []StructDef{{Name: "sht3x", Label: "Handle", Icon: "microchip"}},
	}
	for _, p := range ComputeIncomplete(def) {
		if p == "struct." || strings.HasPrefix(p, "struct.") {
			t.Errorf("C99 def must not emit a struct. path; got %q", p)
		}
	}
}

func TestComputeIncomplete_c99_wireType(t *testing.T) {
	// Missing label/icon → incomplete; both present → complete.
	bad := &BlackBoxDef{WireTypes: []StructDef{{Name: "h"}}}
	if got := ComputeIncomplete(bad); !hasPath(got, "wiretype.h") {
		t.Errorf("wire-type without label/icon should be incomplete; got %v", got)
	}
	ok := &BlackBoxDef{WireTypes: []StructDef{{Name: "h", Label: "Handle", Icon: "plug"}}}
	if got := ComputeIncomplete(ok); len(got) != 0 {
		t.Errorf("complete wire-type should yield no paths; got %v", got)
	}
}

func TestComputeIncomplete_c99_enumValues(t *testing.T) {
	// Incomplete iff a value lacks a label; the enum's own label/icon
	// are not required.
	def := &BlackBoxDef{Enums: []EnumDef{{
		Name: "rep",
		Values: []EnumValueDef{
			{Name: "LOW", Label: "Low"},
			{Name: "HIGH"}, // no label
		},
	}}}
	got := ComputeIncomplete(def)
	if !hasPath(got, "enum.rep.value.HIGH") {
		t.Errorf("enum value without label should be incomplete; got %v", got)
	}
	if hasPath(got, "enum.rep.value.LOW") {
		t.Errorf("labelled enum value should be complete; got %v", got)
	}
}

func TestComputeIncomplete_c99_functionPortRule(t *testing.T) {
	// C99 rule (differs from Go): every parameter — input OR output —
	// needs label + doc + connection; only `return` is exempt (label
	// only).
	mkPort := func(name, label, doc string, missingConn bool) PortDef {
		return PortDef{Name: name, Label: label, Doc: doc, MissingConn: missingConn}
	}
	def := &BlackBoxDef{Functions: []NamedFuncDef{{
		Name: "read",
		FuncDef: FuncDef{
			Label: "Read", Icon: "thermometer",
			Inputs: []PortDef{
				mkPort("dev", "Device", "the handle", false), // complete
				mkPort("temp", "Temp", "", false),            // missing doc
			},
			Outputs: []PortDef{
				mkPort("hum", "Humidity", "rh%", true), // missing conn
				mkPort("return", "Status", "", false),  // return: label only → complete
			},
		},
	}}}
	got := ComputeIncomplete(def)
	want := []string{
		"function.read.in.temp",
		"function.read.out.hum",
	}
	for _, w := range want {
		if !hasPath(got, w) {
			t.Errorf("expected %q to be incomplete; got %v", w, got)
		}
	}
	if hasPath(got, "function.read.in.dev") {
		t.Errorf("complete input should not be flagged; got %v", got)
	}
	if hasPath(got, "function.read.out.return") {
		t.Errorf("return needs only a label; should be complete; got %v", got)
	}
	if hasPath(got, "function.read") {
		t.Errorf("device with label+icon should not be flagged at device level; got %v", got)
	}
}

func hasPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// Edge cases of the C99 port rule: an anonymous port (no Name) is never
// reported (no addressable path); a `return` MISSING its label is
// incomplete (the label-only exemption still requires the label).
func TestComputeIncomplete_c99_portEdgeCases(t *testing.T) {
	def := &BlackBoxDef{Functions: []NamedFuncDef{{
		Name: "f",
		FuncDef: FuncDef{
			Label: "F", Icon: "bolt",
			Inputs: []PortDef{
				{Name: ""}, // anonymous — never incomplete, never a path
			},
			Outputs: []PortDef{
				{Name: "return"}, // return WITHOUT a label — incomplete
			},
		},
	}}}
	got := ComputeIncomplete(def)
	if !hasPath(got, "function.f.out.return") {
		t.Errorf("return without a label should be incomplete; got %v", got)
	}
	for _, p := range got {
		if strings.HasPrefix(p, "function.f.in.") {
			t.Errorf("anonymous port must not produce a path; got %q", p)
		}
	}
}
