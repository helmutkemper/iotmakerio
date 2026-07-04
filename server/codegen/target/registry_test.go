// server/codegen/target/registry_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package target

import "testing"

func TestResolveTarget(t *testing.T) {
	// A known id resolves to that target.
	if got := ResolveTarget("esp32_c6"); got.ID != "esp32_c6" {
		t.Fatalf("known id: want esp32_c6, got %q", got.ID)
	}
	// Empty and unknown ids fall back to the conservative default (Arduino UNO).
	for _, id := range []string{"", "does-not-exist"} {
		if got := ResolveTarget(id); got.ID != "arduino_uno" {
			t.Fatalf("id %q: want fallback arduino_uno, got %q", id, got.ID)
		}
	}
	// The default is fully populated (never a zero Target).
	if d := DefaultTarget(); d.DisplayName == "" || d.StringBufferSize == 0 || d.ProfileName == "" {
		t.Fatalf("default target under-populated: %+v", d)
	}
}

func TestAllTargets_SortedAndCopied(t *testing.T) {
	all := AllTargets()
	if len(all) < 2 {
		t.Fatalf("want at least 2 targets, got %d", len(all))
	}
	// Sorted by Order ascending.
	for i := 1; i < len(all); i++ {
		if all[i-1].Order > all[i].Order {
			t.Fatalf("not sorted by Order: %d before %d", all[i-1].Order, all[i].Order)
		}
	}
	// The returned slice is a copy: mutating it must not leak into the registry.
	all[0].DisplayName = "MUTATED"
	if ResolveTarget(all[0].ID).DisplayName == "MUTATED" {
		t.Fatal("AllTargets leaked the registry backing array")
	}
}
