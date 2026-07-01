// pages/projects.js — Project + Template management page for the IoTMaker SPA.
import { showAlert, showConfirm, showPrompt } from '../utils.js';
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
//   POST   /api/blackbox/parse                        — Go AST parse (reused from blackbox)
//   POST   /api/blackbox/analyze                      — semantic analysis (reused from blackbox)
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
let _monacoReady      = false;
let _parsedData       = null;
let _currentVersion   = 0;
let _nextVersion      = 1;
let _codeVersions     = [];     // [{id, version, filename, source, createdAt}]
let _needsExplicitParse = false;
let _parseStatusType  = '';
let _analyzeTimer     = null;
let _analyzeSeq       = 0;
let _formAlertTimer   = null;

// Current filename for the Monaco editor.
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

// ─── Entry point ──────────────────────────────────────────────────────────────

// leaveProjects is called by the router when navigating away from this page.
// It stops template poll timers and cleans up Monaco instances.
export function leaveProjects() {
    // Stop all template polls.
    Object.keys(_tplPollTimers).forEach(id => {
        clearInterval(_tplPollTimers[id]);
        delete _tplPollTimers[id];
    });
    _tplUploading = false;
}

export function renderProjects(root) {
    _root = root;

    // Do not overwrite the help panel if it is currently open.
    if (_projHelpOpen) return;

    // Clean up Monaco instances — DOM element is gone after navigation.
    try { if (_monacoInst) { _monacoInst.dispose(); } } catch(e) {}
    _monacoInst  = null;
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

    // Load both tabs in parallel; render whichever is active.
    Promise.all([loadMeta(), loadProjects(), _tplLoadList()]).then(() => {
        _projects.forEach(p => _expandedLangs.add(p.programmingLanguageId));
        _renderActiveTab();
    });
}

// ─── Page shell with tabs ─────────────────────────────────────────────────────

function _buildPageShell() {
    return `
<div style="max-width:1200px;margin:0 auto">
  <div style="padding:24px 28px 0;border-bottom:1px solid var(--border)">
    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px">
      <div>
        <h1 style="font-size:22px;font-weight:700;margin:0">
          <i class="fa-solid fa-folder-open" style="color:var(--primary);margin-right:10px"></i>Projects
        </h1>
        <p style="color:var(--text-muted);font-size:14px;margin:4px 0 0">Manage your projects and templates</p>
      </div>
      <button class="btn btn-primary btn-sm" onclick="openCreateProjectModal()">
        <i class="fa-solid fa-plus"></i> New Project
      </button>
    </div>
    <div style="display:flex;gap:0">
      <!-- Projects tab — the ? opens the Projects guide inline -->
      <button id="tab-btn-projects" onclick="switchTab('projects')"
              style="padding:8px 20px;border:none;background:none;cursor:pointer;
                     font-size:14px;font-weight:600;color:var(--primary);
                     border-bottom:2px solid var(--primary);
                     display:flex;align-items:center;gap:8px">
        <i class="fa-solid fa-folder-tree"></i>Projects
        <span onclick="event.stopPropagation();window._projOpenHelp()"
              title="Projects guide"
              style="display:inline-flex;align-items:center;justify-content:center;
                     width:18px;height:18px;border-radius:50%;
                     background:rgba(0,0,0,.06);cursor:pointer;
                     font-size:11px;color:inherit;flex-shrink:0">
          <i class="fa-solid fa-circle-question"></i>
        </span>
      </button>
      <!-- Templates tab — the ? opens the Templates authoring guide inline -->
      <button id="tab-btn-templates" onclick="switchTab('templates')"
              style="padding:8px 20px;border:none;background:none;cursor:pointer;
                     font-size:14px;font-weight:500;color:var(--text-muted);
                     border-bottom:2px solid transparent;
                     display:flex;align-items:center;gap:8px">
        <i class="fa-solid fa-file-export"></i>Templates
        <span onclick="event.stopPropagation();window._tplOpenHelp()"
              title="Template authoring guide"
              style="display:inline-flex;align-items:center;justify-content:center;
                     width:18px;height:18px;border-radius:50%;
                     background:rgba(0,0,0,.06);cursor:pointer;
                     font-size:11px;color:inherit;flex-shrink:0">
          <i class="fa-solid fa-circle-question"></i>
        </span>
      </button>
    </div>
  </div>
  <div id="proj-alert" style="margin:12px 28px 0;display:none"></div>
  <div id="proj-tree" id="tab-content-projects" style="padding:16px 12px">
    <div style="text-align:center;padding:40px;color:var(--text-muted)">
      <i class="fa-solid fa-spinner fa-spin" style="font-size:20px"></i>
      <p style="margin-top:10px">Loading projects…</p>
    </div>
  </div>
  <div id="tab-content-templates" style="display:none;padding:16px 28px">
    <div style="text-align:center;padding:40px;color:var(--text-muted)">
      <i class="fa-solid fa-spinner fa-spin" style="font-size:20px"></i>
      <p style="margin-top:10px">Loading templates…</p>
    </div>
  </div>
  <div id="proj-modal-overlay" style="display:none"></div>
</div>
${_tplBuildStyles()}`;
}

// switchTab switches between the Projects and Templates tabs.
export function switchTab(tab) {
    _activeTab = tab;
    const pBtn  = document.getElementById('tab-btn-projects');
    const tBtn  = document.getElementById('tab-btn-templates');
    const pPane = document.getElementById('proj-tree') || document.getElementById('tab-content-projects');
    const tPane = document.getElementById('tab-content-templates');
    if (!pBtn || !tBtn) return;

    if (tab === 'projects') {
        pBtn.style.cssText = 'padding:8px 20px;border:none;background:none;cursor:pointer;font-size:14px;font-weight:600;color:var(--primary);border-bottom:2px solid var(--primary)';
        tBtn.style.cssText = 'padding:8px 20px;border:none;background:none;cursor:pointer;font-size:14px;font-weight:500;color:var(--text-muted);border-bottom:2px solid transparent';
        if (pPane) pPane.style.display = '';
        if (tPane) tPane.style.display = 'none';
        renderTree();
    } else {
        pBtn.style.cssText = 'padding:8px 20px;border:none;background:none;cursor:pointer;font-size:14px;font-weight:500;color:var(--text-muted);border-bottom:2px solid transparent';
        tBtn.style.cssText = 'padding:8px 20px;border:none;background:none;cursor:pointer;font-size:14px;font-weight:600;color:var(--primary);border-bottom:2px solid var(--primary)';
        if (pPane) pPane.style.display = 'none';
        if (tPane) tPane.style.display = '';
        _tplRenderList();
    }
}

function _renderActiveTab() {
    if (_activeTab === 'projects') renderTree();
    else _tplRenderList();
}

// ─── Data loading ─────────────────────────────────────────────────────────────

