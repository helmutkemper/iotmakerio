// rulesDevice/palette.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesDevice

import "strings"

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

	// KColorTypeUint is the color for uint, uint16 and uint32 values.
	// Deep purple — the unsigned family, clearly separated from the signed
	// blues. Inherited from the historical wire palette so existing scenes
	// keep their hue after the palette unification.
	//
	// NOTE: uint8 is intentionally NOT in this family. In Go, byte IS uint8
	// (a type alias), so uint8 must share KColorTypeByte — otherwise the same
	// value would change color depending on how the specialist spelled the
	// type in their black-box source.
	//
	// Português: Roxo profundo — a família sem sinal, separada dos azuis com
	// sinal. Herdado da paleta histórica dos fios. uint8 fica FORA desta
	// família de propósito: em Go, byte É uint8 (alias), então uint8 usa
	// KColorTypeByte — senão o mesmo valor mudaria de cor conforme a grafia
	// escolhida pelo especialista.
	KColorTypeUint = "#5E35B1"

	// KColorTypeUint64 is the color for uint64 values.
	// Darkest purple — distinguishes the 64-bit unsigned width at a glance,
	// mirroring how int64 vs int32 use different blues.
	KColorTypeUint64 = "#4527A0"

	// KColorTypeStruct is the catch-all color for pointer types (*machine.I2C)
	// and package-qualified named types (spi.Config) coming from black-box
	// components. Violet reads as "structured hardware data" and is distinct
	// from every primitive color. This is the same violet the wire system has
	// always used for its "struct" fallback — now defined in one place.
	//
	// Português: Cor pega-tudo para tipos ponteiro e tipos nomeados
	// qualificados por pacote vindos de black-boxes. Violeta comunica "dado
	// estruturado de hardware". É o mesmo violeta que o wire sempre usou no
	// fallback "struct" — agora definido em um único lugar.
	KColorTypeStruct = "#9C27B0"

	// KColorFamilyDebug is the identity color of the Debug device family
	// (the Print sinks): a burnt orange, deliberately DISTINCT from the
	// bool wire orange (KColorTypeBool #FF8833) so a PrintBool still reads
	// as "orange box, brighter-orange pin" instead of one undifferentiated
	// blob. Family colors paint the BOX (border + header tag); pins, wires
	// and the type label always keep the value type's own color.
	//
	// Português: Cor de identidade da família Debug (os sinks Print):
	// laranja queimado, DISTINTO de propósito do laranja do fio bool
	// (KColorTypeBool #FF8833) — um PrintBool lê como "caixa laranja, pino
	// laranja mais vivo" em vez de um borrão único. Cores de família pintam
	// a CAIXA (borda + tag do header); pinos, fios e o label de tipo sempre
	// ficam na cor do próprio tipo.
	KColorFamilyDebug = "#E8590C"

	// KColorWireSelected — a wire SELECTED by click, i.e. armed for
	// deletion (the next click deletes it). Deliberately loud and shared
	// by every wire type: the lightened per-type color used before was too
	// discreet to read as "about to be deleted". Vivid warning red-pink,
	// distinct from the conflict-rectangle red and from every type color
	// in the palette.
	// Português: Wire SELECIONADO por clique, ou seja, armado para
	// exclusão (o próximo clique apaga). Deliberadamente chamativo e
	// compartilhado por todos os tipos: a cor clareada por tipo usada
	// antes era discreta demais para ler como "prestes a ser apagado".
	// Vermelho-rosa vívido de alerta, distinto do vermelho dos retângulos
	// de conflito e de todas as cores de tipo da paleta.
	KColorWireSelected = "#FF2D55"
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

	// The connector-dot stroke color (KColorConnectorStroke) was RETIRED in
	// the wave-2 connector standardization: the standard pins carry their
	// own outline inside rulesConnection.PinSVGFragment. No legacy, per
	// project rule.
	// Português: A cor do traço do dot (KColorConnectorStroke) foi
	// APOSENTADA na onda 2 da padronização: os pinos padrão carregam o
	// próprio contorno dentro do rulesConnection.PinSVGFragment. Sem
	// legado, regra do projeto.
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

	// KDeviceFontSizeSymbol is the font size for the large operator glyph in
	// the center of ornament-based devices (the "+" of Add, the "=" of
	// EqualTo). Ornaments whose symbol is longer than one character (">=",
	// "!=") may override it downward, but the base size lives here so the
	// operator family shares one visual weight.
	//
	// Português: Tamanho da fonte do glifo grande de operador no centro dos
	// devices baseados em ornament. Ornaments com símbolo maior que um
	// caractere podem reduzir, mas o tamanho base vive aqui.
	KDeviceFontSizeSymbol = 35

	// KDeviceFontFamilyMono is the monospaced font stack for content where
	// column alignment carries meaning: code-like values, hex dumps, byte
	// previews, seven-segment-style readouts. Everything else uses
	// KDeviceFontFamily.
	//
	// Português: Pilha de fontes monoespaçadas para conteúdo onde o
	// alinhamento em colunas tem significado (valores tipo código, dumps hex,
	// prévias de bytes). Todo o resto usa KDeviceFontFamily.
	KDeviceFontFamilyMono = "monospace"
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

	// The connector-dot geometry constants (KConnectorRadius,
	// KConnectorOffsetLeft/Right, KConnectorHitRadius) were RETIRED in the
	// wave-2 connector standardization: every device now uses the standard
	// pin (rulesConnection: PinBodyInset, PinSVGFragment, PinHit,
	// PinAnchor), whose single source of truth is the EDGE POINT — the
	// pin's outer tip on the element border. No legacy, per project rule.
	// Português: As constantes de geometria do dot (KConnectorRadius,
	// KConnectorOffsetLeft/Right, KConnectorHitRadius) foram APOSENTADAS na
	// onda 2 da padronização: todo device usa o pino padrão
	// (rulesConnection: PinBodyInset, PinSVGFragment, PinHit, PinAnchor),
	// cuja fonte única é o EDGE POINT — a ponta externa do pino na borda do
	// element. Sem legado, regra do projeto.
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
	case "int8", "int16":
		// Narrow signed widths share the "narrower int" darker blue used by
		// int32 — the signed family reads as one hue family, widths as shades.
		// The tag still shows the exact width for the specialist's benefit.
		// Português: Larguras estreitas com sinal usam o azul mais escuro do
		// int32 — a família com sinal lê como uma família de matiz; o tag
		// mostra a largura exata para o especialista.
		if goType == "int8" {
			return TypeStyle{Tag: "I8", Color: KColorTypeInt32}
		}
		return TypeStyle{Tag: "I16", Color: KColorTypeInt32}
	case "uint", "uint16", "uint32":
		// Unsigned family — deep purple. uint8 is handled below with byte
		// (they are the same Go type; see KColorTypeUint's doc comment).
		// Português: Família sem sinal — roxo profundo. uint8 é tratado
		// abaixo junto com byte (são o mesmo tipo Go).
		switch goType {
		case "uint16":
			return TypeStyle{Tag: "U16", Color: KColorTypeUint}
		case "uint32":
			return TypeStyle{Tag: "U32", Color: KColorTypeUint}
		}
		return TypeStyle{Tag: "UINT", Color: KColorTypeUint}
	case "uint64":
		return TypeStyle{Tag: "U64", Color: KColorTypeUint64}
	case "float32":
		return TypeStyle{Tag: "F32", Color: KColorTypeFloat32}
	case "float64":
		return TypeStyle{Tag: "F64", Color: KColorTypeFloat64}
	case "float":
		// Abstract float — the maker-facing default, mirroring abstract "int".
		// The maker never picks a bit-width; the target profile decides it, so
		// the tag is the plain word "FLOAT" (read as "a decimal number") rather
		// than a width like F32/F64. It shares the full-precision float64 accent
		// color, exactly as abstract "int" shares the int64 accent above — so
		// the primary numeric families each read as one color regardless of the
		// concrete width the profile ends up emitting.
		//
		// Português: Float abstrato — o padrão que o maker vê, espelhando o
		// "int" abstrato. O maker não escolhe largura; o profile decide, então o
		// tag é a palavra "FLOAT", não F32/F64. Reusa a cor do float64, igual ao
		// "int" abstrato reusa a do int64.
		return TypeStyle{Tag: "FLOAT", Color: KColorTypeFloat64}
	case "bool":
		return TypeStyle{Tag: "BOOL", Color: KColorTypeBool}
	case "string":
		return TypeStyle{Tag: "STR", Color: KColorTypeString}
	case "error":
		return TypeStyle{Tag: "ERR", Color: KColorTypeError}
	case "byte", "uint8", "[]byte":
		// byte and uint8 are the SAME Go type (alias) — one style, always.
		// Português: byte e uint8 são o MESMO tipo Go (alias) — um estilo só.
		return TypeStyle{Tag: "BYTE", Color: KColorTypeByte}
	case "time.Duration":
		return TypeStyle{Tag: "DUR", Color: KColorTypeDuration}
	default:
		// Pointer types (*machine.I2C) and package-qualified named types
		// (spi.Config) from black-box components: the violet "structured
		// data" catch-all. Checked BEFORE the slice derivation because a
		// slice of a complex type ([]machine.Pin) should also read as
		// structured data, and the slice branch below would recurse into
		// this one anyway. time.Duration never reaches here — it has its
		// explicit case above.
		//
		// Português: Tipos ponteiro e tipos qualificados por pacote vindos de
		// black-boxes: o pega-tudo violeta de "dado estruturado". Verificado
		// ANTES da derivação de slice. time.Duration nunca chega aqui — tem
		// case explícito acima.
		if strings.HasPrefix(goType, "*") || strings.Contains(goType, ".") {
			return TypeStyle{Tag: "STRUCT", Color: KColorTypeStruct}
		}
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
