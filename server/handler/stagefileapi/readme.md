# Stage File API

## What this package does

Provides HTTP endpoints for saving, loading, renaming, and deleting IDE stage
files (JSON snapshots of the canvas with devices, wires, and camera state).
Also manages virtual folders that organise files into a tree structure.

Stage files are **private per user** — there is no sharing, publishing, or
public visibility. They are completely independent from the portal's "projects"
feature.

## How it integrates

- **Store layer**: `server/store/stage_files.go` handles all database operations.
  Tables are created by `server/store/db_stage_files.go` during the migration.
- **Auth**: All routes use `spaauth.RequireBearerToken()`. The WASM IDE sends the
  token via `window._ideAuthToken` (same as black-box and template endpoints).
- **WASM client**: `stagefileclient` package on the IDE side calls these endpoints.
- **Menu entry**: The IDE hex menu's Export submenu includes a "Stage files" item
  that opens the file manager overlay.

## File limit system

Each user has a maximum number of stage files. The limit is resolved with a
three-layer cascade (highest priority first):

1. **Per-user override** — `stage_file_user_limits` table
2. **Per-group override** — `stage_file_group_limits` table (highest among user's groups)
3. **Global setting** — `project_settings` key `stage_file_max_per_user`
4. **Hard fallback** — `DefaultStageFileMaxPerUser = 50`

The `GET /limit` endpoint returns both the effective limit and current usage.

## Quick test

```bash
# Create a folder
curl -X POST http://localhost:8080/api/v1/stage-files/folders \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"My robots"}'

# Save a file
curl -X POST http://localhost:8080/api/v1/stage-files \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Arm controller","folderId":"FOLDER_ID","sceneJson":"{\"version\":\"1.0\"}","deviceCount":5}'

# List files
curl http://localhost:8080/api/v1/stage-files \
  -H "Authorization: Bearer $TOKEN"

# Load a file (includes scene_json)
curl http://localhost:8080/api/v1/stage-files/FILE_ID \
  -H "Authorization: Bearer $TOKEN"

# Check limit
curl http://localhost:8080/api/v1/stage-files/limit \
  -H "Authorization: Bearer $TOKEN"
```

## Endpoints

| Method | Route                              | Description                      |
|--------|------------------------------------|----------------------------------|
| GET    | /api/v1/stage-files                | List files (?folderId optional)  |
| POST   | /api/v1/stage-files                | Create file                      |
| GET    | /api/v1/stage-files/limit          | Usage vs capacity                |
| GET    | /api/v1/stage-files/:id            | Load file (with scene_json)      |
| PUT    | /api/v1/stage-files/:id            | Update (rename/move/save scene)  |
| DELETE | /api/v1/stage-files/:id            | Delete file                      |
| GET    | /api/v1/stage-files/folders        | List all folders (flat)          |
| POST   | /api/v1/stage-files/folders        | Create folder                    |
| PUT    | /api/v1/stage-files/folders/:id    | Rename or move folder            |
| DELETE | /api/v1/stage-files/folders/:id    | Delete folder (CASCADE)          |
