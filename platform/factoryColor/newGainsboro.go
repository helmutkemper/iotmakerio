// platform/factoryColor/newGainsboro.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewGainsboro() color.RGBA {
	return color.RGBA{R: 0xdc, G: 0xdc, B: 0xdc, A: 0xff} // rgb(220, 220, 220)
}
