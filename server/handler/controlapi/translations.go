// server/handler/controlapi/translations.go — Admin translation write API.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Routes (mounted on /api/control/v1 by RegisterTranslations):
//
//	POST /api/control/v1/translations/otp       — request OTP code (emailed)
//	PUT  /api/control/v1/translations/:locale   — replace bundle (consumes OTP)
//
// Flow
// ----
//
//  1. Admin edits translations in the control-panel SPA.
//  2. Admin clicks "Save" for a locale.
//  3. Frontend calls POST /translations/otp → server generates a code,
//     persists it with purpose=OTPPurposeTranslationsEdit, emails the admin.
//  4. Modal asks for the 6-digit code.
//  5. Frontend calls PUT /translations/:locale with { messages, otp_code } —
//     server validates input, consumes the OTP atomically, then performs
//     the bulk replace inside a transaction.
//
// Design notes
// ------------
//
//   - One OTP per bundle save. Editing both pt-BR and en-US in sequence
//     requires two independent codes. Per CLAUDE.md: "grupo de palavras" =
//     one locale bundle. This is friction by design — translations ship
//     globally, so every commit must be explicitly acknowledged.
//
//   - The OTP is tied to the admin's user_id, not to the locale. An admin
//     cannot use another admin's code, and a stolen control token alone is
//     insufficient to save a bundle (attacker would also need access to the
//     admin's email inbox).
//
//   - Input is validated BEFORE the OTP is consumed. A malformed request
//     (missing messages field, bad locale) returns 400 without burning a
//     valid code. Once validation passes we consume the code atomically via
//     store.ConsumeOTP, then run ReplaceBundle — if ReplaceBundle itself
//     fails, the OTP is already spent and the admin must request a new one.
//     That is deliberate: retrying with the same code would let an attacker
//     brute-force past a transient DB error.
//
//   - There is intentionally no single-message endpoint. The UI always sends
//     a complete bundle; missing entries are deleted on save. If a single-
//     key API is ever needed, add it here with OTP — never under /api/v1.
package controlapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"server/email"
	"server/permission"
	"server/store"
)

// RegisterTranslations mounts the admin translation write endpoints on the
// given /api/control/v1 group. Called from server/cmd/server/main.go after
// RegisterControl (the ordering inside the group does not matter; echo
// matches routes by path).
func RegisterTranslations(g *echo.Group) {
	g.POST("/translations/otp",
		handleRequestTranslationsOTP,
		RequireControlToken(permission.PermTranslationsEdit),
	)
	g.PUT("/translations/:locale",
		handleReplaceTranslationsBundle,
		RequireControlToken(permission.PermTranslationsEdit),
	)
}

// ─── Handlers ────────────────────────────────────────────────────────────────

// handleRequestTranslationsOTP generates a one-time code and emails it to
// the admin. Returns 200 with a friendly message — the code itself never
// leaves the server over HTTP.
//
// A fresh request invalidates the admin's previous pending translations
// code (CreateOTP wipes older codes for the same user_id + purpose). This
// lets the admin cleanly retry if they mistyped or lost the earlier email.
//
// The locale the admin is about to save can be supplied via ?locale=xx-YY;
// it is only used in the email subject to help the admin recognise the
// message in their inbox — it carries no security meaning.
func handleRequestTranslationsOTP(c echo.Context) error {
	caller := ControlClaims(c)

	admin, err := store.GetUserByID(caller.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not load admin account")
	}

	code, otpID, err := newOTP()
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not generate code")
	}

	if err := store.CreateOTP(&store.OTPCode{
		ID:      otpID,
		UserID:  caller.UserID,
		Code:    code,
		Purpose: store.OTPPurposeTranslationsEdit,
	}); err != nil {
		return fail(c, http.StatusInternalServerError, "could not store code")
	}

	locale := strings.TrimSpace(c.QueryParam("locale"))
	if locale == "" {
		locale = "translations"
	}
	go email.TranslationsEditCode(admin.Email, locale, code)

	return ok(c, map[string]any{"message": "code sent to your registered email"})
}

// handleReplaceTranslationsBundle applies a full bundle replace for the
// locale in the URL path. Consumes one OTP per call.
//
// Request body:
//
//	{
//	  "messages": [ {"id":"nav.home", "other":"Home", ...}, ... ],
//	  "otp_code": "123456"
//	}
//
// Responses:
//
//	200 — bundle saved; response.data is the refreshed TrBundle.
//	400 — malformed body, missing messages or otp_code, invalid locale.
//	401 — otp_code invalid, expired, or already used.
//	403 — caller lacks PermTranslationsEdit (handled by middleware).
//	500 — database error during ReplaceBundle (OTP is already consumed —
//	      admin must request a new code to retry).
func handleReplaceTranslationsBundle(c echo.Context) error {
	caller := ControlClaims(c)

	var body struct {
		Messages []store.TrMessage `json:"messages"`
		OTPCode  string            `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}

	// ── Input validation (happens BEFORE consuming the OTP) ──────────────
	locale := strings.TrimSpace(c.Param("locale"))
	if locale == "" {
		return fail(c, http.StatusBadRequest, "locale is required")
	}
	if body.Messages == nil {
		return fail(c, http.StatusBadRequest, "messages field is required")
	}
	if strings.TrimSpace(body.OTPCode) == "" {
		return fail(c, http.StatusBadRequest, "otp_code is required")
	}

	// Per-message validation — empty IDs would produce unreachable rows.
	for i, m := range body.Messages {
		if strings.TrimSpace(m.ID) == "" {
			return fail(c, http.StatusBadRequest,
				"message at index "+strconv.Itoa(i)+" is missing its id")
		}
	}

	// ── OTP consumption (atomic; only happens after validation passes) ───
	err := store.ConsumeOTP(caller.UserID, body.OTPCode, store.OTPPurposeTranslationsEdit)
	if err != nil {
		return fail(c, http.StatusUnauthorized, "invalid or expired confirmation code")
	}

	// ── Commit the bundle replace ────────────────────────────────────────
	bundle, err := store.ReplaceBundle(locale, body.Messages)
	if err != nil {
		// The OTP is already consumed — admin must request a new one.
		// We do NOT re-insert the OTP here; allowing retries with the same
		// code would widen the window an attacker could exploit.
		return fail(c, http.StatusInternalServerError, "could not save bundle")
	}

	return ok(c, bundle)
}
