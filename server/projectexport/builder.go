// server/projectexport/builder.go — streaming ZIP archive builder.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Writes a single .zip stream containing the full project export:
//
//	<project>/
//	├── <code-filename>            ← user's Go source (e.g. blackbox.go)
//	├── readme.<lang>.md           ← every help file at any depth
//	├── Init.en.md                 ← preserves original tree structure
//	├── examples/
//	│   └── *.png                  ← steg PNGs untouched
//	├── LICENSE                    ← Apache 2.0, generated
//	├── .gitignore                 ← Go-flavoured, generated
//	└── PUBLISHING.md              ← user-locale instructions, generated
//
// The wrapping subfolder matches GitHub's source-archive convention:
// users who unzip in a directory get a tidy `<project>/` rather
// than an explosion of files in their cwd. The user explicitly
// asked for this in the spec discussion.
//
// We stream straight into the io.Writer the handler hands us
// (typically the http.ResponseWriter). archive/zip's Writer flushes
// lazily, so memory usage stays bounded by the largest single file
// inside the ZIP — important because example PNGs can be megabytes.
//
// Português: gerador do ZIP em streaming. Empacota o código,
// todos os help files (incluindo examples/), e adiciona LICENSE,
// .gitignore, PUBLISHING.md gerados na hora. Tudo dentro de uma
// subpasta com o nome sanitizado do projeto.
package projectexport

import (
	"archive/zip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"server/store"
)

// BuildOptions carries the per-export parameters that don't fit
// cleanly as positional args. Kept as a struct so future fields
// (e.g. progress callback, custom timestamp for tests) can be added
// without breaking callers.
type BuildOptions struct {
	// ProjectID identifies the rows to load from the help-files and
	// code-versions tables. Always required.
	ProjectID string

	// ProjectName is the user-visible project name. Used both for
	// the wrapping subfolder inside the ZIP and as the
	// {{PROJECT_NAME}} placeholder in PUBLISHING.md. Sanitised by
	// SanitizeProjectName before use — callers may pass the raw
	// Project.Name from the database.
	ProjectName string

	// OwnerName fills the {{OWNER}} placeholder in the LICENSE.
	// Username is the contractual choice (User has no DisplayName);
	// callers should pass User.Username and let RenderLicense fall
	// back to a neutral string if it's somehow empty.
	OwnerName string

	// Locale routes PUBLISHING.md template selection. "pt-br"
	// (case-insensitive) gets the Portuguese version; anything else
	// falls back to English. Callers should pass User.PreferredLocale.
	Locale string

	// Now is the timestamp to embed in the LICENSE copyright year
	// and to use as the ZIP entries' modification time. Injected
	// (not hardcoded as time.Now()) so tests can produce
	// reproducible archives.
	Now time.Time

	// Language is the project's programming_language_id ("golang" or
	// "c"), passed by the caller who already holds the project row. It
	// routes the asset-header emission (C only — Go's embedding idiom is
	// //go:embed, a maker-side slice); empty defaults to Go, mirroring
	// the parser dispatch's stance.
	//
	// Português: O programming_language_id do projeto, passado pelo
	// chamador que já tem a linha. Roteia a emissão dos headers de
	// asset (só C); vazio = Go, espelhando o dispatch.
	Language string
}

