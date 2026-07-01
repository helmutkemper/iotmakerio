// platform/factoryColor/newTeal.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewTeal() color.RGBA {
	return color.RGBA{R: 0x00, G: 0x80, B: 0x80, A: 0xff} // rgb(0, 128, 128)
}
