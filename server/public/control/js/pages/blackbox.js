// server/public/static/js/pages/blackbox.js
import { showAlert, showConfirm, showPrompt } from '../utils.js';
// pages/blackbox.js — BlackBox Creator
// Editor Go → parse no servidor → visual interativo → salvar

// ─── Estado da página ─────────────────────────────────────────────────────────

let bbParsed    = null;
let bbSavedId   = null;
let monacoInst  = null;
let monacoReady = false;
let bbDragSrc   = null;

// Próxima versão a ser usada no save.
// Calculada automaticamente após cada parse bem-sucedido:
//   - Se o componente nunca foi salvo → 1
//   - Se já existe no banco           → max(version) + 1
let bbNextVersion = 1;

// Timer for real-time semantic analysis debounce.
// Reset on every keystroke; analysis only fires after 600ms of silence.
let bbAnalyzeTimer = null;

// Monotonic counter — incremented on each analysis request.
// Used to discard stale fetch responses when a newer request is already in flight.
let bbAnalyzeSeq = 0;

// Tracks the current parse status type ('ok' | 'error' | 'warning' | '').
// Used by onDidChangeModelContent to decide whether to clear the status bar.
// Only 'ok' and 'warning' messages are cleared on edit — errors stay visible
// until the user fixes the code.
let bbParseStatusType = '';

// Timer for the "O código foi alterado..." warning auto-dismiss (10s).
let bbFormAlertTimer = null;

// Set to true whenever the user edits the code after an explicit parse.
// Only cleared by bbParse() (the Parsear button) — NOT by bbAutoReparse().
// bbSave() uses this flag to block saving stale code.
let bbNeedsExplicitParse = false;

// Flag: when false, real-time analysis is disabled (no debounce calls to the server).
// This is a system-level configuration — change it here in the source, not via UI.
// true  = analysis fires 600ms after each keystroke (uses server CPU per user)
// false = analysis only runs when the user clicks "Parsear"
const BB_REALTIME_ANALYSIS = true;

const DEFAULT_CODE = `// Package blackbox
//
// MyDevice — descrição do componente.
package blackbox

import "machine"

// MyDevice is a sample device connected via I2C.
type MyDevice struct {
  i2c   *machine.I2C
  speed byte \`setting:"Speed" default:"100" options:"50,100,200"\`
}

// Init configures the device.
//
// Params
//   i2c: reference to an I2C bus.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: initialization error.  connection:optional.
func (d *MyDevice) Init(i2c *machine.I2C) (err error) {
  d.i2c = i2c
  return nil
}

// SetColor sets RGB output.
//
// Params
//   r: red channel.    range:0..255.  unit:rgb.  default:0.  connection:mandatory.
//   g: green channel.  range:0..255.  unit:rgb.  default:0.  connection:mandatory.
//   b: blue channel.   range:0..255.  unit:rgb.  default:0.  connection:mandatory.
//
// Returns
//   err: write error.  connection:optional.
func (d *MyDevice) SetColor(r, g, b int) (err error) {
  return nil
}

// Run reads sensor data.
//
// Returns
//   value: measured value.  range:0..4095.  unit:adc_counts.  connection:optional.
//   state: logic state.     encoding:tristate.  options:-1|0|1.  connection:mandatory.
//          -1 = false, 0 = undefined, 1 = true.
func (d *MyDevice) Run() (value uint16, state int) {
  return 0, 0
}
`;

// ─── Entry point ──────────────────────────────────────────────────────────────

export function renderBlackbox(root) {
    bbParsed      = null;
    bbSavedId     = null;
    bbNextVersion = 1;
    root.innerHTML = buildShell();
    loadMonaco('');   // inicia vazio
    loadSavedList();
}

// ─── Shell HTML ───────────────────────────────────────────────────────────────

