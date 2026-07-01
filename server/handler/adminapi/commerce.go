// server/handler/adminapi/commerce.go — Admin CRUD for the BOM/store system.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Routes (all under /admin, with RequireAdmin middleware):
//
//	GET    /components                              — list all components
//	POST   /components                              — create a component
//	PUT    /components/:id                          — update a component
//	DELETE /components/:id                          — delete (fails if listings exist)
//	GET    /components/:id/blackboxes               — list black-box associations
//	POST   /components/:id/blackboxes               — add association
//	DELETE /components/:id/blackboxes/:bb_id        — remove association
//
//	GET    /stores                                  — list all stores
//	POST   /stores                                  — create a store
//	PUT    /stores/:id                              — update a store
//	GET    /stores/:id/listings                     — list listings for a store
//	POST   /stores/:id/listings                     — add a listing
//	PATCH  /stores/:id/listings/:listing_id         — update listing fields
//	DELETE /stores/:id/listings/:listing_id         — remove a listing
//
//	GET    /redirect-log                            — analytics query
//	                                                  ?listing_id= &country= &from= &until=
package adminapi

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/store"
)

// RegisterCommerce wires all commerce admin routes.
func RegisterCommerce(g *echo.Group) {
	// Components
	g.GET("/components", handleListComponents)
	g.POST("/components", handleCreateComponent)
	g.PUT("/components/:id", handleUpdateComponent)
	g.DELETE("/components/:id", handleDeleteComponent)
	g.GET("/components/:id/blackboxes", handleListBlackBoxComponents)
	g.POST("/components/:id/blackboxes", handleAddBlackBoxComponent)
	g.DELETE("/components/:id/blackboxes/:bb_id", handleDeleteBlackBoxComponent)

	// Stores + listings
	g.GET("/stores", handleListStores)
	g.POST("/stores", handleCreateStore)
	g.PUT("/stores/:id", handleUpdateStore)
	g.GET("/stores/:id/listings", handleListStoreListings)
	g.POST("/stores/:id/listings", handleAddStoreListing)
	g.PATCH("/stores/:id/listings/:listing_id", handlePatchStoreListing)
	g.DELETE("/stores/:id/listings/:listing_id", handleDeleteStoreListing)

	// Analytics
	g.GET("/redirect-log", handleListRedirectLog)
}

// ─── Components ───────────────────────────────────────────────────────────────

func handleListComponents(c echo.Context) error {
	components, err := store.ListComponents()
	if err != nil {
		return serverErr(c, "listComponents", err)
	}
	return ok(c, map[string]any{"components": components})
}

func handleCreateComponent(c echo.Context) error {
	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		DatasheetURL string `json:"datasheet_url"`
		ImageURL     string `json:"image_url"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name == "" {
		return badRequest(c, "name is required")
	}

	comp := &store.Component{
		ID:           cryptoauth.MustNewID(),
		Name:         req.Name,
		Description:  req.Description,
		DatasheetURL: req.DatasheetURL,
		ImageURL:     req.ImageURL,
	}
	if err := store.CreateComponent(comp); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "component name already exists")
		}
		return serverErr(c, "createComponent", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"component": comp}))
}

func handleUpdateComponent(c echo.Context) error {
	existing, err := store.GetComponent(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getComponent", err)
	}

	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		DatasheetURL string `json:"datasheet_url"`
		ImageURL     string `json:"image_url"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	existing.Description = req.Description
	existing.DatasheetURL = req.DatasheetURL
	existing.ImageURL = req.ImageURL

	if err := store.UpdateComponent(existing); err != nil {
		return serverErr(c, "updateComponent", err)
	}
	return ok(c, map[string]any{"component": existing})
}

