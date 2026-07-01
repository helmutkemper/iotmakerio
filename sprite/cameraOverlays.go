package sprite

import (
	"fmt"
	"math"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesSprite"
)

// =====================================================================
//  Minimap Configuration | Configuração do Minimapa
// =====================================================================

// MinimapCorner
//
// English:
//
//	Defines which corner of the canvas the minimap is drawn in.
//
// Português:
//
//	Define em qual canto do canvas o minimapa é desenhado.
type MinimapCorner int

const (
	MinimapBottomRight MinimapCorner = iota
	MinimapBottomLeft
	MinimapTopRight
	MinimapTopLeft
)

// minimapConfig holds all minimap settings.
// All numeric values are density-independent base values. They are multiplied
// by rulesDensity.GetDensity() at draw time.
//
// Português: minimapConfig contém todas as configurações do minimapa.
// Todos os valores numéricos são base independente de densidade. São multiplicados
// por rulesDensity.GetDensity() no momento do desenho.
type minimapConfig struct {
	enabled bool

	// Position and size (density-independent base values)
	// Português: Posição e tamanho (valores base independentes de densidade)
	corner  MinimapCorner
	width   float64
	height  float64
	marginX float64
	marginY float64

	// Colors | Cores
	backgroundColor string
	borderColor     string
	elementColor    string
	viewportColor   string
	viewportFill    string

	// Interaction | Interação
	clickToNavigate bool
}

// =====================================================================
//  Help Overlay Configuration | Configuração do Overlay de Ajuda
// =====================================================================

// helpConfig holds all help overlay settings.
// All numeric values are density-independent base values.
//
// Português: helpConfig contém todas as configurações do overlay de ajuda.
// Todos os valores numéricos são base independentes de densidade.
type helpConfig struct {
	enabled bool

	// Position | Posição
	corner  MinimapCorner
	marginX float64
	marginY float64

	// Style (density-independent base values)
	// Português: Estilo (valores base independentes de densidade)
	backgroundColor string
	borderColor     string
	textColor       string
	titleColor      string
	fontSize        int
	lineHeight      float64
	padding         float64

	// Content | Conteúdo
	title string
	lines []string
}

// =====================================================================
//  Camera Fields for Minimap & Help | Campos da Camera para Minimapa & Ajuda
// =====================================================================
//
// NOTE: The following fields must be present in the Camera struct in camera.go:
//   minimap *minimapConfig
//   help    *helpConfig

// =====================================================================
//  Minimap Set Functions | Funções Set do Minimapa
// =====================================================================

// SetMinimapEnabled
//
// English:
//
//	Enables or disables the minimap. When enabled, a small overview of all elements
//	is drawn in the configured corner showing the current viewport position.
//
// Português:
//
//	Habilita ou desabilita o minimapa.
func (c *Camera) SetMinimapEnabled(enabled bool) {
	c.ensureMinimap()
	c.minimap.enabled = enabled
}

// SetMinimapCorner
//
// English:
//
//	Sets which corner of the canvas the minimap appears in.
//	Default: MinimapBottomRight.
//
// Português:
//
//	Define em qual canto do canvas o minimapa aparece.
func (c *Camera) SetMinimapCorner(corner MinimapCorner) {
	c.ensureMinimap()
	c.minimap.corner = corner
}

// SetMinimapSize
//
// English:
//
//	Sets the width and height of the minimap in density-independent pixels.
//	Default: 180×120.
//
// Português:
//
//	Define a largura e altura do minimapa em pixels independentes de densidade.
func (c *Camera) SetMinimapSize(width, height float64) {
	c.ensureMinimap()
	c.minimap.width = width
	c.minimap.height = height
}

// SetMinimapMargin
//
// English:
//
//	Sets the margin between the minimap and the canvas edge in density-independent pixels.
//	Default: 10×10.
//
// Português:
//
//	Define a margem entre o minimapa e a borda do canvas em pixels independentes de densidade.
func (c *Camera) SetMinimapMargin(marginX, marginY float64) {
	c.ensureMinimap()
	c.minimap.marginX = marginX
	c.minimap.marginY = marginY
}

