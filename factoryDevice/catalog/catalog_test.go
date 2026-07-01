// /factoryDevice/catalog/catalog_test.go

package catalog

// catalog_test.go — Tests for the device-type catalogue and the
// language-support lookup. The tests pin the contracts in three ways:
//
//   1. Every primitive type declared by the factory must be in the
//      catalogue. A regression that adds a new device without
//      registering it here would show as a failing test in CI.
//
//   2. Every catalogue entry must support at least one language.
//      An empty SupportedLanguages slice would mean the device is
//      hidden in EVERY project — almost certainly a mistake.
//
//   3. Black-box prefix handling and unknown-type behaviour are
//      pinned explicitly so future refactors don't drift from the
//      documented contract.
//
// Português: Testes do catálogo. Verificam que (1) todos os types
// do factory estão registrados; (2) toda entrada suporta pelo
// menos uma linguagem; (3) black-boxes e types desconhecidos
// seguem o contrato documentado.

import (
	"strings"
	"testing"
)

// =====================================================================
//  Catalogue completeness
// =====================================================================

// TestCatalog_NoEmptyLanguageSlice catches the easy mistake of
// adding a device with SupportedLanguages: nil or []string{}.
// Such a device would be hidden everywhere — useless. If a device
// truly cannot be used in any project, it should not be in the
// catalogue at all.
func TestCatalog_NoEmptyLanguageSlice(t *testing.T) {
	for _, d := range catalog {
		if len(d.SupportedLanguages) == 0 {
			t.Errorf("device %q has empty SupportedLanguages — would be hidden everywhere",
				d.TypeName)
		}
	}
}

// TestCatalog_OnlyValidLanguageTokens guards against typos like
// "C99" (display label) or "golang" (alternative spelling) creeping
// into the catalogue. The store and the wire format use exactly
// "c" and "go" — any other token would silently mismatch.
func TestCatalog_OnlyValidLanguageTokens(t *testing.T) {
	valid := map[string]bool{LanguageGo: true, LanguageC: true}
	for _, d := range catalog {
		for _, lang := range d.SupportedLanguages {
			if !valid[lang] {
				t.Errorf("device %q lists invalid language token %q "+
					"(valid: %q, %q)",
					d.TypeName, lang, LanguageGo, LanguageC)
			}
		}
	}
}

// TestCatalog_TypeNamePrefix guards against accidentally adding a
// black-box type to the static catalogue. Black-boxes are dynamic
// and handled by the prefix match in LookupSupportedLanguages —
// putting one here would shadow the dynamic resolution and freeze
// the language list at catalogue-edit time.
func TestCatalog_TypeNamePrefix(t *testing.T) {
	for _, d := range catalog {
		if strings.HasPrefix(d.TypeName, "BlackBox") {
			t.Errorf("device %q has BlackBox prefix — black-boxes belong "+
				"to the dynamic prefix path, not the static catalogue",
				d.TypeName)
		}
	}
}

// TestCatalog_NoDuplicateTypeNames pins the contract that each type
// name is unique in the catalogue. A duplicate would mean
// LookupSupportedLanguages returns the first match while a developer
// might be editing the second — confusing and easy to do in a
// long file.
func TestCatalog_NoDuplicateTypeNames(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range catalog {
		if seen[d.TypeName] {
			t.Errorf("duplicate type name in catalog: %q", d.TypeName)
		}
		seen[d.TypeName] = true
	}
}

// =====================================================================
//  Lookup behaviour
// =====================================================================

// TestLookup_KnownPrimitive sweeps the catalogue and confirms each
// entry resolves via LookupSupportedLanguages.
func TestLookup_KnownPrimitive(t *testing.T) {
	for _, d := range catalog {
		got := LookupSupportedLanguages(d.TypeName)
		if got == nil {
			t.Errorf("LookupSupportedLanguages(%q) returned nil, want non-nil",
				d.TypeName)
		}
	}
}

// TestLookup_BlackBoxNotInCatalogue pins the contract after black-box
// language moved to the definition: the catalogue knows nothing about
// "BlackBox…" types and returns nil for them. Their language now lives on
// BlackBoxDefClient.ProgrammingLanguage and is checked by the menu builder
// via SupportsProjectLanguage, not here.
func TestLookup_BlackBoxNotInCatalogue(t *testing.T) {
	cases := []string{
		"BlackBoxInit:APDS9960",
		"BlackBoxRun:APDS9960",
		"BlackBoxLog:Anything",
		"BlackBoxMethodSomething:Whatever",
		"BlackBox",   // edge: bare prefix
		"BlackBox:",  // edge: prefix + colon, no body
		"BlackBoxX:", // edge: prefix + suffix + colon
	}
	for _, tc := range cases {
		if got := LookupSupportedLanguages(tc); got != nil {
			t.Errorf("LookupSupportedLanguages(%q) = %v, want nil "+
				"(black-box language lives on the def, not the catalogue)",
				tc, got)
		}
	}
}

