// server/store/menu_tree_section_picker.go — Section item picker and assignment.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The admin opens a full-screen modal showing all public devices and templates
// from official_specialist and admin users. Checking items adds them to the
// selected section (e.g., Sparkfun) with their category/subcategory hierarchy
// preserved. For example, APDS9960 (Sensors > Optical) becomes
// Sparkfun > Sensors > Optical > APDS9960.
//
// Section-scoped nodes use slot_ids like:
//
//	Sec_sparkfun__Cat_Sensors         — category under section
//	Sec_sparkfun__SubCat_Sensors_I2C  — subcategory under section
//
// The device slot_id (Dev_APDS9960) stays the same — only its parent_id
// in menu_layout changes.
package store

import (
	"log"
	"time"
)

// ─── Picker types ───────────────────────────────────────────────────────────

// SectionPickerItem is one device or template returned by the picker endpoint.
type SectionPickerItem struct {
	BlackboxID      string `json:"blackbox_id"`
	Type            string `json:"type"`        // "device" | "template"
	StructName      string `json:"struct_name"` // Go struct name (devices only)
	DisplayName     string `json:"display_name"`
	OwnerUsername   string `json:"owner_username"`
	CategoryName    string `json:"category_name"`
	SubcategoryName string `json:"subcategory_name"`
	SlotID          string `json:"slot_id"`
}

// ─── Picker query ───────────────────────────────────────────────────────────

