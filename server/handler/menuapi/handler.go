// server/handler/menuapi/handler.go — GET /api/menu/sections
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Returns the list of active menu sections visible to the requesting user,
// with their items pre-loaded. The WASM frontend calls this once at startup
// to build the dynamic part of the IDE main menu.
//
// Authentication: optional. Anonymous users receive only sections that have
// no visibility restrictions (no menu_section_visibility rows).
//
// Response shape:
//
//	{
//	  "metadata": { "status": 200, "error": "" },
//	  "data": {
//	    "sections": [
//	      {
//	        "id": "...",
//	        "slug": "sparkfun",
//	        "name": "Sparkfun",
//	        "position": 2,
//	        "color_normal": "#8B0000",
//	        "color_attention": "#C42B2B",
//	        "color_featured": "#E05050",
//	        "icon_fa": "microchip",
//	        "items": [
//	          {
//	            "id": "...",
//	            "item_type": "project",
//	            "item_ref_id": "abc123",
//	            "position": 1,
//	            "title": "RedBoard Blink Starter",
//	            "card_image": "/static/.../img/redboard.png"
//	          }
//	        ]
//	      }
//	    ]
//	  }
//	}
//
// The title and card_image fields are resolved by joining against the
// projects or template_packages tables so the WASM receives display-ready
// data without a second round-trip.
package menuapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"server/middleware"
	"server/store"
)

// Register wires the public menu routes onto the given Echo group.
// No auth middleware is required — the handler handles anonymous users itself.
func Register(g *echo.Group) {
	g.GET("/sections", handleGetSections)
	g.GET("/categories", handleGetCategories)

	// Tree endpoint — returns the full resolved menu tree for the WASM.
	// Replaces the hardcoded menu structure in the WASM's menuBuilder.go
	// with a database-driven tree that supports profiles, custom labels,
	// and per-audience help text.
	registerTree(g)
}

