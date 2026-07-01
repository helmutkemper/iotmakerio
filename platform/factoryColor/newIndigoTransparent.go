// platform/factoryColor/newIndigoTransparent.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewIndigoTransparent() color.RGBA {
	return color.RGBA{R: 0x4b, G: 0x00, B: 0x82, A: 0x00} // rgb(75, 0, 130)
}
