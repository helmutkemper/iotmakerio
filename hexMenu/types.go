// hexMenu/types.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package hexMenu

import (
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/translate"
)

// PositionMode defines where the menu opens.
type PositionMode int

const (
	PositionAtPoint  PositionMode = iota // top-left at given point
	PositionCentered                     // centered on given point
	PositionFixed                        // at Config.FixedX/FixedY
)

// ItemType defines what happens on click.
type ItemType int

const (
	ItemAction  ItemType = iota // execute OnClick callback, close menu
	ItemSubmenu                 // navigate to Submenu page
)

// PipelineState represents visual state of a hexagon icon.
type PipelineState int

const (
	PipelineNormal PipelineState = iota
	PipelineDisabled
	PipelineSelected
	PipelineAttention1
	PipelineAttention2
)

// IconStyle defines colors for one pipeline state.
type IconStyle struct {
	ColorIcon       string
	ColorBorder     string
	ColorLabel      string
	ColorBackground string
}

// =====================================================================
//  Default rules | Regras padrão
// =====================================================================
//
// English:
//
//	Default values used by ApplyDefaults(). Modify these variables to
//	change the behavior of all menus that rely on ApplyDefaults().
//
//	These follow the same pattern as rulesIcon: exported package-level
//	variables that serve as the single source of truth for defaults.
//
//	A dedicated rules package (e.g. rulesMainMenu) can override these
//	values in its init() function to customize the menu globally.
//
// Português:
//
//	Valores padrão usados por ApplyDefaults(). Modifique estas variáveis
//	para alterar o comportamento de todos os menus que dependem de
//	ApplyDefaults().
//
//	Seguem o mesmo padrão de rulesIcon: variáveis exportadas a nível de
//	pacote que servem como fonte única da verdade para os defaults.
//
//	Um pacote de regras dedicado (ex: rulesMainMenu) pode sobrescrever
//	estes valores na sua função init() para personalizar o menu globalmente.

// DefaultHexRadius
//
// English:
//
//	Default hexagon radius in density-independent logical units.
//
// Português:
//
//	Raio padrão do hexágono em unidades lógicas independentes de densidade.
var DefaultHexRadius = rulesDensity.Density(28)

// DefaultBorderWidth
//
// English:
//
//	Default border width in density-independent logical units.
//	Zero is a valid value (no border).
//
// Português:
//
//	Largura padrão da borda em unidades lógicas independentes de densidade.
//	Zero é um valor válido (sem borda).
var DefaultBorderWidth = rulesDensity.Density(2)

// DefaultSpacing
//
// English:
//
//	Default gap between hexagons in the grid, in density-independent
//	logical units. Zero is a valid value (hexagons touch).
//
// Português:
//
//	Espaçamento padrão entre hexágonos na grade, em unidades lógicas
//	independentes de densidade. Zero é um valor válido (hexágonos se tocam).
var DefaultSpacing = rulesDensity.Density(4)

// DefaultZIndex
//
// English:
//
//	Default z-index for menu elements.
//
// Português:
//
//	Z-index padrão para elementos do menu.
var DefaultZIndex = 1000

// DefaultFontFamily
//
// English:
//
//	Default font family for hexagon labels.
//
// Português:
//
//	Família de fonte padrão para os rótulos dos hexágonos.
var DefaultFontFamily = "Arial, sans-serif"

// DefaultFontSize
//
// English:
//
//	Default font size for hexagon labels, in density-independent logical
//	units.
//
// Português:
//
//	Tamanho de fonte padrão para rótulos dos hexágonos, em unidades
//	lógicas independentes de densidade.
var DefaultFontSize = rulesDensity.Density(10)

// =====================================================================
//  Default styles | Estilos padrão
// =====================================================================

// DefaultStyleNormal
//
// English:
//
//	Default colors for the normal pipeline state.
//
// Português:
//
//	Cores padrão para o estado normal do pipeline.
var DefaultStyleNormal = IconStyle{
	ColorIcon: "#FFFFFF", ColorBorder: "#444444",
	ColorLabel: "#FFFFFF", ColorBackground: "#555555",
}

