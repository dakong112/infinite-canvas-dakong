"use client";

import { useQuery } from "@tanstack/react-query";
import { App, Button, Empty, Image, Input, InputNumber, Segmented, Select, Spin, Tabs } from "antd";
import { saveAs } from "file-saver";
import { Download, ImageIcon, Sparkles } from "lucide-react";
import { notFound, useParams } from "next/navigation";
import { useState } from "react";

import { CreditEstimate } from "@/components/billing/credit-estimate";
import { platformChannelId } from "@/lib/model-catalog";
import { runEcommerceTool, type EcommerceToolName } from "@/services/api/ecommerce";
import { fetchGenerationJobs, fetchWorks, type GenerationJob } from "@/services/api/jobs";
import { useConfigStore, useModelCatalog } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import { ImageInputPicker, type PickedImage } from "../components/image-input-picker";

type ToolInput = { key: string; label: string; max: number; required: boolean };
type ToolParam = { key: string; label: string; options?: { label: string; value: string }[]; placeholder?: string; showWhen?: [string, string] };

// Input order matters: the server reads replicate as [reference, ...products] and tryon as [...products, model].
const tools: Record<EcommerceToolName, { title: string; description: string; inputs: ToolInput[]; params: ToolParam[] }> = {
    replicate: {
        title: "图片复刻",
        description: "上传一张参考图和你的商品图，生成同风格、同构图的商品图片。",
        inputs: [
            { key: "reference", label: "参考图", max: 1, required: true },
            { key: "products", label: "商品图", max: 3, required: true },
        ],
        params: [{ key: "extra", label: "补充要求", placeholder: "例如：背景换成暖色调" }],
    },
    "remove-bg": {
        title: "去除背景",
        description: "保留商品主体，输出透明底或纯色底图片。",
        inputs: [{ key: "products", label: "商品图", max: 1, required: true }],
        params: [
            {
                key: "background",
                label: "背景",
                options: [
                    { label: "透明", value: "transparent" },
                    { label: "纯白", value: "white" },
                    { label: "纯色", value: "color" },
                ],
            },
            { key: "color", label: "背景色", placeholder: "#F5F5F5", showWhen: ["background", "color"] },
        ],
    },
    tryon: {
        title: "AI 换装 / 模特图",
        description: "把服饰穿到模特身上，或为商品添加模特与使用场景。",
        inputs: [
            { key: "products", label: "商品图", max: 3, required: true },
            { key: "model", label: "模特图（可选）", max: 1, required: false },
        ],
        params: [
            {
                key: "mode",
                label: "模式",
                options: [
                    { label: "穿到模特身上", value: "wear" },
                    { label: "添加模特与场景", value: "scene" },
                ],
            },
            { key: "description", label: "模特描述", placeholder: "例如：25 岁亚洲女性，短发，气质干练" },
            { key: "extra", label: "补充要求" },
        ],
    },
};

const sizeOptions = [
    { label: "1:1", value: "1024x1024" },
    { label: "2:3", value: "1024x1536" },
    { label: "3:2", value: "1536x1024" },
];

const isActive = (job: GenerationJob) => job.status === "queued" || job.status === "running";

