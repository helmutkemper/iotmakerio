// /ide/ui/mainMenu/wireMenu.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Reusable builders for connector-port context menu items. Each
//	device port click produces a small list: Connect (plus any
//	device-specific extras). The same logic used to live in every
//	device — this package centralises it so the Connect action, its
//	icon, and its help text stay identical across the IDE.
//
//	The Connect action starts visual connect mode: the menu closes,
//	candidate connectors are highlighted on the canvas with glowing
//	dots, and a semi-transparent draft wire follows the pointer.
//	The user can pan/zoom freely, then click a candidate to connect,
//	or click elsewhere / press Escape to cancel.
//
//	  - ConnectorMenu:        Connect + extras (for input ports and
//	                          output ports that also carry a Monitor).
//	  - ConnectorConnectMenu: Connect only (for output-only const
//	                          devices, where Disconnect is not offered).
//
//	Disconnect is not part of these menus by design. The maker
//	disconnects wires by clicking the wire itself or the receiving
//	end, not the source — this keeps the source menu minimal.
//
// Português:
//
//	Construtores reutilizáveis de itens de menu para portas de
//	conector. Clique em cada porta produz uma lista pequena:
//	Connect (mais extras específicos do device).
package mainMenu

import (
	"log"

	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
	"github.com/helmutkemper/iotmakerio/wire"
)

// ConnectorConnectMenu returns a connector menu with only the
// Connect action. Used by output-only constant devices (ConstInt,
// ConstBool, ConstFloat, ConstString, ConstDuration) where the
// user has no reason to disconnect from the source side.
//
// Português: Retorna um menu de conector com apenas a ação Connect.
// Usado em devices de constante onde o usuário não desconecta pela
// saída.
func ConnectorConnectMenu(mgr *wire.Manager, elemID, port string) []contextMenu.Item {
	return []contextMenu.Item{connectItem(mgr, elemID, port)}
}

// ConnectorMenu returns standard connector menu items: Connect
// first, followed by any caller-supplied extras (e.g. a Monitor
// submenu for output ports).
//
// Português: Retorna itens padrão para conector: Connect primeiro,
// seguido de extras fornecidos pelo chamador.
func ConnectorMenu(mgr *wire.Manager, elemID, port string, extra ...contextMenu.Item) []contextMenu.Item {
	items := make([]contextMenu.Item, 0, 1+len(extra))
	items = append(items, connectItem(mgr, elemID, port))
	items = append(items, extra...)
	return items
}

// connectItem builds the shared Connect row used by both public
// functions above. Kept private because callers should not
// instantiate it with different defaults — consistency across the
// IDE is the point of this file.
func connectItem(mgr *wire.Manager, elemID, port string) contextMenu.Item {
	return contextMenu.Item{
		ID:              "connect_" + port,
		Label:           translate.T("menuDeviceConnect", "Connect"),
		FontAwesomePath: rulesIcon.KFALink,
		ViewBox:         "0 0 640 512",
		HelpKey:         "helpMenuConnect",
		HelpFallback:    "Starts visual connect mode. Compatible connectors light up on the stage; click one to draw a wire. Click outside or press Escape to cancel.",
		OnClick: func() {
			sourceID := wire.ConnectorID{ElementID: elemID, PortName: port}
			candidates := mgr.StartConnect(sourceID)
			if len(candidates) == 0 {
				mgr.CancelConnect()
				log.Printf("[WIRE] No compatible targets for %v.%v", elemID, port)
				return
			}
			log.Printf("[WIRE] Visual connect started from %v.%v (%d candidates)",
				elemID, port, len(candidates))
		},
	}
}
