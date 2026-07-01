// server/cmd/worker/main.go — IoTMaker Portal background worker.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Queues and tasks handled:
//
//	"codegen" queue (highest priority — interactive, maker waits):
//	  codegen:run     — run the codegen pipeline (graph → IR → backend)
//	                    against a scene exported by the IDE, publish the
//	                    resulting source plus diagnostics to Redis under
//	                    codegen:job:{id}:result for SSE delivery.
//
//	"devices" queue:
//	  device:github   — download GitHub release ZIP, parse all IDS structs,
//	                    extract markdown help files and images, save to DB.
//
//	"templates" queue:
//	  template:github — download GitHub release ZIP, parse full Go project,
//	                    save parsed definition to DB (status=ready|error)
//
//	"blackbox" queue (legacy — kept for backward compatibility):
//	  blackbox:analyze  — go/types semantic analysis, result → Redis
//	  blackbox:save     — legacy SPA save
//	  blackbox:process  — new API full parse + save
//
//	"default" queue:
//	  (reserved for future use)
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"server/codegen"
	bbparser "server/codegen/blackbox"
	"server/codegen/graph"
	"server/config"
	"server/store"
	"server/tasks"
)

const (
	// jobResultTTL is how long job results live in Redis.
	jobResultTTL = 5 * time.Minute

	// maxGitHubZipBytes caps the download size (50 MB).
	maxGitHubZipBytes = 50 * 1024 * 1024

	// maxUncompressedBytes caps total decompressed content to prevent ZIP bombs.
	maxUncompressedBytes = 200 * 1024 * 1024
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("[worker] create data dir: %v", err)
	}

	// The worker shares the same SQLite file as the server. On a cold start
	// both processes boot simultaneously, but only the server should run
	// migrations (CREATE TABLE, seeds, etc). The worker retries with a delay
	// so the server finishes first. Without this, both processes race to
	// create/migrate the same file and cause "disk I/O error" or "database
	// disk image is malformed" corruption.
	const maxRetries = 10
	const retryDelay = 2 * time.Second
	var dbErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		dbErr = store.Open(cfg.DBPath)
		if dbErr == nil {
			break
		}
		log.Printf("[worker] open database attempt %d/%d: %v — retrying in %v",
			attempt, maxRetries, dbErr, retryDelay)
		// Close the handle if it was partially opened to avoid leaking.
		if store.DB != nil {
			store.DB.Close()
			store.DB = nil
		}
		time.Sleep(retryDelay)
	}
	if dbErr != nil {
		log.Fatalf("[worker] open database failed after %d attempts: %v", maxRetries, dbErr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("[worker] redis unavailable: %v", err)
	}
	log.Printf("[worker] redis connected at %s", cfg.RedisAddr)

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr},
		asynq.Config{
			Queues: map[string]int{
				tasks.QueueCodegen:   5, // highest — interactive (maker waits, every second matters)
				tasks.QueueDevices:   4, // interactive (specialist waits, but parse runs once per release)
				tasks.QueueTemplates: 2, // lower — background parse
				tasks.QueueCleanup:   1, // lowest — hygiene tasks (wizard draft GC)
				"default":            1,
			},
			RetryDelayFunc: func(n int, _ error, _ *asynq.Task) time.Duration {
				return time.Duration(n*n) * 5 * time.Second
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				log.Printf("[worker] task %q failed: %v", task.Type(), err)
			}),
		},
	)

	mux := asynq.NewServeMux()

	// Codegen handler — interactive, lives on its own dedicated queue so
	// neither device nor template parsing can starve a maker who just
	// clicked "Export → Go Code".
	mux.HandleFunc(tasks.TypeCodegenRun, makeCodegenHandler(rdb))

	// GitHub-based handlers (new flow).
	mux.HandleFunc(tasks.TypeDeviceGitHub, makeDeviceGitHubHandler(rdb))
	mux.HandleFunc(tasks.TypeTemplateGitHub, makeTemplateGitHubHandler(cfg, rdb))

	// Hygiene handlers — registered the same way as the work handlers
	// so the asynq.Server hosts everything in one process. The
	// scheduler that emits these tasks lives below.
	mux.HandleFunc(tasks.TypeWizardCleanup, tasks.MakeWizardCleanupHandler())

	// Periodic-task scheduler. The Asynq scheduler is a separate
	// goroutine that emits tasks on a cron schedule into the queue;
	// the asynq.Server above then picks them up like any other task.
	// Splitting scheduler-from-server is the asynq idiom — keeping
	// both in one process keeps the deployment story simple (we have
	// one binary, one container, one Redis).
	scheduler := asynq.NewScheduler(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr},
		nil,
	)
	// "@daily" runs once a day at midnight UTC. The first run happens
	// at the next midnight after process start, NOT immediately. If a
	// cold-start cleanup ever becomes important, enqueue
	// tasks.NewWizardCleanupTask() once on boot before scheduler.Run.
	if _, err := scheduler.Register("@daily", tasks.NewWizardCleanupTask()); err != nil {
		log.Fatalf("[worker] scheduler register wizard:cleanup: %v", err)
	}
	go func() {
		log.Println("[worker] scheduler started — wizard:cleanup runs @daily")
		if err := scheduler.Run(); err != nil {
			log.Printf("[worker] scheduler exited: %v", err)
		}
	}()

	go func() {
		log.Println("[worker] started — waiting for tasks…")
		if err := srv.Run(mux); err != nil {
			log.Fatalf("[worker] server run error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[worker] shutting down…")
	scheduler.Shutdown()
	srv.Shutdown()
	log.Println("[worker] shutdown complete")
}

// =============================================================================
// codegen:run
// =============================================================================

// makeCodegenHandler returns the Asynq handler for the codegen:run task.
//
// Pipeline:
//
//  1. Decode the task payload.
//  2. Mark codegen:job:{id}:state="running" so the status endpoint and any
//     polling SSE stream observe the transition.
//  3. Recover from any panic inside codegen.Generate. A bug in the IR
//     emitter that triggers a nil dereference must not crash the worker
//     and starve every other queue — convert the panic to a "failed" job.
//  4. Unmarshal the BlackBoxDefs blob (may be nil/empty for scenes that
//     use only built-in primitives).
//  5. Invoke codegen.Generate — the same pure-CPU function the old
//     synchronous handler called inline.
//  6. Check ctx.Err() to catch the asynq.Timeout(120s) firing mid-flight.
//     Generate has no IO to interrupt so it returns normally, but the
//     result is meaningless under cancellation — surface it as failed.
//  7. Publish the codegen.Response JSON to codegen:job:{id}:result and
//     transition state to "done".
//
// All Redis writes use tasks.CodegenJobTTL so they expire on the same
// clock as the state key the submit handler primed.
//
// Português:
//
//	Handler do codegen:run. Roda o pipeline síncrono que antes vivia no
//	thread HTTP. Captura panic, respeita o timeout do asynq, e publica
//	o resultado nas chaves codegen:job:{id}:state/result/error com o TTL
//	compartilhado em tasks.CodegenJobTTL.
func makeCodegenHandler(rdb *redis.Client) asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var p tasks.CodegenPayload
		if err := json.Unmarshal(task.Payload(), &p); err != nil {
			// Payload decode failure: there is no JobID to report against.
			// Returning the error puts the task in the Asynq archive for
			// inspection. This should be impossible with the current
			// submit handler — the payload it builds is statically typed.
			return fmt.Errorf("codegen:run: decode payload: %w", err)
		}

		log.Printf("[worker/codegen] job=%s lang=%s sceneBytes=%d bbDefsBytes=%d",
			p.JobID, p.Language, len(p.Scene), len(p.BlackBoxDefs))

		// Transition queued → running. A failure here only affects the
		// status endpoint's visibility into the "running" phase; the
		// job itself continues. Worth logging but not aborting.
		if err := setCodegenState(ctx, rdb, p.JobID, "running"); err != nil {
			log.Printf("[worker/codegen] redis set state=running job=%s: %v", p.JobID, err)
		}

		// Recover from panics inside the pipeline. Must be the outermost
		// defer in the function body so it catches panics raised by any
		// of the subsequent steps, including BlackBoxDefs unmarshalling.
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("codegen panic: %v", r)
				log.Printf("[worker/codegen] %s job=%s", msg, p.JobID)
				_ = publishCodegenError(ctx, rdb, p.JobID, msg)
			}
		}()

		// Preview path: a StatementCase inspect-panel preview. No scene and no
		// black-box defs — render the draft cases through codegen.PreviewCase
		// and publish the same Response shape over the same channel the full
		// generation uses, so the SSE stream, status and ownership all apply
		// unchanged. The recover() above also guards this branch.
		if p.PreviewCase != nil {
			// The inspect panel sends cases in its own shape (caseInspectRow:
			// matchKind/values/isDefault, with no IDs — the panel edits case
			// definitions only). Parse that shape explicitly and convert to
			// graph.CaseDef rather than unmarshalling straight into CaseDef
			// and relying on encoding/json's case-insensitive field matching.
			// The tags below MUST track caseInspectRow in
			// devices/compFlow/statementCase.go.
			var wireCases []struct {
				Label     string   `json:"label"`
				MatchKind string   `json:"matchKind"`
				Values    []string `json:"values"`
				IsDefault bool     `json:"isDefault"`
			}
			if len(p.PreviewCase.Cases) > 0 {
				if err := json.Unmarshal(p.PreviewCase.Cases, &wireCases); err != nil {
					return publishCodegenError(ctx, rdb, p.JobID,
						"unmarshal preview cases: "+err.Error())
				}
			}
			cases := make([]graph.CaseDef, len(wireCases))
			for i, wc := range wireCases {
				cases[i] = graph.CaseDef{
					Label:     wc.Label,
					MatchKind: wc.MatchKind,
					Values:    wc.Values,
					IsDefault: wc.IsDefault,
				}
			}
			code, diags := codegen.PreviewCase(
				p.PreviewCase.ScopeID, p.Language, p.PreviewCase.SelectorType, cases)
			return publishCodegenResult(ctx, rdb, p.JobID, codegen.Response{
				Code:        code,
				Diagnostics: diags,
			})
		}

		// Unmarshal black-box definitions. nil/empty is a valid case —
		// scenes that use only built-in primitives reach here with no
		// defs and codegen.Generate handles a nil map.
		var bbDefs map[string]*bbparser.BlackBoxDef
		if len(p.BlackBoxDefs) > 0 {
			if err := json.Unmarshal(p.BlackBoxDefs, &bbDefs); err != nil {
				return publishCodegenError(ctx, rdb, p.JobID,
					"unmarshal black-box defs: "+err.Error())
			}
		}

		// Early-cancel check: if the user clicked Cancel (or closed the
		// IDE tab) while this task sat in the queue, ctx is already
		// cancelled before we burn any CPU on Generate. Detect that and
		// publish failure without touching the pipeline at all.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return publishCodegenError(ctx, rdb, p.JobID,
				"codegen cancelled before processing started")
		}

		// Run the codegen pipeline. Generate now respects ctx — it
		// checks for cancellation between each of its five steps
		// (parse → build → validate → emit IR → backend), so a Cancel
		// that arrives mid-flight bounds the CPU cost to at most one
		// step of remaining work instead of the full 120s budget.
		resp := codegen.Generate(ctx, codegen.Request{
			Scene:        p.Scene,
			Language:     p.Language,
			BlackBoxDefs: bbDefs,
		})

		// After Generate returns, re-check ctx. If it was cancelled
		// mid-flight (timeout, user Cancel, EventSource disconnect),
		// the response is incomplete and we publish a failure instead
		// of a partial "done". Without this check, the WASM client
		// would receive an event:result with a half-built Code field
		// and a Cancelled diagnostic — confusing.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return publishCodegenError(ctx, rdb, p.JobID,
				"codegen cancelled during processing — partial result discarded")
		}

		return publishCodegenResult(ctx, rdb, p.JobID, resp)
	}
}

