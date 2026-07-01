package sprite

import (
	"fmt"
	"math"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesSprite"
)

// =====================================================================
//  Camera — Infinite Canvas | Câmera — Canvas Infinito
// =====================================================================

// Camera
//
// English:
//
//	Camera controls the viewport of the Stage canvas, enabling pan (scroll) and
//	zoom (scale) without changing the logical coordinates of any Element.
//
//	The Camera is purely a rendering transform applied in the Stage's render loop.
//	Elements continue to store their positions in "world" coordinates. The Camera
//	converts between world and screen (canvas pixel) coordinates.
//
//	Coordinate spaces:
//	  - World: logical coordinates where elements live (0,0 = origin)
//	  - Screen: physical canvas pixels (0,0 = top-left of canvas)
//
//	Conversion:
//	  screenX = (worldX - offsetX) * zoom
//	  worldX  = screenX / zoom + offsetX
//
// Português:
//
//	Camera controla a viewport do canvas do Stage, habilitando pan (rolagem) e
//	zoom (escala) sem alterar as coordenadas lógicas de nenhum Element.
//
//	A Camera é puramente uma transformação de renderização aplicada no loop de
//	render do Stage. Os Elements continuam armazenando suas posições em coordenadas
//	"mundo". A Camera converte entre coordenadas mundo e tela (pixels do canvas).
//
//	Espaços de coordenadas:
//	  - Mundo: coordenadas lógicas onde os elementos vivem (0,0 = origem)
//	  - Tela: pixels físicos do canvas (0,0 = canto superior esquerdo do canvas)
//
//	Conversão:
//	  screenX = (worldX - offsetX) * zoom
//	  worldX  = screenX / zoom + offsetX
type Camera struct {

	// OffsetX, OffsetY: world coordinate of the top-left corner of the viewport.
	// Português: Coordenada mundo do canto superior esquerdo da viewport.
	OffsetX float64
	OffsetY float64

	// Zoom: scale factor. 1.0 = 100%, 0.5 = 50% (zoomed out), 2.0 = 200% (zoomed in).
	// Português: Fator de escala. 1.0 = 100%, 0.5 = 50% (afastado), 2.0 = 200% (aproximado).
	Zoom float64

	// MinZoom, MaxZoom: allowed zoom range.
	// Português: Faixa de zoom permitida.
	MinZoom float64
	MaxZoom float64

	// ZoomStep: multiplier per wheel notch. Default 0.1 means 10% per notch.
	// Português: Multiplicador por notch do scroll. Padrão 0.1 significa 10% por notch.
	ZoomStep float64

	// PanEnabled: whether pan via middle-mouse or two-finger drag is active.
	// Português: Se o pan via botão do meio do mouse ou arrasto com dois dedos está ativo.
	PanEnabled bool

	// ZoomEnabled: whether zoom via wheel or pinch is active.
	// Português: Se o zoom via scroll ou pinch está ativo.
	ZoomEnabled bool

	// GridEnabled: whether to draw a background grid.
	// Português: Se deve desenhar um grid de fundo.
	GridEnabled bool

	// GridSize: spacing between grid lines in world units.
	// Português: Espaçamento entre linhas do grid em unidades mundo.
	GridSize float64

	// GridColor: CSS color for minor grid lines.
	// Português: Cor CSS para linhas menores do grid.
	GridColor string

	// GridMajorEvery: draw a major line every N minor lines. 0 = no major lines.
	// Português: Desenha uma linha maior a cada N linhas menores. 0 = sem linhas maiores.
	GridMajorEvery int

	// GridMajorColor: CSS color for major grid lines.
	// Português: Cor CSS para linhas maiores do grid.
	GridMajorColor string

	// OriginEnabled: whether to draw a crosshair at world origin (0,0).
	// Português: Se deve desenhar um crosshair na origem do mundo (0,0).
	OriginEnabled bool

	// OriginColor: CSS color for the origin crosshair.
	// Português: Cor CSS para o crosshair da origem.
	OriginColor string

	// InfoEnabled: whether to show camera position/zoom info on screen.
	// Português: Se deve mostrar informações de posição/zoom da câmera na tela.
	InfoEnabled bool

	// InfoColor: CSS color for the info text.
	// Português: Cor CSS para o texto de informação.
	InfoColor string

	// InfoFontSize: font size for the info text in pixels.
	// Português: Tamanho da fonte para o texto de informação em pixels.
	InfoFontSize int

	// Animation state | Estado de animação
	animating                                bool
	animStartX, animStartY, animStartZoom    float64
	animTargetX, animTargetY, animTargetZoom float64
	animStartTime                            float64
	animDuration                             float64 // milliseconds
	animCallback                             func()  // called when animation completes

	// Pinch state (multi-touch zoom) | Estado de pinch (zoom multi-touch)
	pinchActive    bool
	pinchPointerA  int
	pinchPointerB  int
	pinchStartDist float64
	pinchStartZoom float64
	pinchCenterX   float64
	pinchCenterY   float64
	pinchPointsX   [2]float64
	pinchPointsY   [2]float64

	// Pan state (middle-mouse or two-finger) | Estado de pan
	panActive       bool
	panStartScreenX float64
	panStartScreenY float64
	panStartOffsetX float64
	panStartOffsetY float64
	panPointerID    int

	// Minimap overlay | Overlay do minimapa
	minimap *minimapConfig

	// Help overlay | Overlay de ajuda
	help *helpConfig

	// Keyboard bindings | Atalhos de teclado
	keys *keysConfig

	// Minimap hit-test cache (computed each frame in DrawMinimap).
	// Português: Cache de hit-test do minimapa (calculado a cada frame em DrawMinimap).
	minimapScreenX        float64
	minimapScreenY        float64
	minimapScreenW        float64 // [DENSITY-FIX] density-scaled width for hit-testing
	minimapScreenH        float64 // [DENSITY-FIX] density-scaled height for hit-testing
	minimapScale          float64
	minimapWorldMinX      float64
	minimapWorldMinY      float64
	minimapContentOffsetX float64
	minimapContentOffsetY float64

	// OnChange is called whenever the camera state changes (pan, zoom, animate).
	// Used by the workspace to trigger backup saves when the user moves the viewport.
	// May fire frequently — callers should debounce if needed.
	//
	// Português: Chamado quando o estado da câmera muda (pan, zoom, animate).
	// Usado pelo workspace para salvar backup ao mover a viewport.
	OnChange func()
}

