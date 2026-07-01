// main.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package main

// main.go — IDE entry point.
//
// English:
//
//	The main package only wires packages together and runs startup sequence.
//	All logic lives in dedicated packages:
//	  - stageViewManager: tab/split management
//	  - stageWorkspace: individual workspace lifecycle
//	  - factoryDevice: device creation
//	  - ui/mainMenu: menu hierarchy and rendering
//	  - translate: i18n loading from server
//
//	The workspaceMode variable controls which workspaces are displayed:
//	  - WorkspaceModeBoth: frontend + backend with tab bar (default)
//	  - WorkspaceModeFrontendOnly: only frontend, full screen
//	  - WorkspaceModeBackendOnly: only backend, full screen
//
// Português:
//
//	O package main apenas conecta packages e executa a sequência de startup.
//	Toda lógica vive em packages dedicados.
//
//	A variável workspaceMode controla quais workspaces são exibidos:
//	  - WorkspaceModeBoth: frontend + backend com barra de abas (padrão)
//	  - WorkspaceModeFrontendOnly: apenas frontend, tela inteira
//	  - WorkspaceModeBackendOnly: apenas backend, tela inteira

import (
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/hexagon"
	"github.com/helmutkemper/iotmakerio/live"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/splashScreen"
	"github.com/helmutkemper/iotmakerio/sprite"
	"github.com/helmutkemper/iotmakerio/stageViewManager"
	"github.com/helmutkemper/iotmakerio/stageWorkspace"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/mainMenu"
	"github.com/helmutkemper/iotmakerio/ui/overlay"
	"github.com/helmutkemper/iotmakerio/utilsText"
	"github.com/helmutkemper/iotmakerio/utilsWindow"
	"github.com/helmutkemper/iotmakerio/welcomeModal"
)

