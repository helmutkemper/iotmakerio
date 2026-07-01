// /ide/server/public/static/js/pages/profile.js
// pages/profile.js — User profile management page.
//
// This page has two views rendered into the same root:
//
//   OWN PROFILE  — accessible at nav('profile')
//     Shows the authenticated user's editable profile: display name, bio,
//     avatar upload, social links, locale preference, and invite codes.
//
//   PUBLIC PROFILE  — accessible at nav('profile', { username: 'alice' })
//     Read-only view of any user's public profile. No auth required
//     (the server enforces this at the API level).
//
// GitHub OAuth result:
//   Previous code read window.location.hash to detect the OAuth callback result.
//   This was fragile because boot() routes the user to 'projects' before the
//   profile page renders, and the stale hash could cause false-positive alerts.
//   Now boot() captures the result into S._githubResult (see main.js) and the
//   profile page consumes it once on render, then clears it.
//
// Locale preference:
//   The "Interface Language" section renders a <select> from S.locales and
//   calls window.switchLocale(code) on change. The switch is immediate — no
//   "Save Profile" button needed. switchLocale() handles state, localStorage,
//   translation reload, DB persistence, and UI re-render.
//
// API endpoints used:
//   GET  /api/v1/profile              — own profile + user data
//   PUT  /api/v1/profile              — update display_name, bio, links
//   PUT  /api/v1/profile/locale       — update locale (called by switchLocale)
//   POST /api/v1/profile/avatar       — upload avatar (multipart)
//   GET  /api/v1/profile/invites      — list own invite codes
//   POST /api/auth/invite             — generate a new invite code
//   GET  /api/v1/users/:username      — public profile (read-only)

import { api } from '../api.js';
import { S }   from '../state.js';

// ─── Entry point ──────────────────────────────────────────────────────────────

// renderProfile is the single entry point registered in router.js.
// When ctx.username is provided, renders the public profile for that user.
// Otherwise renders the authenticated user's own editable profile.
export async function renderProfile(root, ctx) {
    if (ctx?.username) {
        await renderPublicProfile(root, ctx.username);
    } else {
        await renderOwnProfile(root);
    }
}

// ─── Own profile ──────────────────────────────────────────────────────────────

