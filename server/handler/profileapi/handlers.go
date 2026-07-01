// /ide/server/handler/profileapi/handlers.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// handler/profileapi/handlers.go — User profile management implementations.
//
// Routes handled here:
//
//	GET  /api/v1/profile              — authenticated user's own full profile
//	PUT  /api/v1/profile              — update display_name, bio, github_url, website_url
//	PUT  /api/v1/profile/locale       — update the user's preferred UI locale
//	POST /api/v1/profile/avatar       — upload a new avatar image
//	GET  /api/v1/profile/invites      — list invites created by the authenticated user
//	GET  /api/v1/users/:username      — public profile (no auth required)
//
// Avatar storage:
//
//	{UserFilesDir}/{userID}/avatar/avatar.{ext}
//
// The avatar directory holds at most one file at a time. Uploading a new
// avatar clears the directory before writing the new file.
//
// Validation limits are read from project_settings so they can be adjusted
// at runtime without redeploying the server.
package profileapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"server/config"
	"server/handler/spaauth"
	"server/store"

	"github.com/labstack/echo/v4"
)

// ─── Own profile ──────────────────────────────────────────────────────────────

// handleGetOwnProfile returns the authenticated user's profile.
// EnsureProfile is called first so that users who registered before the profile
// feature was introduced always get a valid (empty) profile response.
func handleGetOwnProfile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	if err := store.EnsureProfile(claims.UserID); err != nil {
		return fail(c, 500, "internal error")
	}

	u, err := store.GetUserByID(claims.UserID)
	if err != nil {
		return fail(c, 404, "user not found")
	}

	p, err := store.GetProfileByUserID(claims.UserID)
	if err != nil {
		return fail(c, 500, "internal error")
	}

	// Include menu profile info so the profile page can show a dropdown.
	// menu_profile_id is the user's chosen menu profile (empty = default).
	// menu_profiles is the list of all available profiles for the dropdown.
	menuProfileID := store.GetUserMenuProfileID(claims.UserID)
	menuProfiles, _ := store.ListMenuProfiles()
	if menuProfiles == nil {
		menuProfiles = []*store.MenuProfile{}
	}

	return ok(c, map[string]any{
		"user":          u.Public(),
		"profile":       p,
		"menuProfileId": menuProfileID,
		"menuProfiles":  menuProfiles,
	})
}

// handleUpdateProfile replaces the editable profile fields.
//
// JSON body:
//
//	displayName  — optional, max 50 chars, may contain spaces and unicode
//	bio          — optional, max chars from SettingProfileBioMaxChars (default 280)
//	githubUrl    — optional, must start with "https://github.com/" or be empty
//	websiteUrl   — optional, must start with "https://" or be empty
func handleUpdateProfile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var req store.ProfileUpdate
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Bio = strings.TrimSpace(req.Bio)
	req.GithubURL = strings.TrimSpace(req.GithubURL)
	req.WebsiteURL = strings.TrimSpace(req.WebsiteURL)

	if utf8.RuneCountInString(req.DisplayName) > 50 {
		return fail(c, 400, "display name must be 50 characters or fewer")
	}

	bioMax := store.GetSettingInt(store.SettingProfileBioMaxChars, 280)
	if utf8.RuneCountInString(req.Bio) > bioMax {
		return fail(c, 400, fmt.Sprintf("bio must be %d characters or fewer", bioMax))
	}

	if req.GithubURL != "" && !strings.HasPrefix(req.GithubURL, "https://") {
		return fail(c, 400, "github URL must start with https://")
	}
	if len(req.GithubURL) > 200 {
		return fail(c, 400, "github URL must be 200 characters or fewer")
	}

	if req.WebsiteURL != "" && !strings.HasPrefix(req.WebsiteURL, "https://") {
		return fail(c, 400, "website URL must start with https://")
	}
	if len(req.WebsiteURL) > 200 {
		return fail(c, 400, "website URL must be 200 characters or fewer")
	}

	if err := store.UpdateProfile(claims.UserID, &req); err != nil {
		return fail(c, 500, "internal error")
	}

	p, err := store.GetProfileByUserID(claims.UserID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, p)
}

// ─── Locale preference ────────────────────────────────────────────────────────

