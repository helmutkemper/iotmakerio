package compare

import (
	"github.com/helmutkemper/iotmakerio/ornament/device"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

type OrnamentGreaterThan struct {
	device.OrnamentOpAmpSymbol
}

func (e *OrnamentGreaterThan) Init() (err error) {
	_ = e.OrnamentOpAmpSymbol.Init()
	e.OrnamentOpAmpSymbol.SetSymbolFontSize(16)
	e.OrnamentOpAmpSymbol.SetSymbol(">")
	e.OrnamentOpAmpSymbol.SetAdjustX(0)
	e.OrnamentOpAmpSymbol.SetAdjustY(2)
	return
}

func (e *OrnamentGreaterThan) Update(x, y, width, height rulesDensity.Density) (err error) {
	_ = e.OrnamentOpAmpSymbol.Update(x, y, width, height)
	return
}
