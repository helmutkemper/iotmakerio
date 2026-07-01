// server/store/menu_visibility_rules.go — CRUD for menu visibility rules.
//
// Visibility rules are the 2nd layer of the menu visibility system (see
// docs/CLAUDE_MENU_TREE.md §4.2). A rule whitelists a menu item for a
// specific group, country, or date range. If a slot has ANY rules, it is
// visible ONLY to users who match at least one rule. Slots with NO rules
// are visible to everyone (who passes layer 1 — admin layout).
//
// Schema:
//
//	menu_visibility_rules (
//	    id           TEXT PRIMARY KEY,
//	    slot_id      TEXT NOT NULL REFERENCES menu_items(slot_id),
//	    group_id     TEXT REFERENCES user_groups(id),
//	    country_code TEXT,
//	    valid_from   TEXT,     -- ISO date or NULL (no start limit)
//	    valid_until  TEXT,     -- ISO date or NULL (no end limit)
//	    created_at   TEXT NOT NULL
//	)
//
// Examples:
//   - "SysMath" visible only to group "school_kids" → one rule row.
//   - "Sec_sparkfun" visible only in Brazil → country_code = "BR".
//   - "SysLoop" hidden after 2026-06-01 → valid_until = "2026-06-01".
//   - Combining: group + country means the user must match THAT rule's
//     group AND country. Multiple rules are OR'd: match any one = visible.
package store

import (
	"time"
)

// VisibilityRule is one row from menu_visibility_rules.
type VisibilityRule struct {
	ID          string `json:"id"`
	SlotID      string `json:"slot_id"`
	Mode        string `json:"mode"` // "allow" or "deny"
	GroupID     string `json:"group_id,omitempty"`
	GroupName   string `json:"group_name,omitempty"` // resolved via JOIN for display
	CountryCode string `json:"country_code,omitempty"`
	ValidFrom   string `json:"valid_from,omitempty"`
	ValidUntil  string `json:"valid_until,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// ListVisibilityRules returns all rules, optionally filtered by slot_id.
// When slotID is empty, all rules are returned. Results include the
// group name resolved via LEFT JOIN for display in the admin UI.
func ListVisibilityRules(slotID string) ([]*VisibilityRule, error) {
	query := `
		SELECT r.id, r.slot_id, r.mode,
		       COALESCE(r.group_id, ''), COALESCE(g.name, ''),
		       COALESCE(r.country_code, ''),
		       COALESCE(r.valid_from, ''), COALESCE(r.valid_until, ''),
		       r.created_at
		FROM menu_visibility_rules r
		LEFT JOIN user_groups g ON g.id = r.group_id`

	var args []interface{}
	if slotID != "" {
		query += ` WHERE r.slot_id = ?`
		args = append(args, slotID)
	}
	query += ` ORDER BY r.slot_id, r.created_at ASC`

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*VisibilityRule
	for rows.Next() {
		r := &VisibilityRule{}
		if err := rows.Scan(
			&r.ID, &r.SlotID, &r.Mode,
			&r.GroupID, &r.GroupName,
			&r.CountryCode,
			&r.ValidFrom, &r.ValidUntil,
			&r.CreatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// CreateVisibilityRule inserts a new visibility rule.
func CreateVisibilityRule(r *VisibilityRule) error {
	if r.Mode == "" {
		r.Mode = "allow"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_visibility_rules
			(id, slot_id, mode, group_id, country_code, valid_from, valid_until, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.SlotID, r.Mode,
		nullIfEmpty(r.GroupID),
		nullIfEmpty(r.CountryCode),
		nullIfEmpty(r.ValidFrom),
		nullIfEmpty(r.ValidUntil),
		now,
	)
	return err
}

// DeleteVisibilityRule removes a rule by ID.
func DeleteVisibilityRule(id string) error {
	res, err := DB.Exec(`DELETE FROM menu_visibility_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ListSlotsWithRules returns all unique slot_ids that have at least one
// visibility rule. Used by the admin UI to show which items are restricted.
func ListSlotsWithRules() (map[string]int, error) {
	rows, err := DB.Query(`
		SELECT slot_id, COUNT(*) FROM menu_visibility_rules
		GROUP BY slot_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]int{}
	for rows.Next() {
		var slotID string
		var count int
		rows.Scan(&slotID, &count)
		result[slotID] = count
	}
	return result, rows.Err()
}
