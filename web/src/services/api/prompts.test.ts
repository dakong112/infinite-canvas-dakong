import assert from "node:assert/strict";
import test from "node:test";

import { ALL_PROMPTS_OPTION, promptListParams } from "./prompts";

test("prompt list params carry the media tab only when one is picked", () => {
    assert.deepEqual(promptListParams({ media: "video", category: ALL_PROMPTS_OPTION, page: 1 }), { media: "video", page: 1 });
    assert.deepEqual(promptListParams({ media: "image", category: "system", tag: ["运镜"] }), { media: "image", category: "system", tag: ["运镜"] });
    assert.deepEqual(promptListParams({ media: "" }), {});
});
