// rulesConversion/floatToInt.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesConversion

import (
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func FloatToInt(f float64) (i int) {
	//return int(f)
	return utilsMath.FloatToInt(f)
}