// handleUpdateLocale changes the authenticated user's preferred UI locale.
//
// This endpoint is separate from the general profile update because the locale
// is stored in the users table (authentication record), not user_profiles.
// The sidebar locale switcher and the profile page both call this endpoint.
//
// JSON body:
//
//	locale — required, must match a registered locale code in i18n_bundles
//	         (e.g. "en-US", "pt-BR")
//
// On success, returns the updated locale so the client can confirm the change.
func handleUpdateLocale(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var req struct {
		Locale string `json:"locale"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.Locale = strings.TrimSpace(req.Locale)
	if req.Locale == "" {
		return fail(c, 400, "locale is required")
	}

	// Validate that the requested locale actually exists in the translation
	// system. This prevents persisting invalid codes (typos, unsupported locales).
	exists, err := store.LocaleExists(req.Locale)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if !exists {
		return fail(c, 400, "unsupported locale code")
	}

	if err := store.UpdatePreferredLocale(claims.UserID, req.Locale); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]string{"locale": req.Locale})
}

// ─── Menu profile selection ──────────────────────────────────────────────────

// handleUpdateMenuProfile sets the user's preferred IDE menu profile.
// The menu profile controls which items, order, and labels appear in the
// IDE sidebar. An empty profile_id means "use the default profile".
//
//	PUT /api/v1/profile/menu-profile
//	Body: { "profileId": "kids" } or { "profileId": "" } for default
func handleUpdateMenuProfile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var req struct {
		ProfileID string `json:"profileId"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.ProfileID = strings.TrimSpace(req.ProfileID)

	// Validate that the requested profile actually exists (unless empty = default).
	if req.ProfileID != "" {
		profiles, err := store.ListMenuProfiles()
		if err != nil {
			return fail(c, 500, "internal error")
		}
		found := false
		for _, p := range profiles {
			if p.ProfileID == req.ProfileID {
				found = true
				break
			}
		}
		if !found {
			return fail(c, 400, "unknown menu profile")
		}
	}

	if err := store.SetUserMenuProfileID(claims.UserID, req.ProfileID); err != nil {
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]string{"menuProfileId": req.ProfileID})
}

// ─── Country code ────────────────────────────────────────────────────────────

// handleUpdateCountry sets the user's self-declared country code (ISO 3166-1 alpha-2).
// Used by visibility rules to filter menu items by country.
//
//	PUT /api/v1/profile/country
//	Body: { "countryCode": "BR" }
func handleUpdateCountry(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var req struct {
		CountryCode string `json:"countryCode"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.CountryCode = strings.TrimSpace(strings.ToUpper(req.CountryCode))
	if len(req.CountryCode) > 2 {
		return fail(c, 400, "country code must be 2 characters (ISO 3166-1 alpha-2)")
	}

	if err := store.UpdateCountryCode(claims.UserID, req.CountryCode); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]string{"countryCode": req.CountryCode})
}

// ─── Panel column widths ─────────────────────────────────────────────────────

// handleGetPanelPrefs returns saved column widths for the user's OS+browser.
//
//	GET /api/v1/profile/panel-prefs?os=macos&browser=chrome
func handleGetPanelPrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	os := strings.TrimSpace(c.QueryParam("os"))
	browser := strings.TrimSpace(c.QueryParam("browser"))
	if os == "" || browser == "" {
		return fail(c, 400, "os and browser query params are required")
	}

	prefs, err := store.GetPanelPrefs(claims.UserID, os, browser)
	if err != nil {
		return fail(c, 500, "internal error")
	}

	if prefs == nil {
		// No saved prefs — return defaults.
		return ok(c, map[string]any{
			"rail_width": 96,
			"list_width": 250,
		})
	}

	return ok(c, map[string]any{
		"rail_width": prefs.RailWidth,
		"list_width": prefs.ListWidth,
	})
}

// handleUpdatePanelPrefs saves column widths for the user's OS+browser.
//
//	PUT /api/v1/profile/panel-prefs
//	Body: { "os": "macos", "browser": "chrome", "rail_width": 110, "list_width": 300 }
func handleUpdatePanelPrefs(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var req struct {
		OS        string `json:"os"`
		Browser   string `json:"browser"`
		RailWidth int    `json:"rail_width"`
		ListWidth int    `json:"list_width"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.OS = strings.TrimSpace(req.OS)
	req.Browser = strings.TrimSpace(req.Browser)
	if req.OS == "" || req.Browser == "" {
		return fail(c, 400, "os and browser are required")
	}

	// Clamp to reasonable bounds.
	if req.RailWidth < 60 {
		req.RailWidth = 60
	}
	if req.RailWidth > 200 {
		req.RailWidth = 200
	}
	if req.ListWidth < 150 {
		req.ListWidth = 150
	}
	if req.ListWidth > 600 {
		req.ListWidth = 600
	}

	if err := store.UpsertPanelPrefs(claims.UserID, req.OS, req.Browser, req.RailWidth, req.ListWidth); err != nil {
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]any{
		"rail_width": req.RailWidth,
		"list_width": req.ListWidth,
	})
}