// SetMinimapBackgroundColor
//
// Português: Define a cor de fundo do minimapa.
func (c *Camera) SetMinimapBackgroundColor(color string) {
	c.ensureMinimap()
	c.minimap.backgroundColor = color
}

// SetMinimapBorderColor
//
// Português: Define a cor da borda do minimapa.
func (c *Camera) SetMinimapBorderColor(color string) {
	c.ensureMinimap()
	c.minimap.borderColor = color
}

// SetMinimapElementColor
//
// Português: Define a cor usada para desenhar os retângulos de elementos no minimapa.
func (c *Camera) SetMinimapElementColor(color string) {
	c.ensureMinimap()
	c.minimap.elementColor = color
}

// SetMinimapViewportColor
//
// Português: Define a cor usada para desenhar o retângulo da viewport.
func (c *Camera) SetMinimapViewportColor(strokeColor, fillColor string) {
	c.ensureMinimap()
	c.minimap.viewportColor = strokeColor
	c.minimap.viewportFill = fillColor
}

// SetMinimapClickToNavigate
//
// English:
//
//	Enables or disables clicking on the minimap to navigate the camera.
//	Default: true.
//
// Português:
//
//	Habilita ou desabilita clicar no minimapa para navegar a câmera.
func (c *Camera) SetMinimapClickToNavigate(enabled bool) {
	c.ensureMinimap()
	c.minimap.clickToNavigate = enabled
}

// IsMinimapEnabled
//
// Português: Retorna se o minimapa está habilitado.
func (c *Camera) IsMinimapEnabled() bool {
	return c.minimap != nil && c.minimap.enabled
}

// ensureMinimap creates the minimap config with defaults if it doesn't exist.
// Values are density-independent base values.
// Português: Cria a config do minimapa com valores padrão (independentes de densidade).
func (c *Camera) ensureMinimap() {
	if c.minimap != nil {
		return
	}
	c.minimap = &minimapConfig{
		enabled:         false,
		corner:          MinimapBottomRight,
		width:           rulesSprite.MinimapWidth,
		height:          rulesSprite.MinimapHeight,
		marginX:         rulesSprite.MinimapMarginX,
		marginY:         rulesSprite.MinimapMarginY,
		backgroundColor: rulesSprite.MinimapBackgroundColor,
		borderColor:     rulesSprite.MinimapBorderColor,
		elementColor:    rulesSprite.MinimapElementColor,
		viewportColor:   rulesSprite.MinimapViewportColor,
		viewportFill:    rulesSprite.MinimapViewportFill,
		clickToNavigate: rulesSprite.MinimapClickToNavigate,
	}
}

// =====================================================================
//  Help Set Functions | Funções Set da Ajuda
// =====================================================================

// SetHelpEnabled
//
// Português: Habilita ou desabilita o overlay de ajuda.
func (c *Camera) SetHelpEnabled(enabled bool) {
	c.ensureHelp()
	c.help.enabled = enabled
}

// SetHelpCorner
//
// Português: Define em qual canto do canvas o overlay de ajuda aparece. Padrão: MinimapTopRight.
func (c *Camera) SetHelpCorner(corner MinimapCorner) {
	c.ensureHelp()
	c.help.corner = corner
}

// SetHelpMargin
//
// Português: Define a margem entre o overlay de ajuda e a borda do canvas (independente de densidade).
func (c *Camera) SetHelpMargin(marginX, marginY float64) {
	c.ensureHelp()
	c.help.marginX = marginX
	c.help.marginY = marginY
}

// SetHelpBackgroundColor
//
// Português: Define a cor de fundo do overlay de ajuda.
func (c *Camera) SetHelpBackgroundColor(color string) {
	c.ensureHelp()
	c.help.backgroundColor = color
}

// SetHelpBorderColor
//
// Português: Define a cor da borda do overlay de ajuda.
func (c *Camera) SetHelpBorderColor(color string) {
	c.ensureHelp()
	c.help.borderColor = color
}

// SetHelpTextColor
//
// Português: Define a cor do texto do overlay de ajuda.
func (c *Camera) SetHelpTextColor(color string) {
	c.ensureHelp()
	c.help.textColor = color
}

