// platform/factoryColor/newWhitesmokeHalfTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewWhitesmokeHalfTransparent() color.RGBA {
	return color.RGBA{R: 0xf5, G: 0xf5, B: 0xf5, A: 0x80} // rgb(245, 245, 245)
}
