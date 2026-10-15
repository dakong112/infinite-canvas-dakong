import { test } from "node:test";
import assert from "node:assert/strict";

import { exampleFromJob, imageRefPreview, sampleFilesForTemplate, sampleGroupForLabel, sizeToTemplateParams, templateCost, templatePrefill, templateProblem, templateRunProblem } from "@/lib/image-templates";

const valid = {
    slug: "multi-scene",
    title: "多场景穿搭",
    category: "ecommerce" as const,
    prompt: "把 @图片1 的服装穿到 @图片3 上。{{prompt}}",
    inputs: [
        { key: "product", label: "商品图", max: 2, required: true },
        { key: "model", label: "模特图", max: 1, required: false },
    ],
    ratios: ["1:1"],
    resolutions: ["1K"],
    feeCredits: 0.5,
    examples: [],
};

test("cost adds the per-image template fee unless waived", () => {
    assert.equal(templateCost(1.2, 0.5, 2, false), 2.2);
    assert.equal(templateCost(1.2, 0.5, 2, true), 1.2);
    assert.equal(templateCost(null, 0.5, 2, false), null);
});

test("image refs are numbered across groups by max", () => {
    assert.deepEqual(imageRefPreview(valid.inputs), [
        { label: "商品图", refs: ["@图片1", "@图片2"] },
        { label: "模特图", refs: ["@图片3"] },
    ]);
});

test("template validation mirrors the server", () => {
    assert.equal(templateProblem(valid), "");
    assert.match(templateProblem({ ...valid, slug: "Bad Slug" }), /标识/);
    assert.match(templateProblem({ ...valid, prompt: "@图片4" }), /@图片4/);
    assert.match(templateProblem({ ...valid, inputs: [...valid.inputs, { key: "scene", label: "场景", max: 4, required: false }] }), /合计最多 6/);
    assert.match(templateProblem({ ...valid, inputs: [valid.inputs[0], { ...valid.inputs[1], key: "product" }] }), /唯一/);
    assert.match(templateProblem({ ...valid, inputs: [{ ...valid.inputs[0], max: 5 }] }), /1-4/);
    assert.match(templateProblem({ ...valid, ratios: [] }), /比例/);
    assert.match(templateProblem({ ...valid, feeCredits: -1 }), /模板费/);
});

test("run problems name the first missing input, then the prompt", () => {
    const template = { inputs: valid.inputs, promptRequired: true };
    assert.equal(templateRunProblem(template, {}, ""), "请上传「商品图」");
    assert.equal(templateRunProblem(template, { product: [1] }, " "), "请填写提示词");
    assert.equal(templateRunProblem(template, { product: [1] }, "红色"), "");
});

test("output size maps back to the closest allowed ratio and resolution", () => {
    assert.deepEqual(sizeToTemplateParams(1536, 2048, ["1:1", "3:4", "9:16"], ["1K", "2K"]), { ratio: "3:4", resolution: "2K" });
    assert.deepEqual(sizeToTemplateParams(1024, 1024, ["1:1"], ["1K", "4K"]), { ratio: "1:1", resolution: "1K" });
    assert.deepEqual(sizeToTemplateParams(0, 0, ["1:1"], ["1K"]), {});
});

const samples = { product: ["/api/files/p1/content", "/api/files/p2/content", "https://cdn.example.com/x.png"], portrait: ["/api/files/h1/content"], model: [] as string[] };

test("input labels pick a shared sample group", () => {
    assert.equal(sampleGroupForLabel("人像照片", samples), "portrait");
    assert.equal(sampleGroupForLabel("人物", samples), "portrait");
    assert.equal(sampleGroupForLabel("模特图", samples), "portrait");
    assert.equal(sampleGroupForLabel("模特图", { ...samples, model: ["/api/files/m1/content"] }), "model");
    assert.equal(sampleGroupForLabel("菜品", samples), "product");
    assert.equal(sampleGroupForLabel("背景", samples), "product");
});

test("sample files rotate within a group and report a missing required group", () => {
    const inputs = [
        { key: "a", label: "商品正面", required: true },
        { key: "b", label: "商品背面", required: false },
        { key: "c", label: "商品细节", required: false },
        { key: "d", label: "人像", required: false },
    ];
    assert.deepEqual(sampleFilesForTemplate(inputs, samples), { files: { a: ["p1"], b: ["p2"], c: ["p1"], d: ["h1"] }, missing: "" });
    assert.deepEqual(sampleFilesForTemplate([{ key: "m", label: "模特", required: false }], { ...samples, portrait: [] }), { files: {}, missing: "" });
    assert.equal(sampleFilesForTemplate([{ key: "m", label: "模特", required: true }], { ...samples, portrait: [] }).missing, "模特");
});

test("example jobs become stored-file examples", () => {
    assert.deepEqual(exampleFromJob({ inputFileIds: ["a", "b"], outputFileId: "o", prompt: "白底" }), { inputs: ["/api/files/a/content", "/api/files/b/content"], output: "/api/files/o/content", prompt: "白底" });
});

test("a work prefills a valid draft creative template", () => {
    const prompt = "一只穿着宇航服的橘猫漂浮在空间站里，窗外是蓝色地球，电影级光影";
    const refs = ["/api/files/r1/content", "/api/files/r2/content"];
    const item = templatePrefill({ prompt: ` ${prompt} `, output: "/api/files/o/content", references: refs, width: 1024, height: 1024 }, 0);
    assert.equal(item.title, [...prompt].slice(0, 20).join(""));
    assert.equal(item.prompt, `${prompt}\n补充要求：{{prompt}}`);
    assert.deepEqual(item.inputs, [{ key: "reference", label: "参考图", max: 2, required: true }]);
    assert.deepEqual(item.examples, [{ inputs: refs, output: "/api/files/o/content", prompt: "" }]);
    assert.equal(item.coverUrl, "/api/files/o/content");
    assert.equal(item.category, "creative");
    assert.equal(item.status, "draft");
    assert.deepEqual([item.ratios, item.resolutions], [["1:1"], ["1K"]]);
    assert.equal(templateProblem(item), "");
    const plain = templatePrefill({ prompt: "", output: "/api/files/o/content", references: Array(6).fill("/api/files/r/content") });
    assert.equal(plain.title, "未命名模板");
    assert.equal(plain.inputs?.[0].max, 4);
});
