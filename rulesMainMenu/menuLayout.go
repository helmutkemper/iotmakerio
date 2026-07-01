// rulesMainMenu/menuLayout.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesMainMenu

// menuLayout.go — Radial layout engine for the black-box function hex menu.
//
// English:
//
//	ApplyRadialLayout assigns (Col, Row) values to []hexMenu.MenuItem entries
//	that do not have an explicit position set by the specialist (item.MenuPosSet).
//
//	Design principles:
//	  - Back is always at (0, 0) relative to the center — absolute (2, 2).
//	  - Items with MenuPosSet=true keep their declared position unchanged.
//	  - Items without MenuPosSet receive a position from a fixed priority ring:
//	      ring slot 0 → (-1,-1)   ring slot 1 → (-1,+1)
//	      ring slot 2 → (+1,-1)   ring slot 3 → (+1,+1)
//	      ring slot 4 → ( 0,-2)   ring slot 5 → ( 0,+2)
//	    Beyond 6 auto-placed items, additional slots expand outward following
//	    the same ring pattern at a larger radius.
//	  - All offsets are relative to the Back center (0,0).
//	    The absolute grid position is: abs = center + offset.
//	    Default center = (2, 2), so offset (-1,-1) maps to absolute (1, 1).
//	  - When the total number of user items (Init + methods, excluding Back)
//	    exceeds MaxItemsPerPage, the last slot on each page is replaced by a
//	    "More →" submenu item that holds the overflow entries. This is
//	    recursive — each overflow page follows the same rules.
//
//	Why (0,0) = Back and not (2,2)?
//	  Using relative offsets decouples the layout math from the absolute
//	  center position. The center can be moved (e.g. to (3,3) for a larger
//	  menu) without changing any offset constants.
//
//	Flat-top hex grid reminder:
//	  Same column: adjacent rows are ±2 apart (share a flat edge).
//	  Adjacent column: adjacent items have rows ±1 apart (share a vertex).
//	  Columns 1,3,5,… are the "main" columns; 2,4,6,… sit between them.
//	  Using only odd cols keeps all items on the same visual row level.
//
// Português:
//
//	ApplyRadialLayout atribui (Col, Row) aos itens do menu que não têm
//	posição explícita definida pelo especialista.
//
//	Back sempre fica no centro (0,0) relativo → absoluto (2,2).
//	Itens com MenuPosSet=true mantêm sua posição declarada.
//	Os demais recebem posição automática do anel de prioridade acima.
//	Quando o total excede MaxItemsPerPage, o último slot vira "More →".

import (
	"github.com/helmutkemper/iotmakerio/hexMenu"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/translate"
)

// ─── Configuration ────────────────────────────────────────────────────────────

// MaxItemsPerPage is the maximum number of non-Back items visible on a single
// hex menu page before overflow is paginated into a "More →" submenu.
//
// The value must be ≥ 1. The practical useful range is 3–8.
//   - 6 fits neatly in the two-ring radial layout without any wasted slots.
//   - Increase if the target screen is always large (desktop-only product).
//   - Decrease if the product targets small tablets where the menu looks dense.
//
// Changing this variable before menu construction affects all black-box function
// submenus globally — no recompile needed for different environments.
//
// Português:
//
//	Número máximo de itens não-Back visíveis em uma página do menu hex antes
//	de paginar o excesso em um submenu "More →".
var MaxItemsPerPage = 6

// BackCenterCol is the absolute hex grid column of the Back button.
// All radial offset calculations are relative to this value.
//
// Must be an even-offset position compatible with the flat-top hex grid.
// Default (2) works for menus with items at offsets ±1 from the center.
var BackCenterCol = 2

// BackCenterRow is the absolute hex grid row of the Back button.
// All radial offset calculations are relative to this value.
var BackCenterRow = 2

// ─── Radial slot table ────────────────────────────────────────────────────────

// radialOffset is a signed (col, row) offset relative to the Back center.
// Applied as: absCol = BackCenterCol + offset.Col
//
// Priority order matters: the IDE places Init first (slot 0), then each named
// method in source-file order. Slots are assigned in this fixed sequence so
// the layout is deterministic regardless of how many items are present.
//
// Visual diagram (Back = center B):
//
//	   A   C          A = slot 4 ( 0,-2)    C = slot 2 (+1,-1)
//	0    B    2       0 = slot 0 (-1,-1)    2 = slot 1 (-1,+1) ← note: these are
//	   1   3          1 = slot 1 (-1,+1)    3 = slot 3 (+1,+1)   indexed in the
//	       E          E = slot 5 ( 0,+2)                          type below
//
// Flat-top hex adjacency: items at odd columns touch Back on flat edges.
// Items at even columns touch on vertices — slightly further away visually.
//
// Português:
//
//	Tabela de offsets radiais. Cada entrada é aplicada como
//	(BackCenterCol + Col, BackCenterRow + Row) para obter a posição absoluta.
type radialOffset struct {
	Col int
	Row int
}

