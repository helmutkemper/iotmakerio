// ornament/device/ornamentOpAmpSymbol.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"fmt"
	"image/color"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/connection"
	"github.com/helmutkemper/iotmakerio/connection/factoryConnection"
	"github.com/helmutkemper/iotmakerio/rulesConnection"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesDevice"
)

// opAmpBorder is the inset of the triangle body from the ornament bounds.
// The margin it frees on the left and right is exactly where the connector
// pins live, so they protrude from the triangle while staying inside the
// sprite element.
//
// Português: Recuo do triângulo em relação aos limites do ornamento. A
// margem liberada à esquerda e à direita é onde os pinos vivem, saindo do
// triângulo sem estourar o sprite element.
const opAmpBorder = 8

// PinEdge is one connector's EDGE POINT in the ornament's logical (Density)
// space: the point where the pin meets the device body border, vertically
// centered on the pin. See rulesConnection/pin.go for the full vocabulary.
//
// Português: EDGE POINT de um conector no espaço lógico (Density) do
// ornamento: onde o pino encosta na borda do corpo, centrado verticalmente
// no pino. Ver rulesConnection/pin.go para o vocabulário completo.
type PinEdge struct {
	X rulesDensity.Density
	Y rulesDensity.Density
}

// OpAmpPinEdges returns the three connector edge points of the op-amp
// triangle for the given ornament size (logical Density, label area
// excluded). This is the SINGLE geometry source for the op-amp family:
//
//   - the ornament draws the pins here (Update),
//   - the devices hit-test clicks and the cursor here, and
//   - the devices anchor their wires here (PositionFunc via PinAnchor).
//
// Change a position in this function and every consumer follows — the four
// call sites can never disagree again.
//
//	inputX: left border of the triangle, upper pin
//	inputY: left border of the triangle, lower pin
//	output: right vertex of the triangle, vertical center
//
// Português: Retorna os três edge points do triângulo op-amp para o tamanho
// dado (Density lógico, sem a área do label). É a fonte ÚNICA de geometria
// da família op-amp: o ornamento desenha os pinos aqui, os devices testam
// clique/cursor aqui e ancoram os fios aqui. Mudou aqui, todos os
// consumidores seguem — os quatro pontos de uso não conseguem mais divergir.
func OpAmpPinEdges(width, height rulesDensity.Density) (inputX, inputY, output PinEdge) {
	b := rulesDensity.Density(opAmpBorder)

	// The vertical offsets keep the pins visually where they always were on
	// the 60×60 default ornament (upper pin centered at y=17, lower at
	// height-16) — the standardization moved the wire ANCHORS to the outer
	// tips, not the pins themselves.
	// Português: Os deslocamentos verticais mantêm os pinos onde sempre
	// estiveram no ornamento padrão 60×60 — a padronização moveu os ANCHORS
	// dos fios para as pontas externas, não os pinos.
	inputX = PinEdge{X: b, Y: rulesDensity.Density(17)}
	inputY = PinEdge{X: b, Y: height - rulesDensity.Density(16)}
	output = PinEdge{X: width - b, Y: height / 2}
	return
}

// OrnamentOpAmpSymbol Responsible for drawing the operational amplifier symbol used in analog electronics for mathematical
// operations
type OrnamentOpAmpSymbol struct {
	//ornament.WarningMarkExclamation

	deviceBorderNormalColor     color.RGBA
	deviceBackgroundNormalColor color.RGBA
	deviceSymbolNormalColor     color.RGBA

	deviceBorderSelectedColor     color.RGBA
	deviceBackgroundSelectedColor color.RGBA
	deviceSymbolSelectedColor     color.RGBA

	width         rulesDensity.Density
	height        rulesDensity.Density
	deviceAdjustX rulesDensity.Density
	deviceAdjustY rulesDensity.Density

	deviceSymbolText       string
	deviceSymbolFontSize   rulesDensity.Density
	deviceSymbolFontFamily string
	deviceSymbolFontWeight string

	svg                  *html.TagSvg
	deviceBorder         *html.TagSvgPath
	deviceSymbol         *html.TagSvgText
	inputXConnection     *html.TagSvgPath
	inputXConnectionArea connection.Connection
	inputYConnection     *html.TagSvgPath
	inputYConnectionArea connection.Connection
	outputConnection     *html.TagSvgPath
	outputConnectionArea connection.Connection

	// [PIN] data type per connector — controls each pin's fill color, kept in
	// sync with the wire.Manager's AllowedTypes by the owning device. They are
	// SEPARATE fields (not one shared type) because the comparison devices
	// have mixed ports: operand inputs follow the selected data type while the
	// output is ALWAYS bool.
	//
	// Português: Tipo de dado por conector — controla a cor de cada pino,
	// mantido em sincronia com os AllowedTypes do wire.Manager pelo device
	// dono. São campos SEPARADOS (não um tipo único) porque os devices de
	// comparação têm portas mistas: entradas seguem o tipo selecionado, mas a
	// saída é SEMPRE bool.
	inputXType string
	inputYType string
	outputType string
}

