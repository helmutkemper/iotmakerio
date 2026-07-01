// platform/factoryColor/newSeashell.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColor

import "image/color"

func NewSeashell() color.RGBA {
	return color.RGBA{R: 0xff, G: 0xf5, B: 0xee, A: 0xff} // rgb(255, 245, 238)
}
