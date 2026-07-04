// server/handler/targetapi/routes.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//

// Public hardware-target routes.
//
// Routes:
//
//	GET /api/v1/targets — list the selectable hardware targets (public)
//
// Like GET /api/v1/blackbox, this endpoint is intentionally PUBLIC: the WASM
// IDE loads the target list to populate its board dropdown without a token. The
// list is the small, code-curated registry in server/codegen/target — there is
// nothing user-specific or sensitive to protect, and no write path here (adding
// a target is a code change reviewed in a PR, never an API call).
//
// Registration in cmd/server/main.go:
//
//	targetapi.Register(v1)
//
// Português: Registra a rota pública de placas-alvo. Como GET /api/v1/blackbox,
// é pública de propósito: a IDE WASM carrega a lista para o dropdown de placas
// sem token. A lista é o registro curado em código (server/codegen/target) — não
// há nada específico de usuário a proteger, nem caminho de escrita aqui
// (adicionar uma placa é mudança de código revisada num PR, nunca uma chamada).

package targetapi

import "github.com/labstack/echo/v4"

// Register mounts the public target API on the given /api/v1 group.
func Register(v1 *echo.Group) {
	v1.GET("/targets", handleListTargets)
}
