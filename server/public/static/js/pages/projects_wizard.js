// server/public/static/js/pages/projects_wizard.js — Wizard tab for the Projects page.
//
// Why this file exists
// ====================
//
// The wizard is the assisted-creation flow: users open a project, see
// each struct/method as a card with rows for fields/ports, and click
// rows to open modals (slice 4–5) that emit typed edits sent to
// /wizard/rewrite. The cards use the `incomplete[]` set returned by
// /parse and /rewrite to render ⚠ badges — no client-side recomputation.
//
// We keep the wizard isolated from `projects.js` (which is already
// 6028 lines) so the file does not balloon and so the wizard's state
// machine stays readable in one screen. Exported functions are
// re-exported from `projects.js` and bridged to `window` so the
// existing global-onclick HTML attributes resolve.
//
// State lifecycle
// ===============
//
//   First open (404 from /wizard/draft):
//     1. Read the current Monaco source.
//     2. POST /wizard/parse → { parsed, incomplete }.
//     3. Render cards. Save draft (POST /wizard/draft) so subsequent
//        opens of this tab restore the same view without re-parsing.
//
//   Subsequent open (200 from /wizard/draft):
//     1. Restore source into Monaco, set `_state` from the draft.
//     2. Render cards directly — server already has parsed+incomplete.
//
//   Edit source toggle:
//     1. Monaco becomes editable, cards panel grays out, sticky banner
//        tells the user "Re-parse when done".
//     2. On Parse: POST /wizard/parse, refresh state, re-enable cards.
//
//   Card row click (slice 3 placeholder):
//     1. Toast "Modal coming in slice 4-5". The path that would have
//        opened the modal is logged to console for slice 4 wiring.
//
// Draft saves are debounced at 5s. Saving on every parse/rewrite would
// hammer Redis+SQLite for no benefit — users typically pause between
// edits. Five seconds covers the "panic-close-the-tab" case while not
// turning every keystroke into a write.
//
// API surface
// ===========
//
//   projWizardOpen(projectId)              — called by projSetTab('wizard') on first open
//   projWizardOnRowClick(path)             — called by inline onclick on every clickable row
//   projWizardOnEditorParseSuccess(...)    — bridge: when projParse runs successfully, this
//                                              brings the wizard view in sync. Saves a
//                                              redundant /parse call when the user toggled
//                                              into the wizard right after a Parse.
//
// All three are also assigned to `window` at the bottom so the inline
// `onclick` HTML attributes work.

import { api } from '../api.js';
import { toast } from '../utils.js';
import { FA_ALL } from '../lib/icons.js';
import { openHelpFiles } from './help_files.js';

// =============================================================================
//  Constants
// =============================================================================

// Save debounce window. Five seconds is long enough that quick
// successive parses (the user clicking Parse twice while figuring out
// a syntax error) collapse into one save, but short enough that
// closing the tab right after a successful parse still persists the
// state.
const _DRAFT_SAVE_DEBOUNCE_MS = 5000;

// Native Go types the wizard's modals can render. Mirrored from the
// server's `IsNativePropType` in `server/codegen/blackbox/completion.go`
// — keep the two lists in sync. Used here only to dim non-native
// rows; the server's completion set already filters them out, so this
// is purely a visual cue.
const _NATIVE_TYPES = new Set([
    'bool', 'byte', 'rune', 'string',
    'int', 'int8', 'int16', 'int32', 'int64',
    'uint', 'uint8', 'uint16', 'uint32', 'uint64',
    'float32', 'float64',
]);

// =============================================================================
//  Module state
// =============================================================================

// Per-open state. Reset by projWizardOpen on each tab activation so
// switching between projects (or rapidly between Editor/Wizard tabs)
// doesn't leak data across boundaries.
let _state = {
    projectId: null,
    code: '',          // full Go source
    parsed: null,      // BlackBoxDef (object form)
    incomplete: [],    // []string of dotted paths
    _saveTimer: null,  // setTimeout handle for the debounced save
    _opened: false,    // flag — true after first successful open() so
                       // projWizardOpen doesn't re-run on every tab toggle
};

// =============================================================================
//  Public API
// =============================================================================

// projWizardOpen is called by `projSetTab('wizard')` in projects.js the
// first time the user clicks the Wizard tab. Loads the draft from the
// server (or initialises one from the current Monaco source) and
// populates the tab body. Subsequent tab toggles re-run this — guarded
// by `_state._opened` so a quick toggle does not refetch.
export async function projWizardOpen(projectId) {
    if (_state._opened && _state.projectId === projectId) {
        // Already opened for this project. Two possibilities:
        //
        //  1. User toggled away to Editor/Preview and came back without
        //     editing. The state is current — just re-render.
        //
        //  2. User edited the source on the Editor tab (or via the
        //     wizard's own "edit source" toggle) and came back. The
        //     parsed state is now stale. We must re-parse before
        //     rendering, otherwise the cards reflect the previous
        //     source — which is exactly what hides freshly-introduced
        //     incompleteness like a removed `label:` / `icon:`
        //     directive.
        //
        // Distinguish by comparing the Monaco source to what we
        // hydrated with last. Whitespace-only differences are not
        // worth a network round-trip.
        const liveSource = _readMonacoSource();
        if (liveSource.trim() !== (_state.code || '').trim()) {
            _renderLoading();
            // The language token comes from projects.js — see the
            // window._projGetLanguage bridge it installs. The wizard
            // module never imports projects.js directly (would be a
            // cycle), so we read through the window namespace.
            // Default to "go" when the bridge isn't loaded yet (very
            // early page-init race; the server defaults to "go" too,
            // so the behaviour stays identical to pre-C99).
            const lang = (typeof window !== 'undefined' && window._projGetLanguage)
                ? window._projGetLanguage()
                : 'go';
            const parseResponse = await api(
                'POST',
                '/api/v1/blackbox/wizard/parse',
                { code: liveSource, language: lang },
            );
            if (parseResponse?.metadata?.status !== 200) {
                // Parse failed — show the error in place. The toolbar
                // Parse button already reflects whatever the previous
                // analyze cycle left it in; nothing to update here.
                _renderParseError(
                    parseResponse?.metadata?.error || 'Unknown parse error',
                );
                return;
            }
            _hydrateFromParseResponse(liveSource, parseResponse.data);
            _scheduleDraftSave();
            _renderTab();
            return;
        }
        // Source unchanged — just re-render in case window was
        // resized while we were on another tab.
        _renderTab();
        return;
    }
    _resetState();
    _state.projectId = projectId;

    _renderLoading();

    // Try the draft first. 404 is the normal first-open response and
    // means we should bootstrap from the current editor source.
    const draftResponse = await api('GET', `/api/v1/blackbox/wizard/draft/${encodeURIComponent(projectId)}`);
    if (draftResponse?.metadata?.status === 200 && draftResponse.data) {
        await _hydrateFromDraft(draftResponse.data);
        _state._opened = true;
        _renderTab();
        return;
    }

    // No draft yet — bootstrap from the current Monaco source. If
    // the editor is empty (new project), show an empty state with
    // instructions to write some Go.
    const source = _readMonacoSource();
    if (!source.trim()) {
        _state.code = '';
        _state._opened = true;
        _renderEmptyState();
        return;
    }

    // Same `language` bridge as the re-parse path above. See the
    // earlier comment for why we read through window instead of
    // importing.
    const lang2 = (typeof window !== 'undefined' && window._projGetLanguage)
        ? window._projGetLanguage()
        : 'go';
    const parseResponse = await api('POST', '/api/v1/blackbox/wizard/parse',
        { code: source, language: lang2 });
    if (parseResponse?.metadata?.status !== 200) {
        // Parse failed — usually means the user has invalid source
        // (any supported language) in the editor. The wizard cannot
        // do anything until that is fixed. Show the error and offer
        // the "edit source" path so they can fix it without leaving
        // the tab.
        _state.code = source;
        _state._opened = true;
        _renderParseError(parseResponse?.metadata?.error || 'Unknown parse error');
        return;
    }

    _hydrateFromParseResponse(source, parseResponse.data);
    _state._opened = true;
    // Save the freshly bootstrapped draft so refreshing the page
    // restores this view without a re-parse.
    _scheduleDraftSave();
    _renderTab();
}

// projWizardOnRowClick dispatches to the appropriate modal based on the
// dotted path. The path grammar is documented in
// `server/codegen/blackbox/rewrite.go`'s `parsePath`:
//
//   struct.<S>                          → Struct modal (slice 4)
//   struct.<S>.field.<F>                → Field modal  (slice 4)
//   method.<S>.<M>                      → Method modal (slice 5)
//   method.<S>.<M>.{in|out}.<P>         → Port modal   (slice 5)
//
// Slice 4 ships the first two; slice 5 wires the rest. Unknown shapes
// fall through to a console warning + toast — better to surface
// "wizard doesn't know what to do" loudly than to silently swallow.
export function projWizardOnRowClick(path) {
    // Match the most specific prefix first so `struct.X.field.Y` does
    // not match `struct.X` by accident.
    if (/^struct\.[^.]+\.field\.[^.]+$/.test(path)) {
        _openFieldModal(path);
        return;
    }
    if (/^struct\.[^.]+$/.test(path)) {
        _openStructModal(path);
        return;
    }
    if (/^method\.[^.]+\.[^.]+\.(in|out)\.[^.]+$/.test(path)) {
        _openPortModal(path);
        return;
    }
    if (/^method\.[^.]+\.[^.]+$/.test(path)) {
        _openMethodModal(path);
        return;
    }
    // Slice C99-6: enum paths. Most specific (value) first so the
    // value path doesn't match the enum-level pattern by accident.
    if (/^enum\.[^.]+\.value\.[^.]+$/.test(path)) {
        _openEnumValueModal(path);
        return;
    }
    if (/^enum\.[^.]+$/.test(path)) {
        _openEnumModal(path);
        return;
    }
    // Slice C99-8: function-device paths. The synthetic `return`
    // output is editable by clicking the port itself (consistent
    // with the parameter ports color/text) — it opens a dedicated
    // return modal. Its label is persisted in the function's leading
    // comment, so that modal saves through the device path while
    // preserving the device's own directives. Port (more specific)
    // before header.
    if (/^function\.[^.]+\.out\.return$/.test(path)) {
        const fnName = path.slice('function.'.length, path.indexOf('.out.return'));
        _openFunctionReturnModal(fnName);
        return;
    }
    if (/^function\.[^.]+\.(in|out)\.[^.]+$/.test(path)) {
        _openFunctionPortModal(path);
        return;
    }
    if (/^function\.[^.]+$/.test(path)) {
        _openFunctionModal(path);
        return;
    }

    // Fatia 2: wire-type header opens the label/icon modal. (The
    // `.info` row is inert and never reaches here.)
    if (/^wiretype\.[^.]+$/.test(path)) {
        _openWireTypeModal(path);
        return;
    }

    console.warn('[wizard] unknown row-click path:', path);
    _persistentToast('danger', 'Internal: unknown wizard path ' + path);
}

// projWizardOnEditorParseSuccess is the bridge from projects.js's
// `projParse` to the wizard tab. When a Parse runs successfully and
// the wizard tab has been opened in this session, this picks up the
// fresh data so the wizard view is consistent without a separate
// /parse call.
//
// `data` is the `data` field of the /wizard/parse envelope —
// { parsed, incomplete }.
//
// Returns the number of incomplete (⚠) items in the parsed source, so the
// caller (projParse) can route a fully-correct parse straight to Preview
// and an incomplete one to the Wizard. The count is computed once here, the
// single owner of the card-aware incompleteness logic.
export function projWizardOnEditorParseSuccess(source, data) {
    // The Parse button visibility on the toolbar must stay in sync
    // with `incomplete[]` regardless of whether the user has opened
    // the Wizard tab in this session. The pending-items count is a
    // property of the parsed source, not of the wizard's UI lifecycle.
    //
    // We therefore extract incomplete[] and notify the host BEFORE
    // the early return below — the toolbar button updates on every
    // parse, and the wizard's internal state syncs only when the
    // tab is actually being used.
    const incomplete = Array.isArray(data?.incomplete) ? data.incomplete : [];
    // Count against the rendered cards, not the raw backend list —
    // the backend emits a spurious `struct.` for device-per-function
    // sources (no primary struct) and omits enum/function devices.
    const incompleteCount = projWizardCountIncomplete(data?.parsed, incomplete);
    if (typeof window.projUpdateParseBtnState === 'function') {
        window.projUpdateParseBtnState(incompleteCount);
    }

    if (!_state._opened) {
        // Wizard never opened in this session — toolbar button is
        // already updated above; nothing else to sync until the user
        // actually clicks the Wizard tab.
        return incompleteCount;
    }
    _hydrateFromParseResponse(source, data);
    _scheduleDraftSave();
    // Only re-render if the wizard tab is currently visible. Otherwise
    // the next open will pick up the fresh state.
    const tab = document.getElementById('proj-tab-wizard');
    if (tab && tab.style.display !== 'none') {
        _renderTab();
    }
    return incompleteCount;
}

// =============================================================================
//  State helpers
// =============================================================================

function _resetState() {
    if (_state._saveTimer) {
        clearTimeout(_state._saveTimer);
    }
    _state = {
        projectId: null,
        code: '',
        parsed: null,
        incomplete: [],
        _saveTimer: null,
        _opened: false,
    };
    // The toolbar Parse button is shared across projects — when we
    // reset to a fresh project (or close the editor), it should NOT
    // carry over the previous project's "pending items" count.
    // Notify the host with 0 so the button reads as enabled by
    // default until the new project's parse populates it.
    if (typeof window.projUpdateParseBtnState === 'function') {
        window.projUpdateParseBtnState(0);
    }
}

async function _hydrateFromDraft(d) {
    // The draft persists `code` AND a derived `parsed` blob. `parsed`
    // is produced from `code` by the SERVER parser, so a parser upgrade
    // (e.g. C99 gaining compatibleCallbacks for callback handlers) makes
    // a previously stored blob STALE: the wizard would render the old
    // shape even though the server binary and this JS are current, and
    // it would NOT self-correct, because the re-parse on tab switch only
    // fires when the editor source changes. `code` is the single source
    // of truth, so re-parse it here instead of trusting d.parsed. The
    // parse is cheap and guarantees _state.parsed always matches the
    // running server — no rebuild or manual edit needed after a parser
    // change.
    //
    // Português: o rascunho guarda `code` e um `parsed` DERIVADO. Como
    // `parsed` vem do parser do servidor, uma evolução do parser deixa o
    // blob gravado obsoleto e o wizard renderiza a forma antiga. `code` é
    // a única fonte da verdade — re-parseamos aqui em vez de confiar no
    // d.parsed.
    _state.code = d.code || '';

    // Restore the Monaco view first so the editor shows the draft source
    // while the (fast) re-parse runs behind the loader.
    _writeMonacoSource(_state.code);

    if (_state.code.trim()) {
        // Same `language` bridge as the other /wizard/parse call sites.
        const lang = (typeof window !== 'undefined' && window._projGetLanguage)
            ? window._projGetLanguage()
            : 'go';
        const fresh = await api(
            'POST',
            '/api/v1/blackbox/wizard/parse',
            { code: _state.code, language: lang },
        );
        if (fresh?.metadata?.status === 200 && fresh.data) {
            _state.parsed = fresh.data.parsed || null;
            _setIncomplete(Array.isArray(fresh.data.incomplete) ? fresh.data.incomplete : []);
            return;
        }
        // Re-parse failed (unusual — the source parsed cleanly enough to
        // have been saved as a draft). Fall through to the stored blob so
        // the wizard still renders something rather than erroring out.
    }

    // Empty source, or re-parse failed: use whatever the draft carried.
    _state.parsed = d.parsed || null;
    _setIncomplete(Array.isArray(d.incomplete) ? d.incomplete : []);
}

function _hydrateFromParseResponse(source, data) {
    _state.code = source;
    _state.parsed = data.parsed || null;
    _setIncomplete(Array.isArray(data.incomplete) ? data.incomplete : []);
}

// _setIncomplete assigns _state.incomplete and notifies the host
// (projects.js) so it can repaint anything that depends on the
// completeness state — currently the Parse button in the editor
// header. Centralising the assignment ensures every code path that
// updates the pending-items list goes through the same notification.
//
// We reach the host through window.projUpdateParseBtnState rather
// than importing it directly, mirroring the projSaveBackup pattern
// (see _applyEditAndClose) — this keeps the dependency one-way:
// projects.js imports the wizard module, the wizard module never
// imports projects.js.
//
// Best-effort: if the host hasn't installed the function (older
// host page, test harness, etc.) the call is a no-op. The wizard's
// own ⚠ rendering is independent of this notification.
function _setIncomplete(arr) {
    _state.incomplete = arr;
    if (typeof window.projUpdateParseBtnState === 'function') {
        window.projUpdateParseBtnState(
            projWizardCountIncomplete(_state.parsed, arr));
    }
}

// _scheduleDraftSave debounces the actual save call. Each scheduling
// resets the timer so a burst of parses during a single edit session
// collapses into one network round-trip.
function _scheduleDraftSave() {
    if (_state._saveTimer) clearTimeout(_state._saveTimer);
    _state._saveTimer = setTimeout(_saveDraft, _DRAFT_SAVE_DEBOUNCE_MS);
}

async function _saveDraft() {
    _state._saveTimer = null;
    if (!_state.projectId || !_state.parsed) {
        return;
    }
    const r = await api('POST', `/api/v1/blackbox/wizard/draft/${encodeURIComponent(_state.projectId)}`, {
        code: _state.code,
        parsed: _state.parsed,
    });
    if (r?.metadata?.status !== 200) {
        // Save failures are rare (network, 5xx) and usually require
        // the user to know they happened. Use a persistent toast
        // (no auto-dismiss) so a transient warning isn't gone before
        // the user looks. Click-to-dismiss still works.
        _persistentToast('warning',
            'Could not save wizard draft: ' + (r?.metadata?.error || 'unknown error'));
    }
}

// =============================================================================
//  Monaco bridge
// =============================================================================
//
// We deliberately do NOT import _monacoInst from projects.js because
// that would create a circular dependency. Instead we read the global
// `window._projMonacoInst` which projects.js exposes. If Monaco
// hasn't loaded yet (rare — the wizard tab cannot be opened before
// the editor view exists), we fall back to the textarea.

function _readMonacoSource() {
    const m = window._projMonacoInst;
    if (m) return m.getValue();
    return document.getElementById('proj-fallback')?.value || '';
}

// _writeMonacoSource pushes `text` into the live Monaco editor (or
// the fallback textarea when Monaco hasn't mounted yet, e.g. during
// tests). Used by every wizard code path that hydrates state into
// the editor: draft restore, parse-response hydration, rewrite-
// response hydration, and the explicit "edit source" toggle.
//
// IMPORTANT: we MUST short-circuit when the requested text already
// matches the editor's current value. The reason is subtle:
//
//   Monaco's `setValue()` dispatches `onDidChangeModelContent` even
//   when the new text is byte-identical to the old text — there is
//   no internal equality check. The host (projects.js) listens to
//   that event and, on every fire, invalidates `_parsedData` (the
//   parsed BlackBoxDef cache) on the assumption that "content
//   changed → parser output stale". Most of the time that
//   assumption holds, but on tab-switch into the wizard right after
//   a successful Parse, the wizard's draft restore writes the very
//   same source back into Monaco — invalidating `_parsedData` for
//   no good reason.
//
//   Symptom in the wild: the user clicks Parse, switches to Files
//   (or any feature consuming `_parsedData`), and gets "Please
//   parse first" even though they just parsed. Clicking Parse a
//   second time worked because subsequent wizard tab activations go
//   through the `_state._opened` fast path, which skips the draft
//   restore + setValue call.
//
// The equality check below makes the call a no-op when the editor
// is already in sync, which is the common case (draft on the
// server matches what the user sees). When the wizard genuinely
// rewrites the source (e.g. after `_hydrateFromRewriteResponse`),
// the texts differ and the setValue runs as expected — the host's
// invalidation is then correct.
function _writeMonacoSource(text) {
    const m = window._projMonacoInst;
    if (m) {
        if (m.getValue() === text) return; // no-op; preserves _parsedData
        m.setValue(text);
        return;
    }
    const ta = document.getElementById('proj-fallback');
    if (ta && ta.value !== text) ta.value = text;
}

