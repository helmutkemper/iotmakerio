// server/codegen/blackbox/naming_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestNaming_Family pins every surface of the iotm_<n> family composed from
// one code: folder, file pair, symbol prefix and guard. One knob (the
// radical) moves them all — asserted with both the default and a custom
// radical, and with the long full-id fallback code.
func TestNaming_Family(t *testing.T) {
	def := Naming{} // zero value → DefaultRadical, valid by design
	cases := []struct {
		name       string
		n          Naming
		code, want string
		fn         func(Naming, string) string
	}{
		{"symbol prefix", def, "47", "iotm_47_", Naming.SymbolPrefix},
		{"source dir", def, "47", "iotm_47", Naming.SourceDir},
		{"source name", def, "47", "iotm_47.c", Naming.SourceName},
		{"header name", def, "47", "iotm_47.h", Naming.HeaderName},
		{"guard", def, "47", "IOTM_47_H", Naming.Guard},
		// Custom radical moves the whole family.
		{"custom prefix", NewNaming("acme_"), "47", "acme_47_", Naming.SymbolPrefix},
		{"custom dir", NewNaming("acme_"), "47", "acme_47", Naming.SourceDir},
		{"custom guard", NewNaming("acme_"), "47", "ACME_47_H", Naming.Guard},
		// Fallback code (full database id) rides the same family: long but
		// correct, per the BlackBoxDef.CodeIdent contract.
		{"fallback id", def, "3f9a2b1c", "iotm_3f9a2b1c_", Naming.SymbolPrefix},
	}
	for _, c := range cases {
		if got := c.fn(c.n, c.code); got != c.want {
			t.Errorf("%s: want %q, got %q", c.name, c.want, got)
		}
	}
}

// TestNewNaming_TolerantFallback pins the configuration stance: empty or
// invalid radicals (bad first char, forbidden chars, over the length cap)
// silently degrade to the default family instead of failing the export.
func TestNewNaming_TolerantFallback(t *testing.T) {
	for _, bad := range []string{
		"",                   // empty
		"1abc_",              // digit first — invalid C identifier start
		"acme-",              // '-' is not an identifier char
		"acme prefix_",       // space
		"waaaaaay_too_long_", // over maxRadicalLen
	} {
		if got := NewNaming(bad).Radical(); got != DefaultRadical {
			t.Errorf("NewNaming(%q).Radical() = %q, want default %q", bad, got, DefaultRadical)
		}
	}
	if got := NewNaming("acme_").Radical(); got != "acme_" {
		t.Errorf("valid radical rejected: got %q", got)
	}
	// Trailing underscore is convention, not requirement.
	if got := NewNaming("Acme9").SymbolPrefix("47"); got != "Acme947_" {
		t.Errorf("verbatim radical: want %q, got %q", "Acme947_", got)
	}
}

// TestNaming_PrefixSymbol_AlwaysPrefixes carries the security test forward
// from the P<id>_ era: the prefix is applied UNCONDITIONALLY, and the
// property is independent of the radical value.
func TestNaming_PrefixSymbol_AlwaysPrefixes(t *testing.T) {
	n := Naming{}

	// A plain function name (the simple case) is prefixed.
	if got, want := n.PrefixSymbol("47", "init"), "iotm_47_init"; got != want {
		t.Fatalf("plain: want %q, got %q", want, got)
	}

	// A specialist's own namespace (the community habit) is KEPT and still
	// prefixed — no idempotence check strips or skips it.
	if got, want := n.PrefixSymbol("47", "sensorlib_init"), "iotm_47_sensorlib_init"; got != want {
		t.Fatalf("own namespace: want %q, got %q", want, got)
	}

	// SECURITY: a symbol that already carries ANOTHER box's prefix is stamped
	// with THIS box's code on top — never left as-is. This is the whole reason
	// the prefix is applied unconditionally (no HasPrefix shortcut), and this
	// test guards against anyone "optimising" that away.
	if got, want := n.PrefixSymbol("103", "iotm_47_sensorlib_init"),
		"iotm_103_iotm_47_sensorlib_init"; got != want {
		t.Fatalf("hijack attempt must be stamped over (the security property): want %q, got %q", want, got)
	}

	// The property survives a maker-configured radical too: the attacker
	// cannot even know which radical a given export will use, but the stamp
	// covers every guess anyway.
	custom := NewNaming("acme_")
	if got, want := custom.PrefixSymbol("103", "acme_47_steal"),
		"acme_103_acme_47_steal"; got != want {
		t.Fatalf("hijack under custom radical: want %q, got %q", want, got)
	}
}

// TestNaming_PrefixSymbol_Idempotent_SameSource documents (as a decision, not
// an accident) that applying the SAME box's prefix twice doubles it: pure
// prepend, no "already prefixed?" check — that check would be the security
// hole above.
func TestNaming_PrefixSymbol_Idempotent_SameSource(t *testing.T) {
	n := Naming{}
	once := n.PrefixSymbol("47", "init")
	twice := n.PrefixSymbol("47", once)
	if want := "iotm_47_iotm_47_init"; twice != want {
		t.Fatalf("double application: want %q, got %q", want, twice)
	}
}

// TestValidRadical pins the identifier rules the scene configuration is
// validated against.
func TestValidRadical(t *testing.T) {
	valid := []string{"iotm_", "acme_", "_x", "A9_", "a"}
	invalid := []string{"", "9a", "a-b", "a b", "å_", "waaaaaay_too_long_"}
	for _, v := range valid {
		if !ValidRadical(v) {
			t.Errorf("ValidRadical(%q) = false, want true", v)
		}
	}
	for _, v := range invalid {
		if ValidRadical(v) {
			t.Errorf("ValidRadical(%q) = true, want false", v)
		}
	}
}
