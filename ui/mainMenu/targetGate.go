// ui/mainMenu/targetGate.go — The min-target gate for menu items.
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	The maker's spec, verbatim: the menu keeps showing EVERY device, but
//	devices whose declared minimum hardware class exceeds the project's
//	board are not clickable — visible (discoverability) yet disabled. The
//	gate is evaluated when a submenu is BUILT (menus build per navigation),
//	so a board change on the export picker reflects on the next open — no
//	live refresh machinery.
//
// Português:
//
//	A especificação do maker, ao pé da letra: o menu continua mostrando
//	TODOS os devices, mas os cuja classe mínima declarada excede a placa do
//	projeto não são clicáveis — visíveis (descobribilidade), porém
//	desabilitados. O portão é avaliado quando o submenu é CONSTRUÍDO (menus
//	constroem por navegação), então trocar a placa no picker do export
//	reflete na próxima abertura — sem maquinaria de refresh vivo.

package mainMenu

import (
	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/hexMenu"
)

// projectTargetOrdinal holds the CURRENT project's board rung. The zero
// value is overwritten on workspace init; 3 (posix) is the permissive
// default matching blackbox.TargetIDOrdinal("").
// Português: O degrau da placa ATUAL do projeto. Sobrescrito no init do
// workspace; 3 (posix) é o default permissivo.
var projectTargetOrdinal = 3

// SetProjectTargetID records the project's current board (registry id) for
// the menu gate. Called by the workspace whenever the target changes:
// creation, scene load, export picker.
// Português: Registra a placa atual (id do registro) para o portão do
// menu. Chamado pelo workspace a cada mudança: criação, load de cena,
// picker do export.
func SetProjectTargetID(id string) {
	projectTargetOrdinal = blackbox.TargetIDOrdinal(id)
}

// itemTargetGated reports whether the item's declared minimum class sits
// ABOVE the project's board — the disabled condition.
// Português: Diz se a classe mínima do item fica ACIMA da placa do projeto
// — a condição de desabilitado.
func itemTargetGated(item hexMenu.MenuItem) bool {
	if item.MinTarget == "" {
		return false
	}
	return blackbox.MinTargetOrdinal(item.MinTarget) > projectTargetOrdinal
}
