// platform/factoryColor/newFirebrick.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewFirebrick() color.RGBA {
	return color.RGBA{R: 0xb2, G: 0x22, B: 0x22, A: 0xff} // rgb(178, 34, 34)
}
