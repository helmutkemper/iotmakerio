// /ide/rulesDevice/palette.go

package rulesDevice

// palette.go — Visual design system for all IDE devices.
//
// This file is the single source of truth for colors, sizes, typography, and
// geometry used by every device on the canvas.  All device packages import
// rulesDevice and reference these constants by name instead of embedding magic
// numbers in their SVG generation code.
//
// ─── Design philosophy ────────────────────────────────────────────────────────
//
// Color carries meaning:
//   The color of a connector dot (and its wire) always matches the Go data type
//   it carries.  A device's accent/border color matches its output type.
//   This gives the user an instant visual cue: if two connectors share the same
//   color, they are compatible.
//
// Hierarchy by size:
//   - Constant devices  (no inputs)  — compact, ~120×56px
//   - Operation devices (2→1)        — medium,  ~160×70px
//   - Black-box devices (N→M)        — tall,    dynamic height
//
// Every device shares the same font family, border radius, and border width so
// the canvas looks like a coherent system, not a patchwork.
//
// ─── Quick reference ──────────────────────────────────────────────────────────
//
// Type → border / connector color:
//   int / int64   →  KColorTypeInt     (#5599FF, blue)
//   int32         →  KColorTypeInt32   (#3377EE, darker blue)
//   float32       →  KColorTypeFloat32 (#44CC88, green)
//   float64       →  KColorTypeFloat64 (#55DDAA, teal-green)
//   bool          →  KColorTypeBool    (#FF8833, orange)
//   string        →  KColorTypeString  (#FFCC33, amber)
//   error         →  KColorTypeError   (#FF3333, red — wire stroke is 3px)
//   byte/[]byte   →  KColorTypeByte    (#AA88FF, purple)
//   time.Duration     →  KColorTypeDuration (#00CCCC, cyan)
//
// Backgrounds:
//   All devices share KColorDeviceBg for the main body and KColorDeviceHeader
//   for the top header bar.  Keeping these identical across all devices makes
//   the canvas visually cohesive.
//
// ─── Changing colors ──────────────────────────────────────────────────────────
//
// To retheme the entire IDE:  edit only this file.
// To change one type's color: update the single constant for that type.
//   Every device, wire, and connector that represents that type will update
//   automatically on the next render.

// ─── Type color palette ───────────────────────────────────────────────────────
// One unique, accessible color per Go data type.
// Used for: device border/accent, connector dot fill, wire stroke.

const (
	// KColorTypeInt is the color for int and int64 values.
	// Blue — the most common numeric type in Go.
	KColorTypeInt = "#5599FF"

	// KColorTypeInt32 is the color for int32 values.
	// Slightly darker than KColorTypeInt to distinguish at a glance.
	KColorTypeInt32 = "#3377EE"

	// KColorTypeFloat32 is the color for float32 values.
	// Green — floating-point numbers in low-precision mode.
	KColorTypeFloat32 = "#44CC88"

	// KColorTypeFloat64 is the color for float64 values.
	// Teal-green — floating-point numbers in full-precision mode.
	KColorTypeFloat64 = "#55DDAA"

	// KColorTypeBool is the color for bool values.
	// Orange — warm, stands out clearly against the dark background.
	KColorTypeBool = "#FF8833"

	// KColorTypeString is the color for string values.
	// Amber/gold — text data should feel "warm" and distinct from numbers.
	KColorTypeString = "#FFCC33"

	// KColorTypeError is the color for error values.
	// Red — errors must be impossible to miss.
	// Note: when a wire carries the error type its stroke width is 3px (not 1.5px)
	// to make the data path visually heavier and more attention-grabbing.
	KColorTypeError = "#FF3333"

	// KColorTypeByte is the color for byte and []byte values.
	// Purple — raw binary data; distinct from all numeric types.
	KColorTypeByte = "#AA88FF"

	// KColorTypeDuration is the color for time.Duration values.
	// Cyan — temporal data; visually distinct from all numeric and boolean types.
	// This prevents makers from accidentally connecting an int64 wire to a
	// Duration input (or vice-versa), even though the underlying Go type is int64.
	// Semantic type safety through visual identity.
	KColorTypeDuration = "#00CCCC"
)

