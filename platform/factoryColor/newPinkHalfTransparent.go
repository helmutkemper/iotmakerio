// platform/factoryColor/newPinkHalfTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewPinkHalfTransparent() color.RGBA {
	return color.RGBA{R: 0xff, G: 0xc0, B: 0xcb, A: 0x80} // rgb(255, 192, 203)
}
