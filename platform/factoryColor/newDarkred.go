// platform/factoryColor/newDarkred.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewDarkred() color.RGBA {
	return color.RGBA{R: 0x8b, G: 0x00, B: 0x00, A: 0xff} // rgb(139, 0, 0)
}
