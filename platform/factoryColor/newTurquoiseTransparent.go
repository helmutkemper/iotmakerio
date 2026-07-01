// platform/factoryColor/newTurquoiseTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewTurquoiseTransparent() color.RGBA {
	return color.RGBA{R: 0x40, G: 0xe0, B: 0xd0, A: 0x00} // rgb(64, 224, 208)
}