// =============================================================================
// device:github
// =============================================================================

func makeDeviceGitHubHandler(rdb *redis.Client) asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var p tasks.DeviceGitHubPayload
		if err := json.Unmarshal(task.Payload(), &p); err != nil {
			return fmt.Errorf("device:github: decode payload: %w", err)
		}

		log.Printf("[worker/device] job=%s owner=%s repo=%s tag=%s",
			p.JobID, p.Owner, p.Repo, p.Tag)

		zipBytes, err := downloadGitHubZip(p.Owner, p.Repo, p.Tag)
		if err != nil {
			log.Printf("[worker/device] download failed job=%s: %v", p.JobID, err)
			return publishDeviceError(ctx, rdb, p.JobID, "download failed: "+err.Error())
		}

		limits := store.GetParserLimits(p.UserID)
		found, _, errs := parseDevicesFromZip(zipBytes, limits, p.Owner, p.Repo)

		if len(found) == 0 {
			msg := "no IDS structs found in repository"
			if len(errs) > 0 {
				msg = strings.Join(errs, "; ")
			}
			return publishDeviceError(ctx, rdb, p.JobID, msg)
		}

		var saved []map[string]any
		for _, def := range found {
			parsedJSON, _ := json.Marshal(def)

			existingID := ""
			if p.ExistingIDs != nil {
				existingID = p.ExistingIDs[def.Name]
			}

			visibility := p.Visibility
			if visibility != "public" {
				visibility = "private"
			}

			d := &store.Device{
				ID:               existingID,
				UserID:           p.UserID,
				GithubURL:        p.GithubURL,
				GithubOwner:      p.Owner,
				GithubRepo:       p.Repo,
				GithubTag:        p.Tag,
				DisplayName:      def.Name,
				DisplayNameHuman: p.DisplayNameHuman,
				Tags:             p.Tags,
				Visibility:       visibility,
				CategoryID:       p.CategoryID,
				SubcategoryID:    p.SubcategoryID,
				// GitHub import parses only .go files today (parseDevicesFromZip),
				// so every device it produces is Go. Stamp "golang" truthfully.
				// Full C99/other-language GitHub import is a future feature that
				// would dispatch the parser and set this per device.
				ProgrammingLanguageID: "golang",
				Status:                "ready",
				ParsedJSON:            string(parsedJSON),
				ParseErrors:           errs,
			}
			if err := store.UpsertDevice(d); err != nil {
				log.Printf("[worker/device] upsert failed job=%s struct=%s: %v",
					p.JobID, def.Name, err)
				errs = append(errs, fmt.Sprintf("save failed for %s: %v", def.Name, err))
				continue
			}

			// Auto-insert device into the menu tree (idempotent).
			// Only admin and official_specialist devices appear in the
			// global menu. Regular user devices stay in "My Items" only.
			if d.Visibility == "public" && d.Status == "ready" {
				owner, userErr := store.GetUserByID(p.UserID)
				if userErr == nil && (owner.Role == store.RoleAdmin || owner.Role == store.RoleOfficialSpecialist) {
					menuLabel := d.DisplayNameHuman
					if menuLabel == "" {
						menuLabel = d.DisplayName
					}
					if aiErr := store.AutoInsertDeviceToMenu(
						d.ID, d.DisplayName, menuLabel,
						d.CategoryID, d.SubcategoryID,
					); aiErr != nil {
						log.Printf("[worker/device] menu auto-insert failed job=%s struct=%s: %v",
							p.JobID, def.Name, aiErr)
					}
				}
			}

			saved = append(saved, map[string]any{
				"id":          d.ID,
				"displayName": d.DisplayName,
			})
		}

		return publishDeviceResult(ctx, rdb, p.JobID, map[string]any{
			"status":  "done",
			"devices": saved,
			"errors":  errs,
		})
	}
}

