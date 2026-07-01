// store/categories.go — CRUD for project_categories and project_subcategories.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Categories and subcategories form the taxonomy used by the IDE component
// menu, the marketplace feed, and branded vendor sections.
//
// Each category and subcategory can have a FontAwesome icon name (icon_fa)
// that is used by the IDE WASM to render a visual icon in the menu sidebar.
// When empty, the IDE falls back to a default icon.
//
// The data is seeded on startup (see db.go → seedCategories) and can be
// managed via the Control Panel admin API.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ─── Categories ───────────────────────────────────────────────────────────────

// ListCategories returns all top-level categories ordered by sort_order.
func ListCategories() ([]*ProjectCategory, error) {
	rows, err := DB.Query(`
		SELECT id, name, sort_order, COALESCE(icon_fa, '')
		FROM project_categories
		ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cats []*ProjectCategory
	for rows.Next() {
		var c ProjectCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.SortOrder, &c.IconFA); err != nil {
			return nil, err
		}
		cats = append(cats, &c)
	}
	return cats, rows.Err()
}

// GetCategoryByID returns the category with the given ID.
func GetCategoryByID(id string) (*ProjectCategory, error) {
	var c ProjectCategory
	err := DB.QueryRow(`
		SELECT id, name, sort_order, COALESCE(icon_fa, '')
		FROM project_categories WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.SortOrder, &c.IconFA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// GetCategoryByName returns the category with the given name (case-sensitive).
func GetCategoryByName(name string) (*ProjectCategory, error) {
	var c ProjectCategory
	err := DB.QueryRow(`
		SELECT id, name, sort_order, COALESCE(icon_fa, '')
		FROM project_categories WHERE name = ?`, name,
	).Scan(&c.ID, &c.Name, &c.SortOrder, &c.IconFA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// CreateCategory inserts a new top-level category.
func CreateCategory(c *ProjectCategory) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO project_categories (id, name, sort_order, icon_fa, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.SortOrder, c.IconFA, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateCategory updates the name, sort_order, and icon of an existing category.
func UpdateCategory(c *ProjectCategory) error {
	res, err := DB.Exec(`
		UPDATE project_categories SET name = ?, sort_order = ?, icon_fa = ? WHERE id = ?`,
		c.Name, c.SortOrder, c.IconFA, c.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCategory removes a category and cascades to its subcategories.
func DeleteCategory(id string) error {
	res, err := DB.Exec(`DELETE FROM project_categories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Subcategories ────────────────────────────────────────────────────────────

// ListAllSubcategories returns every subcategory across all categories.
func ListAllSubcategories() ([]*ProjectSubcategory, error) {
	rows, err := DB.Query(`
		SELECT s.id, s.category_id, s.name, s.sort_order, COALESCE(s.icon_fa, '')
		FROM project_subcategories s
		JOIN project_categories c ON c.id = s.category_id
		ORDER BY c.sort_order ASC, s.sort_order ASC, s.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*ProjectSubcategory
	for rows.Next() {
		var s ProjectSubcategory
		if err := rows.Scan(&s.ID, &s.CategoryID, &s.Name, &s.SortOrder, &s.IconFA); err != nil {
			return nil, err
		}
		subs = append(subs, &s)
	}
	return subs, rows.Err()
}

// ListSubcategoriesByCategoryID returns all subcategories for a given category.
func ListSubcategoriesByCategoryID(categoryID string) ([]*ProjectSubcategory, error) {
	rows, err := DB.Query(`
		SELECT id, category_id, name, sort_order, COALESCE(icon_fa, '')
		FROM project_subcategories
		WHERE category_id = ?
		ORDER BY sort_order ASC, name ASC`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*ProjectSubcategory
	for rows.Next() {
		var s ProjectSubcategory
		if err := rows.Scan(&s.ID, &s.CategoryID, &s.Name, &s.SortOrder, &s.IconFA); err != nil {
			return nil, err
		}
		subs = append(subs, &s)
	}
	return subs, rows.Err()
}

// GetSubcategoryByID returns one subcategory by ID.
func GetSubcategoryByID(id string) (*ProjectSubcategory, error) {
	var s ProjectSubcategory
	err := DB.QueryRow(`
		SELECT id, category_id, name, sort_order, COALESCE(icon_fa, '')
		FROM project_subcategories WHERE id = ?`, id,
	).Scan(&s.ID, &s.CategoryID, &s.Name, &s.SortOrder, &s.IconFA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// GetSubcategoryByNameAndCategoryID returns the subcategory with the given
// name inside the given category.
func GetSubcategoryByNameAndCategoryID(name, categoryID string) (*ProjectSubcategory, error) {
	var s ProjectSubcategory
	err := DB.QueryRow(`
		SELECT id, category_id, name, sort_order, COALESCE(icon_fa, '')
		FROM project_subcategories
		WHERE name = ? AND category_id = ?`, name, categoryID,
	).Scan(&s.ID, &s.CategoryID, &s.Name, &s.SortOrder, &s.IconFA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// CreateSubcategory inserts a new subcategory under a parent category.
func CreateSubcategory(s *ProjectSubcategory) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO project_subcategories (id, category_id, name, sort_order, icon_fa, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, s.CategoryID, s.Name, s.SortOrder, s.IconFA, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateSubcategory updates the name, sort_order, and icon of a subcategory.
func UpdateSubcategory(s *ProjectSubcategory) error {
	res, err := DB.Exec(`
		UPDATE project_subcategories SET name = ?, sort_order = ?, icon_fa = ? WHERE id = ?`,
		s.Name, s.SortOrder, s.IconFA, s.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSubcategory removes a subcategory by ID.
func DeleteSubcategory(id string) error {
	res, err := DB.Exec(`DELETE FROM project_subcategories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
