// server/public/control/js/pages/categories.js — Category & subcategory management.
//
// Tree of categories with expandable subcategories. Each entry has name,
// sort order, and optional FontAwesome icon for the IDE menu.
// All writes require OTP via POST /sections/request-otp.

import { cpApi }              from '../api.js';
import { cpToast, cpConfirm } from '../toast.js';
import { FA_FAVORITES, FA_ALL } from '../icons.js';

const API = '/api/control/v1';


let _categories = [];
let _otpCallback = null;

// ── OTP ───────────────────────────────────────────────────────────────────────

async function withOTP(actionFn, onSuccess) {
    const otpR = await cpApi('POST', `${API}/sections/request-otp`);
    if (otpR?.metadata?.status !== 200) {
        cpToast('danger', otpR?.metadata?.error || 'Erro ao solicitar código.');
        return;
    }
    showOTPModal(async (code) => {
        const r = await actionFn(code);
        const s = r?.metadata?.status;
        if (s === 200 || s === 201) { hideOTPModal(); if (onSuccess) await onSuccess(r); return true; }
        if (s === 401) return r?.metadata?.error || 'Código inválido ou expirado.';
        hideOTPModal(); cpToast('danger', r?.metadata?.error || 'Erro.'); return true;
    });
}

function showOTPModal(cb) {
    _otpCallback = cb; removeEl('cp-otp-modal');
    const b = document.createElement('div'); b.id = 'cp-otp-modal'; b.className = 'cp-modal-backdrop';
    b.innerHTML = `<div class="cp-modal" style="max-width:400px">
      <h3><i class="fa-solid fa-envelope-open-text" style="margin-right:8px"></i>Confirmar alteração</h3>
      <p style="margin-top:8px;font-size:13px;color:var(--text-dim)">Código enviado ao seu e-mail. Válido por 15 min.</p>
      <div class="cp-field" style="margin-top:16px"><span>Código</span>
        <input class="cp-input" id="cp-otp-input" type="text" inputmode="numeric" maxlength="6"
               placeholder="000000" style="font-size:22px;letter-spacing:8px;text-align:center"
               oninput="cpCatOtpIn()"></div>
      <div id="cp-otp-error" style="display:none;margin-top:8px;padding:8px 12px;border-radius:var(--r);background:var(--danger-bg);color:var(--danger);font-size:13px"></div>
      <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
        <button class="cp-btn cp-btn-ghost" onclick="cpCatOtpX()">Cancelar</button>
        <button class="cp-btn cp-btn-primary" id="cp-otp-confirm" onclick="cpCatOtpOk()" disabled><i class="fa-solid fa-check"></i> Confirmar</button>
      </div></div>`;
    document.body.appendChild(b);
    b.addEventListener('click', e => { if (e.target === b) hideOTPModal(); });
    setTimeout(() => document.getElementById('cp-otp-input')?.focus(), 80);
}
function hideOTPModal() { _otpCallback = null; removeEl('cp-otp-modal'); }
window.cpCatOtpIn = () => { const v = document.getElementById('cp-otp-input')?.value||''; const btn = document.getElementById('cp-otp-confirm'); if(btn) btn.disabled = v.replace(/\D/g,'').length !== 6; const e = document.getElementById('cp-otp-error'); if(e) e.style.display='none'; };
window.cpCatOtpX = () => hideOTPModal();
window.cpCatOtpOk = async () => {
    if (!_otpCallback) return;
    const input = document.getElementById('cp-otp-input'), btn = document.getElementById('cp-otp-confirm'), errEl = document.getElementById('cp-otp-error');
    const code = input?.value?.trim()||''; if (code.length!==6) return;
    if(btn){btn.disabled=true;btn.innerHTML='<i class="fa-solid fa-spinner fa-spin"></i>';}
    const result = await _otpCallback(code);
    if(btn){btn.disabled=false;btn.innerHTML='<i class="fa-solid fa-check"></i> Confirmar';}
    if(typeof result==='string'){if(errEl){errEl.textContent=result;errEl.style.display='block';}if(input){input.value='';input.focus();}if(btn)btn.disabled=true;}
};

// ── Entry point ───────────────────────────────────────────────────────────────

export async function renderCategories(root) {
    setBreadcrumb('Categorias & Subcategorias');
    root.innerHTML = `
<div class="cp-page-title"><i class="fa-solid fa-folder-tree"></i> Categorias do menu</div>
<div class="cp-card">
  <div class="cp-card-header">
    <span>Categorias e subcategorias disponíveis na IDE</span>
    <button class="cp-btn cp-btn-primary" onclick="cpCatNew()"><i class="fa-solid fa-plus"></i> Nova categoria</button>
  </div>
  <div class="cp-card-body" style="padding:0"><div id="cp-cat-body"><div class="cp-spinner"></div></div></div>
</div>`;
    await loadAndRender();
}

