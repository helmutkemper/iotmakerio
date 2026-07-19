// server/store/menu_tree_seed.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

// server/store/menu_tree_seed.go — Populates the menu tree tables on first boot.
//
// Called from migrate() in db.go after the menu tree tables are created.
// All operations use INSERT OR IGNORE so re-running is safe and idempotent.
//
// Seed order:
//  1. Catalog (menu_items) — all system items
//  2. Default profile (menu_profiles) — the "default" profile with locked=1
//  3. Default layout (menu_layout) — all items positioned in their natural tree
//
// icon_fa stores the FontAwesome icon NAME (e.g., "square-root-variable"),
// not SVG path data. The Control Panel uses this for <i class="fa-solid fa-{name}">.
// The WASM factory functions have their own hardcoded SVG path data and use it
// as the default — the database icon is only relevant for Control Panel display
// and for custom_icon overrides in menu_layout.
//
// When a new item is added to the catalog in the future (e.g., SysSin):
//   - Insert into menu_items (catalog)
//   - For each existing profile in menu_profiles:
//   - is_default=1 → insert into menu_layout with visible=1, position=last
//   - otherwise     → insert into menu_layout with visible=0, position=last
//     This is handled by InsertCatalogItem() in menu_tree.go, not here.
package store

import (
	"log"
	"time"
)

