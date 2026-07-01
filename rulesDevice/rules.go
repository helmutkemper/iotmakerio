// /ide/rulesDevice/rules.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesDevice

// rules.go — Layout and format constants for device ornaments.
//
// For the full visual design system (colors, type palette, sizes) see
// palette.go in this same package.

// KDeviceLabel is the SVG text element format string for the editable device
// name label rendered below every device ornament.
//
// Usage (inside a renderSVG function):
//
//	labelY := ornamentHeight + 3
//	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, labelY, displayLabel)
//
// Uses KColorDeviceTextMuted (#8899AA) — readable on dark canvas without
// competing visually with the device body content.
const KDeviceLabel = `<text x="4" y="%.1f" font-family="Arial,sans-serif" font-size="12" fill="#8899AA" dominant-baseline="hanging">%s</text>`

// KLabelHeight is the pixel height reserved below the device ornament for the
// editable label.  Must be added to the ornament height when creating the
// sprite element and when generating the SVG viewport.
//
//	svgTotalH := ornamentH + KLabelHeight
//	elem height = svgTotalH
const KLabelHeight = 18
