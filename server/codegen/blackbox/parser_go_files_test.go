// server/codegen/blackbox/parser_go_files_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// The fixture mirrors how a real Go package authors one device family:
// the exported struct (and Init) in device.go, the working methods in
// run.go, unexported helpers plus a manual page in helpers.go. Every
// merge rule in parser_go_files.go is pinned against this shape.
//
// Português: O fixture espelha um pacote Go real autorando UMA família:
// struct + Init no device.go, métodos no run.go, helpers não-exportados
// e uma página de manual no helpers.go. Toda regra do merge é fixada
// contra esta forma.

const gomfDevice = `// Package probe is a virtual temperature probe.
//
// The front page: this doc must become def.Doc because device.go is the
// first tab.
package probe

import "fmt"

// label:Probe. icon:temperature-half.
type Probe struct {
	// prop:"Channel". default:"0".
	Channel int
}

// label:Init probe.
func (p *Probe) Init(
	// doc:Analog channel.
	// connection:mandatory.
	channel int,
) error {
	p.Channel = channel
	return fmt.Errorf("stub")
}
`

const gomfRun = `package probe

import (
	"fmt"
	"strings"
)

// label:Read temperature.
// icon:gauge.
func (p *Probe) Read(
	// doc:Raw sample.
	// connection:mandatory.
	raw int,
) (int, error) {
	_ = strings.TrimSpace("")
	return smooth(p.Channel, raw), fmt.Errorf("stub")
}

// label:Describe.
func (p *Probe) Describe() (string, error) {
	return fmt.Sprintf("probe@%d", p.Channel), nil
}
`

const gomfHelpers = `package probe

// smooth is unexported on purpose: helpers must ride along in
// MethodsCode-adjacent source without surfacing as devices.
func smooth(previous, sample int) int {
	return previous + (sample-previous)/4
}

/* manualName: Reading
` + "```markdown" + `
How the probe reads.
` + "```" + ` */
`

func gomfFixture() []FileEntry {
	return []FileEntry{
		{Path: "device.go", Content: gomfDevice},
		{Path: "run.go", Content: gomfRun},
		{Path: "helpers.go", Content: gomfHelpers},
	}
}

// TestParseGoFiles_MergesThePackage pins the happy path: one struct, Init
// from the struct's file, methods from a sibling, front-page doc, import
// union, provenance stamps, MethodsCode concatenation, manual pages, and
// the def carrying its own snapshot.
func TestParseGoFiles_MergesThePackage(t *testing.T) {
	def, err := ParseGoFiles(gomfFixture(), DefaultParserLimits())
	if def == nil {
		t.Fatalf("ParseGoFiles: def = nil, err = %v", err)
	}
	if err != nil {
		t.Fatalf("unexpected warnings/error: %v", err)
	}

	if def.Name != "Probe" {
		t.Errorf("Name: got %q, want Probe", def.Name)
	}
	if def.Init == nil {
		t.Fatalf("Init must merge in from device.go")
	}
	if !strings.Contains(def.Doc, "front page") {
		t.Errorf("Doc must be device.go's package doc (first tab); got %q", def.Doc)
	}
	if len(def.Props) != 1 || def.Props[0].FieldName != "Channel" {
		t.Errorf("Props from the struct file drifted: %+v", def.Props)
	}

	// Methods: run.go's two, in source order, each stamped with its file.
	if len(def.Methods) != 2 {
		t.Fatalf("Methods: got %d, want 2 (Read, Describe)", len(def.Methods))
	}
	if def.Methods[0].Name != "Read" || def.Methods[1].Name != "Describe" {
		t.Errorf("method order drift: %q, %q", def.Methods[0].Name, def.Methods[1].Name)
	}
	for _, m := range def.Methods {
		if m.SourceFile != "run.go" {
			t.Errorf("%s.SourceFile: got %q, want run.go", m.Name, m.SourceFile)
		}
	}

	// Imports: union, first-seen order — fmt from device.go first, then
	// run.go's strings; no duplicates.
	wantImports := []string{"fmt", "strings"}
	if len(def.Imports) != len(wantImports) {
		t.Fatalf("Imports: got %v, want %v", def.Imports, wantImports)
	}
	for i := range wantImports {
		if def.Imports[i] != wantImports[i] {
			t.Fatalf("Imports order: got %v, want %v", def.Imports, wantImports)
		}
	}

	// MethodsCode carries BOTH files' matching methods (Init from
	// device.go, Read/Describe from run.go) — the codegen inlines this
	// string, so it must be the merged implementation.
	for _, needle := range []string{"func (p *Probe) Init", "func (p *Probe) Read", "func (p *Probe) Describe"} {
		if !strings.Contains(def.MethodsCode, needle) {
			t.Errorf("MethodsCode missing %q", needle)
		}
	}
	if strings.Contains(def.MethodsCode, "func smooth") {
		t.Errorf("unexported helper leaked into MethodsCode")
	}

	// The manual parser normalises page names to lowercase — assert the
	// canonical form, not the authored casing.
	if len(def.ManualPages) != 1 || def.ManualPages[0].Name != "reading" {
		t.Errorf("manual page from helpers.go must merge: %+v", def.ManualPages)
	}

	if len(def.Files) != 3 {
		t.Errorf("def must carry its snapshot: got %d files", len(def.Files))
	}
}

