// blackbox/analyzer.go — Semantic analyzer for BlackBox Go source code.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Ported from the original blackboxAnalyzer.go in the legacy project.
// Two analysis passes:
//
//	Pass 1 — go/parser (syntax)
//	  Catches malformed code: missing braces, bad tokens, etc.
//	  Only runs Pass 2 if Pass 1 produces zero errors.
//
//	Pass 2 — go/types (semantics)
//	  Uses a LenientImporter: stdlib resolves normally; any package the
//	  server doesn't have (TinyGo's "machine", IoT drivers, etc.) gets an
//	  empty stub. Stub package names are recorded and used to filter out
//	  false-positive errors (e.g. "machine.I2C undefined") while keeping
//	  real errors (e.g. "undefined: nile", "wrong return type").
//
//	  Critically: isStubArtifact checks for "pkgname." — not just "pkgname" —
//	  so it won't accidentally suppress errors like "undefined: machinelike".
package blackbox

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path"
	"strings"
	"sync"
	"time"
)

// ─── Result types ─────────────────────────────────────────────────────────────

// Diagnostic is one code problem, compatible with Monaco IMarkerData.
// All positions are 1-based.
type Diagnostic struct {
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	EndLine  int    `json:"endLine"`
	EndCol   int    `json:"endCol"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" | "warning" | "info"
	Source   string `json:"source"`   // "go/parser" | "go/types"
}

// AnalysisResult is the payload returned by Analyze.
type AnalysisResult struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	DurationMs  int64        `json:"durationMs"`
	HasErrors   bool         `json:"hasErrors"`
}

// ─── Public API ───────────────────────────────────────────────────────────────

// Analyze runs both passes and returns a complete AnalysisResult.
// Safe to call concurrently — no shared mutable state.
func Analyze(src []byte) AnalysisResult {
	diags, ms := runAnalysis(src)
	hasErrors := false
	for _, d := range diags {
		if d.Severity == "error" {
			hasErrors = true
			break
		}
	}
	return AnalysisResult{
		Diagnostics: ensureNonNil(diags),
		DurationMs:  ms,
		HasErrors:   hasErrors,
	}
}

// ─── Multi-file analysis (GoMF) ───────────────────────────────────────────────

// SourceFile is one authored file handed to AnalyzeFiles. A local type
// on purpose — this package must not import codegen/blackbox just to
// borrow its FileEntry; the HTTP boundary converts.
//
// Português: Um arquivo autoral para o AnalyzeFiles. Tipo local de
// propósito — este pacote não importa codegen/blackbox só para pegar
// FileEntry emprestado; a borda HTTP converte.
type SourceFile struct {
	Path    string
	Content string
}

// FileDiagnostics is one file's bucket of the package-level analysis.
type FileDiagnostics struct {
	Path        string       `json:"path"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	HasErrors   bool         `json:"hasErrors"`
}

// FilesAnalysisResult is the payload returned by AnalyzeFiles: one
// bucket per INPUT file, in input order (tab order), plus aggregates.
type FilesAnalysisResult struct {
	Files      []FileDiagnostics `json:"files"`
	DurationMs int64             `json:"durationMs"`
	HasErrors  bool              `json:"hasErrors"`
}