// =============================================================================
//  Rendering
// =============================================================================

function _renderLoading() {
    const tab = document.getElementById('proj-tab-wizard');
    if (!tab) return;
    tab.innerHTML = `
        <div class="wiz-loading">
            <i class="fa-solid fa-circle-notch fa-spin"></i>
            <span>Loading wizard…</span>
        </div>`;
}

function _renderEmptyState() {
    const tab = document.getElementById('proj-tab-wizard');
    if (!tab) return;
    tab.innerHTML = `
        <div class="wiz-empty">
            <i class="fa-solid fa-wand-magic-sparkles wiz-empty-icon"></i>
            <h3>No source to wizard yet</h3>
            <p>Write some code in the <strong>Editor</strong> tab, then come back.
               The wizard parses the source and shows each struct and method
               as a card you can configure with point-and-click.</p>
        </div>`;
}

function _renderParseError(message) {
    const tab = document.getElementById('proj-tab-wizard');
    if (!tab) return;
    // Note: we deliberately avoid the cards layout here — without a
    // valid parsed def there are no cards to render. The "edit
    // source" toggle is offered so the user can fix the code in
    // place and re-Parse.
    tab.innerHTML = `
        <div class="wiz-parse-error">
            <i class="fa-solid fa-triangle-exclamation wiz-parse-error-icon"></i>
            <h3>Source has parse errors</h3>
            <pre>${esc(message)}</pre>
            <p>Fix the errors in the editor and click Parse again.</p>
            <button class="btn btn-primary" onclick="projSetTab('editor')">
                <i class="fa-solid fa-arrow-left"></i> Open Editor
            </button>
        </div>`;
}

// _renderTab paints the full wizard tab body — the 50/50 grid with
// Monaco on the left (mounted by projects.js, NOT re-mounted here)
// and the cards panel on the right. Called whenever state changes
// in a way that affects the cards.
//
// Monaco preservation: setting tab.innerHTML wipes child nodes,
// which would orphan the Monaco DOM that projects.js parented in
// here. We detach Monaco's host before the innerHTML write and
// reattach after, into the freshly-rendered #proj-wizard-monaco-host.
// The same instance is reused — no re-mount, no state loss.
function _renderTab() {
    const tab = document.getElementById('proj-tab-wizard');
    if (!tab) return;
    if (!_state.parsed) {
        _renderEmptyState();
        return;
    }

    // Pull Monaco off the tab tree before the innerHTML rewrite.
    // window._projMonacoHost is set by projects.js's _doMount.
    const monacoHost = window._projMonacoHost;
    if (monacoHost && tab.contains(monacoHost)) {
        // Park the node temporarily on document.body — invisible
        // (display:none ancestor on the tab does not apply here, but
        // Monaco's automaticLayout copes with detach/reattach).
        document.body.appendChild(monacoHost);
        monacoHost.style.display = 'none';
    }

    // Cards container. The previous slice had a toolbar with
    // "Edit source" (toggled Monaco read-only) and "Cancel wizard"
    // (deleted draft) — both removed at user request:
    //   - Editing source still works: switch to the Editor tab. The
    //     same Monaco instance lives there, writable, with the same
    //     content. No data is lost.
    //   - Discarding a draft: deleting the project removes its
    //     entry; abandoned drafts are also auto-cleaned after 30
    //     idle days by the Asynq task in server/tasks/wizard_cleanup.go.
    const cardsBlock = `
        <div class="wiz-cards">
            ${projWizardBuildCards(_state.parsed, _state.incomplete, _state.code)}
        </div>`;

    tab.innerHTML = `
        <div class="wiz-layout">
            <!--
              The left panel hosts the live Monaco editor instance —
              the same instance that the Editor tab shows. projects.js's
              projSetTab moves Monaco's host div in here when the user
              switches tabs, and back to proj-editor-wrap on return.
              The editor is read-only on the Wizard tab; users edit
              source by switching back to the Editor tab.
            -->
            <div class="wiz-monaco-side" id="proj-wizard-monaco-host"></div>
            <div class="wiz-cards-side">
                ${cardsBlock}
            </div>
        </div>`;

    // Reattach Monaco into the freshly-rendered host. The setTimeout
    // gives the browser a tick to apply layout before Monaco's own
    // resize observer kicks in.
    if (monacoHost) {
        const newHost = document.getElementById('proj-wizard-monaco-host');
        if (newHost) {
            monacoHost.style.display = '';
            newHost.appendChild(monacoHost);
            const m = window._projMonacoInst;
            if (m) {
                setTimeout(() => {
                    m.layout();
                    // Now that Monaco has measured its new container,
                    // hook up the scroll/alignment listeners and run
                    // the first alignment pass. Doing this AFTER the
                    // layout call means getTopForLineNumber returns
                    // numbers that match what's painted on screen.
                    _setupScrollSync(m);
                    _alignCards(m);
                }, 0);
            }
        }
    }
}

// =============================================================================
//  Scroll sync + vertical alignment via Monaco view zones
// =============================================================================
//
// Goal — match the mockup: each card sits next to the source line
// where its entity is declared. Cards are taller than the few lines
// of code they describe (a method might be 5 lines of Go but the
// card has a header + 4-port table = ~250px), so naive alignment
// leaves cards overlapping each other below the first one.
//
// Two approaches were considered:
//   - Move cards to align with code (works only if cards are short).
//   - Insert vertical space in the editor where cards need room.
//
// We pick the second one and use Monaco's `viewZones` API. View
// zones reserve N pixels of vertical space at a given line WITHOUT
// modifying the source — `editor.getValue()` returns the Go source
// untouched, so saves and round-trips are clean. This is exactly
// what extensions like inline-diff and git-blame use.
//
// Algorithm (per render pass):
//   1. Render cards in document flow with normal CSS (no absolute
//      positioning). Browser computes their natural heights.
//   2. Measure each card's height via getBoundingClientRect.
//   3. For each card, compute how much vertical space the source
//      naturally has between this entity's line and the next one
//      (or end of file). Compare with the card height plus a gap.
//      Shortfall, if any, becomes a `viewZone` after the entity's
//      last line.
//   4. Apply view zones in a single Monaco `changeViewZones`
//      transaction. Monaco re-paints — line Y coordinates shift
//      down by the inserted heights.
//   5. Convert each card to `position: absolute` and set
//      `top = getTopForLineNumber(line) - scrollTop`. They now sit
//      exactly at their entity's painted Y, with breathing room.
//   6. On scroll, only step 5 re-runs. View zones stay put across
//      scrolls; only re-renders re-measure and re-reserve.
//
// "Edit source" mode removes the view zones — when the user is
// typing Go, we don't want phantom blank space; the cards are
// grayed out anyway. Re-enabled on the next Parse.

// Monaco view-zone IDs we created on the last alignment pass.
// Cleared and re-applied on every alignment cycle so we never leak
// zones across renders. Module scope because the listeners installed
// on Monaco need to reach this on each scroll.
// _wheelThrottleUntil is the timestamp before which a `wheel`
// event over the cards column should be ignored. Trackpad / smooth
// mouse wheels fire many events per swipe — without throttling,
// a single swipe would jump past 5+ entities. 250 ms feels natural
// in practice (one entity per "swipe burst").
let _wheelThrottleUntil = 0;

let _viewZoneIds = [];
let _scrollSyncDisposable = null;
let _resizeObserver = null;
let _cardsWheelHandler = null;

function _setupScrollSync(m) {
    // Tear down any previous listeners — _renderTab may run multiple
    // times in a single session (after every save), and stacking
    // listeners would multiply work per scroll.
    if (_scrollSyncDisposable) {
        _scrollSyncDisposable.dispose();
        _scrollSyncDisposable = null;
    }
    if (_resizeObserver) {
        _resizeObserver.disconnect();
        _resizeObserver = null;
    }

    _scrollSyncDisposable = m.onDidScrollChange(() => _positionCards(m));

    // Re-align when the cards panel itself resizes — a window
    // resize, a font load, or an unfolded long Comment in a card
    // would shift positions otherwise. Re-alignment here means a
    // FULL recompute (re-measure cards, re-reserve view zones) not
    // just a position update — heights may have changed.
    const cardsHost = document.querySelector('.wiz-cards');
    if (cardsHost && typeof ResizeObserver !== 'undefined') {
        _resizeObserver = new ResizeObserver(() => _alignCards(m));
        _resizeObserver.observe(cardsHost);
    }

    // Wheel-on-cards → scroll the editor entity-by-entity.
    //
    // The wizard layout has the editor on the left and the cards on
    // the right. The editor is the source of truth for vertical
    // position — cards follow it via view zones. Without this
    // listener, scrolling only worked when the mouse was over the
    // editor; users naturally try to scroll where their attention
    // is (the cards), and nothing happened.
    //
    // Behaviour: each `wheel` event jumps the editor to the next
    // (deltaY > 0) or previous (deltaY < 0) entity's commentLine.
    // The existing onDidScrollChange handler then repositions the
    // cards as usual, so alignment is preserved.
    //
    // Throttling: smooth wheels (trackpads, modern mice) fire many
    // events per gesture. Without a guard a single swipe would jump
    // past 5+ entities. The throttle window resets after the last
    // event, so a slow scroll still moves smoothly through the list.
    if (cardsHost) {
        if (_cardsWheelHandler) {
            cardsHost.removeEventListener('wheel', _cardsWheelHandler);
        }
        _cardsWheelHandler = (e) => {
            // Don't hijack scroll inside an open card body — the
            // user might be reading help text or scrolling a long
            // comment. Only pin-jump from the card's outer chrome.
            // Rule: if the wheel target is inside a `.wiz-card`
            // body that has its own scrollable overflow, let the
            // browser handle it natively. Otherwise we take over.
            const card = e.target.closest('.wiz-card');
            if (card) {
                const body = card.querySelector('.wiz-card-body');
                if (body && body.scrollHeight > body.clientHeight) {
                    return; // native scroll inside the card
                }
            }

            const now = Date.now();
            if (now < _wheelThrottleUntil) {
                e.preventDefault();
                return;
            }
            _wheelThrottleUntil = now + 250;

            // Take the wheel event for ourselves — without this,
            // Chrome would scroll the cards column natively and
            // we'd end up double-scrolling.
            e.preventDefault();

            const direction = e.deltaY > 0 ? 1 : -1;
            _scrollEditorByEntity(m, direction);
        };
        // passive:false because we call preventDefault().
        cardsHost.addEventListener('wheel', _cardsWheelHandler, { passive: false });
    }
}

// _scrollEditorByEntity moves the editor's viewport to the next or
// previous entity card. The cards' commentLine values (1-based,
// pointing at the start of each entity's leading // block) are
// stored on the DOM as data-comment-line during _renderCardShell.
// We read them in document order, find the first one strictly
// after (or before) the current visible top line, and reveal it.
function _scrollEditorByEntity(m, direction) {
    if (!m) return;
    const cardsHost = document.querySelector('.wiz-cards');
    if (!cardsHost) return;

    // Build a sorted list of entity commentLines from the cards in
    // the DOM. A shallower lookup than _findEntityLines because we
    // already wrote them out in the cards' dataset.
    const cards = Array.from(cardsHost.querySelectorAll('.wiz-card[data-comment-line]'));
    const lines = cards
        .map(c => parseInt(c.dataset.commentLine, 10))
        .filter(n => !Number.isNaN(n) && n > 0)
        .sort((a, b) => a - b);
    if (lines.length === 0) return;

    // Current viewport top line — round so we don't get stuck on a
    // partially-visible entity.
    const visibleRanges = m.getVisibleRanges();
    const topLine = visibleRanges.length > 0
        ? visibleRanges[0].startLineNumber
        : 1;

    let target;
    if (direction > 0) {
        // Next entity strictly below the top of the viewport.
        target = lines.find(l => l > topLine);
        if (target === undefined) {
            // Already past the last entity — scroll to the end
            // gracefully instead of doing nothing.
            target = lines[lines.length - 1];
        }
    } else {
        // Previous entity strictly above. Walk in reverse.
        for (let i = lines.length - 1; i >= 0; i--) {
            if (lines[i] < topLine) { target = lines[i]; break; }
        }
        if (target === undefined) {
            target = lines[0];
        }
    }

    // revealLineNearTop puts the line near (but not flush with)
    // the top, leaving a few lines of context above. Matches the
    // visual rhythm of clicking an entity in a sidebar.
    m.revealLineNearTop(target);
}

// _clearViewZones removes any view zones we previously installed.
// Called on each alignment pass and when the user enters "Edit
// source" mode (so the editor goes back to natural layout for typing).
function _clearViewZones(m) {
    if (!m || _viewZoneIds.length === 0) return;
    m.changeViewZones(accessor => {
        _viewZoneIds.forEach(id => accessor.removeZone(id));
    });
    _viewZoneIds = [];
}

// _alignCards is the FULL alignment pass — re-measures cards, decides
// how much space each one needs, installs view zones to reserve that
// space, then positions cards absolutely. Called from _renderTab and
// from the ResizeObserver. Scroll uses _positionCards alone (cheap).
//
// Re-entrancy guard: this function modifies card styles, which
// triggers the ResizeObserver, which would call _alignCards again,
// which would loop forever. The `_aligning` flag short-circuits the
// nested call. ResizeObserver fires on the next macrotask anyway, so
// the outer call is the one that lands the final layout.
let _aligning = false;
function _alignCards(m) {
    if (!m) return;
    if (_aligning) return;
    const cardsHost = document.querySelector('.wiz-cards');
    if (!cardsHost) return;

    _aligning = true;
    try {
        _alignCardsInternal(m, cardsHost);
    } finally {
        // Release the flag in the next tick so the trailing
        // ResizeObserver callback (which fires asynchronously after
        // our style writes) finds the flag clear and ignores the
        // self-induced layout shift.
        setTimeout(() => { _aligning = false; }, 0);
    }
}

function _alignCardsInternal(m, cardsHost) {
    // Step 0: clear previous zones so we measure cards in their
    // natural sizes and so we don't leak zones across renders.
    _clearViewZones(m);

    // Step 1: while clearing positions, switch cards to flow so
    // getBoundingClientRect returns natural heights. The actual
    // re-flow is what step 2 reads.
    const cards = cardsHost.querySelectorAll('.wiz-card[data-line]');
    cards.forEach(card => {
        card.style.position = '';
        card.style.top = '';
        card.style.left = '';
        card.style.right = '';
    });

    if (cards.length === 0) return;

    // Step 2: collect entries — each entry has the card's line, its
    // measured height, and the line of the *next* entry (used to
    // compute available space). Sorting by line keeps adjacent
    // entries adjacent in the entries array.
    //
    // `monaco` is exposed as a global by the AMD loader that
    // projects.js bootstraps; we read its EditorOption enum
    // defensively in case the loader hasn't finished yet on a very
    // first paint (fall back to a reasonable line height).
    const monacoGlobal = window.monaco;
    const lineHeight = (monacoGlobal && m.getOption)
        ? (m.getOption(monacoGlobal.editor.EditorOption.lineHeight) || 19)
        : 19;
    const entries = Array.from(cards)
        .map(card => ({
            card,
            line: parseInt(card.dataset.line, 10),
            // commentLine is the start of the leading // block, or
            // equal to `line` when there is none. Used as the
            // boundary for the PREVIOUS entity — see the loop below.
            commentLine: parseInt(card.dataset.commentLine, 10) || parseInt(card.dataset.line, 10),
            height: card.getBoundingClientRect().height,
        }))
        .filter(e => Number.isFinite(e.line) && e.line > 0)
        .sort((a, b) => a.line - b.line);

    // Step 3: for each entry, decide how many extra pixels need
    // reserving below this entity so the next card has room without
    // overlapping. The "boundary" between this entity and the next
    // one is the next entity's commentLine — NOT its declaration
    // line. Without this distinction, view zones would be inserted
    // BETWEEN a leading comment block and its `func`, splitting
    // things that belong together.
    //
    // available  = (nextBoundary - thisLine) * lineHeight
    // needed     = thisCardHeight + gap
    // shortfall  = max(0, needed - available)
    //
    // Shortfall pixels are added as a view zone AFTER this entity's
    // last source line — which is one before the next entity's
    // commentLine.
    const totalLines = m.getModel()?.getLineCount() || 0;
    const gap = 12;
    const stackGap = 8;

    // Collapse co-linear entries into one logical entry per line,
    // summing their heights (+ the inter-card stack gap). Without
    // this, a function-group header and its Init method — both
    // anchored on the same line — would each compute their own
    // reservation against a zero-width slot and either over- or
    // under-reserve. One reservation per line, sized for the whole
    // stack, is correct.
    const lineMap = new Map();
    for (const e of entries) {
        const ex = lineMap.get(e.line);
        if (ex) {
            ex.height += e.height + stackGap;
        } else {
            lineMap.set(e.line, { line: e.line, commentLine: e.commentLine, height: e.height });
        }
    }
    const lineEntries = Array.from(lineMap.values()).sort((a, b) => a.line - b.line);

    const reservations = [];
    for (let i = 0; i < lineEntries.length; i++) {
        const e = lineEntries[i];
        // The next entity's commentLine is the first line we are NOT
        // allowed to push into. For the last entity, push to EOF.
        const nextBoundary = (i + 1 < lineEntries.length)
            ? lineEntries[i + 1].commentLine
            : (totalLines + 1);
        const lastLineOfEntity = nextBoundary - 1;
        const available = (nextBoundary - e.line) * lineHeight;
        const needed = e.height + gap;
        const shortfall = needed - available;
        if (shortfall > 0) {
            reservations.push({
                afterLineNumber: lastLineOfEntity,
                heightInPx: Math.ceil(shortfall),
            });
        }
    }

    // Step 4: install view zones in one transaction. A wider DOM
    // node is required by Monaco's API even for "invisible" zones —
    // we use an empty div.
    if (reservations.length > 0) {
        m.changeViewZones(accessor => {
            reservations.forEach(r => {
                const id = accessor.addZone({
                    afterLineNumber: r.afterLineNumber,
                    heightInPx: r.heightInPx,
                    domNode: document.createElement('div'),
                });
                _viewZoneIds.push(id);
            });
        });
    }

    // Step 5: now that Monaco has applied the new layout, position
    // cards absolutely at their entities' painted Y. Wrap in a
    // setTimeout so Monaco's internal layout has applied before
    // getTopForLineNumber returns the new (post-zones) values.
    setTimeout(() => _positionCards(m), 0);
}

