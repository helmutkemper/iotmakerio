// platform/factoryColor/newGoldenrodTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewGoldenrodTransparent() color.RGBA {
	return color.RGBA{R: 0xda, G: 0xa5, B: 0x20, A: 0x00} // rgb(218, 165, 32)
}
