// server/projectexport/validator.go — pre-flight checks for ZIP export.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The user picked the strictest possible bar in the spec discussion:
// "no errors, no warnings, no incomplete cards, no missing helps, no
// missing examples". This file enforces that in five passes:
//
//  1. Parse the source code via codegen/blackbox.Parse — fails on
//     any non-recoverable Go syntax error or IDS-grammar violation.
//  2. Run blackbox.Analyze (the same two-pass go/parser + go/types
//     pipeline the live editor uses) — fails on any diagnostic with
//     severity "error" or "warning".
//  3. Compute the incomplete set via codegen/blackbox.ComputeIncomplete
//     — fails when any card in the wizard would still show ⚠.
//  4. Walk the project's help-file table and require:
//     - at least one readme.<lang>.md
//     - for each method declared in the parsed BlackBoxDef, at
//     least one <MethodName>.<lang>.md
//  5. Require at least one file under examples/ — the spec calls
//     out steganographic example PNGs as a hard requirement.
//
// Each pass appends to the same Issues slice. A single export
// blocks if any issue exists; the SPA renders the grouped list and
// the user fixes everything in one cycle.
//
// The validator is a pure data layer call — no HTTP, no logging.
// The handler.go in handler/projectexport/ wraps it for the wire
// and is the only place that knows about Echo or claims.
//
// Português: validador pre-flight para a exportação em ZIP. Faz
// cinco passes (parse, analyze, incompletos, helps, examples) e
// retorna a lista combinada de problemas. Sem chamadas HTTP — é
// uma função pura sobre o estado armazenado.
package projectexport

import (
	"path"
	"strings"

	"server/blackbox"
	bbparser "server/codegen/blackbox"
	"server/store"
)

// IssueCategory groups problems for the SPA's modal renderer. The
// SPA expects exactly these strings and renders one collapsible
// section per category in the order they appear here. Adding a new
// category requires a matching entry in the SPA's i18n bundle.
type IssueCategory string

const (
	CategoryParseErrors      IssueCategory = "parse_errors"
	CategoryAnalyzeErrors    IssueCategory = "analyze_errors"
	CategoryAnalyzeWarnings  IssueCategory = "analyze_warnings"
	CategoryWizardIncomplete IssueCategory = "wizard_incomplete"
	CategoryHelpMissing      IssueCategory = "help_missing"
	CategoryExamplesMissing  IssueCategory = "examples_missing"
)

// Issue is a single problem the user must resolve before export.
// All fields are JSON-serialised by the handler — keep wire-stable.
//
// Detail is human-readable and ALREADY ENGLISH-ONLY for now. The
// SPA will localise the category labels (parse_errors → "Parse
// errors" / "Erros do parser") but echoes the Detail as-is. This
// trade-off keeps the validator simple — pre-flight messages are
// usually compiler diagnostics or filenames, both of which read
// the same in any language.
type Issue struct {
	// Category is one of the IssueCategory constants above. The SPA
	// groups by this field.
	Category IssueCategory `json:"category"`

	// Detail is the human-readable problem description. Parse and
	// analyze issues include the line/col when available; help and
	// example issues say which file is missing.
	Detail string `json:"detail"`

	// Line / Col are populated only for parse and analyze issues
	// where the source has a reported position. Zero when not
	// applicable.
	Line int `json:"line,omitempty"`
	Col  int `json:"col,omitempty"`
}

// Result is the validator's full report. Issues is the flat ordered
// list; ParsedDef is the BlackBoxDef the validator computed if parse
// succeeded (nil otherwise). The builder uses ParsedDef to avoid a
// second parse — the validator and the ZIP build cooperate inside
// the same handler.
type Result struct {
	Issues    []Issue               `json:"issues"`
	ParsedDef *bbparser.BlackBoxDef `json:"-"`
}

// OK reports whether the project is exportable. True iff Issues is
// empty. Convenience for the handler — saves a `len(...) == 0` at
// every call site.
func (r *Result) OK() bool {
	return len(r.Issues) == 0
}

