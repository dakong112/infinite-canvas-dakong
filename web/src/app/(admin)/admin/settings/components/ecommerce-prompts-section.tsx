"use client";

import { useQuery } from "@tanstack/react-query";
import { Button, Card, Flex, Form, Input, Typography } from "antd";

import { fetchEcommercePromptDefaults } from "@/services/api/admin";
import { useUserStore } from "@/stores/use-user-store";

const templates = [
    { key: "analyze", label: "一键解析卖点" },
    { key: "plan", label: "套图方案规划" },
    { key: "sellingPoints", label: "卖点 · 换个方向" },
    { key: "sellingPointRewrite", label: "卖点 · AI 优化" },
] as const;

const variantKinds = [
    { key: "main", label: "产品图变体" },
    { key: "white", label: "白底图变体" },
    { key: "detail", label: "详情页变体" },
    { key: "sku", label: "SKU 图变体" },
];

const base = ["private", "ecommercePrompts"];

/** "电商提示词": admin overrides of the ecommerce set templates; an empty field uses the built-in default. */
export function EcommercePromptsSection() {
    const form = Form.useFormInstance();
    const token = useUserStore((state) => state.token);
    const { data: defaults } = useQuery({ queryKey: ["ecommerce-prompt-defaults"], queryFn: () => fetchEcommercePromptDefaults(token!), enabled: Boolean(token), staleTime: Infinity });

    const restore = (name: string[], value: unknown) => (
        <Button size="small" type="link" disabled={!defaults} onClick={() => form.setFieldValue([...base, ...name], value)}>
            恢复默认
        </Button>
    );

    return (
        <Card size="small" title="电商提示词">
            <Typography.Paragraph type="secondary" className="!text-xs">
                商品套图 / 详情图使用的 AI 模板。留空使用内置默认（灰色提示即默认内容），点“恢复默认”可把默认内容填入后修改；保存时会校验默认模板中的占位符是否都还在。
            </Typography.Paragraph>
            <Flex vertical gap={4}>
                {templates.map(({ key, label }) => {
                    const placeholders = defaults?.placeholders[key] ?? [];
                    return (
                        <Form.Item
                            key={key}
                            name={[...base, key]}
                            label={
                                <Flex gap={4} align="center">
                                    {label}
                                    {restore([key], defaults?.[key])}
                                </Flex>
                            }
                            extra={placeholders.length ? `必需占位符：${placeholders.map((name) => `{{${name}}}`).join(" ")}` : "无占位符"}
                        >
                            <Input.TextArea autoSize={{ minRows: 3, maxRows: 16 }} placeholder={defaults?.[key]} />
                        </Form.Item>
                    );
                })}
                {variantKinds.map(({ key, label }) => (
                    <Form.Item
                        key={key}
                        name={[...base, "variants", key]}
                        label={
                            <Flex gap={4} align="center">
                                {label}
                                {restore(["variants", key], defaults?.variants[key])}
                            </Flex>
                        }
                        extra="一行一条；槽位按顺序轮流使用（未规划方案时的兜底提示词）"
                        getValueProps={(value?: string[]) => ({ value: (value ?? []).join("\n") })}
                        normalize={(value: string) => value.split("\n")}
                    >
                        <Input.TextArea autoSize={{ minRows: 2, maxRows: 12 }} placeholder={defaults?.variants[key]?.join("\n")} />
                    </Form.Item>
                ))}
            </Flex>
        </Card>
    );
}
