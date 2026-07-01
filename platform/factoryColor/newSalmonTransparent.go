// platform/factoryColor/newSalmonTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewSalmonTransparent() color.RGBA {
	return color.RGBA{R: 0xfa, G: 0x80, B: 0x72, A: 0x00} // rgb(250, 128, 114)
}
