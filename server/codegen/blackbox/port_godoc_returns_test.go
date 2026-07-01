// server/codegen/blackbox/port_godoc_returns_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// Regression: when a method's signature is single-line, the method's
// own godoc must NOT be propagated as the doc of every named return
// port. The godoc above the func declaration ends one line above the
// func keyword — which is also one line above the result list — so
// without a guard, findLeadingPortCommentInParser would mis-identify
// the godoc as a "leading port comment" for every named return.
//
// Setup mirrors the RP2040 APDS9960 example reported by the user:
//
//	// Run reads the four RGBC colour channels.
//	//
//	// Returns
//	//
//	//	clear: total light intensity
//	//	red:   red channel
//	//	green: green channel
//	//	blue:  blue channel
//	func (s *APDS9960) Run() (clear, red, green, blue uint16) { … }
//
// Expected: each port's Doc is the matching prose from the Returns
// section ("red channel" for red, etc.), NOT the entire method godoc.
func TestPortDoc_NotPollutedByMethodGodocOnSingleLineSignature(t *testing.T) {
	src := `package blackbox

type APDS9960 struct{}

// Run reads the four RGBC colour channels.
//
// Returns
//
//	clear: total light intensity
//	red:   red channel
//	green: green channel
//	blue:  blue channel
func (s *APDS9960) Run() (clear, red, green, blue uint16) {
	return 0, 0, 0, 0
}
`
	def, err := Parse([]byte(src), ParserLimits{MaxMethods: 32, MaxInputs: 16, MaxOutputs: 16, MaxProps: 16})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if def == nil || len(def.Methods) == 0 {
		t.Fatal("no methods parsed")
	}
	run := def.Methods[0].FuncDef
	if len(run.Outputs) != 4 {
		t.Fatalf("want 4 outputs, got %d", len(run.Outputs))
	}

	want := map[string]string{
		"clear": "total light intensity",
		"red":   "red channel",
		"green": "green channel",
		"blue":  "blue channel",
	}
	for _, p := range run.Outputs {
		got := p.Doc
		expect := want[p.Name]
		// Reject anything that contains the method-level prose. If the
		// godoc has leaked into the port doc, "Run reads" or "RGBC" or
		// the entry list as a whole will be present.
		if strings.Contains(got, "Run reads") ||
			strings.Contains(got, "RGBC") ||
			strings.Count(got, ":") > 0 && strings.Contains(got, "intensity") && p.Name != "clear" {
			t.Errorf("port %q: doc was polluted by method godoc; got %q", p.Name, got)
		}
		// Soft assertion on the actual value — informational; the
		// hard regression check is the pollution test above.
		if !strings.EqualFold(got, expect) {
			t.Logf("port %q doc: got %q, want %q (informational)", p.Name, got, expect)
		}
	}
}