// AnalyzeFiles runs the same two passes as Analyze over a WHOLE file
// set, the way Go itself sees it: go/types is a package-level checker,
// and feeding it one file of a multi-file package manufactures noise —
// a methods-only run.go would light "undefined: Probe" on every legit
// line because the struct lives in device.go. So:
//
//   - Pass 1 (syntax) runs per file, each parsed under its REAL path
//     into one shared FileSet; diagnostics bucket by position filename.
//   - A package-name mismatch is reported as a diagnostic on the
//     offending file's package clause (the analyzer never hard-fails —
//     its job is squiggles, not gates; the parser owns the hard error).
//   - ANY syntax error skips pass 2 entirely — the same stance the
//     single-file Analyze takes: type errors over a broken AST are
//     misleading noise, and with several files one broken sibling
//     would smear phantom "undefined" errors across the healthy ones.
//   - Pass 2 (semantics) type-checks ALL files as one package and
//     buckets each error by the file its position lands in.
//
// Every input file gets a bucket, even an empty one — the editor uses
// empty buckets to CLEAR a tab's stale markers.
//
// Português: As mesmas duas passadas do Analyze sobre o CONJUNTO, como
// o Go enxerga: go/types é verificador de pacote, e um run.go só-métodos
// sozinho acenderia "undefined: Probe" em linha legítima. Sintaxe por
// arquivo (caminho real, FileSet compartilhado); pacote divergente vira
// diagnóstico na cláusula package (o analisador nunca barra — rabisco,
// não portão); QUALQUER erro de sintaxe pula a semântica (postura do
// single-file: types sobre AST quebrado é ruído, e um irmão quebrado
// borraria fantasmas nos saudáveis); a semântica checa TUDO como um
// pacote e distribui por arquivo. Todo arquivo ganha bucket, mesmo
// vazio — é assim que o editor LIMPA markers velhos de uma aba.
func AnalyzeFiles(files []SourceFile) FilesAnalysisResult {
	start := time.Now()

	buckets := make(map[string][]Diagnostic, len(files))
	for _, f := range files {
		buckets[f.Path] = nil
	}
	addDiag := func(path string, d Diagnostic) {
		buckets[path] = append(buckets[path], d)
	}

	// ── Pass 1: syntax, per file, real paths, one FileSet ──────────────
	fset := token.NewFileSet()
	var astFiles []*ast.File
	syntaxBroken := false
	for _, f := range files {
		af, parseErr := parser.ParseFile(fset, f.Path, []byte(f.Content),
			parser.AllErrors|parser.ParseComments)
		if parseErr != nil {
			syntaxBroken = true
			var errList scanner.ErrorList
			if el, ok := parseErr.(scanner.ErrorList); ok {
				errList = el
			} else {
				errList = scanner.ErrorList{&scanner.Error{Msg: parseErr.Error()}}
			}
			for _, e := range errList {
				d := Diagnostic{
					Message:  sanitizeMsg(e.Msg),
					Severity: "error",
					Source:   "go/parser",
				}
				if e.Pos.IsValid() {
					d.Line = e.Pos.Line
					d.Col = e.Pos.Column
					d.EndLine = e.Pos.Line
					d.EndCol = e.Pos.Column + 1
				}
				addDiag(f.Path, d)
			}
			continue
		}
		astFiles = append(astFiles, af)
	}

	// ── Package-name coherence as a DIAGNOSTIC on the offender ─────────
	if len(astFiles) > 1 {
		first := astFiles[0].Name.Name
		for _, af := range astFiles[1:] {
			if af.Name.Name == first {
				continue
			}
			pos := fset.Position(af.Name.Pos())
			addDiag(pos.Filename, Diagnostic{
				Line: pos.Line, Col: pos.Column,
				EndLine: pos.Line, EndCol: pos.Column + len(af.Name.Name),
				Message: fmt.Sprintf("package %q here, but the first file declares %q — all files of a black box belong to one package",
					af.Name.Name, first),
				Severity: "error",
				Source:   "go/parser",
			})
			syntaxBroken = true
		}
	}

	// ── Pass 2: semantics over the WHOLE package ─────────────────────
	if !syntaxBroken && len(astFiles) > 0 {
		imp := newLenientImporter()
		var typeErrs []error
		conf := types.Config{
			Importer: imp,
			Error:    func(err error) { typeErrs = append(typeErrs, err) },
		}
		_, _ = conf.Check("blackbox", fset, astFiles, nil)

		stubNames := imp.stubBaseNames()
		for _, e := range typeErrs {
			te, ok := e.(types.Error)
			if !ok {
				continue
			}
			if isStubArtifact(te.Msg, stubNames) {
				continue
			}
			pos := fset.Position(te.Pos)
			addDiag(pos.Filename, Diagnostic{
				Line:     pos.Line,
				Col:      pos.Column,
				EndLine:  pos.Line,
				EndCol:   pos.Column + estimateTokenLen(te.Msg),
				Message:  sanitizeMsg(te.Msg),
				Severity: "error",
				Source:   "go/types",
			})
		}
	}

	// ── Assemble in INPUT order (tab order) ─────────────────────────
	out := FilesAnalysisResult{DurationMs: time.Since(start).Milliseconds()}
	for _, f := range files {
		fd := FileDiagnostics{Path: f.Path, Diagnostics: ensureNonNil(buckets[f.Path])}
		for _, d := range fd.Diagnostics {
			if d.Severity == "error" {
				fd.HasErrors = true
				out.HasErrors = true
				break
			}
		}
		out.Files = append(out.Files, fd)
	}
	return out
}

// ─── Core analysis ──────────────────────────────────────────────────────────────

