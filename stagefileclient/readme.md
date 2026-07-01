# stagefileclient

## What this package does

HTTP client for the stage file API, designed for the WASM IDE. Provides
blocking fetch functions that communicate with the server's stage file
endpoints (`/api/v1/stage-files/*`).

## How it integrates

- **Called by**: `stagefileui` (file manager overlay) and potentially any
  future WASM code that needs to save/load stage files.
- **Server**: `server/handler/stagefileapi/` provides the HTTP endpoints.
- **Auth**: Uses `rulesServer.GetAuthToken()` — the same Bearer token
  mechanism used by `blackbox/loader.go` and `mainMenu/sections.go`.

## Usage

All functions **must be called from a goroutine** — they block on a channel
until the JavaScript fetch Promise resolves.

```go
go func() {
    // List all files
    files, err := stagefileclient.ListFiles("")
    
    // Save current scene
    entry, err := stagefileclient.SaveFile("My robot", folderID, sceneJSON, 12)
    
    // Load a file
    full, err := stagefileclient.LoadFile(fileID)
    
    // Check limits
    info, err := stagefileclient.GetLimit()
    fmt.Printf("%d of %d files used\n", info.UsedFiles, info.MaxFiles)
}()
```

## API covered

| Function       | HTTP Method | Endpoint                          |
|---------------|-------------|-----------------------------------|
| ListFiles      | GET         | /api/v1/stage-files               |
| LoadFile       | GET         | /api/v1/stage-files/:id           |
| SaveFile       | POST        | /api/v1/stage-files               |
| UpdateFile     | PUT         | /api/v1/stage-files/:id           |
| DeleteFile     | DELETE      | /api/v1/stage-files/:id           |
| GetLimit       | GET         | /api/v1/stage-files/limit         |
| ListFolders    | GET         | /api/v1/stage-files/folders       |
| CreateFolder   | POST        | /api/v1/stage-files/folders       |
| RenameFolder   | PUT         | /api/v1/stage-files/folders/:id   |
| DeleteFolder   | DELETE      | /api/v1/stage-files/folders/:id   |
