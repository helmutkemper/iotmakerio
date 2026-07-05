// server/codegen/blackbox/naming_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

func TestSymbolPrefix(t *testing.T) {
	if got, want := SymbolPrefix("3f9a2b1c"), "P3f9a2b1c_"; got != want {
		t.Fatalf("SymbolPrefix: want %q, got %q", want, got)
	}
}

func TestSourceDir(t *testing.T) {
	if got, want := SourceDir("3f9a2b1c"), "bb_3f9a2b1c"; got != want {
		t.Fatalf("SourceDir: want %q, got %q", want, got)
	}
}

func TestPrefixSymbol_AlwaysPrefixes(t *testing.T) {
	const id = "3f9a2b1c"

	// A plain function name (the simple-black-box case) is prefixed.
	if got, want := PrefixSymbol(id, "init"), "P3f9a2b1c_init"; got != want {
		t.Fatalf("plain: want %q, got %q", want, got)
	}

	// A specialist's own namespace (the community habit) is KEPT and still
	// prefixed — no idempotence check strips or skips it.
	if got, want := PrefixSymbol(id, "sensorlib_init"), "P3f9a2b1c_sensorlib_init"; got != want {
		t.Fatalf("own namespace: want %q, got %q", want, got)
	}

	// SECURITY: a symbol that already carries ANOTHER source's prefix is stamped
	// with THIS source's id on top — never left as-is. This is the whole reason
	// the prefix is applied unconditionally (no HasPrefix shortcut), and this
	// test guards against anyone "optimising" that away.
	if got, want := PrefixSymbol("5f9d2b1f", "P3f9a2b1c_sensorlib_init"),
		"P5f9d2b1f_P3f9a2b1c_sensorlib_init"; got != want {
		t.Fatalf("hijack attempt must be stamped over (the security property): want %q, got %q", want, got)
	}
}

func TestPrefixSymbol_Idempotent_SameSource(t *testing.T) {
	// Applying the SAME source's prefix twice does double it — that is expected
	// and harmless in practice, because a source's own emitted symbols are never
	// fed back through PrefixSymbol a second time. Documented here so the
	// behaviour is a decision, not an accident: prefixing is a pure prepend, with
	// NO "already prefixed?" check (which would be the security hole above).
	const id = "3f9a2b1c"
	once := PrefixSymbol(id, "init")
	twice := PrefixSymbol(id, once)
	if want := "P3f9a2b1c_P3f9a2b1c_init"; twice != want {
		t.Fatalf("double application: want %q, got %q", want, twice)
	}
}
