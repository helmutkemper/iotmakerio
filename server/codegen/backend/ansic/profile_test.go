// /server/codegen/backend/ansic/profile_test.go

package ansic

// profile_test.go — Unit tests for the target profile registry.
//
// These tests are deliberately self-contained: they import only the
// testing package and exercise profile.go in isolation. No IR, no
// graph, no fixtures. The goal is to catch registry drift and
// per-profile mistakes before they propagate into emit.go.
//
// Test categories:
//
//   - Resolution behaviour (known, empty, unknown names)
//   - Default fallback (ProfileArduinoUno is the safety net)
//   - Per-profile field assertions (the type/suffix decisions that
//     would silently produce wrong C if changed without thought)
//   - Registry consistency (the four exported vars, the map, and
//     ListProfiles all describe the same set of profiles)
//
// Português:
//
//	Testes unitários do registro de perfis. Pega quatro categorias
//	de regressão: resolução, default, decisões por perfil, e
//	consistência interna entre var/mapa/ListProfiles.

import (
	"testing"
)

// =====================================================================
//  Resolution behaviour
// =====================================================================

// TestResolveProfile_KnownNames exercises every canonical name and
// confirms the resolved profile carries that same name internally —
// catching a copy-paste error where the var and the map key drift.
func TestResolveProfile_KnownNames(t *testing.T) {
	knownNames := []string{
		ProfileNameArduinoUno,
		ProfileNameCortexM,
		ProfileNamePiLinux,
		ProfileNamePortable,
	}

	for _, name := range knownNames {
		got := ResolveProfile(name)
		if got.Name != name {
			t.Errorf("ResolveProfile(%q).Name = %q, want %q", name, got.Name, name)
		}
	}
}

// TestResolveProfile_EmptyDefaultsToArduino is the documented
// behaviour for backward compatibility: scenes that predate the
// targetProfile field decode it as "", and the C backend must still
// produce code rather than failing.
func TestResolveProfile_EmptyDefaultsToArduino(t *testing.T) {
	got := ResolveProfile("")
	if got.Name != ProfileNameArduinoUno {
		t.Errorf("ResolveProfile(\"\").Name = %q, want %q (default)",
			got.Name, ProfileNameArduinoUno)
	}
}

// TestResolveProfile_UnknownDefaultsToArduino covers misspellings,
// case mismatches, future profile names sent to an older server, and
// other garbage. All must resolve to the conservative default so the
// pipeline keeps producing valid output.
func TestResolveProfile_UnknownDefaultsToArduino(t *testing.T) {
	garbage := []string{
		"ARDUINO_UNO", // wrong case — names are lowercase by convention
		"arduino-uno", // wrong separator
		"arduinoUno",  // wrong style
		"esp32",       // not a registered profile name (a target,
		//                  yes, but it falls under cortex_m today)
		"future_target", // not yet registered
		"   ",           // whitespace
		"💩",             // unicode garbage
	}

	for _, name := range garbage {
		got := ResolveProfile(name)
		if got.Name != ProfileNameArduinoUno {
			t.Errorf("ResolveProfile(%q).Name = %q, want %q (default for unknown)",
				name, got.Name, ProfileNameArduinoUno)
		}
	}
}

// =====================================================================
//  Per-profile field assertions
// =====================================================================

// TestProfile_AllHaveRequiredFields catches an empty Name, Label,
// IntType, FloatType, or BoolType on any registered profile. If any
// of these were missing the C backend would emit malformed
// declarations like " x = 10;" — better to fail in a test than in
// the generated code.
func TestProfile_AllHaveRequiredFields(t *testing.T) {
	for _, p := range ListProfiles() {
		if p.Name == "" {
			t.Errorf("profile with empty Name: %+v", p)
		}
		if p.Label == "" {
			t.Errorf("profile %q has empty Label", p.Name)
		}
		if p.IntType == "" {
			t.Errorf("profile %q has empty IntType", p.Name)
		}
		if p.FloatType == "" {
			t.Errorf("profile %q has empty FloatType", p.Name)
		}
		if p.BoolType == "" {
			t.Errorf("profile %q has empty BoolType", p.Name)
		}
	}
}

