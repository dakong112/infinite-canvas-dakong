import assert from "node:assert/strict";
import test from "node:test";

import type { ModelCatalog } from "@/services/api/account";
import type { AdminPublicSettings } from "@/services/api/admin";
import { channelIdForActiveModel, channelRouteHeader, defaultConfig, isOwnChannel, resolvePlatformConfig, type AiConfig } from "./use-config-store";

const catalog: ModelCatalog = {
    platform: { name: "紫紫 AI", defaults: { group: "default", image: "gpt-image-1", text: "deepseek-chat", video: "", audio: "" } },
    userGroup: "vip",
    groups: [
        { name: "vip", desc: "", ratio: 0.65 },
        { name: "default", desc: "", ratio: 1 },
    ],
    vendors: [],
    models: [
        { name: "gpt-image-1", vendorId: 1, kind: "image", groups: ["vip", "default"], quotaType: 1, modelPrice: 0.2, modelRatio: 0 },
        { name: "deepseek-chat", vendorId: 2, kind: "", groups: ["default"], quotaType: 0, modelPrice: 0, modelRatio: 0.5 },
    ],
    creditsPerUnit: 10,
};

const modelChannel = { channels: [], allowCustomChannel: true, defaultImageModel: "", defaultVideoModel: "", defaultTextModel: "", systemPrompt: "" } as unknown as AdminPublicSettings["modelChannel"];
const ownChannel = { id: "local-1", protocol: "openai" as const, name: "我的", baseUrl: "https://x.test", apiKey: "sk-1", models: ["flux-pro"] };
const config: AiConfig = { ...defaultConfig, channelMode: "local", localChannels: [ownChannel], imageChannelId: "local-1" };

test("SSO config merges group channels with own channels and falls back to platform defaults", () => {
    const resolved = resolvePlatformConfig(config, modelChannel, catalog);
    assert.equal(resolved.channelMode, "remote");
    assert.deepEqual(resolved.publicChannels.map((channel) => [channel.id, Boolean(channel.local)]), [["newapi@vip", false], ["newapi@default", false], ["local-1", true]]);
    // gpt-image-2 (the stored model) is not offered, so the platform default wins and is routed to the admin's default group.
    assert.equal(resolved.imageModel, "gpt-image-1");
    assert.equal(resolved.imageChannelId, "newapi@default");
    assert.equal(resolved.textModel, "deepseek-chat");
    assert.equal(resolved.textChannelId, "newapi@default");
    assert.deepEqual(resolved.imageModels, ["gpt-image-1", "flux-pro"]);
});

test("own channel selection is kept; own channels are hidden when the admin disallows them", () => {
    const kept = resolvePlatformConfig({ ...config, imageModel: "flux-pro" }, modelChannel, catalog);
    assert.equal(kept.imageModel, "flux-pro");
    assert.equal(kept.imageChannelId, "local-1");
    const hidden = resolvePlatformConfig(config, { ...modelChannel, allowCustomChannel: false }, catalog);
    assert.deepEqual(hidden.publicChannels.map((channel) => channel.id), ["newapi@vip", "newapi@default"]);
});

test("route header follows the selected channel", () => {
    const resolved = resolvePlatformConfig({ ...config, imageModel: "flux-pro" }, modelChannel, catalog);
    assert.deepEqual(channelRouteHeader(resolved, channelIdForActiveModel({ ...resolved, model: "flux-pro" })), { "X-User-Model-Channel-ID": "local-1" });
    assert.deepEqual(channelRouteHeader(resolved, "newapi@vip"), { "X-Model-Channel-ID": "newapi@vip" });
    assert.deepEqual(channelRouteHeader(resolved, ""), {});
    // Outside SSO: local mode always uses the user header, remote admin channels the platform header.
    assert.equal(isOwnChannel(config, "local-1"), true);
    assert.deepEqual(channelRouteHeader({ ...config, channelMode: "remote", publicChannels: [{ id: "admin-1", models: [] }] }, "admin-1"), { "X-Model-Channel-ID": "admin-1" });
});
