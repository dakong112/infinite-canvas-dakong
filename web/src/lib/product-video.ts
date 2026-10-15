import type { TypicalUsage } from "@/lib/model-catalog";
import type { GenerationJob } from "@/services/api/jobs";
import type { ProductVideoForm, ProductVideoSettings, ProductVideoShot, ProductVideoView, StoryboardShot } from "@/services/api/product-video";
import type { VideoTemplateDefaults } from "@/services/api/templates";

// Mirrors service/product_video.go so each step fails before the request does.
export const videoSteps = ["产品信息", "视频设置", "脚本生成", "视频生成"];
export const videoDurations = [15, 30, 45, 60];
export const videoRatios = ["9:16", "16:9", "1:1"];
export const videoResolutions = ["720p", "1080p"];
export const videoStyles = [
    { value: "unboxing", label: "开箱测评" },
    { value: "scene", label: "场景种草" },
    { value: "demo", label: "功能演示" },
    { value: "promo", label: "促销快剪" },
    { value: "custom", label: "自定义" },
];
export const videoTemplateCategoryLabels = { "video-unboxing": "带货开箱", "video-local": "同城到店", "video-service": "上门服务", "video-promo": "节日促销" } as const;
export const MAX_PRODUCTS = 6;
export const MAX_SHOTS = 20;
export const MIN_SHOT_SECONDS = 2;
/** Typical token usage of the storyboard (product images + brief in, shot JSON out). */
export const scriptUsage: TypicalUsage = { input: 4000, output: 1500 };

export const defaultVideoSettings: ProductVideoSettings = { duration: 30, ratio: "9:16", resolution: "720p", style: "scene", styleText: "", voice: "", bgmFileId: "", subtitles: true };

const runes = (value: string) => [...(value ?? "")].length;

export const storyboardSeconds = (shots: Pick<StoryboardShot, "seconds">[]) => shots.reduce((sum, shot) => sum + (Number(shot.seconds) || 0), 0);

export function formProblem(form: ProductVideoForm) {
    if (form.productFileIds.length < 1 || form.productFileIds.length > MAX_PRODUCTS) return `请上传 1-${MAX_PRODUCTS} 张产品图`;
    const name = form.name.trim();
    if (!name || runes(name) > 50) return "产品名称必填（≤50 字）";
    if (runes(form.intro.trim()) > 1000 || runes(form.sellingPoints.trim()) > 1000) return "产品介绍与卖点各 ≤1000 字";
    return "";
}

export function settingsProblem(settings: ProductVideoSettings) {
    if (!videoDurations.includes(settings.duration)) return "视频时长需为 15 / 30 / 45 / 60 秒";
    if (!videoRatios.includes(settings.ratio) || !videoResolutions.includes(settings.resolution)) return "比例或分辨率无效";
    const text = settings.styleText.trim();
    if (!videoStyles.some((style) => style.value === settings.style) || runes(text) > 300 || (settings.style === "custom" && !text)) return "自定义风格需填写 ≤300 字说明";
    return "";
}

/** Why the storyboard can't be saved, or "": 1-20 shots, each MIN..max seconds with a scene, total within the duration. */
export function storyboardProblem(shots: StoryboardShot[], duration: number, maxShotSeconds: number) {
    if (shots.length < 1 || shots.length > MAX_SHOTS) return `分镜数量需为 1-${MAX_SHOTS} 个`;
    for (const [index, shot] of shots.entries()) {
        if (!Number.isInteger(shot.seconds) || shot.seconds < MIN_SHOT_SECONDS || shot.seconds > maxShotSeconds) return `第 ${index + 1} 镜时长需为 ${MIN_SHOT_SECONDS}-${maxShotSeconds} 秒`;
        const scene = shot.scene.trim();
        if (!scene || runes(scene) > 500 || runes(shot.camera.trim()) > 100 || runes(shot.voiceover.trim()) > 300 || runes(shot.subtitle.trim()) > 50)
            return `第 ${index + 1} 镜：画面描述必填（≤500 字），镜头 ≤100 字，旁白 ≤300 字，字幕 ≤50 字`;
    }
    const total = storyboardSeconds(shots);
    if (total > duration) return `分镜总时长 ${total} 秒超过视频时长 ${duration} 秒`;
    return "";
}

/** What blocks leaving a step forward, or "". Step 3 → 4 also needs the storyboard saved (the page checks that). */
export function stepProblem(step: number, state: { form: ProductVideoForm; settings: ProductVideoSettings; shots: StoryboardShot[]; maxShotSeconds: number }) {
    if (step === 0) return formProblem(state.form);
    if (step === 1) return settingsProblem(state.settings);
    if (step === 2) return storyboardProblem(state.shots, state.settings.duration, state.maxShotSeconds);
    return "";
}

/** Step to open a saved video on: 视频生成 once any shot was generated, else 脚本生成. */
export function initialStep(view: Pick<ProductVideoView, "shots" | "composeJobId">) {
    return view.composeJobId || view.shots.some((shot) => shot.keyframeJobId || shot.videoJobId || shot.audioJobId) ? 3 : 2;
}

