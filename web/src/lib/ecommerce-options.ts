export const platformOptions = ["淘宝", "天猫", "抖音", "小红书", "拼多多", "京东", "阿里巴巴", "亚马逊", "TikTok Shop", "Shopee", "Temu"].map((value) => ({ value, label: value }));

export const marketOptions = ["中国大陆", "港澳台", "美国", "欧洲", "日本", "韩国", "东南亚", "中东", "拉美"].map((value) => ({ value, label: value }));

/** Values are the backend text language keys (1688 TextLang). */
export const languageOptions = [
    { value: "zh", label: "简体中文" },
    { value: "zh-Hant", label: "繁体中文" },
    { value: "en", label: "英文" },
    { value: "ja", label: "日文" },
    { value: "th", label: "泰文" },
    { value: "ru", label: "俄文" },
    { value: "es", label: "西班牙语" },
    { value: "none", label: "无文字" },
];

/** The R-SET-4 standard set image types; keys match the backend ecommerceSlotTypes. */
export const slotTypes = [
    { key: "white", title: "白底主图" },
    { key: "brand", title: "品牌主视觉海报" },
    { key: "selling", title: "核心卖点海报" },
    { key: "material", title: "材质与结构说明图" },
    { key: "craft", title: "工艺细节图" },
    { key: "spec", title: "规格与品质信任图" },
    { key: "scene", title: "真实使用场景图" },
    { key: "closing", title: "收官价值视觉图" },
];

export const defaultCustomCounts: Record<string, number> = Object.fromEntries(slotTypes.map((type) => [type.key, 1]));

const MIN_SET_SLOTS = 7;
const MAX_SET_SLOTS = 15;
const SMART_SET_SLOTS = 9;
const DETAIL_SLOTS = 7;

export function structureTotal(counts: Record<string, number>) {
    return slotTypes.reduce((sum, type) => sum + Math.max(0, Math.floor(counts[type.key] || 0)), 0);
}

export function structureError(counts: Record<string, number>) {
    const total = structureTotal(counts);
    if (total < MIN_SET_SLOTS) return `至少 ${MIN_SET_SLOTS} 张，当前 ${total} 张`;
    if (total > MAX_SET_SLOTS) return `最多 ${MAX_SET_SLOTS} 张，当前 ${total} 张`;
    return "";
}

/** Images a preview will generate before the server has laid out the slots (the detail page offers no SKUs). */
export function setSlotEstimate(preset: "set" | "detail", mode: "smart" | "custom", counts: Record<string, number>, skuCount: number) {
    if (preset === "detail") return DETAIL_SLOTS;
    return (mode === "custom" ? structureTotal(counts) : SMART_SET_SLOTS) + skuCount;
}