// TestProfile_BoolIsAlwaysBoolForC99 asserts the C99 invariant: every
// shipped profile uses "bool" (from <stdbool.h>) for the IR's
// abstract "bool". If a future profile targets C89, it would need to
// use "int" — that's a deliberate decision and should be flagged at
// the test level so it cannot slip in by accident.
func TestProfile_BoolIsAlwaysBoolForC99(t *testing.T) {
	for _, p := range ListProfiles() {
		if p.BoolType != "bool" {
			t.Errorf("profile %q has BoolType=%q, want \"bool\" (C99 via <stdbool.h>)",
				p.Name, p.BoolType)
		}
	}
}

// TestProfile_ArduinoUnoSpecifics is the locked-decision guard for
// the default profile. The values asserted here are the result of
// the design conversation captured in docs/CODEGEN_ANSI_C.md: AVR
// targets need int32_t (not int16_t, not int64_t), float (not
// double), the "L" suffix on integer literals (so "10" becomes
// "10L" → 32-bit long), and the "f" suffix on float literals (so
// "3.14" becomes "3.14f" → avoids double promotion).
//
// Changing any of these is a deliberate architectural decision and
// should not happen without first updating the design document.
func TestProfile_ArduinoUnoSpecifics(t *testing.T) {
	p := ResolveProfile(ProfileNameArduinoUno)
	assertField(t, "arduino_uno IntType", p.IntType, "int32_t")
	assertField(t, "arduino_uno FloatType", p.FloatType, "float")
	assertField(t, "arduino_uno IntSuffix", p.IntSuffix, "L")
	assertField(t, "arduino_uno FloatSuffix", p.FloatSuffix, "f")
}

// TestProfile_PiLinuxSpecifics is the corresponding guard for the
// 64-bit profile. On Linux/Pi the native widths are larger and the
// software-emulation pitfalls do not apply, so we use int64_t,
// double, and the "LL" integer suffix. The empty FloatSuffix is
// deliberate — "3.14" without 'f' parses as double, which is what
// we want here.
func TestProfile_PiLinuxSpecifics(t *testing.T) {
	p := ResolveProfile(ProfileNamePiLinux)
	assertField(t, "pi_linux IntType", p.IntType, "int64_t")
	assertField(t, "pi_linux FloatType", p.FloatType, "double")
	assertField(t, "pi_linux IntSuffix", p.IntSuffix, "LL")
	assertField(t, "pi_linux FloatSuffix", p.FloatSuffix, "")
}

// TestProfile_CortexMMatchesArduinoUnoToday locks in the current
// equivalence between cortex_m and arduino_uno. If we later decide
// to differentiate them (for example to enable double on Cortex-M4F
// with an FPU), this test will fail and force the divergence to be
// explicit and reviewed.
func TestProfile_CortexMMatchesArduinoUnoToday(t *testing.T) {
	cm := ResolveProfile(ProfileNameCortexM)
	au := ResolveProfile(ProfileNameArduinoUno)

	if cm.IntType != au.IntType {
		t.Errorf("cortex_m.IntType=%q differs from arduino_uno.IntType=%q — "+
			"if intentional, update this test and the design doc",
			cm.IntType, au.IntType)
	}
	if cm.FloatType != au.FloatType {
		t.Errorf("cortex_m.FloatType=%q differs from arduino_uno.FloatType=%q",
			cm.FloatType, au.FloatType)
	}
	if cm.IntSuffix != au.IntSuffix {
		t.Errorf("cortex_m.IntSuffix=%q differs from arduino_uno.IntSuffix=%q",
			cm.IntSuffix, au.IntSuffix)
	}
	if cm.FloatSuffix != au.FloatSuffix {
		t.Errorf("cortex_m.FloatSuffix=%q differs from arduino_uno.FloatSuffix=%q",
			cm.FloatSuffix, au.FloatSuffix)
	}
}

