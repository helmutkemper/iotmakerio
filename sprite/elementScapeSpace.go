// sprite/elementScapeSpace.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

// =====================================================================
//  Screen Space | Espaço de Tela
// =====================================================================

// SetScreenSpace
//
// English:
//
//	When true, the element is rendered in screen (canvas pixel) coordinates,
//	ignoring the Camera transform. This means the element stays fixed on screen
//	regardless of pan/zoom. Useful for UI overlays like menus, HUDs, toolbars.
//
//	Screen-space elements:
//	  - Are drawn AFTER the camera transform is restored (on top of world elements)
//	  - Use screen coordinates for position (x, y) and size (width, height)
//	  - Are excluded from GetAllElementsBounds (FitAll ignores them)
//	  - Are excluded from the minimap
//	  - Hit-testing uses screen coordinates directly (no world conversion)
//	  - Drag deltas are NOT divided by zoom (they move in screen pixels)
//
// Português:
//
//	Quando true, o elemento é renderizado em coordenadas de tela (pixels do canvas),
//	ignorando a transformação da Camera. Isso significa que o elemento fica fixo na
//	tela independente de pan/zoom. Útil para overlays de UI como menus, HUDs, toolbars.
//
//	Elementos em screen-space:
//	  - São desenhados APÓS a restauração da transformação da câmera (acima dos elementos mundo)
//	  - Usam coordenadas de tela para posição (x, y) e tamanho (width, height)
//	  - São excluídos de GetAllElementsBounds (FitAll os ignora)
//	  - São excluídos do minimapa
//	  - Hit-testing usa coordenadas de tela diretamente (sem conversão para mundo)
//	  - Deltas de drag NÃO são divididos pelo zoom (movem em pixels de tela)
func (e *elementData) SetScreenSpace(screenSpace bool) {
	e.screenSpace = screenSpace
}

// IsScreenSpace
//
// English:
//
//	Returns whether the element is rendered in screen space.
//
// Português:
//
//	Retorna se o elemento é renderizado em espaço de tela.
func (e *elementData) IsScreenSpace() bool {
	return e.screenSpace
}
