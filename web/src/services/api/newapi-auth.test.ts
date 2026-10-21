import assert from "node:assert/strict";
import test from "node:test";

import { newapiErrorMessage } from "./newapi-auth";

test("translates the SMTP error users hit when email codes cannot be sent", () => {
    assert.equal(newapiErrorMessage("invalid SMTP account"), "邮件服务暂未配置，无法发送验证码，请联系管理员");
});

test("keeps messages it does not know and falls back when empty", () => {
    assert.equal(newapiErrorMessage("用户名已存在"), "用户名已存在");
    assert.equal(newapiErrorMessage(undefined), "请求失败");
});
