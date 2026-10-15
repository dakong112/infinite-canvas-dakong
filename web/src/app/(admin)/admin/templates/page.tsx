"use client";

import { DeleteOutlined, EditOutlined, ExportOutlined, PlusOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Drawer, Flex, Image, Popconfirm, Space, Table, Tag, Tooltip, Typography } from "antd";
import { useState } from "react";

import { exampleFromJob, sampleFilesForTemplate, templateCategoryLabels, templateProblem } from "@/lib/image-templates";
import type { GenerationJob } from "@/services/api/jobs";
import { deleteAdminTemplate, fetchAdminTemplates, fetchTemplateSamples, generateTemplateExamples, saveAdminTemplate, type ImageTemplate } from "@/services/api/templates";
import { useUserStore } from "@/stores/use-user-store";
import { newTemplate, TemplateEditor } from "./components/template-editor";
import { ExampleJobs, TemplateSamplesModal, useTemplateExampleJobs } from "./components/template-examples";

export default function AdminTemplatesPage() {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const [editing, setEditing] = useState<Partial<ImageTemplate> | null>(null);
    const [selected, setSelected] = useState<string[]>([]);
    const [batching, setBatching] = useState(false);
    const [samplesOpen, setSamplesOpen] = useState(false);
    const [recordsOpen, setRecordsOpen] = useState(false);
    const records = useTemplateExampleJobs("", recordsOpen);
    const { data = [], isLoading } = useQuery({ queryKey: ["admin", "templates", token], queryFn: () => fetchAdminTemplates(token), enabled: Boolean(token), retry: false });
    const refresh = () => queryClient.invalidateQueries({ queryKey: ["admin", "templates"] });
    const fail = (error: Error) => message.error(error.message || "操作失败");

    const save = useMutation({
        mutationFn: (item: Partial<ImageTemplate>) => saveAdminTemplate(token, item),
        onSuccess: () => {
            message.success("模板已保存");
            setEditing(null);
            void refresh();
        },
        onError: fail,
    });
    const remove = useMutation({
        mutationFn: (id: string) => deleteAdminTemplate(token, id),
        onSuccess: () => {
            message.success("模板已删除");
            void refresh();
        },
        onError: fail,
    });

    // Batch example runs use one shared sample per input group; templates lacking a required sample are skipped.
    const batchGenerate = async () => {
        setBatching(true);
        const skipped: string[] = [];
        let queued = 0;
        try {
            const samples = await fetchTemplateSamples(token);
            for (const item of data.filter((template) => selected.includes(template.id))) {
                const { files, missing } = sampleFilesForTemplate(item.inputs, samples);
                if (missing) {
                    skipped.push(`${item.title}（缺少「${missing}」素材）`);
                    continue;
                }
                try {
                    await generateTemplateExamples(token, item.id, { files, count: 1 });
                    queued++;
                } catch (error) {
                    skipped.push(`${item.title}（${error instanceof Error ? error.message : "提交失败"}）`);
                }
            }
            if (queued) message.success(`已为 ${queued} 个模板提交示例生成`);
            if (skipped.length) message.warning(`未提交：${skipped.join("；")}`, 8);
            setSelected([]);
            setRecordsOpen(true);
        } catch (error) {
            fail(error instanceof Error ? error : new Error("读取公共示例素材失败"));
        } finally {
            setBatching(false);
        }
    };

    const applyRecord = (job: GenerationJob, cover: boolean) => {
        const item = data.find((template) => template.id === job.templateId);
        if (!item) return void message.error("模板不存在");
        const example = exampleFromJob(job);
        save.mutate(cover ? { ...item, coverUrl: example.output } : { ...item, examples: [...item.examples, example] });
    };

    const submit = (item: Partial<ImageTemplate>) => {
        const problem = templateProblem(item);
        if (problem) message.error(problem);
        else save.mutate(item);
    };

    return (
        <main style={{ padding: 24 }}>
            <Card
                variant="borderless"
                title={
                    <Space>
                        模板库
                        <Tag>{data.length} 个</Tag>
                    </Space>
                }
                extra={
                    <Space wrap>
                        <Button disabled={!selected.length} loading={batching} onClick={() => void batchGenerate()}>
                            批量生成示例{selected.length ? `（${selected.length}）` : ""}
                        </Button>
                        <Button onClick={() => setRecordsOpen(true)}>示例生成记录</Button>
                        <Button onClick={() => setSamplesOpen(true)}>公共示例素材</Button>
                        <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing({ ...newTemplate })}>
                            新增模板
                        </Button>
                    </Space>
                }
            >
                <Table<ImageTemplate>
                    rowKey="id"
                    rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys as string[]) }}
                    loading={isLoading}
                    dataSource={data}
                    pagination={false}
                    scroll={{ x: 900 }}
                    columns={[
                        {
                            title: "封面",
                            dataIndex: "coverUrl",
                            width: 88,
                            render: (_, item) => <Image src={item.coverUrl || "/logo.svg"} alt={item.title} width={56} height={42} style={{ objectFit: "cover", borderRadius: 6 }} fallback="/logo.svg" />,
                        },
                        {
                            title: "标题",
                            dataIndex: "title",
                            render: (_, item) => (
                                <Flex vertical>
                                    <Typography.Link strong onClick={() => setEditing(item)}>
                                        {item.title}
                                    </Typography.Link>
                                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                                        {item.slug}
                                    </Typography.Text>
                                </Flex>
                            ),
                        },
                        {
                            title: "分类",
                            dataIndex: "category",
                            width: 110,
                            filters: Object.entries(templateCategoryLabels).map(([value, text]) => ({ value, text })),
                            onFilter: (value, item) => item.category === value,
                            render: (_, item) => templateCategoryLabels[item.category] ?? item.category,
                        },
                        {
                            title: "状态",
                            dataIndex: "status",
                            width: 100,
                            filters: [
                                { value: "published", text: "已发布" },
                                { value: "draft", text: "草稿" },
                            ],
                            onFilter: (value, item) => item.status === value,
                            render: (_, item) => (item.status === "published" ? <Tag color="success">已发布</Tag> : <Tag>草稿</Tag>),
                        },
                        { title: "排序", dataIndex: "sort", width: 80, sorter: (a, b) => a.sort - b.sort },
                        {
                            title: "模板费",
                            dataIndex: "feeCredits",
                            width: 150,
                            render: (_, item) =>
                                item.feeCredits > 0 ? (
                                    <Space size={4}>
                                        {item.feeCredits} / 张{item.memberFree && <Tag color="gold">会员免费</Tag>}
                                    </Space>
                                ) : (
                                    <Typography.Text type="secondary">免费</Typography.Text>
                                ),
                        },
                        {
                            title: "操作",
                            key: "actions",
                            width: 120,
                            render: (_, item) => (
                                <Space size={4}>
                                    <Tooltip title="预览/试用">
                                        <Button type="text" size="small" icon={<ExportOutlined />} href={`/image/app/${encodeURIComponent(item.slug)}`} target="_blank" />
                                    </Tooltip>
                                    <Tooltip title="编辑">
                                        <Button type="text" size="small" icon={<EditOutlined />} onClick={() => setEditing(item)} />
                                    </Tooltip>
                                    <Popconfirm title={`删除「${item.title}」？`} okText="删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={() => remove.mutateAsync(item.id)}>
                                        <Button type="text" size="small" danger icon={<DeleteOutlined />} />
                                    </Popconfirm>
                                </Space>
                            ),
                        },
                    ]}
                />
            </Card>
            <TemplateEditor item={editing} saving={save.isPending} onSave={submit} onClose={() => setEditing(null)} />
            <TemplateSamplesModal open={samplesOpen} onClose={() => setSamplesOpen(false)} />
            <Drawer title="示例生成记录" open={recordsOpen} size="large" onClose={() => setRecordsOpen(false)}>
                <Typography.Paragraph type="secondary" className="!text-xs">
                    最近 100 条示例生成；「设为封面」「加入示例」会直接保存到对应模板。
                </Typography.Paragraph>
                <ExampleJobs jobs={records.data ?? []} titles={Object.fromEntries(data.map((item) => [item.id, item.title]))} onCover={(job) => applyRecord(job, true)} onExample={(job) => applyRecord(job, false)} />
            </Drawer>
        </main>
    );
}
