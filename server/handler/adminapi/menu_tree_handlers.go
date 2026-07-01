// server/handler/adminapi/menu_tree_handlers.go — Admin CRUD for the
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// database-driven menu tree system.
//
// All write operations require OTP confirmation, reusing the same
// handleRequestMenuOTP / consumeMenuOTP flow from sections_debug.go.
//
// Routes (registered via RegisterMenuTree on the control panel group):
//
//	Catalog:
//	  GET    /menu/catalog                           — list all catalog items
//	  POST   /menu/catalog                           — create item (OTP)
//	  DELETE /menu/catalog/:slot_id                   — delete unlocked item (OTP)
//
//	Profiles:
//	  GET    /menu/profiles                           — list all profiles
//	  POST   /menu/profiles                           — create profile (OTP)
//	  PUT    /menu/profiles/:id                       — update name/desc (OTP)
//	  DELETE /menu/profiles/:id                       — delete non-locked (OTP)
//	  PATCH  /menu/profiles/:id/activate              — set as active (OTP)
//	  POST   /menu/profiles/:id/clone                 — clone from source (OTP)
//
//	Layout:
//	  GET    /menu/profiles/:id/layout                — full tree for profile
//	  PATCH  /menu/profiles/:id/layout/reorder        — bulk reorder (OTP)
//	  PATCH  /menu/profiles/:id/layout/:slot_id       — update fields (OTP)
//
// OTP is shared with sections — POST /sections/request-otp generates a code
// valid for any menu write (sections, catalog, profiles, layout).
package adminapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/store"
)

// RegisterMenuTree wires all menu tree admin routes onto the given group.
// The group should already have RequireControlToken middleware applied.
func RegisterMenuTree(g *echo.Group) {
	// Catalog.
	g.GET("/menu/catalog", handleListCatalog)
	g.POST("/menu/catalog", handleCreateCatalogItem)
	g.DELETE("/menu/catalog/:slot_id", handleDeleteCatalogItem)

	// Profiles.
	g.GET("/menu/profiles", handleListMenuProfiles)
	g.POST("/menu/profiles", handleCreateMenuProfile)
	g.PUT("/menu/profiles/:id", handleUpdateMenuProfile)
	g.DELETE("/menu/profiles/:id", handleDeleteMenuProfile)
	g.PATCH("/menu/profiles/:id/activate", handleActivateMenuProfile)
	g.POST("/menu/profiles/:id/clone", handleCloneMenuProfile)

	// Layout.
	g.GET("/menu/profiles/:id/layout", handleGetLayout)
	g.PATCH("/menu/profiles/:id/layout/reorder", handleReorderLayout)
	g.PATCH("/menu/profiles/:id/layout/:slot_id", handleUpdateLayoutSlot)

	// Layout labels (per-locale overrides).
	g.GET("/menu/profiles/:id/layout/:slot_id/labels", handleListLayoutLabels)
	g.PUT("/menu/profiles/:id/layout/:slot_id/labels/:locale", handleUpsertLayoutLabel)
	g.DELETE("/menu/profiles/:id/layout/:slot_id/labels/:locale", handleDeleteLayoutLabel)

	// Help markdown. Multiple ordered tabs per (slot, profile, locale)
	// bucket are supported — the `ord` path segment positions the entry
	// within the bucket (0 = primary, N >= 1 = numbered tab; same shape
	// as the `readme.<N>.<lang>.md` filename grammar used by devices).
	g.GET("/menu/help/:slot_id", handleListHelp)
	g.PUT("/menu/help/:slot_id/:profile_id/:locale/:ord", handleUpsertHelp)
	g.DELETE("/menu/help/:slot_id/:profile_id/:locale/:ord", handleDeleteHelp)

	// Help files — markdown and binary assets that travel with a slot's
	// help. Mirrors the project_help_files API in projectapi/help_files.go
	// so the Control Panel's file manager can reuse the same client
	// patterns. Markdown is referenced via the readme.<N>.<lang>.md
	// filename grammar; images go in by their own filenames and are
	// rewritten transparently into `data:` URLs at serve time.
	//
	// Echo's "*path" greedy parameter captures the remainder of the URL
	// as a raw string, which is why the same handler accepts
	// "diagram.png" and "examples/wiring.svg" without URL-decoding
	// gymnastics on this side.
	g.GET("/menu/help/:slot_id/files", handleListMenuHelpFiles)
	g.GET("/menu/help/:slot_id/files/*", handleGetMenuHelpFile)
	g.PUT("/menu/help/:slot_id/files/*", handlePutMenuHelpFile)
	g.DELETE("/menu/help/:slot_id/files/*", handleDeleteMenuHelpFile)
	g.POST("/menu/help/:slot_id/files/*/rename", handleRenameMenuHelpFile)

	// Section picker — full-screen modal for adding devices/templates to sections.
	g.GET("/menu/section-picker", handleSectionPicker)
	g.GET("/menu/section-picker/:section_id/children", handleSectionChildren)
	g.POST("/menu/section-items", handleAddSectionItems)
	g.DELETE("/menu/section-items/:section_id/:slot_id", handleRemoveSectionItem)

	// ── Admin user menu prefs — manage a specific user's visibility overrides.
	g.GET("/menu/user-prefs", handleAdminGetUserPrefs)
	g.PUT("/menu/user-prefs", handleAdminSetUserPrefs)
	g.DELETE("/menu/user-prefs", handleAdminResetUserPrefs)
	g.DELETE("/menu/user-panel-prefs", handleAdminResetUserPanelPrefs)

	// ── Visibility rules — restrict items to groups/countries/dates.
	g.GET("/menu/visibility-rules", handleListVisibilityRules)
	g.POST("/menu/visibility-rules", handleCreateVisibilityRule)
	g.DELETE("/menu/visibility-rules/:id", handleDeleteVisibilityRule)
}

