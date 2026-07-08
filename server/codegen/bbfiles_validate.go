// server/codegen/bbfiles_validate.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"fmt"
	"strings"

	"server/codegen/blackbox"
	"server/codegen/diagnostics"
)

// Multi-file black-box validation.
//
// English:
//
//	These rules gate the EXPORT of authored device files — the checks only
//	the codegen side can make, because they need the resolved naming (the
//	generated header's name) and the emitter's contract (the reserved
//	main.c, the folder layout). The HTTP save boundary enforces the
//	spelling rules (charset, extensions per language, uniqueness); this
//	layer enforces the SHIPPING rules:
//
//	  R1  every authored path must be a plain relative path (no "..", no
//	      absolute root, no backslash) — a def can reach the emitter from
//	      a parsed blob that never went through the save boundary, and a
//	      hostile path here becomes a zip-slip in the maker's unzip;
//	  R2  a def with several files MUST have an identity (a database id):
//	      without one there is no folder and no rename prefix, and the
//	      only emission mode left — flat inlining into main.c — cannot
//	      resolve local includes;
//	  R3  an identified C box must ship at least one .c: the generated
//	      header promises definitions the linker must find;
//	  R4  no authored function may be named `main` — the maker's project
//	      already owns main.c, and a second main is a guaranteed link
//	      failure the specialist would only discover downstream;
//	  R5  no authored path may collide with the generated header's name
//	      for this export (naming-dependent, hence checked here).
//
// Português:
//
//	Regras que só o codegen pode checar (precisam do naming resolvido e do
//	contrato do emitter). A borda HTTP impõe a grafia; aqui impõe-se o
//	EMBARQUE: caminho relativo simples (senão vira zip-slip no unzip do
//	maker); vários arquivos exigem identidade (sem id não há pasta nem
//	prefixo, e o inline achatado não resolve include local); caixa C
//	identificada precisa de ≥1 .c (o header gerado promete definições);
//	`main` autoral é proibido (o main.c do maker já existe); e nenhum
//	caminho pode colidir com o nome do header gerado desta exportação.
func validateBlackBoxFiles(bbDefs map[string]*blackbox.BlackBoxDef, naming blackbox.Naming) []Diagnostic {
	var diags []Diagnostic
	seen := make(map[*blackbox.BlackBoxDef]bool)

	errDiag := func(id, msg string) Diagnostic {
		d := Diagnostic{
			Kind:     diagnostics.KindBlackBoxFilesInvalid,
			Severity: diagnostics.SeverityError,
			Message:  msg,
		}
		if id != "" {
			d.Devices = []string{id}
		}
		return d
	}

	for _, def := range bbDefs {
		// The defs map is keyed by function name — several keys share one
		// def (one source, many devices). Validate each def once.
		if def == nil || seen[def] || len(def.Files) == 0 {
			continue
		}
		seen[def] = true

		hasC := false
		for _, f := range def.Files {
			if !safeAuthoredPath(f.Path) {
				diags = append(diags, errDiag(def.ID, fmt.Sprintf(
					"black-box file path %q is not a plain relative path (no absolute paths, no \"..\", no backslash)",
					f.Path)))
			}
			if strings.HasSuffix(f.Path, ".c") {
				hasC = true
			}
		}

		if def.ID == "" && len(def.Files) > 1 {
			diags = append(diags, errDiag("", fmt.Sprintf(
				"a black-box without a database identity cannot ship %d files: no identity means no folder and no rename prefix; publish the device (or keep a single file)",
				len(def.Files))))
			continue // the remaining rules assume the folder layout
		}

		if def.ID != "" && def.Name == "" {
			// Identified C99 function-device box → folder layout rules.
			if !hasC {
				diags = append(diags, errDiag(def.ID,
					"black-box ships no .c file: the generated header promises definitions the linker must find"))
			}
			headerName := naming.HeaderName(def.CodeIdent())
			for _, f := range def.Files {
				if f.Path == headerName {
					diags = append(diags, errDiag(def.ID, fmt.Sprintf(
						"authored file %q collides with the generated header of this export; rename the file (or change the export prefix)",
						f.Path)))
				}
			}
		}

		for _, fn := range def.Functions {
			if fn.Name == "main" {
				diags = append(diags, errDiag(def.ID,
					"authored device code must not define main(): the generated project already owns main.c and a second definition is a guaranteed link failure"))
				break
			}
		}
	}
	return diags
}

// safeAuthoredPath mirrors the emitter's zip-slip guard (see
// emit_bbfiles.go safeRelPath) at the validation layer, where a violation
// can be reported instead of silently skipped. Two small copies beat an
// export of the emitter's internal helper for one predicate.
//
// Português: Espelha o guard do emitter na camada de validação, onde a
// violação pode ser REPORTADA em vez de pulada. Duas cópias pequenas valem
// mais que exportar um helper interno por um predicado.
func safeAuthoredPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