// SeedMenuTree creates the default catalog, profile, and layout if the
// menu_items table is empty. Called once during server boot from migrate().
//
// Label migrations (migrateMenuTreeLabels) ALWAYS run, even when the
// catalog is already populated — that's the only way for an existing
// installation to pick up label changes between releases without
// requiring the user to delete their database. The seed itself uses
// INSERT OR IGNORE so it cannot retroactively fix a row written by
// an earlier version of this file; the explicit UPDATEs in
// migrateMenuTreeLabels close that gap.
func SeedMenuTree() error {
	// Step 0: reconcile label changes for rows seeded by earlier
	// versions of this file. Idempotent — runs every boot.
	if err := migrateMenuTreeLabels(); err != nil {
		return err
	}

	// [FIX 2026-07] The seed RECONCILES on every boot instead of skipping
	// when the catalog is non-empty. Every statement below is INSERT OR
	// IGNORE (idempotent by construction — see the file header), so
	// re-running only fills holes and never overwrites admin edits. The old
	// all-or-nothing guard turned ONE interrupted first boot into a
	// permanently half-seeded database: count>0 skipped the whole seed
	// forever after, and the groups whose children exist ONLY here — the
	// Display widgets and the Export actions, the LAST entries of the
	// layout slice — stayed missing while the per-boot Migrate* functions
	// healed everything else. The count now only picks the log message.
	// Português: [FIX 2026-07] O seed RECONCILIA a cada boot em vez de
	// pular quando o catálogo não está vazio. Todo statement abaixo é
	// INSERT OR IGNORE (idempotente por construção — ver o cabeçalho do
	// arquivo), então re-rodar só preenche buracos e nunca sobrescreve
	// edições do admin. A guarda tudo-ou-nada antiga transformava UM
	// primeiro boot interrompido em banco permanentemente meio-populado:
	// count>0 pulava o seed inteiro para sempre, e os grupos cujos filhos
	// só existem AQUI — os widgets do Display e as ações do Export, as
	// ÚLTIMAS entradas do slice de layout — ficavam ausentes enquanto as
	// Migrate* de todo boot curavam o resto. O count agora só escolhe a
	// mensagem de log.
	var count int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM menu_items`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		log.Printf("[menu_tree_seed] catalog has %d items — reconciling seed (INSERT OR IGNORE fills gaps only)", count)
	} else {
		log.Printf("[menu_tree_seed] seeding menu tree catalog, profile, and layout...")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// ── Step 1: Seed catalog ─────────────────────────────────────────────────

	type catalogItem struct {
		slotID      string
		slotType    string // "system"
		itemType    string // "submenu" | "action" | "exit"
		locked      int
		labelKey    string
		fallback    string
		iconFA      string // FontAwesome icon name for Control Panel display
		iconViewbox string
		colorBrand  string // only for section items
		deviceRefID string // only for device items
	}

	catalog := []catalogItem{
		// ── Root-level items ──────────────────────────────────────────────
		{"SysMath", "system", "submenu", 1, "menuMainMath", "Math", "square-root-variable", "0 0 640 640", "", ""},
		{"SysLogic", "system", "submenu", 1, "menuMainLogic", "Logic", "binary", "0 0 640 512", "", ""},
		{"SysLoop", "system", "submenu", 1, "menuMainLoop", "Loop", "repeat", "0 0 512 512", "", ""},
		{"SysConst", "system", "submenu", 1, "menuMainConst", "Const", "bars", "0 0 448 512", "", ""},
		{"SysVar", "system", "submenu", 1, "menuMainVar", "Variables", "suitcase", "0 0 512 512", "", ""},
		{"SysDisplay", "system", "submenu", 1, "menuMainDisplay", "Display", "desktop", "0 0 576 512", "", ""},
		{"SysData", "system", "submenu", 1, "menuMainData", "Data", "file-export", "0 0 512 512", "", ""},
		{"SysMyItems", "system", "submenu", 1, "menuMainMyItems", "My Items", "box-open", "0 0 640 512", "", ""},
		{"SysExport", "system", "submenu", 1, "menuMainExport", "Export", "file-export", "0 0 576 512", "", ""},
		{"SysSettings", "system", "action", 1, "menuMainSettings", "Settings", "gear", "0 0 512 512", "", ""},
		{"SysExit", "system", "exit", 1, "menuMainExit", "Exit", "right-from-bracket", "0 0 512 512", "", ""},

		// ── Math children ────────────────────────────────────────────────
		{"SysAdd", "system", "action", 1, "menuMainAdd", "Add", "plus", "0 0 448 512", "", ""},
		{"SysSub", "system", "action", 1, "menuMainSub", "Sub", "minus", "0 0 448 512", "", ""},
		{"SysMul", "system", "action", 1, "menuMainMul", "Mul", "xmark", "0 0 384 512", "", ""},
		{"SysDiv", "system", "action", 1, "menuMainDiv", "Div", "divide", "0 0 448 512", "", ""},

		// ── Logic children ───────────────────────────────────────────────
		{"SysEqualTo", "system", "action", 1, "menuMainEqualTo", "Equal to", "equals", "0 0 640 512", "", ""},
		{"SysNotEqualTo", "system", "action", 1, "menuMainNotEqualTo", "Not equal to", "not-equal", "0 0 640 512", "", ""},
		{"SysLessThan", "system", "action", 1, "menuMainLessThan", "Less than", "less-than", "0 0 640 512", "", ""},
		{"SysLessThanOrEqualTo", "system", "action", 1, "menuMainLessThanOrEqualTo", "Less than or equal to", "less-than-equal", "0 0 640 512", "", ""},
		{"SysGreaterThan", "system", "action", 1, "menuMainGreaterThan", "Greater than", "greater-than", "0 0 640 512", "", ""},
		{"SysGreaterThanOrEqualTo", "system", "action", 1, "menuMainGreaterThanOrEqualTo", "Greater than or equal to", "greater-than-equal", "0 0 640 512", "", ""},
		{"SysCaseItem", "system", "action", 1, "menuMainCase", "Case", "layer-group", "0 0 512 512", "", ""},
		{"SysSequenceItem", "system", "action", 1, "menuMainSequence", "Sequence", "list-ol", "0 0 512 512", "", ""},
		{"SysEmbedded", "system", "submenu", 1, "menuMainEmbedded", "Arduino / Embedded", "microchip", "0 0 512 512", "", ""},
		{"SysFunctionItem", "system", "action", 1, "menuMainFunction", "Function", "florin-sign", "0 0 384 512", "", ""},

		// ── Loop children ────────────────────────────────────────────────
		{"SysLoopItem", "system", "action", 1, "menuMainLoop", "Loop", "repeat", "0 0 512 512", "", ""},
		{"SysLoopDurationItem", "system", "action", 1, "menuMainLoopDuration", "Timed", "hourglass-half", "0 0 384 512", "", ""},

		// ── Const children ───────────────────────────────────────────────
		{"SysConstInt", "system", "action", 1, "menuMainConstInt", "Int", "bars", "0 0 448 512", "", ""},
		{"SysConstBool", "system", "action", 1, "menuMainConstBool", "Bool", "toggle-on", "0 0 576 512", "", ""},
		{"SysConstFloat", "system", "action", 1, "menuMainConstFloat", "Float", "divide", "0 0 448 512", "", ""},
		{"SysConstString", "system", "action", 1, "menuMainConstString", "String", "pen", "0 0 512 512", "", ""},
		{"SysConstDuration", "system", "action", 1, "menuMainConstDuration", "Duration", "hourglass-half", "0 0 384 512", "", ""},
		{"SysConstArrayInt", "system", "action", 1, "menuMainConstArrayInt", "Int Array", "layer-group", "0 0 512 512", "", ""},
		{"SysConstArrayFloat", "system", "action", 1, "menuMainConstArrayFloat", "Float Array", "layer-group", "0 0 512 512", "", ""},
		{"SysConstArrayString", "system", "action", 1, "menuMainConstArrayString", "String Array", "layer-group", "0 0 512 512", "", ""},

		// ── Data children ────────────────────────────────────────────────
		{"SysDataFile", "system", "action", 1, "menuDataFile", "File", "file-export", "0 0 512 512", "", ""},
		{"SysDataText", "system", "action", 1, "menuDataText", "Text", "pen", "0 0 512 512", "", ""},

		// ── Variables children ───────────────────────────────────────────
		{"SysGetVarInt", "system", "action", 1, "menuMainGetVarInt", "Get Int", "bars", "0 0 448 512", "", ""},
		{"SysGetVarFloat", "system", "action", 1, "menuMainGetVarFloat", "Get Float", "bars", "0 0 448 512", "", ""},
		{"SysSetVarInt", "system", "action", 1, "menuMainSetVarInt", "Set Int", "bars", "0 0 448 512", "", ""},
		{"SysSetVarFloat", "system", "action", 1, "menuMainSetVarFloat", "Set Float", "bars", "0 0 448 512", "", ""},
		{"SysGetVarString", "system", "action", 1, "menuMainGetVarString", "Get String", "bars", "0 0 448 512", "", ""},
		{"SysSetVarString", "system", "action", 1, "menuMainSetVarString", "Set String", "bars", "0 0 448 512", "", ""},

		// ── Display children ─────────────────────────────────────────────
		{"SysGauge", "system", "action", 1, "menuMainGauge", "Gauge", "gauge-high", "0 0 512 512", "", ""},
		{"SysLED", "system", "action", 1, "menuMainLED", "LED", "lightbulb", "0 0 576 512", "", ""},
		{"SysBarGraph", "system", "action", 1, "menuMainBarGraph", "Bar", "chart-bar", "0 0 448 512", "", ""},
		{"SysTextDisplay", "system", "action", 1, "menuMainTextDisplay", "Text", "font", "0 0 512 512", "", ""},
		{"SysButton", "system", "action", 1, "menuMainButton", "Button", "circle-play", "0 0 384 512", "", ""},
		{"SysSevenSeg", "system", "action", 1, "menuMainSevenSeg", "7-Seg", "display", "0 0 576 512", "", ""},
		{"SysKnob", "system", "action", 1, "menuMainKnob", "Knob", "dial", "0 0 512 512", "", ""},
		{"SysChart", "system", "action", 1, "menuMainChart", "Chart", "chart-line", "0 0 512 512", "", ""},
		{"SysChartPro", "system", "action", 1, "menuMainChartPro", "Chart Pro", "chart-area", "0 0 512 512", "", ""},
		{"SysPieChart", "system", "action", 1, "menuMainPieChart", "Pie Chart", "chart-pie", "0 0 576 512", "", ""},
		{"SysBgImage", "system", "action", 1, "menuMainBackgroundImage", "Background", "image", "0 0 448 512", "", ""},
		{"SysCommStatus", "system", "action", 1, "menuMainCommStatus", "Comm", "network-wired", "0 0 640 512", "", ""},

		// ── Export children ──────────────────────────────────────────────
		{"SysExportJSON", "system", "action", 1, "menuMainExportJSON", "JSON", "file-code", "0 0 576 512", "", ""},
		// slot_id "SysExportGo" kept for historical reasons (avoids a
		// menu_layout rewrite). The label is language-neutral: the
		// codegen pipeline picks the backend from w.Language at click
		// time, so the menu doesn't surface the choice. See
		// stageWorkspace/workspace.go → export().
		{"SysExportGo", "system", "action", 1, "menuMainExport", "Export", "code", "0 0 512 512", "", ""},
		{"SysExportFiles", "system", "action", 1, "menuMainFiles", "Files", "folder-open", "0 0 448 512", "", ""},
		{"SysExportImage", "system", "action", 1, "menuMainImage", "Image", "camera", "0 0 448 512", "", ""},
	}

	for _, item := range catalog {
		_, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_key, label_fallback,
				 icon_fa, icon_viewbox, color_brand, device_ref_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.slotID, item.slotType, item.itemType, item.locked,
			item.labelKey, item.fallback,
			nullIfEmpty(item.iconFA), nullIfEmpty(item.iconViewbox),
			nullIfEmpty(item.colorBrand), nullIfEmpty(item.deviceRefID),
			now,
		)
		if err != nil {
			return err
		}
	}
	log.Printf("[menu_tree_seed] inserted %d catalog items", len(catalog))

	// ── Step 2: Create default profile ───────────────────────────────────────

	_, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_profiles
			(profile_id, name, description, is_default, locked, created_at, updated_at)
		VALUES ('default', 'Default', 'Full menu with all items visible. Cannot be deleted.', 1, 1, ?, ?)`,
		now, now,
	)
	if err != nil {
		return err
	}
	log.Printf("[menu_tree_seed] created default profile")

	// ── Step 3: Seed default layout ──────────────────────────────────────────
	//
	// Each entry defines: slot_id, parent_id (NULL=root), position.
	// All items are visible=1 in the default profile.

	type layoutEntry struct {
		slotID   string
		parentID string // "" means NULL (root level)
		position int
	}

	layout := []layoutEntry{
		// Root items (rail level)
		{"SysMath", "", 1},
		{"SysLogic", "", 2},
		{"SysLoop", "", 3},
		{"SysConst", "", 4},
		{"SysVar", "", 5},
		{"SysDisplay", "", 6},
		{"SysData", "", 7},
		{"SysExport", "", 8},
		{"SysSettings", "", 9},
		{"SysMyItems", "", 10},
		{"SysExit", "", 11},

		// Data children
		{"SysDataFile", "SysData", 1},
		{"SysDataText", "SysData", 2},

		// Math children
		{"SysAdd", "SysMath", 1},
		{"SysSub", "SysMath", 2},
		{"SysMul", "SysMath", 3},
		{"SysDiv", "SysMath", 4},

		// Logic children
		{"SysEqualTo", "SysLogic", 1},
		{"SysNotEqualTo", "SysLogic", 2},
		{"SysLessThan", "SysLogic", 3},
		{"SysLessThanOrEqualTo", "SysLogic", 4},
		{"SysGreaterThan", "SysLogic", 5},
		{"SysGreaterThanOrEqualTo", "SysLogic", 6},
		{"SysCaseItem", "SysLogic", 8},
		{"SysSequenceItem", "SysLogic", 9},
		{"SysEmbedded", "", 10},
		{"SysFunctionItem", "SysEmbedded", 1},

		// Loop children
		{"SysLoopItem", "SysLoop", 1},
		{"SysLoopDurationItem", "SysLoop", 2},

		// Const children
		{"SysConstInt", "SysConst", 1},
		{"SysConstBool", "SysConst", 2},
		{"SysConstFloat", "SysConst", 3},
		{"SysConstString", "SysConst", 4},
		{"SysConstDuration", "SysConst", 5},
		{"SysConstArrayInt", "SysConst", 6},
		{"SysConstArrayFloat", "SysConst", 7},
		{"SysConstArrayString", "SysConst", 8},

		// Variables children
		{"SysSetVarInt", "SysVar", 1},
		{"SysGetVarInt", "SysVar", 2},
		{"SysSetVarFloat", "SysVar", 3},
		{"SysGetVarFloat", "SysVar", 4},
		{"SysSetVarString", "SysVar", 5},
		{"SysGetVarString", "SysVar", 6},

		// Display children
		{"SysGauge", "SysDisplay", 1},
		{"SysLED", "SysDisplay", 2},
		{"SysBarGraph", "SysDisplay", 3},
		{"SysTextDisplay", "SysDisplay", 4},
		{"SysButton", "SysDisplay", 5},
		{"SysSevenSeg", "SysDisplay", 6},
		{"SysKnob", "SysDisplay", 7},
		{"SysChart", "SysDisplay", 8},
		{"SysChartPro", "SysDisplay", 9},
		{"SysPieChart", "SysDisplay", 10},
		{"SysBgImage", "SysDisplay", 11},
		{"SysCommStatus", "SysDisplay", 12},

		// Export children
		{"SysExportJSON", "SysExport", 1},
		{"SysExportGo", "SysExport", 2},
		{"SysExportFiles", "SysExport", 3},
		{"SysExportImage", "SysExport", 4},
	}

	for _, entry := range layout {
		var parentID interface{}
		if entry.parentID == "" {
			parentID = nil
		} else {
			parentID = entry.parentID
		}

		_, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES ('default', ?, ?, ?, 1)`,
			entry.slotID, parentID, entry.position,
		)
		if err != nil {
			return err
		}
	}
	log.Printf("[menu_tree_seed] ensured %d layout entries for default profile", len(layout))

	return nil
}

// MigrateMenuTreeLoopDuration inserts the LoopDuration and ConstDuration
// items into existing databases that were seeded before these items
// existed. Uses INSERT OR IGNORE so it's safe to call multiple times.
//
// Called from migrate() in db.go after SeedMenuTree().
//
// Português: Insere os itens LoopDuration e ConstDuration em bancos
// existentes que foram populados antes desses itens existirem.
func MigrateMenuTreeLoopDuration() error {
	now := time.Now().UTC().Format(time.RFC3339)

	// ── Catalog entries ──────────────────────────────────────────────────
	type catItem struct {
		slotID, labelKey, fallback, iconFA, iconViewbox string
	}
	newItems := []catItem{
		{"SysLoopDurationItem", "menuMainLoopDuration", "Timed", "hourglass-half", "0 0 384 512"},
		{"SysConstDuration", "menuMainConstDuration", "Duration", "hourglass-half", "0 0 384 512"},
	}
	for _, item := range newItems {
		_, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_key, label_fallback,
				 icon_fa, icon_viewbox, created_at)
			VALUES (?, 'system', 'action', 1, ?, ?, ?, ?, ?)`,
			item.slotID, item.labelKey, item.fallback,
			item.iconFA, item.iconViewbox, now,
		)
		if err != nil {
			return err
		}
	}

	// ── Layout entries (all profiles) ────────────────────────────────────
	type layoutItem struct {
		slotID, parentID string
	}
	newLayout := []layoutItem{
		{"SysLoopDurationItem", "SysLoop"},
		{"SysConstDuration", "SysConst"},
	}

	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}

	for _, p := range profiles {
		for _, item := range newLayout {
			// Find the max position under this parent
			var maxPos int
			DB.QueryRow(`
				SELECT COALESCE(MAX(position), 0) FROM menu_layout
				WHERE profile_id = ? AND parent_id = ?`, p.id, item.parentID).Scan(&maxPos)

			visible := 0
			if p.isDefault {
				visible = 1
			}

			DB.Exec(`
				INSERT OR IGNORE INTO menu_layout
					(profile_id, slot_id, parent_id, position, visible)
				VALUES (?, ?, ?, ?, ?)`,
				p.id, item.slotID, item.parentID, maxPos+1, visible,
			)
		}
	}

	log.Printf("[menu_tree_seed] migrated LoopDuration + ConstDuration menu items")
	return nil
}