async function loadAndRender() {
    const r = await cpApi('GET', `${API}/categories`);
    if (r?.metadata?.status !== 200) { cpToast('danger', r?.metadata?.error || 'Erro'); return; }
    _categories = r.data?.categories || [];
    renderTree();
}

// ── Tree view ─────────────────────────────────────────────────────────────────

function renderTree() {
    const body = document.getElementById('cp-cat-body');
    if (!body) return;
    if (_categories.length === 0) {
        body.innerHTML = `<div class="cp-empty"><i class="fa-solid fa-folder-tree"></i> Nenhuma categoria.</div>`;
        return;
    }

    const rows = _categories.map(cat => {
        const icon = cat.iconFa
            ? `<i class="fa-solid fa-${esc(cat.iconFa)}" style="width:16px;text-align:center;color:var(--primary)"></i>`
            : `<i class="fa-solid fa-folder" style="width:16px;text-align:center;color:var(--text-muted)"></i>`;
        const sc = (cat.subcategories||[]).length;
        let subRows = (cat.subcategories||[]).map(sub => {
            const si = sub.iconFa
                ? `<i class="fa-solid fa-${esc(sub.iconFa)}" style="width:14px;text-align:center;color:var(--primary)"></i>`
                : `<i class="fa-solid fa-cube" style="width:14px;text-align:center;color:var(--text-muted)"></i>`;
            return `<tr style="background:var(--bg-input)">
  <td style="padding-left:48px;font-size:13px"><div style="display:flex;align-items:center;gap:8px">
    ${si} <span>${esc(sub.name)}</span>
    <span style="color:var(--text-muted);font-size:11px">#${sub.sortOrder}</span>
    ${sub.iconFa?`<code style="font-size:10px;color:var(--text-dim);background:var(--bg-overlay);padding:1px 6px;border-radius:3px">${esc(sub.iconFa)}</code>`:''}
  </div></td>
  <td style="text-align:right;white-space:nowrap">
    <button class="cp-btn cp-btn-ghost" style="padding:3px 8px;font-size:11px" onclick="cpSubEdit('${esc(cat.id)}','${esc(sub.id)}')"><i class="fa-solid fa-pen"></i></button>
    <button class="cp-btn cp-btn-danger" style="padding:3px 8px;font-size:11px" onclick="cpSubDelete('${esc(cat.id)}','${esc(sub.id)}','${esc(sub.name)}')"><i class="fa-solid fa-xmark"></i></button>
  </td></tr>`;
        }).join('');
        return `<tr>
  <td><div style="display:flex;align-items:center;gap:10px">${icon}
    <div><div style="font-weight:600;font-size:14px">${esc(cat.name)}</div>
    <div style="font-size:11px;color:var(--text-muted)">Ordem: ${cat.sortOrder} · ${sc} sub${sc!==1?'s':''}${cat.iconFa?` · <code style="font-size:10px">${esc(cat.iconFa)}</code>`:''}</div>
  </div></div></td>
  <td style="text-align:right;white-space:nowrap">
    <button class="cp-btn cp-btn-ghost" style="padding:3px 8px;font-size:11px" onclick="cpSubNew('${esc(cat.id)}')"><i class="fa-solid fa-plus"></i> Sub</button>
    <button class="cp-btn cp-btn-ghost" style="padding:3px 8px;font-size:11px" onclick="cpCatEdit('${esc(cat.id)}')"><i class="fa-solid fa-pen"></i></button>
    <button class="cp-btn cp-btn-danger" style="padding:3px 8px;font-size:11px" onclick="cpCatDelete('${esc(cat.id)}','${esc(cat.name)}')"><i class="fa-solid fa-trash-can"></i></button>
  </td></tr>${subRows}`;
    }).join('');

    body.innerHTML = `<div class="cp-table-wrap"><table class="cp-table">
      <thead><tr><th>Nome</th><th style="width:160px;text-align:right">Ações</th></tr></thead>
      <tbody>${rows}</tbody></table></div>`;
}

// ── Edit modal ────────────────────────────────────────────────────────────────

