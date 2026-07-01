// /ide/server/public/static/js/state.js
// state.js — single source of truth for the SPA.
//
// Every piece of mutable application state lives here as a flat property on S.
// Modules import S and read/write properties directly; there is no pub/sub
// layer — simplicity is preferred over indirection at this stage.
//
// Naming conventions:
//   _prefixed   — internal/transient flags consumed once and cleared.
//   camelCase   — persistent session state or API data.
export const S = {
    // ── Auth ──────────────────────────────────────────────────────────────────
    user:         null,                                      // PublicUser after login; null when unauthenticated
    token:        localStorage.getItem('token'),             // JWT bearer token (persisted in localStorage)
    pendingUID:   null,                                      // user ID during 2FA / verify-email flow
    _resetEmail:  null,                                      // email address during password reset flow

    // ── Locale / i18n ─────────────────────────────────────────────────────────
    locale:       localStorage.getItem('locale') || navigator.language || 'pt-BR',
    locales:      [],                                        // available UI locales [{code, display}, ...] — loaded at boot
    tr:           {},                                        // translation map { messageId: text }

    // ── Navigation ────────────────────────────────────────────────────────────
    page:         'home',                                    // current page key (matches PAGE_MAP in router.js)
    currentPost:  null,                                      // blog post open for reading
    editPost:     null,                                      // blog post open in editor

    // ── Blog ──────────────────────────────────────────────────────────────────
    posts:        [],
    postsOff:     0,
    postsMore:    true,
    postsLoading: false,

    // ── Profile ───────────────────────────────────────────────────────────────
    // profile: loaded by checkAuth() so the sidebar avatar is available immediately.
    profile:      null,
    // profileCtx: context passed to the profile page renderer.
    //   null                    → own profile
    //   { username: 'alice' }   → public profile
    profileCtx:   null,

    // ── GitHub OAuth result ───────────────────────────────────────────────────
    // Transient flag set by boot() when the GitHub OAuth callback redirects
    // back to the SPA (hash contains ?github=connected or ?github=error).
    // Consumed once by renderOwnProfile(), then set back to null.
    //
    // Possible values:
    //   null                                — no pending result
    //   'connected'                         — OAuth completed successfully
    //   { error: true, reason: '...' }      — OAuth failed
    _githubResult: null,
};
