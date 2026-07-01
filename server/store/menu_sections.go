// server/store/menu_sections.go — DB access for the dynamic menu system.
//
// Covers:
//   - menu_sections        — branded sections with colors and icon
//   - menu_section_items   — projects/templates pinned inside a section
//   - menu_section_visibility — optional group+country+date filters
//   - menu_user_pins       — maker's personal pins
//   - user_groups          — named user segments
//   - user_group_members   — many-to-many users ↔ groups
//
// All read functions used by the WASM API (ListSectionsForUser) perform
// the visibility evaluation server-side in SQL, returning only the sections
// the requesting user is allowed to see.
package store

import (
	"database/sql"
	"time"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// MenuSection is one branded group of items in the IDE dynamic menu.
// Colors map to hexMenu.IconStyle in the WASM via the menuapi handler.
type MenuSection struct {
	ID             string    `json:"id"`
	Slug           string    `json:"slug"`
	Name           string    `json:"name"`
	Position       int       `json:"position"`
	ColorNormal    string    `json:"color_normal"`
	ColorAttention string    `json:"color_attention"`
	ColorFeatured  string    `json:"color_featured"`
	IconFA         string    `json:"icon_fa"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	// Items is populated by ListSectionsForUser — not stored in this table.
	Items []*MenuSectionItem `json:"items,omitempty"`
}

// MenuSectionItem is one project or template pinned inside a section.
type MenuSectionItem struct {
	ID        string    `json:"id"`
	SectionID string    `json:"section_id"`
	ItemType  string    `json:"item_type"`   // "project" | "template" | "device"
	ItemRefID string    `json:"item_ref_id"` // PK in projects, template_packages, or blackboxes
	Position  int       `json:"position"`
	Visible   bool      `json:"visible"`
	CreatedAt time.Time `json:"created_at"`

	// Resolved fields — populated by the handler, not the store.
	// The store returns the raw IDs; the handler resolves names and card images.
	Title     string `json:"title,omitempty"`
	CardImage string `json:"card_image,omitempty"`

	// StructName is the Go struct name (e.g. "RP2040_I2C") for device items.
	// Used by the WASM to match against BlackBoxDefClient.Name and generate
	// the correct Init/method submenu. Empty for project/template items.
	StructName string `json:"struct_name,omitempty"`

	// Subcategory is the human-readable subcategory name resolved from
	// project_subcategories. Used by the WASM to group devices within a
	// section (e.g. "Sensors", "Boards"). Empty when unassigned.
	Subcategory string `json:"subcategory,omitempty"`

	// Category is the human-readable category name resolved from
	// project_categories (e.g. "Comunicação", "Sensores"). Used together
	// with Subcategory to build the hierarchy inside branded sections.
	Category string `json:"category,omitempty"`

	// CategoryIcon is the FontAwesome icon name for the category (e.g.
	// "tower-broadcast"). Resolved from project_categories.icon_fa.
	CategoryIcon string `json:"category_icon,omitempty"`

	// SubcategoryIcon is the FontAwesome icon name for the subcategory.
	// Resolved from project_subcategories.icon_fa.
	SubcategoryIcon string `json:"subcategory_icon,omitempty"`
}

// MenuSectionVisibility is one filter rule that restricts a section to a subset of users.
// A section with no visibility rows is visible to everyone.
type MenuSectionVisibility struct {
	ID          string     `json:"id"`
	SectionID   string     `json:"section_id"`
	GroupID     *string    `json:"group_id"`     // NULL = any group
	CountryCode *string    `json:"country_code"` // NULL = any country
	ValidFrom   *time.Time `json:"valid_from"`   // NULL = no start restriction
	ValidUntil  *time.Time `json:"valid_until"`  // NULL = no end restriction
	CreatedAt   time.Time  `json:"created_at"`
}

// MenuUserPin is one personal pin added by a maker from the feed.
type MenuUserPin struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ItemType  string    `json:"item_type"`
	ItemRefID string    `json:"item_ref_id"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

// UserGroup is a named segment of users.
type UserGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// UserGroupMember is one user's membership in a group.
type UserGroupMember struct {
	UserID  string    `json:"user_id"`
	GroupID string    `json:"group_id"`
	Source  string    `json:"source"` // "admin" | "auto"
	AddedAt time.Time `json:"added_at"`
}

// ─── Menu sections — public API queries ───────────────────────────────────────

// ListSectionsForUser returns active sections visible to the given user,
// with their visible items pre-loaded.
//
// Visibility evaluation:
//   - Sections with no visibility rows → always included.
//   - Sections with at least one visibility row → included if at least one row matches:
//     (group_id IS NULL OR group_id IN user's groups)
//     AND (country_code IS NULL OR country_code = user's country)
//     AND (valid_from IS NULL OR valid_from <= NOW)
//     AND (valid_until IS NULL OR valid_until >= NOW)
//
// userGroupIDs must be the complete set of group IDs the user belongs to
// (an empty slice is valid for users in no groups).
// countryCode is the user's declared country from users.preferred_locale or profile.
func ListSectionsForUser(userGroupIDs []string, countryCode string) ([]*MenuSection, error) {
	// Build the group_id IN (...) clause dynamically.
	// When the user is in no groups, we still want sections that have
	// group_id IS NULL visibility rows (visible to all).
	groupFilter := "NULL" // placeholder that matches nothing in IN()
	groupArgs := []any{}
	if len(userGroupIDs) > 0 {
		placeholders := make([]string, len(userGroupIDs))
		for i, id := range userGroupIDs {
			placeholders[i] = "?"
			groupArgs = append(groupArgs, id)
		}
		groupFilter = joinStrings(placeholders, ",")
	}

	// A section is included when:
	//   a) it has no visibility rows at all, OR
	//   b) at least one visibility row matches the user's context.
	query := `
		SELECT DISTINCT
			ms.id, ms.slug, ms.name, ms.position,
			ms.color_normal, ms.color_attention, ms.color_featured,
			ms.icon_fa, ms.active, ms.created_at, ms.updated_at
		FROM menu_sections ms
		WHERE ms.active = 1
		  AND (
		      NOT EXISTS (
		          SELECT 1 FROM menu_section_visibility WHERE section_id = ms.id
		      )
		      OR EXISTS (
		          SELECT 1 FROM menu_section_visibility v
		          WHERE v.section_id = ms.id
		            AND (v.group_id IS NULL OR v.group_id IN (` + groupFilter + `))
		            AND (v.country_code IS NULL OR v.country_code = ?)
		            AND (v.valid_from IS NULL OR v.valid_from <= datetime('now'))
		            AND (v.valid_until IS NULL OR v.valid_until >= datetime('now'))
		      )
		  )
		ORDER BY ms.position ASC`

	args := append(groupArgs, countryCode)
	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sections := []*MenuSection{}
	for rows.Next() {
		s := &MenuSection{}
		var activeInt int
		var createdStr, updatedStr string
		if err := rows.Scan(
			&s.ID, &s.Slug, &s.Name, &s.Position,
			&s.ColorNormal, &s.ColorAttention, &s.ColorFeatured,
			&s.IconFA, &activeInt, &createdStr, &updatedStr,
		); err != nil {
			return nil, err
		}
		s.Active = activeInt == 1
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		sections = append(sections, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Pre-load visible items for each section.
	for _, s := range sections {
		items, err := listSectionItems(s.ID, true)
		if err != nil {
			return nil, err
		}
		s.Items = items
	}

	return sections, nil
}

// listSectionItems returns items for one section.
// When visibleOnly is true, only items with visible=1 are returned.
func listSectionItems(sectionID string, visibleOnly bool) ([]*MenuSectionItem, error) {
	query := `
		SELECT id, section_id, item_type, item_ref_id, position, visible, created_at
		FROM menu_section_items
		WHERE section_id = ?`
	if visibleOnly {
		query += ` AND visible = 1`
	}
	query += ` ORDER BY position ASC`

	rows, err := DB.Query(query, sectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []*MenuSectionItem{}
	for rows.Next() {
		item := &MenuSectionItem{}
		var visibleInt int
		var createdStr string
		if err := rows.Scan(
			&item.ID, &item.SectionID, &item.ItemType, &item.ItemRefID,
			&item.Position, &visibleInt, &createdStr,
		); err != nil {
			return nil, err
		}
		item.Visible = visibleInt == 1
		item.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		items = append(items, item)
	}
	return items, rows.Err()
}

// ─── Menu sections — admin CRUD ───────────────────────────────────────────────

// ListAllSections returns all sections ordered by position, regardless of active status.
// Used by the admin panel.
func ListAllSections() ([]*MenuSection, error) {
	rows, err := DB.Query(`
		SELECT id, slug, name, position, color_normal, color_attention, color_featured,
		       icon_fa, active, created_at, updated_at
		FROM menu_sections ORDER BY position ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sections := []*MenuSection{}
	for rows.Next() {
		s := &MenuSection{}
		var activeInt int
		var createdStr, updatedStr string
		if err := rows.Scan(
			&s.ID, &s.Slug, &s.Name, &s.Position,
			&s.ColorNormal, &s.ColorAttention, &s.ColorFeatured,
			&s.IconFA, &activeInt, &createdStr, &updatedStr,
		); err != nil {
			return nil, err
		}
		s.Active = activeInt == 1
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		sections = append(sections, s)
	}
	return sections, rows.Err()
}

// GetSection returns one section by ID with all its items (including hidden ones).
func GetSection(id string) (*MenuSection, error) {
	s := &MenuSection{}
	var activeInt int
	var createdStr, updatedStr string
	err := DB.QueryRow(`
		SELECT id, slug, name, position, color_normal, color_attention, color_featured,
		       icon_fa, active, created_at, updated_at
		FROM menu_sections WHERE id = ?`, id).Scan(
		&s.ID, &s.Slug, &s.Name, &s.Position,
		&s.ColorNormal, &s.ColorAttention, &s.ColorFeatured,
		&s.IconFA, &activeInt, &createdStr, &updatedStr,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.Active = activeInt == 1
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	items, err := listSectionItems(id, false)
	if err != nil {
		return nil, err
	}
	s.Items = items
	return s, nil
}

// CreateSection inserts a new menu section.
func CreateSection(s *MenuSection) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_sections
			(id, slug, name, position, color_normal, color_attention, color_featured,
			 icon_fa, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Slug, s.Name, s.Position,
		s.ColorNormal, s.ColorAttention, s.ColorFeatured,
		s.IconFA, boolToInt(s.Active), now, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateSection replaces all mutable fields of a section.
func UpdateSection(s *MenuSection) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := DB.Exec(`
		UPDATE menu_sections SET
			slug            = ?,
			name            = ?,
			position        = ?,
			color_normal    = ?,
			color_attention = ?,
			color_featured  = ?,
			icon_fa         = ?,
			active          = ?,
			updated_at      = ?
		WHERE id = ?`,
		s.Slug, s.Name, s.Position,
		s.ColorNormal, s.ColorAttention, s.ColorFeatured,
		s.IconFA, boolToInt(s.Active), now, s.ID,
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

// DeleteSection removes a section and cascades to its items and visibility rows.
func DeleteSection(id string) error {
	res, err := DB.Exec(`DELETE FROM menu_sections WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Section items — admin CRUD ───────────────────────────────────────────────

// AddSectionItem pins a project or template inside a section.
func AddSectionItem(item *MenuSectionItem) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_section_items
			(id, section_id, item_type, item_ref_id, position, visible, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.SectionID, item.ItemType, item.ItemRefID,
		item.Position, boolToInt(item.Visible), now,
	)
	return err
}

// UpdateSectionItem updates position and/or visible for one item.
func UpdateSectionItem(id string, position int, visible bool) error {
	res, err := DB.Exec(`
		UPDATE menu_section_items SET position = ?, visible = ? WHERE id = ?`,
		position, boolToInt(visible), id,
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

// DeleteSectionItem removes one item from a section.
func DeleteSectionItem(id string) error {
	res, err := DB.Exec(`DELETE FROM menu_section_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Section visibility — admin CRUD ─────────────────────────────────────────

// ListSectionVisibility returns all visibility rules for a section.
func ListSectionVisibility(sectionID string) ([]*MenuSectionVisibility, error) {
	rows, err := DB.Query(`
		SELECT id, section_id, group_id, country_code, valid_from, valid_until, created_at
		FROM menu_section_visibility WHERE section_id = ?`, sectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []*MenuSectionVisibility{}
	for rows.Next() {
		r := &MenuSectionVisibility{}
		var createdStr string
		var vfStr, vuStr *string
		if err := rows.Scan(
			&r.ID, &r.SectionID, &r.GroupID, &r.CountryCode,
			&vfStr, &vuStr, &createdStr,
		); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		if vfStr != nil {
			t, _ := time.Parse(time.RFC3339, *vfStr)
			r.ValidFrom = &t
		}
		if vuStr != nil {
			t, _ := time.Parse(time.RFC3339, *vuStr)
			r.ValidUntil = &t
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// AddSectionVisibility inserts one visibility rule.
func AddSectionVisibility(r *MenuSectionVisibility) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var vf, vu *string
	if r.ValidFrom != nil {
		s := r.ValidFrom.UTC().Format(time.RFC3339)
		vf = &s
	}
	if r.ValidUntil != nil {
		s := r.ValidUntil.UTC().Format(time.RFC3339)
		vu = &s
	}
	_, err := DB.Exec(`
		INSERT INTO menu_section_visibility
			(id, section_id, group_id, country_code, valid_from, valid_until, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.SectionID, r.GroupID, r.CountryCode, vf, vu, now,
	)
	return err
}

// DeleteSectionVisibility removes one visibility rule.
func DeleteSectionVisibility(id string) error {
	res, err := DB.Exec(`DELETE FROM menu_section_visibility WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── User groups — admin CRUD ─────────────────────────────────────────────────

// ListGroups returns all user groups ordered by name.
func ListGroups() ([]*UserGroup, error) {
	rows, err := DB.Query(`
		SELECT id, name, description, created_at FROM user_groups ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []*UserGroup{}
	for rows.Next() {
		g := &UserGroup{}
		var createdStr string
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &createdStr); err != nil {
			return nil, err
		}
		g.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// GetGroup returns one group by ID.
func GetGroup(id string) (*UserGroup, error) {
	g := &UserGroup{}
	var createdStr string
	err := DB.QueryRow(`
		SELECT id, name, description, created_at FROM user_groups WHERE id = ?`, id,
	).Scan(&g.ID, &g.Name, &g.Description, &createdStr)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return g, nil
}

// CreateGroup inserts a new user group.
func CreateGroup(g *UserGroup) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO user_groups (id, name, description, created_at) VALUES (?, ?, ?, ?)`,
		g.ID, g.Name, g.Description, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateGroup updates the name and description of a group.
func UpdateGroup(g *UserGroup) error {
	res, err := DB.Exec(`
		UPDATE user_groups SET name = ?, description = ? WHERE id = ?`,
		g.Name, g.Description, g.ID,
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

// DeleteGroup removes a group and cascades to its memberships.
func DeleteGroup(id string) error {
	res, err := DB.Exec(`DELETE FROM user_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGroupMembers returns all members of a group with their source and date.
func ListGroupMembers(groupID string) ([]*UserGroupMember, error) {
	rows, err := DB.Query(`
		SELECT user_id, group_id, source, added_at
		FROM user_group_members WHERE group_id = ?
		ORDER BY added_at DESC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []*UserGroupMember{}
	for rows.Next() {
		m := &UserGroupMember{}
		var addedStr string
		if err := rows.Scan(&m.UserID, &m.GroupID, &m.Source, &addedStr); err != nil {
			return nil, err
		}
		m.AddedAt, _ = time.Parse(time.RFC3339, addedStr)
		members = append(members, m)
	}
	return members, rows.Err()
}

// GetUserGroupIDs returns the IDs of all groups a user belongs to.
// Returns an empty slice (never nil) when the user is in no groups.
func GetUserGroupIDs(userID string) ([]string, error) {
	rows, err := DB.Query(`
		SELECT group_id FROM user_group_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AddGroupMember adds a user to a group. source must be "admin" or "auto".
// Returns ErrConflict if the membership already exists.
func AddGroupMember(userID, groupID, source string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO user_group_members (user_id, group_id, source, added_at)
		VALUES (?, ?, ?, ?)`,
		userID, groupID, source, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// RemoveGroupMember removes a user from a group.
func RemoveGroupMember(userID, groupID string) error {
	res, err := DB.Exec(`
		DELETE FROM user_group_members WHERE user_id = ? AND group_id = ?`,
		userID, groupID,
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

// ─── User pins ────────────────────────────────────────────────────────────────

// ListUserPins returns all personal pins for a user, ordered by position.
func ListUserPins(userID string) ([]*MenuUserPin, error) {
	rows, err := DB.Query(`
		SELECT id, user_id, item_type, item_ref_id, position, created_at
		FROM menu_user_pins WHERE user_id = ? ORDER BY position ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pins := []*MenuUserPin{}
	for rows.Next() {
		p := &MenuUserPin{}
		var createdStr string
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.ItemType, &p.ItemRefID, &p.Position, &createdStr,
		); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		pins = append(pins, p)
	}
	return pins, rows.Err()
}

// AddUserPin adds a personal pin. Returns ErrConflict if already pinned.
func AddUserPin(p *MenuUserPin) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_user_pins (id, user_id, item_type, item_ref_id, position, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.ItemType, p.ItemRefID, p.Position, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateUserPinPosition reorders one pin.
func UpdateUserPinPosition(id, userID string, position int) error {
	res, err := DB.Exec(`
		UPDATE menu_user_pins SET position = ? WHERE id = ? AND user_id = ?`,
		position, id, userID,
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

// DeleteUserPin removes a personal pin. The userID check prevents a user from
// deleting another user's pins.
func DeleteUserPin(id, userID string) error {
	res, err := DB.Exec(`
		DELETE FROM menu_user_pins WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// joinStrings joins a slice of strings with a separator.
// Used to build dynamic IN (...) clauses safely — the caller provides
// only placeholder strings ("?"), never values.
func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// ─── Item picker — lightweight summaries for the admin section items modal ────

// ItemPickerEntry is a minimal summary used by the admin panel to populate
// the "add item" dropdown in the section items editor.
type ItemPickerEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "project" | "template"
}

// ListItemPickerEntries returns a combined list of all projects and templates
// in a lightweight format suitable for a search/select dropdown.
// Projects are identified by their ID in the projects table; templates by
// their ID in the template_packages table.
func ListItemPickerEntries() ([]ItemPickerEntry, error) {
	var entries []ItemPickerEntry

	// Projects — use card_title when available, fallback to name.
	rows, err := DB.Query(`
		SELECT id, COALESCE(NULLIF(card_title,''), name) FROM projects ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e ItemPickerEntry
		if err := rows.Scan(&e.ID, &e.Name); err != nil {
			return nil, err
		}
		e.Type = "project"
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Templates.
	tRows, err := DB.Query(`
		SELECT id, name FROM template_packages ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer tRows.Close()
	for tRows.Next() {
		var e ItemPickerEntry
		if err := tRows.Scan(&e.ID, &e.Name); err != nil {
			return nil, err
		}
		e.Type = "template"
		entries = append(entries, e)
	}
	if err := tRows.Err(); err != nil {
		return nil, err
	}

	// Devices (blackboxes) — specialist-submitted hardware components.
	// Use display_name_human when available, fallback to display_name.
	dRows, err := DB.Query(`
		SELECT id, COALESCE(NULLIF(display_name_human,''), display_name)
		FROM blackboxes
		WHERE blocked = 0
		ORDER BY display_name ASC`)
	if err != nil {
		return nil, err
	}
	defer dRows.Close()
	for dRows.Next() {
		var e ItemPickerEntry
		if err := dRows.Scan(&e.ID, &e.Name); err != nil {
			return nil, err
		}
		e.Type = "device"
		entries = append(entries, e)
	}
	return entries, dRows.Err()
}
