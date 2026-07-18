// /ide/server/store/wizard_examples.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package store

// wizard_examples.go — the mission gallery's storage (wizard UI plan,
// 2026-07-13). An example is a curated file bundle at a chosen FINISH
// LEVEL — "ready" teaches by modification (open, change, see), "mission"
// teaches by completion (steps with self-checking predicates). Content is
// DATA: seeded here for day one, managed at /control later, never
// hardcoded in a release.
//
// Steps schema (steps_json), designed now so /control registrations stay
// stable even before the panel ships:
//
//	[{"title": "...", "detail": "...",
//	  "check":  {"kind": "<predicate>", ...args},
//	  "action": {"kind": "openPort", "fn": "...", "port": "..."}}] // optional
//
// Predicate kinds (evaluated client-side over the parse result):
//	parseOk                        — the project parses clean
//	fileIsDict    {path}           — file exists and sniffs as a dictionary
//	portHasLang   {fn, port}       — port carries `lang:`
//	portHasDict   {fn, port}       — port carries a resolvable `dict:`
//	sliceCollapsed{fn, port}       — the (ptr,len) pair collapsed
//
// Português: O armazém da galeria de missões. Um exemplo é um bundle
// curado num NÍVEL DE ACABAMENTO — "ready" ensina por modificação,
// "mission" por conclusão (passos com predicados autoverificáveis).
// Conteúdo é DADO: semeado aqui, gerenciado no /control depois, nunca
// hardcode de release. O schema de passos nasce agora para o /control
// ficar estável antes mesmo do painel existir.

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// WizardExample is one gallery entry. Files/Steps are raw JSON blobs —
// the API relays them; only the client interprets steps.
// Português: Uma entrada da galeria. Files/Steps são JSON cru — a API
// repassa; só o cliente interpreta os passos.
type WizardExample struct {
	ID       string          `json:"id"`
	Language string          `json:"language"`
	Level    string          `json:"level"` // "ready" | "mission"
	Title    string          `json:"title"`
	Subtitle string          `json:"subtitle"`
	Ord      int             `json:"ord"`
	Files    json.RawMessage `json:"files,omitempty"`
	Steps    json.RawMessage `json:"steps,omitempty"`
}

// MigrateWizardExamples creates the table and seeds the inaugural
// gallery: one "ready" example (Blink) and one "mission" (the config +
// autocomplete arc from the 2026-07-13 field session). INSERT OR IGNORE:
// idempotent, never overwrites an admin's edit of the same id.
// Português: Cria a tabela e semeia a galeria inaugural. INSERT OR
// IGNORE: idempotente, nunca sobrescreve edição de admin.
func MigrateWizardExamples() error {
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS wizard_examples (
			id         TEXT PRIMARY KEY,
			language   TEXT NOT NULL,
			level      TEXT NOT NULL,
			title      TEXT NOT NULL,
			subtitle   TEXT NOT NULL DEFAULT '',
			ord        INTEGER NOT NULL DEFAULT 0,
			visible    INTEGER NOT NULL DEFAULT 1,
			files_json TEXT NOT NULL,
			steps_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL
		);`); err != nil {
		return err
	}

	// Provenance: which project a snapshot came from ("" for hand-seeded
	// rows). ALTER on an existing table — the duplicate-column error is
	// the "already migrated" signal and is deliberately swallowed.
	// Português: Proveniência do snapshot ("" para seeds manuais). O erro
	// de coluna duplicada é o sinal de "já migrado" — engolido de
	// propósito.
	if _, err := DB.Exec(`ALTER TABLE wizard_examples
		ADD COLUMN source_project_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}

	// Content lives in a FOLDER, not in code (field rule 2026-07-16:
	// "o conteúdo de school não deve ficar em código") — versionable,
	// human-editable, immune to clean-data. Boot loads it; Export writes
	// it back. Português: Conteúdo mora numa PASTA — versionável,
	// editável, imune ao clean-data. O boot carrega; o Export grava.
	if err := LoadSchoolDir(SchoolDirPath()); err != nil {
		log.Printf("[wizard_examples] school folder load: %v", err)
	}
	return nil
}

