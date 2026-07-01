// server/store/menu_tree.go — CRUD functions for the database-driven menu tree.
//
// This file provides all database access for the menu tree system:
//   - Catalog operations (menu_items)
//   - Menu profile operations (menu_profiles) — prefixed with "Menu" to avoid
//     collision with user profile functions in profiles.go
//   - Layout operations (menu_layout, menu_layout_labels)
//   - Help operations (menu_help)
//   - Tree builder (assembles a ready-to-use JSON tree for the WASM)
//   - User prefs (menu_user_prefs) — maker personal visibility
//   - Visibility rules (menu_visibility_rules) — group/country/date filters
//
// The tree builder (GetMenuTree) is the most important function: it joins
// catalog + layout + labels + help, resolves all cascades server-side,
// applies visibility rules and user prefs, and returns a nested structure
// that the WASM consumes directly without any fallback logic.
//
// See docs/CLAUDE_MENU_TREE.md for the full architecture and cascade rules.
package store

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	bbparser "server/codegen/blackbox"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// MenuTreeNode is one node in the resolved menu tree sent to the WASM.
// Label cascade is resolved by the server, but label_key and label_fallback
// are also sent so the WASM can call translate.T for i18n when no custom
// label override is set by the admin.
type MenuTreeNode struct {
	SlotID           string `json:"slot_id"`
	SlotType         string `json:"slot_type"`                    // "system" | "section" | "device" | "category" | "template"
	ItemType         string `json:"item_type"`                    // "submenu" | "action" | "exit"
	Label            string `json:"label"`                        // resolved via cascade
	LabelKey         string `json:"label_key"`                    // translate.T key for WASM i18n
	LabelFallback    string `json:"label_fallback"`               // translate.T english fallback
	HasCustomLabel   bool   `json:"has_custom_label"`             // true = admin override, WASM should use Label directly
	IconFA           string `json:"icon_fa"`                      // resolved icon (FA name or SVG path)
	IconViewBox      string `json:"icon_viewbox"`                 // resolved viewbox
	ColorBrand       string `json:"color_brand,omitempty"`        // resolved brand color (sections only)
	ColorNormal      string `json:"color_normal,omitempty"`       // section PipelineNormal color
	ColorAttention   string `json:"color_attention,omitempty"`    // section PipelineAttention color
	ColorFeatured    string `json:"color_featured,omitempty"`     // section PipelineSelected color
	DeviceStructName string `json:"device_struct_name,omitempty"` // Go struct name for WASM factory match
	DeviceParsedJSON string `json:"device_parsed_json,omitempty"` // raw BlackBoxDef JSON for curated section devices

	// HelpTabs is the ordered list of help tabs resolved for the current
	// (profile, locale) cascade. The slice has one entry per ord row in
	// the menu_help table for the chosen bucket, sorted ascending by ord
	// with ord=0 first. Image references inside each tab's Content have
	// already been rewritten to inline `data:` URLs by bbparser.RewriteImagePaths
	// before the response leaves the server.
	//
	// Empty slice (not null) when the slot has no help in the chosen
	// cascade — keeps the JS-side handling uniform.
	HelpTabs []bbparser.HelpTab `json:"help_tabs"`

	Children []*MenuTreeNode `json:"children"` // nil for leaf nodes
}

// MenuTreeResult is the complete response from GetMenuTree.
type MenuTreeResult struct {
	ProfileID            string          `json:"profile_id"`
	HideUserPrefsOverlay bool            `json:"hide_user_prefs_overlay"`
	Tree                 []*MenuTreeNode `json:"tree"`
}

// MenuCatalogItem is one row from menu_items (the global catalog).
type MenuCatalogItem struct {
	SlotID           string `json:"slot_id"`
	SlotType         string `json:"slot_type"`
	ItemType         string `json:"item_type"`
	Locked           bool   `json:"locked"`
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
	CreatedAt        string `json:"created_at"`
}