func runAnalysis(src []byte) ([]Diagnostic, int64) {
	start := time.Now()
	var diags []Diagnostic

	// ── Pass 1: syntax ────────────────────────────────────────────────────────
	fset := token.NewFileSet()
	f, parseErr := parser.ParseFile(
		fset, "",
		src,
		parser.AllErrors|parser.ParseComments,
	)

	if parseErr != nil {
		var errList scanner.ErrorList
		if el, ok := parseErr.(scanner.ErrorList); ok {
			errList = el
		} else {
			errList = scanner.ErrorList{&scanner.Error{Msg: parseErr.Error()}}
		}
		for _, e := range errList {
			d := Diagnostic{
				Message:  sanitizeMsg(e.Msg),
				Severity: "error",
				Source:   "go/parser",
			}
			if e.Pos.IsValid() {
				d.Line = e.Pos.Line
				d.Col = e.Pos.Column
				d.EndLine = e.Pos.Line
				d.EndCol = e.Pos.Column + 1
			}
			diags = append(diags, d)
		}
		// Broken AST → type errors would be misleading noise. Stop here.
		return ensureNonNil(diags), time.Since(start).Milliseconds()
	}

	// ── Pass 2: semantics ─────────────────────────────────────────────────────
	imp := newLenientImporter()

	var typeErrs []error
	conf := types.Config{
		Importer: imp,
		// Collect ALL type errors instead of stopping at the first one.
		Error: func(err error) {
			typeErrs = append(typeErrs, err)
		},
	}

	_, _ = conf.Check("blackbox", fset, []*ast.File{f}, nil)

	// Filter and convert type errors to Diagnostics.
	stubNames := imp.stubBaseNames()
	for _, e := range typeErrs {
		te, ok := e.(types.Error)
		if !ok {
			continue
		}
		// Skip errors that are artifacts of a stub (hardware/IoT) package.
		// "machine.I2C undefined" is expected — user's hardware import is intentional.
		if isStubArtifact(te.Msg, stubNames) {
			continue
		}

		pos := fset.Position(te.Pos)
		diags = append(diags, Diagnostic{
			Line:     pos.Line,
			Col:      pos.Column,
			EndLine:  pos.Line,
			EndCol:   pos.Column + estimateTokenLen(te.Msg),
			Message:  sanitizeMsg(te.Msg),
			Severity: "error",
			Source:   "go/types",
		})
	}

	return ensureNonNil(diags), time.Since(start).Milliseconds()
}

// ─── Lenient importer ─────────────────────────────────────────────────────────

type lenientImporter struct {
	stdlib types.Importer
	stubs  map[string]*types.Package // importPath → empty stub
	mu     sync.Mutex
}

func newLenientImporter() *lenientImporter {
	return &lenientImporter{
		stdlib: importer.Default(),
		stubs:  make(map[string]*types.Package),
	}
}

// Import resolves stdlib packages normally.
// Everything else gets a named empty stub and is recorded.
func (li *lenientImporter) Import(importPath string) (*types.Package, error) {
	if pkg, err := li.stdlib.Import(importPath); err == nil {
		return pkg, nil
	}

	li.mu.Lock()
	defer li.mu.Unlock()

	if stub, ok := li.stubs[importPath]; ok {
		return stub, nil
	}

	// path.Base("tinygo.org/x/drivers/dht") → "dht"
	pkgName := path.Base(importPath)
	stub := types.NewPackage(importPath, pkgName)
	stub.MarkComplete() // required: prevents the type-checker from re-importing
	li.stubs[importPath] = stub
	return stub, nil
}

// stubBaseNames returns the set of base package names for all stub packages
// created during this import session.
func (li *lenientImporter) stubBaseNames() map[string]bool {
	li.mu.Lock()
	defer li.mu.Unlock()

	names := make(map[string]bool, len(li.stubs))
	for p := range li.stubs {
		names[path.Base(p)] = true
	}
	return names
}

// ─── False-positive filter ────────────────────────────────────────────────────

// isStubArtifact reports whether a type-error message is a false positive
// caused by a stub (hardware/IoT) package.
//
// It checks for "pkgname." — not just "pkgname" — to avoid accidentally
// suppressing errors that merely contain the package name as a substring.
//
// Examples when "machine" is a stub:
//
//	"machine.I2C undefined"  → filtered   (false positive — hardware type)
//	"undefined: nile"        → kept       (real user error)
//	"machinelike not found"  → kept       (different identifier)
func isStubArtifact(msg string, stubNames map[string]bool) bool {
	for name := range stubNames {
		if strings.Contains(msg, name+".") {
			return true
		}
	}
	return false
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// sanitizeMsg strips the "filename:line:col: " prefix that go/types sometimes
// embeds inside the message string (positions are already in structured fields).
func sanitizeMsg(msg string) string {
	s := msg
	for i := 0; i < 3; i++ {
		colon := strings.Index(s, ":")
		if colon < 0 {
			break
		}
		prefix := s[:colon]
		rest := strings.TrimPrefix(s[colon+1:], " ")

		// Strip if prefix is all digits (line/col number)
		isNum := len(prefix) > 0
		for _, c := range prefix {
			if c < '0' || c > '9' {
				isNum = false
				break
			}
		}
		if isNum {
			s = rest
			continue
		}
		// Strip if prefix looks like a filename (contains dot or slash)
		if strings.ContainsAny(prefix, "./") {
			s = rest
			continue
		}
		break
	}
	return strings.TrimSpace(s)
}

func estimateTokenLen(msg string) int {
	if idx := strings.LastIndex(msg, ": "); idx >= 0 {
		rest := msg[idx+2:]
		end := strings.IndexAny(rest, " (")
		if end > 0 {
			return end
		}
		if len(rest) > 0 {
			return len(rest)
		}
	}
	return 1
}

func ensureNonNil(d []Diagnostic) []Diagnostic {
	if d == nil {
		return []Diagnostic{}
	}
	return d
}
