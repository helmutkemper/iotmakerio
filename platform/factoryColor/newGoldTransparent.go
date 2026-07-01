// platform/factoryColor/newGoldTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewGoldTransparent() color.RGBA {
	return color.RGBA{R: 0xff, G: 0xd7, B: 0x00, A: 0x00} // rgb(255, 215, 0)
}
