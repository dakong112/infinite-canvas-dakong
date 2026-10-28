import { apiGet, compactApiParams } from "@/services/api/request";

export type Prompt = {
    id: string;
    title: string;
    coverUrl: string;
    prompt: string;
    tags: string[];
    category: string;
    githubUrl: string;
    preview: string;
    videoUrl: string;
    media: PromptMedia;
    createdAt: string;
    updatedAt: string;
};

export const ALL_PROMPTS_OPTION = "全部";

/** "" is image prompts; "video" prompts carry a preview clip in videoUrl. */
export type PromptMedia = "" | "video";
/** Library tabs: "" shows everything. */
export type PromptMediaFilter = "" | "image" | "video";
export const promptMediaOptions: { label: string; value: PromptMediaFilter }[] = [
    { label: "全部", value: "" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
];

export type PromptListResponse = {
    items: Prompt[];
    tags: string[];
    categories: string[];
    total: number;
};

type PromptListOptions = { keyword?: string; tag?: string[]; category?: string; media?: PromptMediaFilter; page?: number; pageSize?: number };

export function promptListParams({ keyword = "", tag = [], category = ALL_PROMPTS_OPTION, media = "", page, pageSize }: PromptListOptions = {}) {
    return compactApiParams({
        ...(keyword ? { keyword } : {}),
        ...(tag.length ? { tag } : {}),
        ...(category !== ALL_PROMPTS_OPTION ? { category } : {}),
        ...(media ? { media } : {}),
        ...(page ? { page } : {}),
        ...(pageSize ? { pageSize } : {}),
    });
}

export async function fetchPrompts(options: PromptListOptions = {}) {
    return apiGet<PromptListResponse>("/api/prompts", promptListParams(options));
}

export function formatPromptDate(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "" : new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" }).format(date);
}
