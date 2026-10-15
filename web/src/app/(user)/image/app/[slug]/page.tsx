"use client";

import { useQuery } from "@tanstack/react-query";
import { App, Button, Empty, Input, Spin, Tag } from "antd";
import { Copy, LayoutTemplate, Sparkles } from "lucide-react";
import { useParams } from "next/navigation";
import { useState } from "react";

import { useCopyText } from "@/hooks/use-copy-text";
import { sizeToTemplateParams, templateCategoryLabels, templateCost, templateRunProblem } from "@/lib/image-templates";
import { fetchWorks, type GenerationJob } from "@/services/api/jobs";
import { fetchAdminTemplates, fetchTemplate, generateWithTemplate, type ImageTemplate } from "@/services/api/templates";
import { useAccountStore } from "@/stores/use-account-store";
import { useUserStore } from "@/stores/use-user-store";
import { FormSection, ImageToolShell, PriceText, Stepper, useImagePrice, type ShellTab } from "../../components/image-tool-shell";
import { pickedImageUrl, UploadDropzone, type PickedImage } from "../../components/upload-dropzone";
import { WorkDetailModal, type WorkDetail } from "./work-detail-modal";

const isActive = (job: GenerationJob) => job.status === "queued" || job.status === "running";

function Pills({ options, value, onChange }: { options: string[]; value: string; onChange: (value: string) => void }) {
    return (
        <div className="flex flex-wrap gap-2">
            {options.map((option) => (
                <Tag.CheckableTag key={option} className="!m-0 !rounded-full !border !border-border !px-4 !py-1 !text-sm" checked={value === option} onChange={() => onChange(option)}>
                    {option}
                </Tag.CheckableTag>
            ))}
        </div>
    );
}

export default function TemplateAppPage() {
    const { slug } = useParams<{ slug: string }>();
    const token = useUserStore((state) => state.token);
    const isAdmin = useUserStore((state) => state.user?.role === "admin");
    // Admins can preview drafts (the public endpoint only serves published templates).
    const { data: template, isLoading } = useQuery({
        queryKey: ["template", slug, isAdmin],
        queryFn: async () => {
            try {
                return await fetchTemplate(slug);
            } catch (error) {
                const draft = isAdmin && token ? (await fetchAdminTemplates(token)).find((item) => item.slug === slug) : undefined;
                if (draft) return draft;
                throw error;
            }
        },
        retry: false,
    });
    if (isLoading) return <Spin className="!mt-24 !block" />;
    if (!template) return <Empty className="!mt-24" description="模板不存在或未发布" />;
    return <TemplateApp key={template.id} template={template} />;
}

