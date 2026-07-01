// wire/interationGuide.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

// =====================================================================
//  Integration Guide | Guia de Integração
// =====================================================================
//
// This file documents how to integrate the wire package into the existing
// IDE codebase. It is NOT compiled code — it's a reference guide with
// code examples.
//
// Português:
//
//   Este arquivo documenta como integrar o pacote wire no codebase existente
//   da IDE. NÃO é código compilável — é um guia de referência com exemplos.
//
// =====================================================================
//
// STEP 1: Create the wire.Manager in main.go (alongside spriteStage)
//
//   // In main.go, after creating spriteStage:
//
//   wireMgr := wire.NewManager()
//   wireMgr.SetLayer(wire.WireLayerAbove) // default, user can toggle
//
// =====================================================================
//
// STEP 2: Hook into the Stage render callback
//
//   // The render callback is called AFTER all elements are drawn.
//   // For WireLayerAbove, this is perfect.
//   // For WireLayerBelow, we need a pre-render hook (see note below).
//
//   spriteStage.SetRenderCallback(func() {
//       if wireMgr.GetLayer() == wire.WireLayerAbove {
//           wireMgr.Draw()
//       }
//   })
//
//   // NOTE: For WireLayerBelow, you would need to add a pre-render callback
//   // to the sprite.Stage interface. For now, WireLayerAbove works with the
//   // existing SetRenderCallback.
//
//   // After Start(), set the render context:
//   // (The ctx is the 2D context from the canvas — you may need to expose it
//   //  from the sprite.Stage or obtain it from the DOM directly)
//
//   spriteCanvas := js.Global().Get("document").Call("getElementById", "spriteCanvas")
//   spriteCtx := spriteCanvas.Call("getContext", "2d")
//   wireMgr.SetRenderContext(spriteCtx)
//   wireMgr.MarkDirtyFunc = func() { spriteStage.MarkDirty() }
//
// =====================================================================
//
// STEP 3: Register connectors in StatementAdd.Init()
//
//   // In StatementAdd.Init(), after creating the sprite.Element, register
//   // all connectors with the wire manager:
//
//   func (e *StatementAdd) RegisterConnectors(mgr *wire.Manager) {
//       // inputX: input, type int, position relative to element
//       mgr.RegisterConnector(wire.ConnectorInfo{
//           ID:                 wire.ConnectorID{ElementID: e.id, PortName: "inputX"},
//           IsOutput:           false,
//           AllowedTypes:       []string{"int"},
//           AcceptNotConnected: false,
//           Locked:             false,
//           MaxConnections:     1,
//           Label:              "Input X",
//           PositionFunc: func() (float64, float64) {
//               ex, ey := e.elem.GetPosition()
//               return ex + 2, ey + 15  // hardcoded offset
//           },
//       })
//
//       // inputY: input, type int, position relative to element
//       mgr.RegisterConnector(wire.ConnectorInfo{
//           ID:                 wire.ConnectorID{ElementID: e.id, PortName: "inputY"},
//           IsOutput:           false,
//           AllowedTypes:       []string{"int"},
//           AcceptNotConnected: false,
//           Locked:             false,
//           MaxConnections:     1,
//           Label:              "Input Y",
//           PositionFunc: func() (float64, float64) {
//               ex, ey := e.elem.GetPosition()
//               _, h := e.elem.GetSize()
//               return ex + 2, ey + h - 18  // hardcoded offset
//           },
//       })
//
//       // output: output, type int, unlimited connections
//       mgr.RegisterConnector(wire.ConnectorInfo{
//           ID:                 wire.ConnectorID{ElementID: e.id, PortName: "output"},
//           IsOutput:           true,
//           AllowedTypes:       []string{"int"},
//           AcceptNotConnected: true,
//           Locked:             false,
//           MaxConnections:     0,  // unlimited
//           Label:              "Output",
//           PositionFunc: func() (float64, float64) {
//               ex, ey := e.elem.GetPosition()
//               w, h := e.elem.GetSize()
//               return ex + w - 12, ey + h/2 - 2  // hardcoded offset
//           },
//       })
//   }
//
// =====================================================================
//
// STEP 4: Modify hexMenu "Connect" action to use wire.Manager
//
//   // In getInputXMenuItems() (and similar), replace the log.Printf with:
//
//   OnClick: func() {
//       connID := wire.ConnectorID{ElementID: e.id, PortName: "inputX"}
//       candidates := e.wireMgr.StartConnect(connID)
//       if len(candidates) == 0 {
//           log.Printf("No compatible connections available")
//           return
//       }
//
//       // Build menu items from candidates and show selection menu.
//       // Each candidate becomes a hexMenu item:
//       items := make([]hexMenu.MenuItem, 0, len(candidates))
//       for i, c := range candidates {
//           candidate := c // capture for closure
//           items = append(items, hexMenu.MenuItem{
//               ID:    fmt.Sprintf("target_%d", i),
//               Col:   1,
//               Row:   i*2 + 1,
//               Label: candidate.Label,
//               // FontAwesomePath: choose icon based on candidate type
//               Type:  hexMenu.ItemAction,
//               OnClick: func() {
//                   w, err := e.wireMgr.FinishConnect(candidate.Connector.ID)
//                   if err != nil {
//                       log.Printf("Connect failed: %v", err)
//                       return
//                   }
//                   log.Printf("Connected: %v → %v (type: %v)",
//                       w.From, w.To, w.DataType)
//               },
//               Styles: hexMenu.DefaultStyles(),
//           })
//       }
//
//       // Show the candidates as a submenu
//       menuX, menuY := ... // position from the click event
//       go e.hexMenu.Open(items, menuX, menuY)
//   },
//
// =====================================================================
//
// STEP 5: Modify hexMenu "Disconnect" action
//
//   OnClick: func() {
//       connID := wire.ConnectorID{ElementID: e.id, PortName: "inputX"}
//       count := e.wireMgr.DisconnectConnector(connID)
//       log.Printf("Disconnected %d wires from %v", count, connID)
//   },
//
// =====================================================================
//
// STEP 6: Wire hit-testing for click-to-select/delete
//
//   // In the Stage's OnClickStage (click on empty area):
//   spriteStage.SetOnClickStage(func(event sprite.PointerEvent) {
//       // Check if a wire was clicked
//       w := wireMgr.HitTest(event.CanvasX, event.CanvasY)
//       if w != nil {
//           if w.Selected {
//               // Already selected — delete it
//               wireMgr.DeleteWire(w.ID)
//           } else {
//               // Select it (shows highlight)
//               wireMgr.SelectWire(w.ID)
//           }
//           return
//       }
//
//       // No wire hit — deselect all
//       wireMgr.DeselectAll()
//   })
//
// =====================================================================
//
// STEP 7: Recalculate wires when elements move
//
//   // In StatementAdd.wireEvents(), add to SetOnDragMove or SetOnDragEnd:
//
//   e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
//       // ... existing grid snap code ...
//       e.wireMgr.RecalculateForElement(e.id)
//   })
//
//   // Also in SetOnResizeEnd:
//   e.elem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
//       // ... existing resize code ...
//       e.wireMgr.RecalculateForElement(e.id)
//   })
//
// =====================================================================
//
// STEP 8: Validation (compilation check)
//
//   // When the user "compiles" or "runs" the visual program:
//   errors := wireMgr.ValidateConnections()
//   for _, connID := range errors {
//       log.Printf("Missing required connection: %v", connID)
//       // Show warning on the component:
//       // Find the element and call SetWarning(true)
//   }
//
// =====================================================================
//
// STEP 9: Toggle wire layer (user preference)
//
//   // Add a button or menu option to toggle:
//   func toggleWireLayer() {
//       if wireMgr.GetLayer() == wire.WireLayerAbove {
//           wireMgr.SetLayer(wire.WireLayerBelow)
//       } else {
//           wireMgr.SetLayer(wire.WireLayerAbove)
//       }
//       spriteStage.MarkDirty()
//   }
//
// =====================================================================
