// ide/stageWorkspace/workspace_patch.md
//
// HOW TO INTEGRATE LoadSections INTO workspace.go
// ================================================
//
// In Init() in workspace.go, after the templateDefs block (around line 403),
// add the sections fetch:
//
//     // ── Dynamic menu sections (from database) ─────────────────────────────
//     sectionDefs := mainMenu.LoadSections()
//     if len(sectionDefs) > 0 {
//         menuBuilder.SetSections(sectionDefs)
//         log.Printf("[Workspace:%s] Registered %d menu section(s)",
//             w.Name, len(sectionDefs))
//     } else {
//         log.Printf("[Workspace:%s] No menu sections available", w.Name)
//     }
//
// This block must be BEFORE the menuBuilder.Build() call (line 417).
// No new import needed — mainMenu.LoadSections() is in the same package
// as the rest of the mainMenu calls already made in workspace.go.
