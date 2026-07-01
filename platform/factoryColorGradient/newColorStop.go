// platform/factoryColorGradient/newColorStop.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColorGradient

import (
	"github.com/helmutkemper/iotmaker.santa_isabel_theater.platform/abstractType/gradient"
	"image/color"
)

func NewColorPosition(color color.RGBA, percentPosition float64) gradient.ColorStop {
	return gradient.ColorStop{
		Color: color,
		Stop:  percentPosition,
	}
}