function TemplateApp({ template }: { template: ImageTemplate }) {
    const { message } = App.useApp();
    const copyText = useCopyText();
    const token = useUserStore((state) => state.token);
    const member = useAccountStore((state) => Boolean(state.summary?.member?.active));
    const price = useImagePrice();
    const [files, setFiles] = useState<Record<string, PickedImage[]>>({});
    const [prompt, setPrompt] = useState("");
    const [ratio, setRatio] = useState(template.ratios[0]);
    const [resolution, setResolution] = useState(template.resolutions[0]);
    const [count, setCount] = useState(1);
    const [submitting, setSubmitting] = useState(false);
    const [submitted, setSubmitted] = useState(false);
    const [detail, setDetail] = useState<WorkDetail | null>(null);
    const { data: works, refetch } = useQuery({
        queryKey: ["works", token, "template", template.id],
        queryFn: () => fetchWorks(token!, { kind: "template", templateId: template.id }),
        enabled: Boolean(token),
        refetchInterval: (query) => (query.state.data?.items.some(isActive) ? 3000 : false),
    });

    const feeWaived = template.memberFree && member;
    const cost = templateCost(price([{ model: template.model, count }]), template.feeCredits, count, feeWaived);
    const problem = token ? templateRunProblem(template, files, prompt) : "请先登录后使用";

    const submit = async () => {
        if (problem) return void message.error(problem);
        setSubmitting(true);
        try {
            const input = { files: Object.fromEntries(template.inputs.map((item) => [item.key, (files[item.key] ?? []).map((image) => image.fileId)])), prompt: prompt.trim(), ratio, resolution, count };
            const { jobs } = await generateWithTemplate(token!, template.slug, input);
            message.success(`已提交 ${jobs.length} 个任务`);
            setSubmitted(true);
            void refetch();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "提交失败");
        } finally {
            setSubmitting(false);
        }
    };

    const form = (
        <>
            <div className="flex gap-3">
                <div className="size-16 shrink-0 overflow-hidden rounded-lg bg-linear-to-br from-orange-100 to-violet-100 dark:from-orange-500/20 dark:to-violet-500/20">
                    {template.coverUrl ? <img src={template.coverUrl} alt="" className="size-full object-cover" /> : <LayoutTemplate className="m-4 size-8 text-foreground/40" strokeWidth={1.25} />}
                </div>
                <div className="min-w-0 flex-1">
                    <h2 className="truncate text-base font-semibold">{template.title}</h2>
                    {template.description && <p className="mt-0.5 line-clamp-2 text-xs leading-5 text-muted-foreground">{template.description}</p>}
                    <div className="mt-1.5 flex flex-wrap gap-1">
                        <Tag className="!m-0">{templateCategoryLabels[template.category]}</Tag>
                        {template.status === "draft" && <Tag className="!m-0" color="warning">草稿预览</Tag>}
                        {template.memberFree && template.feeCredits > 0 && (
                            <Tag className="!m-0" color="gold">
                                会员免模板费
                            </Tag>
                        )}
                    </div>
                </div>
            </div>
            {template.inputs.length > 0 && (
                <FormSection title="参数配置">
                    <div className="space-y-4">
                        {template.inputs.map((input) => (
                            <UploadDropzone key={input.key} label={input.required ? input.label : `${input.label}（可选）`} max={input.max} value={files[input.key] ?? []} onChange={(images) => setFiles((value) => ({ ...value, [input.key]: images }))} />
                        ))}
                    </div>
                </FormSection>
            )}
            <FormSection title="自定义提示词" extra={template.promptRequired ? <span className="text-xs text-red-500">必填</span> : <span className="text-xs text-muted-foreground">选填</span>}>
                <Input.TextArea rows={4} maxLength={2000} showCount value={prompt} placeholder={template.promptPlaceholder || "补充你的要求，例如风格、颜色、场景"} onChange={(event) => setPrompt(event.target.value)} />
            </FormSection>
            <FormSection title="比例">
                <Pills options={template.ratios} value={ratio} onChange={setRatio} />
            </FormSection>
            <FormSection title="分辨率">
                <Pills options={template.resolutions} value={resolution} onChange={setResolution} />
            </FormSection>
        </>
    );

    const action = (
        <>
            {cost !== null && (
                <div className="mb-3 flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">本次消耗</span>
                    <PriceText credits={cost} className="font-semibold" />
                </div>
            )}
            <div className="flex items-center gap-3">
                <span className="shrink-0 text-sm text-muted-foreground">生成数量</span>
                <Stepper min={1} max={4} value={count} onChange={setCount} />
                <Button type="primary" className="!h-11 min-w-0 flex-1 !rounded-lg" icon={<Sparkles className="size-4" />} loading={submitting} disabled={Boolean(problem)} onClick={() => void submit()}>
                    立即生成
                </Button>
            </div>
        </>
    );
    const hint = problem || (feeWaived ? "会员免模板费，结果保存在「我的作品」中" : template.feeCredits > 0 ? `含模板费 ${template.feeCredits} 积分/张，生成成功后收取` : "结果保存在「我的作品」中");

    const examples = template.examples.length ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {template.examples.map((example, index) => (
                <div key={`${example.output}-${index}`} className="rounded-xl border border-border bg-background p-3">
                    <button
                        type="button"
                        className="relative grid aspect-[4/3] w-full grid-cols-2 overflow-hidden rounded-lg bg-muted"
                        onClick={() => setDetail({ image: example.output, references: example.inputs, prompt: example.prompt, apply: () => setPrompt(example.prompt) })}
                    >
                        {example.inputs[0] && (
                            <span className="relative">
                                <img src={example.inputs[0]} alt="原图" className="absolute inset-0 size-full object-cover" />
                                <span className="absolute left-2 top-2 rounded-md bg-black/65 px-2 py-0.5 text-xs text-white">原图</span>
                            </span>
                        )}
                        <span className={example.inputs[0] ? "relative" : "relative col-span-2"}>
                            <img src={example.output} alt="AI 作品" className="absolute inset-0 size-full object-cover" />
                            <span className="absolute left-2 top-2 flex items-center gap-1 rounded-md bg-black/65 px-2 py-0.5 text-xs text-white">
                                <Sparkles className="size-3" />
                                AI 作品
                            </span>
                        </span>
                    </button>
                    {example.prompt && (
                        <>
                            <div className="mb-1.5 mt-3 flex items-center justify-between text-xs text-muted-foreground">
                                创作提示词
                                <Button type="text" size="small" aria-label="复制提示词" icon={<Copy className="size-3.5" />} onClick={() => copyText(example.prompt)} />
                            </div>
                            <p className="line-clamp-3 rounded-lg bg-muted px-3 py-2 text-xs leading-5">{example.prompt}</p>
                        </>
                    )}
                </div>
            ))}
        </div>
    ) : (
        <div className="flex h-full min-h-80 flex-col items-center justify-center gap-3 rounded-xl bg-linear-to-br from-orange-50 via-background to-violet-50 text-center text-muted-foreground dark:from-orange-500/10 dark:to-violet-500/10">
            <LayoutTemplate className="size-12" strokeWidth={1} />
            <div className="text-sm">暂无作品示例</div>
            <div className="max-w-xs text-xs">上传素材、写下要求，立即生成你的第一张作品</div>
        </div>
    );

    const items = (works?.items ?? []).filter((job) => job.status !== "cancelled");
    const myWorks = !token ? (
        <Empty className="!mt-16" image={Empty.PRESENTED_IMAGE_SIMPLE} description="登录后查看我的作品" />
    ) : items.length ? (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 2xl:grid-cols-4">
            {items.map((job) => (
                <div key={job.id} className="aspect-square overflow-hidden rounded-xl border border-border bg-muted">
                    {job.outputUrl ? (
                        <button
                            type="button"
                            className="size-full"
                            onClick={() =>
                                setDetail({
                                    image: job.outputUrl,
                                    references: job.inputFileIds.map(pickedImageUrl),
                                    prompt: job.prompt,
                                    apply: () => {
                                        const params = sizeToTemplateParams(job.width, job.height, template.ratios, template.resolutions);
                                        setPrompt(job.prompt);
                                        if (params.ratio) setRatio(params.ratio);
                                        if (params.resolution) setResolution(params.resolution);
                                    },
                                })
                            }
                        >
                            <img src={job.outputUrl} alt={job.prompt || template.title} className="size-full object-cover transition hover:scale-105" />
                        </button>
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
            ))}
        </div>
    ) : (
        <Empty className="!mt-16" image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无作品" />
    );

    // ponytail: "我的作品" takes the shell's "result" key after a submit so the shell switches to it.
    const tabs: ShellTab[] = [
        {
            key: "examples",
            label: "作品示例",
            content: (
                <>
                    {examples}
                    {template.credit && <p className="mt-4 text-xs text-muted-foreground">示例来源：{template.credit}</p>}
                </>
            ),
        },
        { key: submitted ? "result" : "history", label: "我的作品", content: myWorks },
    ];

    return (
        <>
            <ImageToolShell title={template.title} form={form} action={action} hint={hint} tabs={tabs} />
            <WorkDetailModal appName={template.title} work={detail} onClose={() => setDetail(null)} />
        </>
    );
}
