// platform/factoryColor/newLime.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewLime() color.RGBA {
	return color.RGBA{R: 0x00, G: 0xff, B: 0x00, A: 0xff} // rgb(0, 255, 0)
}
