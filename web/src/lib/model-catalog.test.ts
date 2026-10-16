import assert from "node:assert/strict";
import test from "node:test";

import type { ModelCatalog } from "@/services/api/account";
import { catalogCredits, catalogPriceLabel, catalogTotal, typicalUsage, catalogVendors, defaultPlatformChannel, formatGroupRatio, groupLabel, isPlatformChannelId, platformChannels, platformGroupOf, ratioTone } from "./model-catalog";

const catalog: ModelCatalog = {
    platform: { name: "紫紫 AI", defaults: { group: "default", image: "gpt-image-1", text: "deepseek-chat", video: "", audio: "" } },
    userGroup: "vip",
    groups: [
        { name: "vip", desc: "VIP 分组", ratio: 0.65 },
        { name: "default", desc: "默认分组", ratio: 1 },
        { name: "svip", desc: "", ratio: 1.5 },
    ],
    vendors: [{ id: 1, name: "OpenAI" }, { id: 2, name: "DeepSeek" }, { id: 3, name: "Kling" }],
    models: [
        { name: "gpt-image-1", vendorId: 1, kind: "image", groups: ["vip", "default"], quotaType: 1, modelPrice: 0.2, modelRatio: 0 },
        { name: "deepseek-chat", vendorId: 2, kind: "", groups: ["default"], quotaType: 0, modelPrice: 0, modelRatio: 0.5, completionRatio: 4 },
        { name: "sora-2", vendorId: 0, kind: "video", groups: ["svip"], quotaType: 1, modelPrice: 1, modelRatio: 0 },
    ],
    creditsPerUnit: 10,
    quotaPerUnit: 500000,
};

test("one virtual channel per group; only known kinds become capabilities", () => {
    const channels = platformChannels(catalog);
    assert.deepEqual(channels.map((channel) => channel.id), ["newapi@vip", "newapi@default", "newapi@svip"]);
    assert.equal(channels[0].name, "紫紫 AI · vip");
    assert.deepEqual(channels[1].models, ["gpt-image-1", "deepseek-chat"]);
    assert.deepEqual(channels[1].modelCapabilities, { "gpt-image-1": "image" });
    assert.equal(platformGroupOf("newapi@vip"), "vip");
    assert.equal(platformGroupOf("local-1"), "");
    assert.equal(isPlatformChannelId("local-1"), false);
});

test("fixed price × group ratio × credits per unit; usage-based models show the ratio", () => {
    // 0.2 × 0.65 × 10 × 3 = 3.9
    assert.equal(catalogCredits(catalog, "gpt-image-1", "vip", 3), 3.9);
    assert.deepEqual(catalogPriceLabel(catalog, "gpt-image-1", "vip", "积分"), { main: "≈1.3 积分/次", base: "原价 2" });
    assert.deepEqual(catalogPriceLabel(catalog, "gpt-image-1", "default", "积分"), { main: "≈2 积分/次", base: "" });
    assert.deepEqual(catalogPriceLabel(catalog, "deepseek-chat", "vip", "积分"), { main: "按量 · ×0.65", base: "" });
    assert.equal(catalogCredits(catalog, "deepseek-chat", "default"), null);
    assert.equal(catalogCredits(catalog, "missing", "default"), null);
});

test("ratio tone: below 1 is a discount, above 1 a surcharge", () => {
    assert.equal(ratioTone(0.65), "low");
    assert.equal(ratioTone(1), "normal");
    assert.equal(ratioTone(1.5), "high");
});

test("vendors follow the models in view; default channel prefers the admin group, then the account group", () => {
    assert.deepEqual(catalogVendors(catalog, ["gpt-image-1", "deepseek-chat"]).map((vendor) => vendor.name), ["OpenAI", "DeepSeek"]);
    assert.equal(defaultPlatformChannel(catalog, "gpt-image-1"), "newapi@default");
    assert.equal(defaultPlatformChannel(catalog, "sora-2"), "newapi@svip");
    assert.equal(defaultPlatformChannel(catalog, "missing"), "");
});

test("usage-billed models are estimated from typical usage; any unpriced step hides the total", () => {
    // (3000 + 1000 × 4) × 0.5 / 500000 × 0.65 × 10 = 0.0455
    assert.equal(catalogCredits(catalog, "deepseek-chat", "vip", 1, typicalUsage.analyze), 0.05);
    // (6000 + 3000 × 4) × 0.5 / 500000 × 1 × 10 × 2 = 0.36
    assert.equal(catalogCredits(catalog, "deepseek-chat", "default", 2, typicalUsage.plan), 0.36);
    // Without new-api's quota per unit there is no estimate.
    assert.equal(catalogCredits({ ...catalog, quotaPerUnit: 0 }, "deepseek-chat", "vip", 1, typicalUsage.analyze), null);
    // Fixed-price models ignore usage; completion ratio defaults to 1.
    assert.equal(catalogCredits(catalog, "gpt-image-1", "default", 1, typicalUsage.plan), 2);
    const noCompletion = { ...catalog, models: [{ ...catalog.models[1], completionRatio: 0 }] };
    assert.equal(catalogCredits(noCompletion, "deepseek-chat", "default", 1, { input: 1000, output: 1000 }), 0.02);
    // analyze + plan + 9 previews in the default group: 0.07 + 0.18 + 18 = 18.25
    assert.equal(catalogTotal(catalog, "default", [{ model: "deepseek-chat", usage: typicalUsage.analyze }, { model: "deepseek-chat", usage: typicalUsage.plan }, { model: "gpt-image-1", count: 9 }]), 18.25);
    assert.equal(catalogTotal(catalog, "default", [{ model: "gpt-image-1" }, { model: "missing" }]), null);
    assert.equal(catalogTotal(catalog, "default", []), 0);
});

test("the auto group reads 自动 and shows its ratio as decided per call", () => {
    const withAuto: ModelCatalog = { ...catalog, groups: [...catalog.groups, { name: "auto", desc: "自动", ratio: 1 }], models: catalog.models.map((model) => ({ ...model, groups: [...model.groups, "auto"] })) };
    assert.equal(platformChannels(withAuto).at(-1)?.name, "紫紫 AI · 自动");
    assert.equal(groupLabel("auto"), "自动");
    assert.equal(formatGroupRatio("auto", 1), "按实际分组");
    assert.equal(formatGroupRatio("vip", 0.65), "×0.65");
    assert.deepEqual(catalogPriceLabel(withAuto, "deepseek-chat", "auto", "积分").main, "按量 · 按实际分组");
});
