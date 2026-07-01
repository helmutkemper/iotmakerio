# handler/projectapi

This package implements the REST API for the **Project Management** module of the IoTMaker portal.

## What it does

Provides endpoints for authenticated users to create, list and delete projects,
as well as manage the files (code, images, documentation) inside each project.

## How it fits into the project

```
SPA (app.html + JS)
  └── pages/projects.js   ← renders the tree view and forms
        │
        ▼
/api/v1/projects/*         ← this package
        │
        ├── store.CreateProject / ListProjectsByUser / DeleteProject
        └── os.MkdirAll / os.RemoveAll (disk operations)
```

## Project file structure on disk

```
public/static/
└── {user_id}/
    └── project/
        └── customDevice/
            └── {project_id}/
                ├── code/   ← single .go source file
                ├── img/    ← PNG / JPG / GIF / WebP images
                └── docs/   ← Markdown files ({slug}.{lang}.md)
```

The `public/static/` directory is already served by the `/static/*` route in
`cmd/server/main.go`, so all uploaded files are immediately accessible via
their public URL without extra configuration.

⚠ **Private project files are not access-controlled yet.**
All files are publicly accessible by URL regardless of the project's `visibility`
setting. Visibility currently only controls whether the project appears in
public listings. A future iteration should:

1. Move user files outside `public/static/`.
2. Add an authenticated `/user-files/*` route that checks project visibility
	 before serving the file.

## API routes

### Lookup tables (public, no auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/projects/meta/languages` | List programming languages |
| GET | `/api/v1/projects/meta/ui-languages` | List UI languages |

### Projects (requires Bearer token)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/projects` | List all projects for the current user |
| POST | `/api/v1/projects` | Create a new project |
| DELETE | `/api/v1/projects/:id` | Delete project and all its files |

### Files (requires Bearer token)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/projects/:id/files` | List files in a project |
| POST | `/api/v1/projects/:id/files/code` | Upload / replace code file |
| DELETE | `/api/v1/projects/:id/files/code` | Delete code file |
| PUT | `/api/v1/projects/:id/files/code/rename` | Rename code file |
| POST | `/api/v1/projects/:id/files/img` | Upload image |
| DELETE | `/api/v1/projects/:id/files/img/:name` | Delete image |
| POST | `/api/v1/projects/:id/files/docs` | Upload markdown |
| DELETE | `/api/v1/projects/:id/files/docs/:name` | Delete markdown |

## Request / response format

All requests use the same JSON envelope as the rest of the SPA:

```json
{ "metadata": { "status": 200 }, "data": { ... } }
{ "metadata": { "status": 4xx, "error": "message" }, "data": null }
```

## Creating a project — required fields

```json
POST /api/v1/projects
{
  "name": "My Sensor Board",
  "type": "custom_device",
  "visibility": "private",
  "programmingLanguageId": "golang",
  "uiLanguageId": "en"
}
```

`type` currently accepts only `"custom_device"`.  
`visibility` must be `"public"` or `"private"` — both are required (no default).  
`programmingLanguageId` and `uiLanguageId` must match rows in the respective
lookup tables (seeded in `store/db.go`).

## Deleting a project

The UI requires the user to type the exact project name before the DELETE
request is sent. This is enforced client-side only. The server checks ownership
but does not re-validate the name — it trusts that the frontend enforced it.

## Adding new programming languages or UI languages

Edit `store/db.go` — `seedProgrammingLanguages()` or `seedProjectUILanguages()`.
Both use `INSERT OR IGNORE` so adding a row is safe to deploy without a migration.

## Quick test (curl)

```bash
# 1. Log in and get a token
TOKEN=$(curl -sX POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"login":"admin@iotmaker.io","password":"<pwd>"}' | jq -r .data.userId)

# 2. Complete 2FA (check email / server logs)
TOKEN=$(curl -sX POST http://localhost:8080/api/auth/login/2fa \
  -H 'Content-Type: application/json' \
  -d '{"userId":"'$TOKEN'","code":"123456"}' | jq -r .data.token)

# 3. List programming languages
curl http://localhost:8080/api/v1/projects/meta/languages | jq

# 4. Create a project
curl -sX POST http://localhost:8080/api/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test Project","type":"custom_device","visibility":"private",
       "programmingLanguageId":"golang","uiLanguageId":"en"}' | jq

# 5. List projects
curl -s http://localhost:8080/api/v1/projects \
  -H "Authorization: Bearer $TOKEN" | jq

# 6. Upload a code file (replace PROJECT_ID)
curl -sX POST http://localhost:8080/api/v1/projects/PROJECT_ID/files/code \
  -H "Authorization: Bearer $TOKEN" \
  -F 'file=@myDevice.go' | jq

# 7. List project files
curl -s http://localhost:8080/api/v1/projects/PROJECT_ID/files \
  -H "Authorization: Bearer $TOKEN" | jq

# 8. Delete the project
curl -sX DELETE http://localhost:8080/api/v1/projects/PROJECT_ID \
  -H "Authorization: Bearer $TOKEN" | jq
```
