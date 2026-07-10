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

// oneFile wraps a single source into the file-set shape the dispatch
// consumes — the single-file case of the one representation.
func oneFile(path, src string) []FileEntry {
	return []FileEntry{{Path: path, Content: src}}
}

// TestParseForLanguageDispatch verifies the single dispatch point routes each
// language token to the correct parser, and rejects unknown languages instead
// of silently falling back to Go (the exact failure that hid C99 devices).
func TestParseForLanguageDispatch(t *testing.T) {
	limits := DefaultParserLimits()

	// Go token spellings → Go parser. Covers the stage token ("go"), the
	// programming_languages.id token ("golang"), empty (legacy default),
	// case, and whitespace.
	for _, lang := range []string{"go", "golang", "GO", "  go  ", ""} {
		def, err := ParseForLanguageFiles(lang, oneFile("dev.go", goSrcForDispatch), limits)
		if def == nil {
			t.Fatalf("ParseForLanguageFiles(%q, goSrc) = nil def, err=%v", lang, err)
		}
		if len(def.Files) != 1 || def.Files[0].Path != "dev.go" {
			t.Fatalf("ParseForLanguageFiles(%q): def must carry its snapshot, got %+v", lang, def.Files)
		}
		for _, fn := range def.Functions {
			if fn.SourceFile != "dev.go" {
				t.Fatalf("single-file provenance: %s.SourceFile = %q, want dev.go", fn.Name, fn.SourceFile)
			}
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
		def, err := ParseForLanguageFiles(lang, oneFile("dev.c", shtC), limits)
		if def == nil {
			t.Fatalf("ParseForLanguageFiles(%q, shtC) = nil def, err=%v", lang, err)
		}
		if len(def.Functions) == 0 {
			t.Fatalf("ParseForLanguage(%q): a C99 device must expose Functions, got 0", lang)
		}
	}

	// Routing actually differs: a Go source asked for as C must NOT come back
	// as the Go struct result. Proves "go" and "c" hit distinct engines, not
	// one hardcoded parser.
	if cAsDef, _ := ParseForLanguageFiles("c", oneFile("dev.c", goSrcForDispatch), limits); cAsDef != nil && cAsDef.Name == "Sensor" {
		t.Fatal("ParseForLanguageFiles routed a C request to the Go parser")
	}

	// Unknown language is a hard error, never a silent Go fallback.
	def, err := ParseForLanguageFiles("python", oneFile("dev.go", goSrcForDispatch), limits)
	if def != nil || err == nil {
		t.Fatalf("ParseForLanguageFiles(python) = (%v, %v), want (nil, error)", def, err)
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Fatalf("ParseForLanguageFiles(python) error = %q, want to contain 'unsupported language'", err)
	}

	// Multi-file Go (GoMF): the dispatch routes N > 1 through ParseGoFiles.
	// The smoke here is routing-only — the merge semantics live in
	// parser_go_files_test.go. Two copies of the same struct is the
	// two-structs error (one device family per project), which proves the
	// multi-file walker ran instead of the old "not supported" rejection.
	multiGo := []FileEntry{
		{Path: "a.go", Content: goSrcForDispatch},
		{Path: "b.go", Content: goSrcForDispatch},
	}
	if def, err := ParseForLanguageFiles("go", multiGo, limits); def != nil || err == nil {
		t.Fatalf("ParseForLanguageFiles(go, 2 structs) = (%v, %v), want (nil, error)", def, err)
	} else if !strings.Contains(err.Error(), "ONE device family") {
		t.Fatalf("two-structs error = %q, want the one-family rule", err)
	}

	// Assets never reach the walkers (unified asset model): a Go project
	// carrying device.go + an asset routes through the SINGLE-FILE path —
	// len() downstream means SOURCE files. The filter is the dispatch's
	// single choke point.
	goPlusAsset := []FileEntry{
		{Path: "device.go", Content: goSrcForDispatch},
		{Path: "templates/portal.html", Content: "<html>"},
	}
	if def, err := ParseForLanguageFiles("go", goPlusAsset, limits); def == nil || err != nil {
		t.Fatalf("go+asset must parse as single-file: (%v, %v)", def, err)
	} else if len(def.Files) != 1 || def.Files[0].Path != "device.go" {
		t.Fatalf("def.Files must be SOURCE-only; got %+v", def.Files)
	}

	// Empty set mirrors Parse(nil): an empty def, no error — the "new
	// project, nothing typed yet" state must not explode.
	if def, err := ParseForLanguageFiles("go", nil, limits); def == nil || err != nil {
		t.Fatalf("ParseForLanguageFiles(go, empty) = (%v, %v), want (def, nil)", def, err)
	}
}
