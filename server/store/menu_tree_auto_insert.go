// server/store/menu_tree_auto_insert.go — Auto-inserts devices, templates,
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// and their category/subcategory hierarchy into the menu tree.
//
// Called by the worker after a device or template is successfully persisted.
// Every function is idempotent (INSERT OR IGNORE) so it is safe to call
// multiple times for the same device — re-parses do not duplicate entries.
//
// The auto-insert creates three levels of menu nodes:
//
//	Cat_<category>               → slot_type="category", item_type="submenu"
//	└── SubCat_<cat>_<subcat>    → slot_type="category", item_type="submenu"
//	    └── Dev_<struct_name>    → slot_type="device",   item_type="action"
//
// Category and subcategory nodes are auto-created if they don't exist yet.
// The device node carries device_ref_id (blackboxes.id) and device_struct_name
// (the Go struct name) so the WASM can match it against loaded BlackBoxDefClient
// entries and build the correct Init/method submenu.
//
// Layout entries are created for every existing menu profile:
//   - default profile (is_default=1): visible=1
//   - all other profiles: visible=0 (admin must explicitly enable)
//
// If a device already exists in the catalog (re-parse of same struct), only
// the label and device_ref_id are updated — position and visibility are untouched.
//
// When a device has no category, it goes under a fallback "Other" category.
// When a device has a category but no subcategory, it goes directly under the
// category node (no SubCat level).
package store

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// AutoInsertDeviceToMenu ensures that a device and its category hierarchy
// exist in the menu tree. Called by the worker after UpsertDevice succeeds.
//
// Parameters:
//   - deviceID:       blackboxes.id (the FK for device_ref_id)
//   - structName:     Go struct name (e.g. "APDS9960") — becomes Dev_<structName>
//   - displayLabel:   human-readable label for the menu (display_name_human or struct label)
//   - categoryID:     project_categories.id (may be empty for uncategorised)
//   - subcategoryID:  project_subcategories.id (may be empty)
//
// This function is idempotent. It uses INSERT OR IGNORE for catalog entries
// and checks for existing layout entries before inserting.
func AutoInsertDeviceToMenu(deviceID, structName, displayLabel, categoryID, subcategoryID string) error {
	if structName == "" {
		return fmt.Errorf("AutoInsertDeviceToMenu: structName is required")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// ── Resolve category and subcategory names from IDs ──────────────────

	catName := ""
	catIcon := ""
	catSlotID := ""

	if categoryID != "" {
		cat, err := GetCategoryByID(categoryID)
		if err == nil && cat != nil {
			catName = cat.Name
			catIcon = cat.IconFA
		} else {
			log.Printf("[menu_auto_insert] category %s not found, using 'Other'", categoryID)
		}
	}
	if catName == "" {
		catName = "Other"
		catIcon = "folder"
	}
	catSlotID = "Cat_" + sanitizeSlotIDSegment(catName)

	subcatName := ""
	subcatIcon := ""
	subcatSlotID := ""

	if subcategoryID != "" {
		subcat, err := GetSubcategoryByID(subcategoryID)
		if err == nil && subcat != nil {
			subcatName = subcat.Name
			subcatIcon = subcat.IconFA
		}
	}
	if subcatName != "" {
		subcatSlotID = "SubCat_" + sanitizeSlotIDSegment(catName) + "_" + sanitizeSlotIDSegment(subcatName)
	}

	devSlotID := "Dev_" + structName

	// If no display label provided, use the struct name as fallback.
	if displayLabel == "" {
		displayLabel = structName
	}

	// ── Fetch all profiles for layout auto-insert ────────────────────────

	profiles, err := ListMenuProfiles()
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}

	// ── Ensure category node exists ─────────────────────────────────────

	if err := ensureCategoryNode(catSlotID, catName, catIcon, now, profiles); err != nil {
		return fmt.Errorf("ensure category %s: %w", catSlotID, err)
	}

	// ── Ensure subcategory node exists (if applicable) ──────────────────

	parentForDevice := catSlotID
	if subcatSlotID != "" {
		if err := ensureSubcategoryNode(subcatSlotID, subcatName, subcatIcon, catSlotID, now, profiles); err != nil {
			return fmt.Errorf("ensure subcategory %s: %w", subcatSlotID, err)
		}
		parentForDevice = subcatSlotID
	}

	// ── Ensure device node exists ───────────────────────────────────────

	if err := ensureDeviceNode(devSlotID, structName, displayLabel, deviceID, parentForDevice, now, profiles); err != nil {
		return fmt.Errorf("ensure device %s: %w", devSlotID, err)
	}

	log.Printf("[menu_auto_insert] device %s → %s/%s (parent=%s)",
		devSlotID, catName, subcatName, parentForDevice)
	return nil
}

