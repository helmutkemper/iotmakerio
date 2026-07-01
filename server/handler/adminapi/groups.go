// server/handler/adminapi/groups.go — Admin CRUD for user groups.
//
// Routes (all under /admin, with RequireAdmin middleware):
//
//	GET    /groups                        — list all groups
//	POST   /groups                        — create a group
//	PUT    /groups/:id                    — update name / description
//	DELETE /groups/:id                    — delete group (cascades memberships)
//	GET    /groups/:id/members            — list members with source and date
//	POST   /groups/:id/members            — add a user manually (source="admin")
//	DELETE /groups/:id/members/:user_id   — remove a user from the group
package adminapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	cryptoauth "server/auth"
	"server/store"
)

// RegisterGroups wires all group admin routes onto the given group.
func RegisterGroups(g *echo.Group) {
	g.GET("/groups", handleListGroups)
	g.POST("/groups", handleCreateGroup)
	g.PUT("/groups/:id", handleUpdateGroup)
	g.DELETE("/groups/:id", handleDeleteGroup)
	g.GET("/groups/:id/members", handleListGroupMembers)
	g.POST("/groups/:id/members", handleAddGroupMember)
	g.DELETE("/groups/:id/members/:user_id", handleRemoveGroupMember)
}

func handleListGroups(c echo.Context) error {
	groups, err := store.ListGroups()
	if err != nil {
		return serverErr(c, "listGroups", err)
	}
	return ok(c, map[string]any{"groups": groups})
}

func handleCreateGroup(c echo.Context) error {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name == "" {
		return badRequest(c, "name is required")
	}

	g := &store.UserGroup{
		ID:          cryptoauth.MustNewID(),
		Name:        req.Name,
		Description: req.Description,
	}
	if err := store.CreateGroup(g); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "group name already exists")
		}
		return serverErr(c, "createGroup", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{"group": g}))
}

func handleUpdateGroup(c echo.Context) error {
	existing, err := store.GetGroup(c.Param("id"))
	if err == store.ErrNotFound {
		return notFound(c)
	}
	if err != nil {
		return serverErr(c, "getGroup", err)
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	existing.Description = req.Description

	if err := store.UpdateGroup(existing); err != nil {
		return serverErr(c, "updateGroup", err)
	}
	return ok(c, map[string]any{"group": existing})
}

func handleDeleteGroup(c echo.Context) error {
	if err := store.DeleteGroup(c.Param("id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "deleteGroup", err)
	}
	return ok(c, map[string]any{"deleted": true})
}

func handleListGroupMembers(c echo.Context) error {
	members, err := store.ListGroupMembers(c.Param("id"))
	if err != nil {
		return serverErr(c, "listGroupMembers", err)
	}
	return ok(c, map[string]any{"members": members})
}

func handleAddGroupMember(c echo.Context) error {
	groupID := c.Param("id")

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.UserID == "" {
		return badRequest(c, "user_id is required")
	}

	// Verify the user exists before adding them.
	if _, err := store.GetUserByID(req.UserID); err == store.ErrNotFound {
		return badRequest(c, "user not found")
	} else if err != nil {
		return serverErr(c, "getUser", err)
	}

	if err := store.AddGroupMember(req.UserID, groupID, "admin"); err != nil {
		if err == store.ErrConflict {
			return conflict(c, "user is already a member of this group")
		}
		return serverErr(c, "addGroupMember", err)
	}
	return c.JSON(http.StatusCreated, envelope(map[string]any{
		"user_id":  req.UserID,
		"group_id": groupID,
		"source":   "admin",
	}))
}

func handleRemoveGroupMember(c echo.Context) error {
	if err := store.RemoveGroupMember(c.Param("user_id"), c.Param("id")); err == store.ErrNotFound {
		return notFound(c)
	} else if err != nil {
		return serverErr(c, "removeGroupMember", err)
	}
	return ok(c, map[string]any{"deleted": true})
}