async function renderOwnProfile(root) {
    root.innerHTML = _loadingShell('Your Profile');

    const r = await api('GET', '/api/v1/profile');
    if (r?.metadata?.status !== 200) {
        root.innerHTML = _errorShell('Could not load your profile. Please try again.');
        return;
    }

    const { user, profile } = r.data;

    root.innerHTML = `
<div style="max-width:760px;margin:0 auto;padding:24px 20px">

  <!-- ── Header ── -->
  <div style="display:flex;align-items:center;gap:20px;margin-bottom:32px;
              padding-bottom:24px;border-bottom:1px solid var(--border)">
    <!-- Avatar -->
    <div style="position:relative;flex-shrink:0">
      <div id="profile-av-wrap" style="width:80px;height:80px;border-radius:50%;
           overflow:hidden;background:var(--bg-surface);border:2px solid var(--border);
           display:flex;align-items:center;justify-content:center;cursor:pointer"
           title="Click to change avatar" onclick="triggerAvatarUpload()">
        ${profile.avatarUrl
        ? `<img id="profile-av-img" src="${esc(profile.avatarUrl)}" alt="avatar"
                    style="width:100%;height:100%;object-fit:cover">`
        : `<span id="profile-av-init" style="font-size:28px;font-weight:700;
                    color:var(--primary)">${esc(user.username[0].toUpperCase())}</span>`}
      </div>
      <button onclick="triggerAvatarUpload()" title="Change avatar"
        style="position:absolute;bottom:0;right:0;width:24px;height:24px;border-radius:50%;
               background:var(--primary);border:2px solid var(--bg-card);cursor:pointer;
               color:#fff;font-size:11px;display:flex;align-items:center;justify-content:center">
        <i class="fa-solid fa-pen"></i>
      </button>
    </div>
    <input type="file" id="avatar-input" accept=".png,.jpg,.jpeg,.webp" style="display:none"
           onchange="onAvatarSelected(event)">

    <div style="flex:1;min-width:0">
      <div style="font-size:20px;font-weight:700;color:var(--text-primary)">
        ${esc(profile.displayName || user.username)}
      </div>
      <div style="font-size:13px;color:var(--text-muted)">@${esc(user.username)}</div>
      <div style="font-size:12px;color:var(--text-muted);margin-top:2px">
        ${esc(user.email)} · ${esc(user.role)}
      </div>
    </div>
    <div id="profile-save-status" style="font-size:13px;color:var(--success);flex-shrink:0"></div>
  </div>

  <!-- ── Alert ── -->
  <div id="profile-alert" style="margin-bottom:16px;display:none"></div>

  <!-- ── Edit form ── -->
  <div style="display:grid;grid-template-columns:1fr 1fr;gap:20px">

    <div class="fg" style="grid-column:1/-1">
      <label>Display Name
        <span style="font-weight:400;color:var(--text-muted);font-size:12px">(shown in the feed)</span>
      </label>
      <input class="fc" id="pf-dn" type="text" maxlength="50"
             placeholder="Your public display name" value="${esc(profile.displayName || '')}">
      <div class="fhint">Can contain spaces and unicode. Max 50 characters.</div>
    </div>

    <div class="fg" style="grid-column:1/-1">
      <label>Bio</label>
      <textarea class="fc" id="pf-bio" rows="3" maxlength="2000"
                placeholder="Write a short bio about yourself…"
                style="resize:vertical">${esc(profile.bio || '')}</textarea>
      <div class="fhint" id="pf-bio-hint">${_bioHint(profile.bio || '')}</div>
    </div>

    <div class="fg">
      <label>GitHub URL</label>
      <input class="fc" id="pf-gh" type="url" placeholder="https://github.com/yourname"
             value="${esc(profile.githubUrl || '')}">
    </div>

    <div class="fg">
      <label>Website URL</label>
      <input class="fc" id="pf-ws" type="url" placeholder="https://yoursite.com"
             value="${esc(profile.websiteUrl || '')}">
    </div>

    <!-- ── Editor Settings link ── -->
    <div class="fg" style="grid-column:1/-1">
      <label><i class="fa-solid fa-sliders" style="margin-right:4px;color:var(--primary)"></i>
        Editor Settings
      </label>
      <div style="font-size:13px;color:var(--text-muted)">
        Language, menu profile, and menu visibility preferences have moved to
        <a href="javascript:void(0)" onclick="nav('editorSettings')"
           style="color:var(--primary);text-decoration:underline;cursor:pointer">Editor Settings</a>.
      </div>
    </div>

    <!-- ── GitHub Identity ── -->
    <div class="fg" style="grid-column:1/-1">
      <label>GitHub Identity <span style="font-size:11px;color:var(--text-muted);font-weight:400">(required to submit devices &amp; templates)</span></label>
      ${profile.githubUsername
        ? `<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;
                       background:var(--bg-2);border:1px solid var(--border);
                       border-radius:6px;font-size:13px">
             <i class="fa-brands fa-github" style="font-size:16px"></i>
             <span style="font-weight:600">@${esc(profile.githubUsername)}</span>
             <span style="color:var(--success);margin-left:4px">
               <i class="fa-solid fa-circle-check"></i> Verified
             </span>
             <button class="btn btn-sm" style="margin-left:auto;font-size:12px"
                     onclick="connectGithub()" title="Re-connect to change account">
               <i class="fa-solid fa-arrows-rotate"></i> Reconnect
             </button>
           </div>`
        : `<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;
                       background:var(--bg-2);border:1px solid var(--border);
                       border-radius:6px;font-size:13px;color:var(--text-muted)">
             <i class="fa-brands fa-github" style="font-size:16px"></i>
             <span>Not connected</span>
             <button class="btn btn-primary btn-sm" style="margin-left:auto"
                     onclick="connectGithub()">
               <i class="fa-brands fa-github"></i> Connect GitHub
             </button>
           </div>`
    }
      <div class="fhint">
        Connecting lets us verify you own the GitHub account when you submit repositories.
        We only read your public login — no tokens are stored.
      </div>
    </div>

  </div>

  <!-- ── Invite codes ── -->
  <div style="margin-top:40px">
    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px">
      <div>
        <h3 style="font-size:16px;font-weight:700;margin:0">Invite Codes</h3>
        <p style="font-size:13px;color:var(--text-muted);margin:4px 0 0">
          Share these links to invite others to the platform.
        </p>
      </div>
      <button class="btn btn-primary btn-sm" id="invite-gen-btn" onclick="generateInvite()">
        <i class="fa-solid fa-plus"></i> Generate Invite
      </button>
    </div>
    <div id="invite-list-wrap">
      <div style="text-align:center;padding:20px;color:var(--text-muted)">
        <i class="fa-solid fa-spinner fa-spin"></i>
      </div>
    </div>
  </div>

  <!-- ── Save Profile ── -->
  <div style="margin-top:32px;padding-top:24px;border-top:1px solid var(--border);
              display:flex;justify-content:flex-end">
    <button class="btn btn-primary" id="pf-save-btn" onclick="saveProfile()"
            style="min-width:140px;font-size:14px;padding:10px 20px">
      <i class="fa-solid fa-floppy-disk"></i> Save Profile
    </button>
  </div>

</div>`;

    // ── Show GitHub OAuth result (if redirected from callback) ─────────────
    //
    // S._githubResult is set by boot() when the hash contains ?github=...
    // We consume it here once and clear it to prevent stale alerts.
    if (S._githubResult === 'connected') {
        _showProfileAlert('GitHub account connected successfully!', 'success');
        S._githubResult = null;
    } else if (S._githubResult?.error) {
        const reason = S._githubResult.reason || 'unknown';
        _showProfileAlert('GitHub connection failed (' + reason + '). Please try again.', 'danger');
        S._githubResult = null;
    }

    // Wire up bio character counter.
    document.getElementById('pf-bio')?.addEventListener('input', function() {
        document.getElementById('pf-bio-hint').textContent = _bioHint(this.value);
    });

    // Load invite list.
    await _loadInviteList();
}

// ─── Profile actions ──────────────────────────────────────────────────────────

// connectGithub saves the profile form first (if it has content), then
// redirects the user to the GitHub OAuth flow.
// This prevents losing unsaved changes when the browser navigates away.
export async function connectGithub() {
    const token = localStorage.getItem('token');
    if (!token) { window.nav('login'); return; }

    // Save profile first if any fields are filled.
    // This prevents the user from losing changes mid-flow.
    const displayName = document.getElementById('pf-dn')?.value.trim() || '';
    const bio         = document.getElementById('pf-bio')?.value.trim() || '';
    const githubUrl   = document.getElementById('pf-gh')?.value.trim()  || '';
    const websiteUrl  = document.getElementById('pf-ws')?.value.trim()  || '';

    if (displayName || bio || githubUrl || websiteUrl) {
        const btn = document.getElementById('pf-save-btn');
        if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }
        try {
            await api('PUT', '/api/v1/profile', { displayName, bio, githubUrl, websiteUrl });
        } catch (e) {
            // Non-fatal — proceed with OAuth regardless.
            console.warn('[connectGithub] profile save failed:', e);
        }
    }

    // Navigate to the OAuth redirect endpoint.
    // The token is passed via ?token= because this is a full browser navigation,
    // not a fetch() — Authorization headers cannot be sent this way.
    window.location.href = '/api/auth/github?token=' + encodeURIComponent(token);
}

export async function saveProfile() {
    const displayName = document.getElementById('pf-dn')?.value.trim() || '';
    const bio         = document.getElementById('pf-bio')?.value.trim() || '';
    const githubUrl   = document.getElementById('pf-gh')?.value.trim() || '';
    const websiteUrl  = document.getElementById('pf-ws')?.value.trim() || '';

    const btn = document.getElementById('pf-save-btn');
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Saving…'; }

    const r = await api('PUT', '/api/v1/profile', { displayName, bio, githubUrl, websiteUrl });

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-floppy-disk"></i> Save Profile'; }

    if (r?.metadata?.status === 200) {
        const st = document.getElementById('profile-save-status');
        if (st) { st.textContent = '✓ Saved'; setTimeout(() => { st.textContent = ''; }, 3000); }
        _clearProfileAlert();
    } else {
        _showProfileAlert(r?.metadata?.error || 'Could not save profile.', 'danger');
    }
}

export function triggerAvatarUpload() {
    document.getElementById('avatar-input')?.click();
}

export async function onAvatarSelected(event) {
    const file = event.target.files?.[0];
    if (!file) return;
    event.target.value = '';

    const maxBytes = 2 * 1024 * 1024; // client-side guard; server enforces too
    if (file.size > maxBytes) {
        _showProfileAlert('Avatar must be smaller than 2 MB.', 'danger');
        return;
    }

    const form = new FormData();
    form.append('file', file);
    const headers = {};
    if (S.token) headers['Authorization'] = 'Bearer ' + S.token;

    const res  = await fetch('/api/v1/profile/avatar', { method: 'POST', headers, body: form });
    const json = await res.json().catch(() => null);

    if (json?.metadata?.status === 200) {
        const url = json.data.avatarUrl;
        // Update avatar preview without a full page reload.
        const wrap = document.getElementById('profile-av-wrap');
        if (wrap) {
            wrap.innerHTML = `<img id="profile-av-img" src="${esc(url)}?t=${Date.now()}"
                alt="avatar" style="width:100%;height:100%;object-fit:cover">`;
        }
        const st = document.getElementById('profile-save-status');
        if (st) { st.textContent = '✓ Avatar updated'; setTimeout(() => { st.textContent = ''; }, 3000); }
    } else {
        _showProfileAlert(json?.metadata?.error || 'Could not upload avatar.', 'danger');
    }
}

export async function generateInvite() {
    const btn = document.getElementById('invite-gen-btn');
    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Generating…'; }

    const r = await api('POST', '/api/auth/invite');

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-plus"></i> Generate Invite'; }

    if (r?.metadata?.status === 200) {
        // Refresh the invite list — simplest approach, avoids state sync.
        await _loadInviteList();
    } else {
        _showProfileAlert(r?.metadata?.error || 'Could not generate invite code.', 'danger');
    }
}

export async function copyInviteLink(code) {
    const link = `${window.location.origin}/app#register?invite=${code}`;
    try {
        await navigator.clipboard.writeText(link);
        _flashInviteBtn(code, '✓ Copied!');
    } catch {
        window.prompt('Copy this invite link:', link);
    }
}

