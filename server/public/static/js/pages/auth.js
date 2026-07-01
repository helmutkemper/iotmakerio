// /ide/server/public/static/js/pages/auth.js
// pages/auth.js — login, register, 2FA, verify-email, forgot/reset password.
//
// The registration form has three additions over the previous version:
//   1. Language selector — options loaded from GET /api/auth/register-config
//   2. Display name field — optional, shown in the marketplace feed
//   3. Invite code field  — shown only when inviteRequired=true in the config
//
// The register-config endpoint is called once per renderReg() call and its
// result cached in _regConfig for the lifetime of the form render. The cache
// is cleared when the user navigates away from the register page.
//
// Invite URL format: /app#register?invite={code}
// When the URL contains an invite code, the form pre-fills the field and
// shows who invited the user ("You were invited by @username").
//
// Locale sync on login:
//   After a successful 2FA verification, the user's preferredLocale from the
//   DB is applied to S.locale and localStorage. This ensures that the UI
//   language matches the user's saved preference, even on a new device.
import { S }                    from '../state.js';
import { esc, alert2, ld, showDialog } from '../utils.js';
import { api, loadTranslations } from '../api.js';

// _regConfig caches the result of GET /api/auth/register-config for the
// current form render. Cleared in renderReg() on each fresh navigation.
let _regConfig = null;

// ─── Login ────────────────────────────────────────────────────────────────────

export function renderLogin(root) {
    root.innerHTML = `
<div class="auth-wrap"><div class="auth-card">
  <h2><i class="fa-solid fa-right-to-bracket" style="color:var(--primary);margin-right:8px"></i>Entrar</h2>
  <p class="auth-sub">Bem-vindo de volta!</p>
  <div id="la"></div>
  <form onsubmit="event.preventDefault();doLogin()">
    <div class="fg">
      <label>Usuário ou e-mail</label>
      <input class="fc" id="fl" type="text" autocomplete="username" placeholder="usuario ou email@exemplo.com">
    </div>
    <div class="fg">
      <label>Senha</label>
      <input class="fc" id="fp" type="password" autocomplete="current-password" placeholder="••••••••">
    </div>
    <button type="submit" class="btn btn-primary btn-full btn-lg" id="btn-login">Entrar</button>
  </form>
  <div class="divider">ou</div>
  <div style="text-align:center;font-size:14px;color:var(--text-muted)">
    Não tem conta? <button class="flink" onclick="nav('register')">Cadastre-se</button>
  </div>
  <div style="text-align:center;margin-top:10px">
    <button class="flink" onclick="nav('forgot')">Esqueci minha senha</button>
  </div>
</div></div>`;
    document.getElementById('fl').focus();
}

export async function doLogin() {
    const login = document.getElementById('fl').value.trim();
    const pass  = document.getElementById('fp').value;
    if (!login || !pass) { alert2('la', 'warning', 'Preencha todos os campos.'); return; }
    ld('btn-login', true);
    try {
        const r = await api('POST', '/api/auth/login', { login, password: pass });
        if (r?.metadata?.status !== 200) {
            showDialog(r?.metadata?.error || 'Credenciais inválidas.',
                { title: 'Falha no login' });
            return;
        }
        S.pendingUID = r.data.userId;
        window.nav('2fa');
    } finally { ld('btn-login', false); }
}

// ─── 2FA ─────────────────────────────────────────────────────────────────────

export function render2FA(root) {
    root.innerHTML = `
<div class="auth-wrap"><div class="auth-card">
  <h2><i class="fa-solid fa-shield-halved" style="color:var(--primary);margin-right:8px"></i>Verificação em dois fatores</h2>
  <p class="auth-sub">Enviamos um código de 6 dígitos para o seu e-mail.</p>
  <div id="tfa"></div>
  <div class="fg">
    <label>Código de verificação</label>
    <input class="fc code" id="fc2" type="text" maxlength="6"
           inputmode="numeric" placeholder="000000" autocomplete="one-time-code">
  </div>
  <button class="btn btn-primary btn-full btn-lg" id="btn-2fa" onclick="do2FA()">Confirmar</button>
  <div style="text-align:center;margin-top:16px">
    <button class="flink" onclick="nav('login')">← Voltar</button>
  </div>
</div></div>`;
    const inp = document.getElementById('fc2');
    inp.focus();
    inp.addEventListener('keydown', e => { if (e.key === 'Enter') do2FA(); });
}

