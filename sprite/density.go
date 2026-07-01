package sprite

import (
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// =====================================================================
//  Density-aware convenience methods | Métodos de conveniência com Density
// =====================================================================
//
// English:
//
//	These methods eliminate the manual conversion between rulesDensity.Density
//	and float64 that every device currently performs. They correctly apply the
//	density factor in both directions:
//
//	  - "Set" methods: call ToPixel() to convert logical → physical
//	  - "Get" methods: call FromPixel() to convert physical → logical
//
//	This prevents the common bug of using rulesDensity.Density(float64Value)
//	which skips the division by the density factor.
//
// Português:
//
//	Estes métodos eliminam a conversão manual entre rulesDensity.Density e
//	float64 que todo device atualmente realiza. Eles aplicam corretamente o
//	fator de densidade em ambas as direções:
//
//	  - Métodos "Set": chamam ToPixel() para converter lógico → físico
//	  - Métodos "Get": chamam FromPixel() para converter físico → lógico
//
//	Isso previne o bug comum de usar rulesDensity.Density(float64Value) que
//	ignora a divisão pelo fator de densidade.

// =====================================================================
//  Position | Posição
// =====================================================================

// SetPositionD
//
// English:
//
//	Sets the element's position using Density values. The density factor is
//	applied automatically (logical → physical pixel conversion).
//
// Português:
//
//	Define a posição do elemento usando valores Density. O fator de densidade
//	é aplicado automaticamente (conversão lógico → pixel físico).
func (e *elementData) SetPositionD(x, y rulesDensity.Density) { // todo: ajuste para density
	e.SetPosition(x.ToPixel(), y.ToPixel())
}

// GetPositionD
//
// English:
//
//	Returns the element's position as Density values. The physical pixel
//	coordinates are correctly converted back to logical units by dividing
//	by the density factor.
//
// Português:
//
//	Retorna a posição do elemento como valores Density. As coordenadas em
//	pixels físicos são corretamente convertidas de volta para unidades lógicas
//	dividindo pelo fator de densidade.
func (e *elementData) GetPositionD() (x, y rulesDensity.Density) {
	fx, fy := e.GetPosition()
	return rulesDensity.FromPixel(fx), rulesDensity.FromPixel(fy)
}

// GetXD
//
// English:
//
//	Returns the element's horizontal position as a Density value.
//
// Português:
//
//	Retorna a posição horizontal do elemento como valor Density.
func (e *elementData) GetXD() rulesDensity.Density {
	return rulesDensity.FromPixel(e.x)
}

// GetYD
//
// English:
//
//	Returns the element's vertical position as a Density value.
//
// Português:
//
//	Retorna a posição vertical do elemento como valor Density.
func (e *elementData) GetYD() rulesDensity.Density {
	return rulesDensity.FromPixel(e.y)
}

// =====================================================================
//  Size | Tamanho
// =====================================================================

// SetSizeD
//
// English:
//
//	Sets the element's display size using Density values.
//
// Português:
//
//	Define o tamanho de exibição do elemento usando valores Density.
func (e *elementData) SetSizeD(width, height rulesDensity.Density) {
	e.SetSize(width.ToPixel(), height.ToPixel())
}

// GetSizeD
//
// English:
//
//	Returns the element's display size as Density values.
//
// Português:
//
//	Retorna o tamanho de exibição do elemento como valores Density.
func (e *elementData) GetSizeD() (width, height rulesDensity.Density) {
	fw, fh := e.GetSize()
	return rulesDensity.FromPixel(fw), rulesDensity.FromPixel(fh)
}

// GetWidthD
//
// English:
//
//	Returns the element's width as a Density value.
//
// Português:
//
//	Retorna a largura do elemento como valor Density.
func (e *elementData) GetWidthD() rulesDensity.Density {
	return rulesDensity.FromPixel(e.width)
}

// GetHeightD
//
// English:
//
//	Returns the element's height as a Density value.
//
// Português:
//
//	Retorna a altura do elemento como valor Density.
func (e *elementData) GetHeightD() rulesDensity.Density {
	return rulesDensity.FromPixel(e.height)
}

// =====================================================================
//  Min/Max Size | Tamanho Mínimo/Máximo
// =====================================================================

// SetMinSizeD
//
// English:
//
//	Sets the minimum allowed size when resizing, using Density values.
//
// Português:
//
//	Define o tamanho mínimo permitido ao redimensionar, usando valores Density.
func (e *elementData) SetMinSizeD(width, height rulesDensity.Density) {
	e.SetMinSize(width.ToPixel(), height.ToPixel())
}

// GetMinSizeD
//
// English:
//
//	Returns the minimum allowed size as Density values.
//
// Português:
//
//	Retorna o tamanho mínimo permitido como valores Density.
func (e *elementData) GetMinSizeD() (width, height rulesDensity.Density) {
	fw, fh := e.GetMinSize()
	return rulesDensity.FromPixel(fw), rulesDensity.FromPixel(fh)
}

// SetMaxSizeD
//
// English:
//
//	Sets the maximum allowed size when resizing, using Density values.
//
// Português:
//
//	Define o tamanho máximo permitido ao redimensionar, usando valores Density.
func (e *elementData) SetMaxSizeD(width, height rulesDensity.Density) {
	e.SetMaxSize(width.ToPixel(), height.ToPixel())
}

// GetMaxSizeD
//
// English:
//
//	Returns the maximum allowed size as Density values. Zero values mean
//	no constraint.
//
// Português:
//
//	Retorna o tamanho máximo permitido como valores Density. Valores zero
//	significam sem restrição.
func (e *elementData) GetMaxSizeD() (width, height rulesDensity.Density) {
	fw, fh := e.GetMaxSize()
	return rulesDensity.FromPixel(fw), rulesDensity.FromPixel(fh)
}

// =====================================================================
//  Drag Bounds | Limites de Arraste
// =====================================================================

// SetDragBoundsD
//
// English:
//
//	Sets the drag constraint rectangle using Density values.
//	Pass zero values to remove the constraint.
//
// Português:
//
//	Define o retângulo de restrição de arraste usando valores Density.
//	Passe valores zero para remover a restrição.
func (e *elementData) SetDragBoundsD(x, y, width, height rulesDensity.Density) {
	bounds := &Rect{
		X:      x.ToPixel(),
		Y:      y.ToPixel(),
		Width:  width.ToPixel(),
		Height: height.ToPixel(),
	}
	e.SetDragBounds(bounds)
}
