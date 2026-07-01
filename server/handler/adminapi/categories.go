// server/handler/adminapi/categories.go — Admin CRUD for categories and subcategories.
//
// All write operations require the same menu OTP as sections. Read operations
// do not require OTP.
//
// Routes (all under the control panel group):
//
//	GET    /categories                              — list all with subcategories
//	POST   /categories                              — create category (OTP)
//	PUT    /categories/:id                          — update category (OTP)
//	DELETE /categories/:id                          — delete + cascade (OTP)
//	POST   /categories/:id/subcategories            — create subcategory (OTP)
//	PUT    /categories/:id/subcategories/:sub_id    — update subcategory (OTP)
//	DELETE /categories/:id/subcategories/:sub_id    — delete subcategory (OTP)
package adminapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/store"
)

// RegisterCategories wires all category admin routes onto the given group.
func RegisterCategories(g *echo.Group) {
	g.GET("/categories", handleListCategories)
	g.POST("/categories", handleCreateCategory)
	g.PUT("/categories/:id", handleUpdateCategory)
	g.DELETE("/categories/:id", handleDeleteCategory)

	g.POST("/categories/:id/subcategories", handleCreateSubcategory)
	g.PUT("/categories/:id/subcategories/:sub_id", handleUpdateSubcategory)
	g.DELETE("/categories/:id/subcategories/:sub_id", handleDeleteSubcategory)
}

// ─── Categories ───────────────────────────────────────────────────────────────

// handleListCategories returns all categories with their subcategories nested.
func handleListCategories(c echo.Context) error {
	cats, err := store.ListCategories()
	if err != nil {
		return serverErr(c, "listCategories", err)
	}

	// Build response with subcategories nested under each category.
	type catWithSubs struct {
		*store.ProjectCategory
		Subcategories []*store.ProjectSubcategory `json:"subcategories"`
	}
	result := make([]catWithSubs, 0, len(cats))
	for _, cat := range cats {
		subs, err := store.ListSubcategoriesByCategoryID(cat.ID)
		if err != nil {
			return serverErr(c, "listSubcategories", err)
		}
		if subs == nil {
			subs = []*store.ProjectSubcategory{}
		}
		result = append(result, catWithSubs{
			ProjectCategory: cat,
			Subcategories:   subs,
		})
	}

	return ok(c, map[string]any{"categories": result})
}

func handleCreateCategory(c echo.Context) error {
	var req struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
		IconFA    string `json:"iconFa"`
		OTPCode   string `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name == "" {
		return badRequest(c, "name is required")
	}
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	cat := &store.ProjectCategory{
		ID:        cryptoauth.MustNewID(),
		Name:      req.Name,
		SortOrder: req.SortOrder,
		IconFA:    req.IconFA,
	}
	if err := store.CreateCategory(cat); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "category name already exists")
		}
		return serverErr(c, "createCategory", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"category": cat}))
}

func handleUpdateCategory(c echo.Context) error {
	existing, err := store.GetCategoryByID(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getCategory", err)
	}

	var req struct {
		Name      string  `json:"name"`
		SortOrder *int    `json:"sortOrder"`
		IconFA    *string `json:"iconFa"`
		OTPCode   string  `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.SortOrder != nil {
		existing.SortOrder = *req.SortOrder
	}
	if req.IconFA != nil {
		existing.IconFA = *req.IconFA
	}

	if err := store.UpdateCategory(existing); err != nil {
		return serverErr(c, "updateCategory", err)
	}
	return ok(c, map[string]any{"category": existing})
}

func handleDeleteCategory(c echo.Context) error {
	var req struct {
		OTPCode string `json:"otp_code"`
	}
	_ = c.Bind(&req)
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteCategory(c.Param("id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteCategory", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

// ─── Subcategories ────────────────────────────────────────────────────────────

func handleCreateSubcategory(c echo.Context) error {
	categoryID := c.Param("id")
	if _, err := store.GetCategoryByID(categoryID); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "getCategory", err)
	}

	var req struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
		IconFA    string `json:"iconFa"`
		OTPCode   string `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name == "" {
		return badRequest(c, "name is required")
	}
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	sub := &store.ProjectSubcategory{
		ID:         cryptoauth.MustNewID(),
		CategoryID: categoryID,
		Name:       req.Name,
		SortOrder:  req.SortOrder,
		IconFA:     req.IconFA,
	}
	if err := store.CreateSubcategory(sub); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "subcategory name already exists in this category")
		}
		return serverErr(c, "createSubcategory", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"subcategory": sub}))
}

func handleUpdateSubcategory(c echo.Context) error {
	existing, err := store.GetSubcategoryByID(c.Param("sub_id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getSubcategory", err)
	}

	var req struct {
		Name      string  `json:"name"`
		SortOrder *int    `json:"sortOrder"`
		IconFA    *string `json:"iconFa"`
		OTPCode   string  `json:"otp_code"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.SortOrder != nil {
		existing.SortOrder = *req.SortOrder
	}
	if req.IconFA != nil {
		existing.IconFA = *req.IconFA
	}

	if err := store.UpdateSubcategory(existing); err != nil {
		return serverErr(c, "updateSubcategory", err)
	}
	return ok(c, map[string]any{"subcategory": existing})
}

func handleDeleteSubcategory(c echo.Context) error {
	var req struct {
		OTPCode string `json:"otp_code"`
	}
	_ = c.Bind(&req)
	if msg := consumeMenuOTP(c, req.OTPCode); msg != "" {
		return c.JSON(http.StatusUnauthorized, errEnvelope(http.StatusUnauthorized, msg))
	}

	if err := store.DeleteSubcategory(c.Param("sub_id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteSubcategory", err)
	}
	return ok(c, map[string]any{"deleted": true})
}
