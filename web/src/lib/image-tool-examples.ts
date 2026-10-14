import { slotTypes } from "@/lib/ecommerce-options";

export type ImageToolKey = "set" | "detail" | "replicate" | "remove-bg" | "tryon" | "free";
export type ImageExampleItem = { label: string; url?: string };
/** Admin-uploaded examples (`public.imageExamples[tool]`); a bare list is read as the items. */
export type ImageExamplesSetting = Partial<Record<string, { cover?: string; items?: ImageExampleItem[] } | ImageExampleItem[]>>;

type ImageTool = {
    key: ImageToolKey;
    href: string;
    title: string;
    /** One line on the hub card. */
    summary: string;
    /** Heading and sentence of the examples panel. */
    headline: string;
    description: string;
    /** "grid": numbered outputs; "compare": before / after. */
    layout: "grid" | "compare";
    items: string[];
    /** Placeholder gradient while no image is configured. */
    tone: string;
};

export const imageTools: ImageTool[] = [
    {
        key: "set",
        href: "/image/set",
        title: "商品套图",
        summary: "上传商品图，一键生成适配电商平台的整套主图与详情图",
        headline: "AI 商品套图",
        description: "上传商品图，AI 解析卖点并规划整套图片，先低价预览再生成高清正式图。",
        layout: "grid",
        items: slotTypes.map((type) => type.title),
        tone: "from-amber-200/70 via-orange-100 to-rose-100 dark:from-amber-500/25 dark:via-orange-500/10 dark:to-rose-500/15",
    },
    {
        key: "detail",
        href: "/image/detail",
        title: "详情图 / A+",
        summary: "按 7 屏结构规划文案与视觉，生成竖版详情页与 A+ 内容",
        headline: "AI 详情图 / A+",
        description: "从品牌主视觉到收官价值，逐屏规划文案与画面，生成完整的竖版详情页。",
        layout: "grid",
        items: ["品牌主视觉", "核心卖点", "材质与结构", "工艺细节", "规格与品质", "使用场景", "收官价值"],
        tone: "from-sky-200/70 via-indigo-100 to-violet-100 dark:from-sky-500/25 dark:via-indigo-500/10 dark:to-violet-500/15",
    },
    {
        key: "replicate",
        href: "/image/replicate",
        title: "图片复刻",
        summary: "参考一张爆款图的风格与构图，生成你自己的商品图",
        headline: "AI 图片复刻",
        description: "上传一张参考图和你的商品图，保留参考图的风格、构图与光影，换成你的商品。",
        layout: "compare",
        items: ["参考图", "复刻结果"],
        tone: "from-emerald-200/70 via-teal-100 to-cyan-100 dark:from-emerald-500/25 dark:via-teal-500/10 dark:to-cyan-500/15",
    },
    {
        key: "remove-bg",
        href: "/image/remove-bg",
        title: "去除背景",
        summary: "保留商品主体，输出透明底、纯白底或纯色底图片",
        headline: "AI 智能去背景",
        description: "自动识别主体边缘，一次最多处理 4 张，快速得到干净的透明底或纯色底商品图。",
        layout: "compare",
        items: ["原图", "去背景后"],
        tone: "from-slate-200 via-zinc-100 to-stone-100 dark:from-slate-400/20 dark:via-zinc-500/10 dark:to-stone-500/15",
    },
    {
        key: "tryon",
        href: "/image/tryon",
        title: "AI 换装",
        summary: "把服饰穿到模特身上，或为商品添加模特与使用场景",
        headline: "AI 换装 / 模特图",
        description: "上传服饰商品图，可选模特图，AI 生成自然的上身效果或带模特的场景图。",
        layout: "compare",
        items: ["服饰商品", "上身效果"],
        tone: "from-pink-200/70 via-rose-100 to-fuchsia-100 dark:from-pink-500/25 dark:via-rose-500/10 dark:to-fuchsia-500/15",
    },
    {
        key: "free",
        href: "/image/free",
        title: "自由生图",
        summary: "文字描述或参考图自由创作，支持多模型与批量生成",
        headline: "自由生图",
        description: "用文字或参考图自由创作。",
        layout: "grid",
        items: [],
        tone: "from-violet-200/70 via-purple-100 to-blue-100 dark:from-violet-500/25 dark:via-purple-500/10 dark:to-blue-500/15",
    },
];

export function imageTool(key: ImageToolKey) {
    return imageTools.find((tool) => tool.key === key)!;
}

/** Uploaded examples of a tool, or numbered placeholders; the cover falls back to the first uploaded example. */
export function toolExamples(key: ImageToolKey, setting?: ImageExamplesSetting | null): { cover?: string; items: ImageExampleItem[] } {
    const entry = setting?.[key];
    const uploaded = (Array.isArray(entry) ? entry : (entry?.items ?? [])).filter((item) => item?.url);
    const cover = (!Array.isArray(entry) && entry?.cover) || uploaded[0]?.url;
    return { cover, items: uploaded.length ? uploaded : imageTool(key).items.map((label) => ({ label })) };
}
