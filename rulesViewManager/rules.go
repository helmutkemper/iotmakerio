package rulesViewManager

// rulesViewManager — Centralized configuration for the stageViewManager package.
//
// English:
//
//	All hardcoded constants from viewManager.go are defined here as exported
//	variables. Organized by subsystem:
//	  - Layout (tab bar height, z-index)
//	  - Defaults (initial tab, initial view mode)
//	  - Tab bar container style
//	  - Tab button styles (inactive, active, both-active, toggle)
//	  - Icon button styles
//	  - Icons and labels
//
// Português:
//
//	Todas as constantes hardcoded do viewManager.go são definidas aqui.
//	Organizadas por subsistema:
//	  - Layout (altura da barra de abas, z-index)
//	  - Padrões (aba inicial, modo de visualização inicial)
//	  - Estilo do container da barra de abas
//	  - Estilos dos botões de aba (inativo, ativo, ambos-ativos, toggle)
//	  - Estilos dos botões de ícone
//	  - Ícones e labels

// =====================================================================
//  Layout | Disposição
// =====================================================================

// TabBarHeight is the pixel height of the tab bar at the top.
// Português: Altura em pixels da barra de abas no topo.
var TabBarHeight = 36

// TabBarZIndex is the CSS z-index of the tab bar.
// Português: z-index CSS da barra de abas.
var TabBarZIndex = "300"

// =====================================================================
//  Defaults | Padrões
// =====================================================================

// DefaultActiveTab is the initially visible tab ("frontend" or "backend").
// Português: Aba inicialmente visível ("frontend" ou "backend").
var DefaultActiveTab = "backend"

// DefaultViewMode is the initial view mode (0=Tabs, 1=SideBySide, 2=Stacked).
// Português: Modo de visualização inicial (0=Tabs, 1=LadoALado, 2=Empilhado).
var DefaultViewMode = 0

// =====================================================================
//  Tab Bar Container | Container da Barra de Abas
// =====================================================================

// TabBarBackground is the CSS background color of the tab bar.
// Português: Cor de fundo CSS da barra de abas.
var TabBarBackground = "#1a1a2e"

// TabBarBorderBottom is the CSS border-bottom of the tab bar.
// Português: Borda inferior CSS da barra de abas.
var TabBarBorderBottom = "1px solid #333"

// TabBarFontFamily is the CSS font-family for the tab bar.
// Português: font-family CSS da barra de abas.
var TabBarFontFamily = "Arial, sans-serif"

// TabBarFontSize is the CSS font-size for the tab bar.
// Português: font-size CSS da barra de abas.
var TabBarFontSize = "13px"

// TabBarGap is the CSS gap between tab bar items.
// Português: Espaçamento CSS entre itens da barra de abas.
var TabBarGap = "4px"

// TabBarPadding is the CSS padding of the tab bar.
// Português: Padding CSS da barra de abas.
var TabBarPadding = "0 8px"

// =====================================================================
//  Language Badge | Chip de Linguagem
// =====================================================================
//
// The language badge sits at the right edge of the tab bar (after
// the Backend/Frontend tabs and the flex-grow spacer). It shows the
// fixed project language as a coloured pill — coral for C99, blue
// for Go, and future colours for future languages. The chip is read-
// only; the language cannot be changed mid-project.
//
// Colours follow the mockup approved in conversation: muted, easy to
// scan, accessible contrast.
//
// Português: Chip que mostra a linguagem fixa do projeto no canto
// direito da tab bar. Coral pra C99, azul pra Go. Read-only.
// =====================================================================

// LangBadgePadding is the CSS padding inside the language badge.
// Português: Padding CSS interno do chip de linguagem.
var LangBadgePadding = "3px 10px"

// LangBadgeBorderRadius is the CSS border-radius for the pill shape.
// Use a large value so the chip reads as fully rounded regardless of
// font size variations.
// Português: border-radius CSS para a forma de pílula.
var LangBadgeBorderRadius = "999px"

// LangBadgeFontSize is the CSS font-size for the language label.
// Slightly smaller than the tab text so the chip reads as auxiliary
// information rather than a competing nav item.
// Português: font-size CSS do label. Menor que o texto das abas.
var LangBadgeFontSize = "11px"

// LangBadgeFontWeight is the CSS font-weight for the language label.
// Português: font-weight CSS do label.
var LangBadgeFontWeight = "500"