function openEditModal(opts) {
    removeEl('cp-cat-modal');
    const b = document.createElement('div'); b.id='cp-cat-modal'; b.className='cp-modal-backdrop';
    b.innerHTML = `
<div class="cp-modal" style="max-width:520px;width:100%;max-height:85vh;overflow-y:auto">
  <h3><i class="fa-solid fa-pen" style="margin-right:8px"></i>${opts.title}</h3>
  <div style="display:grid;grid-template-columns:1fr 100px;gap:12px;margin-top:16px">
    <label class="cp-field"><span>Nome</span>
      <input class="cp-input" id="catm-name" value="${ea(opts.name)}" placeholder="Ex: Sensors"></label>
    <label class="cp-field"><span>Ordem</span>
      <input class="cp-input" id="catm-order" type="number" min="0" value="${opts.sortOrder}" style="text-align:center"></label>
  </div>
  <div style="margin-top:16px">
    <div style="font-size:13px;font-weight:600;margin-bottom:6px;color:var(--text-dim)">
      <i class="fa-solid fa-icons"></i> Ícone
      <span style="font-weight:400;font-size:11px;color:var(--text-muted);margin-left:8px">Opcional — vazio usa ícone padrão</span>
    </div>
    <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
      <div style="width:40px;height:40px;border-radius:8px;background:var(--bg-overlay);display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="fa-solid fa-${esc(opts.iconFa||'folder')}" id="catm-icon-i" style="color:var(--primary);font-size:18px"></i>
      </div>
      <input class="cp-input" id="catm-icon" value="${ea(opts.iconFa)}" placeholder="vazio = padrão" oninput="cpCatIconChg()" style="flex:1;font-size:13px">
      <button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:11px" onclick="cpCatIconClr()"><i class="fa-solid fa-xmark"></i> Limpar</button>
    </div>
    <input class="cp-input" id="catm-icon-search" placeholder="Buscar ícone…" oninput="cpCatIconFlt()" style="margin-bottom:6px;font-size:12px">
    <div id="catm-icon-grid" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(38px,1fr));gap:3px;max-height:140px;overflow-y:auto;padding:4px;background:var(--bg-input);border-radius:var(--r);border:1px solid var(--border)">${iconGrid(opts.iconFa)}</div>
  </div>
  <div style="display:flex;gap:8px;margin-top:20px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="cpCatModalX()">Cancelar</button>
    <button class="cp-btn cp-btn-primary" onclick="cpCatModalSave()"><i class="fa-solid fa-check"></i> Salvar</button>
  </div>
</div>`;
    document.body.appendChild(b);
    b.addEventListener('click', e => { if(e.target===b) removeEl('cp-cat-modal'); });
    b._onSave = opts.onSave;
    setTimeout(()=>document.getElementById('catm-name')?.focus(),80);
}

window.cpCatIconChg = () => { const v=document.getElementById('catm-icon')?.value||'folder';const i=document.getElementById('catm-icon-i');if(i)i.className=`fa-solid fa-${v}`;const g=document.getElementById('catm-icon-grid');if(g)g.innerHTML=iconGrid(v,document.getElementById('catm-icon-search')?.value); };
window.cpCatIconClr = () => { const i=document.getElementById('catm-icon');if(i)i.value='';window.cpCatIconChg(); };
window.cpCatIconFlt = () => { const g=document.getElementById('catm-icon-grid');if(g)g.innerHTML=iconGrid(document.getElementById('catm-icon')?.value,document.getElementById('catm-icon-search')?.value); };
window.cpCatIconPick = (n) => { const i=document.getElementById('catm-icon');if(i)i.value=n;window.cpCatIconChg(); };
window.cpCatModalX = () => removeEl('cp-cat-modal');
window.cpCatModalSave = () => {
    const modal = document.getElementById('cp-cat-modal'); if(!modal?._onSave)return;
    const name=document.getElementById('catm-name')?.value?.trim()||'';
    const order=parseInt(document.getElementById('catm-order')?.value)||0;
    const icon=document.getElementById('catm-icon')?.value?.trim()||'';
    if(!name){cpToast('warning','Nome é obrigatório.');return;}
    modal._onSave(name,order,icon);
};

