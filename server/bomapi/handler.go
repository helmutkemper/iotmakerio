// server/handler/bomapi/handler.go — GET /api/bom and GET /store/redirect/:listing_id
//
// Two related handlers that together implement the commerce feature:
//
//  1. GET /api/bom?names=APDS9960,ATtiny85
//     Returns the Bill of Materials for a list of black-box struct names,
//     filtered to the user's country. Each entry includes component info
//     and all available store listings so the WASM can render a "Buy" list.
//
//  2. GET /store/redirect/:listing_id
//     Logs the click and redirects the browser to the external store via
//     HTTP 302. The WASM never sees the affiliate URL — the server builds it.
//
// Both endpoints are public (no auth required). Anonymous BOM requests use
// the ?country= query param as a fallback when no user session is present.
package bomapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/middleware"
	"server/store"
)

// Register wires the BOM and store redirect routes.
// g is the base API group (e.g. /api/v1).
// e is the root Echo instance (for /store/redirect which lives outside /api).
func Register(g *echo.Group, e *echo.Echo) {
	g.GET("/bom", handleGetBOM)
	e.GET("/store/redirect/:id", handleStoreRedirect)
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

// ─── Store redirect ───────────────────────────────────────────────────────────

// handleStoreRedirect logs the click and redirects to the external store.
//
// Path param: :id — the store_listings.id value.
//
// The server builds the final URL:
//
//	final = store.base_url + listing.product_url + "?tag=" + store.affiliate_tag
//
// Response: HTTP 302 to the external store URL.
// The browser follows the redirect transparently.
//
// Error cases:
//   - 404 if the listing does not exist.
//   - 503 if the store is marked inactive.
//   - 500 on any other database error.
func handleStoreRedirect(c echo.Context) error {
	listingID := c.Param("id")
	if listingID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "listing id required")
	}

	listing, st, err := store.GetListingWithStore(listingID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "listing not found")
		}
		c.Logger().Errorf("[store] GetListingWithStore %s: %v", listingID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}

	if !st.Active {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "store is currently unavailable")
	}

	// Build the affiliate URL server-side so the client never sees the tag.
	finalURL := buildStoreURL(st.BaseURL, listing.ProductURL, st.AffiliateTag)

	// Resolve optional user ID for analytics.
	var userID *string
	if user := middleware.UserFromContext(c); user != nil {
		uid := user.ID
		userID = &uid
	}

	// Infer country from the request IP header (best-effort, for analytics only).
	ipCountry := c.Request().Header.Get("CF-IPCountry") // Cloudflare header
	if ipCountry == "" {
		ipCountry = resolveCountry(c)
	}

	// Log the click asynchronously so the redirect is not delayed.
	go func() {
		logID := cryptoauth.MustNewID()
		if logErr := store.LogRedirect(logID, listingID, userID, ipCountry); logErr != nil {
			c.Logger().Errorf("[store] LogRedirect %s: %v", listingID, logErr)
		}
	}()

	return c.Redirect(http.StatusFound, finalURL)
}

// buildStoreURL concatenates base URL, product path, and affiliate tag.
//
// Examples:
//
//	buildStoreURL("https://sparkfun.com", "/products/123", "iotmaker10")
//	→ "https://sparkfun.com/products/123?tag=iotmaker10"
//
//	buildStoreURL("https://sparkfun.com", "/products/123", "")
//	→ "https://sparkfun.com/products/123"
func buildStoreURL(baseURL, productURL, affiliateTag string) string {
	// Normalise: remove trailing slash from base, ensure leading slash on path.
	base := strings.TrimRight(baseURL, "/")
	path := productURL
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	full := base + path
	if affiliateTag != "" {
		sep := "?"
		if strings.Contains(full, "?") {
			sep = "&"
		}
		full = fmt.Sprintf("%s%stag=%s", full, sep, affiliateTag)
	}
	return full
}

// resolveCountry returns the country code for the request.
// Authenticated users: from preferred_locale (e.g. "pt-BR" → "BR").
// Anonymous users: from the ?country= query param.
// Fallback: empty string (means "all countries").
func resolveCountry(c echo.Context) string {
	if user := middleware.UserFromContext(c); user != nil {
		// Extract country from locale tag: "pt-BR" → "BR", "en-US" → "US".
		parts := strings.SplitN(user.PreferredLocale, "-", 2)
		if len(parts) == 2 {
			return strings.ToUpper(parts[1])
		}
	}
	return strings.ToUpper(c.QueryParam("country"))
}

// splitAndTrim splits s by sep and trims whitespace from each element.
// Empty elements are discarded.
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
