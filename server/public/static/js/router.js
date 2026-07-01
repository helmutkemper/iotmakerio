// server/public/static/js/router.js — page navigation and rendering dispatcher.
//
// nav(page, ctx) is the single entry point for all page transitions.
// It guards protected pages, cleans up stateful pages (IDE), updates S.page,
// and delegates rendering to the page-specific render function.
//
// The profile page has two modes driven by the ctx argument:
//   nav('profile')                      → own profile (requires auth)
//   nav('profile', { username: 'alice' }) → public profile (no auth required)
//
// Template management is part of the Projects page (two-tab layout).
// There is no longer a separate 'templates' or 'blackbox' route.
//
// Translation editing lives in the /control SPA (admin-only, OTP-gated).
// The previous in-app 'i18n' route was removed — the public i18n endpoints
// continue to serve bundles for UI rendering, but editing no longer happens
// from /app.
import { S }                                from './state.js';
import { renderSB, BLOG_PAGES }             from './sidebar.js';

import { renderHome }                                  from './pages/home.js';
import { renderPost }                                  from './pages/post.js';
import { renderLogin, renderReg, render2FA,
    renderVerify, renderForgot, renderReset }          from './pages/auth.js';
import { renderAdmin }                                 from './pages/admin.js';
import { renderEditor }                                from './pages/editor.js';
import { renderIDE, leaveIDE }                         from './pages/ide.js';
import { renderProjects, leaveProjects }               from './pages/projects.js';
import { renderProfile }                               from './pages/profile.js';
import { renderEditorSettings }                        from './pages/editorSettings.js';
import { renderFeed }                                  from './pages/feed.js';
import { renderFollowing }                             from './pages/following.js';
import { api }                                         from './api.js';

// PAGE_MAP maps route keys to render functions.
// render functions receive (root: HTMLElement, ctx?: any).
const PAGE_MAP = {
    home:      renderHome,
    post:      renderPost,
    login:     renderLogin,
    register:  renderReg,
    verify:    renderVerify,
    '2fa':     render2FA,
    forgot:    renderForgot,
    reset:     renderReset,
    admin:     renderAdmin,
    editor:    renderEditor,
    ide:       renderIDE,
    projects:  renderProjects,   // Project Management + Template Management (tabbed)
    profile:   renderProfile,    // User profile (own or public)
    editorSettings: renderEditorSettings, // Editor settings (language, menu prefs)
    feed:      renderFeed,       // Community marketplace feed
    following: renderFollowing,  // Following / followers management
};

// Protected pages that require a valid session.
const PROTECTED_PAGES = ['projects', 'ide', 'admin', 'editor', 'following', 'editorSettings'];

// Own profile requires auth; public profiles do not.
const OWN_PROFILE_PAGES = ['profile'];

/**
 * Navigate to a page.
 *
 * @param {string} page   - route key from PAGE_MAP
 * @param {*}      ctx    - optional context
 */
export function nav(page, ctx) {
    if (page === 'logout') { doLogout(); return; }

    // Redirect 'templates' to 'projects' — templates now live in the Projects
    // page under the Templates tab. Deep links and bookmarks are preserved.
    if (page === 'templates') { page = 'projects'; }

    // Guard protected pages — redirect to login if not authenticated.
    if (PROTECTED_PAGES.includes(page) && !S.user) {
        page = 'login';
    }

    // Own profile also requires auth; public profiles (ctx.username) do not.
    if (OWN_PROFILE_PAGES.includes(page) && !ctx?.username && !S.user) {
        page = 'login';
    }

    // Clean up IDE when navigating away.
    if (S.page === 'ide' && page !== 'ide') leaveIDE();

    // Clean up Projects page (polls + editor) when navigating away.
    if (S.page === 'projects' && page !== 'projects') leaveProjects();

    // Remove the feed's scroll listener when leaving the feed page.
    if (S.page === 'feed' && page !== 'feed') {
        window.removeEventListener('scroll', window._feedScrollHandler);
    }

    S.page = page;

    if (ctx !== undefined) {
        if (page === 'post')    S.currentPost = ctx;
        if (page === 'editor')  S.editPost    = ctx;
        if (page === 'profile') S.profileCtx  = ctx;
    } else if (page === 'profile') {
        S.profileCtx = null;
    }

    renderSB();
    render();
    window.scrollTo(0, 0);
}

export function render() {
    const root    = document.getElementById('app-root');
    const handler = PAGE_MAP[S.page] || renderHome;

    if (S.page === 'profile') {
        handler(root, S.profileCtx);
        return;
    }

    handler(root);
}

async function doLogout() {
    // Call server to invalidate the session first. Even if this fails
    // (network error), we still clear local state below — the user's
    // intent is to log out.
    try { await api('POST', '/api/auth/logout'); } catch (e) {}

    // Clear all SPA runtime state referencing the user.
    S.token      = null;
    S.user       = null;
    S.profileCtx = null;
    S.menuProfile  = null;

    // Clear the IDE WASM auth token. The IDE may be loaded in an iframe
    // from a previous session — the token lives in the iframe's window,
    // and the WASM reads it via rulesServer.GetAuthToken(). If we do not
    // clear it, the next user logging in on the same browser window could
    // inherit the previous user's access until the iframe reloads.
    try {
        if (window._ideAuthToken) delete window._ideAuthToken;
        const ideFrame = document.querySelector('iframe#ide-iframe, iframe[src*="/ide/"]');
        if (ideFrame && ideFrame.contentWindow) {
            try { delete ideFrame.contentWindow._ideAuthToken; } catch (e) {}
            try { delete ideFrame.contentWindow._ideProjectID; } catch (e) {}
        }
    } catch (e) {}

    // Clear persistent storage. We keep `locale` (UI language preference is
    // not sensitive) but wipe anything tied to the user session.
    localStorage.removeItem('token');

    S.page = 'home';
    renderSB();
    nav('home');
}
