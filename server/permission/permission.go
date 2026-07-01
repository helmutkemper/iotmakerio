// server/permission/permission.go — Role-based access control (RBAC) for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Design: Opção B — permissions are hardcoded per role, verified locally without
// a database query. Each endpoint declares exactly which permission it requires.
// The interface is designed to migrate to database-backed permissions in the
// future without changing any handler code — only this file changes.
//
// How to add a new permission:
//  1. Declare a new Permission constant below.
//  2. Add it to the relevant role(s) in RolePermissions.
//  3. Use RequirePermission(PermYourNew) in the handler route registration.
//
// How to add a new role:
//  1. Add the role constant to store/models.go (e.g. RoleOfficialSpecialist).
//  2. Add an entry to RolePermissions here with the appropriate permissions.
//
// Português:
//
//	Controle de acesso baseado em permissões. Cada endpoint declara a permissão
//	necessária. A verificação é local — sem query ao banco — para máxima velocidade
//	e simplicidade. A interface permite migrar para permissões por banco no futuro.
package permission

import "server/store"

// Permission is a named capability required to access a specific action.
// Format: "<resource>.<action>" — always lowercase, always a constant.
type Permission string

// ─── Permission constants ─────────────────────────────────────────────────────
//
// Resource groups mirror the control panel sections.
// Add new permissions here before using them in handlers.

const (
	// ── Users ────────────────────────────────────────────────────────────────

	// PermUsersView allows listing and reading user accounts.
	PermUsersView Permission = "users.view"

	// PermUsersEditRole allows changing a user's role.
	PermUsersEditRole Permission = "users.edit_role"

	// PermUsersBan allows banning or unbanning a user account.
	PermUsersBan Permission = "users.ban"

	// ── Comments ─────────────────────────────────────────────────────────────

	// PermCommentsView allows reading comments in the control panel.
	PermCommentsView Permission = "comments.view"

	// PermCommentsDelete allows hard-deleting comments.
	PermCommentsDelete Permission = "comments.delete"

	// ── Templates ────────────────────────────────────────────────────────────

	// PermTemplatesView allows listing all templates including private ones.
	PermTemplatesView Permission = "templates.view"

	// PermTemplatesBlock allows blocking or unblocking a template.
	// A blocked template returns 403 on all endpoints and is hidden from menus.
	PermTemplatesBlock Permission = "templates.block"

	// PermTemplatesPromote allows adding an official_specialist's template
	// to a menu section visible to users or groups.
	PermTemplatesPromote Permission = "templates.promote"

	// ── Black-boxes ──────────────────────────────────────────────────────────

	// PermDevicesView allows listing all black-box devices.
	PermDevicesView Permission = "devices.view"

	// PermDevicesBlock allows blocking or unblocking a device.
	PermDevicesBlock Permission = "devices.block"

	// PermDevicesPromote allows promoting a device to a menu section.
	PermDevicesPromote Permission = "devices.promote"

	// ── Menu sections ────────────────────────────────────────────────────────

	// PermMenuView allows reading menu section configuration.
	PermMenuView Permission = "menu.view"

	// PermMenuEdit allows creating, editing, and reordering menu sections.
	PermMenuEdit Permission = "menu.edit"

	// PermMenuDelete allows deleting a menu section.
	PermMenuDelete Permission = "menu.delete"

	// ── Reports ──────────────────────────────────────────────────────────────

	// PermReportsView allows reading user-submitted reports.
	PermReportsView Permission = "reports.view"

	// PermReportsResolve allows marking a report as resolved or dismissed.
	PermReportsResolve Permission = "reports.resolve"

	// ── Settings ─────────────────────────────────────────────────────────────

	// PermSettingsView allows reading portal-wide settings.
	PermSettingsView Permission = "settings.view"

	// PermSettingsEdit allows changing portal-wide settings.
	PermSettingsEdit Permission = "settings.edit"

	// ── Invite codes ─────────────────────────────────────────────────────────

	// PermInvitesView allows listing invite codes.
	PermInvitesView Permission = "invites.view"

	// PermInvitesCreate allows generating new invite codes.
	PermInvitesCreate Permission = "invites.create"

	// PermInvitesRevoke allows revoking an unused invite code.
	PermInvitesRevoke Permission = "invites.revoke"

	// ── Translations (i18n admin) ────────────────────────────────────────────

	// PermTranslationsEdit allows saving a translation bundle for one locale
	// via the control panel. Read endpoints (GET /api/v1/translations and
	// POST /api/v1/translations/:locale/missing) remain public — the portal
	// and the IDE WASM client legitimately need to read translations without
	// authentication. Only the write path is gated, and additionally guarded
	// by OTP confirmation on every bundle save (see
	// server/handler/controlapi/translations.go).
	PermTranslationsEdit Permission = "translations.edit"
)

// ─── Role → Permission mapping ────────────────────────────────────────────────

// RolePermissions maps each role to the set of permissions it holds.
// A role without an entry has no permissions.
// To grant a permission to a role, add it to the slice below and redeploy.
var RolePermissions = map[string][]Permission{

	store.RoleAdmin: {
		// Admins have every permission.
		PermUsersView,
		PermUsersEditRole,
		PermUsersBan,
		PermCommentsView,
		PermCommentsDelete,
		PermTemplatesView,
		PermTemplatesBlock,
		PermTemplatesPromote,
		PermDevicesView,
		PermDevicesBlock,
		PermDevicesPromote,
		PermMenuView,
		PermMenuEdit,
		PermMenuDelete,
		PermReportsView,
		PermReportsResolve,
		PermSettingsView,
		PermSettingsEdit,
		PermInvitesView,
		PermInvitesCreate,
		PermInvitesRevoke,
		PermTranslationsEdit,
	},

	store.RoleOfficialSpecialist: {
		// Official specialists can view their own promoted items and reports,
		// but cannot modify the portal configuration or manage other users.
		PermTemplatesView,
		PermDevicesView,
		PermReportsView,
	},

	store.RoleUser: {
		// Regular users have no control panel permissions.
	},
}

// ─── Verification ─────────────────────────────────────────────────────────────

// Has reports whether the given role holds the requested permission.
// Returns false for unknown roles or empty role strings.
//
// This is the single function all middleware and handlers call.
// Its signature is stable — swapping to database-backed permissions only
// requires reimplementing this function.
func Has(role string, p Permission) bool {
	perms, ok := RolePermissions[role]
	if !ok {
		return false
	}
	for _, perm := range perms {
		if perm == p {
			return true
		}
	}
	return false
}

// HasAny reports whether the role holds at least one of the given permissions.
// Useful for endpoints accessible to multiple roles for different reasons.
func HasAny(role string, permissions ...Permission) bool {
	for _, p := range permissions {
		if Has(role, p) {
			return true
		}
	}
	return false
}

// HasAll reports whether the role holds every one of the given permissions.
func HasAll(role string, permissions ...Permission) bool {
	for _, p := range permissions {
		if !Has(role, p) {
			return false
		}
	}
	return true
}
