// platform/factoryColor/newTomatoTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewTomatoTransparent() color.RGBA {
	return color.RGBA{R: 0xff, G: 0x63, B: 0x47, A: 0x00} // rgb(255, 99, 71)
}