// SetConnectionTypes sets the data type of each connector pin and repaints
// the pins with the canonical palette color of their type (the same hue the
// wire of that type uses, so pin and wire read as one continuous piece).
//
// The owning device must call this:
//   - once after Init(), with the initial types, and
//   - again from its SetDataType(), followed by a recacheOrnament(), so a
//     type switch in the Inspect panel recolors the pins on screen.
//
// Português: Define o tipo de dado de cada pino e repinta os pinos com a cor
// canônica do tipo (o mesmo matiz do fio daquele tipo, para pino e fio lerem
// como uma peça contínua). O device dono deve chamar: uma vez após Init(),
// com os tipos iniciais, e de novo no SetDataType(), seguido de
// recacheOrnament(), para a troca de tipo no Inspect recolorir na tela.
func (e *OrnamentOpAmpSymbol) SetConnectionTypes(inputXType, inputYType, outputType string) {
	e.inputXType = inputXType
	e.inputYType = inputYType
	e.outputType = outputType

	if e.inputXConnection != nil {
		e.inputXConnection.Fill(rulesConnection.TypeToColor(inputXType))
	}
	if e.inputYConnection != nil {
		e.inputYConnection.Fill(rulesConnection.TypeToColor(inputYType))
	}
	if e.outputConnection != nil {
		e.outputConnection.Fill(rulesConnection.TypeToColor(outputType))
	}
}

func (e *OrnamentOpAmpSymbol) InputXSetup(setup connection.Setup) {
	e.inputXConnectionArea.Setup(setup)
}

func (e *OrnamentOpAmpSymbol) InputYSetup(setup connection.Setup) {
	e.inputYConnectionArea.Setup(setup)
}

func (e *OrnamentOpAmpSymbol) OutputSetup(setup connection.Setup) {
	e.outputConnectionArea.Setup(setup)
}

func (e *OrnamentOpAmpSymbol) GetWidth() rulesDensity.Density {
	return e.width
}

func (e *OrnamentOpAmpSymbol) GetHeight() rulesDensity.Density {
	return e.height
}

func (e *OrnamentOpAmpSymbol) ToPngResized(width, height float64) (pngData js.Value) {
	return e.svg.ToPngResized(width, height)
}

func (e *OrnamentOpAmpSymbol) SetSelected(selected bool) {
	if selected {
		e.deviceBorder.Fill(e.deviceBackgroundSelectedColor)
		e.deviceBorder.Stroke(e.deviceBorderSelectedColor)
		e.deviceSymbol.Fill(e.deviceSymbolSelectedColor)
		return
	}

	e.deviceBorder.Fill(e.deviceBackgroundNormalColor)
	e.deviceBorder.Stroke(e.deviceBorderNormalColor)
	e.deviceSymbol.Fill(e.deviceSymbolNormalColor)
}

// SetWarning sets the visibility of the warning mark
func (e *OrnamentOpAmpSymbol) SetWarning(warning bool) {
	//e.WarningMarkExclamation.SetWarning(warning) // todo: fazer
}

// SetAdjustX defines the X adjustment of the symbol
func (e *OrnamentOpAmpSymbol) SetAdjustX(adjustX rulesDensity.Density) {
	e.deviceAdjustX = adjustX
}

// GetAdjustX returns the X adjustment of the symbol
func (e *OrnamentOpAmpSymbol) GetAdjustX() rulesDensity.Density {
	return e.deviceAdjustX
}

// SetAdjustY defines the Y adjustment of the symbol
func (e *OrnamentOpAmpSymbol) SetAdjustY(adjustY rulesDensity.Density) {
	e.deviceAdjustY = adjustY
}

// GetAdjustY returns the Y adjustment of the symbol
func (e *OrnamentOpAmpSymbol) GetAdjustY() rulesDensity.Density {
	return e.deviceAdjustY
}

