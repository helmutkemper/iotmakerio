// server/handler/editorapi/handlers.go — Editor settings endpoints.
//
// These endpoints serve the "Editor Settings" portal page where the maker
// configures IDE preferences: interface language, menu profile, per-item
// menu visibility, and stage (workspace) behaviour.
//
// Routes:
//
//	GET  /api/v1/editor/menu-prefs         — full menu tree + user's hidden items
//	PUT  /api/v1/editor/menu-prefs         — batch update hidden items
//	DELETE /api/v1/editor/menu-prefs       — reset all overrides to admin defaults
//
//	GET    /api/v1/editor/stage-prefs  — per-user stage (canvas) knobs
//	PUT    /api/v1/editor/stage-prefs  — patch-update one or more knobs
//	DELETE /api/v1/editor/stage-prefs  — reset all knobs to defaults
//
// Stage prefs include: zoom sensitivity (`zoomStep`), whether left-click
// drag on empty stage pans the camera (`panEmptyArea`), and whether to
// show a grab-cursor hint on hover (`showGrabCursor`). All are bounded
// scalars — no free-form text. See store/stage_prefs.go for shape.
//
// All routes require authentication (RequireBearerToken).
package editorapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"server/handler/spaauth"
	"server/store"
)

// ─── Menu prefs: full tree + user overrides ─────────────────────────────────

// handleGetMenuPrefs returns the complete menu tree for the user's profile
// (layers 1+2 applied: admin layout + visibility rules) WITHOUT applying
// layer 3 (user prefs). The user's current hidden slots are returned
// separately so the frontend can render checkboxes with the correct state.
//
// Response:
//
//	{
//	  "tree": [ { slot_id, label, children, ... } ],
//	  "hidden": [ "SysLoop", "SysMul", ... ],
//	  "hide_overlay": false
//	}
func handleGetMenuPrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	locale := strings.TrimSpace(c.QueryParam("locale"))
	if locale == "" {
		locale = "en"
	}
	if idx := strings.IndexByte(locale, '-'); idx > 0 {
		locale = locale[:idx]
	}

	// Resolve the user's profile for layers 1+2.
	profileID := store.GetUserMenuProfileID(claims.UserID)

	userGroupIDs, err := store.GetUserGroupIDs(claims.UserID)
	if err != nil {
		userGroupIDs = []string{}
	}

	user, _ := store.GetUserByID(claims.UserID)
	countryCode := ""
	if user != nil {
		countryCode = user.CountryCode
	}

	// Get the tree WITHOUT user prefs (pass empty userID for layer 3 skip).
	// Layers 1 (admin layout visible) and 2 (visibility rules) are applied.
	result, err := store.GetMenuTree(profileID, locale, "", userGroupIDs, countryCode)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "failed to load menu tree")
	}
	if result.Tree == nil {
		result.Tree = []*store.MenuTreeNode{}
	}

	// Get the user's current hidden slots.
	hiddenMap := store.LoadUserHiddenSlots(claims.UserID)
	hidden := make([]string, 0, len(hiddenMap))
	for slotID := range hiddenMap {
		hidden = append(hidden, slotID)
	}

	return ok(c, map[string]any{
		"tree":         result.Tree,
		"hidden":       hidden,
		"hide_overlay": result.HideUserPrefsOverlay,
	})
}

// ─── Menu prefs: batch update ───────────────────────────────────────────────

