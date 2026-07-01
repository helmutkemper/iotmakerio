// pages/following.js — Following / followers management page.
import { showAlert } from '../utils.js';
//
// Shows two tabs:
//   Following — users that the authenticated user follows
//   Followers — users that follow the authenticated user
//
// Each row shows the user's avatar, display name, username, and an
// Unfollow button (for the Following tab).
//
// API endpoints:
//   GET    /api/v1/users/:username/following  — list who username follows
//   GET    /api/v1/users/:username/followers  — list username's followers
//   DELETE /api/v1/users/:username/follow     — unfollow

import { api } from '../api.js';
import { S }   from '../state.js';

let _root = null;
let _tab  = 'following';

// ─── Entry point ──────────────────────────────────────────────────────────────

export async function renderFollowing(root) {
    _root = root;
    _tab  = 'following';

    if (!S.user) {
        root.innerHTML = `
<div style="max-width:680px;margin:40px auto;padding:0 20px;text-align:center;color:var(--text-muted)">
  <i class="fa-solid fa-lock" style="font-size:32px;display:block;margin-bottom:12px;opacity:.3"></i>
  <p>You need to be logged in to see your following list.</p>
  <button class="btn btn-primary btn-sm" style="margin-top:12px" onclick="nav('login')">Log in</button>
</div>`;
        return;
    }

    root.innerHTML = _buildShell();
    await _loadTab(_tab);
}

function _buildShell() {
    return `
<div style="max-width:680px;margin:0 auto;padding:24px 20px">
  <div style="display:flex;align-items:center;justify-content:space-between;
              padding-bottom:20px;border-bottom:1px solid var(--border);margin-bottom:24px">
    <div>
      <h1 style="font-size:22px;font-weight:700;margin:0">
        <i class="fa-solid fa-user-group" style="color:var(--primary);margin-right:10px"></i>Network
      </h1>
      <p style="color:var(--text-muted);font-size:14px;margin:4px 0 0">
        People you follow and people who follow you
      </p>
    </div>
    <button class="btn btn-ghost btn-sm" onclick="nav('feed')">
      <i class="fa-solid fa-globe"></i> Browse Feed
    </button>
  </div>

  <!-- Tabs -->
  <div style="display:flex;gap:2px;margin-bottom:20px;border-bottom:1px solid var(--border)">
    <button id="fol-tab-following" onclick="followingSetTab('following')"
      style="padding:10px 18px;background:none;border:none;border-bottom:3px solid var(--primary);
             font-size:13px;font-weight:700;color:var(--primary);cursor:pointer;font-family:var(--font)">
      <i class="fa-solid fa-user-check" style="font-size:12px"></i> Following
    </button>
    <button id="fol-tab-followers" onclick="followingSetTab('followers')"
      style="padding:10px 18px;background:none;border:none;border-bottom:3px solid transparent;
             font-size:13px;font-weight:700;color:var(--text-muted);cursor:pointer;font-family:var(--font)">
      <i class="fa-solid fa-users" style="font-size:12px"></i> Followers
    </button>
  </div>

  <!-- List -->
  <div id="fol-list">
    <div style="text-align:center;padding:32px;color:var(--text-muted)">
      <i class="fa-solid fa-spinner fa-spin" style="font-size:20px"></i>
    </div>
  </div>
</div>`;
}

// ─── Tab switching ────────────────────────────────────────────────────────────

export async function followingSetTab(tab) {
    _tab = tab;

    ['following', 'followers'].forEach(t => {
        const btn = document.getElementById(`fol-tab-${t}`);
        if (!btn) return;
        const active = t === tab;
        btn.style.borderBottomColor = active ? 'var(--primary)' : 'transparent';
        btn.style.color = active ? 'var(--primary)' : 'var(--text-muted)';
    });

    await _loadTab(tab);
}

