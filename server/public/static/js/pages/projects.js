// pages/projects.js — Project + Template management page for the IoTMaker SPA.
//
// This module has three rendering modes that share the same DOM root (#app-root):
//
//   TAB: PROJECTS  — default; shows the project tree grouped by language → type → project
//   TAB: TEMPLATES — shows the specialist's template packages with upload/version management
//   EDITOR MODE    — activated by openCodeEditor() or projCreateMarkdownFile();
//                    Monaco takes the full content area
//
// Template management was previously a separate page (templates.js / 'templates' route).
// It now lives here as the second tab so both project types share one entry point.
// The router redirects any legacy 'templates' nav to 'projects'.
//
// Template creation uses the same "New Project" modal with type=custom_project.
// The name and description come from the modal; the ZIP is uploaded separately
// via the Templates tab using "Upload Version". Version numbers are auto-incremented
// by the server (MAX+1 per template), matching project_code_versions behaviour.
//
// API endpoints used (projects):
//   GET    /api/v1/projects                           — list projects
//   POST   /api/v1/projects                           — create project
//   PUT    /api/v1/projects/:id                       — update project properties
//   DELETE /api/v1/projects/:id                       — delete project
//   GET    /api/v1/projects/meta/languages            — programming languages
//   GET    /api/v1/projects/meta/ui-languages         — UI languages
//   GET    /api/v1/projects/:id/files                 — list all project files
//   GET    /api/v1/projects/:id/files/code            — latest source + version list
//   POST   /api/v1/projects/:id/files/code/versions   — save from Monaco (JSON)
//   GET    /api/v1/projects/:id/files/code/versions   — list all versions with source
//   POST   /api/v1/projects/:id/files/img             — upload image
//   DELETE /api/v1/projects/:id/files/img/:name       — delete image
//   POST   /api/v1/projects/:id/files/docs            — create new markdown file
//   PUT    /api/v1/projects/:id/files/docs/:name      — update existing markdown file
//   DELETE /api/v1/projects/:id/files/docs/:name      — delete markdown (readme.md protected)
//   POST   /api/v1/blackbox/wizard/parse              — Go AST parse (codegen parser)
//   POST   /api/v1/blackbox/wizard/analyze            — go/parser + go/types diagnostics
//
// API endpoints used (templates):
//   GET    /api/v1/templates?mine=true          — list caller's own templates
//   POST   /api/v1/templates                    — create template package (no ZIP)
//   GET    /api/v1/templates/:id                — get template + def (for device preview)
//   POST   /api/v1/templates/:id/versions       — upload new ZIP version
//   PUT    /api/v1/templates/:id/visibility     — change visibility
//   PUT    /api/v1/templates/:id/publishing     — set publishing flags
//   DELETE /api/v1/templates/:id                — delete template + all ZIPs

import { api } from '../api.js';
import { S }   from '../state.js';
import { showPrompt, showConfirm, showUnsavedConfirm, t } from '../utils.js';

// The wizard module owns the fourth Projects tab. Importing it for
// side effects registers the window.projWizard* bindings used by the
// inline `onclick=""` HTML in renderEditorView. We also pull in the
// two functions we need to call directly (open + parse-success bridge).
import {
    projWizardOpen,
    projWizardOnEditorParseSuccess,
} from './projects_wizard.js';

// ─── Module state ─────────────────────────────────────────────────────────────

// Active tab: 'projects' | 'templates'
let _activeTab = 'projects';

// Tree state
let _projects      = [];
let _progLangs     = [];
let _uiLangs       = [];
let _expandedProjs = new Set();
let _expandedLangs = new Set();
let _projectFiles  = {};
let _root          = null;

// Editor state — mirrors blackbox.js module-level vars
let _editMode         = false;
let _editProject      = null;   // full project object
let _monacoInst       = null;
// _monacoHostDiv is the DOM div that Monaco was mounted into. We track
// it so projSetTab can move it between tabs (Editor wrap ↔ Wizard
// left panel) without disposing/recreating the editor. There is
// exactly one Monaco instance per page — moving the DOM preserves
// cursor, model, undo history, and decorations.
let _monacoHostDiv    = null;
let _monacoReady      = false;
let _parsedData       = null;
// _parsedSnapshotFP holds the FINGERPRINT of the working copy (every
// tab, path+content, strip order — see _snapshotFingerprint) at the
// moment of the last successful parse. Save compares it against the
// copy being committed to decide lastParseOk: with tabs, "the source
// that parsed" is a SET question — comparing one file would let an
// edit parked on an inactive tab ride a stale parse-ok flag.
//
// Português: FINGERPRINT da cópia de trabalho (toda aba, na ordem da
// faixa) no último parse bem-sucedido. O Save compara com a cópia sendo
// cometida: "o fonte que parseou" é pergunta de CONJUNTO — comparar um
// arquivo deixaria edição estacionada em aba inativa carregar um
// parse-ok velho.
let _parsedSnapshotFP = null;

// _snapshotFingerprint: canonical string identity of the working copy.
// JSON of [{path,content}] in strip order — order matters (it IS
// snapshot order), so no sorting.
function _snapshotFingerprint() {
    return JSON.stringify(_tabsCollectFiles());
}
let _currentVersion   = 0;
let _nextVersion      = 1;
let _codeVersions     = [];     // [{id, version, files:[{path,content}], createdAt}]
let _needsExplicitParse = false;
let _parseStatusType  = '';
let _analyzeTimer     = null;
let _analyzeSeq       = 0;
let _formAlertTimer   = null;

// ── Backup state (auto-save, single slot, see store/project_backups.go) ────
//
// _backupPending:  true when there's a backup row newer than the latest
//                  saved version — i.e. the user has unsaved work. Drives
//                  the Save button into its red "danger" tint.
// _backupGreenUntil: timestamp (ms) until which the Save button stays in
//                  the green "success" tint after a successful save.
//                  Set to Date.now()+10000 in projSave's success path.
// _backupDebounce: debounce timer for Monaco onChange — the editor fires
//                  on every keystroke, but we only want to write the
//                  backup ~2s after the user stops typing.
// _backupSeq:      sequence counter used to discard stale backup POST
//                  responses if the user typed faster than the network.
// _backupSaving:   in-flight POST flag; prevents two concurrent saves
//                  (especially when tab switch fires while a Monaco
//                  debounce is also pending).
let _backupPending    = false;
let _backupGreenUntil = 0;
let _backupDebounce   = null;
let _backupSeq        = 0;
let _backupSaving     = false;
// _backupDirty is the per-session "user has touched the source" flag.
// Distinct from _backupPending (which is "there's a backup row newer
// than the latest saved version" — i.e. a state that survives reopens).
//
// Why both? Tab switching shouldn't write a backup if the user opened
// the project and just clicked through tabs without editing anything.
// Writing the same content as is already on disk would be a wasted
// round-trip AND could surprise the user by updating the backup's
// timestamp without any real change.
//
// Set true in three places:
//   - Monaco onDidChangeModelContent (any keystroke)
//   - _applyEditAndClose in projects_wizard.js (modal save success)
// Set false in two places:
//   - openCodeEditor (fresh open — including backup recovery, since
//     no edit has happened in this session yet)
//   - projSave success (the save promoted everything; session is
//     clean again until the next edit)
//
// Tab switch consults this flag and skips the backup write when false.
let _backupDirty      = false;

// Current filename for the Monaco editor. The single-file era's state,
// kept as the ACTIVE-TAB identity in the multi-file model: the editor
// still edits one file today (the tab strip is the next slice), so the
// snapshot it saves is a one-entry set named by this variable.
//
// Português: Estado da era de arquivo único, mantido como identidade da
// ABA ATIVA no modelo multiarquivo — o editor ainda edita um arquivo (a
// faixa de abas é a próxima fatia); o snapshot salvo é um conjunto de
// uma entrada com este nome.
// Set when the editor opens; updated on rename or after save.
let _defaultFilename = 'main.go';

// Disposable returned by the slash-menu onDidChangeModelContent listener.
// Stored so it can be disposed when the editor closes.
let _imageCompletionDisposable = null;

// Position of the '/' character that opened the slash menu in the Go editor.
// Captured at detection time so it is still correct after the user clicks a
// menu item and Monaco loses focus.
let _slashPos = null;

// Diff state
let _diffHunks   = [];
let _diffChoices = [];

// Real-time analysis toggle — matches the blackbox.js constant.
// true = analysis fires 600ms after each keystroke (server CPU per user)
// false = analysis only on explicit parse click
let _realtimeAnalysis = true;

// ─── Markdown editor state ────────────────────────────────────────────────────

let _mdInst       = null;   // Monaco instance for the markdown editor
let _mdProjectId  = null;
let _mdFilename   = null;
let _mdFileUrl    = null;

// _mdIsNew is true when the file has not been saved to disk yet (Create flow).
// On first save, POST /files/docs is used to create the file.
// On subsequent saves (and for files opened from existing docs), PUT is used.
let _mdIsNew = false;

// Slash menu state for the markdown editor — mirrors the Go editor's _slashPos
// and _imageCompletionDisposable but scoped to the markdown Monaco instance.
let _mdSlashPos         = null;
let _mdSlashDisposable  = null;

// readme.md config — loaded from GET /api/v1/projects/meta/readme-config when
// the user opens readme.md. Cached for the duration of the editor session so
// the slash menu can offer /category and /subcategory without extra requests.
// Shape: { descriptionMaxChars: number, categories: [...], subcategories: [...] }
let _readmeConfig = null;

// ─── Template tab state ────────────────────────────────────────────────────────

let _templates    = [];     // TemplatePackage objects
let _tplPollTimers = {};    // { [id]: intervalId }
let _tplHelpOpen   = false; // true while the authoring guide panel is displayed
let _tplUploading  = false; // true while a ZIP version upload is in flight

const TPL_POLL_INTERVAL_MS = 2000;

// ─── Devices tab state ────────────────────────────────────────────────────────

let _devices         = [];  // DeviceSummary objects from GET /api/v1/blackbox/mine
let _devJobTimers    = {};  // { [jobId]: intervalId } — active job polls
let _devSubmitting   = false;

const DEV_POLL_INTERVAL_MS = 2000;

// ─── Entry point ──────────────────────────────────────────────────────────────

// leaveProjects is called by the router when navigating away from this page.
// It stops template and device poll timers and cleans up Monaco instances.
export function leaveProjects() {
    // Stop all template polls.
    Object.keys(_tplPollTimers).forEach(id => {
        clearInterval(_tplPollTimers[id]);
        delete _tplPollTimers[id];
    });
    _tplUploading = false;

    // Stop all device job polls.
    Object.keys(_devJobTimers).forEach(id => {
        clearInterval(_devJobTimers[id]);
        delete _devJobTimers[id];
    });
    _devSubmitting = false;
}

export function renderProjects(root) {
    _root = root;

    // Do not overwrite the help panel if it is currently open.
    if (_projHelpOpen) return;

    // Clean up Monaco instances — DOM element is gone after navigation.
    try { if (_monacoInst) { _monacoInst.dispose(); } } catch(e) {}
    _monacoInst  = null;
    _tabsDisposeAll();
    _monacoReady = false;
    _slashPos    = null;
    if (_imageCompletionDisposable) {
        try { _imageCompletionDisposable.dispose(); } catch(e) {}
        _imageCompletionDisposable = null;
    }
    _editMode    = false;
    _editProject = null;

    if (_mdInst) { try { _mdInst.dispose(); } catch(e) {} _mdInst = null; }
    if (_mdSlashDisposable) {
        try { _mdSlashDisposable.dispose(); } catch(e) {}
        _mdSlashDisposable = null;
    }
    _mdProjectId = null;
    _mdFilename  = null;
    _mdFileUrl   = null;
    _mdIsNew     = false;
    _mdSlashPos  = null;
    _readmeConfig = null;

    // Reset tree state on fresh navigation.
    _expandedProjs.clear();
    _expandedLangs.clear();
    _projectFiles  = {};

    // Reset template state and stop polls from a previous visit.
    leaveProjects();
    _templates = [];

    root.innerHTML = _buildPageShell();

    // The editor CSS (proj-pin, proj-block, proj-tooltip, etc.) is needed by
    // the Templates tab device preview even when the user never opens a project
    // editor. Injecting here ensures the styles are always present.
    _injectEditorStyles();

    // Load all data in parallel, then render the unified list.
    Promise.all([loadMeta(), loadProjects(), loadReadmeConfig()]).then(() => {
        _projects.forEach(p => _expandedLangs.add(p.programmingLanguageId));
        renderUnifiedList();
        _checkGithubConnection();
    });
}

// ─── Page shell with tabs ─────────────────────────────────────────────────────

function _buildPageShell() {
    return `
<div style="max-width:1200px;margin:0 auto">
  <div style="padding:24px 28px 16px;border-bottom:1px solid var(--border)">
    <div style="display:flex;align-items:center;justify-content:space-between">
      <div>
        <h1 style="font-size:22px;font-weight:700;margin:0">
          <i class="fa-solid fa-microchip" style="color:var(--primary);margin-right:10px"></i>Devices &amp; Templates
          <span onclick="window._projOpenHelp()"
                title="Devices &amp; Templates guide"
                style="display:inline-flex;align-items:center;justify-content:center;
                       width:18px;height:18px;border-radius:50%;
                       background:rgba(0,0,0,.06);cursor:pointer;
                       font-size:11px;color:var(--text-muted);flex-shrink:0;margin-left:8px;vertical-align:middle">
            <i class="fa-solid fa-circle-question"></i>
          </span>
        </h1>
      </div>
      <!-- New Project dropdown.
           Two creation paths share the same entry point so the user is
           never lost in a multi-step modal when they just want to write
           Go and use the wizard:
             - "Create with wizard" — minimal modal (name only); creates
               an empty Go device project and drops the user straight
               into the editor. They type Go, click Parse, then switch
               to the Wizard tab to fill in cards.
             - "Import from GitHub" — the original modal with all
               fields (visibility, language, GitHub URL, …). Used when
               the user is publishing an existing GitHub release. -->
      <div class="proj-newbtn-wrap">
        <button class="btn btn-primary btn-sm proj-newbtn"
                onclick="projToggleNewMenu()" id="proj-newbtn"
                aria-haspopup="menu" aria-expanded="false">
          <i class="fa-solid fa-plus"></i> New
          <i class="fa-solid fa-caret-down" style="margin-left:4px;font-size:11px"></i>
        </button>
        <div class="proj-newbtn-menu" id="proj-newbtn-menu" role="menu">
          <button class="proj-newbtn-item" role="menuitem"
                  onclick="openWizardCreateModal()">
            <i class="fa-solid fa-wand-magic-sparkles" style="color:var(--primary)"></i>
            <div>
              <div style="font-weight:600">Create with wizard</div>
              <div style="font-size:11px;color:var(--text-muted)">
                Empty Go project — write code, then click Parse to see cards
              </div>
            </div>
          </button>
          <button class="proj-newbtn-item" role="menuitem"
                  onclick="openCreateProjectModal()">
            <i class="fa-brands fa-github" style="color:var(--text-primary)"></i>
            <div>
              <div style="font-weight:600">Import from GitHub</div>
              <div style="font-size:11px;color:var(--text-muted)">
                Publish a Device or Template from a GitHub release
              </div>
            </div>
          </button>
        </div>
      </div>
    </div>
  </div>
  <!-- Toast stack -->
  <div id="proj-toast-stack"
       style="position:fixed;top:16px;right:16px;z-index:9999;
              display:flex;flex-direction:column;gap:8px;
              max-width:420px;pointer-events:none"></div>
  <!-- Unified list — devices and templates together -->
  <div id="proj-list" style="padding:16px 12px">
    <div style="text-align:center;padding:40px;color:var(--text-muted)">
      <i class="fa-solid fa-spinner fa-spin" style="font-size:20px"></i>
      <p style="margin-top:10px">Loading…</p>
    </div>
  </div>
  <div id="proj-modal-overlay" style="display:none"></div>
</div>
${_tplBuildStyles()}`;
}

// switchTab switches between Projects, Templates, and Devices tabs.
// switchTab — kept for backward compat (called from old bookmarks/code).
// With the unified list there are no tabs; just re-renders the unified list.
export function switchTab(tab) {
    renderUnifiedList();
}

function _renderActiveTab() {
    renderUnifiedList();
}

// ─── Data loading ─────────────────────────────────────────────────────────────

async function loadProjects() {
    const [pr, dr, tr] = await Promise.all([
        api('GET', '/api/v1/projects'),
        api('GET', '/api/v1/blackbox/mine'),
        api('GET', '/api/v1/templates?mine=true'),
    ]);
    _projects  = pr?.metadata?.status === 200 ? (pr.data || []) : [];
    _devices   = Array.isArray(dr) ? dr : (dr?.data || []);
    _templates = tr?.metadata?.status === 200 ? (tr.data || []) : [];
}

async function loadMeta() {
    const [lr, ur] = await Promise.all([
        api('GET', '/api/v1/projects/meta/languages'),
        api('GET', '/api/v1/projects/meta/ui-languages'),
    ]);
    if (lr?.metadata?.status === 200) _progLangs = lr.data || [];
    if (ur?.metadata?.status === 200) _uiLangs   = ur.data || [];
}

async function loadProjectFiles(projectId) {
    const r = await api('GET', `/api/v1/projects/${projectId}/files`);
    _projectFiles[projectId] = (r?.metadata?.status === 200)
        ? r.data
        : { code: [], img: [], docs: [] };
}

// loadReadmeConfig fetches the full component taxonomy and the server-side
// limits (e.g. descriptionMaxChars) needed by the readme.md slash menu.
// The result is cached in _readmeConfig for the duration of the editor session.
async function loadReadmeConfig() {
    const r = await api('GET', '/api/v1/projects/meta/readme-config');
    if (r?.metadata?.status === 200) {
        _readmeConfig = r.data;
    }
}

// ─── Tree rendering ───────────────────────────────────────────────────────────

// _checkGithubConnection checks whether the current user has a GitHub account
// connected. If not, shows a persistent toast guiding them to the Profile page.
// The toast only appears if the account is NOT connected — no noise otherwise.
function _checkGithubConnection() {
    // S.profile is populated by checkAuth() in api.js after login.
    // If githubUsername is empty the specialist cannot submit devices.
    const githubUsername = window.S?.profile?.githubUsername || '';
    if (githubUsername) return; // connected — nothing to show

    showPageAlert(
        '<strong>GitHub account not connected.</strong> ' +
        'To publish devices you need to link your GitHub account first. ' +
        'Go to your <a href="#" onclick="nav(\'profile\');return false" ' +
    'style="color:inherit;font-weight:700;text-decoration:underline">Profile page</a> ' +
    'and click <strong>Connect GitHub</strong>.',
        'warning',
        0  // 0 = stays until manually dismissed
);
}

// ─── Unified list (devices + templates) ──────────────────────────────────────
//
// renderUnifiedList replaces the old two-tab system.
// All devices and templates owned by the user appear in a single flat list.
// The display name comes from displayNameHuman (first # of readme.md).
// Each row: name + type badge + visibility badge + ready badge + [▼] + ⚙ + 🗑
//
// Expanding a row shows the visual block diagram (reuses _tplDevicePreview /
// projBuildDevice logic depending on item type).

function renderUnifiedList() {
    const container = document.getElementById('proj-list');
    if (!container) return;

    const allProjects  = _projects  || [];
    const allDevices   = _devices   || [];
    const allTemplates = _templates || [];

    if (allProjects.length === 0 && allDevices.length === 0 && allTemplates.length === 0) {
        // Empty state: render the four-panel illustration plus a short
        // hint pointing at "+ New" and "(?)". The hydration call is a
        // no-op today, kept as a stable hook for future deferred content
        // (see _hydrateEmptyStateGuide for the rationale).
        container.innerHTML = _renderEmptyStateGuide();
        _hydrateEmptyStateGuide();
        return;
    }

    // Sort: projects (drafts), devices, and templates together, newest first.
    // _kind drives the badge colour and the click target.
    const items = [
        ...allProjects.map(p => ({ ...p, _kind: 'project' })),
        ...allDevices.map(d => ({ ...d, _kind: 'device' })),
        ...allTemplates.map(t => ({ ...t, _kind: 'template' })),
    ].sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));

    container.innerHTML = items.map(item => _buildUnifiedRow(item)).join('');
}

// _renderEmptyStateGuide returns the HTML for the empty state of the
// Devices & Templates page — shown when the user has no projects, no
// devices and no templates. The page is composed of:
//
//   1. A four-panel illustration (SVG) that summarises the value
//      proposition for a specialist who is wondering whether it is
//      worth writing a device:
//
//        - "You develop"   — your craft, your editor, your language.
//        - "We simplify"   — the parser turns it into a visual block.
//        - "Stays useful"  — the block ends up inside real products.
//        - "Anyone can use"— the block reaches people who never coded.
//
//      The previous animated SVG (a 14-second loop showing four scenes)
//      was replaced with this static composition because the animation
//      was decorative without being explanatory: a first-time visitor
//      could not tell what a "device" was after watching it. The four
//      panels deliver the same story at a glance, keep the visual
//      simplicity Kemper asked for (one element per panel), and lead
//      the user toward the call to action.
//
//   2. A single short hint below the illustration pointing to the two
//      next actions: the "+ New" button (primary CTA, top-right of the
//      page shell) and the "(?)" button (secondary, beside the page
//      title) that opens the full Devices & Templates Guide.
//
// An earlier iteration also rendered the full markdown guide inline
// below the illustration. It was removed because the empty state should
// be a quick orientation rather than a wall of text; the full guide
// remains available via the (?) button.
//
// Colours come from portal CSS variables (--primary, --text-secondary,
// --text-muted, --border) so the illustration follows the active theme
// without hardcoded hex values. Catppuccin Mocha applies in the WASM
// IDE only — the portal has its own palette defined in css/main.css.
function _renderEmptyStateGuide() {
    return `
<div style="max-width:1100px;margin:0 auto;padding:8px 4px 40px">

  <!-- Four-panel illustration. Equal-width panels (160px each), one
       element per panel, no internal captions — the panel title above
       each rectangle does the labelling.

       viewBox math: panels start at x=40, x=210, x=380, x=550, each
       160px wide. Last panel ends at x=710. ViewBox width is 750 to
       give 40px safety margin on the right side, matching the 40px
       margin on the left. Earlier the viewBox was 680 and the right
       edge of the fourth panel got clipped at narrow window widths. -->
  <div style="padding:8px 0 24px">
    <svg viewBox="0 0 750 340" width="100%"
         style="max-width:880px;display:block;margin:0 auto"
         xmlns="http://www.w3.org/2000/svg" role="img"
         aria-labelledby="proj-empty-svg-title proj-empty-svg-desc">
      <title id="proj-empty-svg-title">How a device flows from your code to anyone's project</title>
      <desc id="proj-empty-svg-desc">
        Four panels in a row. First, a developer figure with a few lines
        of source code. Second, a single auto-generated visual block.
        Third, the block running inside a polished product (a monitoring
        panel). Fourth, a small crowd of figures, each holding a copy of
        the block.
      </desc>

      <!-- Panel 1: You develop — silhouette + four lines of source.
           Stroke uses --text-secondary so the figure reads in both
           light themes; code keywords use --primary, regular tokens
           use --text-secondary at full opacity, comments at 0.55. -->
      <text x="120" y="36" text-anchor="middle"
            font-size="14" font-weight="600" fill="var(--text-primary)">You develop</text>
      <rect x="40" y="60" width="160" height="240" rx="10"
            fill="none" stroke="var(--border)" stroke-width="1"/>

      <circle cx="120" cy="120" r="14"
              fill="none" stroke="var(--text-secondary)" stroke-width="1.2"/>
      <path d="M95,160 Q120,134 145,160 L145,178 L95,178 Z"
            fill="none" stroke="var(--text-secondary)" stroke-width="1.2"/>

      <text x="56" y="222" font-family="ui-monospace,monospace"
            font-size="11" fill="var(--text-muted)">// label:Sensor.</text>
      <text x="56" y="240" font-family="ui-monospace,monospace"
            font-size="11" font-weight="600" fill="var(--text-secondary)">type Sensor</text>
      <text x="56" y="268" font-family="ui-monospace,monospace"
            font-size="11" fill="var(--text-muted)">// Returns: optional.</text>
      <text x="56" y="286" font-family="ui-monospace,monospace"
            font-size="11" font-weight="600" fill="var(--text-secondary)">func Read()</text>

      <!-- Panel 2: We simplify — a single auto-generated visual block.
           Header is solid --primary; body is a 12% tint of --primary
           via opacity on a separate rect (no rgba math needed). -->
      <text x="290" y="36" text-anchor="middle"
            font-size="14" font-weight="600" fill="var(--text-primary)">We simplify</text>
      <rect x="210" y="60" width="160" height="240" rx="10"
            fill="none" stroke="var(--border)" stroke-width="1"/>

      <rect x="230" y="160" width="120" height="86" rx="8"
            fill="var(--primary)" opacity="0.08"/>
      <rect x="230" y="160" width="120" height="86" rx="8"
            fill="none" stroke="var(--primary)" stroke-width="1.5"/>
      <rect x="230" y="160" width="120" height="26" rx="8" fill="var(--primary)"/>
      <rect x="230" y="176" width="120" height="10" fill="var(--primary)"/>
      <text x="290" y="178" text-anchor="middle"
            font-size="13" font-weight="700" fill="#FFFFFF">Sensor.Read</text>
      <circle cx="350" cy="210" r="4"
              fill="none" stroke="var(--primary)" stroke-width="1.5"/>
      <circle cx="350" cy="232" r="4"
              fill="none" stroke="var(--primary)" stroke-width="1.5"/>

      <!-- Panel 3: Stays useful — a small finished product (a
           monitoring panel). The block from panel 2 is implied to be
           running inside; visually we show the kind of thing it ends
           up powering. Bars and indicators in --primary tie back to
           the block colour. -->
      <text x="460" y="36" text-anchor="middle"
            font-size="14" font-weight="600" fill="var(--text-primary)">Stays useful</text>
      <rect x="380" y="60" width="160" height="240" rx="10"
            fill="none" stroke="var(--border)" stroke-width="1"/>

      <rect x="396" y="120" width="128" height="160" rx="4"
            fill="none" stroke="var(--text-secondary)" stroke-width="1"/>
      <circle cx="406" cy="138" r="2.5" fill="var(--primary)"/>
      <circle cx="416" cy="138" r="2.5"
              fill="none" stroke="var(--text-muted)" stroke-width="0.6"/>
      <circle cx="426" cy="138" r="2.5"
              fill="none" stroke="var(--text-muted)" stroke-width="0.6"/>
      <text x="497" y="142" text-anchor="end"
            font-size="11" fill="var(--text-muted)">power</text>
      <line x1="396" y1="152" x2="524" y2="152"
            stroke="var(--border)" stroke-width="0.5"/>

      <rect x="406" y="170" width="108" height="22" rx="3"
            fill="none" stroke="var(--text-muted)" stroke-width="0.7"/>
      <rect x="406" y="170" width="70" height="22" rx="3"
            fill="var(--primary)" opacity="0.9"/>
      <text x="460" y="186" text-anchor="middle"
            font-size="10" fill="#FFFFFF" font-weight="600">62%</text>

      <rect x="406" y="208" width="32" height="16" rx="2"
            fill="var(--primary)" opacity="0.9"/>
      <rect x="444" y="208" width="32" height="16" rx="2"
            fill="none" stroke="var(--text-muted)" stroke-width="0.6"/>
      <rect x="482" y="208" width="32" height="16" rx="2"
            fill="none" stroke="var(--text-muted)" stroke-width="0.6"/>

      <text x="406" y="254" font-family="ui-monospace,monospace"
            font-size="10" fill="var(--text-muted)">42.5°C OK</text>

      <!-- Panel 4: Anyone can use — a 3×3 crowd of small figures, each
           with a thin --primary bar at chest level (their copy of the
           block). Rows fade from full to 0.6 opacity so the crowd
           visually extends downward — "and growing". -->
      <text x="630" y="36" text-anchor="middle"
            font-size="14" font-weight="600" fill="var(--text-primary)">Anyone can use</text>
      <rect x="550" y="60" width="160" height="240" rx="10"
            fill="none" stroke="var(--border)" stroke-width="1"/>

      <g opacity="1">
        <circle cx="582" cy="115" r="7"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.9"/>
        <path d="M569,136 Q582,121 595,136 L595,152 L569,152 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.9"/>
        <rect x="571" y="156" width="22" height="3" fill="var(--primary)"/>
      </g>
      <g opacity="1">
        <circle cx="618" cy="110" r="8"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.9"/>
        <path d="M603,132 Q618,116 633,132 L633,150 L603,150 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.9"/>
        <rect x="605" y="154" width="26" height="3" fill="var(--primary)"/>
      </g>
      <g opacity="1">
        <circle cx="656" cy="115" r="7"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.9"/>
        <path d="M643,136 Q656,121 669,136 L669,152 L643,152 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.9"/>
        <rect x="645" y="156" width="22" height="3" fill="var(--primary)"/>
      </g>

      <g opacity="0.85">
        <circle cx="582" cy="184" r="7"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.8"/>
        <path d="M569,205 Q582,190 595,205 L595,221 L569,221 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.8"/>
        <rect x="571" y="225" width="22" height="3" fill="var(--primary)"/>
      </g>
      <g opacity="0.85">
        <circle cx="618" cy="179" r="8"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.8"/>
        <path d="M603,201 Q618,185 633,201 L633,219 L603,219 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.8"/>
        <rect x="605" y="223" width="26" height="3" fill="var(--primary)"/>
      </g>
      <g opacity="0.85">
        <circle cx="656" cy="184" r="7"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.8"/>
        <path d="M643,205 Q656,190 669,205 L669,221 L643,221 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.8"/>
        <rect x="645" y="225" width="22" height="3" fill="var(--primary)"/>
      </g>

      <g opacity="0.55">
        <circle cx="582" cy="253" r="7"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.7"/>
        <path d="M569,274 Q582,259 595,274 L595,290 L569,290 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.7"/>
      </g>
      <g opacity="0.55">
        <circle cx="618" cy="248" r="8"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.7"/>
        <path d="M603,270 Q618,254 633,270 L633,288 L603,288 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.7"/>
      </g>
      <g opacity="0.55">
        <circle cx="656" cy="253" r="7"
                fill="none" stroke="var(--text-secondary)" stroke-width="0.7"/>
        <path d="M643,274 Q656,259 669,274 L669,290 L643,290 Z"
              fill="none" stroke="var(--text-secondary)" stroke-width="0.7"/>
      </g>
    </svg>
  </div>

  <!-- A single short line under the illustration. Two roles:
       (a) tells the first-time user how to start;
       (b) points to the (?) button beside the page title for the full
           Devices & Templates Guide.
       Earlier this page rendered the entire guide inline below the
       illustration. It was removed because the empty state should be
       a quick orientation, not a wall of text. The dedicated help
       panel reachable from (?) remains the home of the long-form guide. -->
  <p style="text-align:center;color:var(--text-muted);font-size:13px;
            margin:0;padding:0 16px;line-height:1.7">
    Click <strong>+ New</strong> in the top-right to start your first
    device or template.<br>
    Need a primer? Open the <strong>(?)</strong> beside the page title
    for the full guide.
  </p>

</div>`;
}

// _hydrateEmptyStateGuide is intentionally a no-op kept as a stable hook.
//
// In an earlier iteration the empty state rendered the full Devices &
// Templates Guide markdown inline; that pass needed an async hydration
// step (marked.js had to be present before parsing). The inline guide
// was later removed in favour of the four-panel illustration alone, so
// there is currently nothing to hydrate.
//
// The function stays declared because `renderUnifiedList` calls it
// unconditionally after inserting the empty-state HTML. Keeping a no-op
// here avoids a guard at the call site and leaves a clear extension
// point if a future change wants to fetch and inject any deferred
// content (e.g. the suggested split of the markdown into a separate
// /static/help/devices.md file served by the backend).
function _hydrateEmptyStateGuide() {
    /* no-op — kept as a hook; see comment above. */
}


// _buildUnifiedRow renders one device or template as a collapsible row.
function _buildUnifiedRow(item) {
    const isProject  = item._kind === 'project';
    const isDevice   = item._kind === 'device';
    // Display name resolution: projects use `name`; devices/templates have
    // a richer fallback chain because their displayNameHuman comes from
    // the parsed README.md heading.
    const displayName = isProject
        ? (item.name || item.id)
        : (item.displayNameHuman ||
            (isDevice ? (item.displayName || item.githubRepo || item.id)
                : (item.name || item.githubRepo || item.id)));
    // Type badge — three colours so the kind is obvious at a glance.
    //   project  → blue   (local draft, editable in the IDE)
    //   template → amber  (published, reusable)
    //   device   → violet (published, hardware-bound)
    const typeBadge = isProject
        ? '<span style="font-size:10px;font-weight:600;background:#dbeafe;color:#1e40af;border:1px solid #93c5fd;border-radius:99px;padding:1px 7px">project</span>'
        : isDevice
            ? '<span style="font-size:10px;font-weight:600;background:#ede9fe;color:#6d28d9;border:1px solid #c4b5fd;border-radius:99px;padding:1px 7px">device</span>'
            : '<span style="font-size:10px;font-weight:600;background:#fef3c7;color:#92400e;border:1px solid #fde68a;border-radius:99px;padding:1px 7px">template</span>';
    const visBadge = item.visibility === 'public'
        ? '<span class="badge badge-pub" style="font-size:10px">public</span>'
        : '<span class="badge badge-dft" style="font-size:10px">private</span>';
    const readyBadge = item.readyToUse
        ? '<span style="font-size:10px;font-weight:600;background:#d1fae5;color:#065f46;border:1px solid #6ee7b7;border-radius:99px;padding:1px 7px"><i class="fa-solid fa-check" style="margin-right:3px"></i>ready</span>'
        : '';
    const statusBadge = item.status === 'error'
        ? '<span style="font-size:10px;font-weight:600;background:#fef2f2;color:#b91c1c;border:1px solid #fca5a5;border-radius:99px;padding:1px 7px"><i class="fa-solid fa-circle-xmark" style="margin-right:3px"></i>error</span>'
        : item.status === 'pending' || item.status === 'no_version'
            ? '<span style="font-size:10px;font-weight:600;background:#fff8e1;color:#b7770d;border:1px solid #ffd97d;border-radius:99px;padding:1px 7px"><i class="fa-solid fa-circle-notch fa-spin" style="margin-right:3px"></i>processing</span>'
            : '';
    const repoKey = isProject ? '' : ((item.githubOwner || '') + '/' + (item.githubRepo || ''));
    const tagHTML = item.githubTag ? `<span class="tag" style="font-size:10px">${esc(item.githubTag)}</span>` : '';
    const expandId = 'unified-expand-' + item.id;
    // Projects: clicking the row opens the editor directly. Devices and
    // templates: clicking toggles an inline expand showing the parsed
    // diagram. Different actions on purpose — projects are editable
    // while devices/templates are read-only published artefacts.
    const mainAction = isProject
        ? `openCodeEditor('${esc(item.id)}')`
        : `toggleUnifiedRow('${esc(item.id)}')`;
    const mainIcon = isProject
        ? 'fa-folder'
        : (isDevice ? 'fa-microchip' : 'fa-file-export');
    // Chevron only when the row is expandable (= not a project).
    const chevron = isProject
        ? ''
        : `<i class="fa-solid fa-chevron-down" id="chevron-${esc(item.id)}"
              style="font-size:10px;color:var(--text-muted);transition:transform .2s"></i>`;

    return `
<div style="margin-bottom:2px;border-left:2px solid var(--border)">
  <div style="display:flex;align-items:center;gap:6px;padding:4px 4px 4px 8px">
    <button onclick="${mainAction}"
      style="display:flex;align-items:center;flex:1;gap:8px;padding:6px 8px;
             background:none;border:1px solid transparent;
             border-radius:var(--r);cursor:pointer;font-size:13px;
             color:var(--text-primary);transition:all var(--tr);text-align:left"
      onmouseover="this.style.background='var(--bg-surface)'"
      onmouseout="this.style.background='none'">
      <i class="fa-solid ${mainIcon}"
         style="color:var(--primary);width:16px;text-align:center"></i>
      <span style="font-weight:600">${esc(displayName)}</span>
      ${typeBadge} ${visBadge} ${statusBadge} ${readyBadge}
      <span style="font-size:11px;color:var(--text-muted);flex:1;text-align:right">${esc(repoKey)} ${tagHTML}</span>
      ${chevron}
    </button>
    <button title="Properties"
            onclick="openUnifiedPropertiesModal('${esc(item.id)}','${esc(item._kind)}');event.stopPropagation()"
      style="padding:5px 8px;background:none;border:1px solid transparent;
             border-radius:var(--r);cursor:pointer;color:var(--text-muted);flex-shrink:0"
      onmouseover="this.style.background='var(--bg-surface)';this.style.color='var(--primary)';this.style.borderColor='var(--border)'"
      onmouseout="this.style.background='none';this.style.color='var(--text-muted)';this.style.borderColor='transparent'">
      <i class="fa-solid fa-gear" style="font-size:12px"></i>
    </button>
    <button title="Delete"
            onclick="confirmDeleteUnified('${esc(item.id)}','${esc(item._kind)}','${esc(displayName)}');event.stopPropagation()"
      style="padding:5px 8px;background:none;border:1px solid transparent;
             border-radius:var(--r);cursor:pointer;color:var(--text-muted);flex-shrink:0"
      onmouseover="this.style.background='var(--danger-bg)';this.style.color='var(--danger)';this.style.borderColor='var(--danger)'"
      onmouseout="this.style.background='none';this.style.color='var(--text-muted)';this.style.borderColor='transparent'">
      <i class="fa-solid fa-trash-can" style="font-size:12px"></i>
    </button>
  </div>
  ${isProject ? '' : `
  <!-- Expanded content — block diagrams (devices/templates only) -->
  <div id="${expandId}" style="display:none;padding:8px 8px 8px 32px">
    ${_buildUnifiedExpanded(item)}
  </div>
  `}
</div>`;
}

// _buildUnifiedExpanded renders the method block diagrams for a device or template.
//
// Devices: parsedJson is NOT included in the DeviceSummary returned by
// GET /api/v1/blackbox/mine (it would bloat every list load). Instead, the
// first expand triggers _devFetchDef(id) which calls GET /api/v1/blackbox/:id
// to retrieve the full Device row (with parsedJson). While the fetch is in
// flight, a spinner is shown. Once the data arrives, _devices[idx]._parsedDef
// is populated and the expand area is re-rendered in place.
//
// Templates use the same lazy pattern via _tplFetchDef / t._def.
function _buildUnifiedExpanded(item) {
    if (item.status !== 'ready') {
        return '<p style="font-size:13px;color:var(--text-muted);padding:8px 0">' +
            (item.status === 'error'
                ? '<i class="fa-solid fa-circle-xmark" style="color:var(--danger);margin-right:6px"></i>Parse errors — re-submit after fixing the source.'
                : '<i class="fa-solid fa-circle-notch fa-spin" style="margin-right:6px"></i>Parsing in progress…') +
            '</p>';
    }
    if (item._kind === 'template') {
        // _def is populated lazily on first expand, exactly like _parsedDef for
        // devices. _tplFetchDef stores the result in _templates[idx]._def and
        // replaces the spinner placeholder once the data arrives.
        if (item._def) {
        return _tplDevicePreview(item) || '<p style="font-size:13px;color:var(--text-muted)">No devices defined.</p>';
    }
        if (item._defFetched) {
            return '<p style="font-size:13px;color:var(--text-muted)">No definition available.</p>';
        }
        _tplFetchDefUnified(item.id);
        return `<div id="tpl-preview-${esc(item.id)}"
                     style="padding:8px 0;color:var(--text-muted);font-size:13px">
                  <i class="fa-solid fa-circle-notch fa-spin" style="margin-right:6px"></i>
                  Loading preview…
                </div>`;
    }

    // Device — use cached _parsedDef when available.
    if (item._parsedDef) {
        return projBuildDevice(item._parsedDef);
    }

    // parsedJson was already fetched and cached but is null/empty — give up.
    if (item._defFetched) {
        return '<p style="font-size:13px;color:var(--text-muted)">No definition available.</p>';
    }

    // First expand: trigger a lazy fetch and show a spinner in the meantime.
    // _devFetchDef will replace the spinner content once data arrives.
    _devFetchDef(item.id);
    return `<div id="dev-preview-${esc(item.id)}"
                 style="padding:8px 0;color:var(--text-muted);font-size:13px">
              <i class="fa-solid fa-circle-notch fa-spin" style="margin-right:6px"></i>
              Loading preview…
            </div>`;
}

// _devFetchDef lazily loads the full Device row (which contains parsedJson)
// for the given device id. Mirrors _tplFetchDef for templates.
//
// On success it:
//  1. Parses parsedJson and caches the result in _devices[idx]._parsedDef.
//  2. Updates the already-open expand area (dev-preview-{id}) in place so
//     the user does not need to close and re-open the row.
//
// The _defFetched flag prevents re-fetching when parsedJson is empty/null
// (e.g. a device that was deleted between list load and expand click).
async function _devFetchDef(id) {
    try {
        const res = await api('GET', `/api/v1/blackbox/${id}`);
        const idx = _devices.findIndex(d => d.id === id);
        if (idx < 0) return; // row was removed while fetch was in flight

        // Mark as fetched regardless of outcome so we don't loop.
        _devices[idx]._defFetched = true;

        const raw = res?.parsedJson ?? res?.parsed_json ?? '';
        if (!raw || raw === '{}') return;

        let def;
        try {
            def = typeof raw === 'string' ? JSON.parse(raw) : raw;
        } catch { return; }

        _devices[idx]._parsedDef = def;

        // Replace the spinner that _buildUnifiedExpanded inserted.
        const placeholder = document.getElementById(`dev-preview-${id}`);
        if (placeholder) {
            placeholder.outerHTML = projBuildDevice(def);
    }
    } catch { /* non-critical — the spinner will just stay */ }
}

// _tplFetchDefUnified lazily loads the full template definition for the unified
// list. Mirrors _devFetchDef for devices.
//
// On success it:
//  1. Stores the def in _templates[idx]._def.
//  2. Replaces the spinner placeholder (tpl-preview-{id}) with the rendered
//     device block diagram via _tplDevicePreview.
//
// _defFetched prevents re-fetching when the def is empty (e.g. no devices).
async function _tplFetchDefUnified(id) {
    try {
        const res = await api('GET', `/api/v1/templates/${id}`);
        const idx = _templates.findIndex(t => t.id === id);
        if (idx < 0) return;

        _templates[idx]._defFetched = true;

        if (res?.metadata?.status !== 200) return;
        const def = res?.data?.def;
        if (!def) return;

        _templates[idx]._def = def;

        // Replace the spinner that _buildUnifiedExpanded inserted.
        const placeholder = document.getElementById(`tpl-preview-${id}`);
        if (placeholder) {
            const html = _tplDevicePreview(_templates[idx]);
            placeholder.outerHTML = html ||
                '<p style="font-size:13px;color:var(--text-muted)">No devices defined.</p>';
        }
    } catch { /* non-critical — the spinner will just stay */ }
}

export function toggleUnifiedRow(id) {
    const el      = document.getElementById('unified-expand-' + id);
    const chevron = document.getElementById('chevron-' + id);
    if (!el) return;
    const open = el.style.display !== 'none';
    el.style.display      = open ? 'none' : '';
    if (chevron) chevron.style.transform = open ? '' : 'rotate(180deg)';
}

function renderTree() {
    const container = document.getElementById('proj-tree');
    if (!container) return;

    if (_projects.length === 0 && _devices.length === 0) {
        container.innerHTML = `
<div style="text-align:center;padding:60px 20px;color:var(--text-muted)">
  <i class="fa-solid fa-microchip" style="font-size:48px;opacity:.3;display:block;margin-bottom:16px"></i>
  <p style="font-size:15px;margin-bottom:8px">No devices or templates yet</p>
  <p style="font-size:13px">Click <strong>New</strong> to get started.</p>
</div>`;
        return;
    }

    const byLang = {};
    for (const p of _projects) {
        const lid = p.programmingLanguageId;
        if (!byLang[lid]) byLang[lid] = { lang: p.programmingLanguage, projects: [] };
        byLang[lid].projects.push(p);
    }

    let html = '<div>';
    for (const langId of Object.keys(byLang)) {
        html += renderLangNode(byLang[langId]);
    }
    html += '</div>';

    // Devices (GitHub-sourced) — rendered as a flat group below projects.
    if (_devices.length > 0) {
        html += _renderDeviceGroup();
    }

    container.innerHTML = html;
}

// ─── Device rendering (GitHub-sourced) ───────────────────────────────────────

// _renderDeviceGroup renders all devices grouped by GitHub repo, matching the
// visual style of renderLangNode but for GitHub-sourced devices.
function _renderDeviceGroup() {
    const byRepo = {};
    _devices.forEach(function(d) {
        const key = (d.githubOwner || '') + '/' + (d.githubRepo || '');
        if (!byRepo[key]) byRepo[key] = { url: d.githubUrl, tag: d.githubTag, key, devs: [] };
        byRepo[key].devs.push(d);
    });

    const count = _devices.length;
    return `
<div style="margin-bottom:4px;margin-top:12px">
  <div style="display:flex;align-items:center;padding:8px 12px;
              background:var(--bg-surface);border:1px solid var(--border);
              border-radius:var(--r);font-weight:600;font-size:14px;
              color:var(--text-primary);gap:10px">
    <i class="fa-solid fa-microchip" style="color:var(--primary);width:18px;text-align:center"></i>
    <span style="flex:1">Devices (GitHub)</span>
    <span style="font-size:11px;font-weight:400;color:var(--text-muted)">${count} device${count!==1?'s':''}</span>
  </div>
  <div style="padding-left:20px;margin-top:2px">
    <div style="border-left:2px solid var(--border)">
      ${Object.values(byRepo).map(g => `
        <div style="padding:4px 0 4px 8px;border-bottom:1px solid var(--border);margin-bottom:2px">
          <div style="display:flex;align-items:center;gap:6px;margin-bottom:4px;padding:0 4px">
            <i class="fa-brands fa-github" style="color:var(--text-muted);font-size:12px"></i>
            <a href="${esc(g.url)}" target="_blank" rel="noopener noreferrer"
               style="font-size:12px;font-weight:600;color:var(--text-secondary);text-decoration:none">
              ${esc(g.key)}
            </a>
            ${g.tag ? `<span class="tag">${esc(g.tag)}</span>` : ''}
          </div>
          ${g.devs.map(d => _renderDeviceRow(d)).join('')}
        </div>`).join('')}
    </div>
  </div>
</div>`;
}

// _renderDeviceRow renders one device inside the Projects tree.
// Mirrors renderProjectNode visually: name + status badge + gear + trash.
function _renderDeviceRow(d) {
    const statusBadge = d.status === 'ready'
        ? '<span style="font-size:10px;font-weight:600;background:#d1fae5;color:#065f46;border:1px solid #6ee7b7;border-radius:99px;padding:1px 7px"><i class="fa-solid fa-circle-check" style="margin-right:3px"></i>ready</span>'
        : d.status === 'error'
            ? '<span style="font-size:10px;font-weight:600;background:#fef2f2;color:#b91c1c;border:1px solid #fca5a5;border-radius:99px;padding:1px 7px"><i class="fa-solid fa-circle-xmark" style="margin-right:3px"></i>error</span>'
            : '<span style="font-size:10px;font-weight:600;background:#fff8e1;color:#b7770d;border:1px solid #ffd97d;border-radius:99px;padding:1px 7px"><i class="fa-solid fa-circle-notch fa-spin" style="margin-right:3px"></i>processing</span>';
    const visBadge = d.visibility === 'public'
        ? '<span class="badge badge-pub" style="font-size:10px">public</span>'
        : '<span class="badge badge-dft" style="font-size:10px">private</span>';
    const tagsHtml = ((d.tags || '').split(',').filter(Boolean)
        .map(t => `<span class="tag" style="font-size:10px">${esc(t.trim())}</span>`)
        .join(''));

    return `
<div style="display:flex;align-items:center;gap:6px;padding:4px 4px 4px 8px">
  <div style="display:flex;align-items:center;flex:1;gap:8px;padding:6px 8px;
              border:1px solid transparent;border-radius:var(--r);font-size:13px;
              color:var(--text-primary)">
    <i class="fa-solid fa-microchip" style="color:var(--primary);width:16px;text-align:center"></i>
    <span style="font-weight:500">${esc(d.displayName || d.id)}</span>
    ${statusBadge}
    ${visBadge}
    ${tagsHtml ? '<span style="display:flex;gap:3px;flex-wrap:wrap">' + tagsHtml + '</span>' : ''}
  </div>
  <!-- Properties button -->
  <button title="Device properties"
          onclick="openDevicePropertiesModal('${esc(d.id)}');event.stopPropagation()"
    style="padding:5px 8px;background:none;border:1px solid transparent;
           border-radius:var(--r);cursor:pointer;color:var(--text-muted);flex-shrink:0"
    onmouseover="this.style.background='var(--bg-surface)';this.style.color='var(--primary)';this.style.borderColor='var(--border)'"
    onmouseout="this.style.background='none';this.style.color='var(--text-muted)';this.style.borderColor='transparent'">
    <i class="fa-solid fa-gear" style="font-size:12px"></i>
  </button>
  <!-- Delete button -->
  <button title="Delete device"
          onclick="confirmDeleteDevice('${esc(d.id)}','${esc(d.displayName || d.id)}');event.stopPropagation()"
    style="padding:5px 8px;background:none;border:1px solid transparent;
           border-radius:var(--r);cursor:pointer;color:var(--text-muted);flex-shrink:0"
    onmouseover="this.style.background='var(--danger-bg)';this.style.color='var(--danger)';this.style.borderColor='var(--danger)'"
    onmouseout="this.style.background='none';this.style.color='var(--text-muted)';this.style.borderColor='transparent'">
    <i class="fa-solid fa-trash-can" style="font-size:12px"></i>
  </button>
</div>`;
}

function renderLangNode({ lang, projects }) {
    const open  = _expandedLangs.has(lang.id);
    const count = projects.length;
    return `
<div style="margin-bottom:4px">
  <button onclick="toggleLangNode('${lang.id}')"
    style="display:flex;align-items:center;width:100%;padding:8px 12px;
           background:var(--bg-surface);border:1px solid var(--border);
           border-radius:var(--r);cursor:pointer;gap:10px;font-weight:600;
           font-size:14px;color:var(--text-primary);transition:background var(--tr)">
    <i class="${langIcon(lang.name)}" style="color:var(--primary);width:18px;text-align:center"></i>
    <span style="flex:1;text-align:left">${esc(lang.display)}</span>
    <span style="font-size:11px;font-weight:400;color:var(--text-muted)">${count} project${count!==1?'s':''}</span>
    <i class="fa-solid fa-chevron-${open?'down':'right'}" style="font-size:10px;color:var(--text-muted)"></i>
  </button>
  <div style="display:${open?'block':'none'};padding-left:20px;margin-top:2px">
    <div style="border-left:2px solid var(--border);margin-bottom:2px">
      <div style="display:flex;align-items:center;padding:6px 12px;font-size:13px;
                  font-weight:600;color:var(--text-secondary);gap:8px">
        <i class="fa-solid fa-microchip" style="color:var(--text-muted);width:16px;text-align:center"></i>
        <span>Custom Device</span>
      </div>
      <div style="padding-left:20px">
        ${projects.map(p => renderProjectNode(p)).join('')}
      </div>
    </div>
  </div>
</div>`;
}

// renderProjectNode renders a single project row in the tree.
//
// The row contains:
//   - A toggle button (expand/collapse project files)
//   - A gear button (open Project Properties modal)
//   - A trash button (delete project)
//
// The gear and trash buttons call event.stopPropagation() so clicks on them
// do not bubble up to the toggle button.
function renderProjectNode(p) {
    const open  = _expandedProjs.has(p.id);
    const files = _projectFiles[p.id];
    const badge = p.visibility === 'public'
        ? '<span class="badge badge-pub" style="font-size:10px">public</span>'
        : '<span class="badge badge-dft" style="font-size:10px">private</span>';

    // Show a quality badge when the project is flagged as ready to use.
    const readyBadge = p.readyToUse
        ? '<span style="font-size:10px;font-weight:600;background:#d1fae5;color:#065f46;' +
        'border:1px solid #6ee7b7;border-radius:99px;padding:1px 7px;margin-left:4px">' +
        '<i class="fa-solid fa-check" style="margin-right:3px"></i>ready</span>'
        : '';

    return `
<div style="margin-bottom:2px;border-left:2px solid var(--border)">
  <div style="display:flex;align-items:center;gap:6px;padding:4px 4px 4px 8px">
    <button onclick="toggleProjectNode('${p.id}')"
      style="display:flex;align-items:center;flex:1;gap:8px;padding:6px 8px;
             background:${open?'var(--info-bg)':'none'};
             border:1px solid ${open?'var(--primary)':'transparent'};
             border-radius:var(--r);cursor:pointer;font-size:13px;
             color:var(--text-primary);transition:all var(--tr);text-align:left">
      <i class="fa-solid fa-folder${open?'-open':''}" style="color:var(--primary);width:16px;text-align:center"></i>
      <span style="flex:1;font-weight:500">${esc(p.name)}</span>
      ${badge}${readyBadge}
      <i class="fa-solid fa-chevron-${open?'down':'right'}" style="font-size:10px;color:var(--text-muted)"></i>
    </button>
    <!-- Properties button — opens the project settings modal -->
    <button title="Project properties" onclick="openPropertiesModal('${p.id}');event.stopPropagation()"
      style="padding:5px 8px;background:none;border:1px solid transparent;
             border-radius:var(--r);cursor:pointer;color:var(--text-muted);flex-shrink:0"
      onmouseover="this.style.background='var(--bg-surface)';this.style.color='var(--primary)';this.style.borderColor='var(--border)'"
      onmouseout="this.style.background='none';this.style.color='var(--text-muted)';this.style.borderColor='transparent'">
      <i class="fa-solid fa-gear" style="font-size:12px"></i>
    </button>
    <!-- Delete button -->
    <button title="Delete project" onclick="confirmDeleteProject('${p.id}','${esc(p.name)}');event.stopPropagation()"
      style="padding:5px 8px;background:none;border:1px solid transparent;
             border-radius:var(--r);cursor:pointer;color:var(--text-muted);flex-shrink:0"
      onmouseover="this.style.background='var(--danger-bg)';this.style.color='var(--danger)';this.style.borderColor='var(--danger)'"
      onmouseout="this.style.background='none';this.style.color='var(--text-muted)';this.style.borderColor='transparent'">
      <i class="fa-solid fa-trash-can" style="font-size:12px"></i>
    </button>
  </div>
  <div style="display:${open?'block':'none'};padding:4px 0 4px 24px">
    ${open && files ? renderFileSections(p, files) : ''}
    ${open && !files ? '<div style="padding:8px;color:var(--text-muted);font-size:12px"><i class="fa-solid fa-spinner fa-spin"></i> Loading…</div>' : ''}
  </div>
</div>`;
}

function renderFileSections(p, files) {
    return `
${renderSection(p, 'code', 'fa-solid fa-file-code', 'Code',   files.code, true)}
${renderSection(p, 'img',  'fa-solid fa-image',     'Images', files.img,  false)}
${renderSection(p, 'docs', 'fa-solid fa-file-text', 'Docs',   files.docs, false)}`;
}

// renderSection builds the HTML for one file section (code / img / docs).
//
// Both the code section and the docs section show a "Create" button that opens
// a Monaco editor for a new file. Upload was removed from sections on
// purpose (single upload door: the editor tab strip) — see the addBtn note.
// true. The CODE section is a multi-file SNAPSHOT since Slice 6: every upload
// adds/replaces one file in a new version (the server's snapshot-aware upload
// endpoint), so Upload stays visible up to the server's per-snapshot cap and
// the accept filter follows the PROJECT's language (.go vs .c/.h). Create
// still appears only while the section is empty — it opens the Monaco editor
// for the first file; further files arrive via Upload until the tabbed editor
// ships (Slice 6c). Docs/img keep their original single/multi rules.
//
// Português: A seção de CÓDIGO é um SNAPSHOT multiarquivo desde a Fatia 6:
// cada upload adiciona/substitui UM arquivo numa versão nova, então o Upload
// fica visível até o teto do servidor e o accept segue a linguagem do
// projeto. Create só aparece vazia (abre o Monaco do primeiro arquivo);
// os demais entram por Upload até as abas (6c).
function renderSection(p, section, icon, label, fileList, singleFile) {

    let filesHtml = fileList?.length
        ? fileList.map(f => renderFileRow(p, section, f)).join('')
        : `<div style="font-size:13px;color:var(--text-muted);padding:3px 8px;font-style:italic">No files yet</div>`;

    // The code section and the docs section both get a "Create" button that
    // opens Monaco. Code opens the Go editor; docs opens the Markdown editor.
    let createBtn = '';
    if (section === 'code' && fileList.length === 0) {
        createBtn = `
<button title="Create new file" onclick="projCreateFile('${p.id}')"
  style="padding:3px 8px;background:none;border:1px solid var(--border);
         border-radius:var(--r);cursor:pointer;color:var(--success);font-size:11px;
         display:flex;align-items:center;gap:4px;transition:background var(--tr)"
  onmouseover="this.style.background='var(--bg-surface)'"
  onmouseout="this.style.background='none'">
  <i class="fa-solid fa-plus" style="font-size:10px"></i> Create
</button>`;
    } else if (section === 'docs') {
        // Docs always show a Create button (multiple .md files are allowed).
        createBtn = `
<button title="Create new markdown file" onclick="projCreateMarkdownFile('${p.id}')"
  style="padding:3px 8px;background:none;border:1px solid var(--border);
         border-radius:var(--r);cursor:pointer;color:var(--success);font-size:11px;
         display:flex;align-items:center;gap:4px;transition:background var(--tr)"
  onmouseover="this.style.background='var(--bg-surface)'"
  onmouseout="this.style.background='none'">
  <i class="fa-solid fa-plus" style="font-size:10px"></i> Create
</button>`;
    }

    // [SINGLE UPLOAD DOOR] Card sections are management only (open,
    // rename, delete) — the upload button was removed on purpose; the
    // editor tab strip is the one entry for sources and assets alike.
    // Português: Seções do card são só gestão — o upload foi removido de
    // propósito; a faixa de abas do editor é a única entrada.
    const addBtn = '';

    return `
<div style="margin-bottom:8px">
  <div style="display:flex;align-items:center;gap:8px;padding:4px 6px;
              background:var(--bg-surface);border-radius:var(--r);margin-bottom:3px">
    <i class="${icon}" style="color:var(--text-muted);font-size:12px;width:14px;text-align:center"></i>
    <span style="font-size:12px;font-weight:600;color:var(--text-secondary);flex:1">${label}</span>
    ${createBtn}${addBtn}
  </div>
  <div style="padding-left:22px">${filesHtml}</div>
</div>`;
}

// renderFileRow builds a single file row in the tree.
//
// Design: the entire row is a clickable link that opens the file for editing.
// Action buttons (copy, delete, lock) use event.stopPropagation() so they do
// not trigger the row-level link. This makes the interaction more intuitive —
// users can click anywhere on the row, not just on the file name.
//
// Font size is 14px (previously 12px) for better readability in the file list.
//
// For protected files (e.g. readme.md in docs), the delete button is replaced
// with a lock icon to communicate that the file cannot be deleted. The file
// can still be opened and edited via the Monaco editor.
function renderFileRow(p, section, f) {
    // Determine the primary action triggered by clicking the row.
    let rowAction;
    if (section === 'code') {
        // The row opens the editor AT this file's tab — the path rides
        // as openCodeEditor's activePath.
        rowAction = `openCodeEditor('${p.id}','${esc(f.name)}')`;
    } else if (section === 'img') {
        // Images open in a new tab — use a data attribute and handle in JS
        // to avoid navigating away from the SPA.
        rowAction = `window.open('${f.url}','_blank')`;
    } else {
        rowAction = `projOpenMarkdownEditor('${p.id}','${esc(f.name)}','${esc(f.url)}')`;
    }

    // Build the action buttons. All buttons call event.stopPropagation() so
    // clicks on them do not bubble up to the row's own onclick handler.
    const actions = [];

    if (section === 'img') {
        actions.push(`<button title="Copy Markdown link"
          onclick="event.stopPropagation();copyImageMarkdown('${esc(f.url)}','${esc(f.name)}')"
          style="${rowBtnStyle('primary')}">
          <i class="fa-solid fa-copy" style="font-size:11px"></i></button>`);
    }

    // Code rows get a rename pencil: rename addresses ONE path in the
    // snapshot (the server takes {oldPath, newName} and writes a new
    // version), so the affordance belongs on the row, not on the section
    // toolbar — with several files, "rename the code file" is ambiguous.
    //
    // Português: Linha de código ganha o lápis: rename endereça UM
    // caminho do snapshot, então o botão pertence à linha — com vários
    // arquivos, "renomear o arquivo" seria ambíguo.
    if (section === 'code') {
        actions.push(`<button title="Rename"
          onclick="event.stopPropagation();promptRenameCode('${p.id}','${esc(f.name)}')"
          style="${rowBtnStyle('primary')}">
          <i class="fa-solid fa-pen-line" style="font-size:11px"></i></button>`);
    }

    // Protected files get a lock icon instead of a delete button.
    if (f.protected) {
        actions.push(`<span title="Auto-generated — cannot be deleted"
          style="padding:3px 6px;color:var(--text-muted);font-size:11px;
                 display:inline-flex;align-items:center;opacity:.6">
          <i class="fa-solid fa-lock" style="font-size:11px"></i></span>`);
    } else {
        actions.push(`<button title="Delete"
          onclick="event.stopPropagation();deleteProjectFile('${p.id}','${section}','${esc(f.name)}')"
          style="${rowBtnStyle('danger')}">
          <i class="fa-solid fa-trash-can" style="font-size:11px"></i></button>`);
    }

    return `
<div onclick="${rowAction}"
     style="display:flex;align-items:center;gap:6px;padding:5px 6px;
            border-radius:var(--r);transition:background var(--tr);cursor:pointer"
     onmouseover="this.style.background='var(--bg-surface)'"
     onmouseout="this.style.background='none'">
  <span style="font-size:14px;color:var(--primary);flex:1;
               white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(f.name)}</span>
  <span style="font-size:12px;color:var(--text-muted);flex-shrink:0">${formatBytes(f.size)}</span>
  ${actions.join('')}
</div>`;
}

function rowBtnStyle(v) {
    const c = v === 'danger' ? 'var(--danger)' : 'var(--primary)';
    return `padding:3px 6px;background:none;border:1px solid transparent;border-radius:var(--r);cursor:pointer;color:${c};transition:all var(--tr)`;
}

// ─── Tree interaction ─────────────────────────────────────────────────────────

export function toggleLangNode(langId) {
    if (_expandedLangs.has(langId)) _expandedLangs.delete(langId);
    else _expandedLangs.add(langId);
    renderTree();
}

export async function toggleProjectNode(projectId) {
    if (_expandedProjs.has(projectId)) {
        _expandedProjs.delete(projectId);
        renderTree();
        return;
    }
    _expandedProjs.add(projectId);
    renderTree();
    await loadProjectFiles(projectId);
    renderTree();
}

// ─── Code editor view ─────────────────────────────────────────────────────────

// openCodeEditor loads the project's latest code from the server and switches
// the page to full-width Monaco editor mode.
export async function openCodeEditor(projectId, activePath) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    _editProject = p;
    _editMode    = true;
    _parsedData  = null;
    _parsedSnapshotFP = null;
    _needsExplicitParse = false;
    _parseStatusType    = '';
    _codeVersions       = [];
    _diffHunks          = [];
    _diffChoices        = [];

    // Reset version state.
    _currentVersion = 0;
    _nextVersion    = 1;

    // Install navigation guards so an unsaved buffer prompts the user
    // before the page is closed, refreshed, navigated back, or
    // replaced via an SPA link click. The matching uninstall runs in
    // _doCloseEditor.
    _installEditorNavGuards();

    renderEditorView(_root);

    // Load code from server.
    const r = await api('GET', `/api/v1/projects/${projectId}/files/code`);
    if (r?.metadata?.status !== 200) {
        projSetParseStatus('Failed to load code from server.', 'error');
        return;
    }
    // The snapshot shape: files[] in tab order — one Monaco model per
    // entry (see _tabsInit). An empty project opens with one default
    // tab named from the project's name and LANGUAGE.
    const { files, version, lastParseOk, versions } = r.data;
    _currentVersion = version || 0;
    _nextVersion    = _currentVersion + 1;
    _codeVersions   = versions || [];

    const fileSet = (files && files.length)
        ? files.map(f => ({ path: f.path, content: f.content || '' }))
        : [{ path: slugifyFilename(_editProject.name) + _codeExt(), content: '' }];
    _defaultFilename = fileSet[0].path;

    // ── Recovery from auto-save backup ─────────────────────────────────────
    //
    // If a backup row exists AND its updated_at is newer than the latest
    // version, the user has unsaved work from a previous session — open
    // with the backup content and start the Save button in the red
    // "pending" state.
    //
    // The 404 path is the normal "no unsaved work" response: fall
    // through and use the latest version. Any other failure also falls
    // through to the version path on the principle that being unable
    // to read the backup shouldn't block opening the project.
    _backupPending    = false;
    _backupGreenUntil = 0;
    // Fresh open → session is clean. The user hasn't typed anything
    // YET, even if they're going to recover from a backup below
    // (recovery sets _backupPending true but leaves _backupDirty
    // false: the session itself hasn't produced new edits, so a tab
    // switch right after opening shouldn't re-write the backup).
    _backupDirty      = false;
    try {
        const bk = await api('GET', `/api/v1/projects/${projectId}/files/code/backup`);
        if (bk?.metadata?.status === 200 && bk.data) {
            const latestSavedAt = _codeVersions[0]?.createdAt || '';
            // String comparison works because both timestamps are
            // RFC3339 with the same Z timezone — lexicographic order
            // matches chronological order.
            if (!latestSavedAt || bk.data.updatedAt > latestSavedAt) {
                // The backup carries the WHOLE working copy (6c-3): the
                // recovered set replaces the saved one wholesale, and
                // the recorded activePath wins the focus — unless the
                // user arrived by clicking a specific file in the tree
                // (explicit intent beats remembered state).
                //
                // Português: O backup carrega a CÓPIA INTEIRA: o conjunto
                // recuperado substitui o salvo, e o activePath gravado
                // leva o foco — salvo clique explícito na árvore
                // (intenção explícita vence estado lembrado).
                if (Array.isArray(bk.data.files) && bk.data.files.length) {
                    fileSet.length = 0;
                    for (const f of bk.data.files) {
                        fileSet.push({ path: f.path, content: f.content || '' });
                    }
                    if (!activePath) activePath = bk.data.activePath || '';
                }
                _backupPending = true;
            }
        }
    } catch (e) {
        // Network/parse error reading backup — log and continue with
        // the saved version. Better to open the project than to fail.
        console.warn('[projects] backup recovery failed, using latest version:', e);
    }

    // Ensure project files are loaded so the image autocomplete provider
    // has the img list available as soon as the editor opens.
    if (!_projectFiles[projectId]) {
        await loadProjectFiles(projectId);
    }

    projMountMonaco(fileSet, activePath);
    projRegisterSlashMenu(projectId);
    updateVersionBar();
    // Apply the (possibly red) initial Save button state. Must run
    // after Monaco mounts so the button DOM exists.
    projUpdateSaveBtnState();
    // The Parse button starts disabled on every project open. The
    // transparent silent reparse below (or the user opening the
    // Wizard tab manually) re-enables it once the wizard has
    // verified there are no pending items. This ensures the user
    // never clicks Parse on a project whose wizard state hasn't
    // been checked.
    projUpdateParseBtnState('pending');

    // Trigger initial analysis if there's code.
    if (fileSet.some(f => f.content.trim())) {
        projScheduleAnalyze();

        // Transparent wizard verification on open: always run the
        // parse so the toolbar Parse button can leave its 'pending'
        // state and reflect the real incomplete-items count. The
        // populatePreview flag controls whether the Preview tab
        // also gets filled — that only happens when the saved
        // version was flagged lastParseOk AND the editor matches
        // exactly (no backup recovery, no in-session edits).
        //
        // Failure modes:
        //   - Network error / parse error → silent reparse exits
        //     without touching the button. The button stays
        //     'pending' (disabled). The user resolves manually
        //     by opening the Wizard tab, where the in-tab parse
        //     will surface the error and update the button.
        //   - Editor differs from saved (backup recovery, etc.) →
        //     populatePreview is false; preview stays empty until
        //     user clicks Parse, but the button is still resolved.
        //
        // We do NOT persist the parsed JSON itself: re-running the
        // parser on a known-good source is cheaper than maintaining
        // a snapshot column whose schema would have to evolve in
        // lockstep with BlackBoxDef. The cost is one extra round
        // trip on open; the win is determinism.
        // "Editor matches saved" in set terms: no recovery applied and
        // no tab pending — recovery marks its tab dirty (content ≠
        // savedContent), so _tabsAnyPending covers both readings.
        const populatePreview = !!lastParseOk && !_backupPending && !_tabsAnyPending();
        projSilentReparse(populatePreview);
    }
}

// _unsavedConfirmOpts is a small helper that builds the localized opts
// object for showUnsavedConfirm. The default helper returns English
// labels (Save / Discard / Cancel); we wrap with t() so a Portuguese
// (or any other locale) bundle can override them. The translated keys
// are seeded in server/store/i18n.go — admin can also edit them
// live via the control panel without redeploying.
//
// Português: helper que monta as opções localizadas para o
// showUnsavedConfirm. Cada string passa por t() com fallback em
// inglês — em ambientes que ainda não têm a chave traduzida, o user
// vê inglês ao invés de uma mistura "vai descartar" + "Save".
function _unsavedConfirmOpts(title) {
    return {
        title:        title,
        saveLabel:    t('unsaved.save',    'Save'),
        discardLabel: t('unsaved.discard', 'Discard'),
        cancelLabel:  t('unsaved.cancel',  'Cancel'),
    };
}

// projCloseEditor is the public entry point used by the breadcrumb's
// "Devices & Templates" button. It guards against losing unsaved work
// by checking _isProjectDirty() and routing through showUnsavedConfirm
// when there are pending changes.
//
// _doCloseEditor is the forced version — it tears down Monaco and
// returns to the tree unconditionally. Use it from internal flows
// (programmatic transitions, after a successful save, after a discard
// confirmation). Never bind _doCloseEditor to a user gesture; that's
// what projCloseEditor is for.
//
// Português: porta de saída protegida do editor de código. O botão
// "Devices & Templates" passa por aqui — se o buffer estiver sujo,
// abre o modal Save / Discard / Cancel antes de descartar o trabalho.
// _doCloseEditor é a versão "força bruta" usada por callers
// programáticos que já decidiram fechar (após save bem-sucedido, etc).
export async function projCloseEditor() {
    if (_isProjectDirty()) {
        const choice = await showUnsavedConfirm(
            t('unsaved.editor.leave',
              'The editor has unsaved changes. Leaving now will discard ' +
              'all the work since the last save.'),
            _unsavedConfirmOpts(t('unsaved.editor.title', 'Unsaved changes in the editor'))
        );
        if (choice === 'cancel') return;
        if (choice === 'save') {
            await projSave();
            // projSave clears _backupDirty and _backupPending only on
            // a successful POST. If anything went wrong (parse missing,
            // empty buffer, network error), the flags stay set and we
            // keep the editor open so the user can fix and retry.
            if (_isProjectDirty()) return;
        }
        // discard or successful save → fall through to teardown.
    }
    _doCloseEditor();
}

// _doCloseEditor returns to the tree view, preserving tree expansion state.
// Bypasses the dirty check; only call from flows that have already
// confirmed (or where the buffer is known to be clean).
function _doCloseEditor() {
    _uninstallEditorNavGuards();
    _editMode    = false;
    _editProject = null;
    if (_monacoInst) { _monacoInst.dispose(); _monacoInst = null; }
    _tabsDisposeAll();
    // Dispose the image completion provider so it does not leak across
    // projects when the user navigates back to the tree and opens another.
    if (_imageCompletionDisposable) {
        _imageCompletionDisposable.dispose();
        _imageCompletionDisposable = null;
    }
    clearTimeout(_analyzeTimer);
    _analyzeSeq++;
    renderProjects(_root);
}

// _isProjectDirty is the canonical "user has unsaved work in the
// editor" check. Two flags compose it:
//
//   _backupDirty   — set by Monaco onChange; the user has typed
//                    something this session that hasn't been promoted
//                    to a saved version yet.
//   _backupPending — set when a backup row newer than the latest
//                    version exists on the server. A user who opens a
//                    project that was previously left mid-edit lands
//                    here with _backupDirty=false but the recovered
//                    buffer is still NOT a saved version, so closing
//                    without acting on it would still lose work.
//
// Either flag means "leaving now drops something the user might want
// to keep". The _editMode gate prevents the guard from firing on the
// tree view (where the editor isn't even open).
function _isProjectDirty() {
    // Keystrokes set _backupDirty; recovery sets _backupPending; tab
    // closes/renames leave no keystroke and live only in the ledger —
    // _tabsAnyPending covers them (and re-covers the other two).
    return _editMode && (_backupDirty || _backupPending || _tabsAnyPending());
}

// Navigation guards for the code editor — install on open, uninstall
// on close. Three escape routes are covered:
//
//   1. beforeunload — closing the tab, refreshing the page, navigating
//      to a different origin, quitting the browser. The browser
//      enforces its own modal here; we cannot replace it. By spec
//      (Chrome 60+, anti-phishing) the only thing we can do is set
//      `event.returnValue` on the event, which causes the browser to
//      show its NATIVE confirmation. The text is fixed and not under
//      our control. This is the single departure from the "no native
//      dialogs" rule, and it's forced by the platform.
//
//   2. popstate — Back / Forward buttons, or any history.back() call.
//      The browser has already navigated by the time popstate fires,
//      so to "cancel" we re-push the URL we wanted to keep. Standard
//      SPA pattern.
//
//   3. internal link clicks — anchor tags that would change the page
//      via the SPA router. Captured at the document level; if the
//      buffer is dirty we preventDefault, prompt, and (on cancel)
//      stay put. On save / discard the modal closes and the click is
//      re-emitted programmatically so the SPA's nav() runs as it
//      would have without the guard.
//
// Português: mesmas três rotas que protegem o file manager
// (help_files.js): fechar / recarregar a aba (aviso nativo do
// navegador, imposto por especificação), botão Voltar (popstate
// + restauração via pushState), e cliques em links internos da SPA
// (interceptados na fase de captura).
let _editorNavGuards = null;

function _installEditorNavGuards() {
    if (_editorNavGuards) return;     // idempotent

    const onBeforeUnload = (e) => {
        if (!_isProjectDirty()) return;
        // Both lines are needed for cross-browser behaviour:
        //   - Chrome / modern Edge: preventDefault is enough.
        //   - Firefox / older Safari: returnValue must also be set.
        // The string itself is ignored by every browser since 2018;
        // it stays non-empty as a defensive value for legacy paths.
        e.preventDefault();
        e.returnValue = '';
        return '';
    };

    const onPopState = async (e) => {
        if (!_isProjectDirty()) return;
        // Capture the URL we landed on BEFORE the await — by the
        // time the prompt resolves, the user could have done more
        // navigation, but we restore the entry that matched our
        // intent at the moment popstate fired.
        const url = location.href;
        const choice = await showUnsavedConfirm(
            t('unsaved.editor.back',
              'The editor has unsaved changes. Going back now will ' +
              'discard the work since the last save.'),
            _unsavedConfirmOpts(t('unsaved.editor.title', 'Unsaved changes in the editor'))
        );
        if (choice === 'cancel') {
            history.pushState(null, '', url);
            return;
        }
        if (choice === 'save') {
            await projSave();
            if (_isProjectDirty()) {
                // Save failed — restore the URL and keep the editor.
                history.pushState(null, '', url);
                return;
            }
        }
        // discard or successful save → tear the editor down so the
        // page the user navigated to renders cleanly.
        _doCloseEditor();
    };

    // Click capture for in-page anchor navigations. Only acts on
    // anchors that would actually navigate (left button, no modifier,
    // not target=_blank, not download, not internal to the editor
    // toolbar — which lives inside #proj-editor-view).
    const onLinkClick = async (e) => {
        if (!_isProjectDirty()) return;
        const a = e.target && e.target.closest && e.target.closest('a[href]');
        if (!a) return;
        if (e.button !== 0) return;
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        if (a.target && a.target !== '' && a.target !== '_self') return;
        if (a.hasAttribute('download')) return;
        // Skip clicks within the editor view itself — those are
        // internal UI links (e.g. quota links, file refs) that
        // should never be gated by the dirty prompt.
        const editorRoot = document.getElementById('proj-editor-view');
        if (editorRoot && editorRoot.contains(a)) return;

        e.preventDefault();
        e.stopPropagation();
        const choice = await showUnsavedConfirm(
            t('unsaved.editor.navigate',
              'The editor has unsaved changes. Leaving now will ' +
              'discard the work since the last save.'),
            _unsavedConfirmOpts(t('unsaved.editor.title', 'Unsaved changes in the editor'))
        );
        if (choice === 'cancel') return;
        if (choice === 'save') {
            await projSave();
            if (_isProjectDirty()) return;   // save failed — stay
        }
        // discard or successful save → close the editor and let
        // the user's intended navigation proceed.
        _doCloseEditor();
        // Re-issue the click so the SPA router (or the browser, for
        // full-page hrefs) handles it as it would have without our
        // interception.
        a.click();
    };

    window.addEventListener('beforeunload', onBeforeUnload);
    window.addEventListener('popstate',     onPopState);
    document.addEventListener('click',      onLinkClick, true);

    // Wrap window.nav so SPA-internal navigations triggered by
    // <button onclick="nav('feed')">-style handlers (sidebar buttons,
    // toolbar links that aren't <a> tags) also pass through the dirty
    // check. The original is restored on uninstall.
    //
    // Why monkey-patch instead of adding a hook to router.js? The
    // editor is the only feature that needs this gate, and the gate
    // is only active while the editor is mounted. Owning the wrap
    // here keeps the behaviour close to the state it depends on
    // (_isProjectDirty, projSave) and makes the cleanup explicit:
    // when _doCloseEditor runs, the wrap goes away.
    const originalNav = window.nav;
    const wrappedNav = async function (...args) {
        if (!_isProjectDirty()) return originalNav.apply(this, args);
        const choice = await showUnsavedConfirm(
            t('unsaved.editor.navigate',
              'The editor has unsaved changes. Leaving now will ' +
              'discard the work since the last save.'),
            _unsavedConfirmOpts(t('unsaved.editor.title', 'Unsaved changes in the editor'))
        );
        if (choice === 'cancel') return;
        if (choice === 'save') {
            await projSave();
            if (_isProjectDirty()) return;   // save failed — stay
        }
        // discard or successful save → tear the editor down so the
        // destination renders cleanly, then run the original nav.
        _doCloseEditor();
        return originalNav.apply(this, args);
    };
    window.nav = wrappedNav;

    _editorNavGuards = { onBeforeUnload, onPopState, onLinkClick, originalNav, wrappedNav };
}

function _uninstallEditorNavGuards() {
    if (!_editorNavGuards) return;
    window.removeEventListener('beforeunload', _editorNavGuards.onBeforeUnload);
    window.removeEventListener('popstate',     _editorNavGuards.onPopState);
    document.removeEventListener('click',      _editorNavGuards.onLinkClick, true);
    // Restore window.nav only if we still own it. If something else
    // wrapped nav after we did, we leave that wrap alone — restoring
    // blindly would break the other layer.
    if (window.nav === _editorNavGuards.wrappedNav) {
        window.nav = _editorNavGuards.originalNav;
    }
    _editorNavGuards = null;
}

function renderEditorView(root) {
    const p = _editProject;
    if (!p || !root) return;

    _injectEditorStyles();
    root.innerHTML = `
<div id="proj-editor-view" data-project-id="${esc(p.id)}"
     style="display:flex;flex-direction:column;height:100%">

  <!-- ── Breadcrumb + toolbar ── -->
  <div style="display:flex;align-items:center;gap:10px;padding:10px 20px;
              background:var(--bg-surface);border-bottom:1px solid var(--border);
              flex-wrap:wrap">
    <button class="btn btn-ghost btn-sm" onclick="projCloseEditor()" title="Back to devices">
      <i class="fa-solid fa-arrow-left"></i> Devices & Templates
    </button>
    <span style="color:var(--text-muted);font-size:13px">›</span>
    <span style="font-size:13px;font-weight:600;color:var(--text-primary)">${esc(p.name)}</span>
    <span style="color:var(--text-muted);font-size:13px">› code</span>

    <!-- version bar (dropdown + Diff button) -->
    <div id="proj-version-bar" style="display:flex;align-items:center;gap:6px;margin-left:4px"></div>

    <div style="margin-left:auto;display:flex;gap:6px">
      <button class="btn btn-ghost btn-sm" id="proj-files-btn"
              onclick="projOpenFileManager()"
              title="Attachments for the device manual — not part of the device sources">
        <i class="fa-solid fa-book-open"></i> Manual attachments
      </button>
      <button class="btn btn-ghost btn-sm" id="proj-export-btn"
              onclick="projOpenExportFlow()" title="Export the project as a ZIP ready to publish on GitHub">
        <i class="fa-solid fa-file-export"></i> Github package
      </button>
      <button class="btn btn-ghost btn-sm" id="proj-parse-btn"
              onclick="projParse()" title="Parse Go code">
        <i class="fa-solid fa-play"></i> Parse
      </button>
      <button class="btn btn-primary btn-sm" onclick="projSave()" id="proj-save-btn">
        <i class="fa-solid fa-floppy-disk"></i> Save
      </button>
    </div>
  </div>

  <!-- ── Tab bar: Editor | Preview | Debug  +  Live analysis  +  Rename ── -->
  <div style="display:flex;align-items:center;background:var(--bg-surface);
              border-bottom:1px solid var(--border)">
    <button class="proj-tab active" id="proj-tab-btn-editor" onclick="projSetTab('editor')">
      <i class="fa-solid fa-code"></i> Editor
    </button>
    <button class="proj-tab" id="proj-tab-btn-wizard" onclick="projSetTab('wizard')"
            title="Assisted-creation wizard with cards and modals">
      <i class="fa-solid fa-wand-magic-sparkles"></i> Wizard
    </button>
    <button class="proj-tab" id="proj-tab-btn-preview" onclick="projSetTab('preview')">
      <i class="fa-solid fa-eye"></i> Preview
    </button>
    <button class="proj-tab" id="proj-tab-btn-debug" onclick="projSetTab('debug')">
      <i class="fa-solid fa-bug"></i> Debug
    </button>

    <!-- right-side controls -->
    <div style="margin-left:auto;display:flex;align-items:center;gap:12px;padding:0 14px">
      <label style="display:flex;align-items:center;gap:5px;cursor:pointer;
                    font-size:12px;color:var(--text-muted);user-select:none"
             title="Toggle real-time semantic analysis (600 ms debounce)">
        <input type="checkbox" id="proj-live-chk"
               ${_realtimeAnalysis ? 'checked' : ''}
               onchange="projToggleLiveAnalysis(this.checked)"
               style="cursor:pointer;accent-color:var(--primary)">
        Live analysis
      </label>
      <button class="btn btn-ghost btn-sm" onclick="projRenameActiveTab()"
              title="Rename the active file (committed on Save)">
        <i class="fa-solid fa-pen-line"></i> Rename
      </button>
      <span id="proj-preview-hint" style="font-size:12px;color:var(--text-muted)">
        Click <strong>Parse</strong> to visualise
      </span>
    </div>
  </div>

  <!-- ── Tab bodies (only the active one is visible) ── -->

  <!-- EDITOR tab (contains Monaco) -->
  <div id="proj-editor-wrap" style="width:100%">
    <textarea id="proj-fallback"
      style="width:100%;height:calc(100vh - 186px);min-height:320px;padding:16px;
             border:none;resize:none;font-family:var(--mono);font-size:13px;
             line-height:1.6;background:var(--bg-surface);color:var(--text-primary);
             outline:none"
      placeholder="Loading editor…"></textarea>
  </div>

  <!-- PREVIEW tab -->
  <div id="proj-tab-preview"
    style="display:none;height:calc(100vh - 186px);min-height:320px;
           overflow:auto;background:var(--bg-surface);padding:24px">
    <div style="display:flex;flex-direction:column;align-items:center;
                justify-content:center;min-height:200px;color:var(--text-muted)">
      <i class="fa-solid fa-cube" style="font-size:40px;opacity:.2;display:block;margin-bottom:12px"></i>
      <p>The component will appear here after parsing</p>
    </div>
  </div>

  <!-- DEBUG tab -->
  <div id="proj-tab-debug"
    style="display:none;height:calc(100vh - 186px);min-height:320px;
           overflow:auto;background:var(--bg-surface);padding:16px;
           font-family:var(--mono);font-size:12px"></div>

  <!-- WIZARD tab — body is rendered by projects_wizard.js on first open.
       Layout per CLAUDE_WIZARD_DESIGN.md §11.1: 50/50 grid with Monaco
       on the left (read-only by default) and a scrollable cards panel
       on the right. Slice 3 ships the cards renderer + draft round-trip;
       slices 4–5 wire the modals onto row-click. -->
  <div id="proj-tab-wizard"
    style="display:none;height:calc(100vh - 186px);min-height:320px;
           overflow:auto;background:var(--bg-page)"></div>

  <!-- ── Parse status bar (always visible, below tab content) ── -->
  <div id="proj-parse-status"
    style="padding:8px 20px;font-size:13px;min-height:34px;
           border-top:1px solid var(--border);border-bottom:1px solid var(--border);
           background:var(--bg-page)"></div>

  <!-- ── Bottom toolbar ── -->
  <div style="padding:10px 20px;display:flex;align-items:center;gap:10px;flex-wrap:wrap">
    <span id="proj-save-status" style="font-size:13px;color:var(--success)"></span>
    <div id="proj-form-alert" style="flex:1"></div>
  </div>

</div>
`;
}

// _injectEditorStyles injects the shared .proj-tab and editor CSS into <head>
// once. Called by both the Go code editor and the Markdown editor so both
// share identical tab and pin styles regardless of which one opened first.
function _injectEditorStyles() {
    if (document.getElementById('proj-editor-styles')) return;
    const st = document.createElement('style');
    st.id = 'proj-editor-styles';
    st.textContent = `
/* ── Help panel — rendered Markdown (shared by Projects and Templates guides) ── */
#proj-help-body h1,#tpl-help-body h1{font-size:22px;font-weight:800;color:var(--primary);margin:0 0 20px}
#proj-help-body h2,#tpl-help-body h2{font-size:16px;font-weight:700;margin:28px 0 10px;padding-bottom:6px;border-bottom:1px solid var(--border)}
#proj-help-body h3,#tpl-help-body h3{font-size:14px;font-weight:700;margin:20px 0 8px}
#proj-help-body p,#tpl-help-body p{margin:0 0 12px;color:var(--text-secondary);line-height:1.7}
#proj-help-body ul,#proj-help-body ol,#tpl-help-body ul,#tpl-help-body ol{padding-left:22px;margin:0 0 12px}
#proj-help-body li,#tpl-help-body li{margin-bottom:4px;color:var(--text-secondary)}
#proj-help-body code,#tpl-help-body code{font-family:monospace;font-size:12px;background:var(--bg-surface);padding:2px 6px;border-radius:4px;color:var(--primary)}
#proj-help-body pre,#tpl-help-body pre{background:var(--bg-surface);border:1px solid var(--border);border-radius:8px;padding:16px;overflow-x:auto;margin:0 0 16px}
#proj-help-body pre code,#tpl-help-body pre code{background:none;padding:0;font-size:12px;color:var(--text-primary)}
#proj-help-body table,#tpl-help-body table{border-collapse:collapse;width:100%;margin:0 0 16px;font-size:13px}
#proj-help-body th,#tpl-help-body th{background:var(--bg-surface);font-weight:700;padding:8px 12px;text-align:left;border:1px solid var(--border)}
#proj-help-body td,#tpl-help-body td{padding:7px 12px;border:1px solid var(--border);color:var(--text-secondary)}
#proj-help-body blockquote,#tpl-help-body blockquote{border-left:3px solid var(--primary);margin:0 0 12px;padding:8px 16px;background:var(--bg-surface);border-radius:0 var(--r) var(--r) 0}
#proj-help-body blockquote p,#tpl-help-body blockquote p{margin:0}
#proj-help-body hr,#tpl-help-body hr{border:none;border-top:1px solid var(--border);margin:24px 0}
#proj-help-body a,#tpl-help-body a{color:var(--primary)}.proj-tab{
  background:none;border:none;border-bottom:3px solid transparent;
  padding:10px 16px;font-size:13px;font-weight:700;cursor:pointer;
  color:var(--text-muted);font-family:var(--font);
  transition:color .15s,border-color .15s;
  display:flex;align-items:center;gap:6px;
}
.proj-tab:hover{color:var(--text-primary)}
.proj-tab.active{color:var(--primary);border-bottom-color:var(--primary)}
.proj-ver-select{
  font-size:12px;font-family:var(--mono);
  border:1px solid var(--border);border-radius:var(--r);
  background:var(--bg-input);color:var(--text-primary);
  padding:3px 8px;cursor:pointer;
}
/* pin styles (mirrors blackbox.js) */
.proj-blocks-container{display:flex;flex-direction:column;gap:16px}
.proj-block{display:inline-block;min-width:300px;max-width:100%;
  border:2px solid var(--primary);border-radius:var(--rl);
  background:var(--bg-card);box-shadow:var(--shh);overflow:hidden;
  font-family:var(--mono);font-size:13px;user-select:none}
.proj-block-hdr{background:var(--primary);color:#fff;padding:6px 16px 8px;
  display:flex;flex-direction:column;align-items:center;gap:2px}
.proj-block-hdr-icon{font-size:18px;line-height:1.2}
.proj-block-hdr-label{font-weight:800;font-size:13px;letter-spacing:.03em;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%}
.proj-pin{display:flex;align-items:center;padding:5px 0;
  border-bottom:1px solid var(--border);cursor:default;
  position:relative;transition:background .12s}
.proj-pin:last-child{border-bottom:none}
.proj-pin:hover{background:var(--bg-surface)}
.proj-pin.input{flex-direction:row}
.proj-pin.output{flex-direction:row}
.proj-dot{font-size:18px;line-height:1;flex-shrink:0;cursor:pointer;
  transition:transform .12s;display:flex;align-items:center;
  justify-content:center;width:22px}
.proj-dot.in{color:var(--primary);margin-left:3px}
.proj-dot.out{color:var(--success);margin-right:3px}
.proj-dot:hover{transform:scale(1.2)}
.proj-drag{width:22px;flex-shrink:0;display:flex;align-items:center;
  justify-content:center;color:var(--border);font-size:14px;cursor:grab;
  padding:0 4px;transition:color .12s}
.proj-drag:hover{color:var(--text-muted)}
.proj-drag:active{cursor:grabbing}
.proj-pin-name{padding:1px 6px;border-radius:4px;font-weight:700;
  color:var(--text-primary);min-width:40px;outline:none;
  transition:background .1s,box-shadow .1s;white-space:nowrap;
  overflow:hidden;text-overflow:ellipsis;max-width:100px}
.proj-pin-name[contenteditable="true"]{background:var(--bg-input);
  box-shadow:0 0 0 2px var(--border-focus);cursor:text;user-select:text}
.proj-pin-type{font-size:11px;color:var(--text-muted);padding:1px 7px;
  background:var(--bg-surface);border-radius:99px;border:1px solid var(--border);
  white-space:nowrap;flex-shrink:0;margin:0 3px}
.proj-badges{display:flex;flex-wrap:wrap;gap:3px;padding:0 3px;flex:1;align-items:center}
.proj-badge{font-size:10px;font-family:var(--font);font-weight:600;
  border-radius:99px;padding:1px 7px;white-space:nowrap;
  display:inline-flex;align-items:center;gap:3px}
.proj-badge.range{background:#EEF4FF;color:#2255AA;border:1px solid #BDD3FF}
.proj-badge.unit{background:var(--bg-surface);color:var(--text-muted);border:1px solid var(--border)}
.proj-badge.encoding{background:#FEF3CD;color:#B7770D;border:1px solid #FFD97D}
.proj-badge.flag{background:var(--warning-bg);color:var(--warning);border:1px solid var(--warning)}
.proj-badge.warn{background:#FDE8E8;color:#C0392B;border:1px solid #F5B7B1}
.proj-flag-del{background:none;border:none;cursor:pointer;color:inherit;
  font-size:11px;padding:0 1px;line-height:1;opacity:.7}
.proj-flag-del:hover{opacity:1}
.proj-pin-wrap{position:relative;display:flex;align-items:center;flex:1}
.proj-tooltip{display:none;position:absolute;bottom:calc(100% + 6px);
  left:50%;transform:translateX(-50%);background:#1A1A2E;color:#fff;
  font-size:12px;font-family:var(--font);line-height:1.5;
  padding:8px 12px;border-radius:var(--r);white-space:normal;
  z-index:200;box-shadow:0 4px 16px rgba(0,0,0,.25);pointer-events:none;max-width:320px}
.proj-tooltip::after{content:'';position:absolute;top:100%;left:50%;
  transform:translateX(-50%);border:6px solid transparent;border-top-color:#1A1A2E}
.proj-pin:hover .proj-tooltip{display:block}
.proj-legend{display:flex;gap:20px;justify-content:center;padding:10px 16px;
  border-top:1px solid var(--border);background:var(--bg-surface);
  font-size:12px;color:var(--text-muted);flex-wrap:wrap}
.proj-legend-item{display:flex;align-items:center;gap:5px;font-family:var(--mono)}
/* diff */
.proj-diff-table{width:100%;border-collapse:collapse;table-layout:fixed;
  font-family:var(--mono);font-size:12px;line-height:1.6}
.proj-diff-table td{padding:0;vertical-align:top;border-bottom:1px solid rgba(0,0,0,.04)}
.diff-col-l,.diff-col-r{width:calc(50% - 22px)}
.diff-col-m{width:44px}
.proj-diff-cell-l{padding:0 8px;white-space:pre;overflow:hidden;
  text-overflow:ellipsis;border-right:1px solid var(--border)}
.proj-diff-cell-r{padding:0;white-space:pre;overflow:hidden}
.proj-diff-cell-r[contenteditable]{outline:none;display:block;width:100%;
  min-height:1.6em;padding:0 8px;cursor:text;caret-color:var(--text-primary)}
.proj-diff-cell-m{text-align:center;vertical-align:middle;background:var(--bg-surface);
  border-left:1px solid var(--border);border-right:1px solid var(--border);padding:2px}
.proj-diff-row-eq .proj-diff-cell-l,
.proj-diff-row-eq .proj-diff-cell-r{color:var(--text-muted)}
.proj-diff-arrow{display:block;width:30px;height:20px;margin:1px auto;
  background:var(--bg-card);border:1px solid var(--border);border-radius:4px;
  cursor:pointer;font-size:11px;color:var(--text-muted);
  transition:background .12s,color .12s;line-height:20px;text-align:center}
.proj-diff-arrow:hover{background:var(--primary);color:#fff;border-color:var(--primary)}
.proj-diff-arrow-active{background:var(--primary)!important;color:#fff!important;border-color:var(--primary)!important}
.proj-diff-arrow-off{opacity:.4;background:var(--bg-surface)}
.proj-diff-bg-rem{background:#FFEBE9!important;color:#B91C1C}
.proj-diff-bg-add{background:#E6FFED!important;color:#166534}
.proj-diff-bg-chosen{background:#EFF6FF!important;color:#1D4ED8}
.proj-diff-bg-empty{background:#F9FAFB!important}
.monaco-proj-error-line{background:rgba(239,68,68,.12)!important}
.monaco-proj-error-glyph::before{content:'✕';color:#EF4444;font-size:13px;font-weight:bold;margin-left:2px}
`;
    document.head.appendChild(st);
}

// ─── Monaco (Go editor) ───────────────────────────────────────────────────────

// projMountMonaco is the single entry point for loading and mounting Monaco.
// ─── File tabs: the editor is a WORKING COPY of the snapshot ──────────────────
//
// One Monaco INSTANCE, one MODEL per file. Switching tabs is setModel(),
// which preserves undo history, cursor and scroll per file — setValue()
// would nuke the user's undo stack on every click. Tab operations (add,
// rename, close) are LOCAL to the working copy; Save commits the whole
// set atomically — exactly the snapshot contract the server speaks. The
// tree keeps its snapshot-per-operation semantics: there is no editing
// session there, so immediate commits are honest THERE and wrong HERE
// (three files of scaffolding would burn six noise versions before the
// first line of code).
//
// Deletions and renames need a LEDGER (_tabDeletedPaths): Save merges
// against the server's latest set to preserve files added via the tree
// while the editor was open, and without the ledger that merge would
// resurrect every locally-closed tab.
//
// Português: Um Monaco, um MODEL por arquivo — trocar de aba é setModel()
// (preserva undo/cursor/scroll; setValue() nucaria o histórico a cada
// clique). Operações de aba são LOCAIS à cópia de trabalho; o Save comete
// o conjunto atômico — o contrato que o servidor já fala. A árvore segue
// snapshot-por-operação (lá não há sessão de edição). Deleções/renames
// precisam do LIVRO-RAZÃO: o Save funde com o conjunto mais recente do
// servidor para preservar arquivos subidos pela árvore com o editor
// aberto, e sem o razão essa fusão ressuscitaria toda aba fechada.

let _editorTabs      = [];     // [{path, model, savedContent}] — strip order = snapshot order
let _activeTabIdx    = -1;
let _tabDeletedPaths = new Set(); // paths locally closed/renamed-away; applied at Save
// _tabsSavedOrder is the path sequence of the last load/save. Tab ORDER
// is semantic — it is the snapshot's sort column, the parser's merge
// order (definition-upgrades-prototype scans in it) and "the first
// file's doc is the front page" — so a pure reorder with zero keystrokes
// is still a pending change the Save must be able to see.
//
// Português: Sequência de caminhos do último load/save. A ORDEM das
// abas é semântica (coluna sort, ordem do merge, capa = primeiro
// arquivo), então reordenar sem digitar nada ainda é mudança pendente.
let _tabsSavedOrder  = [];

// Mirrors the server's maxCodeFiles. A drift only hides the "+" button;
// the server remains the gate.
const _TAB_MAX_FILES = 16;

// _tabModelUri builds the in-memory Monaco URI for a tab path. It MUST
// use Uri.from({scheme, path}), never Uri.parse(`inmemory://…/${path}`):
// parse() reads `inmemory://` as scheme+AUTHORITY and then treats the
// rest as a URL, so a path with a slash (an asset folder like
// `templates/portal.html`) is mis-split and model creation throws —
// which, inside the async new-file handler, aborted the prompt silently
// (root files worked, foldered assets did not). from() takes the path
// as a literal, slashes and all.
//
// Português: Constrói a URI do Monaco. TEM que usar from({scheme,path}),
// nunca parse(`inmemory://…/${path}`): parse lê `inmemory://` como
// esquema+AUTORIDADE e trata o resto como URL, então um caminho com
// barra (pasta de asset) é mal-dividido e a criação do model estoura —
// o que, no handler assíncrono, matava o prompt em silêncio.
function _tabModelUri(path) {
    return monaco.Uri.from({
        scheme: 'inmemory',
        path: `/iotm/${_editProject?.id || 'p'}/${path}`,
    });
}

// _tabLang picks the Monaco language by extension. Source keeps the two
// originals; text assets get their natural highlight; anything else
// (binary placeholder tabs) is plaintext.
function _tabLang(path) {
    const p = path.toLowerCase();
    if (p.endsWith('.go')) return 'go';
    if (p.endsWith('.c') || p.endsWith('.h')) return 'c';
    if (p.endsWith('.html') || p.endsWith('.tmpl')) return 'html';
    if (p.endsWith('.json')) return 'json';
    if (p.endsWith('.css')) return 'css';
    if (p.endsWith('.md')) return 'markdown';
    if (p.endsWith('.svg')) return 'xml';
    return 'plaintext';
}

// _tabIsBinary: binary assets travel base64 and render as a read-only
// placeholder — Monaco cannot edit a gif, and pretending otherwise would
// let a keystroke corrupt the payload.
function _tabIsBinary(t) { return t.encoding === 'base64'; }

function _tabsDisposeAll() {
    for (const t of _editorTabs) { try { t.model?.dispose(); } catch (e) {} }
    _editorTabs = [];
    _activeTabIdx = -1;
    _tabDeletedPaths.clear();
}

// _tabsInit rebuilds the working copy from a file set (server load,
// version restore). activePath picks the focused tab — the tree row
// click and the wizard's "focus the touched file" both land here.
function _tabsInit(files, activePath) {
    _tabsDisposeAll();
    const list = (files && files.length) ? files : [];
    for (const f of list) {
        const uri = _tabModelUri(f.path);
        // A stale model under the same URI (previous editor session that
        // skipped cleanup) must go, or createModel throws.
        try { monaco.editor.getModel(uri)?.dispose(); } catch (e) {}
        // Binary assets (encoding base64): the MODEL holds a human
        // placeholder — the real payload stays in rawContent and is what
        // the collector returns. The placeholder is never saved.
        //
        // Português: Asset binário: o MODEL mostra um placeholder; o
        // payload real fica em rawContent e é o que o coletor devolve.
        const isBin = f.encoding === 'base64';
        const shown = isBin
            ? `[binary asset · ${Math.max(1, Math.round((f.content || '').length * 3 / 4 / 1024))} KB · uploaded file]\n`
              + 'This file is stored as-is and shipped with your export.\n'
              + 'Re-upload to replace it.'
            : (f.content || '');
        const model = monaco.editor.createModel(shown, _tabLang(f.path), uri);
        _editorTabs.push({
            path: f.path, model,
            savedContent: shown,
            encoding: f.encoding || '',
            rawContent: isBin ? (f.content || '') : '',
        });
    }
    _tabsSavedOrder = _editorTabs.map(t => t.path);
    let idx = 0;
    if (activePath) {
        const found = _editorTabs.findIndex(t => t.path === activePath);
        if (found >= 0) idx = found;
    }
    if (_editorTabs.length) _tabActivate(idx);
    else _tabsRenderStrip();
}

function _tabActivate(i) {
    if (i < 0 || i >= _editorTabs.length) return;
    _activeTabIdx = i;
    // The single-file era's state variable survives as "the ACTIVE
    // tab's identity" — the backup slot, the wizard bridge and the
    // toolbar rename all read it.
    _defaultFilename = _editorTabs[i].path;
    if (_monacoInst) {
        _monacoInst.setModel(_editorTabs[i].model);
        // Binary assets are read-only — the model shows a placeholder
        // and a keystroke into it would be a lie (the payload lives in
        // rawContent). The flag follows the ACTIVE tab.
        _monacoInst.updateOptions({ readOnly: _tabIsBinary(_editorTabs[i]) });
        setTimeout(() => _monacoInst?.focus(), 0);
    }
    _tabsRenderStrip();
    // The wizard's card column aligns against the ACTIVE model; tell it
    // the model changed (no-op when the wizard isn't showing).
    window._projWizardOnTabSwitch?.();
}

function _tabIsDirty(i) {
    const t = _editorTabs[i];
    return !!t && t.model.getValue() !== t.savedContent;
}

// _tabsAnyPending: does the working copy differ from the last save? Any
// dirty model OR any ledgered deletion/rename counts — the version-switch
// guard and the restore confirm both ask exactly this question.
function _tabsAnyPending() {
    if (_tabDeletedPaths.size > 0) return true;
    for (let i = 0; i < _editorTabs.length; i++) if (_tabIsDirty(i)) return true;
    // A pure reorder: same files, different sequence — semantic, so
    // pending (see _tabsSavedOrder).
    const order = _editorTabs.map(t => t.path);
    if (order.length !== _tabsSavedOrder.length) return true;
    for (let i = 0; i < order.length; i++) {
        if (order[i] !== _tabsSavedOrder[i]) return true;
    }
    return false;
}

// _tabsCollectFiles: the working copy in strip order — tab order IS
// snapshot order (the sort column, the merge order, "first file's doc is
// the front page" all key off it).
function _tabsCollectFiles() {
    // Binary tabs return their preserved payload (the model shows a
    // placeholder); text tabs return the model. Encoding rides along so
    // the gate's rules hold on every save after an upload.
    return _editorTabs.map(t => _tabIsBinary(t)
        ? { path: t.path, content: t.rawContent, encoding: 'base64' }
        : { path: t.path, content: t.model.getValue() });
}

// _tabsValidateName mirrors the server's per-path rules (extension by
// project language, charset, depth, case-insensitive uniqueness) so the
// user hears "no" at the prompt, not at Save. The server remains the
// gate; this copy is UX.
function _tabsValidateName(name, ignoreIdx) {
    const n = (name || '').trim();
    if (!n) return 'Name is required.';
    if (n.length > 160) return 'Name too long (max 160).';
    if (n.startsWith('/') || n.includes('\\')) return 'Use a relative path with "/".';
    const segs = n.split('/');
    if (segs.length > 4) return 'Path too deep (max 4 segments).';
    for (const seg of segs) {
        if (!/^[A-Za-z0-9_][A-Za-z0-9._-]*$/.test(seg)) {
            return `Invalid path segment: "${seg}".`;
        }
    }
    const lang = _projectLangById(_editProject?.id);
    const lower = n.toLowerCase();
    // Extension rules mirror the server's gate: SOURCE by language plus
    // the shared TEXT-asset whitelist. Binary assets (gif/png/jpg) are
    // upload-only — an empty tab named logo.gif would be a lie the gate
    // rejects at Save; say "no" here instead.
    //
    // Português: Espelho do portão: FONTE por linguagem + whitelist de
    // assets de TEXTO. Binário é só-upload — aba vazia chamada logo.gif
    // seria mentira; o não vem aqui, não no Save.
    const ext = lower.slice(lower.lastIndexOf('.'));
    const textAsset = ['.html', '.htm', '.tmpl', '.txt', '.json', '.csv', '.svg', '.md', '.css'].includes(ext);
    const binaryAsset = ['.gif', '.png', '.jpg', '.jpeg'].includes(ext);
    const source = lang === 'c' ? (ext === '.c' || ext === '.h') : ext === '.go';
    if (binaryAsset) {
        return 'Binary assets are added by upload, not created empty.';
    }
    if (!source && !textAsset) {
        return lang === 'c'
            ? 'C projects accept .c/.h source, or text assets (html, htm, tmpl, txt, json, csv, svg, md, css).'
            : 'Go projects accept .go source, or text assets (html, htm, tmpl, txt, json, csv, svg, md, css).';
    }
    for (let i = 0; i < _editorTabs.length; i++) {
        if (i === ignoreIdx) continue;
        if (_editorTabs[i].path.toLowerCase() === lower) {
            return `A file named "${_editorTabs[i].path}" already exists.`;
        }
    }
    return '';
}

async function _tabAdd() {
    if (_editorTabs.length >= _TAB_MAX_FILES) {
        showPageAlert(`A project holds at most ${_TAB_MAX_FILES} files.`, 'warning');
        return;
    }
    const lang = _projectLangById(_editProject?.id);
    const suggestion = lang === 'c' ? 'util.c' : 'helpers.go';
    const name = await showPrompt('New file (' + (lang === 'c' ? '.c/.h' : '.go') + ' or text asset):', suggestion);
    if (!name) return;
    const msg = _tabsValidateName(name);
    if (msg) { showPageAlert(msg, 'warning'); return; }
    const trimmed = name.trim();
    // Anything throwing past this point used to die silently (async
    // handler — unhandled rejection, popup already gone): the exact
    // failure shape that cost a field investigation on 2026-07-08.
    // Surface it instead.
    //
    // Português: Qualquer exceção daqui em diante morria em silêncio
    // (popup já fechado). Agora aparece.
    try {
    const uri = _tabModelUri(trimmed);
    try { monaco.editor.getModel(uri)?.dispose(); } catch (e) {}
    const model = monaco.editor.createModel('', _tabLang(trimmed), uri);
    // savedContent stays '' — a brand-new tab is born dirty only once it
    // has content; an empty new tab that gets closed again cost nothing.
    // Re-creating a path that was ledgered as deleted un-deletes it.
    _tabDeletedPaths.delete(trimmed);
    _editorTabs.push({ path: trimmed, model, savedContent: '' });
    _tabActivate(_editorTabs.length - 1);
    projScheduleBackup?.();
    } catch (e) {
        showPageAlert(`Could not create "${trimmed}": ${e?.message || e}`, 'error');
    }
}

async function _tabRenameAt(i) {
    const t = _editorTabs[i];
    if (!t) return;
    const name = await showPrompt('Rename file:', t.path);
    if (!name || name.trim() === t.path) return;
    const msg = _tabsValidateName(name, i);
    if (msg) { showPageAlert(msg, 'warning'); return; }
    // A rename is delete-old + carry-new in ledger terms: the Save merge
    // must not resurrect the old path from the server's set.
    _tabDeletedPaths.add(t.path);
    t.path = name.trim();
    _tabDeletedPaths.delete(t.path);
    // Language may change with the extension (.c ↔ .h keeps c; the Go
    // case can't cross languages — validation forbids it).
    monaco.editor.setModelLanguage(t.model, _tabLang(t.path));
    if (i === _activeTabIdx) _defaultFilename = t.path;
    _tabsRenderStrip();
}

async function _tabCloseAt(i) {
    const t = _editorTabs[i];
    if (!t) return;
    if (_editorTabs.length === 1) {
        // Client-side mirror of the server's "a snapshot needs ≥1 file".
        showPageAlert('A project needs at least one file. Emptying the project is done from the file tree.', 'warning');
        return;
    }
    if (_tabIsDirty(i)) {
        const ok = await showConfirm(`"${t.path}" has unsaved changes. Close it anyway?`);
        if (!ok) return;
    }
    _tabDeletedPaths.add(t.path);
    try { t.model.dispose(); } catch (e) {}
    _editorTabs.splice(i, 1);
    if (_activeTabIdx >= _editorTabs.length) _activeTabIdx = _editorTabs.length - 1;
    else if (i < _activeTabIdx) _activeTabIdx--;
    _tabActivate(Math.max(0, _activeTabIdx));
}

// _tabsRenderStrip paints the file strip. Full repaint on structural
// changes; the per-keystroke dirty dot is toggled surgically by the
// editor's onDidChangeModelContent (see _doMount) to keep typing cheap.
function _tabsRenderStrip() {
    const strip = document.getElementById('proj-file-tabs');
    if (!strip) return;
    const parts = _editorTabs.map((t, i) => {
        const active = i === _activeTabIdx;
        // Nested paths show whole (they are identity); the strip scrolls
        // horizontally when crowded. Tooltip carries the full path anyway.
        const dot = `<span id="proj-ftab-dot-${i}" style="display:${_tabIsDirty(i) ? 'inline' : 'none'};
            color:var(--warning,#d97706);font-size:10px;line-height:1">●</span>`;
        const close = _editorTabs.length > 1
            ? `<span title="Close" onclick="event.stopPropagation();window._projTabClose(${i})"
                 style="opacity:.55;font-size:12px;padding:0 2px;cursor:pointer"
                 onmouseover="this.style.opacity='1'" onmouseout="this.style.opacity='.55'">×</span>`
            : '';
        return `
<div onclick="window._projTabActivate(${i})" ondblclick="window._projTabRename(${i})"
     draggable="true"
     ondragstart="window._projTabDragStart(event, ${i})"
     ondragover="event.preventDefault()"
     ondrop="window._projTabDrop(event, ${i})"
     title="${esc(t.path)} — drag to reorder · double-click to rename"
     style="display:flex;align-items:center;gap:6px;padding:5px 10px;cursor:pointer;
            white-space:nowrap;font-size:12px;font-family:'Fira Code','Consolas',monospace;
            border-right:1px solid var(--border);
            ${active
                ? 'background:var(--bg-body,#fff);color:var(--text-primary);border-bottom:2px solid var(--primary)'
                : 'background:var(--bg-surface);color:var(--text-secondary);border-bottom:2px solid transparent'}">
  <span>${esc(t.path)}</span>${dot}${close}
</div>`;
    });
    // "+" appears up to the snapshot cap for BOTH languages since GoMF:
    // a Go project is a Go PACKAGE (struct in one file, methods across
    // siblings), so multi-file stopped being a C privilege.
    //
    // Português: "+" até o teto nas DUAS linguagens desde o GoMF:
    // projeto Go é um PACOTE Go — multiarquivo deixou de ser privilégio
    // do C.
    const canAddTab = _editorTabs.length < _TAB_MAX_FILES;
    // [SINGLE UPLOAD DOOR] The editor tab strip is the ONE place files
    // enter a device: New file (existing flow) + Upload file, side by
    // side. Upload reuses the snapshot machinery (onFileSelected →
    // /files/code → editor resync), so sources AND assets land as tabs —
    // binary ones as base64 placeholders. The card sections and the
    // manual-attachments modal no longer take uploads (field report
    // 2026-07-11: two doors named "Files" sent an asset to the manual).
    // Português: A faixa de abas é a ÚNICA porta de entrada de arquivos
    // do device: New file + Upload file, lado a lado. O upload reusa a
    // maquinaria do snapshot, então fontes E assets viram abas — binários
    // como placeholders base64. As seções do card e o modal de anexos do
    // manual não recebem mais upload (report 2026-07-11: duas portas
    // chamadas "Files" mandaram um asset para o manual).
    const upAccept = (_editProject && _projectLangById(_editProject.id) === 'c')
        ? '.c,.h,.html,.htm,.tmpl,.txt,.json,.csv,.svg,.md,.css,.gif,.png,.jpg,.jpeg'
        : '.go,.html,.htm,.tmpl,.txt,.json,.csv,.svg,.md,.css,.gif,.png,.jpg,.jpeg';
    const plus = canAddTab ? `
<button title="New file" onclick="window._projTabAdd()"
  style="padding:4px 8px;background:none;border:none;cursor:pointer;
         color:var(--success);font-size:13px">
  <i class="fa-solid fa-plus"></i>
</button>
<button title="Upload file — sources and assets become part of the device"
  onclick="document.getElementById('proj-editor-upload')?.click()"
  style="padding:4px 8px;background:none;border:none;cursor:pointer;
         color:var(--primary);font-size:12px">
  <i class="fa-solid fa-upload"></i>
</button>
<input type="file" id="proj-editor-upload" accept="${upAccept}"
  style="display:none" onchange="window._projTabUploadPick(event)">` : '';
    strip.innerHTML = `<div style="display:flex;align-items:stretch;overflow-x:auto">${parts.join('')}${plus}</div>`;
}

// Strip handlers go through window: the strip HTML is innerHTML-built,
// so inline onclick needs globals — same convention as the tree.
window._projTabActivate = (i) => _tabActivate(i);
// Drag reorder — native HTML5 DnD, index in dataTransfer. The drop
// splices the tab to the target position; the active tab is tracked by
// OBJECT identity across the splice so focus follows the file, not the
// index. Reordering is a pending change (see _tabsSavedOrder) and the
// parse fingerprint diverges on its own — JSON order is snapshot order
// — so lastParseOk cannot ride a reorder without a re-parse.
//
// Português: Reordenar por arraste — DnD nativo. O drop move a aba; o
// foco segue o ARQUIVO (identidade de objeto), não o índice. Reordenar
// é mudança pendente e o fingerprint do parse diverge sozinho.
window._projTabDragStart = (ev, i) => {
    ev.dataTransfer.setData('text/plain', String(i));
    ev.dataTransfer.effectAllowed = 'move';
};
window._projTabDrop = (ev, to) => {
    ev.preventDefault();
    const from = parseInt(ev.dataTransfer.getData('text/plain'), 10);
    if (isNaN(from) || from === to) return;
    if (from < 0 || from >= _editorTabs.length) return;
    const act = _editorTabs[_activeTabIdx];
    const [moved] = _editorTabs.splice(from, 1);
    _editorTabs.splice(to, 0, moved);
    _activeTabIdx = _editorTabs.indexOf(act);
    _tabsRenderStrip();
    projScheduleBackup?.();
};
window._projTabRename   = (i) => { _tabRenameAt(i); };
window._projTabClose    = (i) => { _tabCloseAt(i); };
window._projTabAdd      = () => { _tabAdd(); };
// Upload picked from the editor tab strip: same snapshot pipeline as any
// code-section upload; the resync inside onFileSelected turns the new
// file into a tab (binary → base64 placeholder).
// Português: Upload da faixa de abas: mesmo pipeline de snapshot; o
// resync do onFileSelected transforma o arquivo em aba.
window._projTabUploadPick = (ev) => {
    if (_editProject) onFileSelected(ev, _editProject.id, 'code');
};

// _resyncOpenEditorFromServer: the tree just changed this project's
// snapshot (upload / per-path delete / pencil rename) WHILE the editor
// may be open on it. A stale working copy would make the next Save
// resurrect or duplicate paths, so: clean copy → silently re-init from
// the server (active tab preserved by path); dirty copy → warn and do
// NOT touch the buffers — discarding unsaved work to chase the tree
// would be worse than the divergence, and the Save merge still
// preserves tree-added files either way.
//
// Português: A árvore mudou o snapshot com o editor possivelmente
// aberto nele. Cópia limpa → re-inicializa do servidor em silêncio;
// suja → avisa e NÃO toca os buffers — descartar trabalho não salvo
// seria pior que a divergência.
async function _resyncOpenEditorFromServer(projectId) {
    if (!_editMode || _editProject?.id !== projectId) return;
    if (_tabsAnyPending()) {
        showPageAlert('The file tree changed this project, but the editor has unsaved work — save or discard before the tabs reflect it.', 'warning');
        return;
    }
    try {
        const r = await api('GET', `/api/v1/projects/${projectId}/files/code`);
        if (r?.metadata?.status === 200) {
            _tabsInit(r.data?.files || [], _editorTabs[_activeTabIdx]?.path);
        }
    } catch (e) {
        console.warn('[projects] editor resync failed:', e);
    }
}

// projRenameActiveTab: the editor toolbar's Rename. LOCAL to the
// working copy (committed on Save) — unlike the tree's pencil, which
// commits a snapshot immediately: the tree has no editing session, the
// editor IS one, and each context gets the semantics that is honest
// there.
//
// Português: Rename da toolbar do editor — LOCAL à cópia de trabalho
// (comete no Save); o lápis da árvore comete na hora: lá não há sessão
// de edição, aqui HÁ, e cada contexto leva a semântica honesta.
export function projRenameActiveTab() { _tabRenameAt(_activeTabIdx); }

// It is called by openCodeEditor after the editor shell is rendered.
// Lazy-loads the CDN script once; subsequent calls mount immediately.
function projMountMonaco(files, activePath) {
    if (window.monaco) {
        _monacoReady = true;
        _doMount(files, activePath);
        return;
    }
    // _monacoReady=true but window.monaco missing means the AMD loader was
    // disrupted (e.g. by the WASM IDE). Remove the stale script tag so it
    // can be re-injected and Monaco reloads cleanly.
    if (_monacoReady) {
        _monacoReady = false;
        const stale = document.getElementById('proj-monaco-loader');
        if (stale) stale.remove();
    }
    if (!document.getElementById('proj-monaco-loader')) {
        const s = document.createElement('script');
        s.id  = 'proj-monaco-loader';
        s.src = '/monaco/vs/loader.js';
        s.onload = () => {
            require.config({ paths: { vs: '/monaco/vs' } });
            require(['vs/editor/editor.main'], () => { _monacoReady = true; _doMount(files, activePath); });
        };
        document.head.appendChild(s);
    }
}

function _doMount(files, activePath) {
    const wrap = document.getElementById('proj-editor-wrap');
    if (!wrap) return;
    const ta = document.getElementById('proj-fallback');
    if (ta) ta.remove();
    if (_monacoInst) { _monacoInst.dispose(); _monacoInst = null; }
    _tabsDisposeAll();

    // The HOST holds the file-tab strip AND the editor, and it is the
    // host that projSetTab re-parents into the Wizard panel — so the
    // wizard inherits the tabs for free, and "focus the touched file"
    // (6c-4) is just _tabActivate from over there.
    //
    // Português: O HOST carrega a faixa de abas E o editor, e é o host
    // que o projSetTab re-parenta pro painel do Wizard — o wizard herda
    // as abas de graça.
    const div = document.createElement('div');
    div.style.cssText = 'display:flex;flex-direction:column;width:100%';

    const strip = document.createElement('div');
    strip.id = 'proj-file-tabs';
    strip.style.cssText = 'flex:0 0 auto;border-bottom:1px solid var(--border);background:var(--bg-surface)';
    div.appendChild(strip);

    const editorDiv = document.createElement('div');
    // Height = viewport minus: breadcrumb(44) + tab-bar(42) + file
    // strip(30) + status(34) + bottom(52) + borders(14) ≈ 216px
    editorDiv.style.cssText = 'height:calc(100vh - 216px);min-height:320px;width:100%';
    div.appendChild(editorDiv);
    wrap.appendChild(div);

    // Track the Monaco host div on the module scope and on window so
    // projSetTab can re-parent it into the Wizard tab's left panel
    // when the user switches there. There is exactly ONE Monaco
    // instance for the page — we never create a second one. Moving
    // the DOM node preserves cursor, undo history, decorations, and
    // model state. Monaco's `automaticLayout: true` option above
    // means we don't need to call `.layout()` after the move; it
    // observes the container and resizes itself.
    _monacoHostDiv = div;
    window._projMonacoHost = div;

    // Language hint for Monaco's syntax highlighter — derived from
    // the project's programmingLanguageId so the editor matches the
    // source it is being asked to host. The Slice-1 mapping covers
    // Go and C; new languages register here when their parser ships.
    //
    // Português: Hint de linguagem pro Monaco — baseado na linguagem
    // do projeto. Slice 1 cobre Go e C.
    const projLang = _currentProjectLanguage();
    const monacoLang = projLang === 'c' ? 'c' : 'go';

    // Indentation style — Go uses real tabs (gofmt convention);
    // C is typically 4-space indented but tolerates both. Match
    // the language norm so what the specialist types lines up with
    // what would be acceptable in their target ecosystem.
    const useTabs = (monacoLang === 'go');

    _monacoInst = monaco.editor.create(editorDiv, {
        // No value/language here: content lives in one MODEL PER FILE
        // (_tabsInit below); monacoLang keeps informing the indentation
        // choice only.
        model: null, theme: 'vs',
        wordWrap: 'off', lineNumbers: 'on',
        minimap: { enabled: true }, scrollBeyondLastLine: false,
        fontSize: 13, fontFamily: "'Fira Code','Consolas',monospace",
        automaticLayout: true, glyphMargin: true,
        wordBasedSuggestions: false,
        quickSuggestions: false,
        suggestOnTriggerCharacters: false,
        parameterHints: { enabled: false },
        // Go uses tabs (gofmt). C is space-indented by convention. The
        // useTabs flag flips both insertSpaces and detectIndentation.
        insertSpaces: !useTabs,
        tabSize: 4,
        detectIndentation: false,
    });

    _monacoInst.onDidChangeModelContent(() => {
        // Dirty dot of the ACTIVE tab, toggled surgically — a full strip
        // repaint per keystroke would be wasteful and would steal focus
        // states mid-typing.
        const dot = document.getElementById(`proj-ftab-dot-${_activeTabIdx}`);
        if (dot) dot.style.display = _tabIsDirty(_activeTabIdx) ? 'inline' : 'none';

        const diffWrap = document.getElementById('proj-diff-table-wrap');
        if (diffWrap) {
            const sel = document.getElementById('proj-diff-ver-sel');
            if (sel) {
                const saved = window._projDiffCodeMap?.[sel.value] || '';
                _diffHunks   = projDiffHunks(saved, projGetCode());
                _diffChoices = _diffHunks.filter(h => h.type !== 'equal').map(() => 'right');
                projRefreshDiffTable();
            }
        }
        projScheduleAnalyze();
        if (_parseStatusType === 'ok' || _parseStatusType === 'warning') projSetParseStatus('', '');
        // When the source changes, the cached parsed data no longer
        // represents the editor's content. Drop it and revert the
        // Preview tab to its empty-state placeholder so the user
        // doesn't read a stale device drawing as authoritative.
        //
        // _parsedSnapshotFP (the source string that was last
        // parsed successfully) is also cleared — its only purpose
        // is to confirm "the parsed data on hand still matches
        // what's in the editor", and after this edit it doesn't.
        if (_parsedData) {
            _parsedData = null;
            _parsedSnapshotFP = null;
            _needsExplicitParse = true;
            const prev = document.getElementById('proj-tab-preview');
            if (prev) {
                prev.innerHTML = `<div style="display:flex;flex-direction:column;align-items:center;
                    justify-content:center;min-height:200px;color:var(--text-muted)">
                  <i class="fa-solid fa-cube" style="font-size:40px;opacity:.2;display:block;margin-bottom:12px"></i>
                  <p>The component will appear here after parsing</p>
                </div>`;
            }
            const dbg = document.getElementById('proj-tab-debug');
            if (dbg) dbg.innerHTML = '';
        }
        // Backup the working source ~2s after the user pauses typing.
        // We don't enter the red "pending" state until the POST returns
        // success — that avoids flicker on the very first keystroke
        // before the network round-trip completes.
        //
        // Mark the session dirty unconditionally: even if the debounce
        // doesn't fire (user keeps typing past 2s), a tab switch needs
        // to know there's pending work to flush. The Monaco onChange
        // handler runs synchronously on every keystroke, so this is
        // the canonical "user touched the source" signal.
        _backupDirty = true;
        projScheduleBackup();
    });

    // Expose Monaco to the Wizard tab module without forcing a circular
    // import. projects_wizard.js reads window._projMonacoInst when it
    // needs to read or write the source. Setting this right after
    // create() (rather than lazily inside projSetTab) means the wizard
    // can be opened any time after this point with no race.
    window._projMonacoInst = _monacoInst;

    // Working copy born here: one model per file, active tab focused.
    _tabsInit(files, activePath);

    // Focus the editor so the user can start typing immediately. This
    // matters most for "Create with wizard" flow where the editor is
    // empty and the user's first action is to write code; without an
    // explicit focus call they'd have to click the editor first.
    // Wrapped in setTimeout so the focus runs after Monaco's own
    // post-mount layout settles, otherwise the cursor sometimes lands
    // mid-render and the caret blinks at row 0 col 0 unhelpfully.
    setTimeout(() => _monacoInst?.focus(), 50);
}

function projGetCode() {
    if (_monacoInst) return _monacoInst.getValue();
    return document.getElementById('proj-fallback')?.value || '';
}

// ─── Parse / Analyze ──────────────────────────────────────────────────────────

// _currentProjectLanguage returns the canonical language token for
// the project currently open in the editor. Reads the project list
// (_projects) using the id from the editor view's data attribute.
// Returns "go" when the project cannot be resolved — the parser
// defaults to Go anyway, so this is the safe fallback.
//
// Token mapping (matches both Monaco's language id and the value
// the server's wizard handler accepts in /wizard/parse):
//
//   programmingLanguageId  →  return value
//   ─────────────────────     ────────────
//   "golang"                  "go"
//   "c"                       "c"
//   (anything else)           "go"
//
// Exposed via window._projGetLanguage so the wizard tab module
// (projects_wizard.js) can read it without importing this file
// directly — projects.js already imports the wizard module, so a
// reverse import would create a cycle. The window bridge follows
// the same convention as window._projMonacoHost / _projMonacoInst.
function _currentProjectLanguage() {
    return _projectLangById(_currentProjectId());
}

// _projectLangById resolves a project's language token by id — the
// context-free sibling of _currentProjectLanguage. The tree view (rename
// pencil, upload accept filter, delete rules) runs OUTSIDE the editor,
// where _currentProjectId() is empty; resolving from the id argument is
// what keeps a C project from being treated as Go there (caught live:
// the rename prompt suggested "main.go" for a C99 project).
//
// Português: Resolve a linguagem PELO id — a irmã sem-contexto de
// _currentProjectLanguage. A árvore roda FORA do editor, onde
// _currentProjectId() é vazio; resolver pelo argumento é o que impede
// projeto C de ser tratado como Go ali (pego ao vivo: o rename sugeria
// "main.go" num projeto C99).
function _projectLangById(projectId) {
    if (!projectId) return 'go';
    const proj = _projects.find(p => p.id === projectId);
    const langId = proj?.programmingLanguageId || '';
    switch (langId) {
        case 'c':      return 'c';
        case 'golang': return 'go';
        default:       return 'go';
    }
}

// _codeExtFor: default source extension for a project resolved BY ID —
// use this in tree-view code; _codeExt() below stays for editor-context
// code where the current project is the open one.
function _codeExtFor(projectId) {
    return _projectLangById(projectId) === 'c' ? '.c' : '.go';
}

// _codeExt returns the default source extension for the CURRENT
// project's language. The save contract validates extensions per
// language server-side (go → .go; c → .c/.h), so a C project whose
// derived default said ".go" would 400 on its very first save.
function _codeExt() {
    return _currentProjectLanguage() === 'c' ? '.c' : '.go';
}

// Make the language resolver visible to other page modules.
// projects_wizard.js calls this when it issues its own
// /wizard/parse requests (the wizard tab has two such call sites
// that bypass projParse — they need the same `language` field).
if (typeof window !== 'undefined') {
    window._projGetLanguage = _currentProjectLanguage;
    // The wizard's parse/rewrite calls send the FILE-SET shape and need
    // the active tab's path so its parses address the same snapshot
    // identity the editor saves — "lastParseOk" only means something if
    // both sides name the file the same way.
    window._projGetFilename = () =>
        _editorTabs[_activeTabIdx]?.path || _defaultFilename ||
        ('main' + _codeExt());
    // The wizard's full-set bridge (consumed in 6c-4): parse/rewrite
    // want EVERY tab — header types only resolve with the whole copy.
    window._projGetAllFiles = () => _tabsCollectFiles();
    // Focus-the-touched-file bridge: the wizard's rewrite response
    // names the file it changed; landing the user on that tab is one
    // call from over there.
    window._projActivateTabByPath = (path) => {
        const i = _editorTabs.findIndex(t => t.path === path);
        if (i >= 0) _tabActivate(i);
    };
    // The wizard's rewrite response returns the full updated file set;
    // this bridge lands it in the MODELS. Each differing file gets an
    // UNDOABLE full-range edit (pushEditOperations — Ctrl+Z reverts a
    // wizard edit like any keystroke). Unknown paths are ignored (the
    // engine never creates files) and savedContent is NOT advanced:
    // a rewrite is unsaved work — the dirty dot lights up until Save.
    //
    // Português: A resposta do rewrite entra nos MODELS por aqui.
    // Edição DESFAZÍVEL por arquivo; caminhos desconhecidos ignorados;
    // savedContent NÃO avança — rewrite é trabalho não salvo.
    window._projApplyRewrittenFiles = (files) => {
        if (!Array.isArray(files)) return;
        for (const f of files) {
            const t = _editorTabs.find(x => x.path === f.path);
            if (!t || typeof f.content !== 'string') continue;
            if (t.model.getValue() === f.content) continue;
            t.model.pushEditOperations(
                [],
                [{ range: t.model.getFullModelRange(), text: f.content }],
                () => null,
            );
        }
        const dot = document.getElementById(`proj-ftab-dot-${_activeTabIdx}`);
        if (dot) dot.style.display = _tabIsDirty(_activeTabIdx) ? 'inline' : 'none';
    };
}

export async function projParse() {
    const rawCode = projGetCode(); // active tab — feeds the preview drawing
    if (!_tabsCollectFiles().some(f => f.content.trim())) {
        projSetParseStatus('Code is empty', 'warning'); return;
    }

    projSetParseStatus('<i class="fa-solid fa-circle-notch fa-spin"></i> Parsing…', '');
    try {
        // api() attaches the Bearer token, sends JSON, and returns the parsed
        // envelope. The wizard endpoint requires auth (see slice 0 in
        // docs/tasks/WIZARD_TASKS.md and CLAUDE_WIZARD_DESIGN.md §8).
        //
        // The `language` field is the Slice-1 addition for C99 support.
        // It is optional — the server defaults to "go" when omitted —
        // but sending it makes the routing explicit and lets the
        // server reject mismatches early.
        const lang = _currentProjectLanguage();
        const json = await api('POST', '/api/v1/blackbox/wizard/parse',
            { files: _tabsCollectFiles(), language: lang });
        if (json?.metadata?.status !== 200) {
            projSetParseStatus('✗ ' + (json?.metadata?.error || 'Parse error'), 'error');
            return;
        }

        projSetParseStatus('<i class="fa-solid fa-circle-notch fa-spin"></i> Analysing…', '');
        clearTimeout(_analyzeTimer);
        const analyzeResult = await projRunAnalyze();
        if (analyzeResult?.hasErrors) {
            // Per-file shape (GoMF): flatten with each diagnostic
            // carrying its path for the status line.
            const flat = (analyzeResult.files || []).flatMap(fd =>
                (fd.diagnostics || []).map(d => ({ ...d, path: fd.path })));
            projSetParseStatus(projFormatAnalyzeErrors(flat), 'error');
            return;
        }

        projApplyMonacoMarkersPerFile([]);
        projFormAlert('', '');
        _needsExplicitParse = false;

        // Slice 2 of the wizard plan made /wizard/parse return
        //   { parsed: <BlackBoxDef>, incomplete: [<dotted paths>] }
        // instead of the BlackBoxDef directly under data. The new
        // `incomplete` peer is the canonical signal for the ⚠ badges
        // the Wizard tab will render in slice 3; the Editor tab below
        // does not consume it yet, so we just store the parsed half.
        _parsedData = json.data.parsed;
        _parsedData.sourceCode = rawCode;
        // Remember which exact WORKING COPY produced this _parsedData
        // so Save can flag lastParseOk honestly and the silent re-parse
        // on open knows whether it can be skipped.
        _parsedSnapshotFP = _snapshotFingerprint();

        // Bridge to the Wizard tab. projWizardOnEditorParseSuccess
        // is a no-op when the user has not opened the wizard tab yet,
        // so calling it unconditionally is cheap and avoids a "stale
        // data" failure mode where the wizard cards lag behind the
        // editor. We pass the raw response data (not _parsedData)
        // because the wizard renders cards directly from the
        // unmodified server output. It returns the incomplete (⚠)
        // count, which decides the post-parse tab below.
        const incompleteCount = projWizardOnEditorParseSuccess(rawCode, json.data);

        // Field names below come from server/codegen/blackbox/types.go
        // (BlackBoxDef): `methods`, `props`, plus per-method `inputs` and
        // `outputs`. The legacy `_parser.go` used `settings` and surfaced
        // a `parseWarnings` field; both are gone now that the codegen
        // parser is the single source of truth (see slice 0 of the
        // wizard plan in docs/CLAUDE_WIZARD_DESIGN.md §2). The status
        // line and the card renderer below speak the live names.
        projSetParseStatus(projParseSummary(_parsedData), 'ok');

        // Post-parse tab routing (applies to BOTH Go and C99 — projParse is
        // the shared button handler):
        //   - parse/analyze errors above → stayed on Editor (early returns);
        //   - fully correct (no incomplete ⚠ items) → jump to Preview so the
        //     specialist sees the finished block;
        //   - still incomplete → go to the Wizard, where the ⚠ cards guide
        //     the fixes.
        // projRenderPreview(true) renders the preview AND switches to it;
        // we render it either way so its content is fresh when reached.
        if (incompleteCount === 0) {
            projRenderPreview(true);
        } else {
            projRenderPreview(false);
            projSetTab('wizard');
        }

    } catch (e) {
        projSetParseStatus('✗ Network error: ' + e.message, 'error');
    }
}

function projSetParseStatus(msg, type) {
    const el = document.getElementById('proj-parse-status');
    if (!el) return;
    _parseStatusType = type;

    if (type === 'error') {
        // [FLOATING STANDARD] The status strip lives at the BOTTOM of the
        // editor layout — below the fold on tall screens, so errors were
        // invisible (field report 2026-07-12: the binary-asset save
        // refusal went unseen). Every error now ALSO fires the floating
        // page alert, the same pattern every other surface uses; the
        // strip stays because it carries the Monaco jump-to-line for
        // parse errors.
        // Português: A faixa de status vive no RODAPÉ do editor — abaixo
        // da dobra em telas altas, erros ficavam invisíveis (report
        // 2026-07-12: a recusa do asset binário digitado passou
        // despercebida). Todo erro agora TAMBÉM dispara o alerta
        // flutuante, o padrão das outras superfícies; a faixa fica
        // porque carrega o pulo-para-linha do Monaco.
        showPageAlert(msg.replace(/^✗\s*/, ''), 'danger');
        el.style.cssText = 'padding:10px 20px;font-size:13px;min-height:34px;' +
            'background:#FEE2E2;border-top:2px solid var(--danger);color:#991B1B;border-bottom:1px solid var(--border)';

        const match = msg.match(/bb\.go:(\d+):(\d+)/);
        if (match && _monacoInst) {
            const line = parseInt(match[1]);
            const col  = parseInt(match[2]);
            _monacoInst.deltaDecorations(_monacoInst._projErrDec || [], []);
            _monacoInst._projErrDec = _monacoInst.deltaDecorations([], [{
                range: new monaco.Range(line, 1, line, 9999),
                options: {
                    isWholeLine: true,
                    className: 'monaco-proj-error-line',
                    glyphMarginClassName: 'monaco-proj-error-glyph',
                    overviewRuler: { color: '#EF4444', position: 1 },
                    minimap: { color: '#EF4444', position: 1 },
                },
            }]);
            _monacoInst.revealLineInCenter(line);
            _monacoInst.setPosition({ lineNumber: line, column: col });
        }
        el.innerHTML = `<i class="fa-solid fa-circle-xmark" style="margin-right:6px"></i>${
            msg.replace(/(bb\.go:\d+:\d+:)/, '<strong style="font-family:var(--mono)">$1</strong>')}`;
    } else {
        el.style.cssText = 'padding:8px 20px;font-size:13px;min-height:34px;border-bottom:1px solid var(--border)';
        const color = type === 'ok' ? 'var(--success)' : 'var(--warning)';
        el.innerHTML = msg ? `<span style="color:${color}">${msg}</span>` : '';
        if (_monacoInst?._projErrDec) {
            _monacoInst.deltaDecorations(_monacoInst._projErrDec, []);
            _monacoInst._projErrDec = [];
        }
    }
}

function projScheduleAnalyze() {
    if (!_realtimeAnalysis) return;
    clearTimeout(_analyzeTimer);
    _analyzeTimer = setTimeout(projRunAnalyze, 600);
}

async function projRunAnalyze() {
    const seq  = ++_analyzeSeq;
    // The WHOLE working copy goes to the analyzer (GoMF): go/types is a
    // package-level checker — one tab of a multi-file Go project alone
    // would light "undefined: <Struct>" on legitimate code. "Empty" is
    // a SET question for the same reason Save's is.
    //
    // Português: A cópia INTEIRA vai ao analisador — go/types é
    // verificador de PACOTE; uma aba sozinha acenderia "undefined" em
    // código legítimo. "Vazio" é pergunta de conjunto.
    const wcFiles = _tabsCollectFiles();
    if (!wcFiles.some(f => f.content.trim()) || !_monacoInst) return null;

    // Live analysis runs `go/parser` + `go/types` on the source — it
    // is intrinsically Go-only. For any other language (C99 today,
    // Arduino later) the analyzer would choke on the first non-Go
    // token (e.g. `#include` produces "illegal character U+0023 '#'").
    //
    // We early-return when the project isn't Go: clear any stale
    // Monaco markers from a previous Go analyse, reset the parse
    // status, and pretend analyse said "all clear". The Wizard tab
    // continues to use /wizard/parse for parsing the source, which
    // does support C99; what is missing here is only the semantic
    // pass. See docs/CLAUDE_C99_DEVICE_SUPPORT.md §2.10 for the
    // long-term plan to bring analyse to non-Go languages.
    if (_currentProjectLanguage() !== 'go') {
        projApplyMonacoMarkersPerFile([]);
        projSetParseStatus('', '');
        return { hasErrors: false, diagnostics: [] };
    }

    try {
        // api() attaches the Bearer token and parses the envelope. The
        // wizard endpoint requires auth — see slice 0 in
        // docs/tasks/WIZARD_TASKS.md. Cancellation by sequence number
        // remains: we discard the response if a newer request started
        // while we were waiting.
        const json = await api('POST', '/api/v1/blackbox/wizard/analyze', { files: wcFiles });
        if (_analyzeSeq !== seq) return null;
        if (json?.metadata?.status !== 200) return null;

        const result = json.data;
        projApplyMonacoMarkersPerFile(result.files || []);

        if (result.hasErrors) {
            // Flatten the per-file buckets for the status line, each
            // diagnostic carrying its file — line numbers alone are
            // ambiguous across tabs.
            const flat = (result.files || []).flatMap(fd =>
                (fd.diagnostics || []).map(d => ({ ...d, path: fd.path })));
            projSetParseStatus(projFormatAnalyzeErrors(flat), 'error');
        } else if (_parsedData) {
            projSetParseStatus(projParseSummary(_parsedData), 'ok');
            projAutoReparse();
        } else {
            projSetParseStatus('', '');
        }
        return result;
    } catch { return null; }
}

// projApplyMonacoMarkersPerFile lands each file's diagnostics on ITS
// tab's MODEL — markers are model-scoped in Monaco, so an error parked
// on an inactive tab keeps its squiggle (and the Problems tooltip) for
// when the user switches there, and an empty bucket CLEARS a tab's
// stale markers. The whole-line red decorations stay ACTIVE-tab only:
// deltaDecorations is a view concern, and painting the visible editor
// for a hidden file would decorate the wrong text. Passing [] clears
// every tab (the non-Go gate).
//
// Português: Cada bucket cai no MODEL da sua aba — markers são do
// model, então erro em aba inativa mantém o rabisco para quando o
// usuário trocar, e bucket vazio LIMPA markers velhos. As decorações
// de linha ficam só na ativa (são da view). [] limpa todas as abas.
function projApplyMonacoMarkersPerFile(fileBuckets) {
    if (!window.monaco) return;
    const byPath = new Map((fileBuckets || []).map(fd => [fd.path, fd.diagnostics || []]));
    const activePath = _editorTabs[_activeTabIdx]?.path;
    for (const t of _editorTabs) {
        const diags = byPath.get(t.path) || [];
        _setMarkersOnModel(t.model, diags);
        if (t.path === activePath) _applyErrorLineDecorations(diags);
    }
}

// _setMarkersOnModel: one model's markers. The message strip now peels
// ANY leading "path:line:col:" prefix — the multi-file analyzer parses
// under real paths, so the old hardcoded "blackbox.go" prefix is gone.
function _setMarkersOnModel(model, diagnostics) {
    if (!model || !window.monaco) return;
    const sevMap = { error: 8, warning: 4, info: 2, hint: 1 };
    monaco.editor.setModelMarkers(model, 'proj-analyzer', diagnostics.map(d => ({
        severity:        sevMap[d.severity] ?? 8,
        message:         d.message.replace(/^\S+\.go:\d+:\d+:\s*/, '').trim(),
        startLineNumber: d.line, startColumn: d.col,
        endLineNumber:   d.endLine, endColumn: d.endCol,
        source:          d.source,
    })));
}

// _applyErrorLineDecorations: the ACTIVE editor-view's whole-line red
// paint (view concern — see projApplyMonacoMarkersPerFile).
function _applyErrorLineDecorations(diagnostics) {
    if (!_monacoInst || !window.monaco) return;
    const errDec = diagnostics.filter(d => d.severity === 'error').map(d => ({
        range: new monaco.Range(d.line, 1, d.endLine, 9999),
        options: {
            isWholeLine: true,
            className: 'monaco-proj-error-line',
            glyphMarginClassName: 'monaco-proj-error-glyph',
            overviewRuler: { color: '#EF4444', position: 1 },
            minimap: { color: '#EF4444', position: 1 },
        },
    }));
    const combined = _monacoInst.deltaDecorations(
        [...(_monacoInst._projErrDec || []), ...(_monacoInst._projAnaDec || [])],
        errDec,
    );
    _monacoInst._projErrDec = [];
    _monacoInst._projAnaDec = combined;
}

function projFormatAnalyzeErrors(diagnostics) {
    const errors = diagnostics.filter(d => d.severity === 'error');
    if (!errors.length) return '✗ Semantic analysis error';
    const shown = errors.slice(0, 2).map(d =>
        `<span style="font-family:var(--mono);font-weight:700">${
            d.path ? esc(d.path) + ':' : 'line&nbsp;'}${d.line}:</span> ${
            esc(d.message.replace(/^\S+\.go:\d+:\d+:\s*/, '').trim())}`
    ).join(' &nbsp;·&nbsp; ');
    const more = errors.length > 2
        ? ` <span style="opacity:.65">(+${errors.length - 2} more)</span>` : '';
    return `✗ ${errors.length} error(s) — ${shown}${more}`;
}

async function projAutoReparse() {
    // Bail when there is no parsed data on hand. With the edit
    // handler now clearing _parsedData on every keystroke, this
    // effectively means the auto-reparse only fires when the user
    // is iterating ON TOP OF an already-parsed session — e.g. they
    // clicked Parse, looked at the preview, and made small edits.
    // For a fresh edit session (post-clear), the user must click
    // Parse explicitly to populate the preview. This matches the
    // "preview clears on edit, fills on parse" requirement.
    if (!_parsedData) return;
    const raw = projGetCode();
    if (!raw.trim()) return;
    try {
        // Same wizard endpoint as projParse — projAutoReparse runs after a
        // successful Live-analysis pass to refresh the parsed model
        // silently (no UI status update). See slice 0 of the wizard plan
        // in docs/tasks/WIZARD_TASKS.md.
        // Full working copy — this call had kept the pre-multi-file
        // {code} shape and 400'd in silence (third such caller caught;
        // see projSilentReparse). Header-owned types need every tab.
        const json = await api('POST', '/api/v1/blackbox/wizard/parse',
            { files: _tabsCollectFiles(), language: _currentProjectLanguage() });
        if (json?.metadata?.status !== 200) return;
        const prev = _parsedData.sourceCode;
        _parsedData = json.data.parsed;
        _parsedData.sourceCode = prev;
        // The auto-reparse just refreshed the parsed model from `raw`.
        // Update _parsedSnapshotFP to match so a subsequent Save
        // can flag this version as parse-ok without forcing the user
        // to click Parse manually.
        _parsedSnapshotFP = _snapshotFingerprint();
        _needsExplicitParse = false;
        projRenderPreview();

        // Bridge to the wizard module so:
        //   1. Toolbar Parse button's pending-items count stays
        //      synced (the wizard module updates it via
        //      projUpdateParseBtnState regardless of whether the
        //      Wizard tab has been opened in this session).
        //   2. If the wizard tab IS open, its cards refresh too.
        // Live analysis silently re-parses while the user types,
        // so this keeps both surfaces current.
        projWizardOnEditorParseSuccess(raw, json.data);
    } catch { /* silent */ }
}

// projSilentReparse runs /wizard/parse on the editor's current
// source so the toolbar Parse button can leave its 'pending' state
// and reflect the real wizard incomplete-items count. Called from
// openCodeEditor on every project open.
//
// The populatePreview flag controls whether the Preview tab also
// gets filled — true only when the saved version was flagged
// lastParseOk AND the editor matches the saved source exactly.
// When false, _parsedData stays null and the Preview tab stays
// at its empty placeholder; only the button state is resolved.
//
// Differs from projAutoReparse in two ways:
//   1. It's the FIRST parse of the session — _parsedData is null
//      coming in, so we can't use the auto-reparse "preserve
//      sourceCode" trick.
//   2. It does not run analyze — the goal is purely to verify
//      the wizard state; live analysis runs separately on its own
//      schedule.
//
// Failure is silent. On failure, the toolbar Parse button stays
// in its 'pending' state (disabled). The user resolves it by
// opening the Wizard tab, which runs its own parse and sets the
// button via _setIncomplete.
async function projSilentReparse(populatePreview) {
    // The FULL working copy goes to the parser — header-owned types
    // (wire types, enums, callbacks) only resolve when every tab rides
    // along; the active tab alone would show cards with orphan types.
    // This call site had silently kept the pre-multi-file {code} shape
    // and 400'd on open — caught while wiring the tabs (6c-2).
    //
    // Português: A cópia de trabalho INTEIRA vai ao parser — tipos do
    // header só resolvem com todas as abas juntas. Este chamador tinha
    // ficado no shape antigo {code} e dava 400 em silêncio no open —
    // pego ao ligar as abas (6c-2).
    const wcFiles = _tabsCollectFiles();
    if (!wcFiles.some(f => f.content.trim())) return;
    const raw = _editorTabs[_activeTabIdx]?.model.getValue() || '';
    try {
        const json = await api('POST', '/api/v1/blackbox/wizard/parse',
            { files: wcFiles, language: _currentProjectLanguage() });
        if (json?.metadata?.status !== 200) return;

        // Wizard bridge ALWAYS fires — the toolbar Parse button
        // depends on the incomplete[] count regardless of whether
        // we populate the Preview tab. This is the call that
        // unblocks the 'pending' state.
        projWizardOnEditorParseSuccess(raw, json.data);

        if (!populatePreview) {
            // Button is resolved; preview stays empty. The user
            // saved an unparsed-OK version (or recovered from a
            // backup), so we deliberately do not present a parsed
            // device drawing as authoritative — they must click
            // Parse explicitly to opt in.
            return;
        }

        _parsedData = json.data.parsed;
        _parsedData.sourceCode = raw;
        _parsedSnapshotFP = _snapshotFingerprint();
        _needsExplicitParse = false;
        projRenderPreview();
    } catch { /* silent */ }
}

// ─── Preview (mirrors blackbox.js buildDevice / buildPin) ─────────────────────

function projRenderPreview(switchTab = false) {
    const el = document.getElementById('proj-tab-preview');
    if (!el || !_parsedData) return;
    const hint = document.getElementById('proj-preview-hint');
    if (hint) hint.textContent = 'Hover pins for details';
    el.innerHTML = projBuildDevice(_parsedData);
    if (switchTab) projSetTab('preview');
    const dbg = document.getElementById('proj-tab-debug');
    if (dbg) dbg.innerHTML = `<pre style="margin:0;white-space:pre-wrap;word-break:break-all">${
        esc(JSON.stringify(_parsedData, null, 2))}</pre>`;
    updateVersionBar();
}

export function projSetTab(tab) {
    // Capture working state into the backup slot before navigating away
    // from the editor surface — but only if the user has actually
    // touched the source in this session. Tab-clicking through a
    // freshly-opened project shouldn't churn the backup row and bump
    // its timestamp for no reason. The _backupDirty flag tracks this.
    //
    // Once dirty stays dirty until the next projSave success: even if
    // we already wrote the backup once during this session, the user
    // may have typed more characters that the Monaco debounce hasn't
    // flushed yet. Tab switch is the user's signal that they're done
    // with the current surface, so capturing whatever the latest
    // source is now is exactly right.
    //
    // Fire-and-forget: we don't `await` because the tab transition
    // shouldn't block on the network. Failures retry on the next
    // trigger.
    if (_editProject && _backupDirty) {
        if (_backupDebounce) {
            // A pending Monaco debounce would race with this save —
            // cancel it and let this call carry the latest content.
            clearTimeout(_backupDebounce);
            _backupDebounce = null;
        }
        projSaveBackup();
    }

    // The Wizard module reads the source via `window._projMonacoInst`
    // (set by _doMount) so it does not need a circular import. Keep
    // the assignment here too — switching tabs is the only event that
    // can lead to projWizardOpen running, so a missed assignment in
    // _doMount would surface with the first wizard click.
    if (_monacoInst && !window._projMonacoInst) {
        window._projMonacoInst = _monacoInst;
    }

    ['editor', 'preview', 'debug', 'wizard'].forEach(t => {
        document.getElementById(`proj-tab-btn-${t}`)?.classList.toggle('active', t === tab);
    });
    const editorWrap = document.getElementById('proj-editor-wrap');
    const previewDiv = document.getElementById('proj-tab-preview');
    const debugDiv   = document.getElementById('proj-tab-debug');
    const wizardDiv  = document.getElementById('proj-tab-wizard');
    if (editorWrap) editorWrap.style.display = tab === 'editor'  ? '' : 'none';
    if (previewDiv) previewDiv.style.display  = tab === 'preview' ? '' : 'none';
    if (debugDiv)   debugDiv.style.display    = tab === 'debug'   ? '' : 'none';
    if (wizardDiv)  wizardDiv.style.display   = tab === 'wizard'  ? '' : 'none';

    // Move Monaco's host div between the Editor wrap and the Wizard's
    // left panel. The same instance is reused — we never mount a
    // second editor. `appendChild` on a node already in the DOM moves
    // it; Monaco's `automaticLayout: true` re-layouts on the new
    // container size automatically.
    //
    // Read-only state follows the tab: writable on Editor, read-only
    // on Wizard (unless the user has explicitly clicked "Edit source"
    // in the wizard, which the wizard module manages on its own via
    // _state.isEditingSource). The `setTimeout` defers the move past
    // the display-toggle above so layout calculations land on the
    // visible container.
    if (_monacoHostDiv && _monacoInst) {
        if (tab === 'editor') {
            // Move back into Editor wrap and unlock.
            if (editorWrap && _monacoHostDiv.parentNode !== editorWrap) {
                editorWrap.appendChild(_monacoHostDiv);
            }
            _monacoInst.updateOptions({ readOnly: false });
            setTimeout(() => _monacoInst.layout(), 0);
        } else if (tab === 'wizard') {
            // Move into the Wizard's left panel host. The wizard
            // module renders an empty <div id="proj-wizard-monaco-host">
            // when it draws the tab; we wait one tick so that DOM
            // exists before reparenting (projWizardOpen is async).
            const moveIn = () => {
                const host = document.getElementById('proj-wizard-monaco-host');
                if (host && _monacoHostDiv.parentNode !== host) {
                    host.appendChild(_monacoHostDiv);
                    _monacoInst.layout();
                }
            };
            // First attempt synchronous (host may already be there
            // from a previous open); if not, retry after the wizard
            // module's render tick lands.
            moveIn();
            setTimeout(moveIn, 0);
            setTimeout(moveIn, 80);
            // Default to read-only on the Wizard. The wizard module
            // flips this when the user toggles "Edit source".
            _monacoInst.updateOptions({ readOnly: true });
        }
        // For preview/debug we do not move Monaco — those tabs do not
        // show source; leaving the editor wherever it was last is fine
        // because `editorWrap.style.display = 'none'` hides it anyway.
    }

    // First click on the Wizard tab triggers the draft load. The
    // wizard module is internally idempotent — if already opened for
    // the same project, the call is a no-op refresh.
    if (tab === 'wizard') {
        _injectWizardStyles();
        const projectId = _currentProjectId();
        if (projectId) {
            projWizardOpen(projectId);
        }
    }
}

// _injectWizardStyles loads projects_wizard.css on demand the first
// time the user clicks the Wizard tab. We delay the load until then
// so the editor view is not penalised by a CSS round-trip nobody
// asked for.
function _injectWizardStyles() {
    if (document.getElementById('proj-wizard-styles-link')) return;
    const link = document.createElement('link');
    link.id = 'proj-wizard-styles-link';
    link.rel = 'stylesheet';
    link.href = '/static/css/projects_wizard.css';
    document.head.appendChild(link);
}

// _injectHelpFilesStyles loads help_files.css on demand the first
// time the user clicks the Files toolbar button. Same lazy-load
// rationale as _injectWizardStyles.
function _injectHelpFilesStyles() {
    if (document.getElementById('proj-helpfiles-styles-link')) return;
    const link = document.createElement('link');
    link.id = 'proj-helpfiles-styles-link';
    link.rel = 'stylesheet';
    link.href = '/static/css/help_files.css';
    document.head.appendChild(link);
}

// projOpenFileManager opens the help-files modal for the currently-
// open project. Wired to the toolbar Files button — see the Files
// <button> in the editor toolbar HTML above.
//
// Exported and registered on window so the inline onclick attribute
// in the toolbar can reach it. The function delegates the heavy
// lifting to window.openHelpFiles, which lives in the dedicated
// help_files.js module so the modal's lifecycle is co-located with
// its rendering.
export function projOpenFileManager() {
    const projectId = _currentProjectId();
    if (!projectId) {
        // Should never happen — the toolbar is only visible inside a
        // project. Defensive in case the user manually navigated.
        return;
    }
    _injectHelpFilesStyles();
    if (typeof window.openHelpFiles === 'function') {
        // Pass _parsedData so the manager's "Create new file" modal
        // can offer the device's method names in its dropdown. When
        // there's no parse yet, the manager shows a "Please parse
        // first" alert and closes itself when the user clicks OK.
        window.openHelpFiles(projectId, { parsed: _parsedData });
    } else {
        // Module not loaded — surface a useful error rather than a
        // silent no-op.
        console.error('openHelpFiles is not registered on window');
    }
}

// projOpenExportFlow opens the "Github package" export flow for the
// currently-open project. Wired to the toolbar Github-package button
// (see the editor toolbar HTML above).
//
// The toolbar onclick fires this with no arguments. We resolve the
// project id and the dirty flag locally — projects.js owns both —
// then delegate the modals + API calls + download to the dedicated
// project_export.js module via window.projExportRun. Same boundary
// pattern as the Files button → openHelpFiles.
//
// We deliberately read _isProjectDirty() HERE (not inside the export
// module) because the dirty flag is tracked in this file (Monaco
// edit handler + backup state). Keeping the read here means the
// export module never has to import projects.js — the dependency
// stays one-way.
export function projOpenExportFlow() {
    const projectId = _currentProjectId();
    if (!projectId) {
        // Toolbar is only visible inside a project; defensive in
        // case the user reached this via some other code path.
        return;
    }
    if (typeof window.projExportRun === 'function') {
        window.projExportRun(projectId, { isDirty: _isProjectDirty() });
    } else {
        console.error('projExportRun is not registered on window');
    }
}

// _currentProjectId reads the project id off the editor view's data
// attribute. The id is the slug used by every other Projects-page
// network call. Returns the empty string when no editor is open.
function _currentProjectId() {
    const editorView = document.getElementById('proj-editor-view');
    return editorView?.dataset?.projectId || '';
}

// projParseSummary builds the green status line. It counts every kind
// of device the parser surfaces, not just struct methods: a block in
// the Preview is a "device" whether it came from a struct method, the
// Init slot, or a standalone C99 function. Enums are wire-types and
// counted separately as "type(s)". Without this, a device-per-function
// source (no methods) read "0 method(s)" even when fully populated.
function projParseSummary(pd) {
    if (!pd) return '✓ OK';
    const nMethods = pd.methods?.length || 0;
    const nEnums   = pd.enums?.length || 0;
    const nProps   = pd.props?.length || 0;

    // Functions contribute their callback-duality variants (see
    // _fnPreviewVariants): a callable card carries the function's own
    // inputs/outputs; a synthetic reference card (the ƒ) carries one
    // `callback` output. Count devices and pins from the variants so the
    // summary matches the cards the Preview actually renders.
    let nFnDevices = 0, nFnPins = 0;
    (pd.functions || []).forEach(fn => {
        _fnPreviewVariants(fn).forEach(v => {
            nFnDevices++;
            nFnPins += (v === 'ref')
                ? 1
                : (fn.inputs?.length || 0) + (fn.outputs?.length || 0);
        });
    });
    const nDevices = nMethods + nFnDevices + (pd.init ? 1 : 0);

    const pinsOf = list => (list || []).reduce(
        (a, m) => a + (m.inputs?.length || 0) + (m.outputs?.length || 0), 0);
    const nPins = pinsOf(pd.methods) + nFnPins + (pd.init ? pinsOf([pd.init]) : 0);

    let s = `✓ OK — ${nDevices} device(s) · ${nPins} pin(s) · ${nProps} prop(s)`;
    if (nEnums) s += ` · ${nEnums} type(s)`;
    return s;
}

function projBuildDevice(bb) {
    // Field naming note: this function consumes the live BlackBoxDef shape
    // produced by `server/codegen/blackbox/types.go`. The struct name lives
    // in `bb.name` (the legacy parser called it `structName`); the icon and
    // label remain `structIcon` and `structLabel`. The legacy parser also
    // exposed a top-level `parseWarnings` array — the codegen parser does
    // not, so we no longer render that warning block here. When per-port
    // "missing connection" warnings come back as part of slice 2 (the
    // completion set), this is the function that will render them.
    const label = bb.structLabel || bb.displayName || bb.name || 'Device';
    const icon  = bb.structIcon  || 'cube';
    const cat   = bb.category    || '';

    // Init is stored as a separate field in BlackBoxDef (not part of methods[]).
    // It must be rendered first, exactly as _tplBuildDevice does.
    const allMethods = [];
    if (bb.init) {
        allMethods.push({ name: 'Init', ...bb.init });
    }
    (bb.methods || []).forEach(m => allMethods.push(m));

    const cards = [];

    // (1) Struct-method model (Go, and C99 functions that have a struct
    //     receiver): one block per method, prefixed with the struct label.
    allMethods.forEach((m, mi) => cards.push(projBuildMethodCard(bb, m, mi, label, icon, cat)));

    // (2) C99 device-per-function model (Slice 8): every standalone public
    //     function is its OWN device. parsed.functions[] is a NamedFuncDef
    //     list — render one block each, ports straight from inputs/outputs.
    //     These blocks are read-only here; port editing lives in the Wizard.
    //     A function marked as a callback handler yields the DUALITY (see
    //     _fnPreviewVariants): mode "both" → the callable card AND a separate
    //     reference (ƒ) card; mode "ref" → only the ƒ card. A plain function
    //     yields just its callable card. This mirrors what the menu offers, so
    //     the Preview shows the maker exactly the blocks they can place.
    (bb.functions || []).forEach(fn => {
        _fnPreviewVariants(fn).forEach(variant => {
            cards.push(variant === 'ref'
                ? projBuildCallbackRefBlock(fn)
                : projBuildFunctionBlock(fn));
        });
    });

    // (3) C99 enum wire-types (Slice 6): not executable devices (no
    //     ports), they describe the type carried on a wire. We surface
    //     them as compact read-only blocks listing the enumerator labels
    //     so the Preview matches what the Wizard shows.
    (bb.enums || []).forEach(ed => cards.push(projBuildEnumBlock(ed)));

    return `<div class="proj-blocks-container">${cards.join('')}</div>
<div class="proj-legend">
  <span class="proj-legend-item"><span style="color:var(--primary);font-size:16px">◎</span> optional</span>
  <span class="proj-legend-item"><span style="color:var(--success);font-size:16px">◉</span> mandatory</span>
</div>`;
}

// projBuildFunctionBlock renders one C99 device-per-function block:
// header (function icon + label) plus its ports. Read-only — unlike
// method cards there is no inline pin rename / drag here; C99 port
// configuration happens in the Wizard's Port modals.
function projBuildFunctionBlock(fn) {
    const label = fn.label || fn.name;
    const iconHtml = projRenderFAIcon(fn.icon || 'bolt');
    const hdr = `<div class="proj-block-hdr">
  <div class="proj-block-hdr-icon">${iconHtml}</div>
  <div class="proj-block-hdr-label">${esc(label)}</div>
</div>`;
    let pins = '';
    (fn.inputs  || []).forEach(p => { pins += projBuildStaticPin(p, 'input');  });
    (fn.outputs || []).forEach(p => { pins += projBuildStaticPin(p, 'output'); });
    return `<div class="proj-block">${hdr}${pins}</div>`;
}

// _fnPreviewVariants decides which device cards a parsed function produces in
// the Preview, mirroring what the menu offers (the callback duality):
//   - not a handler        → ['callable']        the function itself
//   - handler, mode "both" → ['callable','ref']  the callable + the ƒ
//   - handler, mode "ref"  → ['ref']             only the ƒ reference
// A handler is a function with handlerType set; callbackMode defaults to
// "both" when absent (a bare `callback:T.` directive). See
// docs/CODEGEN_C99_CALLBACKS.md.
function _fnPreviewVariants(fn) {
    if (!fn || !fn.handlerType) return ['callable'];
    return fn.callbackMode === 'ref' ? ['ref'] : ['callable', 'ref'];
}

// projBuildCallbackRefBlock renders the SYNTHETIC callback-reference card (the
// wire-ƒ) for a function marked as a callback handler. Under the duality the
// reference is a SEPARATE device from the callable: it has no inputs and a
// single `callback` output typed as the handler's function-pointer type, which
// a maker wires by address into a consumer's callback input (e.g.
// setDisplay.writer). This mirrors the device the factory synthesizes
// (CreateBlackBoxCallbackRef) so the Preview matches the stage. Read-only,
// like the other C99 preview blocks.
function projBuildCallbackRefBlock(fn) {
    const label = `${fn.label || fn.name} ƒ`;
    const iconHtml = projRenderFAIcon(fn.icon || 'bolt');
    const tag = `<span style="font-size:11px;font-weight:600;background:rgba(255,255,255,.2);
        padding:2px 8px;border-radius:99px;margin-left:6px">ref</span>`;
    const hdr = `<div class="proj-block-hdr">
  <div class="proj-block-hdr-icon">${iconHtml}</div>
  <div class="proj-block-hdr-label">${esc(label)}${tag}</div>
</div>`;
    // One synthetic output: the function reference itself, typed as the
    // handler's function-pointer type. No inputs — a reference produces a
    // handle, it does not consume parameters.
    const refPin = projBuildStaticPin({
        name: 'callback',
        label: 'callback',
        goType: fn.handlerType,
        callbackType: fn.handlerType,
        connection: 'optional',
    }, 'output');
    return `<div class="proj-block">${hdr}${refPin}</div>`;
}

// projBuildEnumBlock renders a C99 enum wire-type as a read-only block:
// header (enum icon + label + a "type" tag) plus one row per enumerator
// (human label · numeric value). Enums carry no connection pins, so the
// rows have no dots.
function projBuildEnumBlock(ed) {
    const label = ed.label || ed.name;
    const iconHtml = projRenderFAIcon(ed.icon || 'list-ol');
    const tag = `<span style="font-size:11px;font-weight:600;background:rgba(255,255,255,.2);
        padding:2px 8px;border-radius:99px;margin-left:6px">type</span>`;
    const hdr = `<div class="proj-block-hdr">
  <div class="proj-block-hdr-icon">${iconHtml}</div>
  <div class="proj-block-hdr-label">${esc(label)}${tag}</div>
</div>`;
    const rows = (ed.values || []).map(v => {
        const shown = (v.valueIsRaw ? (v.rawValue || '?') : String(v.value));
        return `<div class="proj-pin out">
  <div class="proj-badges"></div>
  <span class="proj-pin-type">${esc(shown)}</span>
  <span class="proj-pin-name">${esc(v.label || v.name)}</span>
</div>`;
    }).join('');
    return `<div class="proj-block">${hdr}${rows}</div>`;
}

// projBuildStaticPin mirrors projBuildPin's visual structure (connection
// dot · name · type · badges, with a hover tooltip) but drops the
// inline-rename and drag affordances. Used by the read-only
// device-per-function and enum preview blocks.
function projBuildStaticPin(pin, dir) {
    const isInput = dir === 'input';
    const isMissingConn = isInput && pin.missingConn;
    const sym    = isMissingConn ? '⊙' : pin.connection === 'mandatory' ? '◉' : '◎';
    const dotSty = isMissingConn ? 'color:#C0392B' : '';
    const dot = `<span class="proj-dot ${isInput ? 'in' : 'out'}" style="${dotSty}">${sym}</span>`;
    // Ports from the codegen parser carry the type in `goType`; the
    // legacy shape used `type`. Support both.
    const type = pin.goType || pin.type || '';
    const nameEl = `<span class="proj-pin-name">${esc(pin.label || pin.name)}</span>`;
    const typeEl = `<span class="proj-pin-type">${esc(type)}</span>`;
    const badges = projBuildBadges(pin, -1, -1, dir);
    const tooltip = projBuildTooltip(pin, dir);
    const badgesArea = `<div class="proj-badges">${badges}</div>`;
    const inner = isInput
        ? `${dot}${nameEl}${typeEl}${badgesArea}`
        : `${badgesArea}${typeEl}${nameEl}${dot}`;
    return `<div class="proj-pin ${dir}">${tooltip}${inner}</div>`;
}

function projBuildMethodCard(bb, method, mi, structLabel, structIcon, cat) {
    const label = method.label || method.name;
    const catBadge = mi === 0 && cat
        ? `<span style="font-size:11px;font-weight:600;background:rgba(255,255,255,.2);
             padding:3px 10px;border-radius:99px;margin-top:2px">${esc(cat)}</span>` : '';
    const iconHtml = projRenderFAIcon(method.icon || structIcon);
    const hdr = `<div class="proj-block-hdr">
  <div class="proj-block-hdr-icon">${iconHtml}</div>
  <div class="proj-block-hdr-label">${esc(structLabel)} ${esc(label)}${catBadge}</div>
</div>`;
    let pins = '';
    (method.inputs  || []).forEach((p, pi) => { pins += projBuildPin(p, mi, pi, 'input');  });
    (method.outputs || []).forEach((p, pi) => { pins += projBuildPin(p, mi, pi, 'output'); });
    return `<div class="proj-block">${hdr}${pins}</div>`;
}

function projRenderFAIcon(iconValue) {
    if (!iconValue) return `<i class="fa-solid fa-cube"></i>`;
    let candidate = iconValue.replace(/^\\u/i, '').replace(/^0x/i, '');
    let style = '';
    const ci = candidate.lastIndexOf(':');
    if (ci > 0) {
        const suf = candidate.slice(ci + 1).toLowerCase();
        candidate  = candidate.slice(0, ci);
        style = suf === 'b' || suf === 'brands' ? 'b' : suf === 'r' ? 'r' : 's';
    }
    if (/^[0-9a-fA-F]{4,8}$/.test(candidate)) {
        const cls = style === 'b'
            ? 'bb-icon-unicode bb-icon-brands'
            : 'bb-icon-unicode';
        return `<span class="${cls}">&#x${candidate.toLowerCase()};</span>`;
    }
    // When the icon: tag has an explicit style suffix (`:s`, `:r`,
    // `:b`) we honour it — the specialist already declared their
    // intent. Otherwise we resolve via FA_FREE_STYLES, the per-icon
    // map of free font families. Without this, icons whose Free
    // version exists only in `regular` (e.g. `aries`, `aquarius`,
    // `alarm-clock`) get rendered as `fa-solid` and produce the
    // missing-glyph tofu (□) in the browser.
    //
    // Resolution order matches the wizard's _faIconClass:
    //   brands → solid → regular.
    //
    // FA_BRANDS_SET fallback is for older clients where
    // fa-free-styles.js hasn't been generated yet — preserves the
    // previous behaviour without requiring the new file.
    let fc;
    if (style === 'b')      fc = 'fa-brands';
    else if (style === 'r') fc = 'fa-regular';
    else if (style === 's') fc = 'fa-solid';
    else {
        const styles = window.FA_FREE_STYLES?.[candidate];
        if (styles) {
            if (styles.includes('brands'))       fc = 'fa-brands';
            else if (styles.includes('solid'))   fc = 'fa-solid';
            else if (styles.includes('regular')) fc = 'fa-regular';
            else                                  fc = 'fa-solid'; // shouldn't happen for free icons
        } else {
            fc = window.FA_BRANDS_SET?.has(candidate) ? 'fa-brands' : 'fa-solid';
        }
    }
    return `<i class="${fc} fa-${esc(candidate)}"></i>`;
}

function projBuildPin(pin, mi, pi, dir) {
    const isInput = dir === 'input';
    // Slice-7 rule: outputs are always optional connection-wise, so
    // the "missingConn" indicator (⊙ red dot) only applies to inputs.
    // For outputs we render the optional ◎ symbol regardless of
    // pin.missingConn (the server still sets it on outputs that
    // never had a connection: directive, but it's no longer a
    // problem to surface).
    const isMissingConn = isInput && pin.missingConn;
    const sym     = isMissingConn ? '⊙' : pin.connection === 'mandatory' ? '◉' : '◎';
    const dotSty  = isMissingConn ? 'color:#C0392B' : '';
    const dot = `<span class="proj-dot ${isInput ? 'in' : 'out'}" style="${dotSty}">${sym}</span>`;
    const badges  = projBuildBadges(pin, mi, pi, dir);
    // The "+ flag" affordance was retired: free-text flag editing on
    // the Preview tab bypassed the IDS-grammar checks performed by
    // the Wizard tab's directive modals. Flag authoring now lives
    // exclusively in the Wizard (Field/Port modals → directives like
    // `connection:mandatory`, `unit:i2c_bus`); the Preview shows
    // flags as read-only badges. The per-flag × button stays — it's
    // a quick-fix affordance the Wizard wouldn't speed up.
    const nameEl  = `<span class="proj-pin-name" id="pjp-${mi}-${dir}-${pi}"
        contenteditable="false" onclick="projPinStartRename(this)"
        onblur="projPinFinishRename(this,${mi},'${dir}',${pi})"
        onkeydown="if(event.key==='Enter'||event.key==='Escape'){this.blur()}"
      >${esc(pin.name)}</span>`;
    const typeEl  = `<span class="proj-pin-type">${esc(pin.type)}</span>`;
    const dragEl  = `<span class="proj-drag" draggable="false">⠿</span>`;
    const tooltip = projBuildTooltip(pin, dir);
    const badgesArea = `<div class="proj-badges">${badges}</div>`;
    const inner = isInput
        ? `${dot}${dragEl}${nameEl}${typeEl}${badgesArea}`
        : `${badgesArea}${typeEl}${nameEl}${dragEl}${dot}`;
    return `<div class="proj-pin ${dir}" id="pjrow-${mi}-${dir}-${pi}"
  data-mi="${mi}" data-dir="${dir}" data-pi="${pi}"
  draggable="true"
  ondragstart="projDragStart(event,${mi},'${dir}',${pi})"
  ondragover="projDragOver(event)"
  ondragleave="projDragLeave(event)"
  ondrop="projDrop(event,${mi},'${dir}',${pi})"
  ondragend="projDragEnd(event)"
>${tooltip}${inner}</div>`;
}

function projBuildBadges(pin, mi, pi, dir) {
    let b = '';
    // Slice-7 rule: outputs never need a connection: tag, so the
    // "missing" badge only renders for inputs.
    const isInput = dir === 'input';
    if (isInput && pin.missingConn) b += `<span class="proj-badge warn">⚠ connection: missing</span>`;
    if (pin.range)       b += `<span class="proj-badge range">${esc(pin.range)}</span>`;
    else {
        if (pin.rangeMin) b += `<span class="proj-badge range">≥${esc(pin.rangeMin)}</span>`;
        if (pin.rangeMax) b += `<span class="proj-badge range">≤${esc(pin.rangeMax)}</span>`;
    }
    if (pin.unit)     b += `<span class="proj-badge unit">${esc(pin.unit)}</span>`;
    if (pin.encoding) b += `<span class="proj-badge encoding">${esc(pin.encoding)}</span>`;
    (pin.flags || []).forEach((f, fi) => {
        b += `<span class="proj-badge flag">${esc(f)}
<button class="proj-flag-del" onclick="event.stopPropagation();projRemoveFlag(${mi},'${dir}',${pi},${fi})">×</button></span>`;
    });
    return b;
}

function projBuildTooltip(pin, dir) {
    const isInput = dir === 'input';
    const lines = [];
    if (pin.doc) lines.push(`<strong>${esc(pin.doc)}</strong>`);
    // Slice-7 rule: outputs never need connection:, so neither the
    // "missing" warning nor the optional/mandatory line applies.
    // Outputs simply don't show connection state in the tooltip.
    if (isInput) {
        if (pin.missingConn) lines.push(`<span style="color:#F5B7B1">⚠ connection: missing</span>`);
        else if (pin.connection === 'mandatory') lines.push('◉ <strong>mandatory</strong> connection');
        else if (pin.connection === 'optional')  lines.push('◎ <strong>optional</strong> connection');
    }
    if (pin.range)    lines.push(`📏 Range: <code>${esc(pin.range)}</code>`);
    if (pin.unit)     lines.push(`📐 Unit: <code>${esc(pin.unit)}</code>`);
    if (pin.encoding) lines.push(`🔣 Encoding: <code>${esc(pin.encoding)}</code>`);
    if (pin.default)  lines.push(`💡 Default: <code>${esc(pin.default)}</code>`);
    if (!lines.length) return '';
    return `<div class="proj-tooltip">${lines.join('<br>')}</div>`;
}

// ─── Pin rename ───────────────────────────────────────────────────────────────

export function projPinStartRename(el) {
    el.contentEditable = 'true';
    el.focus();
    const r = document.createRange();
    r.selectNodeContents(el);
    const s = window.getSelection();
    s.removeAllRanges();
    s.addRange(r);
}

export function projPinFinishRename(el, mi, dir, pi) {
    el.contentEditable = 'false';
    const name = el.textContent.trim() || 'pin';
    el.textContent = name;
    if (!_parsedData) return;
    const method = _parsedData.methods[mi];
    if (!method) return;
    if (dir === 'input')  method.inputs[pi].name  = name;
    if (dir === 'output') method.outputs[pi].name = name;
}

// ─── Flags ────────────────────────────────────────────────────────────────────

let _projDragSrc = null;

export function projRemoveFlag(mi, dir, pi, fi) {
    if (!_parsedData) return;
    const pins = dir === 'input' ? _parsedData.methods[mi]?.inputs : _parsedData.methods[mi]?.outputs;
    if (!pins?.[pi]?.flags) return;
    pins[pi].flags.splice(fi, 1);
    projRenderPreview();
}

// projAddFlag was retired alongside the "+ flag" button on the
// Preview tab. Flag authoring moved to the Wizard tab's Field/Port
// directive modals, which write IDS-grammar tags (connection:,
// unit:, default:, ...) instead of free-text strings. The Preview
// keeps flag badges as read-only display + the × button (which calls
// projRemoveFlag above) so a stale flag can still be removed
// without round-tripping through the Wizard.

// ─── Drag-and-drop ────────────────────────────────────────────────────────────

export function projDragStart(e, mi, dir, pi) {
    _projDragSrc = { mi, dir, pi };
    e.currentTarget.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
}
export function projDragOver(e) {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    e.currentTarget.classList.add('drag-over');
}
export function projDragLeave(e) { e.currentTarget.classList.remove('drag-over'); }
export function projDrop(e, mi, dir, pi) {
    e.preventDefault();
    e.currentTarget.classList.remove('drag-over');
    if (!_projDragSrc || !_parsedData) return;
    if (_projDragSrc.mi !== mi || _projDragSrc.dir !== dir || _projDragSrc.pi === pi) return;
    const method = _parsedData.methods[mi];
    const pins   = dir === 'input' ? method.inputs : method.outputs;
    const item   = pins.splice(_projDragSrc.pi, 1)[0];
    pins.splice(pi, 0, item);
    _projDragSrc = null;
    projRenderPreview();
}
export function projDragEnd(e) {
    e.currentTarget.classList.remove('dragging');
    _projDragSrc = null;
}

// ─── Save / Version ───────────────────────────────────────────────────────────

// projUpdateSaveBtnState repaints the Save button according to the current
// auto-save state. There are three tints, applied via inline style so
// the button's existing class (`btn btn-primary`) doesn't fight us:
//
//   blue    (default)   — clean: no unsaved work
//   red     (danger)    — _backupPending: there is a backup newer than
//                         the latest saved version
//   green   (success)   — _backupGreenUntil > now: just saved (10s window)
//
// Priority: green > red > blue. Green wins because it's the explicit
// "you just succeeded" feedback the user is looking at right now;
// flipping straight to red the moment they type one character would
// undermine that signal.
//
// CSS variables for the danger/success palette already exist in the
// theme (used by alerts and the delete confirmation flow) so we just
// reuse them — no new colour decisions to maintain.
function projUpdateSaveBtnState() {
    const btn = document.getElementById('proj-save-btn');
    if (!btn) return;

    const now = Date.now();
    const inGreen = now < _backupGreenUntil;

    // Reset the inline overrides; falling back to the CSS class's blue
    // is the default state.
    btn.style.background  = '';
    btn.style.borderColor = '';
    btn.style.color       = '';

    if (inGreen) {
        // Soft green tint — confirmation, not alarm.
        btn.style.background  = 'var(--success-bg)';
        btn.style.borderColor = 'var(--success)';
        btn.style.color       = 'var(--success)';
        // Schedule a repaint right after the green window closes so the
        // button transitions cleanly to red-or-blue without waiting for
        // the next user action.
        const remaining = _backupGreenUntil - now;
        setTimeout(projUpdateSaveBtnState, remaining + 50);
    } else if (_backupPending) {
        // Soft red — pending unsaved work. Same visual family as the
        // theme's "danger" alerts but lighter than a destructive action
        // button, because the user isn't doing anything wrong yet.
        btn.style.background  = 'var(--danger-bg)';
        btn.style.borderColor = 'var(--danger)';
        btn.style.color       = 'var(--danger)';
    }
    // else: leave the inline overrides cleared → falls through to the
    // CSS-defined blue primary.
}

// projUpdateParseBtnState toggles the Parse button between enabled and
// disabled based on whether the wizard has pending items.
//
// The wizard tracks "pending items" via the `incomplete[]` array
// returned by /wizard/parse and /wizard/rewrite — a list of dotted
// paths to fields that need user attention (missing connection: tags,
// untagged props, etc.). The same array drives the ⚠ badges on the
// wizard cards. We use the count: > 0 means "block re-parsing".
//
// The wizard tracks "pending items" via the `incomplete[]` array
// returned by /wizard/parse and /wizard/rewrite — a list of dotted
// paths to fields that need user attention (missing connection: tags,
// untagged props, etc.). The same array drives the ⚠ badges on the
// wizard cards. We use the count: > 0 means "block re-parsing".
//
// Why we block: the user has work-in-progress in the wizard that
// hasn't been resolved yet. Re-parsing throws away the parser-derived
// state mid-flight, which can erase a half-finished decision the
// wizard is still rendering. The user should finish (or cancel) the
// pending items first, then parse.
//
// "Block" here means UX-level only: the button gets the disabled
// attribute and a tooltip explaining why. The underlying projParse()
// function is unchanged — calling it from the keyboard or from code
// still works. This is intentional: the goal is to nudge the user,
// not to make the IDE feel locked.
//
// Called from:
//   - openCodeEditor / projCreateFile — pass 'pending' to start the
//     button in the disabled "awaiting verification" state. The
//     transparent silent reparse then resolves it.
//   - projects_wizard.js whenever _state.incomplete is reassigned
//     (via the window.projUpdateParseBtnState bridge)
//
// Argument:
//   count — number of pending items (0 enables, > 0 disables).
//   The string 'pending' is a sentinel meaning "disable until the
//   wizard verification runs"; used on project open so the button
//   starts disabled and is re-enabled by the silent reparse or by
//   the user clicking the wizard tab. Numeric 0 enables; absence of
//   data (undefined / null) is treated as 0 — back-compat with older
//   call sites that meant "no count info".
export function projUpdateParseBtnState(count) {
    const btn       = document.getElementById('proj-parse-btn');
    const exportBtn = document.getElementById('proj-export-btn');
    if (!btn) return;

    // Helper: apply the same disabled/enabled visual treatment to a
    // toolbar button. The Github-package button mirrors the Parse
    // button's gate exactly (you can't export what hasn't parsed) —
    // the only difference is the tooltip text.
    const applyState = (el, blocked, blockedTitle, enabledTitle) => {
        if (!el) return;
        el.disabled            = blocked;
        el.style.opacity       = blocked ? '0.5' : '';
        el.style.cursor        = blocked ? 'not-allowed' : '';
        el.style.pointerEvents = blocked ? 'none' : '';
        el.title               = blocked ? blockedTitle : enabledTitle;
    };

    // 'pending' sentinel: button stays disabled until the wizard
    // verification runs (silent reparse on open, or the user
    // opening the wizard tab). Different tooltip from the
    // "resolve N items" case so the user understands the state is
    // transient, not a hard block.
    if (count === 'pending') {
        applyState(btn, true,
            'Verifying with wizard… open the Wizard tab if this stays disabled',
            'Parse Go code');
        applyState(exportBtn, true,
            'Verifying with wizard… export becomes available once parse succeeds',
            'Export the project as a ZIP ready to publish on GitHub');
        return;
    }

    const n = (typeof count === 'number') ? count : 0;
    const block = n > 0;

    // Parse button: blocked while wizard has unresolved items.
    applyState(btn, block,
        `Resolve ${n} pending wizard item${n === 1 ? '' : 's'} before parsing again`,
        'Parse Go code');

    // Github-package button: blocked under the same conditions —
    // the export contract requires a clean parse, so a pending wizard
    // queue means the export is guaranteed to fail pre-flight.
    // Disabling here gives the user the same "fix this first" signal
    // they get from the Parse button, instead of letting them click,
    // wait for /check to run, and read the same news from a modal.
    applyState(exportBtn, block,
        `Resolve ${n} pending wizard item${n === 1 ? '' : 's'} before exporting`,
        'Export the project as a ZIP ready to publish on GitHub');
}

// projSaveBackup writes the current editor source to the project's
// backup slot on the server. Called from three sites:
//
//   1. Tab switch (projSetTab)        — capture state before the user
//                                         leaves the editing surface
//   2. Wizard edit (after a modal Save in projects_wizard.js)
//   3. Monaco onChange (debounced 2s) — the editor fires on every
//                                         keystroke
//
// Empty source deletes the backup row server-side (the "empty backup
// is no backup" rule lives in store.SaveProjectBackup). When that
// happens we also clear _backupPending — there's literally nothing to
// recover, so the Save button shouldn't pretend otherwise.
//
// Concurrency: _backupSeq + _backupSaving prevent stale responses
// from racing a fast typist. The newest call wins; older in-flight
// responses are discarded.
export async function projSaveBackup() {
    if (!_editProject) return;
    if (_backupSaving) return; // rely on the debounced retrigger

    // Any code path that asks for a backup save did so because there
    // was a real change. Monaco's onChange already sets the flag
    // synchronously (so tab switches that fire BEFORE the debounce
    // also see dirty=true); this line covers wizard edits that come
    // through window.projSaveBackup without touching the flag first.
    // Setting it here twice (Monaco + this) is harmless — it's
    // idempotent.
    _backupDirty = true;

    // The backup protects the WHOLE working copy — with tabs, a
    // single-slot backup would let a crash eat every sibling of the
    // active file. activePath rides along so recovery lands the user
    // on the tab they were in.
    //
    // Português: O backup protege a CÓPIA INTEIRA — com abas, slot
    // único deixaria um crash comer as irmãs da aba ativa. activePath
    // viaja junto para a recuperação devolver o usuário à aba certa.
    const bkFiles    = _tabsCollectFiles();
    const activePath = _editorTabs[_activeTabIdx]?.path || '';
    const mySeq = ++_backupSeq;
    _backupSaving = true;

    try {
        const r = await api('POST',
            `/api/v1/projects/${_editProject.id}/files/code/backup`,
            { files: bkFiles, activePath }
        );
        // Stale response — a newer save already overtook us.
        if (mySeq !== _backupSeq) return;

        if (r?.metadata?.status === 200) {
            // Empty source → backend deleted the row; pending state
            // clears too. Non-empty → backup exists and the user has
            // unsaved work relative to the last version (we err on
            // the side of "pending" until projSave actually runs).
            // All-blank set → the backend deleted the row (the "empty
            // source deletes" rule, generalised to the set).
            _backupPending = bkFiles.some(f => f.content.trim() !== '');
            projUpdateSaveBtnState();
        }
        // Failures intentionally don't change _backupPending: if the
        // backup failed, the user still HAS unsaved work — we just
        // failed to persist it. The Save button keeps its current
        // tint; next trigger retries.
    } finally {
        _backupSaving = false;
    }
}

// projScheduleBackup is the debounced entry point for Monaco's
// onChange. Resets the timer on every keystroke so we only POST after
// the user pauses for ~2s. Tab switch / wizard edit don't go through
// here — they call projSaveBackup directly because they're discrete
// events, not streams.
function projScheduleBackup() {
    if (_backupDebounce) clearTimeout(_backupDebounce);
    _backupDebounce = setTimeout(() => {
        _backupDebounce = null;
        projSaveBackup();
    }, 2000);
}

export async function projSave() {
    if (!_editProject) return;

    // "Empty" is a SET question now: any tab with content makes the
    // snapshot saveable (the active tab may legitimately be a fresh
    // empty .h while core.c carries the work).
    if (!_tabsCollectFiles().some(f => f.content.trim())) {
        projSaveBtnWarn('Nothing to save — all files are empty');
        return;
    }
    if (_needsExplicitParse) {
        projSaveBtnWarn('Parse first — code has changed');
        return;
    }

    const btn = document.getElementById('proj-save-btn');
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }

    // lastParseOk: tell the server whether the source we're saving
    // has been successfully parsed in this session. The server
    // persists the flag alongside the version row; on next open,
    // we'll use it to skip the "click Parse to visualise" prompt
    // and silently re-populate the Preview tab.
    //
    // Fingerprint check: the parse-of-record must cover the exact
    // WORKING COPY being committed. Files the merge appends from the
    // server (tree uploads mid-session) were NOT parsed — the
    // fingerprints then differ and the flag is honestly false.
    //
    // Português: O parse-de-registro tem que cobrir a CÓPIA exata sendo
    // cometida. Arquivos anexados pela fusão (uploads da árvore no meio
    // da sessão) NÃO foram parseados — fingerprints divergem e a flag é
    // honestamente falsa.
    const lastParseOk = !!_parsedData
        && _parsedSnapshotFP === _snapshotFingerprint()
        && !_needsExplicitParse;

    // Save commits the WORKING COPY atomically — every tab, in strip
    // order (tab order IS snapshot order). The server's latest set is
    // still consulted for one reason: files added via the tree's Upload
    // while this editor was open must survive; they are appended after
    // the tabs, minus anything in the deletion ledger — the ledger is
    // what keeps locally-closed and renamed-away tabs from being
    // resurrected by this merge. The fetch failing falls back to the
    // working copy alone: saving the user's buffers beats losing them
    // to a transient error.
    //
    // Português: O Save comete a CÓPIA DE TRABALHO atômica — toda aba,
    // na ordem da faixa (ordem de aba É ordem do snapshot). O conjunto
    // do servidor entra por um motivo: arquivos subidos pela árvore com
    // o editor aberto sobrevivem, anexados depois das abas, menos o que
    // está no livro-razão — é ele que impede a fusão de ressuscitar
    // aba fechada ou renomeada. GET falhando, vai a cópia sozinha.
    const snapshot = _tabsCollectFiles();
    try {
        const latest = await api('GET',
            `/api/v1/projects/${_editProject.id}/files/code`);
        const latestFiles = latest?.data?.files;
        if (Array.isArray(latestFiles)) {
            const known = new Set(snapshot.map(f => f.path.toLowerCase()));
            for (const f of latestFiles) {
                if (known.has(f.path.toLowerCase())) continue;
                if (_tabDeletedPaths.has(f.path)) continue;
                snapshot.push({ path: f.path, content: f.content });
            }
        }
    } catch (e) {
        console.warn('[projects] latest-snapshot fetch failed; saving working copy only:', e);
    }
    const r = await api('POST',
        `/api/v1/projects/${_editProject.id}/files/code/versions`,
        { files: snapshot, lastParseOk }
    );

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Save'; }

    if (r?.metadata?.status === 200) {
        const saved = r.data;
        _currentVersion  = saved.version;
        _nextVersion     = _currentVersion + 1;
        _needsExplicitParse = false;
        // The working copy is now the saved truth: every tab's
        // savedContent baseline moves, the deletion ledger is settled,
        // and the strip repaints its dirty dots off.
        //
        // Português: A cópia de trabalho virou a verdade salva: a base
        // savedContent de cada aba avança, o livro-razão é quitado e a
        // faixa repinta os pontos.
        for (const t of _editorTabs) t.savedContent = t.model.getValue();
        _tabDeletedPaths.clear();
        _tabsSavedOrder = _editorTabs.map(t => t.path);
        _tabsRenderStrip();
        _defaultFilename = _editorTabs[_activeTabIdx]?.path || _defaultFilename;

        const rv = await api('GET', `/api/v1/projects/${_editProject.id}/files/code/versions`);
        if (rv?.metadata?.status === 200) _codeVersions = rv.data || [];

        updateVersionBar();
        projFormAlert('', '');
        const st = document.getElementById('proj-save-status');
        if (st) { st.textContent = `✓ Saved as v${saved.version}`; setTimeout(() => { st.textContent = ''; }, 3000); }

        // Backup is now redundant (the backend deleted its row when the
        // version landed; see handleSaveCodeVersion). Clear pending and
        // enter the 10-second green confirmation window. The button
        // repaints itself when the timer expires — see
        // projUpdateSaveBtnState.
        //
        // Also clear _backupDirty: the save promoted everything; the
        // session is clean. A tab switch right after Save shouldn't
        // re-write the (just-cleared) backup.
        _backupPending    = false;
        _backupDirty      = false;
        _backupGreenUntil = Date.now() + 10000;
        projUpdateSaveBtnState();
    } else {
        projFormAlert('✗ ' + (r?.metadata?.error || 'Save failed'), 'danger', 8000);
    }
}

export function projReset() {
    _parsedData         = null;
    _parsedSnapshotFP = null;
    _needsExplicitParse = false;
    const prev = document.getElementById('proj-tab-preview');
    if (prev) prev.innerHTML = `<div style="display:flex;flex-direction:column;align-items:center;
        justify-content:center;padding:40px;color:var(--text-muted)">
      <i class="fa-solid fa-cube" style="font-size:40px;opacity:.2;display:block;margin-bottom:12px"></i>
      <p>The component will appear here after parsing</p></div>`;
    const dbg = document.getElementById('proj-tab-debug');
    if (dbg) dbg.innerHTML = '';
    projSetParseStatus('', '');
    clearTimeout(_analyzeTimer);
    _analyzeSeq++;
    projApplyMonacoMarkersPerFile([]);
}

function updateVersionBar() {
    const bar = document.getElementById('proj-version-bar');
    if (!bar) return;

    if (!_codeVersions.length) {
        bar.innerHTML = _currentVersion === 0
            ? '<span style="font-size:12px;color:var(--text-muted)">No saved versions</span>'
            : '';
        return;
    }

    const opts = _codeVersions.map(v =>
        `<option value="${v.id}" ${v.version === _currentVersion ? 'selected' : ''}>v${v.version}</option>`
    ).join('');

    bar.innerHTML = `
<select class="proj-ver-select" id="proj-ver-select" onchange="projVersionChange(this.value)"
        title="Version history">
  ${opts}
</select>
<button class="btn btn-ghost btn-sm" onclick="projDiffVersions()" title="Compare versions" style="font-size:11px">
  <i class="fa-solid fa-code-compare"></i> Diff
</button>`;
}

export async function projVersionChange(versionId) {
    const latest = _codeVersions[0];
    if (!latest) return;
    // Dirty in SET terms: any tab differing from its saved baseline OR
    // a pending ledgered deletion — comparing one file against latest
    // would miss edits parked on an inactive tab.
    if (_tabsAnyPending()) {
        // Three-way prompt instead of the old binary native confirm.
        // 'save' persists the current buffer before swapping versions
        // (so nothing is lost). 'discard' replaces the editor with
        // the chosen version, dropping the in-memory edits. 'cancel'
        // restores the dropdown selection.
        const choice = await showUnsavedConfirm(
            t('unsaved.version.body',
              'The editor has unsaved changes. Switching versions will ' +
              'replace the current content.'),
            _unsavedConfirmOpts(t('unsaved.version.title', 'Switch version'))
        );
        if (choice === 'cancel') {
            const sel = document.getElementById('proj-ver-select');
            const cur = _codeVersions.find(v => v.version === _currentVersion);
            if (sel && cur) sel.value = cur.id;
            return;
        }
        if (choice === 'save') {
            await projSave();
            // If save failed (parse missing, etc.), keep the editor
            // as it was and restore the dropdown — same recovery
            // behaviour as the cancel path.
            if (_isProjectDirty()) {
                const sel = document.getElementById('proj-ver-select');
                const cur = _codeVersions.find(v => v.version === _currentVersion);
                if (sel && cur) sel.value = cur.id;
                return;
            }
        }
        // discard or successful save → fall through to load the
        // chosen version into the editor.
    }
    const v = _codeVersions.find(v => v.id === versionId);
    if (!v) return;
    // Restore replaces the WHOLE working copy — a version is a snapshot
    // of the set, so restoring one file of it would be a lie. The active
    // tab is preserved by path when the restored set still has it;
    // otherwise focus falls to the first tab. savedContent baselines to
    // the restored content: the copy is clean until the user types.
    //
    // Português: Restaurar troca a CÓPIA INTEIRA — versão é snapshot do
    // conjunto; restaurar um arquivo só seria mentira. A aba ativa é
    // preservada por caminho quando existe no conjunto restaurado.
    _tabsInit(v.files || [], _editorTabs[_activeTabIdx]?.path);
    _currentVersion = v.version;
    projSetParseStatus('Loaded v' + v.version + ' — click Parse to validate', 'warning');
}

// ─── Diff engine (mirrors blackbox.js exactly) ────────────────────────────────

function projDiffHunks(codeA, codeB) {
    const lA = codeA === '' ? [] : codeA.split('\n');
    const lB = codeB === '' ? [] : codeB.split('\n');
    const n = lA.length, m = lB.length;
    const dp = Array.from({length: n+1}, () => new Array(m+1).fill(0));
    for (let i = n-1; i >= 0; i--)
        for (let j = m-1; j >= 0; j--)
            dp[i][j] = lA[i] === lB[j] ? dp[i+1][j+1]+1 : Math.max(dp[i+1][j], dp[i][j+1]);
    const ops = [];
    let i = 0, j = 0;
    while (i < n || j < m) {
        if (i < n && j < m && lA[i] === lB[j]) { ops.push({type:'equal',a:lA[i],b:lB[j]}); i++; j++; }
        else if (j < m && (i >= n || dp[i][j+1] >= dp[i+1][j])) { ops.push({type:'insert',b:lB[j]}); j++; }
        else { ops.push({type:'delete',a:lA[i]}); i++; }
    }
    const hunks = [];
    let k = 0;
    while (k < ops.length) {
        if (ops[k].type === 'equal') { hunks.push({type:'equal',linesA:[ops[k].a],linesB:[ops[k].b]}); k++; }
        else {
            const h = {type:'change',linesA:[],linesB:[]};
            while (k < ops.length && ops[k].type !== 'equal') {
                if (ops[k].type === 'delete') h.linesA.push(ops[k].a);
                if (ops[k].type === 'insert') h.linesB.push(ops[k].b);
                k++;
            }
            hunks.push(h);
        }
    }
    return hunks;
}

function projRenderDiffTable(hunks, choices) {
    let rows = '', ci = 0;
    hunks.forEach((hunk, hi) => {
        if (hunk.type === 'equal') {
            hunk.linesA.forEach(line => {
                const safe = esc(line) || ' ';
                rows += `<tr class="proj-diff-row-eq">
  <td class="proj-diff-cell-l">${safe}</td>
  <td class="proj-diff-cell-m"></td>
  <td class="proj-diff-cell-r" contenteditable="true">${safe}</td>
</tr>`;
            });
        } else {
            const choice = choices[ci];
            const lA = hunk.linesA, lB = hunk.linesB;
            const picked = choice === 'left' ? lA : lB;
            const nRows  = Math.max(lA.length, lB.length);
            for (let r = 0; r < nRows; r++) {
                const lTxt = r < lA.length ? lA[r] : null;
                const pTxt = r < picked.length ? picked[r] : null;
                const lCell = lTxt !== null
                    ? `<td class="proj-diff-cell-l proj-diff-bg-rem">- ${esc(lTxt)}</td>`
                    : `<td class="proj-diff-cell-l proj-diff-bg-empty"> </td>`;
                const rBg = pTxt !== null
                    ? (choice === 'right' ? 'proj-diff-bg-add' : 'proj-diff-bg-chosen')
                    : 'proj-diff-bg-empty';
                const rCell = `<td class="proj-diff-cell-r ${rBg}" contenteditable="true"
  data-ci="${ci}" data-r="${r}">${pTxt !== null ? esc(pTxt) : ' '}</td>`;
                const mCell = r === 0
                    ? `<td class="proj-diff-cell-m" rowspan="${nRows}">
  <button class="proj-diff-arrow ${choice==='left'?'proj-diff-arrow-active':'proj-diff-arrow-off'}"
          onclick="projDiffChoose(${hi},${ci},'left')" title="Use saved">◀</button>
  <button class="proj-diff-arrow ${choice==='right'?'proj-diff-arrow-active':'proj-diff-arrow-off'}"
          onclick="projDiffChoose(${hi},${ci},'right')" title="Keep editor">▶</button>
</td>` : '';
                rows += `<tr>${lCell}${mCell}${rCell}</tr>`;
            }
            ci++;
        }
    });
    return `<table class="proj-diff-table">
<colgroup><col class="diff-col-l"><col class="diff-col-m"><col class="diff-col-r"></colgroup>
<tbody>${rows}</tbody></table>`;
}

function projApplyChoices(hunks, choices) {
    const lines = []; let ci = 0;
    hunks.forEach(h => {
        if (h.type === 'equal') lines.push(...h.linesA);
        else { lines.push(...(choices[ci] === 'left' ? h.linesA : h.linesB)); ci++; }
    });
    return lines.join('\n');
}

export function projDiffChoose(hi, ci, side) {
    _diffChoices[ci] = side;
    projRefreshDiffTable();
}

function projRefreshDiffTable() {
    const wrap = document.getElementById('proj-diff-table-wrap');
    if (!wrap) return;
    wrap.innerHTML = projRenderDiffTable(_diffHunks, _diffChoices);
    wrap.querySelectorAll('[contenteditable]').forEach(el => {
        el.textContent = el.textContent.replace(/\u00A0/g, ' ');
    });
}

function projCollectDiffResult() {
    const cells = document.querySelectorAll('#proj-diff-table-wrap .proj-diff-cell-r');
    if (!cells.length) return projApplyChoices(_diffHunks, _diffChoices);
    return Array.from(cells).map(c => c.textContent.replace(/\u00A0/g, ' ')).join('\n');
}

let _projDiffSavedCode = '';

export async function projDiffVersions() {
    if (!_editProject || !_codeVersions.length) {
        alert('No saved versions to compare.');
        return;
    }

    // The diff is PER FILE: the selector below picks which path is
    // compared; left = that path's content in the chosen version, right
    // = that path's MODEL in the working copy. A version predating the
    // file shows an empty left side — an all-added diff is the honest
    // rendering of "this file did not exist yet"; silently falling back
    // to another file would be a lie with an explicit selector present.
    //
    // Português: O diff é POR ARQUIVO: o seletor escolhe o caminho;
    // esquerda = conteúdo dele na versão, direita = o MODEL dele na
    // cópia. Versão anterior ao arquivo mostra esquerda vazia —
    // tudo-adicionado é a renderização honesta de "ainda não existia".
    let diffPath = _editorTabs[_activeTabIdx]?.path || '';
    const codeMap = {};
    function rebuildCodeMap() {
        _codeVersions.forEach(v => {
            const m = v.files?.find(f => f.path === diffPath);
            codeMap[v.id] = m?.content || '';
        });
        window._projDiffCodeMap = codeMap;
    }
    function diffRightSource() {
        const t = _editorTabs.find(x => x.path === diffPath);
        return t ? t.model.getValue() : projGetCode();
    }
    rebuildCodeMap();

    const firstId = _codeVersions[0].id;
    const opts    = _codeVersions.map(v =>
        `<option value="${v.id}">v${v.version} — ${
            v.files?.length > 1
                ? `${v.files[0].path} +${v.files.length - 1}`
                : (v.files?.[0]?.path || '')
        }</option>`
    ).join('');

    function initDiff(savedCode) {
        _projDiffSavedCode = savedCode;
        _diffHunks   = projDiffHunks(savedCode, diffRightSource());
        _diffChoices = _diffHunks.filter(h => h.type !== 'equal').map(() => 'right');
    }
    initDiff(codeMap[firstId]);

    document.getElementById('proj-diff-modal')?.remove();
    const modal = document.createElement('div');
    modal.id = 'proj-diff-modal';
    modal.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,.6);' +
        'z-index:1000;display:flex;align-items:center;justify-content:center;padding:12px 8px';

    modal.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);
            box-shadow:0 8px 40px rgba(0,0,0,.35);
            width:calc(100vw - 16px);height:92vh;
            display:flex;flex-direction:column;overflow:hidden">

  <div style="display:flex;align-items:center;gap:10px;padding:10px 16px;
              border-bottom:1px solid var(--border);background:var(--bg-surface);
              flex-wrap:wrap;flex-shrink:0">
    <i class="fa-solid fa-code-compare" style="color:var(--primary)"></i>
    <strong style="font-size:14px">Diff — ${esc(_editProject.name)}</strong>
    <select id="proj-diff-ver-sel" class="proj-ver-select">${opts}</select>
    <select id="proj-diff-file-sel" class="proj-ver-select"
            title="Which file to compare">${
        _editorTabs.map(t =>
            `<option value="${esc(t.path)}" ${t.path === diffPath ? 'selected' : ''}>${esc(t.path)}</option>`
        ).join('')
    }</select>
    <span style="font-size:12px;color:var(--text-muted)">
      ◀ use saved &nbsp;|&nbsp; ▶ keep editor &nbsp;|&nbsp; edit right column directly
    </span>
    <div style="margin-left:auto;display:flex;gap:8px;align-items:center">
      <button class="btn btn-primary btn-sm" onclick="projDiffApplyToEditor()"
              title="Send result to Monaco editor">
        <i class="fa-solid fa-arrow-right-to-bracket"></i> Apply to editor
      </button>
      <button onclick="document.getElementById('proj-diff-modal').remove()"
              style="background:none;border:none;cursor:pointer;font-size:22px;
                     color:var(--text-muted);line-height:1" title="Close">×</button>
    </div>
  </div>

  <div style="display:flex;background:var(--bg-surface);border-bottom:2px solid var(--border);
              font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.05em;
              color:var(--text-muted);flex-shrink:0">
    <div style="flex:1;padding:5px 10px;border-right:1px solid var(--border)">
      <i class="fa-solid fa-floppy-disk" style="margin-right:4px;color:var(--danger)"></i>Saved version
    </div>
    <div style="width:44px;flex-shrink:0;text-align:center;padding:5px 0">◀▶</div>
    <div style="flex:1;padding:5px 10px;border-left:1px solid var(--border)">
      ${_projectLangById(_editProject.id) === 'c'
          ? '<i class="fa-solid fa-code" style="margin-right:4px;color:#5c6bc0"></i>'
          : '<i class="fa-brands fa-golang" style="margin-right:4px;color:#00ADD8"></i>'}Result
      <span style="font-size:10px;font-weight:400;color:var(--success)">editable</span>
    </div>
  </div>

  <div id="proj-diff-table-wrap" style="overflow:auto;flex:1;background:var(--bg-card)">
    ${projRenderDiffTable(_diffHunks, _diffChoices)}
  </div>
</div>`;

    modal.querySelector('#proj-diff-ver-sel').addEventListener('change', function () {
        _projDiffSavedCode = codeMap[this.value] || '';
        initDiff(_projDiffSavedCode);
        projRefreshDiffTable();
    });

    modal.querySelector('#proj-diff-file-sel').addEventListener('change', function () {
        // Switching file rebuilds BOTH sides coherently: the per-version
        // map for the new path, and the right side from that path's
        // model. Apply-to-editor (below) follows the same selection.
        diffPath = this.value;
        window._projDiffApplyPath = diffPath;
        rebuildCodeMap();
        const verSel = modal.querySelector('#proj-diff-ver-sel');
        _projDiffSavedCode = codeMap[verSel.value] || '';
        initDiff(_projDiffSavedCode);
        projRefreshDiffTable();
    });
    window._projDiffApplyPath = diffPath;

    let _sub = null;
    if (_monacoInst) {
        _sub = _monacoInst.onDidChangeModelContent(() => {
            if (!document.getElementById('proj-diff-modal')) return;
            initDiff(_projDiffSavedCode);
            projRefreshDiffTable();
        });
    }

    modal.addEventListener('click', e => {
        if (e.target === modal) { modal.remove(); _sub?.dispose(); window._projDiffCodeMap = null; }
    });
    modal.querySelector('[title="Close"]').addEventListener('click', () => { _sub?.dispose(); });

    document.body.appendChild(modal);
}

export function projDiffApplyToEditor() {
    const result = projCollectDiffResult().replace(/\u00A0/g, ' ');
    // The result lands in the SELECTED file's model (the diff is per
    // file), as an UNDOABLE edit — same contract as the wizard rewrite
    // bridge — and that tab takes focus so the user sees what changed.
    //
    // Português: O resultado entra no MODEL do arquivo SELECIONADO,
    // como edição desfazível, e a aba dele ganha o foco.
    const path = window._projDiffApplyPath || _editorTabs[_activeTabIdx]?.path;
    const t = _editorTabs.find(x => x.path === path);
    if (t) {
        if (t.model.getValue() !== result) {
            t.model.pushEditOperations(
                [],
                [{ range: t.model.getFullModelRange(), text: result }],
                () => null,
            );
        }
        window._projActivateTabByPath?.(path);
    } else if (_monacoInst) {
        _monacoInst.setValue(result);
    } else {
        const ta = document.getElementById('proj-fallback'); if (ta) ta.value = result;
    }
    document.getElementById('proj-diff-modal')?.remove();
    window._projDiffApplyPath = null;
    projSetParseStatus('⚠ Code updated from diff — click Parse to validate.', 'warning');
}

// projSaveBtnWarn flashes an inline warning on the Save button itself.
function projSaveBtnWarn(msg) {
    const btn = document.getElementById('proj-save-btn');
    if (!btn) { projFormAlert(msg, 'warning', 6000); return; }
    const orig      = btn.innerHTML;
    const origStyle = btn.getAttribute('style') || '';
    btn.style.cssText = 'background:var(--warning,#f59e0b);border-color:var(--warning,#f59e0b);color:#fff';
    btn.innerHTML = `<i class="fa-solid fa-triangle-exclamation"></i> ${msg}`;
    if (btn.animate) {
        btn.animate(
            [{ transform:'translateX(0)' },{ transform:'translateX(-5px)' },
                { transform:'translateX(5px)' },{ transform:'translateX(-5px)' },
                { transform:'translateX(0)' }],
            { duration: 300, easing: 'ease-in-out' }
        );
    }
    setTimeout(() => {
        btn.innerHTML = orig;
        btn.setAttribute('style', origStyle);
    }, 3000);
}

function projFormAlert(msg, type, ms = 0) {
    const el = document.getElementById('proj-form-alert');
    if (!el) return;
    clearTimeout(_formAlertTimer);
    el.innerHTML = msg ? `<div class="alert alert-${type}">${msg}</div>` : '';
    if (msg && ms > 0) _formAlertTimer = setTimeout(() => { el.innerHTML = ''; }, ms);
}

// ─── Publish flags section (shared by Create and Properties modals) ───────────
//
// _buildPublishSection renders the three publish checkboxes used in both
// modals. The `disabled` parameter controls whether the checkboxes can be
// interacted with.
//
// In the Create modal: disabled=true — a brand-new project is never
// immediately ready for publication. The owner enables the flags later via
// the Properties modal.
//
// In the Properties modal: disabled=false — the flags are fully interactive.
//
// The "Ready to use" checkbox triggers a quality-commitment confirmation
// (onReadyToUseChange). If the user cancels, all three flags are unchecked.
//
// namePrefix is used to namespace the checkbox IDs so both modals can coexist
// in the DOM without ID conflicts (e.g. "cp" for create, "pp" for properties).
function _buildPublishSection(namePrefix, publishToFeed, publishToSearch, readyToUse, disabled) {
    const dis = disabled ? 'disabled' : '';
    const opacity = disabled ? 'opacity:.45;' : '';
    return `
<div id="${namePrefix}-publish-section" style="margin-top:12px;padding:12px 14px;
     background:var(--bg-surface);border:1px solid var(--border);border-radius:var(--r)">
  <div style="font-size:12px;font-weight:700;color:var(--text-secondary);margin-bottom:8px;
              text-transform:uppercase;letter-spacing:.04em">
    <i class="fa-solid fa-bullhorn" style="color:var(--primary);margin-right:6px"></i>
    Publishing
    ${disabled ? `<span style="font-size:11px;font-weight:400;color:var(--text-muted);
                               margin-left:8px;text-transform:none">
                   (available after project is created)
                 </span>` : ''}
  </div>
  <label style="display:flex;align-items:flex-start;gap:10px;margin-bottom:8px;
                cursor:${disabled?'default':'pointer'};${opacity}">
    <input type="checkbox" id="${namePrefix}-pub-feed" ${publishToFeed?'checked':''} ${dis}
           onchange="onPublishFlagChange('${namePrefix}')"
           style="margin-top:2px;accent-color:var(--primary)">
    <div>
      <div style="font-size:13px;font-weight:600">Publish to feed</div>
      <div style="font-size:11px;color:var(--text-muted)">
        Show this project in the community feed tabs
      </div>
    </div>
  </label>
  <label style="display:flex;align-items:flex-start;gap:10px;margin-bottom:8px;
                cursor:${disabled?'default':'pointer'};${opacity}">
    <input type="checkbox" id="${namePrefix}-pub-search" ${publishToSearch?'checked':''} ${dis}
           onchange="onPublishFlagChange('${namePrefix}')"
           style="margin-top:2px;accent-color:var(--primary)">
    <div>
      <div style="font-size:13px;font-weight:600">Publish to search</div>
      <div style="font-size:11px;color:var(--text-muted)">
        Include this project in marketplace search results
      </div>
    </div>
  </label>
  <label style="display:flex;align-items:flex-start;gap:10px;
                cursor:${disabled?'default':'pointer'};${opacity}">
    <input type="checkbox" id="${namePrefix}-ready" ${readyToUse?'checked':''} ${dis}
           onchange="onReadyToUseChange('${namePrefix}',this)"
           style="margin-top:2px;accent-color:var(--primary)">
    <div>
      <div style="font-size:13px;font-weight:600">
        Ready to use
        <span style="font-size:10px;font-weight:600;background:#fef3c7;color:#92400e;
                     border:1px solid #fde68a;border-radius:99px;padding:1px 7px;
                     margin-left:6px">quality commitment</span>
      </div>
      <div style="font-size:11px;color:var(--text-muted)">
        I certify this project is documented and ready for use by others
      </div>
    </div>
  </label>
</div>`;
}

// onPublishFlagChange is called by the feed and search checkboxes.
// Currently a no-op — state is read on submit. Kept as a hook for future
// UI logic (e.g. auto-checking "feed" when "search" is checked).
export function onPublishFlagChange(namePrefix) { /* reserved for future logic */ }

// onReadyToUseChange is called when the "Ready to use" checkbox changes.
// When the user checks it, a modal confirmation is shown. If the user
// cancels the confirmation, all three publish flags are unchecked.
export function onReadyToUseChange(namePrefix, checkbox) {
    if (!checkbox.checked) return; // unchecking is always allowed silently

    // Show the quality-commitment confirmation dialog.
    _showReadyToUseConfirm(namePrefix);
}

// _showReadyToUseConfirm renders a centered overlay with the quality commitment
// text. The user must explicitly confirm or cancel.
//
// Confirming: closes the dialog, leaving all checkboxes in their current state.
// Cancelling: unchecks all three publish flags and closes the dialog.
function _showReadyToUseConfirm(namePrefix) {
    const existing = document.getElementById('proj-ready-confirm');
    if (existing) existing.remove();

    const overlay = document.createElement('div');
    overlay.id = 'proj-ready-confirm';
    overlay.style.cssText =
        'position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:10000;' +
        'display:flex;align-items:center;justify-content:center;padding:16px';

    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:520px;box-shadow:0 8px 40px rgba(0,0,0,.4);
            border:2px solid var(--primary);animation:fi .2s ease">
  <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px">
    <i class="fa-solid fa-certificate" style="font-size:24px;color:var(--primary)"></i>
    <h2 style="font-size:17px;font-weight:700;margin:0">Quality Commitment</h2>
  </div>
  <p style="font-size:14px;line-height:1.7;color:var(--text-secondary);margin:0 0 20px">
    I commit to the quality of this project.
    The project is documented and ready to be used by other users, even those
    with only basic knowledge.
    The documentation was written with quality, containing clear and direct texts
    explaining all necessary points for the correct use of the project.
  </p>
  <div style="display:flex;gap:10px;justify-content:flex-end">
    <button class="btn btn-secondary btn-sm"
            onclick="cancelReadyToUse('${namePrefix}')" style="min-width:90px">
      Cancel
    </button>
    <button class="btn btn-primary btn-sm"
            onclick="confirmReadyToUse()" style="min-width:90px">
      <i class="fa-solid fa-check"></i> I Commit
    </button>
  </div>
</div>`;

    document.body.appendChild(overlay);
}

// confirmReadyToUse closes the quality commitment dialog without changing
// any checkbox state — the "Ready to use" checkbox remains checked.
export function confirmReadyToUse() {
    document.getElementById('proj-ready-confirm')?.remove();
}

// cancelReadyToUse closes the confirmation and resets all three publish
// flags to unchecked. This is the safe fallback if the user changes their mind.
export function cancelReadyToUse(namePrefix) {
    document.getElementById('proj-ready-confirm')?.remove();
    const feed   = document.getElementById(`${namePrefix}-pub-feed`);
    const search = document.getElementById(`${namePrefix}-pub-search`);
    const ready  = document.getElementById(`${namePrefix}-ready`);
    if (feed)   feed.checked   = false;
    if (search) search.checked = false;
    if (ready)  ready.checked  = false;
}

// _openTypeHelp loads and displays a markdown help modal for the given project
// type ('device' or 'template'). The markdown file is fetched via AJAX from
// /help/customProject/project-type-{type}.{locale}.md with an automatic fallback
// to the English version when the user's locale file is not available.
//
// The function reads the user's preferred locale from window.S.profile.locale
// (set by checkAuth after login). If no locale is found, 'en' is used.
//
// Clicking outside the modal or the × button closes it. The modal sits at
// z-index 10001 so it always appears above the create modal (9000).
async function _openTypeHelp(type) {
    const locale = (window.S?.profile?.locale || 'en').toLowerCase();
    const base   = `/help/customProject/project-type-${type}`;

    // Fetch the localised file; fall back to English on 4xx/5xx.
    let mdText = '';
    try {
        const tryFetch = async (url) => {
            const r = await fetch(url);
            if (!r.ok) return null;
            return r.text();
        };
        mdText = (locale !== 'en' ? await tryFetch(`${base}.${locale}.md`) : null)
               ?? await tryFetch(`${base}.en.md`)
               ?? `# Help not found\n\nCould not load the help file for **${type}**.`;
    } catch(e) {
        mdText = `# Could not load help\n\n${e?.message || String(e)}`;
    }

    // Remove any existing instance before opening a new one.
    document.getElementById('proj-help-type-modal')?.remove();

    const div = document.createElement('div');
    div.id = 'proj-help-type-modal';
    div.style.cssText =
        'position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:10001;' +
        'display:flex;align-items:center;justify-content:center;padding:16px';

    div.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:28px 32px;
            width:100%;max-width:600px;max-height:80vh;overflow-y:auto;
            box-shadow:var(--shh);border:1px solid var(--border);animation:fi .2s ease">
  <div style="display:flex;justify-content:flex-end;margin-bottom:8px">
    <button onclick="document.getElementById('proj-help-type-modal')?.remove()"
            style="background:none;border:none;cursor:pointer;font-size:18px;
                   color:var(--text-muted);padding:4px 6px;line-height:1">
      <i class="fa-solid fa-xmark"></i>
    </button>
  </div>
  <div class="md-body">${window.marked?.parse ? window.marked.parse(mdText) : `<pre>${mdText}</pre>`}</div>
</div>`;

    // Clicking the backdrop closes the modal.
    div.addEventListener('click', e => { if (e.target === div) div.remove(); });
    document.body.appendChild(div);
}
export { _openTypeHelp };

// ─── Create project modal ─────────────────────────────────────────────────────
//
// The Create modal collects:
//   - Project Name    — free text; used as the initial display name in the menu
//   - Programming Language — always visible; reinforces the Go-only requirement
//   - Project Type: "Device"   (IDS-annotated Go struct published via GitHub)
//                   "Template" (project skeleton published via GitHub)
//   - GitHub Release URL
//   - Tags
//   - Menu Category / Subcategory
//   - Visibility (public / private)
//   - Publishing flags (only when visibility = public)
//
// Both types use the same form. The type choice determines which endpoint
// receives the POST: /api/v1/blackbox/submit (device) or /api/v1/templates (template).

// =============================================================================
//  New-Project dropdown menu
// =============================================================================
//
// The Projects page header has a "+ New Project" button that opens a
// small dropdown with two creation paths:
//
//   - openWizardCreateModal       — empty Go project, lands in editor
//   - openCreateProjectModal      — full GitHub-import modal (legacy)
//
// We split the entry point so the wizard flow does not have to compete
// with the GitHub-publishing form for the user's attention. Most new
// users want to write Go and try the wizard; the GitHub import is for
// returning users who already have a release to publish.

// projToggleNewMenu opens or closes the dropdown. Clicking outside the
// menu while it is open closes it (handled by a one-shot listener
// installed on `document` when the menu opens, removed on close).
export function projToggleNewMenu() {
    const menu = document.getElementById('proj-newbtn-menu');
    const btn  = document.getElementById('proj-newbtn');
    if (!menu || !btn) return;
    const isOpen = menu.classList.contains('open');
    if (isOpen) {
        _closeNewMenu();
        return;
    }
    menu.classList.add('open');
    btn.setAttribute('aria-expanded', 'true');
    // One-shot outside-click handler. Using setTimeout so the click
    // that opened the menu does not also close it (the click event
    // bubbles to `document` after our handler ran).
    setTimeout(() => {
        const handler = (e) => {
            if (!menu.contains(e.target) && !btn.contains(e.target)) {
                _closeNewMenu();
                document.removeEventListener('click', handler);
            }
        };
        document.addEventListener('click', handler);
        // Closing path also reachable via Escape — same UX as our
        // wizard modals.
        const escHandler = (e) => {
            if (e.key === 'Escape') {
                _closeNewMenu();
                document.removeEventListener('keydown', escHandler);
            }
        };
        document.addEventListener('keydown', escHandler);
    }, 0);
}

function _closeNewMenu() {
    const menu = document.getElementById('proj-newbtn-menu');
    const btn  = document.getElementById('proj-newbtn');
    menu?.classList.remove('open');
    btn?.setAttribute('aria-expanded', 'false');
}

// openWizardCreateModal opens the minimal "Create with wizard" modal.
// Only asks for the project name — every other field (type, visibility,
// programming language, UI language) is filled with sensible defaults
// for the wizard flow:
//
//   - type:                  custom_device  (the only Wizard-compatible kind today)
//   - visibility:            private        (projects are always private —
//                                            wizard authors the user's own
//                                            drafts; sharing happens via
//                                            GitHub releases imported into
//                                            the blackboxes table)
//   - programmingLanguageId: chosen in the modal dropdown
//                            (Slice 1: Go or C — both supported by the
//                            wizard parser)
//   - uiLanguageId:          S.locale       (the user's current UI locale)
//
// On success: refreshes the projects list and opens the editor on the
// new project. The user lands in the Editor tab with an empty Monaco
// (no source yet); they type code in the chosen language, click
// Parse, then switch to the Wizard tab to see the cards.
export function openWizardCreateModal() {
    _closeNewMenu();

    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;

    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';

    overlay.innerHTML = `
<div class="proj-wiz-create-modal">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">
    <i class="fa-solid fa-wand-magic-sparkles" style="color:var(--primary);margin-right:6px"></i>
    Create with wizard
  </h2>
  <p style="color:var(--text-muted);font-size:13px;margin-bottom:20px">
    Empty project. After it opens, write code in the editor and click
    <strong>Parse</strong> — the Wizard tab fills with cards for each struct
    and method, with ⚠ on anything still incomplete.
  </p>

  <div id="wc-err" class="alert alert-danger" style="display:none"></div>

  <div class="fg">
    <label>Project Name *</label>
    <input id="wc-name" class="fc" type="text" maxlength="100"
           placeholder="e.g. My Sensor Board"
           oninput="document.getElementById('wc-err').style.display='none'">
    <div class="fhint">Name shown in the IDE menu — can be updated later.</div>
  </div>

  <!-- Language picker (Slice 1 of C99 support). The wizard parser
       accepts Go AND C99; the choice changes the Monaco language, the
       indentation style (tabs for Go, spaces for C), and which
       server parser runs when Parse is clicked.
       The value is the DB id ('c'); the label spells the version
       explicitly so Arduino (which is C++-flavoured) can land as a
       separate row later without confusion.

       The first <option> is a disabled placeholder so the user is
       FORCED to make a deliberate choice — no language is pre-selected.
       Picking the wrong language by accident leaves the specialist
       editing the source in a Monaco mode that doesn't match what they
       actually pasted, which is confusing to debug. -->
  <div class="fg">
    <label>Programming Language *</label>
    <select id="wc-lang" class="fc" required>
      <option value="" disabled selected hidden>Select a language…</option>
      <option value="golang">Go</option>
      <option value="c">C99</option>
    </select>
    <div class="fhint">Pick the language of the code you'll paste into the editor.</div>
  </div>

  <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:24px">
    <button class="btn btn-ghost" onclick="closeWizardCreateModal()">Cancel</button>
    <button class="btn btn-primary" onclick="submitWizardCreate()" id="wc-submit">
      <i class="fa-solid fa-plus"></i> Create &amp; open editor
    </button>
  </div>
</div>`;

    // Click on backdrop closes the modal — same UX as the wizard
    // modals from slice 4. The inner card stops propagation via the
    // .proj-wiz-create-modal class because clicks on the card itself
    // should not close the modal.
    overlay.addEventListener('click', (e) => {
        if (e.target === overlay) closeWizardCreateModal();
    });
    document.addEventListener('keydown', _wcEscHandler);

    setTimeout(() => {
        const inp = document.getElementById('wc-name');
        inp?.focus();
        // Submit on Enter — convenient for keyboard-driven creation.
        inp?.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') submitWizardCreate();
        });
    }, 60);
}

// Module-level handler reference so the keydown listener can be
// removed cleanly when the modal closes.
function _wcEscHandler(e) {
    if (e.key === 'Escape') closeWizardCreateModal();
}

export function closeWizardCreateModal() {
    const overlay = document.getElementById('proj-modal-overlay');
    if (overlay) {
        overlay.style.display = 'none';
        overlay.innerHTML = '';
    }
    document.removeEventListener('keydown', _wcEscHandler);
}

// submitWizardCreate posts the new project and, on success, opens the
// editor. The defaults are all wizard-compatible — the user did not
// have to pick them, and they can change visibility later from the
// project's properties modal if they want.
export async function submitWizardCreate() {
    const nameInput = document.getElementById('wc-name');
    const errBox    = document.getElementById('wc-err');
    const submitBtn = document.getElementById('wc-submit');
    if (!nameInput || !errBox || !submitBtn) return;

    const name = nameInput.value.trim();
    if (!name) {
        errBox.textContent = 'Project name is required.';
        errBox.style.display = '';
        nameInput.focus();
        return;
    }
    if (/[\\/:*?"<>|]/.test(name)) {
        errBox.textContent = 'Project name must not contain: / \\ : * ? " < > |';
        errBox.style.display = '';
        return;
    }

    // Resolve the chosen language. The dropdown starts with no
    // selection (a disabled placeholder option); the user must pick
    // explicitly. An empty value here is an error — we surface it in
    // the same alert banner as the name validation and focus the
    // select so the user sees what to fix.
    const langSel = document.getElementById('wc-lang');
    const chosenLangId = langSel?.value || '';
    if (!chosenLangId) {
        errBox.textContent = 'Please choose a programming language.';
        errBox.style.display = '';
        langSel?.focus();
        return;
    }
    // Cross-check against _progLangs to fail early when the server has
    // not seeded the chosen language. _progLangs comes from
    // /api/v1/projects/meta/languages and is the truth for "which ids
    // does the server know about right now". A miss here means the
    // dropdown is out of sync with the server seed — surface a clear
    // message instead of letting the POST fail with a generic 400.
    const langRow = _progLangs.find(l => l.id === chosenLangId);
    if (!langRow) {
        errBox.textContent = 'Server does not recognise this language id (' +
            chosenLangId + '). Please reload the page.';
        errBox.style.display = '';
        return;
    }
    const langId = langRow.id;

    // UI language must be a value the server recognises. The DB seeds
    // `en` and `pt-BR` (NOT `en-US`) — the legacy create modal happens
    // to send valid IDs because it offers a `<select>` populated from
    // `_uiLangs`. Doing the same here: prefer S.locale only when it
    // is actually present in the loaded list, otherwise fall back to
    // the first server-recognised id. Hardcoded fallbacks like 'en-US'
    // are bugs waiting to happen — the seed list might change.
    const uiLangsList = Array.isArray(_uiLangs) ? _uiLangs : [];
    const matchByLocale = uiLangsList.find(l => l.id === S.locale);
    const uiLangId = (matchByLocale?.id) || (uiLangsList[0]?.id) || '';
    if (!uiLangId) {
        errBox.textContent = 'Server has no UI languages registered. Please report this.';
        errBox.style.display = '';
        return;
    }

    submitBtn.disabled = true;
    submitBtn._origHtml = submitBtn.innerHTML;
    submitBtn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Creating…';

    // visibility: 'private' is the only legal value for projects.
    // The server enforces this regardless of what we send (the
    // handler coerces it; the DB has CHECK(visibility='private')),
    // but sending the right value here keeps the network log
    // honest and lets older middleware in the chain bail early
    // if it ever rejects a public-visibility request to /projects.
    //
    // Wizard authors devices the user owns; sharing happens via
    // GitHub releases ingested into the blackboxes table, not via
    // marking a project public.
    const r = await api('POST', '/api/v1/projects', {
        name,
        type: 'custom_device',
        visibility: 'private',
        programmingLanguageId: langId,
        uiLanguageId: uiLangId,
    });

    submitBtn.disabled = false;
    submitBtn.innerHTML = submitBtn._origHtml;

    if (r?.metadata?.status !== 200 && r?.metadata?.status !== 201) {
        errBox.textContent = r?.metadata?.error || 'Could not create the project.';
        errBox.style.display = '';
        return;
    }

    const newId = r.data?.id || r.data?.project?.id;
    if (!newId) {
        errBox.textContent = 'Server returned no project id.';
        errBox.style.display = '';
        return;
    }

    closeWizardCreateModal();

    // Refresh the projects list so the editor's openCodeEditor — which
    // looks up projects by id in the in-memory `_projects` array —
    // can find the new one. Without this reload, openCodeEditor would
    // silently no-op.
    await loadProjects();
    openCodeEditor(newId);
}


export function openCreateProjectModal() {
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;
    const langOpts = _progLangs.map(l =>
        `<option value="${esc(l.id)}">${esc(l.display)}</option>`).join('');

    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';

    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:480px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease;
            max-height:90vh;overflow-y:auto">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">New device or template</h2>
  <p style="color:var(--text-muted);font-size:13px;margin-bottom:24px">Fill in the details below.</p>
  <div id="create-proj-err" class="alert alert-danger" style="display:none"></div>

  <!-- Project name — visible for both devices and templates.
       The specialist chooses the name that appears in the IDE menu.
       The worker may still overwrite it with the first # heading of readme.md,
       but the user-supplied value is used as the initial placeholder. -->
  <div class="fg">
    <label>Project Name *</label>
    <input id="cp-name" class="fc" type="text" maxlength="100"
           placeholder="e.g. APDS9960 Colour Sensor"
           oninput="clearCreateError()">
    <div class="fhint">Name shown in the IDE menu — can be updated later.</div>
  </div>

  <!-- Programming Language — always visible for both types.
       Keeping it visible reinforces that the project must target Go. -->
  <div class="fg" id="cp-lang-fg">
    <label>Programming Language *</label>
    <select id="cp-lang" class="fc"><option value="">— Select —</option>${langOpts}</select>
  </div>

  <!-- Project Type — Device or Template. Both publish via a GitHub release URL.
       The (?) button on each card opens an AJAX-loaded markdown help modal. -->
  <div class="fg">
    <label>Project Type *</label>
    <div style="display:flex;gap:12px;margin-top:4px">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)" id="type-code-label">
        <input type="radio" name="cp-type" value="custom_device" onchange="onTypeChange()" checked>
        <i class="fa-solid fa-microchip" style="color:var(--primary)"></i>
        <div style="flex:1">
          <div style="font-weight:600">Device</div>
          <div style="font-size:11px;color:var(--text-muted)">Publish via GitHub release</div>
        </div>
        <span onclick="event.preventDefault();event.stopPropagation();_openTypeHelp('device')"
              title="What is a Device?"
              style="display:inline-flex;align-items:center;justify-content:center;
                     width:18px;height:18px;border-radius:50%;flex-shrink:0;
                     background:rgba(0,0,0,.06);cursor:pointer;font-size:11px;color:inherit">
          <i class="fa-solid fa-circle-question"></i>
        </span>
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)" id="type-proj-label">
        <input type="radio" name="cp-type" value="custom_project" onchange="onTypeChange()">
        <i class="fa-solid fa-puzzle-piece" style="color:var(--primary)"></i>
        <div style="flex:1">
          <div style="font-weight:600">Template</div>
          <div style="font-size:11px;color:var(--text-muted)">Publish via GitHub release</div>
        </div>
        <span onclick="event.preventDefault();event.stopPropagation();_openTypeHelp('template')"
              title="What is a Template?"
              style="display:inline-flex;align-items:center;justify-content:center;
                     width:18px;height:18px;border-radius:50%;flex-shrink:0;
                     background:rgba(0,0,0,.06);cursor:pointer;font-size:11px;color:inherit">
          <i class="fa-solid fa-circle-question"></i>
        </span>
      </label>
    </div>
  </div>

  <!-- GitHub URL + tags — shown for both Custom Code and Custom Project.
       For devices: POST /api/v1/blackbox/submit
       For templates: POST /api/v1/templates + POST /api/v1/templates/:id/github -->
  <div id="cp-github-fg">
    <div class="fg">
      <label>GitHub Release URL *</label>
      <input id="cp-github-url" class="fc" type="url"
             placeholder="https://github.com/you/repo/releases/tag/v1.0"
             style="font-size:13px" oninput="clearCreateError()">
      <div class="fhint">Your GitHub account must be connected — Profile → Connect GitHub.</div>
    </div>
    <div class="fg">
      <label>Tags <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(optional)</span></label>
      <input id="cp-tags" class="fc" type="text"
             placeholder="e.g. sensor, i2c, temperature" style="font-size:13px">
      <div class="fhint">Comma-separated keywords used in search and feed.</div>
    </div>
  </div>

  <!-- Menu placement — where the device or template appears in the IDE menu.
       Categories and subcategories are loaded from /api/v1/projects/meta/readme-config.
       Subcategory select is populated dynamically when a category is selected. -->
  <div style="display:flex;gap:12px">
    <div class="fg" style="flex:1">
      <label>Menu Category <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(optional)</span></label>
      <select id="cp-category" class="fc" onchange="onCategoryChange()" style="font-size:13px">
        <option value="">— None —</option>
      </select>
    </div>
    <div class="fg" style="flex:1">
      <label>Subcategory <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(optional)</span></label>
      <select id="cp-subcategory" class="fc" style="font-size:13px">
        <option value="">— None —</option>
      </select>
    </div>
  </div>

  <div class="fg">
    <label>Visibility *</label>
    <div style="display:flex;gap:12px;margin-top:4px">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)" id="vis-pub-label">
        <input type="radio" name="cp-vis" value="public" onchange="onVisChange()">
        <i class="fa-solid fa-globe" style="color:var(--primary)"></i>
        <div><div style="font-weight:600">Public</div>
             <div style="font-size:11px;color:var(--text-muted)">Visible to the community</div></div>
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--primary);
                    background:var(--info-bg);
                    border-radius:var(--r);font-size:14px;transition:border-color var(--tr)" id="vis-prv-label">
        <input type="radio" name="cp-vis" value="private" onchange="onVisChange()" checked>
        <i class="fa-solid fa-lock" style="color:var(--text-secondary)"></i>
        <div><div style="font-weight:600">Private</div>
             <div style="font-size:11px;color:var(--text-muted)">Only you can see it</div></div>
      </label>
    </div>
  </div>

  <!-- Publish flags — only shown when visibility = public AND type = custom_device.
       Feed and search require ready_to_use. Quality commitment gates ready_to_use. -->
  <div id="cp-publish-wrap" style="display:none">
    ${_buildPublishSection('cp', false, false, false, false)}
  </div>

  <div style="display:flex;gap:10px;margin-top:16px">
    <button class="btn btn-secondary btn-sm" onclick="closeCreateProjectModal()" style="flex:1">Cancel</button>
    <button class="btn btn-primary btn-sm" onclick="submitCreateProject()" style="flex:2" id="cp-submit">
      <i class="fa-solid fa-folder-plus"></i> Create Project
    </button>
  </div>
</div>`;

    onTypeChange();
    // Populate category select from already-loaded _readmeConfig.
    _populateCategorySelect('cp-category', 'cp-subcategory', '', '');
}

export function closeCreateProjectModal() {
    const o = document.getElementById('proj-modal-overlay');
    if (o) { o.style.display = 'none'; o.innerHTML = ''; }
}

export function clearCreateError() {
    const el = document.getElementById('create-proj-err');
    if (el) el.style.display = 'none';
}

export async function submitCreateProject() {
    const name       = document.getElementById('cp-name')?.value?.trim() || '';
    const langId     = document.getElementById('cp-lang')?.value;
    const type       = document.querySelector('input[name="cp-type"]:checked')?.value;
    const visibility = document.querySelector('input[name="cp-vis"]:checked')?.value;
    const errEl      = document.getElementById('create-proj-err');
    const btn        = document.getElementById('cp-submit');
    const showErr    = msg => {
        if (!errEl) return;
        errEl.textContent = msg;
        errEl.style.display = 'block';
        // Scroll the error into view — the user may be looking at the bottom of the modal.
        errEl.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    };

    if (!name)       return showErr('Please enter a project name.');
    if (!type)       return showErr('Please choose a project type.');
    if (!visibility) return showErr('Please choose a visibility option.');

    // Read shared fields.
    const githubUrl    = document.getElementById('cp-github-url')?.value?.trim();
    const tags         = document.getElementById('cp-tags')?.value?.trim() || '';
    const categoryId   = document.getElementById('cp-category')?.value || '';
    const subcategoryId = document.getElementById('cp-subcategory')?.value || '';

    // Custom Project (template) — route to templates API.
    if (type === 'custom_project') {
        if (!githubUrl) return showErr('Please enter the GitHub release URL.');
        if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Creating…'; }
        return _tplHandleCreate(name, visibility, githubUrl, tags, categoryId, subcategoryId, errEl, btn);
    }

    // Device (custom_device) — submit via GitHub release URL.
    if (type === 'custom_device') {
        if (!langId)    return showErr('Please select a programming language.');
        if (!githubUrl) return showErr('Please enter the GitHub release URL.');
        if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Submitting…'; }
        // Always create as private — the specialist must verify the device
        // works correctly before publishing it to the community.
        // If they chose public, we honour the intent after they confirm via
        // the Properties modal (gear icon). A toast explains this.
        const wantsPublic = (visibility === 'public');
        try {
            const res = await api('POST', '/api/v1/blackbox/submit', {
                name,
                github_url: githubUrl,
                tags,
                visibility: 'private',
                categoryId,
                subcategoryId,
            });
            if ((res?.error) || (res?.metadata?.status >= 400)) {
                showErr((res.error || res?.metadata?.error) || 'Submission failed');
                return;
            }
            const jobId = res?.job_id;
            if (!jobId) { showErr('No job ID returned.'); return; }
            closeCreateProjectModal();
            if (wantsPublic) {
                showPageAlert(
                    '<i class="fa-solid fa-triangle-exclamation" style="margin-right:8px"></i>' +
                    '<strong>Device created as private.</strong> ' +
                    'Test it in the IDE first to make sure everything works correctly. ' +
                    'When it is ready, click the <i class="fa-solid fa-gear" style="margin:0 3px"></i> ' +
                    'gear icon on the device row and set visibility to Public.',
                    'danger',
                    30000
                );
            } else {
                showPageAlert('Device submitted — parsing in progress…', 'info');
            }
            _startJobPoll(jobId, 'device');
        } catch(e) {
                showErr('Network error: ' + (e?.message || String(e)));
            } finally {
                if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-folder-plus"></i> Create Project'; }
            }
            return;
        }
    }


// onVisChange updates the visual selection state of the visibility radio buttons
// in the Create modal. The publish section is shown whenever visibility = public,
// regardless of project type — both devices and templates support publishing flags.
    export function onVisChange() {
        const val  = document.querySelector('input[name="cp-vis"]:checked')?.value;
        const pub  = document.getElementById('vis-pub-label');
        const prv  = document.getElementById('vis-prv-label');
        if (pub) { pub.style.borderColor = val === 'public'  ? 'var(--primary)' : 'var(--border)'; pub.style.background = val === 'public'  ? 'var(--info-bg)' : ''; }
        if (prv) { prv.style.borderColor = val === 'private' ? 'var(--primary)' : 'var(--border)'; prv.style.background = val === 'private' ? 'var(--info-bg)' : ''; }

        // Publish wrap: shown for any type when visibility is public.
        const pubWrap = document.getElementById('cp-publish-wrap');
        if (pubWrap) {
            pubWrap.style.display = (val === 'public') ? '' : 'none';
        }
    }


    export function onQualityCheckChange() {
        // Reserved — quality commitment is only in the Properties modal for devices.
    }

// onTypeChange updates the Project Type radio button visuals.
// The language field stays visible and functional for both types —
// it reinforces that the project always targets Go regardless of kind.
    export function onTypeChange() {
        const val  = document.querySelector('input[name="cp-type"]:checked')?.value;
        const code = document.getElementById('type-code-label');
        const proj = document.getElementById('type-proj-label');
        if (!code || !proj) return;
        code.style.borderColor = val === 'custom_device'  ? 'var(--primary)' : 'var(--border)';
        code.style.background  = val === 'custom_device'  ? 'var(--info-bg)' : '';
        proj.style.borderColor = val === 'custom_project' ? 'var(--primary)' : 'var(--border)';
        proj.style.background  = val === 'custom_project' ? 'var(--info-bg)' : '';

    // GitHub URL field is always shown — both types use it.
        const githubFg = document.getElementById('cp-github-fg');
    if (githubFg) githubFg.style.display = '';

        // Re-evaluate publish wrap visibility after type change.
        onVisChange();
    }

// onCategoryChange fetches subcategories for the selected category via AJAX
// and populates the subcategory select. The subcategory select is disabled
// until a category is chosen.
export async function onCategoryChange() {
        const catSel = document.getElementById('cp-category');
        const subSel = document.getElementById('cp-subcategory');
        if (!catSel || !subSel) return;
    const categoryId = catSel.value;
    if (!categoryId) {
        subSel.innerHTML = '<option value="">— None —</option>';
        subSel.disabled = true;
        return;
    }
    subSel.disabled = true;
    subSel.innerHTML = '<option value="">Loading…</option>';
    const r = await api('GET', `/api/v1/projects/meta/subcategories?categoryId=${encodeURIComponent(categoryId)}`);
    const subs = r?.data || [];
    subSel.innerHTML = '<option value="">— None —</option>' +
        subs.map(s => `<option value="${esc(s.id)}">${esc(s.name)}</option>`).join('');
    subSel.disabled = subs.length === 0;
    }

// _populateCategorySelect fills a category <select> from _readmeConfig.
// The subcategory select starts disabled — it is populated via AJAX
// when the user picks a category (onCategoryChange / _upCategoryChange).
    function _populateCategorySelect(catElId, subElId, selectedCatId, selectedSubId) {
        const catSel = document.getElementById(catElId);
        const subSel = document.getElementById(subElId);
        if (!catSel || !_readmeConfig?.categories) return;
        catSel.innerHTML = '<option value="">— None —</option>' +
            (_readmeConfig.categories || []).map(c =>
                `<option value="${esc(c.id)}"${c.id === selectedCatId ? ' selected' : ''}>${esc(c.name)}</option>`
            ).join('');
    // If a category was pre-selected (e.g. editing), load its subcategories.
    if (selectedCatId && subSel) {
        subSel.disabled = true;
        subSel.innerHTML = '<option value="">Loading…</option>';
        api('GET', `/api/v1/projects/meta/subcategories?categoryId=${encodeURIComponent(selectedCatId)}`)
            .then(r => {
                const subs = r?.data || [];
        subSel.innerHTML = '<option value="">— None —</option>' +
            subs.map(s =>
                `<option value="${esc(s.id)}"${s.id === selectedSubId ? ' selected' : ''}>${esc(s.name)}</option>`
            ).join('');
                subSel.disabled = subs.length === 0;
            });
    } else if (subSel) {
        subSel.innerHTML = '<option value="">— None —</option>';
        subSel.disabled = true;
    }
    }


export function openPropertiesModal(projectId) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;

    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';

    // Note: we no longer compute isPublic — projects are always
    // private (see the visibility section below). The variable used
    // to drive radio selection and the publish-section visibility
    // gate; both are now hardcoded.

    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:480px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease;
            max-height:90vh;overflow-y:auto">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">
    <i class="fa-solid fa-gear" style="color:var(--primary);margin-right:8px"></i>
    Project Properties
  </h2>
  <p style="color:var(--text-muted);font-size:13px;margin-bottom:24px">${esc(p.name)}</p>
  <div id="pp-err" class="alert alert-danger" style="display:none"></div>

  <!-- Project name -->
  <div class="fg">
    <label>Project Name *</label>
    <input id="pp-name" class="fc" type="text" maxlength="100"
           value="${esc(p.name)}" oninput="clearPropertiesError()">
  </div>

  <!-- Visibility — always private for projects. The radios are kept
       visible (rather than hidden) for transparency: the user can see
       that 'public' exists as a concept but is told why it doesn't
       apply here. Sharing happens via GitHub releases imported into
       the blackboxes table, not via flipping a project flag. -->
  <div class="fg">
    <label>Visibility *</label>
    <div style="display:flex;gap:12px;margin-top:4px"
         title="Projects are always private. To share a device, publish a GitHub release — Anthropic's worker imports it into the marketplace.">
      <label style="display:flex;align-items:center;gap:8px;cursor:not-allowed;flex:1;
                    padding:10px 14px;border:1px solid var(--border);
                    background:var(--bg-surface);
                    border-radius:var(--r);font-size:14px;opacity:0.5"
             id="pp-vis-pub-label">
        <input type="radio" name="pp-vis" value="public" disabled>
        <i class="fa-solid fa-globe" style="color:var(--text-muted)"></i>
        <div><div style="font-weight:600">Public</div>
             <div style="font-size:11px;color:var(--text-muted)">Share via GitHub release</div></div>
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:not-allowed;flex:1;
                    padding:10px 14px;border:1px solid var(--primary);
                    background:var(--info-bg);
                    border-radius:var(--r);font-size:14px"
             id="pp-vis-prv-label">
        <input type="radio" name="pp-vis" value="private" checked disabled>
        <i class="fa-solid fa-lock" style="color:var(--text-secondary)"></i>
        <div><div style="font-weight:600">Private</div>
             <div style="font-size:11px;color:var(--text-muted)">Only you can see it</div></div>
      </label>
    </div>
    <p style="margin-top:8px;font-size:11px;color:var(--text-muted);line-height:1.5">
      <i class="fa-solid fa-circle-info" style="margin-right:4px"></i>
      Projects are always private. To share a device with other users,
      publish a GitHub release — it will be imported into the
      marketplace as a black-box.
    </p>
  </div>

  <!-- Publish section — never shown for projects. Kept commented-out
       so the next person reading this code sees why; the publish
       flags are only meaningful for public projects, and projects
       are never public.
  <div id="pp-publish-wrap" style="display:none">
    ${_buildPublishSection('pp', p.publishToFeed, p.publishToSearch, p.readyToUse, false)}
  </div>
  -->

  <div style="display:flex;gap:10px;margin-top:24px">
    <button class="btn btn-secondary btn-sm" onclick="closePropertiesModal()" style="flex:1">
      Cancel
    </button>
    <button class="btn btn-primary btn-sm" onclick="submitPropertiesModal('${projectId}')"
            style="flex:2" id="pp-submit">
      <i class="fa-solid fa-floppy-disk"></i> Save Properties
    </button>
  </div>
</div>`;
}

export function closePropertiesModal() {
    const o = document.getElementById('proj-modal-overlay');
    if (o) { o.style.display = 'none'; o.innerHTML = ''; }
}

export function clearPropertiesError() {
    const el = document.getElementById('pp-err');
    if (el) el.style.display = 'none';
}

// onPropertiesVisChange updates the visual state of the visibility radio buttons
// in the Properties modal and shows or hides the publish section depending on
// whether "public" or "private" is selected.
//
// DEPRECATED — projects are now always private and the radios are
// rendered disabled. This function is kept (and still exported) only
// because main.js binds it to window.onPropertiesVisChange; removing
// it would require coordinated edits across files. The body still
// works correctly if somehow invoked, but no DOM event reaches it
// any more. Safe to delete in a follow-up cleanup along with the
// matching main.js import.
export function onPropertiesVisChange() {
    const val = document.querySelector('input[name="pp-vis"]:checked')?.value;
    const pub = document.getElementById('pp-vis-pub-label');
    const prv = document.getElementById('pp-vis-prv-label');
    const publishWrap = document.getElementById('pp-publish-wrap');
    if (!pub || !prv) return;

    pub.style.borderColor = val === 'public'  ? 'var(--primary)' : 'var(--border)';
    pub.style.background  = val === 'public'  ? 'var(--info-bg)' : '';
    prv.style.borderColor = val === 'private' ? 'var(--primary)' : 'var(--border)';
    prv.style.background  = val === 'private' ? 'var(--info-bg)' : '';

    // Show the publish section only for public projects. Hiding it for private
    // avoids confusion — the server will zero all flags for private projects
    // regardless of what the client sends.
    if (publishWrap) {
        publishWrap.style.display = val === 'public' ? 'block' : 'none';
    }
}

// submitPropertiesModal collects the form values and calls PUT /api/v1/projects/:id.
// On success it updates the local _projects cache and re-renders the tree so the
// user sees the new name, badge and "ready" indicator immediately.
export async function submitPropertiesModal(projectId) {
    const name       = document.getElementById('pp-name')?.value?.trim();
    const errEl      = document.getElementById('pp-err');
    const btn        = document.getElementById('pp-submit');

    const showErr = msg => {
        if (errEl) { errEl.textContent = msg; errEl.style.display = 'block'; }
    };

    if (!name) return showErr('Project name is required.');

    // Visibility is always 'private' — see openPropertiesModal for
    // the full rationale. The publish flags are correspondingly
    // always false: they only mean anything for public projects,
    // and projects can't be public. The server enforces all of
    // this independently (force-coercion in handleUpdateProject
    // plus the DB CHECK), but sending the right values keeps the
    // network log honest.
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }

    const r = await api('PUT', `/api/v1/projects/${projectId}`, {
        name,
        visibility:      'private',
        publishToFeed:   false,
        publishToSearch: false,
        readyToUse:      false,
    });

    if (btn) {
        btn.disabled = false;
        btn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Save Properties';
    }

    if (r?.metadata?.status === 200) {
        // Update the local cache so the tree reflects the new values without a
        // full reload — keeps the UI snappy and preserves expansion state.
        const updated = r.data;
        const idx = _projects.findIndex(x => x.id === projectId);
        if (idx >= 0) _projects[idx] = updated;

        closePropertiesModal();
        renderTree();
        showPageAlert('Project updated.', 'success');
    } else {
        showErr(r?.metadata?.error || 'Could not save. Please try again.');
    }
}

// ─── Delete project modal ─────────────────────────────────────────────────────

export function confirmDeleteProject(projectId, projectName) {
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;
    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.55);' +
        'z-index:9000;align-items:center;justify-content:center;';
    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:440px;box-shadow:var(--shh);
            border:2px solid var(--danger);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;color:var(--danger);margin-bottom:8px">
    <i class="fa-solid fa-triangle-exclamation"></i> Delete Project
  </h2>
  <p style="color:var(--text-secondary);font-size:14px;margin-bottom:16px">
    This action is <strong>permanent and cannot be undone</strong>.
    All files and version history will be deleted.
  </p>
  <p style="font-size:13px;color:var(--text-secondary);margin-bottom:8px">
    Type <strong>${esc(projectName)}</strong> to confirm:
  </p>
  <input id="del-confirm-input" class="fc" type="text"
    placeholder="${esc(projectName)}" oninput="onDeleteConfirmInput('${esc(projectName)}')">
  <div id="del-proj-err" class="alert alert-danger" style="display:none;margin-top:12px"></div>
  <div style="display:flex;gap:10px;margin-top:20px">
    <button class="btn btn-secondary btn-sm" onclick="closeCreateProjectModal()" style="flex:1">Cancel</button>
    <button class="btn btn-danger btn-sm" id="del-submit" disabled
      onclick="executeDeleteProject('${projectId}')" style="flex:2">
      <i class="fa-solid fa-trash-can"></i> Delete Forever
    </button>
  </div>
</div>`;
}

export function onDeleteConfirmInput(expected) {
    const input = document.getElementById('del-confirm-input');
    const btn   = document.getElementById('del-submit');
    if (!input || !btn) return;
    btn.disabled = input.value !== expected;
}

export async function executeDeleteProject(projectId) {
    const btn   = document.getElementById('del-submit');
    const errEl = document.getElementById('del-proj-err');
    if (btn) { btn.disabled = true; btn.textContent = 'Deleting…'; }
    const r = await api('DELETE', `/api/v1/projects/${projectId}`);
    if (r?.metadata?.status === 200) {
        closeCreateProjectModal();
        _expandedProjs.delete(projectId);
        delete _projectFiles[projectId];
        await loadProjects();
        renderTree();
        showPageAlert('Project deleted permanently.', 'success');
    } else {
        if (errEl) { errEl.textContent = r?.metadata?.error || 'Could not delete.'; errEl.style.display = 'block'; }
        if (btn)   { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-trash-can"></i> Delete Forever'; }
    }
}

// ─── File operations ──────────────────────────────────────────────────────────

export function triggerFileUpload(projectId, section) {
    document.getElementById(`finput-${projectId}-${section}`)?.click();
}

export async function onFileSelected(event, projectId, section) {
    const file = event.target.files?.[0];
    if (!file) return;
    event.target.value = '';
    const form = new FormData();
    form.append('file', file);
    const opts = { method: 'POST', body: form };
    if (S.token) opts.headers = { Authorization: 'Bearer ' + S.token };
    const r = await fetch(`/api/v1/projects/${projectId}/files/${section}`, opts)
        .then(res => res.json()).catch(() => null);
    if (r?.metadata?.status === 200 || r?.metadata?.status === 201) {
        await loadProjectFiles(projectId);
        renderTree();
        // A code upload wrote a new snapshot; an open editor on this
        // project must learn about it (or warn if it has unsaved work).
        if (section === 'code') _resyncOpenEditorFromServer(projectId);
    } else {
        showPageAlert(r?.metadata?.error || 'Upload failed.', 'danger');
    }
}

export async function deleteProjectFile(projectId, section, filename) {
    // Code section, multi-file rules: with siblings present, deletion
    // targets ONE path (?path= → the server writes a new snapshot without
    // it). The LAST file falls back to the bare endpoint — the whole-
    // section clear of the single-file era — because a per-path delete of
    // the only file would fail the server's snapshot contract on purpose
    // (a snapshot needs ≥1 file; "empty the project" is a different
    // intent, and the bare route expresses it).
    //
    // Português: Com irmãos presentes, o delete mira UM caminho (?path= —
    // versão nova sem ele). O ÚLTIMO arquivo cai na rota crua (limpar a
    // seção): apagar por caminho o único arquivo falharia o contrato do
    // snapshot de propósito (snapshot exige ≥1 arquivo; "esvaziar" é
    // outra intenção, e a rota crua a expressa).
    const codeSiblings = _projectFiles[projectId]?.code?.length || 0;
    const url = section === 'code'
        ? (codeSiblings > 1
            ? `/api/v1/projects/${projectId}/files/code?path=${encodeURIComponent(filename)}`
            : `/api/v1/projects/${projectId}/files/code`)
        : `/api/v1/projects/${projectId}/files/${section}/${encodeURIComponent(filename)}`;
    const r = await api('DELETE', url);
    if (r?.metadata?.status === 200) {
        await loadProjectFiles(projectId);
        renderTree();
        if (section === 'code') _resyncOpenEditorFromServer(projectId);
    } else {
        showPageAlert(r?.metadata?.error || 'Could not delete file.', 'danger');
    }
}

export async function promptRenameCode(projectId, currentName) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    // Which file: the row's pencil passes its own name; the toolbar
    // button (no arg) targets the first file — the single-file era's
    // meaning, still correct for one-file projects. With nothing saved
    // yet the suggestion mirrors the editor's first-save default:
    // project-name slug + the LANGUAGE's extension (this ran in tree
    // context where the old code assumed Go — the live "main.go on a
    // C99 project" report).
    //
    // Português: Qual arquivo: o lápis da linha passa o próprio nome; o
    // botão da toolbar (sem arg) mira o primeiro. Sem save ainda, a
    // sugestão espelha o default do editor: slug do nome + extensão DA
    // LINGUAGEM (isto rodava na árvore assumindo Go — o "main.go em
    // projeto C99" reportado ao vivo).
    const files = _projectFiles[projectId];
    const existing = currentName || files?.code?.[0]?.name || '';
    const current = existing || (slugifyFilename(p.name) + _codeExtFor(projectId));
    // showPrompt resolves to null on cancel and to the typed string on
    // OK. We prefer it over window.prompt() because the native dialog
    // sits outside our overlay stack: in fullscreen / kiosk mode some
    // browsers suppress it entirely, and in our editor (which
    // sometimes runs nested inside the wizard tab) the native prompt
    // appears at the BROWSER level rather than over the project UI,
    // which the user reads as "the button does nothing". The portal
    // modal also picks up our theme tokens (--bg-card, --primary)
    // and matches every other dialog in the app.
    const newName = await showPrompt('New filename (' + _codeExtFor(projectId) + '):', current);
    if (!newName || newName.trim() === current) return;

    // oldPath rides along whenever a real file exists so the server
    // renames THAT entry; without it (nothing saved yet) the server
    // answers 400 "no code file" — honest, nothing to rename.
    const body = { newName: newName.trim() };
    if (existing) body.oldPath = existing;
    const r = await api('PUT',
        `/api/v1/projects/${projectId}/files/code/rename`,
        body,
    );
    if (r?.metadata?.status === 200) {
        // Rename is "a save with one path changed": the response is a NEW
        // snapshot ({version, files}). The renamed identity is the name
        // the user just typed — files[0] would lie with several entries.
        _defaultFilename = newName.trim();
        _resyncOpenEditorFromServer(projectId);
        if (typeof r.data?.version === 'number') {
            _currentVersion = r.data.version;
            _nextVersion    = _currentVersion + 1;
        }
        if (_editMode && _editProject?.id === projectId) {
            updateVersionBar();
        } else {
            await loadProjectFiles(projectId);
            renderTree();
        }
        showPageAlert('File renamed to ' + _defaultFilename, 'success');
    } else {
        showPageAlert(r?.metadata?.error || 'Could not rename file.', 'danger');
    }
}

export async function copyImageMarkdown(url, name) {
    const md = `![${name}](${url})`;
    try { await navigator.clipboard.writeText(md); showPageAlert('Markdown link copied!', 'success'); }
    catch { window.prompt('Copy the Markdown link:', md); }
}

// ─── Page helpers ─────────────────────────────────────────────────────────────

// showPageAlert creates a stacking toast notification.
// Each call appends a new toast — they stack vertically and each disappears
// independently after durationMs (default 5000ms). The × button dismisses early.
function showPageAlert(msg, type, durationMs) {
    // The stack is created ON DEMAND: the old `if (!stack) return;`
    // silently swallowed every alert on views that never rendered the
    // container — a validation rejection with no visible feedback is
    // indistinguishable from a bug (field report 2026-07-08: the New
    // File prompt "just closed" — the rejection toast was no-oping).
    // z-index sits ABOVE the modal backdrop (10000) so a rejection
    // fired from a prompt is visible the instant the prompt closes.
    //
    // Português: O stack nasce SOB DEMANDA — o `return` antigo engolia
    // alertas em views sem o container; rejeição sem feedback é
    // indistinguível de bug (relato de campo: o prompt "só fechava").
    // z-index acima do backdrop do modal (10000).
    let stack = document.getElementById('proj-toast-stack');
    if (!stack) {
        stack = document.createElement('div');
        stack.id = 'proj-toast-stack';
        stack.style.cssText =
            'position:fixed;top:16px;right:16px;z-index:10001;display:flex;' +
            'flex-direction:column;gap:8px;max-width:380px;pointer-events:none;';
        document.body.appendChild(stack);
    }

    const ms  = durationMs || 5000;
    const id  = 'toast-' + Date.now() + '-' + Math.random().toString(36).slice(2, 8);

    const borderColor = {
        success: '#16a34a', danger: '#dc2626',
        info: '#3b82f6',    warning: '#d97706',
    }[type] || '#3b82f6';
    const bgColor = {
        success: '#d1fae5', danger: '#fee2e2',
        info: '#dbeafe',    warning: '#fef3c7',
    }[type] || '#dbeafe';
    const icon = {
        success: 'fa-circle-check', danger: 'fa-triangle-exclamation',
        info: 'fa-circle-info',     warning: 'fa-triangle-exclamation',
    }[type] || 'fa-circle-info';

    const toast = document.createElement('div');
    toast.id = id;
    toast.style.cssText =
        'pointer-events:auto;display:flex;align-items:flex-start;gap:10px;' +
        'padding:12px 14px;border-radius:6px;border-left:4px solid ' + borderColor + ';' +
        'background:' + bgColor + ';box-shadow:0 4px 16px rgba(0,0,0,.18);' +
        'font-size:13px;line-height:1.5;color:#111;animation:fi .2s ease;';
    toast.innerHTML =
        '<i class="fa-solid ' + icon + '" style="flex-shrink:0;margin-top:2px;color:' + borderColor + '"></i>' +
        '<div style="flex:1">' + msg + '</div>' +
        '<button onclick="document.getElementById(\'' + id + '\')?.remove()" ' +
    'style="background:none;border:none;cursor:pointer;color:#666;font-size:18px;line-height:1;padding:0 0 0 6px;flex-shrink:0" ' +
    'title="Dismiss">&times;</button>';

    stack.appendChild(toast);

    // Fade out then remove after duration.
    // ms === 0 means the toast stays until the user dismisses it manually.
    if (ms > 0) {
        setTimeout(() => {
            toast.style.transition = 'opacity .4s';
            toast.style.opacity = '0';
            setTimeout(() => toast.remove(), 400);
        }, ms);
    }
}

// ─── Go editor slash menu ─────────────────────────────────────────────────────
//
// A lightweight floating menu that appears when the user types "/" in the Go
// Monaco editor.
//
// Options shown:
//   /image    — always available; inserts a Markdown image tag in a comment
//   /markdown — only on blank lines; inserts a manual help block comment
//
// The "/" that triggered the menu is removed when an item is selected.
// Pressing Escape or clicking outside closes the menu without removing the "/".

function projRegisterSlashMenu(projectId) {
    if (_imageCompletionDisposable) {
        _imageCompletionDisposable.dispose();
        _imageCompletionDisposable = null;
    }
    if (!_monacoInst) return;

    _imageCompletionDisposable = _monacoInst.onDidChangeModelContent(e => {
        if (e.changes.length !== 1 || e.changes[0].text !== '/') return;
        const pos   = _monacoInst.getPosition();
        const model = _monacoInst.getModel();
        const line  = model.getLineContent(pos.lineNumber);
        _slashPos = { lineNumber: pos.lineNumber, column: pos.column - 1 };
        const before = line.substring(0, pos.column - 2);
        const isBlankLine = before.trim() === '';
        _showSlashMenu(projectId, pos, isBlankLine);
    });
}

function _showSlashMenu(projectId, cursorPos, isBlankLine) {
    _closeSlashMenu();
    const editorDom = _monacoInst && _monacoInst.getDomNode();
    if (!editorDom) return;
    const pixelPos = _monacoInst.getScrolledVisiblePosition(cursorPos);
    if (!pixelPos) return;

    const editorRect = editorDom.getBoundingClientRect();
    const cursorTop  = editorRect.top  + pixelPos.top;
    const menuLeft   = editorRect.left + pixelPos.left;
    const spaceBelow = window.innerHeight - cursorTop - 22;
    const menuTop    = spaceBelow >= 180 ? cursorTop + 22 : cursorTop - 4;
    const flipUp = spaceBelow < 180;

    const menu = document.createElement('div');
    menu.id = 'proj-slash-menu';
    menu.style.cssText =
        'position:fixed;z-index:9999;background:var(--bg-card);' +
        'border:1px solid var(--border);border-radius:var(--r);' +
        'box-shadow:0 4px 20px rgba(0,0,0,.2);min-width:200px;overflow:hidden;' +
        'font-size:13px;font-family:var(--font);' +
        'top:' + menuTop + 'px;left:' + menuLeft + 'px;' +
        (flipUp ? 'transform:translateY(-100%)' : '');

    const items = [
        { id: 'image',    icon: 'fa-solid fa-image',      label: 'Image',    hint: 'Insert uploaded image' },
    ];
    if (isBlankLine) {
        items.push({ id: 'markdown', icon: 'fa-solid fa-file-lines', label: 'Markdown', hint: 'Insert manual block' });
    }

    menu.innerHTML = items.map(it =>
        '<div class="proj-slash-item" data-id="' + it.id + '"' +
        ' style="display:flex;align-items:center;gap:10px;padding:9px 14px;' +
        'cursor:pointer;transition:background .1s;border-bottom:1px solid var(--border)"' +
        ' onmouseover="this.style.background=\'var(--info-bg)\'"' +
        ' onmouseout="this.style.background=\'\'"' +
        ' onclick="projSlashPick(\'' + it.id + '\',\'' + projectId + '\')">' +
        '<i class="' + it.icon + '" style="color:var(--primary);width:16px;text-align:center"></i>' +
        '<div>' +
        '<div style="font-weight:600;color:var(--text-primary)">' + it.label + '</div>' +
        '<div style="font-size:11px;color:var(--text-muted)">' + it.hint + '</div>' +
        '</div></div>'
    ).join('');

    document.body.appendChild(menu);

    const escH = function(e) {
        if (e.key === 'Escape') { _closeSlashMenu(); document.removeEventListener('keydown', escH); }
    };
    document.addEventListener('keydown', escH);
    menu._escHandler = escH;

    setTimeout(function() {
        const outH = function(e) {
            if (!menu.contains(e.target)) {
                _closeSlashMenu();
                document.removeEventListener('mousedown', outH);
            }
        };
        document.addEventListener('mousedown', outH);
        menu._outsideHandler = outH;
    }, 0);
}

function _closeSlashMenu() {
    const menu = document.getElementById('proj-slash-menu');
    if (!menu) return;
    if (menu._escHandler)     document.removeEventListener('keydown',   menu._escHandler);
    if (menu._outsideHandler) document.removeEventListener('mousedown', menu._outsideHandler);
    menu.remove();
    const picker = document.getElementById('proj-slash-picker');
    if (picker) picker.remove();
}

export async function projSlashPick(category, projectId) {
    _closeSlashMenu();
    if (category === 'image') {
        await _slashPickImage(projectId);
    } else if (category === 'markdown') {
        await _slashPickMarkdown(projectId);
    }
}

async function _slashPickImage(projectId) {
    if (!_projectFiles[projectId]) await loadProjectFiles(projectId);
    const images = (_projectFiles[projectId] && _projectFiles[projectId].img) || [];
    if (!images.length) {
        projSetParseStatus('No images uploaded for this project.', 'warning');
        return;
    }
    _showFilePicker(
        images.map(function(f) { return { label: f.name, detail: f.url, data: f }; }),
        'Insert image',
        'fa-solid fa-image',
        function(f) {
            const baseName = f.name.replace(/\.[^.]+$/, '');
            _replaceSlash('![img_' + baseName + '](' + f.url + ')');
        },
        _monacoInst
    );
}

async function _slashPickMarkdown(projectId) {
    if (!_projectFiles[projectId]) await loadProjectFiles(projectId);
    const docs = (_projectFiles[projectId] && _projectFiles[projectId].docs) || [];
    if (!docs.length) {
        projSetParseStatus('No markdown files uploaded for this project.', 'warning');
        return;
    }
    _showFilePicker(
        docs.map(function(f) { return { label: f.name, detail: f.url, data: f }; }),
        'Insert manual block',
        'fa-solid fa-file-lines',
        async function(f) {
            let content = '';
            try {
                const r = await fetch(f.url);
                if (r.ok) content = await r.text();
            } catch(e) { /* use empty on failure */ }
            const lines = [
                '/*',
                'manualName:write_a_name_for_help_window.',
                'language:write_the_language. (eg.en,pt-br)',
                'showIn:function_name. (eg. init, run).',
                '```markdown',
                content.trimEnd(),
                '```',
                '*/',
            ];
            _replaceSlash(lines.join('\n'));
        },
        _monacoInst
    );
}

// ─── Shared file picker ───────────────────────────────────────────────────────
//
// _showFilePicker renders a floating list of items near the cursor.
// editorInst is the Monaco editor instance whose cursor position is used
// for placement; it is also focused after the user makes a selection.
// Pass _monacoInst for the Go editor or _mdInst for the Markdown editor.

function _showFilePicker(items, title, icon, onSelect, editorInst) {
    const inst      = editorInst || _monacoInst;
    const pos       = inst && inst.getPosition();
    const editorDom = inst && inst.getDomNode();
    if (!pos || !editorDom) return;
    const pixelPos  = inst.getScrolledVisiblePosition(pos);
    if (!pixelPos) return;

    const editorRect  = editorDom.getBoundingClientRect();
    const cursorTopP  = editorRect.top + pixelPos.top;
    const left        = editorRect.left + pixelPos.left;
    const spaceBelowP = window.innerHeight - cursorTopP - 22;
    const top         = spaceBelowP >= 280
        ? cursorTopP + 22
        : cursorTopP - 4;

    const picker = document.createElement('div');
    picker.id = 'proj-slash-picker';
    const flipUpP = spaceBelowP < 280;
    picker.style.cssText =
        'position:fixed;z-index:9999;background:var(--bg-card);' +
        'border:1px solid var(--border);border-radius:var(--r);' +
        'box-shadow:0 4px 20px rgba(0,0,0,.2);min-width:280px;' +
        'max-height:280px;display:flex;flex-direction:column;overflow:hidden;' +
        'font-size:13px;font-family:var(--font);' +
        'top:' + top + 'px;left:' + left + 'px;' +
        (flipUpP ? 'transform:translateY(-100%)' : '');

    const header =
        '<div style="padding:8px 14px;border-bottom:1px solid var(--border);' +
        'display:flex;align-items:center;gap:8px;background:var(--bg-surface);' +
        'font-weight:600;font-size:12px;color:var(--text-secondary);flex-shrink:0">' +
        '<i class="' + icon + '" style="color:var(--primary)"></i> ' + esc(title) +
        '<button onclick="document.getElementById(\'proj-slash-picker\')?.remove();window._projSlashFocus&&window._projSlashFocus()"' +
        ' style="margin-left:auto;background:none;border:none;cursor:pointer;font-size:16px;color:var(--text-muted);line-height:1">\xd7</button>' +
        '</div>';

    const rows = items.map(function(it, idx) {
        return '<div class="proj-slash-pick-item" data-idx="' + idx + '"' +
            ' style="display:flex;flex-direction:column;padding:8px 14px;cursor:pointer;' +
            'border-bottom:1px solid var(--border);transition:background .1s;flex-shrink:0"' +
            ' onmouseover="this.style.background=\'var(--info-bg)\'"' +
            ' onmouseout="this.style.background=\'\'">' +
            '<span style="font-weight:600;color:var(--text-primary)">' + esc(it.label) + '</span>' +
            '<span style="font-size:11px;color:var(--text-muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(it.detail) + '</span>' +
            '</div>';
    }).join('');

    picker.innerHTML = header + '<div style="overflow-y:auto;flex:1">' + rows + '</div>';
    document.body.appendChild(picker);

    window._projSlashFocus = function() { inst && inst.focus(); };

    picker.querySelectorAll('.proj-slash-pick-item').forEach(function(el, idx) {
        el.addEventListener('click', async function() {
            picker.remove();
            await onSelect(items[idx].data);
            inst && inst.focus();
        });
    });

    const escH = function(e) {
        if (e.key === 'Escape') {
            picker.remove();
            document.removeEventListener('keydown', escH);
            inst && inst.focus();
        }
    };
    document.addEventListener('keydown', escH);

    setTimeout(function() {
        const outH = function(e) {
            if (!picker.contains(e.target)) {
                picker.remove();
                document.removeEventListener('mousedown', outH);
            }
        };
        document.addEventListener('mousedown', outH);
    }, 0);
}

// _replaceSlash replaces the '/' in the Go editor (uses _slashPos).
function _replaceSlash(text) {
    if (!_monacoInst) return;
    const sp = _slashPos;
    _slashPos = null;
    if (!sp) { _insertAtCursor(_monacoInst, text); return; }
    const range = new monaco.Range(sp.lineNumber, sp.column, sp.lineNumber, sp.column + 1);
    _monacoInst.executeEdits('slash-replace', [{ range, text, forceMoveMarkers: true }]);
    _monacoInst.focus();
}

// _insertAtCursor inserts text at the current cursor position of any Monaco instance.
function _insertAtCursor(inst, text) {
    if (!inst) return;
    const pos = inst.getPosition();
    inst.executeEdits('slash-insert', [{
        range: new monaco.Range(pos.lineNumber, pos.column, pos.lineNumber, pos.column),
        text: text,
        forceMoveMarkers: true,
    }]);
    inst.focus();
}

// ─── Markdown editor ─────────────────────────────────────────────────────────
//
// The markdown editor reuses the same full-page layout as the Go code editor.
// It renders into _root, replacing the project tree or the Go code editor.
// Navigating back returns to the project tree.
//
// State variables prefixed _md* track the open document independently of the
// Go editor state so both modes can be switched without interference.
//
// Tabs: [ Edit ] [ Preview ]
//   Edit    — Monaco in 'markdown' language mode, full viewport height
//   Preview — rendered HTML from the _mdToHtml helper
//
// Save strategy:
//   New files (_mdIsNew = true):  POST /files/docs  — creates the file
//   Existing files:               PUT  /files/docs/:name — updates in place
//
// This approach replaces the legacy DELETE+POST pattern which broke for the
// protected readme.md (the server rejects DELETE on that file).
//
// Slash menu:
//   The markdown editor registers a /image slash menu that lets the user pick
//   an uploaded image from the project and inserts the Markdown syntax
//   ![name](url). The menu appears on any "/" typed in the editor.

export async function projOpenMarkdownEditor(projectId, filename, fileUrl) {
    _mdProjectId = projectId;
    _mdFilename  = filename;
    _mdFileUrl   = fileUrl;
    _mdIsNew     = false; // opening an existing file

    if (_mdInst) { _mdInst.dispose(); _mdInst = null; }
    if (_mdSlashDisposable) {
        try { _mdSlashDisposable.dispose(); } catch(e) {}
        _mdSlashDisposable = null;
    }

    renderMarkdownEditorView(_root);

    let content = '';
    try {
        const r = await fetch(fileUrl);
        if (r.ok) content = await r.text();
    } catch(e) { /* leave empty on network error */ }

    // Ensure project image files are loaded for the slash menu.
    if (!_projectFiles[projectId]) {
        await loadProjectFiles(projectId);
    }

    // For readme.md, also load the taxonomy and settings so the slash menu
    // can offer /cover, /category, and /subcategory completions.
    if (filename === 'readme.md' && !_readmeConfig) {
        await loadReadmeConfig();
    }

    _mountMarkdownMonaco(content);
}

// projCreateMarkdownFile opens the Markdown editor for a brand-new file that
// does not yet exist on disk. The user provides a filename via a prompt.
//
// Save behaviour: the first save uses POST /files/docs to create the file;
// subsequent saves use PUT /files/docs/:name (same as editing an existing file).
export async function projCreateMarkdownFile(projectId) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    let filename = window.prompt('New markdown filename (.md):', 'document.md');
    if (!filename || !filename.trim()) return;

    filename = filename.trim();
    if (!filename.toLowerCase().endsWith('.md')) {
        filename += '.md';
    }
    // Guard against reserved characters in the filename.
    if (/[/\\:*?"<>|]/.test(filename)) {
        alert('Invalid filename — must not contain: / \\ : * ? " < > |');
        return;
    }

    _mdProjectId = projectId;
    _mdFilename  = filename;
    _mdFileUrl   = null;
    _mdIsNew     = true; // triggers POST on first save

    if (_mdInst) { _mdInst.dispose(); _mdInst = null; }
    if (_mdSlashDisposable) {
        try { _mdSlashDisposable.dispose(); } catch(e) {}
        _mdSlashDisposable = null;
    }

    renderMarkdownEditorView(_root);

    // Ensure project image files are loaded for the slash menu.
    if (!_projectFiles[projectId]) {
        await loadProjectFiles(projectId);
    }

    // Any new .md file could eventually be renamed to readme.md, but more
    // importantly we always load the config so /cover is available everywhere.
    if (!_readmeConfig) {
        await loadReadmeConfig();
    }

    _mountMarkdownMonaco('');
}

function renderMarkdownEditorView(root) {
    if (!root) return;
    _injectEditorStyles();
    const filename  = _mdFilename  || '';
    const projectId = _mdProjectId || '';
    const newBadge  = _mdIsNew
        ? '<span style="font-size:11px;font-weight:600;background:var(--warning-bg);' +
        'color:var(--warning);border:1px solid var(--warning);border-radius:99px;' +
        'padding:1px 8px;margin-left:8px">unsaved</span>'
        : '';

    root.innerHTML = `
<div style="display:flex;flex-direction:column;height:100%">

  <!-- ── Breadcrumb + toolbar ── -->
  <div style="display:flex;align-items:center;gap:10px;padding:10px 20px;
              background:var(--bg-surface);border-bottom:1px solid var(--border);
              flex-wrap:wrap">
    <button class="btn btn-ghost btn-sm" onclick="projMarkdownClose()" title="Back to devices & templates">
      <i class="fa-solid fa-arrow-left"></i> Devices & Templates
    </button>
    <span style="color:var(--text-muted);font-size:13px">›</span>
    <span style="font-size:13px;font-weight:600;color:var(--text-primary)">${esc(filename)}</span>
    ${newBadge}
    <span style="color:var(--text-muted);font-size:13px">› docs</span>

    <div style="margin-left:auto;display:flex;gap:6px">
      <button id="proj-md-save-btn" class="btn btn-primary btn-sm"
              onclick="projMarkdownSave('${projectId}','${esc(filename)}')">
        <i class="fa-solid fa-floppy-disk"></i> Save
      </button>
    </div>
  </div>

  <!-- ── Tab bar ── -->
  <div style="display:flex;align-items:center;background:var(--bg-surface);
              border-bottom:1px solid var(--border)">
    <button class="proj-tab active" id="proj-md-tab-btn-edit" onclick="projMdSetTab('edit')">
      <i class="fa-solid fa-code"></i> Edit
    </button>
    <button class="proj-tab" id="proj-md-tab-btn-preview" onclick="projMdSetTab('preview')">
      <i class="fa-solid fa-eye"></i> Preview
    </button>
    <!-- right-side hint: slash menu tip -->
    <span style="margin-left:auto;padding:0 14px;font-size:12px;color:var(--text-muted)">
      <i class="fa-solid fa-bolt" style="color:var(--primary);margin-right:4px"></i>
      Type <kbd style="font-family:var(--mono);background:var(--bg-surface);
        border:1px solid var(--border);border-radius:3px;padding:0 4px">/</kbd>
      for image${_mdFilename === 'readme.md' ? ', cover, category, subcategory' : ''}
    </span>
  </div>

  <!-- ── Tab bodies ── -->

  <!-- EDIT tab (Monaco) -->
  <div id="proj-md-editor-wrap" style="width:100%">
    <textarea id="proj-md-fallback"
      style="width:100%;height:calc(100vh - 186px);min-height:320px;padding:16px;
             border:none;resize:none;font-family:var(--mono);font-size:13px;
             line-height:1.6;background:var(--bg-surface);color:var(--text-primary);
             outline:none"
      placeholder="Loading editor…"></textarea>
  </div>

  <!-- PREVIEW tab -->
  <div id="proj-md-preview"
    style="display:none;height:calc(100vh - 186px);min-height:320px;
           overflow-y:auto;background:var(--bg-surface);padding:24px 32px;
           font-size:15px;line-height:1.75;color:var(--text)"></div>

  <!-- ── Status bar ── -->
  <div id="proj-md-status"
    style="padding:8px 20px;font-size:13px;min-height:34px;
           border-top:1px solid var(--border);border-bottom:1px solid var(--border);
           background:var(--bg-page);color:var(--text-muted)">${esc(filename)}</div>

  <!-- ── Bottom toolbar ── -->
  <div style="padding:10px 20px;display:flex;align-items:center;gap:10px;flex-wrap:wrap">
    <span id="proj-md-save-status" style="font-size:13px;color:var(--success)"></span>
    <div style="flex:1"></div>
  </div>

</div>
`;
}

function _mountMarkdownMonaco(content) {
    const wrap = document.getElementById('proj-md-editor-wrap');
    if (!wrap) return;

    const mount = function() {
        if (_mdInst) { _mdInst.dispose(); _mdInst = null; }
        const ta = document.getElementById('proj-md-fallback');
        if (ta) ta.remove();

        const div = document.createElement('div');
        // Same height formula as the Go code editor.
        div.style.cssText = 'height:calc(100vh - 186px);min-height:320px;width:100%';
        wrap.appendChild(div);

        _mdInst = monaco.editor.create(div, {
            value:                content,
            language:             'markdown',
            theme:                'vs',
            wordWrap:             'on',
            lineNumbers:          'on',
            minimap:              { enabled: false },
            scrollBeyondLastLine: false,
            fontSize:             14,
            fontFamily:           "'Fira Code','Consolas',monospace",
            automaticLayout:      true,
        });

        // Update preview live when the preview tab is open.
        _mdInst.onDidChangeModelContent(function() {
            const prev = document.getElementById('proj-md-preview');
            if (prev && prev.style.display !== 'none') {
                prev.innerHTML = _mdFilename === 'readme.md'
                    ? _buildReadmeCardPreview(_mdInst.getValue())
                    : _mdToHtml(_mdInst.getValue());
            }
            // For readme.md: update the description character counter in the
            // status bar so the user knows how many characters remain before
            // the server truncates the description field.
            if (_mdFilename === 'readme.md') {
                _updateDescriptionCounter(_mdInst.getValue());
            }
        });

        // Register the slash menu for the markdown editor.
        _mdRegisterSlashMenu();

        // Add an "Insert image…" entry to Monaco's right-click context menu.
        // This is an ADDITIONAL, more discoverable path to the same uploaded-
        // image insertion the "/" slash menu already provides; the slash menu
        // stays fully intact. The action is bound to this editor instance, so
        // it is disposed together with the editor on the next mount — no
        // separate disposable to track.
        _mdInst.addAction({
            id:                 'iotm.md.insertImage',
            label:              'Insert image…',
            contextMenuGroupId: 'navigation',
            contextMenuOrder:   1.5,
            run: function() { _mdContextInsertImage(); },
        });

        // For readme.md: initialize the description counter in the status bar
        // immediately so the author sees their current character usage without
        // needing to type anything first.
        if (_mdFilename === 'readme.md') {
            _updateDescriptionCounter(content);
        }
    };

    if (window.monaco) {
        mount();
    } else if (!document.getElementById('proj-monaco-loader')) {
        const s = document.createElement('script');
        s.id  = 'proj-monaco-loader';
        s.src = '/monaco/vs/loader.js';
        s.onload = function() {
            require.config({ paths: { vs: '/monaco/vs' } });
            require(['vs/editor/editor.main'], function() {
                _monacoReady = true;
                mount();
            });
        };
        document.head.appendChild(s);
    } else {
        const waitId = setInterval(function() {
            if (window.monaco) { clearInterval(waitId); mount(); }
        }, 100);
    }
}

export function projMdSetTab(tab) {
    ['edit', 'preview'].forEach(function(t) {
        document.getElementById('proj-md-tab-btn-' + t)?.classList.toggle('active', t === tab);
    });
    const editWrap = document.getElementById('proj-md-editor-wrap');
    const prevDiv  = document.getElementById('proj-md-preview');
    if (editWrap) editWrap.style.display = tab === 'edit'    ? '' : 'none';
    if (prevDiv)  prevDiv.style.display  = tab === 'preview' ? '' : 'none';
    if (tab === 'preview' && prevDiv && _mdInst) {
        prevDiv.innerHTML = _mdFilename === 'readme.md'
            ? _buildReadmeCardPreview(_mdInst.getValue())
            : _mdToHtml(_mdInst.getValue());
    }
    if (tab === 'edit' && _mdInst) {
        setTimeout(function() { _mdInst.layout(); }, 0);
    }
}

// projMarkdownSave persists the current editor content to disk.
//
// Strategy:
//   _mdIsNew = true  → POST /files/docs  (create new file)
//   _mdIsNew = false → PUT  /files/docs/:name (update in place)
//
// Using PUT avoids the legacy DELETE+POST pattern which would fail for the
// protected readme.md (the server blocks DELETE on that file).
export async function projMarkdownSave(projectId, filename) {
    if (!_mdInst) return;
    const content = _mdInst.getValue();
    const btn = document.getElementById('proj-md-save-btn');
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }
    try {
        const blob = new Blob([content], { type: 'text/markdown' });
        const form = new FormData();
        form.append('file', blob, filename);
        const headers = {};
        if (S.token) headers['Authorization'] = 'Bearer ' + S.token;

        // Choose the endpoint based on whether the file already exists on disk.
        let url, method;
        if (_mdIsNew) {
            method = 'POST';
            url    = '/api/v1/projects/' + projectId + '/files/docs';
        } else {
            method = 'PUT';
            url    = '/api/v1/projects/' + projectId + '/files/docs/' + encodeURIComponent(filename);
        }

        const res  = await fetch(url, { method, headers, body: form });
        const json = await res.json();

        if (json?.metadata?.status === 200 || json?.metadata?.status === 201) {
            // After the first successful save, the file now exists on disk.
            // Switch to the PUT path for all future saves in this session.
            if (_mdIsNew) {
                _mdIsNew   = false;
                _mdFileUrl = json.data?.url || null;
                // Re-render the breadcrumb to remove the "unsaved" badge.
                const badge = _root?.querySelector('[style*="unsaved"]');
                if (badge) badge.remove();
            }
            const st = document.getElementById('proj-md-save-status');
            if (st) {
                // The server returns a "warning" field when the description was
                // truncated to stay within the configured limit. Surface this to
                // the user in the save status bar so they know what happened.
                if (json.data?.warning) {
                    st.innerHTML = `<span style="color:var(--warning)">
                        <i class="fa-solid fa-triangle-exclamation"></i>
                        Saved — ${esc(json.data.warning)}
                    </span>`;
                    setTimeout(() => { st.textContent = ''; }, 6000);
                } else {
                    st.textContent = '✓ Saved';
                    setTimeout(() => { st.textContent = ''; }, 3000);
                }
            }
            await loadProjectFiles(projectId);
        } else {
            alert('Save failed: ' + (json?.metadata?.error || 'unknown error'));
        }
    } catch(e) {
        alert('Save failed: ' + e.message);
    } finally {
        if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Save'; }
    }
}

// projMarkdownClose returns to the project tree, cleaning up all markdown editor state.
export function projMarkdownClose() {
    if (_mdInst) { _mdInst.dispose(); _mdInst = null; }
    if (_mdSlashDisposable) {
        try { _mdSlashDisposable.dispose(); } catch(e) {}
        _mdSlashDisposable = null;
    }
    _mdProjectId  = null;
    _mdFilename   = null;
    _mdFileUrl    = null;
    _mdIsNew      = false;
    _mdSlashPos   = null;
    _readmeConfig = null;
    renderProjects(_root);
}

// ─── Markdown editor slash menu ───────────────────────────────────────────────
//
// A slash menu for the Markdown Monaco editor, analogous to the one in the
// Go code editor but simpler: only /image is offered because we are already
// writing Markdown (no need for a /markdown manual-block option).
//
// Detection and replacement use _mdSlashPos / _mdInst instead of the Go
// editor's _slashPos / _monacoInst so the two menus never interfere.

// _mdRegisterSlashMenu registers the onDidChangeModelContent listener that
// watches for "/" keystrokes in the Markdown Monaco editor.
// Must be called after _mdInst is created inside _mountMarkdownMonaco.
function _mdRegisterSlashMenu() {
    if (_mdSlashDisposable) {
        try { _mdSlashDisposable.dispose(); } catch(e) {}
        _mdSlashDisposable = null;
    }
    if (!_mdInst) return;

    _mdSlashDisposable = _mdInst.onDidChangeModelContent(function(e) {
        if (e.changes.length !== 1 || e.changes[0].text !== '/') return;
        const pos   = _mdInst.getPosition();
        // Store the position of the '/' (cursor is one column ahead after insertion).
        _mdSlashPos = { lineNumber: pos.lineNumber, column: pos.column - 1 };
        _mdShowSlashMenu(pos);
    });
}

// _mdShowSlashMenu renders the floating slash menu for the Markdown editor.
//
// Items shown depend on whether the file being edited is readme.md:
//   Always:        /image   — insert an uploaded image
//   readme.md only: /cover   — set the card cover image (same as /image but
//                              inserts into the frontmatter image: field)
//                   /category     — pick from the taxonomy
//                   /subcategory  — pick from subcategories of selected category
function _mdShowSlashMenu(cursorPos) {
    _mdCloseMdSlashMenu();
    const editorDom = _mdInst && _mdInst.getDomNode();
    if (!editorDom) return;
    const pixelPos = _mdInst.getScrolledVisiblePosition(cursorPos);
    if (!pixelPos) return;

    const editorRect = editorDom.getBoundingClientRect();
    const cursorTop  = editorRect.top  + pixelPos.top;
    const menuLeft   = editorRect.left + pixelPos.left;
    const spaceBelow = window.innerHeight - cursorTop - 22;
    const menuTop    = spaceBelow >= 120 ? cursorTop + 22 : cursorTop - 4;
    const flipUp     = spaceBelow < 120;

    const isReadme = (_mdFilename === 'readme.md');

    // Build the menu item list. readme.md gets the full taxonomy options;
    // other markdown files only get the generic /image option.
    const items = [
        { id: 'image',       icon: 'fa-solid fa-image',      label: 'Image',       hint: 'Insert uploaded image' },
    ];
    if (isReadme) {
        items.push(
            { id: 'cover',       icon: 'fa-solid fa-star',       label: 'Cover',       hint: 'Set card cover image' },
            { id: 'category',    icon: 'fa-solid fa-tag',         label: 'Category',    hint: 'Set component category' },
            { id: 'subcategory', icon: 'fa-solid fa-tags',        label: 'Subcategory', hint: 'Set component subcategory' },
        );
    }

    const menu = document.createElement('div');
    menu.id = 'proj-md-slash-menu';
    menu.style.cssText =
        'position:fixed;z-index:9999;background:var(--bg-card);' +
        'border:1px solid var(--border);border-radius:var(--r);' +
        'box-shadow:0 4px 20px rgba(0,0,0,.2);min-width:220px;overflow:hidden;' +
        'font-size:13px;font-family:var(--font);' +
        'top:' + menuTop + 'px;left:' + menuLeft + 'px;' +
        (flipUp ? 'transform:translateY(-100%)' : '');

    menu.innerHTML = items.map(it =>
        '<div class="proj-md-slash-item"' +
        ' style="display:flex;align-items:center;gap:10px;padding:9px 14px;' +
        'cursor:pointer;transition:background .1s;border-bottom:1px solid var(--border)"' +
        ' onmouseover="this.style.background=\'var(--info-bg)\'"' +
        ' onmouseout="this.style.background=\'\'"' +
        ' onclick="projMdSlashPick(\'' + it.id + '\')">' +
        '<i class="' + it.icon + '" style="color:var(--primary);width:16px;text-align:center"></i>' +
        '<div>' +
        '<div style="font-weight:600;color:var(--text-primary)">' + it.label + '</div>' +
        '<div style="font-size:11px;color:var(--text-muted)">' + it.hint + '</div>' +
        '</div></div>'
    ).join('');

    document.body.appendChild(menu);

    const escH = function(e) {
        if (e.key === 'Escape') { _mdCloseMdSlashMenu(); document.removeEventListener('keydown', escH); }
    };
    document.addEventListener('keydown', escH);
    menu._escHandler = escH;

    setTimeout(function() {
        const outH = function(e) {
            if (!menu.contains(e.target)) {
                _mdCloseMdSlashMenu();
                document.removeEventListener('mousedown', outH);
            }
        };
        document.addEventListener('mousedown', outH);
        menu._outsideHandler = outH;
    }, 0);
}

// _mdCloseMdSlashMenu removes the Markdown slash menu from the DOM.
function _mdCloseMdSlashMenu() {
    const menu = document.getElementById('proj-md-slash-menu');
    if (!menu) return;
    if (menu._escHandler)     document.removeEventListener('keydown',   menu._escHandler);
    if (menu._outsideHandler) document.removeEventListener('mousedown', menu._outsideHandler);
    menu.remove();
    // Also close any open file picker spawned by the menu.
    document.getElementById('proj-slash-picker')?.remove();
}

// projMdSlashPick handles the selection of a slash menu item in the Markdown editor.
export async function projMdSlashPick(category) {
    _mdCloseMdSlashMenu();
    if (category === 'image') {
        await _mdSlashPickImage();
    } else if (category === 'cover') {
        await _mdSlashPickCover();
    } else if (category === 'category') {
        _mdSlashPickCategory();
    } else if (category === 'subcategory') {
        _mdSlashPickSubcategory();
    }
}

// _mdSlashPickImage loads the project's image list and shows the file picker.
// On selection it replaces the '/' with a Markdown image expression.
async function _mdSlashPickImage() {
    const projectId = _mdProjectId;
    if (!projectId) return;
    if (!_projectFiles[projectId]) await loadProjectFiles(projectId);

    const images = (_projectFiles[projectId] && _projectFiles[projectId].img) || [];
    if (!images.length) {
        // Inform the user through the status bar (mirroring Go editor behaviour).
        const st = document.getElementById('proj-md-status');
        if (st) {
            const prev = st.textContent;
            st.textContent = 'No images uploaded for this project.';
            setTimeout(function() { st.textContent = prev; }, 3000);
        }
        return;
    }

    _showFilePicker(
        images.map(function(f) { return { label: f.name, detail: f.url, data: f }; }),
        'Insert image',
        'fa-solid fa-image',
        function(f) {
            const baseName = f.name.replace(/\.[^.]+$/, '');
            _mdReplaceSlash('![' + baseName + '](' + f.url + ')');
        },
        _mdInst
    );
}

// _mdContextInsertImage is the right-click context-menu counterpart of the
// "/image" slash command (_mdSlashPickImage). It shows the SAME uploaded-image
// picker (_showFilePicker) but, because there is no triggering "/" to remove,
// inserts the Markdown image tag at the current cursor/selection via
// _mdInsertAtCursor. The slash flow is left untouched — this is an additional,
// more discoverable entry point, not a replacement.
async function _mdContextInsertImage() {
    const projectId = _mdProjectId;
    if (!projectId) return;
    if (!_projectFiles[projectId]) await loadProjectFiles(projectId);

    const images = (_projectFiles[projectId] && _projectFiles[projectId].img) || [];
    if (!images.length) {
        _mdStatusMessage('No images uploaded for this project.');
        return;
    }

    _showFilePicker(
        images.map(function(f) { return { label: f.name, detail: f.url, data: f }; }),
        'Insert image',
        'fa-solid fa-image',
        function(f) {
            const baseName = f.name.replace(/\.[^.]+$/, '');
            _mdInsertAtCursor('![' + baseName + '](' + f.url + ')');
        },
        _mdInst
    );
}

// _mdInsertAtCursor writes text at the editor's current selection (or caret,
// when the selection is empty), then refocuses the editor. Used by the
// context-menu image insertion. The slash flow uses _mdReplaceSlash instead,
// because that path must also delete the triggering "/".
function _mdInsertAtCursor(text) {
    if (!_mdInst) return;
    const sel = _mdInst.getSelection();
    _mdInst.executeEdits('md-insert-at-cursor', [
        { range: sel, text: text, forceMoveMarkers: true },
    ]);
    _mdInst.focus();
}

// _mdSlashPickCover shows the image picker and writes the selected URL into
// the frontmatter "image:" field instead of inserting a Markdown image tag.
//
// Strategy: scan the current editor content for "image:" in the frontmatter.
// If found, replace the entire line with the new value. If not found, insert
// a new "image:" line after the first "---" delimiter. Either way, the "/"
// is removed from its original position (like all slash menu insertions).
async function _mdSlashPickCover() {
    const projectId = _mdProjectId;
    if (!projectId) return;
    if (!_projectFiles[projectId]) await loadProjectFiles(projectId);

    const images = (_projectFiles[projectId] && _projectFiles[projectId].img) || [];
    if (!images.length) {
        _mdStatusMessage('No images uploaded for this project.');
        return;
    }

    _showFilePicker(
        images.map(function(f) { return { label: f.name, detail: f.url, data: f }; }),
        'Set cover image',
        'fa-solid fa-star',
        function(f) {
            // Remove the triggering "/" first.
            const sp = _mdSlashPos;
            _mdSlashPos = null;
            if (sp) {
                const range = new monaco.Range(sp.lineNumber, sp.column, sp.lineNumber, sp.column + 1);
                _mdInst.executeEdits('md-cover-remove-slash', [{ range, text: '', forceMoveMarkers: true }]);
            }
            // Now update or insert the "image:" frontmatter field.
            _setFrontmatterField('image', f.url);
        },
        _mdInst
    );
}

// _mdSlashPickCategory shows a list of all top-level categories from _readmeConfig.
// On selection, it writes the chosen category name into the "category:" frontmatter
// field and clears "subcategory:" to avoid stale data.
function _mdSlashPickCategory() {
    if (!_readmeConfig?.categories?.length) {
        _mdStatusMessage('Category list not loaded. Try re-opening the file.');
        return;
    }

    _showFilePicker(
        _readmeConfig.categories.map(function(c) {
            return { label: c.name, detail: c.id, data: c };
        }),
        'Set category',
        'fa-solid fa-tag',
        function(cat) {
            const sp = _mdSlashPos;
            _mdSlashPos = null;
            if (sp) {
                const range = new monaco.Range(sp.lineNumber, sp.column, sp.lineNumber, sp.column + 1);
                _mdInst.executeEdits('md-cat-remove-slash', [{ range, text: '', forceMoveMarkers: true }]);
            }
            _setFrontmatterField('category', cat.name);
            // Clear subcategory when category changes to prevent invalid combinations.
            _setFrontmatterField('subcategory', '');
        },
        _mdInst
    );
}

// _mdSlashPickSubcategory shows subcategories filtered by the category currently
// set in the "category:" frontmatter field. If no category is selected yet, it
// shows all subcategories with a hint to set a category first.
function _mdSlashPickSubcategory() {
    if (!_readmeConfig?.subcategories?.length) {
        _mdStatusMessage('Subcategory list not loaded. Try re-opening the file.');
        return;
    }

    // Read the current "category:" value from the editor content.
    const currentCatName = _readFrontmatterField('category');
    let subs = _readmeConfig.subcategories;

    if (currentCatName) {
        const cat = _readmeConfig.categories.find(function(c) {
            return c.name === currentCatName;
        });
        if (cat) {
            subs = subs.filter(function(s) { return s.categoryId === cat.id; });
        }
    }

    const hint = currentCatName ? '' : '(set a category first for filtered results)';

    _showFilePicker(
        subs.map(function(s) {
            return { label: s.name, detail: hint || s.categoryId, data: s };
        }),
        'Set subcategory',
        'fa-solid fa-tags',
        function(sub) {
            const sp = _mdSlashPos;
            _mdSlashPos = null;
            if (sp) {
                const range = new monaco.Range(sp.lineNumber, sp.column, sp.lineNumber, sp.column + 1);
                _mdInst.executeEdits('md-subcat-remove-slash', [{ range, text: '', forceMoveMarkers: true }]);
            }
            _setFrontmatterField('subcategory', sub.name);
        },
        _mdInst
    );
}

// _setFrontmatterField finds "key: ..." in the YAML front-matter block and
// replaces the value on that line. If the key is not present, it inserts a
// new line immediately after the opening "---" delimiter.
//
// This function uses Monaco's executeEdits so the change is undoable and
// the editor's undo stack remains intact.
function _setFrontmatterField(key, value) {
    if (!_mdInst) return;
    const model = _mdInst.getModel();
    if (!model) return;

    const totalLines = model.getLineCount();
    const prefix = key + ':';

    // Search for the key inside the frontmatter block (between the first two "---").
    let inFrontmatter = false;
    let frontmatterEnd = -1;
    let targetLine = -1;
    let insertAfterLine = -1; // line number of the opening "---"

    for (let i = 1; i <= totalLines; i++) {
        const line = model.getLineContent(i).trim();
        if (i === 1 && line === '---') {
            inFrontmatter = true;
            insertAfterLine = i;
            continue;
        }
        if (inFrontmatter && line === '---') {
            frontmatterEnd = i;
            break;
        }
        if (inFrontmatter && line.toLowerCase().startsWith(prefix.toLowerCase())) {
            targetLine = i;
        }
    }

    if (targetLine > 0) {
        // Replace the existing line.
        const fullLine = model.getLineContent(targetLine);
        const colonIdx = fullLine.indexOf(':');
        const newLine = fullLine.substring(0, colonIdx + 1) + (value ? ' ' + value : '');
        const range = new monaco.Range(targetLine, 1, targetLine, fullLine.length + 1);
        _mdInst.executeEdits('fm-set-field', [{ range, text: newLine, forceMoveMarkers: true }]);
    } else if (insertAfterLine > 0 && value) {
        // Insert a new line after the opening "---".
        const newLine = key + ': ' + value + '\n';
        const range = new monaco.Range(insertAfterLine + 1, 1, insertAfterLine + 1, 1);
        _mdInst.executeEdits('fm-insert-field', [{ range, text: newLine, forceMoveMarkers: true }]);
    }
    _mdInst.focus();
}

// _readFrontmatterField reads the current value of a YAML frontmatter field
// directly from the Monaco model content, without a round-trip to the server.
// Returns an empty string if the field is not present or has no value.
function _readFrontmatterField(key) {
    if (!_mdInst) return '';
    const model = _mdInst.getModel();
    if (!model) return '';

    const totalLines = model.getLineCount();
    const prefix = key.toLowerCase() + ':';

    let inFrontmatter = false;
    for (let i = 1; i <= totalLines; i++) {
        const line = model.getLineContent(i);
        const trimmed = line.trim();
        if (i === 1 && trimmed === '---') { inFrontmatter = true; continue; }
        if (inFrontmatter && trimmed === '---') break;
        if (inFrontmatter && trimmed.toLowerCase().startsWith(prefix)) {
            const colonIdx = trimmed.indexOf(':');
            return trimmed.substring(colonIdx + 1).trim();
        }
    }
    return '';
}

// _updateDescriptionCounter reads the "description:" frontmatter field from
// the current editor content and updates the status bar with a character count.
// The max limit is taken from _readmeConfig (falling back to 500).
function _updateDescriptionCounter(content) {
    const st = document.getElementById('proj-md-status');
    if (!st) return;
    const fm = _parseFrontmatterJS(content);
    const desc   = fm['description'] || '';
    const maxChars = _readmeConfig?.descriptionMaxChars || 500;
    const len    = [...desc].length; // Unicode-aware length
    const over   = len > maxChars;
    st.innerHTML =
        '<span style="font-size:12px">' + esc(_mdFilename || '') + '</span>' +
        ' &nbsp;·&nbsp; ' +
        '<span style="font-size:12px;color:' + (over ? 'var(--danger)' : 'var(--text-muted)') + '">' +
        'description: ' + len + '\u202f/\u202f' + maxChars +
        (over ? ' <strong>⚠ will be truncated</strong>' : '') +
        '</span>';
}

// _parseFrontmatterJS is a lightweight JS mirror of the Go parseFrontmatter
// function. It extracts key→value pairs from the YAML front-matter block so
// the client can compute things like the description character count without
// a server round-trip.
function _parseFrontmatterJS(content) {
    const result = {};
    if (!content.startsWith('---')) return result;

    let rest = content.slice(3);
    if (rest.startsWith('\r\n')) rest = rest.slice(2);
    else if (rest.startsWith('\n')) rest = rest.slice(1);

    const endIdx = rest.indexOf('\n---');
    if (endIdx < 0) return result;

    const block = rest.slice(0, endIdx);
    for (const line of block.split('\n')) {
        const t = line.trim();
        if (!t || t.startsWith('#')) continue;
        const ci = t.indexOf(':');
        if (ci < 0) continue;
        result[t.slice(0, ci).trim().toLowerCase()] = t.slice(ci + 1).trim();
    }
    return result;
}

// _mdStatusMessage briefly displays a message in the markdown editor status bar.
function _mdStatusMessage(msg) {
    const st = document.getElementById('proj-md-status');
    if (!st) return;
    const prev = st.innerHTML;
    st.innerHTML = '<span style="color:var(--warning)">' + esc(msg) + '</span>';
    setTimeout(function() { st.innerHTML = prev; }, 3000);
}

// _mdReplaceSlash replaces the '/' character in the Markdown editor with text.
// Uses _mdSlashPos captured at detection time, same pattern as _replaceSlash.
function _mdReplaceSlash(text) {
    if (!_mdInst) return;
    const sp = _mdSlashPos;
    _mdSlashPos = null;
    if (!sp) { _insertAtCursor(_mdInst, text); return; }
    const range = new monaco.Range(sp.lineNumber, sp.column, sp.lineNumber, sp.column + 1);
    _mdInst.executeEdits('md-slash-replace', [{ range, text, forceMoveMarkers: true }]);
    _mdInst.focus();
}

// _buildReadmeCardPreview renders a live feed-card preview from the readme.md
// frontmatter values. Shown in the Preview tab when editing readme.md so the
// author sees exactly how the component will appear in the marketplace feed.
//
// Layout: the preview is split into two columns —
//   Left:  the feed card as users will see it + a frontmatter summary table
//   Right: the full document body rendered as standard Markdown
//
// This two-column layout lets the author monitor the card while also reading
// and editing the documentation body on the same screen.
function _buildReadmeCardPreview(content) {
    const fm = _parseFrontmatterJS(content);

    const title       = fm['title']       || '(no title)';
    const image       = fm['image']       || '';
    const description = fm['description'] || '';
    const keywords    = fm['keywords']    || '';
    const category    = fm['category']    || '';
    const subcategory = fm['subcategory'] || '';

    const maxChars  = _readmeConfig?.descriptionMaxChars || 500;
    const descLen   = [...description].length;
    const overLimit = descLen > maxChars;

    // Truncate the preview the same way the server will truncate on save.
    const previewDesc = overLimit
        ? [...description].slice(0, maxChars).join('') + '…'
        : description;

    const imgHtml = image
        ? `<img src="${esc(image)}" alt="${esc(title)}"
               style="width:100%;height:180px;object-fit:cover;display:block;
                      border-radius:var(--r) var(--r) 0 0"
               onerror="this.style.display='none'">`
        : `<div style="width:100%;height:180px;background:var(--bg-surface);
                      border-radius:var(--r) var(--r) 0 0;
                      display:flex;align-items:center;justify-content:center;
                      color:var(--text-muted)">
             <i class="fa-solid fa-image" style="font-size:28px;opacity:.25"></i>
           </div>`;

    const catBadge = category
        ? `<span style="font-size:11px;font-weight:600;background:var(--info-bg);
                        color:var(--primary);border:1px solid var(--primary);
                        border-radius:99px;padding:2px 9px;white-space:nowrap">
             ${esc(category)}${subcategory ? ' › ' + esc(subcategory) : ''}
           </span>`
        : '';

    const kwList = keywords
        ? keywords.split(',').map(k => k.trim()).filter(Boolean).map(k =>
            `<span style="font-size:11px;background:var(--bg-surface);
                          border:1px solid var(--border);border-radius:99px;
                          padding:2px 8px;color:var(--text-secondary)">${esc(k)}</span>`
        ).join(' ')
        : '';

    const descWarning = overLimit
        ? `<div style="margin-top:8px;padding:6px 10px;background:#FEE2E2;
                       border-radius:var(--r);font-size:12px;color:#991B1B">
             ⚠ Description exceeds ${maxChars} characters — will be truncated on save.
           </div>`
        : '';

    // Body: everything after the closing "---" of the frontmatter block.
    let bodyContent = content;
    const endFm = content.indexOf('\n---', 3);
    if (endFm > 0) bodyContent = content.slice(endFm + 4).replace(/^\n/, '');

    return `
<div style="display:flex;gap:40px;align-items:flex-start;flex-wrap:wrap">

  <!-- ── Left: card + frontmatter summary ── -->
  <div style="flex-shrink:0">
    <p style="font-size:11px;font-weight:700;text-transform:uppercase;
              letter-spacing:.06em;color:var(--text-muted);margin:0 0 10px">
      <i class="fa-solid fa-eye" style="margin-right:4px"></i>Feed card preview
    </p>

    <div style="border:1px solid var(--border);border-radius:var(--r);
                overflow:hidden;background:var(--bg-card);box-shadow:var(--shh);
                width:340px">
      ${imgHtml}
      <div style="padding:14px 16px">
        <div style="display:flex;align-items:flex-start;gap:8px;margin-bottom:8px;flex-wrap:wrap">
          <span style="font-size:15px;font-weight:700;color:var(--text-primary);
                       flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
            ${esc(title)}
          </span>
          ${catBadge}
        </div>
        <p style="font-size:13px;color:var(--text-secondary);margin:0 0 10px;line-height:1.5;
                  display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden">
          ${esc(previewDesc) || '<em style="opacity:.5">No description yet</em>'}
        </p>
        ${descWarning}
        ${kwList
        ? `<div style="display:flex;flex-wrap:wrap;gap:4px;margin-top:8px">${kwList}</div>`
        : ''}
      </div>
    </div>

    <!-- Frontmatter summary table -->
    <div style="margin-top:16px;font-size:12px;font-family:var(--mono);
                background:var(--bg-surface);border:1px solid var(--border);
                border-radius:var(--r);padding:10px 14px;width:340px">
      <div style="font-weight:700;color:var(--text-muted);margin-bottom:6px;
                  font-family:var(--font);font-size:11px;text-transform:uppercase;
                  letter-spacing:.05em">Frontmatter fields</div>
      ${_fmRow('title',       title,       !!fm['title'])}
      ${_fmRow('image',       image,       !!image)}
      ${_fmRow('description', description, !!description)}
      ${_fmRow('keywords',    keywords,    !!keywords)}
      ${_fmRow('category',    category,    !!category)}
      ${_fmRow('subcategory', subcategory, !!subcategory)}
    </div>
  </div>

  <!-- ── Right: document body ── -->
  <div style="flex:1;min-width:280px">
    <p style="font-size:11px;font-weight:700;text-transform:uppercase;
              letter-spacing:.06em;color:var(--text-muted);margin:0 0 10px">
      <i class="fa-solid fa-file-lines" style="margin-right:4px"></i>Document body
    </p>
    <div id="proj-md-preview-body">${_mdToHtml(bodyContent)}</div>
  </div>

</div>`;
}

// _fmRow renders one row in the frontmatter summary table.
// filled=true shows the value in primary colour; false shows a grey placeholder.
function _fmRow(key, value, filled) {
    const displayVal = filled
        ? `<span style="color:var(--primary);overflow:hidden;text-overflow:ellipsis;
                        white-space:nowrap;max-width:200px;display:inline-block;
                        vertical-align:bottom">${esc(value)}</span>`
        : `<span style="color:var(--text-muted);font-style:italic">not set</span>`;
    return `<div style="display:flex;gap:8px;padding:3px 0;
                         border-bottom:1px solid var(--border);align-items:center">
      <span style="color:var(--text-muted);min-width:90px;flex-shrink:0">${esc(key)}:</span>
      ${displayVal}
    </div>`;
}

// _mdToHtml — minimal Markdown → HTML renderer for the preview pane.
// Handles headings, bold, italic, inline code, code blocks, tables, lists, hr.
function _mdToHtml(md) {
    const escape = function(s) {
        return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    };
    const inline = function(s) {
        return s
            .replace(/`([^`]+)`/g,         function(_,c){ return '<code>' + escape(c) + '</code>'; })
            .replace(/\*\*([^*]+)\*\*/g,   function(_,c){ return '<strong>' + c + '</strong>'; })
            .replace(/\*([^*]+)\*/g,        function(_,c){ return '<em>' + c + '</em>'; })
            .replace(/\[([^\]]+)\]\(([^)]+)\)/g, function(_,t,u){ return '<a href="'+escape(u)+'" target="_blank">'+t+'</a>'; });
    };

    const lines  = md.split('\n');
    let html     = '';
    let inPre    = false, preBuf = '';
    let inTable  = false, inList = false;

    const flushList  = function() { if (inList)  { html += '</ul>';           inList  = false; } };
    const flushTable = function() { if (inTable) { html += '</tbody></table>'; inTable = false; } };

    lines.forEach(function(raw) {
        if (raw.startsWith('```')) {
            if (!inPre) { flushList(); flushTable(); inPre = true; preBuf = ''; }
            else { html += '<pre><code>' + escape(preBuf.replace(/\n$/,'')) + '</code></pre>'; inPre = false; }
            return;
        }
        if (inPre) { preBuf += raw + '\n'; return; }
        if (/^---+$/.test(raw.trim())) { flushList(); flushTable(); html += '<hr>'; return; }

        const hm = raw.match(/^(#{1,3})\s+(.*)/);
        if (hm) { flushList(); flushTable(); html += '<h'+hm[1].length+'>'+inline(escape(hm[2]))+'</h'+hm[1].length+'>'; return; }

        if (raw.startsWith('|')) {
            flushList();
            const cells = raw.split('|').slice(1,-1).map(function(c){ return c.trim(); });
            if (cells.every(function(c){ return /^[-:]+$/.test(c); })) return;
            if (!inTable) { html += '<table><thead><tr>'+cells.map(function(c){ return '<th>'+inline(escape(c))+'</th>'; }).join('')+'</tr></thead><tbody>'; inTable = true; }
            else          { html += '<tr>'+cells.map(function(c){ return '<td>'+inline(escape(c))+'</td>'; }).join('')+'</tr>'; }
            return;
        }
        if (inTable) flushTable();

        const lm = raw.match(/^[-*]\s+(.*)/);
        if (lm) { if (!inList) { html += '<ul>'; inList = true; } html += '<li>'+inline(escape(lm[1]))+'</li>'; return; }
        flushList();

        if (raw.trim() === '') return;
        html += '<p>' + inline(escape(raw)) + '</p>';
    });

    flushList(); flushTable();
    if (inPre) html += '<pre><code>' + escape(preBuf) + '</code></pre>';

    return '<style>' +
        '#proj-md-preview h1{font-size:22px;font-weight:800;color:var(--primary);margin:0 0 20px}' +
        '#proj-md-preview h2{font-size:16px;font-weight:700;margin:24px 0 10px;padding-bottom:6px;border-bottom:1px solid var(--border)}' +
        '#proj-md-preview h3{font-size:14px;font-weight:700;margin:18px 0 8px}' +
        '#proj-md-preview p{margin:0 0 12px;color:var(--text-secondary)}' +
        '#proj-md-preview ul{padding-left:22px;margin:0 0 12px}' +
        '#proj-md-preview li{margin-bottom:4px;color:var(--text-secondary)}' +
        '#proj-md-preview code{font-family:monospace;font-size:12px;background:var(--bg-surface);padding:2px 6px;border-radius:4px;color:var(--primary)}' +
        '#proj-md-preview pre{background:var(--bg-surface);border:1px solid var(--border);border-radius:8px;padding:16px;overflow-x:auto;margin:0 0 16px}' +
        '#proj-md-preview pre code{background:none;padding:0;font-size:13px}' +
        '#proj-md-preview table{border-collapse:collapse;width:100%;margin:0 0 16px;font-size:13px}' +
        '#proj-md-preview th{background:var(--bg-surface);font-weight:700;padding:8px 12px;text-align:left;border:1px solid var(--border)}' +
        '#proj-md-preview td{padding:7px 12px;border:1px solid var(--border);color:var(--text-secondary)}' +
        '#proj-md-preview hr{border:none;border-top:1px solid var(--border);margin:20px 0}' +
        '#proj-md-preview a{color:var(--primary);text-decoration:underline}' +
        '#proj-md-preview img{max-width:100%;height:auto;display:block;border-radius:var(--r)}' +
        '</style>' + html;
}

// ─── Editor helpers ──────────────────────────────────────────────────────────

// slugifyFilename converts a project name to a valid Go filename.
// "My Sensor Board" → "my_sensor_board"
// " IR-Blaster (v2)!" → "ir_blaster_v2"
function slugifyFilename(name) {
    return (name || 'main')
            .toLowerCase()
            .replace(/[^a-z0-9]+/g, '_')  // non-alphanumeric → underscore
            .replace(/^_+|_+$/g, '')       // trim leading/trailing underscores
        || 'main';
}

// projToggleLiveAnalysis is called by the Live analysis checkbox.
export function projToggleLiveAnalysis(enabled) {
    _realtimeAnalysis = enabled;
    if (!enabled) {
        clearTimeout(_analyzeTimer);
        _analyzeSeq++;
    }
}

// projCreateFile opens the Monaco Go editor for a new (unsaved) file.
// The file is pre-filled with an empty editor; the filename is derived from
// the project name. Nothing is saved until the user clicks Save.
export async function projCreateFile(projectId) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    _editProject = p;
    _editMode    = true;
    _parsedData  = null;
    _parsedSnapshotFP = null;
    _needsExplicitParse = false;
    _parseStatusType    = '';
    _codeVersions       = [];
    _diffHunks          = [];
    _diffChoices        = [];
    _currentVersion     = 0;
    _nextVersion        = 1;
    _defaultFilename    = slugifyFilename(p.name) + _codeExtFor(p.id);

    // Install navigation guards (same pattern as openCodeEditor).
    _installEditorNavGuards();

    renderEditorView(_root);

    if (!_projectFiles[projectId]) {
        await loadProjectFiles(projectId);
    }

    // Fresh project: the working copy is born with ONE empty tab named
    // from the project's name and language — same rule as opening an
    // empty project through openCodeEditor.
    projMountMonaco([{ path: _defaultFilename, content: '' }]);
    projRegisterSlashMenu(projectId);
    updateVersionBar();
    // Fresh project — Parse stays disabled until the user opens
    // the Wizard tab. Per the rule "new project → user must
    // visit wizard first", there is no source to silent-reparse
    // against, so the only way out of 'pending' is the wizard
    // tab activating its own verification.
    projUpdateParseBtnState('pending');
}

// buildNewFileTemplate returns the default Go source for a new black-box file.
function buildNewFileTemplate(structName) {
    return `// Package blackbox
//
// ${structName} — describe your device here.
package blackbox

import "machine"

// ${structName} is a sample device connected via I2C.
type ${structName} struct {
\ti2c   *machine.I2C
\tspeed byte \`setting:"Speed" default:"100" options:"50,100,200"\`
}

// Init configures the device.
//
// Params
//   i2c: reference to an I2C bus.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: initialization error.  connection:optional.
func (d *${structName}) Init(i2c *machine.I2C) (err error) {
\td.i2c = i2c
\treturn nil
}

// Run reads sensor data.
//
// Returns
//   value: measured value.  range:0..4095.  unit:adc_counts.  connection:optional.
func (d *${structName}) Run() (value uint16) {
\treturn 0
}
`;
}

// ─── Utilities ────────────────────────────────────────────────────────────────

function esc(str) {
    if (!str) return '';
    return String(str)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;')
        .replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function formatBytes(bytes) {
    if (bytes < 1024)      return `${bytes} B`;
    if (bytes < 1048576)   return `${(bytes/1024).toFixed(1)} KB`;
    return `${(bytes/1048576).toFixed(1)} MB`;
}

function langIcon(name) {
    const m = { golang: 'fa-brands fa-golang', c: 'fa-solid fa-c', cpp: 'fa-solid fa-c' };
    return m[name] || 'fa-solid fa-code';
}

// ═══════════════════════════════════════════════════════════════════════════════
// ─── TEMPLATES TAB ────────────────────────────────────────────────────────────
// ═══════════════════════════════════════════════════════════════════════════════
//
// All functions in this section are prefixed with _tpl to distinguish them from
// the projects functions above. They operate on _templates, _tplPollTimers, and
// _tplUploading module state defined near the top of this file.
//
// Template creation uses the same "New Project" modal (type=custom_project).
// The modal posts to POST /api/v1/templates which creates the parent record with
// status=no_version. The specialist then uploads ZIP versions from this tab.

// ── Styles ────────────────────────────────────────────────────────────────────

function _tplBuildStyles() {
    return `<style>
/* ── New-Project dropdown ──
   Wraps the "+ New Project" button so the dropdown menu can be
   absolutely positioned relative to it without polluting the
   surrounding flex layout. */
.proj-newbtn-wrap { position:relative; display:inline-block }
.proj-newbtn-menu {
  position:absolute; top:calc(100% + 6px); right:0;
  background:var(--bg-card); border:1px solid var(--border);
  border-radius:var(--rl); box-shadow:var(--shh);
  min-width:280px; padding:6px; z-index:100;
  display:none;
}
.proj-newbtn-menu.open { display:block; animation:fi .12s ease-out }
.proj-newbtn-item {
  width:100%; display:flex; gap:10px; align-items:flex-start;
  padding:10px 12px; border:none; background:none; border-radius:var(--r);
  text-align:left; cursor:pointer; color:var(--text-primary);
  font-family:var(--font); font-size:14px;
  transition:background .12s ease;
}
.proj-newbtn-item:hover { background:var(--bg-hover, var(--bg-page)) }
.proj-newbtn-item i { margin-top:2px; flex-shrink:0; width:18px; text-align:center }
.proj-newbtn-item:focus-visible { outline:2px solid var(--primary); outline-offset:-2px }

/* Wizard-create modal — narrow card with just a name field. The
   shared overlay div uses the same z-index strategy as the legacy
   create modal; the inner card is sized smaller to signal "this is
   a quick action". */
.proj-wiz-create-modal {
  background:var(--bg-card); border-radius:var(--rl); padding:28px;
  width:100%; max-width:440px; box-shadow:var(--shh);
  border:1px solid var(--border); animation:fi .2s ease;
}

/* ── Template card ── */
.tpl-card {
  background:var(--bg-card);border:1px solid var(--border);border-radius:var(--rl);
  padding:20px 24px;display:flex;flex-direction:column;gap:12px;
  box-shadow:var(--sh);transition:box-shadow .15s;margin-bottom:16px;
}
.tpl-card:hover { box-shadow:var(--shh) }

/* ── Status badge ── */
.tpl-status {
  display:inline-flex;align-items:center;gap:5px;font-size:12px;font-weight:700;
  padding:3px 10px;border-radius:99px;
}
.tpl-status.no_version { background:#F1F5F9;color:#64748B;border:1px solid #CBD5E1 }
.tpl-status.pending    { background:#FFF8E1;color:#B7770D;border:1px solid #FFD97D }
.tpl-status.ready      { background:#F0FDF4;color:#15803D;border:1px solid #86EFAC }
.tpl-status.error      { background:#FEF2F2;color:#B91C1C;border:1px solid #FCA5A5 }

/* ── Warnings accordion ── */
.tpl-warn-toggle {
  background:none;border:none;cursor:pointer;font-size:12px;color:var(--warning,#B7770D);
  padding:0;display:flex;align-items:center;gap:5px;font-family:var(--font);
}
.tpl-warn-body {
  margin-top:8px;padding:10px 14px;background:#FFF8E1;border:1px solid #FFD97D;
  border-radius:var(--r);font-size:12px;color:#7A5C00;line-height:1.7;
}
.tpl-warn-body ul { margin:0;padding-left:18px }

/* ── Device preview wrapper ── */
/* Uses existing proj-block / proj-pin / proj-badge styles from the editor.   */
/* The wrapper adds a subtle background to separate it from the card content. */
.tpl-preview-wrap {
  background:var(--bg-page,#f8f9fa);
  border:1px solid var(--border);
  border-radius:var(--r);
  padding:14px 16px;
}

/* ── Publishing flags section ── */
.tpl-publish-section {
  border:1px solid var(--border);border-radius:var(--r);padding:14px 16px;
  display:flex;flex-direction:column;gap:10px;
}
.tpl-publish-header {
  font-size:12px;font-weight:700;color:var(--text-secondary);
  display:flex;align-items:center;gap:6px;margin-bottom:2px;
}
.tpl-publish-row { display:flex;align-items:flex-start;gap:10px }
.tpl-publish-row input[type="checkbox"] { margin-top:2px;cursor:pointer;flex-shrink:0 }
.tpl-publish-row input[type="checkbox"]:disabled { cursor:not-allowed;opacity:.5 }
.tpl-publish-label { font-size:13px;font-weight:600;color:var(--text-primary);cursor:pointer }
.tpl-publish-label.disabled { color:var(--text-muted);cursor:not-allowed }
.tpl-publish-hint { font-size:11px;color:var(--text-muted);margin-top:1px }
.tpl-publish-note { font-size:11px;color:var(--text-muted);font-style:italic }

/* ── Empty state ── */
.tpl-empty {
  display:flex;flex-direction:column;align-items:center;justify-content:center;
  padding:60px 20px;color:var(--text-muted);text-align:center;gap:12px;
}
.tpl-empty i { font-size:40px;opacity:.2 }

/* Help panel CSS moved to _injectEditorStyles (shared with Devices & Templates Guide) */
</style>`;
}

// ── Data loading ──────────────────────────────────────────────────────────────

async function _tplLoadList() {
    const res = await api('GET', '/api/v1/templates?mine=true');
    if (res?.metadata?.status === 200) {
        _templates = res.data || [];
        // For ready templates without a cached def, fetch it so device preview works.
        _templates.forEach(t => {
            if (t.status === 'ready' && !t._def) _tplFetchDef(t.id);
        });
        // Start polls for pending templates.
        _templates.forEach(t => {
            if (t.status === 'pending') _tplStartPoll(t.id);
        });
    }
}

// ── Render list ───────────────────────────────────────────────────────────────

function _tplRenderList() {
    // Do not overwrite the help panel if it is currently open.
    if (_tplHelpOpen) return;
    const el = document.getElementById('tab-content-templates');
    if (!el) return;

    if (!_templates.length) {
        el.innerHTML = `
          <div style="display:flex;align-items:center;justify-content:space-between;
               margin-bottom:20px;padding-bottom:16px;border-bottom:1px solid var(--border)">
            <p style="color:var(--text-muted);font-size:13px;margin:0">
              Create a template with <strong>New Project → Custom Project</strong>,
              then upload a ZIP version below.
            </p>
          </div>
          <div class="tpl-empty">
            <i class="fa-solid fa-file-export"></i>
            <p style="font-size:15px;font-weight:600;margin:0">No templates yet</p>
            <p style="font-size:13px;margin:0">
              Click <strong>New Project</strong> and choose <strong>Custom Project</strong>.
            </p>
          </div>`;
        return;
    }

    el.innerHTML = `
      <div style="display:flex;align-items:center;justify-content:space-between;
           margin-bottom:20px;padding-bottom:16px;border-bottom:1px solid var(--border)">
        <p style="color:var(--text-muted);font-size:13px;margin:0">
          Upload ZIP versions to your templates. The latest version is always active.
        </p>
      </div>
      ${_templates.map(_tplBuildCard).join('')}`;
}

// ── Card builder ──────────────────────────────────────────────────────────────

function _tplBuildCard(t) {
    const badge   = _tplStatusBadge(t);
    const meta    = [];
    if (t.latestVersion > 0) meta.push(`<span class="tag">v${t.latestVersion}</span>`);
    if (t.visibility) meta.push(
        `<span class="tag">${t.visibility === 'public' ? '🌐 Public' : '🔒 Private'}</span>`
    );

    return `<div class="tpl-card" id="tpl-card-${esc(t.id)}">
  <div style="display:flex;align-items:flex-start;gap:12px;flex-wrap:wrap">
    <div style="flex:1;min-width:200px">
      <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
        <h3 style="font-size:16px;font-weight:700;margin:0;color:var(--text-primary)">
          ${esc(t.name || '(unnamed)')}
        </h3>
        ${badge}
      </div>
      ${t.description
        ? `<p style="font-size:13px;color:var(--text-muted);margin:4px 0 0">${esc(t.description)}</p>`
        : ''}
    </div>
    <div style="display:flex;align-items:center;gap:8px;flex-shrink:0">
      ${_tplUploadBtn(t)}
      ${_tplVisBtn(t)}
      <button class="btn btn-ghost btn-sm" style="color:var(--danger)"
              onclick="window._tplConfirmDelete('${esc(t.id)}','${esc(t.name||'')}')"
              title="Delete this template">
        <i class="fa-solid fa-trash"></i>
      </button>
    </div>
  </div>
  ${meta.length ? `<div style="display:flex;gap:6px;flex-wrap:wrap">${meta.join('')}</div>` : ''}
  ${_tplDevicePreview(t)}
  ${_tplBuildWarnings(t)}
  ${_tplBuildPublishSection(t)}
</div>`;
}

// ── Status badge ──────────────────────────────────────────────────────────────

function _tplStatusBadge(t) {
    switch (t.status) {
        case 'no_version':
            return `<span class="tpl-status no_version">
              <i class="fa-solid fa-circle-plus"></i> No version yet
            </span>`;
        case 'pending':
            return `<span class="tpl-status pending">
              <i class="fa-solid fa-circle-notch fa-spin"></i> Processing…
            </span>`;
        case 'ready':
            return `<span class="tpl-status ready">
              <i class="fa-solid fa-circle-check"></i> Ready
            </span>`;
        case 'error':
            return `<span class="tpl-status error">
              <i class="fa-solid fa-circle-xmark"></i> Error
            </span>`;
        default:
            return `<span class="tpl-status no_version">${esc(t.status)}</span>`;
    }
}

// ── GitHub submit form (replaces ZIP upload) ──────────────────────────────────

function _tplUploadBtn(t) {
    if (t.status === 'pending') return '';
    // Only show the update form after at least one version has been submitted.
    // The initial submission now happens in the New Project modal.
    if (t.status === 'no_version') return '';
    const label = 'Submit New Release';
    const hint  = 'Submit a newer tag to update this template.';
    const isUpdate = true; // always true here — no_version case handled above
    return '<div style="display:flex;flex-direction:column;gap:8px;padding:12px 14px;' +
        'background:var(--bg-2);border:1px solid var(--border);border-radius:var(--r)">' +
        '<div style="font-size:12px;font-weight:600;color:var(--text-secondary)">' +
        '<i class="fa-brands fa-github"></i> ' + label + '</div>' +
        '<div style="font-size:11px;color:var(--text-muted)">' + hint + '</div>' +
        '<div style="display:flex;gap:8px;flex-wrap:wrap">' +
        '<input class="fc" id="tpl-gh-url-' + esc(t.id) + '" type="url"' +
        ' placeholder="https://github.com/you/repo/releases/tag/v1.0"' +
        ' style="flex:2;min-width:220px;font-size:13px">' +
        '<input class="fc" id="tpl-gh-tags-' + esc(t.id) + '" type="text"' +
        ' placeholder="tags (optional)" style="flex:1;min-width:140px;font-size:13px">' +
        '<button class="btn btn-primary btn-sm" id="tpl-gh-btn-' + esc(t.id) + '"' +
        ' onclick="window._tplSubmitGithub(\'' + esc(t.id) + '\')">' +
        '<i class="fa-solid fa-cloud-arrow-up"></i> Submit</button></div>' +
        '<div id="tpl-gh-err-' + esc(t.id) + '" style="display:none;font-size:12px;color:var(--danger)"></div>' +
        '</div>';
}

// ── Visibility toggle ─────────────────────────────────────────────────────────

function _tplVisBtn(t) {
    if (t.status !== 'ready') return '';
    if (t.visibility === 'public') {
        return `<button class="btn btn-secondary btn-sm"
                        title="Make private"
                        onclick="window._tplSetVisibility('${esc(t.id)}','private')">
          <i class="fa-solid fa-lock"></i> Make Private
        </button>`;
    }
    return `<button class="btn btn-secondary btn-sm"
                    title="Publish — makers can use this template"
                    onclick="window._tplSetVisibility('${esc(t.id)}','public')">
      <i class="fa-solid fa-globe"></i> Publish
    </button>`;
}

// ── Device preview ────────────────────────────────────────────────────────────
//
// Renders the full block diagram for each device in a ready template, using
// the same visual style as the device editor Preview tab (projBuildDevice).
//
// Key differences from the editor preview:
//   - No "+ flag" button — flags are an editing feature, not a display feature.
//   - No drag-to-reorder — read-only diagram.
//   - No contenteditable rename — names come from the source code.
//
// Data shape: t._def.devices[] is an array of BlackBoxDef objects (new parser).
// JSON fields use camelCase tags (name, goType, structIcon, structLabel, etc.)
// which differ from the legacy BBPin shape (type, missingConn → already same).
// The normaliser _tplNormalizeDef bridges the two shapes.

function _tplDevicePreview(t) {
    if (t.status !== 'ready' || !t._def?.devices?.length) return '';

    const blocks = t._def.devices.map(dev => _tplBuildDevice(dev)).join('');
    return `<div class="tpl-preview-wrap">
      <div style="font-size:11px;font-weight:700;text-transform:uppercase;
                  letter-spacing:.05em;color:var(--text-muted);margin-bottom:10px">
        <i class="fa-solid fa-microchip" style="margin-right:4px"></i>
        Devices (${t._def.devices.length})
      </div>
      <div class="proj-blocks-container">${blocks}</div>
      <div class="proj-legend" style="margin-top:8px">
        <span class="proj-legend-item"><span style="color:var(--primary);font-size:16px">◎</span> optional</span>
        <span class="proj-legend-item"><span style="color:var(--success);font-size:16px">◉</span> mandatory</span>
        <span class="proj-legend-item"><span style="color:#C0392B;font-size:16px">⊙</span> connection tag missing</span>
      </div>
    </div>`;
}

// _tplNormalizePort converts a PortDef (new parser JSON shape) to the pin
// shape that _tplBuildPin expects. The new parser uses goType; the display
// functions expect type. All other fields (connection, missingConn, range,
// rangeMin, rangeMax, unit, encoding, default, bits, doc) are identical.
function _tplNormalizePort(p) {
    return {
        name:        p.name       || '',
        type:        p.goType     || p.type || '',   // goType → type
        isError:     p.isError    || false,
        doc:         p.doc        || '',
        connection:  p.connection || 'optional',
        missingConn: p.missingConn === true,          // explicit: only true when server set it
        range:       p.range      || '',
        rangeMin:    p.rangeMin   || '',
        rangeMax:    p.rangeMax   || '',
        unit:        p.unit       || '',
        encoding:    p.encoding   || '',
        default:     p.default    || p.defaultVal || '',
        bits:        p.bits       || '',
        flags:       [],                              // no flags in template preview
    };
}

// _tplBuildDevice renders all method blocks for one device (BlackBoxDef).
// It mirrors projBuildDevice but uses _tplBuildMethodCard instead.
function _tplBuildDevice(dev) {
    const label = dev.structLabel || dev.name || 'Device';
    const icon  = dev.structIcon  || 'cube';
    const cat   = dev.category    || '';

    // Collect all method blocks: Init first (if present), then named methods.
    const allMethods = [];
    if (dev.init) {
        allMethods.push({ name: 'Init', ...dev.init });
    }
    (dev.methods || []).forEach(m => allMethods.push(m));

    return allMethods.map((m, mi) =>
        _tplBuildMethodCard(m, mi, label, icon, cat)
    ).join('');
}

// _tplBuildMethodCard renders one method block. Mirrors projBuildMethodCard
// but calls _tplBuildPin (no drag, no rename, no flag button).
function _tplBuildMethodCard(method, mi, structLabel, structIcon, cat) {
    const methodLabel = method.label || method.name || '';
    const catBadge = mi === 0 && cat
        ? `<span style="font-size:11px;font-weight:600;background:rgba(255,255,255,.2);
             padding:3px 10px;border-radius:99px;margin-top:2px">${esc(cat)}</span>` : '';
    const iconHtml = projRenderFAIcon(method.icon || structIcon);
    const hdr = `<div class="proj-block-hdr">
  <div class="proj-block-hdr-icon">${iconHtml}</div>
  <div class="proj-block-hdr-label">${esc(structLabel)} ${esc(methodLabel)}${catBadge}</div>
</div>`;
    let pins = '';
    (method.inputs  || []).forEach((p, pi) => { pins += _tplBuildPin(_tplNormalizePort(p), mi, pi, 'input');  });
    (method.outputs || []).forEach((p, pi) => { pins += _tplBuildPin(_tplNormalizePort(p), mi, pi, 'output'); });
    return `<div class="proj-block">${hdr}${pins}</div>`;
}

// _tplBuildPin renders one port row — identical to projBuildPin except:
//   - No "+ flag" button (addFlag removed).
//   - No draggable attribute (read-only).
//   - No contenteditable on the name (read-only).
function _tplBuildPin(pin, mi, pi, dir) {
    const isInput = dir === 'input';
    // Slice-7 rule: outputs are connection-free. Same logic as
    // projBuildPin — keep the two builders in sync.
    const isMissingConn = isInput && pin.missingConn;
    const sym     = isMissingConn ? '⊙' : pin.connection === 'mandatory' ? '◉' : '◎';
    const dotSty  = isMissingConn ? 'color:#C0392B' : '';
    const dot     = `<span class="proj-dot ${isInput ? 'in' : 'out'}" style="${dotSty}">${sym}</span>`;
    const badges  = _tplBuildPinBadges(pin, dir);
    const nameEl  = `<span class="proj-pin-name">${esc(pin.name)}</span>`;
    const typeEl  = `<span class="proj-pin-type">${esc(pin.type)}</span>`;
    const tooltip = _tplBuildPinTooltip(pin, dir);
    const badgesArea = `<div class="proj-badges">${badges}</div>`;
    // gap:6px separates dot/name/type — the editor uses the drag handle (⠿)
    // for this spacing; the template preview is read-only so we use gap instead.
    const inner = isInput
        ? `${dot}${nameEl}${typeEl}${badgesArea}`
        : `${badgesArea}${typeEl}${nameEl}${dot}`;
    return `<div class="proj-pin ${dir}" style="gap:6px">${tooltip}${inner}</div>`;
}

// _tplBuildPinBadges mirrors projBuildBadges without the flags section
// (flags are an editor feature — the template preview is read-only).
function _tplBuildPinBadges(pin, dir) {
    let b = '';
    // Slice-7 rule: outputs never get a connection: missing badge.
    const isInput = dir === 'input';
    if (isInput && pin.missingConn) b += `<span class="proj-badge warn">⚠ connection: missing</span>`;
    if (pin.range)       b += `<span class="proj-badge range">${esc(pin.range)}</span>`;
    else {
        if (pin.rangeMin) b += `<span class="proj-badge range">≥${esc(pin.rangeMin)}</span>`;
        if (pin.rangeMax) b += `<span class="proj-badge range">≤${esc(pin.rangeMax)}</span>`;
    }
    if (pin.unit)     b += `<span class="proj-badge unit">${esc(pin.unit)}</span>`;
    if (pin.encoding) b += `<span class="proj-badge encoding">${esc(pin.encoding)}</span>`;
    if (pin.bits)     b += `<span class="proj-badge">${esc(pin.bits)}bit</span>`;
    return b;
}

// _tplBuildPinTooltip mirrors projBuildTooltip.
function _tplBuildPinTooltip(pin, dir) {
    const isInput = dir === 'input';
    const lines = [];
    if (pin.doc)        lines.push(`<strong>${esc(pin.doc)}</strong>`);
    // Slice-7 rule: outputs don't show connection state in tooltip.
    if (isInput) {
        if (pin.missingConn)           lines.push(`<span style="color:#F5B7B1">⚠ connection: missing</span>`);
        else if (pin.connection === 'mandatory') lines.push('◉ <strong>mandatory</strong> connection');
        else if (pin.connection === 'optional')  lines.push('◎ <strong>optional</strong> connection');
    }
    if (pin.range)    lines.push(`📏 Range: <code>${esc(pin.range)}</code>`);
    if (pin.unit)     lines.push(`📐 Unit: <code>${esc(pin.unit)}</code>`);
    if (pin.encoding) lines.push(`🔣 Encoding: <code>${esc(pin.encoding)}</code>`);
    if (pin.default)  lines.push(`💡 Default: <code>${esc(pin.default)}</code>`);
    if (pin.bits)     lines.push(`⬛ Bits: <code>${esc(pin.bits)}</code>`);
    if (!lines.length) return '';
    return `<div class="proj-tooltip">${lines.join('<br>')}</div>`;
}

// ── Warnings ──────────────────────────────────────────────────────────────────
//
// Warnings are split into two groups for clarity:
//   - Fatal parse errors (status=error): shown in red, always visible.
//   - connection: missing advisories: shown collapsed, yellow, with the full
//     "filename: Struct.Method input "name" (type): ..." message so the
//     specialist knows exactly which line to fix.

function _tplBuildWarnings(t) {
    const warns = t.parseErrors;
    if (!warns?.length) return '';

    if (t.status === 'error') {
        // Fatal parse errors — show expanded and red.
        const items = warns.map(w => `<li>${esc(w)}</li>`).join('');
        return `<div style="padding:10px 14px;background:#FEF2F2;border:1px solid #FCA5A5;
                     border-radius:var(--r);font-size:12px;color:#B91C1C;line-height:1.8">
          <strong><i class="fa-solid fa-circle-xmark" style="margin-right:6px"></i>
          ${warns.length} parse error(s) — fix these before publishing:</strong>
          <ul style="margin:6px 0 0 18px;padding:0">${items}</ul>
        </div>`;
    }

    // Soft warnings (status=ready but has advisory messages).
    // Split connection: missing warnings from other warnings so the specialist
    // can see at a glance whether they only need to add IDS tags.
    const connWarns  = warns.filter(w => w.includes('missing connection:'));
    const otherWarns = warns.filter(w => !w.includes('missing connection:'));

    let html = '';

    if (connWarns.length) {
        const toggleId = `tpl-conn-warns-${esc(t.id)}`;
        const items = connWarns.map(w => `<li style="margin-bottom:4px">${esc(w)}</li>`).join('');
        html += `<div>
  <button class="tpl-warn-toggle"
          onclick="var el=document.getElementById('${toggleId}');el.style.display=el.style.display==='none'?'':'none'">
    <i class="fa-solid fa-triangle-exclamation"></i>
    <strong>${connWarns.length} port(s) missing <code>connection:</code> tag</strong>
    <i class="fa-solid fa-chevron-down" style="font-size:10px;margin-left:4px"></i>
  </button>
  <div id="${toggleId}">
    <div class="tpl-warn-body">
      <p style="margin:0 0 8px;color:#92400E">
        Add <code>// connection: mandatory.</code> or <code>// connection: optional.</code>
        as a comment directly above each parameter or return value listed below:
      </p>
      <ul style="margin:0;padding-left:18px">${items}</ul>
    </div>
  </div>
</div>`;
    }

    if (otherWarns.length) {
        const toggleId = `tpl-other-warns-${esc(t.id)}`;
        const items = otherWarns.map(w => `<li>${esc(w)}</li>`).join('');
        html += `<div>
  <button class="tpl-warn-toggle"
          onclick="var el=document.getElementById('${toggleId}');el.style.display=el.style.display==='none'?'':'none'">
    <i class="fa-solid fa-triangle-exclamation"></i>
    ${otherWarns.length} other warning(s)
    <i class="fa-solid fa-chevron-down" style="font-size:10px;margin-left:4px"></i>
  </button>
  <div id="${toggleId}" style="display:none">
    <div class="tpl-warn-body"><ul>${items}</ul></div>
  </div>
</div>`;
    }

    return html;
}

// ── Publishing flags ──────────────────────────────────────────────────────────

function _tplBuildPublishSection(t) {
    if (t.status !== 'ready') return '';
    const can = t.visibility === 'public';
    const note = !can
        ? `<p class="tpl-publish-note"><i class="fa-solid fa-lock" style="margin-right:4px"></i>
           Make this template <strong>public</strong> to enable publishing options.</p>`
        : '';
    const chk = (id, checked, onChange, label, hint) => `
      <div class="tpl-publish-row">
        <input type="checkbox" id="${id}" ${checked?'checked':''} ${can?'':'disabled'}
               onchange="${onChange}">
        <div>
          <label class="tpl-publish-label ${can?'':'disabled'}" for="${id}">${label}</label>
          <div class="tpl-publish-hint">${hint}</div>
        </div>
      </div>`;
    return `<div class="tpl-publish-section">
  <div class="tpl-publish-header"><i class="fa-solid fa-share-nodes"></i> Community Publishing</div>
  ${note}
  ${chk(`tpl-feed-${esc(t.id)}`,   t.publishToFeed,   can?`window._tplFlagChange('${esc(t.id)}')`:'',
        'Publish to feed',   'Show this project in the community feed tabs')}
  ${chk(`tpl-search-${esc(t.id)}`, t.publishToSearch, can?`window._tplFlagChange('${esc(t.id)}')`:'',
        'Publish to search', 'Include this project in marketplace search results')}
  ${chk(`tpl-ready-${esc(t.id)}`,  t.readyToUse,      can?`window._tplReadyChange('${esc(t.id)}')`:'',
        'Ready to use <span style="font-size:10px;font-weight:600;background:#fef3c7;color:#92400e;border:1px solid #fde68a;border-radius:99px;padding:1px 7px;margin-left:4px">quality commitment</span>',
        'I certify this project is documented and ready for use by others')}
</div>`;
}

// ── Submit GitHub release (replaces ZIP upload) ───────────────────────────────

window._tplSubmitGithub = async function(pkgId) {
    if (_tplUploading) return;
    _tplUploading = true;

    const urlEl  = document.getElementById('tpl-gh-url-' + pkgId);
    const tagsEl = document.getElementById('tpl-gh-tags-' + pkgId);
    const btnEl  = document.getElementById('tpl-gh-btn-' + pkgId);
    const errEl  = document.getElementById('tpl-gh-err-' + pkgId);

    const url = urlEl ? urlEl.value.trim() : '';
    if (!url) {
        if (errEl) { errEl.textContent = 'Please enter a GitHub release URL.'; errEl.style.display = 'block'; }
        _tplUploading = false;
        return;
    }

    if (btnEl)  { btnEl.disabled = true; btnEl.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i>'; }
    if (errEl)  { errEl.style.display = 'none'; }

    try {
        const res = await api('POST', '/api/v1/templates/' + pkgId + '/github', {
            github_url: url,
            tags:       tagsEl ? tagsEl.value.trim() : '',
        });

        if (res?.metadata?.status >= 400 || res?.error) {
            const msg = (res?.metadata?.error || res?.error) || 'Submission failed';
            if (errEl) { errEl.textContent = '✗ ' + msg; errEl.style.display = 'block'; }
            return;
        }

        const idx = _templates.findIndex(t => t.id === pkgId);
        if (idx >= 0) {
            _templates[idx].status      = 'pending';
            _templates[idx].parseErrors = [];
            _templates[idx]._def        = null;
            _templates[idx].githubTag   = url.split('/').pop();
        }
        _tplRenderList();
        _tplStartPoll(pkgId);
        showPageAlert('Submitted — parsing in progress…', 'success');

    } catch (e) {
        if (errEl) { errEl.textContent = '✗ Network error: ' + esc(e ? e.message || String(e) : ''); errEl.style.display = 'block'; }
    } finally {
        _tplUploading = false;
        if (btnEl) { btnEl.disabled = false; btnEl.innerHTML = '<i class="fa-solid fa-cloud-arrow-up"></i> Submit'; }
    }
};

// ── Visibility ────────────────────────────────────────────────────────────────

window._tplSetVisibility = async function(id, visibility) {
    const res = await api('PUT', `/api/v1/templates/${id}/visibility`, { visibility });
    if (res?.metadata?.status >= 400) {
        showPageAlert(res.metadata.error || 'Could not update visibility.', 'danger');
        return;
    }
    const t = _templates.find(x => x.id === id);
    if (t) {
        t.visibility = visibility;
        if (visibility === 'private') {
            t.publishToFeed = t.publishToSearch = t.readyToUse = false;
        }
    }
    _tplReplaceCard(id);
};

// ── Publishing flags ──────────────────────────────────────────────────────────

window._tplFlagChange = async function(id) {
    const feed   = document.getElementById(`tpl-feed-${id}`)?.checked   ?? false;
    const search = document.getElementById(`tpl-search-${id}`)?.checked ?? false;
    const ready  = document.getElementById(`tpl-ready-${id}`)?.checked  ?? false;
    await _tplSaveFlags(id, feed, search, ready);
};

window._tplReadyChange = async function(id) {
    const el = document.getElementById(`tpl-ready-${id}`);
    if (!el) return;
    if (el.checked) {
        _tplReadyCommitmentDialog(id);
    } else {
        const feed   = document.getElementById(`tpl-feed-${id}`)?.checked   ?? false;
        const search = document.getElementById(`tpl-search-${id}`)?.checked ?? false;
        await _tplSaveFlags(id, feed, search, false);
    }
};

function _tplReadyCommitmentDialog(id) {
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;
    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);z-index:9000;align-items:center;justify-content:center;';
    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:440px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:8px">
    <i class="fa-solid fa-certificate" style="color:var(--primary);margin-right:8px"></i>Quality Commitment
  </h2>
  <p style="color:var(--text-secondary);font-size:14px;margin-bottom:20px">
    By marking this template as <strong>Ready to use</strong>, you certify that:
  </p>
  <ul style="font-size:13px;color:var(--text-secondary);padding-left:20px;margin-bottom:20px;line-height:1.8">
    <li>The template is fully documented</li>
    <li>All devices are correctly configured</li>
    <li>Output files have been tested</li>
    <li>It is ready to be used by other makers without guidance from you</li>
  </ul>
  <div style="display:flex;gap:10px;margin-top:8px">
    <button class="btn btn-secondary btn-sm" style="flex:1"
            onclick="window._tplCancelReady('${esc(id)}')">Cancel</button>
    <button class="btn btn-primary btn-sm" style="flex:2"
            onclick="window._tplConfirmReady('${esc(id)}')">
      <i class="fa-solid fa-certificate"></i> I Commit to Quality
    </button>
  </div>
</div>`;
}

window._tplCancelReady = function(id) {
    const el = document.getElementById(`tpl-ready-${id}`);
    if (el) el.checked = false;
    closeCreateProjectModal();
};

window._tplConfirmReady = async function(id) {
    closeCreateProjectModal();
    const feed   = document.getElementById(`tpl-feed-${id}`)?.checked   ?? false;
    const search = document.getElementById(`tpl-search-${id}`)?.checked ?? false;
    await _tplSaveFlags(id, feed, search, true);
};

async function _tplSaveFlags(id, feed, search, ready) {
    const res = await api('PUT', `/api/v1/templates/${id}/publishing`, {
        publishToFeed: feed, publishToSearch: search, readyToUse: ready,
    });
    if (res?.metadata?.status >= 400) {
        showPageAlert(res.metadata.error || 'Could not update publishing flags.', 'danger');
        _tplReplaceCard(id);
        return;
    }
    const t = _templates.find(x => x.id === id);
    if (t) { t.publishToFeed = feed; t.publishToSearch = search; t.readyToUse = ready; }
}

// ── Safe delete ───────────────────────────────────────────────────────────────

window._tplConfirmDelete = function(id, name) {
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;
    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:9000;align-items:center;justify-content:center;';
    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:440px;box-shadow:var(--shh);
            border:2px solid var(--danger);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;color:var(--danger);margin-bottom:8px">
    <i class="fa-solid fa-triangle-exclamation"></i> Delete Template
  </h2>
  <p style="color:var(--text-secondary);font-size:14px;margin-bottom:16px">
    This action is <strong>permanent and cannot be undone</strong>.
    All ZIP versions and data will be deleted.
  </p>
  <p style="font-size:13px;color:var(--text-secondary);margin-bottom:8px">
    Type <strong>${esc(name)}</strong> to confirm:
  </p>
  <input id="tpl-del-input" class="fc" type="text" placeholder="${esc(name)}"
         oninput="window._tplDelInput('${esc(name)}')">
  <div id="tpl-del-err" class="alert alert-danger" style="display:none;margin-top:12px"></div>
  <div style="display:flex;gap:10px;margin-top:20px">
    <button class="btn btn-secondary btn-sm" style="flex:1"
            onclick="closeCreateProjectModal()">Cancel</button>
    <button class="btn btn-danger btn-sm" id="tpl-del-submit" disabled style="flex:2"
            onclick="window._tplExecuteDelete('${esc(id)}')">
      <i class="fa-solid fa-trash-can"></i> Delete Forever
    </button>
  </div>
</div>`;
};

window._tplDelInput = function(expected) {
    const input = document.getElementById('tpl-del-input');
    const btn   = document.getElementById('tpl-del-submit');
    if (input && btn) btn.disabled = input.value !== expected;
};

window._tplExecuteDelete = async function(id) {
    const btn   = document.getElementById('tpl-del-submit');
    const errEl = document.getElementById('tpl-del-err');
    if (btn) { btn.disabled = true; btn.textContent = 'Deleting…'; }

    const res = await api('DELETE', `/api/v1/templates/${id}`);
    if (res?.metadata?.status >= 400) {
        if (errEl) { errEl.textContent = res.metadata.error || 'Could not delete.'; errEl.style.display = 'block'; }
        if (btn)   { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-trash-can"></i> Delete Forever'; }
        return;
    }
    closeCreateProjectModal();
    _tplStopPoll(id);
    _templates = _templates.filter(t => t.id !== id);
    _tplRenderList();
};

// ── Poll ──────────────────────────────────────────────────────────────────────

function _tplStartPoll(id) {
    if (_tplPollTimers[id]) return;
    _tplPollTimers[id] = setInterval(() => _tplPollOne(id), TPL_POLL_INTERVAL_MS);
}

function _tplStopPoll(id) {
    if (_tplPollTimers[id]) { clearInterval(_tplPollTimers[id]); delete _tplPollTimers[id]; }
}

async function _tplPollOne(id) {
    try {
        const res = await api('GET', `/api/v1/templates/${id}`);
        if (res?.metadata?.status >= 400) return;

        const updated = res?.data?.template;
        const def     = res?.data?.def;
        if (!updated) return;

        const idx = _templates.findIndex(t => t.id === id);
        if (idx < 0) return;

        _templates[idx] = { ..._templates[idx], ...updated };
        if (def) _templates[idx]._def = def;

        if (updated.status === 'ready' || updated.status === 'error') {
            _tplStopPoll(id);
            if (updated.status === 'ready' && !_templates[idx]._def) {
                _tplFetchDef(id);
                return;
            }
        }
        _tplReplaceCard(id);
    } catch { /* network hiccup — retry */ }
}

// ── Def fetch ─────────────────────────────────────────────────────────────────

async function _tplFetchDef(id) {
    try {
        const res = await api('GET', `/api/v1/templates/${id}`);
        if (res?.metadata?.status !== 200) return;
        const def = res?.data?.def;
        if (!def) return;
        const idx = _templates.findIndex(t => t.id === id);
        if (idx < 0) return;
        _templates[idx]._def = def;
        _tplReplaceCard(id);
    } catch { /* non-critical */ }
}

// ── Card replacement ──────────────────────────────────────────────────────────

function _tplReplaceCard(id) {
    const t = _templates.find(x => x.id === id);
    if (!t) return;
    const el = document.getElementById(`tpl-card-${id}`);
    if (!el) { _tplRenderList(); return; }
    const tmp = document.createElement('div');
    tmp.innerHTML = _tplBuildCard(t);
    el.replaceWith(tmp.firstElementChild);
}

// ── submitCreateProject override for custom_project ───────────────────────────
//
// The original submitCreateProject (above) sends to /api/v1/projects for
// custom_device and custom_project types. For custom_project, we need to call
// /api/v1/templates instead. We wrap the submit function here.

const _origSubmitCreateProject = window.submitCreateProject;

// _tplHandleCreate creates a template package and immediately submits the
// GitHub release URL — one step instead of two.
// Called by submitCreateProject when type=custom_project.
async function _tplHandleCreate(name, visibility, githubUrl, tags, categoryId, subcategoryId, errEl, btn) {
    const resetBtn = () => {
        if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-folder-plus"></i> Create Project'; }
    };
    const showErr = msg => {
        resetBtn();
        if (errEl) { errEl.textContent = msg; errEl.style.display = 'block'; errEl.scrollIntoView({ behavior: 'smooth', block: 'nearest' }); }
    };

    // Always create as private — same policy as devices.
    // If the user wanted public, show a toast guiding them to the gear icon.
    const wantsPublic = (visibility === 'public');

    // Single POST to the unified endpoint (creates record + enqueues job).
    const r = await api('POST', '/api/v1/templates', {
        name,
        github_url:    githubUrl,
        tags,
        visibility:     'private',
        category_id:    categoryId    || '',
        subcategory_id: subcategoryId || '',
    });

    resetBtn();

    if (!r || r?.metadata?.status >= 400 || r?.error) {
        showErr(r?.metadata?.error || r?.error || 'Could not create template. Please try again.');
        return;
    }

    const jobId = r?.job_id;
    if (!jobId) { showErr('No job ID returned.'); return; }

    closeCreateProjectModal();
    await loadProjects();
    renderUnifiedList();

    if (wantsPublic) {
        showPageAlert(
            '<i class="fa-solid fa-triangle-exclamation" style="margin-right:8px"></i>' +
            '<strong>Template created as private.</strong> ' +
            'Test it in the IDE first to make sure everything works correctly. ' +
            'When it is ready, click the <i class="fa-solid fa-gear" style="margin:0 3px"></i> ' +
            'gear icon on the template row and set visibility to Public.',
            'danger',
            30000
        );
    } else {
    showPageAlert(
        '<i class="fa-solid fa-cloud-arrow-up" style="margin-right:6px"></i>' +
            'Template submitted — parsing in progress…',
        'info'
    );
}

    _startJobPoll(jobId, 'template');
}
// ═══════════════════════════════════════════════════════════════════════════════
// ─── TEMPLATE HELP ─────────────────────────────────────────────────────────────
// ═══════════════════════════════════════════════════════════════════════════════
//
// _tplOpenHelp renders the Template Authoring Guide in a full-width read-only
// Monaco markdown editor that replaces the templates list.
//
// The guide content is stored as a JSON string literal (not a template literal)
// so there are no escaping issues with backticks or interpolation sequences.
//
// _tplHelpOpen guards _tplRenderList and _tplReplaceCard so background polls
// and fetch callbacks do not overwrite the help panel while it is open.

// _TPL_HELP_GUIDE — Template Authoring Guide (Markdown).
// Using JSON.stringify encoding avoids all backtick/interpolation escaping issues.
const _TPL_HELP_GUIDE = "# IoTMaker Template Authoring Guide\n\nA **template** is a ZIP file created by a specialist that packages a ready-to-run\nGo project together with one or more configurable **devices**. A maker drags those\ndevices onto the canvas, sets values in the Inspect panel, connects wires, and\nclicks **Generate ZIP** \u2014 out comes a fully working project, personalised with\ntheir choices.\n\nThis guide walks you through creating a template from scratch, using the\n**Echo Hello World** server as a running example.\n\n---\n\n## Table of Contents\n\n1. [How it works \u2014 the big picture](#1-how-it-works--the-big-picture)\n2. [ZIP structure](#2-zip-structure)\n3. [template.json](#3-templatejson)\n4. [Devices \u2014 the configurable blocks](#4-devices--the-configurable-blocks)\n   - [Package and struct](#41-package-and-struct)\n   - [Props \u2014 Inspect panel fields](#42-props--inspect-panel-fields)\n   - [Methods and wire ports](#43-methods-and-wire-ports)\n   - [IDS tag reference](#44-ids-tag-reference)\n   - [Port metadata tags](#45-port-metadata-tags)\n5. [Output files \u2014 the project skeleton](#5-output-files--the-project-skeleton)\n6. [Complete example \u2014 Echo Hello World](#6-complete-example--echo-hello-world)\n7. [Checklist before publishing](#7-checklist-before-publishing)\n\n---\n\n## 1. How it works \u2014 the big picture\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502                     Template ZIP                        \u2502\n  \u2502                                                         \u2502\n  \u2502  devices/                                               \u2502\n  \u2502  \u2514\u2500\u2500 ServerConfig.go   \u2190 specialist writes this         \u2502\n  \u2502                                                         \u2502\n  \u2502  output/                                                \u2502\n  \u2502  \u251c\u2500\u2500 main.go           \u2190 Go project skeleton            \u2502\n  \u2502  \u251c\u2500\u2500 go.mod            \u2502  (may contain {{.Var}}         \u2502\n  \u2502  \u251c\u2500\u2500 README.md         \u2502   placeholders)                \u2502\n  \u2502  \u2514\u2500\u2500 templates/                                         \u2502\n  \u2502      \u2514\u2500\u2500 index.html                                     \u2502\n  \u2502                                                         \u2502\n  \u2502  template.json         \u2190 wires vars to device props     \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n             \u2502\n             \u2502  uploaded by specialist\n             \u25bc\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502   IoTMaker server       \u2502  parses devices/, validates output/\n  \u2502   (worker)              \u2502  stores the definition\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n             \u2502\n             \u2502  maker opens IDE, places device on canvas\n             \u25bc\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502   Maker's canvas        \u2502  fills Inspect panel,\n  \u2502                         \u2502  connects wires\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n             \u2502\n             \u2502  clicks Generate ZIP\n             \u25bc\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502   Generated project     \u2502  {{.Port}} \u2192 \"8081\"\n  \u2502   (downloaded ZIP)      \u2502  {{.Message}} \u2192 \"Hello!\"\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n```\n\n**Two kinds of device values:**\n\n| Source | How the maker sets it | Used in output via |\n|---|---|---|\n| **Prop** (struct field) | Inspect panel text/dropdown | `{{.VarName}}` in output files |\n| **Wire input** (method param) | Wire from another block | codegen pipeline (not in output files) |\n\n---\n\n## 2. ZIP structure\n\n```\nyour-template.zip\n\u251c\u2500\u2500 devices/\n\u2502   \u2514\u2500\u2500 MyDevice.go        (required \u2014 at least one)\n\u251c\u2500\u2500 output/\n\u2502   \u251c\u2500\u2500 main.go            (any files the generated project needs)\n\u2502   \u251c\u2500\u2500 go.mod\n\u2502   \u2514\u2500\u2500 ...\n\u2514\u2500\u2500 template.json          (required)\n```\n\n**Rules:**\n\n- `devices/` must contain at least one `.go` file following the IDS format (see \u00a74).\n- `output/` contains the project skeleton. Text files may use `{{.VarName}}`\n  placeholders. Go source files are included as-is (they may also have placeholders).\n- `template.json` declares which device props map to which template variables.\n- The ZIP root must not contain extra directories beyond `devices/`, `output/`, and\n  `template.json`.\n\n---\n\n## 3. template.json\n\n```json\n{\n  \"vars\": {\n    \"Port\":       \"ServerConfig.Port\",\n    \"Message\":    \"ServerConfig.Message\",\n    \"ModuleName\": \"ServerConfig.ModuleName\"\n  }\n}\n```\n\n**Fields:**\n\n| Field | Required | Description |\n|---|---|---|\n| `vars` | yes | Maps template variable names to device prop paths |\n\n**`vars` format:**  `\"TemplateName\": \"StructName.FieldName\"`\n\n- The left side (`\"Port\"`) is the placeholder used in output files as `{{.Port}}`.\n- The right side (`\"ServerConfig.Port\"`) points to the `Port` field of the\n  `ServerConfig` struct in `devices/`.\n- Only `prop`-tagged struct fields are eligible (wire inputs are handled by codegen).\n\n> **Note:** The `name`, `version`, and `description` fields that older templates\n> may contain are obsolete and are ignored by the server. Name and description are\n> set in the **New Project** modal when you create the template record. Version is\n> auto-incremented by the server on each ZIP upload.\n\n---\n\n## 4. Devices \u2014 the configurable blocks\n\nEach `.go` file in `devices/` becomes one visual device in the IDE. A device is a\nstandard Go struct with methods, annotated with **IDS tags** in doc comments.\n\n### 4.1 Package and struct\n\n```go\n// Package devices \u2014 ServerConfig configures the Hello World server.\n//\n// Place this device on the canvas, fill in the Inspect panel,\n// then generate the ZIP.\npackage devices\n\n// ServerConfig holds top-level server configuration.\n//\n// icon:server. label:Server Config.\ntype ServerConfig struct {\n    // ...\n}\n```\n\nThe struct doc comment accepts two visual directives:\n\n| Directive | Example | Effect |\n|---|---|---|\n| `icon:name.` | `icon:server.` | FontAwesome icon shown in the block header |\n| `label:text.` | `label:Server Config.` | Human-readable block title |\n\nBrowse icons at [fontawesome.com/icons](https://fontawesome.com/icons) (free tier).\nUse the kebab-case name: `fa-server` \u2192 `icon:server.`\n\n### 4.2 Props \u2014 Inspect panel fields\n\nProps are struct fields tagged with `` `prop:\"Label\"` ``. The maker edits them in\nthe Inspect panel. Their values flow into output files via `{{.VarName}}`.\n\n```go\ntype ServerConfig struct {\n    // Port is the HTTP listen port.\n    Port string `prop:\"Port\" default:\"8081\"`\n\n    // Message is the greeting shown on the index page.\n    Message string `prop:\"Message\" default:\"Hello, World!\"`\n\n    // ModuleName is the Go module name written into go.mod.\n    ModuleName string `prop:\"Module Name\" default:\"github.com/example/hello\"`\n}\n```\n\n**Struct field tags:**\n\n| Tag | Required | Description |\n|---|---|---|\n| `` prop:\"Label\" `` | yes | Makes the field a prop; sets the Inspect panel label |\n| `` default:\"value\" `` | recommended | Pre-filled value shown to the maker |\n| `` options:\"a,b,c\" `` | optional | Renders a dropdown instead of a text input |\n\n### 4.3 Methods and wire ports\n\nMethods define the **visual blocks** the maker places on the canvas and connects\nwith wires.\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \u26a1  Server Config Init                            \u2502  \u2190 block header\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502                                                  \u2502\n  \u2502  \u25c9 port   int                                    \u2502  \u2190 input port (wire arrives here)\n  \u2502                                                  \u2502\n  \u2502                                    error  err \u25ce  \u2502  \u2190 output port (wire leaves here)\n  \u2502                                                  \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n\n  \u25c9 = mandatory connection    \u25ce = optional connection    \u2299 = connection tag missing\n```\n\n**Port symbols:**\n\n```\n  \u25c9  mandatory \u2014 the IDE warns the maker when this port is not wired\n  \u25ce  optional  \u2014 the port works unwired (uses default value)\n  \u2299  missing   \u2014 the specialist forgot the connection: tag (fix this!)\n```\n\n**Method doc comment format** (standard Go doc, IDS extensions):\n\n```go\n// Init validates and stores the port.\n//\n// icon:plug. label:Init.\n//\n// Params\n//\n//\tport: HTTP listen port, receives wire from a ConstInt device.  connection:mandatory.  unit:port.\n//\n// Returns\n//\n//\terr: non-nil if port is out of valid range.  connection:optional.\nfunc (s *ServerConfig) Init(port int) (err error) {\n    // ...\n}\n```\n\n**Method-level directives** (in the first lines of the doc comment):\n\n| Directive | Example | Effect |\n|---|---|---|\n| `icon:name.` | `icon:plug.` | Icon in the block header |\n| `label:text.` | `label:Init.` | Block subtitle |\n| `executionOrder:N.` | `executionOrder:1.` | Relative run order (lower = first) |\n\n**`Params` and `Returns` sections:**\n\nEach indented line describes one port **in declaration order**. The format is:\n\n```\n//\tportname: human description.  tag:value.  tag:value.\n```\n\n- `portname:` is for readability only \u2014 matching is done by position, not by name.\n- Tags are dot-terminated and case-insensitive.\n- The tab before `portname` is required (standard Go doc list format).\n\n### 4.4 IDS tag reference\n\n**Struct-level** (in the `type MyStruct struct` doc comment):\n\n| Tag | Example |\n|---|---|\n| `icon:name.` | `icon:server.` |\n| `label:text.` | `label:Server Config.` |\n\n**Method-level** (in the `func` doc comment, before `Params`/`Returns`):\n\n| Tag | Example |\n|---|---|\n| `icon:name.` | `icon:plug.` |\n| `label:text.` | `label:Init.` |\n| `executionOrder:N.` | `executionOrder:1.` |\n\n### 4.5 Port metadata tags\n\nUsed inside `Params` and `Returns` lines:\n\n| Tag | Example | Meaning |\n|---|---|---|\n| `connection:mandatory.` | \u2014 | Wire **must** be connected |\n| `connection:optional.` | \u2014 | Wire is optional |\n| `unit:label.` | `unit:port.` | Physical/logical unit shown in tooltip |\n| `range:min..max.` | `range:0..65535.` | Valid value range |\n| `rangeMin:N.` | `rangeMin:0.` | Lower bound only |\n| `rangeMax:N.` | `rangeMax:255.` | Upper bound only |\n| `encoding:label.` | `encoding:UTF-8.` | Data encoding |\n| `default:value.` | `default:8081.` | Default when unwired |\n| `bits:N.` | `bits:16.` | Significant bit count |\n\n> **Rule:** Every port must have either `connection:mandatory.` or\n> `connection:optional.`. The IDE shows a `\u2299` warning badge on ports that are\n> missing this tag.\n\n---\n\n## 5. Output files \u2014 the project skeleton\n\nEverything inside `output/` is copied verbatim into the generated ZIP, with one\ntransformation: `{{.VarName}}` placeholders are replaced with the values the maker\nconfigured.\n\n**Example `output/go.mod`:**\n\n```\nmodule {{.ModuleName}}\n\ngo 1.22\n\nrequire (\n    github.com/labstack/echo/v4 v4.12.0\n    github.com/labstack/gommon v0.4.2\n)\n```\n\nWhen the maker sets `ModuleName` to `github.com/acme/myserver`, the generated\n`go.mod` will contain `module github.com/acme/myserver`.\n\n**Which variables are available?**\n\nOnly variables declared in `template.json` under `vars`. Each variable maps to a\nprop field on a device. Wire inputs are **not** available as template variables \u2014\nthey flow through the codegen pipeline instead.\n\n**Supported file types for variable substitution:**\n\nAny text file: `.go`, `.mod`, `.html`, `.md`, `.yaml`, `.json`, `.txt`, etc.\nBinary files (images, fonts) are copied unchanged.\n\n---\n\n## 6. Complete example \u2014 Echo Hello World\n\nHere is the full template, file by file, built from scratch.\n\n### Step 1 \u2014 Plan the template\n\nWe want a maker to configure:\n- The HTTP listen port (comes from a wire or the Inspect panel)\n- A greeting message (Inspect panel only)\n- The Go module name (Inspect panel only)\n\nThe generated project is a minimal Echo web server.\n\n### Step 2 \u2014 Create `devices/ServerConfig.go`\n\n```go\n// Package devices \u2014 ServerConfig is the configuration device for the\n// Echo Hello World template.\n//\n// Place this device on the canvas, set Port, Message and Module Name\n// in the Inspect panel, then generate the ZIP.\npackage devices\n\nimport (\n\t\"fmt\"\n)\n\n// ServerConfig holds top-level server configuration.\n//\n// icon:server. label:Server Config.\ntype ServerConfig struct {\n\t// Port is the HTTP listen port.\n\tPort string `prop:\"Port\" default:\"8081\"`\n\n\t// Message is the greeting shown on the index page.\n\tMessage string `prop:\"Message\" default:\"Hello, World!\"`\n\n\t// ModuleName is the Go module name written into go.mod.\n\tModuleName string `prop:\"Module Name\" default:\"github.com/example/hello\"`\n}\n\n// Init validates and stores the port.\n//\n// icon:plug. label:Init.\n//\n// Params\n//\n//\tport: HTTP listen port, receives wire from a ConstInt device.  connection:mandatory.  unit:port.\n//\n// Returns\n//\n//\terr: non-nil if port is out of valid range.  connection:optional.\nfunc (s *ServerConfig) Init(port int) (err error) {\n\tif port < 1 || port > 65535 {\n\t\treturn fmt.Errorf(\"invalid port: %d\", port)\n\t}\n\ts.Port = fmt.Sprintf(\"%d\", port)\n\treturn nil\n}\n```\n\n**What this produces in the IDE:**\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \ud83d\udd0c  Server Config Init                          \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502  \u25c9 port   int                    [unit: port]    \u2502\n  \u2502                                                  \u2502\n  \u2502                        error  err \u25ce              \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n\n  Inspect panel (always visible):\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  Port          [ 8081      ] \u2502\n  \u2502  Message       [ Hello,... ] \u2502\n  \u2502  Module Name   [ github... ] \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n```\n\nThe `port` input has `connection:mandatory` \u2014 the IDE will warn the maker if they\nforget to wire a `ConstInt` block to it. The `err` output has `connection:optional`\n\u2014 it is fine to leave it unwired.\n\n### Step 3 \u2014 Create `template.json`\n\n```json\n{\n  \"vars\": {\n    \"Port\":       \"ServerConfig.Port\",\n    \"Message\":    \"ServerConfig.Message\",\n    \"ModuleName\": \"ServerConfig.ModuleName\"\n  }\n}\n```\n\nThis tells the server: when generating the ZIP, replace `{{.Port}}` with the value\nof the `Port` prop on `ServerConfig`, and so on.\n\n### Step 4 \u2014 Create `output/go.mod`\n\n```\nmodule {{.ModuleName}}\n\ngo 1.22\n\nrequire (\n\tgithub.com/labstack/echo/v4 v4.12.0\n\tgithub.com/labstack/gommon v0.4.2\n)\n```\n\n### Step 5 \u2014 Create `output/main.go`\n\n```go\npackage main\n\n// main.go \u2014 Generated by IoTMaker IDE \u00b7 Echo Hello World template.\n// Run:  go mod tidy && go run .\n// Then: open http://localhost:{{.Port}}\n\nimport (\n\t\"html/template\"\n\t\"io\"\n\t\"net/http\"\n\n\t\"github.com/labstack/echo/v4\"\n\t\"github.com/labstack/echo/v4/middleware\"\n)\n\ntype renderer struct {\n\tt *template.Template\n}\n\nfunc (r *renderer) Render(w io.Writer, name string, data any, c echo.Context) error {\n\treturn r.t.ExecuteTemplate(w, name, data)\n}\n\nfunc main() {\n\te := echo.New()\n\te.HideBanner = true\n\n\te.Use(middleware.Logger())\n\te.Use(middleware.Recover())\n\n\te.Renderer = &renderer{\n\t\tt: template.Must(template.ParseGlob(\"templates/*.html\")),\n\t}\n\n\te.GET(\"/\", func(c echo.Context) error {\n\t\treturn c.Render(http.StatusOK, \"index.html\", map[string]any{\n\t\t\t\"Message\": \"{{.Message}}\",\n\t\t\t\"Port\":    \"{{.Port}}\",\n\t\t})\n\t})\n\n\te.Logger.Fatal(e.Start(\":{{.Port}}\"))\n}\n```\n\n### Step 6 \u2014 Create `output/README.md`\n\n````markdown\n# {{.Message}}\n\nGenerated by **IoTMaker IDE** \u2014 Echo Hello World template.\n\n## Quick start\n\n```bash\ngo mod tidy\ngo run .\n```\n\nOpen [http://localhost:{{.Port}}](http://localhost:{{.Port}})\n\n## Configuration\n\n| Setting | Value |\n|---------|-------|\n| Port    | {{.Port}} |\n| Message | {{.Message}} |\n| Module  | {{.ModuleName}} |\n````\n\n### Step 7 \u2014 Package the ZIP\n\n```\nyour-template.zip\n\u251c\u2500\u2500 devices/\n\u2502   \u2514\u2500\u2500 ServerConfig.go\n\u251c\u2500\u2500 output/\n\u2502   \u251c\u2500\u2500 main.go\n\u2502   \u251c\u2500\u2500 go.mod\n\u2502   \u251c\u2500\u2500 README.md\n\u2502   \u2514\u2500\u2500 templates/\n\u2502       \u2514\u2500\u2500 index.html\n\u2514\u2500\u2500 template.json\n```\n\nZip from the root \u2014 the paths inside the ZIP must start with `devices/`,\n`output/`, or `template.json` directly (no extra top-level folder).\n\n```bash\n# macOS / Linux\nzip -r my-template.zip devices/ output/ template.json\n```\n\n### Step 8 \u2014 Upload to IoTMaker\n\n1. Go to **Devices \u2192 Templates**.\n2. Click **+ New ** \u2192 choose **Custom ** \u2192 fill in name and\n   description.\n3. On the template card, click **Upload Version** and select your ZIP.\n4. Wait for the **Ready** badge to appear.\n5. Review the device diagram \u2014 fix any `\u2299` warnings before publishing.\n6. When satisfied, click **Publish** and check **Ready to use**.\n\n---\n\n## 7. Checklist before publishing\n\n- [ ] Every port has `connection:mandatory.` or `connection:optional.`\n- [ ] Every method has `icon:` and `label:` directives\n- [ ] The struct has `icon:` and `label:` directives\n- [ ] All `{{.VarName}}` in output files are declared in `template.json`\n- [ ] All vars in `template.json` point to real prop fields on real structs\n- [ ] The generated project runs: `go mod tidy && go run .`\n- [ ] The README explains what the template does and how to run it\n- [ ] Default values in props make sense for a first-time maker\n\n---\n\n*IoTMaker Template Authoring Guide \u00b7 revision 1*\n";

window._tplOpenHelp = function() {
    const el = document.getElementById('tab-content-templates');
    if (!el) return;

    _tplHelpOpen = true;

    // Build the panel shell with a scrollable rendered-markdown area.
    // We use marked.js (already bundled at /marked/marked.min.js) to convert
    // the Markdown guide to HTML. Monaco is not used here — it renders source,
    // not formatted output.
    el.innerHTML = `
<div style="display:flex;flex-direction:column;gap:16px">
  <div style="display:flex;align-items:center;justify-content:space-between;
              padding-bottom:16px;border-bottom:1px solid var(--border)">
    <div>
      <h3 style="font-size:16px;font-weight:700;margin:0;color:var(--text-primary)">
        <i class="fa-solid fa-circle-question" style="color:var(--primary);margin-right:8px"></i>
        Template Authoring Guide
      </h3>
      <p style="font-size:13px;color:var(--text-muted);margin:4px 0 0">
        How to create and publish a template ZIP for IoTMaker.
      </p>
    </div>
    <button class="btn btn-secondary btn-sm" onclick="window._tplCloseHelp()">
      <i class="fa-solid fa-arrow-left"></i> Back to Templates
    </button>
  </div>
  <div id="tpl-help-body"
       style="max-height:680px;overflow-y:auto;padding:8px 4px">
  </div>
</div>`;

    function _renderHelp() {
        const body = document.getElementById('tpl-help-body');
        if (!body) return;
        body.innerHTML = window.marked.parse(_TPL_HELP_GUIDE);
    }

    // Load marked.js if not already present, then render.
    if (window.marked) {
        _renderHelp();
    } else {
        const s = document.createElement('script');
        s.src    = '/marked/marked.min.js';
        s.onload = _renderHelp;
        document.head.appendChild(s);
    }
};

// _tplCloseHelp disposes the Monaco help instance and restores the list.
window._tplCloseHelp = function() {
    _tplHelpOpen = false;
    if (window._tplHelpInst) {
        try { window._tplHelpInst.dispose(); } catch(e) {}
        window._tplHelpInst = null;
    }
    _tplRenderList();
};

// ═══════════════════════════════════════════════════════════════════════════════
// ─── PROJECTS HELP ─────────────────────────────────────────────────────────────
// ═══════════════════════════════════════════════════════════════════════════════
//
// _projOpenHelp renders the Devices & Templates Guide in a rendered-Markdown panel that
// replaces the entire page content. Uses marked.js (bundled at
// /marked/marked.min.js). _projCloseHelp disposes the panel and re-navigates
// to the projects page via renderProjects().

// _PROJ_HELP_GUIDE — Devices & Templates Guide (Markdown).
const _PROJ_HELP_GUIDE = "# IoTMaker Devices & Templates Guide\n\nYou wrote a piece of code that does something useful \u2014 read a sensor, talk to a server, run a model. **Devices** turn that code into a visual block any maker can drag onto a stage and connect with wires. No copy-pasting, no compiler arguments, no setup. They wire it, click **Generate**, and walk away with a working project.\n\nThis page is for the developers who publish those blocks.\n\n---\n\n## How it works\n\n<svg viewBox=\"0 0 700 280\" xmlns=\"http://www.w3.org/2000/svg\" style=\"width:100%;max-width:700px;color:var(--primary);font-family:var(--font);margin:8px 0\">\n  <defs>\n    <marker id=\"proj-arrow\" viewBox=\"0 0 10 10\" refX=\"9\" refY=\"5\" markerWidth=\"8\" markerHeight=\"8\" orient=\"auto\">\n      <path d=\"M0,0 L10,5 L0,10 z\" fill=\"currentColor\"/>\n    </marker>\n  </defs>\n  <!-- specialist box -->\n  <rect x=\"20\" y=\"40\" width=\"180\" height=\"120\" rx=\"8\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <text x=\"110\" y=\"28\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">SPECIALIST</text>\n  <text x=\"110\" y=\"80\" text-anchor=\"middle\" font-size=\"13\" fill=\"currentColor\" font-weight=\"600\">writes code</text>\n  <text x=\"110\" y=\"100\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">+ doc comments</text>\n  <text x=\"110\" y=\"120\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">tags release on</text>\n  <text x=\"110\" y=\"135\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">GitHub</text>\n  <!-- arrow 1 -->\n  <line x1=\"205\" y1=\"100\" x2=\"245\" y2=\"100\" stroke=\"currentColor\" stroke-width=\"1.5\" marker-end=\"url(#proj-arrow)\"/>\n  <!-- iotmaker box -->\n  <rect x=\"250\" y=\"40\" width=\"180\" height=\"120\" rx=\"8\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <text x=\"340\" y=\"28\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">IOTMAKER</text>\n  <text x=\"340\" y=\"80\" text-anchor=\"middle\" font-size=\"13\" fill=\"currentColor\" font-weight=\"600\">parses</text>\n  <text x=\"340\" y=\"100\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">turns code into</text>\n  <text x=\"340\" y=\"115\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">a visual block</text>\n  <text x=\"340\" y=\"135\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">with pins</text>\n  <!-- arrow 2 -->\n  <line x1=\"435\" y1=\"100\" x2=\"475\" y2=\"100\" stroke=\"currentColor\" stroke-width=\"1.5\" marker-end=\"url(#proj-arrow)\"/>\n  <!-- maker box -->\n  <rect x=\"480\" y=\"40\" width=\"180\" height=\"120\" rx=\"8\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <text x=\"570\" y=\"28\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">MAKER</text>\n  <text x=\"570\" y=\"80\" text-anchor=\"middle\" font-size=\"13\" fill=\"currentColor\" font-weight=\"600\">drops on stage</text>\n  <text x=\"570\" y=\"100\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">connects wires</text>\n  <text x=\"570\" y=\"115\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">clicks Generate</text>\n  <text x=\"570\" y=\"135\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.7\">\u2192 ready project</text>\n  <!-- bottom labels -->\n  <text x=\"110\" y=\"190\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.5\">writes once</text>\n  <text x=\"340\" y=\"190\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.5\">no human in the loop</text>\n  <text x=\"570\" y=\"190\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" opacity=\"0.5\">no programming required</text>\n  <!-- bottom callout -->\n  <rect x=\"20\" y=\"220\" width=\"640\" height=\"40\" rx=\"6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1\" stroke-dasharray=\"4 3\" opacity=\"0.5\"/>\n  <text x=\"340\" y=\"245\" text-anchor=\"middle\" font-size=\"12\" fill=\"currentColor\" opacity=\"0.8\">A device is reusable. One specialist's work becomes thousands of makers' projects.</text>\n</svg>\n\nThe developer works in their normal editor. The maker works on the IoTMaker stage. They never need to meet \u2014 IoTMaker is the bridge.\n\n---\n\n## Two kinds of entry\n\n| Type | What you publish | When to use it |\n|---|---|---|\n| **Device** | One reusable code block | A driver, a connector, a model \u2014 anything self-contained that other people will plug into their projects |\n| **Template** | A complete project skeleton bundling devices + scaffolding code | A \"starter kit\" \u2014 e.g. a ready-to-run web server where the maker only fills in port and message |\n\nBoth are submitted from this page through **+ New** \u2192 either **Custom Code** (device) or **Custom Project** (template).\n\n---\n\n## Your code becomes a visual block\n\nWhen IoTMaker parses your release, every type with the right doc comments turns into a draggable block. Each method becomes a sub-block with input pins on the left and output pins on the right.\n\n<svg viewBox=\"0 0 700 320\" xmlns=\"http://www.w3.org/2000/svg\" style=\"width:100%;max-width:700px;color:var(--primary);font-family:var(--font);margin:8px 0\">\n  <defs>\n    <marker id=\"proj-arrow2\" viewBox=\"0 0 10 10\" refX=\"9\" refY=\"5\" markerWidth=\"8\" markerHeight=\"8\" orient=\"auto\">\n      <path d=\"M0,0 L10,5 L0,10 z\" fill=\"currentColor\"/>\n    </marker>\n  </defs>\n  <!-- left: source code -->\n  <text x=\"160\" y=\"22\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">YOUR CODE</text>\n  <rect x=\"20\" y=\"35\" width=\"280\" height=\"240\" rx=\"6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1\" opacity=\"0.4\"/>\n  <g font-family=\"ui-monospace,monospace\" font-size=\"11\" fill=\"currentColor\">\n    <text x=\"35\" y=\"60\" opacity=\"0.55\">// icon:lightbulb. label:Sensor.</text>\n    <text x=\"35\" y=\"80\" font-weight=\"700\">type Sensor struct { ... }</text>\n    <text x=\"35\" y=\"115\" opacity=\"0.55\">// icon:plug. label:Init.</text>\n    <text x=\"35\" y=\"130\" opacity=\"0.55\">// Params</text>\n    <text x=\"35\" y=\"145\" opacity=\"0.55\">//   bus: connection:mandatory.</text>\n    <text x=\"35\" y=\"160\" opacity=\"0.55\">// Returns</text>\n    <text x=\"35\" y=\"175\" opacity=\"0.55\">//   err: connection:optional.</text>\n    <text x=\"35\" y=\"195\" font-weight=\"700\">func (s *Sensor) Init(bus...)</text>\n    <text x=\"35\" y=\"230\" opacity=\"0.55\">// icon:sun. label:Read.</text>\n    <text x=\"35\" y=\"245\" font-weight=\"700\">func (s *Sensor) Read() ...</text>\n  </g>\n  <!-- arrow -->\n  <line x1=\"305\" y1=\"155\" x2=\"375\" y2=\"155\" stroke=\"currentColor\" stroke-width=\"1.5\" marker-end=\"url(#proj-arrow2)\"/>\n  <text x=\"340\" y=\"148\" text-anchor=\"middle\" font-size=\"10\" fill=\"currentColor\" opacity=\"0.7\">parse</text>\n  <!-- right: visual block -->\n  <text x=\"540\" y=\"22\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">VISUAL BLOCK</text>\n  <!-- Init method block -->\n  <rect x=\"385\" y=\"40\" width=\"300\" height=\"100\" rx=\"6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <rect x=\"385\" y=\"40\" width=\"300\" height=\"22\" rx=\"6\" fill=\"currentColor\" opacity=\"0.85\"/>\n  <text x=\"535\" y=\"56\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"white\">\u26a1 Sensor Init</text>\n  <!-- Init pins -->\n  <circle cx=\"385\" cy=\"90\" r=\"4\" fill=\"currentColor\"/>\n  <text x=\"398\" y=\"94\" font-size=\"11\" fill=\"currentColor\" font-weight=\"600\">bus</text>\n  <text x=\"430\" y=\"94\" font-size=\"10\" fill=\"currentColor\" opacity=\"0.6\">i2c</text>\n  <circle cx=\"685\" cy=\"115\" r=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <text x=\"675\" y=\"119\" text-anchor=\"end\" font-size=\"11\" fill=\"currentColor\" font-weight=\"600\">err</text>\n  <text x=\"610\" y=\"119\" text-anchor=\"end\" font-size=\"10\" fill=\"currentColor\" opacity=\"0.6\">error</text>\n  <!-- Read method block -->\n  <rect x=\"385\" y=\"160\" width=\"300\" height=\"100\" rx=\"6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <rect x=\"385\" y=\"160\" width=\"300\" height=\"22\" rx=\"6\" fill=\"currentColor\" opacity=\"0.85\"/>\n  <text x=\"535\" y=\"176\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"white\">\u2600 Sensor Read</text>\n  <!-- Read pins -->\n  <circle cx=\"685\" cy=\"210\" r=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <text x=\"675\" y=\"214\" text-anchor=\"end\" font-size=\"11\" fill=\"currentColor\" font-weight=\"600\">value</text>\n  <text x=\"600\" y=\"214\" text-anchor=\"end\" font-size=\"10\" fill=\"currentColor\" opacity=\"0.6\">uint16</text>\n  <circle cx=\"685\" cy=\"235\" r=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <text x=\"675\" y=\"239\" text-anchor=\"end\" font-size=\"11\" fill=\"currentColor\" font-weight=\"600\">ok</text>\n  <text x=\"640\" y=\"239\" text-anchor=\"end\" font-size=\"10\" fill=\"currentColor\" opacity=\"0.6\">bool</text>\n  <!-- legend -->\n  <g transform=\"translate(0,290)\">\n    <circle cx=\"395\" cy=\"6\" r=\"4\" fill=\"currentColor\"/>\n    <text x=\"405\" y=\"10\" font-size=\"10\" fill=\"currentColor\">mandatory</text>\n    <circle cx=\"475\" cy=\"6\" r=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n    <text x=\"485\" y=\"10\" font-size=\"10\" fill=\"currentColor\">optional</text>\n  </g>\n</svg>\n\nThe doc comments do the work. `icon:` chooses the picture in the header. `label:` is the title. `connection:mandatory.` and `connection:optional.` decide which pins must be wired. Everything else (range, units, defaults) becomes hover tooltips on the maker's stage.\n\nThe full doc-comment vocabulary lives below in **\u00a710 \u2014 IDS, the device format**.\n\n---\n\n## How a device is born \u2014 three tabs, one source of truth\n\nWhen you open a device in IoTMaker, you have three views over the same code:\n\n<svg viewBox=\"0 0 700 220\" xmlns=\"http://www.w3.org/2000/svg\" style=\"width:100%;max-width:700px;color:var(--primary);font-family:var(--font);margin:8px 0\">\n  <!-- editor tab -->\n  <rect x=\"20\" y=\"20\" width=\"200\" height=\"160\" rx=\"6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <rect x=\"20\" y=\"20\" width=\"200\" height=\"22\" rx=\"6\" fill=\"currentColor\" opacity=\"0.1\"/>\n  <text x=\"120\" y=\"36\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">&lt;/&gt; EDITOR</text>\n  <g font-family=\"ui-monospace,monospace\" font-size=\"9\" fill=\"currentColor\" opacity=\"0.6\">\n    <text x=\"32\" y=\"60\">1  package blackbox</text>\n    <text x=\"32\" y=\"74\">2</text>\n    <text x=\"32\" y=\"88\">3  type Sensor struct {</text>\n    <text x=\"32\" y=\"102\">4    Gain  byte ...</text>\n    <text x=\"32\" y=\"116\">5    ATime byte ...</text>\n    <text x=\"32\" y=\"130\">6  }</text>\n    <text x=\"32\" y=\"144\">7</text>\n    <text x=\"32\" y=\"158\">8  func (s *Sensor) Init...</text>\n  </g>\n  <text x=\"120\" y=\"200\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" font-weight=\"600\">type the source</text>\n  <!-- arrow -->\n  <line x1=\"227\" y1=\"100\" x2=\"252\" y2=\"100\" stroke=\"currentColor\" stroke-width=\"1.5\" marker-end=\"url(#proj-arrow2)\"/>\n  <!-- wizard tab -->\n  <rect x=\"260\" y=\"20\" width=\"180\" height=\"160\" rx=\"6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <rect x=\"260\" y=\"20\" width=\"180\" height=\"22\" rx=\"6\" fill=\"currentColor\" opacity=\"0.1\"/>\n  <text x=\"350\" y=\"36\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">\u2728 WIZARD</text>\n  <!-- card 1 -->\n  <rect x=\"275\" y=\"55\" width=\"150\" height=\"50\" rx=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1\" opacity=\"0.6\"/>\n  <text x=\"285\" y=\"73\" font-size=\"10\" font-weight=\"700\" fill=\"currentColor\">Sensor</text>\n  <text x=\"285\" y=\"87\" font-size=\"9\" fill=\"currentColor\" opacity=\"0.6\">struct</text>\n  <text x=\"285\" y=\"100\" font-size=\"9\" fill=\"currentColor\" opacity=\"0.6\">Gain \u00b7 ATime</text>\n  <!-- card 2 -->\n  <rect x=\"275\" y=\"115\" width=\"150\" height=\"50\" rx=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1\" opacity=\"0.6\"/>\n  <text x=\"285\" y=\"133\" font-size=\"10\" font-weight=\"700\" fill=\"currentColor\">Init</text>\n  <text x=\"285\" y=\"147\" font-size=\"9\" fill=\"currentColor\" opacity=\"0.6\">method</text>\n  <text x=\"285\" y=\"160\" font-size=\"9\" fill=\"currentColor\" opacity=\"0.6\">2 inputs \u00b7 1 output</text>\n  <text x=\"350\" y=\"200\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" font-weight=\"600\">read &amp; tweak fields</text>\n  <!-- arrow -->\n  <line x1=\"447\" y1=\"100\" x2=\"472\" y2=\"100\" stroke=\"currentColor\" stroke-width=\"1.5\" marker-end=\"url(#proj-arrow2)\"/>\n  <!-- preview tab -->\n  <rect x=\"480\" y=\"20\" width=\"200\" height=\"160\" rx=\"6\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.5\"/>\n  <rect x=\"480\" y=\"20\" width=\"200\" height=\"22\" rx=\"6\" fill=\"currentColor\" opacity=\"0.1\"/>\n  <text x=\"580\" y=\"36\" text-anchor=\"middle\" font-size=\"11\" font-weight=\"700\" fill=\"currentColor\">\ud83d\udc41 PREVIEW</text>\n  <!-- mini block 1 -->\n  <rect x=\"495\" y=\"55\" width=\"170\" height=\"50\" rx=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.2\"/>\n  <rect x=\"495\" y=\"55\" width=\"170\" height=\"14\" rx=\"4\" fill=\"currentColor\" opacity=\"0.85\"/>\n  <text x=\"580\" y=\"65\" text-anchor=\"middle\" font-size=\"9\" font-weight=\"700\" fill=\"white\">Sensor Init</text>\n  <circle cx=\"495\" cy=\"85\" r=\"2.5\" fill=\"currentColor\"/>\n  <text x=\"503\" y=\"88\" font-size=\"9\" fill=\"currentColor\">i2c</text>\n  <circle cx=\"665\" cy=\"98\" r=\"2.5\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1\"/>\n  <text x=\"660\" y=\"101\" text-anchor=\"end\" font-size=\"9\" fill=\"currentColor\">err</text>\n  <!-- mini block 2 -->\n  <rect x=\"495\" y=\"115\" width=\"170\" height=\"50\" rx=\"4\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.2\"/>\n  <rect x=\"495\" y=\"115\" width=\"170\" height=\"14\" rx=\"4\" fill=\"currentColor\" opacity=\"0.85\"/>\n  <text x=\"580\" y=\"125\" text-anchor=\"middle\" font-size=\"9\" font-weight=\"700\" fill=\"white\">Sensor Read</text>\n  <circle cx=\"665\" cy=\"145\" r=\"2.5\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1\"/>\n  <text x=\"660\" y=\"148\" text-anchor=\"end\" font-size=\"9\" fill=\"currentColor\">value</text>\n  <circle cx=\"665\" cy=\"158\" r=\"2.5\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1\"/>\n  <text x=\"660\" y=\"161\" text-anchor=\"end\" font-size=\"9\" fill=\"currentColor\">ok</text>\n  <text x=\"580\" y=\"200\" text-anchor=\"middle\" font-size=\"11\" fill=\"currentColor\" font-weight=\"600\">see what makers see</text>\n</svg>\n\nThe **Editor** is your code, in Monaco. The **Wizard** is the same code rendered as cards \u2014 one per struct, one per method \u2014 so you can scan and tweak field-by-field without scrolling. The **Preview** shows what your block will look like when a maker drags it onto their stage. Click **Parse** at any point and all three tabs sync to the latest source.\n\n---\n\n## Table of Contents\n\n1. [Two kinds of entry](#1-two-kinds-of-entry)\n2. [Creating a device (Custom Code)](#2-creating-a-device-custom-code)\n3. [Creating a template (Custom Project)](#3-creating-a-template-custom-project)\n4. [The unified list](#4-the-unified-list)\n5. [Device properties](#5-device-properties)\n6. [Publishing a device](#6-publishing-a-device)\n7. [Updating a version](#7-updating-a-version)\n8. [Deleting a device](#8-deleting-a-device)\n9. [Prerequisites \u2014 GitHub account](#9-prerequisites--github-account)\n10. [IDS \u2014 the device format](#10-ids--the-device-format)\n11. [Complete example \u2014 APDS9960 colour sensor](#11-complete-example--apds9960-colour-sensor)\n12. [Checklist before publishing](#12-checklist-before-publishing)\n\n---\n\n## 1. Two kinds of entry\n\n| Type | What it creates | Who uses it |\n|---|---|---|\n| **Custom Code** | A device \u2014 a single reusable code block other people drop on a stage | Specialists who want to share one capability |\n| **Custom Project** | A template \u2014 a project skeleton that bundles devices and produces a complete generated project | Specialists who want to ship a starter kit |\n\nBoth appear in the same list on this page, identified by their type badge.\n\n---\n\n## 2. Creating a device (Custom Code)\n\nClick **+ New** \u2192 **Import from GitHub** \u2192 choose **Device**.\n\n| Field | Description |\n|---|---|\n| **Project name** | Human-readable name shown in the list |\n| **Programming language** | The language the source uses |\n| **GitHub Release URL** | Full URL to a release, e.g. `https://github.com/you/repo/releases/tag/v1.0` |\n| **Tags** | Optional comma-separated keywords, e.g. `sensor, i2c, temperature` |\n| **Visibility** | `private` \u2014 only you can use it; `public` \u2014 available to everyone |\n\nAfter clicking **Create**, IoTMaker:\n\n1. Validates that the URL owner matches your connected GitHub account.\n2. Enqueues a background worker to download the release.\n3. Parses every source file with IDS doc comments.\n4. Creates one device entry per type found.\n\nA success alert appears when parsing completes.\n\n> **Tip:** Start with `private`. The device works in your own sessions right away. Publish it only after you have tested it in its target environment.\n\n---\n\n## 3. Creating a template (Custom Project)\n\nClick **+ New** \u2192 **Import from GitHub** \u2192 choose **Template**.\n\nTemplates package one or more devices together with a project skeleton. The maker drops the devices on a stage, fills in their props, and clicks **Generate** to download a complete project \u2014 config files, code, README \u2014 already wired together.\n\nTemplate authoring has its own guide. Open the **(?)** button on the **Template** card in the modal to read it.\n\n---\n\n## 4. The unified list\n\nDevices, templates, and your local drafts all appear in one list, newest first. Each row shows:\n\n- The display name\n- A type badge (`device`, `template`, `project`)\n- Visibility (`private` / `public`)\n- Status \u2014 `processing`, `ready`, or `error`\n- A **ready** badge when the quality commitment is signed\n- The GitHub repo and tag (for devices and templates)\n\nClick a row to expand it: devices and templates show the parsed visual blocks; projects open in the editor.\n\n**Status meanings:**\n\n| Badge | Meaning |\n|---|---|\n| `processing` | Worker is downloading and parsing |\n| `ready` | Parsed successfully \u2014 available to use |\n| `error` | Parsing failed \u2014 re-submit after fixing the source |\n\n**Action buttons on each row:**\n\n| Button | Action |\n|---|---|\n| \u2699 (gear) | Open **Properties** \u2014 edit tags, category, visibility, publish flags |\n| \ud83d\uddd1 (trash) | Delete permanently |\n\n---\n\n## 5. Device properties\n\nClick \u2699 to open **Properties**.\n\n### Tags\n\nComma-separated keywords used in community search and feed, e.g. `sensor, i2c, colour, rp2040`. Tags help others find your device.\n\n### Category and subcategory\n\nPick from the curated taxonomy. The category decides which section of the IoTMaker components menu your device appears under (e.g. *Sensors*, *Communication*, *Math*). Subcategory narrows it further. Both are optional; an uncategorised device is still usable, just harder to discover.\n\n### Visibility\n\n| Option | Effect |\n|---|---|\n| **Private** | Only you can place this device |\n| **Public** | Everyone can see and use it |\n\nSwitching to **Public** reveals the Publishing section.\n\n### Publishing (public devices only)\n\n| Flag | Effect |\n|---|---|\n| **Publish to feed** | Device card appears in the community feed tabs |\n| **Publish to search** | Device appears in marketplace search results |\n| **Ready to use** | Quality badge \u2014 requires the quality commitment |\n\n> **Rule:** Feed and search are only active when **Ready to use** is checked. The quality commitment is the gate.\n\n### Quality commitment\n\nChecking **Ready to use** opens a confirmation dialog. By accepting, you certify that:\n\n- The device is fully documented and tested in its target environment.\n- All types have `icon:` and `label:` directives.\n- All ports have `connection:mandatory.` or `connection:optional.` tags.\n- A maker can use the device without guidance from you.\n\nThis is an honesty pledge. It is not enforced automatically \u2014 it builds trust in the community.\n\n---\n\n## 6. Publishing a device\n\n1. Make sure the device status is **ready**.\n2. Click \u2699 \u2192 set Visibility to **Public**.\n3. Check **Ready to use** \u2192 accept the quality commitment.\n4. Feed and search flags are now active. Check both if you want the device discoverable.\n5. Click **Save**.\n\nThe device card appears in the community feed and search results within minutes.\n\n---\n\n## 7. Updating a version\n\nSubmit the new release URL via **+ New \u2192 Custom Code**. The URL must point to the same GitHub repo (`owner/repo`) \u2014 only the tag changes:\n\n```\nv1.0 \u2192 https://github.com/you/repo/releases/tag/v1.0  (original)\nv2.0 \u2192 https://github.com/you/repo/releases/tag/v2.0  (update)\n```\n\nIoTMaker detects the same `owner/repo` and **updates the existing device row** instead of creating a duplicate. The device id stays the same \u2014 makers who already placed it on a stage keep working without interruption. The parsed definition is replaced with the new version.\n\n> **Note:** If the new release removes a type that existed before, that type's row is not deleted automatically \u2014 delete it manually if needed.\n\n---\n\n## 8. Deleting a device\n\nClick \ud83d\uddd1 on the device row. A confirmation dialog asks you to type the device name.\n\n**This is permanent.** The device is removed from:\n\n- Your list on this page\n- The IoTMaker components menu (for everyone)\n- Any community feed cards\n\nMakers who already placed the device on a stage and saved their scene keep the reference, but the device will no longer load.\n\n---\n\n## 9. Prerequisites \u2014 GitHub account\n\nYour GitHub account must be connected before submitting a device.\n\n1. Go to **Profile** (avatar menu).\n2. Click **Connect GitHub** in the GitHub Identity section.\n3. Authorise IoTMaker in the OAuth flow.\n\nOnce connected, your verified GitHub username is shown. You can only submit repositories where the URL owner matches your verified username. To publish a device from another user's repo, fork it to your own account first.\n\n---\n\n## 10. IDS \u2014 the device format\n\nIDS (IoTMaker Doc Standard) is the convention for writing devices. It extends standard doc comments with machine-readable directives that the parser uses to build the visual block.\n\nCurrently the parser supports Go source. Other languages are on the roadmap; the directive vocabulary below is language-neutral.\n\n### Package comment\n\n```go\n// Package blackbox\n//\n// APDS9960 reads RGBC colour data via I2C on RP2040.\n//\n// Place Init at global scope. Place Run inside a Loop.\npackage blackbox\n```\n\n### Type\n\n```go\n// APDS9960 reads colour (RGBC) data via I2C.\n//\n// icon:lightbulb. label:APDS9960.\ntype APDS9960 struct {\n    gain  byte `prop:\"ADC Gain\"         default:\"0\"   options:\"0,1,2,3\"`\n    atime byte `prop:\"Integration Time\" default:\"255\"`\n}\n```\n\n**Type-level directives:**\n\n| Directive | Example | Effect |\n|---|---|---|\n| `icon:name.` | `icon:lightbulb.` | Icon in the block header (FontAwesome free) |\n| `label:text.` | `label:APDS9960.` | Human-readable block title |\n\n**Field tags:**\n\n| Tag | Description |\n|---|---|\n| `` prop:\"Label\" `` | Makes the field editable in the maker's Inspect panel |\n| `` default:\"value\" `` | Pre-filled default value |\n| `` options:\"a,b,c\" `` | Renders a dropdown instead of a text input |\n\n### Methods\n\n```go\n// Init configures the sensor on the given I2C bus.\n//\n// executionOrder:1. icon:hourglass-start. label:Init.\n//\n// Params\n//\n//\ti2c: I2C bus \u2014 wire from an I2CBus Init block.  connection:mandatory.  unit:i2c_bus.\n//\n// Returns\n//\n//\terr: initialisation error.  connection:optional.\nfunc (s *APDS9960) Init(i2c *machine.I2C) (err error) { ... }\n```\n\n**Method-level directives** (before `Params`/`Returns`):\n\n| Directive | Example | Effect |\n|---|---|---|\n| `icon:name.` | `icon:hourglass-start.` | Icon in the block header |\n| `label:text.` | `label:Init.` | Block subtitle |\n| `executionOrder:N.` | `executionOrder:1.` | Run order (lower = first) |\n\n**`Params` and `Returns` sections:**\n\nEach indented line describes one port in declaration order:\n\n```\n//\tportname: description.  tag:value.  tag:value.\n```\n\n- The tab (`\\t`) before `portname` is **required**.\n- `portname:` is for human readability \u2014 matching is by position, not name.\n- Tags are dot-terminated and case-insensitive.\n\n### Port metadata tags\n\n| Tag | Example | Meaning |\n|---|---|---|\n| `connection:mandatory.` | \u2014 | Wire must be connected |\n| `connection:optional.` | \u2014 | Wire is optional |\n| `unit:label.` | `unit:lux.` | Unit shown in pin tooltip |\n| `range:min..max.` | `range:0..65535.` | Valid value range |\n| `rangeMin:N.` | `rangeMin:0.` | Lower bound only |\n| `rangeMax:N.` | `rangeMax:255.` | Upper bound only |\n| `encoding:label.` | `encoding:I2C-7bit.` | Data encoding |\n| `default:value.` | `default:400000.` | Default when unwired |\n| `bits:N.` | `bits:16.` | Significant bit count |\n\n> **Rule:** Every port must have `connection:mandatory.` or `connection:optional.`. The block shows a `\u2299` warning badge on ports missing this tag.\n\n### Manual pages (inline help)\n\nEmbed documentation directly in the source using `/* */` block comments. These appear as tabs in the maker's component help panel.\n\n```go\n/*\nmanualName:wiring-guide.\nlanguage:en.\nshowIn:init.\n```markdown\n# Wiring Guide\n\n| Signal | Pico Pin |\n|--------|----------|\n| SDA    | GP4      |\n| SCL    | GP5      |\n```\n*/\n```\n\n| Tag | Values | Description |\n|---|---|---|\n| `manualName:` | any identifier | Page id (required) |\n| `language:` | `en`, `pt-br`, etc. | BCP-47 language code |\n| `showIn:` | `init`, `run`, method name | Which method's block shows this page |\n\n---\n\n## 11. Complete example \u2014 APDS9960 colour sensor\n\n```go\n// Package blackbox\n//\n// APDS9960 reads RGBC colour data via I2C on RP2040.\n//\n// Place Init at global scope. Place Run or Log inside a Loop.\n// Wire I2CBus.bus \u2192 APDS9960.i2c before generating code.\npackage blackbox\n\nimport (\n\t\"machine\"\n\t\"time\"\n)\n\n// APDS9960 reads colour (RGBC) data via I2C.\n//\n// icon:lightbulb. label:APDS9960.\ntype APDS9960 struct {\n\tgain  byte `prop:\"ADC Gain\"         default:\"0\"   options:\"0,1,2,3\"`\n\tatime byte `prop:\"Integration Time\" default:\"255\"`\n}\n\n// Init configures the sensor.\n//\n// executionOrder:1. icon:hourglass-start. label:Init.\n//\n// Params\n//\n//\ti2c: I2C bus \u2014 wire from an I2CBus Init block.  connection:mandatory.  unit:i2c_bus.\n//\n// Returns\n//\n//\terr: initialisation error.  connection:optional.\nfunc (s *APDS9960) Init(i2c *machine.I2C) (err error) {\n\ts.i2c = i2c\n\ts.i2c.WriteRegister(0x39, 0x80, []byte{0x01})\n\ts.i2c.WriteRegister(0x39, 0x81, []byte{s.atime})\n\ts.i2c.WriteRegister(0x39, 0x8F, []byte{s.gain})\n\ts.i2c.WriteRegister(0x39, 0x80, []byte{0x03})\n\treturn nil\n}\n\n// Run reads the four RGBC colour channels.\n//\n// executionOrder:10. icon:sun. label:Run.\n//\n// Returns\n//\n//\tclear: total light intensity.  range:0..65535.  unit:lux_counts.   connection:optional.\n//\tred:   red channel.            range:0..65535.  unit:color_counts.  connection:optional.\n//\tgreen: green channel.          range:0..65535.  unit:color_counts.  connection:optional.\n//\tblue:  blue channel.           range:0..65535.  unit:color_counts.  connection:optional.\nfunc (s *APDS9960) Run() (clear, red, green, blue uint16) {\n\tdata := make([]byte, 8)\n\ts.i2c.ReadRegister(0x39, 0x94, data)\n\tclear = uint16(data[0]) | uint16(data[1])<<8\n\tred   = uint16(data[2]) | uint16(data[3])<<8\n\tgreen = uint16(data[4]) | uint16(data[5])<<8\n\tblue  = uint16(data[6]) | uint16(data[7])<<8\n\treturn\n}\n\n/*\nmanualName:wiring-guide.\nlanguage:en.\nshowIn:init.\n```markdown\n# APDS9960 \u2014 Wiring Guide\n\n| Signal | Default Pico Pin |\n|--------|-----------------|\n| SDA    | GP4             |\n| SCL    | GP5             |\n| VCC    | 3V3 (pin 36)    |\n| GND    | GND (pin 38)    |\n\nWire the **I2CBus.bus** output to the **APDS9960.i2c** input.\n```\n*/\n```\n\n**Repository layout expected by IoTMaker:**\n\n```\nyour-repo/\n\u2514\u2500\u2500 *.go          \u2190 one or more IDS-annotated source files\n                     (or in any directory \u2014 the parser walks them all)\n```\n\nTag a release, then submit `https://github.com/you/repo/releases/tag/v1.0`.\n\n---\n\n## 12. Checklist before publishing\n\n- [ ] GitHub account connected (Profile \u2192 Connect GitHub)\n- [ ] Repository owner matches your connected GitHub username\n- [ ] Every type has `icon:` and `label:` directives\n- [ ] Every method has `icon:`, `label:`, and `executionOrder:` directives\n- [ ] Every port has `connection:mandatory.` or `connection:optional.`\n- [ ] Ports with physical values have `unit:` and/or `range:` tags\n- [ ] Status is **ready** (green badge)\n- [ ] At least one inline manual page (`/* */`) explains wiring or usage\n- [ ] Tested in its target environment\n- [ ] Visibility set to **Public** in Properties\n- [ ] **Ready to use** checked \u2014 quality commitment accepted\n\n---\n\n*IoTMaker Devices & Templates Guide \u00b7 revision 3*\n";

// _projHelpOpen is true while the help panel is visible.
// renderProjects() checks this flag and skips re-rendering when set.
let _projHelpOpen = false;

window._projOpenHelp = function() {
    if (!_root) return;
    _projHelpOpen = true;

    _root.innerHTML = `
<div style="max-width:1200px;margin:0 auto;padding:24px 28px">
  <div style="display:flex;align-items:center;justify-content:space-between;
              padding-bottom:16px;margin-bottom:20px;border-bottom:1px solid var(--border)">
    <div>
      <h1 style="font-size:22px;font-weight:700;margin:0">
        <i class="fa-solid fa-circle-question" style="color:var(--primary);margin-right:10px"></i>
        Devices & Templates Guide
      </h1>
      <p style="color:var(--text-muted);font-size:14px;margin:4px 0 0">
        How to write, parse, version and publish black-box components.
      </p>
    </div>
    <button class="btn btn-secondary btn-sm" onclick="window._projCloseHelp()">
      <i class="fa-solid fa-arrow-left"></i> Back to Devices & Templates
    </button>
  </div>
  <div id="proj-help-body"
       style="max-height:calc(100vh - 160px);overflow-y:auto;padding:4px 4px 40px">
  </div>
</div>`;

    function _renderProjHelp() {
        const body = document.getElementById('proj-help-body');
        if (!body) return;
        body.innerHTML = window.marked.parse(_PROJ_HELP_GUIDE);
    }

    if (window.marked) {
        _renderProjHelp();
    } else {
        const s = document.createElement('script');
        s.src    = '/marked/marked.min.js';
        s.onload = _renderProjHelp;
        document.head.appendChild(s);
    }
};

// _projCloseHelp clears the flag and re-renders the full projects page.
window._projCloseHelp = function() {
    _projHelpOpen = false;
    renderProjects(_root);
};


// =============================================================================
// --- DEVICES TAB -------------------------------------------------------------
// =============================================================================
//
// Specialists submit GitHub release URLs here. Each IDS-annotated Go struct
// becomes a device block in the WASM IDE hardware menu.
// State lives in module-level _devices and _devJobTimers.


async function _devLoadList() {
    var el = document.getElementById('dev-list');
    if (!el) return;

    try {
        var res = await api('GET', '/api/v1/blackbox/mine');
        if (res && res.metadata && res.metadata.status >= 400) {
            el.innerHTML = '<div class="alert alert-danger">✗ ' + esc((res.metadata && res.metadata.error) || 'Could not load devices') + '</div>';
            return;
        }
        _devices = Array.isArray(res) ? res : ((res && res.data) || []);
        _devRenderList();
    } catch (e) {
        if (el) el.innerHTML = '<div class="alert alert-danger">✗ Network error: ' + esc(e ? e.message || String(e) : '') + '</div>';
    }
}

function _devRenderList() {
    var el = document.getElementById('dev-list');
    if (!el) return;

    if (!_devices.length) {
        el.innerHTML =
            '<div style="text-align:center;padding:40px 20px;color:var(--text-muted)">' +
            '<i class="fa-solid fa-microchip" style="font-size:32px;opacity:.2;display:block;margin-bottom:12px"></i>' +
            '<p style="font-weight:600;margin:0 0 4px">No devices yet</p>' +
            '<p style="font-size:13px;margin:0">Submit a GitHub release URL above to import your first device.</p>' +
            '</div>';
        return;
    }

    var byRepo = {};
    _devices.forEach(function(d) {
        var key = (d.githubOwner || '') + '/' + (d.githubRepo || '');
        if (!byRepo[key]) byRepo[key] = { url: d.githubUrl, tag: d.githubTag, key: key, devices: [] };
        byRepo[key].devices.push(d);
    });

    el.innerHTML = Object.values(byRepo).map(function(g) {
        var cards = g.devices.map(function(d) {
            var badge = d.status === 'ready'
                ? '<span class="dev-badge ready"><i class="fa-solid fa-circle-check"></i> Ready</span>'
                : d.status === 'error'
                    ? '<span class="dev-badge error"><i class="fa-solid fa-circle-xmark"></i> Error</span>'
                    : '<span class="dev-badge pending"><i class="fa-solid fa-circle-notch fa-spin"></i> Processing</span>';
            var tags = ((d.tags || '').split(',').filter(Boolean)
                .map(function(t) { return '<span class="tag" style="background:var(--bg-2)">' + esc(t.trim()) + '</span>'; })
                .join(''));
            return '<div class="dev-card">' +
                '<div style="flex:1;min-width:0">' +
                '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">' +
                '<span style="font-size:14px;font-weight:700">' + esc(d.displayName || d.id) + '</span>' +
                badge + '</div>' +
                (tags ? '<div style="display:flex;gap:4px;flex-wrap:wrap;margin-top:4px">' + tags + '</div>' : '') +
                '</div>' +
                '<div style="font-size:11px;color:var(--text-muted);flex-shrink:0">' +
                (d.updatedAt ? new Date(d.updatedAt).toLocaleDateString() : '') +
                '</div></div>';
        }).join('');
        return '<div style="margin-bottom:20px">' +
            '<div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">' +
            '<i class="fa-brands fa-github" style="color:var(--text-muted)"></i>' +
            '<a href="' + esc(g.url) + '" target="_blank" rel="noopener noreferrer" style="font-size:13px;font-weight:600;color:var(--text-primary);text-decoration:none">' + esc(g.key) + '</a>' +
            (g.tag ? '<span class="tag">' + esc(g.tag) + '</span>' : '') +
            '<span style="font-size:12px;color:var(--text-muted)">' + g.devices.length + ' device' + (g.devices.length !== 1 ? 's' : '') + '</span>' +
            '</div>' + cards + '</div>';
    }).join('');
}

// ─── Device modals (Properties + Delete) ─────────────────────────────────────

export function openDevicePropertiesModal(deviceId) {
    const d = _devices.find(x => x.id === deviceId);
    if (!d) return;
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;
    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);z-index:9000;align-items:center;justify-content:center;';
    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:480px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">Device Properties</h2>
  <p style="color:var(--text-muted);font-size:13px;margin-bottom:24px">${esc(d.displayName || d.id)}</p>
  <div id="dev-prop-err" class="alert alert-danger" style="display:none"></div>
  <div class="fg">
    <label>Tags <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(comma-separated)</span></label>
    <input id="dp-tags" class="fc" type="text" value="${esc(d.tags || '')}"
           placeholder="e.g. sensor, i2c, temperature" style="font-size:13px">
  </div>
  <div class="fg">
    <label>Visibility *</label>
    <div style="display:flex;gap:12px;margin-top:4px">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)"
             id="dp-pub-label">
        <input type="radio" name="dp-vis" value="public" onchange="onDevVisChange()"
               ${d.visibility === 'public' ? 'checked' : ''}>
        <i class="fa-solid fa-globe" style="color:var(--primary)"></i>
        <div><div style="font-weight:600">Public</div>
             <div style="font-size:11px;color:var(--text-muted)">Visible in the community</div></div>
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)"
             id="dp-prv-label">
        <input type="radio" name="dp-vis" value="private" onchange="onDevVisChange()"
               ${d.visibility !== 'public' ? 'checked' : ''}>
        <i class="fa-solid fa-lock" style="color:var(--text-secondary)"></i>
        <div><div style="font-weight:600">Private</div>
             <div style="font-size:11px;color:var(--text-muted)">Only you can see it</div></div>
      </label>
    </div>
  </div>
  <!-- Publish flags — shown when public. readyToUse triggers quality commitment. -->
  <div id="dp-publish-wrap" style="display:${d.visibility === 'public' ? '' : 'none'}">
    ${_buildPublishSection('dp', d.publishToFeed || false, d.publishToSearch || false, d.readyToUse || false, false)}
  </div>
  <div style="display:flex;gap:10px;margin-top:20px">
    <button class="btn btn-secondary btn-sm" onclick="closeCreateProjectModal()" style="flex:1">Cancel</button>
    <button class="btn btn-primary btn-sm" id="dp-save" onclick="saveDeviceProperties('${esc(deviceId)}')" style="flex:2">
      <i class="fa-solid fa-floppy-disk"></i> Save
    </button>
  </div>
</div>`;
    // Apply initial visual state
    onDevVisChange();
}

export function onDevVisChange() {
    const val = document.querySelector('input[name="dp-vis"]:checked')?.value;
    const pub = document.getElementById('dp-pub-label');
    const prv = document.getElementById('dp-prv-label');
    if (pub) { pub.style.borderColor = val === 'public' ? 'var(--primary)' : 'var(--border)'; pub.style.background = val === 'public' ? 'var(--info-bg)' : ''; }
    if (prv) { prv.style.borderColor = val === 'private' ? 'var(--primary)' : 'var(--border)'; prv.style.background = val === 'private' ? 'var(--info-bg)' : ''; }
    // Show publish flags only when public.
    const wrap = document.getElementById('dp-publish-wrap');
    if (wrap) wrap.style.display = val === 'public' ? '' : 'none';
}

export function onDevQualityChange(deviceId) {
    // Reserved — quality commitment is handled by the shared onReadyToUseChange logic.
}

export async function saveDeviceProperties(deviceId) {
    const tags         = document.getElementById('dp-tags')?.value?.trim() || '';
    const vis          = document.querySelector('input[name="dp-vis"]:checked')?.value || 'private';
    const publishToFeed   = document.getElementById('dp-pub-feed')?.checked   || false;
    const publishToSearch = document.getElementById('dp-pub-search')?.checked || false;
    const readyToUse      = document.getElementById('dp-ready')?.checked      || false;
    const errEl   = document.getElementById('dev-prop-err');
    const saveBtn = document.getElementById('dp-save');
    // readyToUse requires feed + search to also be checked — enforce server-side logic here.
    const effectiveFeed   = readyToUse ? true : publishToFeed;
    const effectiveSearch = readyToUse ? true : publishToSearch;
    if (saveBtn) { saveBtn.disabled = true; saveBtn.textContent = 'Saving…'; }
    const r = await api('PATCH', '/api/v1/blackbox/' + deviceId, {
        tags, visibility: vis,
        publishToFeed: effectiveFeed, publishToSearch: effectiveSearch, readyToUse,
    });
    if (r?.metadata?.status >= 400 || r?.error) {
        if (errEl) { errEl.textContent = r?.metadata?.error || r?.error || 'Could not save.'; errEl.style.display = 'block'; }
        if (saveBtn) { saveBtn.disabled = false; saveBtn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Save'; }
        return;
    }
    closeCreateProjectModal();
    await loadProjects();
    renderTree();
    showPageAlert('Device properties saved.', 'success');
}

export function confirmDeleteDevice(deviceId, deviceName) {
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;
    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:9000;align-items:center;justify-content:center;';
    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:440px;box-shadow:var(--shh);
            border:2px solid var(--danger);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;color:var(--danger);margin-bottom:8px">
    <i class="fa-solid fa-triangle-exclamation"></i> Delete Device
  </h2>
  <p style="color:var(--text-secondary);font-size:14px;margin-bottom:16px">
    This action is <strong>permanent and cannot be undone</strong>.
    The device will be removed from the IDE Hardware menu for all makers.
  </p>
  <p style="font-size:13px;color:var(--text-secondary);margin-bottom:8px">
    Type <strong>${esc(deviceName)}</strong> to confirm:
  </p>
  <input id="dev-del-input" class="fc" type="text" placeholder="${esc(deviceName)}"
         oninput="onDevDeleteInput('${esc(deviceName)}')">
  <div id="dev-del-err" class="alert alert-danger" style="display:none;margin-top:12px"></div>
  <div style="display:flex;gap:10px;margin-top:20px">
    <button class="btn btn-secondary btn-sm" onclick="closeCreateProjectModal()" style="flex:1">Cancel</button>
    <button class="btn btn-danger btn-sm" id="dev-del-submit" disabled
            onclick="executeDeleteDevice('${esc(deviceId)}')" style="flex:2">
      <i class="fa-solid fa-trash-can"></i> Delete Forever
    </button>
  </div>
</div>`;
}

export function onDevDeleteInput(expected) {
    const input = document.getElementById('dev-del-input');
    const btn   = document.getElementById('dev-del-submit');
    if (btn) btn.disabled = input?.value !== expected;
}

export async function executeDeleteDevice(deviceId) {
    const btn   = document.getElementById('dev-del-submit');
    const errEl = document.getElementById('dev-del-err');
    if (btn) { btn.disabled = true; btn.textContent = 'Deleting…'; }
    const r = await api('DELETE', '/api/v1/blackbox/' + deviceId);
    if (r?.metadata?.status === 200 || r?.status === 'deleted') {
        closeCreateProjectModal();
        await loadProjects();
        renderTree();
        showPageAlert('Device deleted permanently.', 'success');
    } else {
        if (errEl) { errEl.textContent = r?.metadata?.error || 'Could not delete.'; errEl.style.display = 'block'; }
        if (btn)   { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-trash-can"></i> Delete Forever'; }
    }
}

window._devSubmit = async function() {
    if (_devSubmitting) return;

    var urlEl  = document.getElementById('dev-url');
    var tagsEl = document.getElementById('dev-tags');
    var btnEl  = document.getElementById('dev-btn');
    var errEl  = document.getElementById('dev-err');
    var okEl   = document.getElementById('dev-ok');

    var url = urlEl ? urlEl.value.trim() : '';
    if (!url) {
        if (errEl) { errEl.textContent = 'Please enter a GitHub release URL.'; errEl.style.display = 'block'; }
        return;
    }

    _devSubmitting = true;
    if (btnEl)  { btnEl.disabled = true; btnEl.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i>'; }
    if (errEl)  { errEl.style.display = 'none'; }
    if (okEl)   { okEl.style.display  = 'none'; }

    try {
        var res = await api('POST', '/api/v1/blackbox/submit', {
            github_url: url,
            tags:       tagsEl ? tagsEl.value.trim() : '',
        });

        if ((res && res.error) || (res && res.metadata && res.metadata.status >= 400)) {
            var msg = (res.error || (res.metadata && res.metadata.error)) || 'Submission failed';
            if (errEl) { errEl.textContent = '✗ ' + msg; errEl.style.display = 'block'; }
            return;
        }

        var jobId = res && res.job_id;
        if (!jobId) {
            if (errEl) { errEl.textContent = '✗ No job ID returned'; errEl.style.display = 'block'; }
            return;
        }

        if (okEl)   { okEl.textContent = '✓ Submitted — parsing in progress…'; okEl.style.display = 'block'; }
        if (urlEl)  urlEl.value  = '';
        if (tagsEl) tagsEl.value = '';

        _devStartJobPoll(jobId);

    } catch (e) {
        if (errEl) { errEl.textContent = '✗ Network error: ' + esc(e ? e.message || String(e) : ''); errEl.style.display = 'block'; }
    } finally {
        _devSubmitting = false;
        if (btnEl) { btnEl.disabled = false; btnEl.innerHTML = '<i class="fa-solid fa-cloud-arrow-up"></i> Submit'; }
    }
};

// _devStartJobPoll polls the worker job result and refreshes the project tree
// when parsing completes. Called after submitting a GitHub device URL.
// _startJobPoll polls a parse job result from Redis.
// prefix is 'device' or 'template' — determines the API endpoint used.
// When done, reloads all data and re-renders the unified list.
function _startJobPoll(jobId, prefix) {
    const key = (prefix || 'device') + '_' + jobId;
    if (_devJobTimers[key]) return;
    const endpoint = prefix === 'template'
        ? '/api/v1/templates/jobs/' + jobId
        : '/api/v1/blackbox/jobs/' + jobId;

    _devJobTimers[key] = setInterval(async function() {
        try {
            var res = await api('GET', endpoint);
            // 202 Accepted means still pending
            if (!res || res.status === 'pending') return;

            clearInterval(_devJobTimers[key]);
            delete _devJobTimers[key];

            if (res.status === 'error') {
                showPageAlert(
                    '<i class="fa-solid fa-circle-xmark" style="margin-right:6px"></i>' +
                    'Parse failed: ' + esc((res.error) || 'unknown error'),
                    'danger', 12000
                );
                await loadProjects();
                renderUnifiedList();
                return;
            }

            // Success — reload and re-render.
            await loadProjects();
            renderUnifiedList();
            if (prefix === 'template') {
                const name = res.displayName || '';
                showPageAlert(
                    '<i class="fa-solid fa-circle-check" style="margin-right:6px"></i>' +
                    (name ? `<strong>${esc(name)}</strong> ` : '') + 'Template imported successfully!',
                    'success'
                );
            } else {
                var count = (res.devices && res.devices.length) || 0;
                showPageAlert(
                    '<i class="fa-solid fa-circle-check" style="margin-right:6px"></i>' +
                    count + ' device' + (count !== 1 ? 's' : '') + ' imported successfully!',
                    'success'
                );
            }
        } catch (e) { /* network hiccup — retry next tick */ }
    }, DEV_POLL_INTERVAL_MS);
}

// Keep backward-compat alias used by existing code paths.
function _devStartJobPoll(jobId) { _startJobPoll(jobId, 'device'); }

// ─── Unified Properties Modal ─────────────────────────────────────────────────
//
// Opens the ⚙ modal for either a device or a template.
// The specialist can edit tags, category, subcategory, visibility, and
// publishing flags. Language, type, and GitHub URL are read-only.

export function openUnifiedPropertiesModal(id, kind) {
    // Projects (local drafts) use their own properties modal — name,
    // visibility, language. The `openPropertiesModal` function is the
    // long-standing handler from the tree-based UI, kept intact.
    if (kind === 'project') {
        openPropertiesModal(id);
        return;
    }
    const item = kind === 'template'
        ? (_templates || []).find(t => t.id === id)
        : (_devices   || []).find(d => d.id === id);
    if (!item) return;

    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;

    const isPublic = item.visibility === 'public';
    const isReady  = !!item.readyToUse;
    const catId    = item.categoryId    || '';
    const subId    = item.subcategoryId || '';

    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';

    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:480px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease;
            max-height:90vh;overflow-y:auto">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">Properties</h2>
  <p style="color:var(--text-muted);font-size:13px;margin-bottom:24px">
    ${esc(item.displayNameHuman || item.githubRepo || id)}
    &nbsp;<span style="font-size:11px;background:var(--bg-2);padding:1px 6px;border-radius:99px">
      ${kind}
    </span>
  </p>
  <div id="unified-props-err" class="alert alert-danger" style="display:none"></div>

  <!-- Read-only source URL -->
  <div class="fg">
    <label style="color:var(--text-muted)">GitHub Release URL</label>
    <div style="font-size:13px;padding:8px 12px;background:var(--bg-2);border-radius:var(--r);
                word-break:break-all;color:var(--text-secondary)">${esc(item.githubUrl || '')}</div>
  </div>

  <div class="fg">
    <label>Tags <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(optional)</span></label>
    <input id="up-tags" class="fc" type="text" value="${esc(item.tags || '')}"
           placeholder="e.g. sensor, i2c, temperature" style="font-size:13px">
  </div>

  <div style="display:flex;gap:12px">
    <div class="fg" style="flex:1">
      <label>Menu Category</label>
      <select id="up-category" class="fc" onchange="window._upCategoryChange()" style="font-size:13px">
        <option value="">— None —</option>
      </select>
    </div>
    <div class="fg" style="flex:1">
      <label>Subcategory</label>
      <select id="up-subcategory" class="fc" style="font-size:13px">
        <option value="">— None —</option>
      </select>
    </div>
  </div>

  <div class="fg">
    <label>Visibility *</label>
    <div style="display:flex;gap:12px;margin-top:4px">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)"
             id="up-vis-pub-label" style="${isPublic ? 'border-color:var(--primary)' : ''}">
        <input type="radio" name="up-vis" value="public" onchange="window._upVisChange()"
               ${isPublic ? 'checked' : ''}> Public
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)"
             id="up-vis-prv-label">
        <input type="radio" name="up-vis" value="private" onchange="window._upVisChange()"
               ${!isPublic ? 'checked' : ''}> Private
      </label>
    </div>
  </div>

  <div id="up-publish-wrap" style="${isPublic ? '' : 'display:none'}">
    ${_buildPublishSection('up', item.publishToFeed, item.publishToSearch, item.readyToUse, false)}
  </div>

  <div style="display:flex;gap:10px;margin-top:24px">
    <button class="btn btn-secondary btn-sm" onclick="closeCreateProjectModal()" style="flex:1">Cancel</button>
    <button class="btn btn-primary btn-sm" id="up-save-btn"
            onclick="window._saveUnifiedProperties('${esc(id)}','${esc(kind)}')" style="flex:2">
      <i class="fa-solid fa-floppy-disk"></i> Save
    </button>
  </div>
</div>`;

    // Populate category selects after DOM is ready.
    _populateCategorySelect('up-category', 'up-subcategory', catId, subId);
    window._upVisChange();
}

window._upCategoryChange = async function() {
    const catSel = document.getElementById('up-category');
    const subSel = document.getElementById('up-subcategory');
    if (!catSel || !subSel) return;
    const categoryId = catSel.value;
    if (!categoryId) {
        subSel.innerHTML = '<option value="">— None —</option>';
        subSel.disabled = true;
        return;
    }
    subSel.disabled = true;
    subSel.innerHTML = '<option value="">Loading…</option>';
    const r = await api('GET', `/api/v1/projects/meta/subcategories?categoryId=${encodeURIComponent(categoryId)}`);
    const subs = r?.data || [];
    subSel.innerHTML = '<option value="">— None —</option>' +
        subs.map(s => `<option value="${esc(s.id)}">${esc(s.name)}</option>`).join('');
    subSel.disabled = subs.length === 0;
};

window._upVisChange = function() {
    const val = document.querySelector('input[name="up-vis"]:checked')?.value;
    const pub  = document.getElementById('up-vis-pub-label');
    const prv  = document.getElementById('up-vis-prv-label');
    const wrap = document.getElementById('up-publish-wrap');
    if (pub) pub.style.borderColor = val === 'public'  ? 'var(--primary)' : 'var(--border)';
    if (prv) prv.style.borderColor = val === 'private' ? 'var(--primary)' : 'var(--border)';
    if (wrap) wrap.style.display = val === 'public' ? '' : 'none';
};

window._saveUnifiedProperties = async function(id, kind) {
    const btn        = document.getElementById('up-save-btn');
    const errEl      = document.getElementById('unified-props-err');
    const tags       = document.getElementById('up-tags')?.value?.trim() || '';
    const categoryId    = document.getElementById('up-category')?.value || '';
    const subcategoryId = document.getElementById('up-subcategory')?.value || '';
    const visibility = document.querySelector('input[name="up-vis"]:checked')?.value || 'private';

    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }

    // Devices use a single PUT /meta endpoint that handles tags + visibility + category.
    // Templates have separate endpoints for each — we call them in parallel.
    let saveErr = null;
    if (kind === 'template') {
    const [vr, tr] = await Promise.all([
            api('PUT', `/api/v1/templates/${id}/visibility`, { visibility }),
            api('PUT', `/api/v1/templates/${id}/tags`,       { tags, categoryId, subcategoryId }),
    ]);
        saveErr = vr?.metadata?.error || tr?.metadata?.error;
    } else {
        const r = await api('PUT', `/api/v1/blackbox/${id}/meta`, {
            tags, visibility, categoryId, subcategoryId,
            publishToFeed:   false,
            publishToSearch: false,
            readyToUse:      false,
        });
        saveErr = r?.metadata?.error;
    }

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Save'; }

    if (saveErr) {
        if (errEl) { errEl.textContent = saveErr; errEl.style.display = 'block'; }
        return;
    }

    closeCreateProjectModal();
    await loadProjects();
    renderUnifiedList();
    showPageAlert('Properties saved.', 'success');
};

// ─── Unified Delete ───────────────────────────────────────────────────────────

export function confirmDeleteUnified(id, kind, displayName) {
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;

    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';
    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:440px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:8px;color:var(--danger)">
    <i class="fa-solid fa-trash-can" style="margin-right:8px"></i>Delete ${esc(kind)}?
  </h2>
  <p style="font-size:14px;color:var(--text-secondary);margin-bottom:20px">
    This will permanently delete <strong>${esc(displayName)}</strong> and all its data.
    This action cannot be undone.
  </p>
  <p style="font-size:13px;color:var(--text-muted);margin-bottom:8px">
    Type <strong>${esc(displayName)}</strong> to confirm:
  </p>
  <input id="del-unified-input" class="fc" type="text"
         placeholder="${esc(displayName)}"
         oninput="window._delUnifiedCheck('${esc(displayName)}')"
         style="font-size:13px;margin-bottom:16px">
  <div style="display:flex;gap:10px">
    <button class="btn btn-secondary btn-sm" onclick="closeCreateProjectModal()" style="flex:1">Cancel</button>
    <button class="btn btn-danger btn-sm" id="del-unified-btn" disabled
            onclick="window._execDeleteUnified('${esc(id)}','${esc(kind)}')" style="flex:2">
      <i class="fa-solid fa-trash-can"></i> Delete permanently
    </button>
  </div>
</div>`;
}

window._delUnifiedCheck = function(expected) {
    const input = document.getElementById('del-unified-input');
    const btn   = document.getElementById('del-unified-btn');
    if (!btn || !input) return;
    btn.disabled = input.value.trim() !== expected;
};

window._execDeleteUnified = async function(id, kind) {
    const btn = document.getElementById('del-unified-btn');
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Deleting…'; }
    // Each kind has its own DELETE endpoint:
    //   project  → /api/v1/projects/:id    (local draft)
    //   template → /api/v1/templates/:id   (published template)
    //   device   → /api/v1/blackbox/:id    (GitHub-sourced device)
    let endpoint;
    if (kind === 'project') {
        endpoint = `/api/v1/projects/${id}`;
    } else if (kind === 'template') {
        endpoint = `/api/v1/templates/${id}`;
    } else {
        endpoint = `/api/v1/blackbox/${id}`;
    }
    const r = await api('DELETE', endpoint);
    closeCreateProjectModal();
    if (r?.metadata?.status >= 400) {
        showPageAlert(r.metadata.error || 'Could not delete.', 'danger');
        return;
    }
    await loadProjects();
    renderUnifiedList();
    showPageAlert('Deleted permanently.', 'success');
};

// ─── Window registrations for unified list ───────────────────────────────────
window.toggleUnifiedRow           = toggleUnifiedRow;
window.openUnifiedPropertiesModal = openUnifiedPropertiesModal;
window.confirmDeleteUnified       = confirmDeleteUnified;
window.onCategoryChange           = onCategoryChange;
// _openTypeHelp is called by the (?) buttons inside the Project Type cards
// in the Create modal. It must be on window because the cards use inline onclick.
window._openTypeHelp              = _openTypeHelp;
