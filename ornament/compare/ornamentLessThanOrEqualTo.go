package compare

import (
	"github.com/helmutkemper/iotmakerio/ornament/device"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

type OrnamentLessThanOrEqualTo struct {
	device.OrnamentOpAmpSymbol
}

func (e *OrnamentLessThanOrEqualTo) Init() (err error) {
	_ = e.OrnamentOpAmpSymbol.Init()
	e.OrnamentOpAmpSymbol.SetSymbolFontSize(16)
	e.OrnamentOpAmpSymbol.SetSymbol("≤")
	e.OrnamentOpAmpSymbol.SetAdjustX(0)
	e.OrnamentOpAmpSymbol.SetAdjustY(2)
	return
}

func (e *OrnamentLessThanOrEqualTo) Update(x, y, width, height rulesDensity.Density) (err error) {
	_ = e.OrnamentOpAmpSymbol.Update(x, y, width, height)
	return
}
