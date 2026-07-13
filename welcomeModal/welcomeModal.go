// /welcomeModal/welcomeModal.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package welcomeModal

import (
	"fmt"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/stagefileclient"
)

// =====================================================================
//  Result type
// =====================================================================

// Mode names the kind of action the user requested in the welcome
// modal. Returned inside Result.
//
// Português: Tipo de ação escolhida no modal.
type Mode string

const (
	// ModeNew — user clicked one of the "+ Go" / "+ C99" cards.
	// Caller proceeds to create a fresh workspace with the chosen
	// Language.
	ModeNew Mode = "new"

	// ModeOpen — user clicked an existing project in the recent list.
	// Caller proceeds to load the project. The Result carries FileID,
	// FileName, and Language pre-resolved.
	ModeOpen Mode = "open"

	// ModeRestore — user picked the "restore backup" card. Reserved
	// for Parcela 2c; ModeRestore is declared here so the consuming
	// switch in main.go can already have its case stub.
	ModeRestore Mode = "restore"
)

// Result is what Show returns to the caller. The fields valid for a
// given Mode are documented per-Mode below; reading a field that does
// not apply (e.g. FileID when Mode == ModeNew) yields the empty
// string and is not an error.
//
// Português: Resultado do modal. Campos válidos variam por Mode;
// ler campo inaplicável dá string vazia, não é erro.
type Result struct {
	// Mode is always set.
	Mode Mode

	// Target is the hardware-target REGISTRY id ("arduino_uno",
	// "esp32_c6", "pc_tablet") chosen on the C99 board step, or "" when
	// no board was picked (Go projects, dismissal, ModeOpen — the scene
	// carries its own). The ids mirror server/codegen/target/registry.go,
	// the single source of truth; the trio below is a hand-kept copy.
	// Português: O id de REGISTRO do target escolhido no passo de placa
	// do C99, ou "" quando nenhum (projetos Go, fechamento, ModeOpen). Os
	// ids espelham server/codegen/target/registry.go, a fonte única; o
	// trio abaixo é cópia mantida à mão.
	Target string

	// Language is always set ("c" or "go"). For ModeOpen it mirrors
	// the language of the chosen project; for ModeNew it mirrors the
	// card the user clicked; for ModeRestore it mirrors the backup's
	// language. For X / ESC dismissal it is the C99 default.
	Language string

	// FileID is set for ModeOpen and ModeRestore. Empty otherwise.
	FileID string

	// FileName is set for ModeOpen and ModeRestore. Empty otherwise.
	FileName string
}

// =====================================================================
//  Styling constants
// =====================================================================
//
// All styling lives here as package-level constants rather than in a
// separate rulesWelcomeModal package. The modal is shown once per
// session, by exactly one call site, and the visual rules are tightly
// coupled (overlay needs to know panel size, panel needs to know card
// grid). Spreading them across files would obscure the relationships.
//
// Colours mirror rulesViewManager.LangBadge* deliberately. The user
// sees the welcome modal cards in coral/blue, picks one, then sees
// the same coral/blue chip in the tab bar of the resulting project.
// Keeping the palette in sync makes the chip feel like a continuation
// of the choice rather than an unrelated UI element.
//
// Português: Constantes de estilo locais ao package. Cores espelham
// LangBadge* pro usuário associar a escolha do modal ao chip da
// tab bar.

