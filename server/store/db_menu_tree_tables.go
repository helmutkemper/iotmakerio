// server/store/db_menu_tree_tables.go — Migration statements for the
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// database-driven menu tree system.
//
// Called from migrate() in db.go, following the same pattern as
// menuCommerceMigrationStmts() and feedMigrationStmts().
//
// Tables created here:
//
//   - menu_items            — catalog of all menu pieces (system, section, device, category, template)
//   - menu_profiles         — audience definitions (default, kids, engineer, etc.)
//   - menu_layout           — tree structure per profile (position, visibility, overrides)
//   - menu_layout_labels    — per-locale label overrides within a profile
//   - menu_help             — markdown help text per item, optionally per profile and locale,
//     with `ord` allowing multiple ordered tabs per (slot, profile, locale)
//   - menu_help_files       — image assets referenced by markdown help (PNG/JPG/SVG/GIF/WebP);
//     stored as SQLite blobs and rewritten transparently at serve time
//   - menu_user_prefs       — maker's personal visibility preferences (checkbox overlay)
//   - menu_visibility_rules — group+country+date filters replacing menu_section_visibility
//
// Design notes:
//
//   - Catalog vs Layout separation: menu_items defines WHAT exists; menu_layout
//     defines WHERE each item appears for a given audience profile. Adding a new
//     item to the catalog does not affect any profile until the admin explicitly
//     activates it (auto-insert creates it with visible=0 in non-default profiles).
//
//   - System items (slot_type="system", locked=1) correspond to Go factory
//     functions compiled into the WASM. They cannot be deleted from the catalog
//     but can be hidden, renamed, moved, or reordered within any profile's layout.
//
//   - slot_type values:
//     "system"   — hardcoded items with Go factory functions (locked=1)
//     "section"  — branded vendor groups (Sparkfun, Adafruit, etc.)
//     "device"   — individual device/blackbox entries (auto-inserted on publish)
//     "category" — auto-generated grouping nodes for category/subcategory hierarchy
//     "template" — user-created templates (copied or published)
//
//   - Auto-insert flow: when a specialist publishes a new blackbox device, the
//     server inserts it into menu_items with slot_type="device", then inserts it
//     into menu_layout for every profile (visible=1 in default, visible=0 elsewhere).
//     Category and subcategory nodes are auto-created if they don't exist yet.
//
//   - Visibility is resolved in 3 layers (AND logic):
//     1. Admin layout: menu_layout.visible (per profile)
//     2. Visibility rules: menu_visibility_rules (group + country + date filters)
//     3. User prefs: menu_user_prefs (maker's personal checkbox overrides)
//     The maker can only HIDE items the admin made visible; never SHOW items the
//     admin hid. This guarantees the admin has ultimate control.
//
//   - Help markdown supports per-profile overrides AND multiple ordered tabs per
//     audience/locale, so the same item can have several pages of documentation
//     for engineers and a different set for children. Tabs are sorted by `ord`
//     with `ord=0` being the primary (unnumbered) page. The cascade for choosing
//     WHICH (profile, locale) bucket to use is:
//     profile-specific+locale → profile-specific+en → generic+locale → generic+en → empty.
//     Within the chosen bucket, ALL tabs (every `ord` value) are returned so the
//     client can render a tab bar.
//
//   - The "default" profile (locked=1) cannot be deleted and is the fallback
//     when no per-user assignment exists. Users choose their profile at registration
//     and can change it in Settings (stored in users.menu_profile_id).
//
//   - Brand colors live in the catalog (menu_items.color_brand) as the default,
//     but can be overridden per-profile via menu_layout.custom_color_brand
//     (e.g., softer colors for a children's profile).
//
//   - slot_id naming convention:
//     Sys*           — system items (SysMath, SysAdd, etc.)
//     Sec_<slug>     — branded sections (Sec_sparkfun, Sec_adafruit)
//     Cat_<name>     — auto-generated categories (Cat_Sensors, Cat_Communication)
//     SubCat_<c>_<s> — auto-generated subcategories (SubCat_Sensors_I2C)
//     Dev_<struct>   — devices/blackboxes (Dev_APDS9960, Dev_BME280)
//     Tmpl_<id>      — templates (Tmpl_abc123)
package store

