import assert from "node:assert/strict";
import test from "node:test";

import { claimSilentLogin, markSilentLogin, newapiErrorMessage, SILENT_LOGIN_KEY, silentLoginTarget } from "./newapi-auth";

function memoryStorage() {
    const values = new Map<string, string>();
    return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => void values.set(key, value), values };
}

const home = { pathname: "/", search: "" };

test("tries the silent new-api check at most once a minute per tab", () => {
    const storage = memoryStorage();
    assert.equal(claimSilentLogin(storage, home, 1_000), true);
    assert.equal(claimSilentLogin(storage, { pathname: "/image", search: "" }, 30_000), false);
    assert.equal(claimSilentLogin(storage, home, 60_999), false);
    assert.equal(claimSilentLogin(storage, home, 61_000), true);
    // A successful sign-in or "登录已失效" clears the stamp, so the next page asks again at once.
    storage.values.delete(SILENT_LOGIN_KEY);
    assert.equal(claimSilentLogin(storage, home, 62_000), true);
    // Logging out stamps it: no check while new-api may still be signed in.
    markSilentLogin(storage, 200_000);
    assert.equal(claimSilentLogin(storage, home, 230_000), false);
});

test("never checks on the SSO callback or after a failed check", () => {
    const storage = memoryStorage();
    assert.equal(claimSilentLogin(storage, { pathname: "/sso/callback", search: "?code=x" }), false);
    assert.equal(claimSilentLogin(storage, { pathname: "/login", search: "?silent=failed&redirect=%2F" }), false);
    assert.equal(storage.values.has(SILENT_LOGIN_KEY), false);
    assert.equal(claimSilentLogin(storage, { pathname: "/login", search: "?redirect=%2F" }), true);
});

test("returns to public pages when nobody is signed in at new-api, shows the form otherwise", () => {
    assert.equal(silentLoginTarget("/image?tab=1", false), "/image?tab=1");
    assert.equal(silentLoginTarget("/", false), "/");
    assert.equal(silentLoginTarget("/asset-library", false), "/login?silent=failed&redirect=%2Fasset-library");
    assert.equal(silentLoginTarget("/admin/users", false), "/login?silent=failed&redirect=%2Fadmin%2Fusers");
    assert.equal(silentLoginTarget("/login?redirect=%2Fworks", false), "/login?silent=failed&redirect=%2Fworks");
    assert.equal(silentLoginTarget("/login?redirect=%2F%2Fevil.com", false), "/login?silent=failed&redirect=%2F");
});

test("signed in, goes to the page it started from (the login page's own target)", () => {
    assert.equal(silentLoginTarget("/image?tab=1", true), "/image?tab=1");
    assert.equal(silentLoginTarget("/admin", true), "/admin");
    assert.equal(silentLoginTarget("/login?redirect=%2Fworks", true), "/works");
});

test("translates the SMTP error users hit when email codes cannot be sent", () => {
    assert.equal(newapiErrorMessage("invalid SMTP account"), "邮件服务暂未配置，无法发送验证码，请联系管理员");
});

test("keeps messages it does not know and falls back when empty", () => {
    assert.equal(newapiErrorMessage("用户名已存在"), "用户名已存在");
    assert.equal(newapiErrorMessage(undefined), "请求失败");
});