// SetSymbol defines the symbol of the device
func (e *OrnamentOpAmpSymbol) SetSymbol(text string) {
	e.deviceSymbolText = text
	e.deviceSymbol.Text(text)
}

// GetSymbol returns the symbol of the device
func (e *OrnamentOpAmpSymbol) GetSymbol() string {
	return e.deviceSymbolText
}

// SetSymbolFontSize defines the font size of the symbol
func (e *OrnamentOpAmpSymbol) SetSymbolFontSize(fontSize rulesDensity.Density) {
	e.deviceSymbolFontSize = fontSize
	e.deviceSymbol.FontSize(fontSize.Pixel())
}

// GetSymbolFontSize returns the font size of the symbol
func (e *OrnamentOpAmpSymbol) GetSymbolFontSize() rulesDensity.Density {
	return e.deviceSymbolFontSize
}

// SetSymbolFontFamily defines the font family of the symbol
func (e *OrnamentOpAmpSymbol) SetSymbolFontFamily(fontFamily string) {
	e.deviceSymbolFontFamily = fontFamily
	e.deviceSymbol.FontFamily(fontFamily)
}

// GetSymbolFontFamily returns the font family of the symbol
func (e *OrnamentOpAmpSymbol) GetSymbolFontFamily() string {
	return e.deviceSymbolFontFamily
}

// SetSymbolFontWeight defines the font weight of the symbol
func (e *OrnamentOpAmpSymbol) SetSymbolFontWeight(fontWeight string) {
	e.deviceSymbolFontWeight = fontWeight
	e.deviceSymbol.FontWeight(fontWeight)
}

// GetSymbolFontWeight returns the font weight of the symbol
func (e *OrnamentOpAmpSymbol) GetSymbolFontWeight() string {
	return e.deviceSymbolFontWeight
}

// SetBorderNormalColor defines the color of the border
func (e *OrnamentOpAmpSymbol) SetBorderNormalColor(color color.RGBA) {
	e.deviceBorderNormalColor = color
	e.deviceBorder.Stroke(color)
}

// SetBorderSelectedColor defines the color of the border
func (e *OrnamentOpAmpSymbol) SetBorderSelectedColor(color color.RGBA) {
	e.deviceBorderSelectedColor = color
	e.deviceBorder.Stroke(color)
}

// GetBorderNormalColor returns the color of the border
func (e *OrnamentOpAmpSymbol) GetBorderNormalColor() color.RGBA {
	return e.deviceBorderNormalColor
}

// GetBorderSelectedColor returns the color of the border
func (e *OrnamentOpAmpSymbol) GetBorderSelectedColor() color.RGBA {
	return e.deviceBorderSelectedColor
}

// SetBackgroundNormalColor defines the color of the device background
func (e *OrnamentOpAmpSymbol) SetBackgroundNormalColor(color color.RGBA) {
	e.deviceBackgroundNormalColor = color
	e.deviceBorder.Fill(color)
}

// SetBackgroundSelectedColor defines the color of the device background
func (e *OrnamentOpAmpSymbol) SetBackgroundSelectedColor(color color.RGBA) {
	e.deviceBackgroundSelectedColor = color
	e.deviceBorder.Fill(color)
}

// GetBackgroundNormalColor returns the color of the device background
func (e *OrnamentOpAmpSymbol) GetBackgroundNormalColor() color.RGBA {
	return e.deviceBackgroundNormalColor
}

// GetBackgroundSelectedColor returns the color of the device background
func (e *OrnamentOpAmpSymbol) GetBackgroundSelectedColor() color.RGBA {
	return e.deviceBackgroundSelectedColor
}

// SetSymbolNormalColor defines the color of the symbol
func (e *OrnamentOpAmpSymbol) SetSymbolNormalColor(color color.RGBA) {
	e.deviceSymbolNormalColor = color
	e.deviceSymbol.Fill(color)
}

// SetSymbolSelectedColor defines the color of the symbol
func (e *OrnamentOpAmpSymbol) SetSymbolSelectedColor(color color.RGBA) {
	e.deviceSymbolSelectedColor = color
	e.deviceSymbol.Fill(color)
}

// GetSymbolNormalColor returns the color of the symbol
func (e *OrnamentOpAmpSymbol) GetSymbolNormalColor() color.RGBA {
	return e.deviceSymbolNormalColor
}