// =============================================================================
// template:github
// =============================================================================

func makeTemplateGitHubHandler(cfg *config.Config, rdb *redis.Client) asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var p tasks.TemplateGitHubPayload
		if err := json.Unmarshal(task.Payload(), &p); err != nil {
			return fmt.Errorf("template:github: decode payload: %w", err)
		}

		log.Printf("[worker/template] versionId=%s pkgId=%s owner=%s repo=%s tag=%s",
			p.VersionID, p.PkgID, p.Owner, p.Repo, p.Tag)

		publishError := func(msg string) error {
			_ = store.UpdateTemplatePkgVersionError(p.VersionID, []string{msg})
			if p.JobID != "" {
				_ = publishTemplateResult(ctx, rdb, p.JobID, map[string]any{
					"status": "error",
					"error":  msg,
				})
			}
			return nil
		}

		zipBytes, err := downloadGitHubZip(p.Owner, p.Repo, p.Tag)
		if err != nil {
			log.Printf("[worker/template] download failed versionId=%s: %v", p.VersionID, err)
			return publishError("download failed: " + err.Error())
		}

		limits := store.GetParserLimits(p.UploaderUserID)
		defJSON, parseErrors, err := parseTemplateFromZip(zipBytes, limits)
		if err != nil {
			log.Printf("[worker/template] fatal parse versionId=%s: %v", p.VersionID, err)
			return publishError(err.Error())
		}

		if dbErr := store.UpdateTemplatePkgVersionReady(p.VersionID, defJSON, parseErrors); dbErr != nil {
			log.Printf("[worker/template] db ready update versionId=%s: %v", p.VersionID, dbErr)
			return fmt.Errorf("template:github: db update: %w", dbErr)
		}

		// The display name comes from the specialist's "New Project" modal (p.Name).
		// The readme.md first-heading extraction was removed — readme.md belongs
		// to the generated project skeleton, not to the template record name.
		// On re-submit (p.Name == "") the worker passes "" and UpdateTemplatePkgMeta
		// skips the name column so the existing name is preserved.
		displayName := p.Name

		// Update the parent package record with the name, tags, and
		// category/subcategory chosen by the specialist at submit time.
		if p.PkgID != "" {
			_ = store.UpdateTemplatePkgMeta(p.PkgID, displayName, p.Tags,
				p.CategoryID, p.SubcategoryID)
		}

		log.Printf("[worker/template] done versionId=%s name=%q warnings=%d",
			p.VersionID, displayName, len(parseErrors))

		// Publish the job result so the frontend polling can detect completion.
		if p.JobID != "" {
			_ = publishTemplateResult(ctx, rdb, p.JobID, map[string]any{
				"status":      "done",
				"displayName": displayName,
				"pkgId":       p.PkgID,
				"errors":      parseErrors,
			})
		}
		return nil
	}
}