// ─── Device background colors ─────────────────────────────────────────────────

const (
	// KColorDeviceBg is the main body fill for all device types.
	// Very dark navy — gives the canvas a professional, IDE-like feel while
	// making the colored connectors pop.
	KColorDeviceBg = "#1a1e2e"

	// KColorDeviceHeader is the header bar background.
	// Slightly lighter than KColorDeviceBg so the header is visible without a
	// heavy border, creating a subtle inset effect.
	KColorDeviceHeader = "#252a3e"

	// KColorDeviceDivider is the horizontal line separating header from body.
	// Slightly lighter than KColorDeviceHeader.
	KColorDeviceDivider = "#323854"

	// KColorDeviceText is the primary text color for values and port labels.
	// Soft blue-white — easy on the eyes, high contrast against the dark body.
	KColorDeviceText = "#DDEEFF"

	// KColorDeviceTextMuted is for secondary labels (port names, type hints).
	// Dimmer than KColorDeviceText to create visual hierarchy.
	KColorDeviceTextMuted = "#8899AA"

	// KColorConnectorStroke is always white for the 1px outline of every
	// connector dot.  The white ring makes connectors visible even when the
	// dot fill color is close to the background color.
	KColorConnectorStroke = "#FFFFFF"
)

// ─── Typography ───────────────────────────────────────────────────────────────

const (
	// KDeviceFontFamily is the font stack used for all text inside SVG devices.
	// Arial is universally available and renders cleanly at small sizes.
	// The sans-serif fallback covers all platforms.
	KDeviceFontFamily = "Arial,sans-serif"

	// KDeviceFontSizeTypeTag is the font size for the short type tag in the
	// device header (e.g. "INT", "BOOL", "F32", "STR", "DUR").
	KDeviceFontSizeTypeTag = 10

	// KDeviceFontSizeValue is the font size for the main value display
	// (the number or text shown in the center of a constant device).
	KDeviceFontSizeValue = 16

	// KDeviceFontSizePort is the font size for input/output port labels.
	// Used in operation devices (Add, Mul, etc.) and black-box devices.
	KDeviceFontSizePort = 11

	// KDeviceFontSizeLabel is the font size for the editable device name label
	// that appears below the device ornament.
	// Kept at 12px to be readable but visually subordinate to the device body.
	KDeviceFontSizeLabel = 12
)

// ─── Geometry ─────────────────────────────────────────────────────────────────

const (
	// KDeviceBorderWidth is the stroke width of the outer device rectangle.
	// 2px gives enough weight to be visible without looking heavy.
	KDeviceBorderWidth = 2.0

	// KDeviceCornerRadius is the rx/ry corner radius for all device rectangles.
	// 5px — modern but not too rounded; looks professional on a dark canvas.
	KDeviceCornerRadius = 5.0

	// KDeviceHeaderHeight is the height of the header bar at the top of every
	// device.  Constant and operation devices use a thinner header than
	// black-box devices (which use their own bbHeaderH constant).
	KDeviceHeaderHeight = 18.0

	// KConnectorRadius is the visual radius of every connector dot.
	KConnectorRadius = 5.0

	// KConnectorOffsetRight is the distance from the right edge of the device
	// body to the center of the output connector dot.
	// Connector center x = device_width - KConnectorOffsetRight.
	KConnectorOffsetRight = 8.0

	// KConnectorOffsetLeft is the distance from the left edge of the device
	// body to the center of an input connector dot.
	// Connector center x = KConnectorOffsetLeft.
	KConnectorOffsetLeft = 8.0

	// KConnectorHitRadius is the radius used for click hit-testing on
	// connectors.  Slightly larger than KConnectorRadius so the user does not
	// have to click pixel-perfectly on the dot.
	KConnectorHitRadius = 10.0
)