// ─── Avatar upload ────────────────────────────────────────────────────────────

// handleUploadAvatar saves a new avatar image for the authenticated user.
//
// Accepts: PNG, JPG/JPEG, WebP. Max size from SettingAvatarMaxBytes (default 2 MB).
// The avatar directory is cleared before writing so there is always at most
// one avatar file per user.
//
// Multipart field: "file"
func handleUploadAvatar(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	fh, err := c.FormFile("file")
	if err != nil {
		return fail(c, 400, "file field is required")
	}

	lower := strings.ToLower(fh.Filename)
	var ext string
	switch {
	case strings.HasSuffix(lower, ".png"):
		ext = ".png"
	case strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg"):
		ext = ".jpg"
	case strings.HasSuffix(lower, ".webp"):
		ext = ".webp"
	default:
		return fail(c, 400, "only PNG, JPG, and WebP avatars are allowed")
	}

	maxBytes := int64(store.GetSettingInt(store.SettingAvatarMaxBytes, 2_097_152))
	if fh.Size > maxBytes {
		return fail(c, 413, fmt.Sprintf("avatar must be smaller than %d bytes", maxBytes))
	}

	cfg := config.Get()
	avatarDir := filepath.Join(cfg.UserFilesDir, claims.UserID, "avatar")
	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		return fail(c, 500, "could not create avatar directory")
	}

	// Remove old avatar before writing — one avatar file per user.
	if err := clearDirectory(avatarDir); err != nil {
		return fail(c, 500, "could not clear old avatar")
	}

	destPath := filepath.Join(avatarDir, "avatar"+ext)

	src, err := fh.Open()
	if err != nil {
		return fail(c, 500, "could not read uploaded file")
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fail(c, 500, "could not write avatar file")
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fail(c, 500, "could not save avatar")
	}

	avatarURL := fmt.Sprintf("/static/%s/avatar/avatar%s", claims.UserID, ext)

	if err := store.UpdateAvatarURL(claims.UserID, avatarURL); err != nil {
		return fail(c, 500, "could not update avatar URL")
	}

	return ok(c, map[string]string{"avatarUrl": avatarURL})
}

// ─── Own invites ──────────────────────────────────────────────────────────────

// handleListOwnInvites returns all invite codes created by the authenticated user.
// Each invite includes its status (active, used, or expired) so the profile
// page can render the appropriate UI for each code.
func handleListOwnInvites(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	invites, err := store.ListInvitesByUser(claims.UserID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if invites == nil {
		invites = []*store.InviteCode{}
	}

	// Compute status for each invite and shape the response.
	type inviteItem struct {
		ID        string             `json:"id"`
		Code      string             `json:"code"`
		Status    store.InviteStatus `json:"status"`
		UsedBy    string             `json:"usedBy,omitempty"`
		ExpiresAt string             `json:"expiresAt"`
		CreatedAt string             `json:"createdAt"`
	}

	items := make([]inviteItem, 0, len(invites))
	for _, inv := range invites {
		items = append(items, inviteItem{
			ID:        inv.ID,
			Code:      inv.Code,
			Status:    inv.Status(),
			UsedBy:    inv.UsedBy,
			ExpiresAt: inv.ExpiresAt.Format("2006-01-02"),
			CreatedAt: inv.CreatedAt.Format("2006-01-02"),
		})
	}
	return ok(c, items)
}

// ─── Public profile ───────────────────────────────────────────────────────────

// handleGetPublicProfile returns the public-facing profile for the given
// username. This endpoint requires no authentication — it is the profile page
// visible to anyone browsing the marketplace.
func handleGetPublicProfile(c echo.Context) error {
	username := strings.TrimSpace(c.Param("username"))
	if username == "" {
		return fail(c, 400, "username is required")
	}

	p, err := store.GetPublicProfileByUsername(username)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}
	return ok(c, p)
}

// ─── File helpers ─────────────────────────────────────────────────────────────

// clearDirectory removes all files in dir without removing the directory.
func clearDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ─── Response helpers ─────────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": 200},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
