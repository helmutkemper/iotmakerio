// handler/spaauth/handlers.go — JWT auth handler implementations for the SPA.
//
// All responses follow the SPA envelope format:
//
//	{ "metadata": { "status": N, "error": "..." }, "data": ... }
//
// Auth flow:
//  1. POST /api/auth/register  { username, email, password, inviteCode?, preferredLocale, displayName? }
//  2. POST /api/auth/verify-email { userId, code } → { verified: true }
//  3. POST /api/auth/login { login, password } → { userId }   (OTP sent)
//  4. POST /api/auth/login/2fa { userId, code } → { token, user }
//  5. Subsequent requests: Authorization: Bearer <token>
//
// Invite flow:
//   - GET  /api/auth/register-config  → { inviteRequired, locales }  (public)
//   - POST /api/auth/invite           → { code, expiresAt }          (requires auth)
//   - GET  /api/auth/invite/:code     → { valid, invitedBy? }        (public)
//
// RequireBearerToken, BearerClaims and SetClaims are exported so other
// handler packages can protect their routes using the same JWT validation,
// and so tests can inject identity without going through the JWT path.
//
// Logging convention inside this file:
//
//	Every path that returns HTTP 500 MUST emit log.Printf with the feature
//	tag "[spaauth/<func>]" and the underlying error BEFORE calling fail().
//	Silent 500s are a debugging nightmare — the client sees the error code
//	but the operator has nothing to correlate against. If you add a new
//	handler that can return 500, follow the same pattern.
package spaauth

import (
	"log"
	"net/http"
	"strings"
	"time"

	cryptoauth "server/auth"
	"server/config"
	"server/email"
	"server/store"

	"github.com/labstack/echo/v4"
)

// contextKeySpaClaims is the echo context key used to store validated JWT claims.
const contextKeySpaClaims = "_spa_claims"

// ─── Register config (public) ─────────────────────────────────────────────────

// handleRegisterConfig returns the data the registration form needs before
// the user fills in any fields:
//   - inviteRequired: whether a code is needed (from project_settings)
//   - locales:        available UI locales for the language selector
//
// This endpoint is intentionally unauthenticated and lightly cached (60 s)
// because it is called on every registration page load.
func handleRegisterConfig(c echo.Context) error {
	inviteRequired := store.GetSettingInt(store.SettingInviteRequired, 1) == 1

	locales, err := store.ListUILocales()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if locales == nil {
		locales = []*store.Locale{}
	}

	c.Response().Header().Set("Cache-Control", "public, max-age=60")
	return ok(c, map[string]any{
		"inviteRequired": inviteRequired,
		"locales":        locales,
	})
}

// ─── Register ─────────────────────────────────────────────────────────────────