// nullIfEmpty returns nil if s is empty, otherwise returns s.
// Used to store NULL instead of empty strings in optional TEXT columns.
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// migrateMenuTreeLabels reconciles label drift between releases for
// rows already present in menu_items. The catalog seed uses
// INSERT OR IGNORE so a row written by an earlier server version is
// preserved verbatim, including its now-outdated label_key and
// label_fallback. Without this function, an existing installation
// would never pick up label changes unless the user deleted the
// database — and "delete the DB" is not a viable upgrade path once
// the user has projects and saved files in it.
//
// Each block below targets a specific drift. The WHERE clause must
// be tight enough that the UPDATE turns into a no-op once the row
// already matches the new values, which is what makes the function
// safe to run on every boot.
//
// To document a new drift: add a comment describing what changed
// and why, then an UPDATE statement scoped to the old label_key (or
// the old fallback, whichever uniquely identifies the row's
// pre-migration state). The function returns the first error
// without rolling back earlier successful UPDATEs — that matches
// the broader migrate() contract in db.go, where each step is
// independently idempotent.
//
// Português: Corrige labels de items de menu já gravados quando a
// estratégia muda entre versões. O seed só insere; isto atualiza.
// Idempotente — pode rodar todo boot.
func migrateMenuTreeLabels() error {
	// SysExportGo: had label_key "menuMainExportGo" with fallback
	// "Go Code". The export-by-language refactor (May 2026) moved
	// the language choice into the project itself; the menu now
	// shows a single language-neutral "Export" button and the
	// codegen pipeline reads w.Language at click time. The slot_id
	// is kept ("SysExportGo") so menu_layout doesn't need a
	// parallel rewrite — the maker never sees the slot_id.
	if _, err := DB.Exec(`
		UPDATE menu_items
		   SET label_key      = 'menuMainExport',
		       label_fallback = 'Export'
		 WHERE slot_id        = 'SysExportGo'
		   AND label_key      = 'menuMainExportGo'`); err != nil {
		return err
	}

	return migrateMenuTreeItemTypes()
}

