// Package rulesIcon
//
// English:
//
//	Defines visual rules and default styles for pipeline icons in the IDE,
//	including colors, dimensions, filters, and status-based theming for both
//	system and element icons.
//
// Português:
//
//	Define regras visuais e estilos padrão para ícones de pipeline na IDE,
//	incluindo cores, dimensões, filtros e tematização baseada em status para
//	ícones de sistema e de elemento.
package rulesIcon

import (
	"image/color"
	"reflect"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// Pipeline status constants
//
// English:
//
//	Defines the possible visual states of a pipeline icon.
//
//	 KPipeLineNormal:     Default state with standard colors.
//	 KPipeLineDisabled:   Disabled/inactive state with muted colors.
//	 KPipeLineSelected:   Selected state with highlighted colors.
//	 KPipeLineAttention1: First level of attention, alters the background color.
//	 KPipeLineAttention2: Second level of attention, alters border and label colors.
//	 KPipeLineAlert:      Alert state (reserved for future use).
//
// Português:
//
//	Define os possíveis estados visuais de um ícone de pipeline.
//
//	 KPipeLineNormal:     Estado padrão com cores normais.
//	 KPipeLineDisabled:   Estado desabilitado/inativo com cores atenuadas.
//	 KPipeLineSelected:   Estado selecionado com cores destacadas.
//	 KPipeLineAttention1: Primeiro nível de atenção, altera a cor de fundo.
//	 KPipeLineAttention2: Segundo nível de atenção, altera as cores da borda e do rótulo.
//	 KPipeLineAlert:      Estado de alerta.
const (
	KPipeLineNormal int = iota
	KPipeLineDisabled
	KPipeLineSelected
	KPipeLineAttention1
	KPipeLineAttention2
	KPipeLineAlert
)

// Data
//
// English:
//
//	Holds all visual properties for rendering a pipeline icon, including
//	position, dimensions, colors, label text, SVG path, and metadata.
//
// Português:
//
//	Contém todas as propriedades visuais para renderizar um ícone de pipeline,
//	incluindo posição, dimensões, cores, texto do rótulo, caminho SVG e
//	metadados.
type Data struct {
	// ID is a unique identifier for the element. If empty, a UUID will be generated.
	//
	// Português: Identificador único do elemento. Se vazio, um UUID será gerado.
	ID string

	Status int

	// X is the initial horizontal position on the canvas.
	//
	// Português: Posição horizontal inicial no canvas.
	X rulesDensity.Density

	// Y is the initial vertical position on the canvas.
	//
	// Português: Posição vertical inicial no canvas.
	Y rulesDensity.Density

	// Width is the initial display width. If zero, uses the SVG intrinsic width.
	//
	// Português: Largura de exibição inicial. Se zero, usa a largura intrínseca do SVG.
	Width rulesDensity.Density

	// Height is the initial display height. If zero, uses the altura intrínseca do SVG.
	//
	// Português: Altura de exibição inicial. Se zero, usa a altura intrínseca do SVG.
	Height rulesDensity.Density

	// Index is the initial z-index for layering order.
	//
	// Português: Z-index inicial para a ordem de camadas.
	Index int

	// Visible determines whether the element is drawn.
	//
	// Português: Determina se o elemento é desenhado.
	Visible bool

	// DragEnable allows the element to be dragged.
	//
	// Português: Permite que o elemento seja arrastado.
	DragEnable bool

	// ResizeEnable allows the element to be resized via edge/corner handles.
	//
	// Português: Permite que o elemento seja redimensionado pelas alças de borda/canto.
	ResizeEnable bool

	// SvgXml is the raw SVG XML string to be rendered and cached.
	//
	// Português: String XML do SVG bruto a ser renderizado e cacheado.
	SvgXml string

	// ResizeHandleSize defines the pixel size of the interactive resize handles area.
	// Default: 8.
	//
	// Português: Define o tamanho em pixels da área interativa das alças de redimensionamento.
	// Padrão: 8.
	ResizeHandleSize rulesDensity.Density

	// MinWidth is the minimum allowed width when resizing. Default: 10.
	//
	// Português: Largura mínima permitida ao redimensionar. Padrão: 10.
	MinWidth rulesDensity.Density

	// MinHeight is the minimum allowed height when resizing. Default: 10.
	//
	// Português: Altura mínima permitida ao redimensionar. Padrão: 10.
	MinHeight rulesDensity.Density

	IconViewBox     []int
	Label           string
	LabelFontSize   rulesDensity.Density
	LabelY          rulesDensity.Density
	Path            string
	ColorIcon       color.RGBA
	ColorBorder     color.RGBA
	ColorLabel      color.RGBA
	ColorBackground color.RGBA
	Name            string
	Category        string
}

// BorderColor
//
// English:
//
//	Default border color for icons in normal state (dark gray).
//
// Português:
//
//	Cor padrão da borda dos ícones no estado normal (cinza escuro).
var BorderColor = color.RGBA{R: 0x5F, G: 0x5F, B: 0x5F, A: 0xFF}

// BorderColorAttention2
//
// English:
//
//	Border color used in the attention level 2 state (red).
//
// Português:
//
//	Cor da borda usada no estado de atenção nível 2 (vermelho).
var BorderColorAttention2 = color.RGBA{R: 255, G: 0, B: 0, A: 255}

// FillColor
//
// English:
//
//	Default fill color for the icon path in normal state (light blue).
//
// Português:
//
//	Cor de preenchimento padrão do caminho do ícone no estado normal (azul
//	claro).
var FillColor = color.RGBA{R: 180, G: 180, B: 255, A: 255}

// TextColor
//
// English:
//
//	Default label text color (black).
//
// Português:
//
//	Cor padrão do texto do rótulo (preto).
var TextColor = color.RGBA{R: 0, G: 0, B: 0, A: 255}

// TextColorSelected
//
// English:
//
//	Label text color when the icon is in the selected state (dark red).
//
// Português:
//
//	Cor do texto do rótulo quando o ícone está no estado selecionado (vermelho
//	escuro).
var TextColorSelected = color.RGBA{R: 128, G: 0, B: 0, A: 255}

// TextColorAttention2
//
// English:
//
//	Label text color when the icon is in the attention level 2 state (light
//	red).
//
// Português:
//
//	Cor do texto do rótulo quando o ícone está no estado de atenção nível 2
//	(vermelho claro).
var TextColorAttention2 = color.RGBA{R: 255, G: 180, B: 180, A: 255}

// TextColorDisabled
//
// English:
//
//	Label text color when the icon is in the disabled state (semi-transparent
//	black).
//
// Português:
//
//	Cor do texto do rótulo quando o ícone está no estado desabilitado (preto
//	semi-transparente).
var TextColorDisabled = color.RGBA{R: 0, G: 0, B: 0, A: 0x6f}

// CategoryIconColorSelected
//
// English:
//
//	Icon fill color for the selected state (dark red).
//
// Português:
//
//	Cor de preenchimento do ícone no estado selecionado (vermelho escuro).
var CategoryIconColorSelected = color.RGBA{R: 128, G: 0, B: 0, A: 255}

// CategoryIconColor
//
// English:
//
//	Default background color for category icons (off-white/cream).
//
// Português:
//
//	Cor de fundo padrão para ícones de categoria (branco amarelado/creme).
var CategoryIconColor = color.RGBA{R: 0xf8, G: 0xf8, B: 0xef, A: 0xff}

// CategoryIconColorAttention1
//
// English:
//
//	Background color for category icons in the attention level 1 state (light
//	red).
//
// Português:
//
//	Cor de fundo para ícones de categoria no estado de atenção nível 1
//	(vermelho claro).
var CategoryIconColorAttention1 = color.RGBA{R: 255, G: 180, B: 180, A: 255}

// CategoryIconColorDisabled
//
// English:
//
//	Icon fill color for the disabled state (semi-transparent light blue).
//
// Português:
//
//	Cor de preenchimento do ícone no estado desabilitado (azul claro
//	semi-transparente).
var CategoryIconColorDisabled = color.RGBA{R: 0xb4, G: 0xb4, B: 0xff, A: 0x6f}

// BorderWidth
//
// English:
//
//	Default border width for icon containers, in density-aware units.
//
// Português:
//
//	Largura padrão da borda dos contêineres de ícone, em unidades sensíveis
//	à densidade.
var BorderWidth = rulesDensity.Density(2)

// TextY
//
// English:
//
//	Default vertical position of the label text, in density-aware units.
//
// Português:
//
//	Posição vertical padrão do texto do rótulo, em unidades sensíveis à
//	densidade.
var TextY = rulesDensity.Density(160)

// FontFamily
//
// English:
//
//	Default font family for label text.
//
// Português:
//
//	Família de fonte padrão para o texto do rótulo.
var FontFamily = "Helvetica"

// FontWeight
//
// English:
//
//	Default font weight for label text.
//
// Português:
//
//	Peso de fonte padrão para o texto do rótulo.
var FontWeight = "normal"

// FontStyle
//
// English:
//
//	Default font style for label text.
//
// Português:
//
//	Estilo de fonte padrão para o texto do rótulo.
var FontStyle = "normal"

// FontSize
//
// English:
//
//	Default font size for label text, in density-aware units.
//
// Português:
//
//	Tamanho de fonte padrão para o texto do rótulo, em unidades sensíveis à
//	densidade.
var FontSize = rulesDensity.Density(20)

// Width
//
// English:
//
//	Default width of the icon container, in density-aware units.
//
// Português:
//
//	Largura padrão do contêiner do ícone, em unidades sensíveis à densidade.
var Width = rulesDensity.Density(200)

// Height
//
// English:
//
//	Default height of the icon container, in density-aware units.
//
// Português:
//
//	Altura padrão do contêiner do ícone, em unidades sensíveis à densidade.
var Height = rulesDensity.Density(200)

// SizeRatio
//
// English:
//
//	Default size ratio applied to the icon relative to its container.
//
// Português:
//
//	Proporção de tamanho padrão aplicada ao ícone em relação ao seu contêiner.
var SizeRatio = rulesDensity.Density(0.5)

// FilterIcon
//
// English:
//
//	SVG filter applied to icons, providing a blur effect with alpha blending.
//
// Português:
//
//	Filtro SVG aplicado aos ícones, fornecendo um efeito de desfoque com
//	mistura alfa.
var FilterIcon *html.TagSvgFilter

// FilterText
//
// English:
//
//	SVG filter applied to label text, providing a subtle blur effect with
//	alpha blending.
//
// Português:
//
//	Filtro SVG aplicado ao texto do rótulo, fornecendo um efeito sutil de
//	desfoque com mistura alfa.
var FilterText *html.TagSvgFilter

// IconX
//
// English:
//
//	Default horizontal position of the icon inside its container, in
//	density-aware units.
//
// Português:
//
//	Posição horizontal padrão do ícone dentro do seu contêiner, em unidades
//	sensíveis à densidade.
var IconX = rulesDensity.Density(60)

// IconY
//
// English:
//
//	Default vertical position of the icon inside its container, in
//	density-aware units.
//
// Português:
//
//	Posição vertical padrão do ícone dentro do seu contêiner, em unidades
//	sensíveis à densidade.
var IconY = rulesDensity.Density(40)

// IconWidth
//
// English:
//
//	Default width of the icon, in density-aware units.
//
// Português:
//
//	Largura padrão do ícone, em unidades sensíveis à densidade.
var IconWidth = rulesDensity.Density(160)

// IconHeight
//
// English:
//
//	Default height of the icon, in density-aware units.
//
// Português:
//
//	Altura padrão do ícone, em unidades sensíveis à densidade.
var IconHeight = rulesDensity.Density(160)

// init
//
// English:
//
//	Initializes the SVG filters for icons and text. FilterIcon applies a
//	Gaussian blur with standard deviation of 5 using the stroke paint as
//	input, blended with the source alpha. FilterText applies a subtle
//	Gaussian blur with standard deviation of 0.5 using the fill paint as
//	input, blended with the source alpha.
//
// Português:
//
//	Inicializa os filtros SVG para ícones e texto. FilterIcon aplica um
//	desfoque gaussiano com desvio padrão de 5 usando o traço como entrada,
//	misturado com o alfa de origem. FilterText aplica um desfoque gaussiano
//	sutil com desvio padrão de 0.5 usando o preenchimento como entrada,
//	misturado com o alfa de origem.
func init() {
	FilterIcon = factoryBrowser.NewTagSvgFilter().Id("iconBlur").Append(
		//factoryBrowser.NewTagSvgFeOffset().Dx(1).Dy(1),
		factoryBrowser.NewTagSvgFeBlend().In2(html.KSvgIn2SourceAlpha),
		factoryBrowser.NewTagSvgFeGaussianBlur().StdDeviation(5).In(html.KSvgInStrokePaint),
	)
	FilterText = factoryBrowser.NewTagSvgFilter().Id("textBlur").Append(
		//factoryBrowser.NewTagSvgFeOffset().Dx(1).Dy(1),
		factoryBrowser.NewTagSvgFeBlend().In2(html.KSvgIn2SourceAlpha),
		factoryBrowser.NewTagSvgFeGaussianBlur().StdDeviation(0.5).In(html.KSvgInFillPaint),
	)
}

// DataVerifySystemIcon
//
// English:
//
//	Fills in default values for a system icon Data struct. If a field is at
//	its zero value, it is set to the corresponding package-level default.
//	Colors are assigned based on the Status field, following the system icon
//	theme rules:
//
//	 ColorIcon:       FillColor (normal), CategoryIconColorSelected (selected),
//	                  CategoryIconColorDisabled (disabled).
//	 ColorBorder:     BorderColor (default), BorderColorAttention2 (attention 2).
//	 ColorLabel:      TextColor (normal), TextColorSelected (selected),
//	                  TextColorDisabled (disabled), TextColorAttention2 (attention 2).
//	 ColorBackground: CategoryIconColor (default),
//	                  CategoryIconColorAttention1 (attention 1).
//
// Português:
//
//	Preenche valores padrão para uma struct Data de ícone de sistema. Se um
//	campo estiver no valor zero, ele é definido com o valor padrão
//	correspondente do pacote. As cores são atribuídas com base no campo
//	Status, seguindo as regras de tema de ícones de sistema:
//
//	 ColorIcon:       FillColor (normal), CategoryIconColorSelected (selecionado),
//	                  CategoryIconColorDisabled (desabilitado).
//	 ColorBorder:     BorderColor (padrão), BorderColorAttention2 (atenção 2).
//	 ColorLabel:      TextColor (normal), TextColorSelected (selecionado),
//	                  TextColorDisabled (desabilitado), TextColorAttention2 (atenção 2).
//	 ColorBackground: CategoryIconColor (padrão),
//	                  CategoryIconColorAttention1 (atenção 1).
func DataVerifySystemIcon(data Data) Data {
	if data.IconViewBox == nil {
		data.IconViewBox = []int{0, 0, 512, 512}
	}

	if data.X == 0 {
		data.X = IconX
	}

	if data.Y == 0 {
		data.Y = IconY
	}

	if data.Width == 0 {
		data.Width = IconWidth
	}

	if data.Height == 0 {
		data.Height = IconHeight
	}

	if data.LabelFontSize == 0 {
		data.LabelFontSize = FontSize
	}

	if data.LabelY == 0 {
		data.LabelY = TextY
	}

	// data.ColorIcon
	switch data.Status {
	case KPipeLineDisabled:
		if reflect.DeepEqual(data.ColorIcon, color.RGBA{}) {
			data.ColorIcon = CategoryIconColorDisabled
		}
	case KPipeLineSelected:
		if reflect.DeepEqual(data.ColorIcon, color.RGBA{}) {
			data.ColorIcon = CategoryIconColorSelected
		}
	default:
		if reflect.DeepEqual(data.ColorIcon, color.RGBA{}) {
			data.ColorIcon = FillColor
		}
	}

	// data.ColorBorder
	switch data.Status {
	case KPipeLineAttention2:
		if reflect.DeepEqual(data.ColorBorder, color.RGBA{}) {
			data.ColorBorder = BorderColorAttention2
		}
	default:
		if reflect.DeepEqual(data.ColorBorder, color.RGBA{}) {
			data.ColorBorder = BorderColor
		}
	}

	// data.ColorLabel
	switch data.Status {
	case KPipeLineDisabled:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = TextColorDisabled
		}
	case KPipeLineSelected:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = TextColorSelected
		}
	case KPipeLineAttention2:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = TextColorAttention2
		}
	default:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = TextColor
		}
	}

	// data.ColorBackground
	switch data.Status {
	case KPipeLineAttention1:
		if reflect.DeepEqual(data.ColorBackground, color.RGBA{}) {
			data.ColorBackground = CategoryIconColorAttention1
		}
	default:
		if reflect.DeepEqual(data.ColorBackground, color.RGBA{}) {
			data.ColorBackground = CategoryIconColor
		}
	}

	return data
}

