"use client";

import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { AutoComplete, Button, Col, DatePicker, Form, Input, InputNumber, Modal, Radio, Row, Space, Switch, Typography } from "antd";
import dayjs, { type Dayjs } from "dayjs";

import { singleUrl, UrlImages } from "@/app/(admin)/admin/templates/components/template-editor";
import { hasPermission, PERMISSIONS } from "@/lib/admin-permissions";
import type { AcademyCourse } from "@/services/api/academy";
import { useUserStore } from "@/stores/use-user-store";

export const newCourse: Partial<AcademyCourse> = { kind: "camp", category: "AI技能提升", lessons: [], capacity: 0, feeCredits: 0, memberOnly: false, memberFree: true, status: "draft", sort: 0 };

/** Dates are kept as ISO strings in the form values. */
const dateField = { getValueProps: (value?: string | null) => ({ value: value ? dayjs(value) : null }), normalize: (value: Dayjs | null) => (value ? value.toISOString() : null) };

export function CourseEditor({ item, categories, saving, onSave, onClose }: { item: Partial<AcademyCourse> | null; categories: string[]; saving: boolean; onSave: (item: Partial<AcademyCourse>) => void; onClose: () => void }) {
    const [form] = Form.useForm<Partial<AcademyCourse>>();
    const canWrite = useUserStore((state) => hasPermission(state.user, PERMISSIONS.academyWrite));

    return (
        <Modal
            title={item?.id ? "编辑课程" : "新增课程"}
            open={Boolean(item)}
            width={880}
            onCancel={onClose}
            destroyOnHidden
            footer={
                <Space>
                    {item?.id && item.status === "published" && (
                        <Button href={`/academy/${encodeURIComponent(item.slug ?? "")}`} target="_blank">
                            查看
                        </Button>
                    )}
                    <Button onClick={onClose}>取消</Button>
                    <Button type="primary" loading={saving} disabled={!canWrite} onClick={() => void form.validateFields().then(() => onSave({ ...item, ...form.getFieldsValue(true) }))}>
                        保存
                    </Button>
                </Space>
            }
        >
            <Form form={form} layout="vertical" requiredMark={false} preserve={false} initialValues={item ?? undefined}>
                <Row gutter={16}>
                    <Col xs={24} md={12}>
                        <Form.Item name="title" label="标题" rules={[{ required: true, max: 100, message: "标题需为 1-100 字" }]}>
                            <Input />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={12}>
                        <Form.Item name="slug" label="标识（链接 /academy/标识）" rules={[{ required: true, pattern: /^[a-z0-9-]{2,64}$/, message: "2-64 位小写字母、数字和 -" }]}>
                            <Input />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="kind" label="类型">
                            <Radio.Group optionType="button" options={[{ label: "训练营", value: "camp" }, { label: "课程", value: "course" }]} />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="category" label="分类（列表标签）" rules={[{ required: true, max: 16, message: "分类 1-16 字" }]}>
                            <AutoComplete options={categories.map((value) => ({ value }))} />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="status" label="状态">
                            <Radio.Group optionType="button" options={[{ label: "草稿", value: "draft" }, { label: "已发布", value: "published" }]} />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="sort" label="排序（小的在前）">
                            <InputNumber precision={0} className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={16}>
                        <Form.Item name="summary" label="简介（列表卡片）" rules={[{ max: 300 }]}>
                            <Input.TextArea rows={3} showCount maxLength={300} />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="coverUrl" label="封面" {...singleUrl}>
                            <UrlImages max={1} />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="startsAt" label="开始时间" {...dateField}>
                            <DatePicker showTime className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="endsAt" label="结束时间" {...dateField}>
                            <DatePicker showTime className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="enrollEndsAt" label="报名截止（空则到结束为止）" {...dateField}>
                            <DatePicker showTime className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="capacity" label="名额（0 不限）">
                            <InputNumber min={0} max={100000} precision={0} className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="feeCredits" label="报名费（积分，0 免费）">
                            <InputNumber min={0} max={100000} precision={2} className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="memberOnly" label="会员专属" valuePropName="checked">
                            <Switch />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={6}>
                        <Form.Item name="memberFree" label="会员免费" valuePropName="checked">
                            <Switch />
                        </Form.Item>
                    </Col>
                </Row>
                <Form.Item name="body" label="详情（Markdown）" rules={[{ max: 20000 }]}>
                    <Input.TextArea autoSize={{ minRows: 4, maxRows: 12 }} maxLength={20000} />
                </Form.Item>
                <Typography.Text strong>课时</Typography.Text>
                <Typography.Paragraph type="secondary" className="!mb-2 !text-xs">
                    视频填 http(s) 链接或上传后的文件地址（/api/files/…）；付费或会员专属课程的视频只对已报名用户显示。
                </Typography.Paragraph>
                <Form.List name="lessons">
                    {(fields, { add, remove }) => (
                        <div className="space-y-2">
                            {fields.map((field, index) => (
                                <div key={field.key} className="grid grid-cols-[1fr_2fr_auto] items-start gap-2">
                                    <Form.Item name={[field.name, "title"]} className="!mb-0" rules={[{ required: true, max: 100, message: "标题 1-100 字" }]}>
                                        <Input placeholder={`第 ${index + 1} 节标题`} />
                                    </Form.Item>
                                    <Form.Item name={[field.name, "videoUrl"]} className="!mb-0" rules={[{ max: 2048 }]}>
                                        <Input placeholder="视频地址（可留空）" />
                                    </Form.Item>
                                    <Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)} />
                                </div>
                            ))}
                            <Button icon={<PlusOutlined />} disabled={fields.length >= 100} onClick={() => add({ title: "", videoUrl: "" })}>
                                添加课时
                            </Button>
                        </div>
                    )}
                </Form.List>
            </Form>
        </Modal>
    );
}