// _positionCards is the cheap pass — only updates `top:` for each
// card based on Monaco's current state. Used by the scroll handler
// and as the final step of _alignCards. Does NOT touch view zones.
function _positionCards(m) {
    if (!m) return;
    const cardsHost = document.querySelector('.wiz-cards');
    if (!cardsHost) return;
    const scrollTop = m.getScrollTop();
    const cards = cardsHost.querySelectorAll('.wiz-card[data-line]');

    // Group cards by their anchor line. Co-linear cards — e.g. a
    // function-group header and its Init method, both anchored on the
    // group's first function — must STACK rather than pile up at the
    // same Y. querySelectorAll preserves DOM order, and Map preserves
    // insertion order, so within each line group the cards keep their
    // render order (header first, then its method).
    const byLine = new Map();
    cards.forEach(card => {
        const line = parseInt(card.dataset.line, 10);
        if (!line) return;
        if (!byLine.has(line)) byLine.set(line, []);
        byLine.get(line).push(card);
    });

    const stackGap = 8;
    for (const [line, group] of byLine) {
        let y = m.getTopForLineNumber(line) - scrollTop;
        for (const card of group) {
            card.style.position = 'absolute';
            card.style.top = `${y}px`;
            card.style.left = '0';
            card.style.right = '0';
            y += card.getBoundingClientRect().height + stackGap;
        }
    }
}

// projWizardBuildCards returns the HTML for the cards panel given a
// BlackBoxDef and the incomplete-paths list. Exported because it's
// useful in tests and admin panels — the slice-2 completion engine's
// rendering is a one-line consumer of this.
//
// Slice C99-4 (2026-05-19) changed the iteration shape:
//
//   - The function now prefers `parsed.structs[]` (introduced by the
//     C99 parser in Slice C99-2). When present and non-empty, it
//     renders ONE card per struct plus their methods, instead of
//     reading only the legacy first-struct fields.
//
//   - When `parsed.structs[]` is absent or empty (Go projects, which
//     haven't been upgraded to populate that field yet), the renderer
//     falls back to the legacy `parsed.name / parsed.init /
//     parsed.methods / parsed.props` path so Go output is unchanged.
//
//   - Slice C99-5 (2026-05-19) eliminated the separate "Extras"
//     pseudo-card. The parser no longer emits parsed.extras —
//     unclassified public functions are now grouped by their
//     longest common underscore-bounded prefix into virtual
//     "function-group" StructDef entries (IsFunctionGroup=true)
//     that render through the same path as real structs. Static
//     functions and internal structs are silently dropped per the
//     "se é interno, não representa" rule (§2.12 of the design doc).
//
// `source` is optional. When supplied, the renderer extracts the
// 1-based line number where each struct/method declaration starts
// (via a small regex pass over the source) and stamps `data-line`
// on each card. The wizard tab uses those numbers to align cards
// vertically with their matching code in Monaco. Tests that only
// care about the cards' content can pass undefined and ignore the
// alignment behaviour.

// projWizardCountIncomplete returns the number of incomplete entities
// that the wizard ACTUALLY renders a ⚠ for. This is what drives the
// toolbar Parse button, and it must match the cards exactly.
//
// The backend's ComputeIncomplete now covers BOTH models:
//   - Go: struct.<name> / method.<S>.<M>[.in|out.<n>] paths.
//   - C99: function.<n>[.in|out.<p>], enum.<n>.value.<V>, wiretype.<n>.
//
// The C99 ⚠ rows are still drawn client-side (Option A: the backend is
// the publish-gate's source of truth; the cards keep computing locally).
// To avoid double-counting now that the backend ALSO emits the C99
// paths, we:
//   1. Keep backend struct./method. paths ONLY when they refer to a
//      struct device that is actually rendered (a stray struct. with no
//      card never reaches here anymore, but the guard is cheap).
//   2. Skip backend function./enum./wiretype. paths — those entities are
//      counted client-side just below, identically to the cards.
export function projWizardCountIncomplete(parsed, backendIncomplete) {
    if (!parsed) return 0;
    let count = 0;

    const structNames = new Set();
    if (Array.isArray(parsed.structs)) {
        parsed.structs.forEach(s => { if (s && s.name) structNames.add(s.name); });
    }
    if (parsed.name) structNames.add(parsed.name);

    const backend = Array.isArray(backendIncomplete) ? backendIncomplete : [];
    for (const path of backend) {
        if (path.indexOf('struct.') === 0 || path.indexOf('method.') === 0) {
            const structName = path.split('.')[1] || '';
            if (!structNames.has(structName)) continue; // spurious / no card
        }
        // C99 entity paths are counted client-side below; skip them here
        // so the backend (which now emits them for the publish gate)
        // doesn't double-count against the cards.
        if (path.indexOf('function.') === 0 ||
            path.indexOf('enum.') === 0 ||
            path.indexOf('wiretype.') === 0) {
            continue;
        }
        count++;
    }

    (parsed.enums || []).forEach(ed => { if (_enumDeviceIncomplete(ed)) count++; });
    (parsed.functions || []).forEach(fn => { if (_functionDeviceIncomplete(fn)) count++; });
    (parsed.wireTypes || []).forEach(wt => { if (_wireTypeIncomplete(wt)) count++; });

    return count;
}

export function projWizardBuildCards(parsed, incomplete, source) {
    if (!parsed) {
        return `<p class="wiz-cards-empty">No source parsed yet.</p>`;
    }
    const setOfIncomplete = new Set(incomplete || []);
    const lines = source ? _findEntityLines(source, parsed) : {};

    const structs = Array.isArray(parsed.structs) && parsed.structs.length > 0
        ? parsed.structs
        : null;

    let html = '';

    if (structs) {
        // Multi-struct path (C99 Slice 2+). Each entry of `structs[]`
        // is a StructDef with its own icon/label/init/methods/props.
        // We adapt the StructDef shape to the `def` shape that
        // _renderStructCard and _renderMethodCard expect by mapping
        // {icon,label} → {structIcon,structLabel}. Other fields
        // (name, init, methods, props) carry through unchanged.
        structs.forEach((sd) => {
            const view = _structDefAsCardView(sd);
            html += _renderStructCard(view, setOfIncomplete, lines);
            if (view.init) {
                html += _renderMethodCard(view, 'Init', view.init, setOfIncomplete, lines);
            }
            (view.methods || []).forEach(m => {
                html += _renderMethodCard(view, m.name, m, setOfIncomplete, lines);
            });
        });
    } else if (parsed.name) {
        // Legacy single-struct path. Go projects + pre-Slice-2 C99
        // dumps live here. Behaviour unchanged.
        html += _renderStructCard(parsed, setOfIncomplete, lines);
        if (parsed.init) {
            html += _renderMethodCard(parsed, 'Init', parsed.init, setOfIncomplete, lines);
        }
        (parsed.methods || []).forEach(m => {
            html += _renderMethodCard(parsed, m.name, m, setOfIncomplete, lines);
        });
    }

    // Slice C99-5 removed the "Extras" pseudo-card. Slice C99-8 then
    // removed function-group devices entirely: every public function
    // WITHOUT a struct receiver is now its OWN device (one device per
    // function), surfaced in parsed.functions[]. Functions WITH a
    // receiver remain methods of their struct. Static functions and
    // internal structs are still dropped ("se é interno, não
    // representa").

    // Fatia 2: wire-types. Each entry of parsed.wireTypes[] is a
    // StructDef for a struct/handle referenced by a public signature —
    // the type that travels on a wire between the device that produces
    // it (a constructor) and the devices that consume it. It is not an
    // executable device, so it renders with a dedicated card (label +
    // icon, no ports). Rendered just before enums so the two "data
    // type" kinds sit together, above the function devices.
    const wireTypes = Array.isArray(parsed.wireTypes) ? parsed.wireTypes : [];
    wireTypes.forEach((wt) => {
        html += _renderWireTypeCard(wt, setOfIncomplete, lines);
    });

    // Slice C99-6: enum type devices. Each entry of parsed.enums[]
    // is an EnumDef surfaced because its type appears in a public
    // function signature. Enums are not executable devices — they
    // carry no ports — so they render with a dedicated card whose
    // rows are (read-only enumerator name + value) plus an editable
    // label. The card is "incomplete" until every value has a label.
    const enums = Array.isArray(parsed.enums) ? parsed.enums : [];
    enums.forEach((ed) => {
        html += _renderEnumCard(ed, setOfIncomplete, lines);
    });

    // Slice C99-8: standalone function devices. Each entry of
    // parsed.functions[] is a NamedFuncDef (a public function with no
    // struct receiver). It renders as a single card: header (full
    // function name / icon / label) plus its ports directly — inputs
    // from the parameters, output from the return type. There is NO
    // method sub-level and NO "runs first" (C99 has neither concept).
    const functions = Array.isArray(parsed.functions) ? parsed.functions : [];
    functions.forEach((fn) => {
        html += _renderFunctionCard(fn, setOfIncomplete, lines);
    });

    if (!html) {
        return `<p class="wiz-cards-empty">No device found in source.</p>`;
    }
    return html;
}

// _structDefAsCardView reshapes a StructDef (the entries in
// BlackBoxDef.Structs[] introduced by C99 Slice 2) into the wider
// "def view" object that _renderStructCard / _renderMethodCard
// already accept for legacy BlackBoxDef rendering.
//
// The fields that change name:
//
//   StructDef.icon   →  view.structIcon
//   StructDef.label  →  view.structLabel
//
// Everything else (name, doc, interactive, init, methods, props) is
// carried through unchanged. This adapter lets the existing card
// renderers keep their API while the multi-struct iteration loops
// without each loop body having to know about the rename.
function _structDefAsCardView(sd) {
    return {
        name:             sd.name,
        doc:              sd.doc || '',
        structIcon:       sd.icon || '',
        structLabel:      sd.label || '',
        interactive:      sd.interactive || '',
        init:             sd.init || null,
        methods:          sd.methods || [],
        props:            sd.props || [],
    };
}

// _findEntityLines runs regex passes over the source to record
// where each top-level entity lives:
//
//   - `struct.<S>`            for Go's `type <S> struct {` AND for
//                              C99's three struct forms:
//                                  struct <S> { ... }
//                                  typedef struct <S> { ... } Alias;
//                                  typedef struct      { ... } <S>;
//                              (Slice C99-4 — see §11 of the C99 doc.)
//   - `method.<S>.<M>`        for Go's `func (… *?<S>) <M>(` AND for
//                              C99's `<RetType> <S>_<M>(struct <S>* s ...)`
//                              functions.
//
// For each entity we record TWO line numbers:
//
//   - `line`         — the 1-based line of the declaration keyword
//                      (Go `type`/`func`, or the C99 equivalent
//                      first non-comment line of the construct).
//                      Used to position the card vertically.
//   - `commentLine`  — the 1-based line of the FIRST `//`/`/*` line
//                      in the contiguous comment block immediately
//                      above the declaration, or equal to `line`
//                      when there is no leading comment.
//
// Why both? When deciding how much vertical space the previous
// entity occupies in the source, "last line of previous" is one
// less than the next entity's `commentLine` — NOT one less than
// its `line`. If we used `line - 1`, the leading godoc/IDS
// comments of the next entity would get attributed to the previous
// entity, and view zones would be inserted between the comment
// block and the declaration, splitting things that belong together.
//
// Result is a `{path: {line, commentLine}}` map. Entities not found
// are simply absent; renderers that rely on the line gracefully skip
// alignment for those.
//
// The regexes are intentionally lenient — they match arbitrary
// whitespace, pointer or value receivers. They do NOT handle:
//   - Go generics (`type X[T any] struct`) — slice 6+ if needed
//   - anonymous struct literals
//   - C function-pointer typedefs (`typedef void (*fn_t)(...)`)
//   - C bit-field declarations
function _findEntityLines(source, parsed) {
    const out = {};
    if (!parsed) return out;
    const lines = source.split('\n');

    // commentRe matches a single comment-only line. Both `//` and
    // `/*` openings count — Go uses `//` exclusively, C99 mixes both.
    // Block comments that span multiple lines are conservatively
    // matched line-by-line: a line beginning with `*` (inside a
    // continued block) also counts as a comment line for the
    // upward walk.
    const commentRe = /^\s*(?:\/\/|\/\*|\*)/;

    // _commentStart returns the 1-based line where the contiguous
    // comment block immediately ABOVE `declIdx` (0-based) begins.
    // Returns `declIdx + 1` when there is no leading comment.
    const _commentStart = (declIdx) => {
        let j = declIdx - 1;
        while (j >= 0 && commentRe.test(lines[j])) {
            j--;
        }
        return j + 2; // +1 0-based→1-based, +1 because j is one above.
    };

    // ── Collect names of every struct we need to track. ─────────────────
    // C99 (Slice 2+) populates parsed.structs[]; Go and legacy parsers
    // populate only parsed.name. We track BOTH cases here so the
    // map covers every card the renderer will emit.
    const structNames = [];
    if (Array.isArray(parsed.structs) && parsed.structs.length > 0) {
        for (const sd of parsed.structs) {
            if (sd && sd.name) structNames.push(sd.name);
        }
    }
    if (parsed.name && !structNames.includes(parsed.name)) {
        structNames.push(parsed.name);
    }

    // Slice C99-6: enum cards also need a source line so they align
    // with their `typedef enum` / `enum` declaration in Monaco. An
    // enum WITHOUT a data-line falls into static flow position while
    // struct/method cards are absolutely positioned by _alignCards,
    // which makes the enum card overlap the others as the editor
    // scrolls. Collecting the enum names here drives the per-line
    // matching below, exactly like structs.
    const enumNames = [];
    if (Array.isArray(parsed.enums)) {
        for (const ed of parsed.enums) {
            if (ed && ed.name) enumNames.push(ed.name);
        }
    }

    // Slice C99-8: standalone function devices need a source line so
    // their card aligns with — and scrolls with — the function in
    // Monaco. A function device's card path is `function.<name>`; we
    // anchor it on the function's DEFINITION (preferred over the .h
    // prototype, same rule as struct methods). Collect the names here
    // to drive the matching below.
    const functionNames = [];
    if (Array.isArray(parsed.functions)) {
        for (const fn of parsed.functions) {
            if (fn && fn.name) functionNames.push(fn.name);
        }
    }

    // Fatia 2: wire-type cards (`wiretype.<tag>`) anchor on the same
    // struct declaration as a struct card would, so they reuse the
    // struct opener/closer patterns below. We collect their tags here
    // and feed them into structPatterns (the OPENER matcher), but NOT
    // into methodPatterns — a wire-type has no methods, and feeding its
    // tag there would spuriously match `tag_func(` device functions.
    // After the scan we copy each `struct.<tag>` hit onto
    // `wiretype.<tag>` (see end of function).
    const wireTypeNames = [];
    if (Array.isArray(parsed.wireTypes)) {
        for (const wt of parsed.wireTypes) {
            if (wt && wt.name) wireTypeNames.push(wt.name);
        }
    }

    // ── Build per-struct regexes. ───────────────────────────────────────
    // For each struct name we generate:
    //   - structPatterns[] — patterns that match the declaration's
    //                        opening line. Different shapes per language.
    //   - methodPatterns[] — patterns that match `<Struct>_<Method>(`
    //                        in C99 OR `func (… *?<S>) <M>(` in Go.
    //
    // We use ONE pass over the source per regex set rather than N passes;
    // the line count is small and the regex engine is fast.
    const structPatterns = [...structNames, ...wireTypeNames].map(name => ({
        name,
        patterns: [
            // Go: `type X struct {`
            new RegExp(`^\\s*type\\s+${_reEsc(name)}\\s+struct\\s*\\{`),
            // C99: `struct X {`
            new RegExp(`^\\s*struct\\s+${_reEsc(name)}\\s*\\{`),
            // C99: `typedef struct X { ... } Alias;` — match the typedef line
            new RegExp(`^\\s*typedef\\s+struct\\s+${_reEsc(name)}\\b`),
            // C99: `typedef struct { ... } X;` — match the OPENING line.
            // We can't trivially regex-match this without multi-line
            // support, so we approximate by spotting the closer
            // `} X;` and walking back to the opening typedef. That
            // walk is done in the per-line loop below; here we record
            // the closer pattern.
            new RegExp(`\\}\\s*${_reEsc(name)}\\s*;`),
        ],
    }));

    const methodPatterns = structNames.map(name => ({
        name,
        patterns: [
            // Go: `func (s *X) Method(` or `func (s X) Method(`
            new RegExp(
                `^\\s*func\\s*\\([^)]*\\b${_reEsc(name)}\\b[^)]*\\)\\s+(\\w+)\\s*\\(`
            ),
            // C99: `<RetType> X_Method(struct X* s ...)`. We don't
            // verify the receiver here (the renderer doesn't care —
            // the parser already filtered); the regex looks for
            // `X_<Method>(` after a return type. Requiring the line to
            // START with an identifier (a type token, not `*` or `/`)
            // is what keeps it from matching a doc-comment that
            // mentions the method in prose — the same class of bug
            // that anchored a function device on its comment.
            new RegExp(
                `^\\s*[A-Za-z_][\\w\\s\\*]*\\b${_reEsc(name)}_(\\w+)\\s*\\(`
            ),
        ],
    }));

    // Enum opener/closer patterns. Three C99 forms, mirroring the
    // struct handling:
    //   - `enum X {`               (tag-only)            → opener
    //   - `typedef enum X {`       (tag + alias)         → opener
    //   - `typedef enum { … } X;`  (anonymous + alias)   → closer,
    //                                                       walk back
    // Name resolution is tag-wins (matching the parser), so for the
    // tag+alias form the opener pattern (which carries the tag) is
    // what matches; for the anonymous form the closer carries the
    // alias, which IS the Name.
    const enumPatterns = enumNames.map(name => ({
        name,
        patterns: [
            new RegExp(`^\\s*enum\\s+${_reEsc(name)}\\s*\\{`),
            new RegExp(`^\\s*typedef\\s+enum\\s+${_reEsc(name)}\\b`),
            new RegExp(`\\}\\s*${_reEsc(name)}\\s*;`),
        ],
    }));

    // Function-device patterns: match a line that DEFINES or DECLARES
    // a function named `name`. The pattern requires a return type
    // before the name (`^\s*<type...> <name>(`), which is what makes
    // it robust:
    //   - comment lines (` * ... display_write() ...`, `// ...`) start
    //     with `*` or `/`, not an identifier, so they never match —
    //     this is the bug that anchored display_write on its own
    //     doc-comment instead of its definition;
    //   - the function-pointer typedef `(*display_write_fn)(...)` has
    //     `*`/`(` before the name, not a plain type token, so it's
    //     skipped;
    //   - a call site `    display_write(...)` is indented with no
    //     return type before the name, so it's skipped too.
    // We still match both the .h prototype and the .c definition; the
    // isDef rule below prefers the definition.
    const functionPatterns = functionNames.map(name => ({
        name,
        re: new RegExp(`^\\s*[A-Za-z_][\\w\\s\\*]*\\b${_reEsc(name)}\\s*\\(`),
    }));

    for (let i = 0; i < lines.length; i++) {
        const ln = i + 1;
        const text = lines[i];

        // Struct opener?
        for (const { name, patterns } of structPatterns) {
            const key = `struct.${name}`;
            if (out[key]) continue; // first match wins

            // Patterns 0..2 are openers. Pattern 3 is the typedef-
            // anonymous closer; we handle that separately so the
            // recorded `line` points at the typedef keyword, not at
            // the closing brace.
            if (patterns[0].test(text) || patterns[1].test(text) || patterns[2].test(text)) {
                out[key] = { line: ln, commentLine: _commentStart(i) };
                continue;
            }
            if (patterns[3].test(text)) {
                // Walk backwards to find the `typedef struct {`
                // opener so the card aligns with the typedef keyword
                // rather than the closer line.
                let openerIdx = -1;
                for (let j = i; j >= 0; j--) {
                    if (/^\s*typedef\s+struct\s*\{?/.test(lines[j])) {
                        openerIdx = j;
                        break;
                    }
                }
                const openerLine = openerIdx >= 0 ? openerIdx + 1 : ln;
                out[key] = {
                    line: openerLine,
                    commentLine: _commentStart(openerIdx >= 0 ? openerIdx : i),
                };
            }
        }

        // Method opener? Test against EVERY tracked struct because a
        // file may declare functions for several structs.
        for (const { name, patterns } of methodPatterns) {
            for (const re of patterns) {
                const m = text.match(re);
                if (m) {
                    let methodName = m[1];
                    // C99: a `_init` function is routed to the Init
                    // slot by the parser (case-insensitive match), and
                    // the card uses the canonical "Init" name. Without
                    // this normalisation the line key (`…init`) would
                    // not match the card's path (`…Init`), so the Init
                    // card would get no data-line and fall into static
                    // flow position.
                    if (methodName.toLowerCase() === 'init') methodName = 'Init';
                    const key = `method.${name}.${methodName}`;

                    // Prefer the DEFINITION over the forward
                    // DECLARATION. A line carrying `);` is a prototype
                    // (header); a line without it is the start of a
                    // definition (the body the user reads). Per §12.6
                    // the card should anchor on the definition, so a
                    // later definition overwrites an earlier prototype.
                    const isDef = !/\)\s*;/.test(text);
                    const existing = out[key];
                    if (!existing || (isDef && !existing.isDef)) {
                        out[key] = { line: ln, commentLine: _commentStart(i), isDef };
                    }
                    break;
                }
            }
        }

        // Enum opener/closer? (Slice C99-6.) Patterns 0..1 are
        // openers; pattern 2 is the anonymous-typedef closer, which
        // we resolve by walking back to the `typedef enum {` line so
        // the card anchors on the keyword, not the closing brace —
        // identical to the struct-anonymous handling above.
        for (const { name, patterns } of enumPatterns) {
            const key = `enum.${name}`;
            if (out[key]) continue;
            if (patterns[0].test(text) || patterns[1].test(text)) {
                out[key] = { line: ln, commentLine: _commentStart(i) };
                continue;
            }
            if (patterns[2].test(text)) {
                let openerIdx = -1;
                for (let j = i; j >= 0; j--) {
                    if (/^\s*typedef\s+enum\s*\{?/.test(lines[j])) {
                        openerIdx = j;
                        break;
                    }
                }
                const openerLine = openerIdx >= 0 ? openerIdx + 1 : ln;
                out[key] = {
                    line: openerLine,
                    commentLine: _commentStart(openerIdx >= 0 ? openerIdx : i),
                };
            }
        }

        // Function device? (Slice C99-8.) Anchor on the DEFINITION,
        // not the prototype: a line carrying `);` is a declaration; a
        // line without it is the start of a definition. A later
        // definition overwrites an earlier prototype, so the card
        // lands on the body the user reads.
        for (const { name, re } of functionPatterns) {
            if (!re.test(text)) continue;
            const key = `function.${name}`;
            const isDef = !/\)\s*;/.test(text);
            const existing = out[key];
            if (!existing || (isDef && !existing.isDef)) {
                out[key] = { line: ln, commentLine: _commentStart(i), isDef };
            }
        }
    }

    // Fatia 2: expose each matched struct line under its wire-type key
    // too, so the wire-type card aligns with (and scrolls with) the
    // struct declaration in Monaco — same as struct/enum/function cards.
    for (const name of wireTypeNames) {
        const sk = `struct.${name}`;
        if (out[sk]) {
            out[`wiretype.${name}`] = out[sk];
        }
    }

    return out;
}

