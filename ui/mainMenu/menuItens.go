// /ide/ui/mainMenu/menuItens.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Standard reusable builders for context menu items. Each function
//	returns a fully-configured contextMenu.Item with icon, translated
//	label, translated help fallback, and a caller-provided OnClick.
//	Devices compose their menus from these building blocks so every
//	"Delete" or "Inspect" looks and reads the same across the IDE.
//
//	Two functions here today:
//	  - DeleteItem: destructive, always first in a body menu.
//	  - InspectItem: opens the property overlay, always second.
//
//	Port menu builders (Connect, Disconnect, …) live in wireMenu.go.
//	Resize was moved to the context-menu package itself for frontend
//	devices — it is not reused across enough devices to justify a
//	generic builder.
//
// Português:
//
//	Construtores padrão e reutilizáveis de itens de menu contextual.
//	Cada função retorna um contextMenu.Item completo com ícone,
//	label traduzido, fallback de help traduzido e um OnClick
//	fornecido pelo chamador. Devices compõem seus menus a partir
//	desses blocos para que "Delete" ou "Inspect" se comportem e
//	apareçam iguais em toda a IDE.
package mainMenu

import (
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
)

// DeleteItem returns the standard "Delete" entry for a body menu.
//
// The OnClick callback should call Remove() on the device. The
// package renderer handles close + 150 ms delay before invoking
// the callback, so the caller does not need to wrap this in a
// goroutine or SafeRun.
//
// By convention Delete is always the first row in a body menu —
// destructive actions go at the top where they are most scannable
// and the "X reflex" from typical menus ends up on the right item.
//
// Português: Retorna a entrada padrão "Delete" para um body menu.
// O callback deve chamar Remove() no device. O pacote cuida do
// close + delay de 150 ms antes de invocar o callback.
func DeleteItem(onClick func()) contextMenu.Item {
	return contextMenu.Item{
		ID:              "delete",
		Label:           translate.T("menuDeviceDelete", "Delete"),
		FontAwesomePath: rulesIcon.KFATrashCan,
		ViewBox:         "0 0 448 512",
		HelpKey:         "helpMenuDelete",
		HelpFallback:    "Removes this device from the stage and disconnects any wires attached to it. This cannot be undone.",
		Danger:          true,
		OnClick:         onClick,
	}
}

// InspectItem returns the standard "Inspect" entry for a body menu.
//
// The OnClick callback should open the device's inspect overlay
// (typically `go e.showInspectOverlay()` — though the 150 ms
// delay done by the context menu is enough for most callers to
// call it directly without the goroutine).
//
// Inspect only appears in backend menus. Frontend context menus
// never offer Inspect — per product decision, property editing is
// a backend concept (userMemories: "Inspect is backend-only").
//
// Português: Retorna a entrada padrão "Inspect" para um body menu.
// O callback deve abrir o overlay de inspeção. Inspect só aparece
// em menus de backend — menus de contexto de frontend nunca oferecem
// Inspect por decisão de produto.
func InspectItem(onClick func()) contextMenu.Item {
	return contextMenu.Item{
		ID:              "inspect",
		Label:           translate.T("menuDeviceInspect", "Inspect"),
		FontAwesomePath: rulesIcon.KFAEye,
		ViewBox:         "0 0 640 640",
		HelpKey:         "helpMenuInspect",
		HelpFallback:    "Opens the property editor for this device. Change its label, configure its type, edit its internal options. Closes without applying when you tap outside.",
		OnClick:         onClick,
	}
}

// ResizeItem returns the standard "Resize" entry used by frontend
// context menus (Chart, ChartPro, PieChart, TextDisplay,
// BackgroundImage). Resize never appears in backend menus — the
// backend body does not need resize affordances because devices
// there auto-size to their content.
//
// The OnClick callback should call SetResizeEnable(!GetResizeEnable())
// or equivalent on the device's frontend element.
//
// Português: Retorna a entrada padrão "Resize" usada pelos menus
// de contexto do frontend. Resize nunca aparece em menus de backend.
func ResizeItem(onClick func()) contextMenu.Item {
	return contextMenu.Item{
		ID:              "resize",
		Label:           translate.T("menuDeviceResize", "Resize"),
		FontAwesomePath: rulesIcon.KFAArrowsUpDownLeftRight,
		ViewBox:         "0 0 512 512",
		HelpKey:         "helpMenuResize",
		HelpFallback:    "Toggles corner handles on the device so you can drag to change its size. Tap Resize again to hide the handles.",
		OnClick:         onClick,
	}
}
