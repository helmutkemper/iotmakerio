// server/handler/controlapi/users.go — Control panel user management endpoints.
//
// Routes (mounted on /api/control/v1 by RegisterControl):
//
//	GET  /api/control/v1/users            — paginated user list     (users.view)
//	POST /api/control/v1/users/role-otp   — request OTP code        (users.edit_role)
//	PUT  /api/control/v1/users/roles      — apply batch role changes (users.edit_role)
//
// Batch role change flow:
//  1. Admin selects new roles for one or more users.
//  2. POST /users/role-otp  → server generates OTP, emails to the admin.
//  3. Admin enters code in the confirmation modal.
//  4. PUT  /users/roles     → server validates OTP once, applies all changes,
//     then consumes the OTP. All changes or none.
//
// The OTP is tied to the admin's own user ID (not the targets) because it
// confirms the admin's identity, not the targets'.
package controlapi

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/email"
	"server/permission"
	"server/store"
)

// defaultPageSize is the number of users returned per page when limit is omitted.
const defaultPageSize = 20

// maxPageSize is the hard cap on the limit param to prevent oversized queries.
const maxPageSize = 100

// RegisterControl mounts all control panel API routes on the given group.
func RegisterControl(g *echo.Group) {
	g.GET("/users",
		handleListUsers,
		RequireControlToken(permission.PermUsersView),
	)
	g.POST("/users/role-otp",
		handleRequestRoleOTP,
		RequireControlToken(permission.PermUsersEditRole),
	)
	g.PUT("/users/roles",
		handleChangeRoles,
		RequireControlToken(permission.PermUsersEditRole),
	)
}

// handleListUsers returns a paginated, optionally filtered list of users.
func handleListUsers(c echo.Context) error {
	page := queryInt(c, "page", 1)
	limit := queryInt(c, "limit", defaultPageSize)
	q := c.QueryParam("q")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > maxPageSize {
		limit = defaultPageSize
	}

	result, err := store.ListUsers(page, limit, q)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not load users")
	}
	return ok(c, result)
}

// handleRequestRoleOTP generates a one-time code and emails it to the admin.
func handleRequestRoleOTP(c echo.Context) error {
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
		Purpose: store.OTPPurposeRoleChange,
	}); err != nil {
		return fail(c, http.StatusInternalServerError, "could not store code")
	}

	go email.RoleChangeCode(admin.Email, code)

	return ok(c, map[string]any{"message": "code sent to your registered email"})
}

// roleChange is one item in the batch request body.
type roleChange struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// handleChangeRoles validates the OTP once and applies all role changes in the
// batch. The OTP is consumed only after all validations pass — if any change is
// invalid, the entire request is rejected before the OTP is consumed.
func handleChangeRoles(c echo.Context) error {
	caller := ControlClaims(c)

	var body struct {
		Changes []roleChange `json:"changes"`
		OTPCode string       `json:"otp_code"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	if len(body.Changes) == 0 {
		return fail(c, http.StatusBadRequest, "no changes provided")
	}
	if body.OTPCode == "" {
		return fail(c, http.StatusBadRequest, "otp_code is required")
	}

	// Validate all changes before touching the database.
	for _, ch := range body.Changes {
		if ch.UserID == caller.UserID {
			return fail(c, http.StatusForbidden, "cannot change your own role")
		}
		switch ch.Role {
		case store.RoleUser, store.RoleOfficialSpecialist, store.RoleAdmin:
			// valid
		default:
			return fail(c, http.StatusBadRequest, "unknown role: "+ch.Role)
		}
	}

	// Validate and consume the OTP atomically.
	// Done after input validation so a malformed request cannot burn a valid code.
	if err := store.ConsumeOTP(caller.UserID, body.OTPCode, store.OTPPurposeRoleChange); err != nil {
		return fail(c, http.StatusUnauthorized, "invalid or expired confirmation code")
	}

	// Apply all changes. Errors here are logged but the OTP is already consumed —
	// the admin must request a new code if they want to retry.
	applied := make([]map[string]any, 0, len(body.Changes))
	for _, ch := range body.Changes {
		if err := store.SetUserRole(ch.UserID, ch.Role); err != nil {
			return fail(c, http.StatusInternalServerError, "could not update role for user "+ch.UserID)
		}
		applied = append(applied, map[string]any{"id": ch.UserID, "role": ch.Role})
	}

	return ok(c, map[string]any{"applied": applied})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newOTP() (code, id string, err error) {
	code, err = cryptoauth.NewOTPCode()
	if err != nil {
		return
	}
	id, err = cryptoauth.NewID()
	return
}

func queryInt(c echo.Context, name string, defaultVal int) int {
	s := c.QueryParam(name)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}