// migrateMenuTreeItemTypes reconciles item_type for the system GROUP slots on
// databases seeded before the linear sidebar existed. In the old design,
// SysExport and SysDisplay were direct actions (they opened an overlay on
// click); the redesign turned every rail group into a submenu with children,
// but INSERT OR IGNORE cannot retroactively fix a catalog row written by an
// earlier version of the seed — the same gap migrateMenuTreeLabels closes for
// labels. A stale 'action' here left the group's Submenu unbuilt on the
// client, so the panel showed the group itself as its only row ("Export ›"
// leading nowhere) — the 2026-07 sidebar bug.
//
// The UPDATE lists every known system group explicitly and only touches rows
// whose type differs, so it is idempotent, runs every boot (called from
// migrateMenuTreeLabels' tail), and never rewrites admin-created slots.
//
// Português: Reconcilia o item_type dos GRUPOS de sistema em bancos populados
// antes da sidebar linear. No design antigo, SysExport e SysDisplay eram
// ações diretas (abriam overlay); o redesign tornou todo grupo do rail um
// submenu com filhos, mas INSERT OR IGNORE não conserta retroativamente uma
// linha escrita por versão anterior do seed — a mesma lacuna que o
// migrateMenuTreeLabels fecha para labels. Um 'action' velho deixava o
// Submenu do grupo sem montar no cliente e o painel mostrava o próprio grupo
// como única linha ("Export ›" sem destino) — o bug da sidebar de 2026-07.
// O UPDATE lista os grupos explicitamente e só toca linhas divergentes:
// idempotente, roda a cada boot e nunca reescreve slots criados pelo admin.
func migrateMenuTreeItemTypes() error {
	_, err := DB.Exec(`
		UPDATE menu_items
		   SET item_type = 'submenu'
		 WHERE slot_id IN ('SysMath','SysLogic','SysLoop','SysConst','SysVar',
		                   'SysDisplay','SysExport','SysMyItems','SysDebug')
		   AND item_type <> 'submenu'`)
	return err
}

