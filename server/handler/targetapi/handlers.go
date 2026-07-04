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

	// StringBufferSize is the board's default string-concatenation buffer, in
	// bytes. Exposed so the picker's advanced panel can PREFILL its field with
	// this board's current value (the maker sees "64 bytes" and adjusts from
	// there). Safe to expose — unlike the type profile, a buffer size is not
	// correctness-critical (snprintf truncates), and the maker can override it
	// anyway.
	//
	// Português: Buffer de concatenação padrão da placa, em bytes. Exposto para o
	// painel avançado do picker PRÉ-PREENCHER seu campo com o valor atual da
	// placa (o maker vê "64 bytes" e ajusta). Seguro expor — diferente do
	// profile, um tamanho de buffer não é crítico (snprintf trunca).
	StringBufferSize int `json:"stringBufferSize"`
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
			ID:               t.ID,
			DisplayName:      t.DisplayName,
			Description:      t.Description,
			RAMBytes:         t.RAMBytes,
			Icon:             t.Icon,
			StringBufferSize: t.StringBufferSize,
		}
	}
	// Wrap in the standard { metadata, data } envelope used across the API
	// (the same shape apiOK produces in the other handlers), so the WASM client
	// (LoadTargets) parses this exactly like it parses the menu tree and
	// translations. Returning the bare { targets } shape makes LoadTargets read
	// Metadata.Status as 0, reject the response, and fall back to the default —
	// which silently skips the board picker. Keep the envelope.
	//
	// Português: Envolve no envelope padrão { metadata, data } usado na API (a
	// mesma forma do apiOK), para o cliente WASM (LoadTargets) fazer o parse
	// igual à árvore de menu e às traduções. Retornar o shape cru { targets } faz
	// o LoadTargets ler Metadata.Status como 0, rejeitar a resposta e cair no
	// default — o que pula o picker em silêncio. Mantenha o envelope.
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK},
		"data":     map[string]any{"targets": views},
	})
}
