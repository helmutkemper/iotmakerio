// platform/factoryColor/newLightcyanTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewLightcyanTransparent() color.RGBA {
	return color.RGBA{R: 0xe0, G: 0xff, B: 0xff, A: 0x00} // rgb(224, 255, 255)
}
