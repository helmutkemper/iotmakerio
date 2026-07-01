# templateclient

Handles all template-related communication between the WASM IDE and the server.

This package is the client-side counterpart of `server/handler/templateapi/`
and `server/templatepack/`. A specialist uploads a template package (ZIP) to
the server; a maker uses this package to load those templates in the IDE and
generate configured project ZIPs.

---

## Quick start

```go
// In stageWorkspace.Init() — already wired up:

// 1. Load templates at startup (blocking network call, call from a goroutine)
templates := templateclient.LoadAllTemplates()
// → returns []*TemplateFullClient for every ready template visible to the user

// 2. Register the generate callback with the menu builder
menuBuilder.SetTemplateDefs(templates)
menuBuilder.SetGenerateTemplateFn(func(templateID string, config map[string]string) {
    go templateclient.GenerateAndDownload(templateID, "My Template", config)
})
```

---

## How templates reach the IDE

```
Server DB (status=ready templates)
        │
        ▼
GET /api/v1/templates           ← LoadAllTemplates() step 1
        │
        ▼
GET /api/v1/templates/:id       ← LoadAllTemplates() step 2 (per template)
        │
        ▼
[]*TemplateFullClient           ← held in Workspace.loadedTemplates
        │
        ├── Meta          → shown in Templates menu label
        ├── Def.Devices   → placed on canvas as blackbox devices
        └── VarDefaults   → pre-built config for Generate ZIP
```

When the maker clicks **Generate ZIP**, the workspace calls `GenerateAndDownload`,
which POSTs the config to the server and triggers a browser download.

---

## Auth token requirement

Template endpoints require a `Bearer` JWT. The token is injected into
`window._ideAuthToken` by the SPA via `postMessage` after the WASM boots.
`rulesServer.GetAuthToken()` reads it.

**If the token is absent**, `LoadAllTemplates()` returns `nil` immediately
and the Templates submenu shows "No templates". The rest of the IDE remains
fully functional — this is graceful degradation, not an error.

The SPA sets the token in `server/public/static/js/pages/ide.js`:

```javascript
// After receiving IDE_READY from the iframe:
iframe.contentWindow.postMessage(
    { type: 'IDE_AUTH_TOKEN', token: 'Bearer ' + S.token },
    window.location.origin
);
```

---

## File overview

| File | Purpose |
|---|---|
| `clientTypes.go` | Lightweight mirrors of server types. `Devices` reuses `*blackbox.BlackBoxDefClient` — zero new rendering code needed. |
| `loader.go` | Two-step fetch: list → detail per ready template. Blocks its goroutine (safe from `stageWorkspace.Init`). |
| `generator.go` | POST generate → `response.blob()` → `URL.createObjectURL` → `<a download>` click. |

---

## Template devices vs. regular blackbox devices

Template devices are `*blackbox.BlackBoxDefClient` values — they are the
**same type** used by the Hardware submenu. The IDE places them on the canvas
with full wire support using `factory.CreateBlackBoxInit()` and
`factory.CreateBlackBoxMethod()`, exactly like any other component.

The only thing that makes a template device "special" is the **Generate ZIP**
action in its submenu, which collects prop values and calls the server.

---

## Design decisions

**Why N+1 fetches at startup?**

The list endpoint (`GET /api/v1/templates`) returns metadata only (no device
definitions). Each detail fetch (`GET /api/v1/templates/:id`) returns the full
`TemplatePackageDef` including devices. This separation keeps the list
response small even with many templates.

For a typical specialist with 1–5 templates, the N+1 pattern costs 2–6
round trips at startup — imperceptible alongside the blackbox and SVG cache
loads that happen concurrently.

**Why `computeVarDefaults` at load time?**

The Generate ZIP action needs a config map (`map[string]string`) at menu-click
time. Pre-computing it during `LoadAllTemplates()` means the menu callback
closure captures a ready map — no device scanning is needed at click time.

A future improvement will scan the canvas for template device instances and
read their *current* prop values (set by the maker in the Inspect panel)
instead of using the static defaults.

**Why `templateclient` instead of extending `blackbox`?**

The `blackbox` package handles component loading only. Templates add a
fundamentally different concern: project generation with file injection.
Mixing them would blur the package's single responsibility. A separate
package also makes it straightforward to disable templates (by not calling
`LoadAllTemplates()`) without touching the blackbox loading path.
