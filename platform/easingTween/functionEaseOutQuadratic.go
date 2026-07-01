// platform/easingTween/functionEaseOutQuadratic.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

// en: quadratic easing out - decelerating to zero velocity
var KEaseOutQuadratic = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	return -1*delta*currentPercentage*(currentPercentage-2) + startValue
}