// =====================================================================
//  Factory | Fábrica
// =====================================================================

// NewCamera
//
// English:
//
//	Creates a Camera with sensible defaults for an IDE canvas:
//	  - Zoom: 1.0 (100%), Range: 0.1–5.0
//	  - Grid: 20px spacing, major every 5
//	  - Origin: enabled, red crosshair
//	  - Info: enabled, bottom-left corner
//	  - Pan and Zoom: enabled
//
// Português:
//
//	Cria uma Camera com valores padrão sensatos para um canvas de IDE.
func NewCamera() *Camera {
	return &Camera{
		Zoom:     rulesSprite.CameraDefaultZoom,
		MinZoom:  rulesSprite.CameraMinZoom,
		MaxZoom:  rulesSprite.CameraMaxZoom,
		ZoomStep: rulesSprite.CameraZoomStep,

		PanEnabled:  rulesSprite.CameraPanEnabled,
		ZoomEnabled: rulesSprite.CameraZoomEnabled,

		GridEnabled:    rulesSprite.CameraGridEnabled,
		GridSize:       rulesSprite.CameraGridSize,
		GridColor:      rulesSprite.CameraGridColor,
		GridMajorEvery: rulesSprite.CameraGridMajorEvery,
		GridMajorColor: rulesSprite.CameraGridMajorColor,

		OriginEnabled: rulesSprite.CameraOriginEnabled,
		OriginColor:   rulesSprite.CameraOriginColor,

		InfoEnabled:  rulesSprite.CameraInfoEnabled,
		InfoColor:    rulesSprite.CameraInfoColor,
		InfoFontSize: rulesSprite.CameraInfoFontSize,
	}
}

