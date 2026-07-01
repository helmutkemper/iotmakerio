// server/handler/menuapi/tree.go — GET /api/v1/menu/tree
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Returns the complete resolved menu tree for the requesting user's profile
// and locale. The WASM frontend calls this once at startup to build the full
// IDE main menu — system items, branded sections, device categories, and templates.
//
// The tree is filtered through 3 visibility layers:
//  1. Admin layout: menu_layout.visible per profile
//  2. Visibility rules: menu_visibility_rules (group + country + date)
//  3. User prefs: menu_user_prefs (maker's personal checkbox overrides)
//
// Authentication: optional (uses OptionalAuth middleware).
//   - Anonymous users receive the default profile's tree with no user prefs.
//   - Authenticated users receive their assigned profile + personal prefs.
//
// Query parameters:
//   - locale: the user's locale (e.g., "pt", "en"). Defaults to "en".
//     Used for label and help cascade resolution.
//
// Response shape:
//
//	{
//	  "metadata": { "status": 200 },
//	  "data": {
//	    "profile_id": "default",
//	    "hide_user_prefs_overlay": false,
//	    "tree": [
//	      {
//	        "slot_id": "SysMath",
//	        "slot_type": "system",
//	        "item_type": "submenu",
//	        "label": "Math",
//	        "label_key": "menuMainMath",
//	        "label_fallback": "Math",
//	        "has_custom_label": false,
//	        "icon_fa": "square-root-variable",
//	        "icon_viewbox": "0 0 640 640",
//	        "children": [...]
//	      },
//	      {
//	        "slot_id": "Dev_APDS9960",
//	        "slot_type": "device",
//	        "item_type": "action",
//	        "label": "APDS-9960",
//	        "device_struct_name": "APDS9960",
//	        "children": null
//	      }
//	    ]
//	  }
//	}
package menuapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"server/middleware"
	"server/store"
)

// registerTree adds the tree endpoint to the menu group.
// Called from Register() in handler.go.
func registerTree(g *echo.Group) {
	// OptionalAuth parses the JWT if present but does not block anonymous.
	// UserFromContext returns nil for anonymous, *User for authenticated.
	g.GET("/tree", handleGetTree, middleware.OptionalAuth())
}

// handleGetTree returns the resolved menu tree for the requesting user.
func handleGetTree(c echo.Context) error {
	// ── Parse locale ────────────────────────────────────────────────────
	locale := strings.TrimSpace(c.QueryParam("locale"))
	if locale == "" {
		locale = "en"
	}
	// Normalize locale: accept "pt-BR" but match as "pt" in the database.
	if idx := strings.IndexByte(locale, '-'); idx > 0 {
		locale = locale[:idx]
	}

	// ── Resolve user context ────────────────────────────────────────────
	// OptionalAuth middleware populates this when a valid token is present.
	user := middleware.UserFromContext(c)

	var userID string
	var userGroupIDs []string
	var countryCode string

	if user != nil {
		userID = user.ID
		countryCode = user.CountryCode

		var err error
		userGroupIDs, err = store.GetUserGroupIDs(user.ID)
		if err != nil {
			c.Logger().Errorf("[menuapi/tree] GetUserGroupIDs %s: %v", user.ID, err)
			// Non-fatal — proceed without group filtering.
			userGroupIDs = []string{}
		}
	}

	// ── Resolve profile ─────────────────────────────────────────────────
	// If the user has a menu_profile_id, use that. Otherwise, use default.
	profileID := ""
	if userID != "" {
		profileID = store.GetUserMenuProfileID(userID)
	}

	// ── Build the tree ──────────────────────────────────────────────────
	result, err := store.GetMenuTree(profileID, locale, userID, userGroupIDs, countryCode)
	if err != nil {
		c.Logger().Errorf("[menuapi/tree] GetMenuTree: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to load menu tree")
	}

	// Return empty tree (not error) if no items exist yet.
	if result.Tree == nil {
		result.Tree = []*store.MenuTreeNode{}
	}

	return ok(c, map[string]any{
		"profile_id":              result.ProfileID,
		"hide_user_prefs_overlay": result.HideUserPrefsOverlay,
		"tree":                    result.Tree,
	})
}