// =============================================================================
// GitHub download
// =============================================================================

// downloadGitHubZip downloads the source ZIP for a GitHub release tag.
// GitHub's API returns a 302 redirect to S3. We follow it manually so we do
// not send GitHub auth headers to S3.
func downloadGitHubZip(owner, repo, tag string) ([]byte, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/zipball/%s",
		owner, repo, tag)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "IoTMaker/1.0")
	req.Header.Set("Accept", "application/vnd.github+json")

	noRedirect := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 15 * time.Second,
	}

	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api request: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release not found: %s/%s@%s", owner, repo, tag)
	}
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return nil, fmt.Errorf("unexpected github api status: %d", resp.StatusCode)
	}

	redirectURL := resp.Header.Get("Location")
	if redirectURL == "" {
		return nil, fmt.Errorf("github api redirect has no Location header")
	}

	zipResp, err := http.Get(redirectURL) // #nosec G107 — URL from GitHub API
	if err != nil {
		return nil, fmt.Errorf("download zip: %w", err)
	}
	defer zipResp.Body.Close()

	if zipResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zip download status: %d", zipResp.StatusCode)
	}

	limited := io.LimitReader(zipResp.Body, maxGitHubZipBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read zip body: %w", err)
	}
	if int64(len(data)) > maxGitHubZipBytes {
		return nil, fmt.Errorf("repository ZIP exceeds %d MB limit", maxGitHubZipBytes/1024/1024)
	}

	return data, nil
}

// =============================================================================
// ZIP parsing — devices
// =============================================================================

// parseDevicesFromZip opens a ZIP in memory and:
//  1. Parses every .go file in the root for IDS structs.
//  2. Collects markdown help files from the root (readme.md, init.en.md, …).
//  3. Saves image files from the root to disk under {UserFilesDir}/devices/{owner}/{repo}/.
//  4. Populates BlackBoxDef.Help on every struct found.
//
// GitHub release ZIPs have a single root directory ("{owner}-{repo}-{hash}/").
// We strip that prefix so callers see clean paths.
// Files in vendor/, testdata/, and *_test.go are skipped.
func parseDevicesFromZip(zipBytes []byte, limits bbparser.ParserLimits, owner, repo string) ([]*bbparser.BlackBoxDef, map[string][]byte, []string) {
	r, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, nil, []string{"could not open ZIP: " + err.Error()}
	}

	var warnings []string
	var totalUncompressed int64

	// Collect file contents in a single pass.
	goFiles := map[string][]byte{}
	mdFiles := map[string][]byte{}  // root-only .md files
	imgFiles := map[string][]byte{} // root-only image files

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		totalUncompressed += int64(f.UncompressedSize64)
		if totalUncompressed > maxUncompressedBytes {
			warnings = append(warnings, "repository too large — stopped after 200 MB")
			break
		}

		cleanPath := stripGitHubRootDir(f.Name)
		if isVendoredPath(cleanPath) {
			continue
		}

		content, readErr := readZipFile(f)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("could not read %s: %v", cleanPath, readErr))
			continue
		}

		switch {
		case strings.HasSuffix(cleanPath, ".go") && !strings.HasSuffix(cleanPath, "_test.go"):
			goFiles[cleanPath] = content

		case isRootFile(cleanPath) && strings.HasSuffix(strings.ToLower(cleanPath), ".md"):
			mdFiles[cleanPath] = content

		case isImageFile(cleanPath):
			// Accept image files from any directory (root, examples/, etc.).
			// Subdirectory structure is preserved when saved to disk and
			// mapped to public URLs for markdown path rewriting.
			imgFiles[cleanPath] = content
		}
	}

	// Save images to disk; build filename → public URL map.
	imageURLs, imgWarns := saveDeviceImages(imgFiles, owner, repo)
	warnings = append(warnings, imgWarns...)

	// Build DeviceHelp from markdown files. The grammar lives in
	// bbparser.BuildDeviceHelp so the live editor (handler) and the
	// publish-time worker share one source of truth — see
	// server/codegen/blackbox/devicehelp.go for the rationale.
	help, helpWarns := bbparser.BuildDeviceHelp(mdFiles, imageURLs)
	warnings = append(warnings, helpWarns...)

	// Parse Go files and attach the shared help payload to each struct.
	var defs []*bbparser.BlackBoxDef
	for cleanPath, src := range goFiles {
		def, parseErr := bbparser.Parse(src, limits)
		if parseErr != nil && def == nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", cleanPath, parseErr))
			continue
		}
		if parseErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", cleanPath, parseErr))
		}
		if def != nil {
			def.Help = help

			// Resolve the interactive: directive to a public URL.
			//
			// The specialist places a dual-mode SVG file in the root of their
			// GitHub release ZIP and references it with "interactive:stem." in
			// the struct doc comment (e.g. "interactive:rp2040." for rp2040.svg).
			//
			// The worker has already saved all root-level image files (including
			// SVGs) to disk via saveDeviceImages. Here we look up the resolved
			// public URL for the referenced SVG and replace the bare stem stored
			// by the parser with that URL so the WASM IDE can fetch it directly.
			//
			// If the file is missing from the ZIP a warning is emitted and the
			// field is cleared — the interactive diagram will simply not activate.
			if def.Interactive != "" {
				svgName := def.Interactive + ".svg"
				if url, ok := imageURLs[svgName]; ok {
					def.Interactive = url

					// Validate the SVG against the Interactive Diagram Spec.
					// Non-blocking: issues are reported as warnings so the
					// specialist can fix them, but the device still works.
					if svgData, svgOk := imgFiles[svgName]; svgOk {
						svgWarns := validateInteractiveSVG(
							svgData, svgName, def.Props,
						)
						warnings = append(warnings, svgWarns...)
					}
				} else {
					warnings = append(warnings, fmt.Sprintf(
						"%s: interactive: SVG file %q not found in ZIP root — "+
							"interactive diagram will not activate; add %s to the repository root",
						cleanPath, svgName, svgName,
					))
					def.Interactive = ""
				}
			}

			defs = append(defs, def)
		}
	}

	return defs, mdFiles, warnings
}

