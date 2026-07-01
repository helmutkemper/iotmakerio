// platform/easingTween/functionEaseInQuintic.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

import "math"

// en: quintic easing in - accelerating from zero velocity
var KEaseInQuintic = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	return delta*math.Pow(currentPercentage, 5.0) + startValue
}
