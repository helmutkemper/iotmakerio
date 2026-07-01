// /ide/ui/overlay/overlay_file_field.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package overlay

// overlay_file_field.go — Rendering logic for FieldFile form inputs.
//
// FieldFile provides a file upload input with live image preview. When the
// user selects a file, it is read via the FileReader API as a data URL
// (base64-encoded). The result is stored in a hidden <input> element so
// the form's doSave() can collect it like any other string field.
//
// This enables components like StatementBackgroundImage to let the user
// upload PNG or SVG images directly from the Inspect panel.
//
// Integration with renderForm() / renderEmbeddedForm():
//
// In the switch on field.Type, add this case BEFORE the default:
//
//   case FieldFile:
//       fileContainer, hiddenInput := buildFileField(doc, field)
//       row.Call("appendChild", fileContainer)
//       input = hiddenInput
//
// The hiddenInput goes into the inputs map (via the `input` variable)
// and its .value is the data URL string (or empty if no file selected).
// The fileContainer holds the visible UI (file button + preview).
//
// Português: Lógica de renderização para inputs FieldFile.
// Lê arquivos como data URL (base64) via FileReader API. O resultado
// é armazenado em um <input> oculto para coleta pelo doSave().

import (
	"fmt"
	"strings"
	"syscall/js"
)

// buildFileField creates the visible UI for a file upload field and a hidden
// input that stores the base64 data URL result.
//
// Returns:
//   - container: the visible DOM element to append to the form row
//   - hiddenInput: the hidden <input> whose .value holds the data URL string
//
// The container includes:
//  1. A styled file-select button (since <input type="file"> is hard to style)
//  2. A filename label showing the selected file name
//  3. An image preview area (shown when a valid image is selected or pre-loaded)
//
// When field.Value is a non-empty data URL, the preview is shown immediately
// (this happens when reopening Inspect on a component that already has an image).
//
// Português: Cria a UI visível para upload de arquivo e um input oculto
// que armazena o data URL base64 resultante.
func buildFileField(doc js.Value, field Field) (container js.Value, hiddenInput js.Value) {
	// Hidden input stores the base64 data URL for form collection.
	hiddenInput = doc.Call("createElement", "input")
	hiddenInput.Set("type", "hidden")
	hiddenInput.Set("value", field.Value)

	// Main container — vertical flex layout.
	container = doc.Call("createElement", "div")
	container.Get("style").Set("cssText",
		"flex:1;display:flex;flex-direction:column;gap:8px;")

	// ── Top row: button + filename ────────────────────────────────────
	topRow := doc.Call("createElement", "div")
	topRow.Get("style").Set("cssText",
		"display:flex;align-items:center;gap:8px;")

	// Actual <input type="file"> — hidden, triggered by the styled button.
	fileInput := doc.Call("createElement", "input")
	fileInput.Set("type", "file")
	if field.Accept != "" {
		fileInput.Set("accept", field.Accept)
	}
	fileInput.Get("style").Set("cssText", "display:none;")

	// Styled button that triggers the file input.
	btn := doc.Call("createElement", "button")
	btn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:5px 12px;cursor:pointer;font-size:12px;font-weight:500;"+
			"font-family:sans-serif;white-space:nowrap;transition:opacity 0.15s;",
		colSurface0, colText, colSurface1))
	btn.Set("textContent", "Choose File…")
	btn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			btn.Get("style").Set("opacity", "0.85")
			return nil
		}))
	btn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			btn.Get("style").Set("opacity", "1")
			return nil
		}))
	btn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("preventDefault")
			fileInput.Call("click")
			return nil
		}))

	// Filename label — shows "No file selected" or the filename.
	fileLabel := doc.Call("createElement", "span")
	fileLabel.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:12px;font-family:sans-serif;"+
			"overflow:hidden;text-overflow:ellipsis;white-space:nowrap;",
		colSubtext))

	// Set initial label based on whether a value already exists.
	if field.Value != "" && strings.HasPrefix(field.Value, "data:") {
		fileLabel.Set("textContent", "Image loaded")
	} else {
		fileLabel.Set("textContent", "No file selected")
	}

	topRow.Call("appendChild", btn)
	topRow.Call("appendChild", fileLabel)
	topRow.Call("appendChild", fileInput)
	container.Call("appendChild", topRow)

	// ── Preview area ─────────────────────────────────────────────────
	preview := doc.Call("createElement", "div")
	preview.Get("style").Set("cssText", fmt.Sprintf(
		"max-height:150px;overflow:hidden;border-radius:4px;"+
			"border:1px solid %s;background:%s;display:flex;"+
			"align-items:center;justify-content:center;",
		colSurface1, colMantle))

	// If a data URL already exists, show the preview immediately.
	if field.Value != "" && strings.HasPrefix(field.Value, "data:") {
		showPreview(doc, preview, field.Value)
	} else {
		// Empty state placeholder.
		placeholder := doc.Call("createElement", "div")
		placeholder.Get("style").Set("cssText", fmt.Sprintf(
			"padding:20px;color:%s;font-size:12px;font-family:sans-serif;"+
				"text-align:center;", colOverlay0))
		placeholder.Set("textContent", "No image")
		preview.Call("appendChild", placeholder)
	}

	container.Call("appendChild", preview)

	// ── File change handler ──────────────────────────────────────────
	// When the user selects a file, read it as a data URL and store
	// the result in the hidden input. Also update the preview and label.
	fileInput.Call("addEventListener", "change",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			files := fileInput.Get("files")
			if files.Get("length").Int() == 0 {
				return nil
			}
			file := files.Index(0)
			fileName := file.Get("name").String()
			fileLabel.Set("textContent", fileName)

			// Read file as data URL (base64).
			reader := js.Global().Get("FileReader").New()
			reader.Call("addEventListener", "load",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					dataURL := reader.Get("result").String()
					hiddenInput.Set("value", dataURL)

					// Update preview.
					preview.Set("innerHTML", "")
					showPreview(doc, preview, dataURL)
					return nil
				}))

			reader.Call("addEventListener", "error",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					fileLabel.Set("textContent", "Error reading file")
					return nil
				}))

			reader.Call("readAsDataURL", file)
			return nil
		}))

	return container, hiddenInput
}

// showPreview inserts an <img> element into the preview container showing
// the image from the given data URL.
//
// For SVG data URLs, the image is displayed with a checkerboard background
// (CSS pattern) to indicate transparent areas — same convention used by
// image editors like Photoshop and GIMP.
//
// Português: Insere um <img> no container de preview mostrando a imagem
// do data URL fornecido. SVGs usam fundo xadrez para indicar transparência.
func showPreview(doc js.Value, container js.Value, dataURL string) {
	img := doc.Call("createElement", "img")
	img.Set("src", dataURL)
	img.Get("style").Set("cssText",
		"max-width:100%;max-height:148px;object-fit:contain;display:block;"+
			"margin:auto;")

	// Checkerboard background for transparency — only for PNG/SVG images.
	if strings.Contains(dataURL, "image/png") || strings.Contains(dataURL, "image/svg") {
		container.Get("style").Set("background",
			"repeating-conic-gradient(#2a2a3e 0% 25%, #1e1e2e 0% 50%) "+
				"50% / 16px 16px")
	}

	container.Call("appendChild", img)
}