// handleRegister creates a new user account.
//
// Required JSON fields:
//
//	username        — 3–32 chars, letters/digits/_ and -
//	email           — valid email address
//	password        — must pass ValidatePassword rules
//	preferredLocale — locale code from /api/auth/register-config (e.g. "en-US")
//
// Optional JSON fields:
//
//	inviteCode      — required when invite_required=1 in project_settings
//	displayName     — public display name shown in the feed (max 50 chars)
func handleRegister(c echo.Context) error {
	var req struct {
		Username        string `json:"username"`
		Email           string `json:"email"`
		Password        string `json:"password"`
		PreferredLocale string `json:"preferredLocale"`
		InviteCode      string `json:"inviteCode"`
		DisplayName     string `json:"displayName"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.InviteCode = strings.TrimSpace(req.InviteCode)

	if req.PreferredLocale == "" {
		req.PreferredLocale = "en-US"
	}

	if err := cryptoauth.ValidateUsername(req.Username); err != nil {
		return fail(c, 400, err.Error())
	}
	if err := cryptoauth.ValidatePassword(req.Password); err != nil {
		return fail(c, 400, err.Error())
	}
	if len([]rune(req.DisplayName)) > 50 {
		return fail(c, 400, "display name must be 50 characters or fewer")
	}

	hash, err := cryptoauth.HashPassword(req.Password)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	id, err := cryptoauth.NewID()
	if err != nil {
		return fail(c, 500, "internal error")
	}

	u := &store.User{
		ID:              id,
		Username:        req.Username,
		Email:           req.Email,
		PasswordHash:    hash,
		Role:            store.RoleUser,
		Verified:        false,
		PreferredLocale: req.PreferredLocale,
	}
	profile := &store.UserProfile{
		UserID:      id,
		DisplayName: req.DisplayName,
	}

	// RegisterUser validates the invite (if required), creates the user,
	// redeems the invite, and creates the profile in a single transaction.
	if err := store.RegisterUser(store.RegisterUserArgs{
		User:       u,
		Profile:    profile,
		InviteCode: req.InviteCode,
	}); err != nil {
		switch err {
		case store.ErrInviteRequired:
			return fail(c, 400, "An invite code is required to register.")
		case store.ErrInvalidInvite:
			return fail(c, 400, "Invite code is invalid, already used, or expired.")
		case store.ErrConflict:
			return fail(c, 400, "Username or email already registered.")
		default:
			return fail(c, 500, "internal error")
		}
	}

	// Send email verification OTP.
	code, otpID, err := newOTP()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if err := store.CreateOTP(&store.OTPCode{
		ID: otpID, UserID: id, Code: code,
		Purpose: store.OTPPurposeVerifyEmail,
	}); err != nil {
		return fail(c, 500, "internal error")
	}
	go email.VerifyEmail(req.Email, code)

	return ok(c, map[string]any{"userId": id})
}

// ─── Verify Email ─────────────────────────────────────────────────────────────

func handleVerifyEmail(c echo.Context) error {
	var req struct {
		UserID string `json:"userId"`
		Code   string `json:"code"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}
	if err := store.ConsumeOTP(req.UserID, req.Code, store.OTPPurposeVerifyEmail); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 400, "Invalid or expired code.")
		}
		return fail(c, 500, "internal error")
	}
	if err := store.VerifyUser(req.UserID); err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]bool{"verified": true})
}

// ─── Login step 1 ─────────────────────────────────────────────────────────────

func handleLogin(c echo.Context) error {
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	u, err := store.GetUserByLogin(strings.TrimSpace(req.Login))
	if err != nil {
		// Timing-safe: always run bcrypt even when user not found.
		_ = cryptoauth.CheckPassword("$2a$12$dummy.hash.for.timing.protection.00000000000", req.Password)
		return fail(c, 401, "Credenciais inválidas.")
	}
	if !cryptoauth.CheckPassword(u.PasswordHash, req.Password) {
		return fail(c, 401, "Credenciais inválidas.")
	}
	if !u.Verified {
		return fail(c, 403, "E-mail não verificado.")
	}

	code, otpID, err := newOTP()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if err := store.CreateOTP(&store.OTPCode{
		ID: otpID, UserID: u.ID, Code: code,
		Purpose: store.OTPPurposeLoginTwoFactor,
	}); err != nil {
		return fail(c, 500, "internal error")
	}
	go email.LoginCode(u.Email, code)

	return ok(c, map[string]any{"userId": u.ID})
}

// ─── Login step 2 (2FA) ───────────────────────────────────────────────────────

