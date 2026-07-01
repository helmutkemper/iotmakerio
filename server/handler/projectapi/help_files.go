// server/handler/projectapi/help_files.go — HTTP handlers for the
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// project help-files feature.
//
// All endpoints live under /api/v1/projects/:id/files/help/* and require
// a Bearer token. Authorisation: the authenticated claims.UserID must own
// the project (verified via store.GetProjectByIDAndUser) — public
// projects are readable by any authenticated user but writable only by
// the owner.
//
// The endpoints use a "*path" Echo parameter to capture the rest of the
// URL after "/help/". This means path "examples/foo.png" arrives as the
// raw string "examples/foo.png" without any URL-decoding gymnastics on
// the handler side.
//
// Endpoint summary (full route paths registered in routes.go):
//
//	GET    .../files/help                — list metadata
//	GET    .../files/help/*path          — fetch one file (raw bytes + headers)
//	PUT    .../files/help/*path          — upsert from raw body
//	POST   .../files/help/insert         — insert .md with position cascade
//	DELETE .../files/help/*path          — remove (idempotent)
//	POST   .../files/help/*path/rename   — rename within the project
//
// The choice of PUT (not POST) for upsert lines up with the "the client
// names the resource" semantic — paths are user-chosen identifiers, not
// server-generated like an auto-increment ID.
package projectapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"server/handler/spaauth"
	"server/store"

	"github.com/labstack/echo/v4"
	"golang.org/x/text/language"
)

// ─── Path validation ──────────────────────────────────────────────────────────

