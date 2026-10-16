import { apiDelete, apiGet, apiPost, compactApiParams } from "@/services/api/request";
import { uploadImage } from "@/services/image-storage";

/** "polling": a submitted video job waiting on the upstream (counts as running). */
export type GenerationJobStatus = "queued" | "running" | "polling" | "succeeded" | "failed" | "cancelled";
export type GenerationJobKind = "image" | "vision" | "text" | "video" | "audio" | "compose";

export type GenerationJob = {
    id: string;
    userId: string;
    batchId: string;
    source: string;
    kind: GenerationJobKind;
    endpoint: string;
    model: string;
    inputFileIds: string[];
    /** Content URLs of inputFileIds, signed per response. */
    inputUrls: string[];
    status: GenerationJobStatus;
    attempts: number;
    maxAttempts: number;
    nextRunAt: string;
    /** A video still polling after 6 hours; the server re-checks it every 10 minutes for up to 24 hours. */
    slow?: boolean;
    error: string;
    outputFileId: string;
    outputUrl: string;
    outputText: string;
    prompt: string;
    slotKind: string;
    stage: string;
    templateId?: string;
    width: number;
    height: number;
    createdAt: string;
    finishedAt: string | null;
    expiresAt: string;
};

export type GenerationJobInput = {
    kind: GenerationJobKind;
    endpoint: string;
    model: string;
    channelId?: string;
    request: Record<string, unknown>;
    inputFileIds?: string[];
    prompt?: string;
    count?: number;
};

export type WorksKind = "image" | "set" | "tool" | "template" | "video";
export type WorksStatus = "succeeded" | "running" | "failed";
export type WorksPage = { items: GenerationJob[]; nextCursor: string; counts: { succeeded: number; running: number; failed: number } };

export const createGenerationJobs = (token: string, title: string, items: GenerationJobInput[]) =>
    apiPost<{ batchId: string; jobs: GenerationJob[] }>("/api/v1/jobs", { title, items }, token);
export const fetchGenerationJobs = (token: string, ids: string[]) => apiPost<GenerationJob[]>("/api/v1/jobs/status", { ids }, token);
export const cancelGenerationJob = (token: string, id: string) => apiPost<GenerationJob>(`/api/v1/jobs/${encodeURIComponent(id)}/cancel`, {}, token);
export const retryGenerationJob = (token: string, id: string) => apiPost<GenerationJob>(`/api/v1/jobs/${encodeURIComponent(id)}/retry`, {}, token);
export const fetchWorks = (token: string, query: { cursor?: string; kind?: WorksKind | ""; templateId?: string; status?: WorksStatus | "" }) =>
    apiGet<WorksPage>("/api/v1/works", compactApiParams(query), token);
export const deleteWork = (token: string, id: string) => apiDelete<{ deleted: boolean }>(`/api/v1/works/${encodeURIComponent(id)}`, token);

/** Returns the server storage object ID for an image, uploading it first when it only lives in the browser. */
export async function ensureServerFileId(source: string | Blob, storageKey?: string) {
    const isServerKey = (key?: string) => Boolean(key?.startsWith("server:") && !key.startsWith("server:webdav:"));
    if (isServerKey(storageKey)) return storageKey!.slice("server:".length);
    const uploaded = await uploadImage(source);
    if (!isServerKey(uploaded.storageKey)) throw new Error("服务端存储未启用，无法提交生成任务");
    return uploaded.storageKey.slice("server:".length);
}
