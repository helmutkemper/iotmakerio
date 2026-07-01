// /ide/server/handler/profileapi/routes.go
// handler/profileapi/routes.go — User profile API routes.
//
// Routes:
//
//	GET  /api/v1/profile              — own full profile (requires bearer)
//	PUT  /api/v1/profile              — update editable fields (requires bearer)
//	PUT  /api/v1/profile/locale       — update preferred UI locale (requires bearer)
//	POST /api/v1/profile/avatar       — upload avatar (requires bearer)
//	GET  /api/v1/profile/invites      — list own invite codes (requires bearer)
//	GET  /api/v1/users/:username      — public profile (no auth)
//
// Route ordering note:
// Static sub-paths (/profile/avatar, /profile/invites, /profile/locale) must
// be registered before the parameterised /users/:username route to avoid Echo
// matching "avatar", "invites", or "locale" as the :username parameter.
package profileapi

import (
	"server/handler/spaauth"

	"github.com/labstack/echo/v4"
)

// Register mounts all profile and public-user routes on the /api/v1 group.
func Register(g *echo.Group) {
	// ── Own profile (requires authentication) ─────────────────────────────────
	profile := g.Group("/profile", spaauth.RequireBearerToken())
	profile.GET("", handleGetOwnProfile)
	profile.PUT("", handleUpdateProfile)
	profile.PUT("/locale", handleUpdateLocale)
	profile.PUT("/menu-profile", handleUpdateMenuProfile)
	profile.PUT("/country", handleUpdateCountry)
	profile.GET("/panel-prefs", handleGetPanelPrefs)
	profile.PUT("/panel-prefs", handleUpdatePanelPrefs)
	profile.POST("/avatar", handleUploadAvatar)
	profile.GET("/invites", handleListOwnInvites)

	// ── Public profile (no authentication required) ───────────────────────────
	g.GET("/users/:username", handleGetPublicProfile)
}