function buildShell() {
    return `
<div class="pw" style="max-width:1280px">

  <div style="display:flex;align-items:center;gap:12px;margin-bottom:24px;flex-wrap:wrap">
    <h1 style="font-size:24px;font-weight:800;color:var(--primary);display:flex;align-items:center;gap:10px">
      <i class="fa-solid fa-cube"></i>Device Creator
    </h1>
    <span style="font-size:13px;color:var(--text-muted);margin-left:4px">
      Cole código Go → gere o componente visual da IDE
    </span>
    <button class="btn btn-ghost btn-sm" onclick="bbOpenHelp()"
            style="margin-left:auto" title="Manual do BlackBox Creator">
      <i class="fa-solid fa-circle-question"></i> Manual
    </button>
  </div>

  <div class="bb-workspace">
    <!-- Editor Go -->
    <div class="bb-panel">
      <div class="bb-panel-header">
        <i class="fa-brands fa-golang" style="color:#00ADD8"></i>
        Código Go
        <div id="bb-version-bar" style="display:flex;align-items:center;gap:6px;margin-left:8px"></div>
        <div style="margin-left:auto;display:flex;gap:6px">
          <button class="btn btn-ghost btn-sm" onclick="bbLoadExample()" title="Carregar código de exemplo">
            <i class="fa-solid fa-lightbulb"></i> Exemplo
          </button>
          <button class="btn btn-primary btn-sm" onclick="bbParse()">
            <i class="fa-solid fa-play"></i> Parsear
          </button>
        </div>
      </div>
      <div id="bb-editor-wrap">
        <textarea id="bb-fallback"
          style="width:100%;height:480px;padding:16px;border:none;resize:none;
                 font-family:var(--mono);font-size:13px;line-height:1.6;
                 background:var(--bg-surface);color:var(--text-primary);outline:none"
          placeholder="Cole o código Go aqui ou clique em Exemplo..."></textarea>
      </div>
      <div id="bb-parse-status" style="padding:8px 16px;font-size:13px;min-height:32px"></div>
    </div>

    <!-- Preview visual -->
    <div class="bb-panel">
      <div class="bb-panel-header" style="gap:0;padding:0">
        <button class="bb-tab active" id="bb-tab-preview" onclick="bbSetTab('preview')">
          <i class="fa-solid fa-eye"></i> Preview
        </button>
        <button class="bb-tab" id="bb-tab-debug" onclick="bbSetTab('debug')">
          <i class="fa-solid fa-bug"></i> Debug
        </button>
        <span id="bb-preview-hint" style="margin-left:auto;font-size:12px;color:var(--text-muted);padding-right:12px">
          Clique em <strong>Parsear</strong> para visualizar
        </span>
      </div>
      <div id="bb-tab-body-preview" style="padding:24px;min-height:480px;background:var(--bg-surface);overflow:auto">
        <div class="bb-empty-state">
          <i class="fa-solid fa-cube" style="font-size:40px;color:var(--border);display:block;margin-bottom:12px"></i>
          <p style="color:var(--text-muted)">O componente aparece aqui após o parse</p>
        </div>
      </div>
      <div id="bb-tab-body-debug" style="display:none;padding:16px;min-height:480px;
           background:var(--bg-surface);overflow:auto;font-family:var(--mono);font-size:12px"></div>
    </div>
  </div>

  <!-- Formulário de metadados -->
  <div id="bb-meta-form" style="display:none" class="card" style="margin-top:24px">
    <div style="display:flex;align-items:center;gap:10px;margin-bottom:20px">
      <i class="fa-solid fa-sliders" style="color:var(--primary)"></i>
      <h2 style="font-size:18px;font-weight:700">Metadados do componente</h2>
    </div>

    <div class="bb-form-grid">
      <div class="bb-field">
        <label>Nome de exibição *</label>
        <input type="text" id="bb-f-name" placeholder="Ex: APDS-9960 Color Sensor"
               class="form-input" oninput="bbUpdateMeta()">
      </div>
      <div class="bb-field">
        <label>Categoria</label>
        <select id="bb-f-cat" class="form-input" onchange="bbUpdateMeta()">
          <option value="">— Selecione —</option>
          <option>sensor</option>
          <option>atuador</option>
          <option>barramento</option>
          <option>display</option>
          <option>comunicação</option>
          <option>energia</option>
          <option>outro</option>
        </select>
      </div>
    </div>

    <!-- Versão: read-only, calculada automaticamente -->
    <div class="bb-field" style="margin-top:16px">
      <label>Versão</label>
      <div id="bb-version-info"
           style="font-size:13px;color:var(--text-secondary);
                  padding:9px 12px;border:1px solid var(--border);
                  border-radius:var(--r);background:var(--bg-surface);
                  display:flex;align-items:center;gap:8px">
        <i class="fa-solid fa-code-branch" style="color:var(--text-muted)"></i>
        <span id="bb-version-label">Calculando...</span>
      </div>
    </div>

    <div class="bb-field" style="margin-top:16px">
      <label>Documentação do pacote (editável)</label>
      <textarea id="bb-f-doc" rows="4" class="form-input"
        style="font-family:var(--mono);font-size:13px;resize:vertical"
        placeholder="Gerada a partir dos comentários Go..."
        oninput="bbUpdateMeta()"></textarea>
    </div>

    <!-- Settings (ex-Props) -->
    <div id="bb-settings-section" style="margin-top:20px;display:none">
      <div style="font-size:14px;font-weight:700;margin-bottom:12px;color:var(--text-secondary)">
        <i class="fa-solid fa-sliders"></i>
        Settings — campos <code>setting</code> (painel Inspect, não são conectores)
      </div>
      <div id="bb-settings-list"></div>
    </div>

    <div id="bb-form-alert" style="margin-top:12px"></div>

    <div style="display:flex;gap:10px;margin-top:20px">
      <button class="btn btn-primary" onclick="bbSave()">
        <i class="fa-solid fa-floppy-disk"></i> Salvar BlackBox
      </button>
      <button class="btn btn-secondary" onclick="bbReset()">
        <i class="fa-solid fa-rotate-left"></i> Resetar
      </button>
      <span id="bb-save-status" style="font-size:13px;align-self:center;color:var(--success)"></span>
    </div>
  </div>

  <!-- Lista de salvos -->
  <div style="margin-top:36px">
    <div style="display:flex;align-items:center;gap:10px;margin-bottom:16px">
      <h2 style="font-size:18px;font-weight:700">
        <i class="fa-solid fa-layer-group" style="color:var(--primary)"></i>
        Componentes salvos
      </h2>
      <button class="btn btn-ghost btn-sm" onclick="loadSavedList()">
        <i class="fa-solid fa-rotate"></i>
      </button>
    </div>
    <div id="bb-saved-list">
      <div class="lspinner"><div class="spinner"></div></div>
    </div>
  </div>

</div>

<style>

/* ── tabs preview/debug ── */
.bb-tab{
  background:none;border:none;border-bottom:3px solid transparent;
  padding:12px 18px;font-size:13px;font-weight:700;cursor:pointer;
  color:var(--text-muted);font-family:var(--font);transition:color .15s,border-color .15s;
  display:flex;align-items:center;gap:6px;
}
.bb-tab:hover{color:var(--text-primary)}
.bb-tab.active{color:var(--primary);border-bottom-color:var(--primary)}

/* ── version-bar dropdown ── */
.bb-ver-select{
  font-size:12px;font-family:var(--mono);
  border:1px solid var(--border);border-radius:var(--r);
  background:var(--bg-input);color:var(--text-primary);
  padding:3px 8px;cursor:pointer;
}

/* workspace */
.bb-workspace{display:grid;grid-template-columns:1fr 1fr;gap:20px;margin-bottom:24px}
@media(max-width:900px){.bb-workspace{grid-template-columns:1fr}}

/* panels */
.bb-panel{border:1px solid var(--border);border-radius:var(--r);overflow:hidden;background:var(--bg-card);box-shadow:var(--sh)}
.bb-panel-header{
  display:flex;align-items:center;gap:8px;padding:12px 16px;
  background:var(--bg-surface);border-bottom:1px solid var(--border);
  font-size:14px;font-weight:700;color:var(--text-secondary)
}

/* form */
.bb-form-grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}
@media(max-width:700px){.bb-form-grid{grid-template-columns:1fr}}
.bb-field label{display:block;font-size:12px;font-weight:700;color:var(--text-muted);text-transform:uppercase;letter-spacing:.05em;margin-bottom:6px}
.form-input{width:100%;padding:9px 12px;border:1px solid var(--border);border-radius:var(--r);background:var(--bg-input);color:var(--text-primary);font-size:14px;font-family:var(--font);transition:border-color .15s,box-shadow .15s}
.form-input:focus{outline:none;border-color:var(--border-focus);box-shadow:0 0 0 3px rgba(34,85,170,.10)}

/* ── device visual ── */
.bb-blocks-container{
  display:flex;flex-direction:column;gap:16px;
}

/* Individual method block — mirrors the WASM SVG block appearance */
.bb-block{
  display:inline-block;min-width:300px;max-width:100%;
  border:2px solid var(--primary);border-radius:var(--rl);
  background:var(--bg-card);box-shadow:var(--shh);overflow:hidden;
  font-family:var(--mono);font-size:13px;user-select:none;
}

/* Block header: icon row + label row, matching WASM bbHeaderH=44px */
.bb-block-hdr{
  background:var(--primary);color:#fff;
  padding:6px 16px 8px;
  display:flex;flex-direction:column;align-items:center;gap:2px;
}
.bb-block-hdr-icon{
  font-size:18px;line-height:1.2;
}
/* Unicode-rendered FA icon: uses the webfont loaded in <head>.
   .bb-icon-unicode  → FA Solid  (font-family "Font Awesome 6 Free", weight 900)
   .bb-icon-brands   → FA Brands (font-family "Font Awesome 6 Brands", weight 400)  */
.bb-icon-unicode{
  font-family:"Font Awesome 6 Free";
  font-weight:900;
  font-style:normal;
}
.bb-icon-unicode.bb-icon-brands{
  font-family:"Font Awesome 6 Brands";
  font-weight:400;
}
.bb-block-hdr-label{
  font-weight:800;font-size:13px;letter-spacing:.03em;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%;
}
/* category badge kept for backward compat */
.bb-block-hdr .bb-cat{
  font-size:11px;font-weight:600;
  background:rgba(255,255,255,.2);padding:3px 10px;border-radius:99px;
  margin-top:2px;
}

/* ── pins ── */
.bb-pin{
  display:flex;align-items:center;
  padding:5px 0;border-bottom:1px solid var(--border);
  cursor:default;position:relative;transition:background .12s;
}
.bb-pin:last-child{border-bottom:none}
.bb-pin:hover{background:var(--bg-surface)}
.bb-pin.dragging{opacity:.4}
.bb-pin.drag-over{background:var(--info-bg);outline:2px dashed var(--primary)}
.bb-pin.input {flex-direction:row}
.bb-pin.output{flex-direction:row}

/* dot de conector */
.bb-dot{
  font-size:18px;line-height:1;flex-shrink:0;
  cursor:pointer;transition:transform .12s;
  display:flex;align-items:center;justify-content:center;
  width:22px;
}
.bb-dot.in {color:var(--primary);margin-left:3px}
.bb-dot.out{color:var(--success);margin-right:3px}
.bb-dot:hover{transform:scale(1.2)}

.bb-drag{
  width:22px;flex-shrink:0;display:flex;align-items:center;justify-content:center;
  color:var(--border);font-size:14px;cursor:grab;padding:0 4px;
  transition:color .12s
}
.bb-drag:hover{color:var(--text-muted)}
.bb-drag:active{cursor:grabbing}

.bb-pin-name{
  padding:1px 6px;border-radius:4px;font-weight:700;
  color:var(--text-primary);min-width:40px;
  outline:none;transition:background .1s,box-shadow .1s;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100px;
}
.bb-pin-name[contenteditable="true"]{
  background:var(--bg-input);
  box-shadow:0 0 0 2px var(--border-focus);
  cursor:text;user-select:text;
}

.bb-pin-type{
  font-size:11px;color:var(--text-muted);
  padding:1px 7px;background:var(--bg-surface);
  border-radius:99px;border:1px solid var(--border);
  white-space:nowrap;flex-shrink:0;margin:0 3px;
}

/* ── badges IDS inline ── */
.bb-badges{display:flex;flex-wrap:wrap;gap:3px;padding:0 3px;flex:1;align-items:center}
.bb-badge{
  font-size:10px;font-family:var(--font);font-weight:600;
  border-radius:99px;padding:1px 7px;white-space:nowrap;
  display:inline-flex;align-items:center;gap:3px;
}
.bb-badge.range   {background:#EEF4FF;color:#2255AA;border:1px solid #BDD3FF}
.bb-badge.unit    {background:var(--bg-surface);color:var(--text-muted);border:1px solid var(--border)}
.bb-badge.encoding{background:#FEF3CD;color:#B7770D;border:1px solid #FFD97D}
.bb-badge.flag    {background:var(--warning-bg);color:var(--warning);border:1px solid var(--warning)}
.bb-badge.warn    {background:#FDE8E8;color:#C0392B;border:1px solid #F5B7B1}

/* ── add-flag button ── */
.bb-add-flag{
  font-size:10px;background:none;border:1px dashed var(--border);
  border-radius:99px;padding:1px 7px;cursor:pointer;color:var(--text-muted);
  font-family:var(--font);transition:border-color .12s,color .12s
}
.bb-add-flag:hover{border-color:var(--primary);color:var(--primary)}
.bb-flag-del{
  background:none;border:none;cursor:pointer;color:inherit;
  font-size:11px;padding:0 1px;line-height:1;opacity:.7;
}
.bb-flag-del:hover{opacity:1}

/* ── tooltip balloon ── */
.bb-pin-wrap{position:relative;display:flex;align-items:center;flex:1}
.bb-tooltip{
  display:none;
  position:absolute;
  bottom:calc(100% + 6px);
  left:50%;transform:translateX(-50%);
  background:#1A1A2E;color:#fff;
  font-size:12px;font-family:var(--font);line-height:1.5;
  padding:8px 12px;border-radius:var(--r);
  white-space:nowrap;z-index:200;
  box-shadow:0 4px 16px rgba(0,0,0,.25);
  pointer-events:none;
  max-width:320px;white-space:normal;
}
.bb-tooltip::after{
  content:'';position:absolute;top:100%;left:50%;
  transform:translateX(-50%);
  border:6px solid transparent;border-top-color:#1A1A2E;
}
.bb-pin:hover .bb-tooltip{display:block}

/* ── empty state ── */
.bb-empty-state{display:flex;flex-direction:column;align-items:center;justify-content:center;height:100%;min-height:200px;padding:40px}

/* ── legend ── */
.bb-legend{
  display:flex;gap:20px;justify-content:center;
  padding:10px 16px;border-top:1px solid var(--border);
  background:var(--bg-surface);font-size:12px;color:var(--text-muted);
  flex-wrap:wrap;
}
.bb-legend-item{display:flex;align-items:center;gap:5px;font-family:var(--mono)}
.bb-legend-sym{font-size:16px}
.bb-legend-sym.opt {color:var(--primary)}

/* ── diff side-by-side (table layout) ── */
.diff-table{
  width:100%;border-collapse:collapse;table-layout:fixed;
  font-family:var(--mono);font-size:12px;line-height:1.6;
}
.diff-table td{padding:0;vertical-align:top;border-bottom:1px solid rgba(0,0,0,.04)}
.diff-col-l,.diff-col-r{width:calc(50% - 22px)}
.diff-col-m{width:44px}
.diff-cell-l{padding:0 8px;white-space:pre;overflow:hidden;text-overflow:ellipsis;
  border-right:1px solid var(--border)}
.diff-cell-r{padding:0;white-space:pre;overflow:hidden}
.diff-cell-r[contenteditable]{
  outline:none;display:block;width:100%;min-height:1.6em;padding:0 8px;
  cursor:text;caret-color:var(--text-primary);
}
.diff-cell-m{
  text-align:center;vertical-align:middle;
  background:var(--bg-surface);
  border-left:1px solid var(--border);border-right:1px solid var(--border);
  padding:2px 2px;
}
/* row colours */
.diff-row-eq  .diff-cell-l,
.diff-row-eq  .diff-cell-r { color:var(--text-muted) }
/* arrows */
.diff-arrow{
  display:block;width:30px;height:20px;margin:1px auto;
  background:var(--bg-card);border:1px solid var(--border);
  border-radius:4px;cursor:pointer;font-size:11px;
  color:var(--text-muted);transition:background .12s,color .12s;
  line-height:20px;text-align:center;
}
.diff-arrow:hover       { background:var(--primary);color:#fff;border-color:var(--primary) }
.diff-arrow-active      { background:var(--primary)!important;color:#fff!important;border-color:var(--primary)!important }
.diff-arrow-off     { opacity:.4;background:var(--bg-surface) }
.diff-bg-rem    { background:#FFEBE9 !important;color:#B91C1C }
.diff-bg-add    { background:#E6FFED !important;color:#166534 }
.diff-bg-chosen { background:#EFF6FF !important;color:#1D4ED8 }
.diff-bg-empty  { background:#F9FAFB !important }
.bb-legend-sym.mand{color:var(--success)}

/* ── parse warnings ── */
.bb-warnings{
  margin-bottom:16px;padding:12px 16px;
  background:#FFF8E1;border:1px solid #FFD97D;border-radius:var(--r);
  font-size:13px;
}
.bb-warnings ul{margin:6px 0 0 18px;color:#7A5C00}
.bb-warnings strong{color:#B7770D}

/* ── saved cards ── */
.bb-card-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:16px}
.bb-saved-card{border:1px solid var(--border);border-radius:var(--r);background:var(--bg-card);padding:16px;display:flex;flex-direction:column;gap:8px;box-shadow:var(--sh);transition:box-shadow .15s}
.bb-saved-card:hover{box-shadow:var(--shh)}
.bb-saved-card h3{font-size:15px;font-weight:700;color:var(--text-primary)}
.bb-saved-card .bb-saved-meta{font-size:12px;color:var(--text-muted);display:flex;flex-wrap:wrap;gap:6px}
.bb-saved-card .tag{background:var(--bg-surface);border:1px solid var(--border);border-radius:99px;padding:1px 8px}
.bb-saved-actions{display:flex;gap:6px;margin-top:4px}

/* ── settings table ── */
.bb-settings-table{width:100%;border-collapse:collapse;border:1px solid var(--border);border-radius:var(--r);overflow:hidden}
.bb-settings-table th{background:var(--bg-surface);padding:8px 12px;font-size:11px;color:var(--text-muted);text-align:left;text-transform:uppercase;letter-spacing:.05em}
.bb-settings-table td{padding:7px 12px;border-top:1px solid var(--border);font-size:13px;font-family:var(--mono)}
</style>`;
}

