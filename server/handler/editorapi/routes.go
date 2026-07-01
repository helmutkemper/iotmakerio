// server/handler/editorapi/routes.go — Editor settings routes.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Routes:
//
//	GET    /api/v1/editor/menu-prefs  — full menu tree + user's hidden items
//	PUT    /api/v1/editor/menu-prefs  — batch update hidden items
//	DELETE /api/v1/editor/menu-prefs  — reset all overrides to admin defaults
//
//	GET    /api/v1/editor/stage-prefs  — per-user stage (canvas) knobs
//	PUT    /api/v1/editor/stage-prefs  — patch-update one or more knobs
//	DELETE /api/v1/editor/stage-prefs  — reset all knobs to defaults
//
// All routes require authentication (RequireBearerToken).
//
// Registration in cmd/server/main.go:
//
//	editorapi.Register(v1)    // /api/v1/editor/*
package editorapi

import (
	"github.com/labstack/echo/v4"

	"server/handler/spaauth"
)

// Register mounts editor settings routes on the /api/v1 group.
func Register(g *echo.Group) {
	editor := g.Group("/editor", spaauth.RequireBearerToken())

	// Menu preferences — which sidebar items the user has hidden.
	editor.GET("/menu-prefs", handleGetMenuPrefs)
	editor.PUT("/menu-prefs", handleSetMenuPrefs)
	editor.DELETE("/menu-prefs", handleResetMenuPrefs)

	// Stage preferences — zoom sensitivity, pan behaviour, cursor hints.
	editor.GET("/stage-prefs", handleGetStagePrefs)
	editor.PUT("/stage-prefs", handleUpdateStagePrefs)
	editor.DELETE("/stage-prefs", handleResetStagePrefs)
}
