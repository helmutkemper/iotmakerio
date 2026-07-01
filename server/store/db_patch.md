// /ide/server/store/db_patch.md
//
// HOW TO INTEGRATE db_menu_commerce_tables.go INTO db.go
// =======================================================
//
// In migrate() in db.go, after the community feature block and before
// `return nil`, add:
//
//     // ── Menu sections + Commerce feature ─────────────────────────────────────
//     for _, stmt := range menuCommerceMigrationStmts() {
//         if _, err := DB.Exec(stmt); err != nil {
//             return err
//         }
//     }
//
// No import needed — same package (store).
