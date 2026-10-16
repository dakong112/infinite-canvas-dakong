import { apiDelete, apiGet, apiPost, apiPut } from "@/services/api/request";
import type { GenerationJob } from "@/services/api/jobs";

export type ProductVideoForm = { productFileIds: string[]; name: string; intro: string; sellingPoints: string };
/** Step 2 of the wizard; an empty voice means no voiceover, an empty bgmFileId no background music. */
export type ProductVideoSettings = {
    duration: number;
    ratio: string;
    resolution: string;
    style: string;
    styleText: string;
    voice: string;
    bgmFileId: string;
    subtitles: boolean;
};
/** A storyboard row as the user edits it; id is unset for new rows. */
export type StoryboardShot = { id?: string; seconds: number; scene: string; camera: string; voiceover: string; subtitle: string };
export type ProductVideoShot = Required<StoryboardShot> & { keyframeJobId: string; videoJobId: string; audioJobId: string; error: string };
export type ProductVideoView = {
    id: string;
    title: string;
    createdAt: string;
    form: ProductVideoForm;
    settings: ProductVideoSettings;
    shots: ProductVideoShot[];
    scriptJobId: string;
    composeJobId: string;
    shotSeq: number;
    notice: string;
    jobs: GenerationJob[];
};
export type ProductVideoSummary = { id: string; title: string; coverUrl: string; composeJobId: string; createdAt: string };

const path = (id: string, action = "") => `/api/v1/video-products/${encodeURIComponent(id)}${action}`;

export const listProductVideos = (token: string) => apiGet<ProductVideoSummary[]>("/api/v1/video-products", undefined, token);
export const createProductVideo = (token: string, input: { form: ProductVideoForm; settings: ProductVideoSettings }) => apiPost<ProductVideoView>("/api/v1/video-products", input, token);
/** The server applies finished jobs (script → storyboard, keyframe → video job) before answering. */
export const fetchProductVideo = (token: string, id: string) => apiGet<ProductVideoView>(path(id), undefined, token);
export const updateProductVideo = (token: string, id: string, input: { form: ProductVideoForm; settings: ProductVideoSettings }) => apiPost<ProductVideoView>(path(id), input, token);
export const generateProductVideoScript = (token: string, id: string) => apiPost<ProductVideoView>(path(id, "/script"), {}, token);
export const saveProductVideoScript = (token: string, id: string, shots: StoryboardShot[]) => apiPut<ProductVideoView>(path(id, "/script"), { shots }, token);
export const generateProductVideoShot = (token: string, id: string, shotId: string) => apiPost<ProductVideoView>(path(id, `/shots/${encodeURIComponent(shotId)}/generate`), {}, token);
export const generateAllProductVideoShots = (token: string, id: string) => apiPost<ProductVideoView>(path(id, "/generate-all"), {}, token);
export const composeProductVideo = (token: string, id: string) => apiPost<ProductVideoView>(path(id, "/compose"), {}, token);

/** A background music library track (public file; url needs no signature). */
export type BgmTrack = { fileId: string; name: string; tags: string[] | null; durationSec: number; url: string };
export const listBgmTracks = () => apiGet<BgmTrack[]>("/api/bgm-tracks");
export const addBgmTrack = (token: string, track: { fileId: string; name: string; tags: string[]; durationSec: number }) => apiPost<BgmTrack[]>("/api/admin/bgm-tracks", track, token);
export const deleteBgmTrack = (token: string, fileId: string) => apiDelete<BgmTrack[]>(`/api/admin/bgm-tracks/${encodeURIComponent(fileId)}`, token);
