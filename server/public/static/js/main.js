// /ide/server/public/static/js/main.js
// main.js — application entry point.
//
// Responsibilities:
//   1. Import every page module.
//   2. Expose all functions used by inline onclick / on* handlers in dynamically
//      generated HTML to the global window object.
//   3. Boot the application (load translations, check auth, route to start page).
//
// Why window assignment?
//   HTML templates are built as template-literal strings inside render functions.
//   Modules are strict — inline handlers like onclick="projParse()" resolve
//   against window, not the module scope. Every function referenced in those
//   strings must be on window before any page renders.
//
// Translation editing is NOT handled by this SPA anymore — it lives in the
// /control admin panel. The i18n API calls here are limited to LOADING the
// current user's locale bundle for rendering the UI.

import { S }                                from './state.js';
import { loadTranslations, checkAuth,
    loadAvailableLocales,
    switchLocale as _switchLocale }          from './api.js';
import { t, toast }                         from './utils.js';
import { renderSB, toggleSB, toggleBlogGrp } from './sidebar.js';
import { nav, render }                      from './router.js';

import { doLogin, do2FA, doReg,
    doVerify, doForgot, doReset,
    onInviteInput }                         from './pages/auth.js';
import { doEdit, delPost }                  from './pages/admin.js';
import { savePost, onFileIn, onDrop,
    rmImg, syncPrev }                       from './pages/editor.js';

// Feed — community marketplace
import {
    renderFeed,
    feedSetTab,
    feedFilterChange,
    feedSearchInput,
    feedClearFilters,
    feedRate,
    feedClearRating,
    feedToggleFollow,
    // comments — toggling panel, posting, deleting, sub-rating stars
    feedToggleComments,
    feedLoadMoreComments,
    feedSubmitComment,
    feedDeleteComment,
    feedSubRatingHover,
    feedSubRatingOut,
    feedSubRatingPick,
    // reports — open dialog, submit, close
    feedOpenReport,
    feedCloseReport,
    feedSubmitReport,
}                                           from './pages/feed.js';

// Following — network management
import {
    renderFollowing,
    followingSetTab,
    followingUnfollow,
}                                           from './pages/following.js';

// Profile page — own profile + public profile
import {
    renderProfile,
    saveProfile,
    triggerAvatarUpload,
    onAvatarSelected,
    generateInvite,
    copyInviteLink,
    connectGithub,
}                                           from './pages/profile.js';

// Projects — tree view + Monaco editor + parse/analyze/diff/versioning
import {
    renderProjects,
    leaveProjects,
    // tabs
    switchTab,
    // tree
    toggleLangNode,
    toggleProjectNode,
    openCreateProjectModal,
    closeCreateProjectModal,
    clearCreateError,
    onVisChange,
    onTypeChange,
    submitCreateProject,
    // wizard-create flow (dropdown entry "Create with wizard")
    projToggleNewMenu,
    openWizardCreateModal,
    openSchoolPage,
    closeWizardCreateModal,
    submitWizardCreate,
    confirmDeleteProject,
    onDeleteConfirmInput,
    executeDeleteProject,
    triggerFileUpload,
    onFileSelected,
    deleteProjectFile,
    promptRenameCode,
    copyImageMarkdown,
    // project properties modal (gear icon on each project row)
    openPropertiesModal,
    closePropertiesModal,
    clearPropertiesError,
    onPropertiesVisChange,
    submitPropertiesModal,
    // publish flags — shared event handlers for create and properties modals
    onPublishFlagChange,
    onReadyToUseChange,
    confirmReadyToUse,
    cancelReadyToUse,
    // editor — Go code
    openCodeEditor,
    projCloseEditor,
    projParse,
    projSave,
    projRenameActiveTab,
    projSaveBackup,
    projUpdateParseBtnState,
    projOpenFileManager,
    projOpenExportFlow,
    projReset,
    projSetTab,
    projVersionChange,
    projDiffVersions,
    projDiffApplyToEditor,
    projDiffChoose,
    projToggleLiveAnalysis,
    projCreateFile,
    projSlashPick,
    // editor — Markdown docs
    projOpenMarkdownEditor,
    projCreateMarkdownFile,
    projMdSetTab,
    projMarkdownSave,
    projMarkdownClose,
    projMdSlashPick,
    // preview interactions
    projPinStartRename,
    projPinFinishRename,
    projRemoveFlag,
    projDragStart,
    projDragOver,
    projDragLeave,
    projDrop,
    projDragEnd,
    // unified device+template list
    toggleUnifiedRow,
    openUnifiedPropertiesModal,
    confirmDeleteUnified,
    onCategoryChange,
}                                           from './pages/projects.js';