// _reEsc escapes regex metacharacters in a string so it can be
// safely interpolated into a RegExp body. Used for the struct
// names that drive entity discovery — names contain `_` and digits
// in practice (e.g. `wifi_conn_config_t`), neither of which need
// escaping, but defensive coding here is cheap.
function _reEsc(s) {
    return String(s).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function _renderStructCard(def, incompleteSet, lines) {
    const path = `struct.${def.name}`;
    const isIncomplete = incompleteSet.has(path);
    const label = def.structLabel || def.name;
    const icon  = _faIcon(def.structIcon || 'cube');

    const fieldRows = (def.props || []).map(p => {
        const fieldPath = `${path}.field.${p.fieldName}`;
        // Prefer the server's nativeType flag (slice 6+) — it uses the
        // authoritative IsNativePropType definition in
        // server/codegen/blackbox/completion.go. Fall back to the
        // local list for older servers that don't emit it yet.
        const native = (typeof p.nativeType === 'boolean')
            ? p.nativeType
            : _NATIVE_TYPES.has(p.goType);

        // Three categories of row, deciding inert + primary text:
        //   - Tagged (Untagged=false): always shows label/fieldName
        //     and current default; inert only when non-native and
        //     somehow tagged anyway (rare). Clickable.
        //   - Untagged native: row says "needs prop tag" via the
        //     ⚠ that incompleteSet already injected; clickable so
        //     the user opens the modal and adds the tag.
        //   - Untagged non-native: inert, informational. The label
        //     box would be misleading ("we don't know what this is")
        //     so we fall back to the field name and explain why
        //     it's not editable in the secondary text.
        const isUntagged = !!p.untagged;
        const isInert = isUntagged && !native;
        const inertReason = isInert
            ? `non-native type (${p.goType}) — wizard can't generate UI`
            : '';

        let primary, secondary;
        if (isInert) {
            primary = p.fieldName;
            secondary = `${p.goType} · internal/structural`;
        } else if (isUntagged && native) {
            // Native, no tag yet — invite the user to configure.
            primary = p.fieldName;
            secondary = `${p.goType} · click to make this a prop`;
        } else {
            // Tagged.
            primary = p.label || p.fieldName;
            secondary = p.goType + (p.default ? ` · default: ${esc(p.default)}` : '');
        }

        return _renderRow({
            path: fieldPath,
            isIncomplete: incompleteSet.has(fieldPath),
            inert: isInert,
            inertReason,
            primary,
            secondary,
        });
    }).join('');

    return _renderCardShell({
        kind: 'struct',
        path,
        line: lines && lines[path] && lines[path].line,
        commentLine: lines && lines[path] && lines[path].commentLine,
        isIncomplete,
        title: `${icon} ${esc(label)}`,
        subtitle: 'struct',
        body: fieldRows || `<p class="wiz-card-empty">No exported fields on this struct.</p>`,
    });
}

function _renderMethodCard(def, methodName, fd, incompleteSet, lines) {
    const path = `method.${def.name}.${methodName}`;
    const isIncomplete = incompleteSet.has(path);
    const label = fd.label || methodName;
    // Method icon falls back to struct icon, matching the IDE's
    // runtime fallback rule documented in the IDS readme.
    const icon = _faIcon(fd.icon || def.structIcon || 'gear');

    const portRows = [
        ...(fd.inputs || []).map(p => _renderPortRow(path, 'in', p, incompleteSet)),
        ...(fd.outputs || []).map(p => _renderPortRow(path, 'out', p, incompleteSet)),
    ].join('');

    return _renderCardShell({
        kind: 'method',
        path,
        line: lines && lines[path] && lines[path].line,
        commentLine: lines && lines[path] && lines[path].commentLine,
        isIncomplete,
        title: `${icon} ${esc(label)}`,
        subtitle: methodName === 'Init' ? 'method · runs first' : 'method',
        body: portRows || `<p class="wiz-card-empty">No ports on this method.</p>`,
    });
}

// _renderFunctionCard renders one standalone function device
// (Slice C99-8). The function IS the device: the card header carries
// the full function name / icon / label, and the ports render
// directly beneath it (inputs from parameters, output from the
// return type). There is no method sub-level and no "runs first".
//
// Paths:
//   - card header → `function.<name>`                (icon/label)
//   - each port   → `function.<name>.<in|out>.<port>` (label/connection)
//
// The synthetic `return` output has no source position, so its row
// is rendered read-only (see _renderPortRow's handling) — clicking
// it does nothing, and it is not required to carry a label.
// _functionPortMissing / _functionDeviceIncomplete / _enumDeviceIncomplete
// are the single source of truth for the client-side ⚠ rules. They are
// used both by the card renderers (to draw the badge) and by
// projWizardCountIncomplete (to drive the toolbar Parse button), so the
// button can never disagree with what the cards show.
//
// Rules for a function device:
//   - device   → needs a label AND an icon (same rule as methods);
//   - param in → needs label + comment + a connection choice;
//   - param out→ needs label + comment;
//   - return   → needs only a label (its type comes from the
//                signature; there is no separate comment or connection).
// Rules for a function device:
//   - device → needs a label AND an icon (same rule as methods);
//   - return → needs only a label (it is the return VALUE, not a
//              parameter — no comment, no connection);
//   - every other port is a PARAMETER, input or output alike: it needs
//     a label + comment + a connection choice. Direction is only how
//     the pin is drawn; in the generated call the parameter still needs
//     an argument, so the mandatory/optional choice applies regardless.
function _functionPortMissing(p, dir) {
    if (p.name === 'return') return !p.label;
    // Synthetic callback reference output: a handler's `callback` pin
    // (produced by `// callback:<type>.`) carries callbackType and has no
    // backing parameter, so it is not editable — no label/comment/
    // connection of its own to complete. It can never be "fixed", so it is
    // never incomplete; exempt it like `return` above. (A callback INPUT —
    // e.g. setDisplay's `writer` — also carries callbackType but IS a real
    // parameter; the dir === 'out' guard keeps inputs subject to the rules.)
    if (dir === 'out' && p.callbackType) return false;
    const noComment = !(p.doc || p.comment);
    return !p.label || noComment || !!p.missingConn;
}

function _functionDeviceIncomplete(fn) {
    if (!fn.label || !fn.icon) return true;
    if ((fn.inputs || []).some(p => _functionPortMissing(p, 'in'))) return true;
    if ((fn.outputs || []).some(p => _functionPortMissing(p, 'out'))) return true;
    return false;
}

// A wire-type (an opaque handle / struct carried on a wire) is
// "incomplete" until the specialist has given it a label AND an icon —
// the same visual-identity bar as a device. Unlike a device it has no
// ports, so those are the only two things to fill in.
function _wireTypeIncomplete(wt) {
    return !wt.label || !wt.icon;
}

// _canBeOutput mirrors the Go-side canBeOutput: a C99 parameter can be
// marked as an output only when it is a mutable (non-const) pointer to
// something other than an opaque handle. Values, const pointers and
// void* cannot carry a value back, so the Wizard never offers the
// "output" checkbox for them — every parameter is an input by default,
// and only a mutable pointer may be flipped.
function _canBeOutput(goType) {
    const t = (goType || '').trim();
    if (!t.includes('*')) return false;       // not a pointer
    if (/\bconst\b/.test(t)) return false;     // const → cannot write through
    if (/\bvoid\s*\*/.test(t)) return false;   // void* → opaque caller handle
    return true;
}

// An enum device is incomplete while any value lacks a label.
function _enumDeviceIncomplete(ed) {
    return (ed.values || []).some(v => !v.label);
}

function _renderFunctionCard(fn, incompleteSet, lines) {
    const path = `function.${fn.name}`;
    const fd = fn; // NamedFuncDef: name + inline FuncDef fields
    const label = fd.label || fn.name;
    const icon = _faIcon(fd.icon || 'bolt');

    const deviceIncomplete = _functionDeviceIncomplete(fd);

    const portRows = [
        ...(fd.inputs || []).map(p => _renderPortRow(path, 'in', p, incompleteSet,
            { forceIncomplete: _functionPortMissing(p, 'in'), isFunctionDevice: true })),
        ...(fd.outputs || []).map(p => _renderPortRow(path, 'out', p, incompleteSet,
            { forceIncomplete: _functionPortMissing(p, 'out'), isFunctionDevice: true })),
    ].join('');

    return _renderCardShell({
        kind: 'function',
        path,
        line: lines && lines[path] && lines[path].line,
        commentLine: lines && lines[path] && lines[path].commentLine,
        isIncomplete: deviceIncomplete || incompleteSet.has(path),
        title: `${icon} ${esc(label)}`,
        subtitle: 'function device',
        body: portRows || `<p class="wiz-card-empty">No ports on this function.</p>`,
    });
}

// Slice C99-5 removed the _renderExtrasCard function. Slice C99-8
// removed function-group devices. Standalone public functions now
// render via _renderFunctionCard above.

// _renderEnumCard renders one EnumDef (Slice C99-6, §12.2). An enum
// is not an executable device: it has no ports and no props. Its
// card lists the enumerators, each a row showing the enumerator's
// name and resolved value, with the specialist-assigned label as
// the primary text. A row is "incomplete" until it has a label; the
// card is "incomplete" while ANY row is.
//
// Paths:
//   - card header → `enum.<Name>`                 (icon/label of the enum)
//   - each row    → `enum.<Name>.value.<ValueName>` (the value's label)
//
// These paths are handled by the C99 rewrite engine's enum
// interception (rewrite_c_enum.go); the SPA does not need to know
// which backend is active — the wire format is the same setStruct-
// Directives op used everywhere else.
function _renderEnumCard(ed, incompleteSet, lines) {
    const path = `enum.${ed.name}`;
    const icon = _faIcon(ed.icon || 'list-ol');
    const label = ed.label || ed.name;
    const values = ed.values || [];

    // The enum is incomplete while any value lacks a label. We
    // compute this locally rather than trusting the incompleteSet,
    // because the label-presence check is unambiguous client-side
    // and keeps the enum self-contained.
    const anyValueMissingLabel = _enumDeviceIncomplete(ed);

    const rows = values.map(v => {
        const valuePath = `${path}.value.${v.name}`;
        const missingLabel = !v.label;

        // The displayed value: a number normally, or the verbatim
        // expression text when the parser could not evaluate it
        // (ValueIsRaw — e.g. `1 << 2`).
        const shownValue = v.valueIsRaw ? esc(v.rawValue || '?') : String(v.value);

        let primary, secondary;
        if (missingLabel) {
            // No label yet — show the enumerator name and invite the
            // user to set a label.
            primary = v.name;
            secondary = `= ${shownValue} · click to set a label`;
        } else {
            // Labeled — show the human label as primary; the C
            // identifier and value as supporting context.
            primary = v.label;
            secondary = `${v.name} = ${shownValue}`;
        }

        return _renderRow({
            path: valuePath,
            isIncomplete: missingLabel || incompleteSet.has(valuePath),
            inert: false,
            inertReason: '',
            primary,
            secondary,
        });
    }).join('');

    return _renderCardShell({
        kind: 'enum',
        path,
        line: lines && lines[path] && lines[path].line,
        commentLine: lines && lines[path] && lines[path].commentLine,
        isIncomplete: anyValueMissingLabel || incompleteSet.has(path),
        title: `${icon} ${esc(label)}`,
        subtitle: 'enum · pick a label for each value',
        body: rows || `<p class="wiz-card-empty">This enum has no values.</p>`,
    });
}

// _renderWireTypeCard renders one wire-type (Fatia 2). A wire-type is a
// struct/handle that travels on a wire — produced by one device (e.g. a
// constructor returning `sht3x_t *`) and consumed by others as an input.
// It is NOT an executable device: no ports, no exec pin. The card lets
// the specialist give it a display label and an icon so the wire has a
// visible identity ("Sensor handle"). The technical name shown is the
// typedef alias when present (what the specialist writes in signatures,
// e.g. `sht3x_t`), falling back to the struct tag.
//
// Path: `wiretype.<Name>` where Name is the struct tag. The save reuses
// the setStructDirectives op on `struct.<Name>`, which the C99 rewrite
// already handles — the directive comment lands above the struct body.
function _renderWireTypeCard(wt, incompleteSet, lines) {
    const path = `wiretype.${wt.name}`;
    const icon = _faIcon(wt.icon || 'plug');
    const techName = wt.alias || wt.name;
    const label = wt.label || techName;
    const incomplete = _wireTypeIncomplete(wt);

    // A single informational, non-clickable row. When the specialist
    // documented the handle (the comment above its typedef), we show
    // that prose — it is what they wrote and expect to see. Otherwise
    // we fall back to a generic explanation of what a wire-type is.
    const desc = (wt.doc && wt.doc.trim())
        ? wt.doc.trim()
        : 'type carried on a wire · produced by one device, consumed by others';
    const body = _renderRow({
        path: `${path}.info`,
        isIncomplete: false,
        inert: true,
        inertReason: '',
        primary: techName,
        secondary: desc,
    });

    return _renderCardShell({
        kind: 'wiretype',
        path,
        line: lines && lines[path] && lines[path].line,
        commentLine: lines && lines[path] && lines[path].commentLine,
        isIncomplete: incomplete || incompleteSet.has(path),
        title: `${icon} ${esc(label)}`,
        subtitle: 'wire type · give it a label and icon',
        body,
    });
}

function _renderPortRow(methodPath, dir, port, incompleteSet, opts) {
    opts = opts || {};
    const portPath = `${methodPath}.${dir}.${port.name}`;
    const isError = !!port.isError;
    const isReturn = port.name === 'return';
    let connectionHint = '';
    if (isError) {
        connectionHint = ' · error return — runtime-handled';
    } else if (dir === 'in') {
        connectionHint = port.missingConn
            ? ' · no connection set'
            : (port.connection ? ` · ${esc(port.connection)}` : '');
    } else if (opts.isFunctionDevice && !isReturn) {
        // C99 (decision b): an output that is NOT the synthetic return
        // is a parameter shown on the output side. It is still a
        // parameter in the generated call, so it carries a connection
        // exactly like an input. The `return` value and Go named
        // returns are not parameters and get no hint.
        connectionHint = port.missingConn
            ? ' · no connection set'
            : (port.connection ? ` · ${esc(port.connection)}` : '');
    }
    //
    // Slice C99-8: the synthetic `return` output of a function device
    // has no source position, so it is rendered inert (not clickable)
    // and never counts as incomplete — the return type is fixed by
    // the signature and needs no label.
    return _renderRow({
        path: portPath,
        isIncomplete: opts.inert ? false : (opts.forceIncomplete || incompleteSet.has(portPath)),
        inert: !!opts.inert,
        inertReason: opts.inertReason || '',
        primary: port.label || port.name,
        secondary: `${dir === 'in' ? 'input' : 'output'} · ${port.goType}${connectionHint}`,
    });
}

function _renderCardShell({ kind, path, isIncomplete, title, subtitle, body, line, commentLine }) {
    // Card header is clickable for entity-level modals (struct / method).
    // The dispatcher routes by path shape, so the header just needs to
    // emit the same `projWizardOnRowClick` call as a row would. Without
    // this, the user could only configure fields/ports — there was no
    // entry point for the struct/method-level directives (Slice 4 added
    // the modals; Slice 5 wires them into the headers here).
    //
    // The body's row clicks still take precedence because the rows are
    // descendants and have their own `onclick` — clicking inside the
    // body never bubbles to the header click.
    //
    // Two data attributes drive vertical alignment:
    //
    //   `data-line`         — the line of the `type`/`func` keyword.
    //                         Used as the card's anchor (the card's Y
    //                         tracks Monaco's getTopForLineNumber for
    //                         this line).
    //   `data-comment-line` — the line where the leading comment
    //                         block (if any) begins. Used by
    //                         `_alignCards` to know that the PREVIOUS
    //                         entity's "last line" is this entity's
    //                         commentLine - 1, NOT this entity's
    //                         line - 1. Without this, view zones get
    //                         inserted between a leading comment and
    //                         its `func`, splitting things that
    //                         belong together.
    //
    // Entities the line finder couldn't locate are stamped with
    // neither attribute and fall back to flow position.
    const dataLine = (typeof line === 'number' && line > 0)
        ? ` data-line="${line}"`
        : '';
    const dataCommentLine = (typeof commentLine === 'number' && commentLine > 0)
        ? ` data-comment-line="${commentLine}"`
        : '';
    return `
        <div class="wiz-card ${isIncomplete ? 'wiz-card-incomplete' : ''} wiz-card-${kind}"
             data-path="${esc(path)}"${dataLine}${dataCommentLine}>
            <div class="wiz-card-header wiz-card-header-clickable"
                 onclick="projWizardOnRowClick('${esc(path)}')"
                 title="Click to edit ${esc(kind)}-level settings">
                ${isIncomplete
                    ? '<span class="wiz-warn-badge"><i class="fa-solid fa-triangle-exclamation"></i> Incomplete</span>'
                    : ''}
                <div class="wiz-card-title">${title}<span class="wiz-card-edit-hint"><i class="fa-solid fa-pen"></i></span></div>
                <div class="wiz-card-subtitle">${esc(subtitle)}</div>
            </div>
            <div class="wiz-card-body">${body}</div>
        </div>`;
}

function _renderRow({ path, isIncomplete, inert, inertReason, primary, secondary }) {
    const cls = [
        'wiz-row',
        isIncomplete ? 'wiz-row-incomplete' : '',
        inert ? 'wiz-row-inert' : 'wiz-row-clickable',
    ].filter(Boolean).join(' ');

    // Inert rows do not get an onclick — they would deceive the user
    // into thinking they are configurable. Their hover/cursor styles
    // also match the read-only intent.
    const clickAttr = inert
        ? ''
        : `onclick="projWizardOnRowClick('${esc(path)}')"`;
    const inertHint = inert && inertReason
        ? `<span class="wiz-row-inert-hint">${esc(inertReason)}</span>`
        : '';

    return `
        <div class="${cls}" ${clickAttr}>
            ${isIncomplete
                ? '<i class="wiz-row-warn fa-solid fa-triangle-exclamation"></i>'
                : '<span class="wiz-row-warn-spacer"></span>'}
            <div class="wiz-row-text">
                <div class="wiz-row-primary">${esc(primary)}</div>
                <div class="wiz-row-secondary">${esc(secondary)}${inertHint}</div>
            </div>
        </div>`;
}

// =============================================================================
//  Modals — Slice 4 (Struct + Field)
// =============================================================================
//
// The wizard's modals share a small infrastructure: a centred backdrop,
// a content card, a header, a body of fields, and a footer with Save +
// Cancel. Each specific modal builds its own field block and its own
// save handler. The infrastructure is local to this module — we
// considered using utils.js's `showPrompt` / `showConfirm` but those
// are single-purpose and would not extend cleanly to multi-field
// forms. The patterns (backdrop dismiss, ESC close, focus trap) are
// the same.
//
// All saves go through `_applyEditAndClose` which:
//   1. Sends a POST /wizard/rewrite with the source plus the single edit.
//   2. On success, hydrates the wizard state from the response,
//      schedules a debounced draft save, and re-renders the cards.
//   3. On failure, leaves the modal open and shows the error inline
//      so the user can correct and retry without losing their input.
//
// Slice 6 will replace the freeform "Icon" inputs with a registry-backed
// picker. The extension point is flagged in the code below.

// _openModal opens a backdrop with `bodyHtml` injected into the body
// slot. Returns the backdrop element so the caller can query its
// children for input bindings. The caller is responsible for calling
// `_closeModal(backdrop)` when done — usually inside the Save handler
// or via the Cancel/backdrop/Escape paths set up here.
function _openModal({ title, bodyHtml, footerHtml, onMount }) {
    // Remove any previous wizard modal first — opening a second modal
    // while one is active is a UX accident we should not encourage.
    document.getElementById('_portal-wizard-modal')?.remove();

    const backdrop = document.createElement('div');
    backdrop.id = '_portal-wizard-modal';
    backdrop.className = 'wiz-modal-backdrop';
    backdrop.innerHTML = `
        <div class="wiz-modal" role="dialog" aria-modal="true" aria-labelledby="wiz-modal-title">
            <div class="wiz-modal-header">
                <h3 id="wiz-modal-title">${esc(title)}</h3>
                <button class="wiz-modal-close" aria-label="Close" type="button">
                    <i class="fa-solid fa-xmark"></i>
                </button>
            </div>
            <div class="wiz-modal-body">${bodyHtml}</div>
            <div class="wiz-modal-error" style="display:none"></div>
            <div class="wiz-modal-footer">${footerHtml || ''}</div>
        </div>`;
    document.body.appendChild(backdrop);

    // Close paths.
    const closeBtn = backdrop.querySelector('.wiz-modal-close');
    closeBtn.addEventListener('click', () => _closeModal(backdrop));
    backdrop.addEventListener('click', (e) => {
        if (e.target === backdrop) _closeModal(backdrop);
    });
    const escHandler = (e) => {
        if (e.key === 'Escape') {
            _closeModal(backdrop);
            document.removeEventListener('keydown', escHandler);
        }
    };
    document.addEventListener('keydown', escHandler);

    if (typeof onMount === 'function') onMount(backdrop);
    return backdrop;
}

function _closeModal(backdrop) {
    if (backdrop && backdrop.parentNode) backdrop.parentNode.removeChild(backdrop);
}

// _setModalError writes a string into the modal's error slot, showing
// it. Empty string hides the slot. Used by save handlers to surface
// /wizard/rewrite errors without closing the modal.
function _setModalError(backdrop, message) {
    const slot = backdrop.querySelector('.wiz-modal-error');
    if (!slot) return;
    if (!message) {
        slot.style.display = 'none';
        slot.textContent = '';
        return;
    }
    slot.style.display = '';
    slot.textContent = message;
}

// _setModalSaving toggles a "saving…" state on the Save button. Keeps
// the user from double-clicking and from giving up too early on a
// slow rewrite (large files take a few hundred ms server-side).
function _setModalSaving(backdrop, on) {
    const btn = backdrop.querySelector('.wiz-modal-save');
    if (!btn) return;
    btn.disabled = on;
    if (on) {
        btn._html = btn.innerHTML;
        btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…';
    } else if (btn._html) {
        btn.innerHTML = btn._html;
    }
}

// _applyEditAndClose performs the rewrite round-trip and, on success,
// hydrates the wizard state, schedules a draft save, re-renders the
// cards, and closes the modal. On failure, leaves the modal open and
// surfaces the error inline.
//
// `edit` is the WizardEdit object — { op, path, args }. We wrap it in
// a single-element array because /wizard/rewrite always takes a list.
async function _applyEditAndClose(backdrop, edit) {
    _setModalError(backdrop, '');
    _setModalSaving(backdrop, true);

    // Same `language` bridge as the parse calls above: read through
    // window._projGetLanguage so we route to the correct server-side
    // rewrite engine (Go vs C99). Default to 'go' for the early-init
    // race (the server also defaults to Go when omitted).
    const lang = (typeof window !== 'undefined' && window._projGetLanguage)
        ? window._projGetLanguage()
        : 'go';
    const r = await api('POST', '/api/v1/blackbox/wizard/rewrite', {
        code: _state.code,
        edits: [edit],
        language: lang,
    });

    _setModalSaving(backdrop, false);

    if (r?.metadata?.status !== 200) {
        _setModalError(backdrop, r?.metadata?.error || 'Unknown rewrite error');
        return;
    }

    // Hydrate from rewrite response. Rewrite returns the new source
    // alongside the parsed/incomplete refresh — same fields as /parse
    // plus the Code we apply to Monaco.
    _hydrateFromRewriteResponse(r.data);
    _scheduleDraftSave();
    _renderTab();
    _closeModal(backdrop);
    toast('success', 'Wizard edit applied.');

    // Trigger the project's working-source backup so the user's
    // wizard edit survives an unexpected close. We reach through the
    // window bridge instead of importing projects.js to avoid a
    // circular module dependency. Best-effort: missing function is
    // fine (older host page), and any backup failure is logged
    // separately by projSaveBackup itself.
    if (typeof window.projSaveBackup === 'function') {
        window.projSaveBackup();
    }
}

// _hydrateFromRewriteResponse mirrors _hydrateFromParseResponse for
// /wizard/rewrite responses. The Code field is what differentiates
// the two — rewrite returns a fresh source string that we push back
// into Monaco so the user sees the rewritten Go.
function _hydrateFromRewriteResponse(data) {
    const newCode = typeof data.code === 'string' ? data.code : _state.code;
    _state.code = newCode;
    _state.parsed = data.parsed || _state.parsed;
    _setIncomplete(Array.isArray(data.incomplete) ? data.incomplete : _state.incomplete);
    _writeMonacoSource(newCode);
}

// =============================================================================
//  Add help / Add img — shared modal helpers
// =============================================================================
//
// The Struct and Method modals each carry an "Add help" button between
// their Cancel and Save buttons. The button hands off to the help
// files manager with a deep-link kind (`newStruct` or `newMethod`)
// that lands the user directly on the "Create new file" sub-modal
// with the right name pre-selected. The flow is:
//
//   wizard modal Add help  →  openHelpFiles({ kind, parsed, ... })
//     → help_files renders the manager shell
//     → help_files raises the create-file sub-modal pre-populated
//     → user picks order + language, hits OK
//     → file is created and opened in the markdown editor
//
// The wizard modal that hosted the button stays open in the background
// — the user comes back to it after closing help_files. (Closing the
// wizard modal is independent of closing help_files, by design: the
// user may want to add help, return, then save the wizard edit.)
//
// Pre-condition: the wizard's local state must hold a parsed
// BlackBoxDef (`_state.parsed`). Without it the help_files manager's
// "name" dropdown cannot list the user's method names — only the
// hard-coded "main menu text" entry. The button is rendered as
// `disabled` whenever `_state.parsed` is absent; the disabled state
// also carries an explanatory tooltip directing the user to Parse.
// In practice the wizard modals only open after a successful parse
// (cards aren't rendered without one), so this is a defence-in-depth
// guard, not the common path.
//
// Português: helper compartilhado para os botões "Add help" dos
// modais Struct e Method. Abre o gerenciador de help_files já com o
// modal "Create new file" e o name pré-selecionado.

// _buildAddHelpButton returns the HTML for the Add help button. The
// button is disabled when the wizard has no parsed BlackBoxDef yet
// (no parse done or last parse errored). The disabled tooltip and
// the click handler check the same condition so a stale `_state` at
// click time still surfaces a useful message.
function _buildAddHelpButton() {
    const enabled = !!_state.parsed;
    const titleAttr = enabled
        ? 'title="Open the help files manager and create a new help markdown for this entry"'
        : 'title="Run Parse first — Add help needs the parsed device definition to know which file to create"';
    const disabledAttr = enabled ? '' : 'disabled';
    return `<button type="button" class="btn btn-ghost wiz-modal-add-help"
                    ${disabledAttr} ${titleAttr}>
                <i class="fa-solid fa-circle-question"></i> Add help
            </button>`;
}

// _wireAddHelpButton attaches the click handler to the Add help button
// inside the given backdrop. `kind` is 'newStruct' or 'newMethod' and
// `methodName` is required for 'newMethod' (the bare Go identifier so
// help_files matches the dropdown basename exactly).
function _wireAddHelpButton(backdrop, kind, methodName) {
    const btn = backdrop.querySelector('.wiz-modal-add-help');
    if (!btn) return;
    btn.addEventListener('click', () => {
        // Re-check at click time — _state.parsed could in principle
        // have been cleared by some other code path between mount and
        // click. The HTML disabled attribute is the fast path; this is
        // belt-and-suspenders for unusual states.
        if (!_state.parsed) {
            _setModalError(backdrop, 'Run Parse first — the help dropdown needs the parsed device definition.');
            return;
        }
        const projectId = _state.projectId;
        if (!projectId) {
            _setModalError(backdrop, 'Internal: project id missing.');
            return;
        }
        const opts = { kind, parsed: _state.parsed };
        if (kind === 'newMethod') {
            if (!methodName) {
                _setModalError(backdrop, 'Internal: method name missing.');
                return;
            }
            opts.methodName = methodName;
        }
        // Fire-and-forget: openHelpFiles is async and resolves when
        // the user closes the manager. We do not await here.
        //
        // We also CLOSE the wizard modal that hosted this button
        // before opening the help-files manager. The previous
        // behaviour (keeping the modal open in the background) read
        // well in theory — "the user comes back after closing help
        // files and finishes their edit" — but in practice it
        // produced an overlap bug: the wizard modal sat on top of
        // the file-manager modal and the user had to click X on it
        // manually before the file manager became usable. Closing
        // here makes the help-files manager the single foreground
        // surface; if the user wants to come back to the wizard
        // modal afterward, the originating card is still on the
        // wizard tab one click away.
        _closeModal(backdrop);
        openHelpFiles(projectId, opts);
    });
}

// =============================================================================
//  Struct modal
// =============================================================================
//
// Fields per design §11.3:
//   - ID            (read-only — the struct name is fixed Go syntax)
//   - Label         (text, required — emitted as `label:<v>.`)
//   - Icon          (text, freeform until slice 6 picker — `icon:<v>.`)
//   - Comment       (textarea — godoc prose above the struct)
//
// Save emits OpSetStructDirectives on the path `struct.<S>`.

function _openStructModal(path) {
    const def = _state.parsed;
    if (!def || `struct.${def.name}` !== path) {
        _persistentToast('danger', 'Internal: struct path mismatch.');
        return;
    }

    // Hydrate from current parsed state. The server emits the
    // human-readable godoc as `doc` (BlackBoxDef.Doc → json:"doc")
    // with IDS directives already stripped — see extractDocDirectives
    // in server/codegen/blackbox/parser.go. Older client code looked
    // at `.comment`/`.structComment` which were never set; tolerate
    // both for safety while preferring the real field.
    // Pre-fill the Label with the struct's Go name when no human label
    // exists yet. This saves a click for the common case where the
    // identifier is already meaningful — the user can edit or clear if
    // they want a different display name. The placeholder remains for
    // the "empty" state where the user has explicitly cleared the field.
    const initialLabel   = def.structLabel || def.displayName || def.name || '';
    const initialIcon    = def.structIcon  || '';
    const initialComment = def.doc || def.structComment || def.comment || '';

    _openModal({
        title: `Struct · ${def.name}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="${esc(def.name)}" readonly
                       title="The Go struct name is fixed by the source.">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-struct-label">Label <span class="wiz-form-req">*</span></label>
                <input id="wiz-struct-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="Display name in the IDE">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-struct-icon-input">Icon</label>
                ${_iconPickerHTML('wiz-struct-icon', initialIcon, { placeholder: 'e.g. cube, gauge, microchip' })}
                <p class="wiz-form-help">
                    Click an icon below or type its FontAwesome name.
                    Leave empty to use the default cube.
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-struct-comment">Comment <span class="wiz-form-req">*</span></label>
                <textarea id="wiz-struct-comment" class="wiz-form-input wiz-form-textarea"
                          rows="3" placeholder="What does this device do? Required.">${esc(initialComment)}</textarea>
                <p class="wiz-form-help">
                    Becomes the godoc above the struct. A short paragraph
                    explaining what the device is for.
                </p>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            ${_buildAddHelpButton()}
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });

            // Wire the icon picker — input/search/grid handlers,
            // live preview, click-to-select.
            _attachIconPicker(backdrop, 'wiz-struct-icon');

            // Add help opens the file manager with the create-file
            // sub-modal pre-populated for the device readme entry
            // ("main menu text"). The wizard modal stays open in the
            // background so the user can return and finish editing.
            _wireAddHelpButton(backdrop, 'newStruct');

            // Save handler.
            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label   = backdrop.querySelector('#wiz-struct-label').value.trim();
                const icon    = backdrop.querySelector('#wiz-struct-icon-input').value.trim();
                const comment = backdrop.querySelector('#wiz-struct-comment').value.trim();

                if (!label) {
                    _setModalError(backdrop, 'Label is required.');
                    return;
                }
                if (!comment) {
                    _setModalError(backdrop, 'Comment is required — describe what this device does.');
                    return;
                }

                await _applyEditAndClose(backdrop, {
                    op: 'setStructDirectives',
                    path: path,
                    args: { label, icon, comment },
                });
            });

            // Auto-focus the first writable input.
            setTimeout(() => backdrop.querySelector('#wiz-struct-label')?.focus(), 60);
        },
    });
}

// =============================================================================
//  Enum modals — Slice C99-6
// =============================================================================
//
// Two modals mirror the enum's two edit targets:
//
//   _openEnumModal      — `enum.<Name>`              : icon + label
//                          of the whole enum (and optional comment).
//   _openEnumValueModal — `enum.<Name>.value.<V>`    : the label of
//                          a single enumerator.
//
// Both emit the shared `setStructDirectives` op; the C99 rewrite
// engine routes enum paths to its enum handlers. The enumerator's
// name and integer value are fixed by the source and shown
// read-only — only the human label is editable.

// _findEnumByPath returns the EnumDef whose name matches `enum.<N>`,
// or null. Used by both enum modals to hydrate their initial state.
function _findEnumByPath(name) {
    const enums = (_state.parsed && _state.parsed.enums) || [];
    return enums.find(e => e.name === name) || null;
}

function _openEnumModal(path) {
    const name = path.slice('enum.'.length);
    const ed = _findEnumByPath(name);
    if (!ed) {
        _persistentToast('danger', 'Internal: enum path mismatch.');
        return;
    }

    const initialLabel   = ed.label || ed.name || '';
    const initialIcon    = ed.icon || '';
    const initialComment = ed.doc || '';

    _openModal({
        title: `Enum · ${ed.name}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="${esc(ed.name)}" readonly
                       title="The C enum name is fixed by the source.">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-enum-label">Label <span class="wiz-form-req">*</span></label>
                <input id="wiz-enum-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="Display name in the IDE">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-enum-icon-input">Icon</label>
                ${_iconPickerHTML('wiz-enum-icon', initialIcon, { placeholder: 'e.g. list-ol, palette' })}
                <p class="wiz-form-help">
                    Click an icon below or type its FontAwesome name.
                    Leave empty to use the default list icon.
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-enum-comment">Comment</label>
                <textarea id="wiz-enum-comment" class="wiz-form-input wiz-form-textarea"
                          rows="2" placeholder="Optional: what does this set of values represent?">${esc(initialComment)}</textarea>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });
            _attachIconPicker(backdrop, 'wiz-enum-icon');

            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label   = backdrop.querySelector('#wiz-enum-label').value.trim();
                const icon    = backdrop.querySelector('#wiz-enum-icon-input').value.trim();
                const comment = backdrop.querySelector('#wiz-enum-comment').value.trim();

                if (!label) {
                    _setModalError(backdrop, 'Label is required.');
                    return;
                }

                await _applyEditAndClose(backdrop, {
                    op: 'setStructDirectives',
                    path: path,
                    args: { label, icon, comment },
                });
            });

            setTimeout(() => backdrop.querySelector('#wiz-enum-label')?.focus(), 60);
        },
    });
}

