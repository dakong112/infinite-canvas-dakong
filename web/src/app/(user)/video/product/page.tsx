"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Empty, Input, InputNumber, Segmented, Select, Spin, Switch, Tag } from "antd";
import { saveAs } from "file-saver";
import { ArrowDown, ArrowUp, Check, Clapperboard, Download, LayoutTemplate, LoaderCircle, Music2, Plus, RotateCcw, Sparkles, Trash2, X } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo, useRef, useState } from "react";

import {
    composeOutput,
    defaultVideoSettings,
    initialStep,
    isRunning,
    MAX_PRODUCTS,
    MAX_SHOTS,
    sameShots,
    scriptUsage,
    shotCostSteps,
    shotStages,
    stepProblem,
    storyboardSeconds,
    templatePrefill,
    videoDurations,
    videoRatios,
    videoResolutions,
    videoSteps,
    videoStyles,
    videoTemplateCategoryLabels,
    type StageStatus,
} from "@/lib/product-video";
import { hasPermission, PERMISSIONS } from "@/lib/admin-permissions";
import { cn } from "@/lib/utils";
import type { GenerationJob } from "@/services/api/jobs";
import {
    composeProductVideo,
    createProductVideo,
    fetchProductVideo,
    generateAllProductVideoShots,
    generateProductVideoScript,
    generateProductVideoShot,
    listProductVideos,
    saveProductVideoScript,
    updateProductVideo,
    type ProductVideoSettings,
    type ProductVideoShot,
    type ProductVideoView,
    type StoryboardShot,
} from "@/services/api/product-video";
import { fetchAdminTemplates, fetchTemplate, fetchTemplates, type ImageTemplateSummary } from "@/services/api/templates";
import { uploadMediaBlob } from "@/services/file-storage";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import { FormSection, ImageToolShell, PriceText, useImagePrice, type ShellTab } from "../../image/components/image-tool-shell";
import { UploadDropzone, pickedImageUrl, type PickedImage } from "../../image/components/upload-dropzone";