export default function EcommerceToolPage() {
    const { tool } = useParams<{ tool: string }>();
    const config = tools[tool as EcommerceToolName];
    if (!config) notFound();
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    const imageModel = useConfigStore((state) => state.publicSettings?.ecommerce?.imageModel || "");
    const catalog = useModelCatalog();
    // E-commerce jobs bill the SSO account group ("newapi@" on the server).
    const channelId = catalog ? platformChannelId(catalog.userGroup) : undefined;
    const [inputs, setInputs] = useState<Record<string, PickedImage[]>>({});
    const [params, setParams] = useState<Record<string, string>>({});
    const [size, setSize] = useState("1024x1024");
    const [count, setCount] = useState(1);
    const [jobIds, setJobIds] = useState<string[]>([]);
    const [submitting, setSubmitting] = useState(false);
    const { data: jobs = [] } = useQuery({
        queryKey: ["tool-jobs", jobIds],
        queryFn: () => fetchGenerationJobs(token!, jobIds),
        enabled: Boolean(token) && jobIds.length > 0,
        refetchInterval: (query) => (!query.state.data || query.state.data.some(isActive) ? 3000 : false),
    });
    // ponytail: keyed on finished count so history refreshes as each job lands, no extra polling.
    const finished = jobs.filter((job) => !isActive(job)).length;
    const { data: works } = useQuery({ queryKey: ["works", token, "tool", finished], queryFn: () => fetchWorks(token!, { kind: "tool" }), enabled: Boolean(token) });

    const history = (works?.items ?? []).filter((item) => item.outputUrl && item.slotKind === tool);

    if (!token) return <Empty className="mt-20" description="请先登录后使用" />;

    const visibleParams = config.params.filter((param) => !param.showWhen || params[param.showWhen[0]] === param.showWhen[1]);

    const submit = async () => {
        const missing = config.inputs.find((input) => input.required && !inputs[input.key]?.length);
        if (missing) {
            message.error(`请上传${missing.label}`);
            return;
        }
        setSubmitting(true);
        try {
            const fileIds = config.inputs.flatMap((input) => (inputs[input.key] ?? []).map((image) => image.fileId));
            const toolParams: Record<string, string> = { size, ...Object.fromEntries(visibleParams.map((param) => [param.key, params[param.key] ?? ""])) };
            if (tool === "tryon" && inputs.model?.length) toolParams.hasModelImage = "true";
            const { jobs: created } = await runEcommerceTool(token, tool as EcommerceToolName, { fileIds, params: toolParams, count });
            setJobIds(created.map((job) => job.id));
            message.success(`已提交 ${created.length} 个任务`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "提交失败");
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3 text-stone-900 dark:text-stone-100 lg:grid lg:grid-cols-[360px_minmax(0,1fr)_260px] lg:overflow-hidden">
            <aside className="flex flex-col overflow-hidden rounded-lg border border-stone-200 bg-card shadow-sm dark:border-stone-800 lg:min-h-0">
                <div className="shrink-0 px-4 pb-2 pt-3">
                    <h1 className="text-base font-semibold">{config.title}</h1>
                    <p className="text-xs text-stone-500">{config.description}</p>
                </div>
                <div className="thin-scrollbar min-h-0 flex-1 space-y-4 overflow-y-auto px-4 pb-3">
                    {config.inputs.map((input) => (
                        <ImageInputPicker key={input.key} label={input.label} max={input.max} value={inputs[input.key] ?? []} onChange={(images) => setInputs((value) => ({ ...value, [input.key]: images }))} />
                    ))}
                    {visibleParams.map((param) => (
                        <label key={param.key} className="block space-y-1 text-sm">
                            <span className="font-medium">{param.label}</span>
                            {param.options ? (
                                <Select className="!w-full" value={params[param.key] || param.options[0].value} options={param.options} onChange={(value) => setParams((current) => ({ ...current, [param.key]: value }))} />
                            ) : (
                                <Input value={params[param.key] ?? ""} maxLength={500} placeholder={param.placeholder} onChange={(event) => setParams((current) => ({ ...current, [param.key]: event.target.value }))} />
                            )}
                        </label>
                    ))}
                    <div className="flex flex-wrap items-center gap-3 text-sm">
                        <span className="font-medium">比例</span>
                        <Segmented size="small" value={size} options={sizeOptions} onChange={(value) => setSize(String(value))} />
                        <span className="font-medium">数量</span>
                        <InputNumber size="small" min={1} max={4} precision={0} value={count} onChange={(value) => setCount(Math.min(Math.max(Number(value) || 1, 1), 4))} />
                    </div>
                </div>
                <div className="shrink-0 border-t border-stone-200 p-4 dark:border-stone-800">
                    <Button type="primary" size="large" block icon={<Sparkles className="size-4" />} loading={submitting} onClick={() => void submit()}>
                        开始生成（{count} 张）
                        <CreditEstimate model={imageModel} count={count} channelId={channelId} className="ml-1 text-xs opacity-80" />
                    </Button>
                </div>
            </aside>

            <main className="thin-scrollbar flex min-h-[420px] flex-col overflow-y-auto rounded-lg border border-stone-200 bg-card p-4 shadow-sm dark:border-stone-800 lg:min-h-0">
                {!jobs.length ? (
                    <Empty className="!mt-20" image={<ImageIcon className="mx-auto size-12 text-stone-300 dark:text-stone-700" />} description="上传图片后开始生成，结果会显示在这里" />
                ) : (
                    <Image.PreviewGroup>
                        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 2xl:grid-cols-4">
                            {jobs.map((job, index) => (
                                <div key={job.id} className="overflow-hidden rounded-lg border border-stone-200 bg-background dark:border-stone-800">
                                    <div className="aspect-square bg-stone-100 dark:bg-stone-900">
                                        {job.outputUrl ? (
                                            <Image src={job.outputUrl} alt={`${config.title} ${index + 1}`} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-contain" />
                                        ) : (
                                            <div className="flex h-full flex-col items-center justify-center gap-2 p-3 text-center text-xs text-stone-500">
                                                {isActive(job) ? (
                                                    <>
                                                        <Spin />
                                                        {job.status === "queued" ? "排队中" : "生成中"}
                                                    </>
                                                ) : (
                                                    <span className="line-clamp-4 text-red-500">{job.error || "生成失败"}</span>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                    {job.outputUrl && (
                                        <div className="flex justify-end px-2 py-1">
                                            <Button size="small" type="text" icon={<Download className="size-3" />} aria-label="下载" onClick={() => saveAs(job.outputUrl, `${config.title}-${index + 1}.png`)} />
                                        </div>
                                    )}
                                </div>
                            ))}
                        </div>
                    </Image.PreviewGroup>
                )}
            </main>

            <aside className="thin-scrollbar overflow-y-auto rounded-lg border border-stone-200 bg-card px-3 pb-3 shadow-sm dark:border-stone-800 lg:min-h-0">
                <Tabs
                    size="small"
                    items={[
                        {
                            key: "history",
                            label: "历史",
                            children: history.length ? (
                                <Image.PreviewGroup>
                                    <div className="grid grid-cols-3 gap-1">
                                        {history.map((item) => (
                                            <div key={item.id} className="aspect-square overflow-hidden rounded bg-stone-100 dark:bg-stone-900">
                                                <Image src={item.outputUrl} alt={item.prompt} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-cover" />
                                            </div>
                                        ))}
                                    </div>
                                </Image.PreviewGroup>
                            ) : (
                                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史" />
                            ),
                        },
                        { key: "examples", label: "示例", children: <p className="text-xs leading-5 text-stone-500">{config.description}所有结果同时保存在"我的作品 · 应用"中。</p> },
                    ]}
                />
            </aside>
        </div>
    );
}
