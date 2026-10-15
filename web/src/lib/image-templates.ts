import type { ImageTemplate, ImageTemplateInput } from "@/services/api/templates";

// Mirrors service/image_template.go normalizeImageTemplate so the admin form fails before the request does.
export const templateRatios = ["1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "9:16", "16:9"];
export const templateResolutions = ["1K", "2K", "4K"];
export const templateCategoryLabels = { ecommerce: "电商专区", creative: "创意应用" } as const;
const resolutionPx: Record<string, number> = { "1K": 1024, "2K": 2048, "4K": 2880 };
const maxInputGroups = 4;
const maxInputFiles = 6;
const maxUserPrompt = 2000;

const runes = (value: string) => [...value].length;

/** Credits for a run: model estimate for all images plus the per-image template fee unless waived; null when the model can't be priced. */
export function templateCost(modelCredits: number | null, feeCredits: number, count: number, feeWaived: boolean) {
    if (modelCredits === null) return null;
    return Math.round((modelCredits + (feeWaived ? 0 : feeCredits * count)) * 100) / 100;
}

/** "@图片N" references each input group gets when every slot is filled (the server numbers actual uploads in input order). */
export function imageRefPreview(inputs: Pick<ImageTemplateInput, "label" | "max">[]) {
    let next = 1;
    return inputs.map((input) => ({ label: input.label, refs: Array.from({ length: Math.max(0, input.max || 0) }, () => `@图片${next++}`) }));
}

/** Why the admin template can't be saved, or "" when the server will accept it. */
export function templateProblem(item: Partial<ImageTemplate>) {
    const inputs = item.inputs ?? [];
    const prompt = (item.prompt ?? "").trim();
    if (!/^[a-z0-9-]{2,64}$/.test((item.slug ?? "").trim())) return "标识只能使用 2-64 位小写字母、数字和 -";
    const title = (item.title ?? "").trim();
    if (!title || runes(title) > 100) return "标题需为 1-100 字";
    if (runes((item.description ?? "").trim()) > 300 || runes((item.promptPlaceholder ?? "").trim()) > 300) return "说明和提示词占位不能超过 300 字";
    if (!item.category || !(item.category in templateCategoryLabels)) return "分类无效";
    if (!prompt || runes(prompt) > 8000) return "提示词需为 1-8000 字";
    if (inputs.length > maxInputGroups) return `最多 ${maxInputGroups} 组输入图片`;
    const keys = new Set<string>();
    let total = 0;
    for (const input of inputs) {
        const key = (input.key ?? "").trim();
        const label = (input.label ?? "").trim();
        if (!/^[a-z0-9_-]{1,32}$/.test(key) || keys.has(key)) return "输入标识需唯一，且只能使用小写字母、数字、_ 和 -";
        if (!label || runes(label) > 20) return "输入名称需为 1-20 字";
        if (!Number.isInteger(input.max) || input.max < 1 || input.max > 4) return "每组输入图片数量需为 1-4 张";
        keys.add(key);
        total += input.max;
    }
    if (total > maxInputFiles) return `输入图片合计最多 ${maxInputFiles} 张`;
    for (const match of prompt.matchAll(/@图片(\d+)/g)) {
        const n = Number(match[1]);
        if (n < 1 || n > total) return `提示词中的 ${match[0]} 超出输入图片数量`;
    }
    if (!item.ratios?.length) return "请至少选择一个比例";
    if (!item.resolutions?.length) return "请至少选择一个分辨率";
    const fee = item.feeCredits ?? 0;
    if (!Number.isFinite(fee) || fee < 0 || fee > 10000) return "模板费需为 0-10000";
    const examples = item.examples ?? [];
    if (examples.length > 20) return "示例最多 20 个";
    if (examples.some((example) => (example.inputs?.length ?? 0) > maxInputFiles || runes(example.prompt ?? "") > maxUserPrompt)) return "示例内容过长";
    return "";
}

/** What still blocks a template run (first missing required upload, required or too long prompt), or "". */
export function templateRunProblem(template: Pick<ImageTemplate, "inputs" | "promptRequired">, files: Record<string, unknown[]>, prompt: string) {
    const missing = template.inputs.find((input) => input.required && !files[input.key]?.length);
    if (missing) return `请上传「${missing.label}」`;
    if (template.promptRequired && !prompt.trim()) return "请填写提示词";
    if (runes(prompt.trim()) > maxUserPrompt) return `提示词不能超过 ${maxUserPrompt} 字`;
    return "";
}

/** Best-matching allowed ratio and resolution for an output size (works don't carry the request), {} when unknown. */
export function sizeToTemplateParams(width: number, height: number, ratios: string[], resolutions: string[]) {
    if (!(width > 0 && height > 0)) return {};
    const closest = <T>(items: T[], distance: (item: T) => number) => items.reduce<T | undefined>((best, item) => (best === undefined || distance(item) < distance(best) ? item : best), undefined);
    const ratio = closest(ratios, (value) => {
        const [w, h] = value.split(":").map(Number);
        return Math.abs(Math.log(width / height) - Math.log(w / h));
    });
    const resolution = closest(resolutions, (value) => Math.abs(Math.log(Math.sqrt(width * height)) - Math.log(resolutionPx[value] ?? 1024)));
    return { ratio, resolution };
}
