package devices

import "syscall/js"

// SerializeSvgToXml
//
// English:
//
//	Converts a detached SVG DOM element (js.Value) into its XML string
//	representation using the browser's XMLSerializer API.
//
//	IMPORTANT: This function ensures the xmlns attribute is present on the root
//	SVG element. Without it, the browser will not recognize the serialized string
//	as valid SVG when loaded via Blob URL → Image, and the image load will fail
//	silently (Image.onerror instead of Image.onload).
//
// Português:
//
//	Converte um elemento SVG DOM desanexado (js.Value) para sua representação
//	XML string usando a API XMLSerializer do navegador.
//
//	IMPORTANTE: Esta função garante que o atributo xmlns esteja presente no elemento
//	SVG raiz. Sem ele, o navegador não reconhecerá a string serializada como SVG
//	válido ao carregar via Blob URL → Image, e o carregamento da imagem falhará
//	silenciosamente (Image.onerror em vez de Image.onload).
func SerializeSvgToXml(svgJsValue js.Value) (xmlStr string) {

	// Ensure the SVG namespace is set.
	// Português: Garante que o namespace SVG esteja definido.
	xmlns := svgJsValue.Call("getAttribute", "xmlns")
	if xmlns.IsNull() || xmlns.String() == "" {
		svgJsValue.Call("setAttribute", "xmlns", "http://www.w3.org/2000/svg")
	}

	serializer := js.Global().Get("XMLSerializer").New()
	xmlStr = serializer.Call("serializeToString", svgJsValue).String()
	return
}
