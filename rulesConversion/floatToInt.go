package rulesConversion

import (
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func FloatToInt(f float64) (i int) {
	//return int(f)
	return utilsMath.FloatToInt(f)
}