// =============================================================================

// isRootFile returns true when cleanPath has no directory component,
// meaning the file lives in the root of the repository.
func isRootFile(cleanPath string) bool {
	return !strings.Contains(cleanPath, "/")
}

// isImageFile reports whether a filename has a recognised image extension.
func isImageFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return true
	}
	return false
}

// saveDeviceImages writes image files to {UserFilesDir}/devices/{owner}/{repo}/
// and returns a map from bare filename to public URL (/static/devices/…).
// Existing files are overwritten — the latest submit is always the source of truth.
func saveDeviceImages(imgs map[string][]byte, owner, repo string) (map[string]string, []string) {
	var warnings []string
	cfg := config.Get()

	baseDir := filepath.Join(cfg.UserFilesDir, "devices", owner, repo)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not create image dir %s: %v", baseDir, err))
		return nil, warnings
	}

	urls := make(map[string]string, len(imgs))
	for name, data := range imgs {
		// name may contain subdirectory (e.g. "examples/example.png").
		// Create the subdirectory if needed.
		dst := filepath.Join(baseDir, name)
		subDir := filepath.Dir(dst)
		if subDir != baseDir {
			if err := os.MkdirAll(subDir, 0755); err != nil {
				warnings = append(warnings, fmt.Sprintf("could not create subdir for %s: %v", name, err))
				continue
			}
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			warnings = append(warnings, fmt.Sprintf("could not save image %s: %v", name, err))
			continue
		}
		urls[name] = "/static/devices/" + owner + "/" + repo + "/" + name
		log.Printf("[worker/help] saved image %s -> %s", name, urls[name])
	}
	return urls, warnings
}

// =============================================================================
// Interactive SVG validation
// =============================================================================

