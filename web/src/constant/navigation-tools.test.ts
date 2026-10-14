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

test("my works sits right after my assets", () => {
    const keys = navigationTools.map((tool) => tool.key);
    assert.equal(keys.indexOf("works"), keys.indexOf("assets") + 1);
    assert.equal(navigationTools.find((tool) => tool.key === "works")?.href, "/works");
});