// =====================================================================
//  Coordinate Conversion | Conversão de Coordenadas
// =====================================================================

// ScreenToWorld
//
// English:
//
//	Converts screen (canvas pixel) coordinates to world coordinates.
//
// Português:
//
//	Converte coordenadas de tela (pixel do canvas) para coordenadas mundo.
func (c *Camera) ScreenToWorld(screenX, screenY float64) (worldX, worldY float64) {
	worldX = screenX/c.Zoom + c.OffsetX
	worldY = screenY/c.Zoom + c.OffsetY
	return
}

// WorldToScreen
//
// English:
//
//	Converts world coordinates to screen (canvas pixel) coordinates.
//
// Português:
//
//	Converte coordenadas mundo para coordenadas de tela (pixel do canvas).
func (c *Camera) WorldToScreen(worldX, worldY float64) (screenX, screenY float64) {
	screenX = (worldX - c.OffsetX) * c.Zoom
	screenY = (worldY - c.OffsetY) * c.Zoom
	return
}

// =====================================================================
//  Pan | Deslocamento
// =====================================================================

// PanScreen
//
// English:
//
//	Moves the camera by the given screen-space delta. Converts to world delta
//	by dividing by zoom, so panning feels consistent regardless of zoom level.
//
// Português:
//
//	Move a câmera pelo delta fornecido no espaço de tela. Converte para delta
//	mundo dividindo pelo zoom.
func (c *Camera) PanScreen(dxScreen, dyScreen float64) {
	c.OffsetX -= dxScreen / c.Zoom
	c.OffsetY -= dyScreen / c.Zoom
	if c.OnChange != nil {
		c.OnChange()
	}
}

// =====================================================================
//  Zoom | Escala
// =====================================================================

// ZoomAt
//
// English:
//
//	Applies a zoom change centered on the given screen coordinates. The world
//	point under the cursor remains fixed on screen.
//
//	delta: positive = zoom in, negative = zoom out (typically ±ZoomStep).
//
// Português:
//
//	Aplica uma mudança de zoom centrada nas coordenadas de tela fornecidas.
//	O ponto mundo sob o cursor permanece fixo na tela.
func (c *Camera) ZoomAt(screenX, screenY, delta float64) {
	worldX, worldY := c.ScreenToWorld(screenX, screenY)
	oldZoom := c.Zoom
	c.Zoom *= 1.0 + delta
	c.Zoom = clampFloat(c.Zoom, c.MinZoom, c.MaxZoom)
	if c.Zoom == oldZoom {
		return
	}
	c.OffsetX = worldX - screenX/c.Zoom
	c.OffsetY = worldY - screenY/c.Zoom
	if c.OnChange != nil {
		c.OnChange()
	}
}

// ZoomTo
//
// English:
//
//	Sets the zoom to an absolute value, centered on the given screen point.
//
// Português:
//
//	Define o zoom para um valor absoluto, centrado no ponto de tela fornecido.
func (c *Camera) ZoomTo(screenX, screenY, newZoom float64) {
	worldX, worldY := c.ScreenToWorld(screenX, screenY)
	c.Zoom = clampFloat(newZoom, c.MinZoom, c.MaxZoom)
	c.OffsetX = worldX - screenX/c.Zoom
	c.OffsetY = worldY - screenY/c.Zoom
	if c.OnChange != nil {
		c.OnChange()
	}
}

// =====================================================================
//  Animation | Animação
// =====================================================================

// AnimateTo
//
// English:
//
//	Starts a smooth animation to the target offset and zoom (ease-in-out cubic).
//	Duration in milliseconds. Optional callback on completion.
//
// Português:
//
//	Inicia uma animação suave até o offset e zoom alvo (ease-in-out cúbico).
//	Duração em milissegundos. Callback opcional ao completar.
func (c *Camera) AnimateTo(targetX, targetY, targetZoom, durationMs float64, callback func()) {
	c.animating = true
	c.animStartX = c.OffsetX
	c.animStartY = c.OffsetY
	c.animStartZoom = c.Zoom
	c.animTargetX = targetX
	c.animTargetY = targetY
	c.animTargetZoom = clampFloat(targetZoom, c.MinZoom, c.MaxZoom)
	c.animStartTime = nowMillis()
	c.animDuration = durationMs
	c.animCallback = callback
}

