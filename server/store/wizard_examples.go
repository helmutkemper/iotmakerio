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
	"encoding/json"
	"log"
	"strings"
	"time"
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

	now := time.Now().UTC().Format(time.RFC3339)

	type seed struct {
		id, lang, level, title, subtitle string
		ord                              int
		files                            []FileSeed
		steps                            string
	}
	seeds := []seed{
		{
			id: "c-blink", lang: "c", level: "ready", ord: 1,
			title:    "Blink LED",
			subtitle: "A working device. Change the interval, re-parse, see it change.",
			files: []FileSeed{
				{Path: "blink_led.c", Content: blinkLedC},
				{Path: "readme.en.md", Content: blinkReadmeEN, Kind: "help"},
				{Path: "readme.pt-br.md", Content: blinkReadmePT, Kind: "help"},
				{Path: "blink_led.en.md", Content: blinkFnEN, Kind: "help"},
				{Path: "blink_led.pt-br.md", Content: blinkFnPT, Kind: "help"},
				{Path: "blink_wiring.png", Content: blinkWiringPNG,
					Kind: "help", Encoding: "base64"},
			},
			steps: "[]",
		},
		{
			id: "c-web-pages", lang: "c", level: "ready", ord: 2,
			title:    "Web page server",
			subtitle: "Makers wire their own html and images into your device.",
			files:    []FileSeed{{Path: "web_pages.c", Content: webPagesC}},
			steps:    "[]",
		},
		{
			id: "c-config-autocomplete", lang: "c", level: "mission", ord: 3,
			title:    "Config with autocomplete",
			subtitle: "Almost done. Finish the dictionary and watch Monaco suggest.",
			files: []FileSeed{
				{Path: "config_device.c", Content: configDeviceC},
				{Path: "config_dict.json", Content: configDictStarterJSON},
			},
			steps: configMissionSteps,
		},
	}

	for _, s := range seeds {
		filesJSON, err := json.Marshal(s.files)
		if err != nil {
			return err
		}
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO wizard_examples
				(id, language, level, title, subtitle, ord, visible,
				 files_json, steps_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
			s.id, s.lang, s.level, s.title, s.subtitle, s.ord,
			string(filesJSON), s.steps, now,
		); err != nil {
			return err
		}
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

	log.Printf("[wizard_examples] ensured %d gallery example(s)", len(seeds))
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

// blinkLedC: the "ready" example — the IoTMaker Blink. Complete, parses
// clean, and the ONE number worth changing is called out in its doc.
const blinkLedC = `// blink_led.c — your first IoTMaker device.
//
// This file already works: press Parse and a "Blink LED" card appears.
// Change interval_ms below, Parse again, and watch the card update —
// that is the whole loop: edit, parse, see.

#include <stdio.h>
#include <unistd.h>

// Blinks forever, printing ON / OFF — wire the interval from a ConstInt.
//
// label:Blink LED.
// icon:lightbulb.
// min-target:posix.
void blink_led(
    // Milliseconds between blinks. Try 100. Try 2000.
    // connection:mandatory.
    // doc:Blink interval in milliseconds.
    int interval_ms
) {
    for (;;) {
        printf("LED ON\n");
        usleep((useconds_t)interval_ms * 1000);
        printf("LED OFF\n");
        usleep((useconds_t)interval_ms * 1000);
    }
}
`

// Blink manual — the "pretty menu" quartet: readme.<lang>.md is the
// device introduction in the sidebar; <function>.<lang>.md documents the
// block. Written for the maker (stage vocabulary, no C required).
// Português: O quarteto do "menu bonito".

const blinkReadmeEN = `# Blink LED

Your first device: it prints ` + "`LED ON` / `LED OFF`" + ` forever, at the
rhythm you choose.

**How to use it on the stage**

1. Drag **Blink LED** onto the stage.
2. Drag a **Const · Int** and type the interval in milliseconds
   (try ` + "`500`" + `).
3. Wire the Const into the **interval_ms** pin.
4. Run — and change the number to feel the rhythm change.

![Blink LED wired to a Const · Int on the stage](blink_wiring.png)

That is the whole loop of IoTMaker: wire, run, tweak.
`

const blinkReadmePT = `# Blink LED

Seu primeiro device: imprime ` + "`LED ON` / `LED OFF`" + ` para sempre, no
ritmo que você escolher.

**Como usar no stage**

1. Arraste o **Blink LED** para o stage.
2. Arraste um **Const · Int** e digite o intervalo em milissegundos
   (experimente ` + "`500`" + `).
3. Ligue o Const no pino **interval_ms**.
4. Rode — e mude o número para sentir o ritmo mudar.

![Blink LED ligado num Const · Int no stage](blink_wiring.png)

Esse é o ciclo inteiro do IoTMaker: ligar, rodar, ajustar.
`

const blinkFnEN = `# blink_led

Blinks forever. One input:

| pin | type | meaning |
|-----|------|---------|
| **interval_ms** | int | milliseconds between ON and OFF |

Small numbers blink fast (100 is frantic); big numbers blink slow
(2000 is a lighthouse). The block never returns — it IS the program's
heartbeat.
`

const blinkFnPT = `# blink_led

Pisca para sempre. Uma entrada:

| pino | tipo | significado |
|------|------|-------------|
| **interval_ms** | int | milissegundos entre ON e OFF |

Números pequenos piscam rápido (100 é frenético); grandes piscam
devagar (2000 é um farol). O bloco nunca retorna — ele É o batimento
do programa.
`

// blinkWiringPNG: the field screenshot of Blink wired to a Const · Int
// (947×674, 42 KB) — captured by Kemper on 2026-07-15, the first maker
// scene ever built from a School manual. Português: O screenshot de
// campo do Blink ligado — a primeira cena montada a partir de um manual
// da School.
const blinkWiringPNG = `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+tnr8vSj98hXH78qb7z8mHTp0rnR7amOIcjst/du8sJT/yf/uvRvjZrN1ltuItf880K58dpL8va/9MKz5J+XnCennXhM3j7pDf/4+5luDof+cd/06nrzLb07SzBbLy+NCCCAAAIIIIAAAgggEEKBvFO64epLZP11B7r2kaPGyNDvfhTf96W6ulquu+piWWXlFdyd2pNPOErszuuYseNkwYIF0nvpnvLUf+9xx9md3PraXacQbQauubqsvOLysvGG65ZtVpvouW0Oaw9co0lzsIC2SQekdSaYTcMgiwACCCCAAAIIIIAAAuESaMpbUZddprdsu/VmbgEfffKFbL/bQbLvQcfKupvsLIsWLXL1fznlODnikP1dfvr0GbLNzgfKdrse5MoW0Pbvt0qD7a5zEzbvv/GMfPz282IB8nuvP5O6I9y925KpUfr07iXPPX6/fPPpazLko0HywD03yuabbJBqz5c59sg/yl677eiaey7V3Y1hhT21zs777edvyJAPB8njD98p6669pjVlJLtrbXem7Zy3Xn+FC/AzOtQW7K3Jdrfb5mdjvvbi/8Teym3NN193mSzTZ2nLyo7bbenu+rpCIzYffdL8z80SzDYCmC4IIIAAAggggAACCCAQfoFNN14/Ncknnn4xlZ8zd67suf9Rctgxp8q1N9wh3bt1c22jxoxz+1mzZksQBC6/2aYbNtjuOqZtOnXs4D5f2qpVq7TaxdluXZeUJZboLPbWZgtgrZ99VveZx+53naxsbxNedZUVpaamRtq1bSsbrr+23HXr1dJzqR6uT76NBcFt27ZxzXan2YLKHt27ydVXXCB2Xqtr166trDmgn9x7x7WuX3Jj57K70PaZYTvndtts7t6SnWxP37/8zIMueLVj7C63neexB2+Xvsv1kb7LLuPufFt/e5v38n2XsWzRU9GD2aKvgBMggAACCCCAAAIIIIBALAU+/rRpd+0GrL5qymHQq2+l8pb5edhvYuPZ/rcRI61KVtPg0T5Te91VF4nnea6uuwaeDbW7jmmbpx+9132+9LYbrkirzc7++tsI2W2fw+XDjz93jRbYbq7B8wnHHuYCWAuoj/jT6S7otrwFovvtvYvrm2/zjyuuk/898ZxrHjtuvKyzyU6y5247yMKFC8WVN95JLr4sEcRawNqxQwfXN7l5/KkXZNtdDhSbm9Vts9WmtstI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`

// configDeviceC: the mission's starting point — the Phase B demo WITHOUT
// the editor-config directives; adding them (via the port modal!) is the
// mission. Português: O ponto de partida da missão — o demo da Fase B
// SEM as diretivas de editor; adicioná-las (pelo modal!) É a missão.
const configDeviceC = `// config_device.c — mission: give the maker autocomplete.
//
// This device receives a YAML configuration and prints it. It parses
// already — but the maker's editor knows nothing about YAML yet. Your
// mission (see the panel): fill the dictionary, then point the cfg port
// at it using the port editor. No directives to memorize — the modal
// writes them for you.

#include <stdint.h>
#include <stdio.h>

// Receives the application configuration and echoes it.
//
// label:App config.
// icon:sliders.
// min-target:posix.
void app_config(
    // The configuration authored by the maker.
    // connection:mandatory.
    // slice:n.
    const uint8_t *cfg,
    unsigned long n
) {
    printf("[config] %lu byte(s) received:\n", n);
    fwrite(cfg, 1, (size_t)n, stdout);
    printf("\n[config] end.\n");
}
`

// configDictStarterJSON: one worked item + a hole to fill — completion
// over creation, even inside the mission. Português: Um item pronto + um
// buraco para preencher — conclusão em vez de criação, até na missão.
const configDictStarterJSON = `[
  {
    "label": "server",
    "insert": "server:\n  host: ${1:0.0.0.0}\n  port: ${2:9000}",
    "doc": "Server block: host and TCP port the app listens on."
  }
]
`

// configMissionSteps: the mission script — each check is a predicate the
// panel evaluates over the parse result (schema documented atop).
const configMissionSteps = `[
  {"title": "Add one more suggestion to the dictionary",
   "detail": "Open config_dict.json and add an item — copy the server one and make a timeout. Parse when done.",
   "check": {"kind": "fileIsDict", "path": "config_dict.json", "minItems": 2}},
  {"title": "Point the port at your dictionary",
   "detail": "Open the cfg port and pick yaml + config_dict.json in the Maker editor section.",
   "check": {"kind": "portHasDict", "fn": "app_config", "port": "cfg"},
   "action": {"kind": "openPort", "fn": "app_config", "port": "cfg"}},
  {"title": "Parse and try it",
   "detail": "Type ser in the port editor's try-it box — your suggestion appears.",
   "check": {"kind": "parseOk"}}
]
`

// webPagesC: the "Web page server" seed — the field-tested two-block
// device from 2026-07-13: makers wire a Data · Text (html) and a
// Data · File (image) into pages the device serves. Português: O device
// testado em campo — makers ligam Data · Text e Data · File nas páginas.
const webPagesC = `// web_pages.c — IoTMaker device: maker-composed web pages.
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// TWO blocks, one tiny server:
//
//   Register page  — endpoint + html (+ optional image). The maker feeds
//                    it with a ConstString ("home", "/home" or "/" for
//                    the site root), a Data · Text device (the html,
//                    written in Monaco) and a Data · File device (the
//                    image). Wire several instances to publish several
//                    pages.
//   Server start   — the port. Serves every registered page at its
//                    endpoint; the page's image (when wired) answers ANY
//                    path under it, so the html may reference
//                    <img src="/home/logo.png"> — or any name at all.
//                    GET / lists the pages, unless a page claims "/".
//
// Execution order: Register defaults to 1 and Server to 2 (directive
// defaults, maker-overridable) — registrations always land before the
// server blocks the loop.
//
// Português: DOIS blocos, um servidor pequeno. "Register page" recebe
// endpoint + html (+ imagem opcional) — o maker liga um ConstString, um
// Data · Text (o html escrito no Monaco) e um Data · File (a imagem);
// várias instâncias publicam várias páginas. "Server start" recebe a
// porta e serve cada página no seu endpoint; a imagem (quando ligada)
// sai em "<endpoint>/img". GET / lista as páginas. Ordem: registro
// nasce 1 e servidor nasce 2 — registros sempre antes do servidor.

#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <signal.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>

/* ── Page registry ─────────────────────────────────────────────────────────
 * Fixed table, no malloc — the IoTMaker way. Eight pages is plenty for a
 * maker portal; raising the cap is one number.
 * Português: Tabela fixa, sem malloc. Oito páginas bastam para um portal
 * de maker; subir o teto é um número. */
#define WEB_MAX_PAGES 8

#define WEB_MAX_ENDPOINT 64

typedef struct {
    char                 endpoint[WEB_MAX_ENDPOINT]; /* normalized: '/'-prefixed */
    const unsigned char *html;       /* page body (UTF-8)               */
    unsigned long        html_len;
    const unsigned char *image;      /* optional: NULL when unwired     */
    unsigned long        image_len;
} web_page_t;

static web_page_t web_pages[WEB_MAX_PAGES];
static int        web_page_count = 0;

// Registers ONE page. Wire the endpoint, the html and (optionally) an
// image; each instance of this block publishes one page. The image, when
// present, is served at "<endpoint>/img".
//
// Português: Registra UMA página. Ligue o endpoint, o html e
// (opcionalmente) uma imagem; cada instância publica uma página. A
// imagem, quando presente, sai em "<endpoint>/img".
//
// label:Register page.
// icon:file-circle-plus.
// min-target:posix.
// executionOrder:1.
void web_register_page(
    // The URL path — "home" and "/home" are both accepted (the missing
    // slash is added for you). Português: O caminho da URL — "home" e
    // "/home" são aceitos (a barra que faltar é adicionada).
    // connection:mandatory.
    // doc:URL path of the page ("home" or "/home").
    const char *endpoint,
    // The page body — wire a Data · Text device and write the html in
    // Monaco. Português: O corpo da página — ligue um Data · Text.
    // connection:mandatory.
    // slice:html_len.
    // doc:Page body (UTF-8 html).
    const uint8_t *html,
    unsigned long html_len,
    // Optional image, served at "<endpoint>/img" — wire a Data · File
    // device, or leave unwired for a text-only page.
    // Português: Imagem opcional, servida em "<endpoint>/img" — ligue um
    // Data · File, ou deixe sem fio para página só-texto.
    // slice:image_len.
    // doc:Optional image (served at endpoint/img).
    const uint8_t *image,
    unsigned long image_len
) {
    if (web_page_count >= WEB_MAX_PAGES) {
        fprintf(stderr, "[web] page table full (%d) — '%s' ignored\n",
                WEB_MAX_PAGES, endpoint ? endpoint : "?");
        return;
    }
    if (endpoint == NULL || endpoint[0] == '\0' || html == NULL) {
        fprintf(stderr, "[web] invalid registration ignored\n");
        return;
    }

    /* Forgiveness over rejection: a maker typing "home" means "/home" —
     * normalize into the slot's own buffer instead of bouncing the page
     * to a stderr nobody reads (field report 2026-07-13: an empty index
     * and a silent stderr are a terrible error message).
     * Português: Perdão em vez de rejeição: maker que digita "home" quer
     * dizer "/home" — normaliza no buffer do slot em vez de derrubar a
     * página para um stderr que ninguém lê (report de campo 2026-07-13:
     * índice vazio e stderr mudo são uma péssima mensagem de erro). */
    {
        const char *fmt = (endpoint[0] == '/') ? "%s" : "/%s";
        int n = snprintf(web_pages[web_page_count].endpoint,
                         WEB_MAX_ENDPOINT, fmt, endpoint);
        if (n >= WEB_MAX_ENDPOINT) {
            fprintf(stderr, "[web] endpoint truncated: %s\n",
                    web_pages[web_page_count].endpoint);
        }
    }
    web_pages[web_page_count].html      = html;
    web_pages[web_page_count].html_len  = html_len;
    web_pages[web_page_count].image     = image;     /* NULL when unwired */
    web_pages[web_page_count].image_len = image_len; /* 0 when unwired    */
    web_page_count++;
    printf("[web] registered %s (%lu bytes html, %lu bytes image)\n",
           endpoint, html_len, image_len);
}

/* ── The server ────────────────────────────────────────────────────────────
 * Every hard-won scar from the reference portal device is here: SIGPIPE
 * ignored (an aborting browser must not kill the process), the request
 * read loop (browser headers exceed one read), send_all (sockets
 * short-write) and the lingering close (FIN, never RST).
 * Português: Toda cicatriz do device portal de referência está aqui:
 * SIGPIPE ignorado, loop de leitura do request, send_all e fechamento
 * com drenagem. */

static void web_send_all(int fd, const unsigned char *buf, unsigned long len) {
    unsigned long off = 0;
    while (off < len) {
        long n = (long)write(fd, buf + off, (size_t)(len - off));
        if (n <= 0) {
            return;
        }
        off += (unsigned long)n;
    }
}

static void web_send(int fd, const char *status, const char *ctype,
                     const unsigned char *body, unsigned long len) {
    char head[256];
    int n = snprintf(head, sizeof(head),
                     "HTTP/1.1 %s\r\n"
                     "Content-Type: %s\r\n"
                     "Content-Length: %lu\r\n"
                     "Connection: close\r\n\r\n",
                     status, ctype, len);
    if (n > 0) {
        web_send_all(fd, (const unsigned char *)head, (unsigned long)n);
    }
    if (body != NULL && len > 0) {
        web_send_all(fd, body, len);
    }
}

/* Sniffs the image Content-Type from magic bytes — good enough for the
 * whitelist formats. Português: Deduz o Content-Type pelos magic bytes. */
static const char *web_image_type(const unsigned char *img, unsigned long len) {
    if (len >= 8 && img[0] == 0x89 && img[1] == 'P') return "image/png";
    if (len >= 3 && img[0] == 'G' && img[1] == 'I')  return "image/gif";
    if (len >= 3 && img[0] == 0xFF && img[1] == 0xD8) return "image/jpeg";
    if (len >= 4 && img[0] == '<')                    return "image/svg+xml";
    return "application/octet-stream";
}

/* GET / — a tiny auto-index of every registered page.
 * Português: GET / — um índice automático das páginas registradas. */
static void web_send_index(int fd) {
    char body[1024];
    int n = snprintf(body, sizeof(body),
                     "<!doctype html><meta charset=\"utf-8\">"
                     "<title>IoTMaker pages</title>"
                     "<h1>Pages</h1><ul>");
    for (int i = 0; i < web_page_count && n < (int)sizeof(body) - 96; i++) {
        n += snprintf(body + n, sizeof(body) - (size_t)n,
                      "<li><a href=\"%s\">%s</a></li>",
                      web_pages[i].endpoint, web_pages[i].endpoint);
    }
    n += snprintf(body + n, sizeof(body) - (size_t)n, "</ul>");
    web_send(fd, "200 OK", "text/html; charset=utf-8",
             (const unsigned char *)body, (unsigned long)n);
}

// Starts the server and blocks forever, serving every registered page at
// its endpoint (and its image at "<endpoint>/img"). Wire the port from a
// ConstInt. Register pages BEFORE this block runs — the default execution
// order (register=1, server=2) already guarantees it.
//
// Português: Sobe o servidor e bloqueia para sempre, servindo cada página
// registrada no seu endpoint (e a imagem em "<endpoint>/img"). Ligue a
// porta de um ConstInt. Registre as páginas ANTES deste bloco — a ordem
// default (registro=1, servidor=2) já garante.
//
// label:Server start.
// icon:server.
// min-target:posix.
// executionOrder:2.
void web_server_start(
    // connection:mandatory.
    // doc:TCP port to listen on (e.g. 9000).
    int port
) {
    signal(SIGPIPE, SIG_IGN);

    int srv = socket(AF_INET, SOCK_STREAM, 0);
    if (srv < 0) {
        fprintf(stderr, "[web] socket() failed\n");
        return;
    }
    int yes = 1;
    setsockopt(srv, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family      = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port        = htons((unsigned short)port);
    if (bind(srv, (struct sockaddr *)&addr, sizeof(addr)) < 0 ||
        listen(srv, 8) < 0) {
        fprintf(stderr, "[web] bind/listen on %d failed\n", port);
        close(srv);
        return;
    }
    printf("[web] serving %d page(s) on http://localhost:%d/\n",
           web_page_count, port);

    for (;;) {
        int cli = accept(srv, NULL, NULL);
        if (cli < 0) {
            continue;
        }

        /* Read until the end of the request headers (browsers exceed one
         * read). Português: Lê até o fim dos headers. */
        char req[2048];
        long got = 0;
        while (got < (long)sizeof(req) - 1) {
            long n = (long)read(cli, req + got, sizeof(req) - 1 - (size_t)got);
            if (n <= 0) {
                break;
            }
            got += n;
            req[got] = '\0';
            if (strstr(req, "\r\n\r\n") != NULL) {
                break;
            }
        }
        if (got <= 0) {
            close(cli);
            continue;
        }

        /* Parse "GET <path> " — path bounded by the two spaces.
         * Português: Extrai o path entre os dois espaços. */
        char path[256] = "/";
        if (strncmp(req, "GET ", 4) == 0) {
            const char *p   = req + 4;
            const char *end = strchr(p, ' ');
            size_t plen = end ? (size_t)(end - p) : 0;
            if (plen > 0 && plen < sizeof(path)) {
                memcpy(path, p, plen);
                path[plen] = '\0';
            }
        }

        /* Routing, two passes — forgiveness by design:
         *
         *   Pass 1: EXACT endpoint match serves the page. A page
         *           registered at "/" IS the site root; the auto-index
         *           only exists while nobody claims "/".
         *   Pass 2: the page's image answers ANY path under its
         *           endpoint — the maker writes <img src="/home/logo.png">
         *           or "/home/img" or whatever feels natural, and it just
         *           works (one image per page keeps this unambiguous;
         *           field report 2026-07-13: a fixed "/img" convention is
         *           knowledge the maker doesn't have).
         *
         * Português: Roteamento em dois passes — perdão por design.
         * Passe 1: match EXATO serve a página; página registrada em "/"
         * É a raiz do site (o índice automático só existe enquanto
         * ninguém reivindica "/"). Passe 2: a imagem da página responde
         * QUALQUER caminho sob o endpoint — o maker escreve o src que
         * lhe parecer natural e funciona (uma imagem por página mantém
         * isso sem ambiguidade; a convenção fixa "/img" era conhecimento
         * que o maker não tem). */
        int served = 0;

        for (int i = 0; i < web_page_count && !served; i++) {
            if (strcmp(path, web_pages[i].endpoint) == 0) {
                web_send(cli, "200 OK", "text/html; charset=utf-8",
                         web_pages[i].html, web_pages[i].html_len);
                served = 1;
            }
        }

        if (!served && strcmp(path, "/") == 0) {
            web_send_index(cli);
            served = 1;
        }

        for (int i = 0; i < web_page_count && !served; i++) {
            const web_page_t *pg = &web_pages[i];
            size_t elen = strlen(pg->endpoint);
            /* Under "/home" means "/home/..."; under "/" means any
             * leftover path. Português: Sob "/home" é "/home/..."; sob
             * "/" é qualquer caminho restante. */
            int under = (strcmp(pg->endpoint, "/") == 0)
                            ? 1
                            : (strncmp(path, pg->endpoint, elen) == 0 &&
                               path[elen] == '/');
            if (under && pg->image != NULL && pg->image_len > 0) {
                web_send(cli, "200 OK",
                         web_image_type(pg->image, pg->image_len),
                         pg->image, pg->image_len);
                served = 1;
            }
        }

        if (!served) {
            static const unsigned char nf[] = "not found";
            web_send(cli, "404 Not Found", "text/plain",
                     nf, (unsigned long)(sizeof(nf) - 1));
        }

        /* Lingering close: FIN after the full response, never RST.
         * Português: Fecha com drenagem — FIN, nunca RST. */
        shutdown(cli, SHUT_WR);
        {
            char sink[512];
            int budget = 16;
            while (budget-- > 0 && read(cli, sink, sizeof(sink)) > 0) {
            }
        }
        close(cli);
    }
}
`
