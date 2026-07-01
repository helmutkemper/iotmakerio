// server/handler/storeapi/handler.go — GET /store/redirect/:listing_id
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Handles the transparent redirect to external stores with affiliate tracking.
// The WASM never receives the affiliate URL — the server builds it here at
// redirect time, so tags can be rotated without any frontend redeployment.
//
// Flow:
//
//  1. Maker clicks "Buy" in the BOM panel.
//  2. WASM navigates to GET /store/redirect/:listing_id
//  3. Server looks up listing + store, logs the click, responds HTTP 302.
//  4. Browser lands on the external store URL transparently.
//
// Final URL format:
//
//	store.base_url + listing.product_url + "?tag=" + store.affiliate_tag
//
// Registration in main.go (on the root Echo instance, not inside /api/v1):
//
//	storeapi.Register(e)  →  GET /store/redirect/:id
package storeapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/middleware"
	"server/store"
)

// Register wires the store redirect route onto the root Echo instance.
// Must be called on `e` directly (not on a sub-group) because the path
// lives at /store/redirect, outside the /api/v1 prefix.
func Register(e *echo.Echo) {
	e.GET("/store/redirect/:id", handleStoreRedirect)
}

// handleStoreRedirect logs the click and issues an HTTP 302 to the external store.
//
// Path param: :id — the store_listings.id value sent by the WASM BOM panel.
//
// Error responses:
//   - 400 if the listing ID param is empty.
//   - 404 if the listing does not exist in the database.
//   - 503 if the store is marked inactive (e.g. affiliate agreement ended).
//   - 500 on any other internal error.
func handleStoreRedirect(c echo.Context) error {
	listingID := c.Param("id")
	if listingID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "listing id required")
	}

	// Fetch listing and its parent store in a single query.
	listing, st, err := store.GetListingWithStore(listingID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "listing not found")
		}
		c.Logger().Errorf("[storeapi] GetListingWithStore %s: %v", listingID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}

	// Reject inactive stores so a deactivated affiliate agreement stops working
	// immediately without requiring a database cleanup of all listings.
	if !st.Active {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "store is currently unavailable")
	}

	// Build the affiliate URL entirely server-side.
	finalURL := buildStoreURL(st.BaseURL, listing.ProductURL, st.AffiliateTag)

	// Resolve optional user ID for analytics — nil for anonymous requests.
	var userID *string
	if user := middleware.UserFromContext(c); user != nil {
		uid := user.ID
		userID = &uid
	}

	// Infer country from Cloudflare header (best-effort, analytics only).
	// Falls back to empty string when the header is absent.
	ipCountry := c.Request().Header.Get("CF-IPCountry")

	// Log the click in a goroutine so the redirect is not delayed by the
	// DB write. A failed log entry is non-fatal — the redirect still succeeds.
	go func() {
		logID := cryptoauth.MustNewID()
		if logErr := store.LogRedirect(logID, listingID, userID, ipCountry); logErr != nil {
			c.Logger().Errorf("[storeapi] LogRedirect %s: %v", listingID, logErr)
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
