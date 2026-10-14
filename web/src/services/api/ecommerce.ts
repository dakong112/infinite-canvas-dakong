import { apiGet, apiPost } from "@/services/api/request";
import type { GenerationJob } from "@/services/api/jobs";

export type EcommerceSellingKind = "main" | "white" | "detail";
export type EcommerceSellingPoints = Record<EcommerceSellingKind, string>;
export type EcommerceAnalysis = {
    sellingPoints: EcommerceSellingPoints;
    targetAudience: string;
    designStyle: string;
    designSpec: string;
    needsModel: boolean;
    modelPrompt: string;
    styleOptions: string[];
};
export type EcommerceStructure = { mode: "smart" | "custom"; counts?: Record<string, number> };
export type EcommerceSku = { label: string; desc: string; fileId: string };
export type EcommerceSetForm = {
    preset?: "set" | "detail";
    productFileIds?: string[];
    market: string;
    platform: string;
    language: string;
    styleMode: "ai" | "reference" | "custom";
    styleText: string;
    referenceFileId: string;
    modelDescription: string;
    structure: EcommerceStructure;
    skus: EcommerceSku[];
    sellingPoints?: EcommerceSellingPoints;
};
export type EcommerceSlot = {
    id: string;
    kind: "main" | "white" | "detail" | "sku";
    size: string;
    title: string;
    theme?: string;
    textLang: string;
    needsModel?: boolean;
    skuDesc?: string;
    skuFileId?: string;
    previewJobId?: string;
    finalJobId?: string;
};
export type EcommercePendingJob = { jobId: string; purpose: "analyze" | "plan" | "rewrite" | "options"; kind?: string; slotIds?: string[] };
export type EcommerceSetView = {
    id: string;
    title: string;
    createdAt: string;
    form: EcommerceSetForm;
    slots: EcommerceSlot[];
    analysis: EcommerceAnalysis | null;
    sellingOptions: Record<string, string[]>;
    slotPrompts: Record<string, string>;
    planned: boolean;
    planRound: number;
    pending: EcommercePendingJob[];
    notice: string;
    jobs: GenerationJob[];
};
export type EcommerceSetSummary = { id: string; title: string; preset: "set" | "detail"; coverUrl: string; createdAt: string };
export type EcommerceToolName = "replicate" | "remove-bg" | "tryon";

const setPath = (id: string, action = "") => `/api/v1/ecommerce/batches/${encodeURIComponent(id)}${action}`;

export const listEcommerceSets = (token: string, preset: "set" | "detail") => apiGet<EcommerceSetSummary[]>("/api/v1/ecommerce/batches", { preset }, token);
export const createEcommerceSet = (token: string, form: EcommerceSetForm) => apiPost<EcommerceSetView>("/api/v1/ecommerce/batches", form, token);
export const fetchEcommerceSet = (token: string, id: string) => apiGet<EcommerceSetView>(setPath(id), undefined, token);
export const updateEcommerceSet = (token: string, id: string, form: EcommerceSetForm) => apiPost<EcommerceSetView>(setPath(id), form, token);
export const analyzeEcommerceSet = (token: string, id: string) => apiPost<EcommerceSetView>(setPath(id, "/analyze"), {}, token);
export const requestSellingPoints = (token: string, id: string, kind: EcommerceSellingKind, mode: "rewrite" | "options") =>
    apiPost<EcommerceSetView>(setPath(id, "/selling-points"), { kind, mode }, token);
export const planEcommerceSet = (token: string, id: string) => apiPost<EcommerceSetView>(setPath(id, "/plan"), {}, token);
export const generateEcommerceSet = (token: string, id: string, stage: "preview" | "final", slotIds: string[]) =>
    apiPost<EcommerceSetView>(setPath(id, "/generate"), { stage, slotIds }, token);
export const runEcommerceTool = (token: string, tool: EcommerceToolName, input: { fileIds: string[]; params: Record<string, string>; count: number }) =>
    apiPost<{ batchId: string; jobs: GenerationJob[] }>(`/api/v1/ecommerce/tools/${tool}`, input, token);
