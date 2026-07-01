// server/handler/projectexport/routes.go — route registration.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Mounted by main.go via projectexport.Register(v1) on the
// /api/v1 group. Both routes nest under /projects/:id so the
// project-scoped permission check inside the handlers can extract
// the id with c.Param("id") in the same way as every other
// project route.
//
// Routes:
//
//	POST /api/v1/projects/:id/export/check  — pre-flight validation
//	GET  /api/v1/projects/:id/export/zip    — stream the ZIP
//
// Both gated behind RequireBearerToken — only the project owner
// can trigger the export. The handler then re-checks ownership via
// store.GetProjectByIDAndUser, defence-in-depth.
//
// Português: registro das rotas /export/check e /export/zip dentro
// de /api/v1/projects/:id. Ambas exigem token bearer e a posse do
// projeto é re-verificada nos handlers.
package projectexport

import (
	"github.com/labstack/echo/v4"

	"server/handler/spaauth"
)

// Register installs the export routes on the given /api/v1 group.
// Called once from main.go alongside the other RegisterXxx helpers.
func Register(g *echo.Group) {
	exp := g.Group("/projects/:id/export", spaauth.RequireBearerToken())
	exp.POST("/check", handleCheck)
	exp.GET("/zip", handleZip)
}
