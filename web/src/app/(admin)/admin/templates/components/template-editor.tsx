"use client";

import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Checkbox, Col, Form, Input, InputNumber, Modal, Radio, Row, Select, Space, Switch, Typography } from "antd";

import { UploadDropzone } from "@/app/(user)/image/components/upload-dropzone";
import { imageRefPreview, templateCategoryLabels, templateRatios, templateResolutions } from "@/lib/image-templates";
import type { ImageTemplate, ImageTemplateInput } from "@/services/api/templates";

export const newTemplate: Partial<ImageTemplate> = {
    category: "ecommerce",
    inputs: [{ key: "product", label: "商品图", max: 1, required: true }],
    promptRequired: false,
    ratios: ["1:1"],
    resolutions: ["1K"],
    feeCredits: 0,
    memberFree: true,
    examples: [],
    status: "draft",
    sort: 0,
};

/** Stored-content URLs edited through the shared upload zone (files upload via the same API, kept as /api/files/:id/content). */
function UrlImages({ value, onChange, max }: { value?: string[]; onChange?: (urls: string[]) => void; max: number }) {
    return <UploadDropzone compact max={max} value={(value ?? []).map((url) => ({ fileId: url, url }))} onChange={(images) => onChange?.(images.map((image) => image.url))} />;
}

const singleUrl = { getValueProps: (value?: string) => ({ value: value ? [value] : [] }), normalize: (urls: string[]) => urls[0] ?? "" };

