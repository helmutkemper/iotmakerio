// server/store/panel_prefs.go — CRUD for IDE panel column widths.
//
// Each user can save their preferred rail/list column widths per OS+browser.
// The preview column (3rd) fills the remaining space automatically.
//
// Schema:
//
//	user_panel_prefs (
//	    user_id    TEXT NOT NULL,
//	    os         TEXT NOT NULL,      -- e.g. "macos", "windows", "linux"
//	    browser    TEXT NOT NULL,      -- e.g. "chrome", "firefox", "safari"
//	    rail_width INTEGER DEFAULT 96,
//	    list_width INTEGER DEFAULT 250,
//	    updated_at TEXT NOT NULL,
//	    PRIMARY KEY (user_id, os, browser)
//	)
package store

import "time"

// PanelPrefs holds the saved column widths for a user's OS+browser combination.
type PanelPrefs struct {
	UserID    string `json:"user_id,omitempty"`
	OS        string `json:"os"`
	Browser   string `json:"browser"`
	RailWidth int    `json:"rail_width"`
	ListWidth int    `json:"list_width"`
}

// GetPanelPrefs returns the saved column widths for a user+OS+browser.
// Returns nil (no error) if no row exists — the caller uses defaults.
func GetPanelPrefs(userID, os, browser string) (*PanelPrefs, error) {
	p := &PanelPrefs{}
	err := DB.QueryRow(`
		SELECT rail_width, list_width
		FROM user_panel_prefs
		WHERE user_id = ? AND os = ? AND browser = ?`,
		userID, os, browser,
	).Scan(&p.RailWidth, &p.ListWidth)

	if err != nil {
		return nil, nil // no row = use defaults
	}

	p.UserID = userID
	p.OS = os
	p.Browser = browser
	return p, nil
}

// UpsertPanelPrefs creates or updates the column widths for a user+OS+browser.
func UpsertPanelPrefs(userID, os, browser string, railWidth, listWidth int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO user_panel_prefs (user_id, os, browser, rail_width, list_width, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, os, browser) DO UPDATE SET
			rail_width = excluded.rail_width,
			list_width = excluded.list_width,
			updated_at = excluded.updated_at`,
		userID, os, browser, railWidth, listWidth, now,
	)
	return err
}

// DeleteAllPanelPrefs removes all saved panel widths for a user across
// all OS+browser combinations. Used by admin to reset a user's layout.
func DeleteAllPanelPrefs(userID string) (int64, error) {
	res, err := DB.Exec(`DELETE FROM user_panel_prefs WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
