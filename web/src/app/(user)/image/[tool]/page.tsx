"use client";

import { useQuery } from "@tanstack/react-query";
import { App, Button, Empty, Image, Input, Segmented, Spin } from "antd";
import { saveAs } from "file-saver";
import { Download, Sparkles } from "lucide-react";
import { notFound, useParams } from "next/navigation";
import { useState } from "react";

import { imageTool } from "@/lib/image-tool-examples";
import { runEcommerceTool, type EcommerceToolName } from "@/services/api/ecommerce";
import { fetchGenerationJobs, fetchWorks, type GenerationJob } from "@/services/api/jobs";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import { FormSection, ImageToolShell, PriceText, Stepper, ToolExamples, useImagePrice, type ShellTab } from "../components/image-tool-shell";
import { UploadDropzone, type PickedImage } from "../components/upload-dropzone";

type ToolInput = { key: string; label: string; max: number; required: boolean };
type ToolParam = { key: string; label: string; options?: { label: string; value: string }[]; placeholder?: string; showWhen?: [string, string] };

// Input order matters: the server reads replicate as [reference, ...products] and tryon as [...products, model].
// remove-bg takes one image per run, so its images are submitted one run each.
const tools: Record<EcommerceToolName, { inputs: ToolInput[]; params: ToolParam[] }> = {
    replicate: {
        inputs: [
            { key: "reference", label: "参考图", max: 1, required: true },
            { key: "products", label: "商品图", max: 3, required: true },
        ],
        params: [{ key: "extra", label: "补充要求", placeholder: "例如：背景换成暖色调" }],
    },
    "remove-bg": {
        inputs: [{ key: "products", label: "商品图", max: 4, required: true }],
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
    const title = imageTool(tool as EcommerceToolName).title;
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    const imageModel = useConfigStore((state) => state.publicSettings?.ecommerce?.imageModel || "");
    const price = useImagePrice();
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
    const runs = tool === "remove-bg" ? Math.max(inputs.products?.length ?? 0, 1) : 1;
    const missing = config.inputs.find((input) => input.required && !inputs[input.key]?.length);

    const submit = async () => {
        if (missing) {
            message.error(`请上传${missing.label}`);
            return;
        }
        setSubmitting(true);
        try {
            const fileIds = config.inputs.flatMap((input) => (inputs[input.key] ?? []).map((image) => image.fileId));
            const toolParams: Record<string, string> = { size, ...Object.fromEntries(visibleParams.map((param) => [param.key, params[param.key] ?? ""])) };
            if (tool === "tryon" && inputs.model?.length) toolParams.hasModelImage = "true";
            const created: GenerationJob[] = [];
            for (const files of tool === "remove-bg" ? fileIds.map((id) => [id]) : [fileIds]) {
                created.push(...(await runEcommerceTool(token, tool as EcommerceToolName, { fileIds: files, params: toolParams, count })).jobs);
            }
            setJobIds(created.map((job) => job.id));
            message.success(`已提交 ${created.length} 个任务`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "提交失败");
        } finally {
            setSubmitting(false);
        }
    };

    const form = (
        <>
            {config.inputs.map((input) => (
                <UploadDropzone key={input.key} label={input.label} max={input.max} value={inputs[input.key] ?? []} onChange={(images) => setInputs((value) => ({ ...value, [input.key]: images }))} />
            ))}
            {visibleParams.map((param) => (
                <FormSection key={param.key} title={param.label}>
                    {param.options ? (
                        <Segmented block size="large" value={params[param.key] || param.options[0].value} options={param.options} onChange={(value) => setParams((current) => ({ ...current, [param.key]: String(value) }))} />
                    ) : (
                        <Input size="large" value={params[param.key] ?? ""} maxLength={500} placeholder={param.placeholder} onChange={(event) => setParams((current) => ({ ...current, [param.key]: event.target.value }))} />
                    )}
                </FormSection>
            ))}
            <FormSection title="比例">
                <Segmented block size="large" value={size} options={sizeOptions} onChange={(value) => setSize(String(value))} />
            </FormSection>
            <FormSection title={tool === "remove-bg" ? "每张生成数量" : "生成数量"} extra={<Stepper min={1} max={4} value={count} onChange={setCount} />} />
        </>
    );

    const tabs: ShellTab[] = [
        ...(jobs.length
            ? [
                  {
                      key: "result" as const,
                      label: "结果",
                      content: (
                          <Image.PreviewGroup>
                              <div className="grid grid-cols-2 gap-3 md:grid-cols-3 2xl:grid-cols-4">
                                  {jobs.map((job, index) => (
                                      <div key={job.id} className="overflow-hidden rounded-xl border border-border bg-background">
                                          <div className="aspect-square bg-muted">
                                              {job.outputUrl ? (
                                                  <Image src={job.outputUrl} alt={`${title} ${index + 1}`} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-contain" />
                                              ) : (
                                                  <div className="flex h-full flex-col items-center justify-center gap-2 p-3 text-center text-xs text-muted-foreground">
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
                                                  <Button size="small" type="text" icon={<Download className="size-3" />} aria-label="下载" onClick={() => saveAs(job.outputUrl, `${title}-${index + 1}.png`)} />
                                              </div>
                                          )}
                                      </div>
                                  ))}
                              </div>
                          </Image.PreviewGroup>
                      ),
                  },
              ]
            : []),
        {
            key: "history",
            label: "历史",
            content: history.length ? (
                <Image.PreviewGroup>
                    <div className="grid grid-cols-3 gap-2 md:grid-cols-4 xl:grid-cols-6">
                        {history.map((item) => (
                            <div key={item.id} className="aspect-square overflow-hidden rounded-lg bg-muted">
                                <Image src={item.outputUrl} alt={item.prompt} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-cover" />
                            </div>
                        ))}
                    </div>
                </Image.PreviewGroup>
            ) : (
                <Empty className="!mt-16" image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史" />
            ),
        },
        { key: "examples", label: "示例", content: <ToolExamples tool={tool as EcommerceToolName} /> },
    ];

    return (
        <ImageToolShell
            title={title}
            form={form}
            action={
                <Button type="primary" block className="!h-11 !rounded-lg" icon={<Sparkles className="size-4" />} loading={submitting} disabled={Boolean(missing)} onClick={() => void submit()}>
                    开始生成（{runs * count} 张）
                    <PriceText credits={price([{ model: imageModel, count: runs * count }])} prefix="消耗" className="text-xs opacity-80" />
                </Button>
            }
            hint={missing ? `请先上传${missing.label}` : '结果同时保存在"我的作品 · 应用"中'}
            tabs={tabs}
        />
    );
}