// ─── Catalog ──────────────────────────────────────────────────────────────────

// handleListCatalog returns all items in the global catalog.
func handleListCatalog(c echo.Context) error {
	items, err := store.ListCatalogItems()
	if err != nil {
		return serverErr(c, "listCatalog", err)
	}
	return ok(c, map[string]any{"items": items})
}

// handleCreateCatalogItem adds a new item to the catalog (OTP required).
// Auto-inserts into all profile layouts (visible=1 in default, visible=0 in others).
func handleCreateCatalogItem(c echo.Context) error {
	var body struct {
		SlotID           string `json:"slot_id"`
		SlotType         string `json:"slot_type"`
		ItemType         string `json:"item_type"`
		LabelKey         string `json:"label_key"`
		LabelFallback    string `json:"label_fallback"`
		IconFA           string `json:"icon_fa"`
		IconViewBox      string `json:"icon_viewbox"`
		ColorBrand       string `json:"color_brand"`
		ColorNormal      string `json:"color_normal"`
		ColorAttention   string `json:"color_attention"`
		ColorFeatured    string `json:"color_featured"`
		DeviceRefID      string `json:"device_ref_id"`
		DeviceStructName string `json:"device_struct_name"`
		ParentSlotID     string `json:"parent_slot_id"` // default parent for layout
		OTPCode          string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if body.SlotID == "" || body.SlotType == "" || body.ItemType == "" {
		return badRequest(c, "slot_id, slot_type, and item_type are required")
	}

	// Validate OTP.
	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	item := &store.MenuCatalogItem{
		SlotID:           body.SlotID,
		SlotType:         body.SlotType,
		ItemType:         body.ItemType,
		LabelKey:         body.LabelKey,
		LabelFallback:    body.LabelFallback,
		IconFA:           body.IconFA,
		IconViewBox:      body.IconViewBox,
		ColorBrand:       body.ColorBrand,
		ColorNormal:      body.ColorNormal,
		ColorAttention:   body.ColorAttention,
		ColorFeatured:    body.ColorFeatured,
		DeviceRefID:      body.DeviceRefID,
		DeviceStructName: body.DeviceStructName,
	}

	if err := store.InsertCatalogItem(item, body.ParentSlotID); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "slot_id already exists")
		}
		return serverErr(c, "createCatalogItem", err)
	}

	return c.JSON(http.StatusCreated, envelope(map[string]any{"slot_id": body.SlotID}))
}

// handleDeleteCatalogItem removes an unlocked item from the catalog (OTP required).
func handleDeleteCatalogItem(c echo.Context) error {
	slotID := c.Param("slot_id")

	var body struct {
		OTPCode string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteCatalogItem(slotID); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		if err == store.ErrConflict {
			return conflict(c, "cannot delete locked system item")
		}
		return serverErr(c, "deleteCatalogItem", err)
	}

	return ok(c, map[string]any{"deleted": slotID})
}

// ─── Profiles ─────────────────────────────────────────────────────────────────

// handleListMenuProfiles returns all menu profiles.
func handleListMenuProfiles(c echo.Context) error {
	profiles, err := store.ListMenuProfiles()
	if err != nil {
		return serverErr(c, "listMenuProfiles", err)
	}
	return ok(c, map[string]any{"profiles": profiles})
}