// Help files modal (file manager: markdown / images / examples)
// Loaded eagerly so the toolbar Files button can call window.openHelpFiles
// without an async chunk load on the first click.
import {
    openHelpFiles,
    closeHelpFiles,
}                                           from './pages/help_files.js';

// Github-package export flow (slice 8). Eagerly imported so the toolbar
// button's onclick handler — which goes via window.projOpenExportFlow,
// which delegates to window.projExportRun — has the runtime ready on
// the first click. Same lazy-CSS / eager-JS pattern as help_files.
import {
    projExportRun,
}                                           from './pages/project_export.js';

// ─── Locale switch (window-facing wrappers) ───────────────────────────────────
//
// switchLocale from api.js handles state + localStorage + translations + DB.
// These wrappers add the UI re-render that api.js cannot do (circular dep).
// Exposed on window so inline onclick handlers in sidebar and profile can call them.

// onLocaleSwitch — full switch: sidebar + current page re-render.
// Used by the sidebar locale buttons (the user is not editing a form there).
async function onLocaleSwitch(newLocale) {
    await _switchLocale(newLocale);
    // Re-render the sidebar (updates active locale indicator) and the current
    // page (re-applies translated strings).
    renderSB();
    render();
    // Brief visual feedback so the user knows the switch took effect.
    const matched = S.locales.find(l => l.code === newLocale);
    const label = matched ? matched.display : newLocale;
    toast('success', `${label} ✓`);
}

// onLocaleSwitchQuiet — quiet switch: sidebar only, no page re-render.
// Used by the profile page locale <select> to avoid discarding unsaved form
// edits and flashing a loading state while the profile is re-fetched.
async function onLocaleSwitchQuiet(newLocale) {
    await _switchLocale(newLocale);
    renderSB();
    const matched = S.locales.find(l => l.code === newLocale);
    const label = matched ? matched.display : newLocale;
    toast('success', `${label} ✓`);
}

// ── Expose globals for inline onclick handlers in dynamically generated HTML ──
Object.assign(window, {
    nav, toggleSB, toggleBlogGrp, S,

    // locale switch — used by sidebar locale buttons and profile page select
    switchLocale: onLocaleSwitch,
    switchLocaleQuiet: onLocaleSwitchQuiet,

    // auth
    doLogin, do2FA, doReg, doVerify, doForgot, doReset,
    onInviteInput,

    // admin blog
    doEdit, delPost,

    // post editor
    savePost, onFileIn, onDrop, rmImg, syncPrev,

    // feed — tabs, filters, ratings, follow
    feedSetTab, feedFilterChange, feedSearchInput,
    feedClearFilters, feedRate, feedClearRating, feedToggleFollow,

    // feed — comments (panel toggle, post, delete, sub-rating stars)
    feedToggleComments, feedLoadMoreComments, feedSubmitComment,
    feedDeleteComment, feedSubRatingHover, feedSubRatingOut, feedSubRatingPick,

    // feed — reports (open dialog, submit, close)
    feedOpenReport, feedCloseReport, feedSubmitReport,

    // following
    followingSetTab, followingUnfollow,

    // profile
    saveProfile, triggerAvatarUpload, onAvatarSelected,
    generateInvite, copyInviteLink, connectGithub,

    // projects — tree
    toggleLangNode, toggleProjectNode,
    openCreateProjectModal, closeCreateProjectModal,
    clearCreateError, onVisChange, onTypeChange, submitCreateProject,
    // projects — wizard-create flow
    projToggleNewMenu, openWizardCreateModal, openSchoolPage,
    closeWizardCreateModal, submitWizardCreate,
    confirmDeleteProject, onDeleteConfirmInput, executeDeleteProject,
    triggerFileUpload, onFileSelected, deleteProjectFile,
    promptRenameCode, copyImageMarkdown,

    // projects — tabs (Projects | Templates)
    switchTab,

    // projects — unified device+template list
    toggleUnifiedRow,
    openUnifiedPropertiesModal,
    confirmDeleteUnified,
    onCategoryChange,

    // projects — properties modal
    openPropertiesModal, closePropertiesModal, clearPropertiesError,
    onPropertiesVisChange, submitPropertiesModal,

    // projects — publish flags (shared by create and properties modals)
    onPublishFlagChange, onReadyToUseChange, confirmReadyToUse, cancelReadyToUse,

    // projects — Go code editor
    openCodeEditor, projCloseEditor,
    projParse, projSave, projRenameActiveTab, projSaveBackup, projUpdateParseBtnState,
    projOpenFileManager,
    projOpenExportFlow, projExportRun,
    projReset, projSetTab,
    projVersionChange, projDiffVersions,
    projDiffApplyToEditor, projDiffChoose,
    projToggleLiveAnalysis, projCreateFile,
    projSlashPick,

    // projects — Markdown editor
    projOpenMarkdownEditor, projCreateMarkdownFile,
    projMdSetTab, projMarkdownSave, projMarkdownClose,
    projMdSlashPick,

    // projects — preview interactions
    projPinStartRename, projPinFinishRename,
    projRemoveFlag,
    projDragStart, projDragOver, projDragLeave, projDrop, projDragEnd,

    // projects — File manager modal (help files: markdown, images, examples)
    openHelpFiles, closeHelpFiles,

    // projects — Github-package export flow (modals + download)
    projExportRun,
});

