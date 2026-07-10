// rulesConnection/consts.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesConnection

const (
	// KWidth Connection width
	KWidth = 6

	// KHeight Connection height
	KHeight = 4

	// KWidthArea Connection width, mouse área.
	//
	//  Creates a larger area to facilitate the click by the user
	KWidthArea = KWidth + 6

	// KHeightArea Connection height, mouse área.
	//
	//  Creates a larger area to facilitate the click by the user
	KHeightArea = KHeight + 6

	KConnectionPrefix = "connection"
)

// The top-left-corner path helpers (GetPathDraw / GetPathAreaDraw) were
// REMOVED in the wave-2 connector standardization: their last callers — the
// container ornaments (loop, if/else, case) — now use the side-aware
// PinPathDraw / PinPathAreaDraw (pin.go), which take the EDGE POINT and
// share their convention with PinAnchor and PinHit. No legacy, per project
// rule.
//
// Português: Os helpers de canto superior esquerdo (GetPathDraw /
// GetPathAreaDraw) foram REMOVIDOS na onda 2 da padronização: seus últimos
// chamadores — os ornaments de container (loop, if/else, case) — agora usam
// os PinPathDraw / PinPathAreaDraw com lado (pin.go), que recebem o EDGE
// POINT e compartilham a convenção com PinAnchor e PinHit. Sem legado,
// regra do projeto.
