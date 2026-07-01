// server/handler/i18n/routes.go — Public translation routes.
//
// All write paths (bundle save, message upsert) have been moved to
// server/handler/controlapi/translations.go under /api/control/v1/*. Only
// the three public endpoints remain here:
//
//	GET  /api/v1/translations                 → list locales
//	GET  /api/v1/translations/:locale         → fetch one bundle
//	POST /api/v1/translations/:locale/missing → IDE WASM reports a missing key
//
// No auth middleware is attached — these routes are intentionally open so
// the portal landing page and the WASM runtime can bootstrap their UI for
// anonymous visitors.
package i18n

import "github.com/labstack/echo/v4"

// Register mounts the public translation API on the given /api/v1 group.
func Register(v1 *echo.Group) {
	tr := v1.Group("/translations")
	tr.GET("", handleListLocales)                    // list locales
	tr.GET("/:locale", handleGetBundle)              // fetch bundle
	tr.POST("/:locale/missing", handleReportMissing) // IDE missing-key telemetry
}
