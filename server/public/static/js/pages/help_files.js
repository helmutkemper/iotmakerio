// server/public/static/js/pages/help_files.js — File Manager modal for
// project help files (markdown, images, SVG assets that travel with a
// published device).
//
// Backend: server/handler/projectapi/help_files.go
// Backend storage: server/store/project_help_files.go (SQLite blobs)
// Spec: docs/tasks/HELP_FILES_FEATURE.md
//
// Public entry point: window.openHelpFiles(projectId, opts)
// (registered by main.js).
//
// opts is reserved for future deep-link semantics from the wizard's
// Struct/Method modals (Slice 3): { kind: 'struct' } or
// { kind: 'method', name: 'Init' } will preselect the matching markdown
// file or pre-create it. v1 ignores opts entirely.
//
// Architectural notes:
//
//   - The modal is a position:fixed; inset:0 div with a high z-index.
//     It does NOT change the route — closing simply removes the div.
//     The Editor / Wizard tabs stay alive in the background; switching
//     back to the project picks up where the user left off.
//
//   - We mount a fresh Monaco instance for the right pane each time a
//     markdown file is selected. The instance is disposed when the user
//     selects another file or closes the modal — cheaper than trying to
//     swap models on a single shared instance, and avoids leaking the
//     model from the Editor's Go editor into the markdown surface.
//
//   - The file tree is a flat list with one optional level of indent
//     (for the "examples/" subdirectory). The path validator on the
//     server only accepts at most one '/', so a recursive tree
//     component would be overkill.
//
//   - Saving and renaming go through fetch() directly rather than the
//     shared api() helper, because PUT carries raw bytes and rename
//     POSTs JSON to a /rename suffix that doesn't fit the helper's
//     "JSON in / JSON out" assumption. This module owns its own
//     networking shape.
//
//   - All copy is currently English. When this lands in production,
//     wrap the user-visible strings in translate.T() using the
//     i18n bundle the rest of the wizard uses; doing it inline now
//     would scatter unverified translation keys across the file.

import { S } from '../state.js';
import { toast, showConfirm, showPrompt } from '../utils.js';

// =============================================================================
//  Module state — one open modal at a time, managed by these closures
// =============================================================================

// Per-open state, reset on every openHelpFiles() call so a fresh open
// after a previous close starts clean even if the user did not wait for
// any pending fetch to settle.
let _state = _emptyState();

function _emptyState() {
    return {
        projectId:    null,
        files:        [],         // [{ path, mimeType, sizeBytes, updatedAt }]
        quota:        null,       // { project: { used, limit }, user: { used, limit } }
        selectedPath: null,       // path of the file currently shown in the right pane
        monacoInst:   null,       // Monaco editor instance for the right pane (or null when an image is shown)
        dirty:        false,      // true when the Monaco buffer differs from disk
        savedContent: '',         // last content that came from the server, used for the dirty check
        rootEl:       null,       // the modal root <div>, kept so we can remove it on close
        // parsed is the BlackBoxDef from the wizard's last successful
        // /wizard/parse call. Passed in via openHelpFiles(opts.parsed).
        // The "Create new file" modal needs it to populate the name
        // dropdown with method names (Init, Run, Log, ...). Without
        // it, only the readme option is meaningful — and per the
        // contract, the alert "Please parse first" fires before the
        // modal ever renders.
        parsed:       null,
        // bcp47Tags is the BCP 47 tag list used by the language
        // datalist. Loaded from /static/data/bcp47-tags.json on
        // first need; cached for the lifetime of this state.
        bcp47Tags:    null,
        // previewMode is true when the right pane shows the rendered
        // markdown instead of the Monaco editor. The toggle is the
        // eye icon on the toolbar; clicking again returns to the
        // editor without losing the dirty buffer (Monaco's DOM is
        // hidden, not destroyed).
        previewMode:  false,
        // imageCache maps a project-relative path (e.g. "logo.png")
        // to a blob URL the preview can use as <img src>. Built
        // lazily as the preview encounters relative <img> tags;
        // every entry is revoked via URL.revokeObjectURL when the
        // file manager closes, so opening and closing the manager
        // many times does not leak browser memory.
        imageCache:   new Map(),
    };
}

// =============================================================================
//  Public entry point
// =============================================================================

/**
 * openHelpFiles(projectId, opts?) — entry point.
 *
 * Mounts the modal as a child of <body> with z-index 9999. Loads the
 * file list, renders the tree, and (when files exist) selects the
 * first one so the user lands on a usable editor instead of an empty
 * pane.
 *
 * The modal is single-instance — calling open while another modal is
 * open cleanly closes the previous one first.
 * opts (all fields optional) controls deep-link behaviour. Used by the
 * "Add help" buttons on the wizard's Struct and Method modals.
 *
 *   { kind: 'newStruct', parsed }
 *       Open the manager, then immediately raise the "Create new file"
 *       modal with name pre-selected to the device readme entry
 *       ("main menu text"). Order and language stay defaulted (next
 *       free position; "en"). Used by Struct modal → Add help.
 *
 *   { kind: 'newMethod', methodName: 'Init', parsed }
 *       Same as newStruct but pre-selects the method-specific entry
 *       ("Device <Name>"). methodName is the bare Go identifier
 *       (Init, Run, Log, …) and matches the basename produced by
 *       `_collectNameOptions`. Used by Method modal → Add help.
 *
 *   { kind: 'addImage' }
 *       Open the manager and immediately trigger the same file picker
 *       used by Action → Upload image. Reserved for the Method modal's
 *       "Add img" button (slice 7c).
 *
 *   undefined / {} / unknown kind
 *       Same as the toolbar button: open the manager, select the first
 *       file (or none). This is the path the toolbar button takes.
 *
 * `parsed` (when supplied) is the BlackBoxDef from the wizard's local
 * state — see _state.parsed in projects_wizard.js. It populates the
 * name dropdown of the create modal with the user's actual struct and
 * method names. Without it the dropdown only shows "main menu text"
 * (the readme entry, hard-coded in _collectNameOptions). The wizard
 * always passes parsed; passing it is a no-op for non-newStruct/Method
 * kinds.
 *
 * The userLang is derived from the SPA's `S.locale` (preferred locale
 * stored on the user, e.g. "pt-BR") and lower-cased to match the IDS
 * filename grammar (readme.pt-br.md, Init.en.md, …).
 */

// _ensureStylesheetLoaded injects /static/css/help_files.css into the
// document head exactly once per page lifetime. The id-based guard
// makes the call idempotent so multiple openHelpFiles() invocations
// don't pile up <link> tags.
//
// The stylesheet is intentionally NOT bundled with main.css — the help
// manager is opened on demand and most page loads never need it. Lazy
// injection trades one extra request (the first time the user opens
// the manager) for a smaller initial CSS payload.
//
// The id "proj-helpfiles-styles-link" matches the one used by
// projects.js' _injectHelpFilesStyles so both code paths share the
// same singleton link tag and never inject twice.
function _ensureStylesheetLoaded() {
    if (document.getElementById('proj-helpfiles-styles-link')) return;
    const link = document.createElement('link');
    link.id   = 'proj-helpfiles-styles-link';
    link.rel  = 'stylesheet';
    link.href = '/static/css/help_files.css';
    document.head.appendChild(link);
}

export async function openHelpFiles(projectId, opts) {
    if (!projectId) {
        // Defensive: every caller is supposed to pass a real id, but a
        // missing id used to crash the toolbar button. Be loud about it
        // rather than silently no-op'ing — silent no-ops on the toolbar
        // are how UI bugs go unnoticed for weeks.
        toast('error', 'No project context — open a project first.');
        return;
    }

    closeHelpFiles();         // dispose any leftover modal from a previous open
    _state = _emptyState();
    _state.projectId = projectId;
    // Stash the parsed BlackBoxDef when the caller has it. The
    // "Create new file" action consults this to populate the name
    // dropdown with the user's method names; it also gates whether
    // the action is allowed at all (per the agreed UX, no parse =>
    // "Please parse first" alert + close).
    _state.parsed = (opts && opts.parsed) ? opts.parsed : null;

    // Self-inject the stylesheet on first use so any caller can open
    // the manager without boilerplate. The toolbar Files button in
    // projects.js pre-injects the same link via _injectHelpFilesStyles
    // (kept for symmetry); both calls are idempotent thanks to the
    // id-based guard. The wizard's "Add help" buttons rely entirely
    // on this self-inject — without it the modal renders unstyled
    // (elements stack at the page edge with no overlay backdrop).
    _ensureStylesheetLoaded();

    _renderShell();
    // Modal is now mounted (_state.rootEl exists). Install the guards
    // that protect against losing an unsaved buffer to: closing the
    // tab / refreshing (browser-native confirm via beforeunload),
    // Back / Forward buttons (popstate), and any in-page link click
    // that would change the URL. See _installNavigationGuards.
    _installNavigationGuards();
    await _refreshList();     // populates _state.files and _state.quota
    _renderTree();

    // Deep-link routing. Resolved AFTER the list is loaded so we can
    // detect existing files vs needing to create them. Falls through
    // to "select first file" when no kind matches.
    const kind = opts && opts.kind;

    if (kind === 'newStruct') {
        // Open the create-file modal with name preselected to "readme"
        // (label: "main menu text"). The user still picks order and
        // language so they can decide whether this is the leading
        // readme tab or a follow-up (readme.1.<lang>.md, readme.2…).
        await _actionNewMarkdown({ prefillBasename: 'readme' });
        return;
    }

    if (kind === 'newMethod' && opts.methodName) {
        // Per IDS spec, method help filenames preserve the method's
        // case (Init.en.md, Run.en.md). _collectNameOptions builds the
        // dropdown items keyed by the same source-case basename, so
        // pre-selecting by raw methodName matches by string equality.
        const safe = String(opts.methodName).replace(/[^A-Za-z0-9_]/g, '');
        if (safe === '') {
            // Defensive: caller passed garbage. Toast and fall through
            // to default behaviour rather than open with broken state.
            toast('error', 'Invalid method name for help file.');
        } else {
            await _actionNewMarkdown({ prefillBasename: safe });
            return;
        }
    }

    if (kind === 'addImage') {
        // Trigger the same file picker the Action menu uses. The user
        // can still pick any other action from the menu afterward — the
        // manager stays open after the upload as a normal session.
        _actionUploadImage();
        return;
    }

    // Default: select the first file so the user sees something useful
    // instead of an empty pane on open.
    if (_state.files.length > 0) {
        await _selectFile(_state.files[0].path);
    }
}

// _openOrCreate selects the file at `path` if it already exists, or
// creates it empty and selects it otherwise. Earlier wizard "Add help"
// deep-links used this to land directly on `readme.<lang>.md` or
// `<method>.<lang>.md`; that flow was replaced by `kind: 'newStruct'`/
// `'newMethod'`, which open the "Create new file" modal instead so
// the user picks order and language explicitly.
//
// Kept available for future callers that want the direct landing — for
// example, an image-management deep-link that lands on a specific
// asset, or a "jump to this file" link from elsewhere in the SPA.
//
// All errors surface as toasts; the function does not throw.
async function _openOrCreate(path) {
    const exists = _state.files.some(f => f.path === path);
    if (!exists) {
        const r = await _saveFile(path, '');
        if (r.status !== 200) {
            const err = r.json?.metadata?.error || `HTTP ${r.status}`;
            toast('error', `Could not create ${path}: ${err}`);
            // Still try to recover gracefully — open whatever was
            // already there so the user is not stranded on a blank
            // pane after pressing a button that promised an editor.
            if (_state.files.length > 0) {
                await _selectFile(_state.files[0].path);
            }
            return;
        }
        await _refreshList();
        _renderTree();
    }
    await _selectFile(path);
}

/**
 * closeHelpFiles disposes the Monaco instance and removes the modal root.
 * Safe to call when nothing is open. Exported so pages that own a global
 * keyboard handler (Escape) can route through here.
 *
 * NOTE: this is the *forced* close. It does not check the dirty flag — it
 * tears down the modal unconditionally. For user-initiated dismissals
 * (X button, Escape, backdrop click) call `_attemptClose` instead, which
 * guards the unsaved buffer behind `_promptDirtyAction`.
 */
