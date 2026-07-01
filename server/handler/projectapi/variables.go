// server/handler/projectapi/variables.go — HTTP handlers for user-declared
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// project variables (the GetVar/SetVar device family).
//
// Routes (registered in routes.go), mirroring the /projects/:id/files group —
// a project-owned sub-resource:
//
//	GET    /api/v1/projects/:id/variables           — list the project's variables
//	POST   /api/v1/projects/:id/variables           — declare one  {name, type}
//	DELETE /api/v1/projects/:id/variables/:varId     — delete one by id
//
// Every handler verifies project ownership via store.GetProjectByIDAndUser
// before touching the variable, exactly like the code-version handlers.
//
// "In use" deletion guard: a variable still referenced by a GetVar/SetVar
// device must not be deleted. That check needs the scene, which lives in the
// IDE (codegen is a stateless scene→code step with no project_id, so the server
// never holds the scene here). The IDE therefore blocks the delete client-side;
// this endpoint is the unconditional storage operation behind it.
//
// Português: Handlers HTTP das variáveis de projeto declaradas pelo usuário
// (família GetVar/SetVar). Espelham o grupo /projects/:id/files — sub-recurso
// que pertence ao projeto. Todo handler confere a posse via
// GetProjectByIDAndUser antes de mexer na variável. A trava de "não apagar em
// uso" precisa da cena (que vive na IDE; o codegen é stateless sem project_id),
// então a IDE bloqueia no cliente — este endpoint é a operação de storage
// incondicional por trás dela.
package projectapi

import (
	"server/handler/spaauth"
	"server/store"

	"github.com/labstack/echo/v4"
)

// handleListVariables returns every variable declared for the project, ordered
// by name (stable for the UI and for the codegen declaration order).
func handleListVariables(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	vars, err := store.ListProjectVariables(projectID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if vars == nil {
		vars = []store.ProjectVariable{}
	}
	return ok(c, vars)
}

// handleCreateVariable declares a new variable. The store validates the name as
// a legal, non-reserved identifier and the type as one of int/float/string;
// each failure maps to a distinct client-facing status so the IDE can show a
// precise message.
func handleCreateVariable(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	// Ownership before validation: do not reveal create behaviour to a caller
	// who does not own the project.
	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	v, err := store.CreateProjectVariable(projectID, req.Name, req.Type)
	if err != nil {
		switch err {
		case store.ErrInvalidVariableName:
			return fail(c, 400, "invalid name: must be a non-reserved identifier (letter/underscore, then letters/digits/underscores)")
		case store.ErrInvalidVariableType:
			return fail(c, 400, "invalid type: must be int, float or string")
		case store.ErrProjectVariableExists:
			return fail(c, 409, "a variable with that name already exists in this project")
		default:
			return fail(c, 500, "could not create variable")
		}
	}
	return ok(c, v)
}

// handleDeleteVariable removes a variable by id, scoped to its project. The
// caller-side "in use" guard lives in the IDE (see file header).
func handleDeleteVariable(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	varID := c.Param("varId")

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	if err := store.DeleteProjectVariable(projectID, varID); err != nil {
		if err == store.ErrProjectVariableNotFound {
			return fail(c, 404, "variable not found")
		}
		return fail(c, 500, "could not delete variable")
	}
	return ok(c, map[string]any{"id": varID, "deleted": true})
}
