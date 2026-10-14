"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Checkbox, Empty, Image, Input, InputNumber, Segmented, Select, Spin, Tabs, Tag } from "antd";
import { saveAs } from "file-saver";
import { Download, ImageIcon, Plus, Sparkles, Trash2, WandSparkles } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import { CreditEstimate } from "@/components/billing/credit-estimate";
import { defaultCustomCounts, languageOptions, marketOptions, platformOptions, setSlotEstimate, slotTypes, structureError } from "@/lib/ecommerce-options";
import { platformChannelId } from "@/lib/model-catalog";
import {
    analyzeEcommerceSet,
    createEcommerceSet,
    fetchEcommerceSet,
    generateEcommerceSet,
    listEcommerceSets,
    planEcommerceSet,
    requestSellingPoints,
    updateEcommerceSet,
    type EcommerceSellingKind,
    type EcommerceSellingPoints,
    type EcommerceSetForm,
    type EcommerceSetView,
} from "@/services/api/ecommerce";
import type { GenerationJob } from "@/services/api/jobs";
import { useConfigStore, useModelCatalog } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import { ImageInputPicker, pickedImageUrl, type PickedImage } from "./image-input-picker";

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

function Panel({ title, extra, children }: { title?: string; extra?: ReactNode; children: ReactNode }) {
    return (
        <section className="rounded-lg border border-stone-200 bg-background dark:border-stone-800">
            {title && (
                <div className="flex items-center justify-between gap-2 border-b border-stone-200 px-3 py-2 dark:border-stone-800">
                    <span className="text-sm font-medium">{title}</span>
                    {extra}
                </div>
            )}
            <div className="space-y-2 p-3">{children}</div>
        </section>
    );
}

