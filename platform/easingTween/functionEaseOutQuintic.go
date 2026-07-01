// platform/easingTween/functionEaseOutQuintic.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

import "math"

// en: quintic easing out - decelerating to zero velocity
var KEaseOutQuintic = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	interactionCurrent /= interactionTotal
	interactionCurrent--
	return delta*(math.Pow(interactionCurrent, 5.0)+1) + startValue
}
