// server/handler/spaauth/github.go — GitHub OAuth for specialist identity verification.
//
// CSRF protection:
//
//	O state é um token hex aleatório de 32 bytes guardado no Redis com TTL de 10 min.
//	O valor é o userID. O callback usa GetDel (atômico) para consumir o token uma
//	única vez. Isso elimina a dependência do JWT_SECRET e é mais robusto que JWT.
//
// Routes:
//
//	GET /api/auth/github           — redirect to GitHub OAuth consent page
//	GET /api/auth/github/callback  — handle GitHub callback, save username
package spaauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	cryptoauth "server/auth"
	"server/config"
	"server/store"
)

const (
	githubAuthURL        = "https://github.com/login/oauth/authorize"
	githubTokenURL       = "https://github.com/login/oauth/access_token"
	githubUserURL        = "https://api.github.com/user"
	githubStateKeyPrefix = "github:oauth:state:"
	githubStateTTL       = 10 * time.Minute
	githubScope          = "read:user"
)

// githubHandler holds the Redis client used for state storage.
type githubHandler struct {
	redis *redis.Client
}

// handleGithubConnect redirects the authenticated user to GitHub's OAuth consent page.
//
// The Bearer token arrives as ?token= because the browser navigates here directly
// via window.location.href — it cannot send Authorization headers in a full navigation.
// The handler validates the token manually before generating the state.
func (h *githubHandler) handleGithubConnect(c echo.Context) error {
	log.Println("[github/connect] connect requested")
	cfg := config.Get()
	if !config.GithubOAuthEnabled() {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "GitHub OAuth is not configured on this server",
		})
	}

	// Resolve user identity — Authorization header first, then ?token=.
	// BearerClaims returns an empty struct (not nil) when no header is present,
	// so we check UserID rather than nil.
	claims := BearerClaims(c)
	if claims.UserID == "" {
		tokenStr := c.QueryParam("token")
		if tokenStr == "" {
			log.Println("[github/connect] ERROR: no bearer header and no ?token= param")
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		}
		parsed, err := cryptoauth.ParseJWT(tokenStr, cfg.JWTSecret)
		if err != nil {
			log.Printf("[github/connect] ERROR: token parse failed: %v", err)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		}
		if parsed.Issuer != "iotmaker" {
			log.Printf("[github/connect] ERROR: wrong issuer: %q", parsed.Issuer)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "wrong token issuer"})
		}
		log.Printf("[github/connect] token valid via ?token= — userID=%s", parsed.UserID)
		claims = parsed
	} else {
		log.Printf("[github/connect] token valid via Authorization header — userID=%s", claims.UserID)
	}

	// Generate a random state token and store userID in Redis.
	// The callback will look up the userID by state token.
	stateToken, err := generateStateToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not generate state"})
	}

	ctx := c.Request().Context()
	if err := h.redis.Set(ctx, githubStateKeyPrefix+stateToken, claims.UserID, githubStateTTL).Err(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not store state"})
	}

	params := url.Values{}
	params.Set("client_id", cfg.GithubClientID)
	params.Set("scope", githubScope)
	params.Set("state", stateToken)

	return c.Redirect(http.StatusFound, githubAuthURL+"?"+params.Encode())
}

// handleGithubCallback receives the OAuth callback from GitHub.
// Validates state via Redis GetDel (atomic read + delete — single use).
func (h *githubHandler) handleGithubCallback(c echo.Context) error {
	log.Println("[github/callback] callback received")

	cfg := config.Get()
	if !config.GithubOAuthEnabled() {
		log.Println("[github/callback] ERROR: OAuth not configured")
		return c.Redirect(http.StatusFound, "/app#profile?github=error&reason=not_configured")
	}

	code := c.QueryParam("code")
	state := c.QueryParam("state")
	log.Printf("[github/callback] code present=%v  state present=%v", code != "", state != "")

	if code == "" || state == "" {
		log.Println("[github/callback] ERROR: missing code or state params")
		return c.Redirect(http.StatusFound, "/app#profile?github=error&reason=missing_params")
	}

	// Consume the state token atomically — prevents replay.
	ctx := c.Request().Context()
	log.Printf("[github/callback] looking up Redis state key %q", githubStateKeyPrefix+state[:8]+"...")
	userID, err := h.redis.GetDel(ctx, githubStateKeyPrefix+state).Result()
	if err == redis.Nil {
		log.Println("[github/callback] ERROR: state not found in Redis (expired or already used)")
		return c.Redirect(http.StatusFound, "/app#profile?github=error&reason=invalid_state")
	}
	if err != nil {
		log.Printf("[github/callback] ERROR: Redis GetDel failed: %v", err)
		return c.Redirect(http.StatusFound, "/app#profile?github=error&reason=redis_error")
	}
	log.Printf("[github/callback] state valid — userID=%s", userID)

	// Exchange code for access token.
	log.Println("[github/callback] exchanging code for access token...")
	accessToken, err := exchangeGithubCode(code, cfg.GithubClientID, cfg.GithubClientSecret)
	if err != nil {
		log.Printf("[github/callback] ERROR: token exchange failed: %v", err)
		return c.Redirect(http.StatusFound, "/app#profile?github=error&reason=token_exchange")
	}
	log.Println("[github/callback] access token obtained (not stored)")

	// Fetch GitHub login from API.
	log.Println("[github/callback] fetching GitHub login from api.github.com/user...")
	githubLogin, err := fetchGithubLogin(accessToken)
	if err != nil {
		log.Printf("[github/callback] ERROR: GitHub API call failed: %v", err)
		return c.Redirect(http.StatusFound, "/app#profile?github=error&reason=api_error")
	}
	log.Printf("[github/callback] GitHub login: %q", githubLogin)

	// Save to database.
	log.Printf("[github/callback] saving github_username=%q for userID=%s...", githubLogin, userID)
	if err := store.SetGithubUsername(userID, githubLogin); err != nil {
		log.Printf("[github/callback] ERROR: store.SetGithubUsername failed: %v", err)
		return c.Redirect(http.StatusFound, "/app#profile?github=error&reason=db_error")
	}
	log.Printf("[github/callback] SUCCESS — github_username=%q saved for userID=%s", githubLogin, userID)

	return c.Redirect(http.StatusFound, "/app#profile?github=connected")
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// generateStateToken returns a cryptographically random 32-byte hex string.
func generateStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func exchangeGithubCode(code, clientID, clientSecret string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body := url.Values{}
	body.Set("client_id", clientID)
	body.Set("client_secret", clientSecret)
	body.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, "POST", githubTokenURL,
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("token exchange decode: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("github: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("github returned empty access token")
	}
	return result.AccessToken, nil
}

func fetchGithubLogin(accessToken string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", githubUserURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "IoTMaker/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github user api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("github user api status %d: %s", resp.StatusCode, b)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("github user decode: %w", err)
	}
	if user.Login == "" {
		return "", fmt.Errorf("github returned empty login")
	}
	return user.Login, nil
}
