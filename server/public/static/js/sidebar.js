// server/public/static/js/sidebar.js
// sidebar.js — sidebar state, rendering, toggle logic, and locale switcher.
//
// The locale switcher renders a compact dropdown at the bottom of the nav area,
// generated dynamically from S.locales (loaded at boot from the server).
// Changing the dropdown calls window.switchLocale(code) defined in main.js.
//
// The switcher adapts to both sidebar states:
//   - Collapsed (56px): only a globe icon is visible (dropdown hidden).
//   - Expanded (220px): globe icon + compact dropdown [ 🇧🇷 BR ▼ ].
//
// Flag resolution: a small lookup table maps known locale codes to emoji flags.
// Unknown locales fall back to a globe icon (🌐).
//
// Translation editing is not exposed in this sidebar. Admins manage strings
// from the /control SPA; regular users only pick a locale via the dropdown
// at the bottom.
import { S } from './state.js';
import { t } from './utils.js';

export let sbOpen      = false;
export let blogGrpOpen = false;

export const BLOG_PAGES = ['admin', 'editor'];

// ── Locale → flag emoji lookup ────────────────────────────────────────────────
// Extend this object when new locales are added to the platform.
const LOCALE_FLAGS = {
    'pt-BR': '🇧🇷',
    'en-US': '🇺🇸',
    'es-ES': '🇪🇸',
    'fr-FR': '🇫🇷',
    'de-DE': '🇩🇪',
    'ja-JP': '🇯🇵',
    'zh-CN': '🇨🇳',
};

// ── Toggle functions ──────────────────────────────────────────────────────────

export function toggleSB() {
    sbOpen = !sbOpen;
    document.getElementById('sidebar').classList.toggle('open', sbOpen);
    document.getElementById('main-content').classList.toggle('sb-open', sbOpen);
    if (document.body.classList.contains('ide-mode')) {
        document.body.classList.toggle('sb-open', sbOpen);
    }
    if (!sbOpen) blogGrpOpen = false;
    renderSB();
}

export function toggleBlogGrp() {
    if (!sbOpen) {
        sbOpen      = true;
        blogGrpOpen = true;
        document.getElementById('sidebar').classList.add('open');
        document.getElementById('main-content').classList.add('sb-open');
    } else {
        blogGrpOpen = !blogGrpOpen;
    }
    renderSB();
}

// ── Render ────────────────────────────────────────────────────────────────────

export function renderSB() {
    const nav     = document.getElementById('sb-nav');
    const isAdmin = S.user?.role === 'admin';
    const grpOpen = blogGrpOpen || BLOG_PAGES.includes(S.page);

    let html = '';

    // ── Visible to all, without login ──────────────────────────────────────────
    html += sbItem('home',     'fa-solid fa-house',    t('nav.home',     'Home'));
    html += sbItem('feed',     'fa-solid fa-globe',    t('nav.feed',     'Community'));

    if (S.user) {
        html += '<div class="sb-sep"></div>';

        // ── Authenticated user items ──────────────────────────────────────────
        html += sbItem('projects', 'fa-solid fa-microchip', t('nav.projects', 'Devices'));
        html += sbItem('ide',      'fa-solid fa-code',        t('nav.ide',      'Editor'));
        html += sbItem('feed',     'fa-solid fa-globe',       t('nav.feed',     'Community'));
        html += sbItem('following','fa-solid fa-user-group',  t('nav.following','Following'));
        html += sbItem('profile',  'fa-solid fa-circle-user', t('nav.profile',  'Profile'));
        html += sbItem('editorSettings', 'fa-solid fa-sliders',     t('nav.editorSettings', 'Editor Settings'));

        if (isAdmin) {
            html += '<div class="sb-sep"></div>';
            html += `
<button class="sb-group-hdr${grpOpen ? ' grp-open' : ''}" onclick="toggleBlogGrp()" title="IoTMaker">
  <i class="fa-solid fa-newspaper si"></i>
  <span class="sl">IoTMaker</span>
  <i class="fa-solid fa-chevron-right sb-arrow"></i>
</button>
<div class="sb-submenu${grpOpen ? ' open' : ''}">
  ${sbSubItem('admin',  'fa-solid fa-list-ul',  t('nav.manage', 'Manage'))}
  ${sbSubItem('editor', 'fa-solid fa-file-pen', t('nav.write',  'New Post'))}
</div>`;
        }

        html += '<div class="sb-sep"></div>';
        html += sbItem('logout', 'fa-solid fa-right-from-bracket', t('nav.logout', 'Logout'));
    } else {
        html += '<div class="sb-sep"></div>';
        html += sbItem('login',    'fa-solid fa-right-to-bracket', t('nav.login',    'Login'));
        html += sbItem('register', 'fa-solid fa-user-plus',        t('nav.register', 'Register'));
    }

    // ── Locale switcher ───────────────────────────────────────────────────────
    // Rendered for all users (authenticated or not) at the bottom of the nav.
    // Uses S.locales which is loaded at boot from the server.
    if (S.locales.length > 1) {
        html += '<div class="sb-sep"></div>';
        html += _localeDropdown();
    }

    nav.innerHTML = html;

    // ── User area (bottom of sidebar) ─────────────────────────────────────────
    const uel = document.getElementById('sb-user');
    if (S.user) {
        uel.style.display = 'flex';

        // Avatar: use the image from the profile if available, otherwise show
        // the username initial as a coloured circle — same as before but now
        // clickable to navigate to the profile page.
        const av = document.getElementById('sb-av');
        if (av) {
            const avatarUrl = S.profile?.avatarUrl;
            if (avatarUrl) {
                av.innerHTML = `<img src="${_esc(avatarUrl)}" alt="avatar"
                    style="width:100%;height:100%;object-fit:cover;border-radius:50%"
                    onerror="this.parentElement.textContent='${_esc(S.user.username[0].toUpperCase())}'">`;
            } else {
                av.textContent = S.user.username[0].toUpperCase();
            }
            // Make the avatar clickable — navigates to own profile page.
            av.style.cursor = 'pointer';
            av.title = 'Go to your profile';
            av.onclick = () => window.nav('profile');
        }

        document.getElementById('sb-uname').textContent = S.user.username;
        document.getElementById('sb-urole').textContent = S.user.role === 'admin' ? 'Admin' : 'User';
    } else {
        uel.style.display = 'none';
    }
}