export async function do2FA() {
    const code = document.getElementById('fc2').value.trim();
    if (code.length !== 6) { alert2('tfa', 'warning', 'Digite o código de 6 dígitos.'); return; }
    ld('btn-2fa', true);
    try {
        const r = await api('POST', '/api/auth/login/2fa', { userId: S.pendingUID, code });
        if (r?.metadata?.status !== 200) {
            showDialog(r?.metadata?.error || 'Código inválido.',
                { title: 'Verificação 2FA' });
            return;
        }
        S.token = r.data.token;
        S.user  = r.data.user;
        localStorage.setItem('token', S.token);

        // ── Sync locale from the user's DB preference ─────────────────────
        //
        // The user's preferredLocale (chosen at registration or changed later)
        // takes precedence over the browser's current locale. This handles
        // the case where the user logs in on a new device or after clearing
        // localStorage.
        if (S.user.preferredLocale && S.user.preferredLocale !== S.locale) {
            S.locale = S.user.preferredLocale;
            localStorage.setItem('locale', S.locale);
            document.documentElement.lang = S.locale;
            // Reload translations for the user's preferred locale before
            // navigating to the first authenticated page.
            await loadTranslations();
        }

        window.nav('projects');
    } finally { ld('btn-2fa', false); }
}

// ─── Register ─────────────────────────────────────────────────────────────────

