// server/handler/controlapi/wizard_examples.go — School gallery administration.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The School's content pipeline (wizard UI plan, 2026-07-14): examples are
// SNAPSHOTS with provenance — "from-project" copies the latest saved code
// version of a real project into the gallery, frozen. A live reference
// would let an edit to the source project silently break a mission's
// predicates; freezing makes updating a deliberate act (re-snapshot).
// All routes gate on PermSchoolEdit.
//
// Português: O duto de conteúdo da School: exemplos são SNAPSHOTS com
// proveniência — "from-project" copia a última versão salva de um projeto
// real para a galeria, congelada. Referência viva deixaria uma edição no
// projeto-fonte quebrar os predicados de uma missão em silêncio; congelar
// torna a atualização um ato deliberado (re-snapshot).

package controlapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	bbparser "server/codegen/blackbox"
	"server/permission"
	"server/store"
)

// RegisterWizardExamples mounts the School admin endpoints on the
// /api/control/v1 group. Called from server/cmd/server/main.go.
func RegisterWizardExamples(g *echo.Group) {
	gate := RequireControlToken(permission.PermSchoolEdit)
	g.GET("/school/examples", handleSchoolList, gate)
	g.GET("/school/examples/:id", handleSchoolGet, gate)
	g.PUT("/school/examples/:id", handleSchoolUpsert, gate)
	g.DELETE("/school/examples/:id", handleSchoolDelete, gate)
	g.POST("/school/examples/from-project", handleSchoolFromProject, gate)
	g.POST("/school/parse", handleSchoolParse, gate)
	g.GET("/school/projects", handleSchoolProjects, gate)
}

// handleSchoolProjects feeds the snapshot modal's project PICKER —
// names first, ids as fine print. Português: Alimenta o SELETOR do
// modal de snapshot — nomes primeiro, id em letra miúda.
func handleSchoolProjects(c echo.Context) error {
	list, err := store.AdminListProjectsLight()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, map[string]any{"projects": list})
}

// schoolParseBody: the lesson editor's Test mission sends the CURRENT
// Monaco buffers — the author verifies the parse-dependent predicates
// (portHasDict, sliceCollapsed, parseOk) without leaving the page.
// Português: O Test mission manda os buffers ATUAIS do Monaco — o autor
// verifica os predicados dependentes de parse sem sair da página.
type schoolParseBody struct {
	Language string `json:"language"`
	Files    []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"files"`
}

// handleSchoolParse runs the SAME dispatch the wizard uses and returns
// the port facts the predicates read. Warnings don't fail the test —
// parseOk means "a device came out". Português: Roda o MESMO dispatch do
// wizard e devolve os fatos de porta que os predicados leem. Warnings
// não derrubam o teste — parseOk significa "saiu um device".
func handleSchoolParse(c echo.Context) error {
	var b schoolParseBody
	if err := c.Bind(&b); err != nil {
		return fail(c, http.StatusBadRequest, "invalid body")
	}
	files := make([]bbparser.FileEntry, 0, len(b.Files))
	for _, f := range b.Files {
		files = append(files, bbparser.FileEntry{Path: f.Path, Content: f.Content})
	}
	def, perr := bbparser.ParseForLanguageFiles(
		b.Language, files, bbparser.DefaultParserLimits())
	if def == nil {
		msg := "parse failed"
		if perr != nil {
			msg = perr.Error()
		}
		return ok(c, map[string]any{"parseOk": false, "error": msg})
	}

	type portFact struct {
		Name         string `json:"name"`
		GoType       string `json:"goType"`
		SliceLenName string `json:"sliceLenName,omitempty"`
		EditorLang   string `json:"editorLang,omitempty"`
		EditorDict   string `json:"editorDict,omitempty"`
	}
	type fnFact struct {
		Name   string     `json:"name"`
		Inputs []portFact `json:"inputs"`
	}
	fns := make([]fnFact, 0, len(def.Functions))
	for _, fn := range def.Functions {
		ff := fnFact{Name: fn.Name}
		for _, p := range fn.Inputs {
			ff.Inputs = append(ff.Inputs, portFact{
				Name: p.Name, GoType: p.GoType,
				SliceLenName: p.SliceLenName,
				EditorLang:   p.EditorLang,
				EditorDict:   p.EditorDict,
			})
		}
		fns = append(fns, ff)
	}
	return ok(c, map[string]any{"parseOk": true, "functions": fns})
}

func handleSchoolList(c echo.Context) error {
	list, err := store.AdminListWizardExamples()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, map[string]any{"examples": list})
}

func handleSchoolGet(c echo.Context) error {
	e, visible, src, err := store.GetWizardExampleAny(c.Param("id"))
	if err != nil {
		return fail(c, http.StatusNotFound, "example not found")
	}
	return ok(c, map[string]any{
		"example": e, "visible": visible, "sourceProjectId": src,
	})
}

// schoolUpsertBody is the admin form's word — the row is written whole.
type schoolUpsertBody struct {
	Language        string          `json:"language"`
	Level           string          `json:"level"`
	Title           string          `json:"title"`
	Subtitle        string          `json:"subtitle"`
	Ord             int             `json:"ord"`
	Visible         bool            `json:"visible"`
	Files           json.RawMessage `json:"files"`
	Steps           json.RawMessage `json:"steps"`
	SourceProjectID string          `json:"sourceProjectId"`
}

func handleSchoolUpsert(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return fail(c, http.StatusBadRequest, "missing example id")
	}
	var b schoolUpsertBody
	if err := c.Bind(&b); err != nil {
		return fail(c, http.StatusBadRequest, "invalid body")
	}
	if b.Language != "c" && b.Language != "go" {
		return fail(c, http.StatusBadRequest, `language must be "c" or "go"`)
	}
	if b.Level != "ready" && b.Level != "mission" {
		return fail(c, http.StatusBadRequest, `level must be "ready" or "mission"`)
	}
	if strings.TrimSpace(b.Title) == "" {
		return fail(c, http.StatusBadRequest, "title is required")
	}
	if len(b.Files) > 0 && !json.Valid(b.Files) {
		return fail(c, http.StatusBadRequest, "files is not valid JSON")
	}
	if len(b.Steps) > 0 && !json.Valid(b.Steps) {
		return fail(c, http.StatusBadRequest, "steps is not valid JSON")
	}
	e := &store.WizardExample{
		ID: id, Language: b.Language, Level: b.Level,
		Title: b.Title, Subtitle: b.Subtitle, Ord: b.Ord,
		Files: b.Files, Steps: b.Steps,
	}
	if err := store.UpsertWizardExample(e, b.Visible, b.SourceProjectID); err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, map[string]any{"id": id})
}

func handleSchoolDelete(c echo.Context) error {
	if err := store.DeleteWizardExample(c.Param("id")); err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, map[string]any{"deleted": true})
}

// schoolFromProjectBody: the snapshot request. The example id defaults to
// the project id, prefixed by language, so re-snapshotting the same
// project UPDATES the same gallery entry.
type schoolFromProjectBody struct {
	ProjectID string `json:"projectId"`
	ID        string `json:"id"`
	Level     string `json:"level"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	Ord       int    `json:"ord"`
	Visible   bool   `json:"visible"`
}

