// store/settings.go — Read/write access to the project_settings table.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings are server-side configurable values that control limits and feature
// flags. They are seeded on startup and can be changed at runtime by editing
// the database directly — no API or admin UI is needed.
//
// All setting key constants are defined in models.go so they are accessible
// from any package that imports store without creating a circular dependency.
//
// Usage pattern:
//
//	maxChars := store.GetSettingInt(store.SettingCardDescriptionMaxChars, 500)
//
// The default value is returned when the key does not exist or the stored
// value cannot be parsed, making the call safe even before the seed runs.
package store

import (
	"strconv"
	"time"
)

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetSettingInt returns the integer value of key.
// Returns defaultValue if the key does not exist or its stored value cannot
// be parsed as an integer. Never returns an error — a missing or malformed
// setting falls back gracefully to the compiled-in default.
func GetSettingInt(key string, defaultValue int) int {
	var raw string
	err := DB.QueryRow(
		`SELECT value FROM project_settings WHERE key = ?`, key,
	).Scan(&raw)
	if err != nil {
		return defaultValue
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return v
}

// GetSettingString returns the string value of key.
// Returns defaultValue if the key does not exist.
func GetSettingString(key, defaultValue string) string {
	var raw string
	err := DB.QueryRow(
		`SELECT value FROM project_settings WHERE key = ?`, key,
	).Scan(&raw)
	if err != nil {
		return defaultValue
	}
	return raw
}

// ─── Write ────────────────────────────────────────────────────────────────────

// SetSetting upserts a setting value. The description is only written on
// INSERT — an UPDATE preserves the existing description so that admin
// annotations added at runtime are not overwritten by server restarts.
func SetSetting(key, value string) error {
	_, err := DB.Exec(`
		INSERT INTO project_settings (key, value, description, updated_at)
		VALUES (?, ?, '', datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
			value      = excluded.value,
			updated_at = excluded.updated_at`,
		key, value,
	)
	return err
}

// ListSettings returns all settings ordered by key.
func ListSettings() ([]*ProjectSetting, error) {
	rows, err := DB.Query(`
		SELECT key, value, description, updated_at
		FROM project_settings
		ORDER BY key ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []*ProjectSetting
	for rows.Next() {
		var s ProjectSetting
		var updatedAt string
		if err := rows.Scan(&s.Key, &s.Value, &s.Description, &updatedAt); err != nil {
			return nil, err
		}
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		settings = append(settings, &s)
	}
	return settings, rows.Err()
}
