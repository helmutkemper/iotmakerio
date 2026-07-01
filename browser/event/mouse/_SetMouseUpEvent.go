// browser/event/mouse/_SetMouseUpEvent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package mouse

import (
	mouse "github.com/helmutkemper/iotmakerio/platform/channels"
	"syscall/js"
)

var mouseUpEvt js.Func

// SetMouseUpEvent
//
// English:
//
//  Mouse up coupling function, passing (x, y) in mouse
//  channel.BrowserMouseUpToPlatformMouseUpEvent
//
// Português:
//
//  Função de acoplamento do mouse up, transmitindo (x, y) no canal
//  mouse.BrowserMouseUpToPlatformMouseUpEvent
func SetMouseUpEvent() js.Func {
	mouseUpEvt = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		e := args[0]
		X = e.Get("clientX").Int()
		Y = e.Get("clientY").Int()

		mouse.BrowserMouseUpToPlatformMouseUpEvent <- mouse.Release{X: X, Y: Y}

		return nil
	})

	return mouseUpEvt
}

func ReleaseMouseUpEvent() {
	mouseUpEvt.Release()
}
