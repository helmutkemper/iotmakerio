// handler/projectapi/routes.go — Project management API routes.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// All project and file routes require a valid Bearer token.
// The authenticated user ID is used as a scoping key — users can only
// access their own projects.
//
// Routes:
//
//	GET    /api/v1/projects/meta/languages         — list programming languages (public)
//	GET    /api/v1/projects/meta/ui-languages      — list UI languages (public)
//	GET    /api/v1/projects/meta/readme-config     — taxonomy + settings for readme.md editor (public)
//
//	GET    /api/v1/projects                        — list user's projects
//	POST   /api/v1/projects                        — create a new project
//	PUT    /api/v1/projects/:id                    — update project properties (name, visibility, publish flags)
//	DELETE /api/v1/projects/:id                    — delete project + disk files
//
//	GET    /api/v1/projects/:id/files              — list all files grouped by section
//
//	GET    /api/v1/projects/:id/files/code         — get latest snapshot {files[]} + version list
//	POST   /api/v1/projects/:id/files/code         — upload: add/replace ONE file in a new snapshot (multipart)
//	DELETE /api/v1/projects/:id/files/code         — ?path= removes one file (new snapshot); bare = clear disk mirror
//	PUT    /api/v1/projects/:id/files/code/rename  — rename a path in a new snapshot {oldPath?, newName}
//
//	GET    /api/v1/projects/:id/files/code/versions      — list all snapshots (each with files[])
//	POST   /api/v1/projects/:id/files/code/versions      — save new snapshot {files:[{path,content}], lastParseOk?}
//
//	GET    /api/v1/projects/:id/files/code/backup        — get the working-source backup (404 if none)
//	POST   /api/v1/projects/:id/files/code/backup        — overwrite the working-source backup
//	DELETE /api/v1/projects/:id/files/code/backup        — clear the working-source backup
//
//	POST   /api/v1/projects/:id/files/img          — upload image
//	DELETE /api/v1/projects/:id/files/img/:name    — delete image
//
//	POST   /api/v1/projects/:id/files/docs         — upload new markdown file (fails if exists)
//	PUT    /api/v1/projects/:id/files/docs/:name   — update existing markdown file (Monaco save)
//	DELETE /api/v1/projects/:id/files/docs/:name   — delete markdown (readme.md returns 403)
//
// Route ordering note:
// The /meta/* routes must be registered BEFORE /projects/:id to prevent Echo
// from matching "meta" as the :id parameter.
// Within /files/code/*, the /versions routes are registered before /rename
// so that Echo resolves the static segments first.
//
// Properties update flow:
//
//	PUT /api/v1/projects/:id → handleUpdateProject
//	  → validates name, visibility
//	  → forces publish flags to false when visibility = "private"
//	  → calls store.UpdateProject
//	  → returns full updated Project object
//
// readme.md save flow:
//
//	PUT /files/docs/readme.md → handleUpdateMarkdown
//	  → reads file content into memory
//	  → writes to disk
//	  → calls applyFrontmatterToProject → parseFrontmatter → UpdateProjectCard
//	  → returns optional "warning" field if description was truncated
package projectapi

import (
	"server/handler/spaauth"

	"github.com/labstack/echo/v4"
)

// Register mounts all project API routes on the /api/v1 group.
func Register(g *echo.Group) {
	// ── Lookup tables (public, no auth required) ──────────────────────────────
	// readme-config returns the full taxonomy (categories + subcategories) and
	// the server-side settings (description max chars) needed by the Monaco
	// editor's readme.md slash menu. It is intentionally public so that the
	// WASM IDE and other consumers can fetch the taxonomy without authentication.
	meta := g.Group("/projects/meta")
	meta.GET("/languages", handleListProgrammingLanguages)
	meta.GET("/ui-languages", handleListUILanguages)
	meta.GET("/readme-config", handleListReadmeConfig)
	meta.GET("/subcategories", handleListSubcategories) // ?categoryId=xxx

	// ── Project CRUD ─────────────────────────────────────────────────────────
	projects := g.Group("/projects", spaauth.RequireBearerToken())
	projects.GET("", handleListProjects)
	projects.POST("", handleCreateProject)
	projects.PUT("/:id", handleUpdateProject)
	projects.DELETE("/:id", handleDeleteProject)

	// ── File listing ──────────────────────────────────────────────────────────
	files := g.Group("/projects/:id/files", spaauth.RequireBearerToken())
	files.GET("", handleListProjectFiles)

	// ── Code file — static sub-paths must be registered before parameterised ones ──
	files.GET("/code/versions", handleListCodeVersions)
	files.POST("/code/versions", handleSaveCodeVersion)
	files.GET("/code", handleGetCodeFile)
	files.POST("/code", handleUploadCodeFile)
	files.DELETE("/code", handleDeleteCodeFile)
	files.PUT("/code/rename", handleRenameCodeFile)

	// ── Image files ───────────────────────────────────────────────────────────
	files.POST("/img", handleUploadImage)
	files.DELETE("/img/:name", handleDeleteImage)

	// ── Markdown docs ─────────────────────────────────────────────────────────
	// POST   — creates a new .md file (409 if name already exists).
	// PUT    — updates an existing .md file in place. For readme.md, also
	//          parses YAML frontmatter and updates the project card in the DB.
	// DELETE — removes a .md file; readme.md returns 403.
	files.POST("/docs", handleUploadMarkdown)
	files.PUT("/docs/:name", handleUpdateMarkdown)
	files.DELETE("/docs/:name", handleDeleteMarkdown)

	// ── Working-source backup ─────────────────────────────────────────────────
	//
	// Single-slot transient backup of the project's working source. Distinct
	// from /code/versions (the user-facing Save history). Triggered by the
	// frontend on tab switches, wizard edits, and debounced Monaco edits.
	// See store/project_backups.go for the lifecycle and recovery rules.
	files.GET("/code/backup", handleGetCodeBackup)
	files.POST("/code/backup", handleSaveCodeBackup)
	files.DELETE("/code/backup", handleDeleteCodeBackup)

	// ── Help files (SQLite-blob storage; wizard-authored device assets) ───
	//
	// Independent from /img and /docs above, which still use disk-backed
	// storage. The new /help/* family uses SQLite blobs and serves
	// authenticated reads via /api/v1/projects/:id/files/help/<path>.
	// See docs/tasks/HELP_FILES_FEATURE.md for design and rationale.
	//
	// Route order matters: the more-specific /rename and /insert
	// suffixes are registered before the catch-all so Echo resolves
	// them first. Echo's routing tree already prefers specific over
	// catch-all, but explicit order keeps future changes obvious to
	// readers.
	files.POST("/help/*/rename", handleRenameHelpFile)
	files.POST("/help/insert", handleInsertHelpFile)
	files.GET("/help", handleListHelpFiles)
	files.GET("/help/*", handleGetHelpFile)
	files.PUT("/help/*", handlePutHelpFile)
	files.DELETE("/help/*", handleDeleteHelpFile)

	// ── Project variables: user-declared GetVar/SetVar named values ────────
	//     Project-owned sub-resource, same shape as the files group above:
	//     collection at "", item at "/:varId". See variables.go for handlers.
	vars := g.Group("/projects/:id/variables", spaauth.RequireBearerToken())
	vars.GET("", handleListVariables)
	vars.POST("", handleCreateVariable)
	vars.DELETE("/:varId", handleDeleteVariable)
}