// Build streams a complete project export ZIP into `w`.
//
// Returns the number of bytes the underlying zip.Writer claimed to
// have produced (best-effort — zip.Writer's Close finalises the
// central directory but doesn't expose a counter; the handler is
// expected to wrap `w` in a counting writer if it needs precise
// byte accounting).
//
// On error the partial stream is left as-is — the caller (an HTTP
// handler) will already have committed headers including
// Content-Type, so there's no clean way to "rollback" mid-stream.
// The expected pattern is: validate first via Validate(); only if
// OK call Build(); the second-pass validation inside Build itself
// (defensive, for the race where state changed between check and
// download) bails BEFORE any bytes hit `w`.
func Build(w io.Writer, opt BuildOptions) error {
	if opt.ProjectID == "" {
		return errors.New("projectexport.Build: ProjectID is required")
	}
	root := SanitizeProjectName(opt.ProjectName)
	if root == "" {
		root = "project" // fallback; should be impossible if caller validated
	}

	// ── Load source code (latest version) ────────────────────────
	// The code lives in project_code_versions; ListHelpFiles only
	// covers help/example files. We need the source as the FIRST
	// entry in the ZIP because it's the canonical artefact — every
	// other file is supporting material.
	latest, lErr := store.GetLatestProjectCodeVersion(opt.ProjectID)
	if lErr != nil {
		return errors.New("load source: " + lErr.Error())
	}
	// The snapshot ships VERBATIM: every authored file, its authored name,
	// its authored bytes, its relative path. Paths were validated at save
	// time (plain relative, bounded depth, extension per language), so
	// each one is a safe ZIP key under the project root. The single-file
	// era's extension-normalising stopgap died with the multi-file model —
	// the specialist now owns real filenames with real extensions.
	//
	// Português: O snapshot embarca VERBATIM — cada arquivo autoral com
	// nome, bytes e caminho relativo próprios. Caminhos foram validados no
	// save; o stopgap de trocar extensão morreu com o modelo multiarquivo:
	// o especialista agora é dono de nomes reais com extensões reais.

	// ── Load help files metadata, then content per entry ────────
	// We don't fetch all blobs up front: a project with several
	// large example PNGs could otherwise hold tens of megabytes in
	// memory just for the listing. Fetching per file keeps memory
	// proportional to the LARGEST file, not the SUM.
	helps, hErr := store.ListHelpFiles(opt.ProjectID)
	if hErr != nil {
		return errors.New("list help files: " + hErr.Error())
	}

	// ── Set up the writer ───────────────────────────────────────
	zw := zip.NewWriter(w)
	// Close on exit so the central directory is always written.
	// Returned errors propagate; we prefer the first error
	// encountered (write error) over any close error, matching
	// stdlib idioms.
	var firstErr error
	defer func() {
		if cErr := zw.Close(); cErr != nil && firstErr == nil {
			firstErr = cErr
		}
	}()

	// Helper: write a single entry. Centralises the modtime + 0644
	// permissions so every file in the archive has uniform metadata.
	writeFile := func(relPath string, content []byte) error {
		// Path inside the ZIP uses forward slashes regardless of
		// host OS — the ZIP spec requires it and unzippers on
		// Windows handle it correctly.
		fullPath := path.Join(root, relPath)
		hdr := &zip.FileHeader{
			Name:     fullPath,
			Method:   zip.Deflate,
			Modified: opt.Now,
		}
		// Set unix mode bits so well-behaved unzippers (most modern
		// ones) reproduce a sane permission set.
		hdr.SetMode(0644)
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := fw.Write(content); err != nil {
			return err
		}
		return nil
	}

	// 1. Source snapshot at the project root — every authored file under
	//    its authored relative path, in tab order (cosmetically pleasant:
	//    the ZIP listing reads like the editor's tab strip).
	//
	//    Português: O snapshot na raiz — cada arquivo autoral no seu
	//    caminho relativo, na ordem das abas.
	isC := strings.EqualFold(strings.TrimSpace(opt.Language), "c")
	for _, f := range latest.Files {
		// Binary assets are stored base64 (the snapshot is JSON — see
		// store.CodeFileEntry.Encoding); the ZIP holds REAL bytes, so
		// this is the decoding edge. The gate proved the payload decodes
		// at save time; a failure here means post-validation corruption
		// — worth aborting the export loudly rather than shipping a
		// text-mangled gif.
		//
		// Português: Asset binário é base64 no snapshot; o ZIP carrega
		// bytes REAIS — esta é a borda de decode. O portão já provou que
		// decodifica; falhar aqui é corrupção pós-validação — melhor
		// abortar alto que embarcar um gif mutilado.
		data := []byte(f.Content)
		if f.Encoding == "base64" {
			decoded, decErr := base64.StdEncoding.DecodeString(f.Content)
			if decErr != nil {
				firstErr = fmt.Errorf("asset %s: corrupt base64 in snapshot: %w", f.Path, decErr)
				return firstErr
			}
			data = decoded
		}
		if err := writeFile(f.Path, data); err != nil {
			firstErr = err
			return firstErr
		}

		// Use 1 of the asset model, C99 half: every asset grows its
		// generated companion header (see asset_headers.go for the
		// naming contract and why emission is unconditional). "Asset"
		// here = any non-source file — the save gate already enforced
		// the whitelist, so the builder needs no extension list of its
		// own to drift out of sync.
		//
		// Português: Uso 1, metade C99: todo asset ganha seu header
		// gerado (contrato de nomes em asset_headers.go). "Asset" =
		// qualquer não-fonte — o portão já impôs a whitelist no save,
		// então o builder não precisa de lista própria para dessincronizar.
		if isC {
			lower := strings.ToLower(f.Path)
			if !strings.HasSuffix(lower, ".c") && !strings.HasSuffix(lower, ".h") {
				if err := writeFile(AssetHeaderPath(f.Path), RenderAssetHeader(f.Path, data)); err != nil {
					firstErr = err
					return firstErr
				}
			}
		}
	}

	// 2. All help files, preserving their stored relative paths.
	//    The store layer already enforces the IDS filename grammar
	//    (HelpFileRe / readmeFileRe in codegen/blackbox/devicehelp.go)
	//    plus the at-most-one-subdirectory rule, so we don't need
	//    to sanitise paths here — they are known-safe.
	for _, hf := range helps {
		full, gErr := store.GetHelpFile(opt.ProjectID, hf.Path)
		if gErr != nil {
			// Skip rather than abort: a single missing blob (e.g.
			// concurrent delete between list and get) shouldn't
			// torpedo the whole export. The user's pre-flight
			// already passed; if a file vanishes mid-build, the
			// resulting archive is still mostly correct and the
			// user can re-export.
			continue
		}
		if err := writeFile(hf.Path, full.Content); err != nil {
			firstErr = err
			return firstErr
		}
	}

	// 3. LICENSE — Apache 2.0 with copyright filled in.
	if err := writeFile("LICENSE", RenderLicense(opt.OwnerName, opt.Now)); err != nil {
		firstErr = err
		return firstErr
	}

	// 4. .gitignore — Go-flavoured.
	if err := writeFile(".gitignore", GitignoreContents()); err != nil {
		firstErr = err
		return firstErr
	}

	// 5. PUBLISHING.md — user-locale instructions.
	pubBytes := RenderPublishing(root, opt.Locale)
	if err := writeFile("PUBLISHING.md", pubBytes); err != nil {
		firstErr = err
		return firstErr
	}

	return firstErr
}