// handleGetSections returns active menu sections for the requesting user.
//
// Anonymous requests receive unrestricted sections only.
// Authenticated requests receive unrestricted sections plus sections whose
// visibility rules match the user's groups and country.
func handleGetSections(c echo.Context) error {
	// Resolve user context — may be nil for anonymous requests.
	user := middleware.UserFromContext(c)

	var groupIDs []string
	countryCode := ""

	if user != nil {
		countryCode = user.CountryCode
		var err error
		groupIDs, err = store.GetUserGroupIDs(user.ID)
		if err != nil {
			c.Logger().Errorf("[menuapi] GetUserGroupIDs %s: %v", user.ID, err)
			return fail(c, http.StatusInternalServerError, "internal error")
		}
	}

	sections, err := store.ListSectionsForUser(groupIDs, countryCode)
	if err != nil {
		c.Logger().Errorf("[menuapi] ListSectionsForUser: %v", err)
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Resolve item titles and card images from the projects / template_packages
	// tables so the WASM does not need a second request.
	if err := resolveItemMeta(sections); err != nil {
		c.Logger().Errorf("[menuapi] resolveItemMeta: %v", err)
		// Non-fatal: return sections with empty titles rather than failing.
	}

	return ok(c, map[string]any{
		"sections": sections,
	})
}

// resolveItemMeta fills in Title and CardImage for every item in every section
// by querying the projects and template_packages tables.
//
// Items whose referenced record no longer exists are left with empty strings
// so that stale references in menu_section_items do not crash the response.
func resolveItemMeta(sections []*store.MenuSection) error {
	// Collect IDs by type to minimise round-trips.
	projectIDs := map[string]bool{}
	templateIDs := map[string]bool{}
	deviceIDs := map[string]bool{}
	for _, s := range sections {
		for _, item := range s.Items {
			switch item.ItemType {
			case "project":
				projectIDs[item.ItemRefID] = true
			case "template":
				templateIDs[item.ItemRefID] = true
			case "device":
				deviceIDs[item.ItemRefID] = true
			}
		}
	}

	// Fetch project card data.
	projectMeta := map[string]itemMeta{}
	for id := range projectIDs {
		p, err := store.GetProjectByID(id)
		if err != nil {
			continue // missing reference — leave title empty
		}
		projectMeta[id] = itemMeta{
			title:     p.CardTitle,
			cardImage: p.CardImage,
		}
	}

	// Fetch template card data.
	templateMeta := map[string]itemMeta{}
	for id := range templateIDs {
		t, err := store.GetTemplatePkg(id)
		if err != nil {
			continue
		}
		templateMeta[id] = itemMeta{
			title:     t.Name,
			cardImage: "", // templates do not yet have card images
		}
	}

	// Fetch device (blackbox) display names, struct names, category and subcategory.
	deviceMeta := map[string]itemMeta{}
	for id := range deviceIDs {
		var displayName, structName, cat, catIcon, subcat, subcatIcon string
		err := store.DB.QueryRow(`
			SELECT COALESCE(NULLIF(b.display_name_human,''), b.display_name),
			       b.display_name,
			       COALESCE(pc.name, ''),
			       COALESCE(pc.icon_fa, ''),
			       COALESCE(ps.name, ''),
			       COALESCE(ps.icon_fa, '')
			FROM blackboxes b
			LEFT JOIN project_categories    pc ON pc.id = b.category_id
			LEFT JOIN project_subcategories ps ON ps.id = b.subcategory_id
			WHERE b.id = ?`, id).Scan(&displayName, &structName, &cat, &catIcon, &subcat, &subcatIcon)
		if err != nil {
			continue
		}
		deviceMeta[id] = itemMeta{
			title:           displayName,
			structName:      structName,
			category:        cat,
			categoryIcon:    catIcon,
			subcategory:     subcat,
			subcategoryIcon: subcatIcon,
		}
	}

	// Populate item fields.
	for _, s := range sections {
		for _, item := range s.Items {
			var meta itemMeta
			switch item.ItemType {
			case "project":
				meta = projectMeta[item.ItemRefID]
			case "template":
				meta = templateMeta[item.ItemRefID]
			case "device":
				meta = deviceMeta[item.ItemRefID]
			}
			item.Title = meta.title
			item.CardImage = meta.cardImage
			item.StructName = meta.structName
			item.Category = meta.category
			item.CategoryIcon = meta.categoryIcon
			item.Subcategory = meta.subcategory
			item.SubcategoryIcon = meta.subcategoryIcon
		}
	}

	return nil
}

// itemMeta holds the display fields resolved from the backing tables.
type itemMeta struct {
	title           string
	cardImage       string
	structName      string // Go struct name — only for devices
	category        string // resolved category name
	categoryIcon    string // resolved category icon_fa
	subcategory     string // resolved subcategory name
	subcategoryIcon string // resolved subcategory icon_fa
}

// handleGetCategories returns all categories with their subcategories and icons.
// Used by the WASM IDE at startup to assign proper icons to category hexes
// in the main menu. No authentication required — this is public metadata.
func handleGetCategories(c echo.Context) error {
	cats, err := store.ListCategories()
	if err != nil {
		return fail(c, 500, "internal error")
	}

	type subOut struct {
		Name   string `json:"name"`
		IconFA string `json:"icon_fa"`
	}
	type catOut struct {
		Name          string   `json:"name"`
		IconFA        string   `json:"icon_fa"`
		Subcategories []subOut `json:"subcategories"`
	}

	result := make([]catOut, 0, len(cats))
	for _, cat := range cats {
		subs, err := store.ListSubcategoriesByCategoryID(cat.ID)
		if err != nil {
			continue
		}
		subList := make([]subOut, 0, len(subs))
		for _, s := range subs {
			subList = append(subList, subOut{Name: s.Name, IconFA: s.IconFA})
		}
		result = append(result, catOut{
			Name:          cat.Name,
			IconFA:        cat.IconFA,
			Subcategories: subList,
		})
	}

	return ok(c, map[string]any{"categories": result})
}

// ─── Response helpers ─────────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK, "error": ""},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
