import { encryptPassword } from "@/lib/password-encryption";
import type { AuthSession } from "@/services/api/auth";
import { apiGet, apiPost } from "@/services/api/request";

export type NewapiAuthConfig = { enabled: boolean; newapiUrl?: string; clientId?: string; redirectUri?: string };

export type NewapiStatus = {
    turnstile_check: boolean;
    email_verification: boolean;
    register_enabled: boolean;
    password_register_enabled: boolean;
    password_login_enabled: boolean;
    password_login_encryption_enabled: boolean;
};

type Envelope<T> = { success: boolean; message?: string; data?: T };

export type LoginChallenge = { require_verification: true; flow_token: string; methods: { method: string; available?: boolean }[] };
type LoginSuccess = { access_token: string };

export const LOGIN_REDIRECT_KEY = "ic-login-redirect";

export function getNewapiAuthConfig() {
    return apiGet<NewapiAuthConfig>("/api/auth/newapi/config");
}

async function newapiFetch<T>(base: string, path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(`${base}${path}`, {
        ...init,
        headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    });
    const body = (await response.json()) as Envelope<T>;
    if (!body.success) throw new Error(body.message || "请求失败");
    return body.data as T;
}

export function getNewapiStatus(base: string) {
    return newapiFetch<NewapiStatus>(base, "/api/status");
}

/** Returns an access token, or a challenge when the account requires 2FA. */
export async function newapiLogin(base: string, status: NewapiStatus, username: string, password: string): Promise<string | LoginChallenge> {
    const payload = status.password_login_encryption_enabled
        ? { username, ...(await encryptPassword(base, password)) }
        : { username, password };
    const data = await newapiFetch<LoginSuccess | LoginChallenge>(base, "/api/user/login", { method: "POST", body: JSON.stringify(payload) });
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

export function newapiSendEmailCode(base: string, email: string) {
    return newapiFetch<unknown>(base, `/api/verification?email=${encodeURIComponent(email)}`);
}

export function newapiRegister(base: string, payload: { username: string; password: string; email: string; verification_code: string; aff_code?: string }) {
    return newapiFetch<unknown>(base, "/api/user/register", { method: "POST", body: JSON.stringify(payload) });
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
