package blackboxapi

// version.go — Version deduplication helper.
//
// The legacy deduplicateLatestByVersion function operated on store.BlackBox
// rows from the blackboxes table. Now that the IDE reads from
// project_code_versions (one MAX(version) row per project via SQL), there is
// nothing left to deduplicate.
//
// This file is kept as a placeholder so git history remains meaningful.
// The deduplicateLatestByVersion stub in handler.go is a no-op.