// SetHelpTitleColor
//
// Português: Define a cor do título do overlay de ajuda.
func (c *Camera) SetHelpTitleColor(color string) {
	c.ensureHelp()
	c.help.titleColor = color
}

// SetHelpFontSize
//
// English:
//
//	Sets the font size of the help overlay text in density-independent pixels.
//	Default: 11.
//
// Português:
//
//	Define o tamanho da fonte do texto do overlay de ajuda em pixels independentes de densidade.
func (c *Camera) SetHelpFontSize(size int) {
	c.ensureHelp()
	c.help.fontSize = size
}

// SetHelpTitle
//
// Português: Define o texto do título do overlay de ajuda.
func (c *Camera) SetHelpTitle(title string) {
	c.ensureHelp()
	c.help.title = title
}

// SetHelpLines
//
// Português: Define as linhas de texto exibidas no overlay de ajuda.
func (c *Camera) SetHelpLines(lines []string) {
	c.ensureHelp()
	if len(lines) == 0 {
		c.help.lines = rulesSprite.HelpLines()
	} else {
		c.help.lines = lines
	}
}

// IsHelpEnabled
//
// Português: Retorna se o overlay de ajuda está habilitado.
func (c *Camera) IsHelpEnabled() bool {
	return c.help != nil && c.help.enabled
}

// ensureHelp creates the help config with defaults if it doesn't exist.
// Values are density-independent base values.
// Português: Cria a config de ajuda com valores padrão (independentes de densidade).
func (c *Camera) ensureHelp() {
	if c.help != nil {
		return
	}
	c.help = &helpConfig{
		enabled:         false,
		corner:          MinimapTopRight, // todo: rulesSprite
		marginX:         rulesSprite.HelpMarginX,
		marginY:         rulesSprite.HelpMarginY,
		backgroundColor: rulesSprite.HelpBackgroundColor,
		borderColor:     rulesSprite.HelpBorderColor,
		textColor:       rulesSprite.HelpTextColor,
		titleColor:      rulesSprite.HelpTitleColor,
		fontSize:        rulesSprite.HelpFontSize,
		lineHeight:      rulesSprite.HelpLineHeight,
		padding:         rulesSprite.HelpPadding,
		title:           rulesSprite.HelpTitle(),
		lines:           rulesSprite.HelpLines(),
	}
}

// =====================================================================
//  Draw Minimap | Desenhar Minimapa
// =====================================================================