const (
	// Overlay covers the whole viewport with a semi-transparent dark
	// layer, so the welcome modal stands out from whatever was on
	// screen before.
	overlayBackground = "rgba(0, 0, 0, 0.75)"
	overlayZIndex     = "9999"

	// Panel is the centred container that holds the welcome content.
	panelBackground   = "#1a1a2e"
	panelBorder       = "1px solid #2a2a3e"
	panelBorderRadius = "12px"
	panelPadding      = "32px 40px"
	panelMinWidth     = "520px"
	panelMaxWidth     = "680px"
	panelFontFamily   = "Arial, sans-serif"
	panelColor        = "#e8e8e8"
	panelBoxShadow    = "0 12px 48px rgba(0, 0, 0, 0.6)"

	// Header (title + close button).
	headerMarginBottom = "8px"
	titleFontSize      = "20px"
	titleFontWeight    = "600"
	closeButtonSize    = "28px"
	closeButtonColor   = "#888"
	closeButtonHover   = "#fff"
	closeButtonFont    = "20px"

	// Subtitle ("Open a recent project or start something new").
	subtitleFontSize     = "13px"
	subtitleColor        = "#bbb"
	subtitleMarginBottom = "20px"

	// Recent-section eyebrow ("RECENT PROJECTS").
	sectionHeaderFontSize      = "11px"
	sectionHeaderFontWeight    = "600"
	sectionHeaderLetterSpacing = "0.08em"
	sectionHeaderColor         = "#888"
	sectionHeaderMarginBottom  = "8px"
	sectionHeaderTextTransform = "uppercase"

	// Recent-list container — max height so very long lists scroll
	// instead of pushing the new-project cards off-screen.
	recentListMaxHeight    = "240px"
	recentListMarginBottom = "20px"

	// Each row in the recent list.
	rowPadding      = "10px 12px"
	rowBorderRadius = "8px"
	rowBgIdle       = "#222238"
	rowBgHover      = "#2c2c4a"
	rowCursor       = "pointer"
	rowMarginBottom = "6px"
	rowGap          = "10px"

	// Project name in a row.
	rowNameFontSize   = "14px"
	rowNameFontWeight = "500"
	rowNameColor      = "#e8e8e8"

	// Timestamp ("5 minutes ago") aligned to the right of a row.
	rowTimestampFontSize = "11px"
	rowTimestampColor    = "#888"

	// Chip inside a row (smaller than the tab-bar badge but same
	// colour vocabulary).
	rowChipPadding      = "2px 8px"
	rowChipBorderRadius = "999px"
	rowChipFontSize     = "10px"
	rowChipFontWeight   = "600"

	// Icon panel on the LEFT of a recent-list row. Mirrors the file
	// manager's icon chip but sized for the more compact modal row.
	// Background is deliberately a touch lighter than rowBgHover
	// (#2c2c4a) so the panel stays visible when the row is hovered.
	//
	// Português: Painel de ícone à ESQUERDA da row. Espelha o chip do
	// file manager, menor pra row compacta. Fundo um pouco mais claro
	// que rowBgHover pra não sumir no hover.
	rowIconSize     = "32px"
	rowIconBg       = "#383860"
	rowIconRadius   = "6px"
	rowIconColor    = "#c9c9e0"
	rowIconFontSize = "15px"

	// rowDefaultIcon mirrors stagefileui.fileDefaultIcon ("cube") so a
	// project without a chosen icon renders the same glyph in the
	// welcome list and the file manager.
	rowDefaultIcon = "cube"

	// "or start new" separator between the recent list and the
	// create-new cards. Only rendered when the recent list is
	// non-empty.
	separatorMargin        = "16px 0 16px 0"
	separatorLineColor     = "#333"
	separatorTextColor     = "#666"
	separatorTextFontSize  = "11px"
	separatorTextPadding   = "0 12px"
	separatorTextTransform = "uppercase"

	// Card grid (the 2 language cards live in a flex row).
	cardGridGap = "16px"

	// Each individual language card.
	cardPadding      = "20px 24px"
	cardBorderRadius = "10px"
	cardMinWidth     = "180px"
	cardFontSize     = "16px"
	cardFontWeight   = "600"
	cardCursor       = "pointer"
	cardTransition   = "transform 0.12s ease, box-shadow 0.12s ease"
	cardBorderWidth  = "2px"

	// Go card colour pair (matches LangBadgeBgGo / LangBadgeColorGo).
	cardBgGo     = "#85B7EB"
	cardColorGo  = "#042C53"
	cardBorderGo = "#5a8db8"

	// C99 card colour pair (matches LangBadgeBgC / LangBadgeColorC).
	cardBgC     = "#F0997B"
	cardColorC  = "#4A1B0C"
	cardBorderC = "#c47053"

	// Fade animation. Same duration on both ends so "appears" and
	// "disappears" feel symmetric. 180 ms — short enough not to
	// hold up fast users, long enough to register as a transition.
	fadeDurationMs = 180
	fadeTransition = "opacity 180ms ease"

	// Backup card. Distinct visual treatment so it does not blend
	// with the recent-list rows: amber palette signals "attention
	// needed" without being as alarming as red. The card sits at
	// the very top of the modal — above even the recent list —
	// because a pending backup is the highest-priority action a
	// returning user can take.
	//
	// Português: Card do backup. Paleta âmbar — chama atenção sem
	// alarmar. Aparece no topo do modal porque backup pendente é
	// a ação de maior prioridade pro usuário.
	backupCardBg           = "#3a2a18" // dark amber background
	backupCardBorder       = "1px solid #8a6a30"
	backupCardBorderRadius = "10px"
	backupCardPadding      = "14px 18px"
	backupCardMarginBottom = "20px"
	backupCardCursor       = "pointer"
	backupCardHoverBg      = "#46341e"
	backupCardHoverBorder  = "1px solid #c4923f"
	backupCardTransition   = "background 0.12s ease, border 0.12s ease"

	backupCardTitleColor      = "#f0c060"
	backupCardTitleFontSize   = "14px"
	backupCardTitleFontWeight = "600"
	backupCardTitleMarginBot  = "4px"

	backupCardBodyColor    = "#d8c4a0"
	backupCardBodyFontSize = "12px"
	backupCardBodyLineHt   = "1.5"

	backupCardCtaColor      = "#f0c060"
	backupCardCtaFontSize   = "12px"
	backupCardCtaFontWeight = "600"
	backupCardCtaMarginTop  = "8px"
)

// =====================================================================
//  Public API
// =====================================================================

