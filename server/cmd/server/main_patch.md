// /ide/server/cmd/server/main.go — PATCH
//
// HOW TO REGISTER THE NEW ROUTES IN main.go
// ==========================================
//
// 1. Add the new imports (alongside the existing handler imports):
//
//     "server/handler/adminapi"
//     "server/handler/bomapi"
//     "server/handler/menuapi"
//     "server/handler/storeapi"
//     "server/middleware"
//
// 2. After the existing v1 route registrations, add:
//
//     // ── Public: menu sections (no auth required) ───────────────────────────
//     menuapi.Register(v1.Group("/menu"))
//     //   → GET /api/v1/menu/sections
//
//     // ── Public: BOM query ──────────────────────────────────────────────────
//     bomapi.Register(v1.Group(""))
//     //   → GET /api/v1/bom
//
//     // ── Public: store redirect (root path, no /api/v1 prefix) ─────────────
//     storeapi.Register(e)
//     //   → GET /store/redirect/:id
//
//     // ── Admin panel (requires auth + admin role) ───────────────────────────
//     adminGroup := e.Group("/admin",
//         middleware.RequireAuth(),
//         middleware.RequireAdmin(),
//     )
//     adminapi.RegisterSections(adminGroup)
//     adminapi.RegisterGroups(adminGroup)
//     adminapi.RegisterCommerce(adminGroup)
//
// 3. The /admin/i18n route that already exists in main.go does NOT need
//    the RequireAdmin middleware — it does its own JS auth check.
//    Keep the existing /admin/i18n route BEFORE the new adminGroup so
//    it is not shadowed.
