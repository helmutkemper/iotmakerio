// server/public/control/js/pages/login.js — Control panel login screen.
//
// The admin does not enter a password here — they must already be logged in
// to the portal (localStorage has a valid portal JWT). The login screen
// exchanges that token for a short-lived control panel token.
//
// Auto-redirect behavior:
//   - No portal token in localStorage → redirect to /app immediately
//   - Portal token exists but is stale (DB reset, expired) → clear token,
//     redirect to /app with a query param so the portal can show a message
//   - Portal token valid but user is not admin → show error (no redirect)
//   - Portal token valid + admin → auto-login, no login screen visible

import { CS, saveControlToken } from '../state.js';
import { exchangeForControlToken } from '../api.js';

/**
 * Renders the login screen.
 * @param {HTMLElement} root
 * @param {function} onSuccess - called with decoded claims when login succeeds
 */
export function renderLogin(root, onSuccess) {
    // Store callback so doLogin (exposed to window) can call it.
    window._cpLoginSuccess = onSuccess;

    // No portal token at all → go straight to /app login.
    const portalToken = CS.portalToken || localStorage.getItem('token');
    if (!portalToken) {
        redirectToPortal();
        return;
    }

    // Put the shell in login mode (hides sidebar and topbar via CSS class).
    document.getElementById('cp-shell').classList.add('login-mode');

    // Show a minimal loading state while the exchange happens.
    root.innerHTML = `
<div class="cp-login-card">
  <h1><i class="fa-solid fa-shield-halved"></i> Control Panel</h1>
  <p id="cp-login-status">Autenticando com sua sessão do portal…</p>
  <div id="cp-login-alert" class="cp-alert cp-alert-danger"></div>
  <div id="cp-login-actions" style="display:none">
    <button class="cp-btn cp-btn-primary cp-btn-full" onclick="cpDoLogin()">
      <i class="fa-solid fa-right-to-bracket"></i>
      Tentar novamente
    </button>
    <p style="margin-top:16px;font-size:12px;text-align:center">
      <a href="/app">← Voltar ao portal</a>
    </p>
  </div>
</div>`;

    // Auto-exchange immediately.
    doLogin();
}

async function doLogin() {
    const alert = document.getElementById('cp-login-alert');
    const status = document.getElementById('cp-login-status');
    const actions = document.getElementById('cp-login-actions');
    if (alert) alert.classList.remove('show');
    if (status) status.textContent = 'Autenticando com sua sessão do portal…';
    if (actions) actions.style.display = 'none';

    const token = CS.portalToken || localStorage.getItem('token');
    if (!token) {
        redirectToPortal();
        return;
    }

    const result = await exchangeForControlToken(token);

    if (result?.error) {
        if (result.status === 401) {
            // Portal token is stale (DB reset, expired, user deleted).
            // Clear everything and redirect to /app so the user logs in fresh.
            localStorage.removeItem('token');
            CS.portalToken = null;
            redirectToPortal();
            return;
        }

        // Non-401 error (e.g., 403 = not admin). Show error with manual retry.
        if (status) status.textContent = '';
        showError(alert, result.error || 'Sem permissão. Apenas administradores podem acessar.');
        if (actions) actions.style.display = 'block';
        return;
    }

    if (!result?.token) {
        showError(alert, 'Resposta inesperada do servidor.');
        if (actions) actions.style.display = 'block';
        return;
    }

    saveControlToken(result.token);

    // Decode claims from the JWT payload (base64url, no verification needed —
    // the server already verified the signature).
    const claims = decodeJWTPayload(result.token);

    document.getElementById('cp-shell').classList.remove('login-mode');

    if (window._cpLoginSuccess) {
        window._cpLoginSuccess(claims);
    }
}

// Expose to window for the onclick handler.
window.cpDoLogin = doLogin;

// redirectToPortal navigates to /app so the user can log in.
// A brief message is shown for 1 second before the redirect so the user
// understands why they were sent there.
function redirectToPortal() {
    const root = document.getElementById('cp-root');
    if (root) {
        document.getElementById('cp-shell')?.classList.add('login-mode');
        root.innerHTML = `
<div class="cp-login-card" style="text-align:center">
  <i class="fa-solid fa-right-to-bracket" style="font-size:32px;color:var(--primary);margin-bottom:12px"></i>
  <p style="font-size:14px;color:var(--text)">Redirecionando para o login…</p>
</div>`;
    }
    setTimeout(() => { window.location.href = '/app'; }, 800);
}

function showError(alertEl, msg) {
    if (!alertEl) return;
    alertEl.textContent = msg;
    alertEl.classList.add('show');
}

/**
 * Decodes the payload of a JWT without verifying its signature.
 * Verification is done server-side; this is only for reading display fields.
 */
function decodeJWTPayload(token) {
    try {
        const [, payload] = token.split('.');
        const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
        return JSON.parse(json);
    } catch {
        return {};
    }
}
