// server/handler/bomapi/handler.go — GET /api/v1/bom
//
// Returns the Bill of Materials for a list of black-box struct names present
// on the maker's canvas, filtered to the user's country. Each entry in the
// response includes component info and all available store listings so the
// WASM BOM panel can render a "Buy" button per component.
//
// The store redirect (GET /store/redirect/:id) lives in handler/storeapi.
//
// Authentication: optional. Anonymous requests must supply ?country= as
// a fallback; without it all store listings are returned regardless of country.
package bomapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"server/middleware"
	"server/store"
)

// Register wires the BOM route onto the given API group.
func Register(g *echo.Group) {
	g.GET("/bom", handleGetBOM)
}

// ─── BOM ──────────────────────────────────────────────────────────────────────

// handleGetBOM returns the Bill of Materials for a list of black-box names.
//
// Query params:
//
//	names    — comma-separated list of Go struct names (e.g. "APDS9960,ATtiny85")
//	country  — ISO 3166-1 alpha-2 fallback when the user is not authenticated
//
// Authenticated users have their country resolved from preferred_locale.
// Anonymous users must supply the ?country= param; without it they receive
// all listings regardless of country (useful for testing).
func handleGetBOM(c echo.Context) error {
	// Parse black-box names from query string.
	raw := c.QueryParam("names")
	if raw == "" {
		return fail(c, http.StatusBadRequest, "names param is required")
	}
	names := splitAndTrim(raw, ",")
	if len(names) == 0 {
		return fail(c, http.StatusBadRequest, "names param is empty")
	}

	// Resolve country code.
	countryCode := resolveCountry(c)

	bom, err := store.GetBOMForBlackBoxes(names, countryCode)
	if err != nil {
		c.Logger().Errorf("[bom] GetBOMForBlackBoxes: %v", err)
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	return ok(c, map[string]any{
		"bom":     bom,
		"country": countryCode,
	})
}

// resolveCountry returns the country code for the request.
// Authenticated users: extracted from preferred_locale (e.g. "pt-BR" → "BR").
// Anonymous users: from the ?country= query param.
// Fallback: empty string (all countries).
func resolveCountry(c echo.Context) string {
	if user := middleware.UserFromContext(c); user != nil {
		parts := strings.SplitN(user.PreferredLocale, "-", 2)
		if len(parts) == 2 {
			return strings.ToUpper(parts[1])
		}
	}
	return strings.ToUpper(c.QueryParam("country"))
}

// splitAndTrim splits s by sep and trims whitespace from each element,
// discarding empty entries.
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ─── Response helpers ─────────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": http.StatusOK, "error": ""},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
