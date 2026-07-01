// Package rulesMainMenu
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Defines visual rules and default styles for the hexagonal main menu,
//	including colors, dimensions, typography, and status-based theming for
//	both the trigger button and the menu hexagon icons.
//
//	All size values use rulesDensity.Density for automatic high-DPI scaling.
//	Colors are CSS hex strings compatible with hexMenu.IconStyle.
//
//	To customize the menu appearance, modify the exported variables in this
//	file before calling Button.Init(). Changes take effect on the next
//	Open() call.
//
// Português:
//
//	Define regras visuais e estilos padrão para o menu principal hexagonal,
//	incluindo cores, dimensões, tipografia e tematização baseada em status
//	para o botão de disparo e os ícones hexagonais do menu.
//
//	Todos os valores de tamanho usam rulesDensity.Density para escala
//	automática em telas de alta resolução.
//	Cores são strings hexadecimais CSS compatíveis com hexMenu.IconStyle.
//
//	Para personalizar a aparência do menu, modifique as variáveis exportadas
//	neste arquivo antes de chamar Button.Init(). As alterações entram em
//	vigor na próxima chamada Open().
package rulesMainMenu

import (
	"github.com/helmutkemper/iotmakerio/hexMenu"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// =====================================================================
//  Dimensions | Dimensões
// =====================================================================

// ButtonHexRadius
//
// English:
//
//	Hexagon radius for the trigger button (density-independent logical
//	units). The actual pixel size is determined by Density automatically.
//
// Português:
//
//	Raio do hexágono do botão de disparo (unidades lógicas independentes
//	de densidade). O tamanho real em pixels é determinado automaticamente
//	pelo Density.
var ButtonHexRadius = rulesDensity.Density(52)

// MenuHexRadius
//
// English:
//
//	Hexagon radius for the menu icons (density-independent logical units).
//
// Português:
//
//	Raio do hexágono dos ícones do menu (unidades lógicas independentes
//	de densidade).
var MenuHexRadius = rulesDensity.Density(60)

// BorderWidth
//
// English:
//
//	Border width for all hexagons (button and menu), in density-aware units.
//
// Português:
//
//	Largura da borda de todos os hexágonos (botão e menu), em unidades
//	sensíveis à densidade.
var BorderWidth = rulesDensity.Density(6)

// Spacing
//
// English:
//
//	Gap between hexagons in the menu grid, in density-aware units.
//
// Português:
//
//	Espaçamento entre hexágonos na grade do menu, em unidades sensíveis
//	à densidade.
var Spacing = rulesDensity.Density(4)

// =====================================================================
//  Typography | Tipografia
// =====================================================================

// FontFamily
//
// English:
//
//	Default font family for hexagon labels.
//
// Português:
//
//	Família de fonte padrão para os rótulos dos hexágonos.
var FontFamily = "Arial, sans-serif"

// MenuFontSize
//
// English:
//
//	Font size for menu hexagon labels, in density-aware units.
//
// Português:
//
//	Tamanho da fonte dos rótulos dos hexágonos do menu, em unidades
//	sensíveis à densidade.
var MenuFontSize = rulesDensity.Density(12)

// ButtonFontSize
//
// English:
//
//	Font size for the trigger button label, in density-aware units.
//
// Português:
//
//	Tamanho da fonte do rótulo do botão de disparo, em unidades sensíveis
//	à densidade.
var ButtonFontSize = rulesDensity.Density(10)

// =====================================================================
//  Layout | Layout
// =====================================================================

// ButtonZIndex
//
// English:
//
//	Z-index for the trigger button. Should be below the menu hexagons
//	but above all other canvas elements.
//
// Português:
//
//	Z-index do botão de disparo. Deve ficar abaixo dos hexágonos do menu
//	mas acima de todos os outros elementos do canvas.
var ButtonZIndex = 900

// MenuZIndex
//
// English:
//
//	Z-index for menu hexagons and backdrop. Menu items render at
//	MenuZIndex+1 so they appear above the backdrop.
//
// Português:
//
//	Z-index dos hexágonos do menu e do backdrop. Os itens do menu renderizam
//	em MenuZIndex+1 para aparecerem acima do backdrop.
var MenuZIndex = 1000

// CornerMarginX
//
// English:
//
//	Horizontal margin from canvas edge when using corner positioning,
//	in density-aware units.
//
// Português:
//
//	Margem horizontal da borda do canvas ao usar posicionamento por canto,
//	em unidades sensíveis à densidade.
var CornerMarginX = rulesDensity.Density(20)

// CornerMarginY
//
// English:
//
//	Vertical margin from canvas edge when using corner positioning,
//	in density-aware units.
//
// Português:
//
//	Margem vertical da borda do canvas ao usar posicionamento por canto,
//	em unidades sensíveis à densidade.
var CornerMarginY = rulesDensity.Density(45)

// MenuGap
//
// English:
//
//	Distance between the trigger button and the menu when opened,
//	in density-aware units.
//
// Português:
//
//	Distância entre o botão de disparo e o menu quando aberto, em unidades
//	sensíveis à densidade.
var MenuGap = rulesDensity.Density(5)

// =====================================================================
//  Menu icon colors — Normal | Cores dos ícones — Normal
// =====================================================================

// ColorIcon
//
// English:
//
//	Default icon path color in normal state (light gray).
//
// Português:
//
//	Cor padrão do caminho do ícone no estado normal (cinza claro).
var ColorIcon = "#C0C0C0"

// ColorBorder
//
// English:
//
//	Default border color in normal state (light yellow).
//
// Português:
//
//	Cor padrão da borda no estado normal (amarelo claro).
var ColorBorder = "#FFE066"

// ColorLabel
//
// English:
//
//	Default label text color in normal state (white).
//
// Português:
//
//	Cor padrão do texto do rótulo no estado normal (branco).
var ColorLabel = "#FFFFFF"

// ColorBackground
//
// English:
//
//	Default background fill color in normal state (blue).
//
// Português:
//
//	Cor padrão do preenchimento de fundo no estado normal (azul).
var ColorBackground = "#2255AA"

// =====================================================================
//  Menu icon colors — Disabled | Cores dos ícones — Desabilitado
// =====================================================================

// ColorIconDisabled
//
// English:
//
//	Icon path color in disabled state (dark gray).
//
// Português:
//
//	Cor do caminho do ícone no estado desabilitado (cinza escuro).
var ColorIconDisabled = "#777777"

// ColorBorderDisabled
//
// English:
//
//	Border color in disabled state (medium gray).
//
// Português:
//
//	Cor da borda no estado desabilitado (cinza médio).
var ColorBorderDisabled = "#666666"

// ColorLabelDisabled
//
// English:
//
//	Label text color in disabled state (medium gray).
//
// Português:
//
//	Cor do texto do rótulo no estado desabilitado (cinza médio).
var ColorLabelDisabled = "#888888"

// ColorBackgroundDisabled
//
// English:
//
//	Background fill color in disabled state (dark blue-gray).
//
// Português:
//
//	Cor do preenchimento de fundo no estado desabilitado (azul-cinza escuro).
var ColorBackgroundDisabled = "#2A2A40"

// =====================================================================
//  Menu icon colors — Selected | Cores dos ícones — Selecionado
// =====================================================================

// ColorIconSelected
//
// English:
//
//	Icon path color in selected state (white).
//
// Português:
//
//	Cor do caminho do ícone no estado selecionado (branco).
var ColorIconSelected = "#FFFFFF"

// ColorBorderSelected
//
// English:
//
//	Border color in selected state (bright yellow/gold).
//
// Português:
//
//	Cor da borda no estado selecionado (amarelo dourado brilhante).
var ColorBorderSelected = "#FFD700"

// ColorLabelSelected
//
// English:
//
//	Label text color in selected state (white).
//
// Português:
//
//	Cor do texto do rótulo no estado selecionado (branco).
var ColorLabelSelected = "#FFFFFF"

// ColorBackgroundSelected
//
// English:
//
//	Background fill color in selected state (bright blue).
//
// Português:
//
//	Cor do preenchimento de fundo no estado selecionado (azul brilhante).
var ColorBackgroundSelected = "#3388DD"

// =====================================================================
//  Menu icon colors — Attention1 | Cores dos ícones — Atenção 1
// =====================================================================

// ColorIconAttention1
//
// English:
//
//	Icon path color in attention level 1 state (white).
//
// Português:
//
//	Cor do caminho do ícone no estado de atenção nível 1 (branco).
var ColorIconAttention1 = "#FFFFFF"

// ColorBorderAttention1
//
// English:
//
//	Border color in attention level 1 state (orange).
//
// Português:
//
//	Cor da borda no estado de atenção nível 1 (laranja).
var ColorBorderAttention1 = "#FF8800"

// ColorLabelAttention1
//
// English:
//
//	Label text color in attention level 1 state (white).
//
// Português:
//
//	Cor do texto do rótulo no estado de atenção nível 1 (branco).
var ColorLabelAttention1 = "#FFFFFF"

// ColorBackgroundAttention1
//
// English:
//
//	Background fill color in attention level 1 state (dark orange).
//
// Português:
//
//	Cor do preenchimento de fundo no estado de atenção nível 1 (laranja
//	escuro).
var ColorBackgroundAttention1 = "#CC6600"

// =====================================================================
//  Menu icon colors — Attention2 | Cores dos ícones — Atenção 2
// =====================================================================

// ColorIconAttention2
//
// English:
//
//	Icon path color in attention level 2 state (white).
//
// Português:
//
//	Cor do caminho do ícone no estado de atenção nível 2 (branco).
var ColorIconAttention2 = "#FFFFFF"

// ColorBorderAttention2
//
// English:
//
//	Border color in attention level 2 state (yellow).
//
// Português:
//
//	Cor da borda no estado de atenção nível 2 (amarelo).
var ColorBorderAttention2 = "#FFCC00"

// ColorLabelAttention2
//
// English:
//
//	Label text color in attention level 2 state (white).
//
// Português:
//
//	Cor do texto do rótulo no estado de atenção nível 2 (branco).
var ColorLabelAttention2 = "#FFFFFF"

// ColorBackgroundAttention2
//
// English:
//
//	Background fill color in attention level 2 state (amber).
//
// Português:
//
//	Cor do preenchimento de fundo no estado de atenção nível 2 (âmbar).
var ColorBackgroundAttention2 = "#AA8800"

// =====================================================================
//  Button-specific colors | Cores específicas do botão
// =====================================================================
//
// The trigger button shares Normal/Disabled/Selected colors with the menu
// icons, but uses red tones for Attention1/Attention2 to create a distinct
// flashing effect that draws the user's eye to the button.
//
// Português: O botão de disparo compartilha cores Normal/Desabilitado/
// Selecionado com os ícones do menu, mas usa tons de vermelho para
// Atenção1/Atenção2 para criar um efeito de flash distinto que atrai
// o olhar do usuário para o botão.

// ButtonColorIconAttention1
//
// English:
//
//	Button icon color during attention flash phase 1 (white).
//
// Português:
//
//	Cor do ícone do botão durante a fase 1 do flash de atenção (branco).
var ButtonColorIconAttention1 = "#FFFFFF"

// ButtonColorBorderAttention1
//
// English:
//
//	Button border color during attention flash phase 1 (bright red).
//
// Português:
//
//	Cor da borda do botão durante a fase 1 do flash de atenção (vermelho
//	brilhante).
var ButtonColorBorderAttention1 = "#FF4444"

// ButtonColorLabelAttention1
//
// English:
//
//	Button label color during attention flash phase 1 (white).
//
// Português:
//
//	Cor do rótulo do botão durante a fase 1 do flash de atenção (branco).
var ButtonColorLabelAttention1 = "#FFFFFF"

// ButtonColorBackgroundAttention1
//
// English:
//
//	Button background color during attention flash phase 1 (dark red).
//
// Português:
//
//	Cor de fundo do botão durante a fase 1 do flash de atenção (vermelho
//	escuro).
var ButtonColorBackgroundAttention1 = "#CC0000"

// ButtonColorIconAttention2
//
// English:
//
//	Button icon color during attention flash phase 2 (light pink).
//
// Português:
//
//	Cor do ícone do botão durante a fase 2 do flash de atenção (rosa claro).
var ButtonColorIconAttention2 = "#FFCCCC"

// ButtonColorBorderAttention2
//
// English:
//
//	Button border color during attention flash phase 2 (dark red).
//
// Português:
//
//	Cor da borda do botão durante a fase 2 do flash de atenção (vermelho
//	escuro).
var ButtonColorBorderAttention2 = "#AA0000"

// ButtonColorLabelAttention2
//
// English:
//
//	Button label color during attention flash phase 2 (light pink).
//
// Português:
//
//	Cor do rótulo do botão durante a fase 2 do flash de atenção (rosa
//	claro).
var ButtonColorLabelAttention2 = "#FFCCCC"

// ButtonColorBackgroundAttention2
//
// English:
//
//	Button background color during attention flash phase 2 (deep red).
//
// Português:
//
//	Cor de fundo do botão durante a fase 2 do flash de atenção (vermelho
//	profundo).
var ButtonColorBackgroundAttention2 = "#880000"

// =====================================================================
//  Style builders | Construtores de estilos
// =====================================================================

// MenuStyles
//
// English:
//
//	Returns a 5-state color palette for the menu hexagon icons, built
//	from the exported color variables. Modify the variables above to
//	change the appearance of all menu icons at once.
//
//	States: Normal, Disabled, Selected, Attention1, Attention2.
//
// Português:
//
//	Retorna uma paleta de 5 estados de cores para os ícones hexagonais
//	do menu, construída a partir das variáveis de cor exportadas. Modifique
//	as variáveis acima para alterar a aparência de todos os ícones do menu
//	de uma vez.
//
//	Estados: Normal, Desabilitado, Selecionado, Atenção1, Atenção2.
func MenuStyles() [5]hexMenu.IconStyle {
	return [5]hexMenu.IconStyle{
		// Normal
		{
			ColorIcon:       ColorIcon,
			ColorBorder:     ColorBorder,
			ColorLabel:      ColorLabel,
			ColorBackground: ColorBackground,
		},
		// Disabled
		{
			ColorIcon:       ColorIconDisabled,
			ColorBorder:     ColorBorderDisabled,
			ColorLabel:      ColorLabelDisabled,
			ColorBackground: ColorBackgroundDisabled,
		},
		// Selected
		{
			ColorIcon:       ColorIconSelected,
			ColorBorder:     ColorBorderSelected,
			ColorLabel:      ColorLabelSelected,
			ColorBackground: ColorBackgroundSelected,
		},
		// Attention1
		{
			ColorIcon:       ColorIconAttention1,
			ColorBorder:     ColorBorderAttention1,
			ColorLabel:      ColorLabelAttention1,
			ColorBackground: ColorBackgroundAttention1,
		},
		// Attention2
		{
			ColorIcon:       ColorIconAttention2,
			ColorBorder:     ColorBorderAttention2,
			ColorLabel:      ColorLabelAttention2,
			ColorBackground: ColorBackgroundAttention2,
		},
	}
}

// ButtonStyles
//
// English:
//
//	Returns a 5-state color palette for the trigger button. Normal,
//	Disabled, and Selected states share the menu theme. Attention1 and
//	Attention2 use red tones to create a distinct flashing animation
//	that draws the user's attention to the button.
//
// Português:
//
//	Retorna uma paleta de 5 estados de cores para o botão de disparo.
//	Os estados Normal, Desabilitado e Selecionado compartilham o tema
//	do menu. Atenção1 e Atenção2 usam tons de vermelho para criar uma
//	animação de flash distinta que atrai a atenção do usuário para o
//	botão.
func ButtonStyles() [5]hexMenu.IconStyle {
	return [5]hexMenu.IconStyle{
		// Normal — same as menu
		{
			ColorIcon:       ColorIcon,
			ColorBorder:     ColorBorder,
			ColorLabel:      ColorLabel,
			ColorBackground: ColorBackground,
		},
		// Disabled — same as menu
		{
			ColorIcon:       ColorIconDisabled,
			ColorBorder:     ColorBorderDisabled,
			ColorLabel:      ColorLabelDisabled,
			ColorBackground: ColorBackgroundDisabled,
		},
		// Selected — same as menu
		{
			ColorIcon:       ColorIconSelected,
			ColorBorder:     ColorBorderSelected,
			ColorLabel:      ColorLabelSelected,
			ColorBackground: ColorBackgroundSelected,
		},
		// Attention1 — red flash (button-specific)
		{
			ColorIcon:       ButtonColorIconAttention1,
			ColorBorder:     ButtonColorBorderAttention1,
			ColorLabel:      ButtonColorLabelAttention1,
			ColorBackground: ButtonColorBackgroundAttention1,
		},
		// Attention2 — dark red flash (button-specific)
		{
			ColorIcon:       ButtonColorIconAttention2,
			ColorBorder:     ButtonColorBorderAttention2,
			ColorLabel:      ButtonColorLabelAttention2,
			ColorBackground: ButtonColorBackgroundAttention2,
		},
	}
}

// MenuConfig
//
// English:
//
//	Returns a hexMenu.Config pre-filled with all default values from this
//	rules package. Use this to create a SpriteHexMenu without hardcoding
//	any configuration values.
//
// Português:
//
//	Retorna um hexMenu.Config pré-preenchido com todos os valores padrão
//	deste pacote de regras. Use para criar um SpriteHexMenu sem valores
//	de configuração hardcoded.
func MenuConfig() hexMenu.Config {
	c := hexMenu.Config{
		HexRadius:  MenuHexRadius,
		ZIndex:     MenuZIndex,
		FontFamily: FontFamily,
		FontSize:   MenuFontSize,
	}
	c.SetBorderWidth(BorderWidth)
	c.SetSpacing(Spacing)
	return c
}

// ButtonConfig
//
// English:
//
//	Returns a hexMenu.Config pre-filled for the trigger button SVG
//	rendering. Uses ButtonHexRadius and ButtonFontSize.
//
// Português:
//
//	Retorna um hexMenu.Config pré-preenchido para a renderização SVG
//	do botão de disparo. Usa ButtonHexRadius e ButtonFontSize.
func ButtonConfig(hexRadius rulesDensity.Density) hexMenu.Config {
	r := hexRadius
	if r == 0 {
		r = ButtonHexRadius
	}
	c := hexMenu.Config{
		HexRadius:  r,
		FontFamily: FontFamily,
		FontSize:   ButtonFontSize,
	}
	c.SetBorderWidth(BorderWidth)
	return c
}
