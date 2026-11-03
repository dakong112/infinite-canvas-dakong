"use client";

import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Flex, Form, Popconfirm, Select, Tag, Tooltip, Typography } from "antd";
import { useState } from "react";

import { groupLabel, platformChannels, platformGroupOf } from "@/lib/model-catalog";
import { isWorkflowProtocol } from "@/lib/model-channel";
import { isVisionModel } from "@/lib/vision-models";
import { testFeatureModel, type AdminModelChannel, type FeatureKey, type FeatureModel, type FeatureModelTestResult } from "@/services/api/admin";
import { filterChannelModelsByCapability, useConfigStore, useModelCatalog, type ModelCapability } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

const features: { key: FeatureKey; title: string; kind: ModelCapability; extra: string }[] = [
    { key: "vision", title: "识图", kind: "text", extra: "解析卖点、AI 优化、换方向、规划套图，需支持 /responses 图片输入；为空时使用默认文本模型" },
    { key: "image", title: "生图", kind: "image", extra: "套图预览 / 正式图、模板生成（模板未指定模型时）、图片工具，需支持 /images/edits；为空时使用默认图片模型" },
    { key: "videoScript", title: "分镜脚本", kind: "text", extra: "带货视频识图写分镜；为空时使用识图模型" },
    { key: "keyframe", title: "关键帧", kind: "image", extra: "带货视频每镜首帧；为空时使用生图模型，仍为空则直接用产品图生成视频" },
    { key: "video", title: "视频", kind: "video", extra: "带货视频逐镜图生视频，需支持 /videos；为空时使用默认视频模型" },
    { key: "tts", title: "配音", kind: "audio", extra: "走 /audio/speech；为空时不提供配音" },
];

/** "功能模型": per feature, the models tried in order; the next one is used when one is unusable for a request. */
export function FeatureModelsSection() {
    const form = Form.useFormInstance();
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    const ssoMode = useConfigStore((state) => state.publicSettings !== null && state.publicSettings.auth?.newapiSso !== false);
    const catalog = useModelCatalog();
    const adminChannels: AdminModelChannel[] = Form.useWatch(["private", "channels"], form) || [];
    const [testing, setTesting] = useState("");
    const [results, setResults] = useState<Record<string, FeatureModelTestResult>>({});

    // SSO: one virtual channel per new-api group; otherwise the enabled admin channels.
    const channels = ssoMode ? (catalog ? platformChannels(catalog).map((channel) => ({ ...channel, id: platformGroupOf(channel.id) })) : []) : adminChannels.filter((channel) => channel.enabled && !isWorkflowProtocol(channel.protocol));
    const groupOptions = [
        { label: ssoMode ? "用户所在分组" : "自动选择渠道", value: "" },
        ...(ssoMode ? (catalog?.groups || []).map((group) => ({ label: `${groupLabel(group.name)}${group.desc ? ` · ${group.desc}` : ""}`, value: group.name })) : channels.map((channel) => ({ label: channel.name || channel.id, value: channel.id }))),
    ];
    const modelOptions = (group: string, kind: ModelCapability, current?: string) => {
        const models = filterChannelModelsByCapability(group ? channels.filter((channel) => channel.id === group) : channels, kind);
        if (current && !models.includes(current)) models.unshift(current);
        return models.map((model) => ({
            value: model,
            label: (
                <Flex gap={6} align="center">
                    <span className="truncate">{model}</span>
                    {isVisionModel(model) && (
                        <Tag color="blue" className="!m-0">
                            支持识图
                        </Tag>
                    )}
                </Flex>
            ),
        }));
    };

    const runTest = async (feature: FeatureKey, item: FeatureModel) => {
        if (!token || !item?.model) return;
        const key = `${feature}|${item.group}|${item.model}`;
        setTesting(key);
        try {
            const result = await testFeatureModel(token, { feature, group: item.group || "", model: item.model });
            setResults((current) => ({ ...current, [key]: result }));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "测试失败");
        } finally {
            setTesting("");
        }
    };

    return (
        <Card size="small" title="功能模型">
            <Typography.Paragraph type="secondary" className="!text-xs">
                每个功能按顺序尝试，最多 5 个：模型不存在 / 不在分组 / 无可用渠道、不支持图片输入、上游 5xx、超时、限流重试用尽时自动换下一个；余额不足和内容审核拒绝不会换。费用始终记在用户自己的账户上。“测试”用你自己的账户真实调用一次。
            </Typography.Paragraph>
            <Flex vertical gap={12}>
                {features.map((feature) => (
                    <Card key={feature.key} size="small" type="inner" title={feature.title}>
                        <Typography.Paragraph type="secondary" className="!mb-2 !text-xs">
                            {feature.extra}
                        </Typography.Paragraph>
                        <Form.List name={["private", "featureModels", feature.key]}>
                            {(fields, { add, remove, move }) => (
                                <Flex vertical gap={8}>
                                    {fields.map((field, index) => (
                                        <Form.Item key={field.key} noStyle shouldUpdate>
                                            {() => {
                                                const item: FeatureModel = form.getFieldValue(["private", "featureModels", feature.key, field.name]) || { group: "", model: "" };
                                                const result = results[`${feature.key}|${item.group}|${item.model}`];
                                                const testButton = (
                                                    <Button size="small" disabled={!item.model} loading={testing === `${feature.key}|${item.group}|${item.model}`} onClick={feature.key === "video" ? undefined : () => void runTest(feature.key, item)}>
                                                        测试
                                                    </Button>
                                                );
                                                return (
                                                    <Flex gap={8} align="center" wrap>
                                                        <span className="w-5 text-xs text-[var(--ant-color-text-tertiary)]">{index + 1}</span>
                                                        <Form.Item name={[field.name, "group"]} noStyle initialValue="">
                                                            <Select className="!w-44" options={groupOptions} onChange={() => form.setFieldValue(["private", "featureModels", feature.key, field.name, "model"], undefined)} />
                                                        </Form.Item>
                                                        <Form.Item name={[field.name, "model"]} noStyle>
                                                            <Select className="!min-w-56 flex-1" showSearch={{ optionFilterProp: "value" }} placeholder="选择模型" options={modelOptions(item.group || "", feature.kind, item.model)} />
                                                        </Form.Item>
                                                        <Button size="small" icon={<ArrowUpOutlined />} disabled={index === 0} onClick={() => move(index, index - 1)} />
                                                        <Button size="small" icon={<ArrowDownOutlined />} disabled={index === fields.length - 1} onClick={() => move(index, index + 1)} />
                                                        {feature.key === "video" ? (
                                                            <Popconfirm title="视频测试会真实提交一个任务并按价格扣费，确定测试？" onConfirm={() => void runTest(feature.key, item)}>
                                                                {testButton}
                                                            </Popconfirm>
                                                        ) : (
                                                            testButton
                                                        )}
                                                        {result && (
                                                            <Tooltip title={result.ok ? result.detail : result.error}>
                                                                <Tag color={result.ok ? "success" : "error"} className="!m-0">
                                                                    {result.ok ? "通过" : "失败"} · {(result.latencyMs / 1000).toFixed(1)}s
                                                                </Tag>
                                                            </Tooltip>
                                                        )}
                                                        <Button size="small" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)} />
                                                    </Flex>
                                                );
                                            }}
                                        </Form.Item>
                                    ))}
                                    <Button icon={<PlusOutlined />} className="self-start" disabled={fields.length >= 5} onClick={() => add({ group: "", model: undefined })}>
                                        添加模型
                                    </Button>
                                </Flex>
                            )}
                        </Form.List>
                    </Card>
                ))}
            </Flex>
        </Card>
    );
}