// helpFilePathRe enforces the path grammar locked in the design doc:
//
//   - characters: A-Z, a-z, 0-9, dot, underscore, hyphen
//   - up to one level of subdirectory; deeper paths are rejected
//   - no leading slash; no parent-directory traversal (".." is not in the
//     character class so it is rejected naturally)
//
// Examples that match: "readme.en.md", "examples/howTo.png"
// Examples that DON'T match: "../etc/passwd", "deeper/nested/foo.png",
// "Init.en.md/", "/readme.md", "name with space.md"
//
// Compiled once at init time to avoid per-request regex compilation.
var helpFilePathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)?$`)

// validateHelpFilePath returns nil when the given path is acceptable as
// a help-file location and a descriptive error otherwise. Used by every
// handler that accepts a path from the client, including the rename
// destination.
//
// In addition to the regex, this function rejects:
//
//   - empty strings
//   - paths longer than 200 characters (DoS guard for the database)
//   - explicit ".." segments (defence-in-depth; the regex would already
//     reject them, but a duplicate check makes the audit trail clearer)
//   - paths whose final segment ends with "." (Windows-hostile names)
//
// MIME-type validity is checked separately by mimeForHelpExt below.
func validateHelpFilePath(path string) error {
	if path == "" {
		return errors.New("path is required")
	}
	if len(path) > 200 {
		return errors.New("path is too long (max 200 characters)")
	}
	if !helpFilePathRe.MatchString(path) {
		return errors.New("invalid path: only [A-Za-z0-9._-] and at most one '/' allowed")
	}
	// Defence-in-depth: explicit ".." rejection. The regex would already
	// catch this because "." is allowed but ".." would have to be a
	// segment, and segments cannot start or end with a dot in many
	// reasonable interpretations… but this is a security boundary and
	// "obviously safe" is better than "regex says so". Reject the moment
	// we see the literal substring.
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errors.New("invalid path: empty or relative segment")
		}
		if strings.HasSuffix(seg, ".") {
			return errors.New("invalid path: segment must not end with '.'")
		}
	}
	return nil
}

// ─── MIME whitelist ───────────────────────────────────────────────────────────

// helpFileExtMime is the locked-down whitelist of file types the help-file
// store will accept. Extending this list requires a deliberate change here
// AND a discussion: the wizard's preview / sanitisation stories assume
// these types specifically.
//
// PDFs are deliberately excluded. The user can link to external PDFs from
// inside markdown — that's enough for v1.
var helpFileExtMime = map[string]string{
	".md":   "text/markdown; charset=utf-8",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".svg":  "image/svg+xml",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// mimeForHelpExt returns the canonical MIME type for the given path.
// Returns ("", false) when the extension is not in the whitelist; the
// handler turns that into a 415 Unsupported Media Type response.
//
// Important: the MIME type is derived FROM THE PATH on the server, never
// trusted from the client's Content-Type header. A client that sends a
// PNG body with path "evil.md" will have its MIME stamped as
// "text/markdown" by the server — and the wizard's renderer will then
// fail to display it as markdown because it isn't actually markdown.
// This is fine: garbage in, garbage out, but no security implication.
func mimeForHelpExt(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := helpFileExtMime[ext]
	return mime, ok
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// handleListHelpFiles answers GET .../files/help
func handleListHelpFiles(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	// Authorise: project must exist and belong to the caller. We use
	// the same helper as the existing project endpoints so that a
	// project visibility change (private -> public, etc.) takes effect
	// uniformly across the API surface.
	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "project not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	files, err := store.ListHelpFiles(projectID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not list help files")
	}

	// Quota meta-data is included on the listing so the file manager
	// UI can render the "X bytes used of Y" footer without making a
	// second round-trip.
	usedProject, err := store.SumProjectBytes(projectID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not compute project quota")
	}
	usedUser, err := store.SumUserBytes(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not compute user quota")
	}

	limProject := store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerProject, defaultHelpMaxBytesPerProject,
	)
	limUser := store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerUser, defaultHelpMaxBytesPerUser,
	)

	return ok(c, map[string]any{
		"files": files,
		"quota": map[string]any{
			"project": map[string]any{
				"used":  usedProject,
				"limit": limProject,
			},
			"user": map[string]any{
				"used":  usedUser,
				"limit": limUser,
			},
		},
	})
}

// handleGetHelpFile answers GET .../files/help/*path
//
// Returns the raw bytes with the appropriate Content-Type and an ETag
// derived from updated_at. The browser's If-None-Match handling makes
// repeat reads cheap.
func handleGetHelpFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	path := c.Param("*")

	if err := validateHelpFilePath(path); err != nil {
		return fail(c, http.StatusBadRequest, err.Error())
	}

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "project not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	hf, err := store.GetHelpFile(projectID, path)
	if errors.Is(err, store.ErrNoHelpFile) {
		return fail(c, http.StatusNotFound, "file not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not fetch file")
	}

	// ETag uses the RFC3339 timestamp directly. Clients shouldn't
	// parse this — it's an opaque identifier — so the format doesn't
	// matter as long as it changes on every write. Wrap in quotes per
	// RFC 7232.
	etag := `"` + hf.UpdatedAt.UTC().Format("20060102T150405") + `"`
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "private, must-revalidate")
	return c.Blob(http.StatusOK, hf.MimeType, hf.Content)
}

// handlePutHelpFile answers PUT .../files/help/*path
//
// Body is the raw file bytes. Content-Type from the client is ignored —
// the server derives the MIME type from the path extension via
// mimeForHelpExt.
func handlePutHelpFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	path := c.Param("*")

	if err := validateHelpFilePath(path); err != nil {
		return fail(c, http.StatusBadRequest, err.Error())
	}

	mime, mimeOK := mimeForHelpExt(path)
	if !mimeOK {
		return fail(c, http.StatusUnsupportedMediaType,
			"unsupported file type: only .md, .png, .jpg, .jpeg, .svg, .gif, .webp are allowed")
	}

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "project not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Read the body up to a generous hard ceiling that always exceeds
	// the configured per-project quota. This protects the server
	// from a malicious client that streams forever; the smaller
	// per-project / per-user limits are then applied below.
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, helpFileBodyHardLimit))
	if err != nil {
		return fail(c, http.StatusBadRequest, "could not read request body")
	}
	if int64(len(body)) >= helpFileBodyHardLimit {
		return fail(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("file is too large (max %d bytes)", helpFileBodyHardLimit-1))
	}
	newSize := int64(len(body))

	// Compute deltas: how many bytes is this PUT *adding* to each
	// quota? If the path already exists, we are replacing — only the
	// size difference counts, not the full new file.
	var existingSize int64
	if existing, err := store.GetHelpFile(projectID, path); err == nil {
		existingSize = existing.SizeBytes
	} else if !errors.Is(err, store.ErrNoHelpFile) {
		return fail(c, http.StatusInternalServerError, "could not check existing file")
	}
	delta := newSize - existingSize

	// Per-project quota.
	limProject := int64(store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerProject, defaultHelpMaxBytesPerProject,
	))
	usedProject, err := store.SumProjectBytes(projectID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not compute project quota")
	}
	if usedProject+delta > limProject {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
			"metadata": map[string]any{
				"status": http.StatusRequestEntityTooLarge,
				"error":  "project quota exceeded",
				"scope":  "project",
				"limit":  limProject,
				"used":   usedProject,
			},
			"data": nil,
		})
	}

	// Per-user quota — sum across every project the user owns.
	limUser := int64(store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerUser, defaultHelpMaxBytesPerUser,
	))
	usedUser, err := store.SumUserBytes(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not compute user quota")
	}
	if usedUser+delta > limUser {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
			"metadata": map[string]any{
				"status": http.StatusRequestEntityTooLarge,
				"error":  "user quota exceeded",
				"scope":  "user",
				"limit":  limUser,
				"used":   usedUser,
			},
			"data": nil,
		})
	}

	if err := store.SaveHelpFile(projectID, path, mime, body); err != nil {
		return fail(c, http.StatusInternalServerError, "could not save file")
	}

	return ok(c, map[string]any{
		"path":      path,
		"mimeType":  mime,
		"sizeBytes": newSize,
		"quota": map[string]any{
			"project": map[string]any{"used": usedProject + delta, "limit": limProject},
			"user":    map[string]any{"used": usedUser + delta, "limit": limUser},
		},
	})
}

// handleDeleteHelpFile answers DELETE .../files/help/*path
//
// Idempotent: deleting a file that does not exist returns 200 with
// {deleted: false}, NOT 404. This lets the client UI react to a stale
// view of the tree without a noisy error.
func handleDeleteHelpFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	path := c.Param("*")

	if err := validateHelpFilePath(path); err != nil {
		return fail(c, http.StatusBadRequest, err.Error())
	}

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "project not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	deleted, err := store.DeleteHelpFile(projectID, path)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not delete file")
	}

	usedProject, _ := store.SumProjectBytes(projectID)
	usedUser, _ := store.SumUserBytes(claims.UserID)
	limProject := store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerProject, defaultHelpMaxBytesPerProject,
	)
	limUser := store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerUser, defaultHelpMaxBytesPerUser,
	)

	return ok(c, map[string]any{
		"deleted": deleted,
		"quota": map[string]any{
			"project": map[string]any{"used": usedProject, "limit": limProject},
			"user":    map[string]any{"used": usedUser, "limit": limUser},
		},
	})
}

// renameHelpFileBody is the JSON shape accepted by the rename endpoint.
type renameHelpFileBody struct {
	NewPath string `json:"newPath"`
}

// handleRenameHelpFile answers POST .../files/help/*path/rename
//
// Path is the old path (URL-encoded if needed). Body is JSON with
// `newPath`. Both paths are validated with validateHelpFilePath; the
// MIME type for the new path is recomputed (extensions can change in a
// rename, e.g. "draft.md" -> "draft.txt" — though "txt" is not in the
// MIME whitelist so that case 415s).
func handleRenameHelpFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	oldPath := c.Param("*")

	// Strip the trailing "/rename" because Echo's "*" greedy match
	// captures everything after "/help/", including the "/rename"
	// segment. Pulling it off here is the cleanest way to keep the
	// route registration simple and the path semantics consistent.
	oldPath = strings.TrimSuffix(oldPath, "/rename")

	if err := validateHelpFilePath(oldPath); err != nil {
		return fail(c, http.StatusBadRequest, "old path: "+err.Error())
	}

	var body renameHelpFileBody
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid JSON body")
	}
	if err := validateHelpFilePath(body.NewPath); err != nil {
		return fail(c, http.StatusBadRequest, "new path: "+err.Error())
	}

	newMime, mimeOK := mimeForHelpExt(body.NewPath)
	if !mimeOK {
		return fail(c, http.StatusUnsupportedMediaType,
			"unsupported new file type: only .md, .png, .jpg, .jpeg, .svg, .gif, .webp are allowed")
	}

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "project not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	if err := store.RenameHelpFile(projectID, oldPath, body.NewPath, newMime); err != nil {
		switch {
		case errors.Is(err, store.ErrNoHelpFile):
			return fail(c, http.StatusNotFound, "file not found")
		case errors.Is(err, store.ErrHelpPathConflict):
			return fail(c, http.StatusConflict, "destination path already exists")
		default:
			return fail(c, http.StatusInternalServerError, "could not rename file")
		}
	}

	return ok(c, map[string]any{
		"oldPath":  oldPath,
		"newPath":  body.NewPath,
		"mimeType": newMime,
	})
}

// ─── Constants ────────────────────────────────────────────────────────────────

// defaultHelpMaxBytesPerProject and defaultHelpMaxBytesPerUser are the
// hardcoded fallbacks used when project_settings has no row (corrupt DB,
// rolled-back migration, etc.). They match the seeded values; raising
// either should be done by editing project_settings, not these constants.
// handleInsertHelpFile answers POST .../files/help/insert
//
// Body (JSON):
//
//	{
//	  "basename": "Init",      // method name or "readme"
//	  "language": "en",        // BCP 47 lower-cased
//	  "position": 0,           // 0 = primary; N >= 1 = numbered
//	  "content":  "# Hello…"   // markdown text (may be empty)
//	}
//
// Behaviour: If a file already occupies the requested position
// (and any positions ≥ position), they are renamed in cascade
// (top-down) to free the slot, and the new file is inserted
// there. The whole sequence is one DB transaction — a partial
// shift never persists.
//
// Constraints:
//   - basename grammar matches the IDS spec for method/readme names
//     (alphanumeric + underscore; "readme" is allowed verbatim).
//     Mirrors the validation already applied to PUT paths.
//   - language must be a syntactically valid BCP 47 tag (parsed by
//     golang.org/x/text/language). The frontend datalist suggests
//     known tags; ad-hoc tags are accepted as long as they parse.
//   - position must be 0..N where N is the current count of files
//     in the (basename, language) family. A larger position would
//     leave a gap, which the IDS spec does not define.
//   - content always becomes a markdown file (mime "text/markdown").
//     Image insertion is intentionally out of scope: images don't
//     have an ordering convention.
//
// Response on success:
//
//	{"data": {"path": "Init.en.md", "position": 0}, "metadata": {"status": 200}}
func handleInsertHelpFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Basename string `json:"basename"`
		Language string `json:"language"`
		Position int    `json:"position"`
		Content  string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}

	req.Basename = strings.TrimSpace(req.Basename)
	req.Language = strings.TrimSpace(req.Language)
	if req.Basename == "" {
		return fail(c, http.StatusBadRequest, "basename is required")
	}
	if req.Language == "" {
		return fail(c, http.StatusBadRequest, "language is required")
	}
	if req.Position < 0 {
		return fail(c, http.StatusBadRequest, "position must be >= 0")
	}

	// Basename grammar: alphanumeric + underscore. This rules out
	// dots (which would collide with the path separator), slashes,
	// and any character disallowed by helpFilePathRe. We do not
	// reuse helpFilePathRe directly because that one is for full
	// paths; basename here is just the leftmost segment.
	if !helpBasenameRe.MatchString(req.Basename) {
		return fail(c, http.StatusBadRequest,
			"basename must match [A-Za-z0-9_]+ (no dots, slashes, or hyphens)")
	}

	// Language: validate as BCP 47. We accept any tag that parses,
	// not just tags from the static autocomplete list — the user
	// may legitimately want a regional variant we did not pre-list.
	// The store and downstream consumers expect lower-case tags
	// (the IDS spec is case-insensitive on language but the
	// filename grammar is lower-case for consistency); we
	// canonicalise here to remove any "pt-BR" vs "pt-br" mismatch.
	if _, err := language.Parse(req.Language); err != nil {
		return fail(c, http.StatusBadRequest,
			"language must be a valid BCP 47 tag (e.g. en, pt-BR, zh-CN)")
	}
	canonicalLang := strings.ToLower(req.Language)

	// Verify the project belongs to the authenticated user. Without
	// this check, a leaked id would let anyone insert files into
	// any project.
	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "project not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	body := []byte(req.Content)
	if int64(len(body)) >= helpFileBodyHardLimit {
		return fail(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("content is too large (max %d bytes)", helpFileBodyHardLimit-1))
	}

	// Per-project / per-user quota. The cascade renames don't
	// change the byte total (no content moves; only paths change),
	// so we only count the new file's bytes against the quota.
	newSize := int64(len(body))

	limProject := int64(store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerProject, defaultHelpMaxBytesPerProject,
	))
	usedProject, err := store.SumProjectBytes(projectID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not compute project quota")
	}
	if usedProject+newSize > limProject {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
			"metadata": map[string]any{
				"status": http.StatusRequestEntityTooLarge,
				"error":  "project quota exceeded",
				"scope":  "project",
				"limit":  limProject,
				"used":   usedProject,
			},
			"data": nil,
		})
	}

	limUser := int64(store.GetSettingInt(
		store.SettingHelpFilesMaxBytesPerUser, defaultHelpMaxBytesPerUser,
	))
	usedUser, err := store.SumUserBytes(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not compute user quota")
	}
	if usedUser+newSize > limUser {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
			"metadata": map[string]any{
				"status": http.StatusRequestEntityTooLarge,
				"error":  "user quota exceeded",
				"scope":  "user",
				"limit":  limUser,
				"used":   usedUser,
			},
			"data": nil,
		})
	}

	if err := store.InsertHelpFileWithShift(
		projectID, req.Basename, canonicalLang, req.Position, body,
	); err != nil {
		if errors.Is(err, store.ErrHelpPathConflict) {
			return fail(c, http.StatusConflict,
				"a file at the requested position already exists outside the standard naming convention")
		}
		return fail(c, http.StatusInternalServerError, "could not insert file")
	}

	// Mirror the new path back so the client can select it
	// immediately without recomputing the convention.
	insertedPath := req.Basename + "." + canonicalLang + ".md"
	if req.Position > 0 {
		insertedPath = fmt.Sprintf("%s.%d.%s.md",
			req.Basename, req.Position, canonicalLang)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK},
		"data": map[string]any{
			"path":     insertedPath,
			"position": req.Position,
		},
	})
}

// helpBasenameRe matches the leftmost segment of an insertable help
// file: alphanumeric and underscore, 1+ characters. The dot, hyphen,
// and slash that helpFilePathRe allows in full paths are rejected
// here because they would clash with the (basename, position,
// language) decomposition.
var helpBasenameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// ─── Quota constants ─────────────────────────────────────────────────────────

const (
	defaultHelpMaxBytesPerProject = 5_000_000
	defaultHelpMaxBytesPerUser    = 50_000_000

	// helpFileBodyHardLimit is the absolute ceiling on a single PUT
	// body, set well above the per-project quota so that an overshoot
	// always trips the quota check (which produces a friendlier error
	// with "used" / "limit" details) rather than this hard cut-off.
	// 8 MB headroom over the 5 MB default per-project quota.
	helpFileBodyHardLimit = 8_000_000
)