function _findWireTypeByName(name) {
    const wts = (_state.parsed && _state.parsed.wireTypes) || [];
    return wts.find(w => w.name === name) || null;
}

// _openWireTypeModal edits a wire-type's display identity (label +
// icon). The wire-type carries no ports, so this is the whole modal.
// The save goes through setStructDirectives on `struct.<Name>` (the
// struct tag), which the C99 rewrite locates in the source and writes
// the directive comment above — exactly the path used for any struct.
function _openWireTypeModal(path) {
    const name = path.slice('wiretype.'.length);
    const wt = _findWireTypeByName(name);
    if (!wt) {
        _persistentToast('danger', 'Internal: wire-type path mismatch.');
        return;
    }

    const techName = wt.alias || wt.name;
    const initialLabel = wt.label || '';
    const initialIcon = wt.icon || '';
    const initialComment = wt.doc || '';

    _openModal({
        title: `Wire type · ${techName}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">Type</label>
                <input type="text" class="wiz-form-input" value="${esc(techName)}" readonly
                       title="The C type name is fixed by the source.">
                <p class="wiz-form-help">
                    A value of this type travels on a wire — produced by one
                    device and consumed by others. It is not a device itself.
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-wiretype-label">Label <span class="wiz-form-req">*</span></label>
                <input id="wiz-wiretype-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="e.g. Sensor handle">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-wiretype-icon-input">Icon <span class="wiz-form-req">*</span></label>
                ${_iconPickerHTML('wiz-wiretype-icon', initialIcon, { placeholder: 'e.g. plug, microchip' })}
                <p class="wiz-form-help">
                    Click an icon below or type its FontAwesome name. Gives
                    the wire a recognisable identity in the IDE.
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-wiretype-comment">Comment</label>
                <textarea id="wiz-wiretype-comment" class="wiz-form-input wiz-form-textarea"
                          rows="2" placeholder="Optional: what does this handle represent?">${esc(initialComment)}</textarea>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });
            _attachIconPicker(backdrop, 'wiz-wiretype-icon');

            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label = backdrop.querySelector('#wiz-wiretype-label').value.trim();
                const icon = backdrop.querySelector('#wiz-wiretype-icon-input').value.trim();
                const comment = backdrop.querySelector('#wiz-wiretype-comment').value.trim();

                if (!label) {
                    _setModalError(backdrop, 'Label is required.');
                    return;
                }
                if (!icon) {
                    _setModalError(backdrop, 'Icon is required — give the wire a recognisable identity.');
                    return;
                }

                await _applyEditAndClose(backdrop, {
                    op: 'setStructDirectives',
                    path: `struct.${wt.name}`,
                    args: { label, icon, comment },
                });
            });

            setTimeout(() => backdrop.querySelector('#wiz-wiretype-label')?.focus(), 60);
        },
    });
}

function _openEnumValueModal(path) {
    // path === enum.<Name>.value.<ValueName>
    const m = /^enum\.([^.]+)\.value\.([^.]+)$/.exec(path);
    if (!m) {
        _persistentToast('danger', 'Internal: enum value path malformed.');
        return;
    }
    const enumName  = m[1];
    const valueName = m[2];
    const ed = _findEnumByPath(enumName);
    const val = ed && (ed.values || []).find(v => v.name === valueName);
    if (!ed || !val) {
        _persistentToast('danger', 'Internal: enum value not found.');
        return;
    }

    const shownValue = val.valueIsRaw ? (val.rawValue || '?') : String(val.value);
    const initialLabel = val.label || '';

    _openModal({
        title: `Enum value · ${valueName}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">Enumerator</label>
                <input type="text" class="wiz-form-input" value="${esc(valueName)}" readonly
                       title="The C enumerator name is fixed by the source.">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label">Value</label>
                <input type="text" class="wiz-form-input" value="${esc(shownValue)}" readonly
                       title="The integer value is computed from the source per C99 rules.">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-enumval-label">Label <span class="wiz-form-req">*</span></label>
                <input id="wiz-enumval-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="What the maker sees in the dropdown">
                <p class="wiz-form-help">
                    This is the human-readable name shown in the IDE dropdown.
                    The integer value (${esc(shownValue)}) is what reaches generated code.
                </p>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });

            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label = backdrop.querySelector('#wiz-enumval-label').value.trim();
                if (!label) {
                    _setModalError(backdrop, 'Label is required.');
                    return;
                }
                await _applyEditAndClose(backdrop, {
                    op: 'setStructDirectives',
                    path: path,
                    args: { label },
                });
            });

            setTimeout(() => backdrop.querySelector('#wiz-enumval-label')?.focus(), 60);
        },
    });
}

