// platform/factoryColor/newDimgreyTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewDimgreyTransparent() color.RGBA {
	return color.RGBA{R: 0x69, G: 0x69, B: 0x69, A: 0x00} // rgb(105, 105, 105)
}
