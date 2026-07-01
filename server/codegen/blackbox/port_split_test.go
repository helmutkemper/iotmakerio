// server/codegen/blackbox/port_split_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// TestSetPortConnection_multiNameField reproduces the user-reported
// bug from the screenshot: when ports are declared as
// `clear, red, green, blue uint16`, editing one of them caused all
// four to share the same metadata on re-parse.
//
// Expected behaviour after fix: the rewrite splits the multi-name
// field into four separate single-name fields, so the doc-comment
// splice targets only the named port and siblings are untouched.
func TestSetPortConnection_multiNameField(t *testing.T) {
	src := `package mydevice

// icon:eye. label:S.
type APDS9960 struct{}

func (s *APDS9960) Run() (clear, red, green, blue uint16) {
	return
}
`
	out, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.APDS9960.Run.out.clear",
		Args: []byte(`{"connection":"mandatory","label":"Clear","comment":"total light intensity"}`),
	}})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	// Source-level assertions: all four port names are present and
	// only "Clear" appears as a label.
	for _, name := range []string{"clear", "red", "green", "blue"} {
		if !strings.Contains(out, name+" uint16") &&
			!strings.Contains(out, name+",") &&
			!strings.Contains(out, name+" ") {
			// Fields might be on separate lines after split — accept
			// any positional spelling, but every port name must
			// appear somewhere.
			if !strings.Contains(out, name) {
				t.Errorf("port %q missing from output:\n%s", name, out)
			}
		}
	}
	if c := strings.Count(out, "label:Clear."); c != 1 {
		t.Errorf("expected exactly 1 label:Clear., got %d:\n%s", c, out)
	}

	// Parser-level assertions: re-parse the rewritten source and
	// verify that ONLY clear has the new metadata.
	def, perr := Parse([]byte(out), DefaultParserLimits())
	if def == nil {
		t.Fatalf("parse after rewrite: %v", perr)
	}
	var run *NamedFuncDef
	for _, m := range def.Methods {
		if m.Name == "Run" {
			rc := m
			run = &rc
			break
		}
	}
	if run == nil {
		t.Fatalf("Run method not found in parsed def: %+v", def)
	}
	if len(run.Outputs) != 4 {
		t.Fatalf("expected 4 output ports, got %d:\n%+v", len(run.Outputs), run.Outputs)
	}
	for _, p := range run.Outputs {
		if p.Name == "clear" {
			if p.Label != "Clear" {
				t.Errorf("clear.Label: got %q, want %q", p.Label, "Clear")
			}
			if p.Doc != "total light intensity" {
				t.Errorf("clear.Doc: got %q, want %q", p.Doc, "total light intensity")
			}
			continue
		}
		// Siblings (red, green, blue) MUST keep their original empty
		// metadata. The bug made them all show up as "Clear".
		if p.Label == "Clear" {
			t.Errorf("sibling port %q got contaminated with Label=%q", p.Name, p.Label)
		}
		if p.Doc == "total light intensity" {
			t.Errorf("sibling port %q got contaminated with Doc=%q", p.Name, p.Doc)
		}
	}
}

// TestSetPortConnection_multiNameField_secondEdit ensures that
// editing two siblings in succession works — the second edit should
// find the already-isolated single-name field, not re-trigger a
// split.
func TestSetPortConnection_multiNameField_secondEdit(t *testing.T) {
	src := `package mydevice

// icon:eye. label:S.
type APDS9960 struct{}

func (s *APDS9960) Run() (clear, red, green, blue uint16) {
	return
}
`
	out1, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.APDS9960.Run.out.clear",
		Args: []byte(`{"connection":"mandatory","label":"Clear","comment":"total"}`),
	}})
	if err != nil {
		t.Fatalf("save 1: %v", err)
	}

	out2, err := Rewrite(out1, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.APDS9960.Run.out.red",
		Args: []byte(`{"connection":"optional","label":"Red","comment":"red channel"}`),
	}})
	if err != nil {
		t.Fatalf("save 2: %v", err)
	}

	def, _ := Parse([]byte(out2), DefaultParserLimits())
	var run *NamedFuncDef
	for _, m := range def.Methods {
		if m.Name == "Run" {
			rc := m
			run = &rc
			break
		}
	}
	if run == nil || len(run.Outputs) != 4 {
		t.Fatalf("expected 4 outputs in Run; got %+v", run)
	}
	expectations := map[string]struct{ label, doc string }{
		"clear": {"Clear", "total"},
		"red":   {"Red", "red channel"},
		"green": {"", ""},
		"blue":  {"", ""},
	}
	for _, p := range run.Outputs {
		want, ok := expectations[p.Name]
		if !ok {
			t.Errorf("unexpected port %q", p.Name)
			continue
		}
		if p.Label != want.label {
			t.Errorf("%s.Label: got %q, want %q", p.Name, p.Label, want.label)
		}
		if p.Doc != want.doc {
			t.Errorf("%s.Doc: got %q, want %q", p.Name, p.Doc, want.doc)
		}
	}
}