// AutoInsertTemplateToMenu ensures that a template exists in the menu tree
// under the appropriate category hierarchy.
//
// Parameters:
//   - templateID:     template_packages.id (the FK for device_ref_id)
//   - displayLabel:   human-readable name for the menu
//   - categoryID:     project_categories.id (may be empty for uncategorised)
//   - subcategoryID:  project_subcategories.id (may be empty)
//
// Same hierarchy and auto-insert rules as AutoInsertDeviceToMenu.
func AutoInsertTemplateToMenu(templateID, displayLabel, categoryID, subcategoryID string) error {
	if templateID == "" {
		return fmt.Errorf("AutoInsertTemplateToMenu: templateID is required")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// ── Resolve category and subcategory names from IDs ──────────────────

	catName := ""
	catIcon := ""
	catSlotID := ""

	if categoryID != "" {
		cat, err := GetCategoryByID(categoryID)
		if err == nil && cat != nil {
			catName = cat.Name
			catIcon = cat.IconFA
		}
	}
	if catName == "" {
		catName = "Other"
		catIcon = "folder"
	}
	catSlotID = "Cat_" + sanitizeSlotIDSegment(catName)

	subcatName := ""
	subcatIcon := ""
	subcatSlotID := ""

	if subcategoryID != "" {
		subcat, err := GetSubcategoryByID(subcategoryID)
		if err == nil && subcat != nil {
			subcatName = subcat.Name
			subcatIcon = subcat.IconFA
		}
	}
	if subcatName != "" {
		subcatSlotID = "SubCat_" + sanitizeSlotIDSegment(catName) + "_" + sanitizeSlotIDSegment(subcatName)
	}

	tmplSlotID := "Tmpl_" + templateID

	if displayLabel == "" {
		displayLabel = tmplSlotID
	}

	// ── Fetch all profiles ──────────────────────────────────────────────

	profiles, err := ListMenuProfiles()
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}

	// ── Ensure hierarchy ────────────────────────────────────────────────

	if err := ensureCategoryNode(catSlotID, catName, catIcon, now, profiles); err != nil {
		return fmt.Errorf("ensure category %s: %w", catSlotID, err)
	}

	parentForTemplate := catSlotID
	if subcatSlotID != "" {
		if err := ensureSubcategoryNode(subcatSlotID, subcatName, subcatIcon, catSlotID, now, profiles); err != nil {
			return fmt.Errorf("ensure subcategory %s: %w", subcatSlotID, err)
		}
		parentForTemplate = subcatSlotID
	}

	// ── Ensure template node ────────────────────────────────────────────

	_, err = DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_fallback,
			 device_ref_id, created_at)
		VALUES (?, 'template', 'action', 0, ?, ?, ?)`,
		tmplSlotID, displayLabel, nullIfEmpty(templateID), now,
	)
	if err != nil {
		return fmt.Errorf("insert template catalog: %w", err)
	}

	// Update label if template was re-published with a new name.
	DB.Exec(`UPDATE menu_items SET label_fallback = ? WHERE slot_id = ? AND label_fallback != ?`,
		displayLabel, tmplSlotID, displayLabel)

	// Insert layout for each profile.
	for _, prof := range profiles {
		maxPos := maxPositionUnder(prof.ProfileID, parentForTemplate)
		visible := 0
		if prof.IsDefault {
			visible = 1
		}
		DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, ?, ?, ?, ?)`,
			prof.ProfileID, tmplSlotID, parentForTemplate, maxPos+1, visible,
		)
	}

	log.Printf("[menu_auto_insert] template %s → %s/%s", tmplSlotID, catName, subcatName)
	return nil
}

// ─── Internal helpers ───────────────────────────────────────────────────────

// ensureCategoryNode creates a Cat_* node in the catalog and layout if it
// doesn't exist yet. Category nodes are root-level submenu items.
func ensureCategoryNode(slotID, name, iconFA, now string, profiles []*MenuProfile) error {
	if iconFA == "" {
		iconFA = "folder"
	}

	_, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES (?, 'category', 'submenu', 0, ?, ?, '0 0 512 512', ?)`,
		slotID, name, iconFA, now,
	)
	if err != nil {
		return err
	}

	// Insert root-level layout entries for each profile.
	for _, prof := range profiles {
		maxPos := maxPositionUnder(prof.ProfileID, "")
		visible := 0
		if prof.IsDefault {
			visible = 1
		}
		DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, ?, NULL, ?, ?)`,
			prof.ProfileID, slotID, maxPos+1, visible,
		)
	}

	return nil
}

// ensureSubcategoryNode creates a SubCat_* node in the catalog and layout
// under the specified parent category node.
func ensureSubcategoryNode(slotID, name, iconFA, parentSlotID, now string, profiles []*MenuProfile) error {
	if iconFA == "" {
		iconFA = "cubes"
	}

	_, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES (?, 'category', 'submenu', 0, ?, ?, '0 0 512 512', ?)`,
		slotID, name, iconFA, now,
	)
	if err != nil {
		return err
	}

	for _, prof := range profiles {
		maxPos := maxPositionUnder(prof.ProfileID, parentSlotID)
		visible := 0
		if prof.IsDefault {
			visible = 1
		}
		DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, ?, ?, ?, ?)`,
			prof.ProfileID, slotID, parentSlotID, maxPos+1, visible,
		)
	}

	return nil
}

