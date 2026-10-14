import type { GenerationJobStatus } from "@/services/api/jobs";

/** Groups a job status the way the works page counts them. */
export function workStatusGroup(status: GenerationJobStatus): "succeeded" | "running" | "failed" {
    if (status === "succeeded") return "succeeded";
    if (status === "queued" || status === "running") return "running";
    return "failed";
}

/** Maps a generation job status onto the canvas image task status the /image workbench polls. */
export function canvasTaskStatus(status: GenerationJobStatus): "queued" | "processing" | "completed" | "failed" {
    if (status === "succeeded") return "completed";
    if (status === "running") return "processing";
    if (status === "queued") return "queued";
    return "failed";
}

/** Whole days left before a work is deleted, rounded up and never below 0. */
export function retentionDaysLeft(expiresAt: string, now = Date.now()) {
    const left = new Date(expiresAt).getTime() - now;
    return Number.isFinite(left) ? Math.max(0, Math.ceil(left / 86_400_000)) : 0;
}

export const isGenerationJobId = (id: string) => id.startsWith("genjob-");
