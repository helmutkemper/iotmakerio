// browser/event/mouse/_SetMouseDoubleClickEvent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package mouse

import (
	mouse "github.com/helmutkemper/iotmakerio/platform/channels"
	"syscall/js"
)

var mouseDoubleClickEvt js.Func

// SetMouseDoubleClickEvent
//
// English:
//
//  Mouse double click coupling function, passing (x, y) in mouse
//  channel.BrowserMouseDoubleClickToPlatformMouseDoubleClickEvent
//
// Português:
//
//  Função de acoplamento do clique duplo do mouse, transmitindo (x, y) no canal
//  mouse.BrowserMouseDoubleClickToPlatformMouseDoubleClickEvent
func SetMouseDoubleClickEvent() js.Func {
	mouseDoubleClickEvt = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		e := args[0]
		X = e.Get("clientX").Int()
		Y = e.Get("clientY").Int()

		mouse.BrowserMouseDoubleClickToPlatformMouseDoubleClickEvent <- mouse.DoubleClick{X: X, Y: Y}

		return nil
	})

	return mouseDoubleClickEvt
}

func ReleaseMouseDoubleClickEvent() {
	mouseDoubleClickEvt.Release()
}