// DrawMinimap
//
// English:
//
//	Draws the minimap overlay on the canvas. Shows all elements as small rectangles
//	with the current viewport highlighted. Called from renderWithCamera after all
//	other overlays.
//
//	[DENSITY-FIX] All dimensions are scaled by rulesDensity.GetDensity() at draw time.
//	Config values remain density-independent base values.
//
// Português:
//
//	Desenha o overlay do minimapa no canvas.
//	[DENSITY-FIX] Todas as dimensões são escaladas por rulesDensity.GetDensity().
func (c *Camera) DrawMinimap(ctx js.Value, canvasW, canvasH int, elements map[string]*elementData) {
	if c.minimap == nil || !c.minimap.enabled {
		return
	}

	mm := c.minimap
	d := rulesDensity.GetDensity()

	// [DENSITY-FIX] Scale minimap dimensions by density.
	// Português: Escala dimensões do minimapa por densidade.
	mmW := mm.width * d
	mmH := mm.height * d
	mxD := mm.marginX * d
	myD := mm.marginY * d

	// [DENSITY-FIX] The canvas pixel dimensions are screenSize × density, but the
	// browser viewport only shows screenSize CSS pixels. Overlays must position
	// within the visible area (canvasW/d × canvasH/d), not the full canvas.
	//
	// Português: As dimensões do canvas em pixels são screenSize × density, mas o
	// viewport do browser só mostra screenSize CSS pixels. Overlays devem posicionar
	// dentro da área visível (canvasW/d × canvasH/d), não do canvas inteiro.
	visibleW := int(float64(canvasW) / d)
	visibleH := int(float64(canvasH) / d)

	// Calculate the world bounding box of all visible elements + current viewport.
	// [DENSITY-FIX] Use visible screen size for viewport bounds, not canvas pixel size.
	// Português: Usa tamanho visível da tela para limites da viewport.
	vpX, vpY := c.ScreenToWorld(0, 0)
	vpX2, vpY2 := c.ScreenToWorld(float64(visibleW), float64(visibleH))
	vpW := vpX2 - vpX
	vpH := vpY2 - vpY

	// Start with viewport bounds, then expand to include all elements.
	// Português: Começa com os limites da viewport, depois expande.
	worldMinX := vpX
	worldMinY := vpY
	worldMaxX := vpX2
	worldMaxY := vpY2

	hasElements := false
	for _, elem := range elements {
		if !elem.visible || elem.screenSpace {
			continue
		}
		hasElements = true
		ex := elem.x
		ey := elem.y
		er := ex + elem.width
		eb := ey + elem.height

		if ex < worldMinX {
			worldMinX = ex
		}
		if ey < worldMinY {
			worldMinY = ey
		}
		if er > worldMaxX {
			worldMaxX = er
		}
		if eb > worldMaxY {
			worldMaxY = eb
		}
	}

	worldW := worldMaxX - worldMinX
	worldH := worldMaxY - worldMinY

	if worldW <= 0 || worldH <= 0 {
		return
	}

	// Add padding around the world bounds (10% each side).
	// Português: Adiciona padding ao redor dos limites mundo.
	padX := worldW * 0.1
	padY := worldH * 0.1
	worldMinX -= padX
	worldMinY -= padY
	worldW += padX * 2
	worldH += padY * 2

	// Calculate minimap position on screen (within visible area).
	// Português: Calcula a posição do minimapa na tela (dentro da área visível).
	mmX, mmY := c.overlayPosition(mm.corner, mmW, mmH, mxD, myD, visibleW, visibleH)

	// Scale factor: world → minimap pixels.
	// Português: Fator de escala: mundo → pixels do minimapa.
	scaleX := mmW / worldW
	scaleY := mmH / worldH
	scale := math.Min(scaleX, scaleY)

	// Center the content within the minimap.
	// Português: Centraliza o conteúdo dentro do minimapa.
	contentW := worldW * scale
	contentH := worldH * scale
	offsetX := mmX + (mmW-contentW)/2
	offsetY := mmY + (mmH-contentH)/2

	// Draw minimap background.
	// Português: Desenha fundo do minimapa.
	ctx.Set("fillStyle", mm.backgroundColor)
	ctx.Call("fillRect", mmX, mmY, mmW, mmH)
	ctx.Set("strokeStyle", mm.borderColor)
	ctx.Set("lineWidth", 1*d)
	ctx.Call("strokeRect", mmX+0.5, mmY+0.5, mmW-1, mmH-1)

	// Clip to minimap bounds to prevent elements from overflowing.
	// Português: Recorta aos limites do minimapa.
	ctx.Call("save")
	ctx.Call("beginPath")
	ctx.Call("rect", mmX, mmY, mmW, mmH)
	ctx.Call("clip")

	// Draw elements as small rectangles.
	// Português: Desenha elementos como pequenos retângulos.
	minElemSize := rulesSprite.MinimapMinElemSize * d // [DENSITY-FIX] minimum element size
	if hasElements {
		ctx.Set("fillStyle", mm.elementColor)
		for _, elem := range elements {
			if !elem.visible || elem.screenSpace {
				continue
			}
			rx := offsetX + (elem.x-worldMinX)*scale
			ry := offsetY + (elem.y-worldMinY)*scale
			rw := math.Max(elem.width*scale, minElemSize)
			rh := math.Max(elem.height*scale, minElemSize)
			ctx.Call("fillRect", rx, ry, rw, rh)
		}
	}

	// Draw viewport rectangle (the "you are here" indicator).
	// Português: Desenha retângulo da viewport.
	vx := offsetX + (vpX-worldMinX)*scale
	vy := offsetY + (vpY-worldMinY)*scale
	vw := vpW * scale
	vh := vpH * scale

	ctx.Set("fillStyle", mm.viewportFill)
	ctx.Call("fillRect", vx, vy, vw, vh)
	ctx.Set("strokeStyle", mm.viewportColor)
	ctx.Set("lineWidth", 1.5*d)
	ctx.Call("strokeRect", vx, vy, vw, vh)

	ctx.Call("restore")

	// Store computed values for click-to-navigate hit-testing.
	// These are already density-scaled screen coordinates.
	// Português: Armazena valores computados (já escalados por densidade).
	c.minimapScreenX = mmX
	c.minimapScreenY = mmY
	c.minimapScreenW = mmW // [DENSITY-FIX] store density-scaled width
	c.minimapScreenH = mmH // [DENSITY-FIX] store density-scaled height
	c.minimapScale = scale
	c.minimapWorldMinX = worldMinX
	c.minimapWorldMinY = worldMinY
	c.minimapContentOffsetX = offsetX
	c.minimapContentOffsetY = offsetY
}

