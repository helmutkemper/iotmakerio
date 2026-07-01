// server/handler/controlapi/controlapi.go — Control panel API handler and middleware.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// This package owns two responsibilities:
//
//  1. POST /api/auth/control-token
//     Exchanges a valid portal Bearer token for a short-lived control panel
//     token (1 hour). Only users with role = admin are granted a control token.
//     The control token is signed with a different issuer claim so it cannot
//     be used on portal routes, and portal tokens cannot be used here.
//
//  2. RequireControlToken(perm) middleware
//     Validates the control panel Bearer token and checks that the caller's
//     role holds the requested permission. Used on all /api/control/v1/* routes.
//
// Registration in cmd/server/main.go:
//
//	controlapi.RegisterAuth(e)                      // POST /api/auth/control-token
//	controlapi.RegisterControl(e, v1ControlGroup)   // /api/control/v1/*
package controlapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/config"
	"server/permission"
	"server/store"
)

// contextKeyControlClaims is the echo context key for validated control JWT claims.
const contextKeyControlClaims = "_control_claims"

// ─── Auth endpoint ────────────────────────────────────────────────────────────

// RegisterAuth mounts the control token exchange endpoint on the root Echo group.
// It must be called before RequireControlToken is used anywhere.
func RegisterAuth(e *echo.Echo) {
	e.POST("/api/auth/control-token", handleControlToken)
}

// handleControlToken exchanges a valid portal Bearer token for a control token.
// Only admins can receive a control token.
//
// Request:  Authorization: Bearer <portal-token>
// Response: { "data": { "token": "<control-token>", "expires_in": 3600 } }
func handleControlToken(c echo.Context) error {
	hdr := c.Request().Header.Get("Authorization")
	if !strings.HasPrefix(hdr, "Bearer ") {
		return fail(c, http.StatusUnauthorized, "missing bearer token")
	}

	cfg := config.Get()
	claims, err := cryptoauth.ParseJWT(strings.TrimPrefix(hdr, "Bearer "), cfg.JWTSecret)
	if err != nil {
		return fail(c, http.StatusUnauthorized, "invalid or expired token")
	}

	// Only admins may enter the control panel.
	if claims.Role != store.RoleAdmin {
		return fail(c, http.StatusForbidden, "control panel requires admin role")
	}

	// Verify the user still exists in the database. A stale JWT (e.g. from
	// a previous DB that was deleted and recreated) would pass signature
	// validation but reference a non-existent user. Reject it so the
	// frontend clears the token and forces a fresh login.
	user, err := store.GetUserByID(claims.UserID)
	if err != nil {
		return fail(c, http.StatusUnauthorized, "user account not found — please log in again")
	}

	// Use the DB role as source of truth (not the JWT claim) in case the
	// role was changed after the portal token was issued.
	controlToken, err := cryptoauth.NewControlJWT(user.ID, user.Role, cfg.JWTSecret)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not issue control token")
	}

	return ok(c, map[string]any{
		"token":      controlToken,
		"expires_in": int(cryptoauth.ControlTokenLifetime.Seconds()),
	})
}

// ─── Middleware ───────────────────────────────────────────────────────────────

// RequireControlToken returns middleware that:
//  1. Validates the Authorization Bearer token as a control panel token.
//  2. Checks that the caller's role holds the requested permission.
//
// Usage in route registration:
//
//	g.GET("/users", h.listUsers, controlapi.RequireControlToken(permission.PermUsersView))
func RequireControlToken(perm permission.Permission) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			hdr := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(hdr, "Bearer ") {
				return fail(c, http.StatusUnauthorized, "missing bearer token")
			}

			cfg := config.Get()
			claims, err := cryptoauth.ParseControlJWT(
				strings.TrimPrefix(hdr, "Bearer "),
				cfg.JWTSecret,
			)
			if err != nil {
				return fail(c, http.StatusUnauthorized, "invalid or expired control token")
			}

			if !permission.Has(claims.Role, perm) {
				return fail(c, http.StatusForbidden, "insufficient permissions")
			}

			c.Set(contextKeyControlClaims, claims)
			return next(c)
		}
	}
}

// ControlClaims extracts the validated control JWT claims from the echo context.
// Must only be called inside a handler protected by RequireControlToken.
func ControlClaims(c echo.Context) *cryptoauth.Claims {
	v := c.Get(contextKeyControlClaims)
	if v == nil {
		return &cryptoauth.Claims{}
	}
	return v.(*cryptoauth.Claims)
}

// ─── Response helpers ─────────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
