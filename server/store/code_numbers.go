// server/store/code_numbers.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"sort"
	"time"
)

// Sequential CODE NUMBERS — the "47" in iotm_47_print_float.
//
// English:
//
//	Every black-box source (a wizard device project, or a marketplace
//	black-box row) is assigned a small sequential integer at creation time.
//	The multi-file C export composes every generated name from it — folder
//	iotm_47/, files iotm_47.{c,h}, symbol prefix iotm_47_, guard IOTM_47_H
//	(see codegen/blackbox/naming.go) — instead of the 32-hex database id,
//	which produced unreadable tabs and unreadable linker errors.
//
//	THE CONTRACT (engine-independent — the database engine is expected to
//	change; only this contract and its tests must survive the migration):
//
//	  1. Numbers are positive and STRICTLY INCREASING in allocation order.
//	  2. A number, once allocated, is NEVER reused — not even after its
//	     owner is deleted. A recycled number would make code exported
//	     before the deletion and code exported after it share an identity:
//	     same folder name, same symbol prefix, silently different content.
//	  3. Allocation is IDEMPOTENT per full id: re-allocating for an id that
//	     already holds a number returns that number. This is what makes the
//	     creation hooks safe under worker retries and re-runs.
//	  4. Allocation is ATOMIC under concurrent creators.
//	  5. ONE counter serves every creation flow. Wizard projects and
//	     marketplace black-boxes both feed BlackBoxDef.ID, so they draw
//	     from the same sequence — two kinds, one identity space.
//
//	The CodeNumberAllocator interface below IS that contract in Go form;
//	code_numbers_test.go asserts it against the current implementation and
//	must be re-pointed (not rewritten) at any future one.
//
// Português:
//
//	Toda fonte de black-box (projeto de device do wizard ou linha de
//	black-box do marketplace) recebe um inteiro sequencial pequeno na
//	criação. O export C multiarquivo compõe todos os nomes gerados a partir
//	dele (pasta iotm_47/, arquivos iotm_47.{c,h}, prefixo iotm_47_, guard
//	IOTM_47_H) em vez do id de 32 hex, que produzia abas e erros de linker
//	ilegíveis.
//
//	O CONTRATO (independente de motor — o banco vai mudar; só o contrato e
//	seus testes precisam sobreviver à migração): (1) números positivos e
//	estritamente crescentes; (2) número NUNCA é reusado, nem após a deleção
//	do dono — reuso faria código antigo e novo compartilharem identidade;
//	(3) alocação IDEMPOTENTE por id (retries seguros); (4) atômica sob
//	concorrência; (5) UM contador para todos os fluxos de criação —
//	projetos e black-boxes alimentam o mesmo espaço de identidade.

// Code-number kinds: which creation flow allocated the number. Recorded for
// audit/debug only — the contract's counter is shared across kinds on
// purpose, so the kind never participates in uniqueness.
//
// Português: Tipo do fluxo de criação. Registrado só para auditoria — o
// contador é compartilhado entre os tipos de propósito.
const (
	CodeKindProject  = "project"
	CodeKindBlackBox = "blackbox"
)

// CodeNumberAllocator is the engine-agnostic contract (see the package doc
// above for the five clauses). Consumers that need allocation — the creation
// flows — should accept this interface, not call the package functions, so
// the future database migration swaps one implementation instead of touching
// call sites.
//
// Português: O contrato agnóstico de motor (cinco cláusulas na doutrina
// acima). Consumidores devem aceitar a interface, não chamar as funções de
// pacote, para a migração futura trocar uma implementação só.
type CodeNumberAllocator interface {
	// AllocateCodeNumber returns fullID's code number, allocating the next
	// one in the sequence when fullID has none yet (idempotent — clause 3).
	AllocateCodeNumber(fullID, kind string) (int64, error)

	// CodeNumberFor looks up fullID's number without allocating.
	// ok reports whether one exists.
	CodeNumberFor(fullID string) (n int64, ok bool, err error)
}