export function closeHelpFiles() {
    _uninstallNavigationGuards();
    if (_state.monacoInst) {
        try { _state.monacoInst.dispose(); } catch { /* ignore */ }
        _state.monacoInst = null;
    }
    // Free every blob URL the preview created during this session.
    // Each entry came from URL.createObjectURL; failing to revoke
    // would keep the underlying file in memory until the page
    // reloads. Cheap and simple.
    if (_state.imageCache && typeof _state.imageCache.forEach === 'function') {
        _state.imageCache.forEach(url => {
            try { URL.revokeObjectURL(url); } catch { /* ignore */ }
        });
        _state.imageCache.clear();
    }
    if (_state.rootEl && _state.rootEl.parentNode) {
        _state.rootEl.parentNode.removeChild(_state.rootEl);
    }
    _state.rootEl = null;
}

// _attemptClose is the user-facing close gateway. Every dismissal that
// originates from a gesture (Escape key, X button, click on the
// backdrop) routes through here so an unsaved markdown buffer cannot
// be lost in a moment of muscle memory.
//
// Behaviour:
//   - If the buffer is clean → close immediately.
//   - If the buffer is dirty → show _promptDirtyAction and act on the
//     three outcomes: 'save' persists then closes, 'discard' closes
//     without persisting, 'cancel' leaves the modal open.
//
// The function is fire-and-forget from the caller's perspective; we do
// not await it from the gesture handlers because the gesture event has
// already been delivered. Returning a Promise just lets tests assert
// completion.
//
// Português: porta de saída protegida. Toda dispensa do modal por
// gesto do usuário (Escape, botão X, clique fora) passa por aqui
// para que um buffer markdown não-salvo não seja descartado por
// engano. Reusa o _promptDirtyAction já existente — Save / Discard /
// Cancel — que é o mesmo modal usado quando o usuário troca de
// arquivo, garantindo um único vocabulário visual para "você tem
// alterações pendentes".
async function _attemptClose() {
    if (!_state.rootEl) return;          // already closed
    if (!_state.dirty) {
        closeHelpFiles();
        return;
    }
    const choice = await _promptDirtyAction(_state.selectedPath);
    if (choice === 'cancel') return;
    if (choice === 'save') {
        await _onSaveClick();
        if (_state.dirty) return;        // save failed — keep the modal open
    }
    // 'discard' or successful 'save' → close.
    closeHelpFiles();
}

// Navigation guards. Installed on open, uninstalled on close. They
// cover three escape routes a user might take while the buffer is
// dirty:
//
//   1. beforeunload — closing the tab, refreshing the page, navigating
//      to a different origin, quitting the browser. The browser
//      enforces the modal here; ours is unreachable. By spec, the
//      only thing we can do is set `event.returnValue` to a non-empty
//      string, which causes the browser's NATIVE confirmation
//      ("Reload site? Changes you made may not be saved."). The text
//      itself is fixed by the browser since Chrome 60+ (anti-phishing
//      mitigation) — we can't pass a custom message even though the
//      old API accepted one. This is the only departure from the
//      "no native dialogs" rule, and it's forced by the platform.
//
//   2. popstate — Back/Forward buttons, or any code that calls
//      history.back()/forward()/go(). We can intercept by checking
//      _state.dirty and re-pushing the state on cancel. Since the
//      browser already moved one step, we use pushState to undo the
//      effect. This is the standard idiom for "trap navigation in a
//      SPA".
//
//   3. internal link clicks — anchor tags, both same-page (#hash) and
//      cross-page (router targets). We capture the click in the
//      bubbling phase, inspect the target, and gate it through the
//      dirty prompt before letting the navigation proceed.
//
// Português: guardas de navegação. Instalados na abertura, removidos
// no fechamento. Cobrem três rotas de fuga: fechar/recarregar a
// aba (beforeunload — única que cai no aviso nativo do navegador,
// imposto por especificação), botões Voltar/Avançar (popstate) e
// cliques em links da SPA (capturados antes da navegação).
let _navGuardsState = null;

function _installNavigationGuards() {
    if (_navGuardsState) return;        // idempotent

    const onBeforeUnload = (e) => {
        if (!_state.dirty) return;
        // Both lines are needed for cross-browser compatibility.
        // - Chrome / modern Edge: preventDefault is enough.
        // - Firefox / older Safari: returnValue must also be set.
        // The string we pass is ignored by every browser since 2018;
        // it's left non-empty for legacy paths that still honour it.
        e.preventDefault();
        e.returnValue = '';
        return '';
    };

    const onPopState = async (e) => {
        if (!_state.rootEl || !_state.dirty) return;
        // The browser has already navigated by the time popstate
        // fires. To "cancel" we re-push the URL we wanted to keep.
        // We capture it before any await — the user could still
        // navigate further while the prompt is on screen, but at
        // least the entry we restore matches our intent at this
        // moment.
        const url = location.href;
        const choice = await _promptDirtyAction(_state.selectedPath);
        if (choice === 'cancel') {
            history.pushState(null, '', url);
            return;
        }
        if (choice === 'save') {
            await _onSaveClick();
            if (_state.dirty) {
                // Save failed — restore the URL and keep the modal.
                history.pushState(null, '', url);
                return;
            }
        }
        // discard or successful save → let the navigation stand
        // and tear the modal down.
        closeHelpFiles();
    };

    // Click capture for in-page anchor navigations. We only act on
    // anchors that would actually navigate (have href, not target=_blank,
    // not download, not modifier-clicked); everything else passes through.
    const onLinkClick = async (e) => {
        if (!_state.rootEl || !_state.dirty) return;
        const a = e.target && e.target.closest && e.target.closest('a[href]');
        if (!a) return;
        // Skip clicks that the browser will not treat as a same-tab
        // navigation: middle-click, modifier-click, target=_blank,
        // and explicit downloads all leave the current page intact.
        if (e.button !== 0) return;
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        if (a.target && a.target !== '' && a.target !== '_self') return;
        if (a.hasAttribute('download')) return;
        // Skip clicks within the help-files modal itself — those are
        // internal UI links (e.g. quota links) that should never be
        // gated by the dirty prompt.
        if (_state.rootEl.contains(a)) return;

        e.preventDefault();
        e.stopPropagation();
        const choice = await _promptDirtyAction(_state.selectedPath);
        if (choice === 'cancel') return;
        if (choice === 'save') {
            await _onSaveClick();
            if (_state.dirty) return;   // save failed — stay
        }
        // discard or successful save → close the modal and let the
        // user's intended navigation proceed.
        closeHelpFiles();
        // Re-issue the click programmatically so the SPA router
        // (or the browser, for full-page hrefs) handles it as it
        // would have without our interception.
        a.click();
    };

    window.addEventListener('beforeunload', onBeforeUnload);
    window.addEventListener('popstate', onPopState);
    document.addEventListener('click', onLinkClick, true);

    _navGuardsState = { onBeforeUnload, onPopState, onLinkClick };
}

function _uninstallNavigationGuards() {
    if (!_navGuardsState) return;
    window.removeEventListener('beforeunload', _navGuardsState.onBeforeUnload);
    window.removeEventListener('popstate',     _navGuardsState.onPopState);
    document.removeEventListener('click',      _navGuardsState.onLinkClick, true);
    _navGuardsState = null;
}

// =============================================================================
//  Networking — direct fetch with bearer auth
// =============================================================================

// _baseURL returns the project-scoped help-files prefix used by every
// endpoint in this module. Centralised so a typo can't drift between
// callers.
function _baseURL() {
    return `/api/v1/projects/${encodeURIComponent(_state.projectId)}/files/help`;
}

// _authHeaders returns an object suitable for fetch() init.headers.
// Spelt out in full because some endpoints add Content-Type and others
// don't — building the object explicitly avoids "ah, I forgot to merge"
// bugs at every call site.
function _authHeaders(extra = {}) {
    const h = { ...extra };
    if (S.token) h.Authorization = 'Bearer ' + S.token;
    return h;
}

/** GET the file list (and quota) and store on _state. */
async function _refreshList() {
    try {
        const r = await fetch(_baseURL(), { headers: _authHeaders() });
        const json = await r.json();
        if (json?.metadata?.status !== 200) {
            toast('error', json?.metadata?.error || 'Failed to load files');
            return;
        }
        _state.files = Array.isArray(json.data?.files) ? json.data.files : [];
        _state.quota = json.data?.quota || null;
        _renderQuotaFooter();
    } catch (e) {
        toast('error', 'Network error loading files');
    }
}

/** GET a single file's content. Returns { ok, content, mimeType }. */
async function _fetchFile(path) {
    const r = await fetch(`${_baseURL()}/${_encodePath(path)}`, {
        headers: _authHeaders(),
    });
    if (!r.ok) {
        return { ok: false, content: null, mimeType: null };
    }
    const mime = r.headers.get('Content-Type') || '';
    if (mime.startsWith('image/')) {
        // For images, return the blob URL — the preview renders an <img>.
        const blob = await r.blob();
        return { ok: true, content: URL.createObjectURL(blob), mimeType: mime, isImage: true };
    }
    const text = await r.text();
    return { ok: true, content: text, mimeType: mime, isImage: false };
}

/** PUT raw bytes to the given path. Body is a string for markdown or a Blob for images. */
async function _saveFile(path, body) {
    const r = await fetch(`${_baseURL()}/${_encodePath(path)}`, {
        method: 'PUT',
        headers: _authHeaders(),
        body,
    });
    const json = await r.json().catch(() => ({}));
    return { status: r.status, json };
}

/** DELETE one file. Server is idempotent — non-existent path is not an error. */
async function _deleteFile(path) {
    const r = await fetch(`${_baseURL()}/${_encodePath(path)}`, {
        method: 'DELETE',
        headers: _authHeaders(),
    });
    return r.json().catch(() => ({}));
}

/** POST a rename. Returns { ok, error?, conflict? }. */
async function _renameFile(oldPath, newPath) {
    const r = await fetch(
        `${_baseURL()}/${_encodePath(oldPath)}/rename`,
        {
            method: 'POST',
            headers: _authHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ newPath }),
        },
    );
    const json = await r.json().catch(() => ({}));
    return {
        ok: r.status === 200,
        error: json?.metadata?.error,
        conflict: r.status === 409,
        status: r.status,
    };
}

// _encodePath URL-encodes each segment but leaves the slash separator
// intact so "examples/foo.png" becomes "examples/foo.png" (not
// "examples%2Ffoo.png"). Echo's "*path" parameter handles both forms,
// but the readable form makes manual curl debugging easier.
function _encodePath(p) {
    return p.split('/').map(encodeURIComponent).join('/');
}

// =============================================================================
//  Rendering — modal shell, tree, right pane, footer
// =============================================================================

