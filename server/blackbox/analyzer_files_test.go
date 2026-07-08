// server/blackbox/analyzer_files_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// The multi-file analyzer's reason to exist, pinned: a methods-only file
// must be CLEAN when its sibling declares the struct — feeding go/types
// one file at a time is what manufactured "undefined: Probe" noise.
//
// Português: A razão de existir do analisador multiarquivo, fixada: um
// arquivo só-métodos fica LIMPO quando o irmão declara o struct —
// alimentar o go/types um arquivo por vez é o que fabricava ruído.
func TestAnalyzeFiles_PackageLevelTypeCheck(t *testing.T) {
	files := []SourceFile{
		{Path: "device.go", Content: "package probe\n\ntype Probe struct{ Channel int }\n"},
		{Path: "run.go", Content: "package probe\n\nfunc (p *Probe) Read() int { return p.Channel }\n"},
	}
	res := AnalyzeFiles(files)
	if res.HasErrors {
		t.Fatalf("legit package flagged: %+v", res.Files)
	}
	if len(res.Files) != 2 {
		t.Fatalf("every input file gets a bucket: got %d", len(res.Files))
	}
	for _, fd := range res.Files {
		if len(fd.Diagnostics) != 0 {
			t.Errorf("%s: unexpected diagnostics %+v", fd.Path, fd.Diagnostics)
		}
	}
}

// A real type error lands in the file it lives in — not smeared onto the
// sibling.
func TestAnalyzeFiles_ErrorAttributedToItsFile(t *testing.T) {
	files := []SourceFile{
		{Path: "device.go", Content: "package probe\n\ntype Probe struct{ Channel int }\n"},
		{Path: "run.go", Content: "package probe\n\nfunc (p *Probe) Read() int { return p.Missing }\n"},
	}
	res := AnalyzeFiles(files)
	if !res.HasErrors {
		t.Fatalf("missing field must be flagged")
	}
	byPath := map[string]FileDiagnostics{}
	for _, fd := range res.Files {
		byPath[fd.Path] = fd
	}
	if len(byPath["device.go"].Diagnostics) != 0 {
		t.Errorf("error smeared onto the healthy sibling: %+v", byPath["device.go"].Diagnostics)
	}
	if !byPath["run.go"].HasErrors {
		t.Errorf("run.go must carry the error; got %+v", byPath["run.go"])
	}
}

// One broken sibling: its syntax errors surface, the semantic pass is
// skipped entirely (the single-file stance, extended: types over a broken
// package smears phantom "undefined" errors across healthy files), so the
// healthy sibling stays visually clean.
func TestAnalyzeFiles_SyntaxErrorSkipsSemantics(t *testing.T) {
	files := []SourceFile{
		{Path: "device.go", Content: "package probe\n\ntype Probe struct{ Channel int }\n\nfunc (p *Probe) Read() int { return p.Channel }\n"},
		{Path: "broken.go", Content: "package probe\n\nfunc (p *Probe) Bad( {\n"},
	}
	res := AnalyzeFiles(files)
	if !res.HasErrors {
		t.Fatalf("syntax error must be flagged")
	}
	byPath := map[string]FileDiagnostics{}
	for _, fd := range res.Files {
		byPath[fd.Path] = fd
	}
	if !byPath["broken.go"].HasErrors {
		t.Errorf("broken.go must carry its syntax error")
	}
	if len(byPath["device.go"].Diagnostics) != 0 {
		t.Errorf("healthy file must stay clean while a sibling is broken; got %+v",
			byPath["device.go"].Diagnostics)
	}
	for _, d := range byPath["broken.go"].Diagnostics {
		if d.Source == "go/types" {
			t.Errorf("semantic pass must be skipped on broken syntax; got %+v", d)
		}
	}
}

// Package mismatch is a DIAGNOSTIC on the offender's package clause —
// the analyzer paints squiggles, the parser owns the hard gate.
func TestAnalyzeFiles_PackageMismatchDiagnostic(t *testing.T) {
	files := []SourceFile{
		{Path: "device.go", Content: "package probe\n\ntype Probe struct{}\n\nfunc (p *Probe) Read() {}\n"},
		{Path: "oops.go", Content: "package sensor\n\nfunc helper() {}\n"},
	}
	res := AnalyzeFiles(files)
	if !res.HasErrors {
		t.Fatalf("mismatch must be flagged")
	}
	var offender *FileDiagnostics
	for i := range res.Files {
		if res.Files[i].Path == "oops.go" {
			offender = &res.Files[i]
		}
	}
	if offender == nil || !offender.HasErrors {
		t.Fatalf("diagnostic must land on the offender; got %+v", res.Files)
	}
	if !strings.Contains(offender.Diagnostics[0].Message, `"sensor"`) {
		t.Errorf("message must name the packages: %q", offender.Diagnostics[0].Message)
	}
	if offender.Diagnostics[0].Line != 1 {
		t.Errorf("diagnostic must sit on the package clause; line = %d", offender.Diagnostics[0].Line)
	}
}
