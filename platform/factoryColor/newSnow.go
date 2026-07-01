// platform/factoryColor/newSnow.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewSnow() color.RGBA {
	return color.RGBA{R: 0xff, G: 0xfa, B: 0xfa, A: 0xff} // rgb(255, 250, 250)
}