// Show displays the welcome modal and blocks the calling goroutine
// until the user makes a choice or dismisses the modal. Returns a
// Result describing what the user chose.
//
// Listing of existing projects happens up front, before any DOM is
// built. A network failure during the listing is not fatal: the
// modal renders with an empty "Recent projects" section (or skips
// the section entirely) and offers only the new-project flow. The
// user is never blocked by an unreachable server.
//
// Dismissing the modal (X button or ESC key) returns Result with
// Mode=ModeNew and Language="c" — same default the server applies
// when stage_files.language is empty and the same fallback that
// Workspace.Init / ViewManager.Init use internally.
//
// IMPORTANT: Show must be called from the main goroutine. It uses
// channel communication with JavaScript event handlers (which run on
// the JS thread); the blocked goroutine is parked while JS event
// handlers wake it via the channel.
//
// Português: Mostra o modal, bloqueia até o usuário escolher.
// Retorna Result. Lista projetos antes de montar DOM; falha de rede
// na listagem não bloqueia. Chamar do main goroutine.
func Show() Result {
	// ── Pre-DOM: list projects and detect pending backup ──────────────────
	//
	// Done before any DOM work for two reasons:
	//
	//  - The two listings share the same network round-trip-eligible
	//    underlying call (stagefileclient.ListFiles); doing them
	//    together lets us scan the response once.
	//
	//  - If the calls block, the user keeps seeing the splash a bit
	//    longer rather than a half-built modal.
	//
	// Network failure is not fatal — both functions degrade to nil
	// (no recent list, no backup card) and the modal still offers
	// the new-project flow. The user is never blocked by an
	// unreachable server.
	//
	// Português: Listagem + detecção de backup antes do DOM. Falha
	// de rede degrada pra nil em ambos — modal sempre abre.
	projects, backup := loadProjectsAndBackup()

	doc := js.Global().Get("document")

	// ── Build the DOM tree ──────────────────────────────────────────────
	overlay := buildOverlay(doc)
	panel := buildPanel(doc)
	overlay.Call("appendChild", panel)

	closeBtn := buildCloseButton(doc)
	header := buildHeader(doc, closeBtn)
	panel.Call("appendChild", header)

	subtitle := buildSubtitle(doc, len(projects) > 0)
	panel.Call("appendChild", subtitle)

	choice := make(chan Result, 1)
	send := func(r Result) {
		select {
		case choice <- r:
		default:
		}
	}

	// Pending backup card — TOP of the modal, above the recent list.
	// A backup is the most time-sensitive item the user has: their
	// in-flight work from a previous session is one click away. We
	// surface it first.
	if backup != nil {
		panel.Call("appendChild", buildBackupCard(doc, backup, send))
	}

	if len(projects) > 0 {
		panel.Call("appendChild", buildSectionHeader(doc, "Recent projects"))
		panel.Call("appendChild", buildRecentList(doc, projects, send))
		panel.Call("appendChild", buildSeparator(doc, "or start new"))
	}

	cardGrid := buildCardGrid(doc)

	goCard := buildLanguageCard(doc, "+ Go project",
		cardBgGo, cardColorGo, cardBorderGo)
	cCard := buildLanguageCard(doc, "+ C99 project",
		cardBgC, cardColorC, cardBorderC)

	goCard.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			send(Result{
				Mode:     ModeNew,
				Language: stagefileclient.StageFileLanguageGo,
			})
			return nil
		}))
	// The C99 card opens a SECOND step — the board picker — instead of
	// resolving immediately: the maker's flow is "choose C99, choose the
	// board", and the min-target gate downstream needs the choice. The
	// trio mirrors server/codegen/target/registry.go (ladder classes:
	// avr < mcu32 < posix). "Back" restores the language step.
	// Português: O card C99 abre um SEGUNDO passo — o seletor de placa —
	// em vez de resolver na hora: o fluxo do maker é "escolhe C99,
	// escolhe a placa", e o portão de min-target adiante precisa da
	// escolha. O trio espelha o registro do server. "Back" volta.
	targetGrid := buildCardGrid(doc)
	targetGrid.Get("style").Set("display", "none")
	targetGrid.Get("style").Set("flexDirection", "column")
	type boardOpt struct{ id, title, desc string }
	boards := []boardOpt{
		{"pc_tablet", "PC / tablet",
			"Desktop program — try it on your computer first. POSIX class."},
		{"esp32_c6", "ESP32-C6",
			"RISC-V, 512 KB RAM — Wi-Fi and networked projects. 32-bit MCU class."},
		{"arduino_uno", "Arduino UNO",
			"8-bit AVR, 2 KB RAM — classic boards, tight memory. AVR class."},
	}
	for _, b := range boards {
		id := b.id
		card := buildTargetCard(doc, b.title, b.desc)
		card.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				send(Result{
					Mode:     ModeNew,
					Language: stagefileclient.StageFileLanguageC,
					Target:   id,
				})
				return nil
			}))
		targetGrid.Call("appendChild", card)
	}
	backLink := buildBackLink(doc)
	backLink.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			targetGrid.Get("style").Set("display", "none")
			cardGrid.Get("style").Set("display", "flex")
			return nil
		}))
	targetGrid.Call("appendChild", backLink)

	cCard.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			cardGrid.Get("style").Set("display", "none")
			targetGrid.Get("style").Set("display", "flex")
			return nil
		}))

	cardGrid.Call("appendChild", goCard)
	cardGrid.Call("appendChild", cCard)
	panel.Call("appendChild", cardGrid)
	panel.Call("appendChild", targetGrid)

	body := doc.Get("body")

	overlay.Get("style").Set("opacity", "0")
	overlay.Get("style").Set("transition", fadeTransition)
	body.Call("appendChild", overlay)

	dismissResult := Result{
		Mode:     ModeNew,
		Language: stagefileclient.StageFileLanguageC,
	}
	closeBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			send(dismissResult)
			return nil
		}))

	escFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("key").String() == "Escape" {
			send(dismissResult)
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", escFn, true)

	js.Global().Call("requestAnimationFrame", js.FuncOf(
		func(this js.Value, args []js.Value) interface{} {
			overlay.Get("style").Set("opacity", "1")
			closeBtn.Call("focus")
			return nil
		}))

	result := <-choice

	doc.Call("removeEventListener", "keydown", escFn, true)
	overlay.Get("style").Set("opacity", "0")

	done := make(chan struct{}, 1)
	js.Global().Call("setTimeout", js.FuncOf(
		func(this js.Value, args []js.Value) interface{} {
			body.Call("removeChild", overlay)
			done <- struct{}{}
			return nil
		}), fadeDurationMs)
	<-done

	return result
}

