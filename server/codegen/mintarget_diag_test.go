// server/codegen/mintarget_diag_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// TestMinTargetDiagnostics pins the hardware-class gate: a posix device on
// an avr project errors; on a posix project passes; an unknown declared
// value gets the typo diagnostic; an undeclared device is never gated.
// Português: Pina o portão de classe: device posix em projeto avr erra; em
// projeto posix passa; valor desconhecido ganha o diagnóstico de typo;
// device sem declaração nunca é barrado.
func TestMinTargetDiagnostics(t *testing.T) {
	def := &blackbox.BlackBoxDef{
		Functions: []blackbox.NamedFuncDef{
			{Name: "portal_server_start"},
			{Name: "portal_page_size"},
			{Name: "weird"},
		},
	}
	def.Functions[0].MinTarget = "posix"
	def.Functions[2].MinTarget = "linux" // typo on purpose

	defs := map[string]*blackbox.BlackBoxDef{
		"portal_server_start": def,
		"portal_page_size":    def,
		"weird":               def,
	}

	got := minTargetDiagnostics(defs, blackbox.MinTargetAvr)
	var gate, typo int
	for _, d := range got {
		if strings.Contains(d.Message, "requires a posix-class") {
			gate++
		}
		if strings.Contains(d.Message, "unknown min-target") {
			typo++
		}
	}
	if gate != 1 || typo != 1 || len(got) != 2 {
		t.Fatalf("avr project: gate=%d typo=%d total=%d (%+v)", gate, typo, len(got), got)
	}

	got = minTargetDiagnostics(defs, blackbox.MinTargetPosix)
	if len(got) != 1 || !strings.Contains(got[0].Message, "unknown min-target") {
		t.Fatalf("posix project must only flag the typo: %+v", got)
	}
}