// Validate runs all pre-flight passes for the given project. The
// userID is needed to resolve per-user parser limits (see
// store.GetParserLimits). Returns a Result whose Issues slice is
// the full grouped list — never returns an error of its own; data
// access errors are themselves reported as issues so the SPA
// shows a usable message rather than failing silently with a 500.
//
// The function deliberately does NOT short-circuit on the first
// failure. Running every pass even when parse fails would be a
// waste, so we DO return early after parse — analyze, incomplete,
// helps and examples all need a parsed definition. But within
// analyze and within the help check, every diagnostic and every
// missing file is collected: the user fixes the whole list in one
// pass instead of one push at a time.
func Validate(projectID, userID string) *Result {
	res := &Result{}

	// Determine the project's programming language up front so the
	// right parser runs and the Go-only passes (semantic analyze,
	// per-method helps) are routed correctly. The language is stored
	// on the project (programming_language_id: "golang" or "c") and is
	// the same signal the wizard handler uses. Default to Go on any
	// lookup problem — back-compat with the pre-multi-language model,
	// mirroring the wizard handler's empty-language default.
	isC99 := false
	// langID is hoisted here because the unified parse entry point
	// (ParseForLanguageFiles, below) wants the raw language token — proj
	// itself deliberately dies with this if-scope. Empty on lookup
	// failure = the dispatch's Go default, same back-compat stance as
	// isC99 above.
	//
	// Português: langID é içado aqui porque a entrada unificada de parse
	// quer o token cru — proj morre com este if-scope de propósito. Vazio
	// em falha de lookup = o default Go do dispatch.
	langID := ""
	if proj, perr := store.GetProjectByIDAndUser(projectID, userID); perr == nil && proj != nil {
		isC99 = strings.EqualFold(proj.ProgrammingLanguageID, "c")
		langID = proj.ProgrammingLanguageID
	}

	// ── Pass 1: load source and parse ───────────────────────────
	latest, err := store.GetLatestProjectCodeVersion(projectID)
	if err != nil {
		// No saved code version yet. The export button is gated in
		// the SPA on having an open project, so this should never
		// happen via the live UI; if it does, surface a clear
		// reason rather than a generic parse failure.
		res.Issues = append(res.Issues, Issue{
			Category: CategoryParseErrors,
			Detail:   "Project has no saved source code yet. Save your code first.",
		})
		return res
	}
	// A snapshot with no non-empty file is "no code" for export purposes:
	// the specialist saved, but saved nothing shippable.
	hasContent := false
	for _, f := range latest.Files {
		if len(strings.TrimSpace(f.Content)) > 0 {
			hasContent = true
			break
		}
	}
	if !hasContent {
		res.Issues = append(res.Issues, Issue{
			Category: CategoryParseErrors,
			Detail:   "Source code is empty.",
		})
		return res
	}

	limits := store.GetParserLimits(userID)

	// One entry point for BOTH languages since GoMF: the dispatch routes
	// Go single-file through Parse, Go multi-file through ParseGoFiles
	// and C through ParseCFiles — the validator stopped owning that
	// knowledge (the goSrc hoist of the single-file era died with it).
	// A hard error carries the offending path so the wizard can point at
	// the right tab.
	//
	// Português: Uma entrada só para as duas linguagens desde o GoMF: o
	// dispatch roteia Go single pelo Parse, Go multi pelo ParseGoFiles e
	// C pelo ParseCFiles — o validador deixou de ser dono desse
	// conhecimento (o içamento do goSrc morreu junto).
	def, parseErr := bbparser.ParseForLanguageFiles(
		langID, store.ToParserFiles(latest.Files), limits)
	if def == nil {
		// Hard parse failure. Surface the error verbatim — the
		// codegen parser already formats line/col in its message
		// when applicable.
		msg := "Parse failed."
		if parseErr != nil {
			msg = parseErr.Error()
		}
		res.Issues = append(res.Issues, Issue{
			Category: CategoryParseErrors,
			Detail:   msg,
		})
		// No ParsedDef → cannot do incomplete/help passes. Bail.
		return res
	}
	res.ParsedDef = def

	// ── Pass 2: semantic analyze (errors AND warnings block) ────
	// blackbox.Analyze is a go/parser + go/types pass — Go only. C99
	// has no semantic analyzer, and running the Go one on C99 source
	// would itself fail (e.g. on `#include`). Skip it for C99; the
	// parse, completion, and help passes still gate the export.
	// blackbox.Analyze is the same call the live editor's projAnalyze
	// fires — keeps wire-level consistency. We split the diagnostics
	// into two issue categories so the SPA's modal shows them
	// separately (the user can decide which to tackle first).
	if !isC99 {
		// AnalyzeFiles type-checks the whole PACKAGE (GoMF): a
		// methods-only sibling is legitimate Go, and analysing files one
		// at a time would flag "undefined: <Struct>" all over it. Each
		// diagnostic's Detail is prefixed with its file so the export
		// modal reads unambiguously in a multi-file project (Line/Col
		// alone would be ambiguous across tabs).
		//
		// Português: O AnalyzeFiles checa o PACOTE inteiro: irmão
		// só-métodos é Go legítimo. Cada Detail leva o arquivo na
		// frente — Line/Col sozinhos seriam ambíguos entre abas.
		srcFiles := make([]blackbox.SourceFile, 0, len(latest.Files))
		for _, f := range latest.Files {
			srcFiles = append(srcFiles, blackbox.SourceFile{Path: f.Path, Content: f.Content})
		}
		an := blackbox.AnalyzeFiles(srcFiles)
		for _, fd := range an.Files {
			for _, d := range fd.Diagnostics {
				detail := fd.Path + ": " + d.Message
				switch d.Severity {
				case "error":
					res.Issues = append(res.Issues, Issue{
						Category: CategoryAnalyzeErrors,
						Detail:   detail,
						Line:     d.Line,
						Col:      d.Col,
					})
				case "warning":
					res.Issues = append(res.Issues, Issue{
						Category: CategoryAnalyzeWarnings,
						Detail:   detail,
						Line:     d.Line,
						Col:      d.Col,
					})
					// Other severities ("info", "hint") are deliberately
					// ignored — they're advisory, not blocking.
				}
			}
		}
	}

	// ── Pass 3: incomplete cards ────────────────────────────────
	// The dotted paths returned by ComputeIncomplete are exactly
	// what the wizard renders ⚠ for. Surface them as-is — the SPA
	// can map them to friendlier labels later if needed, but the
	// raw path ("APDS9960.Init.label") is itself searchable in the
	// editor so it's already useful.
	for _, p := range bbparser.ComputeIncomplete(def) {
		res.Issues = append(res.Issues, Issue{
			Category: CategoryWizardIncomplete,
			Detail:   p,
		})
	}

	// ── Pass 4: required help files ─────────────────────────────
	// We need access to filenames only — the content blob is a
	// detail of the export builder, not the validator. ListHelpFiles
	// returns metadata-only rows, which is exactly what we want.
	helps, hErr := store.ListHelpFiles(projectID)
	if hErr != nil {
		// Database read error — surface as a help-missing issue so
		// the user gets a concrete next step (retry / contact) rather
		// than a silent "everything looks fine" follow-up failure
		// when the build phase reads the same table.
		res.Issues = append(res.Issues, Issue{
			Category: CategoryHelpMissing,
			Detail:   "Could not list help files: " + hErr.Error(),
		})
	} else {
		// Index basenames present, by family ("readme" or method
		// name). For the "at least one of" check we only care
		// whether the family has anything, not what languages are
		// covered — multi-language coverage is a future concern.
		hasReadme := false
		methodCovered := map[string]bool{}
		for _, hf := range helps {
			base := path.Base(hf.Path)
			// readme.md / readme.<lang>.md / readme.<n>.<lang>.md
			if m := readmeBasename(base); m {
				hasReadme = true
				continue
			}
			// <Method>.[<n>.]<lang>.md
			if name, ok := methodBasename(base); ok {
				methodCovered[name] = true
			}
		}
		if !hasReadme {
			res.Issues = append(res.Issues, Issue{
				Category: CategoryHelpMissing,
				Detail:   "No readme.<lang>.md found. Add a project-level readme in the Files manager.",
			})
		}
		// Walk every executable block in the parsed def and demand at
		// least one help file. The matching (methodBasename) already
		// accepts the optional sequence number — Init.<lang>.md,
		// Init.<n>.<lang>.md, Init.md all count as covering "Init" —
		// so this is identical to the Go behaviour, just over a
		// different set of names.
		//
		//   - Go:  the initialiser (Init) plus each named method.
		//   - C99: each standalone function device (no Init, no
		//          methods — function.Name is the block name, e.g.
		//          sht3x_read → sht3x_read.<lang>.md).
		if isC99 {
			for _, fn := range def.Functions {
				if fn.Name != "" && !methodCovered[fn.Name] {
					res.Issues = append(res.Issues, Issue{
						Category: CategoryHelpMissing,
						Detail:   "No help file for device '" + fn.Name + "'. Expected " + fn.Name + ".<lang>.md",
					})
				}
			}
		} else {
			if def.Init != nil && !methodCovered["Init"] {
				res.Issues = append(res.Issues, Issue{
					Category: CategoryHelpMissing,
					Detail:   "No help file for method 'Init'. Expected Init.<lang>.md",
				})
			}
			for _, m := range def.Methods {
				if !methodCovered[m.Name] {
					res.Issues = append(res.Issues, Issue{
						Category: CategoryHelpMissing,
						Detail:   "No help file for method '" + m.Name + "'. Expected " + m.Name + ".<lang>.md",
					})
				}
			}
		}
	}

	// ── Pass 5: at least one example ────────────────────────────
	// Examples are stored under the "examples/" prefix in the same
	// help-files table. The store layer treats them uniformly with
	// markdown — only the path prefix distinguishes them. A simple
	// presence check is enough here; content validity (steg header,
	// PNG decode) is the upload pipeline's responsibility.
	if hErr == nil {
		hasExample := false
		for _, hf := range helps {
			if strings.HasPrefix(hf.Path, "examples/") {
				hasExample = true
				break
			}
		}
		if !hasExample {
			res.Issues = append(res.Issues, Issue{
				Category: CategoryExamplesMissing,
				Detail:   "No example found under examples/. Upload at least one example PNG (with the IOTM header) before exporting.",
			})
		}
	}

	return res
}

