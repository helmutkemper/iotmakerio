// server/handler/adminapi/sections.go — Admin CRUD for menu sections.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Every write operation (create, update, delete) on sections, items, and
// visibility rules requires a valid OTP code. The admin requests the code
// via POST /sections/request-otp, receives it by email, and includes it
// in the write request body as "otp_code".
//
// Read-only operations do not require OTP.
//
// Routes (all under the control panel group, with RequireControlToken middleware):
//
//	POST   /sections/request-otp           — request OTP for any menu write
//	GET    /sections                       — list all sections
//	POST   /sections                       — create a section (OTP)
//	GET    /sections/:id                   — get one section with items + visibility
//	PUT    /sections/:id                   — replace all mutable fields (OTP)
//	DELETE /sections/:id                   — delete section + cascade (OTP)
//	PATCH  /sections/:id/position          — reorder (OTP)
//	GET    /sections/:id/items             — list items
//	POST   /sections/:id/items             — add an item (OTP)
//	PATCH  /sections/:id/items/:item_id    — update position or visible (OTP)
//	DELETE /sections/:id/items/:item_id    — remove an item (OTP)
//	GET    /sections/:id/visibility        — list visibility rules
//	POST   /sections/:id/visibility        — add a rule (OTP)
//	DELETE /sections/:id/visibility/:rule  — delete a rule (OTP)
//	GET    /item-picker                    — lightweight project+template list
package adminapi

import (
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/email"
	controlapi "server/handler/controlapi"
	"server/store"
)

// RegisterSections wires all section admin routes onto the given group.
// The group should already have RequireControlToken middleware applied.
func RegisterSections(g *echo.Group) {
	// OTP request — shared by all write operations.
	g.POST("/sections/request-otp", handleRequestMenuOTP)

	// Sections CRUD.
	g.GET("/sections", handleListSections)
	g.POST("/sections", handleCreateSection)
	g.GET("/sections/:id", handleGetSection)
	g.PUT("/sections/:id", handleUpdateSection)
	g.DELETE("/sections/:id", handleDeleteSection)
	g.PATCH("/sections/:id/position", handlePatchSectionPosition)

	// Section items.
	g.GET("/sections/:id/items", handleListSectionItems)
	g.POST("/sections/:id/items", handleAddSectionItem)
	g.PATCH("/sections/:id/items/:item_id", handlePatchSectionItem)
	g.DELETE("/sections/:id/items/:item_id", handleDeleteSectionItem)

	// Section visibility.
	g.GET("/sections/:id/visibility", handleListVisibility)
	g.POST("/sections/:id/visibility", handleAddVisibility)
	g.DELETE("/sections/:id/visibility/:rule_id", handleDeleteVisibility)

	// Item picker — lightweight list for the add-item dropdown.
	g.GET("/item-picker", handleItemPicker)
}

// ─── OTP ──────────────────────────────────────────────────────────────────────

// handleRequestMenuOTP generates a 6-digit code and emails it to the admin.
// The code is valid for 15 minutes and can be used for any single menu write
// operation (create, update, delete on sections, items, or visibility).
func handleRequestMenuOTP(c echo.Context) error {
	caller := controlapi.ControlClaims(c)
	if caller == nil {
		return badRequest(c, "no control claims in context")
	}

	log.Printf("[admin/menuOTP] caller.UserID=%q caller.Role=%q", caller.UserID, caller.Role)

	admin, err := store.GetUserByID(caller.UserID)
	if err != nil {
		log.Printf("[admin/menuOTP] GetUserByID(%q) failed: %v", caller.UserID, err)
		return serverErr(c, "loadAdmin", err)
	}

	code, otpID, err := newOTP()
	if err != nil {
		return serverErr(c, "genOTP", err)
	}

	if err := store.CreateOTP(&store.OTPCode{
		ID:      otpID,
		UserID:  caller.UserID,
		Code:    code,
		Purpose: store.OTPPurposeMenuChange,
	}); err != nil {
		return serverErr(c, "storeOTP", err)
	}

	go email.MenuChangeCode(admin.Email, code)

	return ok(c, map[string]any{"message": "code sent to your registered email"})
}

// consumeMenuOTP validates and atomically consumes an OTP code for a menu
// change operation. Returns a user-facing error message on failure, or ""
// on success.
func consumeMenuOTP(c echo.Context, otpCode string) string {
	if otpCode == "" {
		return "otp_code is required"
	}
	caller := controlapi.ControlClaims(c)
	if err := store.ConsumeOTP(caller.UserID, otpCode, store.OTPPurposeMenuChange); err != nil {
		return "invalid or expired confirmation code"
	}
	return ""
}

// ─── Sections ─────────────────────────────────────────────────────────────────