// DefaultStyleDisabled
//
// English:
//
//	Default colors for the disabled pipeline state.
//
// Português:
//
//	Cores padrão para o estado desabilitado do pipeline.
var DefaultStyleDisabled = IconStyle{
	ColorIcon: "#888888", ColorBorder: "#666666",
	ColorLabel: "#888888", ColorBackground: "#333333",
}

// DefaultStyleSelected
//
// English:
//
//	Default colors for the selected pipeline state.
//
// Português:
//
//	Cores padrão para o estado selecionado do pipeline.
var DefaultStyleSelected = IconStyle{
	ColorIcon: "#FFFFFF", ColorBorder: "#00AAFF",
	ColorLabel: "#FFFFFF", ColorBackground: "#0077CC",
}

// DefaultStyleAttention1
//
// English:
//
//	Default colors for the attention level 1 pipeline state.
//
// Português:
//
//	Cores padrão para o estado de atenção nível 1 do pipeline.
var DefaultStyleAttention1 = IconStyle{
	ColorIcon: "#FFFFFF", ColorBorder: "#FF8800",
	ColorLabel: "#FFFFFF", ColorBackground: "#CC6600",
}

// DefaultStyleAttention2
//
// English:
//
//	Default colors for the attention level 2 pipeline state.
//
// Português:
//
//	Cores padrão para o estado de atenção nível 2 do pipeline.
var DefaultStyleAttention2 = IconStyle{
	ColorIcon: "#FFFFFF", ColorBorder: "#FFCC00",
	ColorLabel: "#FFFFFF", ColorBackground: "#AA8800",
}

// DefaultStyles
//
// English:
//
//	Returns a 5-state color palette built from the DefaultStyle* variables.
//	Modify the variables to change the default palette globally.
//
// Português:
//
//	Retorna uma paleta de 5 estados de cores construída a partir das
//	variáveis DefaultStyle*. Modifique as variáveis para alterar a paleta
//	padrão globalmente.
func DefaultStyles() [5]IconStyle {
	return [5]IconStyle{
		DefaultStyleNormal,
		DefaultStyleDisabled,
		DefaultStyleSelected,
		DefaultStyleAttention1,
		DefaultStyleAttention2,
	}
}

// =====================================================================
//  Config | Configuração
// =====================================================================