// GoToOrigin
//
// English:
//
//	Animates the camera back to the world origin (0,0) at zoom 1.0.
//	Duration 0 = instant.
//
// Português:
//
//	Anima a câmera de volta à origem do mundo (0,0) no zoom 1.0.
//	Duração 0 = instantâneo.
func (c *Camera) GoToOrigin(durationMs float64) {
	if durationMs <= 0 {
		c.OffsetX = 0
		c.OffsetY = 0
		c.Zoom = 1.0
		if c.OnChange != nil {
			c.OnChange()
		}
		return
	}
	c.AnimateTo(0, 0, 1.0, durationMs, nil)
}

// FitAll
//
// English:
//
//	Animates the camera to fit the given world bounding box within the canvas,
//	with padding in pixels. Duration 0 = instant.
//
// Português:
//
//	Anima a câmera para enquadrar o bounding box mundo fornecido dentro do canvas,
//	com padding em pixels. Duração 0 = instantâneo.
func (c *Camera) FitAll(worldX, worldY, worldW, worldH float64, canvasW, canvasH int, padding float64, durationMs float64) {
	if worldW <= 0 || worldH <= 0 {
		c.GoToOrigin(durationMs)
		return
	}

	// [DENSITY-FIX] Use visible screen area, not canvas pixel dimensions.
	d := rulesDensity.GetDensity()
	visW := float64(canvasW) / d
	visH := float64(canvasH) / d

	availW := visW - padding*2
	availH := visH - padding*2
	if availW <= 0 || availH <= 0 {
		return
	}

	zoomW := availW / worldW
	zoomH := availH / worldH
	zoom := math.Min(zoomW, zoomH)
	zoom = clampFloat(zoom, c.MinZoom, c.MaxZoom)

	centerWorldX := worldX + worldW/2
	centerWorldY := worldY + worldH/2
	offsetX := centerWorldX - visW/(2*zoom)
	offsetY := centerWorldY - visH/(2*zoom)

	if durationMs <= 0 {
		c.OffsetX = offsetX
		c.OffsetY = offsetY
		c.Zoom = zoom
		return
	}
	c.AnimateTo(offsetX, offsetY, zoom, durationMs, nil)
}

// IsAnimating returns whether the camera is currently animating.
// Português: Retorna se a câmera está atualmente animando.
func (c *Camera) IsAnimating() bool {
	return c.animating
}

// Tick advances animation by one frame. Returns true if still animating.
// Português: Avança a animação em um frame. Retorna true se ainda animando.
func (c *Camera) Tick() (needsMoreFrames bool) {
	if !c.animating {
		return false
	}

	now := nowMillis()
	elapsed := now - c.animStartTime
	t := elapsed / c.animDuration

	if t >= 1.0 {
		c.OffsetX = c.animTargetX
		c.OffsetY = c.animTargetY
		c.Zoom = c.animTargetZoom
		c.animating = false
		if c.animCallback != nil {
			c.animCallback()
			c.animCallback = nil
		}
		if c.OnChange != nil {
			c.OnChange()
		}
		return false
	}

	e := easeInOutCubic(t)
	c.OffsetX = lerp(c.animStartX, c.animTargetX, e)
	c.OffsetY = lerp(c.animStartY, c.animTargetY, e)
	c.Zoom = lerp(c.animStartZoom, c.animTargetZoom, e)
	return true
}

// =====================================================================
//  Rendering — Grid | Renderização — Grid
// =====================================================================

