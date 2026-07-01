// platform/factoryColor/newMagentaTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewMagentaTransparent() color.RGBA {
	return color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0x00} // rgb(255, 0, 255)
}
