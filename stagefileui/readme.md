# stagefileui

## What this package does

Renders the stage file manager overlay — a draggable panel where the maker can
save, load, rename, and delete IDE stage files organised in virtual folders.

The overlay follows the same visual language as `ui/overlay` (Catppuccin Mocha
palette, draggable title bar, backdrop click to close, Escape to close).

## How it integrates

- **Menu entry**: The hex menu's Export submenu has a "Files" item that calls
  `stagefileui.Show()` in a goroutine.
- **Workspace**: `stageWorkspace/workspace.go` creates the `stagefileui.Config`
  with callbacks for `GetSceneJSON`, `GetDeviceCount`, and `OnLoad`.
- **Network**: Uses `stagefileclient` for all server communication.

## Tablet support

All interactive elements have a minimum touch target of 44px height. File rows,
buttons, and folder entries are generously padded for comfortable finger
navigation. The panel uses `90vw` width and `80vh` height to fill tablet
screens without overflowing.

## Current behaviour (Phase 1)

| Action | Behaviour |
|--------|-----------|
| Save current | Exports the canvas JSON and creates a new file on the server |
| Open | Downloads the file's scene JSON as a `.json` file |
| Rename | Inline prompt dialog, updates server |
| Delete | Confirmation dialog, deletes from server |
| New folder | Prompt dialog, creates virtual folder on server |
| Delete folder | Confirmation dialog, CASCADE deletes folder + contents |

## Future (Phase 2)

The "Open" action will reconstruct the canvas from the JSON instead of
downloading it. This requires new infrastructure:
- `ClearAll()` on Stage, SceneMgr, and WireMgr
- Device registry (`map[string]func()` for each device type)
- Programmatic wire creation
- Property restoration

## UI structure

```
┌──────────────────────────────────────────────┐
│ Stage files                              [×] │  ← title bar (draggable)
├─────────────┬────────────────────────────────┤
│ Folders     │ All files / Folder name        │  ← breadcrumb + Save btn
│             │                                │
│ All files ◀ │  Robot arm controller          │
│   Tutorials │  8 devices · 2025-04-05        │
│   My robots │                [Open][✏][🗑]  │
│   Sensors   │                                │
│             │  Temperature monitor           │
│ [+New folder]  3 devices · 2025-04-04        │
│             │                [Open][✏][🗑]  │
├─────────────┴────────────────────────────────┤
│ 5 / 50 files                        [Close]  │  ← footer
└──────────────────────────────────────────────┘
```
