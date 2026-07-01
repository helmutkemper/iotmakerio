// ornament/compare/ornamentGreaterThanOrEqualTo.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compare

import (
	"github.com/helmutkemper/iotmakerio/ornament/device"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

type OrnamentGreaterThanOrEqualTo struct {
	device.OrnamentOpAmpSymbol
}

func (e *OrnamentGreaterThanOrEqualTo) Init() (err error) {
	_ = e.OrnamentOpAmpSymbol.Init()
	e.OrnamentOpAmpSymbol.SetSymbolFontSize(16)
	e.OrnamentOpAmpSymbol.SetSymbol("≥")
	e.OrnamentOpAmpSymbol.SetAdjustX(0)
	e.OrnamentOpAmpSymbol.SetAdjustY(2)
	return
}

func (e *OrnamentGreaterThanOrEqualTo) Update(x, y, width, height rulesDensity.Density) (err error) {
	_ = e.OrnamentOpAmpSymbol.Update(x, y, width, height)
	return
}
