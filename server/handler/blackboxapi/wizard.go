// server/handler/blackboxapi/wizard.go — Wizard endpoints used by the
// Projects page (and, in later slices, by the Wizard tab itself).
//
// Why this file exists
// ====================
//
// The Projects page in the SPA portal lets users type Go in a Monaco
// editor, run a Parse, and see live syntax/semantic diagnostics as they
// type. The two endpoints that powered that flow used to live in
// `server/handler/_bblegacy/`. The leading underscore makes the whole
// directory invisible to `go build` — it has been dead code for some
// time. The browser-side JavaScript was never updated, so every Parse
// click and every keystroke that triggered a Live Analysis has been
// silently failing with a 404.
//
// This file resurrects those two endpoints on the live parser
// (`server/codegen/blackbox`) and the live analyzer (`server/blackbox`).
// No behaviour changes for the user — only that the buttons work again.
//
// In later slices this file gains the rewrite, draft, image, help, and
// publish endpoints documented in `docs/CLAUDE_WIZARD_DESIGN.md` §8.
// Slice 0 ships only Parse and Analyze.
//
// Routes registered (Slices 0–1)
// ==============================
//
//	POST /api/v1/blackbox/wizard/parse     — sync AST parse via the codegen parser
//	POST /api/v1/blackbox/wizard/analyze   — go/parser + go/types semantic analysis
//	POST /api/v1/blackbox/wizard/rewrite   — apply typed edits, return new source
//
// Both routes require a valid Bearer token (same as the rest of the
// /blackbox/* tree) and use the canonical { metadata, data } envelope
// shared by every other handler package. The portal expects this shape:
// `projects.js` reads `json.metadata.status` and `json.data`.
//
// Request/response shapes
// =======================
//
// Parse:
//
//	POST /api/v1/blackbox/wizard/parse
//	{ "code": "<full Go source>" }
//
//	200 OK
//	{
//	  "metadata": { "status": 200 },
//	  "data": <BlackBoxDef as produced by codegen/blackbox.Parse>
//	}
//
// Analyze:
//
//	POST /api/v1/blackbox/wizard/analyze
//	{ "code": "<full Go source>" }
//
//	200 OK
//	{
//	  "metadata": { "status": 200 },
//	  "data": <AnalysisResult>     // { diagnostics, durationMs, hasErrors }
//	}
//
// Error envelope (any status):
//
//	{
//	  "metadata": { "status": <int>, "error": "<message>" },
//	  "data": null
//	}
//
// Parser limits
// =============
//
// The parser is invoked with the same limits resolution used everywhere
// else in this package: per-user override → global default → compile-time
// fallback (see `server/store/parser_limits.go`). This keeps the wizard
// honest with admin-configured caps and avoids special-casing.
//
// Future slices
// =============
//
// Slice 3 adds GET/POST/DELETE /wizard/draft/:projectId.
// Slice 6 adds GET   /wizard/icons.
// Slice 7 adds POST  /wizard/draft/:projectId/help (and image variants).
// Slice 8 adds POST  /wizard/publish/:projectId   (GitHub App push).
//
// See docs/CLAUDE_WIZARD_DESIGN.md and docs/tasks/WIZARD_TASKS.md.
package blackboxapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"server/blackbox"
	bbparser "server/codegen/blackbox"
	"server/handler/spaauth"
	"server/store"
)

// ─── Wizard request bodies ────────────────────────────────────────────────────

// wizardParseRequest matches the body the Projects page sends. The field
// is named `code` (not `source`) because that is what the SPA already sends
// — changing the field name would force a coordinated SPA + server release.
// The wizard tab added in later slices will use the same shape.
//
// Language is optional and tells the handler which parser to invoke:
//
//	""   or "go"     → Go AST parser (bbparser.Parse)
//	"c"  or "c99"    → C99 parser    (bbparser.ParseC)
//
// Default (empty) is "go" for back-compat with the pre-multi-language
// SPA. Any other value yields HTTP 400.
type wizardParseRequest struct {
	Code     string `json:"code"`
	Language string `json:"language,omitempty"`
}

