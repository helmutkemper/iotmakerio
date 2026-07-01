// rulesDensity/density.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesDensity

import (
	"strconv"

	"github.com/helmutkemper/iotmakerio/utilsBrowser"
)

// density
//
// English:
//
//	Global scale factor based on screen density (default is 1.0).
//
// Português:
//
//	Fator global de escala com base na densidade da tela (padrão é 1.0).
var density float64 = 1.0

// init
//
// English:
//
//	Initializes the density value using the "scale" query string parameter,
//	if available. Accepts fractional values (e.g., "1.5", "2.0").
//
// Português:
//
//	Inicializa o valor da densidade usando o parâmetro "scale" da query string,
//	se estiver presente. Aceita valores fracionários (ex: "1.5", "2.0").
func init() {
	if scaleStr := utilsBrowser.GetQueryStringParam("scale"); scaleStr != "" {
		if scale, err := strconv.ParseFloat(scaleStr, 64); err == nil && scale > 0 {
			density = scale
			return
		}
	}

	//dpr := js.Global().Get("window").Get("devicePixelRatio")
	//if !dpr.IsUndefined() && !dpr.IsNull() && dpr.Float() > 0 {
	//	density = dpr.Float()
	//}
}

// GetDensity
//
// English:
//
//	Returns the current density value.
//
// Português:
//
//	Retorna o valor atual da densidade.
func GetDensity() float64 {
	return density
}

// SetDensity
//
// English:
//
//	Sets the global density value. Only accepts positive values.
//	Use this to change the density at runtime (e.g., when the user moves
//	the window to a different monitor).
//
// Português:
//
//	Define o valor global da densidade. Aceita apenas valores positivos.
//	Use para alterar a densidade em tempo de execução (ex: quando o usuário
//	move a janela para um monitor diferente).
func SetDensity(d float64) {
	if d > 0 {
		density = d
	}
}

// Density
//
// English:
//
//	Custom type to handle values affected by display density.
//	Internally stores the logical (unscaled) value. All Get methods apply
//	the global density factor automatically.
//
// Português:
//
//	Tipo personalizado para lidar com valores afetados pela densidade da tela.
//	Armazena internamente o valor lógico (sem escala). Todos os métodos Get
//	aplicam o fator de densidade global automaticamente.
type Density float64

// =====================================================================
//  Constructors | Construtores
// =====================================================================

// NewInt
//
// English:
//
//	Creates a new Density pointer from an integer value (unscaled).
//
// Português:
//
//	Cria um novo ponteiro Density a partir de um valor inteiro (sem escala).
func NewInt(value int) (p *Density) {
	p = new(Density)
	*p = Density(value)
	return
}

// NewFloat
//
// English:
//
//	Creates a new Density pointer from a float64 value (unscaled).
//
// Português:
//
//	Cria um novo ponteiro Density a partir de um valor float64 (sem escala).
func NewFloat(value float64) *Density {
	p := new(Density)
	*p = Density(value)
	return p
}

// =====================================================================
//  Conversion from scaled values | Conversão de valores escalados
// =====================================================================

// FromScaledInt
//
// English:
//
//	Converts a pixel value (already scaled) back to a Density (unscaled
//	logical unit). Use when receiving values from the sprite system or
//	any source that works in physical pixels.
//
// Português:
//
//	Converte um valor em pixels (já escalado) de volta para Density (unidade
//	lógica sem escala). Use ao receber valores do sistema sprite ou qualquer
//	fonte que trabalhe em pixels físicos.
func FromScaledInt(pixel int) Density {
	return Density(float64(pixel) / density)
}

// FromScaledFloat
//
// English:
//
//	Converts a float64 pixel value (already scaled) back to a Density
//	(unscaled logical unit).
//
// Português:
//
//	Converte um valor float64 em pixels (já escalado) de volta para Density
//	(unidade lógica sem escala).
func FromScaledFloat(pixel float64) Density {
	return Density(pixel / density)
}

// ConvertFlt
//
// English:
//
//	Converts a float64 pixel value (already scaled) to a Density by removing
//	the scale effect. Equivalent to FromScaledFloat.
//
// Português:
//
//	Converte um valor float64 em pixels (já escalado) para Density removendo
//	o efeito da escala. Equivalente a FromScaledFloat.
func ConvertFlt(value float64) (p Density) {
	return Density(value / density)
}

