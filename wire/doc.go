// Package wire provides a connection/wiring system for a graphical programming IDE.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// It manages visual wires (connections) between output and input connectors on
// graphical components rendered on an HTML canvas via the sprite package. Wires
// are drawn as Manhattan-routed paths (horizontal and vertical segments only)
// with customizable visual styles per data type.
//
// Architecture:
//
//   - A single Manager coordinates all wires and connector registrations.
//   - Each connector (input or output port) is registered with a unique ConnectorID.
//   - Wires are created between compatible connectors based on a type compatibility matrix.
//   - Visual styles (color, width, dash pattern) are defined per data type.
//   - Wires are rendered directly on the canvas via the Stage's render callback.
//   - Hit-testing allows users to click on wires to select or delete them.
//   - Wire layer (above or below components) is user-configurable.
//
// Português:
//
//	Package wire fornece um sistema de conexão/fiação para uma IDE de programação gráfica.
//
//	Gerencia fios visuais (conexões) entre conectores de saída e entrada em componentes
//	gráficos renderizados em um canvas HTML via o pacote sprite. Fios são desenhados
//	como caminhos Manhattan (apenas segmentos horizontais e verticais) com estilos
//	visuais customizáveis por tipo de dado.
package wire
