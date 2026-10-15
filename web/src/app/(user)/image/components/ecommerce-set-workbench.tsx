"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Checkbox, Empty, Image, Input, Segmented, Select, Spin, Tag } from "antd";
import { saveAs } from "file-saver";
import { Check, ChevronLeft, ChevronRight, Download, ImageIcon, Plus, RotateCcw, Sparkles, Trash2, WandSparkles } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import { defaultCustomCounts, languageOptions, marketOptions, platformOptions, setSlotEstimate, slotTypes, structureError, structureTotal } from "@/lib/ecommerce-options";
import { imageTool } from "@/lib/image-tool-examples";
import { typicalUsage } from "@/lib/model-catalog";
import {
    analyzeEcommerceSet,
    chooseEcommerceSlotVersion,
    createEcommerceSet,
    fetchEcommerceSet,
    generateEcommerceSet,
    listEcommerceSets,
    planEcommerceSet,
    regenerateEcommerceSlot,
    requestSellingPoints,
    updateEcommerceSet,
    type EcommerceSellingKind,
    type EcommerceSellingPoints,
    type EcommerceSetForm,
    type EcommerceSetView,
    type EcommerceStage,
} from "@/services/api/ecommerce";
import type { GenerationJob } from "@/services/api/jobs";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import { AiPillButton, FormSection, ImageToolShell, PriceText, Stepper, ToolExamples, useImagePrice, type ShellTab } from "./image-tool-shell";
import { UploadDropzone, pickedImageUrl, type PickedImage } from "./upload-dropzone";

type SkuDraft = { label: string; desc: string; image: PickedImage[] };

const MAX_SKUS = 5; // matches the server's ecommerceMaxSkus

const sellingKinds: { key: EcommerceSellingKind; label: string }[] = [
    { key: "main", label: "主图卖点" },
    { key: "white", label: "白底呈现要点" },
    { key: "detail", label: "详情卖点" },
];

const emptyPoints: EcommerceSellingPoints = { main: "", white: "", detail: "" };

const pendingLabels: Record<string, string> = { analyze: "AI 正在解析商品卖点…", plan: "AI 正在逐页规划套图方案…", rewrite: "AI 正在优化卖点…", options: "AI 正在构思新的卖点方向…" };

function isActive(job?: GenerationJob) {
    return job?.status === "queued" || job?.status === "running";
}

function ChoiceCard({ active, title, description, onClick }: { active: boolean; title: string; description: string; onClick: () => void }) {
    return (
        <button type="button" aria-pressed={active} className={`w-full rounded-lg border p-4 text-left transition ${active ? "border-foreground" : "border-border hover:border-foreground/40"}`} onClick={onClick}>
            <span className="flex items-center justify-between gap-2 text-sm font-medium">
                {title}
                <span className={`flex size-4 items-center justify-center rounded border ${active ? "border-foreground bg-foreground text-background" : "border-border"}`}>{active && <Check className="size-3" strokeWidth={3} />}</span>
            </span>
            <span className="mt-1 block text-xs text-muted-foreground">{description}</span>
        </button>
    );
}