// ─── /control administration ─────────────────────────────────────────────────

// AdminListWizardExamples returns EVERY row (hidden included) with sizes
// instead of bundles — the admin table is a menu too. Português: Todas as
// linhas (ocultas inclusas) com tamanhos em vez de bundles.
func AdminListWizardExamples() ([]map[string]any, error) {
	rows, err := DB.Query(`
		SELECT id, language, level, title, subtitle, ord, visible,
		       length(files_json), length(steps_json), source_project_id
		FROM wizard_examples ORDER BY language, ord, title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, lang, level, title, subtitle, src string
		var ord, visible, fb, sb int
		if err := rows.Scan(&id, &lang, &level, &title, &subtitle,
			&ord, &visible, &fb, &sb, &src); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "language": lang, "level": level, "title": title,
			"subtitle": subtitle, "ord": ord, "visible": visible == 1,
			"filesBytes": fb, "stepsBytes": sb, "sourceProjectId": src,
		})
	}
	return out, rows.Err()
}

// GetWizardExampleAny is the admin read: hidden rows load too.
func GetWizardExampleAny(id string) (*WizardExample, bool, string, error) {
	var e WizardExample
	var files, steps, src string
	var visible int
	err := DB.QueryRow(`
		SELECT id, language, level, title, subtitle, ord, visible,
		       files_json, steps_json, source_project_id
		FROM wizard_examples WHERE id = ?`, id).
		Scan(&e.ID, &e.Language, &e.Level, &e.Title, &e.Subtitle, &e.Ord,
			&visible, &files, &steps, &src)
	if err != nil {
		return nil, false, "", err
	}
	e.Files = json.RawMessage(files)
	e.Steps = json.RawMessage(steps)
	return &e, visible == 1, src, nil
}

// UpsertWizardExample writes the whole row — the admin form's word is
// final. Português: Grava a linha inteira — a palavra do form é final.
func UpsertWizardExample(e *WizardExample, visible bool, sourceProjectID string) error {
	v := 0
	if visible {
		v = 1
	}
	files := string(e.Files)
	if files == "" {
		files = "[]"
	}
	steps := string(e.Steps)
	if steps == "" {
		steps = "[]"
	}
	_, err := DB.Exec(`
		INSERT INTO wizard_examples
			(id, language, level, title, subtitle, ord, visible,
			 files_json, steps_json, source_project_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			language = excluded.language,
			level = excluded.level,
			title = excluded.title,
			subtitle = excluded.subtitle,
			ord = excluded.ord,
			visible = excluded.visible,
			files_json = excluded.files_json,
			steps_json = excluded.steps_json,
			source_project_id = excluded.source_project_id`,
		e.ID, e.Language, e.Level, e.Title, e.Subtitle, e.Ord, v,
		files, steps, sourceProjectID,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// AdminListProjectsLight lists every project as picker rows — id, name,
// language, freshness. The snapshot modal shows NAMES; ids are database
// business (field principle: "as telas devem fazer sentido para um ser
// humano"). Português: Linhas de seletor — o modal mostra NOMES; id é
// assunto de banco.
func AdminListProjectsLight() ([]map[string]any, error) {
	rows, err := DB.Query(`
		SELECT id, name, programming_language_id, updated_at
		FROM projects ORDER BY updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, lang, upd string
		if err := rows.Scan(&id, &name, &lang, &upd); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "name": name, "language": lang, "updatedAt": upd,
		})
	}
	return out, rows.Err()
}

// DeleteWizardExample removes a row permanently.
func DeleteWizardExample(id string) error {
	_, err := DB.Exec(`DELETE FROM wizard_examples WHERE id = ?`, id)
	return err
}

