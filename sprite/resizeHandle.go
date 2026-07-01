package sprite

import "syscall/js"

// resizeHandleCount is the total number of resize handles around an element.
//
// Português: Número total de alças de redimensionamento ao redor de um elemento.
const resizeHandleCount = 8

// allResizeHandles lists all 8 resize handles in a fixed order used for indexing
// the internal cache array.
//
// Português: Lista todas as 8 alças de redimensionamento em uma ordem fixa usada
// para indexar o array de cache interno.
var allResizeHandles = [resizeHandleCount]ResizeHandle{
	ResizeHandleTopLeft,
	ResizeHandleTop,
	ResizeHandleTopRight,
	ResizeHandleRight,
	ResizeHandleBottomRight,
	ResizeHandleBottom,
	ResizeHandleBottomLeft,
	ResizeHandleLeft,
}

// ResizeHandleRenderer
//
// English:
//
//	Interface that provides SVG DOM elements for the 8 resize handle buttons.
//	The sprite package calls RenderHandle once per handle, serializes the returned
//	SVG element to XML via XMLSerializer, caches it as a raster image on the canvas,
//	and then discards the SVG DOM element. This means the SVG is never added to the
//	visible DOM — it is used only as a template to generate the cached bitmap.
//
//	This interface is intentionally decoupled from any specific SVG framework or
//	coordinate system. The user implements it by wrapping their existing button types
//	(e.g., ResizeButtonHexagon) and returning the underlying js.Value.
//
//	Example adapter (user code):
//
//	  type hexagonAdapter struct {
//	      template *block.ResizeButtonHexagon
//	  }
//
//	  func (a *hexagonAdapter) RenderHandle(handle sprite.ResizeHandle) (js.Value, float64, float64) {
//	      btn := a.template.GetNew()
//	      // Set rotation based on handle direction
//	      // ...
//	      return btn.GetSvg().Get(), float64(btn.GetSize()*2), float64(btn.GetSize()*2)
//	  }
//
//	  func (a *hexagonAdapter) GetHandleOffset() float64 {
//	      return a.template.GetSpace().GetFloat()
//	  }
//
// Português:
//
//	Interface que fornece elementos SVG do DOM para os 8 botões de alça de
//	redimensionamento. O sprite package chama RenderHandle uma vez por alça,
//	serializa o elemento SVG retornado para XML via XMLSerializer, cacheia como
//	imagem raster no canvas, e então descarta o elemento SVG do DOM. Isso significa
//	que o SVG nunca é adicionado ao DOM visível — é usado apenas como template para
//	gerar o bitmap cacheado.
//
//	Esta interface é intencionalmente desacoplada de qualquer framework SVG ou
//	sistema de coordenadas específico. O usuário a implementa encapsulando seus
//	tipos de botão existentes (ex: ResizeButtonHexagon) e retornando o js.Value
//	subjacente.
type ResizeHandleRenderer interface {
	// RenderHandle
	//
	// English:
	//
	//  Creates and returns a js.Value representing an SVG DOM element for the given
	//  resize handle position. The element does not need to be attached to the DOM.
	//  The sprite package will serialize it to XML, cache it as a raster image, and
	//  discard the DOM element.
	//
	//  svgElement is the SVG DOM element (e.g., myTagSvg.Get()).
	//  width and height are the intrinsic dimensions of the SVG in pixels.
	//
	// Português:
	//
	//  Cria e retorna um js.Value representando um elemento SVG do DOM para a posição
	//  de alça de resize fornecida. O elemento não precisa estar anexado ao DOM.
	//  O sprite package irá serializá-lo para XML, cacheá-lo como imagem raster, e
	//  descartar o elemento DOM.
	//
	//  svgElement é o elemento SVG do DOM (ex: myTagSvg.Get()).
	//  width e height são as dimensões intrínsecas do SVG em pixels.
	RenderHandle(handle ResizeHandle) (svgElement js.Value, width float64, height float64)

	// GetHandleOffset
	//
	// English:
	//
	//  Returns the pixel distance from the element's edge to the handle center.
	//  A positive value places handles partially outside the element bounds,
	//  making them easier to grab. A value of zero centers handles exactly on the
	//  element edge.
	//
	// Português:
	//
	//  Retorna a distância em pixels da borda do elemento ao centro da alça.
	//  Um valor positivo posiciona as alças parcialmente fora dos limites do elemento,
	//  facilitando pegá-las. Um valor zero centraliza as alças exatamente na borda
	//  do elemento.
	GetHandleOffset() (offset float64)
}

