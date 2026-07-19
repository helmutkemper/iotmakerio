// server/store/wires_functions.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package store

// wires_functions.go — persistence for GRAPHICAL functions (My Items,
// P3 of the "my_function becomes a device" arc, Kemper 2026-07-19).
//
// English:
//
//	A graphical function is a blackboxes row whose parsed_json carries a
//	wires-origin BlackBoxDef (origin=="wires", scene==the captured
//	sub-scene). No schema change: parsed_json was already the def's
//	home. Two laws live here:
//
//	  IMMUTABILITY — published is frozen (Kemper: "não temos como
//	  escrever testes de funções, no momento"). Saving a name the user
//	  already published is refused with ErrWiresNameTaken; evolution is
//	  an explicit new item (my_function_v2).
//
//	  RESOLUTION — LoadBlackBoxDefsForScene (blackbox.go) gains a third
//	  source: StatementFunctionCall instances pull wires defs by name.
//
// Português:
//
//	Função gráfica é uma linha de blackboxes cujo parsed_json leva um
//	BlackBoxDef de origem-fios. Sem migração: parsed_json já era a casa
//	da def. Duas leis: IMUTABILIDADE — publicado é congelado; evoluir é
//	item novo (v2) explícito. RESOLUÇÃO — o loader ganha uma terceira
//	fonte: instâncias StatementFunctionCall puxam defs de fios pelo
//	nome.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	bbparser "server/codegen/blackbox"
)

// ErrWiresNameTaken — the immutability law's voice: this user already
// published a graphical function under this name. Português: A voz da
// lei de imutabilidade — o usuário já publicou este nome.
var ErrWiresNameTaken = errors.New("a graphical function with this name is already published — published items are frozen; save your changes as a new name (for example my_function_v2)")

// SaveWiresFunction persists a wires-origin def as a My Items row and
// returns the minted id. The def arrives ALREADY validated by
// codegen.ExtractFunctionDef — this layer only enforces immutability
// and writes. Português: Persiste a def como linha de My Items e
// devolve o id cunhado. A def chega VALIDADA pelo extrator; aqui só a
// imutabilidade e a escrita.
func SaveWiresFunction(userID, language string, def *bbparser.BlackBoxDef) (string, error) {
	if def == nil || def.Origin != "wires" || def.Name == "" {
		return "", errors.New("not a wires-origin function def")
	}

	taken, err := wiresNameTaken(userID, def.Name)
	if err != nil {
		return "", err
	}
	if taken {
		return "", ErrWiresNameTaken
	}

	parsed, err := json.Marshal(def)
	if err != nil {
		return "", err
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := "wf_" + hex.EncodeToString(b)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = DB.Exec(`
		INSERT INTO blackboxes
			(id, user_id, display_name, display_name_human, status,
			 visibility, ready_to_use, parsed_json, programming_language_id,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, 'ready', 'private', 1, ?, ?, ?, ?)`,
		id, userID, def.Name, def.Name, string(parsed), language, now, now)
	if err != nil {
		return "", err
	}
	return id, nil
}

// wiresNameTaken reports whether this user already published a
// wires-origin def under this name. Português: Se o usuário já
// publicou este nome como origem-fios.
func wiresNameTaken(userID, name string) (bool, error) {
	rows, err := DB.Query(
		`SELECT parsed_json FROM blackboxes
		 WHERE user_id = ? AND display_name = ? AND blocked = 0`,
		userID, name)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			return false, err
		}
		var probe struct {
			Origin string `json:"origin"`
		}
		if json.Unmarshal([]byte(pj), &probe) == nil && probe.Origin == "wires" {
			return true, nil
		}
	}
	return false, rows.Err()
}

// LoadWiresDefsByNames returns the wires-origin defs matching the given
// function names — the loader's THIRD source, called from
// LoadBlackBoxDefsForScene when the scene carries
// StatementFunctionCall instances. Keys are the function names.
// Português: As defs de origem-fios pelos nomes — a TERCEIRA fonte do
// loader, para cenas com instâncias StatementFunctionCall.
func LoadWiresDefsByNames(names map[string]bool) (map[string]*bbparser.BlackBoxDef, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := map[string]*bbparser.BlackBoxDef{}
	rows, err := DB.Query(
		`SELECT parsed_json FROM blackboxes WHERE blocked = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			return nil, err
		}
		var def bbparser.BlackBoxDef
		if json.Unmarshal([]byte(pj), &def) != nil {
			continue
		}
		if def.Origin == "wires" && names[def.Name] && out[def.Name] == nil {
			d := def
			out[def.Name] = &d
		}
	}
	return out, rows.Err()
}