function esc(s) {
    return (s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// ─── Monaco ───────────────────────────────────────────────────────────────────

function loadMonaco(init) {
    if (monacoReady && window.monaco) { mountMonaco(init); return; }
    const s   = document.createElement('script');
    s.src     = 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.44.0/min/vs/loader.min.js';
    s.onload  = () => {
        require.config({ paths: { vs: 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.44.0/min/vs' } });
        require(['vs/editor/editor.main'], () => { monacoReady = true; mountMonaco(init); });
    };
    s.onerror = () => {};
    document.head.appendChild(s);
}

function mountMonaco(init) {
    const wrap = document.getElementById('bb-editor-wrap');
    if (!wrap) return;
    const ta = document.getElementById('bb-fallback');
    if (ta) ta.remove();
    if (monacoInst) { monacoInst.dispose(); monacoInst = null; }
    const div = document.createElement('div');
    div.style.cssText = 'height:480px';
    wrap.appendChild(div);
    monacoInst = monaco.editor.create(div, {
        value: init, language: 'go', theme: 'vs',
        wordWrap: 'off', lineNumbers: 'on',
        minimap: { enabled: true }, scrollBeyondLastLine: false,
        fontSize: 13, fontFamily: "'Fira Code','Consolas',monospace",
        automaticLayout: true, glyphMargin: true,
        wordBasedSuggestions: false,
        quickSuggestions: false,
        suggestOnTriggerCharacters: false,
        parameterHints: { enabled: false },
    });

    monacoInst.onDidChangeModelContent(() => {
        // ── Diff modal: recalcula hunks quando o modal está aberto ──────────
        const wrap = document.getElementById('diff-table-wrap');
        if (wrap) {
            const sel = document.getElementById('diff-ver-sel');
            if (sel) {
                const savedId   = sel.value;
                const savedCode = window._diffCodeMap?.[savedId] || '';
                _diffHunks   = diffHunks(savedCode, monacoInst.getValue());
                _diffChoices = _diffHunks.filter(h => h.type !== 'equal').map(() => 'right');
                refreshDiffTable();
            }
        }
        // ── Real-time semantic analysis (debounced 600ms) ────────────────────
        bbScheduleAnalyze();

        // ── Clear non-error status bar when user starts editing ─────────────
        if (bbParseStatusType === 'ok' || bbParseStatusType === 'warning') {
            setParseStatus('', '');
        }

        // ── Mark that an explicit re-parse is required before saving ─────────
        if (bbParsed) {
            bbNeedsExplicitParse = true;
        }

        // ── Clear the preview so the user sees it is stale ──────────────────
        const previewEl = document.getElementById('bb-tab-body-preview');
        if (previewEl && bbParsed) {
            previewEl.innerHTML = '<p style="padding:24px;color:var(--muted);font-size:13px">' +
                '<i class="fa-solid fa-circle-notch fa-spin" style="margin-right:6px"></i>' +
                'Atualizando preview...</p>';
        }
    });

    if (!document.getElementById('monaco-error-css')) {
        const s = document.createElement('style');
        s.id = 'monaco-error-css';
        s.textContent = `
.monaco-error-line { background: rgba(239,68,68,.12) !important; }
.monaco-error-glyph::before {
    content: '✕';
    color: #EF4444;
    font-size: 13px;
    font-weight: bold;
    margin-left: 2px;
}`;
        document.head.appendChild(s);
    }
}

function getCode() {
    if (monacoInst) return monacoInst.getValue();
    return document.getElementById('bb-fallback')?.value || '';
}

export function bbClearEditor() {
    if (monacoInst) monacoInst.setValue('');
    else { const ta = document.getElementById('bb-fallback'); if (ta) ta.value = ''; }
}

// ─── Example picker ───────────────────────────────────────────────────────────

const EXAMPLE_I2CBUS = `// Package blackbox
//
// I2CBus configures a hardware I2C peripheral on a Raspberry Pi Pico (RP2040).
//
// Place at the global scope (outside any Loop). Wire its "bus" output to
// the "i2c" input of any sensor that requires an I2C connection.
package blackbox

import (
\t"machine"
\t"time"
)

// I2CBus configures an I2C bus. All settings come from the Inspect panel.
type I2CBus struct {
\tsda  string \`prop:"SDA Pin"   default:"GP4"    options:"GP4,GP6,GP8,GP10,GP12,GP14"\`
\tscl  string \`prop:"SCL Pin"   default:"GP5"    options:"GP5,GP7,GP9,GP11,GP13,GP15"\`
\tfreq string \`prop:"Frequency" default:"400000" options:"100000,400000,1000000"\`
}

// Init sets up the I2C peripheral and returns a ready bus reference.
//
// executionOrder:1
//
// Returns
//   bus: configured I2C bus — wire this to sensors that need I2C.  connection:mandatory.  unit:i2c_bus.
//   err: bus initialisation error.  connection:optional.
func (s *I2CBus) Init() (bus *machine.I2C, err error) {
\tb := machine.I2C0
\tb.Configure(machine.I2CConfig{
\t\tFrequency: 400_000,
\t\tSDA:       machine.GP4,
\t\tSCL:       machine.GP5,
\t})
\ttime.Sleep(100 * time.Millisecond)
\treturn b, nil
}

/*
manualName:wiring-guide.
language:en.
showIn:init.
\`\`\`markdown
# I2CBus — Wiring Guide

| Signal | Default Pico Pin |
|--------|-----------------|
| SDA    | GP4             |
| SCL    | GP5             |
| VCC    | 3V3 (pin 36)    |
| GND    | GND (pin 38)    |

Wire the **bus** output to the **i2c** input of any sensor block.
\`\`\`*/
`;

const EXAMPLE_APDS9960 = `// Package blackbox
//
// APDS9960 is a colour, proximity, and gesture sensor connected via I2C.
//
// Place Init at the global scope.
// Wire I2CBus.bus → APDS9960.i2c before generating code.
package blackbox

import "machine"

// APDS9960 reads colour (RGBC) data via I2C.
type APDS9960 struct {
\ti2c   *machine.I2C
\tgain  byte \`prop:"ADC Gain"         default:"0"   options:"0,1,2,3"\`
\tatime byte \`prop:"Integration Time" default:"255"\`
}

// Init configures the sensor on the given I2C bus.
//
// executionOrder:10
//
// Params
//   i2c: I2C bus — wire from an I2CBus Init block.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: initialisation error.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) {
\ts.i2c = i2c
\ts.i2c.WriteRegister(0x39, 0x80, []byte{0x01})
\ts.i2c.WriteRegister(0x39, 0x81, []byte{s.atime})
\ts.i2c.WriteRegister(0x39, 0x8F, []byte{s.gain})
\ts.i2c.WriteRegister(0x39, 0x80, []byte{0x03})
\treturn nil
}

// Run reads the four RGBC colour channels.
//
// Returns
//   clear: total light intensity.  range:0..65535.  unit:lux_counts.   connection:optional.
//   red:   red channel.            range:0..65535.  unit:color_counts.  connection:optional.
//   green: green channel.          range:0..65535.  unit:color_counts.  connection:optional.
//   blue:  blue channel.           range:0..65535.  unit:color_counts.  connection:optional.
func (s *APDS9960) Run() (clear, red, green, blue uint16) {
\tdata := make([]byte, 8)
\ts.i2c.ReadRegister(0x39, 0x94, data)
\tclear = uint16(data[0]) | uint16(data[1])<<8
\tred   = uint16(data[2]) | uint16(data[3])<<8
\tgreen = uint16(data[4]) | uint16(data[5])<<8
\tblue  = uint16(data[6]) | uint16(data[7])<<8
\treturn
}

/*
manualName:wiring-guide.
language:en.
showIn:init.
\`\`\`markdown
# APDS9960 — Wiring Guide

| APDS9960 | Pico Pin | Notes      |
|----------|----------|------------|
| SDA      | GP4      | I2C data   |
| SCL      | GP5      | I2C clock  |
| VCC      | 3V3      | 3.3 V only |
| GND      | GND      |            |

Place an **I2CBus Init** block first and wire its **bus** output here.
\`\`\`*/

/*
manualName:reading-colors.
language:en.
showIn:run.
\`\`\`markdown
# Reading Colour Values

Divide each channel by \`clear\` to get a lighting-independent ratio:

\`\`\`
redRatio   = red   / clear
greenRatio = green / clear
blueRatio  = blue  / clear
\`\`\`

A ratio near 1.0 on a single channel means that colour dominates the scene.
\`\`\`*/
`;

export function bbLoadExample() {
    document.getElementById('bb-example-picker')?.remove();

    const modal = document.createElement('div');
    modal.id = 'bb-example-picker';
    modal.style.cssText =
        'position:fixed;inset:0;background:rgba(0,0,0,.55);' +
        'z-index:1000;display:flex;align-items:center;justify-content:center;padding:16px';

    modal.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);
            box-shadow:0 8px 40px rgba(0,0,0,.35);
            width:min(640px,100%);padding:24px;display:flex;flex-direction:column;gap:20px">

  <!-- header -->
  <div style="display:flex;align-items:center;gap:10px">
    <i class="fa-solid fa-lightbulb" style="color:var(--primary);font-size:18px"></i>
    <strong style="font-size:16px;color:var(--text-primary)">Choose an example</strong>
    <button id="bb-example-close"
            style="margin-left:auto;background:none;border:none;cursor:pointer;
                   font-size:18px;color:var(--text-muted);line-height:1;padding:4px 8px"
            title="Close">&#x2715;</button>
  </div>

  <!-- cards -->
  <div style="display:flex;gap:16px;flex-wrap:wrap">

    <!-- I2CBus card -->
    <button id="bb-ex-i2cbus"
            style="flex:1;min-width:220px;min-height:100px;
                   background:var(--bg-surface);border:2px solid var(--border);
                   border-radius:12px;padding:20px 18px;cursor:pointer;text-align:left;
                   display:flex;flex-direction:column;gap:8px;
                   transition:border-color .15s,background .15s">
      <span style="display:flex;align-items:center;gap:8px">
        <i class="fa-solid fa-bus" style="color:var(--primary);font-size:16px"></i>
        <strong style="font-size:14px;color:var(--text-primary)">I2C Bus</strong>
      </span>
      <span style="font-size:12px;color:var(--text-muted);line-height:1.5">
        Init-only component. Configures a hardware I2C peripheral.<br>
        Shows: <code style="font-size:11px">executionOrder:1</code>, <code style="font-size:11px">prop</code> tags,
        manual page in English.
      </span>
    </button>

    <!-- APDS9960 card -->
    <button id="bb-ex-apds"
            style="flex:1;min-width:220px;min-height:100px;
                   background:var(--bg-surface);border:2px solid var(--border);
                   border-radius:12px;padding:20px 18px;cursor:pointer;text-align:left;
                   display:flex;flex-direction:column;gap:8px;
                   transition:border-color .15s,background .15s">
      <span style="display:flex;align-items:center;gap:8px">
        <i class="fa-solid fa-eye" style="color:var(--primary);font-size:16px"></i>
        <strong style="font-size:14px;color:var(--text-primary)">APDS9960 Colour Sensor</strong>
      </span>
      <span style="font-size:12px;color:var(--text-muted);line-height:1.5">
        Init + Run component. Reads RGBC colour data via I2C.<br>
        Shows: <code style="font-size:11px">executionOrder:10</code>, IDS tags, two manual
        pages (<code style="font-size:11px">showIn:init</code> and <code style="font-size:11px">showIn:run</code>).
      </span>
    </button>

  </div>

  <p style="font-size:11px;color:var(--text-muted);margin:0;text-align:center">
    Loading an example will replace the current editor content.
  </p>
</div>`;

    document.body.appendChild(modal);

    const load = (code) => {
        if (monacoInst) monacoInst.setValue(code);
        else {
            const ta = document.getElementById('bb-fallback');
            if (ta) ta.value = code;
        }
        modal.remove();
    };

    ['bb-ex-i2cbus', 'bb-ex-apds'].forEach(id => {
        const btn = document.getElementById(id);
        btn.addEventListener('mouseenter', () => {
            btn.style.borderColor = 'var(--primary)';
            btn.style.background  = 'var(--bg-page, #eef2f8)';
        });
        btn.addEventListener('mouseleave', () => {
            btn.style.borderColor = 'var(--border)';
            btn.style.background  = 'var(--bg-surface)';
        });
    });

    document.getElementById('bb-ex-i2cbus').addEventListener('click', () => load(EXAMPLE_I2CBUS));
    document.getElementById('bb-ex-apds').addEventListener('click',   () => load(EXAMPLE_APDS9960));
    document.getElementById('bb-example-close').addEventListener('click', () => modal.remove());

    modal.addEventListener('click', e => { if (e.target === modal) modal.remove(); });

    const escHandler = e => { if (e.key === 'Escape') { modal.remove(); document.removeEventListener('keydown', escHandler); } };
    document.addEventListener('keydown', escHandler);
}

// ─── Parse ────────────────────────────────────────────────────────────────────

export async function bbParse() {
    const rawCode = getCode();
    const code    = rawCode.trim();
    if (!code) { setParseStatus('Código vazio', 'warning'); return; }

    setParseStatus('<i class="fa-solid fa-circle-notch fa-spin"></i> Parseando...', '');
    try {
        const res  = await fetch('/api/blackbox/parse', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code }),
        });
        const json = await res.json();
        if (json?.metadata?.status !== 200) {
            setParseStatus('✗ ' + (json?.metadata?.error || 'Erro no parse'), 'error');
            return;
        }

        setParseStatus('<i class="fa-solid fa-circle-notch fa-spin"></i> Analisando...', '');
        clearTimeout(bbAnalyzeTimer);
        const analyzeResult = await bbRunAnalyze();
        if (analyzeResult?.hasErrors) {
            setParseStatus(formatAnalyzeErrors(analyzeResult.diagnostics), 'error');
            return;
        }

        applyMonacoMarkers([]);
        bbFormAlert('', '');
        bbNeedsExplicitParse = false;

        bbParsed = json.data;
        bbParsed.sourceCode = rawCode;
        const nMethods  = bbParsed.methods?.length  || 0;
        const nSettings = bbParsed.settings?.length || 0;
        const nWarns    = bbParsed.parseWarnings?.length || 0;
        const nPins = (bbParsed.methods||[]).reduce((a,m) =>
            a + (m.inputs?.length||0) + (m.outputs?.length||0), 0);
        const warnPart = nWarns
            ? ` · <span style="color:var(--warning)">⚠ ${nWarns} pino(s) sem connection:</span>`
            : '';
        setParseStatus(
            `✓ Parse OK — ${nMethods} método(s) · ${nPins} pino(s) · ${nSettings} setting(s)${warnPart}`,
            'ok'
        );
        renderPreview();
        showMetaForm();

    } catch (e) {
        setParseStatus('✗ Sem conexão: ' + e.message, 'error');
    }
}


function setParseStatus(msg, type) {
    const el = document.getElementById('bb-parse-status');
    if (!el) return;

    bbParseStatusType = type;

    if (type === 'error') {
        el.style.cssText = 'padding:10px 16px;font-size:13px;min-height:32px;' +
            'background:#FEE2E2;border-top:2px solid var(--danger);color:#991B1B';

        const match = msg.match(/bb\.go:(\d+):(\d+)/);
        if (match && monacoInst) {
            const line = parseInt(match[1]);
            const col  = parseInt(match[2]);
            monacoInst.deltaDecorations(monacoInst._errorDecorations || [], []);
            monacoInst._errorDecorations = monacoInst.deltaDecorations([], [{
                range: new monaco.Range(line, 1, line, 9999),
                options: {
                    isWholeLine: true,
                    className:   'monaco-error-line',
                    glyphMarginClassName: 'monaco-error-glyph',
                    overviewRuler: { color: '#EF4444', position: 1 },
                    minimap: { color: '#EF4444', position: 1 },
                }
            }]);
            monacoInst.revealLineInCenter(line);
            monacoInst.setPosition({ lineNumber: line, column: col });
        }

        const fmtMsg = msg.replace(
            /(bb\.go:\d+:\d+:)/,
            '<strong style="font-family:var(--mono)">$1</strong>'
        );
        el.innerHTML = `<i class="fa-solid fa-circle-xmark" style="margin-right:6px"></i>${fmtMsg}`;

    } else {
        el.style.cssText = 'padding:8px 16px;font-size:13px;min-height:32px';
        const color = type === 'ok' ? 'var(--success)' : 'var(--warning)';
        el.innerHTML = `<span style="color:${color}">${msg}</span>`;

        if (monacoInst?._errorDecorations) {
            monacoInst.deltaDecorations(monacoInst._errorDecorations, []);
            monacoInst._errorDecorations = [];
        }
    }
}

// ─── Real-time semantic analysis ─────────────────────────────────────────────

function bbScheduleAnalyze() {
    if (!BB_REALTIME_ANALYSIS) return;
    clearTimeout(bbAnalyzeTimer);
    bbAnalyzeTimer = setTimeout(bbRunAnalyze, 600);
}

async function bbRunAnalyze() {
    const seq  = ++bbAnalyzeSeq;
    const code = getCode();
    if (!code.trim() || !monacoInst) return null;

    try {
        const res = await fetch('/api/blackbox/analyze', {
            method:  'POST',
            headers: { 'Content-Type': 'application/json' },
            body:    JSON.stringify({ code }),
        });
        if (!res.ok) return null;

        if (bbAnalyzeSeq !== seq) return null;

        const json = await res.json();
        if (json?.metadata?.status !== 200) return null;

        const result = json.data;
        applyMonacoMarkers(result.diagnostics || []);

        if (result.hasErrors) {
            setParseStatus(formatAnalyzeErrors(result.diagnostics), 'error');
        } else {
            if (bbParsed) {
                const nMethods  = bbParsed.methods?.length  || 0;
                const nSettings = bbParsed.settings?.length || 0;
                const nWarns    = bbParsed.parseWarnings?.length || 0;
                const nPins = (bbParsed.methods||[]).reduce((a,m) =>
                    a + (m.inputs?.length||0) + (m.outputs?.length||0), 0);
                const warnPart = nWarns
                    ? ` · <span style="color:var(--warning)">⚠ ${nWarns} pino(s) sem connection:</span>`
                    : '';
                setParseStatus(
                    `✓ Parse OK — ${nMethods} método(s) · ${nPins} pino(s) · ${nSettings} setting(s)${warnPart}`,
                    'ok'
                );
            } else {
                setParseStatus('', '');
            }
            bbAutoReparse();
        }

        return result;

    } catch {
        return null;
    }
}

function applyMonacoMarkers(diagnostics) {
    if (!monacoInst || !window.monaco) return;
    const model = monacoInst.getModel();
    if (!model) return;

    const severityMap = { error: 8, warning: 4, info: 2, hint: 1 };

    monaco.editor.setModelMarkers(model, 'blackbox-analyzer',
        diagnostics.map(d => ({
            severity:        severityMap[d.severity] ?? 8,
            message:         formatDiagMsg(d),
            startLineNumber: d.line,
            startColumn:     d.col,
            endLineNumber:   d.endLine,
            endColumn:       d.endCol,
            source:          d.source,
        }))
    );

    const errorDecorations = diagnostics
        .filter(d => d.severity === 'error')
        .map(d => ({
            range: new monaco.Range(d.line, 1, d.endLine, 9999),
            options: {
                isWholeLine:          true,
                className:            'monaco-error-line',
                glyphMarginClassName: 'monaco-error-glyph',
                overviewRuler:        { color: '#EF4444', position: 1 },
                minimap:              { color: '#EF4444', position: 1 },
            },
        }));

    const combined = monacoInst.deltaDecorations(
        [
            ...(monacoInst._errorDecorations  || []),
            ...(monacoInst._analyzeDecorations || []),
        ],
        errorDecorations
    );
    monacoInst._errorDecorations   = [];
    monacoInst._analyzeDecorations = combined;
}

function formatDiagMsg(d) {
    return d.message.replace(/^blackbox\.go:\d+:\d+:\s*/, '').trim();
}

function formatAnalyzeErrors(diagnostics) {
    const errors = diagnostics.filter(d => d.severity === 'error');
    if (!errors.length) return '✗ Erro na análise semântica';

    const shown = errors.slice(0, 2).map(d =>
        `<span style="font-family:var(--mono);font-weight:700">` +
        `linha&nbsp;${d.line}:</span> ${esc(formatDiagMsg(d))}`
    ).join(' &nbsp;·&nbsp; ');

    const more = errors.length > 2
        ? ` <span style="opacity:.65">(+${errors.length - 2} mais)</span>`
        : '';

    return `✗ ${errors.length} erro(s) semântico(s) — ${shown}${more}`;
}

async function bbAutoReparse() {
    if (!bbParsed) return;

    const rawCode = getCode();
    if (!rawCode.trim()) return;

    try {
        const res = await fetch('/api/blackbox/parse', {
            method:  'POST',
            headers: { 'Content-Type': 'application/json' },
            body:    JSON.stringify({ code: rawCode }),
        });
        const json = await res.json();
        if (json?.metadata?.status !== 200) return;

        const prevSourceCode = bbParsed.sourceCode;
        bbParsed = json.data;
        bbParsed.sourceCode = prevSourceCode;
        renderPreview();

    } catch {
        // Network error — silently ignore.
    }
}

// ─── Preview interativo ───────────────────────────────────────────────────────

function renderPreview() {
    const el = document.getElementById('bb-tab-body-preview');
    if (!el || !bbParsed) return;
    const hint = document.getElementById('bb-preview-hint');
    if (hint) hint.textContent = 'Hover nos pinos para detalhes • Clique no nome para renomear';
    el.innerHTML = buildDevice(bbParsed);
    const dbg = document.getElementById('bb-tab-body-debug');
    if (dbg) dbg.innerHTML = `<pre style="margin:0;white-space:pre-wrap;word-break:break-all">${esc(JSON.stringify(bbParsed, null, 2))}</pre>`;
    renderVersionBar();
}

export function bbSetTab(tab) {
    ['preview','debug'].forEach(t => {
        document.getElementById('bb-tab-'+t)?.classList.toggle('active', t === tab);
        const body = document.getElementById('bb-tab-body-'+t);
        if (body) body.style.display = t === tab ? '' : 'none';
    });
}

function buildDevice(bb) {
    let warnHtml = '';
    if (bb.parseWarnings?.length) {
        const items = bb.parseWarnings.map(w => `<li>${esc(w)}</li>`).join('');
        warnHtml = `<div class="bb-warnings">
  <strong>⚠ tag <code>connection:</code> ausente em ${bb.parseWarnings.length} pino(s)</strong>
  <ul>${items}</ul>
</div>`;
    }

    const structLabel = bb.structLabel || bb.displayName || bb.structName || 'Device';
    const structIcon  = bb.structIcon  || 'cube';
    const cat         = bb.category    || '';

    const cards = (bb.methods || []).map((method, mi) => buildMethodCard(bb, method, mi, structLabel, structIcon, cat));

    const legend = `<div class="bb-legend">
  <span class="bb-legend-item">
    <span class="bb-legend-sym opt">◎</span> conexão opcional
  </span>
  <span class="bb-legend-item">
    <span class="bb-legend-sym mand">◉</span> conexão obrigatória
  </span>
</div>`;

    return warnHtml
        + `<div class="bb-blocks-container" id="bb-device">`
        + cards.join('')
        + legend
        + `</div>`;
}

function buildMethodCard(bb, method, mi, structLabel, structIcon, cat) {
    const methodIcon  = method.icon  || structIcon;
    const methodLabel = method.label || method.name;
    const headerTitle = `${esc(structLabel)} ${esc(methodLabel)}`;

    const catBadge = (mi === 0 && cat)
        ? `<span class="bb-cat">${esc(cat)}</span>`
        : '';

    const iconHtml = renderFAIconHtml(methodIcon);

    const hdr = `<div class="bb-block-hdr" id="bb-blk-hdr-${mi}">
  <div class="bb-block-hdr-icon">${iconHtml}</div>
  <div class="bb-block-hdr-label">${headerTitle}${catBadge}</div>
</div>`;

    let pins = '';
    (method.inputs  || []).forEach((pin, pi) => { pins += buildPin(pin, mi, pi, 'input');  });
    (method.outputs || []).forEach((pin, pi) => { pins += buildPin(pin, mi, pi, 'output'); });

    return `<div class="bb-block" id="bb-blk-${mi}">${hdr}${pins}</div>`;
}

function renderFAIconHtml(iconValue) {
    if (!iconValue) {
        return `<i class="fa-solid fa-cube"></i>`;
    }

    let candidate = iconValue.replace(/^\\u/i, '').replace(/^0x/i, '');
    let explicitStyle = '';

    const colonIdx = candidate.lastIndexOf(':');
    if (colonIdx > 0) {
        const suffix = candidate.slice(colonIdx + 1).toLowerCase();
        candidate   = candidate.slice(0, colonIdx);
        if (suffix === 'b' || suffix === 'brands')       explicitStyle = 'b';
        else if (suffix === 'r' || suffix === 'regular') explicitStyle = 'r';
        else if (suffix === 's' || suffix === 'solid')   explicitStyle = 's';
    }

    if (/^[0-9a-fA-F]{4,8}$/.test(candidate)) {
        const isBrands = explicitStyle === 'b';
        const cssClass  = isBrands ? 'bb-icon-unicode bb-icon-brands' : 'bb-icon-unicode';
        const charRef   = `&#x${candidate.toLowerCase()};`;
        return `<span class="${cssClass}">${charRef}</span>`;
    }

    let faClass;
    if (explicitStyle === 'b') {
        faClass = 'fa-brands';
    } else if (explicitStyle === 'r') {
        faClass = 'fa-regular';
    } else if (explicitStyle === 's') {
        faClass = 'fa-solid';
    } else if (window.FA_BRANDS_SET?.has(candidate)) {
        faClass = 'fa-brands';
    } else {
        faClass = 'fa-solid';
    }

    return `<i class="${faClass} fa-${esc(candidate)}"></i>`;
}

// ─── Construção de um pino ────────────────────────────────────────────────────

function buildPin(pin, mi, pi, dir) {
    const isInput = dir === 'input';

    // dot de conector: ◎ opcional  ◉ obrigatório  ⊙ ausente (erro)
    const sym      = pin.missingConn ? '⊙' : pin.connection === 'mandatory' ? '◉' : '◎';
    const dotTitle = pin.missingConn
        ? (isInput ? 'Entrada — connection: AUSENTE' : 'Saída — connection: AUSENTE')
        : pin.connection === 'mandatory'
            ? (isInput ? 'Entrada obrigatória ◉' : 'Saída obrigatória ◉')
            : (isInput ? 'Entrada opcional ◎'    : 'Saída opcional ◎');
    const dotStyle = pin.missingConn ? 'color:#C0392B' : '';
    const dot = `<span class="bb-dot ${isInput ? 'in' : 'out'}"
        title="${dotTitle}" style="${dotStyle}">${sym}</span>`;

    const badges = buildBadges(pin, mi, pi, dir);

    const addFlag = `<button class="bb-add-flag"
        onclick="bbAddFlag(${mi},'${dir}',${pi})" title="Adicionar flag">+ flag</button>`;

    const nameEl = `<span class="bb-pin-name" id="bb-pin-${mi}-${dir}-${pi}"
        contenteditable="false" title="Clique para renomear"
        onclick="bbStartRename(this)"
        onblur="bbFinishRename(this,${mi},'${dir}',${pi})"
        onkeydown="if(event.key==='Enter'||event.key==='Escape'){this.blur()}"
      >${esc(pin.name)}</span>`;

    const typeEl = `<span class="bb-pin-type">${esc(pin.type)}</span>`;

    const dragEl = `<span class="bb-drag" title="Arrastar para reordenar"
        draggable="false">⠿</span>`;

    const tooltip = buildTooltip(pin);

    const badgesArea = `<div class="bb-badges">${badges}${addFlag}</div>`;

    const inner = isInput
        ? `${dot}${dragEl}${nameEl}${typeEl}${badgesArea}`
        : `${badgesArea}${typeEl}${nameEl}${dragEl}${dot}`;

    return `<div class="bb-pin ${dir}"
  id="bb-row-${mi}-${dir}-${pi}"
  data-mi="${mi}" data-dir="${dir}" data-pi="${pi}"
  draggable="true"
  ondragstart="bbDragStart(event,${mi},'${dir}',${pi})"
  ondragover="bbDragOver(event)"
  ondragleave="bbDragLeave(event)"
  ondrop="bbDrop(event,${mi},'${dir}',${pi})"
  ondragend="bbDragEnd(event)"
>${tooltip}${inner}</div>`;
}

function buildBadges(pin, mi, dir, pi) {
    let b = '';

    if (pin.missingConn) {
        b += `<span class="bb-badge warn">⚠ connection: ausente</span>`;
    }
    if (pin.range) {
        b += `<span class="bb-badge range">${esc(pin.range)}</span>`;
    } else {
        if (pin.rangeMin) b += `<span class="bb-badge range">≥${esc(pin.rangeMin)}</span>`;
        if (pin.rangeMax) b += `<span class="bb-badge range">≤${esc(pin.rangeMax)}</span>`;
    }
    if (pin.unit) {
        b += `<span class="bb-badge unit">${esc(pin.unit)}</span>`;
    }
    if (pin.encoding) {
        b += `<span class="bb-badge encoding">${esc(pin.encoding)}</span>`;
    }
    (pin.flags || []).forEach((f, fi) => {
        b += `<span class="bb-badge flag">${esc(f)}
<button class="bb-flag-del" onclick="event.stopPropagation();bbRemoveFlag(${mi},'${dir}',${pi},${fi})" title="Remover">×</button></span>`;
    });
    return b;
}

function buildTooltip(pin) {
    const lines = [];

    if (pin.doc)      lines.push(`<strong>${esc(pin.doc)}</strong>`);
    if (pin.missingConn) lines.push(`<span style="color:#F5B7B1">⚠ connection: ausente — adicione connection:optional ou connection:mandatory</span>`);
    else if (pin.connection === 'mandatory') lines.push(`◉ Conexão <strong>obrigatória</strong>`);
    else if (pin.connection === 'optional')  lines.push(`◎ Conexão <strong>opcional</strong>`);
    if (pin.range)    lines.push(`📏 Intervalo: <code>${esc(pin.range)}</code>`);
    if (pin.rangeMin) lines.push(`📏 Mínimo: <code>${esc(pin.rangeMin)}</code>`);
    if (pin.rangeMax) lines.push(`📏 Máximo: <code>${esc(pin.rangeMax)}</code>`);
    if (pin.unit)     lines.push(`📐 Unidade: <code>${esc(pin.unit)}</code>`);
    if (pin.encoding) lines.push(`🔣 Encoding: <code>${esc(pin.encoding)}</code>`);
    if (pin.bits)     lines.push(`⚙️ Bits: <code>${esc(pin.bits)}</code>`);
    if (pin.default)  lines.push(`💡 Padrão: <code>${esc(pin.default)}</code>`);
    if (pin.options?.length) {
        lines.push(`🎛️ Valores: ${pin.options.map(o=>`<code>${esc(o)}</code>`).join(' | ')}`);
    }
    (pin.flags||[]).forEach(f => lines.push(`🏷️ flag: <code>${esc(f)}</code>`));

    if (!lines.length) return '';

    return `<div class="bb-tooltip">${lines.join('<br>')}</div>`;
}

// ─── Rename ───────────────────────────────────────────────────────────────────

export function bbStartRename(el) {
    el.contentEditable = 'true';
    el.focus();
    const range = document.createRange();
    range.selectNodeContents(el);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
}

export function bbFinishRename(el, mi, dir, pi) {
    el.contentEditable = 'false';
    const name = el.textContent.trim() || 'pin';
    el.textContent = name;
    if (!bbParsed) return;
    const method = bbParsed.methods[mi];
    if (!method) return;
    if (dir === 'input')  method.inputs[pi].name  = name;
    if (dir === 'output') method.outputs[pi].name = name;
}

// ─── Flags ────────────────────────────────────────────────────────────────────

export function bbRemoveFlag(mi, dir, pi, fi) {
    if (!bbParsed) return;
    const method = bbParsed.methods[mi];
    if (!method) return;
    const pins = dir === 'input' ? method.inputs : method.outputs;
    if (!pins[pi]?.flags) return;
    pins[pi].flags.splice(fi, 1);
    renderPreview();
}

export function bbAddFlag(mi, dir, pi) {
    const name = await showPrompt('Nome da flag:', '', { placeholder: 'optional_connection' });
    if (!name?.trim()) return;
    if (!bbParsed) return;
    const method = bbParsed.methods[mi];
    if (!method) return;
    const pins = dir === 'input' ? method.inputs : method.outputs;
    if (!pins[pi].flags) pins[pi].flags = [];
    pins[pi].flags.push(name.trim());
    renderPreview();
}

// ─── Drag-and-drop ────────────────────────────────────────────────────────────

export function bbDragStart(e, mi, dir, pi) {
    bbDragSrc = { mi, dir, pi };
    e.currentTarget.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
}
export function bbDragOver(e) {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    e.currentTarget.classList.add('drag-over');
}
export function bbDragLeave(e) {
    e.currentTarget.classList.remove('drag-over');
}
export function bbDrop(e, mi, dir, pi) {
    e.preventDefault();
    e.currentTarget.classList.remove('drag-over');
    if (!bbDragSrc || !bbParsed) return;
    if (bbDragSrc.mi !== mi || bbDragSrc.dir !== dir) return;
    if (bbDragSrc.pi === pi) return;
    const method = bbParsed.methods[mi];
    const pins   = dir === 'input' ? method.inputs : method.outputs;
    const item   = pins.splice(bbDragSrc.pi, 1)[0];
    pins.splice(pi, 0, item);
    bbDragSrc = null;
    renderPreview();
}
export function bbDragEnd(e) {
    e.currentTarget.classList.remove('dragging');
    bbDragSrc = null;
}

// ─── Formulário de metadados ──────────────────────────────────────────────────

// calcNextVersion busca o banco e retorna max(version) + 1 para o structName
// passado, ou 1 se nenhum registro existir.
async function calcNextVersion(structName) {
    try {
        const res  = await fetch('/api/blackbox');
        const json = await res.json();
        const items = (json?.data?.items || []).filter(b => b.structName === structName);
        if (!items.length) return 1;
        const max = Math.max(...items.map(b => Number(b.version) || 0));
        return max + 1;
    } catch {
        return 1; // Se não conseguir consultar, começa em 1
    }
}

// updateVersionLabel atualiza o badge read-only de versão no formulário.
function updateVersionLabel(version, isNew) {
    const label = document.getElementById('bb-version-label');
    if (!label) return;
    if (isNew) {
        label.innerHTML = `<strong style="color:var(--primary)">${version}</strong>
            <span style="color:var(--text-muted);font-size:12px">(novo componente)</span>`;
    } else {
        label.innerHTML = `<strong style="color:var(--primary)">${version}</strong>
            <span style="color:var(--text-muted);font-size:12px">(versão anterior + 1)</span>`;
    }
}

function showMetaForm() {
    const form = document.getElementById('bb-meta-form');
    if (!form) return;
    form.style.display = 'block';
    form.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    if (bbParsed) {
        const nameEl = document.getElementById('bb-f-name');
        if (nameEl && !nameEl.value) nameEl.value = bbParsed.displayName || bbParsed.structName || '';
        const docEl = document.getElementById('bb-f-doc');
        if (docEl) docEl.value = bbParsed.packageDoc || '';
        renderSettingsSection();

        // Calcula e exibe a próxima versão automaticamente
        const label = document.getElementById('bb-version-label');
        if (label) label.textContent = 'Calculando...';
        calcNextVersion(bbParsed.structName || '').then(next => {
            bbNextVersion = next;
            updateVersionLabel(next, next === 1);
        });
    }
}

function renderSettingsSection() {
    const section = document.getElementById('bb-settings-section');
    const list    = document.getElementById('bb-settings-list');
    if (!section || !list || !bbParsed?.settings?.length) {
        if (section) section.style.display = 'none';
        return;
    }
    section.style.display = 'block';
    list.innerHTML = `<table class="bb-settings-table">
<thead><tr><th>Campo</th><th>Label (painel Inspect)</th><th>Default</th><th>Opções</th></tr></thead>
<tbody>
${bbParsed.settings.map(s => `<tr>
  <td>${esc(s.field)}</td>
  <td>${esc(s.label)}</td>
  <td>${esc(s.default || '—')}</td>
  <td>${s.options?.length
        ? s.options.map(o=>`<code style="background:var(--bg-surface);padding:1px 5px;border-radius:3px">${esc(o)}</code>`).join(' ')
        : '—'}</td>
</tr>`).join('')}
</tbody></table>`;
}

export function bbUpdateMeta() {
    if (!bbParsed) return;
    bbParsed.displayName = document.getElementById('bb-f-name')?.value    || '';
    bbParsed.category    = document.getElementById('bb-f-cat')?.value     || '';
    bbParsed.packageDoc  = document.getElementById('bb-f-doc')?.value     || '';
    // author e version não vêm de campos de formulário:
    //   author  — extraído do código fonte pelo parser (tag "author:" no doc do pacote)
    //   version — calculado automaticamente via calcNextVersion()

    const newStructLabel = bbParsed.structLabel || bbParsed.displayName || bbParsed.structName || 'Device';
    document.querySelectorAll('.bb-block-hdr-label').forEach(el => {
        const current = el.textContent || '';
        const spaceIdx = current.indexOf(' ');
        if (spaceIdx > 0) {
            el.textContent = newStructLabel + current.slice(spaceIdx);
        }
    });
}

// ─── Salvar ───────────────────────────────────────────────────────────────────

function bbSlug(s) {
    return (s || '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'device';
}

export async function bbSave() {
    if (!bbParsed) { bbFormAlert('Faça o parse primeiro.', 'warning', 10_000); return; }
    if (bbNeedsExplicitParse) {
        bbFormAlert('O código foi alterado — clique em Parsear antes de salvar.', 'warning', 10_000);
        return;
    }
    const name = document.getElementById('bb-f-name')?.value?.trim();
    if (!name) { bbFormAlert('Informe o nome de exibição.', 'warning', 10_000); return; }

    bbUpdateMeta();

    // Garante que bbNextVersion é um inteiro positivo.
    // Se o cálculo ainda não terminou (improvável), recalcula sincronamente.
    let version = bbNextVersion;
    if (!version || version < 1) {
        version = await calcNextVersion(bbParsed.structName || '');
        bbNextVersion = version;
        updateVersionLabel(version, version === 1);
    }

    // ID: slug do structName + @ + versão inteira — ex: "my-device@7"
    const payload    = { ...bbParsed };
    payload.version  = version;
    payload.id       = bbSlug(bbParsed.structName || name) + '@' + version;

    const btn = document.querySelector('[onclick="bbSave()"]');
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Salvando...'; }

    try {
        const res  = await fetch('/api/blackbox/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        const json = await res.json();
        if (json?.metadata?.status !== 200) {
            bbFormAlert('✗ ' + (json?.metadata?.error || 'Erro ao salvar'), 'danger', 10_000); return;
        }
        bbSavedId = json.data?.id;
        bbFormAlert('', '');
        const st = document.getElementById('bb-save-status');
        if (st) { st.textContent = `✓ Salvo! (v${version})`; setTimeout(() => { st.textContent = ''; }, 3000); }

        // Após salvar, avança a versão para o próximo save
        bbNextVersion = version + 1;
        updateVersionLabel(bbNextVersion, false);

        await loadSavedList();
    } catch (e) {
        bbFormAlert('✗ Sem conexão', 'danger', 10_000);
    } finally {
        if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Salvar BlackBox'; }
    }
}

function bbFormAlert(msg, type, autoDismissMs = 0) {
    const el = document.getElementById('bb-form-alert');
    if (!el) return;

    clearTimeout(bbFormAlertTimer);
    bbFormAlertTimer = null;

    el.innerHTML = msg ? `<div class="alert alert-${type}">${msg}</div>` : '';

    if (msg && autoDismissMs > 0) {
        bbFormAlertTimer = setTimeout(() => {
            el.innerHTML = '';
            bbFormAlertTimer = null;
        }, autoDismissMs);
    }
}

export function bbReset() {
    bbParsed      = null;
    bbSavedId     = null;
    bbNextVersion = 1;
    const form = document.getElementById('bb-meta-form');
    if (form) form.style.display = 'none';
    const prev = document.getElementById('bb-tab-body-preview');
    if (prev) prev.innerHTML = `<div class="bb-empty-state">
      <i class="fa-solid fa-cube" style="font-size:40px;color:var(--border);display:block;margin-bottom:12px"></i>
      <p style="color:var(--text-muted)">O componente aparece aqui após o parse</p>
    </div>`;
    const dbg = document.getElementById('bb-tab-body-debug');
    if (dbg) dbg.innerHTML = '';
    document.getElementById('bb-version-bar').innerHTML = '';
    setParseStatus('', '');
    clearTimeout(bbAnalyzeTimer);
    bbAnalyzeSeq++;
    applyMonacoMarkers([]);
}

// ─── Version bar ─────────────────────────────────────────────────────────────

// Atualiza o dropdown de versões no header do editor com as versões salvas
// do mesmo componente (mesmo structName). Versão é inteiro — sort numérico.
async function renderVersionBar() {
    const bar = document.getElementById('bb-version-bar');
    if (!bar || !bbParsed?.structName) return;

    const res      = await fetch('/api/blackbox');
    const json     = await res.json();
    const slug     = bbSlug(bbParsed.structName);
    const versions = (json?.data?.items || []).filter(b =>
        b.id.startsWith(slug + '@') || b.structName === bbParsed.structName
    ).sort((a, b) => Number(b.version) - Number(a.version)); // inteiro desc

    if (versions.length < 1) { bar.innerHTML = ''; return; }

    bar._versions = versions;

    const opts = versions.map(b =>
        `<option value="${b.id}" ${b.id === bbSavedId ? 'selected' : ''}>v${esc(String(b.version))}</option>`
    ).join('');

    bar.innerHTML = `
<select class="bb-ver-select" id="bb-ver-select" onchange="bbVersionChange(this.value)"
        title="Versões salvas — trocar substitui o editor">${opts}</select>
<button class="btn btn-ghost btn-sm" onclick="bbDiffVersions()"
        title="Comparar editor atual com uma versão salva" style="font-size:11px">
  <i class="fa-solid fa-code-compare"></i> Diff
</button>`;
}

export function bbVersionChange(id) {
    const editorCode = getCode();
    const savedCode  = bbParsed?.sourceCode || '';
    if (editorCode !== savedCode) {
        const ok = await showConfirm(
            'O editor tem alterações não salvas.\n' +
            'Trocar de versão irá substituir o conteúdo atual.\n\n' +
            'Continuar?'
        );
        if (!ok) {
            const sel = document.getElementById('bb-ver-select');
            if (sel && bbSavedId) sel.value = bbSavedId;
            return;
        }
    }
    bbLoadSaved(id);
}

// ─── Diff engine ─────────────────────────────────────────────────────────────

function diffHunks(codeA, codeB) {
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
        if (i < n && j < m && lA[i] === lB[j]) {
            ops.push({type:'equal', a:lA[i], b:lB[j]}); i++; j++;
        } else if (j < m && (i >= n || dp[i][j+1] >= dp[i+1][j])) {
            ops.push({type:'insert', b:lB[j]}); j++;
        } else {
            ops.push({type:'delete', a:lA[i]}); i++;
        }
    }

    const hunks = [];
    let k = 0;
    while (k < ops.length) {
        if (ops[k].type === 'equal') {
            hunks.push({type:'equal', linesA:[ops[k].a], linesB:[ops[k].b]}); k++;
        } else {
            const hunk = {type:'change', linesA:[], linesB:[]};
            while (k < ops.length && ops[k].type !== 'equal') {
                if (ops[k].type === 'delete')  hunk.linesA.push(ops[k].a);
                if (ops[k].type === 'insert')  hunk.linesB.push(ops[k].b);
                k++;
            }
            hunks.push(hunk);
        }
    }

    const out = [];
    for (let h = 0; h < hunks.length; h++) {
        out.push(hunks[h]);
    }
    return out;
}

function renderDiffTable(hunks, choices) {
    let rows = '';
    let ci   = 0;

    hunks.forEach((hunk, hi) => {
        if (hunk.type === 'equal') {
            hunk.linesA.forEach(line => {
                const safe = esc(line) || ' ';
                rows += `<tr class="diff-row-eq">
  <td class="diff-cell-l">${safe}</td>
  <td class="diff-cell-m"></td>
  <td class="diff-cell-r" contenteditable="true">${safe}</td>
</tr>`;
            });
        } else {
            const choice = choices[ci];
            const lA     = hunk.linesA;
            const lB     = hunk.linesB;
            const picked = choice === 'left' ? lA : lB;
            const nRows  = Math.max(lA.length, lB.length);

            for (let r = 0; r < nRows; r++) {
                const leftTxt   = r < lA.length     ? lA[r]     : null;
                const pickedTxt = r < picked.length  ? picked[r] : null;

                const leftCell = leftTxt !== null
                    ? `<td class="diff-cell-l diff-bg-rem">- ${esc(leftTxt)}</td>`
                    : `<td class="diff-cell-l diff-bg-empty"> </td>`;

                const rightBg = pickedTxt !== null
                    ? (choice === 'right' ? 'diff-bg-add' : 'diff-bg-chosen')
                    : 'diff-bg-empty';
                const rightCell = `<td class="diff-cell-r ${rightBg}" contenteditable="true"
      data-ci="${ci}" data-r="${r}">${pickedTxt !== null ? esc(pickedTxt) : ' '}</td>`;

                const midCell = r === 0
                    ? `<td class="diff-cell-m" rowspan="${nRows}">
  <button class="diff-arrow ${choice==='left'  ? 'diff-arrow-active' : 'diff-arrow-off'}"
          onclick="diffChoose(${hi},${ci},'left')"  title="Usar versão salva">◀</button>
  <button class="diff-arrow ${choice==='right' ? 'diff-arrow-active' : 'diff-arrow-off'}"
          onclick="diffChoose(${hi},${ci},'right')" title="Manter editor">▶</button>
</td>`
                    : '';

                rows += `<tr>${leftCell}${midCell}${rightCell}</tr>`;
            }
            ci++;
        }
    });

    return `<table class="diff-table">
<colgroup><col class="diff-col-l"><col class="diff-col-m"><col class="diff-col-r"></colgroup>
<tbody>${rows}</tbody></table>`;
}

function applyChoices(hunks, choices) {
    const lines = [];
    let ci = 0;
    hunks.forEach(hunk => {
        if (hunk.type === 'equal') {
            lines.push(...hunk.linesA);
        } else {
            const picked = choices[ci] === 'left' ? hunk.linesA : hunk.linesB;
            lines.push(...picked);
            ci++;
        }
    });
    return lines.join('\n');
}

let _diffHunks    = [];
let _diffChoices  = [];
let _diffSavedCode = '';

export function diffChoose(hi, ci, side) {
    _diffChoices[ci] = side;
    refreshDiffTable();
}

function refreshDiffTable() {
    const wrap = document.getElementById('diff-table-wrap');
    if (!wrap) return;
    wrap.innerHTML = renderDiffTable(_diffHunks, _diffChoices);
    wrap.querySelectorAll('[contenteditable]').forEach(el => {
        el.textContent = el.textContent.replace(/\u00A0/g, ' ');
    });
}

function collectDiffResult() {
    const cells = document.querySelectorAll('#diff-table-wrap .diff-cell-r');
    if (!cells.length) return applyChoices(_diffHunks, _diffChoices);
    return Array.from(cells).map(c => c.textContent.replace(/\u00A0/g, ' ')).join('\n');
}

export async function bbDiffVersions() {
    if (!bbParsed) return;

    const res      = await fetch('/api/blackbox');
    const json     = await res.json();
    const slug     = bbSlug(bbParsed.structName || '');
    const versions = (json?.data?.items || []).filter(b =>
        b.id.startsWith(slug + '@') || b.structName === bbParsed.structName
    ).sort((a, b) => Number(b.version) - Number(a.version)); // inteiro desc

    if (!versions.length) { await showAlert('warning', 'Nenhuma versão salva para comparar.'); return; }

    const codeMap    = {};
    versions.forEach(v => { codeMap[v.id] = v.sourceCode || ''; });
    window._diffCodeMap = codeMap;
    const editorCode = getCode();
    const firstId    = versions[0].id;
    const opts       = versions.map(v =>
        `<option value="${v.id}">v${esc(String(v.version))}</option>`
    ).join('');

    function initDiff(savedCode) {
        _diffSavedCode = savedCode;
        _diffHunks   = diffHunks(savedCode, getCode());
        _diffChoices = _diffHunks.filter(h => h.type !== 'equal').map(() => 'right');
    }
    initDiff(codeMap[firstId]);

    document.getElementById('bb-json-modal')?.remove();
    const modal = document.createElement('div');
    modal.id    = 'bb-json-modal';
    modal.style.cssText =
        'position:fixed;inset:0;background:rgba(0,0,0,.6);' +
        'z-index:1000;display:flex;align-items:center;justify-content:center;padding:12px 8px';

    modal.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);
            box-shadow:0 8px 40px rgba(0,0,0,.35);
            width:calc(100vw - 16px);height:92vh;
            display:flex;flex-direction:column;overflow:hidden">

  <!-- header -->
  <div style="display:flex;align-items:center;gap:10px;padding:10px 16px;
              border-bottom:1px solid var(--border);background:var(--bg-surface);
              flex-wrap:wrap;flex-shrink:0">
    <i class="fa-solid fa-code-compare" style="color:var(--primary)"></i>
    <strong style="font-size:14px">Diff</strong>
    <select id="diff-ver-sel" class="bb-ver-select">${opts}</select>
    <span style="font-size:12px;color:var(--text-muted)">
      ◀ usar salvo &nbsp;|&nbsp; ▶ manter editor &nbsp;|&nbsp; edite direto à direita
    </span>
    <div style="margin-left:auto;display:flex;gap:8px;align-items:center">
      <button class="btn btn-primary btn-sm" onclick="diffApplyToEditor()"
              title="Envia o resultado para o editor Código Go">
        <i class="fa-solid fa-arrow-right-to-bracket"></i> Enviar para o editor
      </button>
      <button onclick="document.getElementById('bb-json-modal').remove()"
              style="background:none;border:none;cursor:pointer;font-size:22px;
                     color:var(--text-muted);line-height:1" title="Fechar">×</button>
    </div>
  </div>

  <!-- column headers -->
  <div style="display:flex;background:var(--bg-surface);border-bottom:2px solid var(--border);
              font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.05em;
              color:var(--text-muted);flex-shrink:0">
    <div style="flex:1;padding:5px 10px;border-right:1px solid var(--border)">
      <i class="fa-solid fa-floppy-disk" style="margin-right:4px;color:var(--danger)"></i>Versão salva
    </div>
    <div style="width:44px;flex-shrink:0;text-align:center;padding:5px 0">◀▶</div>
    <div style="flex:1;padding:5px 10px;border-left:1px solid var(--border)">
      <i class="fa-brands fa-golang" style="margin-right:4px;color:#00ADD8"></i>Resultado
      <span style="font-size:10px;font-weight:400;color:var(--success)">editável</span>
    </div>
  </div>

  <!-- unified diff table -->
  <div id="diff-table-wrap"
       style="overflow:auto;flex:1;background:var(--bg-card)">
    ${renderDiffTable(_diffHunks, _diffChoices)}
  </div>
</div>`;

    modal.querySelector('#diff-ver-sel').addEventListener('change', function() {
        _diffSavedCode = codeMap[this.value] || '';
        initDiff(_diffSavedCode);
        refreshDiffTable();
    });

    let _monacoSub = null;
    if (monacoInst) {
        _monacoSub = monacoInst.onDidChangeModelContent(() => {
            if (!document.getElementById('bb-json-modal')) return;
            initDiff(_diffSavedCode);
            refreshDiffTable();
        });
    } else {
        const ta = document.getElementById('bb-fallback');
        if (ta) ta.addEventListener('input', function _autoD() {
            if (!document.getElementById('bb-json-modal')) {
                ta.removeEventListener('input', _autoD); return;
            }
            initDiff(_diffSavedCode);
            refreshDiffTable();
        });
    }

    modal.addEventListener('click', e => {
        if (e.target === modal) {
            modal.remove();
            _monacoSub?.dispose();
            window._diffCodeMap = null;
        }
    });
    modal.querySelector('[title="Fechar"]').addEventListener('click', () => {
        _monacoSub?.dispose();
    });

    document.body.appendChild(modal);
}

export function diffApplyToEditor() {
    const result = collectDiffResult().replace(/\u00A0/g, ' ');
    if (monacoInst) monacoInst.setValue(result);
    else { const fallback = document.getElementById('bb-fallback'); if (fallback) fallback.value = result; }
    document.getElementById('bb-json-modal')?.remove();
    setParseStatus('⚠ Código atualizado pelo diff — clique Parsear para validar.', 'warning');
}

// ─── Lista de salvos ──────────────────────────────────────────────────────────

export async function loadSavedList() {
    const el = document.getElementById('bb-saved-list');
    if (!el) return;
    el.innerHTML = '<div class="lspinner"><div class="spinner"></div></div>';
    try {
        const res   = await fetch('/api/blackbox');

        const ct = res.headers.get('content-type') || '';
        if (!ct.includes('application/json')) {
            throw new Error(`Servidor retornou ${res.status} (${res.statusText}) sem JSON`);
        }

        const json  = await res.json();

        if (json?.metadata?.status && json.metadata.status >= 400) {
            throw new Error(json.metadata.error || `Erro ${json.metadata.status}`);
        }

        // O servidor já retorna uma linha por struct_name (ListLatestBlackBoxes).
        // Agrupamos apenas para contar versões alternativas, se houver.
        const items = json?.data?.items || [];
        if (!items.length) {
            el.innerHTML = `<div style="color:var(--text-muted);font-size:14px;padding:24px;text-align:center">
              <i class="fa-solid fa-inbox" style="font-size:28px;display:block;margin-bottom:8px"></i>
              Nenhum componente salvo ainda.
            </div>`;
            return;
        }

        el.innerHTML = '<div class="bb-card-list">' + items.map(bb => {
            const safeId  = esc(JSON.stringify(bb.id  || ''));
            const safeKey = esc(JSON.stringify(bb.structName || bb.displayName || bb.id || ''));
            const doc     = bb.packageDoc || '';
            const ver     = Number(bb.version) || 1;
            return `
<div class="bb-saved-card">
  <h3>${esc(bb.displayName || bb.structName || '(sem nome)')}</h3>
  <div class="bb-saved-meta">
    ${bb.category ? `<span class="tag">${esc(bb.category)}</span>` : ''}
    ${bb.author   ? `<span class="tag"><i class="fa-solid fa-user"></i> ${esc(bb.author)}</span>` : ''}
    <span class="tag">v${ver}</span>
    <span class="tag">${(bb.methods||[]).length} método(s)</span>
    ${(bb.settings||[]).length ? `<span class="tag">${bb.settings.length} setting(s)</span>` : ''}
  </div>
  ${doc ? `<p style="font-size:12px;color:var(--text-muted);line-height:1.5">${esc(doc.slice(0,120))}${doc.length>120?'…':''}</p>` : ''}
  <div class="bb-saved-actions">
    <button class="btn btn-secondary btn-sm" onclick="bbLoadSaved(${safeId})">
      <i class="fa-solid fa-pen-to-square"></i> Editar
    </button>
    <button class="btn btn-ghost btn-sm" style="color:var(--danger)"
            onclick="bbDeleteAll(${safeKey})"
            title="Excluir todas as versões">
      <i class="fa-solid fa-trash"></i>
    </button>
  </div>
</div>`;
        }).join('') + '</div>';

    } catch (e) {
        console.error('[loadSavedList]', e);
        el.innerHTML = `<div class="alert alert-danger" style="display:flex;flex-direction:column;gap:6px">
          <strong>✗ Erro ao carregar lista</strong>
          <span style="font-size:12px;opacity:.8">${esc(e?.message || String(e))}</span>
          <button class="btn btn-sm btn-ghost" style="margin-top:4px;align-self:flex-start"
                  onclick="loadSavedList()">
            <i class="fa-solid fa-rotate-right"></i> Tentar novamente
          </button>
        </div>`;
    }
}

export async function bbDelete(id) {
    await fetch(`/api/blackbox/${id}`, { method: 'DELETE' });
}

export async function bbDeleteAll(structNameOrSlug) {
    if (!await showConfirm(`Excluir todas as versões de "${structNameOrSlug}"?`, { okLabel: 'Excluir', danger: true })) return;
    const res   = await fetch('/api/blackbox');
    const json  = await res.json();
    const items = (json?.data?.items || []).filter(b =>
        b.structName === structNameOrSlug ||
        b.id.startsWith(bbSlug(structNameOrSlug) + '@')
    );
    await Promise.all(items.map(b => bbDelete(b.id)));
    await loadSavedList();
}

export async function bbLoadSaved(id) {
    const res  = await fetch('/api/blackbox');
    const json = await res.json();
    const bb   = (json?.data?.items || []).find(i => i.id === id);
    if (!bb) return;

    const code = bb.sourceCode || '';
    if (monacoInst) monacoInst.setValue(code);
    else { const ta = document.getElementById('bb-fallback'); if (ta) ta.value = code; }

    bbParsed  = { ...bb };
    bbSavedId = bb.id;

    // Próxima versão = versão atual + 1
    const currentVer = Number(bb.version) || 1;
    bbNextVersion = currentVer + 1;

    renderPreview();
    showMetaForm();
    document.getElementById('bb-f-name').value = bb.displayName || '';
    document.getElementById('bb-f-cat').value  = bb.category    || '';
    document.getElementById('bb-f-doc').value  = bb.packageDoc  || '';
    updateVersionLabel(bbNextVersion, false);
    setParseStatus(
        `Carregado: ${esc(bb.displayName || bb.structName)} v${currentVer} — clique Parsear para atualizar`,
        'ok'
    );
    window.scrollTo({ top: 0, behavior: 'smooth' });
}

export function bbViewJSON(id) {
    fetch('/api/blackbox').then(r => r.json()).then(json => {
        const bb = (json?.data?.items||[]).find(i => i.id === id);
        if (!bb) return;

        document.getElementById('bb-json-modal')?.remove();

        const modal = document.createElement('div');
        modal.id = 'bb-json-modal';
        modal.style.cssText = `
            position:fixed;inset:0;background:rgba(0,0,0,.55);
            z-index:1000;display:flex;align-items:center;justify-content:center;padding:24px`;
        modal.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);box-shadow:0 8px 40px rgba(0,0,0,.3);
            width:100%;max-width:720px;max-height:80vh;display:flex;flex-direction:column;overflow:hidden">
  <div style="display:flex;align-items:center;gap:10px;padding:14px 18px;
              border-bottom:1px solid var(--border);background:var(--bg-surface)">
    <i class="fa-solid fa-code" style="color:var(--primary)"></i>
    <strong style="font-size:14px">${esc(bb.displayName || bb.structName)} — JSON</strong>
    <button onclick="document.getElementById('bb-json-modal').remove()"
            style="margin-left:auto;background:none;border:none;cursor:pointer;
                   font-size:20px;color:var(--text-muted);line-height:1" title="Fechar">×</button>
  </div>
  <pre style="margin:0;padding:18px;overflow:auto;font-family:var(--mono);font-size:12px;
              line-height:1.6;color:var(--text-primary);background:var(--bg-surface);flex:1">${
            esc(JSON.stringify(bb, null, 2))
        }</pre>
</div>`;
        modal.addEventListener('click', e => { if (e.target === modal) modal.remove(); });
        document.body.appendChild(modal);
    });
}

// ─── Manual / Help ────────────────────────────────────────────────────────────

export async function bbOpenHelp() {
    document.getElementById('bb-help-modal')?.remove();

    const lang  = (navigator.language || '').toLowerCase();
    const base  = lang.split('-')[0];
    const alt   = lang.replace('-', '_');

    const userCandidates = [
        `/help/backend/blackBox/${lang}.md`,
        `/help/backend/blackBox/${alt}.md`,
        `/help/backend/blackBox/${base}.md`,
    ].filter((v, i, a) => v && a.indexOf(v) === i);

    let mdText = null;
    let loadedLang = null;

    for (const url of userCandidates) {
        try {
            const res = await fetch(url);
            if (res.ok) { mdText = await res.text(); loadedLang = lang; break; }
        } catch { /* continua */ }
    }

    if (!mdText && base !== 'en') {
        try {
            const res = await fetch('/help/backend/blackBox/en.md');
            if (res.ok) { mdText = await res.text(); loadedLang = 'en'; }
        } catch { /* */ }
    }

    if (!mdText) {
        await showAlert('warning', 'Manual não encontrado. Coloque os arquivos de ajuda em static/help/<idioma>.md');
        return;
    }

    const modal = document.createElement('div');
    modal.id    = 'bb-help-modal';
    modal.style.cssText =
        'position:fixed;inset:0;background:rgba(0,0,0,.55);' +
        'z-index:1000;display:flex;align-items:center;justify-content:center;padding:12px 8px';

    modal.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);
            box-shadow:0 8px 40px rgba(0,0,0,.35);
            width:calc(100vw - 16px);height:92vh;
            display:flex;flex-direction:column;overflow:hidden">

  <!-- header -->
  <div style="display:flex;align-items:center;gap:10px;padding:10px 16px;
              border-bottom:1px solid var(--border);background:var(--bg-surface);
              flex-shrink:0">
    <i class="fa-solid fa-circle-question" style="color:var(--primary)"></i>
    <strong style="font-size:14px">Manual — BlackBox Creator</strong>
    <code style="font-size:11px;color:var(--text-muted);margin-left:4px">${esc(loadedLang || 'en')}</code>
    <button onclick="document.getElementById('bb-help-modal').remove()"
            style="background:none;border:none;cursor:pointer;font-size:22px;
                   color:var(--text-muted);line-height:1;margin-left:auto" title="Fechar">×</button>
  </div>

  <!-- Markdown rendered as HTML -->
  <div id="bb-help-body" style="flex:1;min-height:0;overflow-y:auto;padding:24px 32px;
       font-size:15px;line-height:1.75;color:var(--text)">
    <div id="bb-help-content" style="max-width:860px;margin:0 auto"></div>
  </div>
</div>`;

    modal.addEventListener('click', e => { if (e.target === modal) modal.remove(); });
    const escHandler = e => { if (e.key === 'Escape') { modal.remove(); document.removeEventListener('keydown', escHandler); } };
    document.addEventListener('keydown', escHandler);

    document.body.appendChild(modal);

    if (!document.getElementById('bb-help-css')) {
        const st = document.createElement('style');
        st.id = 'bb-help-css';
        st.textContent = `
#bb-help-content h1{font-size:22px;font-weight:800;color:var(--primary);margin:0 0 20px}
#bb-help-content h2{font-size:16px;font-weight:700;color:var(--text);margin:28px 0 10px;
  padding-bottom:6px;border-bottom:1px solid var(--border)}