// =====================================================================
//  Project listing + backup detection
// =====================================================================

// loadProjectsAndBackup performs a single ListFiles call and splits
// the response into two views the modal needs:
//
//   - projects: every non-backup stage file the user owns, in the
//     order the server returned them (already updated_at DESC).
//
//   - backup: the most recent backup file, if any. nil when no
//     backup exists. The server's ORDER BY (is_backup ASC,
//     updated_at DESC) ranks all non-backups before any backups,
//     and within the backup section the most recent comes first —
//     so the first IsBackup row encountered after the non-backup
//     prefix is the one we want.
//
// Network failure collapses both return values to their zero forms
// (empty slice, nil pointer). The modal handles that case by
// skipping the recent section and the backup card entirely; it
// still renders the new-project flow so the user can always
// proceed.
//
// One call, two views: previously this file had a separate
// loadRecentProjects function that also filtered IsBackup. The
// pending-backup detection added in Parcela 2c made the duplicate
// scan unnecessary — a single pass through the same response is
// strictly better.
//
// Português: Uma chamada ListFiles, duas views — projetos normais
// (sem backups) e o backup mais recente (ou nil). Falha de rede vira
// (nil, nil). Substituiu loadRecentProjects, que duplicava o scan.
func loadProjectsAndBackup() ([]stagefileclient.StageFileEntry, *stagefileclient.StageFileEntry) {
	files, err := stagefileclient.ListFiles("")
	if err != nil {
		return nil, nil
	}
	projects := make([]stagefileclient.StageFileEntry, 0, len(files))
	var backup *stagefileclient.StageFileEntry
	for i := range files {
		if files[i].IsBackup {
			// Keep only the FIRST backup row, which is the most
			// recent because the server orders backups by
			// updated_at DESC after the non-backup prefix.
			if backup == nil {
				backup = &files[i]
			}
			continue
		}
		projects = append(projects, files[i])
	}
	return projects, backup
}

// =====================================================================
//  DOM construction helpers
// =====================================================================

func buildOverlay(doc js.Value) js.Value {
	overlay := doc.Call("createElement", "div")
	overlay.Set("id", "welcomeModalOverlay")
	style := overlay.Get("style")
	style.Set("position", "fixed")
	style.Set("top", "0")
	style.Set("left", "0")
	style.Set("width", "100vw")
	style.Set("height", "100vh")
	style.Set("background", overlayBackground)
	style.Set("zIndex", overlayZIndex)
	style.Set("display", "flex")
	style.Set("alignItems", "center")
	style.Set("justifyContent", "center")
	return overlay
}

func buildPanel(doc js.Value) js.Value {
	panel := doc.Call("createElement", "div")
	panel.Set("id", "welcomeModalPanel")
	style := panel.Get("style")
	style.Set("background", panelBackground)
	style.Set("border", panelBorder)
	style.Set("borderRadius", panelBorderRadius)
	style.Set("padding", panelPadding)
	style.Set("minWidth", panelMinWidth)
	style.Set("maxWidth", panelMaxWidth)
	style.Set("fontFamily", panelFontFamily)
	style.Set("color", panelColor)
	style.Set("boxShadow", panelBoxShadow)
	return panel
}

func buildHeader(doc js.Value, closeBtn js.Value) js.Value {
	header := doc.Call("createElement", "div")
	style := header.Get("style")
	style.Set("display", "flex")
	style.Set("alignItems", "center")
	style.Set("justifyContent", "space-between")
	style.Set("marginBottom", headerMarginBottom)

	title := doc.Call("createElement", "div")
	// i18n note: literals stay inline until translation files are
	// loaded by this point in the boot. Replace with translate.T
	// once that ordering is sorted out.
	title.Set("textContent", "Welcome to IoTMaker")
	tStyle := title.Get("style")
	tStyle.Set("fontSize", titleFontSize)
	tStyle.Set("fontWeight", titleFontWeight)

	header.Call("appendChild", title)
	header.Call("appendChild", closeBtn)
	return header
}

func buildCloseButton(doc js.Value) js.Value {
	btn := doc.Call("createElement", "button")
	btn.Set("textContent", "×") // unicode multiplication sign
	btn.Set("title", "Close (ESC)")
	style := btn.Get("style")
	style.Set("width", closeButtonSize)
	style.Set("height", closeButtonSize)
	style.Set("background", "transparent")
	style.Set("border", "none")
	style.Set("color", closeButtonColor)
	style.Set("fontSize", closeButtonFont)
	style.Set("cursor", "pointer")
	style.Set("outline", "none")
	style.Set("padding", "0")
	style.Set("lineHeight", "1")
	btn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			btn.Get("style").Set("color", closeButtonHover)
			return nil
		}))
	btn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			btn.Get("style").Set("color", closeButtonColor)
			return nil
		}))
	return btn
}

