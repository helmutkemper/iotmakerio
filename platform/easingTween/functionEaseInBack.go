// platform/easingTween/functionEaseInBack.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

var KEaseInBack = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	x := currentPercentage
	p := 2.70158*x*x*x - 1.70158*x*x
	return delta*p + startValue
}
