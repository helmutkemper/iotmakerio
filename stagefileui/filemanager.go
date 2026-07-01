// ide/stagefileui/filemanager.go — Stage file manager overlay for the WASM IDE.
//
// Opens a draggable overlay panel (same visual language as ui/overlay) that lets
// the maker save, load, rename, and delete stage files organised in virtual
// folders.
//
// Touch-friendly: all interactive elements have a minimum height of 44px,
// folder/file rows are generously padded, and buttons have large tap targets.
// This ensures the file manager works well on tablets (the primary non-desktop
// target for the IDE).
//
// Colour palette matches the Catppuccin Mocha theme used throughout the IDE:
//
//	colBase      #1e1e2e   window background
//	colMantle    #181825   sidebar / footer background
//	colSurface0  #313244   title bar, selected items
//	colSurface1  #45475a   borders, inactive elements
//	colText      #cdd6f4   primary text
//	colSubtext   #a6adc8   secondary text, timestamps
//	colBlue      #89b4fa   primary action (Save)
//	colGreen     #a6e3a1   success feedback
//	colRed       #f38ba8   delete, danger
//	colPeach     #fab387   highlighted file name, active accent
//
// Usage:
//
//	stagefileui.Show(stagefileui.Config{
//	    GetSceneJSON:  func() string { return sceneMgr.Export() },
//	    GetDeviceCount: func() int { return len(devices) },
//	    OnLoad:         func(sceneJSON string) { /* reconstruct canvas */ },
//	})
//
// Português:
//
//	Overlay de gerenciamento de arquivos de stage para a IDE WASM.
//	Abre um painel arrastável que permite salvar, carregar, renomear e deletar
//	arquivos de stage organizados em pastas virtuais. Touch-friendly para tablet.
package stagefileui

import (
	"fmt"
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/stagefileclient"
	"github.com/helmutkemper/iotmakerio/translate"
)

// ─── Catppuccin Mocha palette ─────────────────────────────────────────────────

const (
	colBase     = "#1e1e2e"
	colMantle   = "#181825"
	colSurface0 = "#313244"
	colSurface1 = "#45475a"
	colSurface2 = "#585b70"
	colText     = "#cdd6f4"
	colSubtext  = "#a6adc8"
	colBlue     = "#89b4fa"
	colGreen    = "#a6e3a1"
	colRed      = "#f38ba8"
	colPeach    = "#fab387"
)

// ─── FA icon helpers ──────────────────────────────────────────────────────────
//
// The file manager needs to drop FontAwesome glyphs into many spots:
// title bar, sidebar entries, toolbar buttons, file rows, and the
// edit dialog's icon picker. Manually writing the family/name pair
// every time is verbose and easy to drift. These helpers centralise
// the construction so every call site picks up the same defaults
// (resolved family, ARIA-hiding so screen readers don't read the
// glyph) and stays at one line.
//
// newFaIcon resolves the FA family automatically via faIconClass
// (defined in iconpicker.go) — that's the same brands→solid→regular
// priority used by the picker, so a name the maker chose in the
// picker survives the round-trip through the wire and renders here
// with the exact same glyph.
//
// newFaIconFixed is for icons whose family is known at compile time
// — e.g. "fa-solid" for the title bar's folder, where calling
// FA_FREE_STYLES would be wasted work. The two helpers share their
// return type so callers can swap one for the other without
// changing surrounding code.
//
// Português: Helpers pra criar elementos <i> com classes FA. O
// primeiro resolve a família via FA_FREE_STYLES (mesma lógica do
// picker); o segundo aceita a família como parâmetro pra ícones
// conhecidos em compile time.

// newFaIcon builds an <i> element for the given FA icon name,
// resolving the family from window.FA_FREE_STYLES.
func newFaIcon(doc js.Value, name string) js.Value {
	i := doc.Call("createElement", "i")
	i.Set("className", fmt.Sprintf("%s fa-%s", faIconClass(name), name))
	i.Call("setAttribute", "aria-hidden", "true")
	return i
}

// newFaIconFixed builds an <i> element with an explicit family
// ("fa-solid", "fa-regular", "fa-brands"). Use when the family is
// known at the call site — saves a global lookup.
func newFaIconFixed(doc js.Value, family, name string) js.Value {
	i := doc.Call("createElement", "i")
	i.Set("className", fmt.Sprintf("%s fa-%s", family, name))
	i.Call("setAttribute", "aria-hidden", "true")
	return i
}

// fileDefaultIcon returns the FA name the file manager renders when
// a file's IconID is empty. Kept as a named constant so a single
// source of truth covers the picker's default chip AND the row
// thumbnail — if these two ever disagreed, files would look one
// way in the listing and another way in the edit dialog.
const fileDefaultIcon = "cube"

// ─── Config ───────────────────────────────────────────────────────────────────

// Config holds the callbacks that connect the file manager to the IDE workspace.
type Config struct {
	// GetSceneJSON returns the current scene JSON from the serializer.
	GetSceneJSON func() string

	// GetDeviceCount returns the number of devices currently on the canvas.
	GetDeviceCount func() int

	// OnLoad is called when the user opens a file. Receives the scene JSON.
	OnLoad func(sceneJSON string)

	// OnFileOpened is called after a file is opened or saved for the first time.
	// The workspace stores the ID and name to enable quick save (Ctrl+S).
	//
	// Português: Chamado quando um arquivo é aberto ou salvo pela primeira vez.
	// O workspace armazena ID e nome para possibilitar save rápido (Ctrl+S).
	OnFileOpened func(fileID, fileName string)

	// GetCurrentFileID returns the ID of the currently open file.
	// Empty string means no file is open (first save will show the dialog).
	GetCurrentFileID func() string

	// GetCurrentFileName returns the name of the currently open file.
	GetCurrentFileName func() string

	// GetProjectLanguage returns the language of the current project
	// ("c" or "go", from stagefileclient.StageFileLanguage*). Used
	// when creating new files so they inherit the workspace's
	// fixed language. May be nil during early bring-up of the
	// welcome modal feature — in that case the file manager passes
	// an empty string to the server, which resolves to the default
	// "c" (C99) via the schema's NOT NULL DEFAULT.
	//
	// Português: Retorna a linguagem do projeto atual. Usado ao
	// criar arquivos para que herdem a linguagem fixa do workspace.
	// Pode ser nil — server resolve string vazia para "c" via
	// default da coluna.
	GetProjectLanguage func() string

	// OnAfterSave is called after a successful manual save (Save button or
	// Ctrl+S). The workspace uses this to delete the backup file since the
	// original now has the latest state.
	//
	// Português: Chamado após um save manual bem-sucedido. O workspace usa
	// para apagar o arquivo de backup.
	OnAfterSave func()

	// OnImportImage is called when the user clicks "Import Image" in the
	// file manager. Opens a file picker for PNG with embedded stage data.
	//
	// Português: Chamado quando o usuário clica "Import Image".
	OnImportImage func()
}

// ─── Show ─────────────────────────────────────────────────────────────────────

// QuickSave saves the current scene to the currently open file without opening
// the file manager overlay. Returns true if the save was executed, false if
// no file is currently open (the caller should open the file manager instead).
//
// Must be called from a goroutine — it performs a blocking network request.
//
// Português: Salva a cena atual no arquivo aberto sem abrir o overlay.
// Retorna true se salvou, false se nenhum arquivo está aberto.
func QuickSave(cfg Config) bool {
	currentID := ""
	if cfg.GetCurrentFileID != nil {
		currentID = cfg.GetCurrentFileID()
	}
	if currentID == "" {
		return false
	}

	sceneJSON := "{}"
	deviceCount := 0
	if cfg.GetSceneJSON != nil {
		sceneJSON = cfg.GetSceneJSON()
	}
	if cfg.GetDeviceCount != nil {
		deviceCount = cfg.GetDeviceCount()
	}

	err := stagefileclient.UpdateFile(currentID, "", "", "", sceneJSON, "", deviceCount)
	if err != nil {
		log.Printf("[StageFileUI] QuickSave error: %v", err)
		return false
	}

	currentName := ""
	if cfg.GetCurrentFileName != nil {
		currentName = cfg.GetCurrentFileName()
	}
	log.Printf("[StageFileUI] QuickSave: %s (%s) saved", currentName, currentID)
	return true
}