// buildSubtitle adapts its copy to whether the user has any recent
// projects. Empty case explains the choice; has-projects case invites
// both flows ("Open a recent project or start something new").
//
// Português: Texto do subtítulo varia conforme tem ou não projetos
// recentes.
func buildSubtitle(doc js.Value, hasRecent bool) js.Value {
	sub := doc.Call("createElement", "div")
	if hasRecent {
		sub.Set("textContent", "Open a recent project or start something new.")
	} else {
		sub.Set("textContent", "Choose the target language for your new project. This choice is permanent.")
	}
	style := sub.Get("style")
	style.Set("fontSize", subtitleFontSize)
	style.Set("color", subtitleColor)
	style.Set("marginBottom", subtitleMarginBottom)
	return sub
}

// buildSectionHeader creates the small uppercase eyebrow text above
// the recent-list section.
func buildSectionHeader(doc js.Value, text string) js.Value {
	h := doc.Call("createElement", "div")
	h.Set("textContent", text)
	style := h.Get("style")
	style.Set("fontSize", sectionHeaderFontSize)
	style.Set("fontWeight", sectionHeaderFontWeight)
	style.Set("letterSpacing", sectionHeaderLetterSpacing)
	style.Set("color", sectionHeaderColor)
	style.Set("marginBottom", sectionHeaderMarginBottom)
	style.Set("textTransform", sectionHeaderTextTransform)
	return h
}

// buildRecentList creates a scrollable column of project rows. Each
// row is clickable and sends a ModeOpen Result on click.
//
// The per-iteration `p := projects[i]` copy is needed even on Go 1.22+
// where the loop-var scoping was changed: js.FuncOf closures may
// outlive the loop iteration, and explicit re-binding documents the
// intent regardless of toolchain.
//
// Português: Coluna scrollável de rows clicáveis. Click envia
// Result com Mode=ModeOpen.
func buildRecentList(doc js.Value, projects []stagefileclient.StageFileEntry, send func(Result)) js.Value {
	list := doc.Call("createElement", "div")
	style := list.Get("style")
	style.Set("maxHeight", recentListMaxHeight)
	style.Set("overflowY", "auto")
	style.Set("marginBottom", recentListMarginBottom)

	for i := range projects {
		p := projects[i]
		row := buildProjectRow(doc, p)
		row.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				send(Result{
					Mode:     ModeOpen,
					Language: resolveLanguage(p.Language),
					FileID:   p.ID,
					FileName: p.Name,
				})
				return nil
			}))
		list.Call("appendChild", row)
	}
	return list
}

// buildProjectRow renders a single recent-list row. Layout, left to
// right: the maker's chosen icon, the project name (flex), the
// language chip, and the relative timestamp. Icon-left / chip-right
// mirrors the file manager listing so a project reads the same in
// both surfaces.
//
// Português: Row da lista recente. Da esquerda pra direita: ícone
// escolhido pelo maker, nome (flex), chip de linguagem e timestamp
// relativo. Ícone à esquerda / chip à direita igual ao file manager.
func buildProjectRow(doc js.Value, p stagefileclient.StageFileEntry) js.Value {
	row := doc.Call("createElement", "div")
	style := row.Get("style")
	style.Set("display", "flex")
	style.Set("alignItems", "center")
	style.Set("padding", rowPadding)
	style.Set("background", rowBgIdle)
	style.Set("borderRadius", rowBorderRadius)
	style.Set("cursor", rowCursor)
	style.Set("marginBottom", rowMarginBottom)
	style.Set("gap", rowGap)

	row.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			row.Get("style").Set("background", rowBgHover)
			return nil
		}))
	row.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			row.Get("style").Set("background", rowBgIdle)
			return nil
		}))

	// Chosen icon on the LEFT — mirrors the file manager so the same
	// project reads identically in both surfaces. Empty IconID falls
	// back to "cube" inside buildProjectIcon.
	icon := buildProjectIcon(doc, p.IconID)
	row.Call("appendChild", icon)

	// Project name — flex: 1 absorbs the slack between the icon and
	// the right-aligned chip + timestamp.
	name := doc.Call("createElement", "div")
	name.Set("textContent", p.Name)
	nStyle := name.Get("style")
	nStyle.Set("flex", "1 1 0")
	nStyle.Set("fontSize", rowNameFontSize)
	nStyle.Set("fontWeight", rowNameFontWeight)
	nStyle.Set("color", rowNameColor)
	nStyle.Set("overflow", "hidden")
	nStyle.Set("textOverflow", "ellipsis")
	nStyle.Set("whiteSpace", "nowrap")
	row.Call("appendChild", name)

	// Language chip on the RIGHT, immediately before the timestamp.
	chip := buildLanguageChip(doc, p.Language)
	row.Call("appendChild", chip)

	stamp := doc.Call("createElement", "div")
	stamp.Set("textContent", relativeTime(p.UpdatedAt))
	sStyle := stamp.Get("style")
	sStyle.Set("fontSize", rowTimestampFontSize)
	sStyle.Set("color", rowTimestampColor)
	row.Call("appendChild", stamp)

	return row
}