function _renderShell() {
    const root = document.createElement('div');
    root.className = 'hf-modal-root';
    root.innerHTML = `
        <div class="hf-modal" role="dialog" aria-label="File manager">
            <header class="hf-header">
                <h2>Files</h2>
                <div class="hf-header-actions">
                    <div class="hf-action-dropdown">
                        <button class="hf-btn hf-btn-ghost" id="hf-action-toggle">
                            <i class="fa-solid fa-plus"></i> Action
                            <i class="fa-solid fa-caret-down"></i>
                        </button>
                        <ul class="hf-action-menu" id="hf-action-menu" hidden>
                            <li data-action="new-md">New markdown</li>
                            <li data-action="upload-img">Upload image</li>
                            <li data-action="ai-svg-help">How to generate SVG with AI</li>
                        </ul>
                    </div>
                    <button class="hf-btn hf-btn-ghost" id="hf-close" title="Close">
                        <i class="fa-solid fa-xmark"></i>
                    </button>
                </div>
            </header>

            <div class="hf-body">
                <aside class="hf-sidebar">
                    <ul class="hf-tree" id="hf-tree"></ul>
                    <footer class="hf-quota" id="hf-quota"></footer>
                </aside>

                <section class="hf-pane" id="hf-pane">
                    <div class="hf-pane-toolbar" id="hf-pane-toolbar" hidden>
                        <span class="hf-pane-title" id="hf-pane-title"></span>
                        <div class="hf-pane-actions">
                            <button class="hf-btn hf-btn-ghost hf-btn-preview-toggle"
                                    id="hf-preview-toggle"
                                    title="Toggle markdown preview">
                                <i class="fa-solid fa-eye"></i> Preview
                            </button>
                            <button class="hf-btn hf-btn-ghost" id="hf-rename" title="Rename">
                                <i class="fa-solid fa-pen-to-square"></i> Rename
                            </button>
                            <button class="hf-btn hf-btn-ghost hf-btn-danger" id="hf-delete" title="Delete">
                                <i class="fa-solid fa-trash"></i> Delete
                            </button>
                            <button class="hf-btn hf-btn-primary" id="hf-save" title="Save (Ctrl+S)">
                                <i class="fa-solid fa-floppy-disk"></i> Save
                            </button>
                        </div>
                    </div>
                    <div class="hf-pane-body" id="hf-pane-body">
                        <div class="hf-empty">Select a file from the list, or create a new one with <strong>Action</strong>.</div>
                    </div>
                </section>
            </div>
        </div>
    `;
    document.body.appendChild(root);
    _state.rootEl = root;

    // Close on Escape.
    const onKey = (e) => {
        if (e.key === 'Escape') {
            // Route through _attemptClose so a dirty buffer is
            // protected by the three-way prompt (Save / Discard /
            // Cancel) rather than silently discarded.
            _attemptClose();
            document.removeEventListener('keydown', onKey);
        }
        // Save on Ctrl/Cmd+S inside the modal.
        if ((e.ctrlKey || e.metaKey) && e.key === 's' && _state.rootEl) {
            e.preventDefault();
            _onSaveClick();
        }
    };
    document.addEventListener('keydown', onKey);

    // Wire up the toolbar buttons.
    document.getElementById('hf-close').onclick = () => _attemptClose();
    document.getElementById('hf-rename').onclick = _onRenameClick;
    document.getElementById('hf-delete').onclick = _onDeleteClick;
    document.getElementById('hf-save').onclick = _onSaveClick;
    document.getElementById('hf-preview-toggle').onclick = _onPreviewToggleClick;

    // Action dropdown: a click toggles, a click anywhere else closes.
    const toggle = document.getElementById('hf-action-toggle');
    const menu = document.getElementById('hf-action-menu');
    toggle.onclick = (e) => {
        e.stopPropagation();
        menu.hidden = !menu.hidden;
    };
    document.addEventListener('click', () => { menu.hidden = true; });
    menu.onclick = (e) => {
        const li = e.target.closest('li');
        if (!li) return;
        menu.hidden = true;
        switch (li.dataset.action) {
            case 'new-md':       _actionNewMarkdown(); break;
            case 'upload-img':   _actionUploadImage(); break;
            case 'ai-svg-help':  _actionShowAISVGHelp(); break;
        }
    };
}

function _renderTree() {
    const ul = document.getElementById('hf-tree');
    if (!ul) return;
    if (_state.files.length === 0) {
        ul.innerHTML = `<li class="hf-tree-empty">No files yet.</li>`;
        return;
    }

    // Group by leading folder. With the server-side regex allowing at
    // most one '/', the only possible groups are root + named subdirs
    // (in practice, only "examples/" today). Detecting the structure
    // dynamically means we don't have to hardcode the folder list.
    const root = [];
    const folders = new Map(); // name -> [files...]
    for (const f of _state.files) {
        const slash = f.path.indexOf('/');
        if (slash < 0) {
            root.push(f);
        } else {
            const folder = f.path.slice(0, slash);
            const child = f.path.slice(slash + 1);
            if (!folders.has(folder)) folders.set(folder, []);
            folders.get(folder).push({ ...f, child });
        }
    }

    // Build the DOM. Root files first, then each folder header followed
    // by its children. A simple <li>/<details> structure is enough; we
    // don't need draggable reordering.
    let html = '';
    for (const f of root) {
        html += _treeRowHTML(f, false);
    }
    for (const [folder, children] of folders) {
        html += `<li class="hf-tree-folder"><i class="fa-solid fa-folder"></i> ${_escape(folder)}/</li>`;
        for (const f of children) {
            html += _treeRowHTML(f, true);
        }
    }
    ul.innerHTML = html;

    // Click selects.
    ul.querySelectorAll('li.hf-tree-row').forEach(el => {
        el.onclick = () => _selectFile(el.dataset.path);
    });

    _highlightSelected();
}

function _treeRowHTML(file, indented) {
    const icon = _iconForMime(file.mimeType);
    const cls = 'hf-tree-row' + (indented ? ' hf-tree-indent' : '');
    const display = indented ? file.child : file.path;
    return `
        <li class="${cls}" data-path="${_escape(file.path)}">
            <i class="${icon}"></i>
            <span class="hf-tree-name">${_escape(display)}</span>
            <span class="hf-tree-size">${_fmtBytes(file.sizeBytes)}</span>
        </li>
    `;
}

function _iconForMime(mime) {
    if (!mime) return 'fa-solid fa-file';
    if (mime.startsWith('image/svg')) return 'fa-solid fa-bezier-curve';
    if (mime.startsWith('image/'))    return 'fa-solid fa-image';
    if (mime.startsWith('text/markdown') || mime.startsWith('text/'))
        return 'fa-solid fa-file-lines';
    return 'fa-solid fa-file';
}

function _highlightSelected() {
    const ul = document.getElementById('hf-tree');
    if (!ul) return;
    ul.querySelectorAll('li.hf-tree-row').forEach(el => {
        el.classList.toggle('hf-selected', el.dataset.path === _state.selectedPath);
    });
}

function _renderQuotaFooter() {
    const el = document.getElementById('hf-quota');
    if (!el || !_state.quota) return;
    const proj = _state.quota.project || { used: 0, limit: 0 };
    const user = _state.quota.user || { used: 0, limit: 0 };
    const projPct = proj.limit > 0 ? proj.used / proj.limit : 0;
    // Visual warning bands: yellow at 80%, red at 95%.
    const projCls = projPct >= 0.95 ? 'hf-quota-bad'
                  : projPct >= 0.80 ? 'hf-quota-warn'
                  : '';
    el.innerHTML = `
        <div class="hf-quota-row ${projCls}">
            <strong>Project:</strong> ${_fmtBytes(proj.used)} / ${_fmtBytes(proj.limit)}
        </div>
        <div class="hf-quota-row">
            <strong>User:</strong> ${_fmtBytes(user.used)} / ${_fmtBytes(user.limit)}
        </div>
    `;
}

// =============================================================================
//  File selection — load content into the right pane
// =============================================================================

async function _selectFile(path) {
    // Same path means "no-op" — clicking the already-selected row
    // should not discard a dirty buffer or re-fetch.
    if (path === _state.selectedPath) return;

    if (_state.dirty) {
        // Three-way prompt: Save (default), Discard, Cancel.
        //
        // The previous version used showConfirm — a binary
        // Discard/Cancel — which silently lost work whenever the
        // user clicked another file by accident with the wrong
        // expectation of what "OK" meant. Defaulting to Save and
        // wiring Enter to it follows the principle that the safe
        // action is the one most easily reachable.
        const choice = await _promptDirtyAction(_state.selectedPath);
        if (choice === 'cancel')  return;
        if (choice === 'save') {
            // Persist before switching. _onSaveClick handles the
            // toast and the dirty flag reset, but it ignores its
            // return; we wait for the network round-trip and then
            // sanity-check that the save actually cleared the
            // dirty state — if it failed (quota error, etc.), do
            // NOT discard the buffer.
            await _onSaveClick();
            if (_state.dirty) return; // save failed; stay put
        }
        // 'discard' falls through.
    }

    _state.selectedPath = path;
    _state.dirty = false;
    _highlightSelected();

    const result = await _fetchFile(path);
    if (!result.ok) {
        _renderError('Could not load file.');
        return;
    }

    if (result.isImage) {
        _renderImagePreview(path, result.content);
    } else {
        // _renderMarkdownEditor may need to lazy-load Monaco — await
        // ensures the toolbar render below doesn't fight the editor
        // for layout space mid-mount.
        await _renderMarkdownEditor(path, result.content);
    }
    _renderPaneToolbar(path, result.isImage);
}

// _promptDirtyAction shows a three-way modal asking what to do with
// the dirty buffer when the user is about to switch away from the
// current file. Resolves to one of:
//
//   'save'    — Save and continue (default; Enter binds here)
//   'discard' — Throw the changes away
//   'cancel'  — Keep the current file open, do nothing
//
// `currentPath` is shown in the prompt so the user knows which
// file is being switched away from. Same overlay pattern as the
// AI-SVG help and the new-file modals — see those for the layout.
function _promptDirtyAction(currentPath) {
    return new Promise((resolve) => {
        const overlay = document.createElement('div');
        overlay.className = 'hf-ai-help-overlay';
        overlay.innerHTML = `
            <div class="hf-ai-help-modal" style="max-width:440px">
                <header>
                    <h3>Unsaved changes</h3>
                    <button class="hf-btn hf-btn-ghost" data-cancel
                            title="Cancel"><i class="fa-solid fa-xmark"></i></button>
                </header>
                <div class="hf-ai-help-body">
                    <p><strong>${_escape(currentPath || 'this file')}</strong>
                       has unsaved changes.</p>
                    <p>What would you like to do?</p>
                </div>
                <footer>
                    <button class="hf-btn hf-btn-ghost"   data-cancel>Cancel</button>
                    <button class="hf-btn hf-btn-danger"  data-discard>Discard</button>
                    <button class="hf-btn hf-btn-primary" data-save>Save</button>
                </footer>
            </div>
        `;
        document.body.appendChild(overlay);

        const finish = (choice) => {
            overlay.remove();
            document.removeEventListener('keydown', onKey);
            resolve(choice);
        };

        // Default focus on Save so Enter naturally lands on the
        // safe action. Tab still cycles through the buttons in the
        // visual order (Cancel, Discard, Save).
        const saveBtn    = overlay.querySelector('[data-save]');
        const discardBtn = overlay.querySelector('[data-discard]');
        saveBtn.focus();

        saveBtn.onclick    = () => finish('save');
        discardBtn.onclick = () => finish('discard');
        overlay.querySelectorAll('[data-cancel]').forEach(b => b.onclick = () => finish('cancel'));

        // Click on the dim backdrop = cancel (matches the X button).
        overlay.onclick = (e) => { if (e.target === overlay) finish('cancel'); };

        // Enter → Save (default action). Escape → Cancel.
        // We intentionally do NOT bind any letter shortcut to
        // Discard — it should always require an explicit click,
        // mirroring how destructive actions tend to be guarded
        // from accidental keyboard activation.
        const onKey = (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                finish('save');
            } else if (e.key === 'Escape') {
                e.preventDefault();
                finish('cancel');
            }
        };
        document.addEventListener('keydown', onKey);
    });
}

function _renderPaneToolbar(path, isImage) {
    const bar = document.getElementById('hf-pane-toolbar');
    const title = document.getElementById('hf-pane-title');
    if (!bar || !title) return;
    bar.hidden = false;
    title.textContent = path;
    // Disable Save for images — they are uploaded once, not edited
    // in place. Ctrl+S also no-ops in image mode (handled in _onSaveClick).
    document.getElementById('hf-save').disabled = isImage;

    // Preview toggle is only meaningful for markdown. Hide it for
    // images and reset the state so a future markdown selection
    // starts in editor mode (consistent with the user's mental
    // model of "I just opened a file → here's the editor").
    const toggleBtn = document.getElementById('hf-preview-toggle');
    if (toggleBtn) {
        toggleBtn.hidden = isImage;
        toggleBtn.classList.remove('hf-active');
        toggleBtn.innerHTML = '<i class="fa-solid fa-eye"></i> Preview';
    }
    _state.previewMode = false;
}

// _ensureMonaco loads the Monaco loader script and editor.main module
// if they are not already present. The project Editor tab does the same
// thing on first mount (see projects.js _doMount); we reuse the same
// loader URL and config so a session that opened the file manager
// before the Editor tab still gets a working markdown editor.
//
// Returns a Promise that resolves once `window.monaco` is usable.
// Idempotent: calling it after Monaco is loaded resolves immediately.
function _ensureMonaco() {
    return new Promise((resolve, reject) => {
        if (typeof window.monaco !== 'undefined') {
            resolve();
            return;
        }
        // The loader script may already be in the document (Editor tab
        // opened earlier in this session) but the editor.main module
        // may not have been required yet — handle both cases.
        const finishWithRequire = () => {
            try {
                window.require.config({ paths: { vs: '/monaco/vs' } });
                window.require(['vs/editor/editor.main'], () => resolve());
            } catch (e) {
                reject(e);
            }
        };
        if (typeof window.require === 'function') {
            finishWithRequire();
            return;
        }
        // Loader script absent — inject it and wait.
        const existing = document.getElementById('hf-monaco-loader');
        if (existing) {
            existing.addEventListener('load', finishWithRequire, { once: true });
            existing.addEventListener('error', () => reject(new Error('Monaco loader failed to load')), { once: true });
            return;
        }
        const s = document.createElement('script');
        s.id  = 'hf-monaco-loader';
        s.src = '/monaco/vs/loader.js';
        s.onload  = finishWithRequire;
        s.onerror = () => reject(new Error('Monaco loader failed to load'));
        document.head.appendChild(s);
    });
}