// Show opens the stage file manager overlay.
// Must be called from a goroutine — it performs blocking network requests.
func Show(cfg Config) {
	doc := js.Global().Get("document")

	// ── State ─────────────────────────────────────────────────────────────
	var (
		folders    []stagefileclient.StageFolderEntry
		files      []stagefileclient.StageFileEntry
		limitInfo  *stagefileclient.LimitInfo
		currentDir string // empty = root / "all files"
		// kindFilter picks which stage files are rendered in the list.
		// Values: "all" (default), "stage", "tutorial". Maps to the
		// three tabs at the top of the file manager. Filtering happens
		// client-side — the API already delivers every file in one
		// call, so the tab switch is an instant re-render.
		//
		// Português: Filtro do tipo de arquivo mostrado na lista.
		// "all" (padrão), "stage", ou "tutorial".
		kindFilter = "all"
	)

	// Forward declarations for elements that get rebuilt on refresh.
	var fileListEl js.Value
	var folderListEl js.Value
	var breadcrumbEl js.Value
	var footerEl js.Value

	// Forward declaration for renderFileList — the tab filter bar
	// (built before the file list) captures this closure and calls
	// it when the user switches tabs. Declaring the variable here
	// and assigning the body later matches the pattern already used
	// by refreshAll below.
	//
	// Português: Forward declaration de renderFileList porque a
	// barra de tabs precisa do closure antes de ele ser definido.
	var renderFileList func()

	// Forward declaration for the refresh function — closures defined below
	// reference it before its body is assigned. Go closures capture by
	// reference, so the assignment can happen later; the declaration must not.
	var refreshAll func()

	// ── Backdrop ──────────────────────────────────────────────────────────
	backdrop := doc.Call("createElement", "div")
	backdrop.Get("style").Set("cssText",
		"position:fixed;top:0;left:0;width:100vw;height:100vh;"+
			"background:rgba(0,0,0,0.45);z-index:99999;"+
			"display:flex;align-items:center;justify-content:center;")

	// ── Close helper ─────────────────────────────────────────────────────
	closeFn := func() {
		if backdrop.Get("parentNode").Truthy() {
			doc.Get("body").Call("removeChild", backdrop)
		}
	}

	// ── Panel ────────────────────────────────────────────────────────────
	panel := doc.Call("createElement", "div")
	panel.Get("style").Set("cssText", fmt.Sprintf(
		"width:90vw;max-width:720px;height:80vh;max-height:520px;"+
			"background:%s;border:1px solid %s;border-radius:6px;"+
			"display:flex;flex-direction:column;overflow:hidden;"+
			"box-shadow:0 12px 40px rgba(0,0,0,0.6);font-family:sans-serif;",
		colBase, colSurface1))

	// ── Title bar ────────────────────────────────────────────────────────
	// Title bar carries the folder glyph + "Stage files" on the left
	// and a square close button on the right. Square close (with X
	// glyph + hover-darken) replaces the previous round red dot —
	// the dot was macOS-evocative but inconsistent with the rest of
	// the IDE chrome, which uses rectangular buttons everywhere.
	titleBar := doc.Call("createElement", "div")
	titleBar.Get("style").Set("cssText", fmt.Sprintf(
		"height:42px;min-height:42px;"+
			"background:%s;border-bottom:1px solid %s;"+
			"display:flex;align-items:center;justify-content:space-between;"+
			"padding:0 14px;cursor:move;user-select:none;-webkit-user-select:none;",
		colSurface0, colSurface1))

	titleLeft := doc.Call("createElement", "span")
	titleLeft.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;align-items:center;gap:8px;"+
			"color:%s;font-size:13px;font-weight:600;", colText))

	titleIcon := newFaIconFixed(doc, "fa-solid", "folder")
	titleIcon.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:14px;", colBlue))
	titleLeft.Call("appendChild", titleIcon)

	titleText := doc.Call("createElement", "span")
	titleText.Set("textContent", translate.T("stageFileTitle", "Stage files"))
	titleLeft.Call("appendChild", titleText)

	titleBar.Call("appendChild", titleLeft)

	closeBtn := doc.Call("createElement", "button")
	closeBtn.Get("style").Set("cssText", fmt.Sprintf(
		"width:30px;height:30px;border:none;border-radius:4px;"+
			"background:transparent;color:%s;cursor:pointer;padding:0;"+
			"display:flex;align-items:center;justify-content:center;"+
			"font-size:14px;flex-shrink:0;transition:background 0.15s;",
		colSubtext))
	closeBtn.Call("appendChild", newFaIconFixed(doc, "fa-solid", "xmark"))
	// Subtle hover: a real button affordance instead of pretending
	// the title-bar mouse-down is "drag" everywhere.
	closeBtn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeBtn.Get("style").Set("background", colSurface1)
			closeBtn.Get("style").Set("color", colText)
			return nil
		}))
	closeBtn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeBtn.Get("style").Set("background", "transparent")
			closeBtn.Get("style").Set("color", colSubtext)
			return nil
		}))
	closeBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeFn()
			return nil
		}))
	titleBar.Call("appendChild", closeBtn)
	panel.Call("appendChild", titleBar)

	// ── Body (sidebar + main area) ───────────────────────────────────────
	body := doc.Call("createElement", "div")
	body.Get("style").Set("cssText",
		"display:flex;flex:1;overflow:hidden;min-height:0;")

	// ── Sidebar (folders) ────────────────────────────────────────────────
	sidebar := doc.Call("createElement", "div")
	sidebar.Get("style").Set("cssText", fmt.Sprintf(
		"width:180px;min-width:140px;border-right:1px solid %s;"+
			"background:%s;display:flex;flex-direction:column;overflow:hidden;",
		colSurface1, colMantle))

	sidebarHeader := doc.Call("createElement", "div")
	sidebarHeader.Get("style").Set("cssText", fmt.Sprintf(
		"padding:10px 12px 6px;font-size:11px;color:%s;"+
			"text-transform:uppercase;letter-spacing:0.5px;flex-shrink:0;",
		colSubtext))
	sidebarHeader.Set("textContent", translate.T("stageFileFolders", "Folders"))
	sidebar.Call("appendChild", sidebarHeader)

	folderListEl = doc.Call("createElement", "div")
	folderListEl.Get("style").Set("cssText",
		"flex:1;overflow-y:auto;overflow-x:hidden;")
	sidebar.Call("appendChild", folderListEl)

	// New folder button (bottom of sidebar). A row-like outline-
	// dashed button signals "create" without competing with the
	// active-folder accent above.
	newFolderBtn := doc.Call("createElement", "button")
	newFolderBtn.Get("style").Set("cssText", fmt.Sprintf(
		"margin:10px 12px;padding:10px 12px;min-height:44px;"+
			"background:none;border:1px dashed %s;border-radius:4px;"+
			"color:%s;font-size:12px;cursor:pointer;text-align:left;"+
			"flex-shrink:0;display:flex;align-items:center;gap:8px;"+
			"transition:background 0.15s;",
		colSurface2, colSubtext))
	plusIcon := newFaIconFixed(doc, "fa-solid", "plus")
	plusIcon.Get("style").Set("cssText", "font-size:11px;")
	newFolderBtn.Call("appendChild", plusIcon)
	newFolderLabel := doc.Call("createElement", "span")
	newFolderLabel.Set("textContent",
		translate.T("stageFileNewFolder", "New folder"))
	newFolderBtn.Call("appendChild", newFolderLabel)
	newFolderBtn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			newFolderBtn.Get("style").Set("background", colSurface0)
			return nil
		}))
	newFolderBtn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			newFolderBtn.Get("style").Set("background", "none")
			return nil
		}))
	newFolderBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			go func() {
				name := promptInput(doc, backdrop,
					translate.T("stageFileNewFolderTitle", "New folder"),
					translate.T("stageFileNewFolderPlaceholder", "Folder name"),
				)
				if name == "" {
					return
				}
				created, err := stagefileclient.CreateFolder(name, currentDir)
				if err != nil {
					showToast(doc, backdrop, err.Error(), colRed)
					return
				}
				// Select the newly created folder immediately.
				if created != nil {
					currentDir = created.ID
				}
				refreshAll()
			}()
			return nil
		}))
	sidebar.Call("appendChild", newFolderBtn)

	body.Call("appendChild", sidebar)

	// ── Main content area ────────────────────────────────────────────────
	main := doc.Call("createElement", "div")
	main.Get("style").Set("cssText",
		"flex:1;display:flex;flex-direction:column;min-width:0;overflow:hidden;")

	// Toolbar (breadcrumb + save button).
	toolbar := doc.Call("createElement", "div")
	toolbar.Get("style").Set("cssText", fmt.Sprintf(
		"padding:8px 12px;border-bottom:1px solid %s;"+
			"display:flex;align-items:center;justify-content:space-between;"+
			"background:%s;flex-shrink:0;min-height:44px;",
		colSurface1, colBase))

	breadcrumbEl = doc.Call("createElement", "div")
	breadcrumbEl.Get("style").Set("cssText", fmt.Sprintf(
		"font-size:12px;color:%s;", colSubtext))
	toolbar.Call("appendChild", breadcrumbEl)

	saveCurrentBtn := doc.Call("createElement", "button")
	saveCurrentBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:none;border-radius:4px;"+
			"padding:8px 14px;font-size:12px;font-weight:600;cursor:pointer;"+
			"min-height:44px;flex-shrink:0;display:flex;align-items:center;gap:6px;",
		colBlue, colBase))
	saveIcon := newFaIconFixed(doc, "fa-solid", "floppy-disk")
	saveIcon.Get("style").Set("cssText", "font-size:12px;")
	saveCurrentBtn.Call("appendChild", saveIcon)
	saveLabel := doc.Call("createElement", "span")
	saveLabel.Set("textContent",
		translate.T("stageFileSaveCurrent", "Save current"))
	saveCurrentBtn.Call("appendChild", saveLabel)
	saveCurrentBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			go func() {
				// Quick save: if a file is already open, update it directly.
				currentID := ""
				if cfg.GetCurrentFileID != nil {
					currentID = cfg.GetCurrentFileID()
				}
				if currentID != "" {
					sceneJSON := "{}"
					deviceCount := 0
					if cfg.GetSceneJSON != nil {
						sceneJSON = cfg.GetSceneJSON()
					}
					if cfg.GetDeviceCount != nil {
						deviceCount = cfg.GetDeviceCount()
					}
					err := stagefileclient.UpdateFile(currentID, "", "", "", sceneJSON, "", deviceCount)
					if err != nil {
						showToast(doc, backdrop, err.Error(), colRed)
						return
					}
					currentName := ""
					if cfg.GetCurrentFileName != nil {
						currentName = cfg.GetCurrentFileName()
					}
					showToast(doc, backdrop,
						fmt.Sprintf("%s %s",
							currentName,
							translate.T("stageFileSavedQuick", "saved!")),
						colGreen)
					if cfg.OnAfterSave != nil {
						cfg.OnAfterSave()
					}
					refreshAll()
					return
				}

				// No file open — show the save dialog for a new file.
				showSaveDialog(doc, backdrop, cfg, currentDir, func() {
					refreshAll()
				})
			}()
			return nil
		}))
	toolbar.Call("appendChild", saveCurrentBtn)

	// Import Image button — opens a file picker for PNG with embedded stage data.
	importImgBtn := doc.Call("createElement", "button")
	importImgBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:transparent;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:8px 14px;font-size:12px;cursor:pointer;"+
			"min-height:44px;flex-shrink:0;display:flex;align-items:center;gap:6px;"+
			"transition:background 0.15s;",
		colText, colSurface1))
	importIcon := newFaIconFixed(doc, "fa-solid", "image")
	importIcon.Get("style").Set("cssText", "font-size:12px;")
	importImgBtn.Call("appendChild", importIcon)
	importLabel := doc.Call("createElement", "span")
	importLabel.Set("textContent",
		translate.T("stageFileImportImage", "Import Image"))
	importImgBtn.Call("appendChild", importLabel)
	importImgBtn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			importImgBtn.Get("style").Set("background", colSurface0)
			return nil
		}))
	importImgBtn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			importImgBtn.Get("style").Set("background", "transparent")
			return nil
		}))
	importImgBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if cfg.OnImportImage != nil {
				closeFn()
				cfg.OnImportImage()
			}
			return nil
		}))
	toolbar.Call("appendChild", importImgBtn)

	main.Call("appendChild", toolbar)

	// ── Tab filter (All / Stages / Tutorials) ─────────────────────────────
	// Discriminates list view by the `kind` column. Filtering is
	// client-side: renderFileList checks kindFilter against each
	// file's Kind. Switching tabs re-renders instantly from the
	// already-loaded slice without a network round-trip.
	//
	// Português: Filtro no topo da lista — mostra todos os arquivos,
	// apenas stages normais, ou apenas tutoriais. Filtra localmente
	// sem nova chamada ao servidor.
	tabBar := doc.Call("createElement", "div")
	tabBar.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;gap:0;padding:0 12px;"+
			"border-bottom:1px solid %s;background:%s;flex-shrink:0;",
		colSurface1, colBase))

	// Track the three tab buttons so we can restyle on switch.
	var tabAllBtn, tabStagesBtn, tabTutorialsBtn js.Value
	// restyleTabs highlights the currently-selected tab and dims the
	// other two. The visual is an underline on the active tab (peach
	// accent) — quieter than the pill-style buttons we had before,
	// which competed with the toolbar buttons sitting one row above.
	restyleTabs := func() {
		styleFor := func(selected bool) string {
			border := "2px solid transparent"
			color := colSubtext
			weight := "500"
			if selected {
				border = "2px solid " + colPeach
				color = colPeach
				weight = "600"
			}
			return fmt.Sprintf(
				"background:transparent;color:%s;border:none;"+
					"border-bottom:%s;border-radius:0;"+
					"padding:10px 14px;font-size:12px;font-weight:%s;cursor:pointer;"+
					"margin-bottom:-1px;min-height:36px;",
				color, border, weight)
		}
		tabAllBtn.Get("style").Set("cssText", styleFor(kindFilter == "all"))
		tabStagesBtn.Get("style").Set("cssText", styleFor(kindFilter == "stage"))
		tabTutorialsBtn.Get("style").Set("cssText", styleFor(kindFilter == "tutorial"))
	}

	// Renders a single tab button with the given label and value.
	// The value is what kindFilter gets set to when the tab is clicked.
	mkTab := func(label, value string) js.Value {
		btn := doc.Call("createElement", "button")
		btn.Set("textContent", label)
		btn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				kindFilter = value
				restyleTabs()
				// renderFileList is defined further down; it reads
				// kindFilter from closure scope. Trigger a re-render.
				renderFileList()
				return nil
			}))
		return btn
	}

	tabAllBtn = mkTab(translate.T("stageFileTabAll", "All"), "all")
	tabStagesBtn = mkTab(translate.T("stageFileTabStages", "Stages"), "stage")
	tabTutorialsBtn = mkTab(translate.T("stageFileTabTutorials", "Tutorials"), "tutorial")

	tabBar.Call("appendChild", tabAllBtn)
	tabBar.Call("appendChild", tabStagesBtn)
	tabBar.Call("appendChild", tabTutorialsBtn)
	restyleTabs()

	main.Call("appendChild", tabBar)

	// File list (scrollable).
	fileListEl = doc.Call("createElement", "div")
	fileListEl.Get("style").Set("cssText",
		"flex:1;overflow-y:auto;overflow-x:hidden;")
	main.Call("appendChild", fileListEl)

	// Footer (limit indicator + close).
	footerEl = doc.Call("createElement", "div")
	footerEl.Get("style").Set("cssText", fmt.Sprintf(
		"padding:8px 12px;border-top:1px solid %s;"+
			"display:flex;align-items:center;justify-content:space-between;"+
			"background:%s;flex-shrink:0;min-height:44px;",
		colSurface1, colMantle))
	main.Call("appendChild", footerEl)

	body.Call("appendChild", main)
	panel.Call("appendChild", body)

	// ── Drag logic ───────────────────────────────────────────────────────
	attachDrag(doc, titleBar, panel)

	// ── Backdrop events ──────────────────────────────────────────────────
	backdrop.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("target").Equal(backdrop) {
				closeFn()
			}
			return nil
		}))
	doc.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("key").String() == "Escape" {
				closeFn()
			}
			return nil
		}))

	backdrop.Call("appendChild", panel)
	doc.Get("body").Call("appendChild", backdrop)

	// ── Render functions ─────────────────────────────────────────────────

	// renderBreadcrumb updates the breadcrumb path based on currentDir.
	renderBreadcrumb := func() {
		breadcrumbEl.Set("innerHTML", "")
		root := doc.Call("createElement", "span")
		root.Get("style").Set("cssText", fmt.Sprintf(
			"color:%s;cursor:pointer;", colBlue))
		root.Set("textContent", translate.T("stageFileAllFiles", "All files"))
		root.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				currentDir = ""
				go refreshAll()
				return nil
			}))
		breadcrumbEl.Call("appendChild", root)

		if currentDir != "" {
			// Find folder name.
			folderName := currentDir
			for _, f := range folders {
				if f.ID == currentDir {
					folderName = f.Name
					break
				}
			}
			sep := doc.Call("createElement", "span")
			sep.Get("style").Set("cssText", fmt.Sprintf("color:%s;margin:0 6px;", colSurface2))
			sep.Set("textContent", "/")
			breadcrumbEl.Call("appendChild", sep)

			curr := doc.Call("createElement", "span")
			curr.Get("style").Set("cssText", fmt.Sprintf("color:%s;", colText))
			curr.Set("textContent", folderName)
			breadcrumbEl.Call("appendChild", curr)
		}
	}

	// renderFolderList rebuilds the folder sidebar with nested tree support.
	// Folders are rendered recursively with increasing indentation per depth level.
	renderFolderList := func() {
		folderListEl.Set("innerHTML", "")

		// "All files" entry — always at the top. The folder-open icon
		// signals "you are here" when active; the plain folder icon
		// matches every other folder row so the visual rhythm is
		// preserved across the sidebar.
		allRow := doc.Call("createElement", "div")
		allBg := "transparent"
		allBorder := "2px solid transparent"
		allColor := colText
		allIconName := "folder"
		if currentDir == "" {
			allBg = colSurface0
			allBorder = "2px solid " + colBlue
			allColor = colBlue
			allIconName = "folder-open"
		}
		allRow.Get("style").Set("cssText", fmt.Sprintf(
			"padding:10px 12px;font-size:13px;color:%s;background:%s;"+
				"border-left:%s;cursor:pointer;min-height:44px;"+
				"display:flex;align-items:center;gap:10px;",
			allColor, allBg, allBorder))

		allIcon := newFaIconFixed(doc, "fa-solid", allIconName)
		allIcon.Get("style").Set("cssText", fmt.Sprintf(
			"font-size:13px;width:16px;text-align:center;color:%s;flex-shrink:0;",
			allColor))
		allRow.Call("appendChild", allIcon)
		allLabel := doc.Call("createElement", "span")
		allLabel.Set("textContent", translate.T("stageFileAllFiles", "All files"))
		allRow.Call("appendChild", allLabel)

		allRow.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				currentDir = ""
				go refreshAll()
				return nil
			}))
		folderListEl.Call("appendChild", allRow)

		// Build children map: parentID → []folder for tree rendering.
		childMap := make(map[string][]stagefileclient.StageFolderEntry)
		for _, f := range folders {
			childMap[f.ParentID] = append(childMap[f.ParentID], f)
		}

		// renderNode recursively renders a folder and its children.
		// Visual:
		//   - active folder → blue accent (matches the All files row)
		//   - inactive       → text colour
		//   - hierarchy via left padding (16px per depth level)
		// The previous "▾ vs ·" unicode indicator is replaced by a
		// proper folder/folder-open glyph and a chevron-down for
		// nodes that have children (rendered to the left, between
		// the indent space and the folder icon).
		var renderNode func(parentID string, depth int)
		renderNode = func(parentID string, depth int) {
			children := childMap[parentID]
			for _, folder := range children {
				f := folder // capture
				leftPad := 12 + depth*16

				row := doc.Call("createElement", "div")
				bg := "transparent"
				border := "2px solid transparent"
				rowColor := colText
				iconName := "folder"
				if currentDir == f.ID {
					bg = colSurface0
					border = "2px solid " + colBlue
					rowColor = colBlue
					iconName = "folder-open"
				}
				row.Get("style").Set("cssText", fmt.Sprintf(
					"padding:10px 8px 10px %dpx;font-size:13px;color:%s;background:%s;"+
						"border-left:%s;cursor:pointer;min-height:44px;"+
						"display:flex;align-items:center;justify-content:space-between;gap:8px;",
					leftPad, rowColor, bg, border))

				// Folder icon — solid family, swaps to folder-open on
				// active so the active state has two reinforcing cues
				// (colour + glyph variant).
				folderIcon := newFaIconFixed(doc, "fa-solid", iconName)
				folderIcon.Get("style").Set("cssText", fmt.Sprintf(
					"font-size:13px;width:16px;text-align:center;color:%s;flex-shrink:0;",
					rowColor))
				row.Call("appendChild", folderIcon)

				nameSpan := doc.Call("createElement", "span")
				nameSpan.Get("style").Set("cssText",
					"overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1;")
				nameSpan.Set("textContent", f.Name)
				row.Call("appendChild", nameSpan)

				// Delete folder button. Trash icon stays muted in red
				// at low opacity so it doesn't dominate the sidebar
				// row at rest; mouse-over restores full opacity.
				delBtn := doc.Call("createElement", "button")
				delBtn.Get("style").Set("cssText", fmt.Sprintf(
					"background:none;border:none;color:%s;cursor:pointer;"+
						"padding:4px 6px;opacity:0.4;flex-shrink:0;font-size:12px;"+
						"transition:opacity 0.15s;",
					colRed))
				delBtn.Call("appendChild",
					newFaIconFixed(doc, "fa-solid", "trash"))
				delBtn.Call("addEventListener", "mouseenter",
					js.FuncOf(func(this js.Value, args []js.Value) interface{} {
						delBtn.Get("style").Set("opacity", "1")
						return nil
					}))
				delBtn.Call("addEventListener", "mouseleave",
					js.FuncOf(func(this js.Value, args []js.Value) interface{} {
						delBtn.Get("style").Set("opacity", "0.4")
						return nil
					}))
				delBtn.Call("addEventListener", "click",
					js.FuncOf(func(this js.Value, args []js.Value) interface{} {
						args[0].Call("stopPropagation")
						go func() {
							if !confirmAction(doc, backdrop,
								translate.T("stageFileDeleteFolderTitle", "Delete folder"),
								fmt.Sprintf("%q", f.Name),
								translate.T("stageFileDeleteFolderWarn", "All files inside will be deleted."),
							) {
								return
							}
							if err := stagefileclient.DeleteFolder(f.ID); err != nil {
								showToast(doc, backdrop, err.Error(), colRed)
								return
							}
							if currentDir == f.ID {
								currentDir = ""
							}
							refreshAll()
						}()
						return nil
					}))
				row.Call("appendChild", delBtn)

				row.Call("addEventListener", "click",
					js.FuncOf(func(this js.Value, args []js.Value) interface{} {
						currentDir = f.ID
						go refreshAll()
						return nil
					}))
				folderListEl.Call("appendChild", row)

				// Recurse into children.
				renderNode(f.ID, depth+1)
			}
		}

		// Start from root (parentID = "").
		renderNode("", 1)
	}

	// renderFileList rebuilds the file list, honouring the current
	// tab filter (kindFilter from closure scope). Filtering is a
	// simple slice pass — cheap because typical user caps are in the
	// tens of files.
	renderFileList = func() {
		fileListEl.Set("innerHTML", "")

		// Apply the kind filter. kindFilter == "all" passes everything
		// through. "stage" and "tutorial" match against the Kind field
		// on each entry, treating empty Kind as "stage" (back-compat
		// with files written before the column existed).
		visible := make([]stagefileclient.StageFileEntry, 0, len(files))
		for _, f := range files {
			k := f.Kind
			if k == "" {
				k = stagefileclient.StageFileKindStage
			}
			switch kindFilter {
			case "all":
				visible = append(visible, f)
			case "stage", "tutorial":
				if k == kindFilter {
					visible = append(visible, f)
				}
			}
		}

		if len(visible) == 0 {
			empty := doc.Call("createElement", "div")
			empty.Get("style").Set("cssText", fmt.Sprintf(
				"padding:40px 20px;text-align:center;color:%s;font-size:14px;",
				colSubtext))
			// Different empty-state text per tab — the "no tutorials"
			// message guides the user toward the admin; the generic
			// message is used for All and Stages.
			var msg string
			switch kindFilter {
			case "tutorial":
				msg = translate.T("stageFileEmptyTutorials",
					"No tutorials available yet. Ask your admin to publish one.")
			default:
				msg = translate.T("stageFileEmpty",
					"No files yet. Click \"Save current\" to save your stage.")
			}
			empty.Set("textContent", msg)
			fileListEl.Call("appendChild", empty)
			return
		}

		for _, file := range visible {
			f := file // capture
			row := doc.Call("createElement", "div")
			row.Get("style").Set("cssText", fmt.Sprintf(
				"padding:12px 14px;display:flex;align-items:center;gap:12px;"+
					"cursor:pointer;border-bottom:1px solid %s;min-height:60px;"+
					"transition:background 0.12s;",
				colSurface1+"33"))

			// Hover effect — surface0 with a touch of alpha so the
			// row reads as "interactive" without flashing too dark.
			row.Call("addEventListener", "mouseenter",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					row.Get("style").Set("background", colSurface0)
					return nil
				}))
			row.Call("addEventListener", "mouseleave",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					row.Get("style").Set("background", "transparent")
					return nil
				}))

			// ── Icon chip on the left ─────────────────────────────
			// 36×36 surface0 panel with the file's chosen FA icon
			// (or the default `cube` when IconID is empty). Tutorials
			// override to a book glyph regardless of the saved icon —
			// the kind is a stronger signal than the maker's chosen
			// icon for the tutorial-vs-stage distinction.
			iconWrap := doc.Call("createElement", "div")
			iconWrap.Get("style").Set("cssText", fmt.Sprintf(
				"width:36px;height:36px;background:%s;border-radius:6px;"+
					"display:flex;align-items:center;justify-content:center;"+
					"color:%s;font-size:16px;flex-shrink:0;",
				colSurface0, colBlue))

			isTutorial := f.Kind == stagefileclient.StageFileKindTutorial
			iconName := f.IconID
			if iconName == "" {
				iconName = fileDefaultIcon
			}
			if isTutorial {
				// Tutorial visual override: book-open is the
				// universal "guided content" cue.
				iconWrap.Get("style").Set("color", colPeach)
				iconWrap.Call("appendChild",
					newFaIconFixed(doc, "fa-solid", "book-open-reader"))
			} else {
				iconWrap.Call("appendChild", newFaIcon(doc, iconName))
			}
			row.Call("appendChild", iconWrap)

			// ── Info column (name + metadata) ─────────────────────
			infoDiv := doc.Call("createElement", "div")
			infoDiv.Get("style").Set("cssText",
				"flex:1;min-width:0;overflow:hidden;")

			nameEl := doc.Call("createElement", "div")
			nameEl.Get("style").Set("cssText", fmt.Sprintf(
				"font-size:14px;font-weight:500;color:%s;white-space:nowrap;"+
					"overflow:hidden;text-overflow:ellipsis;", colText))
			nameEl.Set("textContent", f.Name)
			infoDiv.Call("appendChild", nameEl)

			// Metadata line — small icons reinforce the data type
			// (cpu for device count, clock for last-modified). Built
			// as a flex row so the icons stay glued to their numbers
			// even when the row is narrow.
			metaEl := doc.Call("createElement", "div")
			metaEl.Get("style").Set("cssText", fmt.Sprintf(
				"font-size:11px;color:%s;margin-top:3px;"+
					"display:flex;align-items:center;gap:10px;",
				colSubtext))

			if isTutorial {
				// Tutorial: book glyph + "Tutorial" label, then date.
				kindIcon := newFaIconFixed(doc, "fa-solid", "book-open")
				kindIcon.Get("style").Set("cssText", "font-size:10px;")
				metaEl.Call("appendChild", kindIcon)
				kindLabel := doc.Call("createElement", "span")
				kindLabel.Set("textContent",
					translate.T("stageFileKindTutorial", "Tutorial"))
				metaEl.Call("appendChild", kindLabel)
			} else {
				// Stage: cpu glyph + device count, then date.
				devIcon := newFaIconFixed(doc, "fa-solid", "microchip")
				devIcon.Get("style").Set("cssText", "font-size:10px;")
				metaEl.Call("appendChild", devIcon)
				devCount := doc.Call("createElement", "span")
				devCount.Set("textContent",
					fmt.Sprintf("%d %s", f.DeviceCount,
						translate.T("stageFileDevices", "devices")))
				metaEl.Call("appendChild", devCount)
			}

			dateIcon := newFaIconFixed(doc, "fa-regular", "clock")
			dateIcon.Get("style").Set("cssText", "font-size:10px;")
			metaEl.Call("appendChild", dateIcon)
			dateSpan := doc.Call("createElement", "span")
			dateSpan.Set("textContent", formatDate(f.UpdatedAt))
			metaEl.Call("appendChild", dateSpan)

			infoDiv.Call("appendChild", metaEl)
			row.Call("appendChild", infoDiv)

			// ── Language chip on the right ────────────────────────
			// "C99" (peach) or "Go" (blue) so the maker can tell a C
			// project from a Go one at a glance. The colors mirror the
			// welcome modal's "+ C99 project" / "+ Go project" buttons.
			// Language is "go" or "c" (empty defaults to C99 per the
			// server schema's NOT NULL DEFAULT).
			langLabel := "C99"
			langColor := colPeach
			if f.Language == stagefileclient.StageFileLanguageGo {
				langLabel = "Go"
				langColor = colBlue
			}
			langChip := doc.Call("createElement", "span")
			langChip.Get("style").Set("cssText", fmt.Sprintf(
				"flex-shrink:0;align-self:center;margin:0 6px;padding:2px 9px;"+
					"border-radius:10px;font-size:10px;font-weight:700;"+
					"letter-spacing:0.3px;background:%s;color:%s;",
				langColor, colBase))
			langChip.Set("textContent", langLabel)
			row.Call("appendChild", langChip)

			// ── Action buttons (Edit, Delete) ─────────────────────
			// Open is gone — the entire row is clickable now (see
			// row.click handler below). Move is gone — its job moved
			// into the Edit dialog's Folder field, so the maker only
			// sees two action buttons here instead of four.
			btns := doc.Call("createElement", "div")
			btns.Get("style").Set("cssText",
				"display:flex;gap:2px;flex-shrink:0;")

			// Edit button — opens the full Edit dialog
			// (name + icon + folder).
			editBtn := doc.Call("createElement", "button")
			editBtn.Get("style").Set("cssText", fmt.Sprintf(
				"background:transparent;border:none;color:%s;cursor:pointer;"+
					"padding:8px 10px;font-size:14px;border-radius:4px;"+
					"transition:background 0.12s,color 0.12s;",
				colSubtext))
			editBtn.Set("title", translate.T("stageFileEdit", "Edit"))
			editBtn.Call("appendChild",
				newFaIconFixed(doc, "fa-solid", "pen"))
			editBtn.Call("addEventListener", "mouseenter",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					editBtn.Get("style").Set("background", colSurface1)
					editBtn.Get("style").Set("color", colText)
					return nil
				}))
			editBtn.Call("addEventListener", "mouseleave",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					editBtn.Get("style").Set("background", "transparent")
					editBtn.Get("style").Set("color", colSubtext)
					return nil
				}))
			editBtn.Call("addEventListener", "click",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					args[0].Call("stopPropagation")
					// Capture by value — the loop variable f is reused
					// on the next iteration. The dialog only reads
					// .ID/.Name/.IconID/.FolderID.
					fCopy := f
					foldersCopy := folders
					go func() {
						showEditDialog(doc, backdrop, fCopy, foldersCopy, func() {
							refreshAll()
						})
					}()
					return nil
				}))
			btns.Call("appendChild", editBtn)

			// Delete button.
			delBtn := doc.Call("createElement", "button")
			delBtn.Get("style").Set("cssText", fmt.Sprintf(
				"background:transparent;border:none;color:%s;cursor:pointer;"+
					"padding:8px 10px;font-size:14px;border-radius:4px;opacity:0.7;"+
					"transition:opacity 0.12s,background 0.12s;",
				colRed))
			delBtn.Set("title", translate.T("stageFileDelete", "Delete"))
			delBtn.Call("appendChild",
				newFaIconFixed(doc, "fa-solid", "trash"))
			delBtn.Call("addEventListener", "mouseenter",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					delBtn.Get("style").Set("opacity", "1")
					delBtn.Get("style").Set("background", colSurface1)
					return nil
				}))
			delBtn.Call("addEventListener", "mouseleave",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					delBtn.Get("style").Set("opacity", "0.7")
					delBtn.Get("style").Set("background", "transparent")
					return nil
				}))
			delBtn.Call("addEventListener", "click",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					args[0].Call("stopPropagation")
					go func() {
						if !confirmAction(doc, backdrop,
							translate.T("stageFileDeleteTitle", "Delete file"),
							fmt.Sprintf("%q", f.Name),
							translate.T("stageFileDeleteWarn", "This action cannot be undone."),
						) {
							return
						}
						if err := stagefileclient.DeleteFile(f.ID); err != nil {
							showToast(doc, backdrop, err.Error(), colRed)
							return
						}
						refreshAll()
					}()
					return nil
				}))
			btns.Call("appendChild", delBtn)

			row.Call("appendChild", btns)

			// Row-wide click → open the file. Single source of truth
			// for the "open this stage" gesture — replaces the
			// previous Open button. Tutorials open exactly the same
			// way; the player integration (C-3) is invoked downstream
			// by the workspace.
			row.Call("addEventListener", "click",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					go func() {
						loaded, err := stagefileclient.LoadFile(f.ID)
						if err != nil {
							showToast(doc, backdrop, err.Error(), colRed)
							return
						}
						log.Printf("[StageFileUI] Loaded file %q (%d bytes, kind=%s)",
							f.Name, len(loaded.SceneJSON), loaded.Kind)
						if cfg.OnLoad != nil {
							cfg.OnLoad(loaded.SceneJSON)
						}
						if cfg.OnFileOpened != nil {
							cfg.OnFileOpened(f.ID, f.Name)
						}
						closeFn()
					}()
					return nil
				}))

			fileListEl.Call("appendChild", row)
		}
	}

	// renderFooter updates the limit indicator.
	renderFooter := func() {
		footerEl.Set("innerHTML", "")

		limitText := doc.Call("createElement", "span")
		limitText.Get("style").Set("cssText", fmt.Sprintf(
			"font-size:12px;color:%s;", colSubtext))
		if limitInfo != nil {
			limitText.Set("textContent",
				fmt.Sprintf("%d / %d %s",
					limitInfo.UsedFiles, limitInfo.MaxFiles,
					translate.T("stageFileFilesUsed", "files")))
		}
		footerEl.Call("appendChild", limitText)

		closeFooterBtn := doc.Call("createElement", "button")
		closeFooterBtn.Get("style").Set("cssText", fmt.Sprintf(
			"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
				"padding:8px 16px;font-size:12px;cursor:pointer;min-height:44px;",
			colSurface0, colText, colSurface1))
		closeFooterBtn.Set("textContent", translate.T("stageFileClose", "Close"))
		closeFooterBtn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				closeFn()
				return nil
			}))
		footerEl.Call("appendChild", closeFooterBtn)
	}

	// ── Refresh (network) ────────────────────────────────────────────────
	refreshAll = func() {
		var err error
		folders, err = stagefileclient.ListFolders()
		if err != nil {
			log.Printf("[StageFileUI] ListFolders error: %v", err)
		}

		files, err = stagefileclient.ListFiles(currentDir)
		if err != nil {
			log.Printf("[StageFileUI] ListFiles error: %v", err)
		}

		info, err := stagefileclient.GetLimit()
		if err != nil {
			log.Printf("[StageFileUI] GetLimit error: %v", err)
		} else {
			limitInfo = info
		}

		renderFolderList()
		renderFileList()
		renderBreadcrumb()
		renderFooter()
	}

	// Initial load — run in goroutine (blocking fetch).
	go refreshAll()
}