// renderReg renders the registration form and fetches its configuration
// (invite requirement flag + available locales) from the server.
// The invite code is pre-filled when the URL contains ?invite=<code>.
export async function renderReg(root) {
    _regConfig = null; // clear stale cache from previous render

    // Extract invite code from the URL hash: /app#register?invite=abc123
    const hashParams = new URLSearchParams(window.location.hash.replace(/^[^?]*\?/, ''));
    const inviteFromURL = hashParams.get('invite') || '';

    // Render skeleton while the config loads so the page is not blank.
    root.innerHTML = `
<div class="auth-wrap"><div class="auth-card">
  <h2><i class="fa-solid fa-user-plus" style="color:var(--primary);margin-right:8px"></i>Criar conta</h2>
  <p class="auth-sub" id="reg-sub">Carregando…</p>
  <div id="ra"></div>
  <div id="reg-form" style="display:none">
    <div class="fg">
      <label>Nome de usuário *</label>
      <input class="fc" id="ru" type="text" autocomplete="username" placeholder="meu_usuario" maxlength="32">
      <div class="fhint">3–32 caracteres: letras, números, _ e -</div>
    </div>
    <div class="fg">
      <label>Nome de exibição <span style="font-weight:400;color:var(--text-muted)">(opcional)</span></label>
      <input class="fc" id="rdn" type="text" autocomplete="name" placeholder="Seu Nome Completo" maxlength="50">
      <div class="fhint">Nome público mostrado no marketplace. Pode conter espaços.</div>
    </div>
    <div class="fg">
      <label>E-mail *</label>
      <input class="fc" id="re" type="email" autocomplete="email" placeholder="email@exemplo.com">
    </div>
    <div class="fg">
      <label>Senha *</label>
      <input class="fc" id="rp" type="password" autocomplete="new-password" placeholder="mínimo 8 caracteres">
    </div>
    <div class="fg">
      <label>Confirmar senha *</label>
      <input class="fc" id="rp2" type="password" autocomplete="new-password" placeholder="••••••••">
    </div>
    <div class="fg" id="fg-locale">
      <label>Idioma de interface *</label>
      <select class="fc" id="rl">
        <option value="">Carregando…</option>
      </select>
      <div class="fhint">Idioma padrão para a interface e documentação dos seus projetos.</div>
    </div>
    <div class="fg" id="fg-invite" style="display:none">
      <label>Código de convite *</label>
      <div id="invite-banner" style="display:none;margin-bottom:8px;padding:10px 14px;
           background:var(--info-bg);border:1px solid var(--primary);border-radius:var(--r);
           font-size:13px;color:var(--primary)"></div>
      <input class="fc" id="rc" type="text" placeholder="código de convite" maxlength="32"
             oninput="onInviteInput(this.value)" autocomplete="off">
      <div class="fhint">Você precisa de um convite para criar uma conta neste momento.</div>
    </div>
    <button class="btn btn-primary btn-full btn-lg" id="btn-reg" onclick="doReg()">Criar conta</button>
  </div>
  <div id="reg-loading" style="text-align:center;padding:24px;color:var(--text-muted)">
    <i class="fa-solid fa-spinner fa-spin" style="font-size:22px"></i>
  </div>
  <div class="divider">ou</div>
  <div style="text-align:center;font-size:14px;color:var(--text-muted)">
    Já tem conta? <button class="flink" onclick="nav('login')">Entrar</button>
  </div>
</div></div>`;

    // Fetch config (invite requirement + locales).
    const r = await api('GET', '/api/auth/register-config');
    if (r?.metadata?.status !== 200) {
        showDialog('Erro ao carregar configurações. Tente novamente.',
            { title: 'Erro de configuração' });
        document.getElementById('reg-loading').style.display = 'none';
        return;
    }
    _regConfig = r.data;

    // Populate locale selector.
    const localeSelect = document.getElementById('rl');
    if (_regConfig.locales?.length) {
        localeSelect.innerHTML = _regConfig.locales.map(l =>
            `<option value="${esc(l.code)}">${esc(l.display)}</option>`
        ).join('');
        // Default to browser language if available, else first option.
        const browserLang = navigator.language || 'en-US';
        const match = _regConfig.locales.find(l =>
            l.code === browserLang || l.code.startsWith(browserLang.split('-')[0])
        );
        localeSelect.value = match ? match.code : _regConfig.locales[0].code;
    } else {
        localeSelect.innerHTML = '<option value="en-US">English (US)</option>';
    }

    // Show invite field if required.
    if (_regConfig.inviteRequired) {
        document.getElementById('fg-invite').style.display = '';
        document.getElementById('reg-sub').textContent =
            'Plataforma em acesso fechado. Você precisa de um convite.';

        // If the URL contained a code, pre-fill and validate it.
        if (inviteFromURL) {
            const inp = document.getElementById('rc');
            inp.value = inviteFromURL;
            inp.readOnly = true; // Code came from the URL — prevent accidental edits.
            await _validateAndShowInviteBanner(inviteFromURL);
        }
    } else {
        document.getElementById('reg-sub').textContent = 'Rápido e gratuito.';
    }

    document.getElementById('reg-loading').style.display = 'none';
    document.getElementById('reg-form').style.display = '';
    document.getElementById('ru').focus();
}

// onInviteInput is called while the user types in the invite code field.
// It debounces the validation call to avoid hammering the server.
let _inviteDebounce = null;
export function onInviteInput(value) {
    clearTimeout(_inviteDebounce);
    const v = value.trim();
    if (v.length < 32) {
        _clearInviteBanner();
        return;
    }
    _inviteDebounce = setTimeout(() => _validateAndShowInviteBanner(v), 400);
}