// SchoolDirPath resolves the school content folder: SCHOOL_SEED_DIR or
// ./schoolseed beside the binary — inside the repo, OUTSIDE data/, so it
// survives clean-data and travels in git. Português: A pasta de conteúdo
// — no repo, FORA de data/, sobrevive ao clean-data e viaja no git.
func SchoolDirPath() string {
	if v := os.Getenv("SCHOOL_SEED_DIR"); v != "" {
		return v
	}
	return "./schoolseed"
}

// schoolMeta is meta.json — everything about an example except its files.
type schoolMeta struct {
	Language        string          `json:"language"`
	Level           string          `json:"level"`
	Title           string          `json:"title"`
	Subtitle        string          `json:"subtitle"`
	Ord             int             `json:"ord"`
	Visible         bool            `json:"visible"`
	SourceProjectID string          `json:"sourceProjectId"`
	Steps           json.RawMessage `json:"steps"`
}

// LoadSchoolDir upserts every <dir>/<id>/ into wizard_examples: meta.json
// + code/* (kind "") + manual/* (kind "help"; binaries — detected by
// invalid UTF-8 — travel base64, mirroring the wire shape the School
// client already speaks). The folder's word is final for the ids it
// holds; unknown ids in the DB are left alone. Português: Upsert de cada
// pasta: meta + code/ + manual/ (binário — UTF-8 inválido — vira base64,
// o mesmo shape que o cliente já fala). A pasta manda nos ids dela.
func LoadSchoolDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[wizard_examples] no school folder at %s — starting empty", dir)
			return nil
		}
		return err
	}
	loaded := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		id := ent.Name()
		metaRaw, err := os.ReadFile(filepath.Join(dir, id, "meta.json"))
		if err != nil {
			log.Printf("[wizard_examples] %s: meta.json: %v — skipped", id, err)
			continue
		}
		var meta schoolMeta
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			log.Printf("[wizard_examples] %s: meta.json invalid: %v — skipped", id, err)
			continue
		}
		var files []FileSeed
		collect := func(sub, kind string) {
			base := filepath.Join(dir, id, sub)
			_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				raw, rerr := os.ReadFile(p)
				if rerr != nil {
					return nil
				}
				rel, _ := filepath.Rel(base, p)
				fs := FileSeed{Path: filepath.ToSlash(rel), Kind: kind}
				if utf8.Valid(raw) {
					fs.Content = string(raw)
				} else {
					fs.Content = base64.StdEncoding.EncodeToString(raw)
					fs.Encoding = "base64"
				}
				files = append(files, fs)
				return nil
			})
		}
		collect("code", "")
		collect("manual", "help")
		filesJSON, err := json.Marshal(files)
		if err != nil {
			continue
		}
		steps := meta.Steps
		if len(steps) == 0 {
			steps = json.RawMessage("[]")
		}
		e := &WizardExample{
			ID: id, Language: meta.Language, Level: meta.Level,
			Title: meta.Title, Subtitle: meta.Subtitle, Ord: meta.Ord,
			Files: filesJSON, Steps: steps,
		}
		if err := UpsertWizardExample(e, meta.Visible, meta.SourceProjectID); err != nil {
			log.Printf("[wizard_examples] %s: upsert: %v", id, err)
			continue
		}
		loaded++
	}
	log.Printf("[wizard_examples] loaded %d example(s) from %s", loaded, dir)
	return nil
}

