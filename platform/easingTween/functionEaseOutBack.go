// platform/easingTween/functionEaseOutBack.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package easingTween

import "math"

var KEaseOutBack = func(interactionCurrent, interactionTotal, currentPercentage, startValue, endValue, delta float64) float64 {
	currentPercentage = currentPercentage - 1
	return (math.Pow(currentPercentage, 2.0)*(2.70158*currentPercentage+1.70158)+1)*delta + startValue
}
