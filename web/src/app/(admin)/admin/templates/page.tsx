"use client";

import { DeleteOutlined, EditOutlined, ExportOutlined, PlusOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Flex, Image, Popconfirm, Space, Table, Tag, Tooltip, Typography } from "antd";
import { useState } from "react";

import { templateCategoryLabels, templateProblem } from "@/lib/image-templates";
import { deleteAdminTemplate, fetchAdminTemplates, saveAdminTemplate, type ImageTemplate } from "@/services/api/templates";
import { useUserStore } from "@/stores/use-user-store";
import { newTemplate, TemplateEditor } from "./components/template-editor";

export default function AdminTemplatesPage() {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const [editing, setEditing] = useState<Partial<ImageTemplate> | null>(null);
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
                    <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing({ ...newTemplate })}>
                        新增模板
                    </Button>
                }
            >
                <Table<ImageTemplate>
                    rowKey="id"
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
        </main>
    );
}