// ─── Save dialog ──────────────────────────────────────────────────────────────

// showSaveDialog opens a modal dialog for saving the current stage.
func showSaveDialog(doc js.Value, parent js.Value, cfg Config, currentDir string, onDone func()) {
	modal := doc.Call("createElement", "div")
	modal.Get("style").Set("cssText",
		"position:absolute;top:0;left:0;width:100%;height:100%;"+
			"background:rgba(0,0,0,0.5);display:flex;"+
			"align-items:center;justify-content:center;z-index:100001;")

	card := doc.Call("createElement", "div")
	card.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;border:1px solid %s;border-radius:6px;"+
			"width:90%%;max-width:420px;overflow:hidden;",
		colBase, colSurface1))

	// Header.
	header := doc.Call("createElement", "div")
	header.Get("style").Set("cssText", fmt.Sprintf(
		"height:36px;background:%s;border-bottom:1px solid %s;"+
			"display:flex;align-items:center;padding:0 10px;",
		colSurface0, colSurface1))
	headerText := doc.Call("createElement", "span")
	headerText.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:12px;font-weight:600;", colText))
	headerText.Set("textContent", translate.T("stageFileSaveTitle", "Save current stage"))
	header.Call("appendChild", headerText)
	card.Call("appendChild", header)

	// Body.
	body := doc.Call("createElement", "div")
	body.Get("style").Set("cssText", "padding:20px;")

	// Name field.
	nameRow := doc.Call("createElement", "div")
	nameRow.Get("style").Set("cssText",
		"display:flex;align-items:center;gap:10px;margin-bottom:16px;")
	nameLabel := doc.Call("createElement", "label")
	nameLabel.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:13px;font-weight:500;min-width:60px;", colText))
	nameLabel.Set("textContent", translate.T("stageFileName", "Name"))
	nameRow.Call("appendChild", nameLabel)
	nameInput := doc.Call("createElement", "input")
	nameInput.Set("type", "text")
	nameInput.Set("placeholder", translate.T("stageFileNamePlaceholder", "My project"))
	nameInput.Get("style").Set("cssText", fmt.Sprintf(
		"flex:1;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:10px;font-size:14px;min-height:44px;"+
			"box-sizing:border-box;font-family:sans-serif;outline:none;",
		colMantle, colText, colSurface1))
	nameRow.Call("appendChild", nameInput)
	body.Call("appendChild", nameRow)

	// Icon picker — lets the maker tag the file with a FontAwesome
	// glyph that appears next to the file name in the list. Empty
	// at this point because Save Dialog is for fresh files; the
	// Edit dialog re-uses the same widget pre-populated with the
	// existing icon. See iconpicker.go for the component contract.
	iconLabel := doc.Call("createElement", "label")
	iconLabel.Get("style").Set("cssText", fmt.Sprintf(
		"display:block;color:%s;font-size:13px;font-weight:500;"+
			"margin-bottom:8px;", colText))
	iconLabel.Set("textContent",
		translate.T("stageFileIcon", "Icon (optional)"))
	body.Call("appendChild", iconLabel)
	iconPicker := NewIconPicker(doc, body, "")

	// Spacer between picker and button row.
	spacer := doc.Call("createElement", "div")
	spacer.Get("style").Set("cssText", "height:16px;")
	body.Call("appendChild", spacer)

	// Buttons.
	btnRow := doc.Call("createElement", "div")
	btnRow.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;justify-content:flex-end;gap:8px;"+
			"border-top:1px solid %s;padding-top:16px;",
		colSurface1))

	saveBtn := doc.Call("createElement", "button")
	saveBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:none;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colBlue, colBase))
	saveBtn.Set("textContent", translate.T("stageFileSave", "Save"))

	cancelBtn := doc.Call("createElement", "button")
	cancelBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colSurface0, colText, colSurface1))
	cancelBtn.Set("textContent", translate.T("stageFileCancel", "Cancel"))

	closeModal := func() {
		if modal.Get("parentNode").Truthy() {
			modal.Get("parentNode").Call("removeChild", modal)
		}
	}

	cancelBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeModal()
			return nil
		}))

	saveBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			name := nameInput.Get("value").String()
			if name == "" {
				nameInput.Get("style").Set("borderColor", colRed)
				return nil
			}
			// Capture the icon choice now — the picker's DOM is
			// removed once closeModal() runs in the success path,
			// so reading it inside the goroutine below would race.
			iconID := iconPicker.Value()
			go func() {
				sceneJSON := "{}"
				deviceCount := 0
				if cfg.GetSceneJSON != nil {
					sceneJSON = cfg.GetSceneJSON()
				}
				if cfg.GetDeviceCount != nil {
					deviceCount = cfg.GetDeviceCount()
				}

				// Try to create a new file first.
				//
				// The language argument flows in via cfg.GetProjectLanguage
				// once Parcela 3 wires the workspace's fixed language into
				// the file manager config. Until then, we pass the
				// neutral default StageFileLanguageC — the server applies
				// the same default when this field is empty, so behaviour
				// is identical to the pre-Parcela 1 code.
				language := stagefileclient.StageFileLanguageC
				if cfg.GetProjectLanguage != nil {
					language = cfg.GetProjectLanguage()
				}
				entry, err := stagefileclient.SaveFile(name, currentDir, language, sceneJSON, iconID, deviceCount)
				if err != nil {
					// If the name already exists, overwrite the existing file.
					errMsg := err.Error()
					if strings.Contains(errMsg, "already exists") {
						// Find the existing file ID by listing files in the folder.
						existing, listErr := stagefileclient.ListFiles(currentDir)
						if listErr == nil {
							for _, f := range existing {
								if f.Name == name {
									// Overwrite path: update the scene AND
									// the icon. Sending the icon here lets a
									// maker change their mind about the glyph
									// when "saving over" a previous version.
									updateErr := stagefileclient.UpdateFile(
										f.ID, "", "", "", sceneJSON, iconID, deviceCount,
									)
									if updateErr != nil {
										showToast(doc, parent, updateErr.Error(), colRed)
										return
									}
									// Track as current file for quick save.
									if cfg.OnFileOpened != nil {
										cfg.OnFileOpened(f.ID, f.Name)
									}
									if cfg.OnAfterSave != nil {
										cfg.OnAfterSave()
									}
									showToast(doc, parent,
										translate.T("stageFileSaved", "File saved!"), colGreen)
									closeModal()
									if onDone != nil {
										onDone()
									}
									return
								}
							}
						}
					}
					showToast(doc, parent, errMsg, colRed)
					return
				}
				// Track as current file for quick save.
				if cfg.OnFileOpened != nil && entry != nil {
					cfg.OnFileOpened(entry.ID, entry.Name)
				}
				if cfg.OnAfterSave != nil {
					cfg.OnAfterSave()
				}
				showToast(doc, parent,
					translate.T("stageFileSaved", "File saved!"), colGreen)
				closeModal()
				if onDone != nil {
					onDone()
				}
			}()
			return nil
		}))

	// Enter key → save.
	nameInput.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("key").String() == "Enter" {
				saveBtn.Call("click")
			}
			return nil
		}))

	btnRow.Call("appendChild", saveBtn)
	btnRow.Call("appendChild", cancelBtn)
	body.Call("appendChild", btnRow)
	card.Call("appendChild", body)

	modal.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("target").Equal(modal) {
				closeModal()
			}
			return nil
		}))

	modal.Call("appendChild", card)
	parent.Call("appendChild", modal)

	// Auto-focus the name input.
	nameInput.Call("focus")
}