// buildLanguageChip is a smaller cousin of the tab-bar badge,
// suitable for inline rendering inside a recent-list row.
//
// Unknown tokens (e.g. a language added server-side that the WASM
// hasn't been rebuilt for) render as the raw token in neutral grey,
// so the row is still usable and the anomaly is visible during
// development.
//
// Português: Chip menor que o badge da tab bar; usado dentro das
// rows da lista. Token desconhecido renderiza como o próprio token
// em cinza neutro.
func buildLanguageChip(doc js.Value, lang string) js.Value {
	var label, bg, color string
	switch lang {
	case stagefileclient.StageFileLanguageGo:
		label = "Go"
		bg = cardBgGo
		color = cardColorGo
	case stagefileclient.StageFileLanguageC, "":
		// Empty mirrors the server default; render as C99 because
		// that's what the server resolves "" to.
		label = "C99"
		bg = cardBgC
		color = cardColorC
	default:
		label = lang
		bg = "#444"
		color = "#bbb"
	}
	chip := doc.Call("createElement", "span")
	chip.Set("textContent", label)
	style := chip.Get("style")
	style.Set("padding", rowChipPadding)
	style.Set("borderRadius", rowChipBorderRadius)
	style.Set("fontSize", rowChipFontSize)
	style.Set("fontWeight", rowChipFontWeight)
	style.Set("background", bg)
	style.Set("color", color)
	style.Set("flex", "0 0 auto")
	return chip
}

// buildProjectIcon renders the small icon panel shown on the LEFT of
// a recent-list row: a rounded surface holding the maker's chosen
// FontAwesome icon. Empty IconID falls back to rowDefaultIcon
// ("cube"), matching the file manager so the same project looks
// identical in both places.
//
// Português: Painel de ícone à ESQUERDA da row — superfície
// arredondada com o ícone FA escolhido pelo maker. IconID vazio cai
// em rowDefaultIcon ("cube"), igual ao file manager.
func buildProjectIcon(doc js.Value, iconID string) js.Value {
	name := iconID
	if name == "" {
		name = rowDefaultIcon
	}
	wrap := doc.Call("createElement", "div")
	style := wrap.Get("style")
	style.Set("width", rowIconSize)
	style.Set("height", rowIconSize)
	style.Set("flex", "0 0 auto")
	style.Set("display", "flex")
	style.Set("alignItems", "center")
	style.Set("justifyContent", "center")
	style.Set("background", rowIconBg)
	style.Set("borderRadius", rowIconRadius)
	style.Set("color", rowIconColor)
	style.Set("fontSize", rowIconFontSize)

	glyph := doc.Call("createElement", "i")
	glyph.Set("className", fmt.Sprintf("%s fa-%s", faIconClass(name), name))
	glyph.Call("setAttribute", "aria-hidden", "true")
	wrap.Call("appendChild", glyph)
	return wrap
}

// faIconClass resolves the FontAwesome family ("fa-solid" /
// "fa-regular" / "fa-brands") for an icon name by consulting the
// window.FA_FREE_STYLES table the host page publishes. It mirrors the
// identical resolver in stagefileui (icon picker + file manager) so a
// glyph chosen there renders with the same family here. A missing
// table or an unknown name defaults to fa-solid.
//
// Português: Resolve a família FA do ícone via window.FA_FREE_STYLES
// (mesma lógica do stagefileui). Tabela ausente / nome desconhecido
// → fa-solid.
func faIconClass(name string) string {
	if name == "" {
		return "fa-solid"
	}
	obj := js.Global().Get("FA_FREE_STYLES")
	if !obj.Truthy() {
		return "fa-solid"
	}
	styles := obj.Get(name)
	if !styles.Truthy() {
		return "fa-solid"
	}
	hasBrands := false
	hasSolid := false
	for i := 0; i < styles.Length(); i++ {
		switch styles.Index(i).String() {
		case "brands":
			hasBrands = true
		case "solid":
			hasSolid = true
		}
	}
	if hasBrands {
		return "fa-brands"
	}
	if hasSolid {
		return "fa-solid"
	}
	return "fa-regular"
}