// _validateAndShowInviteBanner calls the server and shows the "invited by"
// banner when the code is valid, or a warning when it is not.
async function _validateAndShowInviteBanner(code) {
    const banner = document.getElementById('invite-banner');
    if (!banner) return;

    banner.style.display = '';
    banner.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Verificando…';
    banner.style.background = 'var(--info-bg)';
    banner.style.borderColor = 'var(--border)';
    banner.style.color = 'var(--text-muted)';

    const r = await api('GET', `/api/auth/invite/${encodeURIComponent(code)}`);
    if (r?.metadata?.status === 200 && r.data?.valid) {
        banner.style.background = 'var(--success-bg, #f0fdf4)';
        banner.style.borderColor = 'var(--success)';
        banner.style.color = 'var(--success)';
        const invitedBy = r.data.invitedBy
            ? ` — convidado por <strong>@${esc(r.data.invitedBy)}</strong>`
            : '';
        banner.innerHTML = `<i class="fa-solid fa-circle-check"></i> Código válido${invitedBy}`;
    } else {
        banner.style.background = '#FEE2E2';
        banner.style.borderColor = 'var(--danger)';
        banner.style.color = '#991B1B';
        banner.innerHTML = '<i class="fa-solid fa-circle-xmark"></i> Código inválido, já utilizado ou expirado';
    }
}

function _clearInviteBanner() {
    const banner = document.getElementById('invite-banner');
    if (banner) banner.style.display = 'none';
}

export async function doReg() {
    const username    = document.getElementById('ru')?.value.trim();
    const displayName = document.getElementById('rdn')?.value.trim() || '';
    const email       = document.getElementById('re')?.value.trim();
    const pass        = document.getElementById('rp')?.value;
    const pass2       = document.getElementById('rp2')?.value;
    const locale      = document.getElementById('rl')?.value || 'en-US';
    const inviteCode  = _regConfig?.inviteRequired
        ? (document.getElementById('rc')?.value.trim() || '')
        : '';

    if (!username || !email || !pass) { alert2('ra', 'warning', 'Preencha todos os campos obrigatórios.'); return; }
    if (pass !== pass2)               { alert2('ra', 'danger',  'As senhas não coincidem.'); return; }
    if (_regConfig?.inviteRequired && !inviteCode) {
        alert2('ra', 'warning', 'Insira o código de convite.');
        return;
    }

    ld('btn-reg', true);
    try {
        const r = await api('POST', '/api/auth/register', {
            username,
            displayName,
            email,
            password:        pass,
            preferredLocale: locale,
            inviteCode,
        });
        if (r?.metadata?.status !== 200) {
            showDialog(r?.metadata?.error || 'Erro ao criar conta.',
                { title: 'Falha no cadastro' });
            return;
        }
        S.pendingUID = r.data.userId;
        window.nav('verify');
    } finally { ld('btn-reg', false); }
}

// ─── Verify Email ─────────────────────────────────────────────────────────────

export function renderVerify(root) {
    root.innerHTML = `
<div class="auth-wrap"><div class="auth-card">
  <h2><i class="fa-solid fa-envelope-circle-check" style="color:var(--primary);margin-right:8px"></i>Verificar e-mail</h2>
  <p class="auth-sub">Enviamos um código de 6 dígitos para o seu e-mail.</p>
  <div id="va"></div>
  <div class="fg">
    <label>Código de verificação</label>
    <input class="fc code" id="vc" type="text" maxlength="6"
           inputmode="numeric" placeholder="000000" autocomplete="one-time-code">
  </div>
  <button class="btn btn-primary btn-full btn-lg" id="btn-v" onclick="doVerify()">Confirmar e-mail</button>
  <div style="text-align:center;margin-top:16px">
    <button class="flink" onclick="nav('login')">← Voltar</button>
  </div>
</div></div>`;
    document.getElementById('vc').focus();
}

export async function doVerify() {
    const code = document.getElementById('vc').value.trim();
    if (code.length !== 6) { alert2('va', 'warning', 'Digite o código de 6 dígitos.'); return; }
    ld('btn-v', true);
    try {
        const r = await api('POST', '/api/auth/verify-email', { userId: S.pendingUID, code });
        if (r?.metadata?.status !== 200) {
            showDialog(r?.metadata?.error || 'Código inválido.',
                { title: 'Verificação de e-mail' });
            return;
        }
        alert2('va', 'success', 'E-mail confirmado! Agora você pode entrar.');
        setTimeout(() => window.nav('login'), 1800);
    } finally { ld('btn-v', false); }
}

