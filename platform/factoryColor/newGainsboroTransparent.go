// platform/factoryColor/newGainsboroTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewGainsboroTransparent() color.RGBA {
	return color.RGBA{R: 0xdc, G: 0xdc, B: 0xdc, A: 0x00} // rgb(220, 220, 220)
}