export function TemplateEditor({ item, saving, onSave, onClose }: { item: Partial<ImageTemplate> | null; saving: boolean; onSave: (item: Partial<ImageTemplate>) => void; onClose: () => void }) {
    const [form] = Form.useForm<Partial<ImageTemplate>>();
    const inputs = Form.useWatch("inputs", form) as ImageTemplateInput[] | undefined;
    const slug = Form.useWatch("slug", form) as string | undefined;
    const total = (inputs ?? []).reduce((sum, input) => sum + (input?.max || 0), 0);

    return (
        <Modal
            title={item?.id ? "编辑模板" : "新增模板"}
            open={Boolean(item)}
            width={880}
            onCancel={onClose}
            destroyOnHidden
            footer={
                <Space>
                    {item?.id && slug && (
                        <Button href={`/image/app/${encodeURIComponent(slug)}`} target="_blank">
                            预览/试用
                        </Button>
                    )}
                    <Button onClick={onClose}>取消</Button>
                    <Button type="primary" loading={saving} onClick={() => void form.validateFields().then(() => onSave({ ...item, ...form.getFieldsValue(true) }))}>
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
                        <Form.Item name="slug" label="标识（链接 /image/app/标识）" rules={[{ required: true, pattern: /^[a-z0-9-]{2,64}$/, message: "2-64 位小写字母、数字和 -" }]}>
                            <Input />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="category" label="分类">
                            <Select options={Object.entries(templateCategoryLabels).map(([value, label]) => ({ value, label }))} />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={8}>
                        <Form.Item name="status" label="状态">
                            <Radio.Group optionType="button" options={[{ label: "草稿", value: "draft" }, { label: "已发布", value: "published" }]} />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={8}>
                        <Form.Item name="sort" label="排序（小的在前）">
                            <InputNumber precision={0} className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={16}>
                        <Form.Item name="description" label="一句话说明" rules={[{ max: 300 }]}>
                            <Input.TextArea rows={3} showCount maxLength={300} />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="coverUrl" label="封面" {...singleUrl}>
                            <UrlImages max={1} />
                        </Form.Item>
                    </Col>
                </Row>

                <Typography.Text strong>输入图片组</Typography.Text>
                <Typography.Paragraph type="secondary" className="!mb-2 !text-xs">
                    最多 4 组，每组 1-4 张，合计不超过 6 张（当前 {total} 张）。
                </Typography.Paragraph>
                <Form.List name="inputs">
                    {(fields, { add, remove }) => (
                        <div className="mb-4 space-y-2">
                            {fields.map((field) => (
                                <Space key={field.key} align="start" wrap>
                                    <Form.Item name={[field.name, "label"]} className="!mb-0" rules={[{ required: true, max: 20, message: "名称 1-20 字" }]}>
                                        <Input placeholder="名称，如 商品图" className="!w-40" />
                                    </Form.Item>
                                    <Form.Item name={[field.name, "key"]} className="!mb-0" rules={[{ required: true, pattern: /^[a-z0-9_-]{1,32}$/, message: "小写字母、数字、_ -" }]}>
                                        <Input placeholder="标识，如 product" className="!w-36" />
                                    </Form.Item>
                                    <Form.Item name={[field.name, "max"]} className="!mb-0">
                                        <InputNumber min={1} max={4} precision={0} prefix="最多" suffix="张" className="!w-36" />
                                    </Form.Item>
                                    <Form.Item name={[field.name, "required"]} valuePropName="checked" className="!mb-0">
                                        <Checkbox className="!leading-8">必填</Checkbox>
                                    </Form.Item>
                                    <Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)} />
                                </Space>
                            ))}
                            <Button icon={<PlusOutlined />} disabled={fields.length >= 4 || total >= 6} onClick={() => add({ key: `image${fields.length + 1}`, label: "", max: 1, required: false })}>
                                添加输入组
                            </Button>
                        </div>
                    )}
                </Form.List>

                <Form.Item name="prompt" label="系统提示词（不对用户展示）" rules={[{ required: true, max: 8000, message: "提示词需为 1-8000 字" }]}>
                    <Input.TextArea rows={6} showCount maxLength={8000} />
                </Form.Item>
                <Alert
                    className="!-mt-2 !mb-4"
                    type="info"
                    showIcon
                    title={
                        <div className="text-xs leading-5">
                            用 <code>@图片N</code> 引用上传的图片，按输入组顺序、组内上传顺序编号；用户少传时后面的编号会前移（系统会在提示词前说明每个编号对应哪组）。用 <code>{"{{prompt}}"}</code> 插入用户的自定义提示词，不写则追加到末尾。
                            {imageRefPreview(inputs ?? []).map((group, index) => (
                                <div key={index}>
                                    {group.refs.join("、") || "—"} → {group.label || `第 ${index + 1} 组`}
                                </div>
                            ))}
                        </div>
                    }
                />
                <Row gutter={16}>
                    <Col xs={24} md={16}>
                        <Form.Item name="promptPlaceholder" label="用户提示词占位文案" rules={[{ max: 300 }]}>
                            <Input maxLength={300} />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="promptRequired" label="用户提示词必填" valuePropName="checked">
                            <Switch />
                        </Form.Item>
                    </Col>
                    <Col xs={24}>
                        <Form.Item name="ratios" label="比例" rules={[{ required: true, type: "array", min: 1, message: "请至少选择一个比例" }]}>
                            <Checkbox.Group options={templateRatios} />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                        <Form.Item name="resolutions" label="分辨率" rules={[{ required: true, type: "array", min: 1, message: "请至少选择一个分辨率" }]}>
                            <Checkbox.Group options={templateResolutions} />
                        </Form.Item>
                    </Col>
                    <Col xs={24} md={16}>
                        <Form.Item name="model" label="生图模型" rules={[{ max: 128 }]}>
                            <Input placeholder="留空使用电商生图模型" />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={8}>
                        <Form.Item name="feeCredits" label="模板费（积分/张，0 为免费）">
                            <InputNumber min={0} max={10000} step={0.1} precision={2} className="!w-full" />
                        </Form.Item>
                    </Col>
                    <Col xs={12} md={8}>
                        <Form.Item name="memberFree" label="会员免模板费" valuePropName="checked">
                            <Switch />
                        </Form.Item>
                    </Col>
                </Row>

                <Typography.Text strong>作品示例</Typography.Text>
                <Form.List name="examples">
                    {(fields, { add, remove }) => (
                        <div className="mt-2 space-y-3">
                            {fields.map((field, index) => (
                                <Card key={field.key} size="small" title={`示例 ${index + 1}`} extra={<Button type="text" danger size="small" icon={<DeleteOutlined />} onClick={() => remove(field.name)} />}>
                                    <Row gutter={16}>
                                        <Col xs={24} md={14}>
                                            <Form.Item name={[field.name, "inputs"]} label="原图（最多 6 张）">
                                                <UrlImages max={6} />
                                            </Form.Item>
                                        </Col>
                                        <Col xs={24} md={10}>
                                            <Form.Item name={[field.name, "output"]} label="AI 作品" rules={[{ required: true, message: "请上传 AI 作品" }]} {...singleUrl}>
                                                <UrlImages max={1} />
                                            </Form.Item>
                                        </Col>
                                    </Row>
                                    <Form.Item name={[field.name, "prompt"]} label="创作提示词" className="!mb-0" rules={[{ max: 2000 }]}>
                                        <Input.TextArea rows={2} maxLength={2000} />
                                    </Form.Item>
                                </Card>
                            ))}
                            <Button icon={<PlusOutlined />} disabled={fields.length >= 20} onClick={() => add({ inputs: [], output: "", prompt: "" })}>
                                添加示例
                            </Button>
                        </div>
                    )}
                </Form.List>
            </Form>
        </Modal>
    );
}