// TestParseGoFiles_LoudErrors pins every hard-error rule and WHY it is
// loud where the C merge is tolerant: Go has no prototype/definition
// duality, so duplicates are redeclarations the compiler would reject.
func TestParseGoFiles_LoudErrors(t *testing.T) {
	limits := DefaultParserLimits()

	t.Run("duplicate method across files", func(t *testing.T) {
		files := []FileEntry{
			{Path: "device.go", Content: gomfDevice},
			{Path: "run.go", Content: gomfRun},
			{Path: "extra.go", Content: "package probe\n\nfunc (p *Probe) Read(raw int) (int, error) { return raw, nil }\n"},
		}
		def, err := ParseGoFiles(files, limits)
		if def != nil || err == nil {
			t.Fatalf("duplicate method: got (%v, %v), want (nil, error)", def, err)
		}
		for _, needle := range []string{"Probe.Read", "run.go", "extra.go", "redeclaration"} {
			if !strings.Contains(err.Error(), needle) {
				t.Errorf("error %q must name %q", err, needle)
			}
		}
	})

	t.Run("two exported structs", func(t *testing.T) {
		files := []FileEntry{
			{Path: "device.go", Content: gomfDevice},
			{Path: "second.go", Content: "package probe\n\ntype Display struct{}\n\nfunc (d *Display) Show() error { return nil }\n"},
		}
		def, err := ParseGoFiles(files, limits)
		if def != nil || err == nil {
			t.Fatalf("two structs: got (%v, %v), want (nil, error)", def, err)
		}
		for _, needle := range []string{"Probe", "Display", "device.go", "second.go", "ONE device family"} {
			if !strings.Contains(err.Error(), needle) {
				t.Errorf("error %q must name %q", err, needle)
			}
		}
	})

	t.Run("package mismatch", func(t *testing.T) {
		files := []FileEntry{
			{Path: "device.go", Content: gomfDevice},
			{Path: "oops.go", Content: "package sensor\n\nfunc helper() {}\n"},
		}
		def, err := ParseGoFiles(files, limits)
		if def != nil || err == nil {
			t.Fatalf("package mismatch: got (%v, %v), want (nil, error)", def, err)
		}
		for _, needle := range []string{`"probe"`, `"sensor"`, "oops.go"} {
			if !strings.Contains(err.Error(), needle) {
				t.Errorf("error %q must name %q", err, needle)
			}
		}
	})

	t.Run("syntax error carries the real path", func(t *testing.T) {
		files := []FileEntry{
			{Path: "device.go", Content: gomfDevice},
			{Path: "broken.go", Content: "package probe\n\nfunc (p *Probe) Bad( {\n"},
		}
		_, err := ParseGoFiles(files, limits)
		if err == nil || !strings.Contains(err.Error(), "broken.go:") {
			t.Fatalf("syntax error must point at the authored path; got %v", err)
		}
	})
}

// TestParseGoFiles_WarningsCarryTheRealPath: the soft-warning contract
// (def non-nil WITH error) survives the merge, and the message names the
// authored file instead of the single-file era's conventional
// "<Struct>.go".
func TestParseGoFiles_WarningsCarryTheRealPath(t *testing.T) {
	files := []FileEntry{
		{Path: "device.go", Content: gomfDevice},
		{Path: "noisy.go", Content: `package probe

// label:Poke.
func (p *Probe) Poke(value int) error { return nil }
`},
	}
	def, err := ParseGoFiles(files, DefaultParserLimits())
	if def == nil {
		t.Fatalf("warnings must keep the def: err = %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "parse warnings") {
		t.Fatalf("missing-connection input must surface as a soft warning; err = %v", err)
	}
	if !strings.Contains(err.Error(), "noisy.go") {
		t.Errorf("warning must carry the REAL path; got %q", err)
	}
}

// TestParseGoFiles_SingleEntryStaysCoherent: one file through the
// multi-file walker yields the same surface Parse yields — the guard
// against drift between the two paths the dispatch keeps separate.
func TestParseGoFiles_SingleEntryStaysCoherent(t *testing.T) {
	single := []FileEntry{{Path: "device.go", Content: gomfDevice}}
	multi, mErr := ParseGoFiles(single, DefaultParserLimits())
	direct, dErr := Parse([]byte(gomfDevice), DefaultParserLimits())
	if (mErr == nil) != (dErr == nil) {
		t.Fatalf("error parity drift: ParseGoFiles=%v Parse=%v", mErr, dErr)
	}
	if multi == nil || direct == nil {
		t.Fatalf("defs: %v / %v", multi, direct)
	}
	if multi.Name != direct.Name ||
		(multi.Init == nil) != (direct.Init == nil) ||
		len(multi.Methods) != len(direct.Methods) ||
		len(multi.Props) != len(direct.Props) ||
		multi.Doc != direct.Doc {
		t.Errorf("surface drift between walkers:\n multi: name=%q init=%v methods=%d props=%d\ndirect: name=%q init=%v methods=%d props=%d",
			multi.Name, multi.Init != nil, len(multi.Methods), len(multi.Props),
			direct.Name, direct.Init != nil, len(direct.Methods), len(direct.Props))
	}
}