// GetSymbolSelectedColor returns the color of the symbol
func (e *OrnamentOpAmpSymbol) GetSymbolSelectedColor() color.RGBA {
	return e.deviceSymbolSelectedColor
}

// Init Initializes the SVG element and its content
func (e *OrnamentOpAmpSymbol) Init() (err error) {
	//_ = e.WarningMarkExclamation.Init()

	e.deviceBorderNormalColor = color.RGBA{R: 15, G: 48, B: 216, A: 255}
	e.deviceBackgroundNormalColor = color.RGBA{R: 253, G: 255, B: 23, A: 255}
	e.deviceSymbolNormalColor = color.RGBA{R: 83, G: 83, B: 81, A: 255}

	e.deviceBorderSelectedColor = color.RGBA{R: 65, G: 48, B: 216, A: 255}
	e.deviceBackgroundSelectedColor = color.RGBA{R: 253, G: 205, B: 0, A: 255}
	e.deviceSymbolSelectedColor = color.RGBA{R: 133, G: 83, B: 81, A: 255}

	// [FONT] typography comes from the design system (rulesDevice) — the
	// operator glyph shares the family every device uses and the base symbol
	// size the whole op-amp family shares. Individual ornaments with longer
	// symbols (">=", "!=") may still call SetSymbolFontSize to shrink.
	//
	// Português: Tipografia vem do design system (rulesDevice) — o glifo do
	// operador usa a família de todos os devices e o tamanho base da família
	// op-amp. Ornaments com símbolos maiores podem reduzir via
	// SetSymbolFontSize.
	e.deviceSymbolText = "?"
	e.deviceSymbolFontSize = rulesDensity.Density(rulesDevice.KDeviceFontSizeSymbol)
	e.deviceSymbolFontFamily = rulesDevice.KDeviceFontFamily
	e.deviceSymbolFontWeight = "bold"

	e.svg = factoryBrowser.NewTagSvg()

	e.deviceBorder = factoryBrowser.NewTagSvgPath().
		Fill(e.deviceBackgroundNormalColor).
		Stroke(e.deviceBorderNormalColor).
		StrokeWidth(rulesDensity.Density(1).GetInt()).
		MarkerEnd("url(#deviceBorder)")
	e.svg.Append(e.deviceBorder)

	e.deviceSymbol = factoryBrowser.NewTagSvgText().
		Fill(e.deviceSymbolNormalColor).
		Stroke("none").
		MarkerEnd("url(#deviceSymbol)").
		TextAnchor("middle").
		DominantBaseline("middle").
		FontSize(e.deviceSymbolFontSize.Pixel()).
		FontFamily(e.deviceSymbolFontFamily).
		FontWeight(e.deviceSymbolFontWeight).
		Text(e.deviceSymbolText).
		UserSelectNone()
	e.svg.Append(e.deviceSymbol)

	// [PIN] the pins are born with the default types below; the owning device
	// overrides them right after Init() via SetConnectionTypes(). Before the
	// standardization the color was hardcoded to "int" and NEVER updated — an
	// Add switched to float kept blue pins next to a teal wire. The per-pin
	// type fields fix that permanently.
	//
	// Português: Os pinos nascem com os tipos padrão abaixo; o device dono
	// sobrescreve logo após Init() via SetConnectionTypes(). Antes da
	// padronização a cor era "int" fixo e NUNCA atualizada — um Add trocado
	// para float ficava com pinos azuis ao lado de um fio teal. Os campos de
	// tipo por pino corrigem isso em definitivo.
	if e.inputXType == "" {
		e.inputXType = "int"
	}
	if e.inputYType == "" {
		e.inputYType = "int"
	}
	if e.outputType == "" {
		e.outputType = "int"
	}

	e.inputXConnection = factoryConnection.NewConnection(e.inputXType, "url(#inputXConnection)")
	e.svg.Append(e.inputXConnection)

	e.inputXConnectionArea.Init("url(#inputXConnectionArea)")
	e.svg.Append(e.inputXConnectionArea.GetSvgPath())

	e.inputYConnection = factoryConnection.NewConnection(e.inputYType, "url(#inputYConnection)")
	e.svg.Append(e.inputYConnection)

	e.inputYConnectionArea.Init("url(#inputYConnectionArea)")
	e.svg.Append(e.inputYConnectionArea.GetSvgPath())

	e.outputConnection = factoryConnection.NewConnection(e.outputType, "url(#outputConnection)")
	e.svg.Append(e.outputConnection)

	e.outputConnectionArea.Init("url(#stopButtonConnection)")
	e.svg.Append(e.outputConnectionArea.GetSvgPath())

	//e.svg.Append(e.WarningMarkExclamation.GetSvg())
	e.SetWarning(false)
	return
}