export function EcommerceSetWorkbench({ preset }: { preset: "set" | "detail" }) {
    const { message } = App.useApp();
    const router = useRouter();
    const searchParams = useSearchParams();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const ecommerce = useConfigStore((state) => state.publicSettings?.ecommerce);
    const price = useImagePrice();
    const setId = searchParams.get("id") || "";
    const [products, setProducts] = useState<PickedImage[]>([]);
    const [reference, setReference] = useState<PickedImage[]>([]);
    const [market, setMarket] = useState("中国大陆");
    const [platform, setPlatform] = useState("淘宝");
    const [language, setLanguage] = useState("zh");
    const [styleMode, setStyleMode] = useState<EcommerceSetForm["styleMode"]>("ai");
    const [styleText, setStyleText] = useState("");
    const [modelDescription, setModelDescription] = useState("");
    const [structureMode, setStructureMode] = useState<"smart" | "custom">("smart");
    const [counts, setCounts] = useState<Record<string, number>>(defaultCustomCounts);
    const [skus, setSkus] = useState<SkuDraft[]>([]);
    const [points, setPoints] = useState<EcommerceSellingPoints>(emptyPoints);
    const [finalIds, setFinalIds] = useState<string[]>([]);
    const [busy, setBusy] = useState("");
    // "生成预览" chains analyze → plan → preview; this is the step it is waiting on, per set.
    const [auto, setAuto] = useState<{ id: string; step: "analyze" | "plan" } | null>(null);
    const [hydratedId, setHydratedId] = useState("");
    // Version a result card is browsing, by "slotId:stage"; unset shows the slot's current image.
    const [browsing, setBrowsing] = useState<Record<string, string>>({});

    const { data: view } = useQuery({
        queryKey: ["ecommerce-set", setId],
        queryFn: () => fetchEcommerceSet(token!, setId),
        enabled: Boolean(token && setId),
        refetchInterval: (query) => {
            const data = query.state.data;
            return data && ((data.pending ?? []).length > 0 || (data.jobs ?? []).some(isActive)) ? 3000 : false;
        },
    });
    const { data: history } = useQuery({ queryKey: ["ecommerce-sets", token, preset], queryFn: () => listEcommerceSets(token!, preset), enabled: Boolean(token) });
    const jobs = useMemo(() => new Map((view?.jobs ?? []).map((job) => [job.id, job])), [view]);
    const slots = view?.slots ?? [];
    const pending = view?.pending ?? [];
    const aiBusy = pending.length > 0;
    const sameProducts = Boolean(view) && JSON.stringify(view?.form.productFileIds ?? []) === JSON.stringify(products.map((item) => item.fileId));
    const analysis = sameProducts ? view?.analysis : null;
    const serverPoints = JSON.stringify(view?.analysis?.sellingPoints ?? null);
    const structureProblem = preset === "set" && structureMode === "custom" ? structureError(counts) : "";
    const previewCount = setSlotEstimate(preset, structureMode, counts, skus.filter((sku) => sku.label.trim()).length);
    const selectedFinal = finalIds.filter((id) => slots.some((slot) => slot.id === id));

    // Load a saved set into the form once per set.
    useEffect(() => {
        if (!view || view.id === hydratedId) return;
        setHydratedId(view.id);
        setProducts((view.form.productFileIds ?? []).map((fileId) => ({ fileId, url: pickedImageUrl(fileId) })));
        setReference(view.form.referenceFileId ? [{ fileId: view.form.referenceFileId, url: pickedImageUrl(view.form.referenceFileId) }] : []);
        setMarket(view.form.market);
        setPlatform(view.form.platform);
        setLanguage(view.form.language);
        setStyleMode(view.form.styleMode);
        setStyleText(view.form.styleText);
        setModelDescription(view.form.modelDescription);
        setStructureMode(view.form.structure?.mode ?? "smart");
        setCounts(view.form.structure?.counts ?? defaultCustomCounts);
        setSkus((view.form.skus ?? []).map((sku) => ({ label: sku.label, desc: sku.desc, image: sku.fileId ? [{ fileId: sku.fileId, url: pickedImageUrl(sku.fileId) }] : [] })));
        setFinalIds([]);
    }, [view, hydratedId]);

    // Selling points follow the server only when an AI job changed them, so typing is not overwritten by polling.
    useEffect(() => {
        const parsed = JSON.parse(serverPoints) as EcommerceSellingPoints | null;
        if (parsed) setPoints(parsed);
    }, [serverPoints]);

    const act = async (name: string, action: () => Promise<EcommerceSetView>) => {
        setBusy(name);
        try {
            const next = await action();
            queryClient.setQueryData(["ecommerce-set", next.id], next);
            void queryClient.invalidateQueries({ queryKey: ["ecommerce-sets"] });
        } catch (error) {
            setAuto(null);
            message.error(error instanceof Error ? error.message : "操作失败");
        } finally {
            setBusy("");
        }
    };

    // Advances the preview chain once the set is idle; a failed step stops it (the set's notice says why).
    useEffect(() => {
        if (!auto || !token || busy || view?.id !== auto.id || (view.pending ?? []).length) return;
        if (auto.step === "analyze" && view.analysis) {
            setAuto({ id: view.id, step: "plan" });
            void act("preview", () => planEcommerceSet(token, view.id));
            return;
        }
        setAuto(null);
        if (auto.step === "plan" && view.planned) void act("preview", () => generateEcommerceSet(token, view.id, "preview", []));
    }, [auto, busy, view]);

    if (!token) return <Empty className="mt-20" description="请先登录后使用" />;

    const formPayload = (): EcommerceSetForm => ({
        preset,
        productFileIds: products.map((item) => item.fileId),
        market,
        platform,
        language,
        styleMode,
        styleText,
        referenceFileId: styleMode === "reference" ? (reference[0]?.fileId ?? "") : "",
        modelDescription,
        structure: structureMode === "custom" ? { mode: "custom", counts } : { mode: "smart" },
        skus: preset === "set" ? skus.filter((sku) => sku.label.trim()).map((sku) => ({ label: sku.label.trim(), desc: sku.desc.trim(), fileId: sku.image[0]?.fileId ?? "" })) : [],
        ...(analysis ? { sellingPoints: points } : {}),
    });

    // Creates the set on first use (or when the product images change); otherwise saves the form so
    // the server works with what is on screen.
    const saveSet = async () => {
        if (!products.length) throw new Error("请先上传商品图");
        if (view && sameProducts) return updateEcommerceSet(token, view.id, formPayload());
        const created = await createEcommerceSet(token, formPayload());
        setHydratedId(created.id);
        router.replace(`?id=${created.id}`);
        return created;
    };

    const preview = () =>
        act("preview", async () => {
            const saved = await saveSet();
            if (!saved.analysis) {
                setAuto({ id: saved.id, step: "analyze" });
                return analyzeEcommerceSet(token, saved.id);
            }
            if (saved.planned) return generateEcommerceSet(token, saved.id, "preview", []);
            setAuto({ id: saved.id, step: "plan" });
            return planEcommerceSet(token, saved.id);
        });

    const openSet = (id: string) => {
        setAuto(null);
        router.replace(id ? `?id=${id}` : "?");
    };

    const startNew = () => {
        openSet("");
        setHydratedId("");
        setProducts([]);
        setReference([]);
        setPoints(emptyPoints);
        setSkus([]);
        setFinalIds([]);
    };


    const updateSku = (index: number, patch: Partial<SkuDraft>) => setSkus((value) => value.map((item, i) => (i === index ? { ...item, ...patch } : item)));
    const sellingBusy = (purpose: string, kind: string) => busy === `${purpose}-${kind}` || pending.some((item) => item.purpose === purpose && item.kind === kind);
    const previewLabel = auto?.step === "analyze" ? "解析卖点中…" : auto?.step === "plan" ? "规划方案中…" : "生成预览";
    const doneCount = (stage: "preview" | "final") => slots.filter((slot) => jobs.get((stage === "preview" ? slot.previewJobId : slot.finalJobId) || "")?.outputUrl).length;
    const visionModel = ecommerce?.visionModel || "";
    const imageModel = ecommerce?.imageModel || "";
    const analyzeCredits = price([{ model: visionModel, usage: typicalUsage.analyze }]);
    const rewriteCredits = price([{ model: visionModel, usage: typicalUsage.rewrite }]);
    // "生成预览" runs whichever of analyze and plan are still missing before the preview images.
    const previewCredits = price([
        ...(analysis ? [] : [{ model: visionModel, usage: typicalUsage.analyze }]),
        ...(view?.planned && sameProducts ? [] : [{ model: visionModel, usage: typicalUsage.plan }]),
        { model: imageModel, count: previewCount },
    ]);
    const finalCredits = price([{ model: imageModel, count: selectedFinal.length }]);
    const oneImageCredits = price([{ model: imageModel, count: 1 }]);
    const showVersion = (key: string, jobId?: string) => setBrowsing(({ [key]: _, ...rest }) => (jobId ? { ...rest, [key]: jobId } : rest));
    const analyzing = busy === "analyze" || pending.some((item) => item.purpose === "analyze");
    const analyze = () => void act("analyze", async () => analyzeEcommerceSet(token, (await saveSet()).id));
    const hint = !products.length
        ? "请先上传至少一张商品图"
        : structureProblem || (pending.length ? pendingLabels[pending[0].purpose] : "先低画质预览确认方案，再在结果中勾选生成高清正式图");

    const form = (
        <>
            <div className="space-y-2">
                <UploadDropzone label="上传商品图" max={6} value={products} onChange={setProducts} />
                {view && !sameProducts && <div className="text-xs text-amber-600 dark:text-amber-500">商品图已更换，下一步会新建一个{preset === "set" ? "套图" : "详情图"}。</div>}
            </div>

            <FormSection title="投放设置">
                <label className="block space-y-1.5">
                    <span className="text-xs text-muted-foreground">目标市场</span>
                    <Select size="large" className="!w-full" value={market} options={marketOptions} onChange={setMarket} />
                </label>
                <div className="grid grid-cols-2 gap-3">
                    <label className="block space-y-1.5">
                        <span className="text-xs text-muted-foreground">目标平台</span>
                        <Select size="large" className="!w-full" value={platform} options={platformOptions} onChange={setPlatform} />
                    </label>
                    <label className="block space-y-1.5">
                        <span className="text-xs text-muted-foreground">文案语言</span>
                        <Select size="large" className="!w-full" value={language} options={languageOptions} onChange={setLanguage} />
                    </label>
                </div>
            </FormSection>

            <FormSection
                title="产品卖点"
                extra={
                    <AiPillButton credits={analyzeCredits} loading={analyzing} disabled={!products.length || (aiBusy && !analyzing)} onClick={analyze}>
                        {analysis ? "重新解析" : "一键解析"}
                    </AiPillButton>
                }
            >
                {view?.notice && sameProducts && <div className="rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">{view.notice}</div>}
                {analysis ? (
                    sellingKinds.map(({ key, label }) => (
                        <div key={key} className="space-y-1.5">
                            <div className="flex items-center justify-between gap-1 text-xs">
                                <span className="text-muted-foreground">{label}</span>
                                <span className="flex">
                                    <Button
                                        size="small"
                                        type="text"
                                        icon={<WandSparkles className="size-3" />}
                                        loading={sellingBusy("rewrite", key)}
                                        disabled={aiBusy && !sellingBusy("rewrite", key)}
                                        onClick={() => void act(`rewrite-${key}`, async () => requestSellingPoints(token, (await saveSet()).id, key, "rewrite"))}
                                    >
                                        AI 优化
                                        <PriceText credits={rewriteCredits} prefix="·" className="opacity-60" />
                                    </Button>
                                    <Button
                                        size="small"
                                        type="text"
                                        loading={sellingBusy("options", key)}
                                        disabled={aiBusy && !sellingBusy("options", key)}
                                        onClick={() => void act(`options-${key}`, async () => requestSellingPoints(token, (await saveSet()).id, key, "options"))}
                                    >
                                        换个方向
                                        <PriceText credits={rewriteCredits} prefix="·" className="opacity-60" />
                                    </Button>
                                </span>
                            </div>
                            <Input.TextArea value={points[key]} maxLength={500} autoSize={{ minRows: 3, maxRows: 6 }} onChange={(event) => setPoints((value) => ({ ...value, [key]: event.target.value }))} />
                            {(view?.sellingOptions?.[key] ?? []).length > 0 && (
                                <div className="flex flex-wrap gap-1">
                                    {view!.sellingOptions[key].map((option) => (
                                        <Tag key={option} className="!m-0 cursor-pointer whitespace-normal" onClick={() => setPoints((value) => ({ ...value, [key]: option }))}>
                                            {option}
                                        </Tag>
                                    ))}
                                </div>
                            )}
                        </div>
                    ))
                ) : (
                    <div className="rounded-lg border border-border px-3 py-3 text-xs leading-6 text-muted-foreground">
                        AI 会从商品图提炼<span className="text-foreground">主图卖点、白底呈现要点、详情卖点</span>三类卖点，并推荐风格与整体设计规范。也可以直接点“生成预览”，会自动先解析。
                    </div>
                )}
            </FormSection>

            <FormSection title="设计风格">
                <Segmented
                    block
                    value={styleMode}
                    onChange={(value) => {
                        setStyleMode(value as EcommerceSetForm["styleMode"]);
                        setStyleText("");
                    }}
                    options={[
                        { label: "AI 推荐", value: "ai" },
                        { label: "参考排版", value: "reference" },
                        { label: "自定义", value: "custom" },
                    ]}
                />
                {styleMode === "ai" &&
                    (analysis?.styleOptions?.length ? (
                        <div className="space-y-2">
                            <div className="flex flex-wrap gap-1.5">
                                {analysis.styleOptions.map((option) => (
                                    <Tag.CheckableTag key={option} className="!m-0 !rounded-full !border !border-border !px-3 !py-1" checked={styleText === option} onChange={(checked) => setStyleText(checked ? option : "")}>
                                        {option}
                                    </Tag.CheckableTag>
                                ))}
                            </div>
                            {!styleText && analysis.designStyle && <div className="text-xs text-muted-foreground">未选择时使用：{analysis.designStyle}</div>}
                        </div>
                    ) : (
                        <div className="flex flex-col items-center gap-2 rounded-lg bg-muted/50 px-3 py-8 text-center">
                            <AiPillButton credits={analyzeCredits} loading={analyzing} disabled={!products.length || (aiBusy && !analyzing)} onClick={analyze}>
                                AI 推荐风格
                            </AiPillButton>
                            <span className="text-xs text-muted-foreground">{products.length ? "解析商品后推荐 5 种风格" : "上传商品图后可用"}</span>
                        </div>
                    ))}
                {styleMode === "reference" && <UploadDropzone max={1} value={reference} onChange={setReference} />}
                {styleMode === "custom" && <Input.TextArea value={styleText} maxLength={500} autoSize={{ minRows: 3, maxRows: 5 }} placeholder="例如：莫兰迪色系、大留白、杂志感排版" onChange={(event) => setStyleText(event.target.value)} />}
                {analysis?.needsModel && <Input size="large" value={modelDescription} maxLength={200} placeholder={`模特设定（默认：${analysis.modelPrompt}）`} onChange={(event) => setModelDescription(event.target.value)} />}
            </FormSection>

            {preset === "set" ? (
                <FormSection title="套图结构" extra={<span className="text-xs text-muted-foreground">共 {structureMode === "custom" ? structureTotal(counts) : 9} 张{skus.length ? ` + ${skus.length} 张 SKU` : ""}</span>}>
                    <ChoiceCard active={structureMode === "smart"} title="智能匹配" description="AI 分析商品图，生成 4 张主图、1 张白底图与 4 张详情图" onClick={() => setStructureMode("smart")} />
                    <ChoiceCard active={structureMode === "custom"} title="自定义配置" description="自由调整各类型图片数量，7–15 张" onClick={() => setStructureMode("custom")} />
                    {structureMode === "custom" && (
                        <div className="grid grid-cols-1 gap-x-4 gap-y-2 rounded-lg bg-muted/50 p-3 sm:grid-cols-2">
                            {slotTypes.map((type) => (
                                <div key={type.key} className="flex items-center justify-between gap-2 text-xs">
                                    <span className="truncate" title={type.title}>
                                        {type.title}
                                    </span>
                                    <Stepper max={5} value={counts[type.key] ?? 0} onChange={(value) => setCounts((current) => ({ ...current, [type.key]: value }))} />
                                </div>
                            ))}
                            {structureProblem && <div className="text-xs text-red-500 sm:col-span-2">{structureProblem}</div>}
                        </div>
                    )}
                    <div className="flex items-center justify-between pt-1 text-xs">
                        <span className="text-muted-foreground">
                            SKU 白底图（{skus.length}/{MAX_SKUS}）
                        </span>
                        <Button size="small" type="text" icon={<Plus className="size-3" />} disabled={skus.length >= MAX_SKUS} onClick={() => setSkus((value) => [...value, { label: "", desc: "", image: [] }])}>
                            添加 SKU
                        </Button>
                    </div>
                    {skus.map((sku, index) => (
                        <div key={index} className="space-y-2 rounded-lg border border-border p-3">
                            <div className="flex gap-1">
                                <Input placeholder="SKU 名，如 红色 XL" value={sku.label} maxLength={30} onChange={(event) => updateSku(index, { label: event.target.value })} />
                                <Button type="text" danger icon={<Trash2 className="size-3.5" />} aria-label="删除 SKU" onClick={() => setSkus((value) => value.filter((_, i) => i !== index))} />
                            </div>
                            <Input placeholder="款式描述（可选）" value={sku.desc} maxLength={200} onChange={(event) => updateSku(index, { desc: event.target.value })} />
                            <UploadDropzone label="该 SKU 参考图（可选）" max={1} compact value={sku.image} onChange={(image) => updateSku(index, { image })} />
                        </div>
                    ))}
                </FormSection>
            ) : (
                <FormSection title="页面结构" extra={<span className="text-xs text-muted-foreground">固定 7 屏</span>}>
                    <div className="flex flex-wrap gap-1.5">
                        {imageTool("detail").items.map((item, index) => (
                            <span key={item} className="rounded-full border border-border px-3 py-1 text-xs">
                                {String(index + 1).padStart(2, "0")} {item}
                            </span>
                        ))}
                    </div>
                </FormSection>
            )}
        </>
    );

    const resultTab: ShellTab = {
        key: "result",
        label: "结果",
        extra: slots.length > 0 && (
            <div className="flex items-center gap-1 text-xs">
                <span className="text-muted-foreground">已选 {selectedFinal.length}</span>
                <Button size="small" type="text" onClick={() => setFinalIds(slots.map((slot) => slot.id))}>
                    全选
                </Button>
                <Button size="small" type="text" disabled={!selectedFinal.length} onClick={() => setFinalIds([])}>
                    清空
                </Button>
            </div>
        ),
        footer: selectedFinal.length > 0 && (
            <div className="flex items-center justify-between gap-3">
                <span className="text-xs text-muted-foreground">已勾选 {selectedFinal.length} 张，生成高清正式图</span>
                <Button type="primary" className="!h-10 !rounded-lg" icon={<Sparkles className="size-4" />} loading={busy === "final"} disabled={!sameProducts} onClick={() => void act("final", () => generateEcommerceSet(token, view!.id, "final", selectedFinal))}>
                    生成正式图（{selectedFinal.length} 张）
                    <PriceText credits={finalCredits} prefix="·" className="text-xs opacity-80" />
                </Button>
            </div>
        ),
        content: view && (
            <div className="space-y-4">
                <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate font-medium">{view.title}</span>
                    {slots.length > 0 && <Tag className="!m-0">预览 {doneCount("preview")}/{slots.length}</Tag>}
                    {doneCount("final") > 0 && (
                        <Tag color="success" className="!m-0">
                            正式 {doneCount("final")}
                        </Tag>
                    )}
                </div>
                {pending.length > 0 && (
                    <div className="flex items-center gap-2 rounded-lg bg-sky-500/10 px-3 py-2 text-sm text-sky-700 dark:text-sky-300">
                        <Spin size="small" />
                        {pendingLabels[pending[0].purpose]}
                        {pending[0].purpose === "plan" && view.planRound > 1 && <span className="text-xs opacity-80">（补全第 {view.planRound} 轮）</span>}
                    </div>
                )}
                {!slots.length && !pending.length && <Empty className="!mt-16" image={<ImageIcon className="mx-auto size-12 text-muted-foreground/40" />} description="点击“生成预览”，方案与预览图会显示在这里" />}
                <Image.PreviewGroup>
                    <div className="grid grid-cols-2 gap-3 md:grid-cols-3 2xl:grid-cols-4">
                        {slots.map((slot) => {
                            const previewJob = slot.previewJobId ? jobs.get(slot.previewJobId) : undefined;
                            const finalJob = slot.finalJobId ? jobs.get(slot.finalJobId) : undefined;
                            // Once any final image exists the card shows (and versions) the final stage.
                            const stage: EcommerceStage = (slot.finalJobIds ?? []).some((id) => jobs.get(id)?.outputUrl) ? "final" : "preview";
                            const current = stage === "final" ? finalJob : previewJob;
                            const versions = (stage === "final" ? slot.finalJobIds : slot.previewJobIds) ?? [];
                            const versionKey = `${slot.id}:${stage}`;
                            const shownId = browsing[versionKey] && versions.includes(browsing[versionKey]) ? browsing[versionKey] : current?.id;
                            const shown = shownId ? jobs.get(shownId) : undefined;
                            const versionIndex = shownId ? versions.indexOf(shownId) : -1;
                            const running = isActive(finalJob) ? finalJob : isActive(previewJob) ? previewJob : undefined;
                            const canRegenerate = !running && versions.some((id) => jobs.get(id)?.status === "succeeded");
                            const checked = finalIds.includes(slot.id);
                            return (
                                <div key={slot.id} className={`overflow-hidden rounded-xl border bg-background transition ${checked ? "border-foreground ring-1 ring-foreground" : "border-border"}`}>
                                    <div className="relative aspect-[3/4] bg-muted">
                                        <Checkbox
                                            className="!absolute left-2 top-2 z-10"
                                            aria-label={`选择 ${slot.title} 生成正式图`}
                                            checked={checked}
                                            onChange={(event) => setFinalIds((value) => (event.target.checked ? [...value, slot.id] : value.filter((id) => id !== slot.id)))}
                                        />
                                        {shown?.outputUrl ? (
                                            <Image src={shown.outputUrl} alt={slot.title} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-contain" />
                                        ) : (
                                            <div className="flex h-full flex-col items-center justify-center gap-2 p-3 text-center text-xs text-muted-foreground">
                                                {running ? (
                                                    <>
                                                        <Spin />
                                                        {running.status === "queued" ? "排队中" : running === finalJob ? "正式图生成中" : "预览生成中"}
                                                    </>
                                                ) : shown?.error ? (
                                                    <span className="line-clamp-4 text-red-500">{shown.error}</span>
                                                ) : view.slotPrompts?.[slot.id] ? (
                                                    "方案已就绪"
                                                ) : (
                                                    "待生成"
                                                )}
                                            </div>
                                        )}
                                        {shown?.outputUrl && running && (
                                            <span className="absolute bottom-2 left-2 flex items-center gap-1 rounded bg-black/60 px-1.5 py-0.5 text-[11px] text-white">
                                                <Spin size="small" /> {running === finalJob ? "正式图生成中" : "重新预览中"}
                                            </span>
                                        )}
                                    </div>
                                    <div className="flex items-center justify-between gap-1 px-2.5 py-2 text-xs">
                                        <span className="truncate" title={slot.theme || slot.title}>
                                            {slot.title}
                                        </span>
                                        <span className="flex shrink-0 items-center gap-1">
                                            {finalJob?.outputUrl ? (
                                                <Tag color="success" className="!m-0">
                                                    正式
                                                </Tag>
                                            ) : previewJob?.outputUrl ? (
                                                <Tag className="!m-0">预览</Tag>
                                            ) : null}
                                            {shown?.outputUrl && (
                                                <Button size="small" type="text" icon={<Download className="size-3" />} aria-label={`下载 ${slot.title}`} onClick={() => saveAs(shown.outputUrl!, `${slot.title}.png`)} />
                                            )}
                                        </span>
                                    </div>
                                    {(canRegenerate || versions.length > 1) && (
                                        <div className="flex items-center justify-between gap-1 border-t border-border px-1.5 py-1 text-xs">
                                            {versions.length > 1 ? (
                                                <span className="flex items-center">
                                                    <Button size="small" type="text" icon={<ChevronLeft className="size-3" />} aria-label="上一版本" disabled={versionIndex <= 0} onClick={() => showVersion(versionKey, versions[versionIndex - 1])} />
                                                    <span className="tabular-nums text-muted-foreground">
                                                        {versionIndex + 1}/{versions.length}
                                                    </span>
                                                    <Button size="small" type="text" icon={<ChevronRight className="size-3" />} aria-label="下一版本" disabled={versionIndex >= versions.length - 1} onClick={() => showVersion(versionKey, versions[versionIndex + 1])} />
                                                    {shownId !== current?.id && (
                                                        <Button
                                                            size="small"
                                                            type="link"
                                                            loading={busy === `current-${versionKey}`}
                                                            disabled={!sameProducts}
                                                            onClick={() =>
                                                                void act(`current-${versionKey}`, async () => {
                                                                    const next = await chooseEcommerceSlotVersion(token, view.id, slot.id, stage, shownId!);
                                                                    showVersion(versionKey);
                                                                    return next;
                                                                })
                                                            }
                                                        >
                                                            设为当前
                                                        </Button>
                                                    )}
                                                </span>
                                            ) : (
                                                <span />
                                            )}
                                            {canRegenerate && (
                                                <Button
                                                    size="small"
                                                    type="text"
                                                    icon={<RotateCcw className="size-3" />}
                                                    loading={busy === `regenerate-${versionKey}`}
                                                    disabled={!sameProducts}
                                                    onClick={() =>
                                                        void act(`regenerate-${versionKey}`, async () => {
                                                            const next = await regenerateEcommerceSlot(token, view.id, slot.id, stage);
                                                            showVersion(versionKey);
                                                            return next;
                                                        })
                                                    }
                                                >
                                                    再来一次
                                                    <PriceText credits={oneImageCredits} prefix="·" className="opacity-60" />
                                                </Button>
                                            )}
                                        </div>
                                    )}
                                </div>
                            );
                        })}
                    </div>
                </Image.PreviewGroup>
            </div>
        ),
    };

    const tabs: ShellTab[] = [
        ...(view ? [resultTab] : []),
        {
            key: "history",
            label: "历史",
            content: history?.length ? (
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
                    {history.map((item) => (
                        <button
                            key={item.id}
                            type="button"
                            className={`flex w-full items-center gap-3 rounded-lg border p-2 text-left text-sm transition hover:border-foreground/40 ${item.id === setId ? "border-foreground" : "border-border"}`}
                            onClick={() => openSet(item.id)}
                        >
                            {item.coverUrl ? (
                                <img src={item.coverUrl} alt="" className="size-14 shrink-0 rounded-md object-cover" />
                            ) : (
                                <span className="flex size-14 shrink-0 items-center justify-center rounded-md bg-muted">
                                    <ImageIcon className="size-5 text-muted-foreground" />
                                </span>
                            )}
                            <span className="min-w-0 flex-1">
                                <span className="block truncate font-medium">{item.title}</span>
                                <span className="block text-xs text-muted-foreground">{new Date(item.createdAt).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}</span>
                            </span>
                        </button>
                    ))}
                </div>
            ) : (
                <Empty className="!mt-16" image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史" />
            ),
        },
        { key: "examples", label: "示例", content: <ToolExamples tool={preset} /> },
    ];

    return (
        <ImageToolShell
            title={imageTool(preset).title}
            headerExtra={
                view && (
                    <Button size="small" icon={<Plus className="size-3" />} onClick={startNew}>
                        新建
                    </Button>
                )
            }
            form={form}
            action={
                <Button
                    type="primary"
                    block
                    className="!h-11 !rounded-lg"
                    icon={<Sparkles className="size-4" />}
                    loading={busy === "preview" || Boolean(auto)}
                    disabled={!products.length || Boolean(structureProblem) || (aiBusy && !auto)}
                    onClick={() => void preview()}
                >
                    {previewLabel}（{previewCount} 张）
                    <PriceText credits={previewCredits} prefix="消耗" className="text-xs opacity-80" />
                </Button>
            }
            hint={hint}
            tabs={tabs}
        />
    );
}
