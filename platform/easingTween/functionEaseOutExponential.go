// platform/easingTween/functionEaseOutExponential.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

import "math"

// en: exponential easing out - decelerating to zero velocity
var KEaseOutExponential = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	return delta*(-1*math.Pow(2, -10*currentPercentage)+1) + startValue
}