func handleDeleteComponent(c echo.Context) error {
	err := store.DeleteComponent(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err == store.ErrConflict {
		return conflict(c, "component has active store listings and cannot be deleted")
	}
	if err != nil {
		return serverErr(c, "deleteComponent", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

func handleListBlackBoxComponents(c echo.Context) error {
	bbs, err := store.ListBlackBoxComponents(c.Param("id"))
	if err != nil {
		return serverErr(c, "listBlackBoxComponents", err)
	}
	return ok(c, map[string]any{"blackboxes": bbs})
}

func handleAddBlackBoxComponent(c echo.Context) error {
	componentID := c.Param("id")

	var req struct {
		BlackBoxName string `json:"blackbox_name"`
		Quantity     int    `json:"quantity"`
		Notes        string `json:"notes"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.BlackBoxName == "" {
		return badRequest(c, "blackbox_name is required")
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	bb := &store.BlackBoxComponent{
		ID:           cryptoauth.MustNewID(),
		BlackBoxName: req.BlackBoxName,
		ComponentID:  componentID,
		Quantity:     req.Quantity,
		Notes:        req.Notes,
	}
	if err := store.AddBlackBoxComponent(bb); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "this black-box is already associated with this component")
		}
		return serverErr(c, "addBlackBoxComponent", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"blackbox": bb}))
}

func handleDeleteBlackBoxComponent(c echo.Context) error {
	if err := store.DeleteBlackBoxComponent(c.Param("bb_id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteBlackBoxComponent", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

// ─── Stores ───────────────────────────────────────────────────────────────────

func handleListStores(c echo.Context) error {
	stores, err := store.ListStores()
	if err != nil {
		return serverErr(c, "listStores", err)
	}
	return ok(c, map[string]any{"stores": stores})
}

func handleCreateStore(c echo.Context) error {
	var req struct {
		Name         string `json:"name"`
		CountryCode  string `json:"country_code"`
		BaseURL      string `json:"base_url"`
		AffiliateTag string `json:"affiliate_tag"`
		Active       *bool  `json:"active"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name == "" || req.CountryCode == "" || req.BaseURL == "" {
		return badRequest(c, "name, country_code, and base_url are required")
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	s := &store.Store{
		ID:           cryptoauth.MustNewID(),
		Name:         req.Name,
		CountryCode:  req.CountryCode,
		BaseURL:      req.BaseURL,
		AffiliateTag: req.AffiliateTag,
		Active:       active,
	}
	if err := store.CreateStore(s); err != nil {
		return serverErr(c, "createStore", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"store": s}))
}

func handleUpdateStore(c echo.Context) error {
	var req struct {
		Name         string `json:"name"`
		CountryCode  string `json:"country_code"`
		BaseURL      string `json:"base_url"`
		AffiliateTag string `json:"affiliate_tag"`
		Active       *bool  `json:"active"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	s := &store.Store{
		ID:           c.Param("id"),
		Name:         req.Name,
		CountryCode:  req.CountryCode,
		BaseURL:      req.BaseURL,
		AffiliateTag: req.AffiliateTag,
		Active:       req.Active == nil || *req.Active,
	}
	if err := store.UpdateStore(s); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "updateStore", err)
	}
	return ok(c, map[string]any{"store": s})
}

// ─── Store listings ───────────────────────────────────────────────────────────

func handleListStoreListings(c echo.Context) error {
	listings, err := store.ListStoreListings(c.Param("id"))
	if err != nil {
		return serverErr(c, "listStoreListings", err)
	}
	return ok(c, map[string]any{"listings": listings})
}

func handleAddStoreListing(c echo.Context) error {
	storeID := c.Param("id")

	var req struct {
		ComponentID string `json:"component_id"`
		ProductURL  string `json:"product_url"`
		PriceHint   string `json:"price_hint"`
		Currency    string `json:"currency"`
		InStock     *bool  `json:"in_stock"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.ComponentID == "" || req.ProductURL == "" {
		return badRequest(c, "component_id and product_url are required")
	}

	inStock := true
	if req.InStock != nil {
		inStock = *req.InStock
	}

	l := &store.StoreListing{
		ID:          cryptoauth.MustNewID(),
		ComponentID: req.ComponentID,
		StoreID:     storeID,
		ProductURL:  req.ProductURL,
		PriceHint:   req.PriceHint,
		Currency:    orDefault(req.Currency, "USD"),
		InStock:     inStock,
	}
	if err := store.AddStoreListing(l); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "this component already has a listing at this store")
		}
		return serverErr(c, "addStoreListing", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"listing": l}))
}

func handlePatchStoreListing(c echo.Context) error {
	var req struct {
		ProductURL string `json:"product_url"`
		PriceHint  string `json:"price_hint"`
		Currency   string `json:"currency"`
		InStock    *bool  `json:"in_stock"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	inStock := true
	if req.InStock != nil {
		inStock = *req.InStock
	}

	l := &store.StoreListing{
		ID:         c.Param("listing_id"),
		ProductURL: req.ProductURL,
		PriceHint:  req.PriceHint,
		Currency:   req.Currency,
		InStock:    inStock,
	}
	if err := store.UpdateStoreListing(l); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "patchStoreListing", err)
	}
	return ok(c, map[string]any{"listing": l})
}

func handleDeleteStoreListing(c echo.Context) error {
	if err := store.DeleteStoreListing(c.Param("listing_id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteStoreListing", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

// ─── Analytics ────────────────────────────────────────────────────────────────

// handleListRedirectLog returns redirect click events with optional filters.
//
// Query params (all optional):
//
//	listing_id — filter by listing
//	country    — filter by ip_country
//	from       — ISO datetime lower bound (inclusive)
//	until      — ISO datetime upper bound (inclusive)
//	limit      — max rows (default 100, max 1000)
func handleListRedirectLog(c echo.Context) error {
	limit := 100
	if raw := c.QueryParam("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}

	logs, err := store.ListRedirectLog(
		c.QueryParam("listing_id"),
		c.QueryParam("country"),
		c.QueryParam("from"),
		c.QueryParam("until"),
		limit,
	)
	if err != nil {
		return serverErr(c, "listRedirectLog", err)
	}
	return ok(c, map[string]any{"log": logs, "count": len(logs)})
}
