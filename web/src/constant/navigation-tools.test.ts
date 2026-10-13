import { test } from "node:test";
import assert from "node:assert/strict";

import { isNavigationActive, navigationTools } from "@/constant/navigation-tools";

test("home is active only on the exact root path", () => {
    assert.equal(isNavigationActive("/", "/"), true);
    assert.equal(isNavigationActive("/image", "/"), false);
});

test("sections are active on their own path and sub paths", () => {
    assert.equal(isNavigationActive("/canvas", "/canvas"), true);
    assert.equal(isNavigationActive("/canvas/abc", "/canvas"), true);
    assert.equal(isNavigationActive("/canvasx", "/canvas"), false);
});

test("the broken music entry is gone and every entry has an href", () => {
    assert.equal(navigationTools.some((tool) => (tool.href as string) === "/music"), false);
    assert.ok(navigationTools.every((tool) => tool.href.startsWith("/")));
});