// ensureDeviceNode creates a Dev_* node in the catalog and layout under
// the specified parent (category or subcategory).
func ensureDeviceNode(slotID, structName, displayLabel, deviceRefID, parentSlotID, now string, profiles []*MenuProfile) error {
	_, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_fallback,
			 device_ref_id, device_struct_name, created_at)
		VALUES (?, 'device', 'action', 0, ?, ?, ?, ?)`,
		slotID, displayLabel, nullIfEmpty(deviceRefID), structName, now,
	)
	if err != nil {
		return fmt.Errorf("insert device catalog: %w", err)
	}

	// Update label and device_ref_id if re-parsed (struct already exists
	// but the display name or blackbox ID changed).
	DB.Exec(`
		UPDATE menu_items SET
			label_fallback    = ?,
			device_ref_id     = ?,
			device_struct_name = ?
		WHERE slot_id = ?`,
		displayLabel, nullIfEmpty(deviceRefID), structName, slotID,
	)

	// Insert layout entries for each profile.
	for _, prof := range profiles {
		maxPos := maxPositionUnder(prof.ProfileID, parentSlotID)
		visible := 0
		if prof.IsDefault {
			visible = 1
		}
		DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, ?, ?, ?, ?)`,
			prof.ProfileID, slotID, parentSlotID, maxPos+1, visible,
		)
	}

	return nil
}

// maxPositionUnder returns the current maximum position value among children
// of the given parent in the given profile. Returns 0 if no children exist.
// parentSlotID="" means root level (parent_id IS NULL).
func maxPositionUnder(profileID, parentSlotID string) int {
	var maxPos int
	if parentSlotID == "" {
		DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id IS NULL`, profileID,
		).Scan(&maxPos)
	} else {
		DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id = ?`, profileID, parentSlotID,
		).Scan(&maxPos)
	}
	return maxPos
}

// sanitizeSlotIDSegment replaces spaces and special characters in a category
// or subcategory name so it can be used as part of a slot_id.
// Example: "Temp & Humidity" → "Temp_Humidity"
// Example: "I2C/SPI" → "I2C_SPI"
func sanitizeSlotIDSegment(name string) string {
	// Replace common separators with underscore.
	r := strings.NewReplacer(
		" ", "_",
		"/", "_",
		"\\", "_",
		"&", "",
		".", "",
		",", "",
		"(", "",
		")", "",
		"'", "",
		"\"", "",
	)
	s := r.Replace(name)

	// Collapse multiple underscores.
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}

	// Trim leading/trailing underscores.
	s = strings.Trim(s, "_")

	return s
}

// RemoveDeviceFromMenu removes a device node from the catalog and all layouts.
// Called when a device is deleted or unpublished by the specialist.
// Does NOT remove empty category/subcategory parents — an admin cleanup task
// can handle that separately if desired.
func RemoveDeviceFromMenu(structName string) error {
	slotID := "Dev_" + structName
	res, err := DB.Exec(`DELETE FROM menu_items WHERE slot_id = ? AND locked = 0`, slotID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("[menu_auto_insert] removed device %s from menu tree", slotID)
	}
	return nil
}

// RemoveTemplateFromMenu removes a template node from the catalog and all layouts.
func RemoveTemplateFromMenu(templateID string) error {
	slotID := "Tmpl_" + templateID
	res, err := DB.Exec(`DELETE FROM menu_items WHERE slot_id = ? AND locked = 0`, slotID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("[menu_auto_insert] removed template %s from menu tree", slotID)
	}
	return nil
}

// CleanEmptyCategoryNodes removes category and subcategory nodes that have
// no children in any profile's layout. Called periodically or by an admin action.
func CleanEmptyCategoryNodes() (int, error) {
	// Find category/subcategory nodes that have no children in any layout.
	rows, err := DB.Query(`
		SELECT mi.slot_id
		FROM menu_items mi
		WHERE mi.slot_type = 'category'
		  AND mi.locked = 0
		  AND NOT EXISTS (
		      SELECT 1 FROM menu_layout ml
		      WHERE ml.parent_id = mi.slot_id
		  )`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var orphans []string
	for rows.Next() {
		var slotID string
		if err := rows.Scan(&slotID); err != nil {
			continue
		}
		orphans = append(orphans, slotID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, slotID := range orphans {
		DB.Exec(`DELETE FROM menu_items WHERE slot_id = ? AND locked = 0`, slotID)
	}

	if len(orphans) > 0 {
		log.Printf("[menu_auto_insert] cleaned %d empty category nodes", len(orphans))
	}
	return len(orphans), nil
}