// handleSetMenuPrefs receives a list of slot_ids that should be hidden.
// All previous overrides are replaced: items in the list become hidden,
// items NOT in the list are restored to admin default (override deleted).
//
// Request:
//
//	{ "hidden": ["SysLoop", "SysMul", "SysDiv"] }
func handleSetMenuPrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var body struct {
		Hidden []string `json:"hidden"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}

	// Build the new hidden set.
	newHidden := make(map[string]bool, len(body.Hidden))
	for _, slotID := range body.Hidden {
		slotID = strings.TrimSpace(slotID)
		if slotID != "" {
			newHidden[slotID] = true
		}
	}

	// Get current overrides to compute the diff.
	currentHidden := store.LoadUserHiddenSlots(claims.UserID)

	// Items to hide (not currently hidden).
	for slotID := range newHidden {
		if !currentHidden[slotID] {
			if err := store.SetUserMenuPref(claims.UserID, slotID, false); err != nil {
				return fail(c, http.StatusInternalServerError, "failed to save preference")
			}
		}
	}

	// Items to restore (currently hidden but not in the new list).
	for slotID := range currentHidden {
		if !newHidden[slotID] {
			if err := store.SetUserMenuPref(claims.UserID, slotID, true); err != nil {
				return fail(c, http.StatusInternalServerError, "failed to save preference")
			}
		}
	}

	return ok(c, map[string]any{"hidden": len(newHidden)})
}

// ─── Menu prefs: reset ──────────────────────────────────────────────────────

// handleResetMenuPrefs deletes all visibility overrides for the user,
// restoring the admin-defined menu layout.
func handleResetMenuPrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	if err := store.ResetUserMenuPrefs(claims.UserID); err != nil {
		return fail(c, http.StatusInternalServerError, "failed to reset preferences")
	}

	return ok(c, map[string]any{"reset": true})
}

// ─── Response helpers ───────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}

// ─── Stage prefs: zoom, pan, cursor knobs ─────────────────────────────────────

// handleGetStagePrefs returns the resolved stage preferences for the
// authenticated user. Defaults fill in for fields the user never set
// — the response always has concrete values ready for UI display.
//
// Response:
//
//	{ "prefs": { "zoomStep": 0.03, "panEmptyArea": true,
//	             "showGrabCursor": false },
//	  "defaults": { ...same shape... } }
//
// The `defaults` block is included so the portal can render "Reset"
// buttons next to each control showing what the default would be,
// and so the IDE can tell whether the user has customised a value
// (server authority on defaults, IDE never owns the constants).
func handleGetStagePrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	prefs, err := store.GetStagePrefs(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "failed to load stage preferences")
	}

	return ok(c, map[string]any{
		"prefs":    prefs,
		"defaults": store.DefaultStagePrefs(),
	})
}

// handleUpdateStagePrefs applies a partial update to the user's
// preferences. Each field in the request body is optional; omitted
// fields stay as they are in the database.
//
// Request (any subset of the three fields):
//
//	{ "zoomStep": 0.04 }
//	{ "panEmptyArea": false, "showGrabCursor": true }
//
// Response mirrors handleGetStagePrefs — the resolved prefs plus
// the server defaults, so the UI can re-render without a second GET.
//
// Validation: zoomStep must be > 0 and <= 1.0 (sanity; the UI
// constrains tighter to 0.01–0.15). Booleans pass through as-is.
func handleUpdateStagePrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var body store.StagePrefsUpdate
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}

	// Sanity-check the zoom step. The UI should never send an
	// out-of-range value, but the HTTP surface has to defend
	// itself anyway.
	if body.ZoomStep != nil {
		v := *body.ZoomStep
		if v <= 0 || v > 1.0 {
			return fail(c, http.StatusBadRequest,
				"zoomStep must be between 0 (exclusive) and 1.0")
		}
	}

	prefs, err := store.UpdateStagePrefs(claims.UserID, body)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "failed to save stage preferences")
	}

	return ok(c, map[string]any{
		"prefs":    prefs,
		"defaults": store.DefaultStagePrefs(),
	})
}

// handleResetStagePrefs deletes the user's row and returns the
// default prefs. Idempotent — calling when no row exists is a no-op
// and still returns the defaults.
func handleResetStagePrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	prefs, err := store.ResetStagePrefs(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "failed to reset stage preferences")
	}

	return ok(c, map[string]any{
		"prefs":    prefs,
		"defaults": store.DefaultStagePrefs(),
	})
}
