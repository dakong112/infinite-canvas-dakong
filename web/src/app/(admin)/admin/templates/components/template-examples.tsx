"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { App, Button, Empty, Form, Image, Input, InputNumber, Modal, Space, Spin, Tag, Typography } from "antd";
import { useState } from "react";

import { pickedImageUrl, UploadDropzone, type PickedImage } from "@/app/(user)/image/components/upload-dropzone";
import { exampleFromJob, sampleFilesForTemplate } from "@/lib/image-templates";
import type { GenerationJob } from "@/services/api/jobs";
import { fetchTemplateExampleJobs, fetchTemplateSamples, generateTemplateExamples, saveTemplateSamples, type ImageTemplate, type TemplateSamples } from "@/services/api/templates";
import { useUserStore } from "@/stores/use-user-store";
import { UrlImages } from "./template-editor";

const isActive = (job: GenerationJob) => job.status === "queued" || job.status === "running";
const statusText: Record<GenerationJob["status"], string> = { queued: "排队中", running: "生成中", polling: "生成中", succeeded: "已完成", failed: "失败", cancelled: "已取消" };
export const emptySamples: TemplateSamples = { product: [], portrait: [], model: [] };

/** Admin example runs, polled while any is queued or running; templateId "" lists every template's runs. */
export function useTemplateExampleJobs(templateId: string, enabled = true) {
    const token = useUserStore((state) => state.token);
    return useQuery({
        queryKey: ["admin", "template-examples", token, templateId],
        queryFn: () => fetchTemplateExampleJobs(token, templateId),
        enabled: Boolean(token) && enabled,
        refetchInterval: (query) => (query.state.data?.some(isActive) ? 3000 : false),
    });
}

