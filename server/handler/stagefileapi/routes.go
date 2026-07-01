// server/handler/stagefileapi/routes.go — Stage file API route registration.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// All routes require a valid Bearer token (RequireBearerToken middleware).
//
// Route ordering:
//
//	Static sub-paths (/limit, /folders) are registered BEFORE the parameterised
//	/:id route so Echo resolves them correctly instead of treating "limit" or
//	"folders" as an :id value.
//
// Routes:
//
//	GET    /api/v1/stage-files              — list files (?folderId=xxx optional)
//	POST   /api/v1/stage-files              — create file
//	GET    /api/v1/stage-files/limit        — usage vs capacity
//	GET    /api/v1/stage-files/folders      — list folders
//	POST   /api/v1/stage-files/folders      — create folder
//	PUT    /api/v1/stage-files/folders/:id  — rename or move folder
//	DELETE /api/v1/stage-files/folders/:id  — delete folder (CASCADE)
//	GET    /api/v1/stage-files/:id          — load file (includes scene_json)
//	PUT    /api/v1/stage-files/:id          — update file
//	DELETE /api/v1/stage-files/:id          — delete file
package stagefileapi

import (
	"server/handler/spaauth"

	"github.com/labstack/echo/v4"
)

// Register mounts all stage file API routes on the /api/v1 group.
func Register(g *echo.Group) {
	sf := g.Group("/stage-files", spaauth.RequireBearerToken())

	// ── Static sub-paths first (before /:id) ──────────────────────────────
	sf.GET("/limit", handleGetLimit)

	// ── Folder routes ─────────────────────────────────────────────────────
	sf.GET("/folders", handleListFolders)
	sf.POST("/folders", handleCreateFolder)
	sf.PUT("/folders/:id", handleUpdateFolder)
	sf.DELETE("/folders/:id", handleDeleteFolder)

	// ── File routes ───────────────────────────────────────────────────────
	sf.GET("", handleListFiles)
	sf.POST("", handleCreateFile)
	sf.GET("/:id", handleGetFile)
	sf.PUT("/:id", handleUpdateFile)
	sf.DELETE("/:id", handleDeleteFile)
}