// GetSvg Returns the SVG used as a base in the ornament
func (e *OrnamentOpAmpSymbol) GetSvg() (svg *html.TagSvg) {
	return e.svg
}

// Update Desenha o ornamento
func (e *OrnamentOpAmpSymbol) Update(x, y, width, height rulesDensity.Density) (err error) {
	e.width = width
	e.height = height

	//_ = e.WarningMarkExclamation.Update(x, y, width, height)

	//e.svg.ViewBox([]int{0.0, 0.0, width, height})

	// draw the triangle
	border := rulesDensity.Density(opAmpBorder)
	device := []string{
		fmt.Sprintf("M %v %v", 0+border, 0+border),
		fmt.Sprintf("L %v %v", width-border, height/2),
		fmt.Sprintf("L %v %v", 0+border, height-border),
		fmt.Sprintf("L %v %v", 0+border, 0+border),
		"z",
	}
	e.deviceBorder.D(device)

	// calculate the center of the triangle
	a := [2]int{0 + border.GetInt(), 0 + border.GetInt()}
	b := [2]int{width.GetInt() - border.GetInt(), height.GetInt() / 2}
	c := [2]int{0 + border.GetInt(), height.GetInt() - border.GetInt()}

	// center of the triangle
	xc := (a[0] + b[0] + c[0]) / 3
	yc := (a[1] + b[1] + c[1]) / 3

	// update deviceSymbol position
	e.deviceSymbol.X(rulesDensity.FromScaledInt(xc).GetInt() + e.deviceAdjustX.GetInt())
	e.deviceSymbol.Y(rulesDensity.FromScaledInt(yc).GetInt() + e.deviceAdjustY.GetInt())

	// [PIN] connector pins — drawn by the standard pin geometry so the four
	// consumers (this drawing, the devices' click hit-test, cursor hit-test
	// and wire anchor) all read from OpAmpPinEdges and can never disagree.
	// Inputs protrude LEFT from the triangle's left border; the output
	// protrudes RIGHT from the triangle's vertex, so the wire attaches at
	// the pin's OUTER TIP — the product-standard connector look.
	//
	// Português: Pinos dos conectores — desenhados pela geometria padrão para
	// os quatro consumidores (este desenho, o hit-test de clique dos devices,
	// o de cursor e o anchor do fio) lerem de OpAmpPinEdges e nunca
	// divergirem. Entradas saem para a ESQUERDA da borda esquerda do
	// triângulo; a saída sai para a DIREITA do vértice, com o fio preso na
	// PONTA EXTERNA do pino — o visual padrão do produto.
	inputX, inputY, output := OpAmpPinEdges(width, height)

	e.inputXConnection.D(rulesConnection.PinPathDraw(rulesConnection.PinSideLeft, inputX.X, inputX.Y))
	e.inputXConnectionArea.GetSvgPath().D(rulesConnection.PinPathAreaDraw(rulesConnection.PinSideLeft, inputX.X, inputX.Y))
	axX, axY := rulesConnection.PinAnchorD(rulesConnection.PinSideLeft, inputX.X, inputX.Y)
	e.inputXConnectionArea.SetXY(x+axX, y+axY)

	e.inputYConnection.D(rulesConnection.PinPathDraw(rulesConnection.PinSideLeft, inputY.X, inputY.Y))
	e.inputYConnectionArea.GetSvgPath().D(rulesConnection.PinPathAreaDraw(rulesConnection.PinSideLeft, inputY.X, inputY.Y))
	ayX, ayY := rulesConnection.PinAnchorD(rulesConnection.PinSideLeft, inputY.X, inputY.Y)
	e.inputYConnectionArea.SetXY(x+ayX, y+ayY)

	e.outputConnection.D(rulesConnection.PinPathDraw(rulesConnection.PinSideRight, output.X, output.Y))
	e.outputConnectionArea.GetSvgPath().D(rulesConnection.PinPathAreaDraw(rulesConnection.PinSideRight, output.X, output.Y))
	aoX, aoY := rulesConnection.PinAnchorD(rulesConnection.PinSideRight, output.X, output.Y)
	e.outputConnectionArea.SetXY(x+aoX, y+aoY)

	return
}