// DrawGrid draws the background grid with LOD (level of detail).
// Português: Desenha o grid de fundo com LOD (nível de detalhe).
func (c *Camera) DrawGrid(ctx js.Value, canvasW, canvasH int) {
	if !c.GridEnabled || c.GridSize <= 0 {
		return
	}

	// [DENSITY-FIX] Use visible screen area for world bounds calculation.
	d := rulesDensity.GetDensity()
	visW := float64(canvasW) / d
	visH := float64(canvasH) / d

	worldLeft, worldTop := c.ScreenToWorld(0, 0)
	worldRight, worldBottom := c.ScreenToWorld(visW, visH)

	gridSize := c.GridSize

	// LOD: multiply grid size when lines are too close on screen.
	// Português: LOD: multiplica tamanho do grid quando linhas estão muito próximas na tela.
	screenGridSize := gridSize * c.Zoom
	for screenGridSize < 10 {
		gridSize *= float64(c.gridMajorEveryOrDefault())
		screenGridSize = gridSize * c.Zoom
		if gridSize > (worldRight-worldLeft)*2 {
			return
		}
	}

	startX := math.Floor(worldLeft/gridSize) * gridSize
	startY := math.Floor(worldTop/gridSize) * gridSize
	majorEvery := c.gridMajorEveryOrDefault()

	// Minor lines.
	// Português: Linhas menores.
	visWi := int(visW)
	visHi := int(visH)
	ctx.Set("strokeStyle", c.GridColor)
	ctx.Set("lineWidth", 1)
	ctx.Call("beginPath")
	for x := startX; x <= worldRight; x += gridSize {
		if majorEvery > 0 && isGridMajor(x, c.GridSize, majorEvery) {
			continue
		}
		sx, _ := c.WorldToScreen(x, 0)
		sx = math.Round(sx) + 0.5
		ctx.Call("moveTo", sx, 0)
		ctx.Call("lineTo", sx, visHi)
	}
	for y := startY; y <= worldBottom; y += gridSize {
		if majorEvery > 0 && isGridMajor(y, c.GridSize, majorEvery) {
			continue
		}
		_, sy := c.WorldToScreen(0, y)
		sy = math.Round(sy) + 0.5
		ctx.Call("moveTo", 0, sy)
		ctx.Call("lineTo", visWi, sy)
	}
	ctx.Call("stroke")

	// Major lines.
	// Português: Linhas maiores.
	if majorEvery > 0 {
		majorSize := c.GridSize * float64(majorEvery)
		majorStartX := math.Floor(worldLeft/majorSize) * majorSize
		majorStartY := math.Floor(worldTop/majorSize) * majorSize

		ctx.Set("strokeStyle", c.GridMajorColor)
		ctx.Set("lineWidth", 1)
		ctx.Call("beginPath")
		for x := majorStartX; x <= worldRight; x += majorSize {
			sx, _ := c.WorldToScreen(x, 0)
			sx = math.Round(sx) + 0.5
			ctx.Call("moveTo", sx, 0)
			ctx.Call("lineTo", sx, visHi)
		}
		for y := majorStartY; y <= worldBottom; y += majorSize {
			_, sy := c.WorldToScreen(0, y)
			sy = math.Round(sy) + 0.5
			ctx.Call("moveTo", 0, sy)
			ctx.Call("lineTo", visWi, sy)
		}
		ctx.Call("stroke")
	}
}