// FromPixel
//
// English:
//
//	Creates a Density from a sprite/canvas pixel coordinate. Use when
//	converting positions or sizes obtained from the sprite package back
//	to Density units.
//
// Português:
//
//	Cria um Density a partir de uma coordenada pixel do sprite/canvas. Use
//	ao converter posições ou tamanhos obtidos do package sprite de volta
//	para unidades Density.
func FromPixel(px float64) Density {
	return Density(px / density)
}

// =====================================================================
//  Getters — return scaled values | retornam valores escalados
// =====================================================================

// GetInt
//
// English:
//
//	Returns the scaled integer value (logical value × density).
//
// Português:
//
//	Retorna o valor inteiro escalado (valor lógico × densidade).
func (e Density) GetInt() int {
	return int(float64(e) * density)
}

// GetOriginalInt
//
// English:
//
//	Returns the unscaled original integer value (truncates the logical value).
//
// Português:
//
//	Retorna o valor original inteiro sem escala (trunca o valor lógico).
func (e Density) GetOriginalInt() int {
	return int(e)
}

// GetFloat
//
// English:
//
//	Returns the scaled float64 value (logical value × density).
//
// Português:
//
//	Retorna o valor float64 escalado (valor lógico × densidade).
func (e Density) GetFloat() float64 {
	return float64(e) * density
}

// ToPixel
//
// English:
//
//	Converts to the float64 used by the sprite package. Equivalent to
//	GetFloat — provided for semantic clarity when passing values to
//	sprite.Element methods.
//
// Português:
//
//	Converte para o float64 usado pelo package sprite. Equivalente a
//	GetFloat — fornecido para clareza semântica ao passar valores para
//	métodos de sprite.Element.
func (e Density) ToPixel() float64 {
	return float64(e) * density
}

// =====================================================================
//  Formatting | Formatação
// =====================================================================

// String
//
// English:
//
//	Returns the scaled value as a string.
//
// Português:
//
//	Retorna o valor escalado como uma string.
func (e Density) String() string {
	return strconv.FormatFloat(float64(e)*density, 'g', -1, 64)
}

// Pixel
//
// English:
//
//	Returns the scaled value formatted as a CSS pixel string (e.g., "32px").
//
// Português:
//
//	Retorna o valor escalado formatado como uma string de pixel CSS (ex: "32px").
func (e Density) Pixel() string {
	return strconv.FormatFloat(float64(e)*density, 'g', -1, 64) + "px"
}

// =====================================================================
//  Arithmetic | Aritmética
// =====================================================================

// Add
//
// English:
//
//	Returns the sum of two Density values (e + other). Both operands are in
//	the same logical (unscaled) space.
//
// Português:
//
//	Retorna a soma de dois valores Density (e + other). Ambos os operandos
//	estão no mesmo espaço lógico (sem escala).
func (e Density) Add(other Density) Density { return e + other }

// Sub
//
// English:
//
//	Returns the difference of two Density values (e - other).
//
// Português:
//
//	Retorna a diferença de dois valores Density (e - other).
func (e Density) Sub(other Density) Density { return e - other }

// Mul
//
// English:
//
//	Returns the Density multiplied by a scalar factor.
//
// Português:
//
//	Retorna o Density multiplicado por um fator escalar.
func (e Density) Mul(factor float64) Density { return Density(float64(e) * factor) }

// Div
//
// English:
//
//	Returns the Density divided by a scalar factor.
//
// Português:
//
//	Retorna o Density dividido por um fator escalar.
func (e Density) Div(factor float64) Density { return Density(float64(e) / factor) }

// Min
//
// English:
//
//	Returns the smaller of two Density values.
//
// Português:
//
//	Retorna o menor entre dois valores Density.
func (e Density) Min(other Density) Density {
	if e < other {
		return e
	}
	return other
}

// Max
//
// English:
//
//	Returns the larger of two Density values.
//
// Português:
//
//	Retorna o maior entre dois valores Density.
func (e Density) Max(other Density) Density {
	if e > other {
		return e
	}
	return other
}
