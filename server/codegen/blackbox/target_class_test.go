// server/codegen/blackbox/target_class_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestMinTargetLadder pins the ordinal ladder and the profile mapping —
// including the two deliberate defaults: absent declaration = lowest rung,
// scene without a target = posix (permissive).
// Português: Pina a escada ordinal e o mapeamento de profiles — incluindo
// os dois defaults deliberados: declaração ausente = degrau mais baixo,
// cena sem target = posix (permissivo).
func TestMinTargetLadder(t *testing.T) {
	cases := []struct {
		class string
		ord   int
		ok    bool
	}{
		{"", 1, true}, {"avr", 1, true}, {"mcu32", 2, true},
		{"posix", 3, true}, {"linux", 0, false},
	}
	for _, c := range cases {
		ord, ok := MinTargetOrdinal(c.class)
		if ord != c.ord || ok != c.ok {
			t.Fatalf("MinTargetOrdinal(%q) = %d,%v", c.class, ord, ok)
		}
	}
	profs := map[string]string{
		"arduino_uno": MinTargetAvr, "cortex_m": MinTargetMcu32,
		"pi_linux": MinTargetPosix, "portable": MinTargetPosix,
		"": MinTargetPosix, "martian": MinTargetPosix,
	}
	for p, want := range profs {
		if got := ClassOfProfile(p); got != want {
			t.Fatalf("ClassOfProfile(%q) = %q, want %q", p, got, want)
		}
	}
}

// TestParseC_MinTargetDirective pins the directive extraction: the tag is
// captured, does not leak into the prose doc, and absence yields "".
// Português: Pina a extração da diretiva: capturada, sem vazar na prosa,
// e ausência produz "".
func TestParseC_MinTargetDirective(t *testing.T) {
	src := []byte(`
// Serves the portal. Blocks forever.
//
// label:Portal server.
// min-target:posix.
void portal_server_start(
	// connection:mandatory.
	int port
) { (void)port; }

// label:Anywhere.
int tiny(void) { return 0; }
`)
	def, err := ParseC(src, DefaultParserLimits())
	if err != nil {
		t.Logf("parse warnings: %v", err)
	}
	byName := map[string]NamedFuncDef{}
	for _, f := range def.Functions {
		byName[f.Name] = f
	}
	srv := byName["portal_server_start"]
	if srv.MinTarget != "posix" {
		t.Fatalf("MinTarget = %q, want posix", srv.MinTarget)
	}
	if got := srv.FuncDef.Doc; got != "Serves the portal. Blocks forever." {
		t.Fatalf("directive leaked into doc: %q", got)
	}
	if byName["tiny"].MinTarget != "" {
		t.Fatalf("absent tag must stay empty: %+v", byName["tiny"])
	}
}