// SanitizeProjectName turns a free-form project name into a string
// that is safe as a folder name, a GitHub repository name, and a
// filename component. The transformation is:
//
//  1. Trim leading/trailing whitespace.
//  2. Lowercase.
//  3. Replace any run of characters that are not [a-z0-9.] with a
//     single "-".
//  4. Trim leading/trailing dots and hyphens.
//  5. If the result is empty, return "project" (caller can decide
//     to override with their own fallback).
//
// The dot is preserved because some users name projects after the
// device they target ("apds9960.v2") and forcing those dots into
// hyphens reads worse for both humans and GitHub URLs. GitHub
// allows dots in repo names — they're fine.
//
// Exported (capital S) so the SuggestedFilename helper and any
// future caller (e.g. a CLI export command) can reuse the same
// rule. Stable behaviour matters: the sanitised name appears in
// the user's ZIP filename AND in the suggested git remote URL —
// drift between those two would confuse the publish flow.
func SanitizeProjectName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.':
			b.WriteRune(r)
			prevDash = false
		default:
			// Collapse runs of non-allowed chars into a single "-"
			// so "my   project" doesn't become "my---project".
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), ".-")
	return out
}

// SuggestedFilename returns the filename the HTTP handler should
// put in the Content-Disposition header. Format mirrors what we
// agreed on in the spec:
//
//	<sanitized-name>-<ISO8601-without-colons>.zip
//
// e.g. "apds9960-2026-05-07T14-30-22Z.zip"
//
// The colon-replaced ISO 8601 keeps the timestamp human-readable
// while staying ASCII-clean and filesystem-safe on every platform
// (Windows refuses ":" in filenames; some shell escapes turn it
// awkward). UTC is enforced — the user is downloading a snapshot,
// not scheduling a meeting; no benefit to local time and a real
// cost (timezone confusion) when the file is shared.
func SuggestedFilename(projectName string, now time.Time) string {
	stem := SanitizeProjectName(projectName)
	if stem == "" {
		stem = "project"
	}
	// time.Format is timezone-aware; convert to UTC first so the Z
	// suffix is honest. Pattern is RFC3339 with colons replaced by
	// hyphens (no fractional seconds — second precision is plenty).
	ts := now.UTC().Format("2006-01-02T15-04-05Z")
	return stem + "-" + ts + ".zip"
}