// =============================================================================
//  Field modal
// =============================================================================
//
// Fields per design §11.3 + §5.3 (Format conditionals):
//   - ID            (read-only)
//   - Disable       (checkbox — when on, all other inputs are disabled
//                    and Save emits disableFieldProp)
//   - Label         (required when not disabled)
//   - Default       (optional)
//   - Format        (dropdown: empty / options / range_min_max /
//                    range_min / range_max / regex)
//                   - options:        textarea, one value per line
//                   - range_min_max:  two number inputs (min, max)
//                   - range_min:      one number input (min)
//                   - range_max:      one number input (max)
//                   - regex:          one text input (pattern)
//   - Unit          (optional — emitted as `unit:` IDS pair)
//   - Comment       (textarea — field godoc prose)
//
// Note: §11.3's table also lists "Icon" but the engine's
// `setFieldPropArgs` does not accept an icon — IDS standard inherits
// the field's icon from the struct, so a per-field icon would be
// out of spec. The Icon row is omitted here; if Kemper decides
// fields should override icons after all, both the engine arg struct
// and this modal need extending.
//
// Save emits OpSetFieldProp (or OpDisableFieldProp when Disable is on)
// on the path `struct.<S>.field.<F>`.

function _openFieldModal(path) {
    const m = path.match(/^struct\.([^.]+)\.field\.([^.]+)$/);
    if (!m) {
        _persistentToast('danger', 'Internal: field path malformed.');
        return;
    }
    const structName = m[1];
    const fieldName  = m[2];

    const def = _state.parsed;
    if (!def || def.name !== structName) {
        _persistentToast('danger', 'Internal: struct not found for field.');
        return;
    }
    const prop = (def.props || []).find(p => p.fieldName === fieldName);
    if (!prop) {
        _persistentToast('danger', 'Internal: field not found in parsed struct.');
        return;
    }

    // Hydrate. The parser exposes options as `options`, range as
    // `rangeMin`/`rangeMax` (both filled for range_min_max), regex as
    // `inputRegex`, and unit as `unit`. We invert that into our
    // Format dropdown for editing.
    const initial = _propToFormState(prop);

    _openModal({
        title: `Field · ${def.name}.${fieldName}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="${esc(fieldName)}" readonly>
                <p class="wiz-form-help">Type: <code>${esc(prop.goType || '')}</code></p>
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-checkbox-row">
                    <input id="wiz-field-disable" type="checkbox" ${initial.disabled ? 'checked' : ''}>
                    <span>Disable this field as a wizard prop</span>
                </label>
                <p class="wiz-form-help">
                    Removes the IDS prop tag entirely. The Go field stays in the
                    struct but the IDE no longer surfaces it as configurable.
                </p>
            </div>

            <fieldset id="wiz-field-fs" class="wiz-form-fieldset"
                      ${initial.disabled ? 'disabled' : ''}>
                <div class="wiz-form-row">
                    <label class="wiz-form-label" for="wiz-field-label">Label <span class="wiz-form-req">*</span></label>
                    <input id="wiz-field-label" type="text" class="wiz-form-input"
                           value="${esc(initial.label)}">
                </div>

                <div class="wiz-form-row">
                    <label class="wiz-form-label" for="wiz-field-default">Default</label>
                    <input id="wiz-field-default" type="text" class="wiz-form-input"
                           value="${esc(initial.default)}" placeholder="Initial value">
                </div>

                <div class="wiz-form-row">
                    <label class="wiz-form-label" for="wiz-field-format">Format</label>
                    <select id="wiz-field-format" class="wiz-form-input">
                        <option value=""              ${initial.format === ''              ? 'selected' : ''}>None — no constraint</option>
                        <option value="options"       ${initial.format === 'options'       ? 'selected' : ''}>Options — pick from a list</option>
                        <option value="range_min_max" ${initial.format === 'range_min_max' ? 'selected' : ''}>Range min..max</option>
                        <option value="range_min"     ${initial.format === 'range_min'     ? 'selected' : ''}>Range — min only</option>
                        <option value="range_max"     ${initial.format === 'range_max'     ? 'selected' : ''}>Range — max only</option>
                        <option value="regex"         ${initial.format === 'regex'         ? 'selected' : ''}>Regex pattern</option>
                    </select>
                </div>

                <!-- Conditional sub-rows. Visibility is driven by
                     the Format dropdown via _refreshFieldFormatVisibility. -->
                <div class="wiz-form-row wiz-format-sub" data-format="options">
                    <label class="wiz-form-label" for="wiz-field-options">Options (one per line)</label>
                    <textarea id="wiz-field-options" class="wiz-form-input wiz-form-textarea"
                              rows="4" placeholder="value1&#10;value2&#10;value3">${esc(initial.optionsText)}</textarea>
                </div>
                <div class="wiz-form-row wiz-format-sub" data-format="range_min_max">
                    <label class="wiz-form-label">Min and Max</label>
                    <div class="wiz-form-grid-2">
                        <input id="wiz-field-min-mm" type="number" class="wiz-form-input"
                               value="${esc(initial.min)}" placeholder="min">
                        <input id="wiz-field-max-mm" type="number" class="wiz-form-input"
                               value="${esc(initial.max)}" placeholder="max">
                    </div>
                </div>
                <div class="wiz-form-row wiz-format-sub" data-format="range_min">
                    <label class="wiz-form-label" for="wiz-field-min-only">Min</label>
                    <input id="wiz-field-min-only" type="number" class="wiz-form-input"
                           value="${esc(initial.min)}">
                </div>
                <div class="wiz-form-row wiz-format-sub" data-format="range_max">
                    <label class="wiz-form-label" for="wiz-field-max-only">Max</label>
                    <input id="wiz-field-max-only" type="number" class="wiz-form-input"
                           value="${esc(initial.max)}">
                </div>
                <div class="wiz-form-row wiz-format-sub" data-format="regex">
                    <label class="wiz-form-label" for="wiz-field-regex">Pattern</label>
                    <input id="wiz-field-regex" type="text" class="wiz-form-input"
                           value="${esc(initial.regex)}" placeholder="^[A-Z]+$">
                    <p class="wiz-form-help">Standard Go regex (RE2) syntax.</p>
                </div>

                <div class="wiz-form-row">
                    <label class="wiz-form-label" for="wiz-field-unit">Unit</label>
                    <input id="wiz-field-unit" type="text" class="wiz-form-input"
                           value="${esc(initial.unit)}" placeholder="e.g. ms, °C, RPM">
                </div>

                <div class="wiz-form-row">
                    <label class="wiz-form-label" for="wiz-field-comment">Comment <span class="wiz-form-req">*</span></label>
                    <textarea id="wiz-field-comment" class="wiz-form-input wiz-form-textarea"
                              rows="3" placeholder="What does this prop control? Required.">${esc(initial.comment)}</textarea>
                    <p class="wiz-form-help">
                        Becomes the godoc above the field. Surfaces in
                        the IDE inspector tooltip.
                    </p>
                </div>
            </fieldset>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            const fmtSelect = backdrop.querySelector('#wiz-field-format');
            const disableChk = backdrop.querySelector('#wiz-field-disable');
            const fieldset   = backdrop.querySelector('#wiz-field-fs');

            // Show/hide the Format conditional rows. Called on mount
            // and on every change of the Format dropdown.
            //
            // Implementation note: the rows have `.wiz-format-sub`
            // which is `display: none` in CSS. The previous version
            // toggled `style.display = ''` to reveal — but `''` only
            // clears the inline override; the static rule still
            // applies and the row stays hidden. Using a `.is-shown`
            // class with higher specificity is the robust fix.
            const refreshFormat = () => {
                const sel = fmtSelect.value;
                backdrop.querySelectorAll('.wiz-format-sub').forEach(row => {
                    row.classList.toggle('is-shown', row.dataset.format === sel);
                });
            };
            fmtSelect.addEventListener('change', refreshFormat);
            refreshFormat();

            // Disable checkbox grays out the whole fieldset. The
            // <fieldset disabled> attribute also blocks form submission
            // for descendant inputs, which is exactly what we want.
            disableChk.addEventListener('change', () => {
                fieldset.disabled = disableChk.checked;
            });

            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });

            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                if (disableChk.checked) {
                    // Disable path — no other inputs matter.
                    await _applyEditAndClose(backdrop, {
                        op: 'disableFieldProp',
                        path: path,
                        args: {},
                    });
                    return;
                }

                // Build args for setFieldProp.
                const label = backdrop.querySelector('#wiz-field-label').value.trim();
                if (!label) {
                    _setModalError(backdrop, 'Label is required.');
                    return;
                }
                const comment = backdrop.querySelector('#wiz-field-comment').value.trim();
                if (!comment) {
                    _setModalError(backdrop, 'Comment is required — describe what this prop controls.');
                    return;
                }
                const args = {
                    label,
                    default: backdrop.querySelector('#wiz-field-default').value,
                    format:  fmtSelect.value,
                    formatArgs: {},
                    unit:    backdrop.querySelector('#wiz-field-unit').value.trim(),
                    comment,
                };

                // Format-specific args. The shapes here mirror what
                // buildPropPairs in rewrite.go expects exactly — see
                // §5.3 of the design doc.
                switch (fmtSelect.value) {
                    case 'options': {
                        const text = backdrop.querySelector('#wiz-field-options').value;
                        const values = text.split('\n')
                            .map(s => s.trim())
                            .filter(Boolean);
                        if (values.length === 0) {
                            _setModalError(backdrop, 'Options format needs at least one value.');
                            return;
                        }
                        args.formatArgs.values = values;
                        break;
                    }
                    case 'range_min_max': {
                        const min = backdrop.querySelector('#wiz-field-min-mm').valueAsNumber;
                        const max = backdrop.querySelector('#wiz-field-max-mm').valueAsNumber;
                        if (Number.isNaN(min) || Number.isNaN(max)) {
                            _setModalError(backdrop, 'Range min..max needs numeric min and max.');
                            return;
                        }
                        if (min > max) {
                            _setModalError(backdrop, 'Min cannot be greater than Max.');
                            return;
                        }
                        args.formatArgs.min = min;
                        args.formatArgs.max = max;
                        break;
                    }
                    case 'range_min': {
                        const min = backdrop.querySelector('#wiz-field-min-only').valueAsNumber;
                        if (Number.isNaN(min)) {
                            _setModalError(backdrop, 'Range min needs a numeric min.');
                            return;
                        }
                        args.formatArgs.min = min;
                        break;
                    }
                    case 'range_max': {
                        const max = backdrop.querySelector('#wiz-field-max-only').valueAsNumber;
                        if (Number.isNaN(max)) {
                            _setModalError(backdrop, 'Range max needs a numeric max.');
                            return;
                        }
                        args.formatArgs.max = max;
                        break;
                    }
                    case 'regex': {
                        const pattern = backdrop.querySelector('#wiz-field-regex').value;
                        if (!pattern) {
                            _setModalError(backdrop, 'Regex format needs a pattern.');
                            return;
                        }
                        args.formatArgs.pattern = pattern;
                        break;
                    }
                }

                await _applyEditAndClose(backdrop, {
                    op: 'setFieldProp',
                    path: path,
                    args,
                });
            });

            // Auto-focus the first writable input. When Disable is
            // pre-checked (rare), focus the Disable checkbox instead.
            setTimeout(() => {
                if (disableChk.checked) disableChk.focus();
                else backdrop.querySelector('#wiz-field-label')?.focus();
            }, 60);
        },
    });
}

// =============================================================================
//  Method modal
// =============================================================================
//
// Fields per design §11.3:
//   - ID              (read-only — the Go function/method name)
//   - Label           (required — `label:<v>.`)
//   - Icon            (text, freeform until slice 6)
//   - Execution order (numeric, allows 0, allows empty)
//   - Comment         (textarea — godoc prose above the method)
//
// Save emits OpSetMethodDirectives on path `method.<S>.<M>` (where
// <M> may be `Init`).
//
// Execution-order semantics (per design §11.3 / Q3.7):
//   - Empty input  → unordered (no executionOrder directive emitted)
//   - 0            → first in the explicit order
//   - N (N>=1)     → explicit order N
//   - Negative     → rejected at the input level (min="0")
//
// "Implicit order is ordered before unordered" — methods with an
// explicit number run before those without. The IDS readme §8 gets
// updated in slice 6 to match this; today it still says "0 or absent
// = unordered" which is now incorrect.
//
// Icon inheritance note: the parser surfaces `fd.icon` as the value
// actually written on the method (empty when the method inherits
// from the struct). The modal pre-fills from `fd.icon` ONLY — NOT
// `fd.icon || structIcon` — because filling with the inherited
// value would, on Save, write an explicit per-method icon directive
// that overrides inheritance. Leaving the input empty keeps the
// inheritance behaviour intact.

function _openMethodModal(path) {
    const m = path.match(/^method\.([^.]+)\.([^.]+)$/);
    if (!m) {
        _persistentToast('danger', 'Internal: method path malformed.');
        return;
    }
    const structName = m[1];
    const methodName = m[2];

    const def = _state.parsed;
    if (!def || def.name !== structName) {
        _persistentToast('danger', 'Internal: struct not found for method.');
        return;
    }
    // Init lives in `def.init`; everything else in `def.methods[]`.
    const fd = methodName === 'Init'
        ? def.init
        : (def.methods || []).find(m => m.name === methodName);
    if (!fd) {
        _persistentToast('danger', 'Internal: method not found in parsed struct.');
        return;
    }

    // Pre-fill the Label with the method name when no human label
    // exists yet. Same reasoning as the struct modal — the identifier
    // already reads well in most cases, and the user can edit or clear.
    const initialLabel   = fd.label || methodName || '';
    // Icon is the per-method-only value — see the comment block above.
    const initialIcon    = fd.icon  || '';
    // Read the godoc for this method. The Go server emits it as `doc`
    // (NamedFuncDef.Doc → json:"doc") with IDS directives stripped.
    // Older client code looked at `.comment` which is never set —
    // hydration bug.
    const initialComment = fd.doc || fd.comment || '';
    // executionOrder may be undefined (no directive), 0 (explicit but still
    // unordered), or a positive integer. Distinguish "0" from "empty"
    // explicitly — both are falsy in JS but are written differently.
    const initialOrder = (fd.executionOrder === undefined || fd.executionOrder === null)
        ? ''
        : String(fd.executionOrder);

    _openModal({
        title: methodName === 'Init'
            ? `Method · ${def.name}.Init (initialiser)`
            : `Method · ${def.name}.${methodName}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="${esc(methodName)}" readonly>
                ${methodName === 'Init'
                    ? '<p class="wiz-form-help">Init runs once when the device is constructed. It is conventionally first; setting Execution order overrides that.</p>'
                    : ''}
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-method-label">Label <span class="wiz-form-req">*</span></label>
                <input id="wiz-method-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="Display name in the IDE">
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-method-icon-input">Icon</label>
                ${_iconPickerHTML('wiz-method-icon', initialIcon, { placeholder: 'leave empty to inherit struct icon' })}
                <p class="wiz-form-help">
                    Click an icon below or type its FontAwesome name.
                    Leave empty to inherit the struct's icon.
                </p>
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-method-order">Execution order</label>
                <input id="wiz-method-order" type="number" min="0" step="1" class="wiz-form-input"
                       value="${esc(initialOrder)}" placeholder="empty or 0 = unordered; N > 0 for execution order.">
                <p class="wiz-form-help">
                    Methods with an explicit order run first, in ascending order
                    (lower N first, N &ge; 1). Methods without one run after, in
                    source order. Empty or 0 means no specific order, and wires
                    always take precedence over execution order.
                </p>
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-method-comment">Comment <span class="wiz-form-req">*</span></label>
                <textarea id="wiz-method-comment" class="wiz-form-input wiz-form-textarea"
                          rows="3" placeholder="What does this method do? Required.">${esc(initialComment)}</textarea>
                <p class="wiz-form-help">
                    Becomes the godoc above the method. A short paragraph
                    explaining the method's purpose.
                </p>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            ${_buildAddHelpButton()}
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });

            // Wire the icon picker — same widget the struct modal uses.
            _attachIconPicker(backdrop, 'wiz-method-icon');

            // Add help opens the file manager with the create-file
            // sub-modal pre-populated for this method's entry
            // ("Device <Name>"). methodName is the bare Go identifier
            // captured from the path so help_files can match the
            // dropdown basename exactly.
            _wireAddHelpButton(backdrop, 'newMethod', methodName);

            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label   = backdrop.querySelector('#wiz-method-label').value.trim();
                const icon    = backdrop.querySelector('#wiz-method-icon-input').value.trim();
                const comment = backdrop.querySelector('#wiz-method-comment').value.trim();
                const orderStr = backdrop.querySelector('#wiz-method-order').value;

                if (!label) {
                    _setModalError(backdrop, 'Label is required.');
                    return;
                }
                if (!comment) {
                    _setModalError(backdrop, 'Comment is required — describe what this method does.');
                    return;
                }

                // Execution order: empty string → omitted (unordered);
                // anything else → integer. The engine receives a *int
                // pointer in Go; we encode that as either undefined
                // (omitted from JSON) or a number.
                const args = { label, icon, comment };
                if (orderStr.trim() !== '') {
                    const order = Number(orderStr);
                    if (!Number.isInteger(order) || order < 0) {
                        _setModalError(backdrop, 'Execution order must be a non-negative integer.');
                        return;
                    }
                    args.executionOrder = order;
                }

                await _applyEditAndClose(backdrop, {
                    op: 'setMethodDirectives',
                    path: path,
                    args,
                });
            });

            setTimeout(() => backdrop.querySelector('#wiz-method-label')?.focus(), 60);
        },
    });
}