func handleSchoolFromProject(c echo.Context) error {
	var b schoolFromProjectBody
	if err := c.Bind(&b); err != nil {
		return fail(c, http.StatusBadRequest, "invalid body")
	}
	if strings.TrimSpace(b.ProjectID) == "" {
		return fail(c, http.StatusBadRequest, "projectId is required")
	}

	p, err := store.GetProjectByID(b.ProjectID)
	if err != nil || p == nil {
		return fail(c, http.StatusNotFound, "project not found")
	}
	v, err := store.GetLatestProjectCodeVersion(b.ProjectID)
	if err != nil || v == nil || len(v.Files) == 0 {
		return fail(c, http.StatusNotFound,
			"project has no saved code version to snapshot")
	}

	// Freeze the bundle: paths + contents only — Sort/Encoding are editor
	// concerns, not gallery ones. Português: Congela o bundle: caminho +
	// conteúdo; Sort/Encoding são assunto do editor, não da galeria.
	seeds := make([]store.FileSeed, 0, len(v.Files))
	for _, f := range v.Files {
		seeds = append(seeds, store.FileSeed{Path: f.Path, Content: f.Content})
	}
	filesJSON, err := json.Marshal(seeds)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}

	lang := p.ProgrammingLanguageID
	if lang != "c" && lang != "go" {
		lang = "go"
	}
	id := strings.TrimSpace(b.ID)
	if id == "" {
		id = lang + "-" + b.ProjectID
	}
	level := b.Level
	if level != "mission" {
		level = "ready"
	}
	title := strings.TrimSpace(b.Title)
	if title == "" {
		title = p.Name
	}

	e := &store.WizardExample{
		ID: id, Language: lang, Level: level,
		Title: title, Subtitle: b.Subtitle, Ord: b.Ord,
		Files: filesJSON, Steps: json.RawMessage("[]"),
	}
	if err := store.UpsertWizardExample(e, b.Visible, b.ProjectID); err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, map[string]any{
		"id": id, "files": len(seeds), "language": lang,
	})
}