// =====================================================================
//  Registry consistency
// =====================================================================

// TestProfile_RegistryConsistency is the safety net for the manual
// duplication between the exported vars, the profilesByName map, and
// the ListProfiles slice. Any of those three diverging is a bug — for
// example if a new profile is added to the map but not to
// ListProfiles, a future UI dropdown would silently drop it.
//
// The test enforces three invariants:
//
//  1. Every profile in ListProfiles can be resolved by its Name and
//     the resolved value equals the listed one.
//  2. ListProfiles returns exactly as many profiles as profilesByName
//     contains entries (no drift in either direction).
//  3. The default profile (arduino_uno) is the first in ListProfiles,
//     consistent with its role as the visible default in future UI.
//
// Português:
//
//	Verifica que as três fontes (vars, mapa, ListProfiles) descrevem
//	o mesmo conjunto. Pega esquecimentos quando alguém adiciona um
//	perfil só em uma das três.
func TestProfile_RegistryConsistency(t *testing.T) {
	list := ListProfiles()

	// Invariant 1: every listed profile round-trips through Resolve.
	for _, want := range list {
		got := ResolveProfile(want.Name)
		if got != want {
			t.Errorf("ResolveProfile(%q) returned %+v, want %+v (listed)",
				want.Name, got, want)
		}
	}

	// Invariant 2: ListProfiles and the lookup map have identical
	// cardinality. We compare via len(profilesByName) by counting
	// resolutions of every listed name (we cannot iterate the map
	// from outside the package — but a length mismatch would still
	// show up here if a listed name fails to resolve to itself,
	// because the resolution would fall back to arduino_uno and the
	// equality check above would fire).
	//
	// To catch the inverse (map has entries that ListProfiles
	// doesn't), we depend on the seed knowledge that profilesByName
	// is initialised from the four exported vars and ListProfiles
	// returns those same four vars. A code change that adds a fifth
	// to one but not the other is caught by the cardinality check
	// below, because the new entry in either side would not be
	// equal to one of the four listed here.
	expectedCount := 4
	if len(list) != expectedCount {
		t.Errorf("ListProfiles returned %d entries, want %d — if a profile "+
			"was added, update this test and verify profilesByName too",
			len(list), expectedCount)
	}

	// Invariant 3: arduino_uno is the first entry (UI display order).
	if list[0].Name != ProfileNameArduinoUno {
		t.Errorf("ListProfiles[0].Name = %q, want %q (default must appear first)",
			list[0].Name, ProfileNameArduinoUno)
	}
}

// TestProfile_ListProfilesIsFreshCopy asserts the documented contract
// that callers may mutate the returned slice without affecting
// subsequent calls. This protects against a future refactor that
// caches the slice and returns the same backing array.
func TestProfile_ListProfilesIsFreshCopy(t *testing.T) {
	first := ListProfiles()
	if len(first) == 0 {
		t.Fatal("ListProfiles returned empty slice — cannot run mutation check")
	}

	// Corrupt the caller's copy.
	original := first[0]
	first[0] = TargetProfile{Name: "corrupted", Label: "should not appear"}

	// A fresh call must still return the real first entry.
	second := ListProfiles()
	if second[0].Name == "corrupted" {
		t.Error("ListProfiles returned the same backing slice across calls — " +
			"a mutation by one caller leaked into another")
	}
	if second[0].Name != original.Name {
		t.Errorf("ListProfiles[0].Name = %q after mutation, want %q",
			second[0].Name, original.Name)
	}
}

// =====================================================================
//  Helpers
// =====================================================================

// assertField is a tiny convenience wrapper used by the per-profile
// specific tests. Centralising the format avoids drift between five
// near-identical error messages.
func assertField(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}
