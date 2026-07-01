package hexagon

import (
	"fmt"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// Hexagon
//
// English:
//
//	Represents a hexagonal grid cell in both pixel and coordinate space.
//
// It captures various properties such as position, layout, and cube coordinates.
//
// Português:
//
//	Representa uma célula do grid hexagonal tanto no espaço de pixels quanto no
//	espaço de coordenadas. Captura diversas propriedades como posição, layout e
//	coordenadas cúbicas.
type Hexagon struct {
	// col, row: Column and row in the doubled coordinate system.
	//
	// Português: Coluna e linha no sistema de coordenadas dobradas.
	col, row int

	// cx, cy: Center coordinates in logical (Density) space.
	//
	// Português: Coordenadas do centro no espaço lógico (Density).
	cx, cy rulesDensity.Density

	// layout defines how hexagons are positioned and transformed on the screen.
	//
	//   - Orientation specifies whether the hexes are pointy-topped or flat-topped,
	//     along with the transformation matrices for converting between hex and pixel space.
	//   - Size defines the radius (width and height) of a single hexagon in screen units (pixels).
	//   - Origin specifies the pixel coordinate where the hex grid origin (0,0,0) will be drawn.
	//
	// Português:
	//
	//   Layout define como os hexágonos são posicionados e transformados na tela.
	//
	//   - Orientation especifica se os hexágonos são pointy-topped ou flat-topped,
	//     junto com as matrizes de transformação para conversão entre espaço hex e pixel.
	//   - Size define o raio (largura e altura) de um único hexágono em unidades de tela (pixels).
	//   - Origin especifica a coordenada em pixel onde a origem do grid hex (0,0,0) será desenhada.
	layout Layout

	// point: 2D position in pixel or screen space, result of hex-to-pixel conversion.
	//
	// Português: Posição 2D no espaço de pixels ou tela, resultado da conversão hex-para-pixel.
	point Point

	// hex: Cube coordinate (q, r, s) used to model positions in the hexagonal grid.
	// Cube coordinates satisfy q + r + s = 0.
	//
	// Português: Coordenada cúbica (q, r, s) usada para modelar posições no grid hexagonal.
	// Coordenadas cúbicas satisfazem q + r + s = 0.
	hex Hex
}

// =====================================================================
//  Initialization | Inicialização
// =====================================================================

// Init
//
// English:
//
//	Initializes the hexagon with its layout based on the specified origin (x, y)
//	and size. All parameters are in logical (Density) units; the density factor
//	is applied via GetFloat() to produce physical pixel values for the layout.
//
// Português:
//
//	Inicializa o hexágono com seu layout baseado na origem (x, y) e tamanho
//	especificados. Todos os parâmetros estão em unidades lógicas (Density); o
//	fator de densidade é aplicado via GetFloat() para produzir valores em pixels
//	físicos para o layout.
func (e *Hexagon) Init(x, y, size rulesDensity.Density) {
	e.layout = Layout{
		Orientation: LayoutFlat,
		Size:        Point{X: size.GetFloat(), Y: size.GetFloat()},
		Origin:      Point{X: x.GetFloat(), Y: y.GetFloat()},
	}
}

// =====================================================================
//  Grid Coordinates | Coordenadas do Grid
// =====================================================================

// GetColRow
//
// English:
//
//	Returns the column and row indices of the hexagon in the doubled
//	coordinate system.
//
// Português:
//
//	Retorna os índices de coluna e linha do hexágono no sistema de
//	coordenadas dobradas.
func (e *Hexagon) GetColRow() (col, row int) {
	return e.col, e.row
}

// GetCenter
//
// English:
//
//	Returns the center coordinates of the hexagon as Density values
//	(logical units).
//
// Português:
//
//	Retorna as coordenadas do centro do hexágono como valores Density
//	(unidades lógicas).
func (e *Hexagon) GetCenter() (x, y rulesDensity.Density) {
	return e.cx, e.cy
}

// =====================================================================
//  Grid Adjustment — pixel space | Ajuste ao grid — espaço de pixels
// =====================================================================

// AdjustCenter
//
// English:
//
//	Recalculates and returns the adjusted pixel coordinates (cx, cy) for the
//	center of the nearest hexagon based on pixel inputs (x, y).
//	Both input and output are in physical pixel space.
//
// Português:
//
//	Recalcula e retorna as coordenadas em pixels ajustadas (cx, cy) para o
//	centro do hexágono mais próximo com base nas entradas em pixels (x, y).
//	Tanto entrada quanto saída estão no espaço de pixels físicos.
func (e *Hexagon) AdjustCenter(x, y int) (cx, cy int) {
	hex := e.colHexToRow(Point{X: float64(x), Y: float64(y)})
	point := HexToPixel(e.layout, hex)
	return int(point.X), int(point.Y)
}

// =====================================================================
//  Grid Adjustment — Density space | Ajuste ao grid — espaço Density
// =====================================================================

// AdjustCenterD
//
// English:
//
//	Recalculates and returns the adjusted coordinates for the center of the
//	nearest hexagon, using Density values. The conversion flow is:
//
//	  1. Density.ToPixel() → physical pixels (input to hex math)
//	  2. Hex math → snapped physical pixels
//	  3. FromPixel() → Density (output)
//
//	This eliminates the manual float64↔Density casting that was error-prone
//	in device code.
//
// Português:
//
//	Recalcula e retorna as coordenadas ajustadas para o centro do hexágono
//	mais próximo, usando valores Density. O fluxo de conversão é:
//
//	  1. Density.ToPixel() → pixels físicos (entrada para a matemática hex)
//	  2. Matemática hex → pixels físicos ajustados
//	  3. FromPixel() → Density (saída)
//
//	Isso elimina o casting manual float64↔Density que era propenso a erros
//	no código dos devices.
func (e *Hexagon) AdjustCenterD(x, y rulesDensity.Density) (cx, cy rulesDensity.Density) {
	hex := e.colHexToRow(Point{X: x.ToPixel(), Y: y.ToPixel()})
	point := HexToPixel(e.layout, hex)
	return rulesDensity.FromPixel(point.X), rulesDensity.FromPixel(point.Y)
}

// =====================================================================
//  Coordinate Lookups | Consultas de Coordenadas
// =====================================================================

// GetColRowByXY
//
// English:
//
//	Returns the column and row in the doubled coordinate system for the
//	given Density position. The density factor is applied via GetFloat()
//	to convert logical → physical before the hex lookup.
//
// Português:
//
//	Retorna a coluna e linha no sistema de coordenadas dobradas para a
//	posição Density fornecida. O fator de densidade é aplicado via
//	GetFloat() para converter lógico → físico antes da consulta hex.
func (e *Hexagon) GetColRowByXY(x, y rulesDensity.Density) (col, row int) {
	hex := e.colHexToRow(Point{X: x.GetFloat(), Y: y.GetFloat()})
	cord := QDoubledFromCube(hex)
	return cord.Col, cord.Row
}

// GetCowRowByXY
//
// English:
//
//	Deprecated: Use GetColRowByXY instead. This method is kept for backward
//	compatibility only.
//
// Português:
//
//	Obsoleto: Use GetColRowByXY. Este método é mantido apenas para
//	compatibilidade retroativa.
func (e *Hexagon) GetCowRowByXY(x, y rulesDensity.Density) (col, row int) {
	return e.GetColRowByXY(x, y)
}

// =====================================================================
//  Row/Col and Pixel setters | Setters de Row/Col e Pixel
// =====================================================================

// SetPixelXY
//
// English:
//
//	Sets the hexagon's column and row based on the provided pixel coordinates
//	(x, y) in Density units. The density factor is applied via GetFloat().
//
// Português:
//
//	Define a coluna e linha do hexágono com base nas coordenadas em pixels
//	(x, y) fornecidas em unidades Density. O fator de densidade é aplicado
//	via GetFloat().
func (e *Hexagon) SetPixelXY(x, y rulesDensity.Density) {
	hex := e.colHexToRow(Point{X: x.GetFloat(), Y: y.GetFloat()})
	cord := QDoubledFromCube(hex)
	e.SetRowCol(cord.Col, cord.Row)
}

// SetRowCol
//
// English:
//
//	Updates the hexagon's column and row indices and triggers conversion of
//	coordinates and layout adjustments.
//
// Português:
//
//	Atualiza os índices de coluna e linha do hexágono e dispara a conversão
//	de coordenadas e ajustes de layout.
func (e *Hexagon) SetRowCol(col, row int) {
	e.col = col
	e.row = row
	e.convertManager(e.col, e.row)
}

// =====================================================================
//  Path and Points | Caminho e Pontos
// =====================================================================

// GetPath
//
// English:
//
//	Generates a path for the hexagon's outline as a series of SVG-compatible
//	commands based on its corners. The coordinates are in physical pixel space.
//
// Português:
//
//	Gera um caminho para o contorno do hexágono como uma série de comandos
//	compatíveis com SVG baseados em seus vértices. As coordenadas estão no
//	espaço de pixels físicos.
func (e *Hexagon) GetPath() (path []string) {
	points := PolygonCorners(e.layout, e.hex)
	for k, point := range points {
		if k == 0 {
			path = append(path, fmt.Sprintf("M %.2f,%.2f ", point.X, point.Y))
			continue
		}

		path = append(path, fmt.Sprintf("L %.2f,%.2f ", point.X, point.Y))
	}
	path = append(path, "z")
	return
}

// GetPoints
//
// English:
//
//	Returns the 2D coordinates of the hexagon's corners as Density values
//	(logical units). The physical pixel coordinates from the hex math are
//	converted back to logical units via FromPixel.
//
// Português:
//
//	Retorna as coordenadas 2D dos vértices do hexágono como valores Density
//	(unidades lógicas). As coordenadas em pixels físicos da matemática hex
//	são convertidas de volta para unidades lógicas via FromPixel.
func (e *Hexagon) GetPoints() (points [][2]rulesDensity.Density) {
	ps := PolygonCorners(e.layout, e.hex)
	points = make([][2]rulesDensity.Density, len(ps))
	for k, point := range ps {
		points[k] = [2]rulesDensity.Density{
			rulesDensity.FromPixel(point.X),
			rulesDensity.FromPixel(point.Y),
		}
	}

	return
}

// =====================================================================
//  Internal | Interno
// =====================================================================

// colHexToRow
//
// English:
//
//	Converts a 2D pixel position (Point) to a hexagonal grid coordinate (Hex)
//	based on the instance layout. Internal method.
//
// Português:
//
//	Converte uma posição 2D em pixels (Point) para uma coordenada de grid
//	hexagonal (Hex) baseada no layout da instância. Método interno.
func (e *Hexagon) colHexToRow(point Point) (hex Hex) {
	return PixelToHex(e.layout, point)
}

// colRowToHex
//
// English:
//
//	Converts a column and row from the doubled coordinate system to a cube
//	coordinate (Hex). Internal method.
//
// Português:
//
//	Converte coluna e linha do sistema de coordenadas dobradas para uma
//	coordenada cúbica (Hex). Método interno.
func (e *Hexagon) colRowToHex(col, row int) (hex Hex) {
	return QDoubledToCube(DoubledCoordinate{Col: col, Row: row})
}

// convertManager
//
// English:
//
//	Recalculates and updates the hexagon's attributes based on the provided
//	column and row indices in the grid system. Converts grid coordinates to
//	pixel coordinates and stores the result as Density values.
//
// Português:
//
//	Recalcula e atualiza os atributos do hexágono baseado nos índices de
//	coluna e linha fornecidos no sistema de grid. Converte coordenadas de
//	grid para coordenadas de pixel e armazena o resultado como valores Density.
func (e *Hexagon) convertManager(col, row int) {
	e.col = col
	e.row = row
	e.hex = e.colRowToHex(col, row)
	e.point = HexToPixel(e.layout, e.hex)
	e.cx = rulesDensity.FromPixel(e.point.X)
	e.cy = rulesDensity.FromPixel(e.point.Y)
}
