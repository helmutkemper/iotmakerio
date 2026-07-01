// /ide/factoryDevice/factory_patch_instructions.md
//
// This file contains the two additions needed in factory.go.
// Apply them manually to the factory.go file you already have from the
// bug fix (the one with dualStages()).

// =====================================================================
// ADDITION 1: New method — add before the commented-out CreateCompare()
// =====================================================================

// CreateBackgroundImage creates a dual device: backend config node +
// frontend background image layer. Uses dualStages() to correctly resolve
// which stage is backend vs frontend.
func (f *DeviceFactory) CreateBackgroundImage() {
backendStg, frontendStg := f.dualStages()

	stm := new(compFrontend.StatementBackgroundImage)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetHexMenu(f.HexMenu)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementBackgroundImage.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	stm.SetOverlapPolicy(scene.OverlapPolicy{AllowAbove: false, AllowBelow: true, AllowPartial: false})
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementBackgroundImage at (%v, %v)", cx, cy)
}

// =====================================================================
// ADDITION 2: In CreateByType() switch, add this case alongside the
// other frontend components (StatementGauge, StatementLED, etc.):
// =====================================================================

	case "StatementBackgroundImage":
		f.CreateBackgroundImage()