// DataVerifyElementIcon
//
// English:
//
//	Fills in default values for an element icon Data struct. If a field is at
//	its zero value, it is set to the corresponding package-level default.
//	Colors are assigned based on the Status field, following the element icon
//	theme rules, which differ from system icons by using more specific
//	per-status background colors and red highlights for selected and
//	attention 2 states:
//
//	 ColorIcon:       FillColor (normal), CategoryIconColorSelected (selected),
//	                  CategoryIconColorDisabled (disabled).
//	 ColorBorder:     BorderColor (default), red (selected), red (attention 2).
//	 ColorLabel:      TextColor (normal), red (selected), red (attention 2),
//	                  TextColorDisabled (disabled).
//	 ColorBackground: Light blue (normal), light gray (disabled),
//	                  light red (selected), lighter red (attention 1),
//	                  CategoryIconColor (default).
//
// Português:
//
//	Preenche valores padrão para uma struct Data de ícone de elemento. Se um
//	campo estiver no valor zero, ele é definido com o valor padrão
//	correspondente do pacote. As cores são atribuídas com base no campo
//	Status, seguindo as regras de tema de ícones de elemento, que diferem
//	dos ícones de sistema por usar cores de fundo mais específicas por status
//	e destaques em vermelho para os estados selecionado e atenção 2:
//
//	 ColorIcon:       FillColor (normal), CategoryIconColorSelected (selecionado),
//	                  CategoryIconColorDisabled (desabilitado).
//	 ColorBorder:     BorderColor (padrão), vermelho (selecionado),
//	                  vermelho (atenção 2).
//	 ColorLabel:      TextColor (normal), vermelho (selecionado),
//	                  vermelho (atenção 2), TextColorDisabled (desabilitado).
//	 ColorBackground: Azul claro (normal), cinza claro (desabilitado),
//	                  vermelho claro (selecionado), vermelho mais claro (atenção 1),
//	                  CategoryIconColor (padrão).
func DataVerifyElementIcon(data Data) Data {
	if data.IconViewBox == nil {
		data.IconViewBox = []int{0, 0, 512, 512}
	}

	if data.X == 0 {
		data.X = IconX
	}

	if data.Y == 0 {
		data.Y = IconY
	}

	if data.Width == 0 {
		data.Width = IconWidth
	}

	if data.Height == 0 {
		data.Height = IconHeight
	}

	if data.LabelFontSize == 0 {
		data.LabelFontSize = FontSize
	}

	if data.LabelY == 0 {
		data.LabelY = TextY
	}

	// data.ColorIcon
	switch data.Status {
	case KPipeLineDisabled:
		if reflect.DeepEqual(data.ColorIcon, color.RGBA{}) {
			data.ColorIcon = CategoryIconColorDisabled
		}
	case KPipeLineSelected:
		if reflect.DeepEqual(data.ColorIcon, color.RGBA{}) {
			data.ColorIcon = CategoryIconColorSelected
		}
	default:
		if reflect.DeepEqual(data.ColorIcon, color.RGBA{}) {
			data.ColorIcon = FillColor
		}
	}

	// data.ColorBorder
	switch data.Status {
	case KPipeLineSelected:
		if reflect.DeepEqual(data.ColorBorder, color.RGBA{}) {
			data.ColorBorder = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		}
	case KPipeLineAttention2:
		if reflect.DeepEqual(data.ColorBorder, color.RGBA{}) {
			data.ColorBorder = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		}
	default:
		if reflect.DeepEqual(data.ColorBorder, color.RGBA{}) {
			data.ColorBorder = BorderColor
		}
	}

	// data.ColorLabel
	switch data.Status {
	case KPipeLineDisabled:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = TextColorDisabled
		}
	case KPipeLineSelected:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		}
	case KPipeLineAttention2:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		}
	default:
		if reflect.DeepEqual(data.ColorLabel, color.RGBA{}) {
			data.ColorLabel = TextColor
		}
	}

	// data.ColorBackground
	switch data.Status {
	case KPipeLineNormal:
		if reflect.DeepEqual(data.ColorBackground, color.RGBA{}) {
			data.ColorBackground = color.RGBA{R: 220, G: 220, B: 255, A: 255}
		}
	case KPipeLineDisabled:
		if reflect.DeepEqual(data.ColorBackground, color.RGBA{}) {
			data.ColorBackground = color.RGBA{R: 230, G: 230, B: 230, A: 255}
		}
	case KPipeLineSelected:
		if reflect.DeepEqual(data.ColorBackground, color.RGBA{}) {
			data.ColorBackground = color.RGBA{R: 255, G: 220, B: 220, A: 255}
		}
	case KPipeLineAttention1:
		if reflect.DeepEqual(data.ColorBackground, color.RGBA{}) {
			data.ColorBackground = color.RGBA{R: 255, G: 180, B: 180, A: 255}
		}
	default:
		if reflect.DeepEqual(data.ColorBackground, color.RGBA{}) {
			data.ColorBackground = CategoryIconColor
		}
	}

	return data
}
