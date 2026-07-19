// rulesIcon/iconRegistry.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesIcon

// iconRegistry.go — Maps FontAwesome icon name strings to SVG path data,
// and provides unified parsing for all icon value formats.
//
// English:
//
//	An icon value in the IDS `icon:` tag can be written in three forms:
//
//	  1. Name     — FA icon name in kebab-case, e.g. "greater-than-equal".
//	               Requires the icon to be registered in iconDefs below.
//
//	  2. Unicode  — FA codepoint in hex, e.g. "f287" or "\uf287".
//	               Works for ANY FontAwesome icon without needing registration.
//	               Defaults to the Solid style (font-weight 900).
//
//	  3. Unicode + style — codepoint followed by ":b" for Brands, e.g. "f287:b".
//	               Use when the icon is in the FA Brands set (e.g. usb, github).
//
//	Parsing is done by ParseIconValue(). Rendering helpers:
//	  - Name icons   → use FAIconDef.Path + FAIconDef.ViewBox as <path d="...">.
//	  - Unicode icons → render <text font-family="..." font-weight="...">&#xNNNN;</text>.
//
// Português:
//
//	O campo `icon:` aceita: nome kebab-case (ex: "gear"), codepoint hex
//	(ex: "f287", "\uf287"), ou codepoint com estilo brands (ex: "f287:b").
//	ParseIconValue() normaliza qualquer um dos três formatos em um IconValue.

import (
	"strconv"
	"strings"
)

// ─── Icon value types ────────────────────────────────────────────────────────

// IconKind distinguishes the two rendering strategies.
type IconKind int

const (
	// IconKindName uses a pre-registered SVG path from iconDefs.
	// Rendered as <path d="..."> inside a nested <svg>.
	IconKindName IconKind = iota

	// IconKindUnicode uses a FontAwesome codepoint rendered as a <text>
	// element with the appropriate FA font family.
	IconKindUnicode
)

// IconFontStyle controls which FontAwesome font family and weight to use
// when rendering a Unicode icon.
type IconFontStyle int

const (
	// IconFontSolid uses "Font Awesome 6 Free" at font-weight 900.
	// This is the default for any codepoint without an explicit style suffix.
	IconFontSolid IconFontStyle = iota

	// IconFontRegular uses "Font Awesome 6 Free" at font-weight 400.
	IconFontRegular

	// IconFontBrands uses "Font Awesome 6 Brands" at font-weight 400.
	// Required for brand icons such as "usb" (f287), "github" (f09b), etc.
	// Select with the ":b" suffix: "f287:b".
	IconFontBrands
)

// IconValue is the normalised representation of an `icon:` tag value.
// Consumers should switch on Kind to decide how to render.
type IconValue struct {
	// Kind determines whether to use the path registry or font rendering.
	Kind IconKind

	// Name is set when Kind == IconKindName. It is the original kebab-case
	// name (e.g. "greater-than-equal"). Use IconByName(Name) to get the path.
	Name string

	// Codepoint is set when Kind == IconKindUnicode. It is the numeric
	// Unicode codepoint (e.g. 0xf287 for the USB icon).
	Codepoint rune

	// Style controls the FA font family and weight for unicode rendering.
	// Ignored when Kind == IconKindName.
	Style IconFontStyle
}

// FontFamily returns the CSS font-family string for this icon's style.
// Only meaningful for unicode icons (Kind == IconKindUnicode).
func (v IconValue) FontFamily() string {
	if v.Style == IconFontBrands {
		return "'Font Awesome 6 Brands'"
	}
	return "'Font Awesome 6 Free'"
}

// FontWeight returns the CSS font-weight value for this icon's style.
// Only meaningful for unicode icons (Kind == IconKindUnicode).
func (v IconValue) FontWeight() string {
	if v.Style == IconFontSolid {
		return "900"
	}
	return "400"
}

// SVGCharRef returns the XML character reference for the codepoint,
// e.g. "&#xf287;" for Codepoint 0xf287.
// Only meaningful for unicode icons (Kind == IconKindUnicode).
func (v IconValue) SVGCharRef() string {
	if v.Kind != IconKindUnicode {
		return ""
	}
	return "&#x" + strings.ToLower(strconv.FormatInt(int64(v.Codepoint), 16)) + ";"
}

// ─── Parsing ─────────────────────────────────────────────────────────────────