async function _loadTab(tab) {
    const list = document.getElementById('fol-list');
    if (!list) return;

    list.innerHTML = `<div style="text-align:center;padding:32px;color:var(--text-muted)">
      <i class="fa-solid fa-spinner fa-spin" style="font-size:20px"></i>
    </div>`;

    const username = S.user.username;
    const url = tab === 'following'
        ? `/api/v1/users/${encodeURIComponent(username)}/following`
        : `/api/v1/users/${encodeURIComponent(username)}/followers`;

    const r = await api('GET', url);
    if (r?.metadata?.status !== 200) {
        list.innerHTML = `<p style="color:var(--danger);text-align:center;font-size:13px">
            Could not load list. Please try again.</p>`;
        return;
    }

    const users = r.data || [];
    if (!users.length) {
        const msg = tab === 'following'
            ? 'You are not following anyone yet. <button class="flink" onclick="nav(\'feed\')">Browse the feed</button> to find components and follow their authors.'
            : 'Nobody is following you yet.';
        list.innerHTML = `<div style="text-align:center;padding:40px 20px;color:var(--text-muted);font-size:13px">${msg}</div>`;
        return;
    }

    list.innerHTML = users.map(u => _buildUserRow(u, tab)).join('');
}

function _buildUserRow(u, tab) {
    const avatarHtml = u.avatarUrl
        ? `<img src="${esc(u.avatarUrl)}" alt="avatar"
               style="width:44px;height:44px;border-radius:50%;object-fit:cover;flex-shrink:0">`
        : `<span style="width:44px;height:44px;border-radius:50%;background:var(--primary);
                        color:#fff;font-size:18px;font-weight:700;display:flex;
                        align-items:center;justify-content:center;flex-shrink:0">
             ${esc((u.displayName || u.username || '?')[0].toUpperCase())}
           </span>`;

    const unfollowBtn = tab === 'following'
        ? `<button onclick="followingUnfollow('${esc(u.username)}')"
             id="unfollow-btn-${esc(u.username)}"
             style="padding:5px 14px;background:none;border:1px solid var(--border);
                    border-radius:var(--r);cursor:pointer;font-size:12px;
                    color:var(--text-muted);font-family:var(--font);flex-shrink:0"
             onmouseover="this.style.borderColor='var(--danger)';this.style.color='var(--danger)'"
             onmouseout="this.style.borderColor='var(--border)';this.style.color='var(--text-muted)'">
             Unfollow
           </button>`
        : `<button onclick="nav('profile',{username:'${esc(u.username)}'})"
             style="padding:5px 14px;background:none;border:1px solid var(--border);
                    border-radius:var(--r);cursor:pointer;font-size:12px;
                    color:var(--primary);font-family:var(--font);flex-shrink:0">
             View profile
           </button>`;

    return `
<div id="fol-row-${esc(u.username)}"
     style="display:flex;align-items:center;gap:14px;padding:14px 0;
            border-bottom:1px solid var(--border)">
  <button onclick="nav('profile',{username:'${esc(u.username)}'})"
    style="background:none;border:none;cursor:pointer;padding:0;flex-shrink:0">
    ${avatarHtml}
  </button>
  <div style="flex:1;min-width:0">
    <div style="font-size:14px;font-weight:600;color:var(--text-primary);
                overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
      ${esc(u.displayName || u.username)}
    </div>
    <div style="font-size:12px;color:var(--text-muted)">@${esc(u.username)}</div>
  </div>
  <div style="font-size:11px;color:var(--text-muted);flex-shrink:0">
    since ${new Date(u.followedAt).getFullYear()}
  </div>
  ${unfollowBtn}
</div>`;
}

// ─── Actions ──────────────────────────────────────────────────────────────────

export async function followingUnfollow(username) {
    const btn = document.getElementById(`unfollow-btn-${username}`);
    if (btn) { btn.disabled = true; btn.textContent = 'Unfollowing…'; }

    const r = await api('DELETE', `/api/v1/users/${encodeURIComponent(username)}/follow`);

    if (r?.metadata?.status === 200) {
        // Remove the row from the list with a fade-out.
        const row = document.getElementById(`fol-row-${username}`);
        if (row) {
            row.style.transition = 'opacity .3s';
            row.style.opacity = '0';
            setTimeout(() => row.remove(), 300);
        }
    } else {
        if (btn) { btn.disabled = false; btn.textContent = 'Unfollow'; }
        await showAlert('danger', r?.metadata?.error || 'Não foi possível deixar de seguir. Tente novamente.');
    }
}

// ─── Utility ─────────────────────────────────────────────────────────────────

function esc(str) {
    if (!str) return '';
    return String(str)
        .replace(/&/g,'&amp;').replace(/</g,'&lt;')
        .replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}