// ─── Invite list ──────────────────────────────────────────────────────────────

async function _loadInviteList() {
    const wrap = document.getElementById('invite-list-wrap');
    if (!wrap) return;

    const r = await api('GET', '/api/v1/profile/invites');
    if (r?.metadata?.status !== 200) {
        wrap.innerHTML = '<p style="color:var(--danger);font-size:13px">Could not load invites.</p>';
        return;
    }

    const items = r.data || [];
    if (!items.length) {
        wrap.innerHTML = `
<div style="text-align:center;padding:24px;color:var(--text-muted);font-size:13px;
            border:1px dashed var(--border);border-radius:var(--r)">
  <i class="fa-solid fa-envelope-open" style="font-size:22px;opacity:.3;display:block;margin-bottom:8px"></i>
  No invite codes yet. Click <strong>Generate Invite</strong> to create one.
</div>`;
        return;
    }

    const statusColor = { active: 'var(--success)', used: 'var(--text-muted)', expired: 'var(--danger)' };
    const statusIcon  = { active: 'fa-circle-check', used: 'fa-circle-dot', expired: 'fa-circle-xmark' };

    wrap.innerHTML = items.map(inv => `
<div data-invite-code="${esc(inv.code)}"
     style="display:flex;align-items:center;gap:12px;padding:10px 14px;
            background:var(--bg-surface);border:1px solid var(--border);
            border-radius:var(--r);margin-bottom:8px">
  <i class="fa-solid ${statusIcon[inv.status] || 'fa-circle'}"
     style="color:${statusColor[inv.status] || 'var(--text-muted)'};flex-shrink:0"></i>
  <span style="font-family:var(--mono);font-size:12px;color:var(--text-secondary);
               flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"
        title="${esc(inv.code)}">${esc(inv.code)}</span>
  <span style="font-size:11px;color:var(--text-muted);flex-shrink:0;white-space:nowrap">
    ${inv.status === 'used'
        ? `used${inv.usedBy ? ' · @' + esc(inv.usedBy) : ''}`
        : inv.status === 'expired'
            ? 'expired ' + esc(inv.expiresAt)
            : 'expires ' + esc(inv.expiresAt)}
  </span>
  ${inv.status === 'active' ? `
  <button id="copy-btn-${esc(inv.code)}" onclick="copyInviteLink('${esc(inv.code)}')"
    style="padding:4px 10px;background:none;border:1px solid var(--border);
           border-radius:var(--r);cursor:pointer;font-size:11px;color:var(--primary);
           white-space:nowrap;flex-shrink:0">
    <i class="fa-solid fa-copy"></i> Copy link
  </button>` : ''}
</div>`).join('');
}