// DrawOrigin draws a crosshair at (0,0) or an arrow pointing to it if off-screen.
// Português: Desenha um crosshair em (0,0) ou uma seta apontando para ele se fora da tela.
func (c *Camera) DrawOrigin(ctx js.Value, canvasW, canvasH int) {
	if !c.OriginEnabled {
		return
	}

	d := rulesDensity.GetDensity()

	ox, oy := c.WorldToScreen(0, 0)

	// [DENSITY-FIX] Visible area = canvasSize / density.
	cw := float64(canvasW) / d
	ch := float64(canvasH) / d

	crossLen := rulesSprite.OriginCrossLen * d
	dotR := rulesSprite.OriginDotRadius * d
	arrowSize := rulesSprite.OriginArrowSize * d
	arrowW := rulesSprite.OriginArrowWidth * d
	margin := rulesSprite.OriginMargin * d

	if ox >= 0 && ox <= cw && oy >= 0 && oy <= ch {
		ctx.Set("strokeStyle", c.OriginColor)
		ctx.Set("lineWidth", rulesSprite.OriginLineWidth*d)
		ctx.Call("beginPath")
		ctx.Call("moveTo", ox, math.Max(0, oy-crossLen))
		ctx.Call("lineTo", ox, math.Min(ch, oy+crossLen))
		ctx.Call("moveTo", math.Max(0, ox-crossLen), oy)
		ctx.Call("lineTo", math.Min(cw, ox+crossLen), oy)
		ctx.Call("stroke")
		ctx.Call("beginPath")
		ctx.Call("arc", ox, oy, dotR, 0, 2*math.Pi)
		ctx.Call("stroke")
	} else {
		arrowX := clampFloat(ox, margin, cw-margin)
		arrowY := clampFloat(oy, margin, ch-margin)
		angle := math.Atan2(oy-arrowY, ox-arrowX)
		ctx.Set("fillStyle", c.OriginColor)
		ctx.Call("beginPath")
		ctx.Call("moveTo", arrowX+math.Cos(angle)*arrowSize, arrowY+math.Sin(angle)*arrowSize)
		ctx.Call("lineTo", arrowX+math.Cos(angle+2.5)*arrowW, arrowY+math.Sin(angle+2.5)*arrowW)
		ctx.Call("lineTo", arrowX+math.Cos(angle-2.5)*arrowW, arrowY+math.Sin(angle-2.5)*arrowW)
		ctx.Call("closePath")
		ctx.Call("fill")
	}
}

// DrawInfo draws camera position and zoom in the bottom-left corner.
// Português: Desenha posição da câmera e zoom no canto inferior esquerdo.
func (c *Camera) DrawInfo(ctx js.Value, canvasW, canvasH int) {
	if !c.InfoEnabled {
		return
	}

	d := rulesDensity.GetDensity()

	// [DENSITY-FIX] Use visible screen size for center calculation.
	visibleW := float64(canvasW) / d
	visibleH := float64(canvasH) / d
	centerX, centerY := c.ScreenToWorld(visibleW/2, visibleH/2)
	zoomPct := math.Round(c.Zoom * 100)
	text := fmt.Sprintf("(%d , %d)  %d%%", int(math.Round(centerX)), int(math.Round(centerY)), int(math.Round(zoomPct)))

	fontSize := c.InfoFontSize
	if fontSize <= 0 {
		fontSize = rulesSprite.CameraInfoFontSize
	}
	fontSizeD := int(float64(fontSize) * d)     // [DENSITY-FIX]
	marginD := rulesSprite.CameraInfoMargin * d // [DENSITY-FIX]

	ctx.Set("font", fmt.Sprintf("%dpx monospace", fontSizeD))
	ctx.Set("fillStyle", c.InfoColor)
	ctx.Set("textAlign", "left")
	ctx.Set("textBaseline", "bottom")

	// [DENSITY-FIX] Position relative to visible screen area, not canvas pixel size.
	visibleH = float64(canvasH) / d
	ctx.Call("fillText", text, marginD, visibleH-marginD)
}

// =====================================================================
//  Pinch Zoom (Multi-Touch) | Zoom por Pinch (Multi-Touch)
// =====================================================================

// StartPinch begins a pinch-zoom gesture with two touch points.
// Português: Inicia um gesto de pinch-zoom com dois pontos de touch.
func (c *Camera) StartPinch(id1, id2 int, x1, y1, x2, y2 float64) {
	c.pinchActive = true
	c.pinchPointerA = id1
	c.pinchPointerB = id2
	c.pinchStartDist = distance(x1, y1, x2, y2)
	c.pinchStartZoom = c.Zoom
	c.pinchCenterX = (x1 + x2) / 2
	c.pinchCenterY = (y1 + y2) / 2
	c.pinchPointsX = [2]float64{x1, x2}
	c.pinchPointsY = [2]float64{y1, y2}
}

