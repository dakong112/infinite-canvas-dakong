import { test } from "node:test";
import assert from "node:assert/strict";

import { catalogTotal } from "@/lib/model-catalog";
import { composeOutput, defaultVideoSettings, initialStep, sameShots, shotCostSteps, shotStages, stepProblem, storyboardProblem, storyboardSeconds, templatePrefill, videoDefaultsProblem } from "@/lib/product-video";
import type { ModelCatalog } from "@/services/api/account";
import type { GenerationJob } from "@/services/api/jobs";
import type { ProductVideoShot } from "@/services/api/product-video";

const shot = (seconds: number, extra: Partial<ProductVideoShot> = {}): ProductVideoShot => ({
    id: "",
    seconds,
    scene: "画面",
    camera: "",
    voiceover: "",
    subtitle: "",
    keyframeJobId: "",
    videoJobId: "",
    audioJobId: "",
    error: "",
    ...extra,
});

test("each step checks its own fields", () => {
    const form = { productFileIds: [], name: "", intro: "", sellingPoints: "" };
    const state = { form, settings: defaultVideoSettings, shots: [], maxShotSeconds: 5 };
    assert.equal(stepProblem(0, state), "请上传 1-6 张产品图");
    assert.equal(stepProblem(0, { ...state, form: { ...form, productFileIds: ["f1"] } }), "产品名称必填（≤50 字）");
    assert.equal(stepProblem(0, { ...state, form: { ...form, productFileIds: ["f1"], name: " 杯子 " } }), "");
    assert.equal(stepProblem(1, state), "");
    assert.equal(stepProblem(1, { ...state, settings: { ...defaultVideoSettings, style: "custom", styleText: " " } }), "自定义风格需填写 ≤300 字说明");
    assert.equal(stepProblem(1, { ...state, settings: { ...defaultVideoSettings, duration: 20 } }), "视频时长需为 15 / 30 / 45 / 60 秒");
    assert.equal(stepProblem(2, state), "分镜数量需为 1-20 个");
    assert.equal(stepProblem(3, state), "");
});

test("storyboard duration sums shots and stays within the video length", () => {
    assert.equal(storyboardSeconds([shot(5), shot(4), shot(3)]), 12);
    assert.equal(storyboardProblem([shot(5), shot(5), shot(5)], 15, 5), "");
    assert.equal(storyboardProblem([shot(5), shot(5), shot(5), shot(2)], 15, 5), "分镜总时长 17 秒超过视频时长 15 秒");
    assert.equal(storyboardProblem([shot(6)], 15, 5), "第 1 镜时长需为 2-5 秒");
    assert.equal(storyboardProblem([shot(3), shot(1)], 15, 5), "第 2 镜时长需为 2-5 秒");
    assert.match(storyboardProblem([shot(3, { scene: " " })], 15, 5), /画面描述必填/);
});

test("opening step and storyboard edits", () => {
    assert.equal(initialStep({ shots: [], composeJobId: "" }), 2);
    assert.equal(initialStep({ shots: [shot(3)], composeJobId: "" }), 2);
    assert.equal(initialStep({ shots: [shot(3, { keyframeJobId: "j1" })], composeJobId: "" }), 3);
    assert.ok(sameShots([shot(3, { id: "s1", scene: "a " })], [{ id: "s1", seconds: 3, scene: "a", camera: "", voiceover: "", subtitle: "" }]));
    assert.ok(!sameShots([shot(3, { id: "s1" })], [shot(4, { id: "s1" })]));
});