// radialSlots is the priority ring of offsets around the Back center.
// Slots 0–5 form the first ring (6 positions). Beyond slot 5, the ring
// repeats at distance 2 (second ring: 12 more positions, reaching ±3).
//
// The sequence is chosen so that the first two items (Init + first method)
// land symmetrically above Back, then fill outward evenly.
var radialSlots = []radialOffset{
	{-1, -1}, // slot 0: upper-left  of center
	{-1, +1}, // slot 1: lower-left  of center
	{+1, -1}, // slot 2: upper-right of center
	{+1, +1}, // slot 3: lower-right of center
	{0, -2},  // slot 4: directly above center
	{0, +2},  // slot 5: directly below center
	// Second ring (reached only when > 6 items and no overflow pagination)
	{-2, -2}, // slot 6
	{-2, 0},  // slot 7
	{-2, +2}, // slot 8
	{+2, -2}, // slot 9
	{+2, 0},  // slot 10
	{+2, +2}, // slot 11
}

// ─── Public API ───────────────────────────────────────────────────────────────

// ApplyRadialLayout assigns grid positions to a flat list of user items
// (Init + named methods) and wraps them into a paginated menu hierarchy.
//
// The returned slice is ready to be used as the Submenu of the component's
// Hardware entry. It already contains the GoBack item at position (BackCenterCol,
// BackCenterRow).
//
// Parameters:
//   - items: the user items to place (no Back button, no More button).
//     Each item may or may not have MenuPosSet=true.
//   - styles: the visual style palette to apply to generated navigation items
//     (More → and Back). User items keep their own styles unchanged.
//
// Returns nil when items is empty (caller should handle the "no items" case).
//
// Português:
//
//	Atribui posições de grade a uma lista plana de itens do usuário
//	(Init + métodos) e os organiza em uma hierarquia paginada de submenus.
//	A fatia retornada já contém o botão Back na posição central.
func ApplyRadialLayout(items []hexMenu.MenuItem, styles [5]hexMenu.IconStyle) []hexMenu.MenuItem {
	if len(items) == 0 {
		return nil
	}

	// Split items into those with explicit positions and those without.
	// We process all items in order so source-file order is preserved for
	// items without explicit positions.
	assigned := assignPositions(items)

	// Paginate if necessary.
	return paginatePage(assigned, styles)
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// menuItemWithPos carries a MenuItem together with whether its position
// came from the specialist (explicit) or was auto-assigned (auto).
type menuItemWithPos struct {
	item     hexMenu.MenuItem
	explicit bool // true when item.MenuPosSet was true on entry
}

// assignPositions converts user items to absolute (Col, Row) grid coordinates.
//
// Items with MenuPosSet=true keep their Col/Row unchanged.
// Items without are assigned the next available slot from radialSlots,
// skipping any slot that is already occupied by an explicit item.
//
// Collision detection: if two explicit items claim the same cell, the second
// one silently wins (last-write). This should not happen in practice — the
// specialist is responsible for uniqueness in their doc comments.
//
// Português:
//
//	Converte itens do usuário para coordenadas absolutas (Col, Row).
//	Itens com MenuPosSet=true mantêm sua posição. Os demais recebem a
//	próxima posição disponível em radialSlots.
func assignPositions(items []hexMenu.MenuItem) []menuItemWithPos {
	// Build a set of cells already claimed by explicit items.
	// Key: col*1000+row (cheap collision key for small integer pairs).
	occupied := make(map[int]bool, len(items))
	for _, item := range items {
		if item.MenuPosSet {
			// Convert specialist's relative offset to absolute position.
			absCol := BackCenterCol + item.MenuCol
			absRow := BackCenterRow + item.MenuRow
			occupied[absCol*1000+absRow] = true
		}
	}

	result := make([]menuItemWithPos, 0, len(items))
	autoSlot := 0 // index into radialSlots for the next auto-placed item

	for _, item := range items {
		if item.MenuPosSet {
			// Explicit position: convert relative → absolute.
			item.Col = BackCenterCol + item.MenuCol
			item.Row = BackCenterRow + item.MenuRow
			result = append(result, menuItemWithPos{item: item, explicit: true})
			continue
		}

		// Auto-placed: find the next unoccupied slot in the ring.
		for autoSlot < len(radialSlots) {
			off := radialSlots[autoSlot]
			autoSlot++
			absCol := BackCenterCol + off.Col
			absRow := BackCenterRow + off.Row
			if !occupied[absCol*1000+absRow] {
				item.Col = absCol
				item.Row = absRow
				occupied[absCol*1000+absRow] = true
				break
			}
		}
		// If we ran out of radialSlots (extremely large menu, > 12 items),
		// leave Col/Row as zero — they will appear at the center but still
		// be functional. The specialist should use MaxItemsPerPage or explicit
		// positions in this case.
		result = append(result, menuItemWithPos{item: item, explicit: false})
	}

	return result
}

// paginatePage takes the full list of positioned items and splits it into
// pages of MaxItemsPerPage. Each page has a Back button at the center and,
// if there are more items than MaxItemsPerPage, the last slot becomes
// a "More →" submenu pointing to the next page.
//
// The function is recursive: the overflow page is itself paginated.
//
// Português:
//
//	Divide a lista de itens em páginas de MaxItemsPerPage. Cada página
//	tem um botão Back central e, se necessário, o último slot vira
//	"More →" apontando para a próxima página.
func paginatePage(items []menuItemWithPos, styles [5]hexMenu.IconStyle) []hexMenu.MenuItem {
	// Back button always occupies the absolute center (BackCenterCol, BackCenterRow).
	back := hexMenu.GoBackItem(BackCenterCol, BackCenterRow)
	back.Styles = styles

	// Items fit on a single page — no overflow needed.
	if len(items) <= MaxItemsPerPage {
		page := make([]hexMenu.MenuItem, 0, len(items)+1)
		page = append(page, back)
		for _, mip := range items {
			page = append(page, mip.item)
		}
		return page
	}

	// More items than fit on one page.
	// Reserve the last slot of this page for the "More →" button.
	// The first (MaxItemsPerPage - 1) items go on this page; the rest overflow.
	visibleCount := MaxItemsPerPage - 1
	thisPage := items[:visibleCount]
	overflow := items[visibleCount:]

	// Build the overflow page recursively.
	overflowSubmenu := paginatePage(overflow, styles)

	// Build the "More →" item. Its position is the next available auto slot
	// after the visible items. We reuse the last auto-slot index.
	// Since thisPage already has positions assigned, we need the slot index
	// for position visibleCount (0-based).
	moreCol, moreRow := nextAutoSlot(thisPage)

	more := hexMenu.MenuItem{
		ID:              "SysMore_" + moreSlotID(moreCol, moreRow),
		Col:             moreCol,
		Row:             moreRow,
		Label:           translate.T("menuMore", "More"),
		FontAwesomePath: rulesIcon.KFAChevronRight,
		ViewBox:         "0 0 320 512",
		Type:            hexMenu.ItemSubmenu,
		Submenu:         overflowSubmenu,
		Styles:          styles,
	}

	page := make([]hexMenu.MenuItem, 0, visibleCount+2)
	page = append(page, back)
	for _, mip := range thisPage {
		page = append(page, mip.item)
	}
	page = append(page, more)
	return page
}

// nextAutoSlot returns the absolute (col, row) of the first unoccupied radial
// slot not already taken by any of the provided items.
//
// Used to position the "More →" button after the last visible item.
//
// Português:
//
//	Retorna a posição absoluta do primeiro slot radial livre não ocupado
//	por nenhum dos itens fornecidos. Usado para posicionar "More →".
func nextAutoSlot(items []menuItemWithPos) (col, row int) {
	occupied := make(map[int]bool, len(items)+1)
	// Back is always at the center.
	occupied[BackCenterCol*1000+BackCenterRow] = true
	for _, mip := range items {
		occupied[mip.item.Col*1000+mip.item.Row] = true
	}
	for _, off := range radialSlots {
		absCol := BackCenterCol + off.Col
		absRow := BackCenterRow + off.Row
		if !occupied[absCol*1000+absRow] {
			return absCol, absRow
		}
	}
	// Fallback: use a far-away position that will never collide in practice.
	return BackCenterCol + 4, BackCenterRow + 4
}

// moreSlotID returns a compact string identifier for the "More →" item based
// on its grid position. Used to build a unique MenuItem.ID.
//
// Example: moreSlotID(1, 1) = "1_1"
//
// Português:
//
//	Retorna um identificador compacto para o item "More →" baseado
//	na sua posição de grade. Usado para construir um MenuItem.ID único.
func moreSlotID(col, row int) string {
	return itoa(col) + "_" + itoa(row)
}

// itoa converts an integer to its decimal string representation.
// Handles negative numbers correctly. Avoids importing "strconv" or "fmt"
// in this package to keep the dependency graph clean.
//
// Português:
//
//	Converte um inteiro para string decimal. Trata negativos.
//	Evita importar "strconv" ou "fmt" para manter o grafo de dependências limpo.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 12)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// Reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
