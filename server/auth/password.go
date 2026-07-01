// auth/password.go — Password validation rules for the IoTMaker portal.
//
// Rules (NIST SP 800-63B aligned):
//   - Minimum 8 characters
//   - At least one uppercase letter
//   - At least one lowercase letter
//   - At least one digit
//   - Maximum 72 characters (bcrypt hard limit)
//
// These rules strike a balance between security and usability.
// We do NOT enforce special characters — length and character class
// diversity already provide adequate entropy for most threat models.
package auth

import (
	"errors"
	"unicode"
)

// Password validation error messages (exported so handlers can translate them).
var (
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 72 characters")
	ErrPasswordNoUpper  = errors.New("password must contain at least one uppercase letter")
	ErrPasswordNoLower  = errors.New("password must contain at least one lowercase letter")
	ErrPasswordNoDigit  = errors.New("password must contain at least one digit")
)

// ValidatePassword checks the plaintext password against all security rules.
// Returns the first violation found, or nil if the password is acceptable.
func ValidatePassword(p string) error {
	runes := []rune(p)
	if len(runes) < 8 {
		return ErrPasswordTooShort
	}
	if len(runes) > 72 {
		return ErrPasswordTooLong
	}

	var hasUpper, hasLower, hasDigit bool
	for _, r := range runes {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}

	if !hasUpper {
		return ErrPasswordNoUpper
	}
	if !hasLower {
		return ErrPasswordNoLower
	}
	if !hasDigit {
		return ErrPasswordNoDigit
	}
	return nil
}

// ValidateUsername checks username formatting rules.
// Allowed: letters, digits, underscore, hyphen. Length: 3–32.
func ValidateUsername(u string) error {
	runes := []rune(u)
	if len(runes) < 3 {
		return errors.New("username must be at least 3 characters")
	}
	if len(runes) > 32 {
		return errors.New("username must be at most 32 characters")
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return errors.New("username may only contain letters, digits, _ and -")
		}
	}
	return nil
}
