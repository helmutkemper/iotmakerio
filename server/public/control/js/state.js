// server/public/control/js/state.js — Single source of truth for the control panel SPA.
//
// controlToken: the short-lived JWT issued by POST /api/auth/control-token.
//   Stored only in sessionStorage (not localStorage) so it expires when the
//   browser tab is closed. Never reused across sessions.
//
// portalToken: the portal JWT from localStorage, used only to exchange for a
//   control token. Never sent to /api/control/v1/* endpoints directly.

export const CS = {
    // Control panel JWT (1 hour lifetime, sessionStorage only).
    controlToken: sessionStorage.getItem('cp_token') || null,

    // Portal JWT — used only for the control-token exchange on login.
    portalToken: localStorage.getItem('token') || null,

    // Decoded claims from the control token (populated after login).
    user: null,

    // Current page key.
    page: 'login',
};

// saveControlToken stores the control token in sessionStorage and updates CS.
export function saveControlToken(token) {
    CS.controlToken = token;
    sessionStorage.setItem('cp_token', token);
}

// clearControlToken removes the control token from memory and sessionStorage.
export function clearControlToken() {
    CS.controlToken = null;
    sessionStorage.removeItem('cp_token');
}