function _flashInviteBtn(code, msg) {
    const btn = document.getElementById(`copy-btn-${code}`);
    if (!btn) return;
    const orig = btn.innerHTML;
    btn.textContent = msg;
    setTimeout(() => { btn.innerHTML = orig; }, 2000);
}

// ─── Public profile ───────────────────────────────────────────────────────────

async function renderPublicProfile(root, username) {
    root.innerHTML = _loadingShell(username);

    const r = await api('GET', `/api/v1/users/${encodeURIComponent(username)}`);
    if (r?.metadata?.status !== 200) {
        if (r?.metadata?.status === 404) {
            root.innerHTML = _errorShell(`User @${esc(username)} not found.`);
        } else {
            root.innerHTML = _errorShell('Could not load profile. Please try again.');
        }
        return;
    }

    const p = r.data;
    const memberYear = new Date(p.memberSince).getFullYear();

    root.innerHTML = `
<div style="max-width:680px;margin:0 auto;padding:24px 20px">

  <!-- ── Header ── -->
  <div style="display:flex;align-items:flex-start;gap:20px;margin-bottom:28px;
              padding-bottom:24px;border-bottom:1px solid var(--border)">
    <!-- Avatar -->
    <div style="width:72px;height:72px;border-radius:50%;overflow:hidden;
                background:var(--bg-surface);border:2px solid var(--border);
                display:flex;align-items:center;justify-content:center;flex-shrink:0">
      ${p.avatarUrl
        ? `<img src="${esc(p.avatarUrl)}" alt="avatar" style="width:100%;height:100%;object-fit:cover">`
        : `<span style="font-size:26px;font-weight:700;color:var(--primary)">${esc(p.username[0].toUpperCase())}</span>`}
    </div>

    <div style="flex:1;min-width:0">
      <div style="font-size:20px;font-weight:700;color:var(--text-primary)">
        ${esc(p.displayName || p.username)}
      </div>
      <div style="font-size:13px;color:var(--text-muted)">
        @${esc(p.username)} · Member since ${memberYear}
      </div>
      ${p.bio ? `<p style="font-size:14px;color:var(--text-secondary);margin:10px 0 0;
                            line-height:1.6">${esc(p.bio)}</p>` : ''}
      <div style="display:flex;gap:14px;flex-wrap:wrap;margin-top:10px">
        ${p.githubUrl ? `<a href="${esc(p.githubUrl)}" target="_blank" rel="noopener noreferrer"
          style="font-size:13px;color:var(--primary);text-decoration:none;display:flex;align-items:center;gap:5px">
          <i class="fa-brands fa-github"></i> GitHub
        </a>` : ''}
        ${p.websiteUrl ? `<a href="${esc(p.websiteUrl)}" target="_blank" rel="noopener noreferrer"
          style="font-size:13px;color:var(--primary);text-decoration:none;display:flex;align-items:center;gap:5px">
          <i class="fa-solid fa-globe"></i> Website
        </a>` : ''}
      </div>
    </div>
  </div>

  <!-- Back button -->
  <button class="btn btn-ghost btn-sm" onclick="history.back()">
    <i class="fa-solid fa-arrow-left"></i> Back
  </button>
</div>`;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function _bioHint(value) {
    const len = [...(value || '')].length;
    const max = 280; // mirrors SettingProfileBioMaxChars default
    return `${len} / ${max} characters`;
}

function _showProfileAlert(msg, type) {
    const el = document.getElementById('profile-alert');
    if (!el) return;
    el.className = `alert alert-${type}`;
    el.textContent = msg;
    el.style.display = 'block';
}

function _clearProfileAlert() {
    const el = document.getElementById('profile-alert');
    if (el) el.style.display = 'none';
}

function _loadingShell(title) {
    return `
<div style="max-width:760px;margin:0 auto;padding:40px 20px;
            text-align:center;color:var(--text-muted)">
  <i class="fa-solid fa-spinner fa-spin" style="font-size:24px"></i>
  <p style="margin-top:12px">Loading ${esc(title)}…</p>
</div>`;
}

function _errorShell(msg) {
    return `
<div style="max-width:760px;margin:0 auto;padding:40px 20px;text-align:center">
  <i class="fa-solid fa-circle-exclamation" style="font-size:32px;color:var(--danger);display:block;margin-bottom:12px"></i>
  <p style="color:var(--text-secondary)">${esc(msg)}</p>
  <button class="btn btn-ghost btn-sm" style="margin-top:16px" onclick="nav('projects')">
    <i class="fa-solid fa-arrow-left"></i> Back to projects
  </button>
</div>`;
}

function esc(str) {
    if (!str) return '';
    return String(str)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;')
        .replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}
