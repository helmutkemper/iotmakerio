// server/store/menu_help_migrate.go — Migrate legacy help .md files to menu_help table.
//
// The legacy system stored help markdown files on disk under:
//
//	server/public/help/devices/{category}/{slotName}/{locale}.md   (localized)
//	server/public/help/devices/{category}/statement{Name}.md       (single file, en)
//
// This function reads those files and inserts them into the menu_help table
// so the tree endpoint can resolve help markdown from the database.
//
// The migration is idempotent: existing rows are not overwritten.
// Called once at server startup after SeedMenuTree().
package store

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// helpFileMapping maps a legacy filename pattern to a menu slot_id.
// The key is the stem used in disk filenames (e.g. "statementAdd" → "SysAdd").
var helpFileMapping = map[string]string{
	// Math
	"statementAdd": "SysAdd",
	"Add":          "SysAdd",

	// Display
	"statementGauge":       "SysGauge",
	"statementLED":         "SysLED",
	"statementBarGraph":    "SysBarGraph",
	"statementTextDisplay": "SysTextDisplay",
	"statementButton":      "SysButton",
	"statementSevenSeg":    "SysSevenSeg",
	"statementKnob":        "SysKnob",
	"statementChart":       "SysChart",
	"statementChartPro":    "SysChartPro",
	"statementPieChart":    "SysPieChart",
}

// MigrateHelpFilesToDB scans the legacy help directory and inserts any
// .md files into the menu_help table. Existing entries are not overwritten.
//
// helpDir is typically config.StaticDir + "/help/devices".
func MigrateHelpFilesToDB(helpDir string) {
	if helpDir == "" {
		return
	}

	// Check if the directory exists.
	info, err := os.Stat(helpDir)
	if err != nil || !info.IsDir() {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	inserted := 0

	// Walk the help directory looking for .md files.
	err = filepath.Walk(helpDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil || fi.IsDir() {
			return nil
		}
		if !strings.HasSuffix(fi.Name(), ".md") {
			return nil
		}

		// Determine slot_id and locale from the path.
		slotID, locale := resolveHelpSlotAndLocale(helpDir, path)
		if slotID == "" {
			return nil
		}

		// Read file content.
		content, readErr := os.ReadFile(path)
		if readErr != nil || len(content) == 0 {
			return nil
		}

		// Insert only if no entry exists for this (slot, profile="", locale).
		var existing int
		DB.QueryRow(`
			SELECT COUNT(*) FROM menu_help
			WHERE slot_id = ? AND profile_id = '' AND locale = ?`,
			slotID, locale,
		).Scan(&existing)

		if existing == 0 {
			_, err := DB.Exec(`
				INSERT INTO menu_help (slot_id, profile_id, locale, markdown, updated_at)
				VALUES (?, '', ?, ?, ?)`,
				slotID, locale, string(content), now,
			)
			if err == nil {
				inserted++
			}
		}

		return nil
	})

	if err != nil {
		log.Printf("[menu_help_migrate] walk error: %v", err)
	}

	if inserted > 0 {
		log.Printf("[menu_help_migrate] migrated %d help file(s) from disk to database", inserted)
	}
}

// resolveHelpSlotAndLocale determines the slot_id and locale from a file path.
//
// Two patterns are supported:
//
//  1. Localized: {helpDir}/{category}/{SlotName}/{locale}.md
//     Example:   help/devices/math/Add/en.md        → SysAdd, "en"
//     Example:   help/devices/math/Add/pt-br.md     → SysAdd, "pt"
//
//  2. Single file: {helpDir}/{category}/statement{Name}.md
//     Example:   help/devices/display/statementGauge.md → SysGauge, "en"
func resolveHelpSlotAndLocale(helpDir, fullPath string) (slotID, locale string) {
	// Get the relative path from the help directory.
	rel, err := filepath.Rel(helpDir, fullPath)
	if err != nil {
		return "", ""
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")

	switch len(parts) {
	case 3:
		// Pattern 1: {category}/{SlotName}/{locale}.md
		// e.g. math/Add/en.md
		stem := parts[1]
		loc := strings.TrimSuffix(parts[2], ".md")

		sid, ok := helpFileMapping[stem]
		if !ok {
			return "", ""
		}

		// Normalize locale: "pt-br" → "pt"
		if idx := strings.IndexByte(loc, '-'); idx > 0 {
			loc = loc[:idx]
		}

		return sid, loc

	case 2:
		// Pattern 2: {category}/statement{Name}.md
		// e.g. display/statementGauge.md
		stem := strings.TrimSuffix(parts[1], ".md")

		sid, ok := helpFileMapping[stem]
		if !ok {
			return "", ""
		}

		return sid, "en" // single file = english

	default:
		return "", ""
	}
}