// HandleMinimapClick
//
// English:
//
//	Tests if a screen click falls within the minimap and, if so, navigates the
//	camera to center on the clicked world position. Returns true if the click
//	was consumed by the minimap.
//
// Português:
//
//	Testa se um click na tela cai dentro do minimapa e, se sim, navega a câmera
//	para centralizar na posição mundo clicada.
func (c *Camera) HandleMinimapClick(screenX, screenY float64, canvasW, canvasH int) (consumed bool) {
	if c.minimap == nil || !c.minimap.enabled || !c.minimap.clickToNavigate {
		return false
	}

	// [DENSITY-FIX] Use density-scaled stored dimensions for hit-testing.
	// Português: Usa dimensões armazenadas (já escaladas) para hit-testing.
	if screenX < c.minimapScreenX || screenX > c.minimapScreenX+c.minimapScreenW ||
		screenY < c.minimapScreenY || screenY > c.minimapScreenY+c.minimapScreenH {
		return false
	}

	if c.minimapScale <= 0 {
		return false
	}

	// Convert minimap click to world coordinates.
	// Português: Converte click no minimapa para coordenadas mundo.
	worldX := c.minimapWorldMinX + (screenX-c.minimapContentOffsetX)/c.minimapScale
	worldY := c.minimapWorldMinY + (screenY-c.minimapContentOffsetY)/c.minimapScale

	// Center the camera on that world point.
	// [DENSITY-FIX] Use visible screen size, not canvas pixel size.
	// Português: Centraliza a câmera naquele ponto mundo (usa tamanho visível da tela).
	d := rulesDensity.GetDensity()
	visibleW := float64(canvasW) / d
	visibleH := float64(canvasH) / d
	c.OffsetX = worldX - visibleW/(2*c.Zoom)
	c.OffsetY = worldY - visibleH/(2*c.Zoom)

	return true
}

// =====================================================================
//  Draw Help Overlay | Desenhar Overlay de Ajuda
// =====================================================================