/** Example run results with "设为封面" / "加入示例"; titles labels each run with its template (the records view). */
export function ExampleJobs({ jobs, titles, onCover, onExample }: { jobs: GenerationJob[]; titles?: Record<string, string>; onCover: (job: GenerationJob) => void; onExample: (job: GenerationJob) => void }) {
    if (!jobs.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无示例生成记录" />;
    return (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            {jobs.map((job) => (
                <div key={job.id} className="overflow-hidden rounded-lg border border-border">
                    <div className="flex aspect-square items-center justify-center bg-muted text-xs text-muted-foreground">
                        {job.outputUrl ? (
                            <Image src={job.outputUrl} alt={job.prompt || "示例"} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-cover" />
                        ) : isActive(job) ? (
                            <Spin />
                        ) : (
                            <span className="line-clamp-4 p-2 text-red-500">{job.error || statusText[job.status]}</span>
                        )}
                    </div>
                    <div className="space-y-1 p-2 text-xs">
                        {titles && <div className="truncate font-medium">{titles[job.templateId ?? ""] ?? "已删除的模板"}</div>}
                        <div className="truncate text-muted-foreground" title={job.prompt}>
                            {job.prompt || "无补充提示词"}
                        </div>
                        {job.status === "succeeded" && job.outputFileId ? (
                            <Space size={4}>
                                <Button size="small" onClick={() => onCover(job)}>
                                    设为封面
                                </Button>
                                <Button size="small" onClick={() => onExample(job)}>
                                    加入示例
                                </Button>
                            </Space>
                        ) : (
                            <Tag className="!m-0">{statusText[job.status]}</Tag>
                        )}
                    </div>
                </div>
            ))}
        </div>
    );
}

/** 示例生成 inside the template editor: runs the saved template (drafts too) and applies results to the form. */
export function ExamplePanel({ template }: { template: ImageTemplate }) {
    const { message } = App.useApp();
    const form = Form.useFormInstance<Partial<ImageTemplate>>();
    const token = useUserStore((state) => state.token);
    const [files, setFiles] = useState<Record<string, PickedImage[]>>({});
    const [prompt, setPrompt] = useState("");
    const [count, setCount] = useState(1);
    const jobs = useTemplateExampleJobs(template.id);
    const samples = useQuery({ queryKey: ["admin", "template-samples", token], queryFn: () => fetchTemplateSamples(token), enabled: Boolean(token) });
    const generate = useMutation({
        mutationFn: () => generateTemplateExamples(token, template.id, { files: Object.fromEntries(Object.entries(files).map(([key, images]) => [key, images.map((image) => image.fileId)])), prompt: prompt.trim(), count }),
        onSuccess: ({ jobs: created }) => {
            message.success(`已提交 ${created.length} 个示例任务`);
            void jobs.refetch();
        },
        onError: (error: Error) => message.error(error.message || "提交失败"),
    });

    const fillSamples = () => {
        const picked = sampleFilesForTemplate(template.inputs, samples.data ?? emptySamples);
        if (picked.missing) return void message.warning(`公共示例素材里没有适合「${picked.missing}」的图片`);
        setFiles(Object.fromEntries(Object.entries(picked.files).map(([key, ids]) => [key, ids.map((fileId) => ({ fileId, url: pickedImageUrl(fileId) }))])));
    };

    return (
        <div className="space-y-3">
            <Typography.Paragraph type="secondary" className="!mb-0 !text-xs">
                按已保存的模板生成（草稿也可），不收模板费；结果可设为封面或加入示例，保存模板后生效。
            </Typography.Paragraph>
            {template.inputs.map((input) => (
                <UploadDropzone key={input.key} compact label={input.required ? input.label : `${input.label}（可选）`} max={input.max} value={files[input.key] ?? []} onChange={(images) => setFiles((value) => ({ ...value, [input.key]: images }))} />
            ))}
            <Input.TextArea rows={2} maxLength={2000} value={prompt} placeholder="补充提示词（可选），会记录为示例的创作提示词" onChange={(event) => setPrompt(event.target.value)} />
            <Space wrap>
                {template.inputs.length > 0 && <Button onClick={fillSamples}>使用公共示例素材</Button>}
                <InputNumber min={1} max={4} precision={0} prefix="数量" value={count} onChange={(value) => setCount(value ?? 1)} />
                <Button type="primary" loading={generate.isPending} onClick={() => generate.mutate()}>
                    生成示例
                </Button>
            </Space>
            <ExampleJobs
                jobs={jobs.data ?? []}
                onCover={(job) => {
                    form.setFieldValue("coverUrl", exampleFromJob(job).output);
                    message.success("已设为封面，保存模板后生效");
                }}
                onExample={(job) => {
                    form.setFieldValue("examples", [...(form.getFieldValue("examples") ?? []), exampleFromJob(job)]);
                    message.success("已加入示例，保存模板后生效");
                }}
            />
        </div>
    );
}

/** 公共示例素材: the shared 商品组 / 人像组 / 模特组 images used by "使用公共示例素材" and batch example runs. */
export function TemplateSamplesModal({ open, onClose }: { open: boolean; onClose: () => void }) {
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    const [form] = Form.useForm<TemplateSamples>();
    const samples = useQuery({ queryKey: ["admin", "template-samples", token], queryFn: () => fetchTemplateSamples(token), enabled: Boolean(token) && open });
    const save = useMutation({
        mutationFn: (value: TemplateSamples) => saveTemplateSamples(token, value),
        onSuccess: () => {
            message.success("公共示例素材已保存");
            void samples.refetch();
            onClose();
        },
        onError: (error: Error) => message.error(error.message || "保存失败"),
    });
    return (
        <Modal title="公共示例素材" open={open} width={720} onCancel={onClose} destroyOnHidden okText="保存" confirmLoading={save.isPending} onOk={() => save.mutate({ ...emptySamples, ...form.getFieldsValue(true) })}>
            <Typography.Paragraph type="secondary" className="!text-xs">
                批量生成示例时按输入组名称取图：含「模特」用模特组（为空时用人像组），含「人像 / 人物」用人像组，其余用商品组。每组最多 12 张。
            </Typography.Paragraph>
            {samples.isLoading ? (
                <Spin className="!block" />
            ) : (
                <Form form={form} layout="vertical" preserve={false} initialValues={samples.data ?? emptySamples}>
                    <Form.Item name="product" label="商品组">
                        <UrlImages max={12} />
                    </Form.Item>
                    <Form.Item name="portrait" label="人像组">
                        <UrlImages max={12} />
                    </Form.Item>
                    <Form.Item name="model" label="模特组">
                        <UrlImages max={12} />
                    </Form.Item>
                </Form>
            )}
        </Modal>
    );
}
