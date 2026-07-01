// platform/factoryColor/newBisqueTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewBisqueTransparent() color.RGBA {
	return color.RGBA{R: 0xff, G: 0xe4, B: 0xc4, A: 0x00} // rgb(255, 228, 196)
}
