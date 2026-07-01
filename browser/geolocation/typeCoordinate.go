// browser/geolocation/typeCoordinate.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package geolocation

type Coordinate struct {
	Latitude     float64
	Longitude    float64
	Accuracy     float64
	ErrorCode    int
	ErrorMessage string
}
