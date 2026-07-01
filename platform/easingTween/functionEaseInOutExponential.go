// platform/easingTween/functionEaseInOutExponential.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

import "math"

// en: exponential easing in/out - accelerating until halfway, then decelerating
var KEaseInOutExponential = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	interactionCurrent /= interactionTotal / 2
	if interactionCurrent < 1 {
		return delta/2*math.Pow(2, 10*(interactionCurrent-1)) + startValue
	}
	interactionCurrent--
	return delta/2*(-1*math.Pow(2, -10*interactionCurrent)+2) + startValue
}
