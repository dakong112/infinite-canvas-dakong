import assert from "node:assert/strict";
import test from "node:test";

import { isVisionModel } from "./vision-models";

test("known vision families are tagged", () => {
    for (const name of [
        "gpt-4o",
        "gpt-4o-mini",
        "gpt-4.1-mini",
        "gpt-5",
        "o3",
        "o4-mini",
        "openai/o1",
        "gemini-2.5-flash",
        "claude-sonnet-4-5",
        "qwen-vl-max",
        "qwen2.5-vl-72b-instruct",
        "Qwen/Qwen2.5-VL-7B-Instruct",
        "qvq-max",
        "glm-4v-plus",
        "glm-4.5v",
        "doubao-1.5-vision-pro",
        "doubao-seed-1.6-vision",
        "moonshot-v1-8k-vision-preview",
        "kimi-vl-a3b",
        "internvl2.5",
        "llava-v1.6",
    ]) {
        assert.equal(isVisionModel(name), true, name);
    }
});

test("text-only models are not tagged", () => {
    for (const name of ["deepseek-chat", "gpt-3.5-turbo", "qwen-max", "glm-4-plus", "doubao-pro-32k", "moonshot-v1-8k", "o3pro-like", "llama-3-70b", "gpt-image-1"]) {
        assert.equal(isVisionModel(name), false, name);
    }
});
