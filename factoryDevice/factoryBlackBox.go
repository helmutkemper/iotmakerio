// factoryDevice/factoryBlackBox.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryDevice

// factoryBlackBox.go — Black-box device creation methods for DeviceFactory.
//
// English:
//
//	CreateBlackBoxInit and CreateBlackBoxRun create individual devices.
//	Both are called from the menu independently — the user chooses which
//	function to place from the Hardware submenu.
//
//	instanceId sharing:
//	  Init and Run of the same component MUST share the same instanceId so the
//	  codegen creates a single struct variable for both. Since they are placed
//	  separately (independent menu clicks), the factory keeps a cache of
//	  struct name → instanceId. When an Init is placed, its id is cached. When
//	  the matching Run is placed, it reuses that id. If Run is placed first (a
//	  pure-Run device), a new id is generated and cached for a later Init too.
//
//	Why a cache instead of scanning the scene?
//	  The scene serializer's device list is private. A factory-local cache is
//	  simpler, avoids coupling scene ↔ factory, and is correct for the common
//	  case (one instance per component per workspace).
//
// Português:
//
//	Init e Run do mesmo componente devem compartilhar o instanceId.
//	O cache bbInstanceCache do factory garante que o ID gerado pelo Init é
//	reutilizado pelo Run, independentemente da ordem de colocação.

import (
	"log"
	"strings"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/devices/compBlackBox"
	"github.com/helmutkemper/iotmakerio/rulesSequentialId"
)

// factoryBlackBox.go — Black-box device creation methods for DeviceFactory.
//
// English:
//
//	CreateBlackBoxInit and CreateBlackBoxMethod create individual visual blocks.
//	Both are called from the menu independently — the user chooses which
//	function to place from the Hardware submenu.
//
//	instanceId sharing:
//	  All blocks for the same component instance (Init + all named methods)
//	  MUST share the same instanceId so the codegen creates a single struct
//	  variable for all of them. The factory keeps a cache of struct name →
//	  instanceId. When the first block is placed, an id is generated and
//	  cached. Subsequent placements of any block of the same component reuse
//	  that id.
//
// Português:
//
//	Todos os blocos da mesma instância de componente compartilham o instanceId.
//	O cache bbInstanceCache garante que o ID é reutilizado entre Init e todos
//	os métodos nomeados, independente da ordem de colocação.

// bbInstanceId returns the shared instanceId for the given struct name.
func (f *DeviceFactory) bbInstanceId(structName string) string {
	if f.bbInstanceCache == nil {
		f.bbInstanceCache = make(map[string]string)
	}
	key := strings.ToLower(structName)
	if id, ok := f.bbInstanceCache[key]; ok {
		return id
	}
	id := rulesSequentialId.GetIdFromBase(key)
	f.bbInstanceCache[key] = id
	return id
}

// CreateBlackBoxInit creates the Init device for the given black-box definition.
func (f *DeviceFactory) CreateBlackBoxInit(def *blackbox.BlackBoxDefClient) {
	if !def.HasInit() {
		log.Printf("[Factory] %s has no Init() — cannot create Init device", def.Name)
		return
	}
	f.createBlackBoxInit(def, f.bbInstanceId(def.Name))
}

// CreateBlackBoxMethod creates a visual block for one named method
// (Run, Log, Step, …) of the given black-box definition.
//
// methodName must match the Name field of one of the entries in def.Methods.
// If the method is not found, the call is logged and ignored.
//
// Português: Cria um bloco visual para um método nomeado (Run, Log, Step, …).
// methodName deve corresponder ao campo Name de uma entrada em def.Methods.
func (f *DeviceFactory) CreateBlackBoxMethod(def *blackbox.BlackBoxDefClient, methodName string) {
	method := def.GetMethod(methodName)
	if method == nil {
		log.Printf("[Factory] %s has no method %q — cannot create method device", def.Name, methodName)
		return
	}
	f.createBlackBoxMethod(def, method, f.bbInstanceId(def.Name), "")
}

func (f *DeviceFactory) createBlackBoxInit(def *blackbox.BlackBoxDefClient, instanceId string) {
	stm := new(compBlackBox.StatementBlackBoxInit)
	stm.SetStage(f.Stage)
	// Delivery B: linear context menu replaces hex — BlackBox has no tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetDef(def)
	stm.SetInstanceId(instanceId)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] BlackBoxInit(%s).Init: %v", def.Name, err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	f.wireInspectDblClick(stm)
	log.Printf("[Factory] Created BlackBoxInit:%s (instance: %s) at (%v, %v)", def.Name, instanceId, cx, cy)
}

func (f *DeviceFactory) createBlackBoxMethod(def *blackbox.BlackBoxDefClient, method *blackbox.MethodDefClient, instanceId string, callbackRefFn string) {
	stm := new(compBlackBox.StatementBlackBoxMethod)
	// callbackRefFn non-empty makes this the C99 callback REFERENCE variant (the
	// "ƒ" device): it only changes GetDeviceType (→ "CallbackRef:<fn>"). It must
	// be set BEFORE Init/RegisterConnectors/Register so the scene records the
	// right device type. Empty for the normal callable method/function block.
	if callbackRefFn != "" {
		stm.SetCallbackRef(callbackRefFn)
	}
	stm.SetStage(f.Stage)
	// Delivery B: linear context menu replaces hex — BlackBox has no tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetDef(def)
	stm.SetMethod(method)
	stm.SetInstanceId(instanceId)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] BlackBoxMethod(%s.%s).Init: %v", def.Name, method.Name, err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	f.wireInspectDblClick(stm)
	log.Printf("[Factory] Created %s (instance: %s) at (%v, %v)", stm.GetDeviceType(), instanceId, cx, cy)
}
