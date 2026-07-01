// platform/factoryColor/newHoneydewTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewHoneydewTransparent() color.RGBA {
	return color.RGBA{R: 0xf0, G: 0xff, B: 0xf0, A: 0x00} // rgb(240, 255, 240)
}
