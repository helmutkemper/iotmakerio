// server/cmd/server/main.go — IoTMaker Portal HTTP server.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Architecture overview:
//
//	/ (landing)          → public/landingPage/landing.html
//	/app                 → public/app.html  (portal SPA with sidebar, IDE…)
//	/control             → public/control/index.html (admin SPA)
//	/ide/*               → public/ide/      (WebAssembly)
//	/static/*            → USER_FILES_DIR → public/static/ (fallback)
//	/css/*               → public/css/      (landing page design system)
//	/help/*              → public/help/     (help markdown files)
//	/monaco/*, /marked/*, /highlight/* — third-party bundles served with COEP/CORP
//
// API groups:
//
//	/api/auth/*             — portal JWT auth (register, login, 2fa, verify, me…)
//	/api/auth/control-token — exchange portal token for 1h control-panel token
//	/api/v1/*               — portal API (blackbox, codegen, projects, templates,
//	                          feed, live, stage files, translations READ-ONLY,
//	                          menu sections…)
//	/api/control/v1/*       — admin API (users, menu tree, categories, groups,
//	                          sections, TRANSLATIONS write path, all OTP-gated)
//
// Notes on translations:
//
//   - Read endpoints stay public under /api/v1/translations — the IDE WASM
//     runtime and the portal landing page both rely on them before any
//     user logs in.
//   - Write endpoints live under /api/control/v1/translations and require
//     both a control-panel token AND a per-save OTP (see handler/controlapi/
//     translations.go).
//   - The old standalone page at /admin/i18n was removed; the admin UI is
//     now inside the /control SPA.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"server/debug"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"

	"server/config"
	"server/store"

	"server/handler/adminapi"
	"server/handler/blackboxapi"
	codegenhandler "server/handler/codegen"
	controlapi "server/handler/controlapi"
	"server/handler/editorapi"
	"server/handler/feedapi"
	i18nhandler "server/handler/i18n"
	"server/handler/liveapi"
	"server/handler/menuapi"
	"server/handler/profileapi"
	"server/handler/projectapi"
	projectexportapi "server/handler/projectexport"
	"server/handler/spaauth"
	"server/handler/stagefileapi"
	"server/handler/templateapi"
	"server/permission"
)