// MigrateMenuTreeConstArrays inserts the three constant-collection menu
// items (SysConstArrayInt / Float / String) into existing databases that
// were seeded before these devices existed (docs/claude_const_array_plan.md).
// Fresh installs get the items from the seed above; this migration closes
// the gap for already-seeded databases, which SeedMenuTree deliberately
// skips (count > 0 → early return).
//
// It also REMOVES the short-lived single "SysConstArray" slot (and its i18n
// key) if a database picked it up from the intermediate delivery, before the
// per-type split was decided — no legacy, per project rule. The DELETEs are
// no-ops when the slot never existed.
//
// Mirrors MigrateMenuTreeLoopDuration: INSERT OR IGNORE everywhere, so it is
// idempotent and safe to run on every boot. For each new item three things
// are guaranteed:
//
//  1. The catalog row (menu_items) — system namespace, FontAwesome
//     "layer-group".
//  2. A layout row per EXISTING profile (menu_layout) — visible=1 in the
//     default profile, visible=0 elsewhere, positioned after the last child
//     of SysConst (same stance as InsertCatalogItem for admin-created items).
//  3. The i18n keys for both locales — INSERT OR IGNORE never overwrites an
//     admin-edited message, it only fills the key when absent, so the
//     "admin edits are safe" contract of SeedTranslations is preserved.
//
// Called from migrate() in db.go after SeedMenuTree().
//
// Português: Insere os três itens de coleção constante em bancos existentes
// (populados antes dos devices existirem) e remove o slot único
// "SysConstArray" da entrega intermediária, se presente — sem legado.
// Tudo idempotente: catálogo, layout por profile e chaves i18n, com
// INSERT OR IGNORE, sem sobrescrever edições do admin.
func MigrateMenuTreeConstArrays() error {
	now := time.Now().UTC().Format(time.RFC3339)

	// ── Remove the pre-split single slot, if present ──────────────────────
	for _, stmt := range []string{
		`DELETE FROM menu_layout WHERE slot_id = 'SysConstArray'`,
		`DELETE FROM menu_items  WHERE slot_id = 'SysConstArray'`,
		`DELETE FROM i18n_messages WHERE message_id = 'menuMainConstArray'`,
	} {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}

	// ── Catalog entries ───────────────────────────────────────────────────
	type catItem struct {
		slotID, labelKey, fallback string
	}
	newItems := []catItem{
		{"SysConstArrayInt", "menuMainConstArrayInt", "Int Array"},
		{"SysConstArrayFloat", "menuMainConstArrayFloat", "Float Array"},
		{"SysConstArrayString", "menuMainConstArrayString", "String Array"},
	}
	for _, item := range newItems {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_key, label_fallback,
				 icon_fa, icon_viewbox, created_at)
			VALUES (?, 'system', 'action', 1, ?, ?, 'layer-group', '0 0 512 512', ?)`,
			item.slotID, item.labelKey, item.fallback, now,
		); err != nil {
			return err
		}
	}

	// ── Layout entries (all existing profiles) ────────────────────────────
	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		for _, item := range newItems {
			// Position after the last existing child of SysConst.
			var maxPos int
			if err := DB.QueryRow(`
				SELECT COALESCE(MAX(position), 0) FROM menu_layout
				WHERE profile_id = ? AND parent_id = 'SysConst'`, p.id,
			).Scan(&maxPos); err != nil {
				return err
			}

			visible := 0
			if p.isDefault {
				visible = 1
			}

			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO menu_layout
					(profile_id, slot_id, parent_id, position, visible)
				VALUES (?, ?, 'SysConst', ?, ?)`,
				p.id, item.slotID, maxPos+1, visible,
			); err != nil {
				return err
			}
		}
	}

	// ── i18n keys (fill-if-absent — never overwrites admin edits) ─────────
	// i18n_messages.locale is a foreign key into i18n_bundles, and SQLite's
	// OR IGNORE does NOT swallow FK violations — so the bundle row is
	// ensured first (same INSERT OR IGNORE stance ReplaceBundle takes).
	type i18nKey struct {
		id, en, pt string
	}
	keys := []i18nKey{
		{"menuMainConstArrayInt", "Int Array", "Vetor Int"},
		{"menuMainConstArrayFloat", "Float Array", "Vetor Float"},
		{"menuMainConstArrayString", "String Array", "Vetor String"},
	}
	for _, loc := range []struct{ locale string }{{"en-US"}, {"pt-BR"}} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		for _, k := range keys {
			msg := k.en
			if loc.locale == "pt-BR" {
				msg = k.pt
			}
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO i18n_messages
					(locale, message_id, other, one, description)
				VALUES (?, ?, ?, '', '')`,
				loc.locale, k.id, msg,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

// MigrateMenuTreeIndex inserts the array index reader menu item(s) into
// databases first seeded before the reader existed. SeedMenuTree skips
// everything once the catalog is populated, so new system items must arrive
// through an explicit, idempotent migration.
//
// Mirrors MigrateMenuTreeConstArrays: INSERT OR IGNORE everywhere, safe to run
// on every boot. It guarantees, for the reader, the catalog row (menu_items), a
// layout row per EXISTING profile under SysConst (visible=1 in the default
// profile, 0 elsewhere, positioned after the last existing child of SysConst),
// and the i18n keys for both locales (never overwriting an admin-edited message).
//
// All three element-type readers (int, float, string) ship here; they share the
// same OpIndex codegen and differ only in the element type.
//
// Called from migrate() in db.go after MigrateMenuTreeVariables().
//
// Português: Insere o item do leitor de índice em bancos existentes, populados
// antes do device existir. Tudo idempotente (INSERT OR IGNORE): a linha de
// catálogo, o layout por profile sob SysConst e as chaves i18n. Os três leitores
// (int, float, string) entram aqui; diferem só no tipo do elemento.
func MigrateMenuTreeIndex() error {
	now := time.Now().UTC().Format(time.RFC3339)

	// ── Catalog entries ───────────────────────────────────────────────────
	type catItem struct {
		slotID, labelKey, fallback string
	}
	newItems := []catItem{
		{"SysIndexInt", "menuMainIndexInt", "Index (int)"},
		{"SysIndexFloat", "menuMainIndexFloat", "Index (float)"},
		{"SysIndexString", "menuMainIndexString", "Index (string)"},
	}
	for _, item := range newItems {
		// icon_fa reuses the collection glyph for now (same as the const
		// arrays) — swap it once a distinctive reader icon is chosen.
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_key, label_fallback,
				 icon_fa, icon_viewbox, created_at)
			VALUES (?, 'system', 'action', 1, ?, ?, 'layer-group', '0 0 512 512', ?)`,
			item.slotID, item.labelKey, item.fallback, now,
		); err != nil {
			return err
		}
	}

	// ── Layout entries (all existing profiles) ────────────────────────────
	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		for _, item := range newItems {
			// Position after the last existing child of SysConst.
			var maxPos int
			if err := DB.QueryRow(`
				SELECT COALESCE(MAX(position), 0) FROM menu_layout
				WHERE profile_id = ? AND parent_id = 'SysConst'`, p.id,
			).Scan(&maxPos); err != nil {
				return err
			}

			visible := 0
			if p.isDefault {
				visible = 1
			}

			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO menu_layout
					(profile_id, slot_id, parent_id, position, visible)
				VALUES (?, ?, 'SysConst', ?, ?)`,
				p.id, item.slotID, maxPos+1, visible,
			); err != nil {
				return err
			}
		}
	}

	// ── i18n keys (fill-if-absent — never overwrites admin edits) ─────────
	type i18nKey struct {
		id, en, pt string
	}
	keys := []i18nKey{
		{"menuMainIndexInt", "Index (int)", "Índice (int)"},
		{"menuMainIndexFloat", "Index (float)", "Índice (float)"},
		{"menuMainIndexString", "Index (string)", "Índice (string)"},
	}
	for _, loc := range []struct{ locale string }{{"en-US"}, {"pt-BR"}} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		for _, k := range keys {
			msg := k.en
			if loc.locale == "pt-BR" {
				msg = k.pt
			}
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO i18n_messages
					(locale, message_id, other, one, description)
				VALUES (?, ?, ?, '', '')`,
				loc.locale, k.id, msg,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

// MigrateMenuTreeCase inserts the SysCaseItem (N-way selector container,
// "Case") into databases that were first seeded before the Case device
// existed. SeedMenuTree skips everything once the catalog is populated, so
// new system items must arrive through an explicit, idempotent migration.
//
// Mirrors MigrateMenuTreeConstArrays: INSERT OR IGNORE everywhere, so it is
// safe to run on every boot. Three things are guaranteed:
//
//  1. The catalog row (menu_items) — system namespace, FontAwesome
//     "layer-group".
//  2. A layout row per EXISTING profile (menu_layout) under SysLogic —
//     visible=1 in the default profile, visible=0 elsewhere, positioned
//     after the last existing child of SysLogic.
//  3. The i18n keys for both locales — INSERT OR IGNORE never overwrites an
//     admin-edited message, it only fills the key when absent, preserving
//     the "admin edits are safe" contract of SeedTranslations.
//
// Called from migrate() in db.go after MigrateMenuTreeConstArrays().
//
// Português: Insere o item Case (container seletor N-vias) em bancos
// existentes, populados antes do device existir. Tudo idempotente:
// catálogo, layout por profile sob SysLogic e chaves i18n, com
// INSERT OR IGNORE, sem sobrescrever edições do admin.
func MigrateMenuTreeCase() error {
	now := time.Now().UTC().Format(time.RFC3339)

	// ── Catalog entry ─────────────────────────────────────────────────────
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES ('SysCaseItem', 'system', 'action', 1, 'menuMainCase', 'Case',
		        'layer-group', '0 0 512 512', ?)`,
		now,
	); err != nil {
		return err
	}

	// ── Layout entries (all existing profiles) ────────────────────────────
	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		// Position after the last existing child of SysLogic.
		var maxPos int
		if err := DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id = 'SysLogic'`, p.id,
		).Scan(&maxPos); err != nil {
			return err
		}

		visible := 0
		if p.isDefault {
			visible = 1
		}

		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysCaseItem', 'SysLogic', ?, ?)`,
			p.id, maxPos+1, visible,
		); err != nil {
			return err
		}
	}

	// ── i18n keys (fill-if-absent — never overwrites admin edits) ─────────
	for _, loc := range []struct{ locale string }{{"en-US"}, {"pt-BR"}} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		msg := "Case"
		if loc.locale == "pt-BR" {
			msg = "Caso"
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_messages
				(locale, message_id, other, one, description)
			VALUES (?, 'menuMainCase', ?, '', '')`,
			loc.locale, msg,
		); err != nil {
			return err
		}
	}

	return nil
}

// MigrateMenuTreeSequence inserts the SysSequenceItem (ORDER container,
// "Sequence") into databases seeded before the device existed — the
// embedded ladder's slice 1 (2026-07-16). Same shape as
// MigrateMenuTreeCase: INSERT OR IGNORE everywhere; catalog row, a layout
// row per existing profile under SysLogic (after the last child), and
// fill-if-absent i18n keys. Called from migrate() in db.go right after
// MigrateMenuTreeCase().
//
// Português: Insere o item Sequence (container de ORDEM) em bancos
// existentes — fatia 1 da escada embedded. Tudo idempotente, espelho do
// MigrateMenuTreeCase.
func MigrateMenuTreeSequence() error {
	now := time.Now().UTC().Format(time.RFC3339)

	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES ('SysSequenceItem', 'system', 'action', 1, 'menuMainSequence',
		        'Sequence', 'list-ol', '0 0 512 512', ?)`,
		now,
	); err != nil {
		return err
	}

	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		var maxPos int
		if err := DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id = 'SysLogic'`, p.id,
		).Scan(&maxPos); err != nil {
			return err
		}

		visible := 0
		if p.isDefault {
			visible = 1
		}

		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysSequenceItem', 'SysLogic', ?, ?)`,
			p.id, maxPos+1, visible,
		); err != nil {
			return err
		}
	}

	for _, loc := range []struct{ locale string }{{"en-US"}, {"pt-BR"}} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		msg := "Sequence"
		if loc.locale == "pt-BR" {
			msg = "Sequência"
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_messages
				(locale, message_id, other, one, description)
			VALUES (?, 'menuMainSequence', ?, '', '')`,
			loc.locale, msg,
		); err != nil {
			return err
		}
	}

	return nil
}

// MigrateMenuTreeEmbedded inserts the "Arduino / Embedded" ROOT category
// (SysEmbedded) and its first child, the Function device
// (SysFunctionItem), into databases seeded before slice 2 existed
// (2026-07-16). Same idempotent shape as MigrateMenuTreeSequence; the
// category lands after the last ROOT entry, the leaf as its first
// child. Português: Insere a categoria-raiz "Arduino / Embedded" e a
// folha Function em bancos existentes — tudo idempotente.
func MigrateMenuTreeEmbedded() error {
	now := time.Now().UTC().Format(time.RFC3339)

	for _, it := range []struct {
		slot, itemType, key, fallback, icon, viewbox string
	}{
		{"SysEmbedded", "submenu", "menuMainEmbedded", "Arduino / Embedded", "microchip", "0 0 512 512"},
		{"SysFunctionItem", "action", "menuMainFunction", "Function", "florin-sign", "0 0 384 512"},
	} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_key, label_fallback,
				 icon_fa, icon_viewbox, created_at)
			VALUES (?, 'system', ?, 1, ?, ?, ?, ?, ?)`,
			it.slot, it.itemType, it.key, it.fallback, it.icon, it.viewbox, now,
		); err != nil {
			return err
		}
	}

	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		visible := 0
		if p.isDefault {
			visible = 1
		}

		var maxRoot int
		if err := DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id = ''`, p.id,
		).Scan(&maxRoot); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysEmbedded', '', ?, ?)`,
			p.id, maxRoot+1, visible,
		); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysFunctionItem', 'SysEmbedded', 1, ?)`,
			p.id, visible,
		); err != nil {
			return err
		}
	}

	for _, loc := range []struct{ locale, cat, fn string }{
		{"en-US", "Arduino / Embedded", "Function"},
		{"pt-BR", "Arduino / Embarcados", "Função"},
	} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		for key, msg := range map[string]string{
			"menuMainEmbedded": loc.cat,
			"menuMainFunction": loc.fn,
		} {
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO i18n_messages
					(locale, message_id, other, one, description)
				VALUES (?, ?, ?, '', '')`,
				loc.locale, key, msg,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

// MigrateMenuTreeVariables ensures the Variables submenu (SysVar) and its
// device children (SysGetVarInt, SysGetVarFloat, …) exist on databases that
// were seeded before these items were added. SeedMenuTree only runs on a fresh
// menu_items table, so — exactly like MigrateMenuTreeConstArrays and
// MigrateMenuTreeCase — an always-run migration is required for the entries to
// reach existing installations (the "dead migration" trap). Every statement is
// INSERT OR IGNORE, so the migration is idempotent and never disturbs admin
// edits or existing positions.
//
// Português: Garante o submenu Variables (SysVar) e seus filhos (SysGetVarInt,
// SysGetVarFloat, …) em bancos semeados antes desses itens existirem.
// SeedMenuTree só roda com menu_items vazio, então — como ConstArrays e Case —
// é preciso uma migração que sempre roda para os itens chegarem a instalações
// existentes (a armadilha da "dead migration"). Tudo é INSERT OR IGNORE:
// idempotente, sem mexer em edições de admin nem em posições existentes.
func MigrateMenuTreeVariables() error {
	now := time.Now().UTC().Format(time.RFC3339)

	// ── Catalog: the SysVar category + its device children ────────────────
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES ('SysVar', 'system', 'submenu', 1, 'menuMainVar', 'Variables',
		        'suitcase', '0 0 512 512', ?)`, now,
	); err != nil {
		return err
	}

	type catItem struct {
		slotID, labelKey, fallback string
	}
	children := []catItem{
		{"SysSetVarInt", "menuMainSetVarInt", "Set Int"},
		{"SysGetVarInt", "menuMainGetVarInt", "Get Int"},
		{"SysSetVarFloat", "menuMainSetVarFloat", "Set Float"},
		{"SysGetVarFloat", "menuMainGetVarFloat", "Get Float"},
		{"SysSetVarString", "menuMainSetVarString", "Set String"},
		{"SysGetVarString", "menuMainGetVarString", "Get String"},
	}
	for _, c := range children {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_key, label_fallback,
				 icon_fa, icon_viewbox, created_at)
			VALUES (?, 'system', 'action', 1, ?, ?, 'bars', '0 0 448 512', ?)`,
			c.slotID, c.labelKey, c.fallback, now,
		); err != nil {
			return err
		}
	}

	// ── Layout (all profiles) ─────────────────────────────────────────────
	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		visible := 0
		if p.isDefault {
			visible = 1
		}

		// SysVar at the rail root. Appended after the last existing root item
		// only when absent — on a seeded DB it already sits at its slot and the
		// INSERT OR IGNORE is a no-op.
		var rootMax int
		if err := DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND (parent_id = '' OR parent_id IS NULL)`, p.id,
		).Scan(&rootMax); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysVar', '', ?, ?)`,
			p.id, rootMax+1, visible,
		); err != nil {
			return err
		}

		// Children under SysVar, each positioned after the last existing child.
		for _, c := range children {
			var maxPos int
			if err := DB.QueryRow(`
				SELECT COALESCE(MAX(position), 0) FROM menu_layout
				WHERE profile_id = ? AND parent_id = 'SysVar'`, p.id,
			).Scan(&maxPos); err != nil {
				return err
			}
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO menu_layout
					(profile_id, slot_id, parent_id, position, visible)
				VALUES (?, ?, 'SysVar', ?, ?)`,
				p.id, c.slotID, maxPos+1, visible,
			); err != nil {
				return err
			}
		}

		// Reorder the children into the grouped layout (Set+Get of each type
		// together, Set first). The first release positioned all Gets, then all
		// Sets; the INSERT OR IGNORE above cannot move rows that already exist,
		// so this UPDATE repositions them on DBs seeded with the old order.
		// Positions follow the children slice. Idempotent — re-running just
		// re-sets the same values.
		//
		// Português: Reordena os filhos no layout agrupado (Set+Get de cada tipo
		// juntos, Set primeiro). INSERT OR IGNORE não move linhas existentes,
		// então este UPDATE reposiciona em DBs com a ordem antiga. As posições
		// seguem o slice children. Idempotente.
		for i, c := range children {
			if _, err := DB.Exec(`
				UPDATE menu_layout SET position = ?
				WHERE profile_id = ? AND slot_id = ? AND parent_id = 'SysVar'`,
				i+1, p.id, c.slotID,
			); err != nil {
				return err
			}
		}
	}

	// ── i18n keys (fill-if-absent — never overwrites admin edits) ─────────
	// i18n_messages.locale is a foreign key into i18n_bundles, so the bundle is
	// ensured first (OR IGNORE does not swallow FK violations).
	type i18nKey struct {
		id, en, pt string
	}
	keys := []i18nKey{
		{"menuMainVar", "Variables", "Variáveis"},
		{"menuMainGetVarInt", "Get Int", "Ler Int"},
		{"menuMainGetVarFloat", "Get Float", "Ler Float"},
		{"menuMainSetVarInt", "Set Int", "Gravar Int"},
		{"menuMainSetVarFloat", "Set Float", "Gravar Float"},
		{"menuMainGetVarString", "Get String", "Ler String"},
		{"menuMainSetVarString", "Set String", "Gravar String"},
	}
	for _, loc := range []struct{ locale string }{{"en-US"}, {"pt-BR"}} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		for _, k := range keys {
			msg := k.en
			if loc.locale == "pt-BR" {
				msg = k.pt
			}
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO i18n_messages
					(locale, message_id, other, one, description)
				VALUES (?, ?, ?, '', '')`,
				loc.locale, k.id, msg,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

// MigrateMenuTreePrint inserts the SysDebug group and the six Print sink
// devices (int, float, string, bool, byte, []byte) into databases seeded
// before the Debug family existed. Unlike the previous migrations — which
// added items under EXISTING groups — this one also creates the GROUP:
// a root-level submenu row in the catalog plus one layout row per profile,
// positioned after the last existing root item (movable later via the
// profile editor). SeedMenuTree skips everything once the catalog is
// populated, so new system items must arrive through an explicit,
// idempotent migration; INSERT OR IGNORE everywhere makes every-boot
// execution safe.
//
// icon_fa follows the seed's convention (FontAwesome NAME for the Control
// Panel; the WASM builder carries its own path data): "bug" for the group,
// "print" for the items.
//
// Português: Insere o grupo SysDebug e os seis devices Print (int, float,
// string, bool, byte, []byte) em bancos populados antes da família Debug
// existir. Diferente das migrações anteriores — que adicionavam itens em
// grupos EXISTENTES — esta também cria o GRUPO: uma linha de submenu na
// raiz do catálogo mais uma linha de layout por profile, posicionada após
// o último item raiz (movível depois pelo editor de profiles). INSERT OR
// IGNORE em tudo torna a execução a cada boot segura e idempotente.
// icon_fa segue a convenção do seed ("bug" no grupo, "print" nos itens).
func MigrateMenuTreePrint() error {
	now := time.Now().UTC().Format(time.RFC3339)

	// ── Catalog entries ───────────────────────────────────────────────────
	// The group first, then the six actions.
	// Português: O grupo primeiro, depois as seis ações.
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES ('SysDebug', 'system', 'submenu', 1, 'menuMainDebug', 'Debug',
		        'bug', '0 0 512 512', ?)`, now,
	); err != nil {
		return err
	}

	type catItem struct {
		slotID, labelKey, fallback string
	}
	newItems := []catItem{
		{"SysPrintInt", "menuMainPrintInt", "Print (int)"},
		{"SysPrintFloat", "menuMainPrintFloat", "Print (float)"},
		{"SysPrintString", "menuMainPrintString", "Print (string)"},
		{"SysPrintBool", "menuMainPrintBool", "Print (bool)"},
		{"SysPrintByte", "menuMainPrintByte", "Print (byte)"},
		{"SysPrintByteArray", "menuMainPrintByteArray", "Print ([]byte)"},
	}
	for _, item := range newItems {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_items
				(slot_id, slot_type, item_type, locked, label_key, label_fallback,
				 icon_fa, icon_viewbox, created_at)
			VALUES (?, 'system', 'action', 1, ?, ?, 'print', '0 0 512 512', ?)`,
			item.slotID, item.labelKey, item.fallback, now,
		); err != nil {
			return err
		}
	}

	// ── Layout entries (all existing profiles) ────────────────────────────
	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		visible := 0
		if p.isDefault {
			visible = 1
		}

		// The group sits at the ROOT (parent_id IS NULL), after the last
		// existing root item — position queries must use IS NULL, never
		// = '' (the seed stores root parents as SQL NULL).
		// Português: O grupo fica na RAIZ (parent_id IS NULL), após o
		// último item raiz — a query de posição usa IS NULL, nunca = ''
		// (o seed grava pai raiz como NULL SQL).
		var maxRoot int
		if err := DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id IS NULL`, p.id,
		).Scan(&maxRoot); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysDebug', NULL, ?, ?)`,
			p.id, maxRoot+1, visible,
		); err != nil {
			return err
		}

		// The six actions, in the declared order, under the new group.
		// Português: As seis ações, na ordem declarada, sob o grupo novo.
		for i, item := range newItems {
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO menu_layout
					(profile_id, slot_id, parent_id, position, visible)
				VALUES (?, ?, 'SysDebug', ?, ?)`,
				p.id, item.slotID, i+1, visible,
			); err != nil {
				return err
			}
		}
	}

	// ── i18n keys (fill-if-absent — never overwrites admin edits) ─────────
	type i18nKey struct {
		id, en, pt string
	}
	keys := []i18nKey{
		{"menuMainDebug", "Debug", "Depuração"},
		{"menuMainPrintInt", "Print (int)", "Imprimir (int)"},
		{"menuMainPrintFloat", "Print (float)", "Imprimir (float)"},
		{"menuMainPrintString", "Print (string)", "Imprimir (string)"},
		{"menuMainPrintBool", "Print (bool)", "Imprimir (bool)"},
		{"menuMainPrintByte", "Print (byte)", "Imprimir (byte)"},
		{"menuMainPrintByteArray", "Print ([]byte)", "Imprimir ([]byte)"},
	}
	for _, loc := range []struct{ locale string }{{"en-US"}, {"pt-BR"}} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		for _, k := range keys {
			msg := k.en
			if loc.locale == "pt-BR" {
				msg = k.pt
			}
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO i18n_messages
					(locale, message_id, other, one, description)
				VALUES (?, ?, ?, '', '')`,
				loc.locale, k.id, msg,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

// MigrateMenuTreeDebugPosition moves the SysDebug group ABOVE SysExport in
// every profile's rail (owner request, 2026-07: "every device-category icon
// sits above Export"). MigrateMenuTreePrint appends new groups at the rail's
// END — the safe default for unknown layouts — which landed Debug below
// Export/Settings/Exit; this migration performs the one repositioning that
// default cannot know about.
//
// Per profile: when both SysDebug and SysExport sit at the root and Debug is
// BELOW Export, every root item from Export downward shifts one slot down and
// Debug takes Export's old position. The guard (debugPos > exportPos) makes
// it idempotent — a second boot, or a rail the admin already reordered with
// Debug above Export, is left untouched.
//
// Português: Move o grupo SysDebug para CIMA do SysExport no rail de todos
// os profiles (pedido do dono, 2026-07: "toda categoria de devices fica acima
// do Export"). O MigrateMenuTreePrint anexa grupos novos no FIM do rail — o
// default seguro para layouts desconhecidos — o que deixou o Debug abaixo de
// Export/Settings/Exit; esta migração faz o único reposicionamento que esse
// default não tem como saber. Por profile: com SysDebug e SysExport na raiz
// e Debug ABAIXO do Export, tudo do Export para baixo desce um slot e o
// Debug assume a posição antiga do Export. A guarda (debugPos > exportPos)
// torna tudo idempotente — segundo boot, ou rail que o admin já reordenou
// com Debug acima, fica intocado.
func MigrateMenuTreeDebugPosition() error {
	rows, err := DB.Query(`SELECT profile_id FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var profiles []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		profiles = append(profiles, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, profileID := range profiles {
		var debugPos, exportPos int

		err := DB.QueryRow(`
			SELECT position FROM menu_layout
			WHERE profile_id = ? AND slot_id = 'SysDebug' AND parent_id IS NULL`,
			profileID,
		).Scan(&debugPos)
		if err != nil {
			continue // no root Debug in this profile — nothing to move
		}
		err = DB.QueryRow(`
			SELECT position FROM menu_layout
			WHERE profile_id = ? AND slot_id = 'SysExport' AND parent_id IS NULL`,
			profileID,
		).Scan(&exportPos)
		if err != nil {
			continue // no root Export — no anchor to move above
		}
		if debugPos <= exportPos {
			continue // already above (or at) Export — idempotent no-op
		}

		// Shift Export and everything below it one slot down, then drop
		// Debug into Export's old position. The shift includes Debug's own
		// row (harmless: it is overwritten right after), keeping this a
		// two-statement move with no gaps and no duplicates.
		// Português: Desce Export e tudo abaixo um slot e põe o Debug na
		// posição antiga do Export. O shift inclui a própria linha do Debug
		// (inofensivo: é sobrescrita logo em seguida) — dois statements,
		// sem buracos e sem duplicatas.
		if _, err := DB.Exec(`
			UPDATE menu_layout SET position = position + 1
			WHERE profile_id = ? AND parent_id IS NULL AND position >= ?`,
			profileID, exportPos,
		); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			UPDATE menu_layout SET position = ?
			WHERE profile_id = ? AND slot_id = 'SysDebug' AND parent_id IS NULL`,
			exportPos, profileID,
		); err != nil {
			return err
		}
	}
	return nil
}