// Allocator is the CodeNumberAllocator backed by the store's current engine.
// It is what the creation flows receive today; a future engine replaces the
// value, not the consumers.
//
// Português: O CodeNumberAllocator do motor atual. Um motor futuro troca o
// valor, não os consumidores.
var Allocator CodeNumberAllocator = sqliteAllocator{}

// sqliteAllocator adapts the package-level functions (the store's house
// style: package funcs over the global DB) to the CodeNumberAllocator
// contract. Disposable by design — it dies with the engine.
type sqliteAllocator struct{}

func (sqliteAllocator) AllocateCodeNumber(fullID, kind string) (int64, error) {
	return AllocateCodeNumber(fullID, kind)
}

func (sqliteAllocator) CodeNumberFor(fullID string) (int64, bool, error) {
	return CodeNumberFor(fullID)
}

// AllocateCodeNumber implements the contract for the current engine.
//
// Two steps, both idempotent: an insert that yields on conflict (so an id
// that already holds a number is untouched), then the read-back. SQLite
// serialises writers, so the pair cannot interleave into a double
// allocation; the contract test exercises this with concurrent goroutines
// anyway, because the property must hold for ANY engine.
//
// Português: Implementa o contrato no motor atual. Dois passos idempotentes:
// insert que cede em conflito, depois a leitura. O SQLite serializa
// escritores; o teste de contrato exercita concorrência mesmo assim, porque
// a propriedade deve valer em QUALQUER motor.
func AllocateCodeNumber(fullID, kind string) (int64, error) {
	// Fast path FIRST, and not only for speed: an INSERT … ON CONFLICT DO
	// NOTHING against an AUTOINCREMENT column BURNS a sequence step on the
	// conflicting attempt (SQLite reserves the rowid before the conflict
	// clause ignores the row — TestCodeNumbers_IdempotentPerID caught it:
	// a no-op re-allocation advanced the counter). Reading first makes the
	// idempotent path WRITE-FREE, so re-allocation can never move the
	// sequence. Two racing FIRST allocations of the same brand-new id can
	// still burn one step when the loser's insert conflicts — harmless:
	// numbers stay unique and increasing, and gaps are already a fact of
	// life (deletes leave them; the contract never promises density).
	//
	// Português: SELECT primeiro, e não só por velocidade: INSERT com ON
	// CONFLICT DO NOTHING contra AUTOINCREMENT QUEIMA um passo da
	// sequência na tentativa conflitante (o teste pegou: re-alocação
	// no-op avançava o contador). Ler primeiro torna o caminho
	// idempotente livre de escrita. Corrida entre duas PRIMEIRAS
	// alocações do mesmo id pode queimar um passo — inofensivo: números
	// seguem únicos e crescentes, e buracos já existem (deletes).
	if n, found, err := CodeNumberFor(fullID); err != nil {
		return 0, err
	} else if found {
		return n, nil
	}
	_, err := DB.Exec(`
		INSERT INTO code_numbers (full_id, kind, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(full_id) DO NOTHING`,
		fullID, kind, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	var n int64
	err = DB.QueryRow(
		`SELECT n FROM code_numbers WHERE full_id = ?`, fullID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// CodeNumberFor implements the contract's lookup for the current engine.
func CodeNumberFor(fullID string) (int64, bool, error) {
	var n int64
	err := DB.QueryRow(
		`SELECT n FROM code_numbers WHERE full_id = ?`, fullID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

// codeNumberString is the def-loader's view of the registry: fullID's number
// as the canonical decimal string the BlackBoxDef.CodeID contract expects,
// or "" when there is none. Absence AND lookup failure both degrade to "":
// the naming layer then falls back to the long full-id family, so an export
// is never broken over what is, in the end, a cosmetic upgrade — the loader
// stays a pure read path (backfill, not the loader, heals missing numbers).
//
// Português: Visão do loader: o número como string decimal canônica do
// contrato BlackBoxDef.CodeID, ou "" quando não há. Ausência E falha
// degradam para "" — o naming cai na família longa do id completo; export
// nunca quebra por cosmética, e o loader continua leitura pura (quem cura
// ausência é o backfill, não o loader).
func codeNumberString(fullID string) string {
	n, ok, err := CodeNumberFor(fullID)
	if err != nil || !ok {
		return ""
	}
	return formatInt64(n)
}

// formatInt64 renders n in base 10 without importing strconv into this file's
// hot path readers — kept trivial on purpose.
func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// MigrateCodeNumbers creates the registry table and backfills numbers for
// every project and black-box row that predates it, ordered by creation
// time — so the oldest existing device gets the lowest number, matching what
// allocation-at-creation would have produced had the registry always
// existed. Idempotent: the table create is IF NOT EXISTS and the backfill
// inserts yield on conflict, so re-running (every server start runs the
// migration chain) is a no-op.
//
// AUTOINCREMENT is deliberate and load-bearing: it is how THIS engine honors
// contract clause 2 (never reuse). Without it, SQLite reuses max(rowid)+1
// after the highest row is deleted; with it, the sequence table guarantees
// monotonic, never-recycled values. A future engine must provide its own
// equivalent guarantee — that requirement lives in the contract, not here.
//
// The backfill ordering is done in Go (read both tables, sort by created_at,
// insert one by one inside a transaction) rather than with INSERT..SELECT
// ORDER BY, because SQLite does not promise to honor ORDER BY when feeding
// an INSERT — and assignment order is exactly what the backfill is about.
//
// Português: Cria a tabela do registro e numera as linhas pré-existentes de
// projetos e black-boxes por ordem de criação — o device mais antigo ganha o
// menor número, como se o registro sempre tivesse existido. Idempotente
// (re-rodar é no-op). O AUTOINCREMENT é proposital e estrutural: é como ESTE
// motor honra a cláusula 2 (nunca reusar); sem ele o SQLite recicla
// max(rowid)+1 após deleção. A ordenação do backfill é feita em Go porque o
// SQLite não promete honrar ORDER BY alimentando um INSERT — e ordem de
// atribuição é exatamente o assunto do backfill.
func MigrateCodeNumbers() error {
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS code_numbers (
			n          INTEGER PRIMARY KEY AUTOINCREMENT,
			full_id    TEXT NOT NULL UNIQUE,
			kind       TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`); err != nil {
		return err
	}

	// Backfill candidates: (full_id, kind, created_at) from both owner
	// tables. COALESCE guards ancient rows with a NULL created_at — they
	// sort first (empty string), which is the honest position for "older
	// than our timestamps".
	type candidate struct {
		fullID, kind, createdAt string
	}
	var candidates []candidate

	collect := func(query, kind string) error {
		rows, err := DB.Query(query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c candidate
			c.kind = kind
			if err := rows.Scan(&c.fullID, &c.createdAt); err != nil {
				return err
			}
			candidates = append(candidates, c)
		}
		return rows.Err()
	}
	if err := collect(
		`SELECT id, COALESCE(created_at, '') FROM projects`,
		CodeKindProject,
	); err != nil {
		return err
	}
	if err := collect(
		`SELECT id, COALESCE(created_at, '') FROM blackboxes`,
		CodeKindBlackBox,
	); err != nil {
		return err
	}

	// RFC3339 strings sort chronologically as plain strings; ties (same
	// timestamp across tables) break by id for determinism.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].createdAt != candidates[j].createdAt {
			return candidates[i].createdAt < candidates[j].createdAt
		}
		return candidates[i].fullID < candidates[j].fullID
	})

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range candidates {
		if _, err := tx.Exec(`
			INSERT INTO code_numbers (full_id, kind, created_at)
			VALUES (?, ?, ?)
			ON CONFLICT(full_id) DO NOTHING`,
			c.fullID, c.kind, now,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
