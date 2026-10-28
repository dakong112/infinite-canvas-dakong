import { encryptPassword } from "@/lib/password-encryption";
import type { AuthSession } from "@/services/api/auth";
import { safeRedirect } from "@/lib/safe-redirect";
import { apiGet, apiPost, SILENT_LOGIN_KEY } from "@/services/api/request";

export type NewapiAuthConfig = { enabled: boolean; newapiUrl?: string; clientId?: string; redirectUri?: string };

export type NewapiStatus = {
    turnstile_check: boolean;
    turnstile_site_key: string;
    email_verification: boolean;
    register_enabled: boolean;
    password_register_enabled: boolean;
    password_login_enabled: boolean;
    password_login_encryption_enabled: boolean;
};

type Envelope<T> = { success: boolean; message?: string; data?: T };

export type LoginChallenge = { require_verification: true; flow_token: string; methods: { method: string; available?: boolean }[] };
type LoginSuccess = { access_token: string };

/** new-api reads the Turnstile token from the `turnstile` query parameter. */
const withTurnstile = (path: string, turnstile?: string) =>
    turnstile ? `${path}${path.includes("?") ? "&" : "?"}turnstile=${encodeURIComponent(turnstile)}` : path;

export const LOGIN_REDIRECT_KEY = "ic-login-redirect";
export { SILENT_LOGIN_KEY };

const SILENT_RETRY_MS = 60_000;
// Pages that send anonymous visitors to /login (user layout, admin layout, "请先登录" pages).
const LOGIN_ONLY_PREFIXES = ["/login", "/admin", "/asset-library", "/works", "/member", "/recharge"];

/** Stamps a silent new-api check now, so none is started for the next minute (e.g. right after logging out). */
export function markSilentLogin(storage: Pick<Storage, "setItem">, now = Date.now()) {
    storage.setItem(SILENT_LOGIN_KEY, String(now));
}

/**
 * Whether to ask new-api now (prompt=none) if the visitor is signed in there; remembers the attempt. Per tab at most
 * once a minute (the flag is cleared on sign-in and on "登录已失效"), never on the SSO callback or after a failed check.
 */
export function claimSilentLogin(storage: Pick<Storage, "getItem" | "setItem">, location: { pathname: string; search: string }, now = Date.now()): boolean {
    if (location.pathname === "/sso/callback" || new URLSearchParams(location.search).get("silent") === "failed") return false;
    const last = storage.getItem(SILENT_LOGIN_KEY);
    if (last !== null && now - Number(last) < SILENT_RETRY_MS) return false;
    markSilentLogin(storage, now);
    return true;
}

/** Top-level navigation to the silent check; the callback returns to `from` (a path with query). */
export function startSilentLogin(from: string) {
    sessionStorage.setItem(LOGIN_REDIRECT_KEY, from);
    window.location.replace("/api/auth/newapi/start?prompt=none");
}

/** Where the callback goes after a silent check: the page it started from; /login and login-only pages show the form. */
export function silentLoginTarget(from: string, loggedIn: boolean): string {
    const url = new URL(from, "http://local");
    if (url.pathname === "/login") {
        const redirect = safeRedirect(url.searchParams.get("redirect"));
        return loggedIn ? redirect : `/login?silent=failed&redirect=${encodeURIComponent(redirect)}`;
    }
    const loginOnly = LOGIN_ONLY_PREFIXES.some((prefix) => url.pathname === prefix || url.pathname.startsWith(`${prefix}/`));
    return loggedIn || !loginOnly ? from : `/login?silent=failed&redirect=${encodeURIComponent(from)}`;
}

export function getNewapiAuthConfig() {
    return apiGet<NewapiAuthConfig>("/api/auth/newapi/config");
}

async function newapiFetch<T>(base: string, path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(`${base}${path}`, {
        ...init,
        headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    });
    const body = (await response.json()) as Envelope<T>;
    if (!body.success) throw new Error(newapiErrorMessage(body.message));
    return body.data as T;
}

export function getNewapiStatus(base: string) {
    return newapiFetch<NewapiStatus>(base, "/api/status");
}

/** Returns an access token, or a challenge when the account requires 2FA. */
export async function newapiLogin(base: string, status: NewapiStatus, username: string, password: string, turnstile?: string): Promise<string | LoginChallenge> {
    const payload = status.password_login_encryption_enabled
        ? { username, ...(await encryptPassword(base, password)) }
        : { username, password };
    const data = await newapiFetch<LoginSuccess | LoginChallenge>(base, withTurnstile("/api/user/login", turnstile), { method: "POST", body: JSON.stringify(payload) });
    if ("require_verification" in data && data.require_verification) return data;
    return (data as LoginSuccess).access_token;
}

export async function newapiVerifyLogin(base: string, flowToken: string, code: string): Promise<string> {
    const data = await newapiFetch<LoginSuccess>(base, "/api/user/login/verify", {
        method: "POST",
        body: JSON.stringify({ flow_token: flowToken, method: "2fa", code }),
    });
    return data.access_token;
}

export function newapiSendEmailCode(base: string, email: string, turnstile?: string) {
    return newapiFetch<unknown>(base, withTurnstile(`/api/verification?email=${encodeURIComponent(email)}`, turnstile));
}

export function newapiRegister(base: string, payload: { username: string; password: string; email: string; verification_code: string; aff_code?: string }, turnstile?: string) {
    return newapiFetch<unknown>(base, withTurnstile("/api/user/register", turnstile), { method: "POST", body: JSON.stringify(payload) });
}

/** Completes sign-in: asks new-api for a one-time code bound to an infinite state, then lands on /sso/callback. */
export async function finishNewapiLogin(config: NewapiAuthConfig, accessToken: string, redirect: string) {
    const { state } = await apiPost<{ state: string }>("/api/auth/newapi/state");
    const data = await newapiFetch<{ redirect_url: string }>(config.newapiUrl!, "/api/sso/code", {
        method: "POST",
        headers: { Authorization: `Bearer ${accessToken}` },
        body: JSON.stringify({ client_id: config.clientId, redirect_uri: config.redirectUri, state }),
    });
    const target = new URL(data.redirect_url);
    const expected = new URL(config.redirectUri!);
    if (target.origin + target.pathname !== expected.origin + expected.pathname) throw new Error("登录回调地址无效");
    sessionStorage.setItem(LOGIN_REDIRECT_KEY, redirect);
    window.location.href = target.href;
}

export function exchangeNewapiCode(code: string, state: string) {
    return apiPost<AuthSession>("/api/auth/newapi/exchange", { code, state });
}

// new-api returns some account-service errors in English; translate the ones end users can hit on the login page.
const NEWAPI_ERROR_TEXT: [RegExp, string][] = [
    [/SMTP/i, "邮件服务暂未配置，无法发送验证码，请联系管理员"],
    [/verification code.*(invalid|expired|incorrect)|invalid verification/i, "验证码错误或已过期"],
    [/turnstile/i, "人机验证失败，请重试"],
];

export function newapiErrorMessage(message?: string): string {
    if (!message) return "请求失败";
    const hit = NEWAPI_ERROR_TEXT.find(([pattern]) => pattern.test(message));
    return hit ? hit[1] : message;
}