async function _renderMarkdownEditor(path, content) {
    // Dispose any previous instance — we mount a fresh one per file
    // rather than swap models, to keep the lifecycle obvious.
    if (_state.monacoInst) {
        try { _state.monacoInst.dispose(); } catch { /* ignore */ }
        _state.monacoInst = null;
    }
    const body = document.getElementById('hf-pane-body');
    body.innerHTML = `<div id="hf-monaco-host" class="hf-monaco-host"></div>`;
    const host = document.getElementById('hf-monaco-host');

    // Earlier versions assumed the Editor tab had already loaded Monaco
    // and bailed out with an error message when it had not. That broke
    // the common case of "open a fresh project, click Files, create a
    // markdown without ever visiting the Editor tab". Now we load
    // Monaco on demand if it isn't already in the page.
    try {
        await _ensureMonaco();
    } catch (e) {
        body.innerHTML = `<div class="hf-error">Could not load the Monaco editor (${_escape(String(e?.message || e))}).</div>`;
        return;
    }

    // _state.selectedPath may have changed while Monaco was loading
    // (user clicked another file). If so, abort: a later call will
    // mount the editor for the right file.
    if (_state.selectedPath !== path) return;

    _state.savedContent = content;
    _state.monacoInst = window.monaco.editor.create(host, {
        value: content,
        language: 'markdown',
        theme: 'vs',
        wordWrap: 'on',
        lineNumbers: 'on',
        minimap: { enabled: false },
        scrollBeyondLastLine: false,
        fontSize: 13,
        fontFamily: "'Fira Code','Consolas',monospace",
        automaticLayout: true,
    });
    _state.monacoInst.onDidChangeModelContent(() => {
        const cur = _state.monacoInst.getValue();
        const wasDirty = _state.dirty;
        _state.dirty = cur !== _state.savedContent;
        if (_state.dirty !== wasDirty) {
            _updateSaveButton();
        }
    });
    _updateSaveButton();

    // "Insert image…" in Monaco's right-click context menu — lets the author
    // drop an uploaded project image into the markdown without leaving the
    // editor (the discoverable entry point the user asked for). Bound to this
    // editor instance, so it is disposed together with the editor on the next
    // mount; no separate disposable to track.
    _state.monacoInst.addAction({
        id:                 'iotm.hf.insertImage',
        label:              'Insert image…',
        contextMenuGroupId: 'navigation',
        contextMenuOrder:   1.5,
        run: function () { _hfInsertImageDialog(); },
    });

    // Land focus inside the editor so the user can start typing
    // straight away. The setTimeout(0) is intentional — Monaco
    // attaches its DOM during the create() call but sometimes the
    // textarea inside isn't yet in the focus chain when the call
    // returns; deferring one tick lets layout settle before we
    // ask for focus. Without this the focus call silently no-ops
    // about half the time on Chromium.
    setTimeout(() => {
        if (_state.monacoInst === null) return; // disposed during the tick
        try {
            _state.monacoInst.focus();
        } catch { /* editor disposed mid-flight; ignore */ }
    }, 0);
}

// _hfInsertImageDialog shows a small picker of the project's uploaded images
// (the image-MIME entries already in _state.files) and, on selection, inserts
// a Markdown image tag at the editor's caret via _hfInsertAtCursor. Invoked
// from the editor's right-click "Insert image…" action.
//
// The inserted reference is the project-relative file path (e.g.
// "examples/photo.png"), which both the preview resolver (_resolveProjectImage,
// which strips any leading slash) and the published help understand. No new
// network call: the image list is whatever _refreshList already loaded.
function _hfInsertImageDialog() {
    const ed = _state.monacoInst;
    if (!ed) return;

    const images = _state.files.filter(f => (f.mimeType || '').startsWith('image/'));
    if (!images.length) {
        alert('No images uploaded for this project yet. Use Action → Upload image first.');
        return;
    }

    // Single instance — drop any previous picker before opening a new one.
    document.getElementById('hf-img-picker')?.remove();

    const rows = images.map(f => {
        const fileName = f.path.split('/').pop();
        return '<li data-path="' + _escape(f.path) + '"' +
            ' style="display:flex;align-items:center;gap:10px;padding:9px 14px;cursor:pointer;' +
            'border-bottom:1px solid var(--border)"' +
            ' onmouseover="this.style.background=\'var(--info-bg, rgba(127,127,127,.12))\'"' +
            ' onmouseout="this.style.background=\'\'">' +
            '<i class="fa-solid fa-image" style="color:var(--primary);width:16px;text-align:center"></i>' +
            '<div><div style="font-weight:600;color:var(--text-primary)">' + _escape(fileName) + '</div>' +
            '<div style="font-size:11px;color:var(--text-muted)">' + _escape(f.path) + '</div></div>' +
            '</li>';
    }).join('');

    const overlay = document.createElement('div');
    overlay.id = 'hf-img-picker';
    overlay.style.cssText =
        'position:fixed;inset:0;z-index:10000;display:flex;align-items:center;' +
        'justify-content:center;background:rgba(0,0,0,.35)';
    overlay.innerHTML =
        '<div style="background:var(--bg-card);border:1px solid var(--border);' +
        'border-radius:var(--r,8px);min-width:320px;max-width:520px;max-height:70vh;' +
        'overflow:auto;box-shadow:0 8px 30px rgba(0,0,0,.3);font-family:var(--font)">' +
        '<div style="padding:12px 14px;border-bottom:1px solid var(--border);' +
        'font-weight:700;color:var(--text-primary)">Insert image</div>' +
        '<ul style="list-style:none;margin:0;padding:0">' + rows + '</ul></div>';
    document.body.appendChild(overlay);

    const close = () => {
        overlay.remove();
        document.removeEventListener('keydown', onKey);
    };
    const onKey = (e) => { if (e.key === 'Escape') close(); };
    document.addEventListener('keydown', onKey);
    overlay.addEventListener('mousedown', (e) => { if (e.target === overlay) close(); });
    overlay.querySelector('ul').addEventListener('click', (e) => {
        const li = e.target.closest('li[data-path]');
        if (!li) return;
        const p = li.getAttribute('data-path');
        const baseName = p.split('/').pop().replace(/\.[^.]+$/, '');
        close();
        _hfInsertAtCursor('![' + baseName + '](' + p + ')');
    });
}

// _hfInsertAtCursor writes text at the editor's current selection (or caret,
// when the selection is empty) and refocuses the editor.
function _hfInsertAtCursor(text) {
    const ed = _state.monacoInst;
    if (!ed) return;
    const sel = ed.getSelection();
    ed.executeEdits('hf-insert-image', [
        { range: sel, text: text, forceMoveMarkers: true },
    ]);
    ed.focus();
}

function _renderImagePreview(path, blobUrl) {
    if (_state.monacoInst) {
        try { _state.monacoInst.dispose(); } catch { /* ignore */ }
        _state.monacoInst = null;
    }
    const body = document.getElementById('hf-pane-body');
    body.innerHTML = `
        <div class="hf-image-preview">
            <img src="${blobUrl}" alt="${_escape(path)}">
        </div>
    `;
}

function _renderError(message) {
    const body = document.getElementById('hf-pane-body');
    if (body) body.innerHTML = `<div class="hf-error">${_escape(message)}</div>`;
}

function _updateSaveButton() {
    const btn = document.getElementById('hf-save');
    if (!btn) return;
    btn.classList.toggle('hf-btn-dirty', _state.dirty);
}

// =============================================================================
//  Toolbar actions
// =============================================================================

async function _onSaveClick() {
    if (!_state.selectedPath || !_state.monacoInst) return;
    const content = _state.monacoInst.getValue();
    const result = await _saveFile(_state.selectedPath, content);
    if (result.status !== 200) {
        const err = result.json?.metadata?.error || `HTTP ${result.status}`;
        if (result.status === 413) {
            // Quota error — friendlier message with numbers.
            const meta = result.json?.metadata || {};
            toast('error',
                `${meta.scope === 'user' ? 'User' : 'Project'} quota exceeded ` +
                `(${_fmtBytes(meta.used)} / ${_fmtBytes(meta.limit)})`);
        } else {
            toast('error', `Save failed: ${err}`);
        }
        return;
    }
    _state.savedContent = content;
    _state.dirty = false;
    _updateSaveButton();
    toast('success', 'Saved');
    // Refresh the list to update sizes and quota.
    await _refreshList();
    _renderTree();
}

// =============================================================================
//  Markdown preview
// =============================================================================
//
// The preview lives in the same area as the Monaco editor. Toggle
// click -> swap visibility, render HTML on-demand from the editor's
// CURRENT value (so unsaved edits show up in the preview), rewrite
// any relative <img src> to project-scoped blob URLs the browser
// can fetch with the Bearer token, and inject the result into a
// .hf-md-preview div. Toggle again -> hide the preview, show the
// editor (its DOM and undo history are preserved).

async function _onPreviewToggleClick() {
    // No-op when there's no markdown editor mounted (image selected,
    // empty pane, etc.). Defensive even though the toolbar hides the
    // button in those cases — keyboard shortcuts could still reach
    // here.
    if (!_state.monacoInst || !_state.selectedPath) return;
    if (!_state.selectedPath.endsWith('.md')) return;

    const toggleBtn = document.getElementById('hf-preview-toggle');
    if (_state.previewMode) {
        // Coming out of preview: dispose any preview-only DOM and
        // unhide the editor host. Monaco's auto-layout picks the
        // size back up on the next event tick.
        _hideMarkdownPreview();
        if (toggleBtn) {
            toggleBtn.classList.remove('hf-active');
            toggleBtn.innerHTML = '<i class="fa-solid fa-eye"></i> Preview';
        }
        _state.previewMode = false;
        // Re-focus Monaco so the user can keep typing without an
        // extra click. The setTimeout is the same trick used in
        // _renderMarkdownEditor — Monaco needs one tick to reattach
        // its focus chain after being hidden.
        setTimeout(() => {
            try { _state.monacoInst && _state.monacoInst.focus(); }
            catch { /* disposed mid-tick */ }
        }, 0);
    } else {
        // Going into preview: render and show. Render is async because
        // we may need to load marked.js and fetch any referenced
        // images.
        if (toggleBtn) {
            toggleBtn.disabled = true;
            toggleBtn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Preview';
        }
        try {
            await _renderMarkdownPreview();
            _state.previewMode = true;
            if (toggleBtn) {
                toggleBtn.classList.add('hf-active');
                toggleBtn.innerHTML = '<i class="fa-solid fa-pen"></i> Edit';
            }
        } catch (e) {
            console.error('preview render failed:', e);
            toast('error', 'Could not render preview');
        } finally {
            if (toggleBtn) toggleBtn.disabled = false;
        }
    }
}

