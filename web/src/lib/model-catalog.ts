import type { ModelCatalog } from "@/services/api/account";
import type { ModelCapabilities } from "@/stores/use-config-store";

/** Virtual channel id of one new-api group; the backend swaps in that group's key. */
export const PLATFORM_CHANNEL_PREFIX = "newapi@";

export function platformChannelId(group: string) {
    return `${PLATFORM_CHANNEL_PREFIX}${group}`;
}

export function isPlatformChannelId(channelId?: string) {
    return Boolean(channelId?.startsWith(PLATFORM_CHANNEL_PREFIX));
}

export function platformGroupOf(channelId?: string) {
    return channelId && isPlatformChannelId(channelId) ? channelId.slice(PLATFORM_CHANNEL_PREFIX.length) : "";
}

/** One channel per group, shaped like a public channel so the picker and request routing treat it as remote. Models without a kind are left to the name rules. */
export function platformChannels(catalog: ModelCatalog) {
    return catalog.groups.map((group) => {
        const models = catalog.models.filter((model) => model.groups.includes(group.name));
        const modelCapabilities: ModelCapabilities = {};
        for (const model of models) if (model.kind) modelCapabilities[model.name] = model.kind;
        return { id: platformChannelId(group.name), protocol: "openai" as const, name: `${catalog.platform.name || "平台"} · ${group.name}`, models: models.map((model) => model.name), modelCapabilities, enabled: true };
    });
}

export function groupRatio(catalog: ModelCatalog, group: string) {
    return catalog.groups.find((item) => item.name === group)?.ratio ?? 1;
}

export function ratioTone(ratio: number): "low" | "normal" | "high" {
    return ratio < 1 ? "low" : ratio > 1 ? "high" : "normal";
}

export function formatRatio(ratio: number) {
    return `×${Number(ratio.toFixed(2))}`;
}

/** Credits for `count` calls of a fixed-price model in a group, or null for usage-based and unknown models. */
export function catalogCredits(catalog: ModelCatalog, modelName: string, group: string, count = 1): number | null {
    const model = catalog.models.find((item) => item.name === modelName);
    if (!model || model.quotaType !== 1 || model.modelPrice <= 0) return null;
    const calls = Math.max(1, Math.floor(count) || 1);
    return Math.round(model.modelPrice * groupRatio(catalog, group) * catalog.creditsPerUnit * calls * 100) / 100;
}

/** Price column of the picker: "≈x 积分/次" plus the list price when the group changes it, or "按量 · ×ratio". */
export function catalogPriceLabel(catalog: ModelCatalog, modelName: string, group: string, creditName: string) {
    const credits = catalogCredits(catalog, modelName, group);
    if (credits === null) return { main: `按量 · ${formatRatio(groupRatio(catalog, group))}`, base: "" };
    const model = catalog.models.find((item) => item.name === modelName)!;
    const base = Math.round(model.modelPrice * catalog.creditsPerUnit * 100) / 100;
    return { main: `≈${credits > 0 ? credits : "<0.01"} ${creditName}/次`, base: base === credits ? "" : `原价 ${base}` };
}

/** Vendors of the given models, in catalog order; models without a vendor show as "其他" and get no chip. */
export function catalogVendors(catalog: ModelCatalog, modelNames: string[]) {
    const ids = new Set(catalog.models.filter((model) => modelNames.includes(model.name)).map((model) => model.vendorId));
    return catalog.vendors.filter((vendor) => ids.has(vendor.id));
}

/** Channel for a model: the admin's default group, then the account group, then the first group offering it. */
export function defaultPlatformChannel(catalog: ModelCatalog, modelName: string) {
    const groups = catalog.models.find((model) => model.name === modelName)?.groups || [];
    const group = [catalog.platform.defaults.group, catalog.userGroup].find((name) => name && groups.includes(name)) || groups[0];
    return group ? platformChannelId(group) : "";
}
