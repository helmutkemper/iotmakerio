// platform/factoryColor/newLimegreenTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewLimegreenTransparent() color.RGBA {
	return color.RGBA{R: 0x32, G: 0xcd, B: 0x32, A: 0x00} // rgb(50, 205, 50)
}