// _renderMarkdownPreview converts the Monaco buffer to HTML and
// shows it in the right pane. The editor host is hidden (display:
// none) rather than removed so toggling back doesn't lose state.
//
// Image resolution: any <img src="..."> whose src is a relative
// path is rewritten to a blob URL fetched from the project's help
// files endpoint. The fetch uses _authHeaders so the Bearer token
// reaches the server (the browser would NOT send it on a normal
// <img> request — that's why the rewrite is necessary).
//
// Failed images get a class hf-md-img-broken so the user sees a
// dashed placeholder rather than a silent gap. We do not block the
// whole preview on image fetches: each <img> becomes a microtask
// that updates the src when ready.
async function _renderMarkdownPreview() {
    const host = document.getElementById('hf-pane-body');
    if (!host || !_state.monacoInst) return;

    await _ensureMarked();

    const md = _state.monacoInst.getValue();
    // marked.parse synchronously returns the HTML string. We do NOT
    // pass `breaks: true` etc. — we want the same flavour as the
    // documentation pages and the wizard help pages already render
    // with elsewhere in the app.
    const html = window.marked.parse(md || '');

    // Hide the Monaco host but keep it in the DOM. Monaco's layout
    // is driven by automaticLayout which respects display:none —
    // toggling back just re-displays.
    const monacoHost = host.querySelector('#hf-monaco-host');
    if (monacoHost) {
        monacoHost.style.display = 'none';
    }

    // Replace any earlier preview div (e.g. user toggled twice
    // quickly) with a fresh one, so the DOM reflects the current
    // buffer rather than a stale render.
    let prev = host.querySelector('.hf-md-preview');
    if (prev) prev.remove();

    prev = document.createElement('div');
    prev.className = 'hf-md-preview';

    // ── Rewrite <img> srcs BEFORE they enter a live document ───
    //
    // Critical: assigning `prev.innerHTML = html` would construct
    // <img> elements whose browser-native loader fires immediately
    // — even on a detached div. A path like "/examples/example.png"
    // would resolve to localhost:8080/examples/example.png and 404
    // before our resolver gets a chance to fetch the bytes from
    // the help-files endpoint with the Bearer token.
    //
    // We avoid that by parsing marked's HTML string with DOMParser,
    // which builds nodes in an inert document (no fetches), then
    // replace every <img src> with a transparent placeholder and
    // stash the original on a data-* attribute. Only after the
    // adopt-into-prev step does the browser see the elements, and
    // by then they're harmless until our resolver swaps them in
    // for blob URLs.
    const TRANSPARENT_GIF =
        'data:image/gif;base64,R0lGODlhAQABAIAAAP///wAAACH5BAEAAAAALAAAAAABAAEAAAICRAEAOw==';
    const doc = new DOMParser().parseFromString(html, 'text/html');
    const sourceImgs = Array.from(doc.querySelectorAll('img'));
    sourceImgs.forEach(img => {
        const original = img.getAttribute('src') || '';
        // Stash the original on a data-* attribute (not src) so the
        // browser will not auto-fetch when the node is adopted.
        img.setAttribute('data-original-src', original);
        img.setAttribute('src', TRANSPARENT_GIF);
    });
    while (doc.body.firstChild) {
        prev.appendChild(doc.body.firstChild);
    }
    host.appendChild(prev);

    // ── Image resolution pass ───────────────────────────────────
    //
    // For each <img>, look at its data-original-src and decide:
    //
    //   - absolute URL (http://, https://, //host/) or data: URL
    //     → restore as src, browser fetches normally
    //   - "/path"  (origin-absolute as the markdown author wrote it,
    //              a common convention for "absolute in the project")
    //     → strip the slash, treat as project-relative
    //   - "path"   (project-relative)
    //     → fetch via help-files endpoint with Bearer token; on
    //       success swap the placeholder for a blob URL, on
    //       failure mark the <img> with the broken-image class
    //
    // The cache lives on _state.imageCache so multiple <img> tags
    // pointing at the same file share one fetch and one blob URL.
    // The cache is freed in closeHelpFiles via URL.revokeObjectURL.
    const imgs = prev.querySelectorAll('img[data-original-src]');
    imgs.forEach(img => {
        const original = img.getAttribute('data-original-src') || '';
        if (!original) {
            img.classList.add('hf-md-img-broken');
            img.alt = img.alt || '(empty src)';
            return;
        }
        // Absolute URLs (http://, https://, network-relative //host)
        // and data: URLs go through unchanged.
        if (/^[a-z][a-z0-9+.-]*:\/\//i.test(original) || original.startsWith('//') || original.startsWith('data:')) {
            img.setAttribute('src', original);
            return;
        }
        // Strip leading slashes so origin-absolute paths become
        // project-relative. Markdown authors commonly write
        // `![](/examples/example.png)` thinking "absolute in the
        // project". After this rewrite we have a path the resolver
        // can hand to the help-files endpoint.
        const candidate = original.replace(/^\/+/, '');
        _resolveProjectImage(candidate).then(blobUrl => {
            if (blobUrl) {
                img.setAttribute('src', blobUrl);
            } else {
                img.classList.add('hf-md-img-broken');
                img.alt = img.alt || candidate;
                console.warn('[help_files] image not found in project:', candidate);
            }
        }).catch((e) => {
            img.classList.add('hf-md-img-broken');
            img.alt = img.alt || candidate;
            console.warn('[help_files] image resolve threw:', candidate, e);
        });
    });
}

function _hideMarkdownPreview() {
    const host = document.getElementById('hf-pane-body');
    if (!host) return;
    const prev = host.querySelector('.hf-md-preview');
    if (prev) prev.remove();
    const monacoHost = host.querySelector('#hf-monaco-host');
    if (monacoHost) monacoHost.style.display = '';
}

