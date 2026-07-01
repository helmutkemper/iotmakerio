// platform/factoryColor/newPlumTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewPlumTransparent() color.RGBA {
	return color.RGBA{R: 0xdd, G: 0xa0, B: 0xdd, A: 0x00} // rgb(221, 160, 221)
}
