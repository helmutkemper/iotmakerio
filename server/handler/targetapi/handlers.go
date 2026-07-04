// server/handler/targetapi/handlers.go — Target-list handler.
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package targetapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"server/codegen/target"
)

// targetView is the JSON shape the WASM IDE's board dropdown consumes. It
// deliberately exposes only the DISPLAY fields of a target — the maker needs the
// name, the explanatory text, the RAM figure, and an icon. The codegen-internal
// fields (which type-family profile, what string-buffer size) are NOT sent:
// they are resolved server-side from the chosen id at generation time, so the
// client has no reason to see them, and cannot tamper with type widths by
// editing a response.
//
// Português: É a forma JSON que o dropdown de placas da IDE WASM consome. Expõe
// só os campos de EXIBIÇÃO de um target — o maker precisa do nome, do texto
// explicativo, da RAM e de um ícone. Os campos internos do codegen (qual
// profile, qual tamanho de buffer) NÃO são enviados: são resolvidos no servidor
// a partir do id escolhido na hora de gerar, então o cliente não tem por que
// vê-los nem consegue mexer nas larguras de tipo editando uma resposta.
type targetView struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	RAMBytes    int    `json:"ramBytes"` // 0 = ample / not applicable; the UI renders "ample"
	Icon        string `json:"icon"`
}

// handleListTargets returns every selectable hardware target, already ordered
// for display, for the IDE's board dropdown. Public — no auth, mirroring the
// black-box list endpoint. The maker's chosen id later travels back in the
// scene as Metadata.Target and the codegen resolves it.
//
// Português: Retorna todas as placas selecionáveis, já ordenadas, para o
// dropdown da IDE. Público — sem auth, espelhando o endpoint de black-boxes. O
// id escolhido volta depois na cena como Metadata.Target e o codegen o resolve.
func handleListTargets(c echo.Context) error {
	all := target.AllTargets()
	views := make([]targetView, len(all))
	for i, t := range all {
		views[i] = targetView{
			ID:          t.ID,
			DisplayName: t.DisplayName,
			Description: t.Description,
			RAMBytes:    t.RAMBytes,
			Icon:        t.Icon,
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"targets": views})
}
