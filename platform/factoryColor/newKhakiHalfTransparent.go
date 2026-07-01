// platform/factoryColor/newKhakiHalfTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewKhakiHalfTransparent() color.RGBA {
	return color.RGBA{R: 0xf0, G: 0xe6, B: 0x8c, A: 0x80} // rgb(240, 230, 140)
}
