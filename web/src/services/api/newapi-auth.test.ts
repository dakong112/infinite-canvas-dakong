import assert from "node:assert/strict";
import test from "node:test";

import { claimSilentLogin, newapiErrorMessage, SILENT_LOGIN_KEY } from "./newapi-auth";

function memoryStorage() {
    const values = new Map<string, string>();
    return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => void values.set(key, value), values };
}

test("tries the silent new-api check once per tab", () => {
    const storage = memoryStorage();
    assert.equal(claimSilentLogin(storage, false), true);
    assert.equal(claimSilentLogin(storage, false), false);
    // A successful sign-in clears the flag, so an expired session tries again.
    storage.values.delete(SILENT_LOGIN_KEY);
    assert.equal(claimSilentLogin(storage, false), true);
});

test("never retries after new-api answered login_required", () => {
    const storage = memoryStorage();
    assert.equal(claimSilentLogin(storage, true), false);
    assert.equal(claimSilentLogin(storage, false), false);
});

test("translates the SMTP error users hit when email codes cannot be sent", () => {
    assert.equal(newapiErrorMessage("invalid SMTP account"), "邮件服务暂未配置，无法发送验证码，请联系管理员");
});

test("keeps messages it does not know and falls back when empty", () => {
    assert.equal(newapiErrorMessage("用户名已存在"), "用户名已存在");
    assert.equal(newapiErrorMessage(undefined), "请求失败");
});
