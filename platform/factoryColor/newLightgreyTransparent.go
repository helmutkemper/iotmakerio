// platform/factoryColor/newLightgreyTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewLightgreyTransparent() color.RGBA {
	return color.RGBA{R: 0xd3, G: 0xd3, B: 0xd3, A: 0x00} // rgb(211, 211, 211)
}