// validateInteractiveSVG checks an SVG file against the IoTMaker Interactive
// Diagram Specification (docs/INTERACTIVE_DIAGRAM_SPEC.md). Returns a list of
// warnings for issues found. All checks are non-blocking — the device works
// even with an invalid SVG, but the specialist should fix the issues.
//
// Checks performed:
//  1. data-palette attribute on <svg> root
//  2. At least one <g class="conn-group"> element
//  3. Each conn-group has a data-id attribute
//  4. Required children: .pad, .conn-role-bg, .conn-role, .readme-badges
//  5. If props have connection: tags, those roles exist in the palette
//
// Uses simple string matching — no full XML parser needed for validation.
//
// Português: Valida um SVG interativo contra a especificação. Retorna
// warnings para o especialista corrigir. Não impede o uso do device.
func validateInteractiveSVG(data []byte, filename string, props []bbparser.PropDef) []string {
	var w []string
	svg := string(data)
	prefix := filename + ": "

	// 1. Check data-palette on <svg> root.
	hasPalette := strings.Contains(svg, "data-palette=")
	if !hasPalette {
		w = append(w, prefix+"missing data-palette attribute on <svg> root — "+
			"diagram colours will fall back to neutral grey. "+
			"Add data-palette=\"ROLE:#hex, ...\" to the <svg> element")
	}

	// 2. Check for at least one conn-group.
	connGroupCount := strings.Count(svg, `class="conn-group"`)
	if connGroupCount == 0 {
		w = append(w, prefix+"no <g class=\"conn-group\"> elements found — "+
			"the diagram has no interactive elements. "+
			"Each selectable element must be a <g class=\"conn-group\" data-id=\"...\">")
		// No point checking children if there are no groups.
		return w
	}

	// 3. Check that conn-groups have data-id.
	// Count data-id occurrences and compare with conn-group count.
	dataIdCount := strings.Count(svg, "data-id=")
	if dataIdCount < connGroupCount {
		w = append(w, fmt.Sprintf(
			prefix+"%d conn-group(s) found but only %d have data-id — "+
				"every conn-group must have a data-id attribute",
			connGroupCount, dataIdCount))
	}

	// 4. Check for required CSS classes in child elements.
	requiredClasses := []struct {
		cls  string
		desc string
	}{
		{`class="pad"`, ".pad (clickable shape)"},
		{`class="readme-badges"`, ".readme-badges (badge container)"},
		{`class="conn-role-bg"`, ".conn-role-bg (inspector role background)"},
		{`class="conn-role"`, ".conn-role (inspector role text)"},
	}
	for _, rc := range requiredClasses {
		if !strings.Contains(svg, rc.cls) {
			w = append(w, prefix+"no elements with "+rc.cls+" found — "+
				"each conn-group needs a child with class "+rc.desc)
		}
	}

	// 5. Check required CSS rules.
	if !strings.Contains(svg, ".active") || !strings.Contains(svg, ".dimmed") {
		w = append(w, prefix+"missing .active/.dimmed CSS rules — "+
			"add the inspector-mode CSS to <defs><style> inside the SVG. "+
			"See docs/INTERACTIVE_DIAGRAM_SPEC.md section 4")
	}

	// 6. If props have connection: tags, validate roles against palette.
	if hasPalette {
		// Extract the palette string for role lookup.
		paletteStart := strings.Index(svg, `data-palette="`)
		if paletteStart >= 0 {
			paletteStart += len(`data-palette="`)
			paletteEnd := strings.Index(svg[paletteStart:], `"`)
			if paletteEnd >= 0 {
				paletteStr := strings.ToUpper(svg[paletteStart : paletteStart+paletteEnd])

				for _, p := range props {
					if p.Connection == "" {
						continue
					}
					roleUpper := strings.ToUpper(p.Connection)
					if !strings.Contains(paletteStr, roleUpper) {
						w = append(w, fmt.Sprintf(
							prefix+"prop %q has connection:%q but role %q is not in "+
								"the SVG data-palette — the element will use a neutral "+
								"fallback colour instead of a meaningful one",
							p.FieldName, p.Connection, p.Connection))
					}
				}
			}
		}
	}

	if len(w) == 0 {
		log.Printf("[worker/svg] %s: validation passed (%d conn-groups)", filename, connGroupCount)
	} else {
		log.Printf("[worker/svg] %s: validation found %d issue(s)", filename, len(w))
	}

	return w
}

// =============================================================================
// ZIP parsing — templates
// =============================================================================

