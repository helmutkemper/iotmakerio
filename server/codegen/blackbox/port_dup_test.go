package blackbox

import (
	"strings"
	"testing"
)

// TestSetPortConnection_doesNotDuplicate reproduces the user-reported
// duplication bug: editing a port's metadata via setPortConnection
// twice should NOT accumulate directive blocks above the parameter.
//
// The original bug was in docEditTargetRange's pathMethodPort branch:
// when the field had an existing Doc, startPos was rewound to cover
// it but endPos stayed at field.Pos(), so the splice was a pure
// insertion that prepended a fresh comment block without removing
// the old one. After two saves the user saw the same directive
// triplicated above the parameter.
//
// The deeper bug under that one: Go's parser doesn't populate
// field.Doc for fields nested inside a parameter list, so even with
// endPos fixed the `field.Doc != nil` guard would never trigger.
// Both fixes ship together — see findLeadingPortComment in rewrite.go
// and findLeadingPortCommentInParser in parser.go.
func TestSetPortConnection_doesNotDuplicate(t *testing.T) {
	src := `package mydevice

func (s *Sensor) Init(
	i2c *machine.I2C,
) (err error) {
	return nil
}
`
	// First save: set connection + label + comment.
	out1, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.Sensor.Init.in.i2c",
		Args: []byte(`{"connection":"mandatory","label":"I2C","comment":"I2C bus"}`),
	}})
	if err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if c := strings.Count(out1, "label:I2C."); c != 1 {
		t.Fatalf("after save 1: expected 1 label:I2C., got %d.\nSource:\n%s", c, out1)
	}
	if c := strings.Count(out1, "connection:mandatory."); c != 1 {
		t.Fatalf("after save 1: expected 1 connection:mandatory., got %d.\nSource:\n%s", c, out1)
	}

	// Second save with the same args: should still produce ONE
	// label:I2C. and ONE connection:mandatory., not duplicates.
	out2, err := Rewrite(out1, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.Sensor.Init.in.i2c",
		Args: []byte(`{"connection":"mandatory","label":"I2C","comment":"I2C bus"}`),
	}})
	if err != nil {
		t.Fatalf("save 2: %v", err)
	}
	if c := strings.Count(out2, "label:I2C."); c != 1 {
		t.Errorf("after save 2: expected 1 label:I2C., got %d.\nSource:\n%s", c, out2)
	}
	if c := strings.Count(out2, "connection:mandatory."); c != 1 {
		t.Errorf("after save 2: expected 1 connection:mandatory., got %d.\nSource:\n%s", c, out2)
	}
	if c := strings.Count(out2, "I2C bus"); c != 1 {
		t.Errorf("after save 2: expected 1 'I2C bus' prose line, got %d.\nSource:\n%s", c, out2)
	}
}

// TestSetPortConnection_roundTrip ensures the parser reads back the
// label, comment, and connection that the rewrite engine wrote — so
// that re-opening the modal hydrates with the user's saved values
// instead of empty fields.
func TestSetPortConnection_roundTrip(t *testing.T) {
	src := `package mydevice

// icon:eye. label:S.
type Sensor struct {}

func (s *Sensor) Init(
	i2c *machine.I2C,
) (err error) {
	return nil
}
`
	out, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.Sensor.Init.in.i2c",
		Args: []byte(`{"connection":"mandatory","label":"I2C","comment":"I2C bus"}`),
	}})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	def, err := Parse([]byte(out), DefaultParserLimits())
	if def == nil {
		t.Fatalf("parse after rewrite: %v", err)
	}
	if def.Init == nil || len(def.Init.Inputs) == 0 {
		t.Fatalf("expected Init.Inputs to have 1 port, got def.Init=%+v", def.Init)
	}
	p := def.Init.Inputs[0]
	if p.Label != "I2C" {
		t.Errorf("Label: got %q, want %q", p.Label, "I2C")
	}
	if p.Doc != "I2C bus" {
		t.Errorf("Doc: got %q, want %q", p.Doc, "I2C bus")
	}
	if p.Connection != "mandatory" {
		t.Errorf("Connection: got %q, want %q", p.Connection, "mandatory")
	}
	if p.MissingConn {
		t.Error("MissingConn should be false after setPortConnection wrote a connection: tag")
	}
}