// LangBadgeMarginRight is the CSS margin-right of the badge so it
// is not flush against the viewport edge. The first iteration used
// 4px and the chip read as glued to the border; 14px gives a clear
// gap that matches the visual rhythm of the rest of the bar (tab
// padding is 4px × 4, button padding is similar) without making the
// chip feel orphaned from the edge.
// Português: margin-right CSS pra não ficar colado na borda. Valor
// inicial era 4px, ficou apertado; 14px abre respiro.
var LangBadgeMarginRight = "14px"

// LangBadgeBgGo is the CSS background colour of the Go badge.
// Português: Cor de fundo CSS do chip Go.
var LangBadgeBgGo = "#85B7EB"

// LangBadgeColorGo is the CSS text colour of the Go badge.
// Português: Cor do texto CSS do chip Go.
var LangBadgeColorGo = "#042C53"

// LangBadgeBgC is the CSS background colour of the C99 badge.
// Português: Cor de fundo CSS do chip C99.
var LangBadgeBgC = "#F0997B"

// LangBadgeColorC is the CSS text colour of the C99 badge.
// Português: Cor do texto CSS do chip C99.
var LangBadgeColorC = "#4A1B0C"

// LangBadgeBgUnknown is the CSS background for the fallback badge
// when the project language is empty or unrecognised. Stays neutral
// grey so an unexpected state is visible without alarming the user.
// Português: Cor de fundo CSS do chip quando linguagem é vazia ou
// desconhecida — cinza neutro, visível sem alarmar.
var LangBadgeBgUnknown = "#444"

// LangBadgeColorUnknown is the CSS text colour for the fallback badge.
// Português: Cor do texto CSS do chip fallback.
var LangBadgeColorUnknown = "#bbb"

// =====================================================================
//  Tab Button — Base (Inactive) | Botão de Aba — Base (Inativo)
// =====================================================================

// TabBtnPadding is the CSS padding for inactive tab buttons.
// Português: Padding CSS para botões de aba inativos.
var TabBtnPadding = "4px 16px"

// TabBtnBorder is the CSS border for tab buttons.
// Português: Borda CSS para botões de aba.
var TabBtnBorder = "1px solid #444"

// TabBtnBorderRadius is the CSS border-radius for tab buttons.
// Português: border-radius CSS para botões de aba.
var TabBtnBorderRadius = "4px 4px 0 0"

// TabBtnBackground is the CSS background for inactive tab buttons.
// Português: Fundo CSS para botões de aba inativos.
var TabBtnBackground = "#2a2a3e"

// TabBtnColor is the CSS text color for inactive tab buttons.
// Português: Cor do texto CSS para botões de aba inativos.
var TabBtnColor = "#aaa"

// TabBtnFontFamily is the CSS font-family for tab buttons.
// Português: font-family CSS para botões de aba.
var TabBtnFontFamily = "Arial, sans-serif"

// TabBtnFontSize is the CSS font-size for inactive tab buttons.
// Português: font-size CSS para botões de aba inativos.
var TabBtnFontSize = "13px"

// =====================================================================
//  Tab Button — Active | Botão de Aba — Ativo
// =====================================================================

// TabActiveBackground is the CSS background for the active tab.
// Português: Fundo CSS para a aba ativa.
var TabActiveBackground = "#2255AA"

// TabActiveColor is the CSS text color for the active tab.
// Português: Cor do texto CSS para a aba ativa.
var TabActiveColor = "#FFFFFF"

// TabActiveBorderBottom is the CSS border-bottom for the active tab.
// Português: Borda inferior CSS para a aba ativa.
var TabActiveBorderBottom = "3px solid #FFE066"

// TabActiveFontWeight is the CSS font-weight for the active tab.
// Português: font-weight CSS para a aba ativa.
var TabActiveFontWeight = "bold"

// TabActiveFontSize is the CSS font-size for the active tab.
// Português: font-size CSS para a aba ativa.
var TabActiveFontSize = "14px"

// TabActivePadding is the CSS padding for the active tab.
// Português: Padding CSS para a aba ativa.
var TabActivePadding = "6px 20px"

// TabActivePrefix is the text prefix prepended to the active tab label.
// Português: Prefixo de texto adicionado antes do label da aba ativa.
var TabActivePrefix = "● "

// =====================================================================
//  Tab Button — Inactive | Botão de Aba — Inativo
// =====================================================================

