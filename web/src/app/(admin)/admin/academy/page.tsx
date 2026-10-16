"use client";

import { DeleteOutlined, EditOutlined, PlusOutlined, TeamOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Drawer, Flex, Image, Popconfirm, Space, Table, Tag, Tooltip, Typography } from "antd";
import dayjs from "dayjs";
import { useState } from "react";

import { hasPermission, PERMISSIONS } from "@/lib/admin-permissions";
import { courseProblem, seatsLabel } from "@/lib/academy";
import { deleteAdminAcademyCourse, fetchAdminAcademyCourses, fetchAdminAcademyEnrollments, saveAdminAcademyCourse, type AcademyCourse } from "@/services/api/academy";
import { useUserStore } from "@/stores/use-user-store";
import { CourseEditor, newCourse } from "./components/course-editor";

const time = (value?: string | null) => (value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "-");

export default function AdminAcademyPage() {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const canWrite = useUserStore((state) => hasPermission(state.user, PERMISSIONS.academyWrite));
    const [editing, setEditing] = useState<Partial<AcademyCourse> | null>(null);
    const [roster, setRoster] = useState<AcademyCourse | null>(null);
    const { data = [], isLoading } = useQuery({ queryKey: ["admin", "academy", token], queryFn: () => fetchAdminAcademyCourses(token), enabled: Boolean(token), retry: false });
    const enrollments = useQuery({ queryKey: ["admin", "academy", "enrollments", roster?.id], queryFn: () => fetchAdminAcademyEnrollments(token, roster!.id), enabled: Boolean(roster), retry: false });
    const refresh = () => queryClient.invalidateQueries({ queryKey: ["admin", "academy"] });
    const fail = (error: Error) => message.error(error.message || "操作失败");

    const save = useMutation({
        mutationFn: (item: Partial<AcademyCourse>) => saveAdminAcademyCourse(token, item),
        onSuccess: () => {
            message.success("课程已保存");
            setEditing(null);
            void refresh();
        },
        onError: fail,
    });
    const remove = useMutation({
        mutationFn: (id: number) => deleteAdminAcademyCourse(token, id),
        onSuccess: () => {
            message.success("课程已删除");
            void refresh();
        },
        onError: fail,
    });
    const submit = (item: Partial<AcademyCourse>) => {
        const problem = courseProblem(item);
        if (problem) message.error(problem);
        else save.mutate(item);
    };

    return (
        <main style={{ padding: 24 }}>
            <Card
                variant="borderless"
                title={
                    <Space>
                        学院课程
                        <Tag>{data.length} 个</Tag>
                    </Space>
                }
                extra={
                    <Button type="primary" icon={<PlusOutlined />} disabled={!canWrite} onClick={() => setEditing({ ...newCourse })}>
                        新增课程
                    </Button>
                }
            >
                <Table<AcademyCourse>
                    rowKey="id"
                    loading={isLoading}
                    dataSource={data}
                    pagination={false}
                    scroll={{ x: 1000 }}
                    columns={[
                        {
                            title: "封面",
                            dataIndex: "coverUrl",
                            width: 80,
                            render: (_, item) => <Image src={item.coverUrl || "/logo.svg"} alt={item.title} width={48} height={48} style={{ objectFit: "cover", borderRadius: 6 }} fallback="/logo.svg" />,
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
                                        {item.kind === "camp" ? "训练营" : "课程"} · {item.category} · {item.slug}
                                    </Typography.Text>
                                </Flex>
                            ),
                        },
                        { title: "开始", dataIndex: "startsAt", width: 150, render: (_, item) => time(item.startsAt) },
                        { title: "报名", key: "seats", width: 200, render: (_, item) => seatsLabel(item) },
                        {
                            title: "报名费",
                            dataIndex: "feeCredits",
                            width: 160,
                            render: (_, item) => (
                                <Space size={4} wrap>
                                    {item.feeCredits > 0 ? item.feeCredits : <Typography.Text type="secondary">免费</Typography.Text>}
                                    {item.memberOnly && <Tag color="gold">会员专属</Tag>}
                                    {item.memberFree && item.feeCredits > 0 && <Tag>会员免费</Tag>}
                                </Space>
                            ),
                        },
                        {
                            title: "状态",
                            dataIndex: "status",
                            width: 90,
                            render: (_, item) => (item.status === "published" ? <Tag color="success">已发布</Tag> : <Tag>草稿</Tag>),
                        },
                        { title: "排序", dataIndex: "sort", width: 70 },
                        {
                            title: "操作",
                            key: "actions",
                            width: 120,
                            render: (_, item) => (
                                <Space size={4}>
                                    <Tooltip title="报名名单">
                                        <Button type="text" size="small" icon={<TeamOutlined />} onClick={() => setRoster(item)} />
                                    </Tooltip>
                                    <Tooltip title="编辑">
                                        <Button type="text" size="small" icon={<EditOutlined />} onClick={() => setEditing(item)} />
                                    </Tooltip>
                                    <Popconfirm title={`删除「${item.title}」？`} okText="删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={() => remove.mutateAsync(item.id)}>
                                        <Button type="text" size="small" danger icon={<DeleteOutlined />} disabled={!canWrite} />
                                    </Popconfirm>
                                </Space>
                            ),
                        },
                    ]}
                />
            </Card>
            <CourseEditor item={editing} categories={[...new Set(data.map((item) => item.category))]} saving={save.isPending} onSave={submit} onClose={() => setEditing(null)} />
            <Drawer title={roster ? `报名名单 · ${roster.title}` : ""} open={Boolean(roster)} size="large" onClose={() => setRoster(null)}>
                <Table
                    rowKey="id"
                    size="small"
                    loading={enrollments.isLoading}
                    dataSource={enrollments.data ?? []}
                    pagination={{ pageSize: 50 }}
                    columns={[
                        { title: "用户", dataIndex: "username", render: (_, row) => row.username || row.userId },
                        { title: "费用", dataIndex: "feeStatus", width: 140, render: (_, row) => (row.feeStatus === "charged" ? <Tag color="blue">已扣 {row.feeQuota} 额度</Tag> : <Tag>免费</Tag>) },
                        { title: "报名时间", dataIndex: "createdAt", width: 160, render: (_, row) => time(row.createdAt) },
                    ]}
                />
            </Drawer>
        </main>
    );
}
