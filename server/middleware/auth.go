// server/middleware/auth.go — HTTP middleware for the IoTMaker server.
//
// Three middlewares are exported:
//
//	RequireAuth   — validates the JWT Bearer token and loads the User into
//	                the Echo context. Returns 401 if the token is missing or
//	                invalid. All authenticated routes must use this first.
//
//	OptionalAuth  — validates the JWT Bearer token if present and loads the
//	                User into the Echo context. Does NOT block anonymous
//	                requests — UserFromContext returns nil in that case.
//	                Used by endpoints that work for both anonymous and
//	                authenticated users (e.g., the menu tree endpoint).
//
//	RequireAdmin  — asserts that the authenticated user has role == "admin".
//	                Returns 403 otherwise. Always stack after RequireAuth.
//
// Usage in main.go:
//
//	api := e.Group("/api/v1", middleware.RequireAuth())
//	admin := e.Group("/admin", middleware.RequireAuth(), middleware.RequireAdmin())
//	public := e.Group("/api/v1/menu", middleware.OptionalAuth())
//
// The User is stored in the context under the key ContextKeyUser so that
// any handler can retrieve it without repeating the DB lookup:
//
//	user := middleware.UserFromContext(c)
//
// Design notes:
//
//   - The middleware intentionally re-fetches the User from the DB (not just
//     the JWT claims) so that deactivated accounts are rejected immediately
//     even if a valid token exists. The DB lookup is a single indexed query
//     and adds negligible latency at this scale.
//
//   - The JWT validation delegates to spaauth.RequireBearerToken() and
//     spaauth.BearerClaims() which are already used by all other protected
//     routes. This keeps auth logic in one place.
//
//   - OptionalAuth parses the JWT manually (not through RequireBearerToken
//     middleware) because RequireBearerToken returns 401 on missing tokens.
//     OptionalAuth extracts the token, validates it silently, and populates
//     the context only when the token is valid.
package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/config"
	"server/handler/spaauth"
	"server/store"
)

// contextKeyUser is the unexported Echo context key for the authenticated user.
// Use UserFromContext() to retrieve it — never access the key directly.
const contextKeyUser = "_iotmaker_user"

// RequireAuth returns an Echo middleware that validates the Authorization
// Bearer token and stores the User in the context.
//
// Responds with 401 when:
//   - The Authorization header is missing or malformed.
//   - The JWT signature is invalid or the token has expired.
//   - The user referenced in the token no longer exists in the database.
func RequireAuth() echo.MiddlewareFunc {
	// Delegate token validation to the existing spaauth middleware so that
	// JWT parsing lives in exactly one place across the entire codebase.
	bearerMiddleware := spaauth.RequireBearerToken()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return bearerMiddleware(func(c echo.Context) error {
			// spaauth.RequireBearerToken already validated the token and
			// stored the claims. Extract the user ID from the claims.
			claims := spaauth.BearerClaims(c)
			if claims == nil {
				// Should not happen if bearerMiddleware ran correctly,
				// but guard anyway.
				return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
			}

			// Re-fetch the full User from the database so we get the
			// current role and verified status, not just what was in the
			// token at the time of login.
			user, err := store.GetUserByID(claims.UserID)
			if err != nil {
				if err == store.ErrNotFound {
					return echo.NewHTTPError(http.StatusUnauthorized, "account not found")
				}
				c.Logger().Errorf("[auth] GetUserByID %s: %v", claims.UserID, err)
				return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
			}

			// Store the user in the context for downstream handlers.
			c.Set(contextKeyUser, user)
			return next(c)
		})
	}
}

// OptionalAuth returns an Echo middleware that validates the Authorization
// Bearer token if present and loads the User into the Echo context. If the
// token is missing, invalid, or expired, the request proceeds without a user
// (UserFromContext returns nil).
//
// This is used for endpoints that serve both anonymous and authenticated users:
//   - Anonymous: receives the default profile menu tree
//   - Authenticated: receives the user's assigned profile + personal prefs
//
// The middleware never returns 401 — it always calls next(c).
func OptionalAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract the Bearer token from the Authorization header.
			auth := c.Request().Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				// No token — proceed as anonymous.
				return next(c)
			}

			tokenStr := strings.TrimPrefix(auth, "Bearer ")
			if tokenStr == "" {
				return next(c)
			}

			// Validate the JWT. On failure, proceed as anonymous.
			cfg := config.Get()
			claims, err := cryptoauth.ParseJWT(tokenStr, cfg.JWTSecret)
			if err != nil {
				// Token invalid or expired — treat as anonymous.
				return next(c)
			}

			if claims.UserID == "" {
				return next(c)
			}

			// Fetch the full User from the database.
			user, err := store.GetUserByID(claims.UserID)
			if err != nil {
				// User not found or DB error — proceed as anonymous.
				c.Logger().Warnf("[auth/optional] GetUserByID %s: %v", claims.UserID, err)
				return next(c)
			}

			// Store user in context — UserFromContext will now return non-nil.
			c.Set(contextKeyUser, user)
			return next(c)
		}
	}
}

// RequireAdmin returns an Echo middleware that allows only users with
// role == "admin" to proceed. Must be stacked after RequireAuth().
//
// Responds with 403 when the authenticated user is not an admin.
func RequireAdmin() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := UserFromContext(c)
			if user == nil {
				// RequireAuth was not applied before RequireAdmin — misconfiguration.
				c.Logger().Error("[admin] RequireAdmin called without RequireAuth")
				return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
			}
			if user.Role != store.RoleAdmin {
				return echo.NewHTTPError(http.StatusForbidden, "admin access required")
			}
			return next(c)
		}
	}
}

// UserFromContext retrieves the authenticated User stored by RequireAuth
// or OptionalAuth. Returns nil when called outside an authenticated route
// or when the user is anonymous.
func UserFromContext(c echo.Context) *store.User {
	v := c.Get(contextKeyUser)
	if v == nil {
		return nil
	}
	u, _ := v.(*store.User)
	return u
}
