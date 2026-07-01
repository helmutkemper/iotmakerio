// server/projectexport/gitignore.go — basic .gitignore for Go projects.
//
// Bundled with every exported project so the user can `git init`
// without immediately tracking build artefacts, IDE config, or OS
// junk. Intentionally minimal — anything project-specific the user
// can add themselves; the goal here is to cover the cases that
// regularly surprise newcomers (compiled binaries on Windows, IDE
// folders, `.DS_Store` on macOS).
//
// We do NOT include entries for vendored dependencies (`/vendor/`)
// because the IoTMaker projects target TinyGo on small MCUs where
// vendoring is rare; the user is free to add it after publish if
// their workflow needs it.
//
// Português: .gitignore básico para projetos Go publicados pelo
// IoTMaker. Cobre artefatos de build, configurações de IDE e
// arquivos do SO. Não inclui /vendor/ — projetos TinyCo raramente
// usam vendoring; o usuário pode adicionar depois conforme o seu
// fluxo.
package projectexport

// GitignoreContents returns the .gitignore byte slice ready to be
// written into the export ZIP. Returned as []byte (not const string)
// because every other "render this file" function in this package
// uses []byte — keeps the call sites uniform inside builder.go.
func GitignoreContents() []byte {
	return []byte(gitignoreTemplate)
}

const gitignoreTemplate = `# IoTMaker — generated at export time. Adapt to your needs.

# ── Compiled binaries ────────────────────────────────────────────────
*.exe
*.exe~
*.dll
*.so
*.dylib
*.a
*.o

# ── Test binaries and coverage ───────────────────────────────────────
*.test
*.out
coverage.html
coverage.txt
*.prof

# ── Go workspace files ───────────────────────────────────────────────
go.work
go.work.sum

# ── Common build/output directories ──────────────────────────────────
/bin/
/build/
/dist/
/out/

# ── Editor / IDE local config ────────────────────────────────────────
.vscode/
.idea/
*.swp
*.swo
*~

# ── Operating system metadata ────────────────────────────────────────
.DS_Store
Thumbs.db
desktop.ini

# ── Environment files (often hold secrets) ───────────────────────────
.env
.env.local
.env.*.local
`