// showEditDialog opens a modal to edit an existing file's name,
// icon, and folder. Replaces the bare promptInput-based rename so
// the maker can adjust three fields from one place. The Folder
// dropdown subsumes what used to be the row's standalone Move
// button — fewer buttons on the row, equivalent reach via Edit.
//
// Behaviour:
//
//   - Pre-populated with the file's current name, icon, and folder.
//   - On Save, diffs against the original values and sends only the
//     fields that changed:
//     · same name           → name field omitted (no change)
//     · same folder         → folderId omitted (no change)
//     · moved to root       → folderId "__root__"
//     · empty icon, had one → iconID "__clear__" (reset to NULL)
//     · same icon           → iconID omitted (no change)
//     · changed name/icon   → that field carries the new value
//   - On Cancel or backdrop click, the dialog closes without writing.
//
// folders is the full sidebar folder list (parent → children
// already flattened by ListFolders), passed in so the dropdown can
// offer every existing target without a second API round-trip.
//
// onDone runs after a successful save so the caller can refresh the
// file list. The dialog handles toast feedback on errors internally.
//
// Português: Modal de edição de arquivo (nome + ícone + pasta).
// Substitui o prompt de rename e absorve o botão Move da linha.
// Envia ao servidor apenas os campos que mudaram; "__clear__" no
// iconID limpa o ícone e "__root__" no folderId move para a raiz.
func showEditDialog(doc js.Value, parent js.Value, file stagefileclient.StageFileEntry, folders []stagefileclient.StageFolderEntry, onDone func()) {
	modal := doc.Call("createElement", "div")
	modal.Get("style").Set("cssText",
		"position:absolute;top:0;left:0;width:100%;height:100%;"+
			"background:rgba(0,0,0,0.5);display:flex;"+
			"align-items:center;justify-content:center;z-index:100001;")

	card := doc.Call("createElement", "div")
	card.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;border:1px solid %s;border-radius:6px;"+
			"width:90%%;max-width:420px;overflow:hidden;",
		colBase, colSurface1))

	// Header.
	header := doc.Call("createElement", "div")
	header.Get("style").Set("cssText", fmt.Sprintf(
		"height:36px;background:%s;border-bottom:1px solid %s;"+
			"display:flex;align-items:center;padding:0 10px;",
		colSurface0, colSurface1))
	headerText := doc.Call("createElement", "span")
	headerText.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:12px;font-weight:600;", colText))
	headerText.Set("textContent",
		translate.T("stageFileEditTitle", "Edit file"))
	header.Call("appendChild", headerText)
	card.Call("appendChild", header)

	// Body.
	body := doc.Call("createElement", "div")
	body.Get("style").Set("cssText", "padding:20px;")

	// Name field — pre-populated with the current name.
	nameRow := doc.Call("createElement", "div")
	nameRow.Get("style").Set("cssText",
		"display:flex;align-items:center;gap:10px;margin-bottom:16px;")
	nameLabel := doc.Call("createElement", "label")
	nameLabel.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:13px;font-weight:500;min-width:60px;", colText))
	nameLabel.Set("textContent", translate.T("stageFileName", "Name"))
	nameRow.Call("appendChild", nameLabel)
	nameInput := doc.Call("createElement", "input")
	nameInput.Set("type", "text")
	nameInput.Set("value", file.Name)
	nameInput.Get("style").Set("cssText", fmt.Sprintf(
		"flex:1;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:10px;font-size:14px;min-height:44px;"+
			"box-sizing:border-box;font-family:sans-serif;outline:none;",
		colMantle, colText, colSurface1))
	nameRow.Call("appendChild", nameInput)
	body.Call("appendChild", nameRow)

	// Folder field — native <select> so the dropdown picker stays
	// touch-friendly on tablets (every OS renders the listbox in
	// a system-native way) and accessible to keyboards. Options
	// are "Root" (empty value) plus every existing folder. The
	// folders slice is already sorted by ListFolders, so we render
	// them in the order the maker sees in the sidebar.
	folderRow := doc.Call("createElement", "div")
	folderRow.Get("style").Set("cssText",
		"display:flex;align-items:center;gap:10px;margin-bottom:16px;")
	folderLabel := doc.Call("createElement", "label")
	folderLabel.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:13px;font-weight:500;min-width:60px;", colText))
	folderLabel.Set("textContent",
		translate.T("stageFileFolder", "Folder"))
	folderRow.Call("appendChild", folderLabel)
	folderSelect := doc.Call("createElement", "select")
	folderSelect.Get("style").Set("cssText", fmt.Sprintf(
		"flex:1;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:10px;font-size:14px;min-height:44px;"+
			"box-sizing:border-box;font-family:sans-serif;outline:none;cursor:pointer;",
		colMantle, colText, colSurface1))

	rootOpt := doc.Call("createElement", "option")
	rootOpt.Set("value", "")
	rootOpt.Set("textContent",
		translate.T("stageFileFolderRoot", "(root)"))
	if file.FolderID == "" {
		rootOpt.Set("selected", true)
	}
	folderSelect.Call("appendChild", rootOpt)
	for _, fld := range folders {
		opt := doc.Call("createElement", "option")
		opt.Set("value", fld.ID)
		opt.Set("textContent", fld.Name)
		if fld.ID == file.FolderID {
			opt.Set("selected", true)
		}
		folderSelect.Call("appendChild", opt)
	}
	folderRow.Call("appendChild", folderSelect)
	body.Call("appendChild", folderRow)

	// Icon picker — pre-populated with the file's current icon (may
	// be empty for files saved before the icon column existed).
	iconLabel := doc.Call("createElement", "label")
	iconLabel.Get("style").Set("cssText", fmt.Sprintf(
		"display:block;color:%s;font-size:13px;font-weight:500;"+
			"margin-bottom:8px;", colText))
	iconLabel.Set("textContent",
		translate.T("stageFileIcon", "Icon (optional)"))
	body.Call("appendChild", iconLabel)
	iconPicker := NewIconPicker(doc, body, file.IconID)

	// Spacer.
	spacer := doc.Call("createElement", "div")
	spacer.Get("style").Set("cssText", "height:16px;")
	body.Call("appendChild", spacer)

	// Buttons.
	btnRow := doc.Call("createElement", "div")
	btnRow.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;justify-content:flex-end;gap:8px;"+
			"border-top:1px solid %s;padding-top:16px;",
		colSurface1))

	saveBtn := doc.Call("createElement", "button")
	saveBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:none;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colBlue, colBase))
	saveBtn.Set("textContent", translate.T("stageFileSave", "Save"))

	cancelBtn := doc.Call("createElement", "button")
	cancelBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colSurface0, colText, colSurface1))
	cancelBtn.Set("textContent", translate.T("stageFileCancel", "Cancel"))

	closeModal := func() {
		if modal.Get("parentNode").Truthy() {
			modal.Get("parentNode").Call("removeChild", modal)
		}
	}

	cancelBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeModal()
			return nil
		}))

	saveBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			newName := strings.TrimSpace(nameInput.Get("value").String())
			if newName == "" {
				nameInput.Get("style").Set("borderColor", colRed)
				return nil
			}
			newIcon := iconPicker.Value()
			newFolder := folderSelect.Get("value").String()
			// Diff against the original values and translate into
			// the UpdateFile contract:
			//   - same name → "" (no change)
			//   - same folder → "" (no change)
			//   - had folder, now root → "__root__"
			//   - had icon, now empty → "__clear__"
			//   - same icon → "" (no change)
			//   - different non-empty values → those values
			nameArg := ""
			if newName != file.Name {
				nameArg = newName
			}
			folderArg := ""
			switch {
			case file.FolderID != "" && newFolder == "":
				folderArg = "__root__"
			case newFolder != file.FolderID:
				folderArg = newFolder
			}
			iconArg := ""
			switch {
			case file.IconID != "" && newIcon == "":
				iconArg = "__clear__"
			case newIcon != file.IconID:
				iconArg = newIcon
			}
			// No-op short-circuit: nothing changed, treat as cancel.
			if nameArg == "" && folderArg == "" && iconArg == "" {
				closeModal()
				if onDone != nil {
					onDone()
				}
				return nil
			}
			go func() {
				err := stagefileclient.UpdateFile(file.ID, nameArg, folderArg, "", "", iconArg, 0)
				if err != nil {
					showToast(doc, parent, err.Error(), colRed)
					return
				}
				showToast(doc, parent,
					translate.T("stageFileUpdated", "File updated!"), colGreen)
				closeModal()
				if onDone != nil {
					onDone()
				}
			}()
			return nil
		}))

	// Enter key on name input → save.
	nameInput.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("key").String() == "Enter" {
				saveBtn.Call("click")
			}
			return nil
		}))

	btnRow.Call("appendChild", saveBtn)
	btnRow.Call("appendChild", cancelBtn)
	body.Call("appendChild", btnRow)
	card.Call("appendChild", body)

	modal.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("target").Equal(modal) {
				closeModal()
			}
			return nil
		}))

	modal.Call("appendChild", card)
	parent.Call("appendChild", modal)

	// Auto-focus the name input + select all for fast retype.
	nameInput.Call("focus")
	nameInput.Call("select")
}

