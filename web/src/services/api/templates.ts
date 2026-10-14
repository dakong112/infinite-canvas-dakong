import { apiGet } from "@/services/api/request";

export type ImageTemplateCategory = "ecommerce" | "creative";
export type ImageTemplateSummary = { slug: string; title: string; description: string; category: ImageTemplateCategory; coverUrl: string };

export const fetchTemplates = (category: ImageTemplateCategory) => apiGet<ImageTemplateSummary[]>("/api/templates", { category });
