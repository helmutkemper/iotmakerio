// /ide/server/handler/blackboxapi/wizard_examples.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackboxapi

// wizard_examples.go — the mission gallery's read API (wizard UI plan,
// 2026-07-13). The list is a menu (no bundles); one entry travels whole
// on demand. Content lives in the wizard_examples table — seeded by
// migration, managed at /control later.
// Português: A API de leitura da galeria de missões. A lista é um
// cardápio (sem bundles); uma entrada viaja inteira sob demanda.

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"server/store"
)

// handleWizardExamples GET /wizard/examples?language=c|go
func (h *handler) handleWizardExamples(c echo.Context) error {
	lang := c.QueryParam("language")
	if lang == "" {
		lang = "go"
	}
	list, err := store.ListWizardExamples(lang)
	if err != nil {
		return wizardErr(c, http.StatusInternalServerError, err.Error())
	}
	return wizardOK(c, map[string]any{"examples": list})
}

// handleWizardExample GET /wizard/examples/:id
func (h *handler) handleWizardExample(c echo.Context) error {
	ex, err := store.GetWizardExample(c.Param("id"))
	if err != nil {
		return wizardErr(c, http.StatusNotFound, "example not found")
	}
	return wizardOK(c, ex)
}