// ─── Confirm dialog ───────────────────────────────────────────────────────────

// confirmAction opens a blocking confirmation dialog. Returns true if confirmed.
func confirmAction(doc js.Value, parent js.Value, title, itemName, warning string) bool {
	ch := make(chan bool, 1)

	modal := doc.Call("createElement", "div")
	modal.Get("style").Set("cssText",
		"position:absolute;top:0;left:0;width:100%;height:100%;"+
			"background:rgba(0,0,0,0.5);display:flex;"+
			"align-items:center;justify-content:center;z-index:100001;")

	card := doc.Call("createElement", "div")
	card.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;border:1px solid %s;border-radius:6px;"+
			"width:90%%;max-width:380px;overflow:hidden;",
		colBase, colSurface1))

	header := doc.Call("createElement", "div")
	header.Get("style").Set("cssText", fmt.Sprintf(
		"height:36px;background:%s;border-bottom:1px solid %s;"+
			"display:flex;align-items:center;padding:0 10px;",
		colSurface0, colSurface1))
	headerText := doc.Call("createElement", "span")
	headerText.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:12px;font-weight:600;", colRed))
	headerText.Set("textContent", title)
	header.Call("appendChild", headerText)
	card.Call("appendChild", header)

	body := doc.Call("createElement", "div")
	body.Get("style").Set("cssText", "padding:20px;")

	nameEl := doc.Call("createElement", "p")
	nameEl.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:14px;font-weight:600;margin:0 0 8px;", colPeach))
	nameEl.Set("textContent", itemName)
	body.Call("appendChild", nameEl)

	warnEl := doc.Call("createElement", "p")
	warnEl.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:12px;margin:0 0 20px;", colSubtext))
	warnEl.Set("textContent", warning)
	body.Call("appendChild", warnEl)

	btnRow := doc.Call("createElement", "div")
	btnRow.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;justify-content:flex-end;gap:8px;"+
			"border-top:1px solid %s;padding-top:16px;",
		colSurface1))

	delBtn := doc.Call("createElement", "button")
	delBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:none;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colRed, colBase))
	delBtn.Set("textContent", translate.T("stageFileConfirmDelete", "Delete"))

	cancelBtn := doc.Call("createElement", "button")
	cancelBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colSurface0, colText, colSurface1))
	cancelBtn.Set("textContent", translate.T("stageFileCancel", "Cancel"))

	closeModal := func() {
		if modal.Get("parentNode").Truthy() {
			modal.Get("parentNode").Call("removeChild", modal)
		}
	}

	delBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeModal()
			ch <- true
			return nil
		}))
	cancelBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeModal()
			ch <- false
			return nil
		}))

	btnRow.Call("appendChild", delBtn)
	btnRow.Call("appendChild", cancelBtn)
	body.Call("appendChild", btnRow)
	card.Call("appendChild", body)
	modal.Call("appendChild", card)
	parent.Call("appendChild", modal)

	return <-ch
}

