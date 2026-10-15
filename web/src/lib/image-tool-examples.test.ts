import assert from "node:assert/strict";
import test from "node:test";

import { imageTool, imageTools, toolExamples } from "./image-tool-examples";

test("placeholders: 8 set images, 7 detail screens, before/after for single-image tools", () => {
    assert.equal(imageTools.length, 6);
    assert.deepEqual(toolExamples("set").items.map((item) => item.label).slice(0, 1), ["白底主图"]);
    assert.equal(toolExamples("set").items.length, 8);
    assert.equal(toolExamples("set").items[7].label, "收官价值视觉图");
    assert.equal(toolExamples("detail").items.length, 7);
    for (const key of ["replicate", "remove-bg", "tryon"] as const) assert.equal(imageTool(key).layout === "compare" && toolExamples(key).items.length, 2);
    assert.deepEqual(toolExamples("set", null), { cover: undefined, items: toolExamples("set").items });
});

test("uploaded examples replace placeholders; cover falls back to the first one", () => {
    const items = [{ label: "原图", url: "/a.png" }, { label: "去背景后", url: "/b.png" }];
    assert.deepEqual(toolExamples("remove-bg", { "remove-bg": { cover: "/c.png", items } }), { cover: "/c.png", items });
    assert.deepEqual(toolExamples("remove-bg", { "remove-bg": { cover: "", items } }), { cover: "/a.png", items });
    // Entries without an image are ignored.
    assert.equal(toolExamples("set", { set: { items: [{ label: "x" }] } }).items.length, 8);
    assert.equal(toolExamples("free", { free: { cover: "/f.png" } }).cover, "/f.png");
});

test("before/after examples expand into a before cell and a result cell on compare tools only", () => {
    const items = [{ label: "", url: "/after.png", before: "/before.png" }, { label: "单图", url: "/one.png" }];
    assert.deepEqual(toolExamples("remove-bg", { "remove-bg": { items } }), {
        cover: "/after.png",
        items: [{ label: "原图", url: "/before.png" }, { label: "去背景后", url: "/after.png" }, { label: "单图", url: "/one.png" }],
    });
    assert.deepEqual(toolExamples("set", { set: { items } }).items, items);
});