// _resolveProjectImage fetches a help-file image and returns a blob
// URL. Reuses cached blob URLs so flipping the preview on/off does
// not re-download. Returns null on any error so the caller can
// render the broken-image placeholder.
//
// Resolution strategy: instead of consulting an in-memory file
// list (which can be stale or empty depending on when the preview
// runs), we attempt the fetch directly and let the server's 404
// be the source of truth.
//
//   1. Normalise the user-written path (strip leading "./").
//   2. If the path is exactly what's on disk, fetch it.
//   3. If the path has no subdirectory and step 2 returned 404,
//      try "examples/<name>" — this covers the common case of
//      a stego scene PNG referenced by bare filename.
//   4. If neither works, return null and the preview shows the
//      broken-image placeholder.
//
// We deliberately do NOT pre-filter using _state.files: the cache
// can be stale (the upload that created the file ran in another
// session, the listing happened before the upload, etc.). Trying
// the network is a single round-trip per candidate; on the
// happy path we hit step 2 once and stop.
//
// Cache keys: both the user-written path AND the resolved on-disk
// path, so a markdown that says `![](scene.png)` and one that
// says `![](examples/scene.png)` share one fetch.
async function _resolveProjectImage(relPath) {
    // Strip a leading "./" — both forms are common in markdown and
    // they refer to the same path on disk.
    const requested = relPath.replace(/^\.\//, '');
    if (!requested) return null;

    // Cache hit on the user-written path: reuse blob URL.
    if (_state.imageCache.has(requested)) {
        return _state.imageCache.get(requested);
    }

    // Build the candidate list. Always try the literal path first;
    // when the path has no subdirectory, also try the examples/
    // fallback. The order matters: root wins when both exist
    // (illustrations live at the root; examples/ is the stego pool).
    const candidates = [requested];
    if (!requested.includes('/')) {
        candidates.push('examples/' + requested);
    }

    for (const candidate of candidates) {
        // Cache hit on a candidate (a previous resolve already
        // landed on this on-disk path under a different markdown
        // path).
        if (_state.imageCache.has(candidate)) {
            const blobUrl = _state.imageCache.get(candidate);
            _state.imageCache.set(requested, blobUrl);
            return blobUrl;
        }

        const url = `${_baseURL()}/${_encodePath(candidate)}`;
        try {
            const r = await fetch(url, { headers: _authHeaders() });
            if (!r.ok) continue;            // 404 → try next candidate
            const ct = r.headers.get('Content-Type') || '';
            if (!ct.startsWith('image/')) continue;
            const blob = await r.blob();
            const blobUrl = URL.createObjectURL(blob);
            // Cache under both keys so subsequent references to
            // either form (`scene.png` or `examples/scene.png`)
            // hit the cache and skip the fetch.
            _state.imageCache.set(requested, blobUrl);
            if (candidate !== requested) {
                _state.imageCache.set(candidate, blobUrl);
            }
            return blobUrl;
        } catch {
            // Network error on this candidate — try the next one.
            // If both fail, the loop exits and we fall through to
            // returning null, producing the broken-image placeholder.
            continue;
        }
    }
    return null;
}

// _ensureMarked lazy-loads marked.min.js exactly once per page.
// Idempotent.
//
// Subtle problem solved here: marked is a UMD bundle. Its wrapper
// runs an if/else chain:
//
//   if (module.exports) { ... CommonJS ... }
//   else if (define && define.amd) { define(['exports'], factory) }
//   else { window.marked = {} }
//
// The page already has Monaco's AMD loader installed (window.define
// with .amd === true), so the second branch wins and marked
// registers itself as an AMD module — but our code never asks for
// it via require(['marked']), so window.marked stays undefined
// and `marked.parse(...)` crashes.
//
// Fix: stash window.define before injecting the script, so the UMD
// wrapper sees no AMD environment and falls through to the
// browser-global branch. Restore window.define after the script
// loads so Monaco keeps working. The trick is well known among
// projects that mix Monaco + UMD libraries.
function _ensureMarked() {
    return new Promise((resolve, reject) => {
        if (window.marked && typeof window.marked.parse === 'function') {
            resolve();
            return;
        }
        const existing = document.getElementById('hf-marked-loader');
        if (existing) {
            // A previous _ensureMarked call is still loading the
            // script; piggyback on it.
            existing.addEventListener('load',  () => {
                if (window.marked && typeof window.marked.parse === 'function') resolve();
                else reject(new Error('marked loaded but window.marked.parse is missing'));
            }, { once: true });
            existing.addEventListener('error', () => reject(new Error('marked failed to load')), { once: true });
            return;
        }

        // Stash define / require so the UMD wrapper picks the
        // browser-global branch. We only need to hide `define.amd`,
        // but stashing the whole pair is simpler and equally safe.
        const stashedDefine  = window.define;
        const stashedRequire = window.require;
        try { window.define  = undefined; } catch { /* read-only? unlikely */ }
        try { window.require = undefined; } catch { /* read-only? unlikely */ }

        const restore = () => {
            // Putting the originals back is critical — Monaco needs
            // window.define to keep loading workers and language
            // services. Restoring even on error so we don't leave
            // the page in a half-broken AMD state.
            try { window.define  = stashedDefine;  } catch { /* ignore */ }
            try { window.require = stashedRequire; } catch { /* ignore */ }
        };

        const s = document.createElement('script');
        s.id  = 'hf-marked-loader';
        s.src = '/marked/marked.min.js';
        s.onload  = () => {
            restore();
            if (window.marked && typeof window.marked.parse === 'function') {
                resolve();
            } else {
                reject(new Error('marked loaded but window.marked.parse is missing'));
            }
        };
        s.onerror = () => {
            restore();
            reject(new Error('marked failed to load'));
        };
        document.head.appendChild(s);
    });
}

async function _onRenameClick() {
    if (!_state.selectedPath) return;
    const oldPath = _state.selectedPath;
    const newPath = await showPrompt(
        `Rename "${oldPath}" to:`,
        oldPath,
    );
    if (!newPath || newPath === oldPath) return;
    const r = await _renameFile(oldPath, newPath);
    if (!r.ok) {
        if (r.conflict) {
            toast('error', `Cannot rename: "${newPath}" already exists.`);
        } else {
            toast('error', r.error || `Rename failed (${r.status})`);
        }
        return;
    }
    // Server-side rename may have changed the mime type; reload list and
    // pick the new path.
    await _refreshList();
    _renderTree();
    await _selectFile(newPath);
    toast('success', 'Renamed');
}

async function _onDeleteClick() {
    if (!_state.selectedPath) return;
    const path = _state.selectedPath;
    const goAhead = await showConfirm(
        `Delete "${path}"? This cannot be undone.`,
        'Delete',
        'Cancel',
    );
    if (!goAhead) return;
    await _deleteFile(path);
    _state.selectedPath = null;
    _state.dirty = false;
    if (_state.monacoInst) {
        try { _state.monacoInst.dispose(); } catch { /* ignore */ }
        _state.monacoInst = null;
    }
    document.getElementById('hf-pane-toolbar').hidden = true;
    document.getElementById('hf-pane-body').innerHTML =
        `<div class="hf-empty">Select a file from the list.</div>`;
    await _refreshList();
    _renderTree();
    toast('success', 'Deleted');
}

// =============================================================================
//  Action dropdown actions
// =============================================================================

async function _actionNewMarkdown(opts) {
    // Pre-condition: a successful parse is required because the
    // "name" dropdown lists "main menu text" plus the device's
    // method names (Init, Run, Log, …). Without parsed data we
    // cannot offer a method choice — and per the agreed UX, even
    // the readme path is gated behind a successful parse so the
    // user always lands in the manager with a fully working
    // dropdown.
    //
    // No-parse → alert + close everything (alert AND file manager,
    // per the contract). The user goes back to the editor, clicks
    // Parse, and re-enters via Action → New file when ready. The
    // wizard's "Add help" buttons are disabled while parse is
    // missing, so when called from there we never hit this path —
    // the gate stays defensive for direct toolbar entry.
    const parsed = _state.parsed;
    if (!parsed) {
        await _alertParseFirst();
        closeHelpFiles();
        return;
    }

    // Lazy-load the BCP 47 tag list. The fetch is cached at the
    // browser layer (Cache-Control: max-age=86400 in the static
    // handler) so subsequent opens don't pay the round trip. We
    // store the parsed array on _state so the modal's datalist
    // can rebuild without re-fetching even within the same
    // session.
    if (!_state.bcp47Tags) {
        try {
            const r = await fetch('/static/data/bcp47-tags.json',
                { credentials: 'same-origin' });
            if (r.ok) {
                _state.bcp47Tags = await r.json();
            }
        } catch (e) {
            console.warn('[help_files] BCP 47 list unavailable:', e);
        }
        // Fallback: a tiny built-in list keeps the datalist non-empty
        // even when the static file is unreachable. The user can still
        // type any tag — the server validates with golang.org/x/text/
        // language so unknown-but-valid tags are accepted regardless.
        if (!Array.isArray(_state.bcp47Tags) || !_state.bcp47Tags.length) {
            _state.bcp47Tags = ['en', 'pt-br', 'es', 'fr', 'de', 'zh-cn', 'ja'];
        }
    }

    // Compute the per-name occupancy from the current file list so
    // the order dropdown can offer [0..count] for each name. Cheap
    // O(F * M) where F = files in project, M = methods + readme;
    // both are small.
    const names = _collectNameOptions(parsed);
    const occupancy = _computeOccupancy(_state.files, names);

    // prefillBasename (when provided by an "Add help" deep-link)
    // pre-selects the matching <option> in the name dropdown so the
    // user lands on the right entry without having to scroll. Falls
    // through to the dropdown's natural first option ("main menu
    // text") when absent or when no name matches.
    const choice = await _promptNewMarkdownModal({
        names,
        occupancy,
        bcp47Tags: _state.bcp47Tags,
        prefillBasename: opts && opts.prefillBasename,
    });
    if (!choice) return;

    // Send to the new insert endpoint. The server handles cascade
    // renames atomically; the client only needs to know the name,
    // language, position, and content.
    //
    // Auth is via the same Bearer token used by every other call
    // in this module (_authHeaders adds it from S.token). An
    // earlier draft used credentials:'same-origin' — wrong, the
    // backend doesn't read session cookies for /api/v1/* and was
    // returning 401.
    const url = `/api/v1/projects/${encodeURIComponent(_state.projectId)}/files/help/insert`;
    const r = await fetch(url, {
        method: 'POST',
        headers: _authHeaders({ 'Content-Type': 'application/json' }),
        body: JSON.stringify({
            basename: choice.basename,
            language: choice.language,
            position: choice.position,
            content:  '',
        }),
    });

    let json = null;
    try { json = await r.json(); } catch { /* HTML error page */ }

    if (r.status !== 200) {
        const err = json?.metadata?.error || `HTTP ${r.status}`;
        toast('error', `Could not create: ${err}`);
        return;
    }

    const insertedPath = json?.data?.path;
    try {
        await _refreshList();
        _renderTree();
        if (insertedPath) {
            await _selectFile(insertedPath);
        }
        toast('success', 'Created');
    } catch (e) {
        console.error('post-create UI failed:', e);
        toast('error', `Created on server, but the editor could not open (${e?.message || e}). Refresh the file manager.`);
    }
}

// _alertParseFirst shows a blocking modal explaining the parse
// pre-condition. Resolves when the user clicks OK. Used by
// _actionNewMarkdown when there is no parsed data.
//
// We render a separate modal (rather than reusing showConfirm) so
// the user sees only one button — there is no choice to make,
// only an acknowledgement. The dim backdrop matches the rest of
// the file manager.
function _alertParseFirst() {
    return new Promise((resolve) => {
        const overlay = document.createElement('div');
        overlay.className = 'hf-ai-help-overlay';
        overlay.innerHTML = `
            <div class="hf-ai-help-modal" style="max-width:380px">
                <header>
                    <h3>Please parse first</h3>
                </header>
                <div class="hf-ai-help-body" style="padding:18px 20px">
                    <p style="margin:0">
                        New help files are named after methods discovered by
                        the parser. Run <strong>Parse</strong> in the editor
                        toolbar first, then come back here to create the file.
                    </p>
                </div>
                <footer>
                    <button class="hf-btn hf-btn-primary" data-ok>OK</button>
                </footer>
            </div>
        `;
        document.body.appendChild(overlay);

        const cleanup = () => {
            overlay.remove();
            resolve();
        };
        overlay.querySelector('[data-ok]').onclick = cleanup;
        // Backdrop click does NOT close — we want the user to
        // explicitly acknowledge before the file manager dismisses.
        // (Escape still works for accessibility.)
        const onKey = (e) => {
            if (e.key === 'Escape' || e.key === 'Enter') {
                e.preventDefault();
                document.removeEventListener('keydown', onKey);
                cleanup();
            }
        };
        document.addEventListener('keydown', onKey);
    });
}

// _collectNameOptions builds the ordered list shown in the name
// dropdown of the create-file modal. The list depends on the
// language of the parsed source:
//
//   Go:
//     1. "main menu text"   → basename "readme"
//     2. "Device Init"      (when parsed.init is present)
//     3. each "Device <Method>" in source order (parsed.methods)
//
//   C99:
//     1. "main menu text"   → basename "readme"
//     2. each "Device <function>" in source order (parsed.functions)
//        — C99 has no Init/methods concept; every public function is
//        its own block.
//
// Branch selection is implicit: the JSON marshalling uses omitempty,
// so parsed.functions is undefined for Go and parsed.methods /
// parsed.init are undefined for C99. The three checks are mutually
// exclusive at runtime, but the code stays language-agnostic.
//
// The user-facing label and the underlying basename differ:
//
//   - "main menu text" is friendlier than the internal "readme"
//   - blocks are prefixed with "Device " in the label so the
//     dropdown reads "Device Init / Device Run / Device sht3x_read" —
//     this is what the maker thinks of when they look at a wired-up
//     device on the Stage. The basename stays the bare block name
//     (Init, Run, sht3x_read, …) so the file on disk follows the IDS
//     naming convention exactly: Init.en.md, Run.en.md,
//     sht3x_read.en.md.
//
// Keeping label ≠ basename means the disk-side filenames don't
// drift from the spec when the UI vocabulary changes; only the
// dropdown gets retranslated.
function _collectNameOptions(parsed) {
    const out = [];
    out.push({ label: 'main menu text', basename: 'readme' });
    // Go: initialiser + named methods.
    if (parsed.init) {
        out.push({ label: 'Device Init', basename: 'Init' });
    }
    if (Array.isArray(parsed.methods)) {
        for (const m of parsed.methods) {
            if (m && m.name) {
                out.push({ label: 'Device ' + m.name, basename: m.name });
            }
        }
    }
    // C99: standalone function devices. Same shape as methods in the
    // dropdown — a "Device <name>" label with the bare name as the
    // disk basename — so the rest of the modal (_computeOccupancy,
    // order picker, language picker) works without further changes.
    if (Array.isArray(parsed.functions)) {
        for (const fn of parsed.functions) {
            if (fn && fn.name) {
                out.push({ label: 'Device ' + fn.name, basename: fn.name });
            }
        }
    }
    return out;
}

// _computeOccupancy returns { basenameLowercased: { lang: count } }
// reflecting how many files exist for each (basename, language)
// family. The order dropdown uses count to offer positions
// [0, 1, …, count].
//
// We index by lowercase basename because the IDS spec is case-
// insensitive on method names, but the parser preserves source
// case. A method named "Init" with files at "Init.en.md" produces
// occupancy["init"]["en"] = 1.
//
// The language key is the file's lang segment as-is; the create
// modal canonicalises before lookup.
function _computeOccupancy(files, names) {
    // Build a basename set first so we don't waste effort on files
    // that belong to families we won't show.
    const wanted = new Set(names.map(n => n.basename.toLowerCase()));
    const out = {};
    for (const n of names) out[n.basename.toLowerCase()] = {};

    // Path patterns to recognise:
    //   <basename>.<lang>.md       — position 0
    //   <basename>.<N>.<lang>.md   — position N (N >= 1, no leading 0)
    for (const f of files || []) {
        if (!f || !f.path || !f.path.endsWith('.md')) continue;
        const stem = f.path.slice(0, -3); // strip ".md"
        const parts = stem.split('.');
        // valid shapes: [base, lang]   (parts.length === 2, position 0)
        //               [base, N, lang] (parts.length === 3, position N)
        if (parts.length < 2 || parts.length > 3) continue;
        const base = parts[0].toLowerCase();
        if (!wanted.has(base)) continue;
        let lang;
        if (parts.length === 2) {
            lang = parts[1];
        } else {
            // parts[1] must be a positive integer with no leading zero,
            // matching server-side classifyHelpPath. Anything else is
            // a foreign filename we ignore.
            const mid = parts[1];
            if (!/^[1-9][0-9]*$/.test(mid)) continue;
            lang = parts[2];
        }
        const family = out[base];
        family[lang] = (family[lang] || 0) + 1;
    }
    return out;
}

// _promptNewMarkdownModal renders the create-file modal and
// resolves to { basename, language, position } or null on cancel.
//
// Inputs:
//   - names:            [{ label, basename }] from _collectNameOptions
//   - occupancy:        map produced by _computeOccupancy; drives the
//                       order dropdown so the user sees a live count
//   - bcp47Tags:        array of strings backing the language
//                       autocomplete (filtered prefix-match-first,
//                       substring as fallback)
//   - prefillBasename:  optional basename string. When supplied, the
//                       <option> whose names[i].basename matches is
//                       pre-selected. Used by the wizard's "Add help"
//                       deep-links — Struct → "readme", Method → bare
//                       method name. Falls through silently when the
//                       basename is missing or absent from `names` so
//                       the dropdown's natural first option wins.
//
// The modal has four controls:
//
//   1. name (select)        — chosen from `names`
//   2. order (select)       — [0, 1, …, count] given (basename, language)
//   3. language (input + custom AC dropdown) — see acRender et al.
//   4. type (select)        — locked to markdown (kept for future types)
//
// The order dropdown rebuilds whenever name OR language changes —
// the count depends on both axes.
function _promptNewMarkdownModal({ names, occupancy, bcp47Tags, prefillBasename }) {
    return new Promise((resolve) => {
        const overlay = document.createElement('div');
        overlay.className = 'hf-ai-help-overlay';

        // Resolve the prefill index. When prefillBasename matches a
        // names[i].basename (case-sensitive — `_collectNameOptions`
        // preserves source case), select that <option>; otherwise let
        // the natural first option win. Comparison is exact-string
        // because the wizard always passes one of the canonical
        // basenames the dropdown was built from.
        let prefillIdx = 0;
        if (prefillBasename) {
            const idx = names.findIndex(n => n.basename === prefillBasename);
            if (idx >= 0) prefillIdx = idx;
        }

        const optionsHTML = names.map((n, i) =>
            `<option value="${i}"${i === prefillIdx ? ' selected' : ''}>${_escAttr(n.label)}</option>`
        ).join('');

        overlay.innerHTML = `
            <div class="hf-ai-help-modal" style="max-width:480px">
                <header>
                    <h3>Create new file</h3>
                    <button class="hf-btn hf-btn-ghost" data-cancel
                            title="Close"><i class="fa-solid fa-xmark"></i></button>
                </header>
                <div class="hf-ai-help-body">
                    <p style="margin:0 0 12px">File name (without extension):</p>
                    <div class="hf-form-row">
                        <label for="hf-new-name">name:</label>
                        <select id="hf-new-name">${optionsHTML}</select>
                    </div>
                    <div class="hf-form-row">
                        <label for="hf-new-order">order:</label>
                        <select id="hf-new-order"></select>
                    </div>
                    <div class="hf-form-row">
                        <label for="hf-new-lang">language:</label>
                        <div class="hf-ac-wrap"
                             style="position:relative;flex:1;min-width:0">
                            <input id="hf-new-lang" type="text" value="en"
                                   autocomplete="off" spellcheck="false"
                                   placeholder="e.g. en, pt-br, zh-cn"
                                   style="width:100%;box-sizing:border-box">
                        </div>
                    </div>
                    <div class="hf-form-row">
                        <label for="hf-new-type">type:</label>
                        <select id="hf-new-type" disabled>
                            <option value="md" selected>markdown</option>
                        </select>
                    </div>
                </div>
                <footer>
                    <button class="hf-btn hf-btn-ghost" data-cancel>Cancel</button>
                    <button class="hf-btn hf-btn-primary" data-ok>OK</button>
                </footer>
            </div>
        `;
        document.body.appendChild(overlay);

        const nameEl  = overlay.querySelector('#hf-new-name');
        const orderEl = overlay.querySelector('#hf-new-order');
        const langEl  = overlay.querySelector('#hf-new-lang');

        // The autocomplete dropdown is portal-ed to document.body
        // (instead of nesting under .hf-ac-wrap) because the parent
        // modal sets `overflow: hidden` to clip its body's scroll
        // region. An absolutely-positioned child inside that modal
        // gets cropped at the modal's border — the dropdown would
        // disappear after a few items. Mounting on body and using
        // `position: fixed` lets the popup escape the clip and float
        // over both modals (the create-file one and any wizard modal
        // behind it).
        //
        // The element is created here (not in the overlay's HTML)
        // and removed in cleanup() so we don't leak DOM nodes if the
        // user opens/closes the prompt repeatedly. acReposition() is
        // called whenever the popup is shown to align it under the
        // input — it also runs on window resize while open.
        const acList = document.createElement('ul');
        acList.id = 'hf-new-lang-ac';
        acList.setAttribute('style', `
            display:none;position:fixed;
            margin:0;padding:4px 0;list-style:none;
            background:var(--bg-card,#1e1e2e);
            color:var(--text-primary,#cdd6f4);
            border:1px solid var(--border,#444);
            border-radius:6px;
            box-shadow:0 4px 12px rgba(0,0,0,0.35);
            z-index:10002;
            max-height:240px;overflow-y:auto;
            font-size:13px;font-family:ui-monospace,monospace
        `.replace(/\s+/g, ' ').trim());
        // z-index 10002 is one above the .hf-ai-help-overlay rule
        // (10001) which is itself above .wiz-modal-backdrop (10000).
        // The dropdown needs to render ON TOP of all of them — it's
        // the foreground UI the user is interacting with. Without
        // this, the overlay's translucent backdrop renders over the
        // dropdown and dims it (looks like the popup "fell through"
        // the modal).
        document.body.appendChild(acList);

        // Position the popup directly under the language input,
        // matching its width. We measure on every show because the
        // modal can be repositioned (window resize) or scrolled
        // (rare for our overlay, but cheap to handle). When there's
        // not enough room below, flip above — protects against
        // small viewports where the dropdown would otherwise spill
        // off the bottom of the screen.
        //
        // POPUP_MAX_H mirrors the CSS max-height so the flip
        // decision uses the rendered height the user will actually
        // see. Reading scrollHeight before the layout flush gives
        // unreliable values (we hit a bug where flip-up was selected
        // erroneously and the popup landed near the page bottom);
        // a constant matched to the CSS removes the race entirely.
        const POPUP_MAX_H = 240;
        const acReposition = () => {
            const r = langEl.getBoundingClientRect();
            const spaceBelow = window.innerHeight - r.bottom;
            const spaceAbove = r.top;
            acList.style.left  = r.left  + 'px';
            acList.style.width = r.width + 'px';
            if (spaceBelow >= POPUP_MAX_H + 8 || spaceBelow >= spaceAbove) {
                // Below: anchor top under the input.
                acList.style.top    = (r.bottom + 2) + 'px';
                acList.style.bottom = 'auto';
            } else {
                // Flip up: anchor the popup's bottom edge to just
                // above the input so it grows upward. Use top:auto
                // explicitly so the prior placement doesn't bleed
                // through (style isn't reset between calls).
                acList.style.top    = 'auto';
                acList.style.bottom = (window.innerHeight - r.top + 2) + 'px';
            }
        };

        // Re-position on viewport changes while the popup is open.
        // Using a single shared listener keeps things tidy; we add
        // it on first show and remove it in cleanup().
        const onViewportChange = () => {
            if (acList.style.display !== 'none') acReposition();
        };
        window.addEventListener('resize', onViewportChange);
        window.addEventListener('scroll', onViewportChange, true); // capture for nested scrollers

        // Rebuild the order dropdown given the currently-selected
        // name + language. Pos count is _occupancy[basename][lang]
        // (or 0 if the family has no files yet); valid positions
        // are [0, 1, …, count].
        const refreshOrder = () => {
            const idx = parseInt(nameEl.value, 10) || 0;
            const basename = names[idx]?.basename || '';
            // Lowercase the lang for occupancy lookup so the count
            // matches files-on-disk (which the parser also indexes
            // lowercased). The user might still be mid-typing in
            // mixed case; lowercasing here is purely a lookup detail.
            const lang = (langEl.value || '').trim().toLowerCase();
            const fam = occupancy[basename.toLowerCase()] || {};
            const count = fam[lang] || 0;
            const opts = [];
            for (let p = 0; p <= count; p++) {
                opts.push(`<option value="${p}">${p}</option>`);
            }
            orderEl.innerHTML = opts.join('');
            // Default to the next-free slot (= count). Selecting 0
            // when the slot is occupied is a deliberate user choice
            // (cascade rename) — making it the default would
            // surprise the casual user.
            orderEl.value = String(count);
        };

        // ── Custom autocomplete for the language input ──────────────
        //
        // We rolled our own dropdown instead of using <datalist>
        // because Chrome's native <datalist> popup, when rendered
        // inside an overlay modal, sometimes "freezes" — the popup
        // shows the initial filtered list and stops re-filtering as
        // the user types. The behaviour was reproducible and
        // browser-version-dependent. A custom dropdown gives us the
        // exact behaviour we want: prefix-match first, substring as
        // fallback, keyboard navigation, click-to-select.
        //
        // State machine:
        //   - Closed: acList.style.display === 'none'.
        //   - Open:   acList.style.display === 'block'; acHighlight
        //             holds the active <li> index (or -1 when none).
        const tags = bcp47Tags || [];
        const AC_MAX_VISIBLE = 50; // cap rendered <li>s; long lists are slow
        let acHighlight = -1;
        let acItems = []; // currently-rendered subset of `tags`

        const acRender = () => {
            // Filter: prefix match first (most expected behaviour),
            // fall back to substring match when nothing prefixes —
            // helps the user find "az" by typing "uz" etc. Empty
            // input shows the whole capped list so the user can
            // browse.
            const q = (langEl.value || '').trim().toLowerCase();
            let filtered;
            if (q === '') {
                filtered = tags.slice(0, AC_MAX_VISIBLE);
            } else {
                const prefix = tags.filter(t => t.startsWith(q));
                filtered = (prefix.length > 0)
                    ? prefix
                    : tags.filter(t => t.includes(q));
                filtered = filtered.slice(0, AC_MAX_VISIBLE);
            }
            acItems = filtered;
            acHighlight = filtered.length > 0 ? 0 : -1;
            if (filtered.length === 0) {
                acList.style.display = 'none';
                return;
            }
            // Inline styles for each <li> so we don't depend on
            // help_files.css having matching rules; the modal must
            // work even if the stylesheet fails to load.
            acList.innerHTML = filtered.map((t, i) => `
                <li data-idx="${i}"
                    style="padding:6px 12px;cursor:pointer;
                           ${i === acHighlight
                             ? 'background:var(--accent,#3a3a55)'
                             : ''}">${_escAttr(t)}</li>
            `).join('');
            acList.style.display = 'block';
            // Reposition AFTER setting display:block — the height
            // measurement needs the element to be in flow so we can
            // decide between flip-up and flip-down placement.
            acReposition();
        };

        const acHighlightSync = () => {
            // Light re-style of <li>s without rebuilding the list.
            // Faster and keeps scroll position intact when the user
            // arrows through items.
            const lis = acList.querySelectorAll('li');
            lis.forEach((li, i) => {
                li.style.background = (i === acHighlight)
                    ? 'var(--accent,#3a3a55)'
                    : '';
            });
            if (acHighlight >= 0 && lis[acHighlight]) {
                lis[acHighlight].scrollIntoView({ block: 'nearest' });
            }
        };

        const acClose = () => {
            acList.style.display = 'none';
            acHighlight = -1;
        };

        const acAccept = (idx) => {
            if (idx < 0 || idx >= acItems.length) return;
            langEl.value = acItems[idx];
            acClose();
            refreshOrder();
        };

        // 'input' fires on every keystroke — re-filter and refresh
        // both the dropdown and the order count.
        langEl.addEventListener('input', () => {
            acRender();
            refreshOrder();
        });

        // Open the dropdown when the user CLICKS the field. We
        // deliberately use 'click' (not 'focus') because the modal
        // gives the input programmatic focus on open when the wizard
        // pre-fills a basename — and a programmatic focus() call
        // does NOT fire 'click', so the dropdown stays closed at
        // mount time. The user opts in by clicking. Other entry
        // paths into the dropdown stay covered:
        //   - typing a character        → 'input' handler above
        //   - ArrowDown while focused   → 'keydown' handler below
        // Tab-into-the-field does not open the dropdown either,
        // which matches the user's stated UX preference ("só abrir
        // quando o usuário interagir").
        langEl.addEventListener('click', acRender);

        // Close on blur with a short delay so a click on a <li>
        // beats the close (mousedown fires before blur, but the
        // <li>'s click handler runs after focus moves).
        langEl.addEventListener('blur', () => {
            setTimeout(acClose, 150);
        });

        // Keyboard navigation. ArrowUp/Down move highlight, Enter
        // accepts, Escape closes the dropdown (without closing the
        // outer modal).
        langEl.addEventListener('keydown', (e) => {
            if (acList.style.display === 'none') {
                if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    acRender(); // open with current filter
                }
                return;
            }
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                if (acHighlight < acItems.length - 1) {
                    acHighlight++;
                    acHighlightSync();
                }
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                if (acHighlight > 0) {
                    acHighlight--;
                    acHighlightSync();
                }
            } else if (e.key === 'Enter') {
                if (acHighlight >= 0) {
                    // Intercept Enter only when an item is highlighted.
                    // Without an item, fall through to the global
                    // Enter handler which submits the form.
                    e.preventDefault();
                    e.stopPropagation();
                    acAccept(acHighlight);
                }
            } else if (e.key === 'Escape') {
                // Close the dropdown only — don't bubble up so the
                // modal-level Escape handler doesn't also fire and
                // close the whole modal.
                e.stopPropagation();
                acClose();
            }
        });

        // Mousedown (not click) so the selection beats the input's
        // blur-then-close race. preventDefault keeps focus on the
        // input so the user can continue typing if they change their
        // mind.
        acList.addEventListener('mousedown', (e) => {
            const li = e.target.closest('li[data-idx]');
            if (!li) return;
            e.preventDefault();
            acAccept(parseInt(li.dataset.idx, 10));
        });

        // Hover highlight tracks the mouse — feels native and avoids
        // confusion when the user mixes keyboard and mouse navigation.
        acList.addEventListener('mousemove', (e) => {
            const li = e.target.closest('li[data-idx]');
            if (!li) return;
            const idx = parseInt(li.dataset.idx, 10);
            if (idx !== acHighlight) {
                acHighlight = idx;
                acHighlightSync();
            }
        });

        nameEl.addEventListener('change', refreshOrder);
        refreshOrder();
        // When opening fresh from the toolbar, focus the name picker
        // so the user can immediately type-to-search the dropdown.
        // When deep-linked from the wizard's "Add help" button the
        // name is already decided — focus the language box instead so
        // the user lands on the next decision they actually need to
        // make (idioma). Saves one tab keypress in the common path.
        if (prefillBasename) {
            langEl.focus();
            langEl.select();
        } else {
            nameEl.focus();
        }

        const cleanup = (value) => {
            overlay.remove();
            // Tear down the portal-ed autocomplete dropdown and the
            // viewport listeners that drove its repositioning.
            // Without this we'd leak a <ul> in document.body and a
            // pair of window listeners for every prompt cycle.
            acList.remove();
            window.removeEventListener('resize', onViewportChange);
            window.removeEventListener('scroll', onViewportChange, true);
            document.removeEventListener('keydown', onKey);
            resolve(value);
        };

        const submit = () => {
            const idx = parseInt(nameEl.value, 10);
            const sel = names[idx];
            if (!sel) {
                toast('error', 'Please pick a name.');
                return;
            }
            // Lowercase the BCP 47 tag at submit time so the filename
            // grammar stays canonical regardless of what the user
            // typed. The bcp47-tags.json datalist also ships entirely
            // in lowercase — this is the safety net for free-form
            // input ("PT-BR" pasted from somewhere → "pt-br"). The
            // backend's filename regex matches lowercase only, so
            // keeping the wire form lowercase avoids a future mismatch
            // between the file on disk and what the parser will
            // recognise after a publish.
            const lang = (langEl.value || '').trim().toLowerCase();
            if (!lang) {
                toast('error', 'Language is required.');
                return;
            }
            // Cheap client-side syntax pre-check. The server
            // re-validates with golang.org/x/text/language and is
            // the source of truth, but rejecting "@!" here saves a
            // round trip. Pattern is now lowercase-only since we
            // already canonicalised above.
            if (!/^[a-z]{2,3}(-[a-z0-9]{2,8})*$/.test(lang)) {
                toast('error', 'Language must be a BCP 47 tag (e.g. en, pt-br).');
                return;
            }
            const position = parseInt(orderEl.value, 10);
            if (Number.isNaN(position) || position < 0) {
                toast('error', 'Please pick an order.');
                return;
            }
            cleanup({
                basename: sel.basename,
                language: lang,
                position,
            });
        };

        overlay.querySelectorAll('[data-cancel]').forEach(b => b.onclick = () => cleanup(null));
        overlay.querySelector('[data-ok]').onclick = submit;
        overlay.onclick = (e) => { if (e.target === overlay) cleanup(null); };

        const onKey = (e) => {
            if (e.key === 'Enter' && overlay.contains(document.activeElement)) {
                e.preventDefault();
                submit();
            } else if (e.key === 'Escape') {
                e.preventDefault();
                cleanup(null);
            }
        };
        document.addEventListener('keydown', onKey);
    });
}