// ListSectionPickerItems returns all public, ready, non-blocked devices
// and templates owned by official_specialist or admin users.
func ListSectionPickerItems() ([]SectionPickerItem, error) {
	var items []SectionPickerItem

	// ── Devices ──────────────────────────────────────────────────────────
	rows, err := DB.Query(`
		SELECT
			b.id, b.display_name, COALESCE(b.display_name_human, ''),
			COALESCE(pc.name, ''), COALESCE(ps.name, ''),
			COALESCE(u.username, ''), COALESCE(mi.slot_id, '')
		FROM blackboxes b
		JOIN users u ON u.id = b.user_id
		LEFT JOIN project_categories    pc ON pc.id = b.category_id
		LEFT JOIN project_subcategories ps ON ps.id = b.subcategory_id
		LEFT JOIN menu_items            mi ON mi.device_struct_name = b.display_name
		                                   AND mi.slot_type = 'device'
		WHERE b.status     = 'ready'
		  AND b.blocked    = 0
		  AND b.visibility = 'public'
		  AND u.role       IN ('admin', 'official_specialist')
		ORDER BY COALESCE(pc.name, 'zzz'), COALESCE(ps.name, 'zzz'), b.display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item SectionPickerItem
		var displayNameHuman string
		if err := rows.Scan(
			&item.BlackboxID, &item.StructName, &displayNameHuman,
			&item.CategoryName, &item.SubcategoryName,
			&item.OwnerUsername, &item.SlotID,
		); err != nil {
			return nil, err
		}
		item.Type = "device"
		item.DisplayName = displayNameHuman
		if item.DisplayName == "" {
			item.DisplayName = item.StructName
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// ── Templates ────────────────────────────────────────────────────────
	tRows, err := DB.Query(`
		SELECT
			tp.id, tp.name,
			COALESCE(pc.name, ''), COALESCE(ps.name, ''),
			COALESCE(u.username, '')
		FROM template_packages tp
		JOIN users u ON u.id = tp.user_id
		LEFT JOIN project_categories    pc ON pc.id = tp.category_id
		LEFT JOIN project_subcategories ps ON ps.id = tp.subcategory_id
		WHERE tp.status     = 'ready'
		  AND tp.blocked    = 0
		  AND tp.visibility = 'public'
		  AND u.role        IN ('admin', 'official_specialist')
		ORDER BY COALESCE(pc.name, 'zzz'), COALESCE(ps.name, 'zzz'), tp.name`)
	if err != nil {
		return nil, err
	}
	defer tRows.Close()

	for tRows.Next() {
		var item SectionPickerItem
		if err := tRows.Scan(
			&item.BlackboxID, &item.DisplayName,
			&item.CategoryName, &item.SubcategoryName,
			&item.OwnerUsername,
		); err != nil {
			return nil, err
		}
		item.Type = "template"
		item.SlotID = "Tmpl_" + item.BlackboxID
		items = append(items, item)
	}

	return items, tRows.Err()
}

// ─── Section children ───────────────────────────────────────────────────────

// ListSectionChildren returns the slot_ids of all items currently inside
// a section (at any depth in the default profile layout).
func ListSectionChildren(sectionSlotID string) (map[string]bool, error) {
	children := map[string]bool{}
	rows, err := DB.Query(`
		SELECT slot_id FROM menu_layout
		WHERE profile_id = 'default'
		  AND (parent_id = ? OR parent_id LIKE ?)`,
		sectionSlotID, sectionSlotID+"__%",
	)
	if err != nil {
		return children, err
	}
	defer rows.Close()

	for rows.Next() {
		var slotID string
		rows.Scan(&slotID)
		children[slotID] = true
	}
	return children, rows.Err()
}

// ─── Add items to section ───────────────────────────────────────────────────

// AddItemsToSection adds devices/templates to a section, creating
// section-scoped category/subcategory nodes as needed.
func AddItemsToSection(sectionSlotID string, items []SectionPickerItem) error {
	now := time.Now().UTC().Format(time.RFC3339)

	profiles, err := ListMenuProfiles()
	if err != nil {
		return err
	}

	for _, item := range items {
		var devSlotID string
		switch item.Type {
		case "device":
			devSlotID = "Dev_" + item.StructName
		case "template":
			devSlotID = "Tmpl_" + item.BlackboxID
		default:
			continue
		}

		// Ensure device exists in catalog.
		if item.Type == "device" {
			DB.Exec(`INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_fallback,
				 device_ref_id, device_struct_name, created_at)
				VALUES (?, 'device', 'action', 0, ?, ?, ?, ?)`,
				devSlotID, item.DisplayName,
				nullIfEmpty(item.BlackboxID), item.StructName, now,
			)
		} else {
			DB.Exec(`INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_fallback,
				 device_ref_id, created_at)
				VALUES (?, 'template', 'action', 0, ?, ?, ?)`,
				devSlotID, item.DisplayName, nullIfEmpty(item.BlackboxID), now,
			)
		}

		// Build section-scoped category hierarchy.
		parentSlotID := sectionSlotID

		catName := item.CategoryName
		if catName == "" {
			catName = "Other"
		}
		catSlot := sectionSlotID + "__Cat_" + sanitizeSlotIDSegment(catName)

		DB.Exec(`INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_fallback,
			 icon_fa, icon_viewbox, created_at)
			VALUES (?, 'category', 'submenu', 0, ?, 'folder', '0 0 512 512', ?)`,
			catSlot, catName, now,
		)
		for _, prof := range profiles {
			mp := maxPositionUnder(prof.ProfileID, sectionSlotID)
			vis := 0
			if prof.IsDefault {
				vis = 1
			}
			DB.Exec(`INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
				VALUES (?, ?, ?, ?, ?)`,
				prof.ProfileID, catSlot, sectionSlotID, mp+1, vis,
			)
		}
		parentSlotID = catSlot

		// Subcategory (if applicable).
		if item.SubcategoryName != "" {
			subSlot := sectionSlotID + "__SubCat_" + sanitizeSlotIDSegment(catName) + "_" + sanitizeSlotIDSegment(item.SubcategoryName)
			DB.Exec(`INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_fallback,
				 icon_fa, icon_viewbox, created_at)
				VALUES (?, 'category', 'submenu', 0, ?, 'cubes', '0 0 512 512', ?)`,
				subSlot, item.SubcategoryName, now,
			)
			for _, prof := range profiles {
				mp := maxPositionUnder(prof.ProfileID, catSlot)
				vis := 0
				if prof.IsDefault {
					vis = 1
				}
				DB.Exec(`INSERT OR IGNORE INTO menu_layout
					(profile_id, slot_id, parent_id, position, visible)
					VALUES (?, ?, ?, ?, ?)`,
					prof.ProfileID, subSlot, catSlot, mp+1, vis,
				)
			}
			parentSlotID = subSlot
		}

		// Move device into section.
		for _, prof := range profiles {
			mp := maxPositionUnder(prof.ProfileID, parentSlotID)
			vis := 0
			if prof.IsDefault {
				vis = 1
			}
			res, _ := DB.Exec(`UPDATE menu_layout SET parent_id = ?, position = ?
				WHERE profile_id = ? AND slot_id = ?`,
				parentSlotID, mp+1, prof.ProfileID, devSlotID,
			)
			n, _ := res.RowsAffected()
			if n == 0 {
				DB.Exec(`INSERT OR IGNORE INTO menu_layout
					(profile_id, slot_id, parent_id, position, visible)
					VALUES (?, ?, ?, ?, ?)`,
					prof.ProfileID, devSlotID, parentSlotID, mp+1, vis,
				)
			}
		}

		log.Printf("[section_picker] added %s to %s (parent=%s)", devSlotID, sectionSlotID, parentSlotID)
	}

	return nil
}

// ─── Remove item from section ───────────────────────────────────────────────

// RemoveItemFromSection moves a device or template out of a section and back
// to the global category hierarchy. If the device has category/subcategory
// metadata in the blackboxes table, it is re-inserted under the corresponding
// global Cat_*/SubCat_* nodes. Otherwise it goes under Cat_Other.
//
// After removal, any empty section-scoped category/subcategory nodes are
// cleaned up automatically.
func RemoveItemFromSection(sectionSlotID, devSlotID string) error {
	// ── Step 1: Find the device's original category from blackboxes ──────
	var structName, deviceRefID string
	DB.QueryRow(`
		SELECT COALESCE(device_struct_name,''), COALESCE(device_ref_id,'')
		FROM menu_items WHERE slot_id = ?`, devSlotID,
	).Scan(&structName, &deviceRefID)

	var catID, subcatID string
	if deviceRefID != "" {
		DB.QueryRow(`
			SELECT COALESCE(category_id,''), COALESCE(subcategory_id,'')
			FROM blackboxes WHERE id = ?`, deviceRefID,
		).Scan(&catID, &subcatID)
	}

	// ── Step 2: Delete the device's layout entries (all profiles) ────────
	DB.Exec(`DELETE FROM menu_layout WHERE slot_id = ?`, devSlotID)

	// ── Step 3: Re-insert under global category hierarchy ────────────────
	label := ""
	if deviceRefID != "" {
		DB.QueryRow(`SELECT COALESCE(display_name_human,'') FROM blackboxes WHERE id = ?`,
			deviceRefID).Scan(&label)
		if label == "" {
			DB.QueryRow(`SELECT COALESCE(display_name,'') FROM blackboxes WHERE id = ?`,
				deviceRefID).Scan(&label)
		}
	}
	if label == "" {
		label = devSlotID
	}

	if structName != "" {
		if err := AutoInsertDeviceToMenu(deviceRefID, structName, label, catID, subcatID); err != nil {
			log.Printf("[section_picker] re-insert %s to global: %v", devSlotID, err)
		}
	} else {
		// Template or unknown — just re-create layout entries under root.
		profiles, _ := ListMenuProfiles()
		for _, prof := range profiles {
			mp := maxPositionUnder(prof.ProfileID, "")
			vis := 0
			if prof.IsDefault {
				vis = 1
			}
			DB.Exec(`INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
				VALUES (?, ?, NULL, ?, ?)`,
				prof.ProfileID, devSlotID, mp+1, vis,
			)
		}
	}

	// ── Step 4: Clean empty section-scoped category nodes ────────────────
	cleaned, _ := CleanEmptyCategoryNodes()
	if cleaned > 0 {
		log.Printf("[section_picker] cleaned %d empty category node(s) after removing %s from %s",
			cleaned, devSlotID, sectionSlotID)
	}

	log.Printf("[section_picker] removed %s from %s", devSlotID, sectionSlotID)
	return nil
}