// handleCreateMenuProfile creates a new menu profile (OTP required).
func handleCreateMenuProfile(c echo.Context) error {
	var body struct {
		ProfileID   string `json:"profile_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		CloneFrom   string `json:"clone_from"` // optional: copy layout from this profile
		OTPCode     string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if body.ProfileID == "" || body.Name == "" {
		return badRequest(c, "profile_id and name are required")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	// Generate a unique ID if the admin didn't provide one.
	if body.ProfileID == "" {
		body.ProfileID = cryptoauth.MustNewID()
	}

	if err := store.CreateMenuProfile(body.ProfileID, body.Name, body.Description); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "profile_id already exists")
		}
		return serverErr(c, "createMenuProfile", err)
	}

	// Optionally clone layout from another profile.
	if body.CloneFrom != "" {
		if err := store.CloneMenuProfileLayout(body.CloneFrom, body.ProfileID); err != nil {
			// Profile was created but clone failed — log but don't rollback.
			c.Logger().Errorf("[admin/menuTree] clone from %q failed: %v", body.CloneFrom, err)
		}
	}

	return c.JSON(http.StatusCreated, envelope(map[string]any{"profile_id": body.ProfileID}))
}

// handleUpdateMenuProfile updates name and description of a profile (OTP required).
func handleUpdateMenuProfile(c echo.Context) error {
	profileID := c.Param("id")

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		OTPCode     string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if body.Name == "" {
		return badRequest(c, "name is required")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.UpdateMenuProfile(profileID, body.Name, body.Description); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		return serverErr(c, "updateMenuProfile", err)
	}

	return ok(c, map[string]any{"profile_id": profileID})
}

// handleDeleteMenuProfile removes a non-locked profile (OTP required).
func handleDeleteMenuProfile(c echo.Context) error {
	profileID := c.Param("id")

	var body struct {
		OTPCode string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteMenuProfile(profileID); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		if err == store.ErrConflict {
			return conflict(c, "cannot delete the locked default profile")
		}
		return serverErr(c, "deleteMenuProfile", err)
	}

	return ok(c, map[string]any{"deleted": profileID})
}

// handleActivateMenuProfile sets a profile as the active default (OTP required).
func handleActivateMenuProfile(c echo.Context) error {
	profileID := c.Param("id")

	var body struct {
		OTPCode string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.ActivateMenuProfile(profileID); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		return serverErr(c, "activateMenuProfile", err)
	}

	return ok(c, map[string]any{"active_profile": profileID})
}

// handleCloneMenuProfile copies layout+labels+help from a source profile (OTP required).
func handleCloneMenuProfile(c echo.Context) error {
	targetID := c.Param("id")

	var body struct {
		SourceID string `json:"source_id"`
		OTPCode  string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if body.SourceID == "" {
		return badRequest(c, "source_id is required")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.CloneMenuProfileLayout(body.SourceID, targetID); err != nil {
		return serverErr(c, "cloneMenuProfile", err)
	}

	return ok(c, map[string]any{"cloned_from": body.SourceID, "target": targetID})
}

// ─── Layout ───────────────────────────────────────────────────────────────────

// handleGetLayout returns the full layout tree for a profile.
func handleGetLayout(c echo.Context) error {
	profileID := c.Param("id")

	items, err := store.GetProfileLayout(profileID)
	if err != nil {
		return serverErr(c, "getLayout", err)
	}

	return ok(c, map[string]any{"profile_id": profileID, "items": items})
}

// handleReorderLayout updates parent_id and position of multiple items (OTP required).
func handleReorderLayout(c echo.Context) error {
	profileID := c.Param("id")

	var body struct {
		Items   []store.ReorderLayoutItem `json:"items"`
		OTPCode string                    `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if len(body.Items) == 0 {
		return badRequest(c, "items array is required")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.ReorderLayout(profileID, body.Items); err != nil {
		return serverErr(c, "reorderLayout", err)
	}

	return ok(c, map[string]any{"reordered": len(body.Items)})
}

// handleUpdateLayoutSlot updates visible, custom_label, etc. for one slot (OTP required).
func handleUpdateLayoutSlot(c echo.Context) error {
	profileID := c.Param("id")
	slotID := c.Param("slot_id")

	var body struct {
		Visible          *bool   `json:"visible"`
		CustomLabel      *string `json:"custom_label"`
		CustomIcon       *string `json:"custom_icon"`
		CustomIconVB     *string `json:"custom_icon_viewbox"`
		CustomColorBrand *string `json:"custom_color_brand"`
		OTPCode          string  `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.UpdateLayoutSlot(
		profileID, slotID,
		body.Visible,
		body.CustomLabel, body.CustomIcon, body.CustomIconVB, body.CustomColorBrand,
	); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		return serverErr(c, "updateLayoutSlot", err)
	}

	return ok(c, map[string]any{"updated": slotID})
}

// ─── Layout Labels ────────────────────────────────────────────────────────────

// handleListLayoutLabels returns all locale-specific label overrides for a slot.
func handleListLayoutLabels(c echo.Context) error {
	profileID := c.Param("id")
	slotID := c.Param("slot_id")

	labels, err := store.ListLayoutLabels(profileID, slotID)
	if err != nil {
		return serverErr(c, "listLayoutLabels", err)
	}
	return ok(c, map[string]any{"labels": labels})
}

// handleUpsertLayoutLabel creates or updates a locale-specific label (OTP required).
func handleUpsertLayoutLabel(c echo.Context) error {
	profileID := c.Param("id")
	slotID := c.Param("slot_id")
	locale := c.Param("locale")

	var body struct {
		Label   string `json:"label"`
		OTPCode string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if body.Label == "" {
		return badRequest(c, "label is required")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.UpsertLayoutLabel(profileID, slotID, locale, body.Label); err != nil {
		return serverErr(c, "upsertLayoutLabel", err)
	}

	return ok(c, map[string]any{"profile_id": profileID, "slot_id": slotID, "locale": locale})
}

// handleDeleteLayoutLabel removes a locale-specific label override (OTP required).
func handleDeleteLayoutLabel(c echo.Context) error {
	profileID := c.Param("id")
	slotID := c.Param("slot_id")
	locale := c.Param("locale")

	var body struct {
		OTPCode string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteLayoutLabel(profileID, slotID, locale); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		return serverErr(c, "deleteLayoutLabel", err)
	}

	return ok(c, map[string]any{"deleted": true})
}

// ─── Help ─────────────────────────────────────────────────────────────────────

// handleListHelp returns all help entries for a slot across all profiles and locales.
func handleListHelp(c echo.Context) error {
	slotID := c.Param("slot_id")

	entries, err := store.ListHelpEntries(slotID)
	if err != nil {
		return serverErr(c, "listHelp", err)
	}
	return ok(c, map[string]any{"entries": entries})
}

// handleUpsertHelp creates or replaces a single help-tab row (OTP required).
// Use profile_id="_default" for generic help that applies to all profiles.
//
// `ord` positions the entry within the (slot, profile, locale) bucket of
// ordered tabs:
//   - ord = 0 → primary (unnumbered) tab, equivalent to `readme.md`
//   - ord ≥ 1 → numbered tab, equivalent to `readme.<ord>.md`
//
// Negative or non-numeric `ord` is rejected with 400.
func handleUpsertHelp(c echo.Context) error {
	slotID := c.Param("slot_id")
	profileID := c.Param("profile_id")
	locale := c.Param("locale")
	ordStr := c.Param("ord")

	// "_default" in the URL maps to "" in the database (generic help).
	if profileID == "_default" {
		profileID = ""
	}

	ord, err := strconv.Atoi(ordStr)
	if err != nil || ord < 0 {
		return badRequest(c, "ord must be a non-negative integer")
	}

	var body struct {
		Markdown string `json:"markdown"`
		OTPCode  string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.UpsertHelp(slotID, profileID, locale, ord, body.Markdown); err != nil {
		return serverErr(c, "upsertHelp", err)
	}

	return ok(c, map[string]any{
		"slot_id":    slotID,
		"profile_id": profileID,
		"locale":     locale,
		"ord":        ord,
	})
}

// handleDeleteHelp removes a specific help-tab row (OTP required).
//
// Deleting `ord=0` while numbered tabs exist is allowed (mirrors the
// device-help convention): the numbered tabs remain as orphans of the
// primary, the sort rule in loadResolvedHelp still presents them in
// ascending order, and the renderer continues to work. The admin can
// renumber manually if they want a clean sequence.
func handleDeleteHelp(c echo.Context) error {
	slotID := c.Param("slot_id")
	profileID := c.Param("profile_id")
	locale := c.Param("locale")
	ordStr := c.Param("ord")

	if profileID == "_default" {
		profileID = ""
	}

	ord, err := strconv.Atoi(ordStr)
	if err != nil || ord < 0 {
		return badRequest(c, "ord must be a non-negative integer")
	}

	var body struct {
		OTPCode string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteHelp(slotID, profileID, locale, ord); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		return serverErr(c, "deleteHelp", err)
	}

	return ok(c, map[string]any{"deleted": true})
}

// ─── Section Picker ─────────────────────────────────────────────────────────

// handleSectionPicker returns all public, ready devices and templates from
// official_specialist and admin users. The admin uses this list to select
// which items go inside a branded section (e.g. Sparkfun).
func handleSectionPicker(c echo.Context) error {
	items, err := store.ListSectionPickerItems()
	if err != nil {
		return serverErr(c, "sectionPicker", err)
	}
	if items == nil {
		items = []store.SectionPickerItem{}
	}
	return ok(c, map[string]any{"items": items})
}

// handleSectionChildren returns the slot_ids of devices currently inside
// a section. Used by the picker modal to pre-check items already added.
func handleSectionChildren(c echo.Context) error {
	sectionID := c.Param("section_id")
	if sectionID == "" {
		return badRequest(c, "section_id is required")
	}

	children, err := store.ListSectionChildren(sectionID)
	if err != nil {
		return serverErr(c, "sectionChildren", err)
	}

	// Convert map to slice for JSON.
	slotIDs := make([]string, 0, len(children))
	for id := range children {
		slotIDs = append(slotIDs, id)
	}

	return ok(c, map[string]any{"children": slotIDs})
}

// handleAddSectionItems adds selected devices/templates to a section (OTP required).
// Creates section-scoped category/subcategory hierarchy automatically.
func handleAddSectionItems(c echo.Context) error {
	var body struct {
		SectionSlotID string                    `json:"section_slot_id"`
		Items         []store.SectionPickerItem `json:"items"`
		OTPCode       string                    `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if body.SectionSlotID == "" {
		return badRequest(c, "section_slot_id is required")
	}
	if len(body.Items) == 0 {
		return badRequest(c, "at least one item is required")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.AddItemsToSection(body.SectionSlotID, body.Items); err != nil {
		return serverErr(c, "addSectionItems", err)
	}

	return ok(c, map[string]any{"added": len(body.Items)})
}

// handleRemoveSectionItem removes a device/template from a section and moves
// it back to the global category hierarchy (OTP required).
func handleRemoveSectionItem(c echo.Context) error {
	sectionID := c.Param("section_id")
	slotID := c.Param("slot_id")
	if sectionID == "" || slotID == "" {
		return badRequest(c, "section_id and slot_id are required")
	}

	var body struct {
		OTPCode string `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.RemoveItemFromSection(sectionID, slotID); err != nil {
		return serverErr(c, "removeSectionItem", err)
	}

	return ok(c, map[string]any{"removed": slotID})
}

// ══════════════════════════════════════════════════════════════════════════════
//  Admin user menu prefs — manage a specific user's visibility overrides
// ══════════════════════════════════════════════════════════════════════════════

// handleAdminGetUserPrefs looks up a user by email and returns their menu
// tree (layers 1+2, no layer 3) plus their current hidden items.
//
//	GET /api/control/v1/menu/user-prefs?email=alice@example.com
func handleAdminGetUserPrefs(c echo.Context) error {
	email := strings.TrimSpace(c.QueryParam("email"))
	if email == "" {
		return badRequest(c, "email query parameter is required")
	}

	user, err := store.GetUserByEmail(email)
	if err != nil {
		return c.JSON(http.StatusNotFound, errEnvelope(http.StatusNotFound, "user not found"))
	}

	locale := strings.TrimSpace(c.QueryParam("locale"))
	if locale == "" {
		locale = "en"
	}

	profileID := store.GetUserMenuProfileID(user.ID)

	userGroupIDs, _ := store.GetUserGroupIDs(user.ID)
	countryCode := user.CountryCode

	// Get tree WITHOUT user prefs (pass empty userID for layer 3 skip).
	result, err := store.GetMenuTree(profileID, locale, "", userGroupIDs, countryCode)
	if err != nil {
		return serverErr(c, "adminGetUserPrefs", err)
	}
	if result.Tree == nil {
		result.Tree = []*store.MenuTreeNode{}
	}

	hiddenMap := store.LoadUserHiddenSlots(user.ID)
	hidden := make([]string, 0, len(hiddenMap))
	for slotID := range hiddenMap {
		hidden = append(hidden, slotID)
	}

	return ok(c, map[string]any{
		"user_id":      user.ID,
		"email":        user.Email,
		"username":     user.Username,
		"role":         user.Role,
		"menu_profile": profileID,
		"tree":         result.Tree,
		"hidden":       hidden,
	})
}

// handleAdminSetUserPrefs batch-updates a user's hidden items.
//
//	PUT /api/control/v1/menu/user-prefs
//	Body: { "user_id": "abc", "hidden": ["SysLoop", "SysMul"] }
func handleAdminSetUserPrefs(c echo.Context) error {
	var body struct {
		UserID string   `json:"user_id"`
		Hidden []string `json:"hidden"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	if body.UserID == "" {
		return badRequest(c, "user_id is required")
	}

	newHidden := make(map[string]bool, len(body.Hidden))
	for _, slotID := range body.Hidden {
		slotID = strings.TrimSpace(slotID)
		if slotID != "" {
			newHidden[slotID] = true
		}
	}

	currentHidden := store.LoadUserHiddenSlots(body.UserID)

	for slotID := range newHidden {
		if !currentHidden[slotID] {
			store.SetUserMenuPref(body.UserID, slotID, false)
		}
	}
	for slotID := range currentHidden {
		if !newHidden[slotID] {
			store.SetUserMenuPref(body.UserID, slotID, true)
		}
	}

	return ok(c, map[string]any{"hidden": len(newHidden)})
}

// handleAdminResetUserPrefs clears all visibility overrides for a user.
//
//	DELETE /api/control/v1/menu/user-prefs?user_id=abc
func handleAdminResetUserPrefs(c echo.Context) error {
	userID := strings.TrimSpace(c.QueryParam("user_id"))
	if userID == "" {
		return badRequest(c, "user_id query parameter is required")
	}

	if err := store.ResetUserMenuPrefs(userID); err != nil {
		return serverErr(c, "adminResetUserPrefs", err)
	}

	return ok(c, map[string]any{"reset": true})
}

// handleAdminResetUserPanelPrefs deletes all saved panel column widths for a user.
//
//	DELETE /api/control/v1/menu/user-panel-prefs?user_id=abc123
func handleAdminResetUserPanelPrefs(c echo.Context) error {
	userID := strings.TrimSpace(c.QueryParam("user_id"))
	if userID == "" {
		return badRequest(c, "user_id query parameter is required")
	}

	deleted, err := store.DeleteAllPanelPrefs(userID)
	if err != nil {
		return serverErr(c, "adminResetUserPanelPrefs", err)
	}

	return ok(c, map[string]any{"deleted": deleted})
}

// ══════════════════════════════════════════════════════════════════════════════
//  Visibility rules — restrict menu items to groups/countries/dates
// ══════════════════════════════════════════════════════════════════════════════

// handleListVisibilityRules returns all rules, optionally filtered by slot_id.
//
//	GET /api/control/v1/menu/visibility-rules?slot_id=SysMath
func handleListVisibilityRules(c echo.Context) error {
	slotID := strings.TrimSpace(c.QueryParam("slot_id"))

	rules, err := store.ListVisibilityRules(slotID)
	if err != nil {
		return serverErr(c, "listVisibilityRules", err)
	}
	if rules == nil {
		rules = []*store.VisibilityRule{}
	}

	// Also return catalog items and groups for the UI dropdowns.
	catalog, _ := store.ListCatalogItems()
	if catalog == nil {
		catalog = []*store.MenuCatalogItem{}
	}

	groups, _ := store.ListGroups()
	if groups == nil {
		groups = []*store.UserGroup{}
	}

	return ok(c, map[string]any{
		"rules":   rules,
		"catalog": catalog,
		"groups":  groups,
	})
}

// handleCreateVisibilityRule creates a new rule.
//
//	POST /api/control/v1/menu/visibility-rules
//	Body: { "slot_id": "SysMath", "group_id": "abc", "country_code": "BR",
//	         "valid_from": "2026-01-01", "valid_until": "2026-12-31" }
func handleCreateVisibilityRule(c echo.Context) error {
	var body struct {
		SlotID      string `json:"slot_id"`
		Mode        string `json:"mode"`
		GroupID     string `json:"group_id"`
		CountryCode string `json:"country_code"`
		ValidFrom   string `json:"valid_from"`
		ValidUntil  string `json:"valid_until"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	if strings.TrimSpace(body.SlotID) == "" {
		return badRequest(c, "slot_id is required")
	}
	if body.Mode != "allow" && body.Mode != "deny" {
		body.Mode = "allow"
	}
	if body.GroupID == "" && body.CountryCode == "" && body.ValidFrom == "" && body.ValidUntil == "" {
		return badRequest(c, "at least one filter (group, country, or date range) is required")
	}

	rule := &store.VisibilityRule{
		ID:          cryptoauth.MustNewID(),
		SlotID:      strings.TrimSpace(body.SlotID),
		Mode:        body.Mode,
		GroupID:     strings.TrimSpace(body.GroupID),
		CountryCode: strings.TrimSpace(body.CountryCode),
		ValidFrom:   strings.TrimSpace(body.ValidFrom),
		ValidUntil:  strings.TrimSpace(body.ValidUntil),
	}

	if err := store.CreateVisibilityRule(rule); err != nil {
		return serverErr(c, "createVisibilityRule", err)
	}

	return c.JSON(http.StatusCreated, envelope(map[string]any{"rule": rule}))
}

// handleDeleteVisibilityRule deletes a rule by ID.
//
//	DELETE /api/control/v1/menu/visibility-rules/:id
func handleDeleteVisibilityRule(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return badRequest(c, "rule id is required")
	}

	if err := store.DeleteVisibilityRule(id); err != nil {
		if err == store.ErrNotFound {
			return notFound(c)
		}
		return serverErr(c, "deleteVisibilityRule", err)
	}

	return ok(c, map[string]any{"deleted": true})
}

// ══════════════════════════════════════════════════════════════════════════════
//  Help files — markdown and image assets stored as SQLite blobs
// ══════════════════════════════════════════════════════════════════════════════
//
// Endpoints under /menu/help/:slot_id/files/* manage the per-slot pool
// of help assets — both the ordered markdown tabs (readme.<N>.<lang>.md)
// and the binary images referenced by those tabs (PNG, JPG, SVG, GIF,
// WebP). Mirrors the project_help_files API in
// server/handler/projectapi/help_files.go so the Control Panel's file
// manager can reuse the same client patterns.
//
// Why two separate paths for markdown (menu_help table) and images
// (menu_help_files table) under the same /files/* endpoint?
//
//   - Markdown lives in menu_help because its primary key is
//     (slot_id, profile_id, locale, ord) — three of those four
//     dimensions are URL-structural, not file-content. Encoding them
//     in the filename would be ambiguous (is "kids" a profile name
//     or a directory?).
//   - Images live in menu_help_files because their primary key is
//     (slot_id, path) — a simple filename pool with no profile or
//     locale dimension. Same shape as project_help_files.
//
// The "/files/*" endpoint family operates on menu_help_files only.
// Markdown stays under "/help/:slot_id/:profile_id/:locale/:ord".
// The Control Panel file manager treats them as one logical pool
// for UX purposes but talks to two different endpoints on the wire.
//
// All write endpoints (PUT, POST, DELETE) require OTP confirmation,
// consistent with the other menu-write endpoints. Image uploads used
// to skip OTP under the old FS-backed flow — that was an oversight,
// now harmonised.

// helpFilePathRe enforces the path grammar for menu help files,
// identical to project_help_files:
//
//   - characters: A-Z, a-z, 0-9, dot, underscore, hyphen
//   - up to one level of subdirectory; deeper paths are rejected
//   - no leading slash; no parent-directory traversal
//
// Examples that match: "readme.en.md", "examples/wiring.png"
// Examples that don't: "../etc/passwd", "deep/nest/foo.png",
// "/readme.md", "file with space.md"
var menuHelpFilePathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)?$`)

// menuHelpFileExtMime is the locked-down whitelist of file types the
// menu_help_files store will accept. Image formats only — markdown
// help lives in the menu_help table (keyed by slot+profile+locale+ord),
// not here. The /files/* endpoint family operates exclusively on the
// per-slot image pool.
//
// PDFs are deliberately excluded — admins link to external PDFs from
// inside markdown if they need them.
var menuHelpFileExtMime = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".svg":  "image/svg+xml",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// menuHelpFileBodyHardLimit is the absolute ceiling on a single PUT
// body. Mirrors helpFileBodyHardLimit in project_help_files (8 MB)
// even though the menu has no per-slot quota — the hard limit
// protects the server from a malicious client streaming forever.
const menuHelpFileBodyHardLimit = 8_000_000

// validateMenuHelpFilePath returns nil when the given path is
// acceptable as a menu help-file location, descriptive error
// otherwise. Used by every handler that accepts a path from the
// client, including rename destinations.
//
// Rejects:
//   - empty strings
//   - paths longer than 200 characters (DoS guard for the database)
//   - paths that fail the regex above
//   - explicit ".." segments (defence-in-depth)
//   - paths whose final segment ends with "." (Windows-hostile names)
func validateMenuHelpFilePath(path string) error {
	if path == "" {
		return errors.New("path is required")
	}
	if len(path) > 200 {
		return errors.New("path is too long (max 200 characters)")
	}
	if !menuHelpFilePathRe.MatchString(path) {
		return errors.New("invalid path: only [A-Za-z0-9._-] and at most one '/' allowed")
	}
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

// mimeForMenuHelpExt returns the canonical MIME type for the given
// path. Returns ("", false) when the extension is not in the whitelist;
// the handler turns that into a 415 Unsupported Media Type response.
//
// Important: the MIME type is derived FROM THE PATH on the server,
// never trusted from the client's Content-Type header. A client that
// sends a PNG body with path "evil.md" gets its MIME stamped as
// "text/markdown" and the renderer will then fail to display it as
// markdown because it isn't actually markdown. This is fine — garbage
// in, garbage out, no security implication.
func mimeForMenuHelpExt(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := menuHelpFileExtMime[ext]
	return mime, ok
}

// handleListMenuHelpFiles answers GET /menu/help/:slot_id/files
//
// Returns metadata (no content blobs) for every file in the slot's
// pool. The Control Panel file manager calls this on open to render
// the file tree.
func handleListMenuHelpFiles(c echo.Context) error {
	slotID := c.Param("slot_id")
	if slotID == "" {
		return badRequest(c, "slot_id is required")
	}

	files, err := store.ListMenuHelpFiles(slotID)
	if err != nil {
		return serverErr(c, "listMenuHelpFiles", err)
	}

	return ok(c, map[string]any{"files": files})
}

// handleGetMenuHelpFile answers GET /menu/help/:slot_id/files/*path
//
// Returns the raw bytes with the appropriate Content-Type and an ETag
// derived from updated_at. The browser's If-None-Match handling makes
// repeat reads cheap.
//
// Note: image references inside menu markdown are normally rewritten
// to `data:` URLs at serve time (see loadResolvedHelp), so direct
// fetches via this endpoint are mostly used by the Control Panel
// file manager's preview pane — not by the WASM IDE itself.
func handleGetMenuHelpFile(c echo.Context) error {
	slotID := c.Param("slot_id")
	path := c.Param("*")

	if slotID == "" {
		return badRequest(c, "slot_id is required")
	}
	if err := validateMenuHelpFilePath(path); err != nil {
		return badRequest(c, err.Error())
	}

	hf, err := store.GetMenuHelpFile(slotID, path)
	if errors.Is(err, store.ErrNoMenuHelpFile) {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getMenuHelpFile", err)
	}

	// ETag uses the RFC3339 timestamp compacted. Clients shouldn't
	// parse this — it's an opaque identifier — so the exact format
	// doesn't matter as long as it changes on every write.
	etag := `"` + hf.UpdatedAt.UTC().Format("20060102T150405") + `"`
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "private, must-revalidate")
	return c.Blob(http.StatusOK, hf.MimeType, hf.Content)
}

// handlePutMenuHelpFile answers PUT /menu/help/:slot_id/files/*path (OTP required).
//
// Body is the raw file bytes. Content-Type from the client is ignored;
// the server derives the MIME type from the path extension. OTP is in
// a header (`X-Menu-OTP`) rather than a body field so that PUT bodies
// can be raw bytes without a JSON wrapper — same convention as
// project_help_files.handlePutHelpFile.
func handlePutMenuHelpFile(c echo.Context) error {
	slotID := c.Param("slot_id")
	path := c.Param("*")

	if slotID == "" {
		return badRequest(c, "slot_id is required")
	}
	if err := validateMenuHelpFilePath(path); err != nil {
		return badRequest(c, err.Error())
	}

	mime, mimeOK := mimeForMenuHelpExt(path)
	if !mimeOK {
		return c.JSON(http.StatusUnsupportedMediaType, errEnvelope(
			http.StatusUnsupportedMediaType,
			"unsupported file type: only image types .png, .jpg, .jpeg, .svg, .gif, .webp are allowed",
		))
	}

	// OTP comes via header for raw-body PUTs. The wider menu-tree
	// admin endpoints all share the same OTP machinery
	// (consumeMenuOTP); only the transport differs here.
	if msg := consumeMenuOTP(c, c.Request().Header.Get("X-Menu-OTP")); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	body, err := io.ReadAll(io.LimitReader(c.Request().Body, menuHelpFileBodyHardLimit))
	if err != nil {
		return badRequest(c, "could not read request body")
	}
	if int64(len(body)) >= menuHelpFileBodyHardLimit {
		return c.JSON(http.StatusRequestEntityTooLarge, errEnvelope(
			http.StatusRequestEntityTooLarge,
			fmt.Sprintf("file is too large (max %d bytes)", menuHelpFileBodyHardLimit-1),
		))
	}

	if err := store.SaveMenuHelpFile(slotID, path, mime, body); err != nil {
		return serverErr(c, "saveMenuHelpFile", err)
	}

	return ok(c, map[string]any{
		"path":      path,
		"mimeType":  mime,
		"sizeBytes": int64(len(body)),
	})
}

// handleDeleteMenuHelpFile answers DELETE /menu/help/:slot_id/files/*path (OTP required).
//
// Idempotent: deleting a file that does not exist returns 200 with
// {deleted: false}, NOT 404. Matches the project_help_files semantics
// so a Control Panel UI reacting to a stale view of the pool does not
// surface a noisy error.
func handleDeleteMenuHelpFile(c echo.Context) error {
	slotID := c.Param("slot_id")
	path := c.Param("*")

	if slotID == "" {
		return badRequest(c, "slot_id is required")
	}
	if err := validateMenuHelpFilePath(path); err != nil {
		return badRequest(c, err.Error())
	}

	// OTP via JSON body (DELETE with body is allowed; this matches the
	// existing menu-tree DELETE endpoints' style).
	var body struct {
		OTPCode string `json:"otp_code"`
	}
	_ = c.Bind(&body) // empty body is fine — OTP header fallback below
	otp := body.OTPCode
	if otp == "" {
		otp = c.Request().Header.Get("X-Menu-OTP")
	}
	if msg := consumeMenuOTP(c, otp); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	deleted, err := store.DeleteMenuHelpFile(slotID, path)
	if err != nil {
		return serverErr(c, "deleteMenuHelpFile", err)
	}

	return ok(c, map[string]any{"deleted": deleted})
}

// renameMenuHelpFileBody is the JSON shape accepted by the rename
// endpoint. Separate struct (rather than an anonymous one in the
// handler) so the JSON contract is greppable.
type renameMenuHelpFileBody struct {
	NewPath string `json:"newPath"`
	OTPCode string `json:"otp_code"`
}

// handleRenameMenuHelpFile answers POST /menu/help/:slot_id/files/*path/rename (OTP required).
//
// Path is the old path (captured by Echo's "*" greedy match, which
// includes the trailing "/rename" — stripped here). Body is JSON with
// `newPath` and `otp_code`. Both paths are validated; the MIME type
// for the new path is recomputed (extensions can change in a rename,
// e.g. .png → .svg; if the new extension is not in the whitelist the
// rename 415s).
//
// Mirrors project_help_files.handleRenameHelpFile, including the
// trailing-suffix trick.
func handleRenameMenuHelpFile(c echo.Context) error {
	slotID := c.Param("slot_id")
	oldPath := c.Param("*")
	if slotID == "" {
		return badRequest(c, "slot_id is required")
	}

	// Echo's "*" greedy match captures everything after "/files/",
	// including the trailing "/rename" segment. Strip it here so the
	// downstream path semantics are clean. Same trick as
	// project_help_files.handleRenameHelpFile.
	oldPath = strings.TrimSuffix(oldPath, "/rename")
	if err := validateMenuHelpFilePath(oldPath); err != nil {
		return badRequest(c, "old path: "+err.Error())
	}

	var body renameMenuHelpFileBody
	if err := c.Bind(&body); err != nil {
		return badRequest(c, "invalid JSON body")
	}
	if err := validateMenuHelpFilePath(body.NewPath); err != nil {
		return badRequest(c, "new path: "+err.Error())
	}

	newMime, mimeOK := mimeForMenuHelpExt(body.NewPath)
	if !mimeOK {
		return c.JSON(http.StatusUnsupportedMediaType, errEnvelope(
			http.StatusUnsupportedMediaType,
			"unsupported new file type: only image types .png, .jpg, .jpeg, .svg, .gif, .webp are allowed",
		))
	}

	if msg := consumeMenuOTP(c, body.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.RenameMenuHelpFile(slotID, oldPath, body.NewPath, newMime); err != nil {
		switch {
		case errors.Is(err, store.ErrNoMenuHelpFile):
			return notFound(c)
		case errors.Is(err, store.ErrMenuHelpPathConflict):
			return c.JSON(http.StatusConflict, errEnvelope(http.StatusConflict, "destination path already exists"))
		default:
			return serverErr(c, "renameMenuHelpFile", err)
		}
	}

	return ok(c, map[string]any{
		"oldPath":  oldPath,
		"newPath":  body.NewPath,
		"mimeType": newMime,
	})
}