// buildBackupCard renders the amber notice that surfaces a pending
// backup. Sits at the top of the modal (above the recent list) and
// is clickable as a single large hit area — no separate "Restore"
// button, because the entire card IS the restore affordance.
//
// What the card shows:
//
//   - Title: "Backup found"
//   - Body line: derived from the backup row's name. The convention
//     established by saveBackup is "OriginalName (backup)", so we
//     strip that suffix and reconstruct the user-facing name.
//   - Timestamp: "Last edited X ago" (relative).
//   - Language chip: backup inherits its parent project's language,
//     so the chip is informative without being redundant — the user
//     sees they're about to restore a Go vs C99 project.
//   - CTA line: "Click to restore your work."
//
// Click publishes Result{Mode: ModeRestore, FileID, FileName,
// Language}. main.go routes ModeRestore to vm.RestoreBackup, which
// loads the scene and points currentFile at the original (not at
// the backup itself), so the next Ctrl+S overwrites the original
// and the backup is deleted by the OnAfterSave cleanup that already
// exists.
//
// Português: Card âmbar de backup pendente. Card inteiro é
// clicável — sem botão separado. Mostra título, nome original
// (strip do " (backup)"), timestamp relativo e chip de linguagem.
// Click envia Result{ModeRestore, ...}; main.go chama
// vm.RestoreBackup, que aponta currentFile pro ORIGINAL.
func buildBackupCard(doc js.Value, backup *stagefileclient.StageFileEntry, send func(Result)) js.Value {
	card := doc.Call("createElement", "div")
	style := card.Get("style")
	style.Set("background", backupCardBg)
	style.Set("border", backupCardBorder)
	style.Set("borderRadius", backupCardBorderRadius)
	style.Set("padding", backupCardPadding)
	style.Set("marginBottom", backupCardMarginBottom)
	style.Set("cursor", backupCardCursor)
	style.Set("transition", backupCardTransition)

	card.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			s := card.Get("style")
			s.Set("background", backupCardHoverBg)
			s.Set("border", backupCardHoverBorder)
			return nil
		}))
	card.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			s := card.Get("style")
			s.Set("background", backupCardBg)
			s.Set("border", backupCardBorder)
			return nil
		}))

	// Title row — icon glyph + "Backup found".
	title := doc.Call("createElement", "div")
	title.Set("textContent", "⚠  Backup found")
	tStyle := title.Get("style")
	tStyle.Set("color", backupCardTitleColor)
	tStyle.Set("fontSize", backupCardTitleFontSize)
	tStyle.Set("fontWeight", backupCardTitleFontWeight)
	tStyle.Set("marginBottom", backupCardTitleMarginBot)
	card.Call("appendChild", title)

	// Body line — name + timestamp + chip, laid out in a flex row.
	bodyRow := doc.Call("createElement", "div")
	bStyle := bodyRow.Get("style")
	bStyle.Set("display", "flex")
	bStyle.Set("alignItems", "center")
	bStyle.Set("gap", "10px")
	bStyle.Set("color", backupCardBodyColor)
	bStyle.Set("fontSize", backupCardBodyFontSize)
	bStyle.Set("lineHeight", backupCardBodyLineHt)

	// Strip the " (backup)" suffix so the user sees the original
	// project name they recognise. Reusing the same convention
	// saveBackup writes with — "OriginalName (backup)".
	originalName := strings.TrimSuffix(backup.Name, " (backup)")

	nameText := doc.Call("createElement", "span")
	nameText.Set("textContent", fmt.Sprintf("Unsaved changes from %q", originalName))
	nStyle := nameText.Get("style")
	nStyle.Set("flex", "1 1 0")
	nStyle.Set("overflow", "hidden")
	nStyle.Set("textOverflow", "ellipsis")
	nStyle.Set("whiteSpace", "nowrap")
	bodyRow.Call("appendChild", nameText)

	chip := buildLanguageChip(doc, backup.Language)
	bodyRow.Call("appendChild", chip)

	card.Call("appendChild", bodyRow)

	// Sub-line — "Last edited X ago".
	sub := doc.Call("createElement", "div")
	sub.Set("textContent", "Last edited "+relativeTime(backup.UpdatedAt))
	subStyle := sub.Get("style")
	subStyle.Set("color", backupCardBodyColor)
	subStyle.Set("fontSize", backupCardBodyFontSize)
	subStyle.Set("opacity", "0.85")
	subStyle.Set("marginTop", "2px")
	card.Call("appendChild", sub)

	// CTA line — emphasised so it reads as the action.
	cta := doc.Call("createElement", "div")
	cta.Set("textContent", "Click to restore your work.")
	ctaStyle := cta.Get("style")
	ctaStyle.Set("color", backupCardCtaColor)
	ctaStyle.Set("fontSize", backupCardCtaFontSize)
	ctaStyle.Set("fontWeight", backupCardCtaFontWeight)
	ctaStyle.Set("marginTop", backupCardCtaMarginTop)
	card.Call("appendChild", cta)

	// The whole card is the click target — single hit area is
	// simpler for the user than a small "Restore" button and the
	// amber styling already telegraphs "this is interactive".
	card.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			send(Result{
				Mode:     ModeRestore,
				Language: resolveLanguage(backup.Language),
				FileID:   backup.ID,
				FileName: backup.Name,
			})
			return nil
		}))

	return card
}

// buildSeparator creates a thin line with centred text overlapping
// it, e.g. "── or start new ──". Used between the recent list and
// the create-new cards.
func buildSeparator(doc js.Value, text string) js.Value {
	sep := doc.Call("createElement", "div")
	style := sep.Get("style")
	style.Set("display", "flex")
	style.Set("alignItems", "center")
	style.Set("margin", separatorMargin)

	lineL := doc.Call("createElement", "div")
	lStyle := lineL.Get("style")
	lStyle.Set("flex", "1 1 0")
	lStyle.Set("borderTop", "1px solid "+separatorLineColor)

	t := doc.Call("createElement", "span")
	t.Set("textContent", text)
	tStyle := t.Get("style")
	tStyle.Set("fontSize", separatorTextFontSize)
	tStyle.Set("color", separatorTextColor)
	tStyle.Set("textTransform", separatorTextTransform)
	tStyle.Set("padding", separatorTextPadding)

	lineR := doc.Call("createElement", "div")
	rStyle := lineR.Get("style")
	rStyle.Set("flex", "1 1 0")
	rStyle.Set("borderTop", "1px solid "+separatorLineColor)

	sep.Call("appendChild", lineL)
	sep.Call("appendChild", t)
	sep.Call("appendChild", lineR)
	return sep
}