// ─── Constant device sizes ────────────────────────────────────────────────────
// Constant devices are output-only boxes (no inputs).  They are intentionally
// smaller than operation devices so the canvas is not cluttered by simple
// literal values.

const (
	// KConstDefaultWidth is the initial ornament width for a constant device.
	// 120px fits "false" (the longest bool text) plus the connector comfortably.
	KConstDefaultWidth = 120.0

	// KConstDefaultHeight is the initial ornament height for a constant device.
	// 56px gives enough room for the 18px header + 38px value area.
	// The element total height is KConstDefaultHeight + KLabelHeight = 74px.
	KConstDefaultHeight = 56.0

	// KConstMinWidth is the minimum allowed ornament width after a resize.
	KConstMinWidth = 80.0

	// KConstMinHeight is the minimum allowed ornament height after a resize.
	KConstMinHeight = 44.0

	// KConstDurationDefaultWidth is the initial ornament width for a duration
	// constant device.  Wider than other constants because it displays both
	// a numeric value and a unit label (e.g. "5 Second").
	KConstDurationDefaultWidth = 150.0
)

// ─── Type label map ───────────────────────────────────────────────────────────
// Maps a Go type string to (short tag, accent color).
// Devices can call TypeTagAndColor to avoid embedding type-to-color logic.

// TypeStyle bundles the short display tag and accent color for a Go type.
type TypeStyle struct {
	// Tag is the 2-4 character label shown in the device header (e.g. "INT").
	Tag string
	// Color is the CSS hex color for the border and connector dot.
	Color string
}

// TypeStyleFor returns the display tag and accent color for the given Go type
// string.  Falls back to a neutral style if the type is unrecognised.
//
// This is the canonical place to look up "what color is a float32?" —
// search for this function to find all type-color mappings.
func TypeStyleFor(goType string) TypeStyle {
	switch goType {
	case "int", "int64":
		return TypeStyle{Tag: "INT", Color: KColorTypeInt}
	case "int32":
		return TypeStyle{Tag: "I32", Color: KColorTypeInt32}
	case "float32":
		return TypeStyle{Tag: "F32", Color: KColorTypeFloat32}
	case "float64":
		return TypeStyle{Tag: "F64", Color: KColorTypeFloat64}
	case "bool":
		return TypeStyle{Tag: "BOOL", Color: KColorTypeBool}
	case "string":
		return TypeStyle{Tag: "STR", Color: KColorTypeString}
	case "error":
		return TypeStyle{Tag: "ERR", Color: KColorTypeError}
	case "byte", "[]byte":
		return TypeStyle{Tag: "BYTE", Color: KColorTypeByte}
	case "time.Duration":
		return TypeStyle{Tag: "DUR", Color: KColorTypeDuration}
	default:
		// Slice of a known element type ("[]int", "[]float32", …): derive
		// the style from the ELEMENT — same accent color, tag prefixed
		// with "[]". This matches the wire system's rule for collections
		// (wire/registry.go: a slice wire uses the base type's color,
		// drawn thicker), so a collection device and its wire stay
		// color-coherent with the scalar family they belong to. Note
		// "[]byte" never reaches here — it has its explicit case above
		// (a byte slice is a buffer, not a numeric collection).
		//
		// Português: Fatia de um tipo conhecido — deriva o estilo do
		// ELEMENTO (mesma cor, tag com prefixo "[]"), espelhando a regra
		// do wire/registry.go (fio de coleção usa a cor do tipo base,
		// mais grosso). "[]byte" não chega aqui — tem case explícito.
		if len(goType) > 2 && goType[:2] == "[]" {
			elem := TypeStyleFor(goType[2:])
			if elem.Tag != "???" {
				return TypeStyle{Tag: "[]" + elem.Tag, Color: elem.Color}
			}
		}
		// Unknown types render in neutral grey.
		return TypeStyle{Tag: "???", Color: KColorDeviceTextMuted}
	}
}