// ─── Forgot Password ──────────────────────────────────────────────────────────

export function renderForgot(root) {
    root.innerHTML = `
<div class="auth-wrap"><div class="auth-card">
  <h2><i class="fa-solid fa-key" style="color:var(--primary);margin-right:8px"></i>Recuperar senha</h2>
  <p class="auth-sub">Informe seu e-mail para receber um código de recuperação.</p>
  <div id="fga"></div>
  <div class="fg">
    <label>E-mail cadastrado</label>
    <input class="fc" id="fge" type="email" autocomplete="email" placeholder="email@exemplo.com">
  </div>
  <button class="btn btn-primary btn-full btn-lg" id="btn-fg" onclick="doForgot()">Enviar código</button>
  <div style="text-align:center;margin-top:16px">
    <button class="flink" onclick="nav('login')">← Voltar</button>
  </div>
</div></div>`;
    document.getElementById('fge').focus();
}

export async function doForgot() {
    const email = document.getElementById('fge').value.trim();
    if (!email) { alert2('fga', 'warning', 'Preencha o e-mail.'); return; }
    ld('btn-fg', true);
    try {
        await api('POST', '/api/auth/forgot-password', { email });
        S._resetEmail = email;
        alert2('fga', 'info', 'Se o e-mail existir, você receberá o código em instantes.');
        setTimeout(() => window.nav('reset'), 1800);
    } finally { ld('btn-fg', false); }
}

// ─── Reset Password ───────────────────────────────────────────────────────────

export function renderReset(root) {
    root.innerHTML = `
<div class="auth-wrap"><div class="auth-card">
  <h2><i class="fa-solid fa-lock" style="color:var(--primary);margin-right:8px"></i>Nova senha</h2>
  <p class="auth-sub">Digite o código recebido e escolha uma nova senha.</p>
  <div id="rsa"></div>
  <div class="fg">
    <label>Código de recuperação</label>
    <input class="fc code" id="rsc" type="text" maxlength="6" inputmode="numeric" placeholder="000000">
  </div>
  <div class="fg">
    <label>Nova senha</label>
    <input class="fc" id="rsp" type="password" autocomplete="new-password" placeholder="mínimo 8 caracteres">
  </div>
  <div class="fg">
    <label>Confirmar nova senha</label>
    <input class="fc" id="rsp2" type="password" autocomplete="new-password" placeholder="••••••••">
  </div>
  <button class="btn btn-primary btn-full btn-lg" id="btn-rs" onclick="doReset()">Redefinir senha</button>
  <div style="text-align:center;margin-top:16px">
    <button class="flink" onclick="nav('login')">← Voltar</button>
  </div>
</div></div>`;
}

export async function doReset() {
    const code  = document.getElementById('rsc').value.trim();
    const pass  = document.getElementById('rsp').value;
    const pass2 = document.getElementById('rsp2').value;
    if (!code || !pass || !pass2)  { alert2('rsa', 'warning', 'Preencha todos os campos.'); return; }
    if (pass !== pass2)            { alert2('rsa', 'danger',  'As senhas não coincidem.'); return; }
    ld('btn-rs', true);
    try {
        const r = await api('POST', '/api/auth/reset-password', {
            email: S._resetEmail || '', code, newPassword: pass,
        });
        if (r?.metadata?.status !== 200) {
            showDialog(r?.metadata?.error || 'Erro ao redefinir.',
                { title: 'Redefinição de senha' });
            return;
        }
        alert2('rsa', 'success', 'Senha alterada! Redirecionando...');
        setTimeout(() => window.nav('login'), 1800);
    } finally { ld('btn-rs', false); }
}