func handleLogin2FA(c echo.Context) error {
	var req struct {
		UserID string `json:"userId"`
		Code   string `json:"code"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}
	if err := store.ConsumeOTP(req.UserID, req.Code, store.OTPPurposeLoginTwoFactor); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 401, "Código inválido.")
		}
		return fail(c, 500, "internal error")
	}

	u, err := store.GetUserByID(req.UserID)
	if err != nil {
		return fail(c, 404, "user not found")
	}

	cfg := config.Get()
	token, err := cryptoauth.NewJWT(u.ID, u.Role, cfg.JWTSecret)
	if err != nil {
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]any{
		"token": token,
		"user":  u.Public(),
	})
}

// ─── Me ───────────────────────────────────────────────────────────────────────

func handleMe(c echo.Context) error {
	claims := BearerClaims(c)
	u, err := store.GetUserByID(claims.UserID)
	if err != nil {
		return fail(c, 404, "user not found")
	}
	return ok(c, u.Public())
}

// ─── Logout ───────────────────────────────────────────────────────────────────

// JWT is stateless — the client discards the token. No server action needed.
func handleLogout(c echo.Context) error {
	return ok(c, map[string]bool{"ok": true})
}

// ─── Forgot Password ──────────────────────────────────────────────────────────

func handleForgotPassword(c echo.Context) error {
	var req struct {
		Email string `json:"email"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}
	// Anti-enumeration: always return 200.
	u, err := store.GetUserByEmail(strings.TrimSpace(req.Email))
	if err == nil && u.Verified {
		if code, otpID, err := newOTP(); err == nil {
			_ = store.CreateOTP(&store.OTPCode{
				ID: otpID, UserID: u.ID, Code: code,
				Purpose: store.OTPPurposeResetPassword,
			})
			go email.PasswordReset(req.Email, code)
		}
	}
	return ok(c, map[string]bool{"sent": true})
}

// ─── Reset Password ───────────────────────────────────────────────────────────

func handleResetPassword(c echo.Context) error {
	var req struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}
	if err := cryptoauth.ValidatePassword(req.NewPassword); err != nil {
		return fail(c, 400, err.Error())
	}

	u, err := store.GetUserByEmail(strings.TrimSpace(req.Email))
	if err != nil {
		return fail(c, 400, "Código inválido ou expirado.")
	}
	if err := store.ConsumeOTP(u.ID, req.Code, store.OTPPurposeResetPassword); err != nil {
		return fail(c, 400, "Código inválido ou expirado.")
	}
	hash, err := cryptoauth.HashPassword(req.NewPassword)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if err := store.UpdatePassword(u.ID, hash); err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]bool{"reset": true})
}

// ─── Invite: generate ─────────────────────────────────────────────────────────

