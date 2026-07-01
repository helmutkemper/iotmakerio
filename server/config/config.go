// server/config/config.go — Application configuration for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Configuration is loaded once from environment variables at startup.
// Call Load() from main(), then Get() anywhere in the application.
//
// Environment variables:
//
//	SERVER_PORT           — HTTP listen port              (default: 8080)
//	REDIS_ADDR            — Redis/Asynq address           (default: localhost:6379)
//	DB_PATH               — SQLite database file path     (default: data/iotmaker.db)
//	JWT_SECRET            — HMAC-SHA256 signing key        (default: dev-secret — CHANGE IN PROD)
//	TEMPLATES_DIR         — Path to HTML templates         (default: public/templates)
//	STATIC_DIR            — Path to static assets          (default: public)
//	USER_FILES_DIR        — Base path for user-uploaded project files (default: public/static)
//	GITHUB_CLIENT_ID      — GitHub OAuth App client ID (required for GitHub identity verification)
//	GITHUB_CLIENT_SECRET  — GitHub OAuth App client secret (required for GitHub identity verification)
//
// GitHub OAuth is used exclusively to verify the specialist's GitHub username.
// The flow: redirect → GitHub authorises → callback → server gets login → stores in user_profiles.
// No tokens are stored. Only the public login field is kept.
// Scope requested: read:user (minimum required to get the login).
//
// To create the GitHub OAuth App:
//  1. https://github.com/settings/developers → "New OAuth App"
//  2. Authorization callback URL: http://localhost:8080/api/auth/github/callback
//  3. Copy Client ID and Client Secret to docker-compose.yml.
//
// USER_FILES_DIR note:
//
//	User project files are stored under:
//	  {USER_FILES_DIR}/{user_id}/project/{type_slug}/{project_id}/code/
//	  {USER_FILES_DIR}/{user_id}/project/{type_slug}/{project_id}/img/
//	  {USER_FILES_DIR}/{user_id}/project/{type_slug}/{project_id}/docs/
//
// WARNING: Set JWT_SECRET to a strong random value in production.
package config

import (
	"log"
	"os"
	"sync"
)

// Config holds all runtime configuration for the server.
type Config struct {
	// HTTP server
	ServerPort string

	// Queue / cache
	RedisAddr string

	// Persistence
	DBPath string

	// Security
	JWTSecret string

	// Template and asset directories
	TemplatesDir string
	StaticDir    string

	// Base directory where user-uploaded project files are stored.
	UserFilesDir string

	// GitHub OAuth — used to verify the specialist's GitHub username.
	// Both fields must be set for the GitHub connect feature to work.
	// If either is empty, the /api/auth/github endpoints return 503.
	GithubClientID     string
	GithubClientSecret string
}

var (
	once   sync.Once
	global *Config
)

// Load reads environment variables and stores the configuration globally.
// It is safe to call multiple times — only the first call has any effect.
// Returns the Config so callers can also use it directly from main().
func Load() *Config {
	once.Do(func() {
		global = &Config{
			ServerPort:   envOr("SERVER_PORT", "8080"),
			RedisAddr:    envOr("REDIS_ADDR", "localhost:6379"),
			DBPath:       envOr("DB_PATH", "data/iotmaker.db"),
			JWTSecret:    envOr("JWT_SECRET", "dev-secret-CHANGE-IN-PRODUCTION"),
			TemplatesDir: envOr("TEMPLATES_DIR", "public/templates"),
			StaticDir:    envOr("STATIC_DIR", "public"),
			UserFilesDir: envOr("USER_FILES_DIR", "public/static"),

			// GitHub OAuth — optional. Empty = feature disabled, endpoints return 503.
			GithubClientID:     envOr("GITHUB_CLIENT_ID", ""),
			GithubClientSecret: envOr("GITHUB_CLIENT_SECRET", ""),
		}

		if global.JWTSecret == "dev-secret-CHANGE-IN-PRODUCTION" {
			log.Println("[config] WARNING: JWT_SECRET is using the insecure default." +
				" Set the JWT_SECRET environment variable before deploying to production.")
		}
		if global.GithubClientID == "" {
			log.Println("[config] GitHub OAuth not configured — GITHUB_CLIENT_ID is empty." +
				" Specialists cannot verify their GitHub identity until this is set.")
		}
	})
	return global
}

// Get returns the global Config.
// Panics if Load() has not been called — this is a programming error.
func Get() *Config {
	if global == nil {
		panic("config.Get() called before config.Load()")
	}
	return global
}

// GithubOAuthEnabled reports whether GitHub OAuth is configured.
// Returns false if either GITHUB_CLIENT_ID or GITHUB_CLIENT_SECRET is empty.
func GithubOAuthEnabled() bool {
	cfg := Get()
	return cfg.GithubClientID != "" && cfg.GithubClientSecret != ""
}

// envOr returns the value of the environment variable named key,
// or def if the variable is unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