func main() {
	utilsWindow.InjectBodyNoMargin()
	utilsText.InjectFontAwesomeCSS()

	screenWidth, screenHeight := utilsWindow.GetScreenSize()

	// =====================================================================
	// Workspace mode configuration.
	//
	// Change this value to control which workspaces are displayed:
	//   stageViewManager.WorkspaceModeBoth          — frontend + backend (default)
	//   stageViewManager.WorkspaceModeFrontendOnly   — only frontend
	//   stageViewManager.WorkspaceModeBackendOnly    — only backend
	//
	// Português: Mude este valor para controlar quais workspaces são exibidos.
	// =====================================================================
	workspaceMode := stageViewManager.WorkspaceModeBoth

	// =====================================================================
	// Menu placement configuration.
	//
	// Controls where hex menus open on the canvas for ALL devices and the
	// main menu button:
	//   mainMenu.PlacementCentered  — center of screen (best for touch/tablet)
	//   mainMenu.PlacementAtCursor  — near the click position (best for mouse)
	//
	// Português: Controla onde os hex menus abrem no canvas para TODOS os
	// devices e o botão do menu principal.
	// =====================================================================
	mainMenu.Placement = mainMenu.PlacementCentered

	// =====================================================================
	// Menu backdrop opacity.
	//
	// Controls how much the canvas dims when a hex menu is open:
	//   0.0  — no dimming (legacy behavior, just catches click-outside)
	//   0.3  — subtle dimming
	//   0.5  — moderate dimming (good for focus)
	//   0.8  — strong dimming
	//
	// Português: Controla o escurecimento do canvas quando um hex menu está aberto.
	// =====================================================================
	mainMenu.BackdropOpacity = 0.5

	// =====================================================================
	// Device placement mode.
	//
	// Controls how new devices appear on the canvas:
	//   mainMenu.DevicePlaceImmediate — create at last click or screen center
	//   mainMenu.DevicePlaceOnClick   — ghost cursor follows pointer, click to place
	//
	// Português: Controla como novos devices aparecem no canvas.
	// =====================================================================
	mainMenu.DevicePlacement = mainMenu.DevicePlaceOnClick

	// =====================================================================
	// Editor library URLs.
	//
	// By default, Monaco and Marked load from CDN. To serve locally:
	//   1. Download: npm install monaco-editor@0.45.0 marked@11.1.1
	//   2. Copy files to server: static/monaco/ and static/marked/
	//   3. Register server route: mux.Handle("/static/", handler.NewStaticHandler("static"))
	//   4. Uncomment the lines below:
	//
	// Português: Por padrão, Monaco e Marked carregam de CDN.
	// Para servir localmente, siga os passos acima.
	// =====================================================================
	// overlay.MonacoLoaderURL = "http://localhost:8080/static/monaco/vs/loader.js"
	// overlay.MonacoBaseURL   = "http://localhost:8080/static/monaco/vs"
	// overlay.MarkedURL       = "http://localhost:8080/static/marked/marked.min.js"

	// --- Splash screen ---
	splashCanvasW := rulesDensity.Density(screenWidth).GetInt()
	splashCanvasH := rulesDensity.Density(screenHeight).GetInt()

	doc := js.Global().Get("document")
	splashCanvas := doc.Call("createElement", "canvas")
	splashCanvas.Set("id", "splashCanvas")
	splashCanvas.Set("width", splashCanvasW)
	splashCanvas.Set("height", splashCanvasH)
	splashCanvas.Get("style").Set("position", "absolute")
	splashCanvas.Get("style").Set("top", "0")
	splashCanvas.Get("style").Set("left", "0")
	splashCanvas.Get("style").Set("zIndex", "9999")
	doc.Get("body").Call("appendChild", splashCanvas)

	splashStage, splashErr := sprite.NewStage(sprite.StageConfig{
		CanvasID: "splashCanvas",
		Width:    splashCanvasW,
		Height:   splashCanvasH,
	})
	if splashErr != nil {
		log.Printf("splash NewStage: %v", splashErr)
	}
	if splashErr == nil {
		_ = splashStage.Start()
	}

	splash := splashScreen.New(splashScreen.Config{
		ImagePath:  "./splashScreen/splashScreen.png",
		FontFamily: "Verdana",
		FontSize:   20,
		TextColor:  "white",
		Border:     50,
		TextBox: splashScreen.TextBoxRatio{
			X: 0.2, Y: 0.1, Width: 0.6, Height: 0.15,
		},
		FadeDurationMs: 500,
	})
	if err := splash.Show(splashStage); err != nil {
		log.Printf("splash.Show: %v", err)
	}

	_ = splash.AddText("Loading translation")
	translate.Load()
	_ = splash.AddText("Translation loaded")

	// Preload external libraries during splash (non-blocking parallel load).
	// These are large downloads; loading them now avoids lag when the user
	// first opens an Inspect panel.
	//
	// Português: Pré-carrega bibliotecas externas durante o splash.
	// São downloads grandes; carregar agora evita lag quando o usuário
	// abre o painel Inspect pela primeira vez.
	_ = splash.AddText("Loading code editor")
	if err := overlay.PreloadMonaco(); err != nil {
		log.Printf("[Splash] Monaco preload: %v", err)
	}
	_ = splash.AddText("Code editor ready")

	_ = splash.AddText("Loading markdown renderer")
	if err := overlay.PreloadMarked(); err != nil {
		log.Printf("[Splash] Marked preload: %v", err)
	}
	_ = splash.AddText("Markdown renderer ready")

	_ = splash.AddText("Loading code highlighter")
	if err := overlay.PreloadHighlight(); err != nil {
		log.Printf("[Splash] Highlight preload: %v", err)
	}
	_ = splash.AddText("Code highlighter ready")

	// ── Splash done ─────────────────────────────────────────────────────
	//
	// Everything essential to the IDE has finished loading. The splash
	// served its purpose — fade it out before showing the welcome modal
	// so the screen transitions splash → modal cleanly rather than
	// stacking the modal on top of a still-visible splash.
	//
	// The splash itself fades over its configured FadeDurationMs
	// (currently 500 ms). Hide() blocks until the animation completes,
	// so by the time we reach the next line the canvas is empty.
	//
	// Português: Splash some antes do welcome modal. Função do splash
	// é cobrir o carregamento — terminou o carregamento, splash sai.
	if err := splash.Hide(); err != nil {
		log.Printf("splash.Hide: %v", err)
	}
	doc.Get("body").Call("removeChild", splashCanvas)

	// ── Welcome modal ───────────────────────────────────────────────────
	//
	// Block here until the maker either picks a language for a new
	// project or picks an existing project from the recent list.
	// Splash is already gone by this point; the modal renders on an
	// empty body which the modal's own dark overlay fills.
	//
	// The Result struct carries everything downstream needs:
	//   - Language: always set; flows into sharedCfg.Language.
	//   - Mode: tells us whether this is a fresh project (ModeNew),
	//     an open-existing (ModeOpen, with FileID/FileName), or a
	//     backup restore (ModeRestore — Parcela 2c).
	//
	// X / ESC dismissal returns Result{Mode: ModeNew, Language: "c"} —
	// behaves exactly like clicking the C99 card.
	//
	// Português: Bloqueia esperando o usuário escolher. Result tem
	// Language (sempre), Mode (new/open/restore) e FileID/FileName
	// quando aplicável. Fechar = C99 novo.
	welcomeResult := welcomeModal.Show()
	projectLanguage := welcomeResult.Language

	size := rulesDensity.Density(3)
	hex := new(hexagon.Hexagon)
	hex.Init(0, 0, size)

	// --- View Manager (two workspaces + tab bar) ---
	vm := stageViewManager.NewViewManager(
		rulesDensity.Density(screenWidth).GetInt(),
		rulesDensity.Density(screenHeight).GetInt(),
		workspaceMode,
	)

	// ── Live communication client ─────────────────────────────────────────
	// Created before vm.Init so the LiveConfigFn callback can be passed
	// into the workspace config. SceneMgr and SendFunc are wired after Init.
	liveClient := live.NewClient(nil)

	sharedCfg := stageWorkspace.Config{
		ResizeButton:  stageWorkspace.NewSharedResizeButton(),
		DraggerButton: stageWorkspace.NewSharedDraggerButton(),
		GridAdjust:    hex,
		LiveConfigFn:  func() { liveClient.ShowConfigDialog() },
		Language:      projectLanguage,
	}

	if err := vm.Init(sharedCfg); err != nil {
		log.Printf("ViewManager.Init: %v", err)
	}

	// ── Open existing project or restore backup ───────────────────────
	//
	// After vm.Init the workspace exists and is ready to receive a
	// scene. We branch on the welcome modal's Result.Mode:
	//
	//   - ModeOpen: load the chosen project and place it in the
	//     workspace; Ctrl+S writes back to that same file.
	//
	//   - ModeRestore: load the backup row's scene into the workspace
	//     and point currentFile at the ORIGINAL (not the backup).
	//     Ctrl+S then overwrites the original, and the existing
	//     OnAfterSave cleanup deletes the backup row. The user
	//     experiences "I clicked Restore, my work is back, my
	//     project name is unchanged".
	//
	//   - ModeNew / dismissal: skip — a fresh empty workspace is the
	//     correct state. The auto-restore that used to run from
	//     Workspace.Init was removed in Parcela 2c precisely because
	//     this branch is the single source of truth for restore now.
	//
	// Both paths run in a goroutine because LoadFile is a blocking
	// network call; main goroutine continues straight to live-client
	// wiring below, which does not depend on the scene being loaded.
	//
	// Português: Branch por Mode. Open carrega projeto normal;
	// Restore carrega backup e aponta currentFile pro ORIGINAL pra
	// Ctrl+S sobrescrever o original. Goroutine pra não bloquear o
	// resto do bootstrap.
	switch welcomeResult.Mode {
	case welcomeModal.ModeOpen:
		go func() {
			if err := vm.OpenFile(welcomeResult.FileID); err != nil {
				log.Printf("welcome: open project %q failed: %v",
					welcomeResult.FileName, err)
			}
		}()
	case welcomeModal.ModeRestore:
		go func() {
			if err := vm.RestoreBackup(welcomeResult.FileID); err != nil {
				log.Printf("welcome: restore backup %q failed: %v",
					welcomeResult.FileName, err)
			}
		}()
	}

	// Wire live client to the backend workspace (SceneMgr + factory SendFunc).
	if vm.HasBackend() {
		liveClient.SetSceneMgr(vm.Backend.SceneMgr)
		if vm.Backend.Factory != nil {
			vm.Backend.Factory.LiveSendFunc = liveClient.Send
		}
		if vm.HasFrontend() && vm.Frontend.Factory != nil {
			vm.Frontend.Factory.LiveSendFunc = liveClient.Send
		}
		// Live connection is NOT started automatically. The user must open
		// Settings → enter project ID → create API keys → click Connect.
		// This avoids unnecessary WebSocket load for users who don't use
		// live communication.
	}

	// Splash and welcome modal both ran earlier in this function, so
	// there is nothing more to dismiss here — the workspace is now
	// the only thing on screen. The "Workspaces ready" text and the
	// trailing splash.Hide / removeChild that used to live here were
	// moved upstream (before the welcome modal) once the splash's
	// role was scoped strictly to "cover loading time".

	// --- Keyboard: 'E' to export active workspace scene ---
	// todo: tirar isto daqui e colocar em uma máquina de status para teclado
	doc.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			key := args[0].Get("key").String()
			if key == "e" || key == "E" {
				tagName := args[0].Get("target").Get("tagName").String()
				if tagName != "INPUT" && tagName != "TEXTAREA" {
					log.Printf("[SCENE] Manual export triggered")
					if vm.HasFrontend() {
						vm.Frontend.SceneMgr.NotifyChange()
					}
					if vm.HasBackend() {
						vm.Backend.SceneMgr.NotifyChange()
					}
				}
			}
			return nil
		}),
	)

	// Default: backend tab (programming view).
	// todo: em examples/ide/stageWorkspace/workspace.go
	// w.Stage.SetCamera(w.CameraBackend) define a câmera
	// tem que fazer a função
	vm.SetActiveTab("backend")

	done := make(chan struct{})
	<-done
}
