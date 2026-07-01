// ornament/math/ornamentMultiplier.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package math

import "github.com/helmutkemper/iotmakerio/ornament/device"

type OrnamentMultiplier struct {
	device.OrnamentOpAmpSymbol
}

func (o *OrnamentMultiplier) Init() (err error) {
	o.OrnamentOpAmpSymbol.Init()
	o.OrnamentOpAmpSymbol.SetSymbol("×")
	o.OrnamentOpAmpSymbol.SetAdjustX(0)
	o.OrnamentOpAmpSymbol.SetAdjustY(3)
	return
}
