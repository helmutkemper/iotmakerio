package sprite

// NewStage
//
// English:
//
//	Creates a new Stage bound to the HTML canvas element identified by the given
//	configuration. The Stage is created in a stopped state — call Start() to begin
//	the render loop and enable event handling.
//
//	The canvas element must already exist in the DOM when Start() is called, but
//	does not need to exist when NewStage() is called.
//
//	  stage, err := sprite.NewStage(sprite.StageConfig{
//	      CanvasID: "myCanvas",
//	      Width:    800,
//	      Height:   600,
//	  })
//	  if err != nil {
//	      log.Fatal(err)
//	  }
//
//	  element, err := stage.CreateElement(sprite.ElementConfig{
//	      ID:     "icon1",
//	      X:      100,
//	      Y:      50,
//	      Index:  1,
//	      SvgXml: "<svg>...</svg>",
//	      DragEnable: true,
//	  })
//	  if err != nil {
//	      log.Fatal(err)
//	  }
//
//	  err = stage.Start()
//
// Português:
//
//	Cria um novo Stage vinculado ao elemento HTML canvas identificado pela configuração
//	fornecida. O Stage é criado em estado parado — chame Start() para iniciar o loop
//	de renderização e habilitar o tratamento de eventos.
//
//	O elemento canvas deve existir no DOM quando Start() for chamado, mas não precisa
//	existir quando NewStage() for chamado.
func NewStage(config StageConfig) (stage Stage, err error) {
	if config.CanvasID == "" {
		err = ErrCanvasNotFound
		return
	}

	stage = newStage(config)
	return
}