// parseTemplateFromZip parses a GitHub release ZIP as a full Go project template.
// Returns serialised defJSON, non-fatal parse warnings, and a fatal error.
func parseTemplateFromZip(zipBytes []byte, limits bbparser.ParserLimits) (defJSON string, parseErrors []string, err error) {
	r, zipErr := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if zipErr != nil {
		return "", nil, fmt.Errorf("could not open ZIP: %w", zipErr)
	}

	// projectFile mirrors the client-side OutputFileMetaClient shape so that
	// def_json can be unmarshalled directly by TemplateDefClient.OutputFiles.
	type projectFile struct {
		Path    string `json:"path"`    // full path as stored (used by handleGenerate)
		Content string `json:"content"` // used by handleGenerate for substitution
		IsBin   bool   `json:"isBinary,omitempty"`

		// OutputFiles manifest fields — consumed by the WASM client.
		RelPath  string   `json:"relPath,omitempty"`  // path relative to output/
		IsText   bool     `json:"isText,omitempty"`   // true → text/template substitution
		VarsUsed []string `json:"varsUsed,omitempty"` // placeholder names found in the file
	}

	// templateManifest mirrors TemplateManifestClient on the WASM side.
	type templateManifest struct {
		Name        string            `json:"name,omitempty"`
		Version     string            `json:"version,omitempty"`
		Description string            `json:"description,omitempty"`
		Vars        map[string]string `json:"vars"`
	}

	var devices []*bbparser.BlackBoxDef
	var files []projectFile
	var manifest templateManifest
	var totalUncompressed int64

	// rootMdFiles holds markdown files at the ZIP root (not inside devices/ or
	// output/). They are used to build the template-level help payload — the same
	// readme.md / init.en.md convention used by standalone devices.
	rootMdFiles := map[string][]byte{}

	// devicesMdFiles holds markdown files inside devices/ for per-device help.
	// Key is the filename relative to devices/ (e.g. "readme.md", "init.en.md").
	devicesMdFiles := map[string][]byte{}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		totalUncompressed += int64(f.UncompressedSize64)
		if totalUncompressed > maxUncompressedBytes {
			parseErrors = append(parseErrors, "repository too large — stopped after 200 MB")
			break
		}

		cleanPath := stripGitHubRootDir(f.Name)
		if isVendoredPath(cleanPath) {
			continue
		}

		content, readErr := readZipFile(f)
		if readErr != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("could not read %s: %v", cleanPath, readErr))
			continue
		}

		// Parse template.json into the manifest struct so the WASM client
		// receives Manifest.Vars and can drive the sceneresolver correctly.
		if strings.EqualFold(cleanPath, "template.json") {
			if jsonErr := json.Unmarshal(content, &manifest); jsonErr != nil {
				parseErrors = append(parseErrors, fmt.Sprintf("template.json: %v", jsonErr))
			}
			// Still store the raw file so handleGenerate can also read it.
			files = append(files, projectFile{
				Path:    cleanPath,
				Content: string(content),
			})
			continue
		}

		// Collect root-level markdown files (readme.md, init.en.md, etc.)
		// for the template-level help payload.
		if isRootFile(cleanPath) && strings.HasSuffix(strings.ToLower(cleanPath), ".md") {
			rootMdFiles[cleanPath] = content
		}

		// Collect markdown files inside devices/ for device-level help.
		const devPrefix = "devices/"
		if strings.HasPrefix(cleanPath, devPrefix) {
			rel := strings.TrimPrefix(cleanPath, devPrefix)
			if isRootFile(rel) && strings.HasSuffix(strings.ToLower(rel), ".md") {
				devicesMdFiles[rel] = content
			}
		}

		// Parse Go files inside devices/ for the device block definitions.
		if strings.HasSuffix(cleanPath, ".go") && !strings.HasSuffix(cleanPath, "_test.go") {
			def, parseErr := bbparser.Parse(content, limits)
			if parseErr != nil && def != nil {
				parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", cleanPath, parseErr))
			}
			if def != nil {
				devices = append(devices, def)
			}
		}

		pf := projectFile{
			Path:    cleanPath,
			Content: string(content),
		}

		// Classify output/ files for the WASM OutputFiles manifest.
		// RelPath and IsText are the two fields the WASM client uses.
		const outputPrefix = "output/"
		if strings.HasPrefix(cleanPath, outputPrefix) {
			rel := strings.TrimPrefix(cleanPath, outputPrefix)
			if rel != "" {
				pf.RelPath = rel
				pf.IsText = tmplIsTextBytes(content)
			}
		}

		files = append(files, pf)
	}

	// Build template-level help from root markdown files.
	// Same convention as standalone devices: readme.md → overview,
	// init.en.md / run.en.md / … → method tabs.
	templateHelp, helpWarns := bbparser.BuildDeviceHelp(rootMdFiles, nil)
	parseErrors = append(parseErrors, helpWarns...)

	// Build device-level help from devices/ markdown files and attach
	// the shared payload to every device found in the package.
	deviceHelp, devHelpWarns := bbparser.BuildDeviceHelp(devicesMdFiles, nil)
	parseErrors = append(parseErrors, devHelpWarns...)
	for _, def := range devices {
		def.Help = deviceHelp
	}

	// Ensure Vars is never nil — the WASM does a nil-map range which is safe,
	// but an explicit empty map makes the JSON cleaner.
	if manifest.Vars == nil {
		manifest.Vars = map[string]string{}
	}

	// def_json shape that satisfies both:
	//   - TemplateDefClient (WASM): needs "manifest", "outputFiles", and "help"
	//   - handleGenerate (server): needs "files" for content substitution
	type templateDef struct {
		Manifest    templateManifest        `json:"manifest"`
		Devices     []*bbparser.BlackBoxDef `json:"devices"`
		Files       []projectFile           `json:"files"`       // full file tree for generation
		OutputFiles []projectFile           `json:"outputFiles"` // output/ subset for WASM manifest
		// Help is the template-level help payload built from root markdown files.
		// The WASM uses it to show readme and method tabs for the template itself,
		// using the same resolution logic as standalone device help.
		Help bbparser.DeviceHelp `json:"help,omitempty"`
	}

	// Build OutputFiles: only the output/ entries that have a RelPath set.
	var outputFiles []projectFile
	for _, pf := range files {
		if pf.RelPath != "" {
			outputFiles = append(outputFiles, pf)
		}
	}

	b, marshalErr := json.Marshal(templateDef{
		Manifest:    manifest,
		Devices:     devices,
		Files:       files,
		OutputFiles: outputFiles,
		Help:        templateHelp,
	})
	if marshalErr != nil {
		return "", parseErrors, fmt.Errorf("marshal template def: %w", marshalErr)
	}

	return string(b), parseErrors, nil
}

// tmplIsTextBytes returns true when the byte slice looks like a UTF-8 text
// file (valid UTF-8, no null bytes). Used to classify output/ files.
func tmplIsTextBytes(b []byte) bool {
	return utf8.Valid(b) && !bytes.Contains(b, []byte{0})
}

// =============================================================================
// ZIP helpers
// =============================================================================

// stripGitHubRootDir removes the GitHub-generated root directory from a ZIP
// entry path. GitHub creates ZIPs with a single root directory named
// "{owner}-{repo}-{shortHash}/".
func stripGitHubRootDir(path string) string {
	idx := strings.Index(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

// isVendoredPath reports whether the path is inside vendor/ or testdata/.
func isVendoredPath(path string) bool {
	return strings.HasPrefix(path, "vendor/") ||
		strings.Contains(path, "/vendor/") ||
		strings.HasPrefix(path, "testdata/") ||
		strings.Contains(path, "/testdata/")
}

// readZipFile reads the full content of a ZIP file entry into memory.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxGitHubZipBytes))
}

// =============================================================================
// Redis publishing
// =============================================================================

func publishDeviceResult(ctx context.Context, rdb *redis.Client, jobID string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("publishDeviceResult: marshal: %w", err)
	}
	key := fmt.Sprintf("device:job:%s", jobID)
	if err := rdb.Set(ctx, key, string(b), jobResultTTL).Err(); err != nil {
		return fmt.Errorf("publishDeviceResult: redis set key=%s: %w", key, err)
	}
	log.Printf("[worker] published result key=%s", key)
	return nil
}

func publishDeviceError(ctx context.Context, rdb *redis.Client, jobID, msg string) error {
	return publishDeviceResult(ctx, rdb, jobID, map[string]any{
		"status": "error",
		"error":  msg,
	})
}

