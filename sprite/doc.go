// Package sprite provides a high-performance, z-indexed sprite management system
// for Go WebAssembly applications.
//
// It renders SVG objects as cached images on a single visible canvas, supporting
// mouse and touch interactions including click, double-click, drag, and resize.
//
// Architecture:
//
//   - A single Stage manages one visible HTML canvas element and all its sprites.
//   - Each Element represents an SVG cached as a raster image on an offscreen canvas.
//   - Elements are drawn in z-index order (lowest index first, highest on top).
//   - All DOM event listeners are attached to the Stage canvas only; hit-testing
//     dispatches events to the correct Element by iterating from highest to lowest index.
//   - Rendering uses requestAnimationFrame with a dirty flag, so the canvas is only
//     redrawn when something actually changes.
//   - Touch double-click is detected by tap interval (~300ms) since the native
//     dblclick event is unreliable on touch devices.
//
// Português:
//
//	Package sprite fornece um sistema de gerenciamento de sprites de alta performance,
//	com z-index virtual, para aplicações Go WebAssembly.
//
//	Renderiza objetos SVG como imagens cacheadas em um único canvas visível, suportando
//	interações de mouse e touch incluindo click, double-click, arrastar e redimensionar.
package sprite
