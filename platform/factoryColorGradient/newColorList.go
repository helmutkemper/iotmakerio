// platform/factoryColorGradient/newColorList.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryColorGradient

import "github.com/helmutkemper/iotmaker.santa_isabel_theater.platform/abstractType/gradient"

func NewColorList(list ...gradient.ColorStop) []gradient.ColorStop {
	return list
}