// TabInactiveBackground is the CSS background for inactive tabs.
// Português: Fundo CSS para abas inativas.
var TabInactiveBackground = "#2a2a3e"

// TabInactiveColor is the CSS text color for inactive tabs.
// Português: Cor do texto CSS para abas inativas.
var TabInactiveColor = "#666"

// TabInactiveBorderBottom is the CSS border-bottom for inactive tabs.
// Português: Borda inferior CSS para abas inativas.
var TabInactiveBorderBottom = "1px solid #444"

// TabInactiveFontWeight is the CSS font-weight for inactive tabs.
// Português: font-weight CSS para abas inativas.
var TabInactiveFontWeight = "normal"

// TabInactiveFontSize is the CSS font-size for inactive tabs.
// Português: font-size CSS para abas inativas.
var TabInactiveFontSize = "13px"

// TabInactivePadding is the CSS padding for inactive tabs.
// Português: Padding CSS para abas inativas.
var TabInactivePadding = "4px 16px"

// =====================================================================
//  Tab Button — Both Active (Split/Stack) | Botão de Aba — Ambos Ativos
// =====================================================================

// TabBothBackground is the CSS background when both tabs are visible.
// Português: Fundo CSS quando ambas as abas estão visíveis.
var TabBothBackground = "#2255AA"

// TabBothColor is the CSS text color when both tabs are visible.
// Português: Cor do texto CSS quando ambas as abas estão visíveis.
var TabBothColor = "#FFFFFF"

// TabBothBorderBottom is the CSS border-bottom when both tabs are visible.
// Português: Borda inferior CSS quando ambas as abas estão visíveis.
var TabBothBorderBottom = "2px solid #FFE066"

// TabBothFontWeight is the CSS font-weight when both tabs are visible.
// Português: font-weight CSS quando ambas as abas estão visíveis.
var TabBothFontWeight = "bold"

// TabBothFontSize is the CSS font-size when both tabs are visible.
// Português: font-size CSS quando ambas as abas estão visíveis.
var TabBothFontSize = "13px"

// TabBothPadding is the CSS padding when both tabs are visible.
// Português: Padding CSS quando ambas as abas estão visíveis.
var TabBothPadding = "4px 16px"

// =====================================================================
//  Toggle Button (Split/Stack active state) | Botão Toggle
// =====================================================================

// ToggleActiveBackground is the CSS background for active toggle buttons.
// Português: Fundo CSS para botões toggle ativos.
var ToggleActiveBackground = "#4488ff"

// ToggleActiveColor is the CSS text color for active toggle buttons.
// Português: Cor do texto CSS para botões toggle ativos.
var ToggleActiveColor = "#fff"

// ToggleInactiveBackground is the CSS background for inactive toggle buttons.
// Português: Fundo CSS para botões toggle inativos.
var ToggleInactiveBackground = "#2a2a3e"

// ToggleInactiveColor is the CSS text color for inactive toggle buttons.
// Português: Cor do texto CSS para botões toggle inativos.
var ToggleInactiveColor = "#aaa"

// =====================================================================
//  Icon Button | Botão de Ícone
// =====================================================================

// IconBtnPadding is the CSS padding for icon buttons.
// Português: Padding CSS para botões de ícone.
var IconBtnPadding = "4px 8px"

// IconBtnBorder is the CSS border for icon buttons.
// Português: Borda CSS para botões de ícone.
var IconBtnBorder = "1px solid #444"

// IconBtnBorderRadius is the CSS border-radius for icon buttons.
// Português: border-radius CSS para botões de ícone.
var IconBtnBorderRadius = "4px"

// IconBtnBackground is the CSS background for icon buttons.
// Português: Fundo CSS para botões de ícone.
var IconBtnBackground = "#2a2a3e"

// IconBtnColor is the CSS text color for icon buttons.
// Português: Cor do texto CSS para botões de ícone.
var IconBtnColor = "#aaa"

// IconBtnFontSize is the CSS font-size for icon buttons.
// Português: font-size CSS para botões de ícone.
var IconBtnFontSize = "16px"

// =====================================================================
//  Icons | Ícones
// =====================================================================

// IconSplitView is the unicode icon for the side-by-side button.
// Português: Ícone unicode para o botão lado a lado.
var IconSplitView = "⬒"

// IconStackView is the unicode icon for the stacked button.
// Português: Ícone unicode para o botão empilhado.
var IconStackView = "⬓"
