// server/store/project_code_files.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"sort"
)

// Multi-file device sources — the snapshot model.
//
// English:
//
//	A device project's source is a SET of files (a specialist's real C
//	project has api.h, core.c, util.c — not one blob). The storage model:
//
//	    project_code_versions   → the snapshot HEADER (id, project, number,
//	                              parse flag, timestamp)
//	    project_code_files      → the snapshot CONTENT (one row per file,
//	                              keyed by version_id + path)
//
//	Every save writes a NEW version row plus its full file set — a version
//	is an immutable snapshot, so "restore version 3" means "read version
//	3's rows", never a diff replay. Deleting a version cascades to its
//	files; deleting a project cascades through versions to files.
//
//	Why a child table and not a JSON blob in the version row: the relational
//	shape is the same proven pattern as project help files (path + content
//	rows per owner), it lets the export builder stream file-by-file (memory
//	proportional to the LARGEST file, not the SUM — the doctrine already
//	written in projectexport/builder.go), and it keeps SQL able to answer
//	"which projects have a non-empty latest version" without deserialising
//	anything.
//
//	Path rules are enforced at the HTTP boundary (see projectapi's
//	validateCodeFilePath): relative, no "..", no absolute, no backslashes,
//	unique case-insensitively, extension whitelisted by the project's
//	language. The store trusts its callers and stores paths verbatim.
//
// Português:
//
//	O fonte de um projeto de device é um CONJUNTO de arquivos (um projeto C
//	real tem api.h, core.c, util.c — não um blob). Modelo: a versão é o
//	CABEÇALHO do snapshot; os arquivos são linhas filhas (version_id +
//	path). Cada save grava uma versão nova com o conjunto completo —
//	snapshot imutável; restaurar = ler as linhas daquela versão. Por quê
//	tabela filha e não blob JSON: é o mesmo padrão provado dos help files,
//	permite streaming por arquivo no export (memória proporcional ao MAIOR
//	arquivo) e mantém o SQL capaz de responder consultas sem desserializar.
//	As regras de caminho são impostas na borda HTTP; o store grava verbatim.

// CodeFileEntry is one file of a snapshot: a project-relative path and its
// full content. The Sort field preserves the specialist's tab order in the
// editor — cosmetic, but losing it on every reload would feel broken.
//
// Português: Um arquivo do snapshot: caminho relativo ao projeto + conteúdo.
// Sort preserva a ordem das abas do editor.
type CodeFileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Sort    int    `json:"sort,omitempty"`
}

// MigrateProjectCodeFiles creates the snapshot-content table. Idempotent
// (IF NOT EXISTS); registered in the db.go migration chain. There is no
// backfill: the project has no legacy data to carry (pre-release decision,
// 2026-07 — see docs/CLAUDE_C99_DEVICE_SUPPORT.md, Slice 6).
//
// Português: Cria a tabela de conteúdo dos snapshots. Idempotente; sem
// backfill — não há dados legados a carregar (decisão pré-release, 2026-07).
func MigrateProjectCodeFiles() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS project_code_files (
			version_id TEXT NOT NULL
				REFERENCES project_code_versions(id) ON DELETE CASCADE,
			path       TEXT NOT NULL,
			content    TEXT NOT NULL DEFAULT '',
			sort       INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (version_id, path)
		)`)
	return err
}

// insertCodeFilesTx writes a snapshot's file set inside the caller's
// transaction — always alongside the version-row insert, never alone, so a
// snapshot can never exist half-written. Sort is stamped from slice order:
// the caller's ordering IS the tab ordering.
//
// Português: Grava o conjunto de arquivos dentro da transação do chamador —
// sempre junto do insert da versão, nunca sozinho. Sort vem da ordem do
// slice: a ordem do chamador É a ordem das abas.
func insertCodeFilesTx(tx txExecer, versionID string, files []CodeFileEntry) error {
	for i, f := range files {
		if _, err := tx.Exec(`
			INSERT INTO project_code_files (version_id, path, content, sort)
			VALUES (?, ?, ?, ?)`,
			versionID, f.Path, f.Content, i,
		); err != nil {
			return err
		}
	}
	return nil
}

// txExecer is the minimal execution surface insertCodeFilesTx needs — both
// *sql.Tx and *sql.DB satisfy it, which keeps the helper testable and lets
// single-statement callers skip the transaction ceremony when appropriate.
type txExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// loadCodeFiles reads one snapshot's file set, ordered by the stored sort
// (then path, for determinism if two rows ever tie). Returns an empty —
// never nil — slice for a version with no rows, so callers can range
// without a nil check.
//
// Português: Lê o conjunto de arquivos de um snapshot, na ordem gravada.
// Retorna slice vazio (nunca nil) para versão sem linhas.
func loadCodeFiles(versionID string) ([]CodeFileEntry, error) {
	rows, err := DB.Query(`
		SELECT path, content, sort
		FROM project_code_files
		WHERE version_id = ?
		ORDER BY sort ASC, path ASC`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := []CodeFileEntry{}
	for rows.Next() {
		var f CodeFileEntry
		if err := rows.Scan(&f.Path, &f.Content, &f.Sort); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	// Defensive re-sort: the ORDER BY already guarantees this, but the
	// contract ("slice order = tab order") is cheap to enforce locally and
	// survives a future engine whose driver reorders differently.
	sort.SliceStable(files, func(i, j int) bool { return files[i].Sort < files[j].Sort })
	return files, rows.Err()
}