// ── Boot ──────────────────────────────────────────────────────────────────────

async function boot() {
    // Load available locales and translations in parallel for faster boot.
    await Promise.all([loadTranslations(), loadAvailableLocales()]);

    // Validate token and populate S.user.
    // checkAuth() also syncs S.locale from the DB preference when they differ,
    // which triggers a second loadTranslations() call if needed.
    await checkAuth();

    const title = t('site.title', 'IoTMaker');
    document.title = title;
    const h = document.getElementById('site-title');
    if (h) h.textContent = title;

    // Set <html lang="..."> to the resolved locale for accessibility.
    document.documentElement.lang = S.locale;

    renderSB();

    // ── Parse hash: separate page name from query parameters ──────────────
    //
    // The URL hash can carry query parameters in two cases:
    //   1. GitHub OAuth callback redirect: #profile?github=connected
    //   2. Registration invite:            #register?invite=abc123
    //
    // Previous code compared the raw hash (including params) against the
    // authPages list, which always failed for hashes with query params.
    // Now we split cleanly: hashPage is the route key, hashParams carries
    // any extra data.
    const rawHash = location.hash.replace(/^#/, '');
    const qIndex  = rawHash.indexOf('?');
    const hashPage   = qIndex >= 0 ? rawHash.substring(0, qIndex) : rawHash;
    const hashParams = new URLSearchParams(qIndex >= 0 ? rawHash.substring(qIndex + 1) : '');

    // ── Detect GitHub OAuth result ────────────────────────────────────────
    //
    // When the OAuth callback redirects to /app#profile?github=connected (or
    // ?github=error&reason=...), we capture the result into S._githubResult,
    // clean the hash immediately, and force-navigate to the profile page.
    //
    // This prevents:
    //   - The stale-hash bug where the success alert showed on later visits
    //   - The wrong-page bug where boot() routed to 'projects' because
    //     'profile?github=connected' didn't match the authPages list
    if (hashParams.has('github')) {
        const ghValue = hashParams.get('github');
        if (ghValue === 'connected') {
            S._githubResult = 'connected';
        } else {
            S._githubResult = {
                error:  true,
                reason: hashParams.get('reason') || 'unknown',
            };
        }
        // Clean the URL so the result cannot fire again on refresh/back.
        history.replaceState(null, '', window.location.pathname + '#profile');
        nav('profile');
        return;
    }

    // ── Standard routing ──────────────────────────────────────────────────
    const hash = hashPage || '';

    // Protected pages require authentication.
    // NOTE: 'i18n' was removed when the translation editor moved to /control.
    //       Legacy deep links are handled by router.js (redirects to 'home').
    const authPages = ['admin', 'editor', 'ide', 'projects', 'profile', 'following'];
    // Public pages accessible without login.
    const pubPages  = ['login', 'register', 'feed'];

    const startPage = S.user
        ? (authPages.includes(hash) ? hash : 'projects')
        : (pubPages.includes(hash)  ? hash : 'home');

    nav(startPage);
}

boot();