// MigrateMenuTreeData ensures the DATA category (SysData submenu) and its
// first child (SysDataFile) exist on databases seeded before the category
// was born (2026-07-13). Fresh installs get both from the seed above;
// SeedMenuTree deliberately skips populated tables, so — exactly like
// MigrateMenuTreeVariables — this always-run, INSERT-OR-IGNORE migration
// closes the gap for existing databases. The SysData root lands after the
// last existing root; Exit stays visually last because buildFromTree
// defers it regardless of position.
//
// Called from migrate() in db.go after MigrateMenuTreeVariables().
//
// Português: Garante a categoria DATA (submenu SysData) e seu primeiro
// filho (SysDataFile) em bancos semeados antes da categoria nascer.
// Instalações novas ganham ambos do seed acima; esta migração idempotente
// fecha a lacuna dos bancos existentes. A raiz SysData cai após a última
// raiz existente; o Exit segue visualmente por último porque o
// buildFromTree o adia independentemente da posição.
func MigrateMenuTreeData() error {
	now := time.Now().UTC().Format(time.RFC3339)

	// ── Catalog entries ───────────────────────────────────────────────────
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES ('SysData', 'system', 'submenu', 1, 'menuMainData', 'Data',
		        'file-export', '0 0 512 512', ?)`,
		now,
	); err != nil {
		return err
	}
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES ('SysDataFile', 'system', 'action', 1, 'menuDataFile', 'File',
		        'file-export', '0 0 512 512', ?)`,
		now,
	); err != nil {
		return err
	}
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO menu_items
			(slot_id, slot_type, item_type, locked, label_key, label_fallback,
			 icon_fa, icon_viewbox, created_at)
		VALUES ('SysDataText', 'system', 'action', 1, 'menuDataText', 'Text',
		        'pen', '0 0 512 512', ?)`,
		now,
	); err != nil {
		return err
	}

	// ── Layout entries (all existing profiles) ────────────────────────────
	rows, err := DB.Query(`SELECT profile_id, is_default FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type prof struct {
		id        string
		isDefault bool
	}
	var profiles []prof
	for rows.Next() {
		var p prof
		var isDef int
		if err := rows.Scan(&p.id, &isDef); err != nil {
			return err
		}
		p.isDefault = isDef == 1
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		visible := 0
		if p.isDefault {
			visible = 1
		}

		// SysData root — after the last existing root (parent IS NULL).
		var maxRoot int
		if err := DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id IS NULL`, p.id,
		).Scan(&maxRoot); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysData', NULL, ?, ?)`,
			p.id, maxRoot+1, visible,
		); err != nil {
			return err
		}

		// SysDataFile — first child of SysData.
		var maxChild int
		if err := DB.QueryRow(`
			SELECT COALESCE(MAX(position), 0) FROM menu_layout
			WHERE profile_id = ? AND parent_id = 'SysData'`, p.id,
		).Scan(&maxChild); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysDataFile', 'SysData', ?, ?)`,
			p.id, maxChild+1, visible,
		); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO menu_layout
				(profile_id, slot_id, parent_id, position, visible)
			VALUES (?, 'SysDataText', 'SysData', ?, ?)`,
			p.id, maxChild+2, visible,
		); err != nil {
			return err
		}
	}

	// ── i18n keys (fill-if-absent — never overwrites admin edits) ─────────
	for _, loc := range []struct{ locale string }{{"en-US"}, {"pt-BR"}} {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			loc.locale, loc.locale+"-custom", now,
		); err != nil {
			return err
		}
		catMsg, fileMsg, textMsg := "Data", "File", "Text"
		if loc.locale == "pt-BR" {
			catMsg, fileMsg, textMsg = "Dados", "Arquivo", "Texto"
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_messages
				(locale, message_id, other, one, description)
			VALUES (?, 'menuMainData', ?, '', '')`,
			loc.locale, catMsg,
		); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_messages
				(locale, message_id, other, one, description)
			VALUES (?, 'menuDataFile', ?, '', '')`,
			loc.locale, fileMsg,
		); err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_messages
				(locale, message_id, other, one, description)
			VALUES (?, 'menuDataText', ?, '', '')`,
			loc.locale, textMsg,
		); err != nil {
			return err
		}
	}

	log.Printf("[menu_tree_seed] migrated Data category (SysData + SysDataFile + SysDataText)")
	return nil
}