// readmeBasename reports whether `base` matches the IDS readme
// filename grammar. Mirrors the regex in
// codegen/blackbox/devicehelp.go (readmeFileRe) but keeps the check
// local to avoid widening the package's public surface for one
// boolean.
//
// Accepted shapes:
//   - readme.md
//   - readme.<lang>.md
//   - readme.<n>.<lang>.md
func readmeBasename(base string) bool {
	lower := strings.ToLower(base)
	if !strings.HasSuffix(lower, ".md") {
		return false
	}
	// Strip ".md" and split on '.'. A valid readme has 1, 2, or 3
	// segments where segment 0 must be "readme". We don't bother
	// validating <lang> or <n> here — a malformed sub-segment is
	// not a security concern, just means the file won't be served
	// by the runtime. Pre-flight is about completeness, not
	// well-formedness.
	stem := strings.TrimSuffix(lower, ".md")
	parts := strings.Split(stem, ".")
	return len(parts) >= 1 && parts[0] == "readme"
}

// methodBasename reports whether `base` is a method help file and,
// if so, returns the method name (preserving its source case). A
// method help file matches:
//
//	<Method>.md
//	<Method>.<lang>.md
//	<Method>.<n>.<lang>.md
//
// where <Method> is a Go identifier (starts with a letter, then
// alnum / underscore). The check is lower-cased for the suffix and
// language tag, but the method name itself stays in the original
// case for the coverage comparison against def.Methods.
func methodBasename(base string) (string, bool) {
	if !strings.HasSuffix(strings.ToLower(base), ".md") {
		return "", false
	}
	if readmeBasename(base) {
		// Readme is its own thing — never count as a method help.
		return "", false
	}
	stem := strings.TrimSuffix(base, base[len(base)-3:]) // strip ".md" preserving case in stem
	parts := strings.Split(stem, ".")
	if len(parts) == 0 {
		return "", false
	}
	first := parts[0]
	if !isGoIdentifier(first) {
		return "", false
	}
	return first, true
}

// isGoIdentifier is a minimal Go identifier check: first char a
// letter or underscore, remaining chars alnum or underscore.
// Sufficient for filename validation; the parser validates real
// identifier rules at a higher level.
func isGoIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_':
			// always allowed
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z':
			// always allowed
		case (r >= '0' && r <= '9') && i > 0:
			// digits allowed only after position 0
		default:
			return false
		}
	}
	return true
}