// UpdatePinch updates the pinch gesture as fingers move.
// Português: Atualiza o gesto de pinch conforme os dedos se movem.
func (c *Camera) UpdatePinch(id int, sx, sy float64) {
	if !c.pinchActive {
		return
	}

	if id == c.pinchPointerA {
		c.pinchPointsX[0] = sx
		c.pinchPointsY[0] = sy
	} else if id == c.pinchPointerB {
		c.pinchPointsX[1] = sx
		c.pinchPointsY[1] = sy
	} else {
		return
	}

	currentDist := distance(c.pinchPointsX[0], c.pinchPointsY[0], c.pinchPointsX[1], c.pinchPointsY[1])
	if c.pinchStartDist <= 0 {
		return
	}

	ratio := currentDist / c.pinchStartDist
	newZoom := c.pinchStartZoom * ratio
	centerX := (c.pinchPointsX[0] + c.pinchPointsX[1]) / 2
	centerY := (c.pinchPointsY[0] + c.pinchPointsY[1]) / 2

	c.ZoomTo(centerX, centerY, newZoom)

	panDX := centerX - c.pinchCenterX
	panDY := centerY - c.pinchCenterY
	c.PanScreen(panDX, panDY)
	c.pinchCenterX = centerX
	c.pinchCenterY = centerY
}

// EndPinch ends the pinch-zoom gesture.
// Português: Finaliza o gesto de pinch-zoom.
func (c *Camera) EndPinch() {
	c.pinchActive = false
	if c.OnChange != nil {
		c.OnChange()
	}
}

// IsPinching returns whether a pinch gesture is active.
// Português: Retorna se um gesto de pinch está ativo.
func (c *Camera) IsPinching() bool {
	return c.pinchActive
}

// =====================================================================
//  Pan State | Estado de Pan
// =====================================================================

// StartPan begins a pan operation.
// Português: Inicia uma operação de pan.
func (c *Camera) StartPan(screenX, screenY float64, pointerID int) {
	c.panActive = true
	c.panStartScreenX = screenX
	c.panStartScreenY = screenY
	c.panStartOffsetX = c.OffsetX
	c.panStartOffsetY = c.OffsetY
	c.panPointerID = pointerID
}

// UpdatePan updates the pan from start state (avoids floating-point drift).
// Português: Atualiza o pan a partir do estado inicial (evita drift de ponto flutuante).
func (c *Camera) UpdatePan(screenX, screenY float64) {
	if !c.panActive {
		return
	}
	dx := screenX - c.panStartScreenX
	dy := screenY - c.panStartScreenY
	c.OffsetX = c.panStartOffsetX - dx/c.Zoom
	c.OffsetY = c.panStartOffsetY - dy/c.Zoom
}

// EndPan ends the pan operation.
// Português: Finaliza a operação de pan.
func (c *Camera) EndPan() {
	c.panActive = false
	if c.OnChange != nil {
		c.OnChange()
	}
}

// IsPanning returns whether a pan is active.
// Português: Retorna se um pan está ativo.
func (c *Camera) IsPanning() bool {
	return c.panActive
}

// =====================================================================
//  Query | Consulta
// =====================================================================

// GetVisibleWorldRect returns the world rectangle currently visible on canvas.
// Português: Retorna o retângulo mundo atualmente visível no canvas.
func (c *Camera) GetVisibleWorldRect(canvasW, canvasH int) (x, y, w, h float64) {
	// [DENSITY-FIX] Use visible screen area, not canvas pixel dimensions.
	d := rulesDensity.GetDensity()
	visW := float64(canvasW) / d
	visH := float64(canvasH) / d
	x, y = c.ScreenToWorld(0, 0)
	x2, y2 := c.ScreenToWorld(visW, visH)
	w = x2 - x
	h = y2 - y
	return
}

// =====================================================================
//  Internal Helpers | Helpers Internos
// =====================================================================

func (c *Camera) gridMajorEveryOrDefault() int {
	if c.GridMajorEvery <= 0 {
		return 5
	}
	return c.GridMajorEvery
}

func isGridMajor(coord, gridSize float64, majorEvery int) bool {
	majorSize := gridSize * float64(majorEvery)
	return math.Abs(math.Remainder(coord, majorSize)) < 0.01
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

func easeInOutCubic(t float64) float64 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	return 1 - math.Pow(-2*t+2, 3)/2
}
