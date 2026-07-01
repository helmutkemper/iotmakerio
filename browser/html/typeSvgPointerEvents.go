// browser/html/typeSvgPointerEvents.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgPointerEvents string

func (e SvgPointerEvents) String() string {
	return string(e)
}

const (
	KSvgPointerEventsBoundingBox    SvgPointerEvents = "bounding-box"
	KSvgPointerEventsVisiblePainted SvgPointerEvents = "visiblePainted"
	KSvgPointerEventsVisibleFill    SvgPointerEvents = "visibleFill"
	KSvgPointerEventsVisibleStroke  SvgPointerEvents = "visibleStroke"
	KSvgPointerEventsVisible        SvgPointerEvents = "visible"
	KSvgPointerEventsPainted        SvgPointerEvents = "painted"
	KSvgPointerEventsFill           SvgPointerEvents = "fill"
	KSvgPointerEventsStroke         SvgPointerEvents = "stroke"
	KSvgPointerEventsAll            SvgPointerEvents = "all"
	KSvgPointerEventsNone           SvgPointerEvents = "none"
)