// MenuProfile is one row from menu_profiles.
// Named MenuProfile (not Profile) to avoid collision with user profiles.
type MenuProfile struct {
	ProfileID   string `json:"profile_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
	Locked      bool   `json:"locked"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// MenuLayoutItem is one row from menu_layout joined with menu_items.
// Used by the Control Panel to display the full tree of a profile.
type MenuLayoutItem struct {
	// From menu_layout
	ProfileID        string `json:"profile_id"`
	SlotID           string `json:"slot_id"`
	ParentID         string `json:"parent_id"` // "" for root
	Position         int    `json:"position"`
	Visible          bool   `json:"visible"`
	CustomLabel      string `json:"custom_label"`
	CustomIcon       string `json:"custom_icon"`
	CustomIconVB     string `json:"custom_icon_viewbox"`
	CustomColorBrand string `json:"custom_color_brand"`

	// From menu_items (catalog)
	SlotType      string `json:"slot_type"`
	ItemType      string `json:"item_type"`
	Locked        bool   `json:"locked"`
	LabelKey      string `json:"label_key"`
	LabelFallback string `json:"label_fallback"`
	IconFA        string `json:"icon_fa"`
	IconViewBox   string `json:"icon_viewbox"`
	ColorBrand    string `json:"color_brand"`
}

// MenuHelpEntry is one row from menu_help. The `Ord` field positions
// the entry within a (slot, profile, locale) bucket of ordered tabs;
// `Ord = 0` is the primary unnumbered tab (equivalent to `readme.md`
// in the device-help filename grammar), and larger values are numbered
// tabs (`readme.1.md`, `readme.2.md`, …). See db_menu_tree_tables.go
// for the cascade rules.
type MenuHelpEntry struct {
	SlotID    string `json:"slot_id"`
	ProfileID string `json:"profile_id"`
	Locale    string `json:"locale"`
	Ord       int    `json:"ord"`
	Markdown  string `json:"markdown"`
	UpdatedAt string `json:"updated_at"`
}

// ReorderLayoutItem is one item in a batch reorder operation.
type ReorderLayoutItem struct {
	SlotID   string `json:"slot_id"`
	ParentID string `json:"parent_id"` // "" for root
	Position int    `json:"position"`
}

// ─── Tree Builder ─────────────────────────────────────────────────────────────

// GetMenuTree builds the complete resolved menu tree for the WASM.
//
// The tree is filtered through 3 visibility layers (AND logic):
//  1. Admin layout: menu_layout.visible (per profile)
//  2. Visibility rules: menu_visibility_rules (group + country + date)
//  3. User prefs: menu_user_prefs (maker's personal checkbox overrides)
//
// Parameters:
//   - profileID:    which menu profile to use (empty = resolve from user or default)
//   - locale:       user's locale for label/help cascade (empty = "en")
//   - userID:       authenticated user's ID (empty = anonymous)
//   - userGroupIDs: user's group memberships (nil = no groups)
//   - countryCode:  user's country for visibility rules (empty = no country filter)
func GetMenuTree(profileID, locale, userID string, userGroupIDs []string, countryCode string) (*MenuTreeResult, error) {
	// Resolve profile if not specified.
	if profileID == "" {
		if userID != "" {
			// Try user's assigned profile.
			profileID = GetUserMenuProfileID(userID)
		}
		if profileID == "" {
			var err error
			profileID, err = getActiveProfileID()
			if err != nil {
				return nil, fmt.Errorf("no active profile: %w", err)
			}
		}
	}

	if locale == "" {
		locale = "en"
	}

	// Fetch profile metadata for the response.
	var hideOverlay bool
	var hideOverlayInt int
	err := DB.QueryRow(
		`SELECT COALESCE(hide_user_prefs_overlay, 0) FROM menu_profiles WHERE profile_id = ?`,
		profileID,
	).Scan(&hideOverlayInt)
	if err == nil {
		hideOverlay = hideOverlayInt == 1
	}

	// ── Step 1: Fetch all layout items for this profile + catalog data ───

	rows, err := DB.Query(`
		SELECT
			ml.slot_id,
			ml.parent_id,
			ml.position,
			ml.visible,
			COALESCE(ml.custom_label, ''),
			COALESCE(ml.custom_icon, ''),
			COALESCE(ml.custom_icon_viewbox, ''),
			COALESCE(ml.custom_color_brand, ''),
			mi.slot_type,
			mi.item_type,
			COALESCE(mi.label_key, ''),
			COALESCE(mi.label_fallback, ''),
			COALESCE(mi.icon_fa, ''),
			COALESCE(mi.icon_viewbox, ''),
			COALESCE(mi.color_brand, ''),
			COALESCE(mi.color_normal, ''),
			COALESCE(mi.color_attention, ''),
			COALESCE(mi.color_featured, ''),
			COALESCE(mi.device_struct_name, ''),
			COALESCE(mi.device_ref_id, ''),
			COALESCE(bb.parsed_json, '')
		FROM menu_layout ml
		JOIN menu_items mi ON mi.slot_id = ml.slot_id
		LEFT JOIN blackboxes bb ON bb.id = mi.device_ref_id
		                       AND mi.slot_type = 'device'
		WHERE ml.profile_id = ?
		ORDER BY ml.parent_id, ml.position ASC`,
		profileID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type flatNode struct {
		slotID           string
		parentID         string // "" for root
		position         int
		visible          bool
		customLabel      string
		customIcon       string
		customIconVB     string
		customColorBrand string
		slotType         string
		itemType         string
		labelKey         string
		labelFallback    string
		iconFA           string
		iconViewbox      string
		colorBrand       string
		colorNormal      string
		colorAttention   string
		colorFeatured    string
		deviceStructName string
		deviceRefID      string
		deviceParsedJSON string // raw BlackBoxDef JSON from blackboxes table
	}

	var items []flatNode
	for rows.Next() {
		var n flatNode
		var parentID sql.NullString
		var visibleInt int

		err := rows.Scan(
			&n.slotID, &parentID, &n.position, &visibleInt,
			&n.customLabel, &n.customIcon, &n.customIconVB, &n.customColorBrand,
			&n.slotType, &n.itemType,
			&n.labelKey, &n.labelFallback,
			&n.iconFA, &n.iconViewbox, &n.colorBrand,
			&n.colorNormal, &n.colorAttention, &n.colorFeatured,
			&n.deviceStructName,
			&n.deviceRefID, &n.deviceParsedJSON,
		)
		if err != nil {
			return nil, err
		}

		n.visible = visibleInt == 1
		if parentID.Valid {
			n.parentID = parentID.String
		}

		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// ── Step 2: Load visibility rules ────────────────────────────────────

	blockedSlots := loadBlockedSlots(userGroupIDs, countryCode)

	// ── Step 3: Load user prefs ──────────────────────────────────────────

	userHiddenSlots := map[string]bool{}
	if userID != "" {
		userHiddenSlots = LoadUserHiddenSlots(userID)
	}

	// ── Step 4: Pre-fetch locale labels ──────────────────────────────────

	localeLabels := make(map[string]string)
	fallbackLabels := make(map[string]string)
	if err := loadLocaleLabels(profileID, locale, localeLabels, fallbackLabels); err != nil {
		log.Printf("[menu_tree] warning: failed to load locale labels: %v", err)
	}

	// ── Step 5: Pre-fetch help markdown ──────────────────────────────────

	helpMap := make(map[string][]bbparser.HelpTab)
	if err := loadResolvedHelp(profileID, locale, helpMap); err != nil {
		log.Printf("[menu_tree] warning: failed to load help: %v", err)
	}

	// ── Step 6: Build resolved nodes ─────────────────────────────────────

	nodeMap := make(map[string]*MenuTreeNode)
	childrenOf := make(map[string][]*MenuTreeNode) // parentID → children

	for _, item := range items {
		// Layer 1: admin visibility.
		if !item.visible {
			continue
		}

		// Layer 2: visibility rules (group + country + date).
		if blockedSlots[item.slotID] {
			continue
		}

		// Layer 3: user prefs.
		if userHiddenSlots[item.slotID] {
			continue
		}

		node := &MenuTreeNode{
			SlotID:           item.slotID,
			SlotType:         item.slotType,
			ItemType:         item.itemType,
			LabelKey:         item.labelKey,
			LabelFallback:    item.labelFallback,
			DeviceStructName: item.deviceStructName,
			DeviceParsedJSON: item.deviceParsedJSON,
		}

		// ── Resolve label (cascade §4.1) ─────────────────────────────
		if l, ok := localeLabels[item.slotID]; ok && l != "" {
			node.Label = l
			node.HasCustomLabel = true
		} else if l, ok := fallbackLabels[item.slotID]; ok && l != "" {
			node.Label = l
			node.HasCustomLabel = true
		} else if item.customLabel != "" {
			node.Label = item.customLabel
			node.HasCustomLabel = true
		} else if item.labelFallback != "" {
			node.Label = item.labelFallback
			node.HasCustomLabel = false
		} else {
			node.Label = item.slotID
			node.HasCustomLabel = false
		}

		// ── Resolve icon (cascade §4.3) ──────────────────────────────
		if item.customIcon != "" {
			node.IconFA = item.customIcon
			if item.customIconVB != "" {
				node.IconViewBox = item.customIconVB
			} else {
				node.IconViewBox = item.iconViewbox
			}
		} else {
			node.IconFA = item.iconFA
			node.IconViewBox = item.iconViewbox
		}

		// ── Resolve brand color (cascade §4.4) ───────────────────────
		if item.customColorBrand != "" {
			node.ColorBrand = item.customColorBrand
		} else {
			node.ColorBrand = item.colorBrand
		}

		// ── Section brand colors (3-color pipeline) ──────────────────
		node.ColorNormal = item.colorNormal
		node.ColorAttention = item.colorAttention
		node.ColorFeatured = item.colorFeatured

		// ── Resolve help tabs (cascade §4.2) ─────────────────────────
		// loadResolvedHelp returns nil for slots with no entries; we
		// normalise to an empty (non-nil) slice so the JSON contract
		// is always an array, never null. The WASM iterates
		// node.HelpTabs and an array (empty or otherwise) is uniform.
		if tabs := helpMap[item.slotID]; tabs != nil {
			node.HelpTabs = tabs
		} else {
			node.HelpTabs = []bbparser.HelpTab{}
		}

		nodeMap[item.slotID] = node
		childrenOf[item.parentID] = append(childrenOf[item.parentID], node)
	}

	// Assemble tree: attach children to parents.
	for slotID, node := range nodeMap {
		if kids, ok := childrenOf[slotID]; ok {
			node.Children = kids
		}
	}

	// Root nodes are those with parentID="" (stored as key "" in childrenOf).
	roots := childrenOf[""]

	return &MenuTreeResult{
		ProfileID:            profileID,
		HideUserPrefsOverlay: hideOverlay,
		Tree:                 roots,
	}, nil
}

// ─── Tree builder helpers ───────────────────────────────────────────────────

// getActiveProfileID returns the profile_id of the profile with is_default=1.
func getActiveProfileID() (string, error) {
	var id string
	err := DB.QueryRow(`SELECT profile_id FROM menu_profiles WHERE is_default = 1 LIMIT 1`).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetUserMenuProfileID returns the menu_profile_id for a user.
// Returns "" if the user has no assignment (use default profile).
func GetUserMenuProfileID(userID string) string {
	var profileID sql.NullString
	err := DB.QueryRow(
		`SELECT menu_profile_id FROM users WHERE id = ?`, userID,
	).Scan(&profileID)
	if err != nil || !profileID.Valid {
		return ""
	}
	return profileID.String
}

// SetUserMenuProfileID updates the user's menu profile assignment.
// Pass "" to revert to the default profile.
func SetUserMenuProfileID(userID, profileID string) error {
	var val interface{}
	if profileID != "" {
		val = profileID
	}
	_, err := DB.Exec(
		`UPDATE users SET menu_profile_id = ? WHERE id = ?`, val, userID,
	)
	return err
}

// loadLocaleLabels fetches all locale-specific labels for a profile.
// Populates two maps: one for the requested locale and one for English fallback.
func loadLocaleLabels(profileID, locale string, localeMap, fallbackMap map[string]string) error {
	rows, err := DB.Query(`
		SELECT slot_id, locale, label
		FROM menu_layout_labels
		WHERE profile_id = ? AND locale IN (?, 'en')`,
		profileID, locale,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var slotID, loc, label string
		if err := rows.Scan(&slotID, &loc, &label); err != nil {
			return err
		}
		if loc == locale {
			localeMap[slotID] = label
		} else {
			fallbackMap[slotID] = label
		}
	}
	return rows.Err()
}

// loadResolvedHelp resolves the help cascade for every item in the given profile.
//
// For each slot, this function picks ONE (profile_id, locale) bucket per
// the cascade priority below, then materialises every `ord` row from
// that bucket as an ordered slice of bbparser.HelpTab — the same shape
// devices use for their built-in help. The WASM iterates that slice
// and renders a tab bar (len > 1) or a single page (len == 1).
//
// Image references in each tab's content are rewritten transparently
// before the slice is built. The admin writes `![alt](./diagram.png)`
// in the editor and the server replaces it with the inline base64
// `data:` URL of the image stored in menu_help_files for the same
// slot, using bbparser.RewriteImagePaths (the same function devices
// use). Authors never see the URL form; renames/moves of the image
// pool require no edit to the markdown.
//
// Cascade priority (highest wins; first match per slot picks the bucket):
//
//	6: profile-specific + exact locale match           (e.g. profile="kids", locale="pt-br")
//	5: profile-specific + regional variant             (profile="kids", locale="pt" → "pt-br")
//	4: profile-specific + English fallback             (profile="kids", locale="en")
//	3: generic         + exact locale match            (profile="", locale="pt-br")
//	2: generic         + regional variant              (profile="", locale="pt" → "pt-br")
//	1: generic         + English fallback              (profile="", locale="en")
//
// Within the chosen bucket, every row becomes one HelpTab; rows are
// sorted ascending by `ord` with `ord=0` always first (mirrors the
// devicehelp.go assembleTabs rule). The Title for each tab is derived
// from the first "# heading" of the markdown, truncated per
// config.HelpTabTitleMaxLen at a word boundary.
func loadResolvedHelp(profileID, locale string, helpMap map[string][]bbparser.HelpTab) error {
	// The query matches:
	//   - Exact locale (e.g., "pt")
	//   - Regional variants (e.g., "pt-br", "pt-pt" when locale is "pt")
	//   - English fallback ("en")
	// This allows the admin to save help as "pt-br" and the user with
	// locale "pt" (normalized from "pt-BR") still sees it.
	localePrefix := locale + "-"
	rows, err := DB.Query(`
		SELECT slot_id, profile_id, locale, ord, markdown
		FROM menu_help
		WHERE profile_id IN (?, '')
		  AND (locale = ? OR locale LIKE ? OR locale = 'en')
		ORDER BY slot_id, ord`,
		profileID, locale, localePrefix+"%",
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Each slot gets a "chosen bucket" — a (profile_id, locale) pair
	// that wins the cascade — plus the ordered list of markdown strings
	// from that bucket. We discover the winning bucket on the fly and
	// then keep appending tabs to it; rows from losing buckets are
	// discarded as they arrive.
	type bucket struct {
		priority int    // 1..6 per the cascade above
		profID   string // remembered to compare against late-arriving rows
		loc      string
		tabs     []string // ordered by `ord` thanks to the ORDER BY in the query
	}
	best := make(map[string]*bucket)

	for rows.Next() {
		var (
			slotID, profID, loc, md string
			ord                     int
		)
		if err := rows.Scan(&slotID, &profID, &loc, &ord, &md); err != nil {
			return err
		}

		// Compute this row's priority. Same six-tier ladder used by
		// the pre-`ord` version of this function; only the
		// per-tab accumulation is new.
		p := 0
		isExact := loc == locale
		isVariant := !isExact && strings.HasPrefix(loc, localePrefix)
		isEN := loc == "en"

		switch {
		case profID == profileID && isExact:
			p = 6
		case profID == profileID && isVariant:
			p = 5
		case profID == profileID && isEN:
			p = 4
		case profID == "" && isExact:
			p = 3
		case profID == "" && isVariant:
			p = 2
		case profID == "" && isEN:
			p = 1
		}
		if p == 0 {
			// Neither the cascade nor an explicit override applies;
			// row is irrelevant.
			continue
		}

		existing, ok := best[slotID]
		switch {
		case !ok:
			// First row we see for this slot — it wins by default.
			best[slotID] = &bucket{
				priority: p, profID: profID, loc: loc,
				tabs: []string{md},
			}
		case p > existing.priority:
			// A higher-priority bucket appeared — replace the
			// previous bucket entirely. Any tabs accumulated for a
			// losing bucket are discarded.
			best[slotID] = &bucket{
				priority: p, profID: profID, loc: loc,
				tabs: []string{md},
			}
		case p == existing.priority && profID == existing.profID && loc == existing.loc:
			// Same winning bucket — this is another tab (higher
			// `ord`) of the same (profile, locale). Append.
			existing.tabs = append(existing.tabs, md)
		default:
			// p < existing.priority, OR equal priority but a
			// different bucket (rare, can happen when two language
			// variants both qualify as "regional"). Skip.
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Load every image referenced by markdown across the entire menu.
	// One query for the whole catalogue is acceptable here because
	// menu help is admin-only with no per-slot quota, and the typical
	// envelope is ~100 slots × a handful of small images. If this ever
	// becomes a bottleneck, optimisation is straightforward: scan the
	// markdown text for `![…](…)` references first, then fetch images
	// only for the slots that contain references. The
	// LoadAllMenuHelpImageURLs surface stays the same; only the call
	// site changes.
	//
	// A failure here is non-fatal: log and continue with empty image
	// maps. The markdown still renders, just without the embedded
	// images — better than 500-ing the whole menu tree.
	imageURLs, imgErr := LoadAllMenuHelpImageURLs()
	if imgErr != nil {
		log.Printf("[menu_tree] warning: failed to load help images: %v", imgErr)
		imageURLs = map[string]map[string]string{}
	}

	// Materialise the chosen buckets into ordered HelpTab slices.
	// RewriteImagePaths runs per-tab so each tab's image references are
	// resolved against the same per-slot image pool. Tab Order is the
	// row's `ord` (already in slice order thanks to the ORDER BY in the
	// query), and Title is derived from the first "# heading" of the
	// markdown via firstHeading() — mirrors devicehelp.assembleTabs so
	// the WASM tab bar shows identical labels for menu help and device
	// help.
	for slotID, b := range best {
		if len(b.tabs) == 0 {
			continue
		}
		slotImages := imageURLs[slotID]
		tabs := make([]bbparser.HelpTab, 0, len(b.tabs))
		for i, raw := range b.tabs {
			content := bbparser.RewriteImagePaths(raw, slotImages)
			tabs = append(tabs, bbparser.HelpTab{
				// Order tracks the position in the ordered bucket.
				// We use i (0,1,2,…) rather than the raw `ord`
				// column because gaps in `ord` (e.g. someone
				// deleted ord=1 leaving 0,2) shouldn't render as
				// "Tab 0, Tab 2" — devicehelp treats Order as
				// a sort hint, not an absolute identifier.
				Order:   i,
				Title:   firstHeadingTruncated(raw),
				Content: content,
			})
		}
		helpMap[slotID] = tabs
	}
	return nil
}

// firstHeadingTruncated extracts the first "# heading" line of the
// markdown and truncates it to 24 runes at a word boundary. Mirrors
// truncateTitle() in server/codegen/blackbox/devicehelp.go so menu-tab
// titles match device-tab titles in style.
//
// Returns "" when the markdown has no "# heading" — the WASM client
// treats this as a sentinel and substitutes a localised "title not
// found" message so the missing heading is surfaced to the author.
func firstHeadingTruncated(md string) string {
	if md == "" {
		return ""
	}
	// Find the first "# " line (skipping leading whitespace).
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "# ") {
			continue
		}
		raw := strings.TrimSpace(t[2:])
		return truncateMenuTabTitle(raw)
	}
	return ""
}

// truncateMenuTabTitle caps a tab title at 24 runes, breaking at the
// last space before the limit. Mirrors truncateTitle() in
// devicehelp.go. A single-word over-long title gets a hard cut +
// ellipsis.
//
// 24 is the historical default that devicehelp.assembleTabs uses
// (config.HelpTabTitleMaxLen) — keeping it hardcoded here avoids
// pulling server/config into the store layer. If config ever needs
// to govern this, the constant moves out, not the function.
func truncateMenuTabTitle(s string) string {
	const max = bbparser.HelpTabTitleMaxLen
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	cut := max
	for cut > 0 && runes[cut-1] != ' ' {
		cut--
	}
	if cut == 0 {
		cut = max
	}
	return strings.TrimRight(string(runes[:cut]), " ") + "..."
}

// loadBlockedSlots returns a set of slot_ids that should be hidden from the
// user based on menu_visibility_rules. A slot is blocked when it HAS rules
// but NONE of them match the user's groups and country.
func loadBlockedSlots(userGroupIDs []string, countryCode string) map[string]bool {
	blocked := map[string]bool{}

	// ── Step 1: Load all rules with their mode ──────────────────────────
	type rule struct {
		slotID      string
		mode        string // "allow" or "deny"
		groupID     string
		countryCode string
		validFrom   string
		validUntil  string
	}

	ruleRows, err := DB.Query(`
		SELECT slot_id, mode,
		       COALESCE(group_id, ''), COALESCE(country_code, ''),
		       COALESCE(valid_from, ''), COALESCE(valid_until, '')
		FROM menu_visibility_rules`)
	if err != nil {
		return blocked
	}
	defer ruleRows.Close()

	var rules []rule
	for ruleRows.Next() {
		var r rule
		ruleRows.Scan(&r.slotID, &r.mode, &r.groupID, &r.countryCode, &r.validFrom, &r.validUntil)
		rules = append(rules, r)
	}
	if len(rules) == 0 {
		return blocked
	}

	// ── Step 2: Build user context for matching ─────────────────────────
	groupSet := map[string]bool{}
	for _, gid := range userGroupIDs {
		groupSet[gid] = true
	}

	// matchesUser returns true if a rule's filters match the current user.
	matchesUser := func(r rule) bool {
		// Group filter: empty = matches all; non-empty = must be in user's groups.
		if r.groupID != "" && !groupSet[r.groupID] {
			return false
		}
		// Country filter: empty = matches all; non-empty = must match user's country.
		if r.countryCode != "" && r.countryCode != countryCode {
			return false
		}
		// Date filter: valid_from/valid_until checked via string comparison (ISO dates).
		now := time.Now().UTC().Format("2006-01-02")
		if r.validFrom != "" && now < r.validFrom {
			return false
		}
		if r.validUntil != "" && now > r.validUntil {
			return false
		}
		return true
	}

	// ── Step 3: Group rules by slot and evaluate ────────────────────────
	// For each slot:
	//   - If it has ALLOW rules: blocked unless at least one allow rule matches.
	//   - If it has DENY rules: blocked if any deny rule matches.
	//   - Deny takes priority over allow.

	type slotState struct {
		hasAllow   bool
		allowMatch bool
		denyMatch  bool
	}
	slots := map[string]*slotState{}

	for _, r := range rules {
		s, ok := slots[r.slotID]
		if !ok {
			s = &slotState{}
			slots[r.slotID] = s
		}

		matches := matchesUser(r)

		switch r.mode {
		case "deny":
			if matches {
				s.denyMatch = true
			}
		default: // "allow"
			s.hasAllow = true
			if matches {
				s.allowMatch = true
			}
		}
	}

	// ── Step 4: Compute blocked set ─────────────────────────────────────
	for slotID, s := range slots {
		// Deny always wins — if any deny rule matches, the slot is blocked.
		if s.denyMatch {
			blocked[slotID] = true
			continue
		}
		// Allow rules: if the slot has allow rules but none matched, blocked.
		if s.hasAllow && !s.allowMatch {
			blocked[slotID] = true
		}
	}

	return blocked
}

// LoadUserHiddenSlots returns the set of slot_ids that the user has explicitly
// hidden via the preferences overlay (menu_user_prefs.visible = 0).
// Exported for the editor settings handler; also used internally by GetMenuTree.
func LoadUserHiddenSlots(userID string) map[string]bool {
	hidden := map[string]bool{}

	rows, err := DB.Query(
		`SELECT slot_id FROM menu_user_prefs WHERE user_id = ? AND visible = 0`,
		userID,
	)
	if err != nil {
		return hidden
	}
	defer rows.Close()

	for rows.Next() {
		var slotID string
		rows.Scan(&slotID)
		hidden[slotID] = true
	}
	return hidden
}

// ─── User Prefs CRUD ────────────────────────────────────────────────────────

// SetUserMenuPref sets a maker's visibility preference for a menu item.
// visible=false hides the item; visible=true restores admin default.
// When restoring (visible=true), the row is deleted to keep the table sparse.
func SetUserMenuPref(userID, slotID string, visible bool) error {
	if visible {
		// Restore default — remove the override row.
		_, err := DB.Exec(
			`DELETE FROM menu_user_prefs WHERE user_id = ? AND slot_id = ?`,
			userID, slotID,
		)
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_user_prefs (user_id, slot_id, visible, updated_at)
		VALUES (?, ?, 0, ?)
		ON CONFLICT(user_id, slot_id)
		DO UPDATE SET visible = 0, updated_at = excluded.updated_at`,
		userID, slotID, now,
	)
	return err
}

// ListUserMenuPrefs returns all visibility overrides for a user.
// Used by the preferences overlay to populate the checkbox list.
func ListUserMenuPrefs(userID string) (map[string]bool, error) {
	prefs := map[string]bool{}

	rows, err := DB.Query(
		`SELECT slot_id, visible FROM menu_user_prefs WHERE user_id = ?`,
		userID,
	)
	if err != nil {
		return prefs, err
	}
	defer rows.Close()

	for rows.Next() {
		var slotID string
		var visibleInt int
		if err := rows.Scan(&slotID, &visibleInt); err != nil {
			continue
		}
		prefs[slotID] = visibleInt == 1
	}
	return prefs, rows.Err()
}

// ResetUserMenuPrefs deletes all visibility overrides for a user,
// restoring the admin-defined defaults.
func ResetUserMenuPrefs(userID string) error {
	_, err := DB.Exec(`DELETE FROM menu_user_prefs WHERE user_id = ?`, userID)
	return err
}

// ─── Catalog CRUD ─────────────────────────────────────────────────────────────

// ListCatalogItems returns all items in the catalog, ordered by slot_type then slot_id.
func ListCatalogItems() ([]*MenuCatalogItem, error) {
	rows, err := DB.Query(`
		SELECT slot_id, slot_type, item_type, locked,
			   COALESCE(label_key,''), COALESCE(label_fallback,''),
			   COALESCE(icon_fa,''), COALESCE(icon_viewbox,''),
			   COALESCE(color_brand,''),
			   COALESCE(color_normal,''), COALESCE(color_attention,''), COALESCE(color_featured,''),
			   COALESCE(device_ref_id,''), COALESCE(device_struct_name,''),
			   created_at
		FROM menu_items
		ORDER BY
			CASE slot_type WHEN 'system' THEN 0 WHEN 'section' THEN 1 WHEN 'category' THEN 2 ELSE 3 END,
			slot_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*MenuCatalogItem
	for rows.Next() {
		var item MenuCatalogItem
		var lockedInt int
		if err := rows.Scan(
			&item.SlotID, &item.SlotType, &item.ItemType, &lockedInt,
			&item.LabelKey, &item.LabelFallback,
			&item.IconFA, &item.IconViewBox,
			&item.ColorBrand,
			&item.ColorNormal, &item.ColorAttention, &item.ColorFeatured,
			&item.DeviceRefID, &item.DeviceStructName,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Locked = lockedInt == 1
		items = append(items, &item)
	}
	return items, rows.Err()
}

// InsertCatalogItem adds a new item to the catalog and auto-inserts it into
// all existing profile layouts. In the default profile it's visible=1;
// in all other profiles it's visible=0.
//
// parentSlotID is the default parent for the layout (e.g., "SysMath" for a
// new math operation). If empty, the item is placed at root level.
func InsertCatalogItem(item *MenuCatalogItem, parentSlotID string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Insert into catalog.
	_, err := DB.Exec(`
		INSERT INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, color_brand,
			 color_normal, color_attention, color_featured,
			 device_ref_id, device_struct_name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.SlotID, item.SlotType, item.ItemType, boolToInt(item.Locked),
		nullIfEmpty(item.LabelKey), nullIfEmpty(item.LabelFallback),
		nullIfEmpty(item.IconFA), nullIfEmpty(item.IconViewBox),
		nullIfEmpty(item.ColorBrand),
		nullIfEmpty(item.ColorNormal), nullIfEmpty(item.ColorAttention), nullIfEmpty(item.ColorFeatured),
		nullIfEmpty(item.DeviceRefID), nullIfEmpty(item.DeviceStructName),
		now,
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}

	// Auto-insert into all existing profiles.
	profiles, err := ListMenuProfiles()
	if err != nil {
		return fmt.Errorf("auto-insert profiles: %w", err)
	}

	for _, prof := range profiles {
		// Find the max position under the target parent in this profile.
		var maxPos int
		var parentID interface{}
		if parentSlotID == "" {
			parentID = nil
			DB.QueryRow(`
				SELECT COALESCE(MAX(position), 0) FROM menu_layout
				WHERE profile_id = ? AND parent_id IS NULL`, prof.ProfileID).Scan(&maxPos)
		} else {
			parentID = parentSlotID
			DB.QueryRow(`
				SELECT COALESCE(MAX(position), 0) FROM menu_layout
				WHERE profile_id = ? AND parent_id = ?`, prof.ProfileID, parentSlotID).Scan(&maxPos)
		}

		visible := 0
		if prof.IsDefault {
			visible = 1
		}

		DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, ?, ?, ?, ?)`,
			prof.ProfileID, item.SlotID, parentID, maxPos+1, visible,
		)
	}

	return nil
}

// DeleteCatalogItem removes an unlocked item from the catalog and all layouts.
// Returns ErrNotFound if the item doesn't exist, ErrConflict if it's locked.
func DeleteCatalogItem(slotID string) error {
	// Check if locked.
	var locked int
	err := DB.QueryRow(`SELECT locked FROM menu_items WHERE slot_id = ?`, slotID).Scan(&locked)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if locked == 1 {
		return ErrConflict
	}

	// CASCADE deletes handle menu_layout, menu_layout_labels, and menu_help.
	res, err := DB.Exec(`DELETE FROM menu_items WHERE slot_id = ? AND locked = 0`, slotID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Menu Profile CRUD ───────────────────────────────────────────────────────
//
// All functions are prefixed with "Menu" to avoid collision with the user
// profile functions in profiles.go (CreateProfile, UpdateProfile, etc.).

// ListMenuProfiles returns all menu profiles ordered by is_default DESC, name ASC.
func ListMenuProfiles() ([]*MenuProfile, error) {
	rows, err := DB.Query(`
		SELECT profile_id, name, description, is_default, locked, created_at, updated_at
		FROM menu_profiles
		ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []*MenuProfile
	for rows.Next() {
		var p MenuProfile
		var isDefault, locked int
		if err := rows.Scan(
			&p.ProfileID, &p.Name, &p.Description,
			&isDefault, &locked, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.IsDefault = isDefault == 1
		p.Locked = locked == 1
		profiles = append(profiles, &p)
	}
	return profiles, rows.Err()
}

// GetMenuProfile returns a single menu profile by ID.
func GetMenuProfile(profileID string) (*MenuProfile, error) {
	var p MenuProfile
	var isDefault, locked int
	err := DB.QueryRow(`
		SELECT profile_id, name, description, is_default, locked, created_at, updated_at
		FROM menu_profiles WHERE profile_id = ?`, profileID,
	).Scan(&p.ProfileID, &p.Name, &p.Description, &isDefault, &locked, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.IsDefault = isDefault == 1
	p.Locked = locked == 1
	return &p, nil
}

// CreateMenuProfile creates a new menu profile with is_default=0 and locked=0.
// The profile starts with an empty layout — use CloneMenuProfileLayout to copy
// from another profile.
func CreateMenuProfile(profileID, name, description string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_profiles
			(profile_id, name, description, is_default, locked, created_at, updated_at)
		VALUES (?, ?, ?, 0, 0, ?, ?)`,
		profileID, name, description, now, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateMenuProfile updates the name and description of a menu profile.
func UpdateMenuProfile(profileID, name, description string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := DB.Exec(`
		UPDATE menu_profiles SET name = ?, description = ?, updated_at = ?
		WHERE profile_id = ?`,
		name, description, now, profileID,
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

// DeleteMenuProfile removes a menu profile and all its layout data (CASCADE).
// Returns ErrConflict if the profile is locked (the "default" profile).
func DeleteMenuProfile(profileID string) error {
	var locked int
	err := DB.QueryRow(`SELECT locked FROM menu_profiles WHERE profile_id = ?`, profileID).Scan(&locked)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if locked == 1 {
		return ErrConflict
	}

	res, err := DB.Exec(`DELETE FROM menu_profiles WHERE profile_id = ? AND locked = 0`, profileID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActivateMenuProfile sets the given menu profile as the active default and
// deactivates all other profiles (only one is_default=1 at a time).
func ActivateMenuProfile(profileID string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Verify the profile exists.
	var exists int
	if err := DB.QueryRow(`SELECT 1 FROM menu_profiles WHERE profile_id = ?`, profileID).Scan(&exists); err != nil {
		return ErrNotFound
	}

	// Deactivate all, then activate the target.
	if _, err := DB.Exec(`UPDATE menu_profiles SET is_default = 0, updated_at = ?`, now); err != nil {
		return err
	}
	_, err := DB.Exec(`UPDATE menu_profiles SET is_default = 1, updated_at = ? WHERE profile_id = ?`, now, profileID)
	return err
}

// CloneMenuProfileLayout copies the entire layout, labels, and help overrides
// from sourceID into targetID. The target profile must already exist and
// its current layout will be replaced entirely.
func CloneMenuProfileLayout(sourceID, targetID string) error {
	// Verify both profiles exist.
	for _, id := range []string{sourceID, targetID} {
		var exists int
		if err := DB.QueryRow(`SELECT 1 FROM menu_profiles WHERE profile_id = ?`, id).Scan(&exists); err != nil {
			return fmt.Errorf("profile %q not found", id)
		}
	}

	// Clear existing layout for target.
	if _, err := DB.Exec(`DELETE FROM menu_layout WHERE profile_id = ?`, targetID); err != nil {
		return err
	}

	// Copy layout.
	if _, err := DB.Exec(`
		INSERT INTO menu_layout (profile_id, slot_id, parent_id, position, visible,
			custom_label, custom_icon, custom_icon_viewbox, custom_color_brand)
		SELECT ?, slot_id, parent_id, position, visible,
			custom_label, custom_icon, custom_icon_viewbox, custom_color_brand
		FROM menu_layout WHERE profile_id = ?`,
		targetID, sourceID,
	); err != nil {
		return err
	}

	// Copy locale labels.
	if _, err := DB.Exec(`
		INSERT INTO menu_layout_labels (profile_id, slot_id, locale, label)
		SELECT ?, slot_id, locale, label
		FROM menu_layout_labels WHERE profile_id = ?`,
		targetID, sourceID,
	); err != nil {
		return err
	}

	// Copy help overrides (profile-specific help only, not generic).
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := DB.Exec(`
		INSERT INTO menu_help (slot_id, profile_id, locale, markdown, updated_at)
		SELECT slot_id, ?, locale, markdown, ?
		FROM menu_help WHERE profile_id = ?`,
		targetID, now, sourceID,
	); err != nil {
		return err
	}

	return nil
}

// ─── Layout CRUD ──────────────────────────────────────────────────────────────

// GetProfileLayout returns all layout items for a profile, joined with catalog
// data. Used by the Control Panel to display the tree editor.
func GetProfileLayout(profileID string) ([]*MenuLayoutItem, error) {
	rows, err := DB.Query(`
		SELECT
			ml.profile_id, ml.slot_id,
			COALESCE(ml.parent_id, ''),
			ml.position, ml.visible,
			COALESCE(ml.custom_label, ''),
			COALESCE(ml.custom_icon, ''),
			COALESCE(ml.custom_icon_viewbox, ''),
			COALESCE(ml.custom_color_brand, ''),
			mi.slot_type, mi.item_type, mi.locked,
			COALESCE(mi.label_key, ''), COALESCE(mi.label_fallback, ''),
			COALESCE(mi.icon_fa, ''), COALESCE(mi.icon_viewbox, '')
		FROM menu_layout ml
		JOIN menu_items mi ON mi.slot_id = ml.slot_id
		WHERE ml.profile_id = ?
		ORDER BY ml.parent_id, ml.position ASC`,
		profileID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*MenuLayoutItem
	for rows.Next() {
		var item MenuLayoutItem
		var visibleInt, lockedInt int
		if err := rows.Scan(
			&item.ProfileID, &item.SlotID, &item.ParentID,
			&item.Position, &visibleInt,
			&item.CustomLabel, &item.CustomIcon, &item.CustomIconVB, &item.CustomColorBrand,
			&item.SlotType, &item.ItemType, &lockedInt,
			&item.LabelKey, &item.LabelFallback,
			&item.IconFA, &item.IconViewBox,
		); err != nil {
			return nil, err
		}
		item.Visible = visibleInt == 1
		item.Locked = lockedInt == 1
		items = append(items, &item)
	}
	return items, rows.Err()
}

// ReorderLayout updates the parent_id and position of multiple items at once.
// Used by the Control Panel's drag-and-drop tree editor.
func ReorderLayout(profileID string, items []ReorderLayoutItem) error {
	for _, item := range items {
		var parentID interface{}
		if item.ParentID == "" {
			parentID = nil
		} else {
			parentID = item.ParentID
		}

		_, err := DB.Exec(`
			UPDATE menu_layout SET parent_id = ?, position = ?
			WHERE profile_id = ? AND slot_id = ?`,
			parentID, item.Position, profileID, item.SlotID,
		)
		if err != nil {
			return fmt.Errorf("reorder %s: %w", item.SlotID, err)
		}
	}
	return nil
}

// UpdateLayoutSlot updates the mutable fields of one layout entry.
// Only non-nil fields are updated.
func UpdateLayoutSlot(profileID, slotID string, visible *bool, customLabel, customIcon, customIconVB, customColorBrand *string) error {
	// Build the update dynamically to only set provided fields.
	sets := []string{}
	args := []interface{}{}

	if visible != nil {
		sets = append(sets, "visible = ?")
		args = append(args, boolToInt(*visible))
	}
	if customLabel != nil {
		sets = append(sets, "custom_label = ?")
		args = append(args, nullIfEmpty(*customLabel))
	}
	if customIcon != nil {
		sets = append(sets, "custom_icon = ?")
		args = append(args, nullIfEmpty(*customIcon))
	}
	if customIconVB != nil {
		sets = append(sets, "custom_icon_viewbox = ?")
		args = append(args, nullIfEmpty(*customIconVB))
	}
	if customColorBrand != nil {
		sets = append(sets, "custom_color_brand = ?")
		args = append(args, nullIfEmpty(*customColorBrand))
	}

	if len(sets) == 0 {
		return nil // nothing to update
	}

	query := "UPDATE menu_layout SET "
	for i, s := range sets {
		if i > 0 {
			query += ", "
		}
		query += s
	}
	query += " WHERE profile_id = ? AND slot_id = ?"
	args = append(args, profileID, slotID)

	res, err := DB.Exec(query, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Help CRUD ────────────────────────────────────────────────────────────────

// UpsertHelp inserts or replaces a single help-tab row.
//
// `ord` positions the entry within the (slot, profile, locale) bucket
// of ordered tabs:
//   - ord = 0 → primary (unnumbered) tab, equivalent to `readme.md`
//   - ord ≥ 1 → numbered tab, equivalent to `readme.<ord>.md`
//
// Multiple tabs can coexist for the same (slot, profile, locale) — the
// row is unique on (slot, profile, locale, ord). Cascade rules for
// choosing the bucket live in loadResolvedHelp (this function does not
// care; it just writes one row to the exact key supplied).
func UpsertHelp(slotID, profileID, locale string, ord int, markdown string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_help (slot_id, profile_id, locale, ord, markdown, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(slot_id, profile_id, locale, ord)
		DO UPDATE SET markdown = excluded.markdown, updated_at = excluded.updated_at`,
		slotID, profileID, locale, ord, markdown, now,
	)
	return err
}

// DeleteHelp removes a specific help-tab row.
//
// Deleting the primary tab (`ord = 0`) when numbered tabs exist is
// allowed — numbered tabs become "orphans" of the primary, the sort
// rule in assembleTabs still presents them in ascending order, and the
// renderer continues to work. This mirrors the device-help convention
// (see server/codegen/blackbox/devicehelp.go) where `readme.1.en.md`
// without a `readme.en.md` is a valid configuration. The admin can
// renumber manually if they want a clean sequence.
func DeleteHelp(slotID, profileID, locale string, ord int) error {
	res, err := DB.Exec(`
		DELETE FROM menu_help
		WHERE slot_id = ? AND profile_id = ? AND locale = ? AND ord = ?`,
		slotID, profileID, locale, ord,
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

// ListHelpEntries returns all help entries for a given slot_id across
// all profiles, locales, and tab positions. Used by the Control Panel
// help editor to populate the per-tab editor and the locale/profile
// pickers.
//
// Order: (profile_id, locale, ord) ascending. The UI groups by
// (profile, locale) and renders tabs in `ord` order; receiving rows
// pre-sorted by `ord` lets the client iterate without re-sorting.
func ListHelpEntries(slotID string) ([]MenuHelpEntry, error) {
	rows, err := DB.Query(`
		SELECT slot_id, profile_id, locale, ord, markdown, updated_at
		FROM menu_help WHERE slot_id = ?
		ORDER BY profile_id, locale, ord`, slotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []MenuHelpEntry
	for rows.Next() {
		var e MenuHelpEntry
		if err := rows.Scan(&e.SlotID, &e.ProfileID, &e.Locale, &e.Ord, &e.Markdown, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ─── Layout Labels CRUD ──────────────────────────────────────────────────────

// UpsertLayoutLabel inserts or replaces a locale-specific label override.
func UpsertLayoutLabel(profileID, slotID, locale, label string) error {
	_, err := DB.Exec(`
		INSERT INTO menu_layout_labels (profile_id, slot_id, locale, label)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(profile_id, slot_id, locale)
		DO UPDATE SET label = excluded.label`,
		profileID, slotID, locale, label,
	)
	return err
}

// DeleteLayoutLabel removes a locale-specific label override.
func DeleteLayoutLabel(profileID, slotID, locale string) error {
	res, err := DB.Exec(`
		DELETE FROM menu_layout_labels
		WHERE profile_id = ? AND slot_id = ? AND locale = ?`,
		profileID, slotID, locale,
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

// MenuLayoutLabel is one row from menu_layout_labels.
type MenuLayoutLabel struct {
	ProfileID string `json:"profile_id"`
	SlotID    string `json:"slot_id"`
	Locale    string `json:"locale"`
	Label     string `json:"label"`
}

// ListLayoutLabels returns all locale-specific label overrides for a given
// slot within a profile. Used by the Control Panel label editor.
func ListLayoutLabels(profileID, slotID string) ([]MenuLayoutLabel, error) {
	rows, err := DB.Query(`
		SELECT profile_id, slot_id, locale, label
		FROM menu_layout_labels
		WHERE profile_id = ? AND slot_id = ?
		ORDER BY locale ASC`, profileID, slotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []MenuLayoutLabel
	for rows.Next() {
		var l MenuLayoutLabel
		if err := rows.Scan(&l.ProfileID, &l.SlotID, &l.Locale, &l.Label); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}
