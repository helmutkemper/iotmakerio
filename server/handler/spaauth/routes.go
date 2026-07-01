// handler/spaauth/routes.go — Auth API routes for the SPA.
//
// Routes:
//
//	GET  /api/auth/register-config  — invite required flag + available locales (public)
//	POST /api/auth/register         — create account (invite code + locale + display name)
//	POST /api/auth/verify-email     — verify email OTP
//	POST /api/auth/login            — step 1: credentials → OTP sent
//	POST /api/auth/login/2fa        — step 2: OTP → JWT token
//	GET  /api/auth/me               — return authenticated user (requires bearer)
//	POST /api/auth/logout           — client-side token discard (stateless JWT)
//	POST /api/auth/forgot-password  — send password-reset OTP
//	POST /api/auth/reset-password   — consume OTP and set new password
//	POST /api/auth/invite           — generate an invite code (requires bearer)
//	GET  /api/auth/invite/:code     — validate an invite code (public, anti-enumeration)
//	GET  /api/auth/github           — redirect to GitHub OAuth consent (requires bearer)
//	GET  /api/auth/github/callback  — GitHub OAuth callback, saves github_username
package spaauth

import (
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// Register mounts all auth routes on the provided Echo instance.
// rdb is required for GitHub OAuth state storage.
func Register(e *echo.Echo, rdb *redis.Client) {
	g := e.Group("/api/auth")

	// ── Public endpoints ──────────────────────────────────────────────────────
	g.GET("/register-config", handleRegisterConfig) // form bootstrap data
	g.POST("/register", handleRegister)
	g.POST("/verify-email", handleVerifyEmail)
	g.POST("/login", handleLogin)
	g.POST("/login/2fa", handleLogin2FA)
	g.POST("/forgot-password", handleForgotPassword)
	g.POST("/reset-password", handleResetPassword)
	g.GET("/invite/:code", handleValidateInvite) // always 200 — anti-enumeration

	// ── Protected endpoints ───────────────────────────────────────────────────
	auth := g.Group("", RequireBearerToken())
	auth.GET("/me", handleMe)
	auth.POST("/logout", handleLogout)
	auth.POST("/invite", handleGenerateInvite)

	// ── GitHub OAuth — specialist identity verification ───────────────────────
	// Both endpoints are PUBLIC — no RequireBearerToken middleware.
	//
	// /github:          Browser navigates here directly (window.location.href).
	//                   Bearer token arrives as ?token= query param.
	//                   State is stored in Redis; no JWT signing dependency.
	//
	// /github/callback: GitHub makes this redirect — state validated via Redis GetDel.
	gh := &githubHandler{redis: rdb}
	g.GET("/github", gh.handleGithubConnect)
	g.GET("/github/callback", gh.handleGithubCallback)
}