// MenuItem defines one hexagon in the menu.
type MenuItem struct {
	ID string

	// MinTarget is the device's declared minimum hardware class ("",
	// "avr", "mcu32", "posix" — see blackbox.MinTargetOrdinal). The
	// sprite menu disables the item when the project's board sits below
	// it. Set only for black-box function entries today.
	// Português: Classe mínima de hardware declarada pelo device. O menu
	// desabilita o item quando a placa do projeto fica abaixo dela. Hoje
	// só entradas de função de black-box a preenchem.
	MinTarget    string
	Col          int
	Row          int
	AdjustIconX  int
	AdjustIconY  int
	AdjustLabelX int
	AdjustLabelY int
	Label        string

	// FontAwesomePath is the SVG <path d="..."> value for a registered icon.
	// Used when the icon was specified by name (e.g. "greater-than-equal").
	// When non-empty, FontAwesomeUnicode is ignored.
	FontAwesomePath string

	// ViewBox is the SVG viewBox for FontAwesomePath (e.g. "0 0 512 512").
	ViewBox string

	// FontAwesomeUnicode is a FA codepoint rendered as a <text> element using
	// the page-loaded FontAwesome webfont. Used when the icon was specified as
	// a hex codepoint (e.g. "f287", "\uf287").
	//
	// When non-empty, FontAwesomePath is ignored and the glyph is drawn with
	// the CSS font family selected by FontAwesomeBrands.
	//
	// Format: the raw rune value (not a string like "f287" — use
	// rulesIcon.ParseIconValue() to obtain this from the raw tag string).
	FontAwesomeUnicode rune

	// FontAwesomeBrands, when true, uses "Font Awesome 6 Brands" (weight 400)
	// instead of "Font Awesome 6 Free" (weight 900) for unicode icon rendering.
	// Required for brand icons such as USB (f287), GitHub (f09b), etc.
	FontAwesomeBrands bool

	Type    ItemType
	Submenu []MenuItem   // used when Type == ItemSubmenu
	OnClick func()       // used when Type == ItemAction
	Styles  [5]IconStyle // one per PipelineState

	// ── Layout hints from the IDS "menu:col,row." directive ──────────────
	//
	// These fields are set by the blackbox.BlackBoxDefClient loader when the
	// specialist wrote a "menu:col,row." directive in a method doc comment.
	// They carry the signed offset from the Back button center to this item.
	//
	// rulesMainMenu.ApplyRadialLayout reads MenuPosSet to decide whether to
	// use these values (explicit position) or assign a slot automatically.
	//
	// MenuCol and MenuRow are RELATIVE offsets, not absolute grid coordinates.
	// ApplyRadialLayout converts them to absolute Col/Row before rendering.
	//
	// Example: MenuCol=-1, MenuRow=-1 with BackCenterCol=2, BackCenterRow=2
	// → absolute Col=1, Row=1 (upper-left of Back).
	//
	// (0,0) is reserved for the Back button and must not be used.
	//
	// Português:
	//   Campos de hint de layout da diretiva IDS "menu:col,row.".
	//   Offsets relativos ao centro do botão Back.
	//   ApplyRadialLayout converte para coordenadas absolutas.
	MenuCol    int  // column offset from Back center
	MenuRow    int  // row offset from Back center
	MenuPosSet bool // true when the specialist explicitly declared menu:col,row.

	// BrandColor is the hex color string (e.g. "#E62E2E") used by the panel
	// to visually distinguish branded section entries from regular items.
	// When non-empty, the panel rail button and preview icon are tinted with
	// this color. Set only for top-level section items created by
	// buildSectionMenuItems in sections.go. Empty for all other items.
	BrandColor string
}

// Config configures menu appearance.
//
// All size fields use rulesDensity.Density: they store logical (unscaled)
// values and apply the global density factor automatically via
// .GetFloat() / .GetInt().
//
// Português: Todos os campos de tamanho usam rulesDensity.Density:
// armazenam valores lógicos (sem escala) e aplicam o fator de densidade
// global automaticamente via .GetFloat() / .GetInt().
type Config struct {
	HexRadius   rulesDensity.Density
	BorderWidth rulesDensity.Density
	Spacing     rulesDensity.Density
	ZIndex      int
	FontFamily  string
	FontSize    rulesDensity.Density
	FixedX      rulesDensity.Density
	FixedY      rulesDensity.Density

	// borderWidthSet and spacingSet track whether the field was explicitly
	// assigned. This allows ApplyDefaults to distinguish "not set" from
	// "intentionally set to zero".
	//
	// Português: borderWidthSet e spacingSet rastreiam se o campo foi
	// atribuído explicitamente. Isso permite que ApplyDefaults distinga
	// "não definido" de "definido intencionalmente como zero".
	borderWidthSet bool
	spacingSet     bool
}

// SetBorderWidth
//
// English:
//
//	Explicitly sets the border width. Zero is a valid value (no border).
//	ApplyDefaults() will not overwrite this value.
//
// Português:
//
//	Define explicitamente a largura da borda. Zero é um valor válido
//	(sem borda). ApplyDefaults() não sobrescreverá este valor.
func (c *Config) SetBorderWidth(bw rulesDensity.Density) {
	c.BorderWidth = bw
	c.borderWidthSet = true
}

