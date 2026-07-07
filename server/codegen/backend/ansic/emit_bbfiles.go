// server/codegen/backend/ansic/emit_bbfiles.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

import (
	"sort"
	"strings"

	"server/codegen/blackbox"
)

// Multi-file C output — one folder per black-box, like Arduino libraries.
//
// English:
//
//	The single-file model inlined every black-box's authored source verbatim
//	into main.c (see deviceSources). That does not scale: two specialists can
//	export the same symbol name, and a giant main.c is unreadable. This file
//	implements the folder model instead:
//
//	    main.c                      ← maker's generated program
//	    Makefile                    ← explicit build rules (generated)
//	    iotm_47/iotm_47.h           ← generated header (prefixed surface)
//	    iotm_47/iotm_47.c           ← preamble + authored source VERBATIM
//	    iotmaker_runtime.{h,c}      ← only when the body uses the runtime
//
//	("47" is the box's sequential code number; "iotm_" is the naming
//	radical, maker-configurable per scene in a future UI — see
//	blackbox/naming.go and docs/C99_EXPORT_NAMING.md.)
//
//	The box's sequentially-allocated CODE NUMBER is the single source of
//	uniqueness — it names the folder and prefixes every exported symbol (see
//	blackbox/naming.go and blackbox/csurface.go for the full rationale and
//	the security property). The number is stitched into def.CodeID by the
//	store loader; a def without one falls back to its full database id in
//	the same family (long but correct).
//	main.c includes each bb header and calls the PREFIXED names; the bb unit
//	renames itself to those names via its generated #define preamble.
//
//	A def WITHOUT an id (never touched the database — the documented
//	BlackBoxDef.ID contract) keeps the old inline path: the emitter must never
//	invent an identity. Both paths can coexist in one build.
//
// Português:
//
//	Modelo de pastas no lugar do inline: main.c do maker, uma pasta por
//	black-box (ex.: iotm_47/ — header gerado + fonte autoral verbatim com
//	preâmbulo de renomeação) e um Makefile explícito. O NÚMERO DE CÓDIGO
//	sequencial é a única fonte de unicidade — nomeia a pasta e prefixa os
//	símbolos; def sem número cai no id longo do banco. Def sem id segue o
//	caminho inline antigo (o emitter nunca inventa identidade); os dois
//	caminhos convivem no mesmo build.

// surfaceFor returns the public surface of the black-box that owns function
// fn, computing and caching it on first touch. Returns nil when the function
// has no def, the def is not a C99 function-device, or the def has no ID —
// nil means "bare names, inline fallback" everywhere it is consulted.
//
// Português: Superfície pública da black-box dona de fn, cacheada. Nil = sem
// identidade → nomes crus, caminho inline.
func (e *cEmitter) surfaceFor(fn string) *blackbox.CSurface {
	if e.prog == nil || e.prog.BlackBoxDefs == nil {
		return nil
	}
	def := e.prog.BlackBoxDefs[fn]
	if def == nil || def.Name != "" || def.ID == "" {
		return nil // Go-path def or no isolated identity
	}
	if e.bbSurfaces == nil {
		e.bbSurfaces = make(map[string]*blackbox.CSurface)
	}
	if s, ok := e.bbSurfaces[def.ID]; ok {
		return s
	}
	s := blackbox.NewCSurface(def, e.naming)
	e.bbSurfaces[def.ID] = s
	return s
}

// bbSymbol returns the name main.c must use to call function fn: the
// prefixed symbol when fn belongs to a multi-file black-box, the bare name
// otherwise (inline fallback, where the definition sits above main() under
// its authored name).
//
// Português: Nome que o main.c usa para chamar fn — prefixado no modelo
// multiarquivo, cru no fallback inline.
func (e *cEmitter) bbSymbol(fn string) string {
	if s := e.surfaceFor(fn); s != nil {
		return e.naming.PrefixSymbol(s.Code(), fn)
	}
	return fn
}

// bbText runs text through the surface of fn's black-box, prefixing every
// identifier that belongs to it (wire-type names, enum tags and constants,
// callback typedefs, function names). main.c-side text composed from
// AUTHORED strings — cast prefixes, return/out-param declaration types,
// "=<literal>" enum-constant defaults — must pass through here, or it would
// name the pre-rename identifiers that no longer exist outside the bb unit.
// A no-op for the inline fallback (nil surface).
//
// Português: Prefixa em text os identificadores da superfície da black-box
// de fn. Todo texto do lado do main.c composto de strings AUTORAIS (casts,
// tipos declarados, defaults "=enumerador") passa por aqui. No-op no
// fallback inline.
func (e *cEmitter) bbText(fn, text string) string {
	if s := e.surfaceFor(fn); s != nil {
		return s.PrefixIdentifiers(text)
	}
	return text
}