// ParseIconValue normalises any of the three `icon:` tag formats into an IconValue.
//
// Accepted formats (s is the value after "icon:", trimmed, before the "."):
//
//	"greater-than-equal"   → IconKindName, Name="greater-than-equal"
//	"f287"                 → IconKindUnicode, Codepoint=0xf287, Style=Solid
//	"\uf287"               → same as "f287" (Go unicode escape stripped)
//	"0xf287"               → same as "f287"
//	"f287:b"               → IconKindUnicode, Codepoint=0xf287, Style=Brands
//	"f287:brands"          → same as "f287:b"
//	"f287:r"               → IconKindUnicode, Codepoint=0xf287, Style=Regular
//	"f287:regular"         → same as "f287:r"
//
// Unknown names return a fallback IconValue with Kind=IconKindName and
// Name="gear" so the caller always gets a valid, renderable result.
func ParseIconValue(s string) IconValue {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallbackIconValue()
	}

	// ── Style suffix detection ────────────────────────────────────────────
	// Check for ":b", ":brands", ":r", ":regular" before the unicode check.
	style := IconFontSolid
	if idx := strings.LastIndex(s, ":"); idx > 0 {
		suffix := strings.ToLower(strings.TrimSpace(s[idx+1:]))
		switch suffix {
		case "b", "brands":
			style = IconFontBrands
			s = s[:idx]
		case "r", "regular":
			style = IconFontRegular
			s = s[:idx]
		}
	}

	// ── Unicode detection ─────────────────────────────────────────────────
	// Strip optional "\u" or "0x" prefix, then check if remaining chars
	// are a valid 4-8 digit hexadecimal codepoint.
	candidate := s
	candidate = strings.TrimPrefix(candidate, `\u`)
	candidate = strings.TrimPrefix(candidate, `\U`)
	candidate = strings.TrimPrefix(candidate, "0x")
	candidate = strings.TrimPrefix(candidate, "0X")

	if isHexCodepoint(candidate) {
		n, err := strconv.ParseInt(candidate, 16, 32)
		if err == nil && n > 0 {
			return IconValue{
				Kind:      IconKindUnicode,
				Codepoint: rune(n),
				Style:     style,
			}
		}
	}

	// ── Name fallback ─────────────────────────────────────────────────────
	// Style suffix is not meaningful for name-based icons; ignore it.
	return IconValue{
		Kind: IconKindName,
		Name: strings.ToLower(s),
	}
}

