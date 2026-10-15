import { apiDelete, apiGet, apiPost, apiPut, compactApiParams } from "@/services/api/request";
import type { GenerationJob } from "@/services/api/jobs";

export type ImageTemplateCategory = "ecommerce" | "creative";
export type ImageTemplateSummary = { slug: string; title: string; description: string; category: ImageTemplateCategory; coverUrl: string };
export type ImageTemplateInput = { key: string; label: string; max: number; required: boolean };
export type ImageTemplateExample = { inputs: string[]; output: string; prompt: string };
export type ImageTemplate = ImageTemplateSummary & {
    id: string;
    inputs: ImageTemplateInput[];
    /** Admin endpoints only: the public API never returns the system prompt. */
    prompt?: string;
    promptRequired: boolean;
    promptPlaceholder: string;
    ratios: string[];
    resolutions: string[];
    model: string;
    feeCredits: number;
    memberFree: boolean;
    examples: ImageTemplateExample[];
    /** Attribution of the example images, shown under 作品示例. */
    credit: string;
    status: "draft" | "published";
    sort: number;
    createdAt: string;
    updatedAt: string;
};
export type TemplateGenerateInput = { files: Record<string, string[]>; prompt: string; ratio: string; resolution: string; count: number };
/** Shared sample images (/api/files/<id>/content) for admin example runs: 商品组 / 人像组 / 模特组. */
export type TemplateSamples = { product: string[]; portrait: string[]; model: string[] };

export const fetchTemplates = (category: ImageTemplateCategory) => apiGet<ImageTemplateSummary[]>("/api/templates", { category });
export const fetchTemplate = (slug: string) => apiGet<ImageTemplate>(`/api/templates/${encodeURIComponent(slug)}`);
export const generateWithTemplate = (token: string, slug: string, input: TemplateGenerateInput) =>
    apiPost<{ batchId: string; jobs: GenerationJob[] }>(`/api/v1/templates/${encodeURIComponent(slug)}/generate`, input, token);

export const fetchAdminTemplates = (token: string) => apiGet<ImageTemplate[]>("/api/admin/templates", undefined, token);
export const saveAdminTemplate = (token: string, item: Partial<ImageTemplate>) =>
    item.id ? apiPut<ImageTemplate>(`/api/admin/templates/${encodeURIComponent(item.id)}`, item, token) : apiPost<ImageTemplate>("/api/admin/templates", item, token);
export const deleteAdminTemplate = (token: string, id: string) => apiDelete<boolean>(`/api/admin/templates/${encodeURIComponent(id)}`, token);
export const generateTemplateExamples = (token: string, id: string, input: Partial<TemplateGenerateInput>) =>
    apiPost<{ batchId: string; jobs: GenerationJob[] }>(`/api/admin/templates/${encodeURIComponent(id)}/examples`, input, token);
export const fetchTemplateExampleJobs = (token: string, templateId?: string) => apiGet<GenerationJob[]>("/api/admin/templates/examples", compactApiParams({ templateId }), token);
export const fetchTemplateSamples = (token: string) => apiGet<TemplateSamples>("/api/admin/templates/samples", undefined, token);
export const saveTemplateSamples = (token: string, samples: TemplateSamples) => apiPut<TemplateSamples>("/api/admin/templates/samples", samples, token);
