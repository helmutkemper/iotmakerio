// server/codegen/blackbox/parser_go_files.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// parser_go_files.go — Multi-file Go parsing: N authored .go files in,
// ONE merged BlackBoxDef out.
//
// English:
//
//	What "multi-file Go" MEANS here (the governing decision): the same
//	single-device-family black box the Go path has always produced,
//	authored the way a real Go package is — the exported struct in one
//	file, methods spread across siblings, unexported helpers anywhere.
//	It is NOT "several devices per project": the Go product model is
//	struct-centric (exactly one exported struct = one device family),
//	and changing that would be a different feature, not a file-count
//	change.
//
//	This file is an ORCHESTRATOR over parser.go's per-file primitives
//	(findExportedStruct, extractFuncDef, findMethod, parseManualBlocks,
//	…). Parse itself — the battle-tested single-file path — is not
//	touched: the dispatch keeps routing one-file sets through it, and
//	this walker exists only for N > 1. The duplication between the two
//	is the loop SKELETON, not logic; the skeletons genuinely differ
//	(cross-file deduplication, path-aware messages, package equality).
//
//	Merge rules, and why they are LOUD where the C merge is tolerant:
//
//	  package name    all files must declare the same package — hard
//	                  error naming both files otherwise. Go itself
//	                  refuses to compile such a set.
//	  exported struct exactly ONE across the whole set. Zero keeps the
//	                  single-file error; two or more is a hard error
//	                  naming the files ("one device family per
//	                  project"). Within one file, the first exported
//	                  struct wins — the same per-file tolerance Parse
//	                  has always had.
//	  Init / methods  collected across files in tab order (tab order IS
//	                  snapshot order — the same doctrine as the C
//	                  merge). A method name seen twice is a HARD error:
//	                  unlike C, Go has no prototype/definition duality —
//	                  a second definition is a redeclaration the Go
//	                  compiler would reject, so surfacing it loudly at
//	                  parse time beats an inscrutable link-stage story.
//	  package doc     the first file (tab order) carrying one is the
//	                  front page — mirroring the C merge's rule.
//	  imports         union, first-seen order. Methods inlined by the
//	                  Go codegen may need imports declared in their own
//	                  file; the union is what keeps the generated
//	                  program compiling.
//	  manual pages    appended across files in tab order.
//	  provenance      every method is stamped with SourceFile (6c-1),
//	                  so wizard cards badge and rewrites target the
//	                  file the method actually lives in. Init has no
//	                  SourceFile slot (it is a bare FuncDef by design);
//	                  its card simply carries no badge.
//	  warnings        the soft-warning contract is preserved: a non-nil
//	                  def returned WITH an error means "parsed, with
//	                  warnings" — messages carry the REAL file path
//	                  instead of Parse's conventional "<Struct>.go".
//
// Português:
//
//	O que "Go multiarquivo" SIGNIFICA aqui (a decisão governante): o
//	mesmo black box de UMA família de devices de sempre, autorado como
//	um pacote Go real — struct exportado num arquivo, métodos nos
//	irmãos, helpers não-exportados em qualquer lugar. NÃO é "vários
//	devices por projeto": o modelo Go é struct-cêntrico, e mudar isso
//	seria outra feature.
//
//	Este arquivo é um ORQUESTRADOR sobre as primitivas por-arquivo do
//	parser.go; o Parse (caminho de arquivo único, testado em batalha)
//	não é tocado — o dispatch continua roteando conjuntos de um arquivo
//	por ele.
//
//	Regras do merge, ALTAS onde o C é tolerante: pacote igual em todos
//	os arquivos (erro nomeando ambos); exatamente UM struct exportado
//	no conjunto; método repetido é ERRO — Go não tem a dualidade
//	protótipo/definição do C, redeclaração é erro de compilador, então
//	gritar no parse vence uma história de link inescrutável. Doc de
//	pacote = primeiro arquivo que tiver (capa, como no C); imports em
//	união; páginas de manual anexadas; proveniência carimbada por
//	método (6c-1). O contrato de warnings é preservado (def não-nil +
//	erro = "parseou, com avisos"), com o caminho REAL do arquivo nas
//	mensagens.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// goFilePart is one authored file after the syntactic pass: its AST plus
// everything the merge needs to reference it by name.
type goFilePart struct {
	path string
	src  []byte
	file *ast.File
}