// ─── Prompt input dialog ──────────────────────────────────────────────────────

// promptInput opens a blocking text input dialog. Returns the entered value
// or empty string on cancel.
func promptInput(doc js.Value, parent js.Value, title, placeholder string) string {
	ch := make(chan string, 1)

	modal := doc.Call("createElement", "div")
	modal.Get("style").Set("cssText",
		"position:absolute;top:0;left:0;width:100%;height:100%;"+
			"background:rgba(0,0,0,0.5);display:flex;"+
			"align-items:center;justify-content:center;z-index:100001;")

	card := doc.Call("createElement", "div")
	card.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;border:1px solid %s;border-radius:6px;"+
			"width:90%%;max-width:380px;overflow:hidden;",
		colBase, colSurface1))

	header := doc.Call("createElement", "div")
	header.Get("style").Set("cssText", fmt.Sprintf(
		"height:36px;background:%s;border-bottom:1px solid %s;"+
			"display:flex;align-items:center;padding:0 10px;",
		colSurface0, colSurface1))
	headerText := doc.Call("createElement", "span")
	headerText.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:12px;font-weight:600;", colText))
	headerText.Set("textContent", title)
	header.Call("appendChild", headerText)
	card.Call("appendChild", header)

	body := doc.Call("createElement", "div")
	body.Get("style").Set("cssText", "padding:20px;")

	input := doc.Call("createElement", "input")
	input.Set("type", "text")
	input.Set("value", placeholder)
	input.Get("style").Set("cssText", fmt.Sprintf(
		"width:100%%;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:10px;font-size:14px;min-height:44px;"+
			"font-family:sans-serif;outline:none;box-sizing:border-box;",
		colMantle, colText, colSurface1))
	body.Call("appendChild", input)

	btnRow := doc.Call("createElement", "div")
	btnRow.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;justify-content:flex-end;gap:8px;"+
			"margin-top:16px;"))

	okBtn := doc.Call("createElement", "button")
	okBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:none;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colBlue, colBase))
	okBtn.Set("textContent", "OK")

	cancelBtn := doc.Call("createElement", "button")
	cancelBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:10px 24px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;",
		colSurface0, colText, colSurface1))
	cancelBtn.Set("textContent", translate.T("stageFileCancel", "Cancel"))

	closeModal := func() {
		if modal.Get("parentNode").Truthy() {
			modal.Get("parentNode").Call("removeChild", modal)
		}
	}

	okBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			val := input.Get("value").String()
			closeModal()
			ch <- val
			return nil
		}))
	cancelBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeModal()
			ch <- ""
			return nil
		}))
	input.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("key").String() == "Enter" {
				okBtn.Call("click")
			}
			return nil
		}))

	btnRow.Call("appendChild", okBtn)
	btnRow.Call("appendChild", cancelBtn)
	body.Call("appendChild", btnRow)
	card.Call("appendChild", body)
	modal.Call("appendChild", card)
	parent.Call("appendChild", modal)

	// Focus and select all text.
	input.Call("focus")
	input.Call("select")

	return <-ch
}