// ── Locale dropdown builder ───────────────────────────────────────────────────
//
// Generates a compact dropdown styled to match the sidebar theme.
// Uses the same sb-item layout pattern: icon (.si) + content (.sl).
//
// Layout:
//   - Collapsed sidebar: globe icon only (dropdown hidden via .sl opacity rule).
//   - Expanded sidebar:  globe icon + compact select [ 🇧🇷 BR ▼ ].
//
// The <select> options show flag emoji + short country code (e.g. "🇧🇷 BR").
// The onchange fires window.switchLocale(code) which handles everything.
function _localeDropdown() {
    const options = S.locales.map(loc => {
        const flag  = LOCALE_FLAGS[loc.code] || '🌐';
        // Short label: country part after the hyphen (e.g. "pt-BR" → "BR").
        const short = loc.code.split('-')[1] || loc.code.split('-')[0].toUpperCase();
        const sel   = loc.code === S.locale ? ' selected' : '';
        return `<option value="${_esc(loc.code)}"${sel}>${flag} ${short}</option>`;
    }).join('');

    // The wrapper reuses the sb-item class for consistent height, padding, and
    // hover behaviour. The <select> is wrapped in a .sl span so it respects
    // the collapsed/expanded visibility rule already defined in main.css.
    return `
<div class="sb-item" title="${_esc(t('nav.language', 'Language'))}" style="cursor:default">
  <i class="fa-solid fa-globe si"></i>
  <span class="sl" style="margin-left:12px;flex:1;min-width:0">
    <select onchange="switchLocale(this.value)"
      style="
        width:100%;max-width:120px;
        padding:3px 6px;
        border:1px solid rgba(255,255,255,.25);border-radius:4px;
        background:rgba(255,255,255,.08);color:#fff;
        font-size:12px;font-family:var(--font);font-weight:500;
        cursor:pointer;outline:none;
        -webkit-appearance:auto;appearance:auto;
      ">${options}</select>
  </span>
</div>`
        ;
}

// ── Nav item builders ─────────────────────────────────────────────────────────

function sbItem(page, icon, label) {
    return `<button class="sb-item${S.page === page ? ' active' : ''}" onclick="nav('${page}')" title="${label}">
  <i class="${icon} si"></i><span class="sl">${label}</span>
</button>`;
}

function sbSubItem(page, icon, label) {
    return `<button class="sb-sub-item${S.page === page ? ' active' : ''}" onclick="nav('${page}')" title="${label}">
  <i class="${icon} si"></i><span class="sl">${label}</span>
</button>`;
}

// _esc is a minimal HTML-attribute-safe escaper used for the avatar URL and
// username initial in inline event handlers. The full esc() in utils.js is
// not imported here to avoid a circular dependency.
function _esc(str) {
    if (!str) return '';
    return String(str).replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}