export function EcommerceSetWorkbench({ preset }: { preset: "set" | "detail" }) {
    const { message } = App.useApp();
    const router = useRouter();
    const searchParams = useSearchParams();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const ecommerce = useConfigStore((state) => state.publicSettings?.ecommerce);
    const catalog = useModelCatalog();
    // E-commerce jobs bill the SSO account group ("newapi@" on the server).
    const channelId = catalog ? platformChannelId(catalog.userGroup) : undefined;
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

    return (
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3 text-stone-900 dark:text-stone-100 lg:grid lg:grid-cols-[400px_minmax(0,1fr)_260px] lg:overflow-hidden">
            <aside className="flex flex-col overflow-hidden rounded-lg border border-stone-200 bg-card shadow-sm dark:border-stone-800 lg:min-h-0">
                <div className="flex shrink-0 items-center justify-between gap-2 px-4 pb-2 pt-3">
                    <div className="min-w-0">
                        <h1 className="text-base font-semibold">{preset === "set" ? "商品套图" : "详情图 / A+"}</h1>
                        <p className="truncate text-xs text-stone-500">上传商品图 → 解析卖点 → 低价预览 → 勾选生成正式图</p>
                    </div>
                    {view && (
                        <Button size="small" icon={<Plus className="size-3" />} onClick={startNew}>
                            新建
                        </Button>
                    )}
                </div>

                <div className="thin-scrollbar min-h-0 flex-1 space-y-3 overflow-y-auto px-4 pb-3">
                    <Panel>
                        <ImageInputPicker label="商品图" max={6} value={products} onChange={setProducts} />
                        {view && !sameProducts && <div className="text-xs text-amber-600 dark:text-amber-500">商品图已更换，下一步会新建一个套图。</div>}
                    </Panel>

                    <Panel title="投放设置">
                        <div className="grid grid-cols-3 gap-2 text-xs text-stone-500">
                            <label className="space-y-1">
                                <span>目标市场</span>
                                <Select className="w-full" value={market} options={marketOptions} onChange={setMarket} />
                            </label>
                            <label className="space-y-1">
                                <span>目标平台</span>
                                <Select className="w-full" value={platform} options={platformOptions} onChange={setPlatform} />
                            </label>
                            <label className="space-y-1">
                                <span>文案语言</span>
                                <Select className="w-full" value={language} options={languageOptions} onChange={setLanguage} />
                            </label>
                        </div>
                    </Panel>

                    <Panel
                        title="产品卖点"
                        extra={
                            <Button
                                size="small"
                                type={analysis ? "default" : "primary"}
                                icon={<Sparkles className="size-3" />}
                                loading={busy === "analyze" || pending.some((item) => item.purpose === "analyze")}
                                disabled={!products.length || (aiBusy && !pending.some((item) => item.purpose === "analyze"))}
                                onClick={() => void act("analyze", async () => analyzeEcommerceSet(token, (await saveSet()).id))}
                            >
                                {analysis ? "重新解析" : "一键解析卖点"}
                                <CreditEstimate model={ecommerce?.visionModel || ""} channelId={channelId} className="ml-1 text-xs opacity-80" />
                            </Button>
                        }
                    >
                        {view?.notice && sameProducts && <div className="rounded bg-amber-500/10 px-2 py-1 text-xs text-amber-700 dark:text-amber-400">{view.notice}</div>}
                        {analysis ? (
                            sellingKinds.map(({ key, label }) => (
                                <div key={key} className="space-y-1">
                                    <div className="flex items-center justify-between text-xs">
                                        <span className="text-stone-500">{label}</span>
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
                                                <CreditEstimate model={ecommerce?.visionModel || ""} channelId={channelId} className="ml-1 opacity-70" />
                                            </Button>
                                            <Button
                                                size="small"
                                                type="text"
                                                loading={sellingBusy("options", key)}
                                                disabled={aiBusy && !sellingBusy("options", key)}
                                                onClick={() => void act(`options-${key}`, async () => requestSellingPoints(token, (await saveSet()).id, key, "options"))}
                                            >
                                                换个方向
                                            </Button>
                                        </span>
                                    </div>
                                    <Input.TextArea value={points[key]} maxLength={500} autoSize={{ minRows: 2, maxRows: 5 }} onChange={(event) => setPoints((value) => ({ ...value, [key]: event.target.value }))} />
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
                            <div className="text-xs leading-5 text-stone-500">AI 会从商品图提炼主图、白底、详情三类卖点，并推荐风格与整体设计规范。也可以直接点"生成预览"，会自动先解析。</div>
                        )}
                    </Panel>

                    <Panel title="设计风格">
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
                                <>
                                    <div className="flex flex-wrap gap-1">
                                        {analysis.styleOptions.map((option) => (
                                            <Tag.CheckableTag key={option} className="!m-0" checked={styleText === option} onChange={(checked) => setStyleText(checked ? option : "")}>
                                                {option}
                                            </Tag.CheckableTag>
                                        ))}
                                    </div>
                                    {!styleText && analysis.designStyle && <div className="text-xs text-stone-500">未选择时使用：{analysis.designStyle}</div>}
                                </>
                            ) : (
                                <div className="text-xs text-stone-500">解析卖点后会推荐 5 种风格</div>
                            ))}
                        {styleMode === "reference" && <ImageInputPicker label="参考排版图" max={1} value={reference} onChange={setReference} />}
                        {styleMode === "custom" && <Input.TextArea value={styleText} maxLength={500} autoSize={{ minRows: 2, maxRows: 4 }} placeholder="例如：莫兰迪色系、大留白、杂志感排版" onChange={(event) => setStyleText(event.target.value)} />}
                        {analysis?.needsModel && <Input value={modelDescription} maxLength={200} placeholder={`模特设定（默认：${analysis.modelPrompt}）`} onChange={(event) => setModelDescription(event.target.value)} />}
                    </Panel>

                    {preset === "set" ? (
                        <Panel title="套图结构">
                            <Segmented
                                block
                                value={structureMode}
                                onChange={(value) => setStructureMode(value as "smart" | "custom")}
                                options={[
                                    { label: "智能匹配（9 张）", value: "smart" },
                                    { label: "自定义", value: "custom" },
                                ]}
                            />
                            {structureMode === "custom" && (
                                <div className="grid grid-cols-2 gap-x-3 gap-y-1.5">
                                    {slotTypes.map((type) => (
                                        <label key={type.key} className="flex items-center justify-between gap-2 text-xs">
                                            <span className="truncate" title={type.title}>
                                                {type.title}
                                            </span>
                                            <InputNumber size="small" className="!w-14 shrink-0" min={0} max={5} value={counts[type.key] ?? 0} onChange={(value) => setCounts((current) => ({ ...current, [type.key]: Number(value) || 0 }))} />
                                        </label>
                                    ))}
                                    {structureProblem && <div className="col-span-2 text-xs text-red-500">{structureProblem}</div>}
                                </div>
                            )}
                            <div className="flex items-center justify-between pt-1 text-xs">
                                <span className="text-stone-500">
                                    SKU 白底图（{skus.length}/{MAX_SKUS}）
                                </span>
                                <Button size="small" type="text" icon={<Plus className="size-3" />} disabled={skus.length >= MAX_SKUS} onClick={() => setSkus((value) => [...value, { label: "", desc: "", image: [] }])}>
                                    添加 SKU
                                </Button>
                            </div>
                            {skus.map((sku, index) => (
                                <div key={index} className="space-y-1.5 rounded-md border border-dashed border-stone-300 p-2 dark:border-stone-700">
                                    <div className="flex gap-1">
                                        <Input size="small" placeholder="SKU 名，如 红色 XL" value={sku.label} maxLength={30} onChange={(event) => updateSku(index, { label: event.target.value })} />
                                        <Button size="small" type="text" danger icon={<Trash2 className="size-3" />} aria-label="删除 SKU" onClick={() => setSkus((value) => value.filter((_, i) => i !== index))} />
                                    </div>
                                    <Input size="small" placeholder="款式描述（可选）" value={sku.desc} maxLength={200} onChange={(event) => updateSku(index, { desc: event.target.value })} />
                                    <ImageInputPicker label="该 SKU 参考图（可选）" max={1} value={sku.image} onChange={(image) => updateSku(index, { image })} />
                                </div>
                            ))}
                        </Panel>
                    ) : (
                        <Panel title="页面结构">
                            <div className="text-xs leading-5 text-stone-500">固定 7 屏竖版详情页：品牌主视觉、核心卖点、材质与结构、工艺细节、规格与品质、使用场景、收官价值。</div>
                        </Panel>
                    )}
                </div>

                <div className="shrink-0 space-y-2 border-t border-stone-200 p-4 dark:border-stone-800">
                    <Button
                        type="primary"
                        size="large"
                        block
                        icon={<Sparkles className="size-4" />}
                        loading={busy === "preview" || Boolean(auto)}
                        disabled={!products.length || Boolean(structureProblem) || (aiBusy && !auto)}
                        onClick={() => void preview()}
                    >
                        {previewLabel}（{previewCount} 张）
                        <CreditEstimate model={ecommerce?.imageModel || ""} count={previewCount} channelId={channelId} className="ml-1 text-xs opacity-80" />
                    </Button>
                    <Button
                        block
                        loading={busy === "final"}
                        disabled={!view || !selectedFinal.length || !sameProducts}
                        onClick={() => void act("final", () => generateEcommerceSet(token, view!.id, "final", selectedFinal))}
                    >
                        生成正式图（{selectedFinal.length} 张）
                        <CreditEstimate model={ecommerce?.imageModel || ""} count={Math.max(selectedFinal.length, 1)} channelId={channelId} className="ml-1 text-xs opacity-80" />
                    </Button>
                    <div className="text-center text-xs text-stone-500">先低画质预览确认方案，再勾选满意的图生成高清正式图</div>
                </div>
            </aside>

            <main className="flex min-h-[420px] flex-col overflow-hidden rounded-lg border border-stone-200 bg-card shadow-sm dark:border-stone-800 lg:min-h-0">
                <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-stone-200 px-4 py-2.5 dark:border-stone-800">
                    <div className="flex min-w-0 items-center gap-2">
                        <span className="truncate font-medium">{view?.title || "生成结果"}</span>
                        {slots.length > 0 && (
                            <>
                                <Tag className="!m-0">预览 {doneCount("preview")}/{slots.length}</Tag>
                                {doneCount("final") > 0 && (
                                    <Tag color="success" className="!m-0">
                                        正式 {doneCount("final")}
                                    </Tag>
                                )}
                            </>
                        )}
                    </div>
                    {slots.length > 0 && (
                        <div className="flex items-center gap-1 text-xs">
                            <span className="text-stone-500">已选 {selectedFinal.length}</span>
                            <Button size="small" type="text" onClick={() => setFinalIds(slots.map((slot) => slot.id))}>
                                全选
                            </Button>
                            <Button size="small" type="text" disabled={!selectedFinal.length} onClick={() => setFinalIds([])}>
                                清空
                            </Button>
                        </div>
                    )}
                </div>

                <div className="thin-scrollbar min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
                    {pending.length > 0 && (
                        <div className="flex items-center gap-2 rounded-md bg-sky-500/10 px-3 py-2 text-sm text-sky-700 dark:text-sky-300">
                            <Spin size="small" />
                            {pendingLabels[pending[0].purpose]}
                            {pending[0].purpose === "plan" && view!.planRound > 1 && <span className="text-xs opacity-80">（补全第 {view!.planRound} 轮）</span>}
                        </div>
                    )}
                    {!view ? (
                        <Empty className="!mt-20" image={<ImageIcon className="mx-auto size-12 text-stone-300 dark:text-stone-700" />} description="上传商品图并生成预览，结果会显示在这里" />
                    ) : (
                        <Image.PreviewGroup>
                            <div className="grid grid-cols-2 gap-3 md:grid-cols-3 2xl:grid-cols-4">
                                {slots.map((slot) => {
                                    const previewJob = slot.previewJobId ? jobs.get(slot.previewJobId) : undefined;
                                    const finalJob = slot.finalJobId ? jobs.get(slot.finalJobId) : undefined;
                                    const shown = finalJob?.outputUrl ? finalJob : previewJob;
                                    const running = isActive(finalJob) ? finalJob : isActive(previewJob) ? previewJob : undefined;
                                    const checked = finalIds.includes(slot.id);
                                    return (
                                        <div
                                            key={slot.id}
                                            className={`overflow-hidden rounded-lg border bg-background transition ${checked ? "border-sky-500 ring-1 ring-sky-500" : "border-stone-200 dark:border-stone-800"}`}
                                        >
                                            <div className="relative aspect-[3/4] bg-stone-100 dark:bg-stone-900">
                                                <Checkbox
                                                    className="!absolute left-2 top-2 z-10"
                                                    aria-label={`选择 ${slot.title} 生成正式图`}
                                                    checked={checked}
                                                    onChange={(event) => setFinalIds((value) => (event.target.checked ? [...value, slot.id] : value.filter((id) => id !== slot.id)))}
                                                />
                                                {shown?.outputUrl ? (
                                                    <Image src={shown.outputUrl} alt={slot.title} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-contain" />
                                                ) : (
                                                    <div className="flex h-full flex-col items-center justify-center gap-2 p-3 text-center text-xs text-stone-500">
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
                                            <div className="flex items-center justify-between gap-1 px-2 py-1.5 text-xs">
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
                                                        <Button size="small" type="text" icon={<Download className="size-3" />} aria-label={`下载 ${slot.title}`} onClick={() => saveAs(shown.outputUrl, `${slot.title}.png`)} />
                                                    )}
                                                </span>
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        </Image.PreviewGroup>
                    )}
                </div>
            </main>

            <aside className="thin-scrollbar overflow-y-auto rounded-lg border border-stone-200 bg-card px-3 pb-3 shadow-sm dark:border-stone-800 lg:min-h-0">
                <Tabs
                    size="small"
                    items={[
                        {
                            key: "history",
                            label: "历史",
                            children: history?.length ? (
                                <div className="space-y-1">
                                    {history.map((item) => (
                                        <button
                                            key={item.id}
                                            type="button"
                                            className={`flex w-full items-center gap-2 rounded-md p-1.5 text-left text-xs transition hover:bg-stone-100 dark:hover:bg-stone-800 ${item.id === setId ? "bg-stone-100 font-semibold dark:bg-stone-800" : ""}`}
                                            onClick={() => openSet(item.id)}
                                        >
                                            {item.coverUrl ? (
                                                <img src={item.coverUrl} alt="" className="size-10 shrink-0 rounded object-cover" />
                                            ) : (
                                                <span className="flex size-10 shrink-0 items-center justify-center rounded bg-stone-100 dark:bg-stone-900">
                                                    <ImageIcon className="size-4 text-stone-400" />
                                                </span>
                                            )}
                                            <span className="min-w-0 flex-1">
                                                <span className="block truncate">{item.title}</span>
                                                <span className="block font-normal text-stone-500">{new Date(item.createdAt).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}</span>
                                            </span>
                                        </button>
                                    ))}
                                </div>
                            ) : (
                                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史" />
                            ),
                        },
                        {
                            key: "examples",
                            label: "示例",
                            children: (
                                <div className="space-y-2 text-xs leading-5 text-stone-500">
                                    <p>{preset === "set" ? "智能匹配生成 4 张产品主图、1 张白底图与 4 张详情图；自定义可按下列标准图类型组合，至少 7 张：" : "详情图 / A+ 按下列 7 屏逐屏规划文案与视觉："}</p>
                                    <ul className="list-disc pl-4">
                                        {slotTypes
                                            .filter((type) => preset === "set" || type.key !== "white")
                                            .map((type) => (
                                                <li key={type.key}>{type.title}</li>
                                            ))}
                                    </ul>
                                    <p>所有结果同时保存在"我的作品"中。</p>
                                </div>
                            ),
                        },
                    ]}
                />
            </aside>
        </div>
    );
}