// publishTemplateResult writes the template job result to Redis under
// "template:job:{jobID}" with the same TTL as device jobs.
func publishTemplateResult(ctx context.Context, rdb *redis.Client, jobID string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("publishTemplateResult: marshal: %w", err)
	}
	key := fmt.Sprintf("template:job:%s", jobID)
	if err := rdb.Set(ctx, key, string(b), jobResultTTL).Err(); err != nil {
		return fmt.Errorf("publishTemplateResult: redis set key=%s: %w", key, err)
	}
	log.Printf("[worker] published template result key=%s", key)
	return nil
}

// =============================================================================
// Codegen Redis publishing
// =============================================================================

// setCodegenState writes codegen:job:{id}:state with the shared codegen TTL.
// The state machine the stream handler observes is queued → running → done
// or queued → running → failed. The submit handler primes queued; this
// helper covers the remaining three transitions.
//
// Português:
//
//	Grava o estado do job no Redis com o TTL compartilhado. Cobre as
//	transições running → done/failed que o stream handler observa.
func setCodegenState(ctx context.Context, rdb *redis.Client, jobID, state string) error {
	key := fmt.Sprintf("codegen:job:%s:state", jobID)
	return rdb.Set(ctx, key, state, tasks.CodegenJobTTL).Err()
}

// publishCodegenResult serialises a codegen.Response into Redis and flips
// the state key to "done". Order matters: result is written BEFORE state.
//
//   - If we wrote state first, an SSE stream that ticks between the two
//     writes would see state="done" with no result and emit an infra
//     error. Writing result first eliminates that window.
//
//   - If the Redis SET for the result fails, leaving state at "running"
//     is safer than leaving state at "done" without a result. The
//     stream handler will simply keep polling — wasted CPU is recoverable;
//     a false completion is not.
//
// Português:
//
//	Ordem importa: gravar resultado primeiro, estado depois. Inverter
//	abre janela onde stream handler veria done sem resultado.
func publishCodegenResult(ctx context.Context, rdb *redis.Client, jobID string, resp codegen.Response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return publishCodegenError(ctx, rdb, jobID,
			"marshal codegen response: "+err.Error())
	}

	resultKey := fmt.Sprintf("codegen:job:%s:result", jobID)
	if err := rdb.Set(ctx, resultKey, b, tasks.CodegenJobTTL).Err(); err != nil {
		return fmt.Errorf("publishCodegenResult: redis set result key=%s: %w", resultKey, err)
	}
	if err := setCodegenState(ctx, rdb, jobID, "done"); err != nil {
		return fmt.Errorf("publishCodegenResult: redis set state job=%s: %w", jobID, err)
	}

	log.Printf("[worker/codegen] published job=%s codeBytes=%d errors=%d warnings=%d diagnostics=%d",
		jobID, len(resp.Code), len(resp.Errors), len(resp.Warnings), len(resp.Diagnostics))
	return nil
}

// publishCodegenError writes the failure message to codegen:job:{id}:error
// and flips state to "failed". The order (error first, then state) matches
// publishCodegenResult: the stream handler should never see state="failed"
// without a corresponding error message available.
//
// IMPORTANT: this function is typically called from cancellation paths,
// where the caller's ctx is itself cancelled (asynq.Timeout fired, user
// clicked Cancel, EventSource disconnected). Using a cancelled ctx for
// Redis writes would fail immediately with context.Canceled and leave
// the state stuck at "running" forever. When we detect ctx.Err() we
// detach into a fresh background context with a short timeout so the
// publication always lands. When ctx is healthy (panic-recovery path
// where Generate aborted but the caller did not cancel), we keep using
// it so the operator's deadline still applies.
//
// Returns nil when both writes succeed — the handler has done its job
// by publishing the failure even though the user-visible outcome is an
// error. Returning nil keeps the task out of the Asynq "archived" list
// since nothing about the infrastructure failed.
//
// Returns the Redis error when the state write fails — at that point we
// cannot guarantee the client will ever see a terminal event, so surfacing
// the failure to Asynq for operator visibility is worth the noise.
//
// Português:
//
//	Publica o erro do job. Quando o ctx do caller já está cancelado
//	(caso comum: estamos publicando porque foi cancelado), desprende
//	em um background context com timeout para que a publicação ainda
//	aconteça. Sem isso a chamada ao Redis falharia imediatamente com
//	context.Canceled e o state ficaria preso em "running".
func publishCodegenError(ctx context.Context, rdb *redis.Client, jobID, msg string) error {
	// Detach when the caller's ctx is already dead. Three reasons:
	//
	//  (a) The common case for this function is "we were cancelled
	//      and now we need to publish that fact". Using the cancelled
	//      ctx is self-defeating — go-redis returns context.Canceled
	//      on the very first Set.
	//
	//  (b) The 5-second timeout bounds the worst case where Redis
	//      itself is unreachable. Without a timeout, a stuck Redis
	//      would block the worker goroutine forever and prevent the
	//      Asynq runtime from acknowledging the task.
	//
	//  (c) When ctx is still healthy (panic-recovery path before any
	//      cancellation), we keep using it so any deadline the caller
	//      imposed still applies.
	writeCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		writeCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	errKey := fmt.Sprintf("codegen:job:%s:error", jobID)
	if err := rdb.Set(writeCtx, errKey, msg, tasks.CodegenJobTTL).Err(); err != nil {
		// Logged but not fatal — we still try to flip state so the
		// stream handler at least sees "failed" and stops polling.
		log.Printf("[worker/codegen] redis set error key=%s: %v", errKey, err)
	}
	if err := setCodegenState(writeCtx, rdb, jobID, "failed"); err != nil {
		return fmt.Errorf("publishCodegenError: redis set state job=%s: %w", jobID, err)
	}
	log.Printf("[worker/codegen] published failure job=%s msg=%q", jobID, msg)
	return nil
}
