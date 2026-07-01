// platform/factoryColor/newDarkcyanTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewDarkcyanTransparent() color.RGBA {
	return color.RGBA{R: 0x00, G: 0x8b, B: 0x8b, A: 0x00} // rgb(0, 139, 139)
}