// isHexCodepoint reports whether s is a valid hex codepoint string.
// Accepts 4–8 hex digits (FA codepoints are typically 4–5 hex digits).
func isHexCodepoint(s string) bool {
	n := len(s)
	if n < 4 || n > 8 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// fallbackIconValue returns "gear" as a safe, always-renderable fallback.
func fallbackIconValue() IconValue {
	return IconValue{Kind: IconKindName, Name: "gear"}
}

// ─── Name-based icon registry ────────────────────────────────────────────────

// FAIconDef holds the SVG path data and viewBox for one FontAwesome icon.
// Both fields are required to correctly render the icon at any size.
type FAIconDef struct {
	// Path is the raw SVG <path d="..."> value.
	Path string

	// ViewBox is the original SVG viewBox string (e.g. "0 0 512 512").
	// Required to compute the correct scale transform when the icon is drawn
	// at a size different from the original coordinate system.
	ViewBox string
}

// iconDefs maps FontAwesome icon names (kebab-case) to their path + viewBox.
//
// This map starts with the hand-written entries from this file. When the
// generated file (faIconsGenerated.go) is present, its init() function
// replaces this map with the full FA Free icon set (~1400 icons).
// To add a single icon without re-running the generator, add it here and
// register its SVG path as a constant in falcons.go.
var iconDefs = map[string]FAIconDef{
	// ── Navigation / UI ────────────────────────────────────────────────────
	"arrows-up-down-left-right": {KFAArrowsUpDownLeftRight, "0 0 512 512"},
	"floppy-disk":               {KFAFloppyDisk, "0 0 448 512"},
	"bars":                      {KFABars, "0 0 448 512"},
	"eye":                       {KFAEye, "0 0 512 512"},
	"link":                      {KFALink, "0 0 640 512"},
	"link-slash":                {KFALinkSlash, "0 0 640 512"},
	"pen":                       {KFAPen, "0 0 512 512"},
	"clock-rotate-left":         {KFAClockRotateLeft, "0 0 512 512"},

	// ── System / Hardware ──────────────────────────────────────────────────
	"gear":        {KFAGear, "0 0 512 512"},
	"desktop":     {KFADesktop, "0 0 576 512"},
	"file-export": {KFAFileExport, "0 0 512 512"},
	"copy":        {KFACopy, "0 0 448 512"},
	"trash-can":   {KFATrashCan, "0 0 448 512"},
	"repeat":      {KFARepeat, "0 0 512 512"},
	"play":        {KFAPlay, "0 0 384 512"},

	// ── Math operations ────────────────────────────────────────────────────
	"plus":                 {KFAPlus, "0 0 448 512"},
	"minus":                {KFAMinus, "0 0 448 512"},
	"xmark":                {KFAXmark, "0 0 384 512"},
	"divide":               {KFADivide, "0 0 448 512"},
	"square-root-variable": {KFASquareRootVariable, "0 0 576 512"},

	// ── Comparison operators ───────────────────────────────────────────────
	"equals":             {KFaEqual, "0 0 640 640"},
	"not-equal":          {KFaNotEqual, "0 0 640 640"},
	"less-than":          {KFaLessThan, "0 0 640 640"},
	"less-than-equal":    {KFaLessThanEqual, "0 0 640 640"},
	"greater-than":       {KFaGreaterThan, "0 0 640 640"},
	"greater-than-equal": {KFaGreaterThanEqual, "0 0 640 640"},

	// ── Binary / digital ──────────────────────────────────────────────────
	"square-binary":  {KFaSquareBinary, "0 0 640 640"},
	"scale-balanced": {KFAScaleBalanced, "0 0 640 512"},

	// ── Containers / N-way selection ──────────────────────────────────────
	"layer-group": {KFALayerGroup, "0 0 512 512"},
}

// faIconByCodepoint maps hex codepoint strings (e.g. "f287", "f287:b") to
// FAIconDef entries. Populated by the init() in faIconsGenerated.go when
// that file is present. Empty map when the generator has not been run.
//
// Key format (matches what ParseIconValue produces after parsing):
//
//	"f287"   → solid (or brands when no solid with that codepoint exists)
//	"f287:b" → brands
//	"f287:r" → regular
var faIconByCodepoint = map[string]FAIconDef{}

// IconByName looks up a FontAwesome icon by its kebab-case name.
//
// Returns the FAIconDef and true when found; returns a zero FAIconDef and
// false when the name is not registered.
func IconByName(name string) (FAIconDef, bool) {
	def, ok := iconDefs[name]
	return def, ok
}

// IconByNameOrDefault returns the icon for the given name, or the fallback
// icon when the name is not found. The fallback is never empty — if
// fallbackName is also missing, "gear" is used as the last resort.
func IconByNameOrDefault(name, fallbackName string) FAIconDef {
	if def, ok := iconDefs[name]; ok {
		return def
	}
	if def, ok := iconDefs[fallbackName]; ok {
		return def
	}
	return iconDefs["gear"]
}

// IconDefForValue resolves an IconValue to its FAIconDef.
//
// For IconKindName: looks up the name in iconDefs. Falls back to "gear".
//
// For IconKindUnicode: builds the lookup key from the codepoint and style,
// then checks faIconByCodepoint (populated by faIconsGenerated.go when
// the generator has been run). When the codepoint is not found — either
// because the generator was not run or the codepoint is unknown — falls
// back to "gear" so the caller always receives a valid, renderable result.
//
// Returns (def, true) on success, (gear, false) on fallback.
func IconDefForValue(v IconValue) (FAIconDef, bool) {
	switch v.Kind {
	case IconKindUnicode:
		// Build the key the same way ParseIconValue would present it.
		hex := strings.ToLower(strconv.FormatInt(int64(v.Codepoint), 16))
		// Pad to minimum 4 digits.
		for len(hex) < 4 {
			hex = "0" + hex
		}
		key := hex
		switch v.Style {
		case IconFontBrands:
			key += ":b"
		case IconFontRegular:
			key += ":r"
		}
		if def, ok := faIconByCodepoint[key]; ok {
			return def, true
		}
		// Codepoint not in the generated table — generator not run yet, or
		// the icon does not exist in FA Free. Return gear as a safe fallback.
		if def, ok := iconDefs["gear"]; ok {
			return def, false
		}
		return FAIconDef{}, false

	default: // IconKindName
		if def, ok := iconDefs[v.Name]; ok {
			return def, true
		}
		if def, ok := iconDefs["gear"]; ok {
			return def, false
		}
		return FAIconDef{}, false
	}
}