// handleGenerateInvite creates a new invite code for the authenticated user.
//
// Only verified users can generate invites. The expiry is controlled by the
// invite_code_expires_days setting in project_settings.
//
// Response:
//
//	{ "code": "abc123...", "expiresAt": "2025-01-08T..." }
//
// The client constructs the full invite URL using window.location.origin:
//
//	`${window.location.origin}/app#register?invite=${code}`
//
// Every error path logs with the tag "[spaauth/invite]" before calling fail().
// See the logging convention in the package doc. Silent 500s leave the
// operator with nothing to correlate against the client-side complaint.
func handleGenerateInvite(c echo.Context) error {
	claims := BearerClaims(c)

	// Verify the user is verified (verified=1) before allowing invite creation.
	u, err := store.GetUserByID(claims.UserID)
	if err != nil {
		log.Printf("[spaauth/invite] user lookup failed for uid=%q: %v", claims.UserID, err)
		return fail(c, 404, "user not found")
	}
	if !u.Verified {
		log.Printf("[spaauth/invite] rejecting invite generation for unverified uid=%q", claims.UserID)
		return fail(c, 403, "only verified users can generate invite codes")
	}

	expireDays := store.GetSettingInt(store.SettingInviteCodeExpiresDays, 7)
	expiresAt := time.Now().UTC().Add(time.Duration(expireDays) * 24 * time.Hour)

	code, err := cryptoauth.NewInviteCode()
	if err != nil {
		log.Printf("[spaauth/invite] NewInviteCode failed for uid=%q: %v", claims.UserID, err)
		return fail(c, 500, "internal error")
	}
	invID, err := cryptoauth.NewID()
	if err != nil {
		log.Printf("[spaauth/invite] NewID failed for uid=%q: %v", claims.UserID, err)
		return fail(c, 500, "internal error")
	}

	inv := &store.InviteCode{
		ID:        invID,
		Code:      code,
		CreatedBy: claims.UserID,
		ExpiresAt: expiresAt,
	}
	if err := store.CreateInvite(inv); err != nil {
		// Most likely causes, in order: the invite_codes table is missing
		// (failed migration), a FK constraint against users(id) is violated
		// (stale token), or the unique constraint on code collided (extremely
		// unlikely with a cryptographic random).
		log.Printf("[spaauth/invite] CreateInvite failed for uid=%q invID=%q: %v",
			claims.UserID, invID, err)
		return fail(c, 500, "could not create invite code")
	}

	log.Printf("[spaauth/invite] invite generated: uid=%q invID=%q expiresAt=%s",
		claims.UserID, invID, expiresAt.Format(time.RFC3339))

	return ok(c, map[string]any{
		"code":      code,
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

// handleValidateInvite checks whether an invite code is currently valid.
// It never returns an error status for invalid codes — it always returns 200
// with valid:true or valid:false to prevent enumeration timing attacks.
//
// When valid, it also returns the username of who created the invite so the
// registration form can show "You were invited by @username".
func handleValidateInvite(c echo.Context) error {
	code := strings.TrimSpace(c.Param("code"))
	if code == "" {
		return ok(c, map[string]any{"valid": false})
	}

	inv, err := store.ValidateInviteCode(code)
	if err != nil {
		// Invalid, used, or expired — always 200.
		return ok(c, map[string]any{"valid": false})
	}

	// Resolve the username of the inviter for the "invited by" display.
	creator, err := store.GetUserByID(inv.CreatedBy)
	if err != nil {
		// Creator account deleted — still a valid code.
		return ok(c, map[string]any{"valid": true})
	}
	return ok(c, map[string]any{
		"valid":     true,
		"invitedBy": creator.Username,
	})
}

// ─── Bearer middleware (exported) ─────────────────────────────────────────────

// RequireBearerToken validates the Authorization Bearer token and stores the
// parsed claims in the echo context. Exported for use by other handler packages.
func RequireBearerToken() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			hdr := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(hdr, "Bearer ") {
				return fail(c, 401, "missing bearer token")
			}
			tokenStr := strings.TrimPrefix(hdr, "Bearer ")
			cfg := config.Get()
			claims, err := cryptoauth.ParseJWT(tokenStr, cfg.JWTSecret)
			if err != nil {
				return fail(c, 401, "invalid or expired token")
			}
			SetClaims(c, claims)
			return next(c)
		}
	}
}

// BearerClaims extracts the validated JWT claims from the echo context.
// Must only be called inside a handler protected by RequireBearerToken,
// or after a SetClaims call (used by tests to inject identity without
// going through the JWT path).
//
// Returns an empty Claims (not nil) when the context carries no claims.
// Callers that need to detect "no identity" should check claims.UserID
// against the empty string rather than nil-checking the result.
func BearerClaims(c echo.Context) *cryptoauth.Claims {
	v := c.Get(contextKeySpaClaims)
	if v == nil {
		return &cryptoauth.Claims{}
	}
	return v.(*cryptoauth.Claims)
}

// SetClaims stores the given claims in the echo context using the same key
// BearerClaims reads from. RequireBearerToken calls it after a successful
// JWT parse; tests call it directly from a stub middleware so handlers
// can be exercised without minting real tokens.
//
// Exported because the context key is intentionally private — anything
// that needs to populate claims must come through this function so the
// storage location stays a single-point-of-truth.
//
// Português:
//
//	Setter exportado para a chave de contexto onde BearerClaims lê.
//	Usado pelo próprio middleware Bearer e por middlewares de teste
//	que injetam identidade sem passar pelo pipeline JWT.
func SetClaims(c echo.Context, claims *cryptoauth.Claims) {
	c.Set(contextKeySpaClaims, claims)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": 200},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}

func newOTP() (code, id string, err error) {
	code, err = cryptoauth.NewOTPCode()
	if err != nil {
		return
	}
	id, err = cryptoauth.NewID()
	return
}