func handleListSections(c echo.Context) error {
	sections, err := store.ListAllSections()
	if err != nil {
		return serverErr(c, "listSections", err)
	}
	return ok(c, map[string]any{"sections": sections})
}

func handleGetSection(c echo.Context) error {
	s, err := store.GetSection(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getSection", err)
	}
	return ok(c, map[string]any{"section": s})
}

func handleCreateSection(c echo.Context) error {
	var req struct {
		Slug           string `json:"slug"`
		Name           string `json:"name"`
		Position       int    `json:"position"`
		ColorNormal    string `json:"color_normal"`
		ColorAttention string `json:"color_attention"`
		ColorFeatured  string `json:"color_featured"`
		IconFA         string `json:"icon_fa"`
		Active         *bool  `json:"active"`
		OTPCode        string `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Slug == "" || req.Name == "" {
		return badRequest(c, "slug and name are required")
	}

	// Validate OTP before any mutation.
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	s := &store.MenuSection{
		ID:             cryptoauth.MustNewID(),
		Slug:           req.Slug,
		Name:           req.Name,
		Position:       req.Position,
		ColorNormal:    orDefault(req.ColorNormal, "#185FA5"),
		ColorAttention: orDefault(req.ColorAttention, "#C42B2B"),
		ColorFeatured:  orDefault(req.ColorFeatured, "#1D9E75"),
		IconFA:         orDefault(req.IconFA, "gear"),
		Active:         active,
	}

	if err := store.CreateSection(s); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "slug already exists")
		}
		return serverErr(c, "createSection", err)
	}

	created, err := store.GetSection(s.ID)
	if err != nil {
		return serverErr(c, "getCreatedSection", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"section": created}))
}

func handleUpdateSection(c echo.Context) error {
	existing, err := store.GetSection(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getSection", err)
	}

	var req struct {
		Slug           string `json:"slug"`
		Name           string `json:"name"`
		Position       int    `json:"position"`
		ColorNormal    string `json:"color_normal"`
		ColorAttention string `json:"color_attention"`
		ColorFeatured  string `json:"color_featured"`
		IconFA         string `json:"icon_fa"`
		Active         *bool  `json:"active"`
		OTPCode        string `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	// Validate OTP before any mutation.
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	// Apply only provided fields; keep existing values for empty ones.
	// Exception: color fields are ALWAYS applied because empty string means
	// "clear the brand color and use the default". The frontend always sends
	// all three color fields in every request.
	if req.Slug != "" {
		existing.Slug = req.Slug
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Position != 0 {
		existing.Position = req.Position
	}
	// Colors — always applied (empty = use default).
	existing.ColorNormal = req.ColorNormal
	existing.ColorAttention = req.ColorAttention
	existing.ColorFeatured = req.ColorFeatured
	if req.IconFA != "" {
		existing.IconFA = req.IconFA
	}
	if req.Active != nil {
		existing.Active = *req.Active
	}

	if err := store.UpdateSection(existing); err != nil {
		return serverErr(c, "updateSection", err)
	}

	updated, err := store.GetSection(existing.ID)
	if err != nil {
		return serverErr(c, "getUpdatedSection", err)
	}
	return ok(c, map[string]any{"section": updated})
}

func handleDeleteSection(c echo.Context) error {
	var req struct {
		OTPCode string `json:"otp_code"`
	}
	// DELETE with body — Echo handles this via Bind.
	_ = c.Bind(&req)

	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteSection(c.Param("id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteSection", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

func handlePatchSectionPosition(c echo.Context) error {
	existing, err := store.GetSection(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getSection", err)
	}

	var req struct {
		Position int    `json:"position"`
		OTPCode  string `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	existing.Position = req.Position
	if err := store.UpdateSection(existing); err != nil {
		return serverErr(c, "patchPosition", err)
	}
	return ok(c, map[string]any{"position": req.Position})
}

// ─── Section items ────────────────────────────────────────────────────────────

func handleListSectionItems(c echo.Context) error {
	s, err := store.GetSection(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getSection", err)
	}
	return ok(c, map[string]any{"items": s.Items})
}

func handleAddSectionItem(c echo.Context) error {
	sectionID := c.Param("id")
	if _, err := store.GetSection(sectionID); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "getSection", err)
	}

	var req struct {
		ItemType  string `json:"item_type"`
		ItemRefID string `json:"item_ref_id"`
		Position  int    `json:"position"`
		OTPCode   string `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.ItemType != "project" && req.ItemType != "template" && req.ItemType != "device" {
		return badRequest(c, "item_type must be 'project', 'template', or 'device'")
	}
	if req.ItemRefID == "" {
		return badRequest(c, "item_ref_id is required")
	}

	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	item := &store.MenuSectionItem{
		ID:        cryptoauth.MustNewID(),
		SectionID: sectionID,
		ItemType:  req.ItemType,
		ItemRefID: req.ItemRefID,
		Position:  req.Position,
		Visible:   true,
	}
	if err := store.AddSectionItem(item); err != nil {
		return serverErr(c, "addSectionItem", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"item": item}))
}

func handlePatchSectionItem(c echo.Context) error {
	var req struct {
		Position *int   `json:"position"`
		Visible  *bool  `json:"visible"`
		OTPCode  string `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	s, err := store.GetSection(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getSection", err)
	}

	itemID := c.Param("item_id")
	var current *store.MenuSectionItem
	for _, it := range s.Items {
		if it.ID == itemID {
			current = it
			break
		}
	}
	if current == nil {
		return notFound(c)
	}

	position := current.Position
	visible := current.Visible
	if req.Position != nil {
		position = *req.Position
	}
	if req.Visible != nil {
		visible = *req.Visible
	}

	if err := store.UpdateSectionItem(itemID, position, visible); err != nil {
		return serverErr(c, "updateSectionItem", err)
	}
	return ok(c, map[string]any{"position": position, "visible": visible})
}

func handleDeleteSectionItem(c echo.Context) error {
	var req struct {
		OTPCode string `json:"otp_code"`
	}
	_ = c.Bind(&req)

	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteSectionItem(c.Param("item_id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteSectionItem", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

// ─── Section visibility ───────────────────────────────────────────────────────

func handleListVisibility(c echo.Context) error {
	rules, err := store.ListSectionVisibility(c.Param("id"))
	if err != nil {
		return serverErr(c, "listVisibility", err)
	}
	return ok(c, map[string]any{"rules": rules})
}

func handleAddVisibility(c echo.Context) error {
	sectionID := c.Param("id")
	if _, err := store.GetSection(sectionID); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "getSection", err)
	}

	var req struct {
		GroupID     *string `json:"group_id"`
		CountryCode *string `json:"country_code"`
		ValidFrom   *string `json:"valid_from"`
		ValidUntil  *string `json:"valid_until"`
		OTPCode     string  `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	rule := &store.MenuSectionVisibility{
		ID:          cryptoauth.MustNewID(),
		SectionID:   sectionID,
		GroupID:     req.GroupID,
		CountryCode: req.CountryCode,
	}

	if req.ValidFrom != nil {
		t, err := time.Parse(time.RFC3339, *req.ValidFrom)
		if err != nil {
			return badRequest(c, "valid_from must be RFC3339")
		}
		rule.ValidFrom = &t
	}
	if req.ValidUntil != nil {
		t, err := time.Parse(time.RFC3339, *req.ValidUntil)
		if err != nil {
			return badRequest(c, "valid_until must be RFC3339")
		}
		rule.ValidUntil = &t
	}

	if err := store.AddSectionVisibility(rule); err != nil {
		return serverErr(c, "addVisibility", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"rule": rule}))
}

func handleDeleteVisibility(c echo.Context) error {
	var req struct {
		OTPCode string `json:"otp_code"`
	}
	_ = c.Bind(&req)

	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteSectionVisibility(c.Param("rule_id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteVisibility", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

// ─── Item picker ──────────────────────────────────────────────────────────────

func handleItemPicker(c echo.Context) error {
	entries, err := store.ListItemPickerEntries()
	if err != nil {
		return serverErr(c, "itemPicker", err)
	}
	return ok(c, map[string]any{"items": entries})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// newOTP generates a 6-digit code and a random ID for storage.
func newOTP() (code, id string, err error) {
	code, err = cryptoauth.NewOTPCode()
	if err != nil {
		return
	}
	id, err = cryptoauth.NewID()
	return
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func envelope(data map[string]any) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"status": http.StatusCreated, "error": ""},
		"data":     data,
	}
}

func errEnvelope(status int, msg string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	}
}

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK, "error": ""},
		"data":     data,
	})
}

func badRequest(c echo.Context, msg string) error {
	return c.JSON(http.StatusBadRequest, errEnvelope(http.StatusBadRequest, msg))
}

func notFound(c echo.Context) error {
	return c.JSON(http.StatusNotFound, errEnvelope(http.StatusNotFound, "not found"))
}

func conflict(c echo.Context, msg string) error {
	return c.JSON(http.StatusConflict, errEnvelope(http.StatusConflict, msg))
}

func serverErr(c echo.Context, op string, err error) error {
	c.Logger().Errorf("[admin/%s] %v", op, err)
	return c.JSON(http.StatusInternalServerError, errEnvelope(http.StatusInternalServerError, "internal error"))
}
