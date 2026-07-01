// server/handler/i18n/handlers.go — Public translation read API.
//
// Scope
// -----
//
// This package owns only the PUBLIC portion of the translation API:
//
//	GET  /api/v1/translations                — list registered locales
//	GET  /api/v1/translations/:locale        — fetch one locale bundle
//	POST /api/v1/translations/:locale/missing — IDE WASM reports missing key
//
// All WRITE operations (save bundle, create/update locale) live in
// server/handler/controlapi/translations.go and require a control-panel
// token plus per-save OTP confirmation. That split is intentional:
//
//   - Reads are needed by every anonymous visitor who loads the portal or
//     the IDE; requiring auth would break the UI for logged-out users.
//   - Writes can only come from an admin and must be individually
//     acknowledged — translations ship to every user of the product.
//
// The /missing endpoint is a telemetry sink and therefore public: the IDE
// WASM client calls it automatically whenever it fails to resolve a key,
// and those users are not always admins. InsertMissingMessage never
// overwrites an existing translation, so there is no attack vector beyond
// spurious "*" rows that the admin can review and prune.
package i18n

import (
	"net/http"
	"sync"

	"github.com/labstack/echo/v4"

	"server/store"
)

// ─── Read handlers ────────────────────────────────────────────────────────────

// handleListLocales returns every registered locale code.
// Used by the IDE WASM client on startup to pick the right bundle.
func handleListLocales(c echo.Context) error {
	locales, err := store.ListLocales()
	if err != nil {
		return apiErr(c, http.StatusInternalServerError, "internal error")
	}
	return apiOK(c, map[string]any{"locales": locales})
}

// handleGetBundle returns the full translation bundle for one locale.
// Unknown locales return 404 — the caller should fall back to en-US.
func handleGetBundle(c echo.Context) error {
	b, err := store.GetBundle(c.Param("locale"))
	if err != nil {
		if err == store.ErrNotFound {
			return apiErr(c, http.StatusNotFound, "locale not found")
		}
		return apiErr(c, http.StatusInternalServerError, "internal error")
	}
	return apiOK(c, b)
}

// ─── Missing-key telemetry ────────────────────────────────────────────────────
//
// missingMu serialises concurrent writes of missing keys. On the first load
// of the IDE WASM runtime, dozens of lookup failures can fire at once; the
// mutex turns them into a queue so SQLite never sees a write storm.

var missingMu sync.Mutex

// handleReportMissing records a translation key the IDE WASM runtime failed
// to resolve. The key is inserted into every registered locale with the
// caller-supplied value prefixed by "*" so the admin can spot it at a glance.
//
// Contract:
//   - Always returns 200. The IDE does not wait on this endpoint and there
//     is no user-visible action to take on failure; a silent log warning is
//     sufficient.
//   - Never overwrites an existing translation (store.InsertMissingMessage
//     uses INSERT OR IGNORE).
//   - A malformed or missing body is a no-op — the IDE may retry later.
func handleReportMissing(c echo.Context) error {
	var msg store.TrMessage
	if err := c.Bind(&msg); err != nil || msg.ID == "" {
		// Silent success — the client does not care and there is no user
		// to show an error to.
		return c.JSON(http.StatusOK, map[string]bool{"ok": true})
	}

	// The "*" prefix flags the entry as "auto-captured, needs translation".
	// The admin UI filters on it to surface work-to-do.
	msg.Other = "*" + msg.Other

	missingMu.Lock()
	defer missingMu.Unlock()

	if err := store.InsertMissingMessage(msg); err != nil {
		c.Logger().Warnf("[i18n/missing] id=%s: %v", msg.ID, err)
	}

	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// ─── Response envelope helpers ────────────────────────────────────────────────
//
// Mirror the canonical { metadata, data } envelope used across the portal.
// Defined locally because each handler package owns its own helpers (see
// ENDPOINT_STYLE notes in CLAUDE.md).

func apiOK(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK},
		"data":     data,
	})
}

func apiErr(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
