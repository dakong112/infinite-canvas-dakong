import { test } from "node:test";
import assert from "node:assert/strict";

import { cleanVideoPrompt, videoPromptMessages } from "@/lib/video-prompt";

test("videoPromptMessages lists referenced assets by kind and order", () => {
    const [system, user] = videoPromptMessages(" 猫咪跳上沙发 ", { images: [{ name: "cat.png" }, { name: "sofa.jpg" }], audios: [{ name: "bgm.mp3" }] });
    assert.equal(system.role, "system");
    assert.equal(user.content, "猫咪跳上沙发\n\n参考素材：\n图片1：cat.png\n图片2：sofa.jpg\n音频1：bgm.mp3");
    assert.equal(videoPromptMessages("海边日落")[1].content, "海边日落");
});

test("cleanVideoPrompt strips fences, labels and quotes", () => {
    assert.equal(cleanVideoPrompt("```text\n镜头缓慢推进\n```"), "镜头缓慢推进");
    assert.equal(cleanVideoPrompt("提示词：“镜头缓慢推进”"), "镜头缓慢推进");
    assert.equal(cleanVideoPrompt("  镜头缓慢推进，暖色调  "), "镜头缓慢推进，暖色调");
});