export const sameShots = (a: StoryboardShot[], b: StoryboardShot[]) => JSON.stringify(a.map(shotFields)) === JSON.stringify(b.map(shotFields));
const shotFields = ({ id, seconds, scene, camera, voiceover, subtitle }: StoryboardShot) => ({ id: id ?? "", seconds, scene: scene.trim(), camera: camera.trim(), voiceover: voiceover.trim(), subtitle: subtitle.trim() });

export const isRunning = (job?: GenerationJob) => job?.status === "queued" || job?.status === "running" || job?.status === "polling";

export type VideoModels = { model: string; keyframeModel: string; ttsModel: string };

/**
 * Priced steps of generating shots: a keyframe (when a keyframe model is set) and a clip per shot, plus
 * the voiceover characters as TTS input when voiceover is on. Clips are priced per call (the catalog has no per-second price).
 */
export function shotCostSteps(models: VideoModels, shots: Pick<StoryboardShot, "voiceover">[], voice: string) {
    if (!shots.length) return [];
    const steps: Array<{ model: string; count?: number; usage?: TypicalUsage }> = [];
    if (models.keyframeModel) steps.push({ model: models.keyframeModel, count: shots.length });
    steps.push({ model: models.model, count: shots.length });
    const chars = voice && models.ttsModel ? shots.reduce((sum, shot) => sum + runes(shot.voiceover.trim()), 0) : 0;
    if (chars) steps.push({ model: models.ttsModel, usage: { input: chars, output: 0 } });
    return steps;
}

export type StageStatus = "idle" | "running" | "done" | "failed";

/** 关键帧 → 视频 → 配音 progress of a shot; keyframe and voiceover only when they apply. */
export function shotStages(shot: ProductVideoShot, jobs: Map<string, GenerationJob>, models: VideoModels, voice: string) {
    const status = (id: string): StageStatus => {
        const job = jobs.get(id);
        if (!job) return "idle";
        if (isRunning(job)) return "running";
        return job.status === "succeeded" ? "done" : "failed";
    };
    const stages: Array<{ key: "keyframe" | "video" | "audio"; label: string; status: StageStatus }> = [];
    if (shot.keyframeJobId || models.keyframeModel) stages.push({ key: "keyframe", label: "关键帧", status: status(shot.keyframeJobId) });
    stages.push({ key: "video", label: "视频", status: shot.error && !shot.videoJobId ? "failed" : status(shot.videoJobId) });
    if (shot.audioJobId || (voice && models.ttsModel && shot.voiceover.trim())) stages.push({ key: "audio", label: "配音", status: status(shot.audioJobId) });
    return stages;
}

/** Compose job outputText: the .srt link and whether subtitles were burned in; null without subtitles. */
export function composeOutput(job?: GenerationJob): { subtitleFileId: string; subtitleUrl: string; subtitlesBurned: boolean } | null {
    if (!job?.outputText) return null;
    try {
        const parsed = JSON.parse(job.outputText);
        return parsed?.subtitleUrl ? parsed : null;
    } catch {
        return null;
    }
}

/** Wizard fields a video template fills: product copy, duration / ratio / style and its storyboard (without ids). */
export function templatePrefill(defaults: VideoTemplateDefaults, settings: ProductVideoSettings) {
    return {
        copy: { name: defaults.name ?? "", intro: defaults.intro ?? "", sellingPoints: defaults.sellingPoints ?? "" },
        settings: {
            ...settings,
            duration: videoDurations.includes(defaults.duration) ? defaults.duration : settings.duration,
            ratio: videoRatios.includes(defaults.ratio) ? defaults.ratio : settings.ratio,
            style: videoStyles.some((style) => style.value === defaults.style) ? defaults.style : settings.style,
            styleText: "",
        },
        shots: (defaults.shots ?? []).map(({ seconds, scene, camera, voiceover, subtitle }): StoryboardShot => ({ seconds, scene, camera: camera ?? "", voiceover: voiceover ?? "", subtitle: subtitle ?? "" })),
    };
}

/** Why an admin video template can't be saved, or "" (mirrors normalizeVideoTemplate). */
export function videoDefaultsProblem(defaults?: Partial<VideoTemplateDefaults>) {
    const duration = defaults?.duration ?? 30;
    if (!videoDurations.includes(duration)) return "视频时长需为 15 / 30 / 45 / 60 秒";
    if (defaults?.ratio && !videoRatios.includes(defaults.ratio)) return "视频比例无效";
    if (defaults?.style && (defaults.style === "custom" || !videoStyles.some((style) => style.value === defaults.style))) return "视频风格无效";
    if (runes(defaults?.name ?? "") > 50 || runes(defaults?.intro ?? "") > 1000 || runes(defaults?.sellingPoints ?? "") > 1000) return "示例产品名称 ≤50 字，介绍与卖点各 ≤1000 字";
    const shots = (defaults?.shots ?? []).map((shot) => ({ ...shot, scene: shot?.scene ?? "", camera: shot?.camera ?? "", voiceover: shot?.voiceover ?? "", subtitle: shot?.subtitle ?? "" }));
    return shots.length ? storyboardProblem(shots, duration, 15) : "";
}