// ParseGoFiles parses a multi-file Go source set into one merged
// BlackBoxDef. See the file-level doc for the merge rules. The dispatch
// routes single-file sets through Parse — callers should not special-case;
// use ParseForLanguageFiles.
//
// Português: Parseia um conjunto Go multiarquivo num BlackBoxDef fundido.
// Regras no doc do arquivo. O dispatch roteia conjuntos de um arquivo pelo
// Parse; chamadores usam ParseForLanguageFiles.
func ParseGoFiles(files []FileEntry, limits ParserLimits) (*BlackBoxDef, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go source files provided")
	}

	// ── Pass 1: syntax, per file. One shared FileSet keeps positions
	// coherent and — because ParseFile is given the REAL authored path —
	// makes every go/parser error message read "helpers.go:12: …" instead
	// of the single-file era's conventional "blackbox.go".
	fset := token.NewFileSet()
	parts := make([]goFilePart, 0, len(files))
	for _, f := range files {
		src := []byte(f.Content)
		astFile, err := parser.ParseFile(fset, f.Path, src, parser.ParseComments)
		if err != nil {
			// go/parser already prefixes the position with the path we
			// gave it; wrapping again would print the path twice.
			return nil, fmt.Errorf("parse error: %w", err)
		}
		parts = append(parts, goFilePart{path: f.Path, src: src, file: astFile})
	}

	// ── Package equality: Go itself refuses a mixed-package directory,
	// so the parser mirrors the compiler instead of inventing tolerance
	// the toolchain downstream would not honour.
	pkgName := parts[0].file.Name.Name
	for _, p := range parts[1:] {
		if p.file.Name.Name != pkgName {
			return nil, fmt.Errorf(
				"package mismatch: %s declares package %q but %s declares package %q; all files of a black box belong to one package",
				parts[0].path, pkgName, p.path, p.file.Name.Name)
		}
	}

	// ── The ONE exported struct across the set. Within a file, the first
	// exported struct wins (Parse's own per-file tolerance); across files,
	// a second struct-carrying file is a hard error — one device family
	// per project is the Go product model, not a parser limitation.
	var (
		structPart *goFilePart
		structName string
		structType *ast.StructType
		structNode ast.Node
		structDoc  string
	)
	for i := range parts {
		name, st, node, doc := findExportedStruct(parts[i].file)
		if name == "" {
			continue
		}
		if structPart != nil {
			return nil, fmt.Errorf(
				"two exported structs found: %q in %s and %q in %s; a black box authors ONE device family — split into separate projects",
				structName, structPart.path, name, parts[i].path)
		}
		structPart = &parts[i]
		structName, structType, structNode, structDoc = name, st, node, doc
	}
	if structPart == nil {
		return nil, fmt.Errorf("no exported struct found; black-box requires exactly one exported struct")
	}

	def := &BlackBoxDef{}
	def.Name = structName
	def.Props = extractProps(structType, limits)
	def.StructCode = nodeSource(fset, structPart.src, structNode)
	if structDoc != "" {
		_, _, def.StructIcon, def.StructLabel, _, _, _ = extractDocDirectives(structDoc)
		def.Interactive = extractInteractiveDirective(structDoc)
	}

	// ── Front page: the first file (tab order) with a package doc — the
	// same rule the C merge applies, and the same "first file is the
	// cover" instinct a reader brings to any codebase.
	for _, p := range parts {
		if p.file.Doc != nil {
			def.Doc = strings.TrimSpace(p.file.Doc.Text())
			break
		}
	}

	// ── Imports: union, first-seen order. The Go codegen inlines method
	// bodies into the generated program; a method living in run.go may
	// lean on an import only run.go declares, so dropping any file's
	// imports would emit a program that does not compile.
	seenImport := make(map[string]bool)
	for _, p := range parts {
		for _, imp := range extractImports(p.file) {
			if seenImport[imp] {
				continue
			}
			seenImport[imp] = true
			def.Imports = append(def.Imports, imp)
		}
	}

	// ── Init + methods across files, tab order. Duplicates are LOUD (see
	// the file doc for the contrast with the C merge's tolerance).
	var parseWarnings []string
	methodFile := make(map[string]string) // method name → path of first sighting

	checkPortLimits := func(where, methodName string, rawInputs, rawOutputs int) {
		if rawInputs > clamp(limits.MaxInputs, compiledDefaultMaxInputs) {
			parseWarnings = append(parseWarnings, fmt.Sprintf(
				"%s: %s.%s: %d input ports found but only the first %d are used (limit: %d)",
				where, structName, methodName, rawInputs,
				clamp(limits.MaxInputs, compiledDefaultMaxInputs),
				clamp(limits.MaxInputs, compiledDefaultMaxInputs)))
		}
		if rawOutputs > clamp(limits.MaxOutputs, compiledDefaultMaxOutputs) {
			parseWarnings = append(parseWarnings, fmt.Sprintf(
				"%s: %s.%s: %d output ports found but only the first %d are used (limit: %d)",
				where, structName, methodName, rawOutputs,
				clamp(limits.MaxOutputs, compiledDefaultMaxOutputs),
				clamp(limits.MaxOutputs, compiledDefaultMaxOutputs)))
		}
	}
	checkMissingConn := func(where, methodName string, fd FuncDef) {
		for _, prt := range fd.Inputs {
			if prt.MissingConn {
				parseWarnings = append(parseWarnings, fmt.Sprintf(
					"%s: %s.%s input %q (%s): missing connection: tag — add // connection: mandatory. or // connection: optional. above the parameter",
					where, structName, methodName, prt.Name, prt.GoType))
			}
		}
	}

	for pi := range parts {
		p := &parts[pi]
		for _, decl := range p.file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || funcDecl.Recv == nil {
				continue
			}
			if !ast.IsExported(funcDecl.Name.Name) {
				continue
			}
			matchesStruct := false
			for _, recv := range funcDecl.Recv.List {
				if receiverTypeName(recv.Type) == structName {
					matchesStruct = true
					break
				}
			}
			if !matchesStruct {
				continue
			}

			name := funcDecl.Name.Name
			if prev, dup := methodFile[name]; dup {
				return nil, fmt.Errorf(
					"method %s.%s defined twice: in %s and in %s; Go does not allow redeclaration — remove one",
					structName, name, prev, p.path)
			}
			methodFile[name] = p.path

			if name == "Init" {
				initDef := extractFuncDef(fset, p.file, funcDecl, limits)
				def.Init = &initDef
				rawIn, rawOut := countRawPorts(funcDecl)
				checkPortLimits(p.path, "Init", rawIn, rawOut)
				checkMissingConn(p.path, "Init", initDef)
				continue
			}

			if len(def.Methods) >= limits.MaxMethods {
				return nil, fmt.Errorf(
					"%s has more than %d exported methods (found at least %q in %s); "+
						"reduce the number of exported methods or split into multiple devices",
					structName, limits.MaxMethods, name, p.path)
			}

			fd := extractFuncDef(fset, p.file, funcDecl, limits)
			nfd := NamedFuncDef{Name: name, FuncDef: fd}
			// Provenance (6c-1): the wizard badges the card and the
			// rewrite targets the file this method actually lives in.
			nfd.SourceFile = p.path
			def.Methods = append(def.Methods, nfd)

			rawIn, rawOut := countRawPorts(funcDecl)
			checkPortLimits(p.path, name, rawIn, rawOut)
			checkMissingConn(p.path, name, fd)
		}
	}

	if def.Init == nil && len(def.Methods) == 0 {
		return nil, fmt.Errorf("no methods found on %s; at least one method (Init or any other exported method) is required", structName)
	}

	// ── Prop-count soft warning, attributed to the struct's file.
	var rawPropCount int
	if structType.Fields != nil {
		for _, f := range structType.Fields.List {
			if len(f.Names) == 0 || !ast.IsExported(f.Names[0].Name) {
				continue
			}
			rawPropCount++
		}
	}
	if rawPropCount > clamp(limits.MaxProps, compiledDefaultMaxProps) {
		parseWarnings = append(parseWarnings, fmt.Sprintf(
			"%s: %s: %d prop-tagged fields found but only the first %d are used (limit: %d)",
			structPart.path, structName, rawPropCount,
			clamp(limits.MaxProps, compiledDefaultMaxProps),
			clamp(limits.MaxProps, compiledDefaultMaxProps)))
	}

	// ── MethodsCode: each file's matching methods, verbatim, concatenated
	// in tab order — the Go codegen inlines this string into the generated
	// program, so the concatenation IS the merged implementation.
	var methodChunks []string
	for _, p := range parts {
		if chunk := extractAllMethods(fset, p.src, p.file, structName); strings.TrimSpace(chunk) != "" {
			methodChunks = append(methodChunks, chunk)
		}
	}
	def.MethodsCode = strings.Join(methodChunks, "\n\n")

	// ── Manual pages: appended across files in tab order. Same-name
	// collisions get whatever behaviour two same-name blocks in ONE file
	// already have — the merge invents no new policy here.
	var pageWarnings []string
	for _, p := range parts {
		pages, ws := parseManualBlocks(string(p.src), def)
		def.ManualPages = append(def.ManualPages, pages...)
		pageWarnings = append(pageWarnings, ws...)
	}

	// The def carries its own snapshot (the single-representation rule —
	// see BlackBoxDef.Files), on the warnings path too: "parsed, with
	// warnings" is still parsed.
	def.Files = files

	allWarnings := append(parseWarnings, pageWarnings...)
	if len(allWarnings) > 0 {
		return def, fmt.Errorf("parse warnings: %s", joinWarnings(allWarnings))
	}
	return def, nil
}
