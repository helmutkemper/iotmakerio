package splashScreen

import (
	"fmt"
	"strings"
	"syscall/js"
)

// =====================================================================
//  Text Measurement | Medição de Texto
// =====================================================================

// measureTextWidth
//
// English:
//
//	Measures the pixel width of a text string using an offscreen canvas context.
//	This avoids depending on external utility packages and works in any WASM
//	environment that has a canvas API available.
//
//	A new canvas is created per call for simplicity. In practice, AddText is called
//	infrequently (during loading) so the overhead is negligible.
//
// Português:
//
//	Mede a largura em pixels de uma string de texto usando um contexto canvas offscreen.
//	Evita depender de packages utilitários externos e funciona em qualquer ambiente
//	WASM que tenha a API canvas disponível.
func measureTextWidth(text string, fontFamily string, fontSize int, fontWeight string, fontStyle string) (width float64) {
	global := js.Global()
	canvas := global.Get("document").Call("createElement", "canvas")
	ctx := canvas.Call("getContext", "2d")
	ctx.Set("font", fmt.Sprintf("%s %s %dpx %s", fontStyle, fontWeight, fontSize, fontFamily))
	metrics := ctx.Call("measureText", text)
	width = metrics.Get("width").Float()
	return
}

// measureLineHeight
//
// English:
//
//	Estimates the line height for the given font configuration. Uses a representative
//	string containing ascenders and descenders to capture the full vertical extent.
//
// Português:
//
//	Estima a altura da linha para a configuração de fonte fornecida. Usa uma string
//	representativa contendo ascendentes e descendentes para capturar a extensão
//	vertical completa.
func measureLineHeight(fontFamily string, fontSize int, fontWeight string, fontStyle string) (height float64) {
	global := js.Global()
	canvas := global.Get("document").Call("createElement", "canvas")
	ctx := canvas.Call("getContext", "2d")
	ctx.Set("font", fmt.Sprintf("%s %s %dpx %s", fontStyle, fontWeight, fontSize, fontFamily))

	// Use actualBoundingBoxAscent + actualBoundingBoxDescent for a precise height.
	// These properties are widely supported in modern browsers.
	//
	// Português: Usa actualBoundingBoxAscent + actualBoundingBoxDescent para altura precisa.
	metrics := ctx.Call("measureText", "AaBbCcÇçDdGgJjPpQqYy0123456789")

	ascent := metrics.Get("actualBoundingBoxAscent")
	descent := metrics.Get("actualBoundingBoxDescent")

	if !ascent.IsUndefined() && !descent.IsUndefined() {
		height = ascent.Float() + descent.Float()
	} else {
		// Fallback: approximate line height as fontSize.
		// Português: Fallback: aproxima altura da linha como fontSize.
		height = float64(fontSize)
	}
	return
}

// =====================================================================
//  Line Breaking | Quebra de Linha
// =====================================================================

// wrapText
//
// English:
//
//	Breaks a text string into lines that fit within the given pixel width.
//	Uses character-level wrapping (same as the original splashScreen) rather than
//	word-level wrapping, because loading messages may contain long paths or
//	identifiers without spaces.
//
// Português:
//
//	Quebra uma string de texto em linhas que cabem na largura em pixels fornecida.
//	Usa quebra por caractere (igual ao splashScreen original) ao invés de quebra por
//	palavra, porque mensagens de carregamento podem conter caminhos longos ou
//	identificadores sem espaços.
func wrapText(text string, maxWidth float64, fontFamily string, fontSize int, fontWeight string, fontStyle string) (lines []string) {
	lines = make([]string, 0)

	if text == "" {
		return
	}

	runes := []rune(text)
	textLen := len(runes)
	start := 0

	for i := 1; i <= textLen; i++ {
		segment := string(runes[start:i])
		w := measureTextWidth(segment, fontFamily, fontSize, fontWeight, fontStyle)
		if w >= maxWidth && i-start > 1 {
			// The segment up to i-1 fits; break here.
			// Português: O segmento até i-1 cabe; quebra aqui.
			lines = append(lines, string(runes[start:i-1]))
			start = i - 1
		}
	}

	// Append the remaining text.
	// Português: Adiciona o texto restante.
	if start < textLen {
		lines = append(lines, string(runes[start:]))
	}

	return
}

