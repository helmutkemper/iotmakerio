// ornament/math/ornamentDivider.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package math

import "github.com/helmutkemper/iotmakerio/ornament/device"

type OrnamentDivider struct {
	device.OrnamentOpAmpSymbol
	Symbol  string
	AdjustX int
	AdjustY int
}

func (e *OrnamentDivider) Init() (err error) {
	e.OrnamentOpAmpSymbol.Init()
	e.OrnamentOpAmpSymbol.SetSymbol("÷")
	e.OrnamentOpAmpSymbol.SetAdjustX(0)
	e.OrnamentOpAmpSymbol.SetAdjustY(3)
	return
}
