import { test } from "node:test";
import assert from "node:assert/strict";

import { spaceHeader, spaceStorageKey } from "@/lib/space";

test("local caches are kept per space and the personal one keeps its old key", () => {
    assert.equal(spaceStorageKey("infinite-canvas:canvas_store", ""), "infinite-canvas:canvas_store");
    assert.equal(spaceStorageKey("infinite-canvas:canvas_store", "team:7"), "infinite-canvas:canvas_store:team:7");
});

test("the space header names the personal space explicitly", () => {
    assert.equal(spaceHeader(""), "personal");
    assert.equal(spaceHeader("team:7"), "team:7");
});
