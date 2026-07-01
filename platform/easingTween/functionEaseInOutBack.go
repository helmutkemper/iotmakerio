// platform/easingTween/functionEaseInOutBack.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

import "math"

var KEaseInOutBack = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	currentPercentage = currentPercentage / 0.5

	if currentPercentage < 1 {
		return 0.5*(math.Pow(currentPercentage, 2.0)*(3.5949095*currentPercentage-2.5949095))*delta + startValue
	}
	currentPercentage -= 2
	return 0.5*(math.Pow(currentPercentage, 2.0)*(3.5949095*currentPercentage+2.5949095)+2)*delta + startValue
}