// SetSpacing
//
// English:
//
//	Explicitly sets the spacing between hexagons. Zero is a valid value
//	(hexagons touch). ApplyDefaults() will not overwrite this value.
//
// Português:
//
//	Define explicitamente o espaçamento entre hexágonos. Zero é um valor
//	válido (hexágonos se tocam). ApplyDefaults() não sobrescreverá este
//	valor.
func (c *Config) SetSpacing(s rulesDensity.Density) {
	c.Spacing = s
	c.spacingSet = true
}

// ApplyDefaults
//
// English:
//
//	Fills zero-value fields with the package-level Default* variables.
//	BorderWidth and Spacing are only defaulted if they were not explicitly
//	set via SetBorderWidth() / SetSpacing(), because zero is a valid value
//	for both (no border, no gap).
//
//	All default values come from exported variables (DefaultHexRadius,
//	DefaultBorderWidth, etc.) following the rules pattern established by
//	rulesIcon. Override the variables to change defaults globally.
//
// Português:
//
//	Preenche campos com valor zero usando as variáveis Default* do pacote.
//	BorderWidth e Spacing só recebem default se não foram definidos
//	explicitamente via SetBorderWidth() / SetSpacing(), porque zero é um
//	valor válido para ambos (sem borda, sem espaçamento).
//
//	Todos os valores padrão vêm de variáveis exportadas (DefaultHexRadius,
//	DefaultBorderWidth, etc.) seguindo o padrão de regras estabelecido por
//	rulesIcon. Sobrescreva as variáveis para alterar defaults globalmente.
func (c *Config) ApplyDefaults() {
	if c.HexRadius == 0 {
		c.HexRadius = DefaultHexRadius
	}
	if !c.borderWidthSet && c.BorderWidth == 0 {
		c.BorderWidth = DefaultBorderWidth
	}
	if !c.spacingSet && c.Spacing == 0 {
		c.Spacing = DefaultSpacing
	}
	if c.ZIndex == 0 {
		c.ZIndex = DefaultZIndex
	}
	if c.FontFamily == "" {
		c.FontFamily = DefaultFontFamily
	}
	if c.FontSize == 0 {
		c.FontSize = DefaultFontSize
	}
}

// =====================================================================
//  Tutorial | Tutorial
// =====================================================================

// TutorialStep defines one step in a tutorial sequence.
type TutorialStep struct {
	PagePath []string // submenu IDs to navigate through
	ItemID   string   // ID of the item to flash
}

// =====================================================================
//  Helpers | Auxiliares
// =====================================================================

// IsStylesEmpty checks if all styles have empty colors.
func IsStylesEmpty(styles [5]IconStyle) bool {
	for _, s := range styles {
		if s.ColorIcon != "" || s.ColorBorder != "" ||
			s.ColorLabel != "" || s.ColorBackground != "" {
			return false
		}
	}
	return true
}

// GoBackItem returns a pre-built "Back" menu item.
// The caller should set OnClick or the menu system will handle it automatically.
func GoBackItem(col, row int) MenuItem {
	return MenuItem{
		ID:              "SysGoBack",
		Col:             col,
		Row:             row,
		AdjustIconY:     4,
		Label:           translate.T("menuMainBack", "Back"),
		FontAwesomePath: "M48.5 224L40 224c-13.3 0-24-10.7-24-24L16 72c0-9.7 5.8-18.5 14.8-22.2s19.3-1.7 26.2 5.2L98.6 96.6c87.6-86.5 228.7-86.2 315.8 1c87.5 87.5 87.5 229.3 0 316.8s-229.3 87.5-316.8 0c-12.5-12.5-12.5-32.8 0-45.3s32.8-12.5 45.3 0c62.5 62.5 163.8 62.5 226.3 0s62.5-163.8 0-226.3c-62.2-62.2-162.7-62.5-225.3-1L185 183c6.9 6.9 8.9 17.2 5.2 26.2s-12.5 14.8-22.2 14.8L48.5 224z",
		ViewBox:         "0 0 512 512",
		Type:            ItemAction,
		Styles:          DefaultStyles(),
	}
}
