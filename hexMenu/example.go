// hexMenu/example.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package hexMenu

// -----------------------------------------------------------------------
// Integration Example
// -----------------------------------------------------------------------
//
// This file shows how a device (StatementAdd, StatementLoop, etc.)
// would define and open a hexagonal context menu.
//
// This is example code only — it is NOT compiled as part of the package.
// Copy and adapt the patterns below into your device files.
//
// -----------------------------------------------------------------------

/*

// === Example 1: Simple action menu for StatementAdd ===

// In statementAdd.go, define the menu items:
func (e *StatementAdd) getHexMenuItems() []hexMenu.MenuItem {
	return []hexMenu.MenuItem{
		{
			ID:              "resize",
			Col:             1,
			Row:             1,
			Label:           "Resize",
			FontAwesomePath: "M32 32C14.3 32 0 46.3 0 64...",  // expand-arrows FA icon
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { e.SetResizeEnable(true) },
			Styles:          hexMenu.DefaultStyles(),
		},
		{
			ID:              "delete",
			Col:             1,
			Row:             3,
			Label:           "Delete",
			FontAwesomePath: "M135.2 17.7L128 32 32 32...",    // trash FA icon
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { e.Delete() },
			Styles:          hexMenu.DefaultStyles(),
		},
	}
}

// In wireEvents(), handle double-click:
func (e *StatementAdd) wireEvents() {
	e.elem.SetOnDoubleClick(func(x, y float64, button int) {
		if e.menu == nil {
			e.menu = hexMenu.New(e.stage, hexMenu.Config{
				HexRadius: 28,
				ZIndex:    1000,
			})
		}
		canvasX, canvasY := e.elem.GetPosition()
		e.menu.Open(
			e.getHexMenuItems(),
			hexMenu.PositionAtClick,
			canvasX + x,
			canvasY + y,
		)
	})
}


// === Example 2: Hierarchical menu with submenus ===

// Math submenu items (Add, Sub, Mul, Div)
mathSubItems := []hexMenu.MenuItem{
	hexMenu.GoBackItem(5, 7),  // convenience: pre-built GoBack button
	{
		ID: "Add", Col: 4, Row: 6,
		Label: "Add",
		FontAwesomePath: kFontAwesomePlus,
		ViewBox: "0 0 448 512",
		Type: hexMenu.ItemAction,
		OnClick: func() { createStatementAdd() },
		Styles: hexMenu.DefaultStyles(),
	},
	{
		ID: "Sub", Col: 4, Row: 8,
		Label: "Sub",
		FontAwesomePath: kFontAwesomeMinus,
		ViewBox: "0 0 448 512",
		Type: hexMenu.ItemAction,
		OnClick: func() { createStatementSub() },
		Styles: hexMenu.DefaultStyles(),
	},
	{
		ID: "Mul", Col: 6, Row: 6,
		Label: "Mul",
		FontAwesomePath: kFontAwesomeXmark,
		ViewBox: "0 0 384 512",
		Type: hexMenu.ItemAction,
		OnClick: func() { createStatementMul() },
		Styles: hexMenu.DefaultStyles(),
	},
	{
		ID: "Div", Col: 6, Row: 8,
		Label: "Div",
		FontAwesomePath: kFontAwesomeDivide,
		ViewBox: "0 0 448 512",
		Type: hexMenu.ItemAction,
		OnClick: func() { createStatementDiv() },
		Styles: hexMenu.DefaultStyles(),
	},
}

// Main menu with submenu navigation
mainMenuItems := []hexMenu.MenuItem{
	{
		ID: "SysMath", Col: 2, Row: 2,
		Label: "Math",
		FontAwesomePath: kFontAwesomeSquareRootVariable,
		ViewBox: "0 0 576 512",
		Type: hexMenu.ItemSubmenu,
		Submenu: mathSubItems,
		Styles: hexMenu.DefaultStyles(),
	},
	{
		ID: "SysLoop", Col: 2, Row: 4,
		Label: "Loop",
		FontAwesomePath: kFontAwesomeRepeat,
		ViewBox: "0 0 512 512",
		Type: hexMenu.ItemSubmenu,
		Submenu: loopSubItems,
		Styles: hexMenu.DefaultStyles(),
	},
	// ... more items
}

menu.Open(mainMenuItems, hexMenu.PositionAtClick, clickX, clickY)


// === Example 3: Tutorial mode ===

// Guide the user: "click Math, then click Add"
menu.StartTutorial(
	mainMenuItems,
	[]hexMenu.TutorialStep{
		{
			PagePath: nil,      // on root page
			ItemID:   "SysMath", // flash Math button
		},
		{
			PagePath: []string{"SysMath"}, // inside Math submenu
			ItemID:   "Add",               // flash Add button
		},
	},
	hexMenu.PositionCentered,
	400, 300, // center of screen
)


// === Example 4: Custom icon styles ===

item := hexMenu.MenuItem{
	ID: "special", Col: 1, Row: 1,
	Label: "Special",
	FontAwesomePath: "...",
	ViewBox: "0 0 512 512",
	Type: hexMenu.ItemAction,
	OnClick: func() { ... },
	Styles: [5]hexMenu.IconStyle{
		{ColorIcon: "#FFD700", ColorBorder: "#DAA520", ColorLabel: "#FFD700", ColorBackground: "#4A3728"}, // Normal: gold
		{ColorIcon: "#888888", ColorBorder: "#666666", ColorLabel: "#888888", ColorBackground: "#333333"}, // Disabled
		{ColorIcon: "#FFD700", ColorBorder: "#FFD700", ColorLabel: "#FFD700", ColorBackground: "#6B4C2A"}, // Selected
		{ColorIcon: "#FF4444", ColorBorder: "#FF0000", ColorLabel: "#FF4444", ColorBackground: "#660000"}, // Attention1
		{ColorIcon: "#FF8888", ColorBorder: "#FF4444", ColorLabel: "#FF8888", ColorBackground: "#880000"}, // Attention2
	},
}

*/
