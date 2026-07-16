// server/public/control/js/main.js — Control panel entry point.
//
// Boots the SPA: checks for an existing control token, shows login if absent,
// then routes to the appropriate page. The sidebar groups are defined in
// NAV_ITEMS; the page renderers are wired in PAGE_MAP. Both tables are the
// only two edits needed to expose a new admin screen.

import { CS, clearControlToken } from './state.js';
import { cpToast }               from './toast.js';
import { cpApi }                 from './api.js';
import { renderLogin }           from './pages/login.js';
import { renderUsers }           from './pages/users.js';
import { renderCategories }      from './pages/categories.js';
import { renderMenuTree }        from './pages/menuTree.js';
import { renderUserMenuPrefs }   from './pages/userMenuPrefs.js';
import { renderVisibilityRules } from './pages/visibilityRules.js';
import { renderGroups }          from './pages/groups.js';
import { renderTranslations }    from './pages/translations.js';
import { renderWizardExamples }  from './pages/wizardExamples.js';

// ── Navigation items ──────────────────────────────────────────────────────────
//
// Each entry in NAV_ITEMS becomes a section heading in the sidebar, followed
// by its buttons. The "key" matches the PAGE_MAP key below, the "icon" is a
// FontAwesome class name, and the "label" is the visible button text.

const NAV_ITEMS = [
    {
        section: 'Pessoas',
        items: [
            { key: 'users',    icon: 'fa-users',         label: 'Usuários' },
            { key: 'groups', icon: 'fa-people-group', label: 'Grupos' },
        ],
    },
    {
        section: 'Conteúdo',
        items: [
            { key: 'templates', icon: 'fa-cubes',        label: 'Templates' },
            { key: 'devices',   icon: 'fa-microchip',    label: 'Devices' },
            { key: 'comments',  icon: 'fa-comments',     label: 'Comentários' },
            { key: 'reports',   icon: 'fa-flag',         label: 'Reportes' },
        ],
    },
    {
        section: 'Menu',
        items: [
            { key: 'menuTree', icon: 'fa-sitemap',        label: 'Árvore do menu' },
            { key: 'school',   icon: 'fa-graduation-cap', label: 'School' },
            { key: 'userMenuPrefs', icon: 'fa-user-gear',   label: 'Menu do Usuário' },
            { key: 'visibilityRules', icon: 'fa-shield-halved',  label: 'Regras de Visibilidade' },
            { key: 'categories', icon: 'fa-folder-tree',    label: 'Categorias' },
        ],
    },
    {
        section: 'Plataforma',
        items: [
            { key: 'translations', icon: 'fa-language', label: 'Traduções' },
        ],
    },
    {
        section: 'Sistema',
        items: [
            { key: 'invites',  icon: 'fa-envelope',      label: 'Convites' },
            { key: 'settings', icon: 'fa-sliders',       label: 'Configurações' },
        ],
    },
];

// PAGE_MAP maps route keys to render functions.
// Pages not yet implemented show a "coming soon" placeholder.
const PAGE_MAP = {
    users:     renderUsers,
    groups:          renderGroups,
    templates: renderComingSoon('Templates', 'fa-cubes'),
    devices:   renderComingSoon('Devices',   'fa-microchip'),
    comments:  renderComingSoon('Comentários', 'fa-comments'),
    reports:   renderComingSoon('Reportes',  'fa-flag'),
    menuTree:  renderMenuTree,
    userMenuPrefs:  renderUserMenuPrefs,
    visibilityRules: renderVisibilityRules,
    categories: renderCategories,
    translations: renderTranslations,
    school:    renderWizardExamples,
    invites:   renderComingSoon('Convites',  'fa-envelope'),
    settings:  renderComingSoon('Configurações', 'fa-sliders'),
};

// ── Boot ──────────────────────────────────────────────────────────────────────

async function boot() {
    const root = document.getElementById('cp-root');

    // If no control token exists, show login.
    if (!CS.controlToken) {
        renderLogin(root, onLoginSuccess);
        return;
    }

    // Token exists — decode claims and enter the panel.
    const claims = decodeJWTPayload(CS.controlToken);

    // If the token has expired, go back to login.
    if (claims.exp && claims.exp * 1000 < Date.now()) {
        clearControlToken();
        renderLogin(root, onLoginSuccess);
        return;
    }

    onLoginSuccess(claims);
}