// bbUnits returns the surfaces of every multi-file black-box the scene uses,
// deduplicated by id and sorted by id for deterministic output. Multiple
// function-devices from one source share one *BlackBoxDef — and therefore
// one id, one folder, one unit: the Arduino-library shape.
//
// Português: Superfícies de todas as black-boxes multiarquivo da cena,
// dedupe por id, ordenadas. Várias funções de um fonte = um def = uma pasta.
func (e *cEmitter) bbUnits() []*blackbox.CSurface {
	if e.prog == nil {
		return nil
	}
	seen := make(map[string]bool)
	var units []*blackbox.CSurface
	for fn, def := range e.prog.BlackBoxDefs {
		if def == nil || def.Name != "" || def.ID == "" || def.RawSource == "" {
			continue // Go-path, no identity, or nothing to ship
		}
		if seen[def.ID] {
			continue
		}
		seen[def.ID] = true
		if s := e.surfaceFor(fn); s != nil {
			units = append(units, s)
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].ID() < units[j].ID() })
	return units
}

// bbIncludeLines returns the `#include "iotm_47/iotm_47.h"` block main.c
// carries — one line per multi-file black-box, sorted. Quoted includes are
// resolved relative to the including file, so main.c at the project root
// reaches into each folder with no -I gymnastics.
//
// Português: Bloco de #include dos headers das black-boxes no main.c, um por
// pasta, ordenado. Include com aspas resolve relativo ao arquivo que inclui.
func (e *cEmitter) bbIncludeLines(units []*blackbox.CSurface) string {
	if len(units) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("/* black-box devices (one folder per black-box) */\n")
	for _, u := range units {
		sb.WriteString("#include \"")
		sb.WriteString(e.naming.SourceDir(u.Code()))
		sb.WriteString("/")
		sb.WriteString(e.naming.HeaderName(u.Code()))
		sb.WriteString("\"\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// bbFiles assembles the per-black-box output files for units:
//
//	<dir>/<stem>.h — the generated header, main.c's ONLY view of the box
//	                    (fully generated → the caller stamps it with the
//	                    Generated Code Exception like any other emitted file).
//	<dir>/<stem>.c — generated preamble (attribution, whole-unit rename
//	                    defines, marker) + the authored source VERBATIM +
//	                    generated postamble (per-function declaration check —
//	                    the unit-local replacement for including the header,
//	                    which would redefine the surface types; see
//	                    blackbox.CSurface.Postamble). Deliberately NOT
//	                    stamped with the exception header: below the marker
//	                    this is the AUTHOR's code under the author's license,
//	                    and stamping "you may license as you choose" over it
//	                    would misstate its provenance — the exact licensing
//	                    smear the single-file model suffered from.
//
// Keys are ZIP-entry paths (forward slashes); archive/zip on the WASM side
// creates the folders from them.
//
// Português: Monta os arquivos por black-box. O .h é 100% gerado (recebe a
// exceção de código gerado) e é a única visão do main.c; o .c é preâmbulo +
// fonte do AUTOR verbatim + posâmbulo (cross-check de declaração dentro da
// unidade — incluir o header redefiniria os tipos). O .c de propósito NÃO
// recebe a exceção (o carimbo indevido era exatamente a mancha de licença do
// modelo inline). Chaves são caminhos de entrada do ZIP.
func (e *cEmitter) bbFiles(units []*blackbox.CSurface, out map[string]string) {
	for _, u := range units {
		dir := e.naming.SourceDir(u.Code())

		out[dir+"/"+e.naming.HeaderName(u.Code())] = u.Header()

		src := u.Preamble() + e.rawSourceOf(u.ID())
		if !strings.HasSuffix(src, "\n") {
			src += "\n"
		}
		src += u.Postamble()
		key := dir + "/" + e.naming.SourceName(u.Code())
		out[key] = src

		// Register the authored key so Emit's stamping loop can exempt it
		// from the Generated Code Exception BY IDENTITY, not by name pattern
		// — with a maker-configurable radical, "starts with iotm_" would be
		// both wrong (custom radicals) and fragile.
		if e.authoredFiles == nil {
			e.authoredFiles = make(map[string]bool)
		}
		e.authoredFiles[key] = true
	}
}

// rawSourceOf returns the RawSource of the def with the given id. The defs
// map is keyed by function name (several keys can share one def), so this
// scans for the id — the map is tiny (one entry per device the scene uses),
// so a scan beats maintaining a second index.
//
// Português: RawSource do def com este id. O map é chaveado por nome de
// função (várias chaves por def), então varre — o map é minúsculo.
func (e *cEmitter) rawSourceOf(id string) string {
	for _, def := range e.prog.BlackBoxDefs {
		if def != nil && def.ID == id {
			return def.RawSource
		}
	}
	return ""
}

// makefile generates the project Makefile for the multi-file output. Every
// rule is EXPLICIT — no pattern rules, no wildcards — for two reasons: the
// emitter knows the exact file list (a wildcard could swallow stray files
// the maker drops next to the project, breaking determinism), and explicit
// rules are the portable core every make dialect (GNU, BSD, POSIX) agrees
// on. Objects compile INTO the black-box folders (iotm_47/iotm_47.o) so a
// duplicate-symbol link error names both offending black-boxes by id — the
// loud, self-attributing tripwire the multi-file design promises for
// symbol collisions between unrewritten sources.
//
// CC/CFLAGS/TARGET use ?= so the maker can override from the environment
// without editing the file. -I. lets every unit resolve project-root
// includes the same way regardless of the compiler's working directory.
//
// Português: Makefile com regras EXPLÍCITAS — sem pattern rules nem
// wildcard: o emitter conhece a lista exata (wildcard engoliria arquivos
// estranhos) e regra explícita é o núcleo portátil de qualquer make. Objetos
// compilam DENTRO das pastas (iotm_47/iotm_47.o) para o erro de símbolo
// duplicado nomear as duas black-boxes culpadas pelo id. ?= permite override
// por ambiente.
func (e *cEmitter) makefile(units []*blackbox.CSurface) string {
	type src struct{ c, o, extraDep string }
	srcs := []src{{c: "main.c", o: "main.o"}}
	if e.usesRuntime {
		srcs = append(srcs, src{c: "iotmaker_runtime_stub.c", o: "iotmaker_runtime_stub.o"})
	}
	for _, u := range units {
		dir := e.naming.SourceDir(u.Code())
		stem := strings.TrimSuffix(e.naming.SourceName(u.Code()), ".c")
		srcs = append(srcs, src{
			c:        dir + "/" + e.naming.SourceName(u.Code()),
			o:        dir + "/" + stem + ".o",
			extraDep: dir + "/" + e.naming.HeaderName(u.Code()),
		})
	}

	// main.c textually includes every bb header (and the runtime header when
	// used), so main.o must rebuild when any of them changes.
	var mainDeps []string
	for _, u := range units {
		mainDeps = append(mainDeps, e.naming.SourceDir(u.Code())+"/"+e.naming.HeaderName(u.Code()))
	}
	if e.usesRuntime {
		mainDeps = append(mainDeps, "iotmaker_runtime.h")
	}

	var objs []string
	for _, s := range srcs {
		objs = append(objs, s.o)
	}

	var sb strings.Builder
	sb.WriteString("# Generated by IoTMaker (https://iotmaker.io).\n")
	sb.WriteString("# This generated file is provided under the IoTMaker Generated Code Exception\n")
	sb.WriteString("# and is NOT subject to the AGPL. You may license it as you choose.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Host build (any POSIX system with a C99 compiler). Override from the\n")
	sb.WriteString("# environment when needed, e.g.:  make CC=clang TARGET=myapp\n\n")
	sb.WriteString("CC      ?= cc\n")
	sb.WriteString("CFLAGS  ?= -std=c99 -Wall -Wextra -O2\n")
	sb.WriteString("TARGET  ?= app\n\n")
	sb.WriteString("OBJS = " + strings.Join(objs, " \\\n       ") + "\n\n")
	sb.WriteString("all: $(TARGET)\n\n")
	sb.WriteString("$(TARGET): $(OBJS)\n")
	sb.WriteString("\t$(CC) $(CFLAGS) -o $(TARGET) $(OBJS)\n\n")
	for _, s := range srcs {
		deps := s.c
		if s.c == "main.c" && len(mainDeps) > 0 {
			deps += " " + strings.Join(mainDeps, " ")
		}
		if s.extraDep != "" {
			deps += " " + s.extraDep
		}
		sb.WriteString(s.o + ": " + deps + "\n")
		sb.WriteString("\t$(CC) $(CFLAGS) -I. -c " + s.c + " -o " + s.o + "\n\n")
	}
	sb.WriteString("clean:\n")
	sb.WriteString("\trm -f $(TARGET) $(OBJS)\n\n")
	sb.WriteString(".PHONY: all clean\n")
	return sb.String()
}