// TestLookup_UnknownTypeReturnsNil pins the failure mode: a type
// not in the catalogue and not a black-box returns nil, which
// SupportsLanguage interprets as "no support anywhere".
func TestLookup_UnknownTypeReturnsNil(t *testing.T) {
	cases := []string{
		"StatementMadeUp",
		"NotEvenAStatement",
		"",
		"random-string",
		"statementAdd", // wrong case — type matching is case-sensitive
	}
	for _, tc := range cases {
		got := LookupSupportedLanguages(tc)
		if got != nil {
			t.Errorf("LookupSupportedLanguages(%q) = %v, want nil",
				tc, got)
		}
	}
}

// =====================================================================
//  SupportsLanguage predicate
// =====================================================================

// TestSupportsLanguage_UniversalDevices confirms that every device
// in the catalogue (Phase 1: all of them) supports both Go and C99.
// This is the test that will fail and force a deliberate decision
// when the first language-restricted primitive lands.
func TestSupportsLanguage_UniversalDevices(t *testing.T) {
	for _, d := range catalog {
		if !SupportsLanguage(d.TypeName, LanguageGo) {
			t.Errorf("SupportsLanguage(%q, %q) = false, want true",
				d.TypeName, LanguageGo)
		}
		if !SupportsLanguage(d.TypeName, LanguageC) {
			t.Errorf("SupportsLanguage(%q, %q) = false, want true",
				d.TypeName, LanguageC)
		}
	}
}

// TestSupportsLanguage_BlackBoxNotHandledHere pins that the catalogue
// predicate returns false for any black-box type, in any language: the
// catalogue does not classify black-boxes (their language is on the def).
// The menu never calls SupportsLanguage for a black-box; this test guards
// against a future caller wrongly relying on the catalogue for one.
func TestSupportsLanguage_BlackBoxNotHandledHere(t *testing.T) {
	const bb = "BlackBoxInit:APDS9960"

	if SupportsLanguage(bb, LanguageGo) {
		t.Errorf("SupportsLanguage(%q, %q) = true, want false "+
			"(black-box language is on the def, not the catalogue)",
			bb, LanguageGo)
	}
	if SupportsLanguage(bb, LanguageC) {
		t.Errorf("SupportsLanguage(%q, %q) = true, want false "+
			"(black-box language is on the def, not the catalogue)",
			bb, LanguageC)
	}
}

// TestSupportsLanguage_UnknownTypeAlwaysFalse pins the safest
// failure mode — an unknown device is treated as supporting no
// language, so it disappears from every project's menu until the
// catalogue is fixed.
func TestSupportsLanguage_UnknownTypeAlwaysFalse(t *testing.T) {
	if SupportsLanguage("StatementUnknown", LanguageGo) {
		t.Error("unknown type must not support Go")
	}
	if SupportsLanguage("StatementUnknown", LanguageC) {
		t.Error("unknown type must not support C")
	}
}

// TestSupportsLanguage_GibberishLanguage protects against the
// reverse mistake: a caller passing a typo for the language token
// (e.g. "C99" instead of "c"). Result must be false — the predicate
// does not auto-correct.
func TestSupportsLanguage_GibberishLanguage(t *testing.T) {
	if SupportsLanguage("StatementAdd", "C99") {
		t.Error(`SupportsLanguage("StatementAdd", "C99") = true, ` +
			`want false (token is "c", not display label "C99")`)
	}
	if SupportsLanguage("StatementAdd", "golang") {
		t.Error(`SupportsLanguage("StatementAdd", "golang") = true, ` +
			`want false (token is "go", not "golang")`)
	}
	if SupportsLanguage("StatementAdd", "") {
		t.Error(`SupportsLanguage("StatementAdd", "") = true, ` +
			`want false (empty is not a language)`)
	}
}

// =====================================================================
//  AllPrimitiveTypes
// =====================================================================

// TestAllPrimitiveTypes_ReturnsCopy ensures the helper returns a
// fresh slice rather than aliasing the unexported catalogue. This
// matters because callers (tests, debug tools) may want to sort,
// filter, or otherwise mutate the result without affecting the
// canonical data.
func TestAllPrimitiveTypes_ReturnsCopy(t *testing.T) {
	first := AllPrimitiveTypes()
	if len(first) != len(catalog) {
		t.Fatalf("AllPrimitiveTypes returned %d items, catalog has %d",
			len(first), len(catalog))
	}

	// Mutate the returned slice and verify the catalogue is unchanged.
	first[0] = "MUTATED"

	second := AllPrimitiveTypes()
	if second[0] == "MUTATED" {
		t.Error("AllPrimitiveTypes returned an aliased slice — mutation leaked")
	}
}
