// platform/factoryColor/newLavenderTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewLavenderTransparent() color.RGBA {
	return color.RGBA{R: 0xe6, G: 0xe6, B: 0xfa, A: 0x00} // rgb(230, 230, 250)
}
