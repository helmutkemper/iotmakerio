// email/mailer.go — Email delivery simulation for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// During development, all emails are printed to stdout with a clear visual
// format. When a real SMTP service is needed, replace the Send() function body
// while keeping the same signature — callers require no changes.
//
// Output format:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│  [EMAIL]  To: user@example.com                              │
//	│           Subject: Your IoTMaker verification code          │
//	│           Body: Your code is: 123456 (valid 15 min)         │
//	└─────────────────────────────────────────────────────────────┘
package email

import (
	"fmt"
	"log"
)

// Message is the data needed to send a single email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Send delivers an email. In this implementation it logs to stdout.
// Replace the body of this function to integrate a real SMTP client
// (e.g., net/smtp, sendgrid, mailgun, SES) without changing any caller.
//
// The body is printed without truncation so OTP codes and other important
// values are never cut off in the development log.
func Send(m Message) {
	border := "────────────────────────────────────────────────────────"
	log.Printf("\n┌%s┐\n│  [EMAIL]  To:      %-37s│\n│           Subject: %-37s│\n│           Body:    %s\n└%s┘",
		border,
		truncate(m.To, 37),
		truncate(m.Subject, 37),
		m.Body,
		border,
	)
}

// ─── Convenience constructors ─────────────────────────────────────────────────

// VerifyEmail composes and sends the account verification code email.
func VerifyEmail(to, code string) {
	Send(Message{
		To:      to,
		Subject: "Verify your IoTMaker account",
		Body:    fmt.Sprintf("Your verification code is: %s  (valid 15 min)", code),
	})
}

// LoginCode composes and sends the 2-factor login code email.
func LoginCode(to, code string) {
	Send(Message{
		To:      to,
		Subject: "Your IoTMaker login code",
		Body:    fmt.Sprintf("Your login code is: %s  (valid 15 min)", code),
	})
}

// PasswordReset composes and sends the password reset code email.
func PasswordReset(to, code string) {
	Send(Message{
		To:      to,
		Subject: "Reset your IoTMaker password",
		Body:    fmt.Sprintf("Your password reset code is: %s  (valid 15 min)", code),
	})
}

// ─── Control panel OTP emails ────────────────────────────────────────────────

// RoleChangeCode sends the OTP required to confirm a role change in the
// control panel. The code is sent to the admin performing the action,
// not the target user.
func RoleChangeCode(to, code string) {
	Send(Message{
		To:      to,
		Subject: "IoTMaker — confirm role change",
		Body:    fmt.Sprintf("Role change confirmation code: %s  (valid 15 min)", code),
	})
}

// MenuChangeCode sends the OTP required to confirm a menu section change
// (create, edit, or delete) in the control panel.
func MenuChangeCode(to, code string) {
	Send(Message{
		To:      to,
		Subject: "IoTMaker — confirm menu change",
		Body:    fmt.Sprintf("Menu change confirmation code: %s  (valid 15 min)", code),
	})
}

// TranslationsEditCode sends the OTP required to confirm a translation bundle
// save in the control panel. One email is sent per admin bundle save (i.e.
// per PUT /api/control/v1/translations/:locale) — saving several locales in a
// row produces several separate emails.
//
// The locale being edited is included in the subject so the admin can glance
// at their inbox and see what change they were about to confirm; the code
// itself is the sole authorization token.
func TranslationsEditCode(to, locale, code string) {
	Send(Message{
		To:      to,
		Subject: fmt.Sprintf("IoTMaker — confirm %s translations save", locale),
		Body: fmt.Sprintf("Translations save confirmation code for %s: %s  (valid 15 min)",
			locale, code),
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// truncate returns s cut to max runes, appending "…" when truncated.
// Used to keep the fixed-width email log boxes from wrapping.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
