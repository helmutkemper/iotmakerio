// server/handler/projectexport/handler.go — HTTP handlers for the
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// "Github package" export feature.
//
// Two endpoints, both per-project, both gated by the bearer token
// of the project's owner:
//
//	POST /api/v1/projects/:id/export/check
//	  Runs the full pre-flight validation (parse, analyze,
//	  incomplete cards, help-file coverage, examples). Returns 200
//	  with `{ ok: bool, issues: [...] }`. The SPA renders the
//	  grouped issue list when ok==false; the user fixes everything
//	  in one go and re-checks.
//
//	GET  /api/v1/projects/:id/export/zip
//	  Re-validates internally as a defence-in-depth measure (the
//	  state could have changed between check and download). On
//	  success streams `application/zip` with a Content-Disposition
//	  filename derived from the project name and a UTC ISO 8601
//	  timestamp. On validation failure returns 409 + JSON of the
//	  same shape as /check — the SPA detects this via Content-Type
//	  and re-shows the issues modal.
//
// Both handlers go through GetProjectByIDAndUser so cross-tenant
// access is impossible. The same auth pattern as every other
// project route in this codebase.
//
// Português: handlers HTTP para o feature "Github package".
// Endpoint /check valida e devolve a lista de problemas; endpoint
// /zip stream o ZIP em si, com revalidação interna como defesa em
// profundidade. Auth idêntico aos demais endpoints de projeto.
package projectexport

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"server/handler/spaauth"
	pe "server/projectexport"
	"server/store"
)

// checkResponse is the wire shape of POST /check. Defined as a
// named struct so the SPA can refer to a stable schema rather than
// chasing inline maps. The same shape doubles as the 409 body of
// /zip (when re-validation rejects the export mid-download
// preparation), so the SPA's modal renderer has one decoder for
// both endpoints.
type checkResponse struct {
	OK     bool       `json:"ok"`
	Issues []pe.Issue `json:"issues"`
}

// handleCheck runs Validate and returns the grouped issues. Always
// 200 — even when the project is unexportable — because the
// validation result is data, not an HTTP error. The SPA branches
// on the `ok` field.
func handleCheck(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	// Auth + existence check. GetProjectByIDAndUser returns
	// ErrNotFound both for "no such project" and "wrong owner" —
	// the merged error is intentional (avoids leaking the
	// existence of other users' projects).
	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return c.JSON(http.StatusNotFound, map[string]any{
				"metadata": map[string]any{"status": http.StatusNotFound, "error": "project not found"},
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"metadata": map[string]any{"status": http.StatusInternalServerError, "error": "internal error"},
		})
	}

	res := pe.Validate(projectID, claims.UserID)
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK},
		"data": checkResponse{
			OK:     res.OK(),
			Issues: res.Issues,
		},
	})
}

// handleZip streams the ZIP archive. Validates one more time
// before opening the response stream — so a 409 body can be
// returned cleanly (no headers committed yet to a binary stream).
func handleZip(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	// Auth + load project to get the user-visible name (for the
	// filename) and the user record (for owner / locale).
	proj, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return c.JSON(http.StatusNotFound, map[string]any{
				"metadata": map[string]any{"status": http.StatusNotFound, "error": "project not found"},
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"metadata": map[string]any{"status": http.StatusInternalServerError, "error": "internal error"},
		})
	}

	owner, oErr := store.GetUserByID(claims.UserID)
	if oErr != nil {
		// User record missing under our own bearer claims is
		// abnormal but recoverable — fall back to a neutral owner
		// name so the export still produces a valid LICENSE.
		owner = &store.User{Username: "the project author", PreferredLocale: "en"}
	}

	// ── Defence-in-depth re-validation ──────────────────────────
	// The SPA hits /check first and only fires /zip when it sees
	// ok=true. But seconds can pass between the two; the user might
	// edit something in another tab. Re-running Validate here
	// catches that race. We return 409 (Conflict) with the same
	// JSON shape /check uses — the SPA detects this via
	// Content-Type and re-displays the issues modal.
	res := pe.Validate(projectID, claims.UserID)
	if !res.OK() {
		return c.JSON(http.StatusConflict, map[string]any{
			"metadata": map[string]any{"status": http.StatusConflict, "error": "project not exportable"},
			"data": checkResponse{
				OK:     false,
				Issues: res.Issues,
			},
		})
	}

	// ── Stream the ZIP ──────────────────────────────────────────
	// We commit the binary headers BEFORE writing any bytes so the
	// browser starts the download UI immediately. archive/zip's
	// writer flushes lazily, so the actual bytes only start flowing
	// after the first file is added.
	now := time.Now()
	filename := pe.SuggestedFilename(proj.Name, now)

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "application/zip")
	w.Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+filename+`"`)
	// Disable proxies/caches: every export is a fresh snapshot;
	// nothing about this response is cacheable.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	if buildErr := pe.Build(w, pe.BuildOptions{
		ProjectID:   projectID,
		ProjectName: proj.Name,
		OwnerName:   owner.Username,
		Locale:      owner.PreferredLocale,
		Now:         now,
	}); buildErr != nil {
		// Headers are already on the wire — we can't change them.
		// Best we can do is log and bail; the client will see a
		// truncated ZIP and can retry. echo.Context's Error path
		// would try to write a JSON body which is incorrect for
		// our committed Content-Type.
		c.Logger().Errorf("projectexport.Build failed: %v", buildErr)
		return nil
	}
	return nil
}
