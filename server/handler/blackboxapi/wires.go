// server/handler/blackboxapi/wires.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackboxapi

// wires.go — the "Save to My Items" endpoint for GRAPHICAL functions
// (P3). The client sends the FULL scene plus the Function container's
// id; the server extracts the subtree, validates the signature by the
// emitter's own laws (codegen.ExtractFunctionDef), enforces the
// immutability law (published is frozen — evolve as a new name), and
// stores the def. Português: O endpoint "Salvar em My Items" — o
// cliente manda a cena completa + o id do container; o servidor extrai
// o subtree, valida pela lei do emitter, aplica a imutabilidade e
// grava.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"server/codegen"
	"server/handler/spaauth"
	"server/store"
)

type wiresSaveRequest struct {
	FunctionID string          `json:"functionId"`
	Scene      json.RawMessage `json:"scene"`
	Language   string          `json:"language"`
}

type wiresSaveResponse struct {
	ID          string               `json:"id,omitempty"`
	Name        string               `json:"name,omitempty"`
	Diagnostics []codegen.Diagnostic `json:"diagnostics,omitempty"`
	Error       string               `json:"error,omitempty"`
}

// handleWiresSave — POST /api/v1/blackboxes/wires (auth).
func (h *handler) handleWiresSave(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	if claims == nil || claims.UserID == "" {
		return c.JSON(http.StatusUnauthorized, wiresSaveResponse{Error: "not logged in"})
	}
	userID := claims.UserID
	var req wiresSaveRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, wiresSaveResponse{Error: "bad request: " + err.Error()})
	}
	if req.FunctionID == "" || len(req.Scene) == 0 {
		return c.JSON(http.StatusBadRequest, wiresSaveResponse{Error: "functionId and scene are required"})
	}

	def, diags := codegen.ExtractFunctionDef(req.Scene, req.FunctionID)
	if def == nil {
		return c.JSON(http.StatusUnprocessableEntity, wiresSaveResponse{Diagnostics: diags})
	}

	id, err := store.SaveWiresFunction(userID, req.Language, def)
	if err != nil {
		if errors.Is(err, store.ErrWiresNameTaken) {
			return c.JSON(http.StatusConflict, wiresSaveResponse{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, wiresSaveResponse{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, wiresSaveResponse{ID: id, Name: def.Name, Diagnostics: diags})
}