// ─── Toast notification ──────────────────────────────────────────────────────

// showToast displays a brief notification at the bottom of the parent element.
func showToast(doc js.Value, parent js.Value, message, color string) {
	toast := doc.Call("createElement", "div")
	toast.Get("style").Set("cssText", fmt.Sprintf(
		"position:absolute;bottom:16px;left:50%%;transform:translateX(-50%%);"+
			"background:%s;color:%s;padding:10px 20px;border-radius:6px;"+
			"font-size:13px;font-weight:600;z-index:100002;"+
			"transition:opacity 0.3s;pointer-events:none;",
		color, colBase))
	toast.Set("textContent", message)
	parent.Call("appendChild", toast)

	// Auto-remove after 2 seconds.
	js.Global().Call("setTimeout",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			toast.Get("style").Set("opacity", "0")
			js.Global().Call("setTimeout",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					if toast.Get("parentNode").Truthy() {
						toast.Get("parentNode").Call("removeChild", toast)
					}
					return nil
				}), 300)
			return nil
		}), 2000)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// formatDate converts an RFC3339 timestamp to "YYYY-MM-DD HH:MM:SS" format.
func formatDate(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		if len(rfc3339) >= 10 {
			return rfc3339[:10]
		}
		return rfc3339
	}
	return t.Format("2006-01-02 15:04:05")
}