// ExportSchoolDir writes every DB example back to <dir>/<id>/ — the
// backup the /control button triggers. Write-only: it never prunes
// folders whose ids left the DB (deleting content is a human decision).
// Português: Grava o DB de volta na pasta — o backup do botão. Nunca
// apaga pastas órfãs (deletar conteúdo é decisão humana).
func ExportSchoolDir(dir string) (int, error) {
	list, err := AdminListWizardExamples()
	if err != nil {
		return 0, err
	}
	written := 0
	for _, row := range list {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		e, visible, srcID, err := GetWizardExampleAny(id)
		if err != nil {
			continue
		}
		meta := schoolMeta{
			Language: e.Language, Level: e.Level, Title: e.Title,
			Subtitle: e.Subtitle, Ord: e.Ord, Visible: visible,
			SourceProjectID: srcID, Steps: e.Steps,
		}
		metaJSON, _ := json.MarshalIndent(meta, "", "  ")
		base := filepath.Join(dir, id)
		if err := os.MkdirAll(base, 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(filepath.Join(base, "meta.json"),
			append(metaJSON, '\n'), 0o644); err != nil {
			return written, err
		}
		var files []FileSeed
		if err := json.Unmarshal(e.Files, &files); err != nil {
			continue
		}
		for _, f := range files {
			sub := "code"
			if f.Kind == "help" {
				sub = "manual"
			}
			p := filepath.Join(base, sub, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				continue
			}
			var raw []byte
			if f.Encoding == "base64" {
				raw, err = base64.StdEncoding.DecodeString(f.Content)
				if err != nil {
					continue
				}
			} else {
				raw = []byte(f.Content)
			}
			_ = os.WriteFile(p, raw, 0o644)
		}
		written++
	}
	return written, nil
}

// FileSeed is the {path, content} shape files_json stores — identical to
// the wizard parse payload, so the client relays it untouched.
// Português: O shape {path, content} do files_json — idêntico ao payload
// do parse, então o cliente repassa intocado.
type FileSeed struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Kind routes the file at project-creation time: "" (code) goes to
	// the code version; "help" goes to project_help_files — the manual
	// that makes the device's menu entry pretty (readme.<lang>.md and
	// <function>.<lang>.md, per bbparser.HelpFileRe). Português: "" =
	// código; "help" = manual do menu bonito.
	Kind string `json:"kind,omitempty"`
	// Encoding says how Content travels: "" = plain text; "base64" =
	// binary (images for the manual — the help PUT takes raw bytes and
	// the menu inlines them as data: URLs). Same convention as
	// CodeFileEntry. Português: "" = texto; "base64" = binário (imagens
	// do manual).
	Encoding string `json:"encoding,omitempty"`
}

// ListWizardExamples returns the visible gallery entries for a language,
// ordered, WITHOUT the file bundles (the list is a menu; the bundle
// travels on demand via GetWizardExample).
// Português: Lista as entradas visíveis por linguagem, SEM os bundles (a
// lista é um cardápio; o bundle viaja sob demanda).
func ListWizardExamples(language string) ([]WizardExample, error) {
	rows, err := DB.Query(`
		SELECT id, language, level, title, subtitle, ord
		FROM wizard_examples
		WHERE visible = 1 AND language = ?
		ORDER BY ord, title`, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WizardExample
	for rows.Next() {
		var e WizardExample
		if err := rows.Scan(&e.ID, &e.Language, &e.Level, &e.Title,
			&e.Subtitle, &e.Ord); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetWizardExample returns one entry WITH its file bundle and steps.
func GetWizardExample(id string) (*WizardExample, error) {
	var e WizardExample
	var files, steps string
	err := DB.QueryRow(`
		SELECT id, language, level, title, subtitle, ord, files_json, steps_json
		FROM wizard_examples WHERE id = ? AND visible = 1`, id).
		Scan(&e.ID, &e.Language, &e.Level, &e.Title, &e.Subtitle, &e.Ord,
			&files, &steps)
	if err != nil {
		return nil, err
	}
	e.Files = json.RawMessage(files)
	e.Steps = json.RawMessage(steps)
	return &e, nil
}

// ── Seed content ──────────────────────────────────────────────────────────────

// Blink manual — the "pretty menu" quartet: readme.<lang>.md is the
// device introduction in the sidebar; <function>.<lang>.md documents the
// block. Written for the maker (stage vocabulary, no C required).
// Português: O quarteto do "menu bonito".
