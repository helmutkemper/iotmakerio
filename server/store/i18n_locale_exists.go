// /ide/server/store/i18n_locale_exists.go
// store/i18n_locale_exists.go — Locale existence check for the IoTMaker portal.
//
// This file provides a lightweight validation function used by the locale
// update endpoint (PUT /api/v1/profile/locale) to verify that the requested
// locale code exists in i18n_bundles before persisting it to the users table.
//
// Separated from i18n.go because it serves a different concern: input
// validation for user preferences, not translation bundle CRUD.
package store

// LocaleExists returns true if the given locale code has a corresponding
// row in i18n_bundles. This is the cheapest possible validation — a single
// COUNT(*) query with no joins or message loading.
//
// Used by the locale update handler to reject codes that don't correspond
// to any registered translation bundle (e.g. typos, unsupported locales).
func LocaleExists(code string) (bool, error) {
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM i18n_bundles WHERE locale = ?`, code,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
