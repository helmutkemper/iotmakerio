// /server/codegen/blackbox/parser_dispatch_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// goSrcForDispatch is a minimal valid Go black-box: one exported struct with
// an Init method. ParseForLanguage with a Go token must route it to Parse,
// which requires the exported struct the C parser would never produce.
const goSrcForDispatch = `package blackbox

// Sensor reads a value.
//
// icon:gear. label:Sensor.
type Sensor struct {
}

// Init starts the sensor.
//
// icon:play. label:Init.
func (s *Sensor) Init() (err error) { return nil }
`

// TestParseForLanguageDispatch verifies the single dispatch point routes each
// language token to the correct parser, and rejects unknown languages instead
// of silently falling back to Go (the exact failure that hid C99 devices).
func TestParseForLanguageDispatch(t *testing.T) {
	limits := DefaultParserLimits()

	// Go token spellings → Go parser. Covers the stage token ("go"), the
	// programming_languages.id token ("golang"), empty (legacy default),
	// case, and whitespace.
	for _, lang := range []string{"go", "golang", "GO", "  go  ", ""} {
		def, err := ParseForLanguage(lang, []byte(goSrcForDispatch), limits)
		if def == nil {
			t.Fatalf("ParseForLanguage(%q, goSrc) = nil def, err=%v", lang, err)
		}
		if def.Name != "Sensor" {
			t.Fatalf("ParseForLanguage(%q): want struct Sensor, got %q", lang, def.Name)
		}
		if len(def.Functions) != 0 {
			t.Fatalf("ParseForLanguage(%q): a Go device must have no C99 Functions, got %d",
				lang, len(def.Functions))
		}
	}

	// C token spellings → C parser. shtC is the decision-b C99 fixture; the C
	// parser yields device-functions and (no primary struct) an empty Name.
	for _, lang := range []string{"c", "c99", "C99", " c "} {
		def, err := ParseForLanguage(lang, []byte(shtC), limits)
		if def == nil {
			t.Fatalf("ParseForLanguage(%q, shtC) = nil def, err=%v", lang, err)
		}
		if len(def.Functions) == 0 {
			t.Fatalf("ParseForLanguage(%q): a C99 device must expose Functions, got 0", lang)
		}
	}

	// Routing actually differs: a Go source asked for as C must NOT come back
	// as the Go struct result. Proves "go" and "c" hit distinct engines, not
	// one hardcoded parser.
	if cAsDef, _ := ParseForLanguage("c", []byte(goSrcForDispatch), limits); cAsDef != nil && cAsDef.Name == "Sensor" {
		t.Fatal("ParseForLanguage routed a C request to the Go parser")
	}

	// Unknown language is a hard error, never a silent Go fallback.
	def, err := ParseForLanguage("python", []byte(goSrcForDispatch), limits)
	if def != nil || err == nil {
		t.Fatalf("ParseForLanguage(python) = (%v, %v), want (nil, error)", def, err)
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Fatalf("ParseForLanguage(python) error = %q, want to contain 'unsupported language'", err)
	}
}
