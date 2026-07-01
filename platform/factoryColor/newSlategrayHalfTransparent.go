// platform/factoryColor/newSlategrayHalfTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewSlategrayHalfTransparent() color.RGBA {
	return color.RGBA{R: 0x70, G: 0x80, B: 0x90, A: 0x80} // rgb(112, 128, 144)
}