// =====================================================================
//  SVG Text Generation | Geração de Texto SVG
// =====================================================================

// buildTextSvg
//
// English:
//
//	Generates an SVG XML string containing the given text lines rendered as <tspan>
//	elements inside a <text> element. The SVG has explicit width/height matching
//	the text box dimensions so the sprite Element can be positioned and sized correctly.
//
//	Lines are positioned using absolute y coordinates within the SVG, with each line
//	offset by lineHeight + textPadding.
//
// Português:
//
//	Gera uma string SVG XML contendo as linhas de texto fornecidas renderizadas como
//	elementos <tspan> dentro de um elemento <text>. O SVG tem width/height explícitos
//	correspondendo às dimensões da caixa de texto para que o sprite Element possa ser
//	posicionado e dimensionado corretamente.
func buildTextSvg(lines []string, boxWidth float64, boxHeight float64, fontFamily string, fontSize int, fontWeight string, fontStyle string, textColor string, textPadding int, lineHeight float64) (svgXml string) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(boxWidth), int(boxHeight),
	))

	b.WriteString(fmt.Sprintf(
		`<text fill="%s" font-family="%s" font-size="%d" font-weight="%s" font-style="%s">`,
		escapeXmlAttr(textColor),
		escapeXmlAttr(fontFamily),
		fontSize,
		escapeXmlAttr(fontWeight),
		escapeXmlAttr(fontStyle),
	))

	for i, line := range lines {
		// y position: first line at lineHeight (baseline), subsequent lines offset.
		// Português: posição y: primeira linha em lineHeight (baseline), linhas
		// subsequentes deslocadas.
		y := lineHeight + float64(i)*(lineHeight+float64(textPadding))
		b.WriteString(fmt.Sprintf(
			`<tspan x="0" y="%.1f">%s</tspan>`,
			y,
			escapeXmlText(line),
		))
	}

	b.WriteString(`</text></svg>`)

	svgXml = b.String()
	return
}

// =====================================================================
//  Overlay SVG | SVG do Overlay
// =====================================================================

// buildOverlaySvg
//
// English:
//
//	Generates an SVG XML string for a full-size colored rectangle used as the
//	semi-transparent overlay behind the splash image.
//
// Português:
//
//	Gera uma string SVG XML para um retângulo colorido de tamanho completo usado
//	como overlay semi-transparente atrás da imagem do splash.
func buildOverlaySvg(width int, height int, color string) (svgXml string) {
	svgXml = fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`+
			`<rect x="0" y="0" width="%d" height="%d" fill="%s"/>`+
			`</svg>`,
		width, height, width, height, escapeXmlAttr(color),
	)
	return
}

// =====================================================================
//  XML Escaping | Escape de XML
// =====================================================================

// escapeXmlText
//
// English:
//
//	Escapes special characters in XML text content.
//
// Português:
//
//	Escapa caracteres especiais em conteúdo de texto XML.
func escapeXmlText(s string) (escaped string) {
	escaped = s
	escaped = strings.ReplaceAll(escaped, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")
	return
}

// escapeXmlAttr
//
// English:
//
//	Escapes special characters in XML attribute values.
//
// Português:
//
//	Escapa caracteres especiais em valores de atributos XML.
func escapeXmlAttr(s string) (escaped string) {
	escaped = s
	escaped = strings.ReplaceAll(escaped, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")
	escaped = strings.ReplaceAll(escaped, "\"", "&quot;")
	escaped = strings.ReplaceAll(escaped, "'", "&apos;")
	return
}