// attachDrag adds mouse+touch drag to a panel via its title bar.
func attachDrag(doc js.Value, titleBar js.Value, panel js.Value) {
	var isDragging bool
	var dragStartX, dragStartY float64
	var panelStartX, panelStartY float64

	startDrag := func(clientX, clientY float64) {
		isDragging = true
		dragStartX = clientX
		dragStartY = clientY
		rect := panel.Call("getBoundingClientRect")
		panelStartX = rect.Get("left").Float()
		panelStartY = rect.Get("top").Float()
		panel.Get("style").Set("position", "fixed")
		panel.Get("style").Set("left", fmt.Sprintf("%.0fpx", panelStartX))
		panel.Get("style").Set("top", fmt.Sprintf("%.0fpx", panelStartY))
		panel.Get("style").Set("transform", "none")
		panel.Get("style").Set("margin", "0")
	}

	moveDrag := func(clientX, clientY float64) {
		if !isDragging {
			return
		}
		dx := clientX - dragStartX
		dy := clientY - dragStartY
		newY := panelStartY + dy
		if newY < 0 {
			newY = 0
		}
		panel.Get("style").Set("left", fmt.Sprintf("%.0fpx", panelStartX+dx))
		panel.Get("style").Set("top", fmt.Sprintf("%.0fpx", newY))
	}

	titleBar.Call("addEventListener", "mousedown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			e := args[0]
			if e.Get("target").Get("tagName").String() == "BUTTON" {
				return nil
			}
			e.Call("preventDefault")
			startDrag(e.Get("clientX").Float(), e.Get("clientY").Float())
			return nil
		}))

	doc.Call("addEventListener", "mousemove",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if isDragging {
				args[0].Call("preventDefault")
				moveDrag(args[0].Get("clientX").Float(), args[0].Get("clientY").Float())
			}
			return nil
		}))

	doc.Call("addEventListener", "mouseup",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			isDragging = false
			return nil
		}))

	// Touch support.
	touchOpts := js.Global().Get("Object").New()
	touchOpts.Set("passive", false)

	titleBar.Call("addEventListener", "touchstart",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			e := args[0]
			if e.Get("target").Get("tagName").String() == "BUTTON" {
				return nil
			}
			e.Call("preventDefault")
			touch := e.Get("touches").Index(0)
			startDrag(touch.Get("clientX").Float(), touch.Get("clientY").Float())
			return nil
		}), touchOpts)

	doc.Call("addEventListener", "touchmove",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if isDragging {
				args[0].Call("preventDefault")
				touch := args[0].Get("touches").Index(0)
				moveDrag(touch.Get("clientX").Float(), touch.Get("clientY").Float())
			}
			return nil
		}), touchOpts)

	doc.Call("addEventListener", "touchend",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			isDragging = false
			return nil
		}))
}
