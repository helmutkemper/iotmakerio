// server/store/parser_limits.go — Resolver for parser complexity limits.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Parser limits control the maximum structural complexity of a single black-box
// or template device file: number of methods, ports, and prop-tagged fields.
//
// Resolution order (highest priority first):
//
//  1. Per-user override — row in user_parser_limits for (userID, key).
//  2. Global setting    — row in project_settings for key.
//  3. Hard fallback     — blackbox.DefaultParserLimits() (compile-time constant).
//
// The hard fallback guarantees the parser always has a valid limit even if the
// database is unavailable or a key has been accidentally deleted. Its values
// match the seed values in db_parser_limits.go so behaviour is identical before
// and after the first server start.
//
// Usage:
//
//	limits := store.GetParserLimits(userID)
//	def, err  := bbparser.Parse(src, limits)
package store

import (
	"strconv"

	bbparser "server/codegen/blackbox"
)

// GetParserLimits resolves all four parser limits for the given user and
// returns a bbparser.ParserLimits value ready to pass to bbparser.Parse.
//
// When userID is empty (anonymous caller, background worker without a specific
// user, or tests), only the global settings and hard fallbacks are consulted.
//
// This function never returns an error. Any database failure falls through to
// the next resolution layer so the caller always receives a complete,
// positive-integer ParserLimits value.
func GetParserLimits(userID string) bbparser.ParserLimits {
	def := bbparser.DefaultParserLimits()
	return bbparser.ParserLimits{
		MaxMethods: resolveParserLimit(userID, SettingParserMaxMethods, def.MaxMethods),
		MaxInputs:  resolveParserLimit(userID, SettingParserMaxInputs, def.MaxInputs),
		MaxOutputs: resolveParserLimit(userID, SettingParserMaxOutputs, def.MaxOutputs),
		MaxProps:   resolveParserLimit(userID, SettingParserMaxProps, def.MaxProps),
	}
}

// resolveParserLimit returns the effective limit for (userID, key) using the
// three-layer resolution order: user override → global setting → fallback.
func resolveParserLimit(userID, key string, fallback int) int {
	// Layer 1: per-user override.
	if userID != "" {
		if v, ok := getUserParserLimitInt(userID, key); ok {
			return positiveOrFallback(v, fallback)
		}
	}

	// Layer 2: global setting.
	if v := GetSettingInt(key, 0); v > 0 {
		return v
	}

	// Layer 3: hard fallback.
	return fallback
}

// getUserParserLimitInt looks up a single per-user limit from user_parser_limits.
// Returns (value, true) when a row exists and the value is a valid positive integer.
// Returns (0, false) on any error (missing row, parse failure, DB error).
func getUserParserLimitInt(userID, key string) (int, bool) {
	var raw string
	if err := DB.QueryRow(
		`SELECT value FROM user_parser_limits WHERE user_id = ? AND limit_key = ?`,
		userID, key,
	).Scan(&raw); err != nil {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// positiveOrFallback returns v when v > 0, otherwise fallback. Prevents a
// misconfigured limit of 0 from rejecting every parse input.
func positiveOrFallback(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

// ─── Admin write helpers ──────────────────────────────────────────────────────

// SetUserParserLimit upserts a per-user override for the given limit key.
// note is an optional admin annotation (e.g. "trusted specialist").
// Passing value ≤ 0 removes the override — equivalent to DeleteUserParserLimit.
func SetUserParserLimit(userID, key string, value int, note string) error {
	if value <= 0 {
		return DeleteUserParserLimit(userID, key)
	}
	_, err := DB.Exec(`
		INSERT INTO user_parser_limits (user_id, limit_key, value, note, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT(user_id, limit_key) DO UPDATE SET
			value      = excluded.value,
			note       = excluded.note,
			updated_at = excluded.updated_at`,
		userID, key, strconv.Itoa(value), note,
	)
	return err
}

// DeleteUserParserLimit removes the per-user override for the given key,
// restoring the user to the global default. No-op if no override exists.
func DeleteUserParserLimit(userID, key string) error {
	_, err := DB.Exec(
		`DELETE FROM user_parser_limits WHERE user_id = ? AND limit_key = ?`,
		userID, key,
	)
	return err
}

// ListUserParserLimits returns all per-user overrides for the given user,
// ordered by limit key. Returns nil (not an empty slice) when no overrides exist.
func ListUserParserLimits(userID string) ([]*UserParserLimit, error) {
	rows, err := DB.Query(`
		SELECT user_id, limit_key, value, note, updated_at
		FROM user_parser_limits
		WHERE user_id = ?
		ORDER BY limit_key ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var limits []*UserParserLimit
	for rows.Next() {
		var l UserParserLimit
		if err := rows.Scan(
			&l.UserID, &l.LimitKey, &l.Value, &l.Note, &l.UpdatedAt,
		); err != nil {
			return nil, err
		}
		limits = append(limits, &l)
	}
	return limits, rows.Err()
}