// resizeHandleButton
//
// English:
//
//	Internal struct holding the cached raster image and dimensions for one
//	resize handle button. There are 8 of these per element (one per handle position).
//
// Português:
//
//	Struct interna que armazena a imagem raster cacheada e dimensões de um
//	botão de alça de resize. Existem 8 destes por elemento (um por posição de alça).
type resizeHandleButton struct {
	handle      ResizeHandle
	cachedImage js.Value
	width       float64
	height      float64
	cached      bool
}

// =====================================================================
//  Handle Positioning | Posicionamento de Alças
// =====================================================================

// resizeHandleCenter
//
// English:
//
//	Computes the center point (in canvas coordinates) where a resize handle button
//	should be placed for a given element geometry.
//
//	The layout is:
//
//	  TL ─── T ─── TR
//	  │             │
//	  L    elem     R
//	  │             │
//	  BL ─── B ─── BR
//
//	Offset pushes the handle outward from the element edge. A positive offset
//	means the handle center is outside the element bounding box, which makes it
//	easier to grab on touch devices without overlapping the element content.
//
// Português:
//
//	Computa o ponto central (em coordenadas do canvas) onde um botão de alça de
//	resize deve ser posicionado para uma dada geometria de elemento.
//
//	Offset empurra a alça para fora da borda do elemento. Um offset positivo
//	significa que o centro da alça está fora do bounding box do elemento, o que
//	facilita pegá-lo em dispositivos touch sem sobrepor o conteúdo do elemento.
func resizeHandleCenter(handle ResizeHandle, elemX float64, elemY float64, elemW float64, elemH float64, offset float64) (cx float64, cy float64) {
	switch handle {
	case ResizeHandleTopLeft:
		cx = elemX - offset
		cy = elemY - offset

	case ResizeHandleTop:
		cx = elemX + elemW/2
		cy = elemY - offset

	case ResizeHandleTopRight:
		cx = elemX + elemW + offset
		cy = elemY - offset

	case ResizeHandleRight:
		cx = elemX + elemW + offset
		cy = elemY + elemH/2

	case ResizeHandleBottomRight:
		cx = elemX + elemW + offset
		cy = elemY + elemH + offset

	case ResizeHandleBottom:
		cx = elemX + elemW/2
		cy = elemY + elemH + offset

	case ResizeHandleBottomLeft:
		cx = elemX - offset
		cy = elemY + elemH + offset

	case ResizeHandleLeft:
		cx = elemX - offset
		cy = elemY + elemH/2
	}
	return
}

// resizeHandleDrawRect
//
// English:
//
//	Returns the top-left position for drawing a handle image, given the handle's
//	center point and image dimensions. The image is centered on the point.
//
// Português:
//
//	Retorna a posição superior esquerda para desenhar uma imagem de alça, dado o
//	ponto central da alça e as dimensões da imagem. A imagem é centralizada no ponto.
func resizeHandleDrawRect(centerX float64, centerY float64, width float64, height float64) (drawX float64, drawY float64) {
	drawX = centerX - width/2
	drawY = centerY - height/2
	return
}

// =====================================================================
//  SVG Serialization | Serialização SVG
// =====================================================================

// svgElementToXml
//
// English:
//
//	Converts a js.Value SVG DOM element to its XML string representation using
//	the browser's XMLSerializer API. This works on both attached and detached
//	DOM nodes.
//
//	This is the bridge between the user's SVG framework (which creates DOM elements)
//	and the sprite package's image caching (which needs XML strings for Blob URL
//	creation).
//
// Português:
//
//	Converte um elemento SVG DOM js.Value para sua representação string XML usando
//	a API XMLSerializer do navegador. Funciona em nós DOM tanto anexados quanto
//	desanexados.
//
//	Esta é a ponte entre o framework SVG do usuário (que cria elementos DOM) e o
//	cache de imagens do sprite package (que precisa de strings XML para criação de
//	Blob URL).
func svgElementToXml(svgElement js.Value) (xmlStr string) {
	serializer := js.Global().Get("XMLSerializer").New()
	xmlStr = serializer.Call("serializeToString", svgElement).String()
	return
}