func buildCardGrid(doc js.Value) js.Value {
	grid := doc.Call("createElement", "div")
	style := grid.Get("style")
	style.Set("display", "flex")
	style.Set("gap", cardGridGap)
	style.Set("flexWrap", "wrap")
	return grid
}

// buildLanguageCard creates a single clickable card for one language.
// Hover lifts the card by 1px and bumps the shadow — pure visual.
// buildTargetCard renders one board option of the C99 second step: a bold
// title line and a muted one-line description — richer than the language
// card because the maker is choosing HARDWARE, and the RAM/class hint is
// the whole point of the step.
// Português: Um cartão de placa do segundo passo do C99: título em negrito
// e descrição de uma linha — mais rico que o cartão de linguagem porque o
// maker está escolhendo HARDWARE, e a dica de RAM/classe é o ponto do
// passo.
func buildTargetCard(doc js.Value, title, desc string) js.Value {
	card := doc.Call("createElement", "button")
	style := card.Get("style")
	style.Set("width", "100%")
	style.Set("padding", "12px 16px")
	style.Set("background", cardBgC)
	style.Set("color", cardColorC)
	style.Set("border", "1px solid "+cardBorderC)
	style.Set("borderRadius", "10px")
	style.Set("cursor", "pointer")
	style.Set("textAlign", "left")
	style.Set("fontFamily", "inherit")
	style.Set("marginBottom", "8px")

	t := doc.Call("createElement", "div")
	t.Set("textContent", title)
	t.Get("style").Set("fontWeight", "700")
	t.Get("style").Set("fontSize", "15px")
	card.Call("appendChild", t)

	d := doc.Call("createElement", "div")
	d.Set("textContent", desc)
	d.Get("style").Set("fontSize", "12px")
	d.Get("style").Set("opacity", "0.75")
	d.Get("style").Set("marginTop", "3px")
	card.Call("appendChild", d)
	return card
}

// buildBackLink is the target step's escape hatch back to the language
// choice. Português: A volta do passo de placa para a escolha de linguagem.
func buildBackLink(doc js.Value) js.Value {
	link := doc.Call("createElement", "button")
	link.Set("textContent", "\u2190 back")
	style := link.Get("style")
	style.Set("background", "none")
	style.Set("border", "none")
	style.Set("color", cardColorC)
	style.Set("cursor", "pointer")
	style.Set("fontSize", "13px")
	style.Set("padding", "6px 0 0")
	style.Set("textAlign", "center")
	style.Set("width", "100%")
	style.Set("fontFamily", "inherit")
	return link
}

func buildLanguageCard(doc js.Value, label, bg, color, border string) js.Value {
	card := doc.Call("createElement", "button")
	card.Set("textContent", label)
	style := card.Get("style")
	style.Set("flex", "1 1 0")
	style.Set("minWidth", cardMinWidth)
	style.Set("padding", cardPadding)
	style.Set("background", bg)
	style.Set("color", color)
	style.Set("border", cardBorderWidth+" solid "+border)
	style.Set("borderRadius", cardBorderRadius)
	style.Set("fontSize", cardFontSize)
	style.Set("fontWeight", cardFontWeight)
	style.Set("fontFamily", panelFontFamily)
	style.Set("cursor", cardCursor)
	style.Set("outline", "none")
	style.Set("transition", cardTransition)
	style.Set("textAlign", "center")

	card.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			s := card.Get("style")
			s.Set("transform", "translateY(-1px)")
			s.Set("boxShadow", "0 4px 16px rgba(0, 0, 0, 0.4)")
			return nil
		}))
	card.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			s := card.Get("style")
			s.Set("transform", "translateY(0)")
			s.Set("boxShadow", "none")
			return nil
		}))

	return card
}

// =====================================================================
//  Small helpers
// =====================================================================

// resolveLanguage normalises the language string from a project row
// before publishing it on the Result. Empty means "C99 by default"
// (mirrors the server-side schema default). Anything else passes
// through — even an unrecognised token, so the caller can decide
// how to handle a future language gracefully.
//
// Português: Normaliza linguagem do projeto. Vazio vira "c";
// resto passa direto.
func resolveLanguage(lang string) string {
	if lang == "" {
		return stagefileclient.StageFileLanguageC
	}
	return lang
}

// relativeTime renders an RFC3339 timestamp as a short, human-friendly
// "X ago" string. Falls back to the raw string on parse error — better
// to show something the user can correlate with the server than to
// hide the row entirely.
//
// Buckets chosen to be informative without precision overkill: minutes
// up to an hour, then hours up to a day, then days up to a week, then
// weeks. Anything older than ~8 weeks shows the YYYY-MM-DD date.
//
// Português: Formata timestamp em "X atrás". Fallback pro raw se
// parse falha. Buckets: minutos, horas, dias, semanas, depois data.
func relativeTime(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		// Defensive: show whatever the server gave us rather than
		// hiding the row.
		return rfc3339
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return formatAgo(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return formatAgo(int(d/time.Hour), "hour")
	case d < 7*24*time.Hour:
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "yesterday"
		}
		return formatAgo(days, "day")
	case d < 8*7*24*time.Hour:
		return formatAgo(int(d/(7*24*time.Hour)), "week")
	default:
		return t.Format("2006-01-02")
	}
}

// formatAgo joins the number, the unit (pluralised if needed) and
// the trailing "ago" word.
func formatAgo(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s ago", n, unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}