async function loadProjects() {
    const r = await api('GET', '/api/v1/projects');
    if (r?.metadata?.status === 200) _projects = r.data || [];
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

function renderTree() {
    const container = document.getElementById('proj-tree');
    if (!container) return;

    if (_projects.length === 0) {
        container.innerHTML = `
<div style="text-align:center;padding:60px 20px;color:var(--text-muted)">
  <i class="fa-solid fa-folder-open" style="font-size:48px;opacity:.3;display:block;margin-bottom:16px"></i>
  <p style="font-size:15px;margin-bottom:8px">No projects yet</p>
  <p style="font-size:13px">Click <strong>New Project</strong> to get started.</p>
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
    container.innerHTML = html;
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
// a Monaco editor for a new file. The "Upload" button is shown for all sections
// when canAdd is true. For code and docs, canAdd is determined by whether there
// is already a single file present (these sections allow only one code file;
// docs allows many but the Create button triggers a modal to name the file).
function renderSection(p, section, icon, label, fileList, singleFile) {
    const canAdd = !singleFile || fileList.length === 0;
    const accept = section==='code' ? '.go' : section==='img' ? 'image/*' : '.md';
    const inputId = `finput-${p.id}-${section}`;

    let filesHtml = fileList?.length
        ? fileList.map(f => renderFileRow(p, section, f)).join('')
        : `<div style="font-size:13px;color:var(--text-muted);padding:3px 8px;font-style:italic">No files yet</div>`;

    // The code section and the docs section both get a "Create" button that
    // opens Monaco. Code opens the Go editor; docs opens the Markdown editor.
    let createBtn = '';
    if (canAdd && section === 'code') {
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

    const addBtn = canAdd ? `
<button title="Upload" onclick="triggerFileUpload('${p.id}','${section}')"
  style="padding:3px 8px;background:none;border:1px solid var(--border);
         border-radius:var(--r);cursor:pointer;color:var(--primary);font-size:11px;
         display:flex;align-items:center;gap:4px;transition:background var(--tr)"
  onmouseover="this.style.background='var(--info-bg)'"
  onmouseout="this.style.background='none'">
  <i class="fa-solid fa-upload" style="font-size:10px"></i> Upload
</button>` : '';

    return `
<div style="margin-bottom:8px">
  <div style="display:flex;align-items:center;gap:8px;padding:4px 6px;
              background:var(--bg-surface);border-radius:var(--r);margin-bottom:3px">
    <i class="${icon}" style="color:var(--text-muted);font-size:12px;width:14px;text-align:center"></i>
    <span style="font-size:12px;font-weight:600;color:var(--text-secondary);flex:1">${label}</span>
    ${createBtn}${addBtn}
  </div>
  <input type="file" id="${inputId}" accept="${accept}" style="display:none"
    onchange="onFileSelected(event,'${p.id}','${section}')">
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
        rowAction = `openCodeEditor('${p.id}')`;
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
export async function openCodeEditor(projectId) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    _editProject = p;
    _editMode    = true;
    _parsedData  = null;
    _needsExplicitParse = false;
    _parseStatusType    = '';
    _codeVersions       = [];
    _diffHunks          = [];
    _diffChoices        = [];

    // Reset version state.
    _currentVersion = 0;
    _nextVersion    = 1;

    renderEditorView(_root);

    // Load code from server.
    const r = await api('GET', `/api/v1/projects/${projectId}/files/code`);
    if (r?.metadata?.status !== 200) {
        projSetParseStatus('Failed to load code from server.', 'error');
        return;
    }
    const { source, version, filename, versions } = r.data;
    _currentVersion = version || 0;
    _nextVersion    = _currentVersion + 1;
    _codeVersions   = versions || [];

    // When there are no saved versions yet, derive the default filename
    // from the project name: "My Sensor Board" → "my_sensor_board.go".
    if (!_codeVersions.length && !filename) {
        _defaultFilename = slugifyFilename(_editProject.name) + '.go';
    } else {
        _defaultFilename = filename || (slugifyFilename(_editProject.name) + '.go');
    }

    const initCode = source || '';

    // Ensure project files are loaded so the image autocomplete provider
    // has the img list available as soon as the editor opens.
    if (!_projectFiles[projectId]) {
        await loadProjectFiles(projectId);
    }

    projMountMonaco(initCode);
    projRegisterSlashMenu(projectId);
    updateVersionBar();

    // Trigger initial analysis if there's code.
    if (initCode.trim()) {
        projScheduleAnalyze();
    }
}

// projCloseEditor returns to the tree view, preserving tree expansion state.
export function projCloseEditor() {
    _editMode    = false;
    _editProject = null;
    if (_monacoInst) { _monacoInst.dispose(); _monacoInst = null; }
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

function renderEditorView(root) {
    const p = _editProject;
    if (!p || !root) return;

    _injectEditorStyles();
    root.innerHTML = `
<div style="display:flex;flex-direction:column;height:100%">

  <!-- ── Breadcrumb + toolbar ── -->
  <div style="display:flex;align-items:center;gap:10px;padding:10px 20px;
              background:var(--bg-surface);border-bottom:1px solid var(--border);
              flex-wrap:wrap">
    <button class="btn btn-ghost btn-sm" onclick="projCloseEditor()" title="Back to projects">
      <i class="fa-solid fa-arrow-left"></i> Projects
    </button>
    <span style="color:var(--text-muted);font-size:13px">›</span>
    <span style="font-size:13px;font-weight:600;color:var(--text-primary)">${esc(p.name)}</span>
    <span style="color:var(--text-muted);font-size:13px">› code</span>

    <!-- version bar (dropdown + Diff button) -->
    <div id="proj-version-bar" style="display:flex;align-items:center;gap:6px;margin-left:4px"></div>

    <div style="margin-left:auto;display:flex;gap:6px">
      <button class="btn btn-ghost btn-sm" onclick="projReset()" title="Clear parse result and error markers">
        <i class="fa-solid fa-broom"></i> Clear parse
      </button>
      <button class="btn btn-ghost btn-sm" onclick="projParse()" title="Parse Go code">
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
      <button class="btn btn-ghost btn-sm" onclick="promptRenameCode('${p.id}')"
              title="Rename source file">
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
.proj-add-flag{font-size:10px;background:none;border:1px dashed var(--border);
  border-radius:99px;padding:1px 7px;cursor:pointer;color:var(--text-muted);
  font-family:var(--font);transition:border-color .12s,color .12s}
.proj-add-flag:hover{border-color:var(--primary);color:var(--primary)}
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
// It is called by openCodeEditor after the editor shell is rendered.
// Lazy-loads the CDN script once; subsequent calls mount immediately.
function projMountMonaco(init) {
    if (window.monaco) {
        _monacoReady = true;
        _doMount(init);
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
            require(['vs/editor/editor.main'], () => { _monacoReady = true; _doMount(init); });
        };
        document.head.appendChild(s);
    }
}

function _doMount(init) {
    const wrap = document.getElementById('proj-editor-wrap');
    if (!wrap) return;
    const ta = document.getElementById('proj-fallback');
    if (ta) ta.remove();
    if (_monacoInst) { _monacoInst.dispose(); _monacoInst = null; }

    const div = document.createElement('div');
    // Height = viewport minus: breadcrumb(44) + tab-bar(42) + status(34) + bottom(52) + borders(14) ≈ 186px
    div.style.cssText = 'height:calc(100vh - 186px);min-height:320px;width:100%';
    wrap.appendChild(div);

    _monacoInst = monaco.editor.create(div, {
        value: init, language: 'go', theme: 'vs',
        wordWrap: 'off', lineNumbers: 'on',
        minimap: { enabled: true }, scrollBeyondLastLine: false,
        fontSize: 13, fontFamily: "'Fira Code','Consolas',monospace",
        automaticLayout: true, glyphMargin: true,
        wordBasedSuggestions: false,
        quickSuggestions: false,
        suggestOnTriggerCharacters: false,
        parameterHints: { enabled: false },
        // Go uses tabs, not spaces. These three options together ensure the
        // Tab key inserts a real tab character (\t) and that existing tabs
        // are displayed at the standard Go width of 4 columns.
        insertSpaces: false,
        tabSize: 4,
        detectIndentation: false,
    });

    _monacoInst.onDidChangeModelContent(() => {
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
        if (_parsedData) _needsExplicitParse = true;
        const prev = document.getElementById('proj-tab-preview');
        if (prev && _parsedData) {
            prev.innerHTML = '<p style="padding:24px;color:var(--text-muted);font-size:13px">' +
                '<i class="fa-solid fa-circle-notch fa-spin" style="margin-right:6px"></i>Updating preview…</p>';
        }
    });
}

function projGetCode() {
    if (_monacoInst) return _monacoInst.getValue();
    return document.getElementById('proj-fallback')?.value || '';
}

// ─── Parse / Analyze ──────────────────────────────────────────────────────────

export async function projParse() {
    const rawCode = projGetCode();
    if (!rawCode.trim()) { projSetParseStatus('Code is empty', 'warning'); return; }

    projSetParseStatus('<i class="fa-solid fa-circle-notch fa-spin"></i> Parsing…', '');
    try {
        const res  = await fetch('/api/blackbox/parse', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code: rawCode }),
        });
        const json = await res.json();
        if (json?.metadata?.status !== 200) {
            projSetParseStatus('✗ ' + (json?.metadata?.error || 'Parse error'), 'error');
            return;
        }

        projSetParseStatus('<i class="fa-solid fa-circle-notch fa-spin"></i> Analysing…', '');
        clearTimeout(_analyzeTimer);
        const analyzeResult = await projRunAnalyze();
        if (analyzeResult?.hasErrors) {
            projSetParseStatus(projFormatAnalyzeErrors(analyzeResult.diagnostics), 'error');
            return;
        }

        projApplyMonacoMarkers([]);
        projFormAlert('', '');
        _needsExplicitParse = false;

        _parsedData = json.data;
        _parsedData.sourceCode = rawCode;
        const nMethods  = _parsedData.methods?.length  || 0;
        const nSettings = _parsedData.settings?.length || 0;
        const nWarns    = _parsedData.parseWarnings?.length || 0;
        const nPins     = (_parsedData.methods || []).reduce((a, m) =>
            a + (m.inputs?.length || 0) + (m.outputs?.length || 0), 0);
        const warnPart = nWarns
            ? ` · <span style="color:var(--warning)">⚠ ${nWarns} pin(s) missing connection:</span>`
            : '';
        projSetParseStatus(
            `✓ OK — ${nMethods} method(s) · ${nPins} pin(s) · ${nSettings} setting(s)${warnPart}`,
            'ok'
        );
        projRenderPreview(true);

    } catch (e) {
        projSetParseStatus('✗ Network error: ' + e.message, 'error');
    }
}

function projSetParseStatus(msg, type) {
    const el = document.getElementById('proj-parse-status');
    if (!el) return;
    _parseStatusType = type;

    if (type === 'error') {
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
    const code = projGetCode();
    if (!code.trim() || !_monacoInst) return null;
    try {
        const res = await fetch('/api/blackbox/analyze', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code }),
        });
        if (!res.ok) return null;
        if (_analyzeSeq !== seq) return null;

        const json = await res.json();
        if (json?.metadata?.status !== 200) return null;

        const result = json.data;
        projApplyMonacoMarkers(result.diagnostics || []);

        if (result.hasErrors) {
            projSetParseStatus(projFormatAnalyzeErrors(result.diagnostics), 'error');
        } else if (_parsedData) {
            const nMethods  = _parsedData.methods?.length  || 0;
            const nSettings = _parsedData.settings?.length || 0;
            const nPins     = (_parsedData.methods || []).reduce((a, m) =>
                a + (m.inputs?.length || 0) + (m.outputs?.length || 0), 0);
            projSetParseStatus(
                `✓ OK — ${nMethods} method(s) · ${nPins} pin(s) · ${nSettings} setting(s)`,
                'ok'
            );
            projAutoReparse();
        } else {
            projSetParseStatus('', '');
        }
        return result;
    } catch { return null; }
}

function projApplyMonacoMarkers(diagnostics) {
    if (!_monacoInst || !window.monaco) return;
    const model = _monacoInst.getModel();
    if (!model) return;
    const sevMap = { error: 8, warning: 4, info: 2, hint: 1 };
    monaco.editor.setModelMarkers(model, 'proj-analyzer', diagnostics.map(d => ({
        severity:        sevMap[d.severity] ?? 8,
        message:         d.message.replace(/^blackbox\.go:\d+:\d+:\s*/, '').trim(),
        startLineNumber: d.line, startColumn: d.col,
        endLineNumber:   d.endLine, endColumn: d.endCol,
        source:          d.source,
    })));
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
        `<span style="font-family:var(--mono);font-weight:700">line&nbsp;${d.line}:</span> ${
            esc(d.message.replace(/^blackbox\.go:\d+:\d+:\s*/, '').trim())}`
    ).join(' &nbsp;·&nbsp; ');
    const more = errors.length > 2
        ? ` <span style="opacity:.65">(+${errors.length - 2} more)</span>` : '';
    return `✗ ${errors.length} error(s) — ${shown}${more}`;
}

async function projAutoReparse() {
    if (!_parsedData) return;
    const raw = projGetCode();
    if (!raw.trim()) return;
    try {
        const res = await fetch('/api/blackbox/parse', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code: raw }),
        });
        const json = await res.json();
        if (json?.metadata?.status !== 200) return;
        const prev = _parsedData.sourceCode;
        _parsedData = json.data;
        _parsedData.sourceCode = prev;
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
    ['editor', 'preview', 'debug'].forEach(t => {
        document.getElementById(`proj-tab-btn-${t}`)?.classList.toggle('active', t === tab);
    });
    const editorWrap = document.getElementById('proj-editor-wrap');
    const previewDiv = document.getElementById('proj-tab-preview');
    const debugDiv   = document.getElementById('proj-tab-debug');
    if (editorWrap) editorWrap.style.display = tab === 'editor'  ? '' : 'none';
    if (previewDiv) previewDiv.style.display  = tab === 'preview' ? '' : 'none';
    if (debugDiv)   debugDiv.style.display    = tab === 'debug'   ? '' : 'none';
    if (tab === 'editor' && _monacoInst) setTimeout(() => _monacoInst.layout(), 0);
}

function projBuildDevice(bb) {
    let warnHtml = '';
    if (bb.parseWarnings?.length) {
        warnHtml = `<div style="margin-bottom:16px;padding:12px 16px;background:#FFF8E1;
          border:1px solid #FFD97D;border-radius:var(--r);font-size:13px">
          <strong>⚠ connection: missing on ${bb.parseWarnings.length} pin(s)</strong>
          <ul style="margin:6px 0 0 18px;color:#7A5C00">${
            bb.parseWarnings.map(w => `<li>${esc(w)}</li>`).join('')}</ul></div>`;
    }
    const label = bb.structLabel || bb.displayName || bb.structName || 'Device';
    const icon  = bb.structIcon  || 'cube';
    const cat   = bb.category    || '';
    const cards = (bb.methods || []).map((m, mi) => projBuildMethodCard(bb, m, mi, label, icon, cat));
    return warnHtml + `<div class="proj-blocks-container">${cards.join('')}</div>
<div class="proj-legend">
  <span class="proj-legend-item"><span style="color:var(--primary);font-size:16px">◎</span> optional</span>
  <span class="proj-legend-item"><span style="color:var(--success);font-size:16px">◉</span> mandatory</span>
</div>`;
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
    const fc = style === 'b' ? 'fa-brands'
        : style === 'r' ? 'fa-regular'
            : style === 's' ? 'fa-solid'
                : window.FA_BRANDS_SET?.has(candidate) ? 'fa-brands' : 'fa-solid';
    return `<i class="${fc} fa-${esc(candidate)}"></i>`;
}

function projBuildPin(pin, mi, pi, dir) {
    const isInput = dir === 'input';
    const sym     = pin.missingConn ? '⊙' : pin.connection === 'mandatory' ? '◉' : '◎';
    const dotSty  = pin.missingConn ? 'color:#C0392B' : '';
    const dot = `<span class="proj-dot ${isInput ? 'in' : 'out'}" style="${dotSty}">${sym}</span>`;
    const badges  = projBuildBadges(pin, mi, pi, dir);
    const addFlag = `<button class="proj-add-flag" onclick="projAddFlag(${mi},'${dir}',${pi})" title="Add flag">+ flag</button>`;
    const nameEl  = `<span class="proj-pin-name" id="pjp-${mi}-${dir}-${pi}"
        contenteditable="false" onclick="projPinStartRename(this)"
        onblur="projPinFinishRename(this,${mi},'${dir}',${pi})"
        onkeydown="if(event.key==='Enter'||event.key==='Escape'){this.blur()}"
      >${esc(pin.name)}</span>`;
    const typeEl  = `<span class="proj-pin-type">${esc(pin.type)}</span>`;
    const dragEl  = `<span class="proj-drag" draggable="false">⠿</span>`;
    const tooltip = projBuildTooltip(pin);
    const badgesArea = `<div class="proj-badges">${badges}${addFlag}</div>`;
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
    if (pin.missingConn) b += `<span class="proj-badge warn">⚠ connection: missing</span>`;
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

function projBuildTooltip(pin) {
    const lines = [];
    if (pin.doc) lines.push(`<strong>${esc(pin.doc)}</strong>`);
    if (pin.missingConn) lines.push(`<span style="color:#F5B7B1">⚠ connection: missing</span>`);
    else if (pin.connection === 'mandatory') lines.push('◉ <strong>mandatory</strong> connection');
    else if (pin.connection === 'optional')  lines.push('◎ <strong>optional</strong> connection');
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

export function projAddFlag(mi, dir, pi) {
    const name = await showPrompt('Flag name:', '', { placeholder: 'optional_connection' });
    if (!name?.trim()) return;
    if (!_parsedData) return;
    const pins = dir === 'input' ? _parsedData.methods[mi]?.inputs : _parsedData.methods[mi]?.outputs;
    if (!pins?.[pi]) return;
    if (!pins[pi].flags) pins[pi].flags = [];
    pins[pi].flags.push(name.trim());
    projRenderPreview();
}

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

export async function projSave() {
    if (!_editProject) return;

    const rawCode = projGetCode();
    if (!rawCode.trim()) {
        projSaveBtnWarn('Nothing to save — editor is empty');
        return;
    }
    if (_needsExplicitParse) {
        projSaveBtnWarn('Parse first — code has changed');
        return;
    }

    let filename = _defaultFilename || 'main.go';
    if (_codeVersions.length > 0) {
        filename = _codeVersions[0].filename || filename;
    }

    const btn = document.getElementById('proj-save-btn');
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }

    const r = await api('POST',
        `/api/v1/projects/${_editProject.id}/files/code/versions`,
        { source: rawCode, filename }
    );

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Save'; }

    if (r?.metadata?.status === 200) {
        const saved = r.data;
        _currentVersion  = saved.version;
        _nextVersion     = _currentVersion + 1;
        _needsExplicitParse = false;
        _defaultFilename = saved.filename || _defaultFilename;

        const rv = await api('GET', `/api/v1/projects/${_editProject.id}/files/code/versions`);
        if (rv?.metadata?.status === 200) _codeVersions = rv.data || [];

        updateVersionBar();
        projFormAlert('', '');
        const st = document.getElementById('proj-save-status');
        if (st) { st.textContent = `✓ Saved as v${saved.version}`; setTimeout(() => { st.textContent = ''; }, 3000); }
    } else {
        projFormAlert('✗ ' + (r?.metadata?.error || 'Save failed'), 'danger', 8000);
    }
}

export function projReset() {
    _parsedData         = null;
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
    projApplyMonacoMarkers([]);
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

export function projVersionChange(versionId) {
    const editorCode = projGetCode();
    const latest     = _codeVersions[0];
    if (!latest) return;
    if (editorCode !== (latest.source || '')) {
        const ok = await showConfirm('The editor has unsaved changes. Switching versions will replace the content. Continue?', { okLabel: 'Continue', danger: true });
        if (!ok) {
            const sel = document.getElementById('proj-ver-select');
            const cur = _codeVersions.find(v => v.version === _currentVersion);
            if (sel && cur) sel.value = cur.id;
            return;
        }
    }
    const v = _codeVersions.find(v => v.id === versionId);
    if (!v) return;
    if (_monacoInst) _monacoInst.setValue(v.source || '');
    else { const ta = document.getElementById('proj-fallback'); if (ta) ta.value = v.source || ''; }
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
        await showAlert('warning', 'No saved versions to compare.');
        return;
    }

    const codeMap = {};
    _codeVersions.forEach(v => { codeMap[v.id] = v.source || ''; });
    window._projDiffCodeMap = codeMap;

    const firstId = _codeVersions[0].id;
    const opts    = _codeVersions.map(v =>
        `<option value="${v.id}">v${v.version} — ${v.filename}</option>`
    ).join('');

    function initDiff(savedCode) {
        _projDiffSavedCode = savedCode;
        _diffHunks   = projDiffHunks(savedCode, projGetCode());
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
      <i class="fa-brands fa-golang" style="margin-right:4px;color:#00ADD8"></i>Result
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
    if (_monacoInst) _monacoInst.setValue(result);
    else { const ta = document.getElementById('proj-fallback'); if (ta) ta.value = result; }
    document.getElementById('proj-diff-modal')?.remove();
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

// ─── Create project modal ─────────────────────────────────────────────────────
//
// The Create modal collects:
//   - Project Name
//   - Programming Language
//   - Project Type: "Custom Code" (a standalone Go project edited in Monaco)
//                   "Custom Project" (a template-based project with configurable devices)
//   - Visibility (public / private)
//   - User Interface Language
//
// Publishing flags are intentionally absent on creation — a brand-new project
// is never published. The owner enables them later via the Properties modal.
//
// Project Type → API "type" value mapping:
//   Custom Code    → "custom_device"   (preserves the DB value used by existing projects)
//   Custom Project → "custom_project"

export function openCreateProjectModal() {
    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;
    const langOpts = _progLangs.map(l =>
        `<option value="${esc(l.id)}">${esc(l.display)}</option>`).join('');
    const uiOpts = _uiLangs.map(l =>
        `<option value="${esc(l.id)}">${esc(l.display)}</option>`).join('');

    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';

    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:480px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease;
            max-height:90vh;overflow-y:auto">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">New Project</h2>
  <p style="color:var(--text-muted);font-size:13px;margin-bottom:24px">Fill in the details below.</p>
  <div id="create-proj-err" class="alert alert-danger" style="display:none"></div>
  <div class="fg">
    <label>Project Name *</label>
    <input id="cp-name" class="fc" type="text" maxlength="100"
      placeholder="e.g. My Sensor Board" oninput="clearCreateError()">
  </div>
  <div class="fg" id="cp-lang-fg">
    <label>Programming Language *</label>
    <select id="cp-lang" class="fc"><option value="">— Select —</option>${langOpts}</select>
  </div>

  <!-- Project Type selector.
       Custom Code    → standalone Go project (Monaco editor, no template devices).
       Custom Project → template-based project (configurable devices from a template ZIP). -->
  <div class="fg">
    <label>Project Type *</label>
    <div style="display:flex;gap:12px;margin-top:4px">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)" id="type-code-label">
        <input type="radio" name="cp-type" value="custom_device" onchange="onTypeChange()" checked>
        <i class="fa-solid fa-code" style="color:var(--primary)"></i>
        <div><div style="font-weight:600">Custom Code</div>
             <div style="font-size:11px;color:var(--text-muted)">Write Go code in the editor</div></div>
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)" id="type-proj-label">
        <input type="radio" name="cp-type" value="custom_project" onchange="onTypeChange()">
        <i class="fa-solid fa-puzzle-piece" style="color:var(--primary)"></i>
        <div><div style="font-weight:600">Custom Project</div>
             <div style="font-size:11px;color:var(--text-muted)">Use a template with devices</div></div>
      </label>
    </div>
  </div>

  <div class="fg">
    <label>Visibility * <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(required)</span></label>
    <div style="display:flex;gap:12px;margin-top:4px">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)" id="vis-pub-label">
        <input type="radio" name="cp-vis" value="public" onchange="onVisChange()">
        <i class="fa-solid fa-globe" style="color:var(--primary)"></i>
        <div><div style="font-weight:600">Public</div>
             <div style="font-size:11px;color:var(--text-muted)">Visible to everyone</div></div>
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid var(--border);border-radius:var(--r);
                    font-size:14px;transition:border-color var(--tr)" id="vis-prv-label">
        <input type="radio" name="cp-vis" value="private" onchange="onVisChange()">
        <i class="fa-solid fa-lock" style="color:var(--text-secondary)"></i>
        <div><div style="font-weight:600">Private</div>
             <div style="font-size:11px;color:var(--text-muted)">Only you can see it</div></div>
      </label>
    </div>
  </div>

  <!-- Publish section — always rendered but checkboxes are disabled on create.
       The section is shown regardless of visibility because the user may toggle
       between public and private before submitting, and the note explains why
       the flags are disabled. -->
  ${_buildPublishSection('cp', false, false, false, true)}

  <div class="fg" id="cp-uilang-fg" style="margin-top:16px">
    <label>User Interface Language * <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(required)</span></label>
    <select id="cp-uilang" class="fc"><option value="">— Select —</option>${uiOpts}</select>
    <div class="fhint">Language for your project's documentation.</div>
  </div>
  <div style="display:flex;gap:10px;margin-top:8px">
    <button class="btn btn-secondary btn-sm" onclick="closeCreateProjectModal()" style="flex:1">Cancel</button>
    <button class="btn btn-primary btn-sm" onclick="submitCreateProject()" style="flex:2" id="cp-submit">
      <i class="fa-solid fa-folder-plus"></i> Create Project
    </button>
  </div>
</div>`;

    // Apply the initial selected state for the default "Custom Code" type.
    onTypeChange();
}

export function closeCreateProjectModal() {
    const o = document.getElementById('proj-modal-overlay');
    if (o) { o.style.display = 'none'; o.innerHTML = ''; }
}

export function clearCreateError() {
    const el = document.getElementById('create-proj-err');
    if (el) el.style.display = 'none';
}

// onVisChange updates the visual selection state of the visibility radio buttons
// in the Create modal. The publish section remains visible but disabled
// regardless of the selected visibility — the note in the section header
// already explains that flags are only available after creation.
export function onVisChange() {
    const val = document.querySelector('input[name="cp-vis"]:checked')?.value;
    const pub = document.getElementById('vis-pub-label');
    const prv = document.getElementById('vis-prv-label');
    if (!pub || !prv) return;
    pub.style.borderColor = val === 'public'  ? 'var(--primary)' : 'var(--border)';
    pub.style.background  = val === 'public'  ? 'var(--info-bg)' : '';
    prv.style.borderColor = val === 'private' ? 'var(--primary)' : 'var(--border)';
    prv.style.background  = val === 'private' ? 'var(--info-bg)' : '';
}

// onTypeChange updates the Project Type radio button visuals and adapts the
// language fields. Custom Project (template) does not need a programming
// language — the devices in the ZIP define the target platform. Instead of
// hiding the field we replace it with an explanatory note to avoid a jarring
// layout jump, and restore the selectors when switching back to Custom Code.
export function onTypeChange() {
    const val  = document.querySelector('input[name="cp-type"]:checked')?.value;
    const code = document.getElementById('type-code-label');
    const proj = document.getElementById('type-proj-label');
    if (!code || !proj) return;
    code.style.borderColor = val === 'custom_device'  ? 'var(--primary)' : 'var(--border)';
    code.style.background  = val === 'custom_device'  ? 'var(--info-bg)' : '';
    proj.style.borderColor = val === 'custom_project' ? 'var(--primary)' : 'var(--border)';
    proj.style.background  = val === 'custom_project' ? 'var(--info-bg)' : '';

    const isTemplate = val === 'custom_project';
    const langFg     = document.getElementById('cp-lang-fg');
    const uilangFg   = document.getElementById('cp-uilang-fg');

    if (isTemplate) {
        // Replace language selector with an explanatory note.
        if (langFg) langFg.innerHTML = `
            <label style="color:var(--text-muted)">Programming Language</label>
            <div class="fhint" style="margin-top:4px">
              <i class="fa-solid fa-circle-info" style="margin-right:4px"></i>
              Defined by the devices inside the template ZIP — no selection needed.
            </div>`;
        if (uilangFg) uilangFg.style.display = 'none';
    } else {
        // Restore the original selectors for Custom Code.
        const langOpts = (_progLangs || []).map(l =>
            `<option value="${esc(l.id)}">${esc(l.display)}</option>`).join('');
        const uiOpts = (_uiLangs || []).map(l =>
            `<option value="${esc(l.id)}">${esc(l.display)}</option>`).join('');
        if (langFg) langFg.innerHTML = `
            <label>Programming Language *</label>
            <select id="cp-lang" class="fc"><option value="">— Select —</option>${langOpts}</select>`;
        if (uilangFg) {
            uilangFg.style.display = '';
            uilangFg.innerHTML = `
                <label>User Interface Language * <span style="font-size:12px;font-weight:400;color:var(--text-muted)">(required)</span></label>
                <select id="cp-uilang" class="fc"><option value="">— Select —</option>${uiOpts}</select>
                <div class="fhint">Language for your project's documentation.</div>`;
        }
    }
}

export async function submitCreateProject() {
    const name       = document.getElementById('cp-name')?.value?.trim();
    const langId     = document.getElementById('cp-lang')?.value;
    const type       = document.querySelector('input[name="cp-type"]:checked')?.value;
    const visibility = document.querySelector('input[name="cp-vis"]:checked')?.value;
    const uiLangId   = document.getElementById('cp-uilang')?.value;
    const errEl      = document.getElementById('create-proj-err');
    const btn        = document.getElementById('cp-submit');
    const showErr    = msg => { if (errEl) { errEl.textContent = msg; errEl.style.display = 'block'; } };

    if (!name)       return showErr('Project name is required.');
    if (!type)       return showErr('Please choose a project type.');
    if (!visibility) return showErr('Please choose a visibility option.');

    // Custom Project (template) does not need a programming language or UI language
    // — those are defined by the ZIP devices. Route to the templates API instead.
    if (type === 'custom_project') {
        if (btn) { btn.disabled = true; btn.textContent = 'Creating…'; }
        return _tplHandleCreate(name, visibility, uiLangId, errEl, btn);
    }

    // Custom Code — requires language selections.
    if (!langId)   return showErr('Please select a programming language.');
    if (!uiLangId) return showErr('Please select a user interface language.');

    if (btn) { btn.disabled = true; btn.textContent = 'Creating…'; }

    const r = await api('POST', '/api/v1/projects', {
        name, type, visibility,
        programmingLanguageId: langId, uiLanguageId: uiLangId,
    });

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-folder-plus"></i> Create Project'; }

    if (r?.metadata?.status === 201 || r?.metadata?.status === 200) {
        closeCreateProjectModal();
        await loadProjects();
        _expandedLangs.add(langId);
        renderTree();
        showPageAlert('Project created successfully.', 'success');
    } else {
        showErr(r?.metadata?.error || 'Could not create project. Please try again.');
    }
}

// ─── Project Properties modal ─────────────────────────────────────────────────
//
// The Properties modal lets the owner edit:
//   - Project name
//   - Visibility (public / private)
//   - Publishing flags (publishToFeed, publishToSearch, readyToUse)
//
// Publishing flags are enabled here (unlike in the Create modal). They are
// only meaningful when visibility is "public" — switching to "private" hides
// the publish section and the server will zero the flags regardless.

//
// The modal is opened via the gear icon in renderProjectNode.

export function openPropertiesModal(projectId) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    const overlay = document.getElementById('proj-modal-overlay');
    if (!overlay) return;

    overlay.style.cssText = 'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';

    const isPublic = p.visibility === 'public';

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

  <!-- Visibility -->
  <div class="fg">
    <label>Visibility *</label>
    <div style="display:flex;gap:12px;margin-top:4px">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid ${isPublic?'var(--primary)':'var(--border)'};
                    background:${isPublic?'var(--info-bg)':''};
                    border-radius:var(--r);font-size:14px;transition:border-color var(--tr)"
             id="pp-vis-pub-label">
        <input type="radio" name="pp-vis" value="public"
               ${isPublic ? 'checked' : ''}
               onchange="onPropertiesVisChange()">
        <i class="fa-solid fa-globe" style="color:var(--primary)"></i>
        <div><div style="font-weight:600">Public</div>
             <div style="font-size:11px;color:var(--text-muted)">Visible to everyone</div></div>
      </label>
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;flex:1;
                    padding:10px 14px;border:1px solid ${!isPublic?'var(--primary)':'var(--border)'};
                    background:${!isPublic?'var(--info-bg)':''};
                    border-radius:var(--r);font-size:14px;transition:border-color var(--tr)"
             id="pp-vis-prv-label">
        <input type="radio" name="pp-vis" value="private"
               ${!isPublic ? 'checked' : ''}
               onchange="onPropertiesVisChange()">
        <i class="fa-solid fa-lock" style="color:var(--text-secondary)"></i>
        <div><div style="font-weight:600">Private</div>
             <div style="font-size:11px;color:var(--text-muted)">Only you can see it</div></div>
      </label>
    </div>
  </div>

  <!-- Publish section — only shown when Public is selected.
       The section is hidden for private projects because the flags have no
       effect there and showing them would be confusing. -->
  <div id="pp-publish-wrap" style="display:${isPublic?'block':'none'}">
    ${_buildPublishSection('pp', p.publishToFeed, p.publishToSearch, p.readyToUse, false)}
  </div>

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
    const visibility = document.querySelector('input[name="pp-vis"]:checked')?.value;
    const errEl      = document.getElementById('pp-err');
    const btn        = document.getElementById('pp-submit');

    const showErr = msg => {
        if (errEl) { errEl.textContent = msg; errEl.style.display = 'block'; }
    };

    if (!name)       return showErr('Project name is required.');
    if (!visibility) return showErr('Please choose a visibility.');

    // Read publish flags — they are only meaningful when public. The server
    // enforces the same rule, but we also zero them client-side to avoid
    // sending confusing data.
    const isPublic      = visibility === 'public';
    const publishToFeed   = isPublic && !!(document.getElementById('pp-pub-feed')?.checked);
    const publishToSearch = isPublic && !!(document.getElementById('pp-pub-search')?.checked);
    const readyToUse      = isPublic && !!(document.getElementById('pp-ready')?.checked);

    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }

    const r = await api('PUT', `/api/v1/projects/${projectId}`, {
        name, visibility, publishToFeed, publishToSearch, readyToUse,
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
    } else {
        showPageAlert(r?.metadata?.error || 'Upload failed.', 'danger');
    }
}

export async function deleteProjectFile(projectId, section, filename) {
    const url = section === 'code'
        ? `/api/v1/projects/${projectId}/files/code`
        : `/api/v1/projects/${projectId}/files/${section}/${encodeURIComponent(filename)}`;
    const r = await api('DELETE', url);
    if (r?.metadata?.status === 200) {
        await loadProjectFiles(projectId);
        renderTree();
    } else {
        showPageAlert(r?.metadata?.error || 'Could not delete file.', 'danger');
    }
}

export async function promptRenameCode(projectId) {
    const p = _projects.find(x => x.id === projectId);
    if (!p) return;

    const files = _projectFiles[projectId];
    const current = files?.code?.[0]?.name || 'main.go';
    const newName = await showPrompt('New filename (.go):', current);
    if (!newName || newName.trim() === current) return;

    const r = await api('PUT',
        `/api/v1/projects/${projectId}/files/code/rename`,
        { newName: newName.trim() },
    );
    if (r?.metadata?.status === 200) {
        _defaultFilename = r.data?.name || newName.trim();
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
    catch { await showPrompt('Copy the Markdown link:', md, { label: 'OK', cancelLabel: 'Close' }); }
}

// ─── Page helpers ─────────────────────────────────────────────────────────────

function showPageAlert(msg, type) {
    const el = document.getElementById('proj-alert');
    if (!el) return;
    el.className = `alert alert-${type}`;
    el.textContent = msg;
    el.style.display = 'block';
    setTimeout(() => { el.style.display = 'none'; }, 4000);
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

    const filename = await showPrompt('New markdown filename (.md):', 'document.md');
    if (!filename || !filename.trim()) return;

    filename = filename.trim();
    if (!filename.toLowerCase().endsWith('.md')) {
        filename += '.md';
    }
    // Guard against reserved characters in the filename.
    if (/[/\\:*?"<>|]/.test(filename)) {
        await showAlert('danger', 'Invalid filename — must not contain: / \\ : * ? " < > |');
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
    <button class="btn btn-ghost btn-sm" onclick="projMarkdownClose()" title="Back to projects">
      <i class="fa-solid fa-arrow-left"></i> Projects
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
            await showAlert('danger', 'Save failed: ' + (json?.metadata?.error || 'unknown error'));
        }
    } catch(e) {
        await showAlert('danger', 'Save failed: ' + e.message);
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
    _needsExplicitParse = false;
    _parseStatusType    = '';
    _codeVersions       = [];
    _diffHunks          = [];
    _diffChoices        = [];
    _currentVersion     = 0;
    _nextVersion        = 1;
    _defaultFilename    = slugifyFilename(p.name) + '.go';

    renderEditorView(_root);

    if (!_projectFiles[projectId]) {
        await loadProjectFiles(projectId);
    }

    projMountMonaco('');
    projRegisterSlashMenu(projectId);
    updateVersionBar();
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

/* Help panel CSS moved to _injectEditorStyles (shared with Projects guide) */
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

// ── Upload version button ─────────────────────────────────────────────────────

function _tplUploadBtn(t) {
    // Always shown — allows uploading a first version or replacing an errored one.
    return `<label style="cursor:pointer" title="Upload a new ZIP version">
      <span class="btn btn-secondary btn-sm" id="tpl-upload-label-${esc(t.id)}">
        <i class="fa-solid fa-upload"></i>
        ${t.status === 'no_version' ? 'Upload Version' : 'New Version'}
      </span>
      <input type="file" accept=".zip" style="display:none"
             onchange="window._tplFileSelected(this,'${esc(t.id)}')">
    </label>`;
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
    const sym     = pin.missingConn ? '⊙' : pin.connection === 'mandatory' ? '◉' : '◎';
    const dotSty  = pin.missingConn ? 'color:#C0392B' : '';
    const dot     = `<span class="proj-dot ${isInput ? 'in' : 'out'}" style="${dotSty}">${sym}</span>`;
    const badges  = _tplBuildPinBadges(pin);
    const nameEl  = `<span class="proj-pin-name">${esc(pin.name)}</span>`;
    const typeEl  = `<span class="proj-pin-type">${esc(pin.type)}</span>`;
    const tooltip = _tplBuildPinTooltip(pin);
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
function _tplBuildPinBadges(pin) {
    let b = '';
    if (pin.missingConn) b += `<span class="proj-badge warn">⚠ connection: missing</span>`;
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
function _tplBuildPinTooltip(pin) {
    const lines = [];
    if (pin.doc)        lines.push(`<strong>${esc(pin.doc)}</strong>`);
    if (pin.missingConn)           lines.push(`<span style="color:#F5B7B1">⚠ connection: missing</span>`);
    else if (pin.connection === 'mandatory') lines.push('◉ <strong>mandatory</strong> connection');
    else if (pin.connection === 'optional')  lines.push('◎ <strong>optional</strong> connection');
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

// ── Upload version ────────────────────────────────────────────────────────────

window._tplFileSelected = async function(input, pkgId) {
    const file = input.files?.[0];
    if (!file) return;
    input.value = '';

    const label = document.getElementById(`tpl-upload-label-${pkgId}`);
    if (label) label.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Uploading…';

    const form = new FormData();
    form.append('file', file);
    const opts = { method: 'POST', body: form };
    if (S.token) opts.headers = { Authorization: 'Bearer ' + S.token };

    try {
        const r    = await fetch(`/api/v1/templates/${pkgId}/versions`, opts);
        const json = await r.json().catch(() => ({ metadata: { status: r.status, error: 'parse error' } }));

        if (!r.ok || json?.metadata?.status >= 400) {
            showPageAlert(json?.metadata?.error || `Upload failed (HTTP ${r.status})`, 'danger');
            if (label) label.innerHTML = '<i class="fa-solid fa-upload"></i> New Version';
            return;
        }

        // Version uploaded — update local state and start polling.
        // Clear stale parse errors and def immediately so the card does not
        // show warnings from the previous version while the new one is being
        // processed by the worker.
        const idx = _templates.findIndex(t => t.id === pkgId);
        if (idx >= 0) {
            _templates[idx].status      = 'pending';
            _templates[idx].parseErrors = [];
            _templates[idx]._def        = null;
            _templates[idx].latestVersion = (json?.data?.version) || (_templates[idx].latestVersion + 1);
        }
        _tplRenderList();
        _tplStartPoll(pkgId);
        showPageAlert('Version uploaded — processing…', 'success');

    } catch (e) {
        showPageAlert('Network error: ' + (e?.message || String(e)), 'danger');
        if (label) label.innerHTML = '<i class="fa-solid fa-upload"></i> New Version';
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

// _tplHandleCreate intercepts modal submission for type=custom_project and
// redirects to the templates API. Called by submitCreateProject.
async function _tplHandleCreate(name, visibility, uiLangId, errEl, btn) {
    const r = await api('POST', '/api/v1/templates', {
        name, visibility, description: '',
    });

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-folder-plus"></i> Create Project'; }

    if (r?.metadata?.status === 201 || r?.metadata?.status === 200) {
        closeCreateProjectModal();
        const newTpl = r.data;
        _templates.unshift(newTpl);
        _activeTab = 'templates';
        // Re-render page shell so the Templates tab shows active.
        renderProjects(_root);
        showPageAlert(`Template "${esc(newTpl.name)}" created. Upload a ZIP version to get started.`, 'success');
    } else {
        if (errEl) { errEl.textContent = r?.metadata?.error || 'Could not create template. Please try again.'; errEl.style.display = 'block'; }
    }
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
const _TPL_HELP_GUIDE = "# IoTMaker Template Authoring Guide\n\nA **template** is a ZIP file created by a specialist that packages a ready-to-run\nGo project together with one or more configurable **devices**. A maker drags those\ndevices onto the canvas, sets values in the Inspect panel, connects wires, and\nclicks **Generate ZIP** \u2014 out comes a fully working project, personalised with\ntheir choices.\n\nThis guide walks you through creating a template from scratch, using the\n**Echo Hello World** server as a running example.\n\n---\n\n## Table of Contents\n\n1. [How it works \u2014 the big picture](#1-how-it-works--the-big-picture)\n2. [ZIP structure](#2-zip-structure)\n3. [template.json](#3-templatejson)\n4. [Devices \u2014 the configurable blocks](#4-devices--the-configurable-blocks)\n   - [Package and struct](#41-package-and-struct)\n   - [Props \u2014 Inspect panel fields](#42-props--inspect-panel-fields)\n   - [Methods and wire ports](#43-methods-and-wire-ports)\n   - [IDS tag reference](#44-ids-tag-reference)\n   - [Port metadata tags](#45-port-metadata-tags)\n5. [Output files \u2014 the project skeleton](#5-output-files--the-project-skeleton)\n6. [Complete example \u2014 Echo Hello World](#6-complete-example--echo-hello-world)\n7. [Checklist before publishing](#7-checklist-before-publishing)\n\n---\n\n## 1. How it works \u2014 the big picture\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502                     Template ZIP                        \u2502\n  \u2502                                                         \u2502\n  \u2502  devices/                                               \u2502\n  \u2502  \u2514\u2500\u2500 ServerConfig.go   \u2190 specialist writes this         \u2502\n  \u2502                                                         \u2502\n  \u2502  output/                                                \u2502\n  \u2502  \u251c\u2500\u2500 main.go           \u2190 Go project skeleton            \u2502\n  \u2502  \u251c\u2500\u2500 go.mod            \u2502  (may contain {{.Var}}         \u2502\n  \u2502  \u251c\u2500\u2500 README.md         \u2502   placeholders)                \u2502\n  \u2502  \u2514\u2500\u2500 templates/                                         \u2502\n  \u2502      \u2514\u2500\u2500 index.html                                     \u2502\n  \u2502                                                         \u2502\n  \u2502  template.json         \u2190 wires vars to device props     \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n             \u2502\n             \u2502  uploaded by specialist\n             \u25bc\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502   IoTMaker server       \u2502  parses devices/, validates output/\n  \u2502   (worker)              \u2502  stores the definition\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n             \u2502\n             \u2502  maker opens IDE, places device on canvas\n             \u25bc\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502   Maker's canvas        \u2502  fills Inspect panel,\n  \u2502                         \u2502  connects wires\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n             \u2502\n             \u2502  clicks Generate ZIP\n             \u25bc\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502   Generated project     \u2502  {{.Port}} \u2192 \"8081\"\n  \u2502   (downloaded ZIP)      \u2502  {{.Message}} \u2192 \"Hello!\"\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n```\n\n**Two kinds of device values:**\n\n| Source | How the maker sets it | Used in output via |\n|---|---|---|\n| **Prop** (struct field) | Inspect panel text/dropdown | `{{.VarName}}` in output files |\n| **Wire input** (method param) | Wire from another block | codegen pipeline (not in output files) |\n\n---\n\n## 2. ZIP structure\n\n```\nyour-template.zip\n\u251c\u2500\u2500 devices/\n\u2502   \u2514\u2500\u2500 MyDevice.go        (required \u2014 at least one)\n\u251c\u2500\u2500 output/\n\u2502   \u251c\u2500\u2500 main.go            (any files the generated project needs)\n\u2502   \u251c\u2500\u2500 go.mod\n\u2502   \u2514\u2500\u2500 ...\n\u2514\u2500\u2500 template.json          (required)\n```\n\n**Rules:**\n\n- `devices/` must contain at least one `.go` file following the IDS format (see \u00a74).\n- `output/` contains the project skeleton. Text files may use `{{.VarName}}`\n  placeholders. Go source files are included as-is (they may also have placeholders).\n- `template.json` declares which device props map to which template variables.\n- The ZIP root must not contain extra directories beyond `devices/`, `output/`, and\n  `template.json`.\n\n---\n\n## 3. template.json\n\n```json\n{\n  \"vars\": {\n    \"Port\":       \"ServerConfig.Port\",\n    \"Message\":    \"ServerConfig.Message\",\n    \"ModuleName\": \"ServerConfig.ModuleName\"\n  }\n}\n```\n\n**Fields:**\n\n| Field | Required | Description |\n|---|---|---|\n| `vars` | yes | Maps template variable names to device prop paths |\n\n**`vars` format:**  `\"TemplateName\": \"StructName.FieldName\"`\n\n- The left side (`\"Port\"`) is the placeholder used in output files as `{{.Port}}`.\n- The right side (`\"ServerConfig.Port\"`) points to the `Port` field of the\n  `ServerConfig` struct in `devices/`.\n- Only `prop`-tagged struct fields are eligible (wire inputs are handled by codegen).\n\n> **Note:** The `name`, `version`, and `description` fields that older templates\n> may contain are obsolete and are ignored by the server. Name and description are\n> set in the **New Project** modal when you create the template record. Version is\n> auto-incremented by the server on each ZIP upload.\n\n---\n\n## 4. Devices \u2014 the configurable blocks\n\nEach `.go` file in `devices/` becomes one visual device in the IDE. A device is a\nstandard Go struct with methods, annotated with **IDS tags** in doc comments.\n\n### 4.1 Package and struct\n\n```go\n// Package devices \u2014 ServerConfig configures the Hello World server.\n//\n// Place this device on the canvas, fill in the Inspect panel,\n// then generate the ZIP.\npackage devices\n\n// ServerConfig holds top-level server configuration.\n//\n// icon:server. label:Server Config.\ntype ServerConfig struct {\n    // ...\n}\n```\n\nThe struct doc comment accepts two visual directives:\n\n| Directive | Example | Effect |\n|---|---|---|\n| `icon:name.` | `icon:server.` | FontAwesome icon shown in the block header |\n| `label:text.` | `label:Server Config.` | Human-readable block title |\n\nBrowse icons at [fontawesome.com/icons](https://fontawesome.com/icons) (free tier).\nUse the kebab-case name: `fa-server` \u2192 `icon:server.`\n\n### 4.2 Props \u2014 Inspect panel fields\n\nProps are struct fields tagged with `` `prop:\"Label\"` ``. The maker edits them in\nthe Inspect panel. Their values flow into output files via `{{.VarName}}`.\n\n```go\ntype ServerConfig struct {\n    // Port is the HTTP listen port.\n    Port string `prop:\"Port\" default:\"8081\"`\n\n    // Message is the greeting shown on the index page.\n    Message string `prop:\"Message\" default:\"Hello, World!\"`\n\n    // ModuleName is the Go module name written into go.mod.\n    ModuleName string `prop:\"Module Name\" default:\"github.com/example/hello\"`\n}\n```\n\n**Struct field tags:**\n\n| Tag | Required | Description |\n|---|---|---|\n| `` prop:\"Label\" `` | yes | Makes the field a prop; sets the Inspect panel label |\n| `` default:\"value\" `` | recommended | Pre-filled value shown to the maker |\n| `` options:\"a,b,c\" `` | optional | Renders a dropdown instead of a text input |\n\n### 4.3 Methods and wire ports\n\nMethods define the **visual blocks** the maker places on the canvas and connects\nwith wires.\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \u26a1  Server Config Init                            \u2502  \u2190 block header\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502                                                  \u2502\n  \u2502  \u25c9 port   int                                    \u2502  \u2190 input port (wire arrives here)\n  \u2502                                                  \u2502\n  \u2502                                    error  err \u25ce  \u2502  \u2190 output port (wire leaves here)\n  \u2502                                                  \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n\n  \u25c9 = mandatory connection    \u25ce = optional connection    \u2299 = connection tag missing\n```\n\n**Port symbols:**\n\n```\n  \u25c9  mandatory \u2014 the IDE warns the maker when this port is not wired\n  \u25ce  optional  \u2014 the port works unwired (uses default value)\n  \u2299  missing   \u2014 the specialist forgot the connection: tag (fix this!)\n```\n\n**Method doc comment format** (standard Go doc, IDS extensions):\n\n```go\n// Init validates and stores the port.\n//\n// icon:plug. label:Init.\n//\n// Params\n//\n//\tport: HTTP listen port, receives wire from a ConstInt device.  connection:mandatory.  unit:port.\n//\n// Returns\n//\n//\terr: non-nil if port is out of valid range.  connection:optional.\nfunc (s *ServerConfig) Init(port int) (err error) {\n    // ...\n}\n```\n\n**Method-level directives** (in the first lines of the doc comment):\n\n| Directive | Example | Effect |\n|---|---|---|\n| `icon:name.` | `icon:plug.` | Icon in the block header |\n| `label:text.` | `label:Init.` | Block subtitle |\n| `executionOrder:N.` | `executionOrder:1.` | Relative run order (lower = first) |\n\n**`Params` and `Returns` sections:**\n\nEach indented line describes one port **in declaration order**. The format is:\n\n```\n//\tportname: human description.  tag:value.  tag:value.\n```\n\n- `portname:` is for readability only \u2014 matching is done by position, not by name.\n- Tags are dot-terminated and case-insensitive.\n- The tab before `portname` is required (standard Go doc list format).\n\n### 4.4 IDS tag reference\n\n**Struct-level** (in the `type MyStruct struct` doc comment):\n\n| Tag | Example |\n|---|---|\n| `icon:name.` | `icon:server.` |\n| `label:text.` | `label:Server Config.` |\n\n**Method-level** (in the `func` doc comment, before `Params`/`Returns`):\n\n| Tag | Example |\n|---|---|\n| `icon:name.` | `icon:plug.` |\n| `label:text.` | `label:Init.` |\n| `executionOrder:N.` | `executionOrder:1.` |\n\n### 4.5 Port metadata tags\n\nUsed inside `Params` and `Returns` lines:\n\n| Tag | Example | Meaning |\n|---|---|---|\n| `connection:mandatory.` | \u2014 | Wire **must** be connected |\n| `connection:optional.` | \u2014 | Wire is optional |\n| `unit:label.` | `unit:port.` | Physical/logical unit shown in tooltip |\n| `range:min..max.` | `range:0..65535.` | Valid value range |\n| `rangeMin:N.` | `rangeMin:0.` | Lower bound only |\n| `rangeMax:N.` | `rangeMax:255.` | Upper bound only |\n| `encoding:label.` | `encoding:UTF-8.` | Data encoding |\n| `default:value.` | `default:8081.` | Default when unwired |\n| `bits:N.` | `bits:16.` | Significant bit count |\n\n> **Rule:** Every port must have either `connection:mandatory.` or\n> `connection:optional.`. The IDE shows a `\u2299` warning badge on ports that are\n> missing this tag.\n\n---\n\n## 5. Output files \u2014 the project skeleton\n\nEverything inside `output/` is copied verbatim into the generated ZIP, with one\ntransformation: `{{.VarName}}` placeholders are replaced with the values the maker\nconfigured.\n\n**Example `output/go.mod`:**\n\n```\nmodule {{.ModuleName}}\n\ngo 1.22\n\nrequire (\n    github.com/labstack/echo/v4 v4.12.0\n    github.com/labstack/gommon v0.4.2\n)\n```\n\nWhen the maker sets `ModuleName` to `github.com/acme/myserver`, the generated\n`go.mod` will contain `module github.com/acme/myserver`.\n\n**Which variables are available?**\n\nOnly variables declared in `template.json` under `vars`. Each variable maps to a\nprop field on a device. Wire inputs are **not** available as template variables \u2014\nthey flow through the codegen pipeline instead.\n\n**Supported file types for variable substitution:**\n\nAny text file: `.go`, `.mod`, `.html`, `.md`, `.yaml`, `.json`, `.txt`, etc.\nBinary files (images, fonts) are copied unchanged.\n\n---\n\n## 6. Complete example \u2014 Echo Hello World\n\nHere is the full template, file by file, built from scratch.\n\n### Step 1 \u2014 Plan the template\n\nWe want a maker to configure:\n- The HTTP listen port (comes from a wire or the Inspect panel)\n- A greeting message (Inspect panel only)\n- The Go module name (Inspect panel only)\n\nThe generated project is a minimal Echo web server.\n\n### Step 2 \u2014 Create `devices/ServerConfig.go`\n\n```go\n// Package devices \u2014 ServerConfig is the configuration device for the\n// Echo Hello World template.\n//\n// Place this device on the canvas, set Port, Message and Module Name\n// in the Inspect panel, then generate the ZIP.\npackage devices\n\nimport (\n\t\"fmt\"\n)\n\n// ServerConfig holds top-level server configuration.\n//\n// icon:server. label:Server Config.\ntype ServerConfig struct {\n\t// Port is the HTTP listen port.\n\tPort string `prop:\"Port\" default:\"8081\"`\n\n\t// Message is the greeting shown on the index page.\n\tMessage string `prop:\"Message\" default:\"Hello, World!\"`\n\n\t// ModuleName is the Go module name written into go.mod.\n\tModuleName string `prop:\"Module Name\" default:\"github.com/example/hello\"`\n}\n\n// Init validates and stores the port.\n//\n// icon:plug. label:Init.\n//\n// Params\n//\n//\tport: HTTP listen port, receives wire from a ConstInt device.  connection:mandatory.  unit:port.\n//\n// Returns\n//\n//\terr: non-nil if port is out of valid range.  connection:optional.\nfunc (s *ServerConfig) Init(port int) (err error) {\n\tif port < 1 || port > 65535 {\n\t\treturn fmt.Errorf(\"invalid port: %d\", port)\n\t}\n\ts.Port = fmt.Sprintf(\"%d\", port)\n\treturn nil\n}\n```\n\n**What this produces in the IDE:**\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \ud83d\udd0c  Server Config Init                          \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502  \u25c9 port   int                    [unit: port]    \u2502\n  \u2502                                                  \u2502\n  \u2502                        error  err \u25ce              \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n\n  Inspect panel (always visible):\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  Port          [ 8081      ] \u2502\n  \u2502  Message       [ Hello,... ] \u2502\n  \u2502  Module Name   [ github... ] \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n```\n\nThe `port` input has `connection:mandatory` \u2014 the IDE will warn the maker if they\nforget to wire a `ConstInt` block to it. The `err` output has `connection:optional`\n\u2014 it is fine to leave it unwired.\n\n### Step 3 \u2014 Create `template.json`\n\n```json\n{\n  \"vars\": {\n    \"Port\":       \"ServerConfig.Port\",\n    \"Message\":    \"ServerConfig.Message\",\n    \"ModuleName\": \"ServerConfig.ModuleName\"\n  }\n}\n```\n\nThis tells the server: when generating the ZIP, replace `{{.Port}}` with the value\nof the `Port` prop on `ServerConfig`, and so on.\n\n### Step 4 \u2014 Create `output/go.mod`\n\n```\nmodule {{.ModuleName}}\n\ngo 1.22\n\nrequire (\n\tgithub.com/labstack/echo/v4 v4.12.0\n\tgithub.com/labstack/gommon v0.4.2\n)\n```\n\n### Step 5 \u2014 Create `output/main.go`\n\n```go\npackage main\n\n// main.go \u2014 Generated by IoTMaker IDE \u00b7 Echo Hello World template.\n// Run:  go mod tidy && go run .\n// Then: open http://localhost:{{.Port}}\n\nimport (\n\t\"html/template\"\n\t\"io\"\n\t\"net/http\"\n\n\t\"github.com/labstack/echo/v4\"\n\t\"github.com/labstack/echo/v4/middleware\"\n)\n\ntype renderer struct {\n\tt *template.Template\n}\n\nfunc (r *renderer) Render(w io.Writer, name string, data any, c echo.Context) error {\n\treturn r.t.ExecuteTemplate(w, name, data)\n}\n\nfunc main() {\n\te := echo.New()\n\te.HideBanner = true\n\n\te.Use(middleware.Logger())\n\te.Use(middleware.Recover())\n\n\te.Renderer = &renderer{\n\t\tt: template.Must(template.ParseGlob(\"templates/*.html\")),\n\t}\n\n\te.GET(\"/\", func(c echo.Context) error {\n\t\treturn c.Render(http.StatusOK, \"index.html\", map[string]any{\n\t\t\t\"Message\": \"{{.Message}}\",\n\t\t\t\"Port\":    \"{{.Port}}\",\n\t\t})\n\t})\n\n\te.Logger.Fatal(e.Start(\":{{.Port}}\"))\n}\n```\n\n### Step 6 \u2014 Create `output/README.md`\n\n````markdown\n# {{.Message}}\n\nGenerated by **IoTMaker IDE** \u2014 Echo Hello World template.\n\n## Quick start\n\n```bash\ngo mod tidy\ngo run .\n```\n\nOpen [http://localhost:{{.Port}}](http://localhost:{{.Port}})\n\n## Configuration\n\n| Setting | Value |\n|---------|-------|\n| Port    | {{.Port}} |\n| Message | {{.Message}} |\n| Module  | {{.ModuleName}} |\n````\n\n### Step 7 \u2014 Package the ZIP\n\n```\nyour-template.zip\n\u251c\u2500\u2500 devices/\n\u2502   \u2514\u2500\u2500 ServerConfig.go\n\u251c\u2500\u2500 output/\n\u2502   \u251c\u2500\u2500 main.go\n\u2502   \u251c\u2500\u2500 go.mod\n\u2502   \u251c\u2500\u2500 README.md\n\u2502   \u2514\u2500\u2500 templates/\n\u2502       \u2514\u2500\u2500 index.html\n\u2514\u2500\u2500 template.json\n```\n\nZip from the root \u2014 the paths inside the ZIP must start with `devices/`,\n`output/`, or `template.json` directly (no extra top-level folder).\n\n```bash\n# macOS / Linux\nzip -r my-template.zip devices/ output/ template.json\n```\n\n### Step 8 \u2014 Upload to IoTMaker\n\n1. Go to **Projects \u2192 Templates**.\n2. Click **+ New Project** \u2192 choose **Custom Project** \u2192 fill in name and\n   description.\n3. On the template card, click **Upload Version** and select your ZIP.\n4. Wait for the **Ready** badge to appear.\n5. Review the device diagram \u2014 fix any `\u2299` warnings before publishing.\n6. When satisfied, click **Publish** and check **Ready to use**.\n\n---\n\n## 7. Checklist before publishing\n\n- [ ] Every port has `connection:mandatory.` or `connection:optional.`\n- [ ] Every method has `icon:` and `label:` directives\n- [ ] The struct has `icon:` and `label:` directives\n- [ ] All `{{.VarName}}` in output files are declared in `template.json`\n- [ ] All vars in `template.json` point to real prop fields on real structs\n- [ ] The generated project runs: `go mod tidy && go run .`\n- [ ] The README explains what the template does and how to run it\n- [ ] Default values in props make sense for a first-time maker\n\n---\n\n*IoTMaker Template Authoring Guide \u00b7 revision 1*\n";

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
// _projOpenHelp renders the Projects Guide in a rendered-Markdown panel that
// replaces the entire page content. Uses marked.js (bundled at
// /marked/marked.min.js). _projCloseHelp disposes the panel and re-navigates
// to the projects page via renderProjects().

// _PROJ_HELP_GUIDE — Projects Guide (Markdown).
const _PROJ_HELP_GUIDE = "# IoTMaker Projects Guide\n\n**Projects** is where specialists write, parse, version, and publish\n**black-box components** \u2014 the reusable Go blocks that makers wire together\non the canvas. This guide covers every feature in the Projects tab, from\ncreating your first project to publishing a component to the community feed.\n\n---\n\n## Table of Contents\n\n1. [What is a project?](#1-what-is-a-project)\n2. [Creating a project](#2-creating-a-project)\n3. [The project tree](#3-the-project-tree)\n4. [The code editor](#4-the-code-editor)\n5. [Parse and Analyze](#5-parse-and-analyze)\n6. [The Preview tab \u2014 the visual block](#6-the-preview-tab--the-visual-block)\n7. [Pins \u2014 renaming and flags](#7-pins--renaming-and-flags)\n8. [Saving and versions](#8-saving-and-versions)\n9. [Diff \u2014 comparing versions](#9-diff--comparing-versions)\n10. [The Markdown editor \u2014 writing docs](#10-the-markdown-editor--writing-docs)\n11. [Images](#11-images)\n12. [Project properties and publishing](#12-project-properties-and-publishing)\n13. [Deleting a project](#13-deleting-a-project)\n14. [IDS \u2014 the component format](#14-ids--the-component-format)\n15. [Complete example \u2014 APDS9960 colour sensor](#15-complete-example--apds9960-colour-sensor)\n16. [Checklist before publishing](#16-checklist-before-publishing)\n\n---\n\n## 1. What is a project?\n\nA **project** is a Go source file written by a specialist following the\n**IDS (IoTMaker Doc Standard)**. The server parses the file and extracts\na component definition \u2014 struct name, icon, label, methods, input/output\nports, and Inspect-panel settings. The result becomes a visual block in\nthe WASM IDE that any maker can place on a canvas and wire up.\n\n```\n  Specialist writes Go code\n          \u2502\n          \u2502  Parse + Analyze\n          \u25bc\n  Component definition extracted\n          \u2502\n          \u2502  Maker opens WASM IDE\n          \u25bc\n  Visual block appears in the Hardware menu\n          \u2502\n          \u2502  Maker wires it up and generates code\n          \u25bc\n  Working Go program for the target device\n```\n\nA project lives under **one programming language** (e.g. Go / TinyGo) and\ncan have **one code file**, any number of **image files**, and any number of\n**Markdown documentation files**.\n\n---\n\n## 2. Creating a project\n\nClick **+ New Project** in the top-right corner.\n\n| Field | Description |\n|---|---|\n| **Project name** | Human-readable name shown in the tree and the community feed |\n| **Project type** | Choose **Custom Code** for a black-box component |\n| **Visibility** | `private` \u2014 only you can see it; `public` \u2014 visible to all makers |\n| **Programming language** | The language this component targets (e.g. Go, TinyGo) |\n| **UI language** | Language for your documentation |\n\nPublishing flags (feed, search, ready-to-use) are available later via\n**Project Properties** (\u2699 icon), once the project exists.\n\n> **Tip:** Start with `private`. Make it public only when the component is\n> documented, parsed without errors, and tested on hardware.\n\n---\n\n## 3. The project tree\n\nAfter creation the project appears in the tree, grouped by language.\n\n```\n  \u25be Go / TinyGo\n    \u25be \ud83d\udcc1 APDS9960           \u2190 click to expand\n        \u2699  \ud83d\uddd1\n      \u25be \ud83d\udcc4 Code\n          apds9960.go       \u2190 the source file\n          [+ Create] [\u2191 Upload]\n      \u25be \ud83d\uddbc Images\n          wiring.png\n          [\u2191 Upload]\n      \u25be \ud83d\udcc4 Docs\n          readme.md\n          [+ Create] [\u2191 Upload]\n```\n\n**Actions on the project row:**\n\n| Button | Action |\n|---|---|\n| Click project name | Expand / collapse the file sections |\n| \u2699 (gear) | Open **Project Properties** modal |\n| \ud83d\uddd1 (trash) | Delete the project (requires typing the name to confirm) |\n\n**File sections:**\n\n| Section | Contents | Limit |\n|---|---|---|\n| Code | One `.go` source file | 1 file |\n| Images | PNG, JPEG, SVG, etc. | Unlimited |\n| Docs | `.md` documentation files | Unlimited |\n\nEach section shows **Create** (new file in editor) and **Upload** (from disk)\nbuttons when the section is empty or allows more files.\n\n---\n\n## 4. The code editor\n\nClick a `.go` file in the Code section to open the editor.\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \u2190 Projects  /  APDS9960  /  code     v3 \u25be  [Diff]                 \u2502\n  \u2502\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2502\n  \u2502  [ Editor ]  [ Preview ]  [ Debug ]     \u2611 Live analysis   [Rename] \u2502\n  \u2502\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2502\n  \u2502                                                                     \u2502\n  \u2502   1  // Package blackbox                                            \u2502\n  \u2502   2  //                                                             \u2502\n  \u2502   3  // APDS9960 reads colour (RGBC) data via I2C.                  \u2502\n  \u2502  ...                                                                \u2502\n  \u2502                                                                     \u2502\n  \u2502\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2502\n  \u2502  [Clear parse]  [\u25b6 Parse]  [\ud83d\udcbe Save]      \u2713 OK \u2014 3 method(s) ...   \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n```\n\n### Toolbar\n\n| Control | Description |\n|---|---|\n| **\u2190 Projects** | Close editor, return to the project tree |\n| **Breadcrumb** | Shows: project name / section / filename |\n| **Version selector** (`v3 \u25be`) | Switch to a previously saved version |\n| **Diff** | Compare any two saved versions side-by-side |\n| **Editor / Preview / Debug** | Switch between the three tabs |\n| **\u2611 Live analysis** | Toggle real-time semantic analysis while you type |\n| **Rename** | Rename the file |\n\n### Editor tab\n\nFull Monaco editor configured for Go:\n\n- **Tab key** inserts a real tab character (`\\t`) \u2014 required for IDS\n  `Params`/`Returns` sections and Go indentation.\n- **Tab size** is 4 columns (standard Go).\n- Type `/` on a blank line to open the **slash menu** (insert image or\n  Markdown block).\n\n### Status bar\n\nThe bar below the editor shows the parse/analyze result:\n\n| Status | Meaning |\n|---|---|\n| *(empty)* | Not yet parsed |\n| \u26a0 *Code has changed* | Edit made after last parse \u2014 parse again before saving |\n| \u2713 OK \u2014 3 method(s) \u00b7 10 pin(s) \u00b7 2 setting(s) | Parsed successfully |\n| \u2717 bb.go:12:5: \u2026 | Syntax or type error \u2014 line highlighted in the editor |\n\n---\n\n## 5. Parse and Analyze\n\nThe workflow is always **Parse \u2192 Save**. You cannot save without a\nsuccessful parse.\n\n### Parse\n\nClick **\u25b6 Parse** (or `Ctrl+Enter`).\n\n1. Sends the source to the server.\n2. The server runs `go/ast` to extract the component definition.\n3. The result is shown in the **Preview** tab.\n4. Soft warnings (missing `connection:` tags) appear in the status bar.\n\nParse checks **structure only** \u2014 struct shape, IDS tags, method signatures.\nIt does not check whether the code compiles.\n\n### Analyze\n\nAnalysis runs automatically after a successful parse, and also in the\nbackground while you type when **Live analysis** is enabled.\n\nThe analyzer uses `go/types` with a lenient importer \u2014 TinyGo packages\n(`machine`, `time`, etc.) are stubbed so they do not produce false errors.\nReal errors (wrong types, undefined variables, missing imports) are\nhighlighted in the editor with red underlines and gutter markers.\n\n> **Live analysis** is useful while writing but can be disabled on slow\n> machines \u2014 the checkbox is in the editor toolbar.\n\n### Clear parse\n\n**Clear parse** resets the Preview and status bar without touching the\neditor content. Use it when you want to start fresh after switching versions.\n\n---\n\n## 6. The Preview tab \u2014 the visual block\n\nAfter a successful parse, the **Preview** tab shows exactly how the\ncomponent will look in the maker's WASM IDE.\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \u23f3  APDS9960 Init                                              \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502  \u25c9 i2c    *machine.I2C    [unit: i2c_bus]         + flag       \u2502\n  \u2502                                            error  err \u25ce        \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \u2600  APDS9960 Run                                               \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502                             uint16  clear \u25ce  [unit: lux_counts]\u2502\n  \u2502                              uint16  red  \u25ce  [unit: color_counts\u2502\n  \u2502                            uint16  green \u25ce                     \u2502\n  \u2502                             uint16  blue \u25ce                     \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n```\n\n**Port symbols:**\n\n```\n  \u25c9  mandatory \u2014 maker must wire this port\n  \u25ce  optional  \u2014 wire is optional\n  \u2299  missing   \u2014 connection: tag absent (fix this before publishing)\n```\n\n**Hover a pin** to see its tooltip: description, unit, range, and encoding.\n\nThe **Debug** tab shows the raw JSON of the parsed component \u2014 useful for\ndiagnosing unexpected behaviour.\n\n---\n\n## 7. Pins \u2014 renaming and flags\n\n### Renaming a pin\n\nClick the pin name in the Preview tab to make it editable. Type a new name\nand press `Enter` or click away. The rename is local to the preview \u2014 it\ndoes not modify the source code. It is used to give more descriptive names\nto pins that have short Go identifiers (e.g. rename `b` to `i2c_bus`).\n\n> **Note:** Pin renames are only stored in the preview session. They are\n> not persisted. The canonical name comes from the Go source.\n\n### Flags\n\nEach pin has a **+ flag** button. A flag is a free-form label you can\nattach to a pin to annotate it for the maker (e.g. `active-low`,\n`pull-up required`). Flags appear as small badges on the pin row.\n\nTo remove a flag, click the **\u00d7** on the badge.\n\n---\n\n## 8. Saving and versions\n\n### Save\n\n1. Parse successfully (status shows \u2713).\n2. Click **\ud83d\udcbe Save**.\n\nThe server stores the source code as a new version. Version numbers start\nat 1 and increment on each save. The **WASM IDE always loads the\nhighest version** of each component.\n\n> If the editor shows *\"Parse first \u2014 code has changed\"*, make a successful\n> parse before saving. This prevents saving code that hasn't been validated.\n\n### Version selector\n\nThe version selector in the toolbar (`v3 \u25be`) lists all saved versions of\nthe file. Selecting an older version loads its source into the editor.\n\nIf the editor has unsaved changes, a confirmation dialog appears before\nloading the older version \u2014 changes will be lost.\n\n---\n\n## 9. Diff \u2014 comparing versions\n\nClick **Diff** in the toolbar to open the diff view.\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u252c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  Left  v2                        \u2502  Right  v3                       \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u253c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502  func (s *APDS9960) Run() (      \u2502  func (s *APDS9960) Run() (      \u2502\n  \u2502-     clear uint16,               \u2502+     lux uint16,                 \u2502\n  \u2502      red   uint16,               \u2502      red   uint16,               \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2534\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n  [Apply to editor \u2190]\n```\n\n- Select any two versions in the left and right dropdowns.\n- Changed lines are highlighted: red for removed, green for added.\n- Click **Apply to editor** to load the right-hand version into the editor.\n- For individual hunks, click **\u2190** or **\u2192** on each changed block to\n  cherry-pick which side to keep.\n\n---\n\n## 10. The Markdown editor \u2014 writing docs\n\nEvery project should have at least one documentation file in the **Docs**\nsection. Good documentation helps makers understand how to use your\ncomponent and is required before marking a project as **Ready to use**.\n\n### Creating a doc file\n\nClick **+ Create** in the Docs section. A modal asks for a filename\n(e.g. `readme.md`). The editor opens with a starter template including\nfrontmatter fields.\n\n### Frontmatter\n\nThe first section of every `.md` file is a YAML frontmatter block that\ncontrols how the file appears in the community feed:\n\n```markdown\n---\ntitle: APDS9960 Colour Sensor\ndescription: Reads RGBC colour channels via I2C on RP2040.\ncategory: sensors\nsubcategory: light-colour\nkeywords: colour, i2c, rp2040, tinygo\nimage: wiring.png\n---\n\n# APDS9960 Colour Sensor\n\n...\n```\n\n| Field | Description |\n|---|---|\n| `title` | Display title in feed cards (max 80 chars) |\n| `description` | One-sentence summary (max 200 chars) |\n| `category` | Top-level category (e.g. `sensors`, `actuators`, `networking`) |\n| `subcategory` | Sub-category (e.g. `light-colour`, `temperature`) |\n| `keywords` | Comma-separated search keywords |\n| `image` | Filename of an uploaded image to use as the card thumbnail |\n\n### Slash menu in the Markdown editor\n\nType `/` on a blank line in the Markdown editor to open the slash menu:\n\n| Option | Inserts |\n|---|---|\n| **Image** | A Markdown image reference to one of your uploaded files |\n| **Component** | A link/snippet referencing another project component |\n| **Category** | A category picker to fill the frontmatter `category` field |\n| **Subcategory** | A subcategory picker |\n\n### Markdown preview\n\nThe **Preview** tab in the Markdown editor shows a live rendered preview\nof the document as it will appear in the community feed, including the\nfeed card thumbnail at the top.\n\n### Saving a doc file\n\nClick **\ud83d\udcbe Save** in the Markdown editor toolbar. Doc files are versioned\nthe same way as code files.\n\n---\n\n## 11. Images\n\nImages are stored in the **Images** section of a project. They can be:\n\n- Referenced from Markdown files using the slash menu \u2192 Image.\n- Set as the feed card thumbnail via the `image:` frontmatter field.\n- Embedded in doc pages.\n\n**Uploading an image:**\n\n1. Click **\u2191 Upload** in the Images section.\n2. Select a PNG, JPEG, SVG, or WebP file.\n3. The filename appears in the list.\n\n**Copying the Markdown reference:**\n\nEach image row has a copy button that puts the Markdown image syntax\n(`![alt](url)`) on the clipboard, ready to paste into a doc file.\n\n**Deleting an image:**\n\nClick \ud83d\uddd1 on the image row. This is permanent and cannot be undone.\n\n---\n\n## 12. Project properties and publishing\n\nClick the \u2699 (gear) icon on the project row to open **Project Properties**.\n\n### General settings\n\n| Field | Description |\n|---|---|\n| **Name** | Rename the project |\n| **Visibility** | `private` / `public` |\n\n### Publishing\n\nPublishing controls are enabled only when the project is **public**.\n\n| Flag | Effect |\n|---|---|\n| **Publish to feed** | Project appears in the community feed (Discover, Recent, Popular tabs) |\n| **Publish to search** | Project appears in marketplace search results |\n| **Ready to use** | Marks the project with a quality badge \u2014 requires a quality commitment |\n\n**Quality commitment:**\n\nChecking **Ready to use** shows a confirmation dialog. By accepting, you\ncertify that:\n\n- The component is fully documented.\n- All methods have `icon:` and `label:` directives.\n- All ports have `connection:` tags.\n- The component has been tested on real hardware.\n- A maker can use it without guidance from you.\n\nThis is an honesty pledge \u2014 it is not enforced automatically. But it builds\ntrust in the community.\n\n---\n\n## 13. Deleting a project\n\nClick \ud83d\uddd1 on the project row. A confirmation dialog asks you to type the\nproject name before deletion proceeds. **This is permanent** \u2014 all versions,\nimages, and doc files are deleted with it.\n\n---\n\n## 14. IDS \u2014 the component format\n\nIDS (IoTMaker Doc Standard) is the convention for writing black-box\ncomponents. It extends standard Go doc comments with machine-readable\ndirectives.\n\n### Package comment\n\n```go\n// Package blackbox\n//\n// APDS9960 reads RGBC colour data via I2C on RP2040.\n//\n// Place Init at global scope.\npackage blackbox\n```\n\nThe package comment becomes the component description shown in the IDE.\n\n### Struct\n\n```go\n// APDS9960 reads colour (RGBC) data via I2C.\n//\n// icon:lightbulb. label:APDS9960.\ntype APDS9960 struct {\n    gain  byte `prop:\"ADC Gain\"         default:\"0\"   options:\"0,1,2,3\"`\n    atime byte `prop:\"Integration Time\" default:\"255\"`\n}\n```\n\n**Struct doc directives:**\n\n| Directive | Example | Required |\n|---|---|---|\n| `icon:name.` | `icon:lightbulb.` | Recommended |\n| `label:text.` | `label:APDS9960.` | Recommended |\n\n**Struct field tags:**\n\n| Tag | Description |\n|---|---|\n| `` prop:\"Label\" `` | Makes the field editable in the Inspect panel |\n| `` default:\"value\" `` | Default value pre-filled for the maker |\n| `` options:\"a,b,c\" `` | Dropdown options instead of free text |\n\n### Methods\n\n```go\n// Init configures the sensor on the given I2C bus.\n//\n// executionOrder:1. icon:hourglass-start. label:Init.\n//\n// Params\n//\n//\ti2c: I2C bus \u2014 wire from an I2CBus Init block.  connection:mandatory.  unit:i2c_bus.\n//\n// Returns\n//\n//\terr: initialisation error.  connection:optional.\nfunc (s *APDS9960) Init(i2c *machine.I2C) (err error) { ... }\n```\n\n**Method doc directives** (before `Params`/`Returns`):\n\n| Directive | Example | Effect |\n|---|---|---|\n| `icon:name.` | `icon:hourglass-start.` | Block header icon |\n| `label:text.` | `label:Init.` | Block subtitle |\n| `executionOrder:N.` | `executionOrder:1.` | Run order (lower = first) |\n\n**Special method \u2014 `Init`:**\n\n`Init` has a unique semantic: it is always placed before all other methods\nin the generated code, regardless of execution order. It is optional \u2014 a\ncomponent may have only regular methods.\n\n**`Params` and `Returns` sections:**\n\nEach indented line describes one port in declaration order:\n\n```\n//\tportname: description.  tag:value.  tag:value.\n```\n\n- The tab (`\\t`) before `portname` is **required** \u2014 use the Tab key in the\n  editor (configured to insert real tabs, not spaces).\n- `portname:` is for human readability \u2014 matching is by position, not name.\n- Tags are dot-terminated and case-insensitive.\n\n### Port metadata tags\n\n| Tag | Example | Meaning |\n|---|---|---|\n| `connection:mandatory.` | \u2014 | Wire must be connected |\n| `connection:optional.` | \u2014 | Wire is optional |\n| `unit:label.` | `unit:lux.` | Unit shown in pin tooltip |\n| `range:min..max.` | `range:0..65535.` | Valid value range |\n| `rangeMin:N.` | `rangeMin:0.` | Lower bound only |\n| `rangeMax:N.` | `rangeMax:255.` | Upper bound only |\n| `encoding:label.` | `encoding:I2C-7bit.` | Data encoding |\n| `default:value.` | `default:400000.` | Default when unwired |\n| `bits:N.` | `bits:16.` | Significant bit count |\n\n> **Rule:** Every port must have `connection:mandatory.` or\n> `connection:optional.`. The IDE shows a `\u2299` warning badge and the server\n> emits a soft warning for ports missing this tag.\n\n### Manual pages\n\nYou can embed inline documentation inside the Go source using `/* */` block\ncomments. These appear as pages in the maker's component manual overlay.\n\n```go\n/*\nmanualName:wiring-guide.\nlanguage:en.\nshowIn:init.\n```markdown\n# Wiring Guide\n\n| Signal | Pico Pin |\n|--------|----------|\n| SDA    | GP4      |\n| SCL    | GP5      |\n```\n*/\n```\n\n| Tag | Values | Description |\n|---|---|---|\n| `manualName:` | any identifier | Page ID (required) |\n| `language:` | `en`, `pt`, etc. | BCP-47 language code |\n| `showIn:` | `init`, `run`, method name, `both` | Which block shows this page |\n\n---\n\n## 15. Complete example \u2014 APDS9960 colour sensor\n\n```go\n// Package blackbox\n//\n// APDS9960 reads RGBC colour data via I2C on RP2040.\n//\n// Place Init at global scope.\n// Wire I2CBus.bus \u2192 APDS9960.i2c before generating code.\npackage blackbox\n\nimport (\n\t\"machine\"\n\t\"time\"\n)\n\n// APDS9960 reads colour (RGBC) data via I2C.\n//\n// icon:lightbulb. label:APDS9960.\ntype APDS9960 struct {\n\tgain  byte `prop:\"ADC Gain\"         default:\"0\"   options:\"0,1,2,3\"`\n\tatime byte `prop:\"Integration Time\" default:\"255\"`\n}\n\n// Init configures the sensor.\n//\n// executionOrder:1. icon:hourglass-start. label:Init.\n//\n// Params\n//\n//\ti2c: I2C bus \u2014 wire from an I2CBus Init block.  connection:mandatory.  unit:i2c_bus.\n//\n// Returns\n//\n//\terr: initialisation error.  connection:optional.\nfunc (s *APDS9960) Init(i2c *machine.I2C) (err error) {\n\ts.i2c = i2c\n\ts.i2c.WriteRegister(0x39, 0x80, []byte{0x01})\n\ts.i2c.WriteRegister(0x39, 0x81, []byte{s.atime})\n\ts.i2c.WriteRegister(0x39, 0x8F, []byte{s.gain})\n\ts.i2c.WriteRegister(0x39, 0x80, []byte{0x03})\n\treturn nil\n}\n\n// Run reads the four RGBC colour channels.\n//\n// executionOrder:10. icon:sun. label:Run.\n//\n// Returns\n//\n//\tclear: total light intensity.  range:0..65535.  unit:lux_counts.   connection:optional.\n//\tred:   red channel.            range:0..65535.  unit:color_counts.  connection:optional.\n//\tgreen: green channel.          range:0..65535.  unit:color_counts.  connection:optional.\n//\tblue:  blue channel.           range:0..65535.  unit:color_counts.  connection:optional.\nfunc (s *APDS9960) Run() (clear, red, green, blue uint16) {\n\tdata := make([]byte, 8)\n\ts.i2c.ReadRegister(0x39, 0x94, data)\n\tclear = uint16(data[0]) | uint16(data[1])<<8\n\tred   = uint16(data[2]) | uint16(data[3])<<8\n\tgreen = uint16(data[4]) | uint16(data[5])<<8\n\tblue  = uint16(data[6]) | uint16(data[7])<<8\n\treturn\n}\n\n// Log prints the RGBC values over serial.\n//\n// executionOrder:20. icon:terminal. label:Log.\n//\n// Params\n//\n//\tclear: clear channel value.  range:0..65535.  unit:lux_counts.   connection:mandatory.\n//\tred:   red channel value.    range:0..65535.  unit:color_counts.  connection:mandatory.\n//\tgreen: green channel value.  range:0..65535.  unit:color_counts.  connection:mandatory.\n//\tblue:  blue channel value.   range:0..65535.  unit:color_counts.  connection:mandatory.\nfunc (s *APDS9960) Log(clear, red, green, blue uint16) {\n\tprintln(\"C:\", clear, \"R:\", red, \"G:\", green, \"B:\", blue)\n\ttime.Sleep(500 * time.Millisecond)\n}\n\n/*\nmanualName:wiring-guide.\nlanguage:en.\nshowIn:init.\n```markdown\n# APDS9960 \u2014 Wiring Guide\n\n| Signal | Default Pico Pin |\n|--------|-----------------|\n| SDA    | GP4             |\n| SCL    | GP5             |\n| VCC    | 3V3 (pin 36)    |\n| GND    | GND (pin 38)    |\n\nWire the **I2CBus.bus** output to the **APDS9960.i2c** input.\n```\n*/\n```\n\n**What the IDE shows after parsing:**\n\n```\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \u23f3  APDS9960 Init                                         \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502  \u25c9 i2c  *machine.I2C  [unit: i2c_bus]            + flag   \u2502\n  \u2502                                        error  err \u25ce       \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \u2600  APDS9960 Run                                          \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502                      uint16  clear \u25ce  [unit: lux_counts]  \u2502\n  \u2502                       uint16  red  \u25ce  [unit: color_counts]\u2502\n  \u2502                     uint16  green \u25ce                       \u2502\n  \u2502                      uint16  blue \u25ce                       \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n\n  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510\n  \u2502  \ud83d\udcbb  APDS9960 Log                                         \u2502\n  \u251c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2524\n  \u2502  \u25c9 clear  uint16  [unit: lux_counts]              + flag  \u2502\n  \u2502  \u25c9 red    uint16  [unit: color_counts]            + flag  \u2502\n  \u2502  \u25c9 green  uint16                                  + flag  \u2502\n  \u2502  \u25c9 blue   uint16                                  + flag  \u2502\n  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518\n```\n\n---\n\n## 16. Checklist before publishing\n\n- [ ] Package comment describes what the component does and how to place it\n- [ ] Struct has `icon:` and `label:` directives\n- [ ] Every method has `icon:`, `label:`, and `executionOrder:` directives\n- [ ] Every port has `connection:mandatory.` or `connection:optional.`\n- [ ] Ports that carry physical values have `unit:` and/or `range:` tags\n- [ ] Parse succeeds with **\u2713 OK** and zero \u2299 warnings\n- [ ] Live analysis shows no red underlines\n- [ ] At least one documentation file (`readme.md`) explains usage\n- [ ] A wiring image or diagram is uploaded and referenced in the docs\n- [ ] The component has been tested on real hardware\n- [ ] Project is set to `public` in Properties\n- [ ] **Ready to use** is checked (quality commitment accepted)\n\n---\n\n*IoTMaker Projects Guide \u00b7 revision 1*\n";

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
        Projects Guide
      </h1>
      <p style="color:var(--text-muted);font-size:14px;margin:4px 0 0">
        How to write, parse, version and publish black-box components.
      </p>
    </div>
    <button class="btn btn-secondary btn-sm" onclick="window._projCloseHelp()">
      <i class="fa-solid fa-arrow-left"></i> Back to Projects
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