// menuTreeMigrationStmts returns all CREATE TABLE and CREATE INDEX statements
// for the database-driven menu tree system.
// Every statement is idempotent (IF NOT EXISTS).
//
// Schema upgrades: this file follows the project-wide convention documented in
// db.go — "no ALTER TABLE; delete the database file to reset". Installations
// running an older schema for menu_help (without the `ord` column) will skip
// the CREATE here and continue with the old schema. The fix in that case is
// to wipe the DB file (pre-release, no production data to migrate). See
// INVARIANTS.md § "no legacy code, no backward-compatibility shims".
func menuTreeMigrationStmts() []string {
	return []string{

		// ── menu_items — Catalog ─────────────────────────────────────────────
		//
		// The global inventory of every menu piece that can appear in the IDE.
		// Contains no position or visibility information — that belongs in
		// menu_layout (per-profile).
		//
		// slot_type values:
		//   "system"   — hardcoded items with Go factory functions (locked=1)
		//   "section"  — branded vendor groups (Sparkfun, Adafruit, etc.)
		//   "device"   — individual blackbox/device entries
		//   "category" — auto-generated grouping nodes for category/subcategory hierarchy
		//   "template" — user-created templates (copied or published)
		//
		// item_type values:
		//   "submenu" — has children, opens a submenu when clicked
		//   "action"  — leaf item, triggers a factory function or callback
		//   "exit"    — special exit item (treated as action with exit semantics)
		//
		// locked=1 means the item cannot be deleted from the catalog.
		// All system items are locked. Sections, devices, categories, and templates are not.
		//
		// label_key + label_fallback are the arguments for translate.T(key, fallback).
		// The server uses these for label resolution when no custom_label is set.
		//
		// icon_fa contains the FontAwesome icon name (e.g. "square-root-variable").
		// The WASM factory functions have their own hardcoded SVG path data; the DB
		// icon is used by the Control Panel and for custom_icon overrides.
		//
		// color_brand is the default brand color for sections (e.g., "#E62E2E" for
		// Sparkfun). NULL for non-section items. Can be overridden per-profile.
		//
		// color_normal, color_attention, color_featured store the three brand colors
		// that map to hex menu pipeline states. Migrated from old menu_sections table.
		// NULL for non-section items.
		//
		// device_ref_id links to the blackboxes or template_packages table.
		// NULL for system, section, and category items.
		//
		// device_struct_name is the Go struct name (e.g. "APDS9960") for device items.
		// The WASM uses this to match against loaded BlackBoxDefClient entries and
		// build the correct Init/method submenu. NULL for non-device items.
		`CREATE TABLE IF NOT EXISTS menu_items (
			slot_id             TEXT    PRIMARY KEY,
			slot_type           TEXT    NOT NULL CHECK(slot_type IN ('system','section','device','category','template')),
			item_type           TEXT    NOT NULL CHECK(item_type IN ('submenu','action','exit')),
			locked              INTEGER NOT NULL DEFAULT 0,
			label_key           TEXT,
			label_fallback      TEXT,
			icon_fa             TEXT,
			icon_viewbox        TEXT,
			color_brand         TEXT,
			color_normal        TEXT,
			color_attention     TEXT,
			color_featured      TEXT,
			device_ref_id       TEXT,
			device_struct_name  TEXT,
			created_at          TEXT    NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mi_type ON menu_items(slot_type);`,
		`CREATE INDEX IF NOT EXISTS idx_mi_device_struct ON menu_items(device_struct_name) WHERE device_struct_name IS NOT NULL;`,

		// ── menu_profiles — Audiences ────────────────────────────────────────
		//
		// Each profile defines a complete menu layout for a target audience.
		// Only one profile has is_default=1 at any time — this is the profile
		// the WASM loads when the user has no explicit assignment (users.menu_profile_id IS NULL).
		//
		// The profile with profile_id="default" has locked=1 and cannot be
		// deleted. Other profiles are freely created, cloned, and deleted.
		//
		// Users choose their profile at registration (stored in users.menu_profile_id)
		// and can change it in Settings. NULL means "use the default profile".
		//
		// hide_user_prefs_overlay: when 1, the "Personalizar meu menu" overlay
		// is hidden for users on this profile. Useful for kids or toy products
		// where the admin wants full control with no user customization.
		`CREATE TABLE IF NOT EXISTS menu_profiles (
			profile_id              TEXT    PRIMARY KEY,
			name                    TEXT    NOT NULL,
			description             TEXT    NOT NULL DEFAULT '',
			is_default              INTEGER NOT NULL DEFAULT 0,
			locked                  INTEGER NOT NULL DEFAULT 0,
			hide_user_prefs_overlay INTEGER NOT NULL DEFAULT 0,
			created_at              TEXT    NOT NULL,
			updated_at              TEXT    NOT NULL
		);`,

		// ── menu_layout — Tree per profile ──────────────────────────────────
		//
		// Each row places one catalog item into a profile's menu tree.
		// The tree structure is defined by parent_id:
		//   NULL           → root level (appears in the sidebar rail)
		//   "SysMath"      → child of SysMath (appears in Math submenu)
		//   "Sec_sparkfun" → child of the Sparkfun branded section
		//   "Cat_Sensors"  → child of the auto-generated Sensors category
		//
		// Items can be freely moved between parents via drag-and-drop in the
		// Control Panel. A system item CAN be moved from Math to Logic if the
		// admin wants an exotic menu configuration.
		//
		// custom_label overrides the label for ALL locales in this profile.
		// For per-locale overrides, use menu_layout_labels instead.
		//
		// custom_icon + custom_icon_viewbox override the catalog icon for this
		// profile only. NULL means "use catalog default".
		//
		// custom_color_brand overrides the catalog brand color for this profile.
		// Example: softer red for a children's profile.
		`CREATE TABLE IF NOT EXISTS menu_layout (
			profile_id          TEXT    NOT NULL REFERENCES menu_profiles(profile_id) ON DELETE CASCADE,
			slot_id             TEXT    NOT NULL REFERENCES menu_items(slot_id) ON DELETE CASCADE,
			parent_id           TEXT,
			position            INTEGER NOT NULL DEFAULT 0,
			visible             INTEGER NOT NULL DEFAULT 1,
			custom_label        TEXT,
			custom_icon         TEXT,
			custom_icon_viewbox TEXT,
			custom_color_brand  TEXT,
			PRIMARY KEY (profile_id, slot_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ml_parent ON menu_layout(profile_id, parent_id, position);`,

		// ── menu_layout_labels — Per-locale label overrides ──────────────────
		//
		// Used when the admin wants different labels in different languages
		// within the same profile. For example, profile "kids" might have:
		//   locale="pt" label="Somar"
		//   locale="en" label="Add Up"
		//
		// This table is optional — most of the time custom_label in menu_layout
		// handles the simple case (same label for all languages).
		//
		// Label resolution priority (server resolves, highest first):
		//   1. menu_layout_labels (profile, slot, current locale)
		//   2. menu_layout_labels (profile, slot, "en")
		//   3. menu_layout.custom_label (profile, slot)
		//   4. translate.T(menu_items.label_key, menu_items.label_fallback)
		//   5. menu_items.slot_id (last resort)
		`CREATE TABLE IF NOT EXISTS menu_layout_labels (
			profile_id TEXT NOT NULL,
			slot_id    TEXT NOT NULL,
			locale     TEXT NOT NULL,
			label      TEXT NOT NULL,
			PRIMARY KEY (profile_id, slot_id, locale),
			FOREIGN KEY (profile_id, slot_id)
				REFERENCES menu_layout(profile_id, slot_id)
				ON DELETE CASCADE
		);`,

		// ── menu_help — Markdown help per item, per profile, per locale, per tab ──
		//
		// Each menu item can have rich markdown documentation shown in the
		// preview column when the maker selects it. Help text supports:
		//   - Multiple locales (en, pt, es, de, ...)
		//   - Per-profile overrides (technical for engineers, friendly for kids)
		//   - Multiple ordered tabs per (slot, profile, locale) bucket via `ord`
		//   - Code blocks with syntax highlighting (Go, C)
		//   - Images stored in menu_help_files and inlined as data: URLs at serve time
		//
		// `ord` column values:
		//   0   → primary (unnumbered) tab — equivalent to `readme.md` in the
		//         device-help filename grammar.
		//   1+  → numbered tabs — equivalent to `readme.1.md`, `readme.2.md`, …
		//
		// The renderer sorts ascending by `ord` with unnumbered (`ord=0`) always
		// first. Mirrors the assembly rule in
		// server/codegen/blackbox/devicehelp.go::assembleTabs so a future caller
		// that builds tabs from filenames OR from rows in this table gets the
		// same ordering.
		//
		// profile_id = '' means generic help that applies to all profiles
		// unless a profile-specific override exists.
		//
		// Help resolution priority for picking the right (profile, locale) bucket
		// (server resolves, highest first):
		//   1. menu_help (slot, current profile, current locale, ANY ord)
		//   2. menu_help (slot, current profile, "en",              ANY ord)
		//   3. menu_help (slot, '',              current locale,    ANY ord)
		//   4. menu_help (slot, '',              "en",              ANY ord)
		//   5. (no help — preview column shows no help body)
		//
		// Once a bucket is chosen, EVERY row with that (slot, profile_id, locale)
		// is returned, sorted by ord. The renderer hides the tab bar when only
		// one row matches (preserves the simple single-readme UX).
		`CREATE TABLE IF NOT EXISTS menu_help (
			slot_id    TEXT    NOT NULL REFERENCES menu_items(slot_id) ON DELETE CASCADE,
			profile_id TEXT    NOT NULL DEFAULT '',
			locale     TEXT    NOT NULL,
			ord        INTEGER NOT NULL DEFAULT 0,
			markdown   TEXT    NOT NULL,
			updated_at TEXT    NOT NULL,
			PRIMARY KEY (slot_id, profile_id, locale, ord)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mh_profile ON menu_help(profile_id, locale);`,

		// ── menu_help_files — Image assets referenced by help markdown ───────
		//
		// Per-slot pool of binary assets (PNG, JPG, SVG, GIF, WebP) that
		// markdown help can reference with relative paths like
		// `![alt](./diagram.png)`. The server rewrites those references at
		// serve time into `data:image/png;base64,...` URLs so the browser
		// can render them without a separate authenticated fetch
		// (the `<img src=...>` tag would not replay the Bearer token,
		// causing a 401). See
		// server/codegen/blackbox/devicehelp.go::RewriteImagePaths.
		//
		// Schema mirrors project_help_files exactly except:
		//   - `slot_id` replaces `project_id` (FK to menu_items instead of projects)
		//   - no per-slot or per-user byte quota — menu help is admin-only and
		//     the size envelope is trusted. If abuse appears later, add a
		//     SettingMenuHelpFilesMaxBytesPerSlot config in project_settings
		//     and a SumMenuHelpBytes() helper; the column shape needs no change.
		//
		// Path grammar (enforced by the handler, not by the schema): same as
		// project help files — `^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)?$`, max one
		// level of subdirectory, max 200 characters. Path traversal and
		// hostile segment names are rejected at the handler boundary.
		//
		// Columns:
		//   slot_id    — FK to menu_items with ON DELETE CASCADE. Deleting a
		//                catalog item sweeps its image pool automatically.
		//   path       — relative path within the slot's pool, e.g.
		//                "diagram.png" or "examples/wiring.svg". The composite
		//                PK (slot_id, path) gives O(1) lookup and uniqueness.
		//   mime_type  — derived server-side from the extension at write time
		//                and stored explicitly so a future whitelist tweak
		//                doesn't invalidate already-stored rows.
		//   content    — raw file bytes. BLOBs work for any binary content.
		//   size_bytes — denormalised length(content) so any future quota
		//                sum query stays cheap. Always written together with
		//                content in a single statement, so cannot drift.
		//   updated_at — RFC3339 timestamp of the last write.
		`CREATE TABLE IF NOT EXISTS menu_help_files (
			slot_id     TEXT    NOT NULL REFERENCES menu_items(slot_id) ON DELETE CASCADE,
			path        TEXT    NOT NULL,
			mime_type   TEXT    NOT NULL,
			content     BLOB    NOT NULL,
			size_bytes  INTEGER NOT NULL,
			updated_at  TEXT    NOT NULL,
			PRIMARY KEY (slot_id, path)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mhf_slot ON menu_help_files (slot_id);`,

		// ── menu_user_prefs — Maker's personal visibility preferences ────────
		//
		// Each maker can hide items from their own menu via a checkbox overlay
		// in the WASM Settings. This table stores only exceptions — if no row
		// exists for a (user_id, slot_id) pair, the item inherits the admin's
		// visibility setting from menu_layout.
		//
		// The maker can only SUBTRACT from the admin's choices:
		//   - Admin visible=1, user visible=0 → hidden for this maker
		//   - Admin visible=0, user visible=1 → still hidden (admin wins)
		//   - Admin visible=1, no pref row    → visible (default behavior)
		//
		// The visibility formula is:
		//   final_visible = admin_layout.visible AND COALESCE(user_prefs.visible, 1)
		//
		// When a maker hides a parent node (e.g., "Hardware"), all its children
		// are implicitly hidden (the tree builder skips subtrees of hidden parents).
		//
		// The overlay can be hidden per-profile via menu_profiles.hide_user_prefs_overlay
		// (e.g., kids profile or branded toy product).
		`CREATE TABLE IF NOT EXISTS menu_user_prefs (
			user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			slot_id    TEXT    NOT NULL REFERENCES menu_items(slot_id) ON DELETE CASCADE,
			visible    INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT    NOT NULL,
			PRIMARY KEY (user_id, slot_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_muprefs_user ON menu_user_prefs(user_id);`,

		// ── menu_visibility_rules — Group+country+date filters ───────────────
		//
		// Replaces the old menu_section_visibility table with a general-purpose
		// visibility filter that applies to ANY menu item (not just sections).
		//
		// A slot with NO visibility rules is visible to everyone (who passes the
		// admin layout visible check). A slot WITH rules is visible only to users
		// who match at least one rule.
		//
		// Matching rules (server-side, evaluated at tree build time):
		//   group_id     NULL → matches any group (or no group at all)
		//   country_code NULL → matches any country
		//   valid_from   NULL → open start (always started)
		//   valid_until  NULL → open end (never expires)
		//
		// Example: section Sparkfun visible only to Brazilian users:
		//   INSERT INTO menu_visibility_rules (id, slot_id, country_code)
		//   VALUES ('rule1', 'Sec_sparkfun', 'BR');
		//
		// Example: category "Advanced" visible only to engineer group:
		//   INSERT INTO menu_visibility_rules (id, slot_id, group_id)
		//   VALUES ('rule2', 'Cat_Advanced', 'group_engineers');
		`CREATE TABLE IF NOT EXISTS menu_visibility_rules (
			id           TEXT PRIMARY KEY,
			slot_id      TEXT NOT NULL REFERENCES menu_items(slot_id) ON DELETE CASCADE,
			mode         TEXT NOT NULL DEFAULT 'allow',
			group_id     TEXT REFERENCES user_groups(id) ON DELETE CASCADE,
			country_code TEXT,
			valid_from   TEXT,
			valid_until  TEXT,
			created_at   TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mvr_slot  ON menu_visibility_rules(slot_id);`,
		`CREATE INDEX IF NOT EXISTS idx_mvr_group ON menu_visibility_rules(group_id);`,

		// ── user_panel_prefs — Saved column widths for the IDE WASM panel ──
		//
		// Each user can have different panel widths per OS+browser combination.
		// This allows the same user to have different layouts on a laptop vs
		// a wide monitor, or on Chrome vs Firefox.
		//
		// Columns:
		//   rail_width — width in pixels of the icon rail (column 1, default 96)
		//   list_width — width in pixels of the item list (column 2, default 250)
		//
		// The preview column (column 3) always fills the remaining space.
		`CREATE TABLE IF NOT EXISTS user_panel_prefs (
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			os         TEXT NOT NULL,
			browser    TEXT NOT NULL,
			rail_width INTEGER NOT NULL DEFAULT 96,
			list_width INTEGER NOT NULL DEFAULT 250,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, os, browser)
		);`,
	}
}
