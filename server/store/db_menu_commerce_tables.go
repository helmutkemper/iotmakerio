// server/store/db_menu_commerce_tables.go — migration statements for the dynamic
// menu section system and commerce/BOM feature.
//
// Called from migrate() in db.go, following the same pattern as
// feedMigrationStmts() and communityMigrationStmts().
//
// Tables created here:
//
//	user_groups             — named segments of users (e.g. "attiny85_users")
//	user_group_members      — many-to-many users ↔ groups
//	menu_sections           — branded menu sections (Sparkfun, Adafruit, …)
//	menu_section_items      — projects/templates pinned inside a section
//	menu_section_visibility — optional group+country+date filter per section
//	menu_user_pins          — maker's personal pins from the feed
//	components              — physical chip/module catalogue
//	blackbox_components     — maps a Go struct name to one or more components
//	stores                  — external stores per country
//	store_listings          — component ↔ store join with affiliate URL info
//	store_redirect_log      — click analytics for store redirects
//
// Design notes:
//
//   - user_groups are created by admins or auto-detected by the system.
//     source = "admin" | "auto" distinguishes the two cases.
//
//   - menu_sections use a slug (URL-safe identifier) that becomes the MenuItem.ID
//     in the WASM. The Go code never hardcodes section names — they are loaded
//     from the API at startup.
//
//   - menu_section_visibility is optional: a section with no visibility rows
//     is visible to all users in all countries at all times.
//     NULL group_id    = any group.
//     NULL country_code = any country.
//     NULL valid_from / valid_until = open-ended.
//
//   - blackbox_components references the Go struct name (e.g. "APDS9960"),
//     not the method. The maker buys the chip once regardless of how many
//     methods they place on the canvas.
//
//   - store_listings.product_url is relative to stores.base_url.
//     The server concatenates them at redirect time so affiliate tags can be
//     rotated without a frontend redeployment.
//
//   - price_hint is informational only. The external store is the price authority.
package store