function iconGrid(sel, filter) {
    const lf=(filter||'').toLowerCase();
    let icons, label;
    if (lf) {
        icons = FA_ALL.filter(i => i.includes(lf));
        const total = icons.length;
        if (icons.length > 120) icons = icons.slice(0, 120);
        label = `<div style="grid-column:1/-1;font-size:11px;color:var(--text-muted);padding:2px 4px">${total} resultado${total!==1?'s':''} ${total>120?'(mostrando 120)':''}</div>`;
    } else {
        icons = FA_FAVORITES;
        label = `<div style="grid-column:1/-1;font-size:11px;color:var(--text-muted);padding:2px 4px">Favoritos — digite para buscar nos ${FA_ALL.length.toLocaleString()} ícones</div>`;
    }
    if(!icons.length) return '<div style="padding:10px;text-align:center;color:var(--text-muted);font-size:12px;grid-column:1/-1">Nenhum ícone encontrado</div>';
    return label + icons.map(ic => {
        const bg = ic===sel?'background:var(--primary);color:#fff;':'';
        return `<button style="width:100%;aspect-ratio:1;display:flex;align-items:center;justify-content:center;border:1px solid var(--border);border-radius:4px;cursor:pointer;background:none;color:var(--text);font-size:14px;${bg}" title="${ic}" onclick="cpCatIconPick('${ic}')"><i class="fa-solid fa-${ic}"></i></button>`;
    }).join('');
}

// ── Category CRUD ─────────────────────────────────────────────────────────────

window.cpCatNew = () => openEditModal({
    title:'Nova categoria', name:'', sortOrder:_categories.length+1, iconFa:'',
    onSave:(n,o,i) => withOTP(
        code => cpApi('POST',`${API}/categories`,{name:n,sortOrder:o,iconFa:i,otp_code:code}),
        async()=>{cpToast('success','Categoria criada.');removeEl('cp-cat-modal');await loadAndRender();}
    )
});

window.cpCatEdit = (id) => { const c=_categories.find(x=>x.id===id);if(!c)return;openEditModal({
    title:`Editar: ${c.name}`, name:c.name, sortOrder:c.sortOrder, iconFa:c.iconFa||'',
    onSave:(n,o,i) => withOTP(
        code => cpApi('PUT',`${API}/categories/${id}`,{name:n,sortOrder:o,iconFa:i,otp_code:code}),
        async()=>{cpToast('success','Atualizada.');removeEl('cp-cat-modal');await loadAndRender();}
    )
});};

window.cpCatDelete = async(id,name) => {
    if(!await cpConfirm('<i class="fa-solid fa-trash-can"></i> Excluir',`Excluir <strong>${esc(name)}</strong> e suas subcategorias?`,'Excluir','Cancelar'))return;
    withOTP(code=>cpApi('DELETE',`${API}/categories/${id}`,{otp_code:code}),async()=>{cpToast('success','Excluída.');await loadAndRender();});
};

// ── Subcategory CRUD ──────────────────────────────────────────────────────────

window.cpSubNew = (catId) => { const c=_categories.find(x=>x.id===catId);if(!c)return;openEditModal({
    title:`Nova sub em ${c.name}`, name:'', sortOrder:(c.subcategories||[]).length+1, iconFa:'',
    onSave:(n,o,i) => withOTP(
        code => cpApi('POST',`${API}/categories/${catId}/subcategories`,{name:n,sortOrder:o,iconFa:i,otp_code:code}),
        async()=>{cpToast('success','Subcategoria criada.');removeEl('cp-cat-modal');await loadAndRender();}
    )
});};

window.cpSubEdit = (catId,subId) => { const c=_categories.find(x=>x.id===catId);const s=c?.subcategories?.find(x=>x.id===subId);if(!s)return;openEditModal({
    title:`Editar: ${s.name}`, name:s.name, sortOrder:s.sortOrder, iconFa:s.iconFa||'',
    onSave:(n,o,i) => withOTP(
        code => cpApi('PUT',`${API}/categories/${catId}/subcategories/${subId}`,{name:n,sortOrder:o,iconFa:i,otp_code:code}),
        async()=>{cpToast('success','Atualizada.');removeEl('cp-cat-modal');await loadAndRender();}
    )
});};

window.cpSubDelete = async(catId,subId,name) => {
    if(!await cpConfirm('<i class="fa-solid fa-xmark"></i> Excluir',`Excluir <strong>${esc(name)}</strong>?`,'Excluir','Cancelar'))return;
    withOTP(code=>cpApi('DELETE',`${API}/categories/${catId}/subcategories/${subId}`,{otp_code:code}),async()=>{cpToast('success','Excluída.');await loadAndRender();});
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function removeEl(id){document.getElementById(id)?.remove();}
function setBreadcrumb(l){const el=document.getElementById('cp-breadcrumb');if(el)el.innerHTML=`<strong>${l}</strong>`;}
function esc(s){return(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');}
function ea(s){return(s||'').replace(/"/g,'&quot;').replace(/</g,'&lt;');}
