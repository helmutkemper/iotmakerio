// platform/factoryColor/newGhostwhite.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewGhostwhite() color.RGBA {
	return color.RGBA{R: 0xf8, G: 0xf8, B: 0xff, A: 0xff} // rgb(248, 248, 255)
}