// =============================================================================
//  Function-device modals — Slice C99-8
// =============================================================================
//
// A standalone function is its own device. Two modals:
//   _openFunctionModal     — `function.<name>`              : icon/label/comment
//   _openFunctionPortModal — `function.<name>.<in|out>.<P>` : label/connection
//
// Both emit ops the C99 rewrite engine routes via its function-path
// interception (rewrite_c_function.go). The synthetic `return` output
// is rendered inert and never reaches the port modal.

function _findFunctionByName(name) {
    const fns = (_state.parsed && _state.parsed.functions) || [];
    return fns.find(f => f.name === name) || null;
}

function _openFunctionModal(path) {
    const name = path.slice('function.'.length);
    const fn = _findFunctionByName(name);
    if (!fn) {
        _persistentToast('danger', 'Internal: function device not found.');
        return;
    }

    const initialLabel   = fn.label || fn.name || '';
    const initialIcon    = fn.icon || '';
    const initialComment = fn.doc || fn.comment || '';

    // executionOrder may be undefined (no directive), 0 (explicit but still
    // unordered), or a positive integer. Distinguish "0" from "empty"
    // explicitly — both are falsy in JS but are written differently.
    // Mirrors the Go method modal (_openMethodModal) so the C99 and Go
    // wizards behave the same.
    const initialOrder = (fn.executionOrder === undefined || fn.executionOrder === null)
        ? ''
        : String(fn.executionOrder);

    // The return label is NOT edited here (it is edited by clicking
    // the `return` port, for consistency with the other ports). But
    // it lives in the SAME leading comment, so we must preserve it
    // when this modal rewrites the comment — we read the current
    // value and send it back unchanged.
    const returnPort = (fn.outputs || []).find(p => p.name === 'return');
    const currentReturnLabel = returnPort ? (returnPort.label || '') : '';

    // Callback-handler dropdown. The parsed BlackBoxDef gives each function
    // the list of callback typedefs its signature is COMPATIBLE with
    // (compatibleCallbacks) and its current handler type (handlerType). We keep
    // ONE uniform control: a dropdown that is always present, lists
    // "— Not a callback handler —" plus each COMPATIBLE typedef, and is
    // DISABLED when this function matches none (so e.g. an `int(void)` function
    // never gets offered a `void(const char*)` callback). See
    // CODEGEN_C99_CALLBACKS.md §3.3.
    const compatibleCallbacks = fn.compatibleCallbacks || [];
    const initialHandler = fn.handlerType || '';
    const hasCompatible = compatibleCallbacks.length > 0;
    // Callback MODE (both/ref) from the parsed def — the step-1 DTO carries it.
    // "both" (default) → the IDE offers a callable block AND a separate reference
    // block; "ref" → reference only. The checkbox below is checked for both and
    // unchecked for ref, so re-opening a `:ref` function shows it unchecked.
    const initialCbMode = fn.callbackMode || 'both';
    const cbBothChecked = initialCbMode !== 'ref';
    let cbOptions = `<option value="">— Not a callback handler —</option>`;
    compatibleCallbacks.forEach(typeName => {
        const sel = typeName === initialHandler ? ' selected' : '';
        cbOptions += `<option value="${esc(typeName)}"${sel}>${esc(typeName)}</option>`;
    });
    // Defensive: if the function is already a handler of a type no longer
    // present in the compatible list (e.g. its typedef was removed from the
    // source), keep its value visible/selected so saving does not silently
    // drop it.
    if (initialHandler && !compatibleCallbacks.includes(initialHandler)) {
        cbOptions += `<option value="${esc(initialHandler)}" selected>${esc(initialHandler)} (type not found)</option>`;
    }

    _openModal({
        title: `Function · ${fn.name}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="${esc(fn.name)}" readonly
                       title="The C function name is fixed by the source.">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fn-label">Label <span class="wiz-form-req">*</span></label>
                <input id="wiz-fn-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="Display name in the IDE">
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fn-icon-input">Icon</label>
                ${_iconPickerHTML('wiz-fn-icon', initialIcon, { placeholder: 'e.g. bolt, gear' })}
                <p class="wiz-form-help">
                    Click an icon below or type its FontAwesome name.
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fn-callback">Callback handler</label>
                <select id="wiz-fn-callback" class="wiz-form-input"${hasCompatible ? '' : ' disabled'}>
                    ${cbOptions}
                </select>
                <p class="wiz-form-help">
                    ${hasCompatible
                        ? 'If this function is meant to be passed by reference into a callback input (e.g. a "setter"), pick the matching function-pointer type. The IDE then offers a separate reference block (the "ƒ") you can wire into that input — the checkbox below controls whether the normal callable block is offered too.'
                        : 'Disabled — no function-pointer typedef in this source matches this function\'s signature, so it cannot be a callback handler. Define a matching <code>typedef</code> (same return type and parameters) to enable this.'}
                </p>
            </div>
            <div class="wiz-form-row" id="wiz-fn-cb-mode-row"${initialHandler ? '' : ' style="display:none;"'}>
                <label class="wiz-form-label">Block variants</label>
                <label style="display:flex; align-items:center; gap:8px; cursor:pointer;">
                    <input id="wiz-fn-cb-both" type="checkbox"${cbBothChecked ? ' checked' : ''}>
                    <span>Also generate the normal function block</span>
                </label>
                <p class="wiz-form-help">
                    Checked: the IDE offers BOTH a callable block (its parameters as
                    inputs) and a separate reference block (the "ƒ", passed by
                    address). Unchecked: only the reference block is offered.
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fn-order">Execution order</label>
                <input id="wiz-fn-order" type="number" min="0" step="1" class="wiz-form-input"
                       value="${esc(initialOrder)}" placeholder="empty or 0 = unordered; N > 0 for execution order.">
                <p class="wiz-form-help">
                    Functions with an explicit order run first, in ascending
                    order (lower N first, N &ge; 1). Functions without one run
                    after, in source order. Empty or 0 means no specific order,
                    and wires always take precedence over execution order.
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fn-comment">Comment <span class="wiz-form-req">*</span></label>
                <textarea id="wiz-fn-comment" class="wiz-form-input wiz-form-textarea"
                          rows="3" placeholder="What does this function do? Required.">${esc(initialComment)}</textarea>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            ${_buildAddHelpButton()}
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });
            _attachIconPicker(backdrop, 'wiz-fn-icon');
            _wireAddHelpButton(backdrop, 'newMethod', fn.name);

            // Show the both/ref checkbox only while a callback type is selected
            // ("— Not a callback handler —" hides it — the mode is meaningless).
            const cbSelect = backdrop.querySelector('#wiz-fn-callback');
            const cbModeRow = backdrop.querySelector('#wiz-fn-cb-mode-row');
            if (cbSelect && cbModeRow) {
                cbSelect.addEventListener('change', () => {
                    cbModeRow.style.display = cbSelect.value ? '' : 'none';
                });
            }

            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label   = backdrop.querySelector('#wiz-fn-label').value.trim();
                const icon    = backdrop.querySelector('#wiz-fn-icon-input').value.trim();
                const comment = backdrop.querySelector('#wiz-fn-comment').value.trim();
                const orderStr = backdrop.querySelector('#wiz-fn-order').value;
                const callback = backdrop.querySelector('#wiz-fn-callback').value;

                if (!label) {
                    _setModalError(backdrop, 'Label is required.');
                    return;
                }
                if (!comment) {
                    _setModalError(backdrop, 'Comment is required — describe what this function does.');
                    return;
                }

                // Execution order: empty string → omitted (unordered);
                // anything else → integer. Same encoding as the Go method
                // modal — the engine receives a *int (undefined = clear,
                // number = set). returnLabel is preserved so editing the
                // device doesn't wipe a label set on the return port.
                const args = { label, icon, comment, returnLabel: currentReturnLabel };
                if (orderStr.trim() !== '') {
                    const order = Number(orderStr);
                    if (!Number.isInteger(order) || order < 0) {
                        _setModalError(backdrop, 'Execution order must be a non-negative integer.');
                        return;
                    }
                    args.executionOrder = order;
                }
                // Callback handler: a chosen type marks the function as that
                // callback's handler (`callback:T.`); the empty first option
                // omits it, which clears the directive (block rebuilt from args).
                if (callback) {
                    args.callback = callback;
                    // "Also generate the normal function block" checked → both
                    // (callable + reference); unchecked → ref (reference only).
                    // Only sent when a handler type is chosen — meaningless
                    // otherwise, and the rewrite writes `:ref` only for ref.
                    const alsoCallable = backdrop.querySelector('#wiz-fn-cb-both');
                    args.callbackMode = (alsoCallable && alsoCallable.checked) ? 'both' : 'ref';
                }

                await _applyEditAndClose(backdrop, {
                    op: 'setStructDirectives',
                    path: path,
                    args,
                });
            });

            setTimeout(() => backdrop.querySelector('#wiz-fn-label')?.focus(), 60);
        },
    });
}

// _openFunctionReturnModal edits the human label of a function's
// `return` output. C99 gives the return value no name or position,
// so the label is persisted as `return:<label>.` in the function's
// leading comment — written by the device directives planner. To
// avoid clobbering the device's own label/icon/comment (which share
// that comment), this modal reads their current values and sends
// them back unchanged, editing only returnLabel.
function _openFunctionReturnModal(fnName) {
    const fn = _findFunctionByName(fnName);
    const returnPort = fn && (fn.outputs || []).find(p => p.name === 'return');
    if (!fn || !returnPort) {
        _persistentToast('danger', 'Internal: return port not found.');
        return;
    }

    const initialLabel = returnPort.label || '';

    _openModal({
        title: `Return · ${fnName}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="return" readonly>
                <p class="wiz-form-help">
                    Direction: <code>output</code> ·
                    Type: <code>${esc(returnPort.goType || '')}</code>
                </p>
            </div>
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-ret-label">Label</label>
                <input id="wiz-ret-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="Human name for the return value">
                <p class="wiz-form-help">
                    Shown on the return pin in the IDE. Codegen still uses
                    the real type (<code>${esc(returnPort.goType || '')}</code>).
                </p>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });
            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const returnLabel = backdrop.querySelector('#wiz-ret-label').value.trim();
                await _applyEditAndClose(backdrop, {
                    op: 'setStructDirectives',
                    path: `function.${fnName}`,
                    // Preserve the device's own directives; change only
                    // the return label.
                    args: {
                        label:   fn.label || fn.name || '',
                        icon:    fn.icon || '',
                        comment: fn.doc || fn.comment || '',
                        returnLabel,
                    },
                });
            });
            setTimeout(() => backdrop.querySelector('#wiz-ret-label')?.focus(), 60);
        },
    });
}