// wizardAnalyzeRequest is identical to wizardParseRequest; kept as a
// distinct type so future fields can be added independently to either
// endpoint without surprising one with the other.
type wizardAnalyzeRequest struct {
	Code string `json:"code"`
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// handleWizardParse runs the codegen parser over a single Go source
// file and returns the parsed BlackBoxDef plus the incomplete set.
//
// Two responsibilities live here:
//
//  1. Hard parse errors (syntactically invalid Go) produce HTTP 400
//     with a message in the envelope.
//  2. Soft warnings (missing `connection:` tags, manual-page
//     malformation, prop/port truncation) are silently dropped because
//     the same conditions are surfaced more cleanly via the
//     `incomplete` set computed below. Surfacing them twice would
//     double-warn the user — the wizard renders ⚠ from the set.
//
// Response shape (consumed by projects.js and the wizard tab in slice 3):
//
//	{
//	  "metadata": { "status": 200 },
//	  "data": {
//	    "parsed":     <BlackBoxDef as JSON>,
//	    "incomplete": [<sorted dotted paths>]
//	  }
//	}
//
// `incomplete` is always a JSON array — empty (`[]`) for a fully
// configured device, never null. The client can range over it
// unconditionally.
func (h *handler) handleWizardParse(c echo.Context) error {
	var body wizardParseRequest
	if err := c.Bind(&body); err != nil {
		return wizardErr(c, http.StatusBadRequest, "invalid request body")
	}

	body.Code = strings.TrimSpace(body.Code)
	if body.Code == "" {
		return wizardErr(c, http.StatusBadRequest, "code is required")
	}

	// Per-user limits — same resolution path as every other call into
	// the parser in this codebase. An anonymous claims object (empty
	// UserID) safely falls back to global defaults, but in practice the
	// route is gated behind RequireBearerToken so UserID is always set.
	limits := store.GetParserLimits(spaauth.BearerClaims(c).UserID)

	// Route to the language-specific parser. Both parsers share the
	// BlackBoxDef target type — the response shape is identical
	// regardless of which one ran. Adding a new language amounts to
	// writing a new ParseX and an extra case here.
	//
	// Português: Roteia pra parser específico da linguagem. Ambos
	// produzem BlackBoxDef — a resposta tem mesma forma. Adicionar
	// uma linguagem nova é escrever ParseX + um case aqui.
	def, err := bbparser.ParseForLanguage(body.Language, []byte(body.Code), limits)
	// Parse can return (def != nil, err != nil) simultaneously: a
	// non-nil def with soft warnings (missing connection: tags, prop
	// truncation, malformed manual blocks). The wizard tab consumes
	// those concerns through the incomplete set, not through the
	// warning string, so we treat any non-nil def as success and
	// discard err. A truly broken source — or an unsupported language —
	// returns def == nil with err set; that's the only thing that should
	// surface as an HTTP error here.
	if def == nil {
		return wizardErr(c, http.StatusBadRequest, err.Error())
	}

	// Marshal `def` once and ship the bytes via json.RawMessage so
	// Echo's encoder doesn't re-marshal independently. This is no
	// longer about HMAC — see the comment on `parsedHmac` removal in
	// CLAUDE_WIZARD_DESIGN.md / Slice 3 closing notes — but is still
	// the right shape for the response so byte-stability is
	// predictable for any downstream consumer.
	parsedJSON, mErr := json.Marshal(def)
	if mErr != nil {
		return wizardErr(c, http.StatusInternalServerError, "marshal parsed: "+mErr.Error())
	}

	return wizardOK(c, wizardParseData{
		Parsed:     parsedJSON,
		Incomplete: bbparser.ComputeIncomplete(def),
	})
}

// wizardParseData is the success-case `data` payload of /wizard/parse.
// Defined as a named struct (rather than an inline map) so the JSON
// shape is reviewable in one place.
type wizardParseData struct {
	// Parsed is the canonical BlackBoxDef as raw JSON. RawMessage (not
	// *BlackBoxDef) so the bytes stored in the response are exactly
	// the bytes Marshal produced — protects the client from any
	// future encoder change reordering fields. The wizard tab
	// consumes the live shape via JSON.parse on receipt.
	Parsed json.RawMessage `json:"parsed"`

	// Incomplete is the sorted list of dotted paths that need user
	// attention before the device can be published. Driven by
	// codegen/blackbox.ComputeIncomplete, which is the single source of
	// truth for the wizard's ⚠ rendering.
	Incomplete []string `json:"incomplete"`
}

// handleWizardAnalyze runs the two-pass semantic analyzer (go/parser then
// go/types with a LenientImporter) and returns the diagnostics. Always
// returns 200 unless the body itself is invalid — diagnostics are data,
// not errors.
//
// The analyzer is intentionally tolerant of unknown imports (TinyGo's
// `machine`, vendor drivers) so the maker can write code that targets
// hardware the server does not have. See server/blackbox/analyzer.go for
// how that works.
func (h *handler) handleWizardAnalyze(c echo.Context) error {
	var body wizardAnalyzeRequest
	if err := c.Bind(&body); err != nil {
		return wizardErr(c, http.StatusBadRequest, "invalid request body")
	}

	if strings.TrimSpace(body.Code) == "" {
		// An empty body returns an empty diagnostics array rather than a
		// 400. The SPA debounces analyse-on-keystroke and may fire while
		// the user is mid-edit; treating that case as an error spams the
		// console with red squiggles for no reason.
		return wizardOK(c, blackbox.AnalysisResult{
			Diagnostics: []blackbox.Diagnostic{},
		})
	}

	result := blackbox.Analyze([]byte(body.Code))
	return wizardOK(c, result)
}

// ─── /wizard/rewrite ──────────────────────────────────────────────────────────

// wizardRewriteRequest carries the source code plus a list of typed
// edits. Source and Edits are both required — an empty Edits list is
// valid and acts as a "format only" pass via codegen/blackbox.Rewrite.
type wizardRewriteRequest struct {
	// Code is the full source the user is editing. The same field
	// name as parse and analyze for client-side consistency.
	Code string `json:"code"`

	// Edits is the ordered list of mutations to apply. Each edit is a
	// JSON object with `op`, `path`, and an op-specific `args`. See
	// codegen/blackbox.WizardEdit for the exact shape.
	Edits []bbparser.WizardEdit `json:"edits"`

	// Language selects the rewrite engine. Empty / "go" / "golang"
	// → Go engine (bbparser.Rewrite). "c" / "c99" → C99 engine
	// (bbparser.RewriteC). Any other value returns HTTP 400.
	//
	// Default "go" keeps pre-Slice-3 callers working unchanged.
	Language string `json:"language,omitempty"`
}

// wizardRewriteResponse is the success-case envelope payload. The
// caller receives the rewritten source plus a freshly computed parsed
// BlackBoxDef and the incomplete set, so the wizard tab does not need
// to follow up with a /parse call after every save.
//
// We do NOT echo back the original source — the client already has it,
// and including it would double the payload of a debounced save.
type wizardRewriteResponse struct {
	// Code is the rewritten, gofmt-clean Go source.
	Code string `json:"code"`

	// Parsed is the BlackBoxDef as raw JSON, recomputed from the
	// rewritten source. RawMessage (not *BlackBoxDef) protects against
	// any future encoder change reordering fields between marshal
	// passes — keeps the wire shape stable for clients.
	Parsed json.RawMessage `json:"parsed"`

	// Incomplete is the sorted list of dotted paths recomputed from
	// the rewritten source. Same semantics as in /wizard/parse.
	Incomplete []string `json:"incomplete"`

	// Applied is the count of edits successfully applied. Always equal
	// to len(request.Edits) on success today; the field stays numeric
	// so future slices can introduce partial-apply semantics without
	// changing the wire shape.
	Applied int `json:"applied"`
}

// handleWizardRewrite is the slice-1 endpoint that turns typed edits
// into rewritten Go. Slice 2 extended it to also re-parse the result
// and return a fresh `incomplete` set, removing the need for a
// follow-up /parse call after each save.
//
// The flow is intentionally thin: bind, validate, delegate to
// codegen/blackbox.Rewrite, re-parse, compute incomplete, return.
//
// On any failure (parse error, malformed edit, post-format error) the
// engine returns the original source unchanged. We surface the error
// message in the envelope so the wizard can show it next to the
// offending modal. The HTTP status is 422 (semantic problem) rather
// than 500 (server fault) — the user input is the cause, not the
// server.
//
// Forward compatibility: handler is intentionally permissive about
// unknown args fields inside each edit. If a future SPA sends a new
// optional arg the engine doesn't recognise, it is silently ignored.
// This lets the SPA roll out new fields without coordinating a server
// release.
func (h *handler) handleWizardRewrite(c echo.Context) error {
	var body wizardRewriteRequest
	if err := c.Bind(&body); err != nil {
		return wizardErr(c, http.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(body.Code) == "" {
		return wizardErr(c, http.StatusBadRequest, "code is required")
	}

	// Edits == nil is a valid no-op: format the code (Go) or echo
	// it back (C99) and return it. This lets the wizard reuse this
	// endpoint as a "save and format" path without inventing a
	// separate route.
	//
	// Routing by language mirrors handleWizardParse: empty or "go"
	// → Go engine; "c"/"c99" → C99 engine. Unknown values reject
	// with 400 to avoid ambiguity.
	var rewritten string
	var rewriteErr error
	switch strings.ToLower(strings.TrimSpace(body.Language)) {
	case "", "go", "golang":
		rewritten, rewriteErr = bbparser.Rewrite(body.Code, body.Edits)
	case "c", "c99":
		rewritten, rewriteErr = bbparser.RewriteC(body.Code, body.Edits)
	default:
		return wizardErr(c, http.StatusBadRequest,
			"unsupported language: "+body.Language)
	}
	if rewriteErr != nil {
		// Engine errors are user-visible (bad path, bad args, parse
		// error). 422 keeps these distinct from network or 5xx faults.
		return wizardErr(c, http.StatusUnprocessableEntity, rewriteErr.Error())
	}

	// Re-parse the rewritten source to refresh the BlackBoxDef and the
	// incomplete set. Soft warnings are dropped (same rationale as in
	// handleWizardParse) — the incomplete set carries the same
	// information in the canonical form. A nil def would mean Rewrite
	// produced unparseable output, which is a server bug; treat it as
	// 500 with a generic message rather than leaking implementation
	// detail to the user.
	limits := store.GetParserLimits(spaauth.BearerClaims(c).UserID)
	def, parseErr := bbparser.ParseForLanguage(body.Language, []byte(rewritten), limits)
	if def == nil {
		return wizardErr(c, http.StatusInternalServerError,
			"rewritten source failed to parse: "+parseErr.Error())
	}

	// Marshal once and ship via RawMessage. See the parse handler
	// for the byte-stability rationale.
	parsedJSON, mErr := json.Marshal(def)
	if mErr != nil {
		return wizardErr(c, http.StatusInternalServerError, "marshal parsed: "+mErr.Error())
	}

	return wizardOK(c, wizardRewriteResponse{
		Code:       rewritten,
		Parsed:     parsedJSON,
		Incomplete: bbparser.ComputeIncomplete(def),
		Applied:    len(body.Edits),
	})
}

// ─── /wizard/draft/:projectId ─────────────────────────────────────────────────

// wizardDraftSaveRequest is the body for POST /wizard/draft/:projectId.
//
// `code` (the source) follows the slice-0 naming convention used by
// /parse and /rewrite — the SPA already speaks this name, so renaming
// to the design doc's `source` would force a coordinated release for
// no functional benefit. Documented here and in the closing note of
// slice 3 in WIZARD_TASKS.md.
type wizardDraftSaveRequest struct {
	// Code is the full Go source of the draft. Required.
	Code string `json:"code"`

	// Parsed is the BlackBoxDef JSON the server emitted on the most
	// recent /parse or /rewrite. Stored verbatim so subsequent reads
	// of this draft see the same bytes the server originally produced.
	//
	// Note on previous design: an earlier slice carried a `parsedHmac`
	// alongside this field as a "sanity check" between the server's
	// emitted JSON and what the client echoed back on save. That
	// check broke because JSON.parse on the client cannot guarantee
	// byte-identical re-serialization on the way back. The check was
	// also misframed — its real purpose was protecting JSON published
	// to GitHub at deploy time (slice 8), not the in-flight draft
	// round-trip. The HMAC helpers in store/wizard_drafts.go have
	// been removed; slice 8 will pick its own signing scheme.
	Parsed json.RawMessage `json:"parsed"`
}

// wizardDraftGetResponse is the success-case `data` payload of
// GET /wizard/draft/:projectId. The fields come straight from the
// row — the server never recomputes on read, so the client sees
// exactly the same view it last saved.
//
// images and helps are emitted as RawMessage even when empty so the
// JS can iterate them without null-guards. They will carry real
// content in slices 7+; for slice 3 they are always `[]`.
type wizardDraftGetResponse struct {
	Code       string          `json:"code"`
	Parsed     json.RawMessage `json:"parsed"`
	Incomplete []string        `json:"incomplete"`
	Images     json.RawMessage `json:"images"`
	Helps      json.RawMessage `json:"helps"`
	UpdatedAt  int64           `json:"updatedAt"`
}

// handleWizardDraftGet returns the user's draft for the given project.
// 404 when none exists — this is normal first-time-open behaviour, not
// an error. The wizard tab handles 404 by initialising an empty draft
// from whatever source is currently in the editor.
func (h *handler) handleWizardDraftGet(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return wizardErr(c, http.StatusBadRequest, "projectId is required")
	}

	draft, err := store.GetWizardDraft(claims.UserID, projectID)
	if errors.Is(err, store.ErrWizardDraftNotFound) {
		return wizardErr(c, http.StatusNotFound, "no draft for this project")
	}
	if err != nil {
		return wizardErr(c, http.StatusInternalServerError, err.Error())
	}

	// CompletionJSON is `[]string` written by us; a parse failure here
	// would mean the column was tampered with on disk. Return an empty
	// slice rather than 500 — the wizard re-derives ⚠ on its next
	// save anyway.
	var incomplete []string
	if draft.CompletionJSON != "" {
		if err := json.Unmarshal([]byte(draft.CompletionJSON), &incomplete); err != nil {
			incomplete = []string{}
		}
	}
	if incomplete == nil {
		incomplete = []string{}
	}

	return wizardOK(c, wizardDraftGetResponse{
		Code:       draft.Source,
		Parsed:     json.RawMessage(draft.ParsedJSON),
		Incomplete: incomplete,
		Images:     json.RawMessage(draft.ImagesJSON),
		Helps:      json.RawMessage(draft.HelpsJSON),
		UpdatedAt:  draft.UpdatedAt,
	})
}

// handleWizardDraftSave upserts the user's draft for the given project.
// The server recomputes `incomplete` from the posted Parsed and
// persists it — that recomputation is the actual integrity barrier
// here: even if a malicious client posted a tampered `parsed`, the
// `incomplete` set the server stores reflects what the server
// actually saw, not what the client claimed.
//
// History note: previously this handler verified an HMAC echoed by
// the client. The check was unreliable (JSON.parse → JSON.stringify
// is not byte-stable) and misframed (the real publish-side integrity
// concern lives in slice 8). HMAC fields are removed from the request
// shape; the database column `parsed_hmac` is kept (NOT NULL DEFAULT
// ”) for migration simplicity and is now always written empty.
func (h *handler) handleWizardDraftSave(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return wizardErr(c, http.StatusBadRequest, "projectId is required")
	}

	var body wizardDraftSaveRequest
	if err := c.Bind(&body); err != nil {
		return wizardErr(c, http.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(body.Code) == "" {
		return wizardErr(c, http.StatusBadRequest, "code is required")
	}
	if len(body.Parsed) == 0 {
		return wizardErr(c, http.StatusBadRequest, "parsed is required")
	}

	// Recompute incomplete server-side. The unmarshal also acts as a
	// shape check — a parsed payload that doesn't decode into
	// BlackBoxDef shape gets rejected with a clear message.
	var def bbparser.BlackBoxDef
	if err := json.Unmarshal(body.Parsed, &def); err != nil {
		return wizardErr(c, http.StatusUnprocessableEntity, "parsed is not a BlackBoxDef: "+err.Error())
	}
	incomplete := bbparser.ComputeIncomplete(&def)
	completionJSON, mErr := json.Marshal(incomplete)
	if mErr != nil {
		// Unreachable in practice (we just constructed the slice from
		// stdlib types) but reported anyway.
		return wizardErr(c, http.StatusInternalServerError, "marshal incomplete: "+mErr.Error())
	}

	if err := store.UpsertWizardDraft(&store.WizardDraft{
		UserID:         claims.UserID,
		ProjectID:      projectID,
		Source:         body.Code,
		ParsedJSON:     string(body.Parsed),
		ParsedHMAC:     "", // Reserved for slice 8 (publish-time signing); empty during draft.
		CompletionJSON: string(completionJSON),
		// Images and helps are managed by their own endpoints (slice 7+);
		// the upsert preserves whatever the row already has.
	}); err != nil {
		return wizardErr(c, http.StatusInternalServerError, err.Error())
	}

	return wizardOK(c, map[string]any{"ok": true})
}

// handleWizardDraftDelete drops the user's draft for the given
// project. Idempotent — deleting a non-existent draft is a no-op and
// still returns 200, matching the design doc's "Cancel wizard" UX
// (the user clicks Cancel; the server doesn't care whether anything
// was there to cancel).
func (h *handler) handleWizardDraftDelete(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return wizardErr(c, http.StatusBadRequest, "projectId is required")
	}

	if err := store.DeleteWizardDraft(claims.UserID, projectID); err != nil {
		return wizardErr(c, http.StatusInternalServerError, err.Error())
	}
	return wizardOK(c, map[string]any{"ok": true})
}

// ─── Envelope helpers ─────────────────────────────────────────────────────────
//
// The portal uses { metadata, data } as its canonical JSON envelope. Each
// handler package owns its own helpers (see server/handler/i18n/handlers.go
// for the pattern used here). We do NOT use the existing fail() helper in
// submit.go because that one uses a flat { error } shape — it predates the
// envelope convention and is only kept as-is to avoid breaking the older
// submit clients. New code uses the canonical envelope.

func wizardOK(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK},
		"data":     data,
	})
}

func wizardErr(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
