// server/codegen/blackbox/port_output_optional_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// TestSetPortConnection_outputDropsConnection verifies the slice-7
// rule: outputs never carry a connection: directive. When the user
// sends `connection:mandatory` for an output (e.g. older client,
// manual API call), the rewrite engine silently drops it.
func TestSetPortConnection_outputDropsConnection(t *testing.T) {
	src := `package mydevice

// icon:eye. label:S.
type APDS9960 struct{}

func (s *APDS9960) Run() (
	clear uint16,
) {
	return
}
`
	out, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.APDS9960.Run.out.clear",
		Args: []byte(`{"connection":"mandatory","label":"Clear","comment":"total light"}`),
	}})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	// label: and doc: should be present
	if !strings.Contains(out, "label:Clear.") {
		t.Errorf("expected label:Clear. in output:\n%s", out)
	}
	if !strings.Contains(out, "doc:total light.") {
		t.Errorf("expected doc:total light. in output:\n%s", out)
	}
	// connection: should NOT be present
	if strings.Contains(out, "connection:") {
		t.Errorf("output port should not have connection: directive:\n%s", out)
	}
}

// TestSetPortConnection_outputZombieCleanup verifies that a source
// where an output has a stale `// connection:mandatory.` directive
// (from before the slice-7 rule) gets cleaned on the next save —
// the organic cleanup path. The splice replaces the entire leading
// comment block, so the zombie disappears without a migration step.
func TestSetPortConnection_outputZombieCleanup(t *testing.T) {
	src := `package mydevice

// icon:eye. label:S.
type APDS9960 struct{}

func (s *APDS9960) Run() (
	// doc:old description.
	// connection:mandatory.
	// label:Old.
	clear uint16,
) {
	return
}
`
	// Re-save with new label/comment. Don't pass connection at all.
	out, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.APDS9960.Run.out.clear",
		Args: []byte(`{"label":"Clear","comment":"total light"}`),
	}})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if strings.Contains(out, "connection:") {
		t.Errorf("zombie connection: should have been cleaned:\n%s", out)
	}
	if !strings.Contains(out, "label:Clear.") {
		t.Errorf("new label missing:\n%s", out)
	}
	// Even if user explicitly passes connection on an output, we drop it.
	out2, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.APDS9960.Run.out.clear",
		Args: []byte(`{"connection":"mandatory","label":"Clear","comment":"total light"}`),
	}})
	if err != nil {
		t.Fatalf("rewrite 2: %v", err)
	}
	if strings.Contains(out2, "connection:") {
		t.Errorf("connection: should be dropped on outputs even when explicit:\n%s", out2)
	}
}

// TestSetPortConnection_inputKeepsConnection ensures we did NOT
// regress the input case — inputs still get connection: written
// when the user sets it.
func TestSetPortConnection_inputKeepsConnection(t *testing.T) {
	src := `package mydevice

// icon:eye. label:S.
type APDS9960 struct{}

func (s *APDS9960) Init(
	i2c *machine.I2C,
) (err error) {
	return nil
}
`
	out, err := Rewrite(src, []WizardEdit{{
		Op:   OpSetPortConnection,
		Path: "method.APDS9960.Init.in.i2c",
		Args: []byte(`{"connection":"mandatory","label":"I2C","comment":"I2C bus"}`),
	}})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !strings.Contains(out, "connection:mandatory.") {
		t.Errorf("input port should keep connection:mandatory.:\n%s", out)
	}
}