test("price steps add keyframes, clips and voiceover, and hide when a model can't be priced", () => {
    const models = { model: "veo", keyframeModel: "img", ttsModel: "tts" };
    const shots = [{ voiceover: "你好世界" }, { voiceover: "" }];
    assert.deepEqual(shotCostSteps(models, shots, "alloy"), [
        { model: "img", count: 2 },
        { model: "veo", count: 2 },
        { model: "tts", usage: { input: 4, output: 0 } },
    ]);
    assert.deepEqual(shotCostSteps({ ...models, keyframeModel: "" }, shots, ""), [{ model: "veo", count: 2 }]);
    assert.deepEqual(shotCostSteps(models, [], "alloy"), []);
    const catalog = {
        platform: { name: "", defaults: { group: "", image: "", text: "", video: "", audio: "" } },
        userGroup: "default",
        groups: [{ name: "default", desc: "", ratio: 1 }],
        vendors: [],
        models: [
            { name: "img", vendorId: 0, kind: "image", groups: ["default"], quotaType: 1, modelPrice: 0.5, modelRatio: 0 },
            { name: "veo", vendorId: 0, kind: "video", groups: ["default"], quotaType: 1, modelPrice: 2, modelRatio: 0 },
        ],
        creditsPerUnit: 1,
    } satisfies ModelCatalog;
    assert.equal(catalogTotal(catalog, "default", shotCostSteps(models, shots, "")), 5);
    assert.equal(catalogTotal(catalog, "default", shotCostSteps(models, shots, "alloy")), null);
});

test("shot stages follow the jobs and compose output parses", () => {
    const jobs = new Map<string, GenerationJob>([
        ["k", { id: "k", status: "succeeded" } as GenerationJob],
        ["v", { id: "v", status: "polling" } as GenerationJob],
    ]);
    const models = { model: "veo", keyframeModel: "img", ttsModel: "tts" };
    const stages = shotStages(shot(3, { keyframeJobId: "k", videoJobId: "v", voiceover: "旁白" }), jobs, models, "alloy");
    assert.deepEqual(
        stages.map((stage) => `${stage.key}:${stage.status}`),
        ["keyframe:done", "video:running", "audio:idle"],
    );
    assert.deepEqual(
        shotStages(shot(3, { error: "提交失败" }), jobs, { ...models, keyframeModel: "" }, "").map((stage) => `${stage.key}:${stage.status}`),
        ["video:failed"],
    );
    assert.equal(composeOutput({ outputText: "" } as GenerationJob), null);
    assert.equal(composeOutput({ outputText: "oops" } as GenerationJob), null);
    assert.equal(composeOutput({ outputText: '{"subtitleUrl":"/api/files/s/content","subtitlesBurned":false}' } as GenerationJob)?.subtitlesBurned, false);
});

test("templates prefill copy, settings and shots, and admin defaults are checked", () => {
    const prefill = templatePrefill(
        { duration: 15, ratio: "1:1", style: "promo", name: "杯子", intro: "介绍", sellingPoints: "卖点", shots: [{ seconds: 3, scene: "开场", camera: "推近", voiceover: "来了", subtitle: "大促" }] },
        { ...defaultVideoSettings, style: "custom", styleText: "旧" },
    );
    assert.deepEqual(prefill.copy, { name: "杯子", intro: "介绍", sellingPoints: "卖点" });
    assert.equal(prefill.settings.duration, 15);
    assert.equal(prefill.settings.ratio, "1:1");
    assert.equal(prefill.settings.style, "promo");
    assert.equal(prefill.settings.styleText, "");
    assert.equal(prefill.settings.resolution, "720p");
    assert.deepEqual(prefill.shots, [{ seconds: 3, scene: "开场", camera: "推近", voiceover: "来了", subtitle: "大促" }]);
    assert.equal(templatePrefill({ duration: 20, ratio: "4:3", style: "x", name: "", intro: "", sellingPoints: "", shots: [] }, defaultVideoSettings).settings.duration, 30);

    assert.equal(videoDefaultsProblem({ duration: 15, ratio: "9:16", style: "scene" }), "");
    assert.equal(videoDefaultsProblem({ duration: 15, style: "custom" }), "视频风格无效");
    assert.equal(videoDefaultsProblem({ duration: 15, shots: [{ seconds: 10, scene: "a", camera: "", voiceover: "", subtitle: "" }, { seconds: 10, scene: "b", camera: "", voiceover: "", subtitle: "" }] }), "分镜总时长 20 秒超过视频时长 15 秒");
});