async function onLoginSuccess(claims) {
    CS.user = claims;
    // Fetch username from the users list — the JWT only carries uid + role.
    // We use the first page of the users list and find our own entry by uid.
    const r = await cpApi('GET', '/api/control/v1/users?page=1&limit=100');

    // If the API rejects the control token (e.g. after a DB reset), clear
    // all stale tokens and redirect to the portal login.
    if (r?.metadata?.status === 401 || r?.metadata?.status === 403) {
        clearControlToken();
        localStorage.removeItem('token');
        CS.portalToken = null;
        window.location.href = '/app';
        return;
    }

    if (r?.metadata?.status === 200) {
        const me = (r.data?.users || []).find(u => u.id === claims.uid);
        if (me) {
            CS.user.username = me.username;
        } else {
            // The JWT is technically valid (correct signature, not expired) but
            // the user ID does not exist in the database — stale token from a
            // previous DB that was deleted and recreated. Clear everything and
            // force a fresh login cycle through the portal.
            console.warn('[cp] stale token: uid', claims.uid, 'not found in users table');
            clearControlToken();
            localStorage.removeItem('token');
            CS.portalToken = null;
            const root = document.getElementById('cp-root');
            renderLogin(root, onLoginSuccess);
            return;
        }
    }
    renderShell(claims);
    nav('users');
}

// ── Shell ─────────────────────────────────────────────────────────────────────

function renderShell(claims) {
    renderSidebar(claims);
    renderUserInfo(claims);
}

function renderSidebar(claims) {
    const nav = document.getElementById('cp-nav');
    if (!nav) return;

    nav.innerHTML = NAV_ITEMS.map(group => `
<div class="cp-nav-section">${group.section}</div>
${group.items.map(item => `
<button class="cp-nav-item" id="cp-nav-${item.key}"
        onclick="cpNav('${item.key}')">
  <i class="fa-solid ${item.icon}"></i>
  ${item.label}
</button>`).join('')}
`).join('');
}

function renderUserInfo(claims) {
    const el = document.getElementById('cp-user-info');
    if (!el) return;
    const username = CS.user?.username || claims.uid || 'Admin';
    const initial  = username[0].toUpperCase();
    const roleLabel = {
        admin:                'Admin',
        official_specialist:  'Official Specialist',
        user:                 'User',
    }[claims.role] || claims.role || 'Admin';

    el.innerHTML = `
<div style="display:flex;align-items:center;gap:10px">
  <div class="cp-avatar">${initial}</div>
  <div>
    <div style="font-weight:600;font-size:13px;color:var(--text)">${username}</div>
    <span class="cp-role-badge">${roleLabel}</span>
  </div>
</div>`;
}

// ── Router ────────────────────────────────────────────────────────────────────

// _leaveGuard is set by a page that needs to confirm before navigation.
// It receives the destination page key and returns true to allow, false to block.
let _leaveGuard = null;

// registerLeaveGuard is called by a page to install a navigation guard.
// Pass null to remove it.
export function registerLeaveGuard(fn) {
    _leaveGuard = fn;
}

function nav(page) {
    // Ask the current page if it is safe to leave.
    if (_leaveGuard && !_leaveGuard(page)) {
        return; // navigation blocked by the page
    }
    _leaveGuard = null; // clear guard after leaving

    CS.page = page;

    // Update active state on sidebar.
    document.querySelectorAll('.cp-nav-item').forEach(btn => {
        btn.classList.toggle('active', btn.id === `cp-nav-${page}`);
    });

    const root = document.getElementById('cp-root');
    const handler = PAGE_MAP[page];
    if (handler) {
        handler(root);
    } else {
        root.innerHTML = `<div class="cp-empty"><i class="fa-solid fa-circle-question"></i>Página não encontrada.</div>`;
    }
}

// Expose to window for inline onclick handlers.
window.cpNav = nav;
window.cpToast = cpToast;

window.cpLogout = function() {
    clearControlToken();
    // Redirect to portal — the admin can re-enter the control panel from there.
    window.location.href = '/app';
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderComingSoon(label, icon) {
    return function(root) {
        const el = document.getElementById('cp-breadcrumb');
        if (el) el.innerHTML = `<strong>${label}</strong>`;
        root.innerHTML = `
<div class="cp-page-title">
  <i class="fa-solid ${icon}"></i> ${label}
</div>
<div class="cp-card">
  <div class="cp-card-body">
    <div class="cp-empty">
      <i class="fa-solid fa-hammer"></i>
      Em construção — em breve.
    </div>
  </div>
</div>`;
    };
}

function decodeJWTPayload(token) {
    try {
        const [, payload] = token.split('.');
        const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
        return JSON.parse(json);
    } catch {
        return {};
    }
}

boot();
