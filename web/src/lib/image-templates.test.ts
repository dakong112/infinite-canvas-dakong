import { test } from "node:test";
import assert from "node:assert/strict";

import { imageRefPreview, sizeToTemplateParams, templateCost, templateProblem, templateRunProblem } from "@/lib/image-templates";

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
