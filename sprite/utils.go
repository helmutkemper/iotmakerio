package sprite

import (
	"fmt"
	"math"
	"syscall/js"
)

// =====================================
//  ID Generation | Geração de ID
// =====================================

// idCounter is a package-level counter used to generate unique element IDs.
//
// Using a simple incrementing counter instead of a UUID library because WASM binaries
// should be as small as possible. A UUID library would add unnecessary weight for
// something that only needs to be unique within this runtime session.
//
// Português:
// Usando um contador incremental simples ao invés de uma biblioteca UUID porque binários
// WASM devem ser os menores possíveis. Uma biblioteca UUID adicionaria peso desnecessário
// para algo que só precisa ser único dentro desta sessão de runtime.
var idCounter uint64

// generateID
//
// English:
//
//	Generates a unique string ID for an element using a monotonically increasing counter.
//
// Português:
//
//	Gera um ID string único para um elemento usando um contador monotonicamente crescente.
func generateID() (id string) {
	idCounter++
	id = fmt.Sprintf("sprite_%d", idCounter)
	return
}

// =====================================
//  Math Helpers | Auxiliares Matemáticos
// =====================================

// clamp
//
// English:
//
//	Constrains a value to the range [min, max].
//
// Português:
//
//	Restringe um valor ao intervalo [min, max].
func clamp(value float64, min float64, max float64) (result float64) {
	result = math.Max(min, math.Min(max, value))
	return
}

// distance
//
// English:
//
//	Returns the Euclidean distance between two points.
//
// Português:
//
//	Retorna a distância euclidiana entre dois pontos.
func distance(x1 float64, y1 float64, x2 float64, y2 float64) (dist float64) {
	dx := x2 - x1
	dy := y2 - y1
	dist = math.Sqrt(dx*dx + dy*dy)
	return
}

// =====================================
//  Coordinate Conversion | Conversão de Coordenadas
// =====================================

// canvasCoords
//
// English:
//
//	Converts a DOM pointer event's clientX/clientY to canvas-space coordinates,
//	accounting for CSS scaling (when the canvas element's CSS size differs from its
//	internal resolution).
//
//	This approach uses getBoundingClientRect once per event, which is faster than
//	caching the rect and invalidating on scroll/resize. In WASM, the JS interop
//	cost of one getBoundingClientRect call is negligible compared to the complexity
//	of maintaining a cached rect with scroll/resize listeners.
//
// Português:
//
//	Converte clientX/clientY de um evento de ponteiro DOM para coordenadas do espaço
//	do canvas, considerando escala CSS (quando o tamanho CSS do canvas difere de sua
//	resolução interna).
//
//	Esta abordagem usa getBoundingClientRect uma vez por evento, o que é mais rápido
//	do que cachear o rect e invalidar em scroll/resize. Em WASM, o custo de interop
//	JS de uma chamada getBoundingClientRect é desprezível comparado à complexidade de
//	manter um rect cacheado com listeners de scroll/resize.
func canvasCoords(canvas js.Value, canvasWidth int, canvasHeight int, event js.Value) (x float64, y float64) {
	rect := canvas.Call("getBoundingClientRect")

	clientX := event.Get("clientX").Float()
	clientY := event.Get("clientY").Float()

	cssWidth := rect.Get("width").Float()
	cssHeight := rect.Get("height").Float()

	// Scale factor handles CSS transforms or different CSS size vs canvas resolution.
	// Português: Fator de escala trata transformações CSS ou tamanho CSS diferente da resolução do canvas.
	scaleX := float64(canvasWidth) / cssWidth
	scaleY := float64(canvasHeight) / cssHeight

	x = (clientX - rect.Get("left").Float()) * scaleX
	y = (clientY - rect.Get("top").Float()) * scaleY
	return
}

// =====================================
//  Cursor Mapping | Mapeamento de Cursor
// =====================================