func main() {
	cfg := config.Load()

	debug.SetLevel(debug.LevelNotice) // enable all messages
	debug.Noticeln("debug level notice: is set")

	// ── Database ──────────────────────────────────────────────────────────────
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("[main] create data dir: %v", err)
	}

	// On a cold start the server and worker boot simultaneously. Both try to
	// open and migrate the same SQLite file. Occasionally the filesystem is
	// not fully ready (especially right after docker-compose recreates a
	// bind-mount volume) and the first attempt hits "disk I/O error" or
	// "database disk image is malformed". A short retry loop lets the
	// filesystem settle before giving up.
	const maxRetries = 5
	const retryDelay = time.Second
	var dbErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		dbErr = store.Open(cfg.DBPath)
		if dbErr == nil {
			break
		}
		log.Printf("[main] open database attempt %d/%d: %v — retrying in %v",
			attempt, maxRetries, dbErr, retryDelay)
		if store.DB != nil {
			store.DB.Close()
			store.DB = nil
		}
		time.Sleep(retryDelay)
	}
	if dbErr != nil {
		log.Fatalf("[main] open database failed after %d attempts: %v", maxRetries, dbErr)
	}

	// Seeds — admin bootstrap runs here because SeedTranslations (which
	// populates the i18n tables) is already handled inside store.Open() by
	// the migrate() routine. Adding a separate translations seed call here
	// would be dead code.
	if err := store.SeedAdmin(); err != nil {
		log.Printf("[main] seed admin: %v", err)
	}

	// Migrate legacy help .md files from disk to the menu_help table.
	// Idempotent — only inserts entries that don't already exist.
	store.MigrateHelpFilesToDB(filepath.Join(cfg.StaticDir, "help", "devices"))

	// ── Redis + Asynq ─────────────────────────────────────────────────────────
	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisAddr}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("[main] redis unavailable: %v", err)
	}
	log.Printf("[main] redis connected at %s", cfg.RedisAddr)

	asynqClient := asynq.NewClient(redisOpt)
	defer asynqClient.Close()

	// ── Live communication Hub ────────────────────────────────────────────
	// The Hub manages WebSocket connections from browsers and bridges them
	// to Redis PubSub channels for bidirectional hardware communication.
	liveHub := liveapi.NewHub(rdb)
	go liveHub.Run(context.Background())

	// ── Echo ──────────────────────────────────────────────────────────────────
	e := echo.New()
	e.HideBanner = true
	//e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	// ── Static assets ─────────────────────────────────────────────────────────
	e.Static("/css", cfg.StaticDir+"/css")   // landing page design system
	e.Static("/help", cfg.StaticDir+"/help") // help markdown files

	// /static/* serves two roots with a fallback chain:
	//
	//   1. USER_FILES_DIR  — user project files (code, images, docs).
	//      Path pattern: /static/{userID}/project/{type}/{projectID}/{section}/{file}
	//      Stored outside the image so files survive docker-compose up --build.
	//
	//   2. public/static   — SPA assets (JS, CSS, fonts).
	//      Served only when the file is not found in USER_FILES_DIR.
	//
	// Echo's Static() uses the first registered route that matches, so we must
	// use a custom handler to implement the fallback.
	//
	// NOTE: Private project files are currently served without access control.
	// See handler/projectapi/readme.md for the planned improvement.
	e.GET("/static/*", func(c echo.Context) error {
		relPath := c.Param("*")

		// Try USER_FILES_DIR first (user-uploaded project files).
		userFilePath := cfg.UserFilesDir + "/" + relPath
		if _, err := os.Stat(userFilePath); err == nil {
			return c.File(userFilePath)
		}

		// Fall back to SPA assets inside the image.
		return c.File(cfg.StaticDir + "/static/" + relPath)
	})

	// Monaco e Marked servidos localmente com o header que COEP exige.
	// Cross-Origin-Resource-Policy: cross-origin permite que uma página
	// com COEP: require-corp carregue esses arquivos sem bloqueio.
	corpHeader := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
			return next(c)
		}
	}

	e.GET("/monaco/*", func(c echo.Context) error {
		// Set isolation headers required by WebAssembly
		c.Response().Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		c.Response().Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		return c.File(cfg.StaticDir + "/monaco/" + c.Param("*"))
	})

	e.GET("/marked/*", func(c echo.Context) error {
		return c.File(cfg.StaticDir + "/marked/" + c.Param("*"))
	}, corpHeader)

	e.GET("/highlight/*", func(c echo.Context) error {
		return c.File(cfg.StaticDir + "/highlight/" + c.Param("*"))
	}, corpHeader)

	// IDE — WebAssembly requires:
	//   Content-Type: application/wasm       for instantiateStreaming()
	//   Cross-Origin-Embedder-Policy: require-corp  \  for SharedArrayBuffer
	//   Cross-Origin-Opener-Policy: same-origin      /  (WASM isolation)
	// Echo's Static() doesn't set COOP/COEP, so we use a custom file server.
	e.GET("/ide/*", func(c echo.Context) error {
		// Set isolation headers required by WebAssembly
		c.Response().Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		c.Response().Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		return c.File(cfg.StaticDir + "/ide/" + c.Param("*"))
	})

	// ── Pages ─────────────────────────────────────────────────────────────────
	serveFile := func(path string) echo.HandlerFunc {
		return func(c echo.Context) error { return c.File(cfg.StaticDir + path) }
	}

	// Landing page
	e.GET("/", serveFile("/landingPage/landing.html"))
	e.GET("/index", serveFile("/landingPage/landing.html"))
	e.GET("/index.html", serveFile("/landingPage/landing.html"))
	e.GET("/favicon.ico", serveFile("/landingPage/favicon/favicon.ico"))

	// SPA — serves app.html for /app and any /app/* sub-path (hash router)
	e.GET("/app", serveFile("/app.html"))
	e.GET("/app/*", serveFile("/app.html"))

	// Control panel — separate SPA at /control
	e.GET("/control", serveFile("/control/index.html"))
	e.GET("/control/", serveFile("/control/index.html"))
	e.Static("/control/css", cfg.StaticDir+"/control/css")
	e.Static("/control/js", cfg.StaticDir+"/control/js")

	// NOTE: The old standalone /admin/i18n page was removed. The translation
	// admin now lives inside the /control SPA under its own nav entry. The
	// file public/templates/i18n-admin.html can be safely deleted.

	// ── API routes ────────────────────────────────────────────────────────────
	v1 := e.Group("/api/v1")

	spaauth.Register(e, rdb)
	controlapi.RegisterAuth(e)                                                // POST /api/auth/control-token
	blackboxapi.Register(v1, asynqClient, rdb)                                // /api/v1/blackbox (WASM IDE + device submit)
	i18nhandler.Register(v1)                                                  // /api/v1/translations/* (read-only + missing-key telemetry)
	codegenhandler.Register(v1.Group("/codegen"), asynqClient, rdb, redisOpt) // /api/v1/codegen/*
	profileapi.Register(v1)                                                   // /api/v1/profile/* + /api/v1/users/:username
	editorapi.Register(v1)                                                    // /api/v1/editor/* (menu prefs)
	feedapi.Register(v1)                                                      // /api/v1/feed + ratings + follow
	projectapi.Register(v1)                                                   // /api/v1/projects/*
	projectexportapi.Register(v1)                                             // /api/v1/projects/:id/export/* (Github package)
	templateapi.Register(v1, asynqClient, rdb)                                // /api/v1/templates/*
	stagefileapi.Register(v1)                                                 // /api/v1/stage-files/*
	menuapi.Register(v1.Group("/menu"))                                       // /api/v1/menu/sections (WASM IDE dynamic menu)
	liveapi.Register(e, liveHub)                                              // /ws/live/* + /api/v1/webhook/* + /api/v1/live/keys/*

	// Control panel API — separate group, requires control token
	vControl := e.Group("/api/control/v1")
	controlapi.RegisterControl(vControl)      // /api/control/v1/users/*
	controlapi.RegisterTranslations(vControl) // /api/control/v1/translations/* (OTP-gated writes)

	// Menu sections and groups — admin CRUD for the dynamic IDE menu.
	// Uses the same control token auth as the rest of /api/control/v1/*.
	// PermMenuView is the minimum gate; write handlers share the same gate
	// because only admin holds any menu.* permission (see permission.go).
	menuGroup := vControl.Group("", controlapi.RequireControlToken(permission.PermMenuView))
	adminapi.RegisterSections(menuGroup)   // /api/control/v1/sections/*
	adminapi.RegisterGroups(menuGroup)     // /api/control/v1/groups/*
	adminapi.RegisterCategories(menuGroup) // /api/control/v1/categories/*
	adminapi.RegisterMenuTree(menuGroup)   // /api/control/v1/menu/* (catalog, profiles, layout)

	// ── Maintenance goroutine ─────────────────────────────────────────────────
	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for range t.C {
			_ = store.PruneExpiredOTPs()
		}
	}()

	// ── Start ─────────────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  2 * time.Minute,
	}

	go func() {
		log.Printf("[main] listening on :%s", cfg.ServerPort)
		if err := e.StartServer(srv); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[main] %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[main] shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		log.Fatalf("[main] shutdown: %v", err)
	}
}
