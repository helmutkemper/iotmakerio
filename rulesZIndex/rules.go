// /ide/rulesZIndex/rules.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesZIndex

// rulesZIndex — Centralized z-index constants for device stacking order.
//
// English:
//
//	Defines the visual stacking order of devices on the stage. Lower values
//	render behind higher values. Each category has a base value with spacing
//	of 10 for future sub-levels.
//
//	Stacking order (bottom to top):
//	  0. BackgroundFrontend           — background images (plant layouts, etc.)
//	  1. Container (Loop)             — renders behind everything else
//	  2. Math (Add, Sub, Mul, Div) — standard leaf devices
//	  3. Constant (ConstInt)       — above math for visibility
//	  4. Display (Gauge backend)   — topmost data devices
//	  5. Display Frontend (LED, etc.) — frontend visual indicators
//	  6. Wire                         — wire connections
//	  7. UI (menus, overlays)         — always on top
//
// Português:
//
//	Define a ordem de empilhamento visual dos devices no stage.
//	Valores menores renderizam atrás de valores maiores.
//
//	Ordem (baixo para cima):
//	  0. BackgroundFrontend           — imagens de fundo (plantas industriais)
//	  1. Container (Loop)          — atrás de tudo
//	  2. Math (Add, Sub, Mul, Div) — devices folha padrão
//	  3. Constant (ConstInt)       — acima de math
//	  4. Display (Gauge backend)   — topo dos devices de dados
//	  5. Display Frontend (LED, etc.) — indicadores visuais do frontend
//	  6. Wire                         — conexões de fio
//	  7. UI (menus, overlays)         — sempre acima

// todo:
//   Create categories for graphic elements.
//   For example:
//   The "Lasso" always stays at the bottom, it's a container;
//   The "Device" always stays above the container;
//   Math must be renamed for device

const (
	// BackgroundFrontend is used for background images (industrial plant
	// layouts, diagrams, etc.) that must always render BELOW interactive
	// frontend elements like gauges, LEDs, and knobs. Multiple background
	// images can coexist — the user controls their relative order via
	// "Bring Forward" / "Send Backward" menu items within this z-range.
	//
	// Português: Usado para imagens de fundo (plantas industriais, diagramas)
	// que devem renderizar ABAIXO de elementos interativos do frontend.
	BackgroundFrontend = 5

	Container       = 10
	Math            = 20
	Constant        = 30
	Display         = 40
	DisplayFrontend = 40
	Wire            = 50
	UI              = 100
)