#bb-help-content h3{font-size:14px;font-weight:700;margin:20px 0 8px;color:var(--text)}
#bb-help-content p{margin:0 0 12px;color:var(--text-secondary,#4B5E7A)}
#bb-help-content ul,#bb-help-content ol{padding-left:22px;margin:0 0 12px}
#bb-help-content li{margin-bottom:4px;color:var(--text-secondary,#4B5E7A)}
#bb-help-content code{font-family:'Fira Mono','Consolas',monospace;font-size:12px;
  background:var(--bg-surface,#f0f4fb);padding:2px 6px;border-radius:4px;
  color:var(--primary,#2255AA)}
#bb-help-content pre{background:var(--bg-surface,#f0f4fb);border:1px solid var(--border);
  border-radius:8px;padding:16px;overflow-x:auto;margin:0 0 16px}
#bb-help-content pre code{background:none;padding:0;font-size:13px;color:var(--text)}
#bb-help-content table{border-collapse:collapse;width:100%;margin:0 0 16px;font-size:13px}
#bb-help-content th{background:var(--bg-surface,#f0f4fb);font-weight:700;
  padding:8px 12px;text-align:left;border:1px solid var(--border)}
#bb-help-content td{padding:7px 12px;border:1px solid var(--border);
  color:var(--text-secondary,#4B5E7A);vertical-align:top}
#bb-help-content tr:nth-child(even) td{background:var(--bg-alt,#f7f9fc)}
#bb-help-content hr{border:none;border-top:1px solid var(--border);margin:24px 0}
#bb-help-content strong{font-weight:700;color:var(--text)}
#bb-help-content a{color:var(--primary);text-decoration:underline}
`;
        document.head.appendChild(st);
    }

    function renderHelp() {
        const content = document.getElementById('bb-help-content');
        if (!content) return;
        content.innerHTML = mdToHtml(mdText);
    }
    renderHelp();
}

function mdToHtml(md) {
    const esc = s => s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');

    const lines  = md.split('\n');
    let html     = '';
    let inPre    = false;
    let preBuf   = '';
    let inTable  = false;
    let inList   = false;

    const flushList  = () => { if (inList)  { html += '</ul>';           inList  = false; } };
    const flushTable = () => { if (inTable) { html += '</tbody></table>'; inTable = false; } };

    const inline = s => s
        .replace(/`([^`]+)`/g,   (_,c) => `<code>${esc(c)}</code>`)
        .replace(/\*\*([^*]+)\*\*/g, (_,c) => `<strong>${c}</strong>`)
        .replace(/\*([^*]+)\*/g,     (_,c) => `<em>${c}</em>`)
        .replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_,t,u) => `<a href="${esc(u)}">${t}</a>`);

    for (let i = 0; i < lines.length; i++) {
        const raw = lines[i];

        if (raw.startsWith('```')) {
            if (!inPre) {
                flushList(); flushTable();
                inPre = true; preBuf = '';
            } else {
                html += `<pre><code>${esc(preBuf.replace(/\n$/,''))}</code></pre>`;
                inPre = false;
            }
            continue;
        }
        if (inPre) { preBuf += raw + '\n'; continue; }

        if (/^---+$/.test(raw.trim())) {
            flushList(); flushTable();
            html += '<hr>'; continue;
        }

        const hm = raw.match(/^(#{1,3})\s+(.*)/);
        if (hm) {
            flushList(); flushTable();
            const lvl = hm[1].length;
            html += `<h${lvl}>${inline(esc(hm[2]))}</h${lvl}>`;
            continue;
        }

        if (raw.startsWith('|')) {
            flushList();
            const cells = raw.split('|').slice(1,-1).map(c => c.trim());
            const isSep = cells.every(c => /^[-:]+$/.test(c));
            if (isSep) continue;
            if (!inTable) {
                html += '<table><thead><tr>' +
                    cells.map(c => `<th>${inline(esc(c))}</th>`).join('') +
                    '</tr></thead><tbody>';
                inTable = true;
            } else {
                html += '<tr>' + cells.map(c => `<td>${inline(esc(c))}</td>`).join('') + '</tr>';
            }
            continue;
        }
        if (inTable) flushTable();

        const lm = raw.match(/^[-*]\s+(.*)/);
        if (lm) {
            if (!inList) { html += '<ul>'; inList = true; }
            html += `<li>${inline(esc(lm[1]))}</li>`;
            continue;
        }
        flushList();

        if (raw.trim() === '') { html += ''; continue; }

        html += `<p>${inline(esc(raw))}</p>`;
    }

    flushList(); flushTable();
    if (inPre) html += `<pre><code>${esc(preBuf)}</code></pre>`;

    return html;
}

// ─── Test-only exports ────────────────────────────────────────────────────────
export {
    esc,
    bbSlug,
    formatDiagMsg,
    formatAnalyzeErrors,
    diffHunks,
    setParseStatus,
    bbFormAlert,
    applyMonacoMarkers,
    bbRunAnalyze,
    bbAutoReparse,
    getTestState,
};

function getTestState() {
    return {
        bbParsed,
        bbSavedId,
        bbNextVersion,
        monacoInst,
        bbAnalyzeSeq,
        bbParseStatusType,
        bbNeedsExplicitParse,
        BB_REALTIME_ANALYSIS,
    };
}
