// platform/factoryColor/newVioletTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewVioletTransparent() color.RGBA {
	return color.RGBA{R: 0xee, G: 0x82, B: 0xee, A: 0x00} // rgb(238, 130, 238)
}