function _openFunctionPortModal(path) {
    const m = /^function\.([^.]+)\.(in|out)\.([^.]+)$/.exec(path);
    if (!m) {
        _persistentToast('danger', 'Internal: function port path malformed.');
        return;
    }
    const fnName = m[1];
    const dir = m[2];
    const portName = m[3];

    const fn = _findFunctionByName(fnName);
    const list = fn && (dir === 'in' ? fn.inputs : fn.outputs);
    const port = list && list.find(p => p.name === portName);
    if (!fn || !port) {
        _persistentToast('danger', 'Internal: function port not found.');
        return;
    }

    // ── Synthetic callback reference output — READ-ONLY ──────────────────────
    // A `// callback:<type>.` directive turns the function into a handler and
    // gives it ONE output pin named "callback", typed as the callback typedef
    // (callbackType set, direction "out"). That pin is SYNTHETIC: it has no
    // backing parameter in the C source, so there is nothing for the port
    // editor to rewrite. Emitting setPortConnection for it makes the rewrite
    // engine look for a parameter named `${portName}` that does not exist and
    // reject the edit ("port <fn>.<port> not found") — exactly the error the
    // editable form produced. Its label and type are fixed by the directive;
    // the maker's only interaction with it is WIRING it into a callback input
    // on the stage. So show a read-only explainer instead of the form.
    //
    // NOTE: a callback INPUT (e.g. setDisplay's `writer`, callbackType set but
    // direction "in") IS a real parameter and stays fully editable below — the
    // guard is intentionally limited to the output reference.
    const isCallbackRef = (dir === 'out') && !!(port.callbackType && String(port.callbackType).trim());
    if (isCallbackRef) {
        const cbType = String(port.callbackType).trim();
        _openModal({
            title: `Port · ${fnName} · output · ${portName}`,
            bodyHtml: `
                <div class="wiz-form-row">
                    <label class="wiz-form-label">ID</label>
                    <input type="text" class="wiz-form-input" value="${esc(portName)}" readonly>
                    <p class="wiz-form-help">
                        Direction: <code>output</code> ·
                        Type: <code>${esc(port.goType || cbType)}</code>
                    </p>
                </div>
                <div class="wiz-form-row">
                    <p class="wiz-form-help" style="margin-top:0">
                        This is the <strong>callback reference</strong> generated by the
                        <code>// callback:${esc(cbType)}.</code> directive on
                        <code>${esc(fnName)}</code>. It is a reference to the function
                        itself, not a parameter, so it has no label, connection or
                        comment of its own to edit here.
                    </p>
                    <p class="wiz-form-help">
                        To use it, wire this pin into a matching callback input — any
                        parameter typed <code>${esc(cbType)}</code> — on the IDE stage.
                        The generated code passes this function by reference into that
                        parameter.
                    </p>
                </div>`,
            footerHtml: `
                <button type="button" class="btn btn-primary wiz-modal-cancel">Close</button>`,
            onMount: (backdrop) => {
                backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                    _closeModal(backdrop);
                });
            },
        });
        return;
    }

    // This modal mirrors the Go method-port modal (_openPortModal) so
    // the maker sees the SAME fields, options, and help text whether
    // the device came from Go or C99. Go is the standard; the only
    // differences here are the path shape (`function.*`) and the data
    // source (parsed.functions[] instead of struct methods).
    const initialLabel = port.label || portName || '';
    const initialComment = port.doc || port.comment || '';

    // Connection is REQUIRED for inputs. Like the Go modal, we branch
    // on the server's `missingConn` flag rather than port.connection,
    // because the parser fills connection with "optional" by default
    // when missingConn is true — so reading connection alone makes
    // "no choice yet" indistinguishable from "user picked optional".
    let initialConnection = '';
    if (!port.missingConn) {
        const currentConn = (port.connection || '').toLowerCase();
        if (currentConn === 'optional' || currentConn === 'mandatory') {
            initialConnection = currentConn;
        }
    }

    // Only inputs collect a connection. A parameter that is a mutable
    // pointer MAY be flipped to an output via the checkbox below; when
    // it is an output, the connection dropdown is hidden (outputs are
    // not "wired-or-not" the way inputs are). The checkbox itself only
    // appears for pointers that CAN be outputs (canBeOutput); a value,
    // const pointer or void* is always an input.
    const canOut = _canBeOutput(port.goType);
    const isCurrentlyOut = (dir === 'out');

    _openModal({
        title: `Port · ${fnName} · ${dir === 'in' ? 'input' : 'output'} · ${portName}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="${esc(portName)}" readonly>
                <p class="wiz-form-help">
                    Direction: <code>${dir === 'in' ? 'input' : 'output'}</code> ·
                    Type: <code>${esc(port.goType || '')}</code>
                </p>
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fnport-label">Label</label>
                <input id="wiz-fnport-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="Display name in the IDE">
            </div>

            ${canOut ? `
            <div class="wiz-form-row">
                <label class="wiz-form-label" style="display:flex;align-items:center;gap:8px;cursor:pointer">
                    <input id="wiz-fnport-output" type="checkbox" ${isCurrentlyOut ? 'checked' : ''}>
                    Output — show this pin on the output side
                </label>
                <p class="wiz-form-help">
                    Direction is only how the pin is drawn. In the generated
                    code this is still a parameter, so set its Connection
                    below either way. Tick this if the function returns a
                    value through this pointer and you want it to flow out.
                </p>
            </div>
            ` : ''}

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fnport-conn">Connection <span class="wiz-form-req">*</span></label>
                <select id="wiz-fnport-conn" class="wiz-form-input">
                    <option value="" ${initialConnection === '' ? 'selected' : ''}>— choose —</option>
                    <option value="optional"  ${initialConnection === 'optional'  ? 'selected' : ''}>Optional — port may be left unwired</option>
                    <option value="mandatory" ${initialConnection === 'mandatory' ? 'selected' : ''}>Mandatory — port must be wired</option>
                </select>
                <p class="wiz-form-help">
                    Required. Every parameter — input or output — takes an
                    argument in the generated call, so this decides whether
                    the IDE's wiring validator flags the pin as required.
                </p>
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-fnport-comment">Comment <span class="wiz-form-req">*</span></label>
                <textarea id="wiz-fnport-comment" class="wiz-form-input wiz-form-textarea"
                          rows="3" placeholder="What does this port carry? Required.">${esc(initialComment)}</textarea>
                <p class="wiz-form-help">
                    Surfaces in the IDE inspector and as godoc on the source.
                    A short sentence is enough.
                </p>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            const outputCb = backdrop.querySelector('#wiz-fnport-output');
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });
            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label = backdrop.querySelector('#wiz-fnport-label').value.trim();
                const comment = backdrop.querySelector('#wiz-fnport-comment').value.trim();

                if (!comment) {
                    _setModalError(backdrop, 'Comment is required — describe what this port carries.');
                    return;
                }

                // Connection is required for every parameter, regardless
                // of direction (it is still an argument in the call).
                const connection = backdrop.querySelector('#wiz-fnport-conn').value;
                if (connection === '') {
                    _setModalError(backdrop, 'Connection is required — pick Optional or Mandatory.');
                    return;
                }
                if (connection !== 'optional' && connection !== 'mandatory') {
                    _setModalError(backdrop, 'Internal: invalid connection value.');
                    return;
                }

                const isOutput = !!(outputCb && outputCb.checked);

                await _applyEditAndClose(backdrop, {
                    op: 'setPortConnection',
                    path: path,
                    args: { label, connection, comment, direction: isOutput ? 'out' : 'in' },
                });
            });
            setTimeout(() => backdrop.querySelector('#wiz-fnport-label')?.focus(), 60);
        },
    });
}

// =============================================================================
//  Port modal
// =============================================================================
//
// Fields per design §11.3:
//   - ID         (read-only — the Go parameter/return name)
//   - Label      (text — emitted as `label:<v>.`)
//   - Connection (dropdown — Optional / Mandatory)
//   - Comment    (textarea — godoc prose on the port)
//
// Save emits OpSetPortConnection on path
// `method.<S>.<M>.{in|out}.<P>`.
//
// The engine accepts an empty `connection` (no directive emitted) but
// the spec lists only Optional and Mandatory for the dropdown — a
// port without a connection directive is precisely what the
// completion engine flags as incomplete, so the modal nudges the
// user toward setting one. The dropdown defaults to "optional" when
// no value is currently set.
//
// Error returns: the parser surfaces `port.isError = true` for a
// method's `error` return value. The cards renderer marks those
// rows inert (the dispatcher never routes them here), but if a path
// to an error-port ever reaches this function we surface a polite
// refusal rather than emitting a connection directive that the
// codegen rule explicitly exempts.

function _openPortModal(path) {
    const m = path.match(/^method\.([^.]+)\.([^.]+)\.(in|out)\.([^.]+)$/);
    if (!m) {
        _persistentToast('danger', 'Internal: port path malformed.');
        return;
    }
    const [, structName, methodName, dir, portName] = m;

    const def = _state.parsed;
    if (!def || def.name !== structName) {
        _persistentToast('danger', 'Internal: struct not found for port.');
        return;
    }
    const fd = methodName === 'Init'
        ? def.init
        : (def.methods || []).find(mm => mm.name === methodName);
    if (!fd) {
        _persistentToast('danger', 'Internal: method not found for port.');
        return;
    }
    const portList = dir === 'in' ? (fd.inputs || []) : (fd.outputs || []);
    const port = portList.find(p => p.name === portName);
    if (!port) {
        _persistentToast('danger', 'Internal: port not found in parsed method.');
        return;
    }

    // Error returns can still have a Label — the IDE shows it in the
    // device's pin tooltip — but their Connection is fixed: the
    // runtime auto-handles errors, so the wiring validator doesn't
    // care whether they are wired or not. Hide the Connection
    // dropdown for error ports rather than refuse the modal entirely.
    const isError = !!port.isError;

    // Pre-fill the Label with the port name (the Go parameter or
    // return identifier). Same reasoning as the struct/method modals
    // — short identifiers like "red" or "i2c" already read well; the
    // user can edit or clear to use the placeholder.
    const initialLabel   = port.label || portName || '';
    // Read the godoc for this port. The Go server emits it as `doc`
    // (see PortDef.Doc → json:"doc"). Older client code looked at
    // .comment which is never set — that was a hydration bug.
    const initialComment = port.doc || port.comment || '';
    // Connection is REQUIRED. The source either declares
    // `connection:mandatory.` / `connection:optional.` explicitly,
    // or it doesn't — and when it doesn't, the wizard wants the
    // user to make the choice deliberately rather than accept a
    // silent default.
    //
    // The authoritative "no connection set" signal is the server's
    // `missingConn` flag. The `port.connection` field is NOT a
    // reliable signal because the parser fills it with "optional"
    // by default when missingConn is true, so reading
    // port.connection alone makes "no choice yet" look identical
    // to "user picked optional". We branch on missingConn first.
    let initialConnection = '';
    if (!port.missingConn) {
        const currentConn = (port.connection || '').toLowerCase();
        if (currentConn === 'optional' || currentConn === 'mandatory') {
            initialConnection = currentConn;
        }
    }

    // Slice-7 rule: only inputs require a connection: directive.
    // Outputs (regular and error) are always optional connection-wise
    // — there is no semantic for "this output must be wired", so the
    // wizard hides the dropdown entirely on outputs. The save handler
    // sends connection="" in that case and the rewrite engine drops
    // the directive (and any pre-existing zombie `connection:` line).
    const showConnection = (dir === 'in');

    _openModal({
        title: `Port · ${def.name}.${methodName} · ${dir === 'in' ? 'input' : 'output'} · ${portName}`,
        bodyHtml: `
            <div class="wiz-form-row">
                <label class="wiz-form-label">ID</label>
                <input type="text" class="wiz-form-input" value="${esc(portName)}" readonly>
                <p class="wiz-form-help">
                    Direction: <code>${dir === 'in' ? 'input' : 'output'}</code> ·
                    Type: <code>${esc(port.goType || '')}</code>
                    ${isError ? ' · <strong>error return — runtime-handled</strong>' : ''}
                </p>
            </div>

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-port-label">Label</label>
                <input id="wiz-port-label" type="text" class="wiz-form-input"
                       value="${esc(initialLabel)}" placeholder="Display name in the IDE">
            </div>

            ${showConnection ? `
            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-port-connection">Connection <span class="wiz-form-req">*</span></label>
                <select id="wiz-port-connection" class="wiz-form-input">
                    <option value="" ${initialConnection === '' ? 'selected' : ''}>— choose —</option>
                    <option value="optional"  ${initialConnection === 'optional'  ? 'selected' : ''}>Optional — port may be left unwired</option>
                    <option value="mandatory" ${initialConnection === 'mandatory' ? 'selected' : ''}>Mandatory — port must be wired</option>
                </select>
                <p class="wiz-form-help">
                    Required. Determines whether the IDE's wiring validator
                    flags this input as required when laying out the device
                    on the canvas.
                </p>
            </div>
            ` : ''}

            <div class="wiz-form-row">
                <label class="wiz-form-label" for="wiz-port-comment">Comment <span class="wiz-form-req">*</span></label>
                <textarea id="wiz-port-comment" class="wiz-form-input wiz-form-textarea"
                          rows="3" placeholder="What does this port carry? Required.">${esc(initialComment)}</textarea>
                <p class="wiz-form-help">
                    Surfaces in the IDE inspector and as godoc on the source.
                    A short sentence is enough.
                </p>
            </div>`,
        footerHtml: `
            <button type="button" class="btn btn-ghost wiz-modal-cancel">Cancel</button>
            <button type="button" class="btn btn-primary wiz-modal-save">
                <i class="fa-solid fa-floppy-disk"></i> Save
            </button>`,
        onMount: (backdrop) => {
            backdrop.querySelector('.wiz-modal-cancel').addEventListener('click', () => {
                _closeModal(backdrop);
            });

            backdrop.querySelector('.wiz-modal-save').addEventListener('click', async () => {
                const label   = backdrop.querySelector('#wiz-port-label').value.trim();
                const comment = backdrop.querySelector('#wiz-port-comment').value.trim();

                if (!comment) {
                    _setModalError(backdrop, 'Comment is required — describe what this port carries.');
                    return;
                }

                // Connection: only inputs collect this from the user.
                // Outputs (regular and error) always send "" because
                // the rewrite engine ignores connection on outputs
                // anyway (slice-7 rule). Keeping the empty string
                // explicit here makes the contract obvious.
                let connection = '';
                if (showConnection) {
                    connection = backdrop.querySelector('#wiz-port-connection').value;
                    if (connection === '') {
                        _setModalError(backdrop, 'Connection is required — pick Optional or Mandatory.');
                        return;
                    }
                    if (connection !== 'optional' && connection !== 'mandatory') {
                        _setModalError(backdrop, 'Internal: invalid connection value.');
                        return;
                    }
                }

                await _applyEditAndClose(backdrop, {
                    op: 'setPortConnection',
                    path: path,
                    args: { connection, label, comment },
                });
            });

            setTimeout(() => backdrop.querySelector('#wiz-port-label')?.focus(), 60);
        },
    });
}


// _propToFormState maps a parsed prop into the modal's initial form
// state. The parser surfaces format-specific data through distinct
// fields (`options`, `rangeMin`, `rangeMax`, `inputRegex`) rather
// than a single `format` discriminator; we collapse them back here.
function _propToFormState(prop) {
    // The "disabled" path was originally inferred from "no data
    // anywhere" because the only props the parser surfaced were
    // tagged ones — empty meant "tag was disabled by the user".
    // Slice 6+ surfaces untagged exported fields too, where empty
    // is the EXPECTED initial state. So now we drive `disabled`
    // off the explicit `untagged` flag from the parser when
    // available, falling back to the old heuristic only for older
    // server responses that don't emit the flag.
    //
    // Important: an untagged field is NOT disabled — it's just
    // un-promoted-yet. Saving the modal adds the prop tag.
    const untaggedKnown = (typeof prop.untagged === 'boolean');
    const disabled = untaggedKnown
        ? false  // never start disabled when we know there's no tag yet
        : (!prop.label && !prop.default && !prop.options && !prop.rangeMin && !prop.rangeMax && !prop.inputRegex && !prop.unit);

    const out = {
        disabled,
        label:       prop.label   || prop.fieldName || '',
        default:     prop.default || '',
        unit:        prop.unit    || '',
        comment:     prop.doc || prop.comment || '',
        format:      '',
        optionsText: '',
        min:         '',
        max:         '',
        regex:       '',
    };
    // The parser fills options/rangeMin/rangeMax/inputRegex from the
    // IDS tag at parse time. We invert the discriminator: range_min_max
    // takes priority over the single-bound versions because both
    // bounds being present implies the user actually meant min..max.
    if (Array.isArray(prop.options) && prop.options.length > 0) {
        out.format = 'options';
        out.optionsText = prop.options.join('\n');
    } else if (prop.rangeMin !== undefined && prop.rangeMax !== undefined
               && prop.rangeMin !== '' && prop.rangeMax !== '') {
        out.format = 'range_min_max';
        out.min = String(prop.rangeMin);
        out.max = String(prop.rangeMax);
    } else if (prop.rangeMin !== undefined && prop.rangeMin !== '') {
        out.format = 'range_min';
        out.min = String(prop.rangeMin);
    } else if (prop.rangeMax !== undefined && prop.rangeMax !== '') {
        out.format = 'range_max';
        out.max = String(prop.rangeMax);
    } else if (prop.inputRegex) {
        out.format = 'regex';
        out.regex = prop.inputRegex;
    }
    return out;
}

// =============================================================================
//  Helpers
// =============================================================================

// _persistentToast surfaces a notification that does not auto-dismiss.
// The shared `toast()` helper in utils.js treats `duration === 0` as
// "keep until the user clicks it" — see utils.js. Used for errors and
// warnings the user must actually see, like a draft save failure
// mid-session, where a 3-second auto-flash could miss them entirely.
function _persistentToast(type, message) {
    toast(type, message, 0);
}

// _faIconClass returns the Font Awesome family class for a given icon
// name. FontAwesome Free splits its catalogue between three families —
// `fa-solid`, `fa-regular`, and `fa-brands` — and an icon may exist in
// only one of them under the Free licence. Picking the wrong family
// renders the missing-glyph tofu (□) in the browser even though the
// icon name is correct.
//
// The authoritative source is `window.FA_FREE_STYLES`, populated at
// page load by the generated `/static/js/fa-free-styles.js`. It maps
// each free icon name to a list of font families it ships in:
//
//   FA_FREE_STYLES = {
//     "alarm-clock": ["regular"],   // only available as outline
//     "abacus":      ["solid"],     // only available as filled
//     "github":      ["brands"],    // brand glyph
//     "address-book":["regular","solid"],
//   }
//
// Resolution order: brands → solid → regular. We prefer brands when
// applicable (most specific), then solid (most common), and only
// fall back to regular when the icon ships ONLY as outline — that's
// the path that fixes the user-reported tofu on `aries`, `aquarius`,
// `alarm-clock`, etc.
//
// Fallbacks for missing FA_FREE_STYLES (older clients, regen not run
// yet): consult FA_BRANDS_SET as before, defaulting to fa-solid.
function _faIconClass(name) {
    const styles = window.FA_FREE_STYLES?.[name];
    if (styles) {
        if (styles.includes('brands'))  return 'fa-brands';
        if (styles.includes('solid'))   return 'fa-solid';
        if (styles.includes('regular')) return 'fa-regular';
        // No usable free style — caller will render tofu, but at
        // least we don't lie about the family.
    }
    return window.FA_BRANDS_SET?.has(name) ? 'fa-brands' : 'fa-solid';
}

function _faIcon(name) {
    // Same brand-aware lookup used by the icon picker — keeps cards,
    // method-method headers and ad-hoc icon usages consistent.
    return `<i class="${_faIconClass(name)} fa-${esc(name)}"></i>`;
}

// =============================================================================
//  Icon picker
// =============================================================================
//
// Layout per the user's mockup:
//
//   Icon  [ name input ]  [ live preview ]
//         ┌────────────────────────────────┐
//         │ ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐ │
//         │ │  │ │  │ │  │ │  │ │  │ │  │ │  ← grid of icons
//         │ └──┘ └──┘ └──┘ └──┘ └──┘ └──┘ │     filters live as the user
//         │ ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐ │     types in the Icon input
//         │ │  │ │  │ │  │ │  │ │  │ │  │ │
//         │ └──┘ └──┘ └──┘ └──┘ └──┘ └──┘ │
//         │ ...                            │
//         └────────────────────────────────┘
//
// One input — the same field that holds the canonical icon name is
// also the live filter for the grid. Empty input → all 4111 icons.
// Typing → grid narrows to icons whose name contains the typed
// substring (cap of 120 results so a generic word like "circle"
// doesn't render 600 cells).
//
// Clicking a grid cell sets the input. Typing in the input updates
// both the grid (filter) and the preview (selected icon). All icons
// shown are FontAwesome free and supported by the WASM IDE — same
// list /control's pickers use.
//
// Per-modal namespacing via `idPrefix` so multiple pickers in the
// same backdrop don't collide.
//
// IDs used per prefix `P`:
//   P-input   — the icon-name text input (canonical value + filter)
//   P-preview — the live <i> showing the current icon
//   P-grid    — the grid container

// _iconPickerHTML returns the markup for an icon picker block. Goes
// inside a .wiz-form-row in the modal. The caller passes the current
// icon name (may be empty) and a unique idPrefix.
function _iconPickerHTML(idPrefix, currentIcon, options) {
    const opts = options || {};
    const placeholder = opts.placeholder || 'type to filter — e.g. cube, gauge, microchip';
    const inputId   = `${idPrefix}-input`;
    const previewId = `${idPrefix}-preview`;
    const gridId    = `${idPrefix}-grid`;
    return `
        <div class="wiz-iconpick-row">
            <input id="${inputId}" type="text" class="wiz-form-input"
                   value="${esc(currentIcon)}" placeholder="${esc(placeholder)}">
            <div class="wiz-iconpick-preview">
                <i id="${previewId}" class="${_faIconClass(currentIcon || 'cube')} fa-${esc(currentIcon || 'cube')}"></i>
            </div>
        </div>
        <div id="${gridId}" class="wiz-iconpick-grid">${_iconGridHTML(currentIcon, currentIcon)}</div>`;
}

// _iconGridHTML renders the inner HTML of the grid. Pure function of
// (selected, filter); the caller dumps it into the grid container on
// every input change.
//
// Empty filter → all icons (capped at 120). Non-empty filter →
// substring match, also capped at 120. The cap stops generic words
// from spewing hundreds of cells; a small "showing X of Y" hint
// signals when truncation happens.
function _iconGridHTML(selected, filter) {
    const lf = (filter || '').toLowerCase().trim();
    // Filter out Pro-only icons. window.FA_FREE_SET is populated by
    // /static/js/fa-free-set.js — auto-generated from the FontAwesome
    // metadata's `free` field. IoTMaker is not licensed to display
    // Pro icons, AND the Free CSS webfont doesn't ship Pro glyphs
    // (they would render as the missing-glyph tofu). Both reasons
    // converge on: never show Pro names in the picker.
    //
    // If FA_FREE_SET is missing (older client without the regen step
    // applied), fall back to no filter rather than break the picker.
    const freeFilter = window.FA_FREE_SET
        ? (i => window.FA_FREE_SET.has(i))
        : (() => true);
    const pool = FA_ALL.filter(freeFilter);
    const matches = lf ? pool.filter(i => i.includes(lf)) : pool;
    const total = matches.length;
    const capped = total > 120 ? matches.slice(0, 120) : matches;

    if (total === 0) {
        return `<div class="wiz-iconpick-grid-empty">No icons match "${esc(filter)}"</div>`;
    }

    const cells = capped.map(ic => {
        const cls = ic === selected ? 'wiz-iconpick-cell selected' : 'wiz-iconpick-cell';
        return `<button type="button" class="${cls}" title="${esc(ic)}" data-iconpick-pick="${esc(ic)}"><i class="${_faIconClass(ic)} fa-${esc(ic)}"></i></button>`;
    }).join('');

    // Footer hint only when truncating — no chrome when the user
    // sees everything that matches.
    const hint = total > 120
        ? `<div class="wiz-iconpick-grid-label">Showing first 120 of ${total.toLocaleString()} matches — keep typing to narrow.</div>`
        : '';

    return cells + hint;
}

// _attachIconPicker wires up the input/grid for a given prefix. Call
// once after the modal has been mounted to the DOM. Handlers are
// scoped via DOM lookups under `backdrop`, so multiple pickers in
// the same backdrop never cross wires.
function _attachIconPicker(backdrop, idPrefix) {
    const input   = backdrop.querySelector(`#${idPrefix}-input`);
    const preview = backdrop.querySelector(`#${idPrefix}-preview`);
    const grid    = backdrop.querySelector(`#${idPrefix}-grid`);
    if (!input || !preview || !grid) return;

    // refresh = re-render preview + grid based on the current input.
    // The input's value is BOTH the canonical icon name AND the
    // filter for the grid — same field, two roles. This is the
    // user's mockup behaviour.
    const refresh = () => {
        const v = (input.value || '').trim();
        // If the input value is an exact match for an icon name,
        // that icon becomes the preview. Otherwise the preview falls
        // back to the placeholder cube. The grid uses `v` as filter
        // either way.
        const previewName = (v && FA_ALL.includes(v)) ? v : (v || 'cube');
        preview.className = `${_faIconClass(previewName)} fa-${previewName}`;
        grid.innerHTML = _iconGridHTML(v, v);
    };

    // Live filter — every keystroke updates the grid + preview.
    input.addEventListener('input', refresh);

    // Click in the grid sets the input. Event delegation on the
    // grid container so re-renders don't have to re-bind every cell.
    grid.addEventListener('click', (e) => {
        const cell = e.target.closest('[data-iconpick-pick]');
        if (!cell) return;
        const name = cell.getAttribute('data-iconpick-pick') || '';
        input.value = name;
        refresh();
    });
}

// HTML escape — minimal duplicate of projects.js's `esc`. Kept local
// so this module has zero imports from projects.js (avoids circular
// dependency on the import graph).
function esc(s) {
    return String(s ?? '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
}

// =============================================================================
//  window-binding for inline onclick attributes
// =============================================================================
//
// The Projects page is built with inline `onclick="..."` HTML, which
// resolves names against `window`. Our exports above are ES module
// scope and need an explicit bridge.

if (typeof window !== 'undefined') {
    window.projWizardOpen = projWizardOpen;
    window.projWizardOnRowClick = projWizardOnRowClick;
    window.projWizardOnEditorParseSuccess = projWizardOnEditorParseSuccess;
}