// OpenAI-style /audio/speech voices; the admin's default voice is added when it isn't one of them.
const voices = ["alloy", "ash", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer"];
const MAX_BGM_BYTES = 20 * 1024 * 1024;
const emptyCopy = { name: "", intro: "", sellingPoints: "" };
const ratioAspect: Record<string, string> = { "9:16": "aspect-[9/16] w-20", "16:9": "aspect-video w-32", "1:1": "aspect-square w-24" };
const stageTone: Record<StageStatus, string> = {
    idle: "border-border text-muted-foreground",
    running: "border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-300",
    done: "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
    failed: "border-red-500/40 bg-red-500/10 text-red-600 dark:text-red-400",
};
const styleLabel = (value: string) => videoStyles.find((style) => style.value === value)?.label ?? value;
const categoryLabel = (value: string) => videoTemplateCategoryLabels[value as keyof typeof videoTemplateCategoryLabels] ?? "";

export default function ProductVideoPage() {
    return (
        <Suspense>
            <ProductVideoWizard />
        </Suspense>
    );
}

function ProductVideoWizard() {
    const { message, modal } = App.useApp();
    const router = useRouter();
    const searchParams = useSearchParams();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const isAdmin = useUserStore((state) => hasPermission(state.user, PERMISSIONS.templateRead));
    const videoSetting = useConfigStore((state) => state.publicSettings?.video);
    const price = useImagePrice();
    const id = searchParams.get("id") || "";
    const templateSlug = searchParams.get("template") || "";
    const bgmInput = useRef<HTMLInputElement>(null);
    const [step, setStep] = useState(0);
    const [products, setProducts] = useState<PickedImage[]>([]);
    const [copy, setCopy] = useState(emptyCopy);
    const [settings, setSettings] = useState<ProductVideoSettings>(defaultVideoSettings);
    const [bgmName, setBgmName] = useState("");
    const [shots, setShots] = useState<StoryboardShot[]>([]);
    const [busy, setBusy] = useState("");
    const [hydratedId, setHydratedId] = useState("");
    const [category, setCategory] = useState("");

    const { data: view } = useQuery({
        queryKey: ["product-video", id],
        queryFn: () => fetchProductVideo(token!, id),
        enabled: Boolean(token && id),
        refetchInterval: (query) => {
            const data = query.state.data;
            if (!data) return false;
            // Slow videos are re-checked by the server every 10 minutes, so the page can poll them slowly too.
            return data.scriptJobId || data.jobs.some((job) => isRunning(job) && !job.slow) ? 3000 : data.jobs.some(isRunning) ? 60000 : false;
        },
    });
    const { data: history } = useQuery({ queryKey: ["product-videos", token], queryFn: () => listProductVideos(token!), enabled: Boolean(token) });
    const { data: templates } = useQuery({ queryKey: ["templates", "video"], queryFn: () => fetchTemplates("", "video"), retry: false, staleTime: 5 * 60 * 1000 });
    // Admins open drafts from the template list (the public endpoint only serves published ones).
    const { data: urlTemplate } = useQuery({
        queryKey: ["template", templateSlug, isAdmin],
        queryFn: async () => {
            try {
                return await fetchTemplate(templateSlug);
            } catch (error) {
                const draft = isAdmin && token ? (await fetchAdminTemplates(token)).find((item) => item.slug === templateSlug) : undefined;
                if (draft) return draft;
                throw error;
            }
        },
        enabled: Boolean(templateSlug),
        retry: false,
    });
    const jobs = useMemo(() => new Map((view?.jobs ?? []).map((job) => [job.id, job])), [view]);
    const models = { model: videoSetting?.model || "", keyframeModel: videoSetting?.keyframeModel || "", ttsModel: videoSetting?.ttsModel || "" };
    const maxShotSeconds = videoSetting?.maxShotSeconds || 5;

    // Load a saved video into the wizard once per video.
    useEffect(() => {
        if (!view || view.id === hydratedId) return;
        setHydratedId(view.id);
        setProducts(view.form.productFileIds.map((fileId) => ({ fileId, url: pickedImageUrl(fileId) })));
        setCopy({ name: view.form.name, intro: view.form.intro, sellingPoints: view.form.sellingPoints });
        setSettings(view.settings);
        setBgmName(view.settings.bgmFileId ? "已上传的背景音乐" : "");
        setShots(view.shots);
        setStep(initialStep(view));
    }, [view, hydratedId]);

    // The storyboard follows the server only when its content changed there (AI script), so typing is not overwritten by polling.
    const serverShots = JSON.stringify((view?.shots ?? []).map(({ id, seconds, scene, camera, voiceover, subtitle }) => ({ id, seconds, scene, camera, voiceover, subtitle })));
    useEffect(() => {
        const parsed = JSON.parse(serverShots) as StoryboardShot[];
        if (parsed.length) setShots(parsed);
    }, [serverShots]);

    // Voiceover defaults to the admin's voice once the settings arrive.
    useEffect(() => {
        if (videoSetting?.ttsModel && !id) setSettings((value) => (value.voice ? value : { ...value, voice: videoSetting.ttsVoice || "alloy" }));
    }, [videoSetting?.ttsModel]);

    const applyTemplate = (template: ImageTemplateSummary) => {
        if (!template.videoDefaults) return;
        const prefill = templatePrefill(template.videoDefaults, settings);
        router.replace("?");
        setHydratedId("");
        setCopy(prefill.copy);
        setSettings(prefill.settings);
        setShots(prefill.shots);
        setStep(0);
        message.success(`已套用「${template.title}」，上传产品图后继续`);
    };

    useEffect(() => {
        if (urlTemplate) applyTemplate(urlTemplate);
    }, [urlTemplate]);

    if (!token) return <Empty className="mt-20" description="请先登录后使用" />;

    const act = async (name: string, action: () => Promise<ProductVideoView>) => {
        setBusy(name);
        try {
            const next = await action();
            queryClient.setQueryData(["product-video", next.id], next);
            void queryClient.invalidateQueries({ queryKey: ["product-videos"] });
            return next;
        } catch (error) {
            message.error(error instanceof Error ? error.message : "操作失败");
        } finally {
            setBusy("");
        }
    };

    const input = () => ({ form: { productFileIds: products.map((item) => item.fileId), ...copy }, settings });
    // Creates the video on first save; later saves send the wizard only when it changed.
    const save = async () => {
        if (!view) {
            const created = await createProductVideo(token, input());
            setHydratedId(created.id);
            router.replace(`?id=${created.id}`);
            return created;
        }
        return JSON.stringify(input()) === JSON.stringify({ form: view.form, settings: view.settings }) ? view : updateProductVideo(token, view.id, input());
    };

    const startNew = () => {
        router.replace("?");
        setHydratedId("");
        setProducts([]);
        setCopy(emptyCopy);
        setSettings({ ...defaultVideoSettings, voice: videoSetting?.ttsModel ? videoSetting.ttsVoice || "alloy" : "" });
        setBgmName("");
        setShots([]);
        setStep(0);
    };

    const uploadBgm = async (file?: File) => {
        if (!file) return;
        if (!file.type.startsWith("audio/") || file.size > MAX_BGM_BYTES) return void message.warning("请选择 20MB 以内的音频文件");
        setBusy("bgm");
        try {
            const { storageKey } = await uploadMediaBlob(file, file.name);
            if (!storageKey.startsWith("server:") || storageKey.startsWith("server:webdav:")) throw new Error("服务端存储未启用，无法使用背景音乐");
            setSettings((value) => ({ ...value, bgmFileId: storageKey.slice("server:".length) }));
            setBgmName(file.name);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "上传失败");
        } finally {
            setBusy("");
        }
    };

    const scriptRunning = Boolean(view?.scriptJobId);
    const shotsDirty = !view || !sameShots(shots, view.shots);
    const problem = stepProblem(step, { form: input().form, settings, shots, maxShotSeconds });
    const reachable = view ? (view.shots.length ? 3 : 2) : 1;
    const voice = view?.settings.voice ?? settings.voice;
    const clipJob = (shot: ProductVideoShot) => jobs.get(shot.videoJobId) ?? jobs.get(shot.keyframeJobId);
    // Mirrors the server's generate-all: shots with an error or without a finished / running clip.
    const pendingShots = (view?.shots ?? []).filter((shot) => shot.error || !(clipJob(shot)?.status === "succeeded" || isRunning(clipJob(shot))));
    const shotReady = (shot: ProductVideoShot) => jobs.get(shot.videoJobId)?.status === "succeeded" && (!shot.audioJobId || jobs.get(shot.audioJobId)?.status === "succeeded");
    const allReady = Boolean(view?.shots.length) && view!.shots.every(shotReady);
    const composeJob = view?.composeJobId ? jobs.get(view.composeJobId) : undefined;
    const composing = isRunning(composeJob);
    const subtitle = composeOutput(composeJob);
    const scriptCredits = price([{ model: videoSetting?.scriptModel || "", usage: scriptUsage }]);
    const pendingCredits = price(shotCostSteps(models, pendingShots, voice));

    const next = async () => {
        if (problem) return void message.warning(problem);
        if (step === 1) {
            if (await act("save", save)) setStep(2);
            return;
        }
        if (step === 2 && shotsDirty) {
            if (await act("save", async () => saveProductVideoScript(token, (await save()).id, shots))) setStep(3);
            return;
        }
        setStep(step + 1);
    };

    const writeScript = () => {
        const run = () => void act("script", async () => generateProductVideoScript(token, (await save()).id));
        if (!shots.length) return run();
        modal.confirm({ title: "重新生成分镜？", content: "AI 生成的分镜会替换当前分镜。", okText: "重新生成", cancelText: "取消", onOk: run });
    };

    const updateShot = (index: number, patch: Partial<StoryboardShot>) => setShots((value) => value.map((shot, i) => (i === index ? { ...shot, ...patch } : shot)));
    const moveShot = (index: number, offset: number) =>
        setShots((value) => {
            const items = [...value];
            [items[index], items[index + offset]] = [items[index + offset], items[index]];
            return items;
        });
    const total = storyboardSeconds(shots);

    const stepBar = (
        <ol className="grid grid-cols-4 gap-1">
            {videoSteps.map((title, index) => (
                <li key={title}>
                    <button
                        type="button"
                        disabled={index > reachable}
                        aria-current={index === step ? "step" : undefined}
                        className={cn("flex w-full flex-col items-center gap-1 text-xs transition disabled:opacity-40", index === step ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground")}
                        onClick={() => setStep(index)}
                    >
                        <span className={cn("flex size-6 items-center justify-center rounded-full border text-[11px] tabular-nums", index < step ? "border-foreground bg-foreground text-background" : index === step ? "border-foreground" : "border-border")}>
                            {index < step ? <Check className="size-3" strokeWidth={3} /> : index + 1}
                        </span>
                        {title}
                    </button>
                </li>
            ))}
        </ol>
    );

    const notice = view?.notice && <div className="rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">{view.notice}</div>;

    const productStep = (
        <>
            <FormSection title="产品图" extra={<span className="text-xs text-muted-foreground">最多 {MAX_PRODUCTS} 张</span>}>
                <UploadDropzone label="上传产品图" max={MAX_PRODUCTS} value={products} onChange={setProducts} />
            </FormSection>
            <FormSection
                title="产品信息"
                extra={
                    <span className="flex items-center gap-1">
                        {(templates ?? []).length > 0 && (
                            <Select
                                size="small"
                                variant="borderless"
                                placeholder="模板"
                                value={null}
                                popupMatchSelectWidth={false}
                                options={(templates ?? []).map((item) => ({ label: item.title, value: item.slug }))}
                                onChange={(slug) => applyTemplate(templates!.find((item) => item.slug === slug)!)}
                            />
                        )}
                        <Button size="small" type="text" onClick={() => setCopy(emptyCopy)}>
                            清空
                        </Button>
                    </span>
                }
            >
                <Input size="large" placeholder="产品名称（必填）" maxLength={50} value={copy.name} onChange={(event) => setCopy({ ...copy, name: event.target.value })} />
                <Input.TextArea placeholder="产品介绍：材质、规格、适用人群等" maxLength={1000} autoSize={{ minRows: 3, maxRows: 6 }} value={copy.intro} onChange={(event) => setCopy({ ...copy, intro: event.target.value })} />
                <Input.TextArea placeholder="产品卖点：每行一个，不填由 AI 从产品图提炼" maxLength={1000} autoSize={{ minRows: 3, maxRows: 6 }} value={copy.sellingPoints} onChange={(event) => setCopy({ ...copy, sellingPoints: event.target.value })} />
            </FormSection>
        </>
    );

    const settingsStep = (
        <>
            <FormSection title="总时长">
                <Segmented block value={settings.duration} options={videoDurations.map((value) => ({ label: `${value} 秒`, value }))} onChange={(duration) => setSettings({ ...settings, duration })} />
            </FormSection>
            <div className="grid grid-cols-2 gap-3">
                <FormSection title="比例">
                    <Segmented block value={settings.ratio} options={videoRatios} onChange={(ratio) => setSettings({ ...settings, ratio })} />
                </FormSection>
                <FormSection title="分辨率">
                    <Segmented block value={settings.resolution} options={videoResolutions} onChange={(resolution) => setSettings({ ...settings, resolution })} />
                </FormSection>
            </div>
            <FormSection title="风格">
                <div className="flex flex-wrap gap-1.5">
                    {videoStyles.map((style) => (
                        <Tag.CheckableTag key={style.value} className="!m-0 !rounded-full !border !border-border !px-3 !py-1" checked={settings.style === style.value} onChange={() => setSettings({ ...settings, style: style.value })}>
                            {style.label}
                        </Tag.CheckableTag>
                    ))}
                </div>
                {settings.style === "custom" && (
                    <Input.TextArea placeholder="描述想要的风格，如：vlog 口播、ins 风慢镜头" maxLength={300} autoSize={{ minRows: 2, maxRows: 4 }} value={settings.styleText} onChange={(event) => setSettings({ ...settings, styleText: event.target.value })} />
                )}
            </FormSection>
            <FormSection title="配音" extra={!models.ttsModel && <span className="text-xs text-muted-foreground">管理员未配置配音模型</span>}>
                <Select
                    size="large"
                    className="!w-full"
                    disabled={!models.ttsModel}
                    value={models.ttsModel ? settings.voice : ""}
                    options={[{ label: "关闭配音", value: "" }, ...[...new Set([videoSetting?.ttsVoice || "alloy", ...voices])].map((value) => ({ label: value, value }))]}
                    onChange={(value) => setSettings({ ...settings, voice: value })}
                />
            </FormSection>
            <FormSection
                title="背景音乐"
                extra={
                    <Button size="small" type="text" icon={<Music2 className="size-3.5" />} loading={busy === "bgm"} onClick={() => bgmInput.current?.click()}>
                        上传音频
                    </Button>
                }
            >
                <input ref={bgmInput} type="file" accept="audio/*" hidden onChange={(event) => void uploadBgm(event.target.files?.[0]).finally(() => (event.target.value = ""))} />
                {settings.bgmFileId ? (
                    <div className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
                        <Music2 className="size-4 shrink-0 text-muted-foreground" />
                        <span className="min-w-0 flex-1 truncate">{bgmName}</span>
                        <Button
                            size="small"
                            type="text"
                            icon={<X className="size-3.5" />}
                            aria-label="移除背景音乐"
                            onClick={() => {
                                setSettings({ ...settings, bgmFileId: "" });
                                setBgmName("");
                            }}
                        />
                    </div>
                ) : (
                    <div className="text-xs text-muted-foreground">无背景音乐；可上传自己有版权的音频，合成时会压低音量循环铺底。</div>
                )}
            </FormSection>
            <label className="flex items-center justify-between gap-3 text-sm">
                <span>
                    字幕
                    <span className="ml-2 text-xs text-muted-foreground">烧录到画面并提供 .srt 下载</span>
                </span>
                <Switch checked={settings.subtitles} onChange={(subtitles) => setSettings({ ...settings, subtitles })} />
            </label>
        </>
    );

    const scriptStep = (
        <>
            {notice}
            {scriptRunning && (
                <div className="flex items-center gap-2 rounded-lg bg-sky-500/10 px-3 py-2 text-sm text-sky-700 dark:text-sky-300">
                    <Spin size="small" />
                    AI 正在编写分镜…
                </div>
            )}
            <FormSection
                title={`分镜（${shots.length}/${MAX_SHOTS}）`}
                extra={
                    <span className={cn("text-xs tabular-nums", total > settings.duration ? "text-red-500" : "text-muted-foreground")}>
                        总时长 {total} / {settings.duration} 秒
                    </span>
                }
            >
                {!shots.length && !scriptRunning && <div className="rounded-lg border border-dashed border-border px-3 py-8 text-center text-xs text-muted-foreground">点击下方“AI 生成分镜”，或手动添加分镜</div>}
                {shots.map((shot, index) => (
                    <div key={index} className="space-y-2 rounded-lg border border-border p-3">
                        <div className="flex items-center gap-2">
                            <span className="text-sm font-medium tabular-nums">#{index + 1}</span>
                            <InputNumber size="small" min={2} max={maxShotSeconds} precision={0} suffix="秒" className="!w-24" value={shot.seconds} onChange={(value) => updateShot(index, { seconds: value ?? 0 })} />
                            <span className="flex-1" />
                            <Button size="small" type="text" icon={<ArrowUp className="size-3.5" />} aria-label="上移" disabled={index === 0} onClick={() => moveShot(index, -1)} />
                            <Button size="small" type="text" icon={<ArrowDown className="size-3.5" />} aria-label="下移" disabled={index === shots.length - 1} onClick={() => moveShot(index, 1)} />
                            <Button size="small" type="text" danger icon={<Trash2 className="size-3.5" />} aria-label="删除分镜" onClick={() => setShots((value) => value.filter((_, i) => i !== index))} />
                        </div>
                        <Input.TextArea placeholder="画面描述（必填）" maxLength={500} autoSize={{ minRows: 2, maxRows: 5 }} value={shot.scene} onChange={(event) => updateShot(index, { scene: event.target.value })} />
                        <div className="grid grid-cols-2 gap-2">
                            <Input placeholder="镜头运动，如缓慢推近" maxLength={100} value={shot.camera} onChange={(event) => updateShot(index, { camera: event.target.value })} />
                            <Input placeholder="字幕" maxLength={50} value={shot.subtitle} onChange={(event) => updateShot(index, { subtitle: event.target.value })} />
                        </div>
                        <Input.TextArea placeholder="旁白（可空）" maxLength={300} autoSize={{ minRows: 1, maxRows: 4 }} value={shot.voiceover} onChange={(event) => updateShot(index, { voiceover: event.target.value })} />
                    </div>
                ))}
                <Button block type="dashed" icon={<Plus className="size-3.5" />} disabled={shots.length >= MAX_SHOTS} onClick={() => setShots((value) => [...value, { seconds: Math.min(3, maxShotSeconds), scene: "", camera: "", voiceover: "", subtitle: "" }])}>
                    添加分镜
                </Button>
            </FormSection>
        </>
    );

    const stageIcon = (status: StageStatus) => (status === "running" ? <LoaderCircle className="size-3 animate-spin" /> : status === "done" ? <Check className="size-3" /> : status === "failed" ? <X className="size-3" /> : null);

    const generateStep = view && (
        <>
            {notice}
            <FormSection title={`分镜（${view.shots.filter(shotReady).length}/${view.shots.length} 完成）`}>
                {view.shots.map((shot, index) => {
                    const frame = jobs.get(shot.keyframeJobId);
                    const clip = jobs.get(shot.videoJobId);
                    const shotJobs = [frame, clip, jobs.get(shot.audioJobId)].filter((job): job is GenerationJob => Boolean(job));
                    const error = shot.error || shotJobs.find((job) => job.status === "failed")?.error;
                    return (
                        <div key={shot.id} className="flex gap-3 rounded-lg border border-border p-2.5">
                            <div className={cn("relative shrink-0 overflow-hidden rounded-md bg-muted", ratioAspect[view.settings.ratio])}>
                                {clip?.outputUrl ? (
                                    <video
                                        src={clip.outputUrl}
                                        muted
                                        loop
                                        playsInline
                                        preload="metadata"
                                        className="size-full object-cover"
                                        onMouseEnter={(event) => void event.currentTarget.play().catch(() => undefined)}
                                        onMouseLeave={(event) => event.currentTarget.pause()}
                                    />
                                ) : frame?.outputUrl ? (
                                    <img src={frame.outputUrl} alt="" className="size-full object-cover" />
                                ) : (
                                    <Clapperboard className="absolute inset-0 m-auto size-5 text-muted-foreground/50" />
                                )}
                            </div>
                            <div className="min-w-0 flex-1 space-y-1.5">
                                <div className="flex items-center justify-between gap-1 text-xs">
                                    <span className="font-medium tabular-nums">
                                        #{index + 1} · {shot.seconds} 秒
                                    </span>
                                    <Button
                                        size="small"
                                        type="text"
                                        icon={<RotateCcw className="size-3" />}
                                        loading={busy === `shot-${shot.id}`}
                                        disabled={shotJobs.some(isRunning) || scriptRunning}
                                        onClick={() => void act(`shot-${shot.id}`, () => generateProductVideoShot(token, view.id, shot.id))}
                                    >
                                        {shotJobs.length ? "重新生成" : "生成"}
                                        <PriceText credits={price(shotCostSteps(models, [shot], voice))} prefix="·" className="opacity-60" />
                                    </Button>
                                </div>
                                <div className="line-clamp-2 text-xs text-muted-foreground" title={shot.scene}>
                                    {shot.subtitle ? `「${shot.subtitle}」` : ""}
                                    {shot.scene}
                                </div>
                                <div className="flex flex-wrap items-center gap-1">
                                    {shotStages(shot, jobs, models, voice).map((stage, i) => (
                                        <span key={stage.key} className="flex items-center gap-1">
                                            {i > 0 && <span className="text-[10px] text-muted-foreground">→</span>}
                                            <span className={cn("inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]", stageTone[stage.status])}>
                                                {stageIcon(stage.status)}
                                                {stage.label}
                                            </span>
                                        </span>
                                    ))}
                                </div>
                                {clip?.slow && isRunning(clip) && <div className="text-[11px] text-muted-foreground">生成较慢，完成后会自动更新</div>}
                                {error && <div className="line-clamp-2 text-[11px] text-red-500">{error}</div>}
                            </div>
                        </div>
                    );
                })}
            </FormSection>
            <FormSection title="成片">
                {composeJob?.outputUrl ? (
                    <div className="space-y-2">
                        <video src={composeJob.outputUrl} controls playsInline className="max-h-[480px] w-full rounded-lg bg-black" />
                        <div className="flex flex-wrap gap-2">
                            <Button icon={<Download className="size-3.5" />} onClick={() => saveAs(composeJob.outputUrl, `${view.title}.mp4`)}>
                                下载成片
                            </Button>
                            {subtitle && !subtitle.subtitlesBurned && (
                                <Button icon={<Download className="size-3.5" />} onClick={() => saveAs(subtitle.subtitleUrl, `${view.title}.srt`)}>
                                    下载字幕 .srt
                                </Button>
                            )}
                        </div>
                    </div>
                ) : composing ? (
                    <div className="flex items-center gap-2 rounded-lg bg-sky-500/10 px-3 py-2 text-sm text-sky-700 dark:text-sky-300">
                        <Spin size="small" />
                        成片合成中…
                    </div>
                ) : composeJob?.status === "failed" ? (
                    <div className="text-xs text-red-500">合成失败：{composeJob.error || "未知错误"}</div>
                ) : (
                    <div className="text-xs text-muted-foreground">所有分镜生成完成后点击“合成成片”，拼接画面并混入配音、背景音乐与字幕。</div>
                )}
            </FormSection>
        </>
    );

    const primary =
        step === 2 ? (
            <Button type="primary" block className="!h-11 !rounded-lg" icon={<Sparkles className="size-4" />} loading={busy === "script" || scriptRunning} disabled={!view} onClick={writeScript}>
                {shots.length ? "重新生成分镜" : "AI 生成分镜"}
                <PriceText credits={scriptCredits} prefix="消耗" className="text-xs opacity-80" />
            </Button>
        ) : step === 3 && view ? (
            pendingShots.length ? (
                <Button type="primary" block className="!h-11 !rounded-lg" icon={<Sparkles className="size-4" />} loading={busy === "all"} disabled={scriptRunning} onClick={() => void act("all", () => generateAllProductVideoShots(token, view.id))}>
                    全部生成（{pendingShots.length} 镜）
                    <PriceText credits={pendingCredits} prefix="消耗" className="text-xs opacity-80" />
                </Button>
            ) : (
                <Button type="primary" block className="!h-11 !rounded-lg" icon={<Clapperboard className="size-4" />} loading={busy === "compose" || composing} disabled={!allReady} onClick={() => void act("compose", () => composeProductVideo(token, view.id))}>
                    {composeJob?.status === "succeeded" ? "重新合成" : "合成成片"}
                </Button>
            )
        ) : null;

    const hint =
        step === 2
            ? scriptRunning
                ? "AI 正在编写分镜…"
                : !view
                  ? "请先完成视频设置"
                  : shots.length && problem
                    ? problem
                    : shotsDirty && shots.length
                      ? "分镜有改动，下一步时自动保存"
                      : "确认分镜后进入视频生成"
            : step === 3
              ? pendingShots.length
                  ? "每镜依次生成关键帧、视频与配音，可单镜重新生成"
                  : allReady
                    ? "合成不额外扣费"
                    : "分镜生成中，完成后可合成成片"
              : problem || (step === 0 ? "上传产品图并填写名称，下一步设置视频参数" : "保存后进入脚本生成");

    const action = (
        <div className="space-y-2">
            {primary}
            <div className="flex gap-2">
                <Button block disabled={step === 0} onClick={() => setStep(step - 1)}>
                    上一步
                </Button>
                {step < 3 && (
                    <Button block type={step < 2 ? "primary" : "default"} loading={busy === "save"} disabled={scriptRunning} onClick={() => void next()}>
                        {step === 1 ? "保存并下一步" : "下一步"}
                    </Button>
                )}
            </div>
        </div>
    );

    const shownTemplates = (templates ?? []).filter((item) => !category || item.category === category);
    const tabs: ShellTab[] = [
        {
            key: "history",
            label: "我的作品",
            content: history?.length ? (
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
                    {history.map((item) => (
                        <button
                            key={item.id}
                            type="button"
                            className={cn("flex w-full items-center gap-3 rounded-lg border p-2 text-left text-sm transition hover:border-foreground/40", item.id === id ? "border-foreground" : "border-border")}
                            onClick={() => router.replace(`?id=${item.id}`)}
                        >
                            {item.coverUrl ? (
                                <img src={item.coverUrl} alt="" className="size-14 shrink-0 rounded-md object-cover" />
                            ) : (
                                <span className="flex size-14 shrink-0 items-center justify-center rounded-md bg-muted">
                                    <Clapperboard className="size-5 text-muted-foreground" />
                                </span>
                            )}
                            <span className="min-w-0 flex-1">
                                <span className="block truncate font-medium">{item.title}</span>
                                <span className="flex items-center gap-2 text-xs text-muted-foreground">
                                    {new Date(item.createdAt).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}
                                    {item.composeJobId && (
                                        <Tag color="success" className="!m-0 !text-[11px]">
                                            已合成
                                        </Tag>
                                    )}
                                </span>
                            </span>
                        </button>
                    ))}
                </div>
            ) : (
                <Empty className="!mt-16" image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无作品" />
            ),
        },
        {
            key: "examples",
            label: "模板库",
            extra: (templates ?? []).length > 0 && (
                <Segmented size="small" value={category} options={[{ label: "全部", value: "" }, ...Object.entries(videoTemplateCategoryLabels).map(([value, label]) => ({ label, value }))]} onChange={setCategory} />
            ),
            content: shownTemplates.length ? (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
                    {shownTemplates.map((item) => (
                        <div key={item.slug} className="flex flex-col overflow-hidden rounded-xl border border-border bg-background">
                            <div className="relative aspect-[16/10] bg-linear-to-br from-rose-100 via-amber-50 to-orange-100 dark:from-rose-500/20 dark:via-amber-500/5 dark:to-orange-500/15">
                                {item.coverUrl ? <img src={item.coverUrl} alt="" className="absolute inset-0 size-full object-cover" /> : <LayoutTemplate className="absolute right-4 top-4 size-12 text-foreground/15" strokeWidth={1} />}
                                <span className="absolute left-2 top-2 rounded-md bg-black/60 px-2 py-0.5 text-[11px] text-white">{categoryLabel(item.category)}</span>
                            </div>
                            <div className="flex flex-1 flex-col gap-1 p-3">
                                <div className="truncate text-sm font-medium">{item.title}</div>
                                <div className="line-clamp-2 flex-1 text-xs text-muted-foreground">{item.description}</div>
                                <div className="flex items-center justify-between gap-2 pt-1 text-[11px] text-muted-foreground">
                                    <span className="truncate">
                                        {item.videoDefaults && `${item.videoDefaults.duration} 秒 · ${item.videoDefaults.ratio} · ${styleLabel(item.videoDefaults.style)}${item.videoDefaults.shots?.length ? ` · ${item.videoDefaults.shots.length} 镜` : ""}`}
                                    </span>
                                    <Button size="small" type="primary" onClick={() => applyTemplate(item)}>
                                        使用
                                    </Button>
                                </div>
                            </div>
                        </div>
                    ))}
                </div>
            ) : (
                <Empty className="!mt-16" image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无视频模板" />
            ),
        },
    ];

    return (
        <ImageToolShell
            title="电商带货视频"
            back={{ href: "/video", label: "返回视频创作" }}
            headerExtra={
                view && (
                    <Button size="small" icon={<Plus className="size-3" />} onClick={startNew}>
                        新建
                    </Button>
                )
            }
            form={
                <>
                    {stepBar}
                    {[productStep, settingsStep, scriptStep, generateStep][step]}
                </>
            }
            action={action}
            hint={hint}
            tabs={tabs}
        />
    );
}