// cursorForResizeHandle
//
// English:
//
//	Returns the appropriate CSS cursor style for a given resize handle position.
//
// Português:
//
//	Retorna o estilo de cursor CSS apropriado para uma dada posição de alça de redimensionamento.
func cursorForResizeHandle(handle ResizeHandle) (cursor CursorStyle) {
	switch handle {
	case ResizeHandleTop:
		cursor = CursorNResize
	case ResizeHandleBottom:
		cursor = CursorSResize
	case ResizeHandleLeft:
		cursor = CursorWResize
	case ResizeHandleRight:
		cursor = CursorEResize
	case ResizeHandleTopLeft:
		cursor = CursorNWResize
	case ResizeHandleTopRight:
		cursor = CursorNEResize
	case ResizeHandleBottomLeft:
		cursor = CursorSWResize
	case ResizeHandleBottomRight:
		cursor = CursorSEResize
	default:
		cursor = CursorDefault
	}
	return
}

// =====================================
//  Time | Tempo
// =====================================

// nowMillis
//
// English:
//
//	Returns the current time in milliseconds using performance.now() for high-resolution
//	timing. Falls back to Date.now() if performance is unavailable.
//
//	Using performance.now() instead of Go's time.Now() because in WASM the Go time
//	package uses Date.now() internally, and performance.now() has sub-millisecond
//	precision which is better for double-click detection.
//
// Português:
//
//	Retorna o tempo atual em milissegundos usando performance.now() para temporização
//	de alta resolução. Recorre a Date.now() se performance não estiver disponível.
//
//	Usando performance.now() ao invés de time.Now() do Go porque em WASM o pacote
//	time do Go usa Date.now() internamente, e performance.now() tem precisão
//	sub-milissegundo que é melhor para detecção de double-click.
func nowMillis() (ms float64) {
	perf := js.Global().Get("performance")
	if !perf.IsUndefined() {
		ms = perf.Call("now").Float()
	} else {
		ms = js.Global().Get("Date").Call("now").Float()
	}
	return
}

// =====================================
//  Image Loading | Carregamento de Imagem
// =====================================

// svgToImage
//
// English:
//
//	Renders an SVG XML string into an HTML Image element and returns it as a js.Value
//	ready for use with canvas drawImage. Blocks the calling goroutine until the image
//	loads (must be called from a goroutine, not the main thread).
//
//	This is a standalone version of the element's loadImage pattern, extracted so it
//	can be used for resize buttons and other cases where we need to cache multiple
//	independent SVGs without going through the Element's single cache slot.
//
//	Uses the Blob URL approach (same as Element.CacheFromSvg) for efficiency:
//	no base64 overhead, browser decodes natively, URL revoked after load.
//
// Português:
//
//	Renderiza uma string XML SVG em um elemento HTML Image e retorna como js.Value
//	pronto para uso com canvas drawImage. Bloqueia a goroutine chamadora até a imagem
//	carregar (deve ser chamado de uma goroutine, não da thread principal).
//
//	Esta é uma versão independente do padrão loadImage do element, extraída para que
//	possa ser usada para botões de resize e outros casos onde precisamos cachear
//	múltiplos SVGs independentes sem passar pelo slot de cache único do Element.
func svgToImage(svgXml string) (img js.Value, err error) {
	if svgXml == "" {
		err = ErrSvgEmpty
		return
	}

	done := make(chan error, 1)
	var result js.Value

	global := js.Global()

	// Create Blob from SVG XML.
	// Português: Cria Blob a partir do XML SVG.
	blobParts := global.Get("Array").New()
	blobParts.Call("push", svgXml)

	blobOpts := global.Get("Object").New()
	blobOpts.Set("type", "image/svg+xml;charset=utf-8")
	blob := global.Get("Blob").New(blobParts, blobOpts)

	blobURL := global.Get("URL").Call("createObjectURL", blob)
	src := blobURL.String()

	imgElem := global.Get("document").Call("createElement", "img")

	var onLoad, onError js.Func

	onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		result = imgElem
		global.Get("URL").Call("revokeObjectURL", src)
		onLoad.Release()
		onError.Release()
		done <- nil
		return nil
	})

	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		global.Get("URL").Call("revokeObjectURL", src)
		onLoad.Release()
		onError.Release()
		done <- ErrSvgRenderFailed
		return nil
	})

	imgElem.Set("onload", onLoad)
	imgElem.Set("onerror", onError)
	imgElem.Set("src", src)

	err = <-done
	if err == nil {
		img = result
	}
	return
}
