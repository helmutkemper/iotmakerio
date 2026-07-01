// platform/easingTween/functionEaseInOutQuintic.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

import "math"

// en: quintic easing in/out - acceleration until halfway, then deceleration
var KEaseInOutQuintic = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	currentPercentage = currentPercentage * 2
	if currentPercentage < 1 {
		return delta/2*math.Pow(currentPercentage, 5.0) + startValue
	}
	currentPercentage -= 2
	return delta/2*(math.Pow(currentPercentage, 5.0)+2) + startValue
}
