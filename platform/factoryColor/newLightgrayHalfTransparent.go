// platform/factoryColor/newLightgrayHalfTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewLightgrayHalfTransparent() color.RGBA {
	return color.RGBA{R: 0xd3, G: 0xd3, B: 0xd3, A: 0x80} // rgb(211, 211, 211)
}