// _escAttr is a tiny HTML attribute-value escape used by the modal
// when interpolating user-data into option labels and datalist
// values. The wider help_files.js code uses framework-free DOM
// building elsewhere; for the limited innerHTML chunks above this
// keeps the data path safe without dragging in a sanitizer.
function _escAttr(s) {
    return String(s == null ? '' : s)
        .replace(/&/g, '&amp;')
        .replace(/"/g, '&quot;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');
}

function _actionUploadImage() {
    // Hidden <input type="file"> — the click() trick gives us native
    // file picker without a permanent DOM artifact.
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/png, image/jpeg, image/svg+xml, image/gif, image/webp';
    input.onchange = async () => {
        const file = input.files?.[0];
        if (!file) return;

        // Sanitised filename (server's path validator mirrors this regex).
        const safeName = file.name.replace(/[^A-Za-z0-9._-]/g, '_');

        // Routing rule (matches the IDE WASM convention):
        //
        //   examples/<file>  —  ONLY for PNG screenshots that carry the
        //                       IoTMaker steganographic scene header
        //                       ("IOTM" magic in the LSBs of the first
        //                       pixels). These PNGs double as scene
        //                       loaders inside the WASM IDE.
        //
        //   <file>           —  Everything else (illustrative PNGs, SVGs,
        //                       JPGs, GIFs, WebP) lives at the project
        //                       root alongside readme / per-method
        //                       markdown.
        //
        // Detection runs client-side via canvas pixel inspection — no
        // round-trip needed.

        // First gate: only PNGs can possibly carry an IOTM header.
        // Use BOTH the MIME type AND the extension so the rule survives
        // browsers that report quirky types (Safari has been known to
        // hand "" as the type for SVGs dropped from Finder; some Linux
        // setups report "application/octet-stream" for any image).
        // The conjunction means "png MIME AND .png extension" — a SVG
        // that is somehow reported as image/png is still kept out of
        // examples/ by its extension. A PNG with the wrong MIME but
        // correct extension is still kept out, which is fine: a user
        // mis-tagging a file shouldn't leak into the IDE's stego pool.
        const ext = safeName.toLowerCase().slice(safeName.lastIndexOf('.'));
        const looksLikePNG = file.type === 'image/png' && ext === '.png';

        let inExamples = false;
        if (looksLikePNG) {
            try {
                inExamples = await _hasIOTMHeader(file);
            } catch (e) {
                // Decoding failed (corrupt PNG, blocked by browser).
                // Treat as a regular illustrative PNG and put it at the
                // root — safer than dumping it into examples/ where the
                // IDE WASM would later try to extract a scene from it.
                console.warn('IOTM detection failed; treating as plain PNG:', e);
                inExamples = false;
            }
        }

        // Diagnostic log — keeps the routing decision auditable from
        // the browser console without firing off an alert. If a future
        // file is mis-routed, this log tells us whether the file was
        // even classified as a PNG and whether the IOTM check ran.
        // Cheap; one line per upload.
        console.log('[help_files] upload routing', {
            name: file.name,
            type: file.type,
            ext,
            looksLikePNG,
            inExamples,
            target: inExamples ? `examples/${safeName}` : safeName,
        });

        const path = inExamples ? `examples/${safeName}` : safeName;

        const r = await _saveFile(path, file);
        if (r.status !== 200) {
            const err = r.json?.metadata?.error || `HTTP ${r.status}`;
            if (r.status === 413) {
                const meta = r.json?.metadata || {};
                toast('error',
                    `${meta.scope === 'user' ? 'User' : 'Project'} quota exceeded ` +
                    `(${_fmtBytes(meta.used)} / ${_fmtBytes(meta.limit)})`);
            } else if (r.status === 415) {
                toast('error', 'Unsupported image type.');
            } else {
                toast('error', `Upload failed: ${err}`);
            }
            return;
        }
        try {
            await _refreshList();
            _renderTree();
            await _selectFile(path);
            const where = inExamples ? 'examples/' : 'root';
            toast('success', `Uploaded to ${where}`);
        } catch (e) {
            console.error('post-upload UI failed:', e);
            toast('error', `Uploaded to server, but the editor could not open (${e?.message || e}). Refresh the file manager.`);
        }
    };
    input.click();
}

// _hasIOTMHeader checks whether a PNG file contains an IoTMaker
// steganographic scene header. Returns true only for screenshots
// produced by the IDE's "Save Stage as Image" feature, which embeds
// gzipped scene JSON in the LSBs of the first pixels.
//
// Header layout (steganography/stego.go):
//
//   bytes 0-3 : "IOTM"   (magic)
//   byte    4 : version  (1)
//   byte    5 : flags
//   bytes 6-9 : payload length (uint32 BE)
//
// We only need to read the first 4 bytes (the magic marker) to make
// the routing decision, but reading 10 (a full header) keeps the code
// symmetric with the Go extractor and gives us an obvious extension
// point if we ever want to surface the version number in the UI.
//
// Decoding is done via a hidden canvas: load the file as an Image,
// draw it, getImageData, walk the LSB of R,G,B for the first 27
// pixels (27 * 3 = 81 bits ≥ 80 bits = 10 bytes).
async function _hasIOTMHeader(file) {
    // Defensive caps: a 1x1 transparent PNG cannot carry a 10-byte
    // header (needs ≥ 27 pixels of capacity), so skip the round-trip.
    if (file.size < 100) return false;

    const blobUrl = URL.createObjectURL(file);
    try {
        const img = await new Promise((resolve, reject) => {
            const i = new Image();
            i.onload  = () => resolve(i);
            i.onerror = () => reject(new Error('PNG decode failed'));
            i.src = blobUrl;
        });

        if (img.naturalWidth * img.naturalHeight < 27) return false;

        // Draw onto an offscreen canvas of just the size we need to
        // read — 27 pixels in a single row is enough, and keeps the
        // memory footprint trivial. We crop from the source's top-left
        // because the embed routine writes from pixel 0 forward.
        const canvas = document.createElement('canvas');
        canvas.width = Math.min(img.naturalWidth, 32);
        canvas.height = 1;
        const ctx = canvas.getContext('2d', { willReadFrequently: false });
        if (!ctx) return false;
        ctx.drawImage(img, 0, 0);
        const { data } = ctx.getImageData(0, 0, canvas.width, 1);

        // Walk LSBs of R, G, B (skip A) to reconstruct the first 4
        // bytes (32 bits) of the header. 4 bytes × 8 bits = 32 bits.
        // Each pixel contributes 3 bits, so we need 11 pixels (33 bits
        // available; we use the first 32).
        const PIXELS_FOR_MAGIC = 11;
        if (canvas.width < PIXELS_FOR_MAGIC) return false;

        let bitIdx = 0;
        const magicBytes = [0, 0, 0, 0];
        for (let p = 0; p < PIXELS_FOR_MAGIC && bitIdx < 32; p++) {
            const base = p * 4;     // RGBA stride
            for (let ch = 0; ch < 3 && bitIdx < 32; ch++) {
                const bit = data[base + ch] & 1;
                const byteIdx = bitIdx >> 3;
                const bitInByte = 7 - (bitIdx & 7);     // big-endian
                magicBytes[byteIdx] |= bit << bitInByte;
                bitIdx++;
            }
        }
        // ASCII codes for "IOTM": 0x49, 0x4F, 0x54, 0x4D
        return magicBytes[0] === 0x49 && magicBytes[1] === 0x4F &&
               magicBytes[2] === 0x54 && magicBytes[3] === 0x4D;
    } finally {
        URL.revokeObjectURL(blobUrl);
    }
}

function _actionShowAISVGHelp() {
    // Custom mini-modal — showConfirm/showDialog from utils.js both
    // escape their message (which is the right default for safety),
    // but we want formatted prose here. So we render a small overlay
    // ourselves. v1 keeps the copy minimal; Slice 3 may evolve it
    // into a richer document if the team has more guidance to share.
    const overlay = document.createElement('div');
    overlay.className = 'hf-ai-help-overlay';
    overlay.innerHTML = `
        <div class="hf-ai-help-modal">
            <header>
                <h3>Generating SVG illustrations with AI</h3>
                <button class="hf-btn hf-btn-ghost" data-close
                        title="Close"><i class="fa-solid fa-xmark"></i></button>
            </header>
            <div class="hf-ai-help-body">
                <p>Modern AI assistants can produce SVG diagrams from a text
                prompt. A few tips for getting clean output:</p>
                <ol>
                    <li>Ask for a <strong>plain SVG</strong> (no embedded raster, no scripts).</li>
                    <li>Specify a <strong>viewBox</strong> like <code>0 0 600 400</code> — keeps the file scalable.</li>
                    <li>Describe shapes by purpose ("a microcontroller as a green rectangle with two pin headers"), not by colour codes.</li>
                    <li>Ask the assistant to <strong>label connection points</strong> with <code>id="conn-..."</code> so the IoTMaker interactive diagram spec can pick them up.</li>
                    <li>Save the result as <code>diagram.svg</code> at the project root, or under <code>examples/</code> for example illustrations.</li>
                </ol>
                <p>See <code>docs/INTERACTIVE_DIAGRAM_SPEC.md</code> for the full conventions.</p>
            </div>
            <footer>
                <button class="hf-btn hf-btn-primary" data-close>Got it</button>
            </footer>
        </div>
    `;
    document.body.appendChild(overlay);
    const dismiss = () => overlay.remove();
    overlay.querySelectorAll('[data-close]').forEach(b => b.onclick = dismiss);
    overlay.onclick = (e) => { if (e.target === overlay) dismiss(); };
}

// =============================================================================
//  Helpers
// =============================================================================

function _escape(s) {
    return String(s).replace(/[&<>"']/g, c => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
}

function _fmtBytes(n) {
    if (typeof n !== 'number' || isNaN(n)) return '—';
    if (n < 1024)         return `${n} B`;
    if (n < 1024 * 1024)  return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