// MigrateMenuLayoutGraphicalAboveDebug enforces the rail-ordering law
// (Kemper 2026-07-19): every GRAPHICAL category sits ABOVE Debug —
// Arduino/Embedded included — with the administrative tail (Export,
// Settings, My Items, Exit) after. Deterministic positions via
// idempotent UPDATEs; a profile missing a slot is a no-op for it.
// Note: My Items' visual placement (directly below Data) is enforced
// client-side by the MenuBuilder's placement law; its DB position here
// only matters as a fallback. Português: Aplica a lei de ordenação do
// trilho — todo ícone GRÁFICO acima de Debug (Arduino incluso), cauda
// administrativa depois. UPDATEs determinísticos e idempotentes.
func MigrateMenuLayoutGraphicalAboveDebug() error {
	order := []struct {
		slot string
		pos  int
	}{
		{"SysMath", 1},
		{"SysLogic", 2},
		{"SysLoop", 3},
		{"SysConst", 4},
		{"SysVar", 5},
		{"SysDisplay", 6},
		{"SysData", 7},
		{"SysEmbedded", 8},
		{"SysDebug", 9},
		{"SysExport", 10},
		{"SysSettings", 11},
		{"SysMyItems", 12},
		{"SysExit", 13},
	}

	rows, err := DB.Query(`SELECT profile_id FROM menu_profiles`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var profiles []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		profiles = append(profiles, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, pid := range profiles {
		for _, o := range order {
			if _, err := DB.Exec(`
				UPDATE menu_layout SET position = ?
				WHERE profile_id = ? AND parent_id = '' AND slot_id = ?`,
				o.pos, pid, o.slot,
			); err != nil {
				return err
			}
		}
	}
	return nil
}