// menuCommerceMigrationStmts returns all CREATE TABLE and CREATE INDEX
// statements for the dynamic menu and commerce features.
// Every statement is idempotent (IF NOT EXISTS).
func menuCommerceMigrationStmts() []string {
	return []string{

		// ── User groups ──────────────────────────────────────────────────────
		//
		// Groups are plain named segments. New groups are just new rows —
		// no code change required. The system ships with no groups; admins
		// create them via the admin panel or directly in the database.
		`CREATE TABLE IF NOT EXISTS user_groups (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ugroups_name ON user_groups(name);`,

		// ── User group members ───────────────────────────────────────────────
		//
		// Many-to-many join. source distinguishes manual admin assignments
		// from automatic behaviour-based assignments made by the system.
		// ON DELETE CASCADE keeps this table clean when users or groups are removed.
		`CREATE TABLE IF NOT EXISTS user_group_members (
			user_id    TEXT NOT NULL REFERENCES users(id)        ON DELETE CASCADE,
			group_id   TEXT NOT NULL REFERENCES user_groups(id)  ON DELETE CASCADE,
			source     TEXT NOT NULL DEFAULT 'admin'
			           CHECK(source IN ('admin','auto')),
			added_at   TEXT NOT NULL,
			PRIMARY KEY (user_id, group_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ugm_group  ON user_group_members(group_id);`,
		`CREATE INDEX IF NOT EXISTS idx_ugm_user   ON user_group_members(user_id);`,

		// ── Menu sections ────────────────────────────────────────────────────
		//
		// Each section is a branded group of projects/templates that appears
		// as a submenu in the IDE. Colors map directly to hexMenu.IconStyle:
		//   color_normal    → PipelineNormal background
		//   color_attention → PipelineAttention1 background (flash state)
		//   color_featured  → PipelineSelected background (highlighted state)
		//
		// The slug becomes the MenuItem.ID in WASM. It must be unique and
		// URL-safe (lowercase, hyphens only). Example: "sparkfun", "adafruit".
		`CREATE TABLE IF NOT EXISTS menu_sections (
			id               TEXT PRIMARY KEY,
			slug             TEXT NOT NULL UNIQUE,
			name             TEXT NOT NULL,
			position         INTEGER NOT NULL DEFAULT 0,
			color_normal     TEXT NOT NULL DEFAULT '#185FA5',
			color_attention  TEXT NOT NULL DEFAULT '#C42B2B',
			color_featured   TEXT NOT NULL DEFAULT '#1D9E75',
			icon_fa          TEXT NOT NULL DEFAULT 'gear',
			active           INTEGER NOT NULL DEFAULT 1,
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_msections_position ON menu_sections(position, active);`,
		`CREATE INDEX IF NOT EXISTS idx_msections_slug     ON menu_sections(slug);`,

		// ── Menu section items ───────────────────────────────────────────────
		//
		// Projects and templates pinned inside a section by an admin.
		// item_type ∈ {"project", "template"}.
		// item_ref_id is the primary key in the projects or template_packages table.
		// position controls the order within the section (lower = higher).
		// visible is a per-item soft switch — the section stays intact even
		// when individual items are temporarily hidden.
		`CREATE TABLE IF NOT EXISTS menu_section_items (
			id          TEXT PRIMARY KEY,
			section_id  TEXT NOT NULL REFERENCES menu_sections(id) ON DELETE CASCADE,
			item_type   TEXT NOT NULL CHECK(item_type IN ('project','template','device')),
			item_ref_id TEXT NOT NULL,
			position    INTEGER NOT NULL DEFAULT 0,
			visible     INTEGER NOT NULL DEFAULT 1,
			created_at  TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_msi_section   ON menu_section_items(section_id, position);`,
		`CREATE INDEX IF NOT EXISTS idx_msi_ref       ON menu_section_items(item_type, item_ref_id);`,

		// ── Menu section visibility ──────────────────────────────────────────
		//
		// Optional filter rows that restrict a section to a subset of users.
		// A section with NO visibility rows is visible to everyone.
		// A section WITH rows is visible only to users who match at least one row.
		//
		// Matching rules (server-side, evaluated at API time):
		//   group_id    NULL → matches any group (or no group at all)
		//   country_code NULL → matches any country
		//   valid_from  NULL → open start (always started)
		//   valid_until NULL → open end (never expires)
		`CREATE TABLE IF NOT EXISTS menu_section_visibility (
			id           TEXT PRIMARY KEY,
			section_id   TEXT NOT NULL REFERENCES menu_sections(id) ON DELETE CASCADE,
			group_id     TEXT REFERENCES user_groups(id)             ON DELETE CASCADE,
			country_code TEXT,
			valid_from   TEXT,
			valid_until  TEXT,
			created_at   TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_msv_section ON menu_section_visibility(section_id);`,
		`CREATE INDEX IF NOT EXISTS idx_msv_group   ON menu_section_visibility(group_id);`,

		// ── Menu user pins ───────────────────────────────────────────────────
		//
		// Maker's personal menu pins, added from the feed ("Add to my menu").
		// Appears as a personal submenu separate from admin-curated sections.
		// The maker controls position via drag-and-drop in a future UI.
		`CREATE TABLE IF NOT EXISTS menu_user_pins (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			item_type   TEXT NOT NULL CHECK(item_type IN ('project','template','device')),
			item_ref_id TEXT NOT NULL,
			position    INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL,
			UNIQUE(user_id, item_type, item_ref_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mup_user ON menu_user_pins(user_id, position);`,

		// ── Components ───────────────────────────────────────────────────────
		//
		// Physical chip or module catalogue. One row per real-world part,
		// shared across all stores and countries.
		// datasheet_url and image_url are optional external links.
		`CREATE TABLE IF NOT EXISTS components (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL UNIQUE,
			description   TEXT NOT NULL DEFAULT '',
			datasheet_url TEXT NOT NULL DEFAULT '',
			image_url     TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_components_name ON components(name);`,

		// ── Black-box components ─────────────────────────────────────────────
		//
		// Maps a Go struct name (e.g. "APDS9960") to one or more physical
		// components. The maker buys the chip once regardless of how many
		// methods they place on the canvas, so the reference is to the struct
		// name — not to a method.
		//
		// quantity is how many units of the component one black-box instance needs.
		// Most chips are quantity=1; some modules may include multiple chips.
		// notes is an optional free-text annotation for the admin (e.g. "use DIP-8 package").
		`CREATE TABLE IF NOT EXISTS blackbox_components (
			id             TEXT PRIMARY KEY,
			blackbox_name  TEXT NOT NULL,
			component_id   TEXT NOT NULL REFERENCES components(id) ON DELETE CASCADE,
			quantity       INTEGER NOT NULL DEFAULT 1,
			notes          TEXT NOT NULL DEFAULT '',
			created_at     TEXT NOT NULL,
			UNIQUE(blackbox_name, component_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_bbc_name      ON blackbox_components(blackbox_name);`,
		`CREATE INDEX IF NOT EXISTS idx_bbc_component ON blackbox_components(component_id);`,

		// ── Stores ───────────────────────────────────────────────────────────
		//
		// One row per external store per country. Multiple stores can serve
		// the same country (e.g. Mouser + DigiKey for "US").
		// base_url is the store's root URL (e.g. "https://www.sparkfun.com").
		// affiliate_tag is appended to every redirect URL by the server.
		// active = 0 disables the store without deleting its listings.
		`CREATE TABLE IF NOT EXISTS stores (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL,
			country_code  TEXT NOT NULL,
			base_url      TEXT NOT NULL,
			affiliate_tag TEXT NOT NULL DEFAULT '',
			active        INTEGER NOT NULL DEFAULT 1,
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_stores_country ON stores(country_code, active);`,

		// ── Store listings ───────────────────────────────────────────────────
		//
		// Joins a component to a store with a relative product path and
		// optional informational price. price_hint is never used in any
		// checkout flow — the external store is the price authority.
		//
		// product_url is relative to stores.base_url.
		// The server concatenates them at redirect time:
		//   final_url = base_url + product_url + "?tag=" + affiliate_tag
		`CREATE TABLE IF NOT EXISTS store_listings (
			id           TEXT PRIMARY KEY,
			component_id TEXT NOT NULL REFERENCES components(id) ON DELETE CASCADE,
			store_id     TEXT NOT NULL REFERENCES stores(id)     ON DELETE CASCADE,
			product_url  TEXT NOT NULL,
			price_hint   TEXT NOT NULL DEFAULT '',
			currency     TEXT NOT NULL DEFAULT 'USD',
			in_stock     INTEGER NOT NULL DEFAULT 1,
			updated_at   TEXT NOT NULL,
			UNIQUE(component_id, store_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_listings_component ON store_listings(component_id);`,
		`CREATE INDEX IF NOT EXISTS idx_listings_store     ON store_listings(store_id);`,

		// ── Store redirect log ───────────────────────────────────────────────
		//
		// Analytics only. Records every click on a "Buy" button.
		// user_id is NULL for anonymous clicks (not authenticated).
		// ip_country is the inferred country from the request IP — used only
		// for analytics, never for access control.
		// No personal data beyond user_id is stored.
		`CREATE TABLE IF NOT EXISTS store_redirect_log (
			id         TEXT PRIMARY KEY,
			listing_id TEXT NOT NULL REFERENCES store_listings(id) ON DELETE CASCADE,
			user_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
			ip_country TEXT NOT NULL DEFAULT '',
			clicked_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_redirect_listing ON store_redirect_log(listing_id, clicked_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_redirect_user    ON store_redirect_log(user_id, clicked_at DESC);`,
	}
}