// DrawHelp
//
// English:
//
//	Draws the help overlay showing camera control instructions.
//	Called from renderWithCamera after other overlays.
//
//	[DENSITY-FIX] Font size, padding, margins, and border radius are all
//	scaled by rulesDensity.GetDensity() at draw time.
//
// Português:
//
//	Desenha o overlay de ajuda.
//	[DENSITY-FIX] Tamanho da fonte, padding, margens e raio de borda são
//	escalados por rulesDensity.GetDensity().
func (c *Camera) DrawHelp(ctx js.Value, canvasW, canvasH int) {
	if c.help == nil || !c.help.enabled {
		return
	}

	h := c.help
	d := rulesDensity.GetDensity()

	// [DENSITY-FIX] Scale all visual properties by density.
	// Português: Escala todas as propriedades visuais por densidade.
	fontSize := h.fontSize
	if fontSize <= 0 {
		fontSize = rulesSprite.HelpFontSize
	}
	fontSizeD := float64(fontSize) * d
	lineH := fontSizeD * h.lineHeight
	pad := h.padding * d
	mxD := h.marginX * d
	myD := h.marginY * d
	borderRadius := rulesSprite.HelpBorderRadius * d

	// [DENSITY-FIX] Visible area = canvasSize / density (browser viewport).
	// Português: Área visível = canvasSize / density (viewport do browser).
	visibleW := int(float64(canvasW) / d)
	visibleH := int(float64(canvasH) / d)

	// Calculate content dimensions.
	// Português: Calcula dimensões do conteúdo.
	ctx.Set("font", fmt.Sprintf("bold %dpx monospace", int(fontSizeD)))

	// Measure max line width.
	// Português: Mede a largura máxima das linhas.
	maxWidth := 0.0
	titleWidth := ctx.Call("measureText", h.title).Get("width").Float()
	if titleWidth > maxWidth {
		maxWidth = titleWidth
	}

	ctx.Set("font", fmt.Sprintf("%dpx monospace", int(fontSizeD)))
	for _, line := range h.lines {
		w := ctx.Call("measureText", line).Get("width").Float()
		if w > maxWidth {
			maxWidth = w
		}
	}

	boxW := maxWidth + pad*2
	boxH := lineH*float64(len(h.lines)+1) + pad*2 // +1 for title

	// Position (within visible area).
	// Português: Posição (dentro da área visível).
	bx, by := c.overlayPosition(h.corner, boxW, boxH, mxD, myD, visibleW, visibleH)

	// Background.
	// Português: Fundo.
	roundedRect(ctx, bx, by, boxW, boxH, borderRadius)
	ctx.Set("fillStyle", h.backgroundColor)
	ctx.Call("fill")
	ctx.Set("strokeStyle", h.borderColor)
	ctx.Set("lineWidth", 1*d)
	ctx.Call("stroke")

	// Title.
	// Português: Título.
	ctx.Set("font", fmt.Sprintf("bold %dpx monospace", int(fontSizeD)))
	ctx.Set("fillStyle", h.titleColor)
	ctx.Set("textAlign", "left")
	ctx.Set("textBaseline", "top")
	ctx.Call("fillText", h.title, bx+pad, by+pad)

	// Lines.
	// Português: Linhas.
	ctx.Set("font", fmt.Sprintf("%dpx monospace", int(fontSizeD)))
	ctx.Set("fillStyle", h.textColor)
	for i, line := range h.lines {
		y := by + pad + lineH*float64(i+1)
		ctx.Call("fillText", line, bx+pad, y)
	}
}

// =====================================================================
//  Shared Helper — Overlay Position | Helper Compartilhado — Posição de Overlay
// =====================================================================

// overlayPosition calculates the top-left position for an overlay in the given corner.
// All parameters (w, h, mx, my) should already be density-scaled by the caller.
//
// Português: Calcula a posição superior-esquerda para um overlay no canto dado.
// Todos os parâmetros (w, h, mx, my) já devem estar escalados por densidade.
func (c *Camera) overlayPosition(corner MinimapCorner, w, h, mx, my float64, canvasW, canvasH int) (x, y float64) {
	cw := float64(canvasW)
	ch := float64(canvasH)

	switch corner {
	case MinimapTopLeft:
		x = mx
		y = my
	case MinimapTopRight:
		x = cw - w - mx
		y = my
	case MinimapBottomLeft:
		x = mx
		y = ch - h - my
	case MinimapBottomRight:
		x = cw - w - mx
		y = ch - h - my
	}
	return
}

// roundedRect draws a rounded rectangle path (does not fill or stroke).
// Português: Desenha um caminho de retângulo arredondado (não preenche nem traça).
func roundedRect(ctx js.Value, x, y, w, h, r float64) {
	ctx.Call("beginPath")
	ctx.Call("moveTo", x+r, y)
	ctx.Call("lineTo", x+w-r, y)
	ctx.Call("arcTo", x+w, y, x+w, y+r, r)
	ctx.Call("lineTo", x+w, y+h-r)
	ctx.Call("arcTo", x+w, y+h, x+w-r, y+h, r)
	ctx.Call("lineTo", x+r, y+h)
	ctx.Call("arcTo", x, y+h, x, y+h-r, r)
	ctx.Call("lineTo", x, y+r)
	ctx.Call("arcTo", x, y, x+r, y, r)
	ctx.Call("closePath")
}

// Ensure fmt and math are used.
var _ = fmt.Sprintf
var _ = math.Min
