// server/codegen/blackbox/attribution_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// authored is a tiny helper: a def with (or without) an author.
func authored(username, url string) *BlackBoxDef {
	if username == "" && url == "" {
		return &BlackBoxDef{}
	}
	return &BlackBoxDef{Author: &AuthorInfo{Username: username, URL: url}}
}

func TestAttributionManifest_Empty(t *testing.T) {
	// Nil map, and defs that carry no author (e.g. the maker's own code), both
	// yield no manifest so the caller can append unconditionally.
	if got := AttributionManifest(nil); got != "" {
		t.Fatalf("nil defs: want empty, got %q", got)
	}
	defs := map[string]*BlackBoxDef{
		"A": authored("", ""), // authorless
		"B": {Author: nil},    // explicit nil
	}
	if got := AttributionManifest(defs); got != "" {
		t.Fatalf("authorless defs: want empty, got %q", got)
	}
}

func TestAttributionManifest_DedupAndOrder(t *testing.T) {
	// Two devices from the same author collapse to one entry; multiple authors
	// are sorted deterministically regardless of Go map-iteration order.
	defs := map[string]*BlackBoxDef{
		"Zebra":  authored("zoe", "https://github.com/zoe/zebra"),
		"Zebra2": authored("zoe", "https://github.com/zoe/zebra"), // duplicate
		"Alpha":  authored("amy", "https://github.com/amy/alpha"),
	}
	got := AttributionManifest(defs)
	if got == "" {
		t.Fatal("want a manifest, got empty")
	}

	// Deterministic order: amy sorts before zoe.
	if iAmy, iZoe := strings.Index(got, "amy"), strings.Index(got, "zoe"); iAmy < 0 || iZoe < 0 || iAmy > iZoe {
		t.Fatalf("authors not sorted (amy before zoe):\n%s", got)
	}

	// Dedup: exactly one zoe entry.
	if n := strings.Count(got, "https://github.com/zoe/zebra"); n != 1 {
		t.Fatalf("want 1 zoe entry, got %d:\n%s", n, got)
	}

	// Every line is a // comment (valid in both Go and C99).
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if !strings.HasPrefix(line, "//") {
			t.Fatalf("non-comment line %q in:\n%s", line, got)
		}
	}

	// Entry format: "- <username> (<url>)".
	if !strings.Contains(got, "- amy (https://github.com/amy/alpha)") {
		t.Fatalf("missing/badly-formatted amy entry:\n%s", got)
	}

	// Determinism: the same input yields byte-identical output.
	if got2 := AttributionManifest(defs); got2 != got {
		t.Fatalf("non-deterministic output:\n--- 1 ---\n%s\n--- 2 ---\n%s", got, got2)
	}
}

func TestAttributionManifest_PartialFields(t *testing.T) {
	// Username-only and URL-only authors still render sensibly.
	defs := map[string]*BlackBoxDef{
		"U": authored("onlyname", ""),
		"R": authored("", "https://github.com/x/only-url"),
	}
	got := AttributionManifest(defs)
	if !strings.Contains(got, "- onlyname\n") {
		t.Fatalf("username-only entry wrong:\n%s", got)
	}
	if !strings.Contains(got, "- https://github.com/x/only-url\n") {
		t.Fatalf("url-only entry wrong:\n%s", got)
	}
}
