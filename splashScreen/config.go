package splashScreen

// TextBoxRatio
//
// English:
//
//	Defines the text area position and size as ratios (0.0–1.0) relative to the
//	splash image dimensions. For example, X=0.2 means the text box starts at 20%
//	of the image width from its left edge.
//
// Português:
//
//	Define a posição e tamanho da área de texto como proporções (0.0–1.0) relativas
//	às dimensões da imagem do splash. Por exemplo, X=0.2 significa que a caixa de
//	texto começa a 20% da largura da imagem a partir de sua borda esquerda.
type TextBoxRatio struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// Config
//
// English:
//
//	Configuration for creating a SplashScreen. All fields have sensible defaults.
//	Either ImagePath or SvgXml must be provided for the splash image — if both are
//	set, SvgXml takes priority.
//
// Português:
//
//	Configuração para criar um SplashScreen. Todos os campos possuem valores padrão
//	sensatos. ImagePath ou SvgXml deve ser fornecido para a imagem do splash — se
//	ambos forem definidos, SvgXml tem prioridade.
type Config struct {
	// ImagePath is the URL or relative path to the splash image (PNG, JPG, etc.).
	// Used when SvgXml is empty. The browser loads it relative to the page URL.
	//
	// Português: URL ou caminho relativo da imagem do splash (PNG, JPG, etc.).
	// Usado quando SvgXml está vazio. O navegador carrega relativo à URL da página.
	ImagePath string

	// SvgXml is raw SVG XML string for the splash image. Takes priority over ImagePath.
	//
	// Português: String XML SVG bruta para a imagem do splash. Tem prioridade sobre ImagePath.
	SvgXml string

	// FontFamily is the CSS font family for text. Default: "Verdana".
	//
	// Português: Família de fonte CSS para texto. Padrão: "Verdana".
	FontFamily string

	// FontSize is the font size in pixels. Default: 20.
	//
	// Português: Tamanho da fonte em pixels. Padrão: 20.
	FontSize int

	// FontWeight is the CSS font weight. Default: "normal".
	//
	// Português: Peso da fonte CSS. Padrão: "normal".
	FontWeight string

	// FontStyle is the CSS font style. Default: "normal".
	//
	// Português: Estilo da fonte CSS. Padrão: "normal".
	FontStyle string

	// TextColor is the CSS fill color for text. Default: "white".
	//
	// Português: Cor de preenchimento CSS do texto. Padrão: "white".
	TextColor string

	// OverlayColor is the CSS color for the full-screen overlay rectangle.
	// Supports any CSS color including rgba. Default: "rgba(0,0,0,0.86)".
	//
	// Português: Cor CSS do retângulo overlay de tela cheia.
	// Suporta qualquer cor CSS incluindo rgba. Padrão: "rgba(0,0,0,0.86)".
	OverlayColor string

	// TextBox defines the text area as ratios (0.0–1.0) relative to the splash image.
	// Default: {X: 0.2, Y: 0.1, Width: 0.6, Height: 0.15}.
	//
	// Português: Define a área de texto como proporções (0.0–1.0) relativas à imagem.
	// Padrão: {X: 0.2, Y: 0.1, Width: 0.6, Height: 0.15}.
	TextBox TextBoxRatio

	// TextPadding is extra vertical padding in pixels between text lines.
	// Default: 0.
	//
	// Português: Espaçamento vertical extra em pixels entre linhas de texto. Padrão: 0.
	TextPadding int

	// FadeDurationMs is the fade-out animation duration in milliseconds. Default: 1000.
	//
	// Português: Duração da animação de fade-out em milissegundos. Padrão: 1000.
	FadeDurationMs float64

	// Border is the margin in pixels from the canvas edges to the splash content.
	// Default: 50.
	//
	// Português: Margem em pixels das bordas do canvas ao conteúdo do splash. Padrão: 50.
	Border int

	// ZIndex is the base z-index for splash elements. The overlay uses ZIndex,
	// the image uses ZIndex+1, and the text uses ZIndex+2. Use a very high value
	// to ensure the splash appears above all application elements.
	// Default: 100000.
	//
	// Português: Z-index base para os elementos do splash. O overlay usa ZIndex,
	// a imagem usa ZIndex+1, e o texto usa ZIndex+2. Use um valor muito alto para
	// garantir que o splash apareça acima de todos os elementos da aplicação.
	// Padrão: 100000.
	ZIndex int

	// ElementPrefix is the prefix for sprite Element IDs created by this package.
	// Default: "splash".
	//
	// Português: Prefixo para os IDs de sprite Element criados por este package.
	// Padrão: "splash".
	ElementPrefix string
}

// applyDefaults
//
// English:
//
//	Returns a copy of the config with all zero-value fields replaced by sensible defaults.
//
// Português:
//
//	Retorna uma cópia da config com todos os campos zero-value substituídos por valores padrão.
func applyDefaults(cfg Config) (out Config) {
	out = cfg

	if out.FontFamily == "" {
		out.FontFamily = "Verdana"
	}
	if out.FontSize == 0 {
		out.FontSize = 20
	}
	if out.FontWeight == "" {
		out.FontWeight = "normal"
	}
	if out.FontStyle == "" {
		out.FontStyle = "normal"
	}
	if out.TextColor == "" {
		out.TextColor = "white"
	}
	if out.OverlayColor == "" {
		out.OverlayColor = "rgba(0,0,0,0.86)"
	}
	if out.TextBox == (TextBoxRatio{}) {
		out.TextBox = TextBoxRatio{
			X:      0.2,
			Y:      0.1,
			Width:  0.6,
			Height: 0.15,
		}
	}
	if out.FadeDurationMs == 0 {
		out.FadeDurationMs = 1000
	}
	if out.Border == 0 {
		out.Border = 50
	}
	if out.ZIndex == 0 {
		out.ZIndex = 100000
	}
	if out.ElementPrefix == "" {
		out.ElementPrefix = "splash"
	}

	return
}
