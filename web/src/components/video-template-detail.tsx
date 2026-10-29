"use client";

import { Button, Modal, Table, Tag } from "antd";
import { LayoutTemplate, Play } from "lucide-react";

import { formatSeconds, storyboardSeconds, videoStyles, videoTemplateCategoryLabels } from "@/lib/product-video";
import type { ImageTemplateSummary, VideoTemplateShot } from "@/services/api/templates";

const categoryLabel = (value: string) => videoTemplateCategoryLabels[value as keyof typeof videoTemplateCategoryLabels] ?? "";
const styleLabel = (value: string) => videoStyles.find((style) => style.value === value)?.label ?? value;
const exampleOf = (item: ImageTemplateSummary) => item.examples?.find((example) => example.video);

/** 30 秒 · 9:16 · 开箱测评 · 6 镜 */
export const videoTemplateMeta = (item: ImageTemplateSummary) => {
    const defaults = item.videoDefaults;
    return defaults ? `${defaults.duration} 秒 · ${defaults.ratio} · ${styleLabel(defaults.style)}${defaults.shots?.length ? ` · ${defaults.shots.length} 镜` : ""}` : "";
};

/** Template card: cover (the example clip plays muted on hover), category, title, meta; clicking opens the detail. */
export function VideoTemplateCard({ item, onOpen, onUse }: { item: ImageTemplateSummary; onOpen: () => void; onUse?: () => void }) {
    const example = exampleOf(item);
    const poster = item.coverUrl || example?.poster;
    return (
        <div className="flex cursor-pointer flex-col overflow-hidden rounded-xl border border-border bg-background transition hover:border-foreground/40" onClick={onOpen}>
            <div className="relative aspect-[16/10] bg-linear-to-br from-rose-100 via-amber-50 to-orange-100 dark:from-rose-500/20 dark:via-amber-500/5 dark:to-orange-500/15">
                {example ? (
                    <video
                        src={example.video}
                        poster={poster}
                        muted
                        loop
                        playsInline
                        preload="none"
                        className="absolute inset-0 size-full object-cover"
                        onMouseEnter={(event) => void event.currentTarget.play().catch(() => undefined)}
                        onMouseLeave={(event) => event.currentTarget.pause()}
                    />
                ) : poster ? (
                    <img src={poster} alt="" className="absolute inset-0 size-full object-cover" />
                ) : (
                    <LayoutTemplate className="absolute right-4 top-4 size-12 text-foreground/15" strokeWidth={1} />
                )}
                <span className="absolute left-2 top-2 rounded-md bg-black/60 px-2 py-0.5 text-[11px] text-white">{categoryLabel(item.category)}</span>
                {example && (
                    <span className="pointer-events-none absolute bottom-2 right-2 flex items-center gap-1 rounded-md bg-black/60 px-1.5 py-0.5 text-[11px] text-white">
                        <Play className="size-3" fill="currentColor" />
                        {formatSeconds(example.duration ?? 0)}
                    </span>
                )}
            </div>
            <div className="flex flex-1 flex-col gap-1 p-3">
                <div className="truncate text-sm font-medium">{item.title}</div>
                <div className="line-clamp-2 flex-1 text-xs text-muted-foreground">{item.description}</div>
                <div className="flex items-center justify-between gap-2 pt-1 text-[11px] text-muted-foreground">
                    <span className="truncate">{videoTemplateMeta(item)}</span>
                    {onUse && (
                        <Button
                            size="small"
                            type="primary"
                            onClick={(event) => {
                                event.stopPropagation();
                                onUse();
                            }}
                        >
                            使用
                        </Button>
                    )}
                </div>
            </div>
        </div>
    );
}

const shotColumns = [
    { title: "镜号", key: "index", width: 56, render: (_: unknown, __: VideoTemplateShot, index: number) => index + 1 },
    { title: "时长", dataIndex: "seconds", width: 64, render: (seconds: number) => `${seconds} 秒` },
    { title: "画面", dataIndex: "scene" },
    { title: "运镜", dataIndex: "camera", width: 110 },
    { title: "旁白", dataIndex: "voiceover", width: 180 },
    { title: "字幕", dataIndex: "subtitle", width: 120 },
];

/** Template detail: the example video (poster first, plays on click), the storyboard table and 使用模板. */
export function VideoTemplateDetail({ item, onClose, onUse }: { item: ImageTemplateSummary | null; onClose: () => void; onUse: (item: ImageTemplateSummary) => void }) {
    const example = item && exampleOf(item);
    const shots = item?.videoDefaults?.shots ?? [];
    return (
        <Modal
            title={item?.title}
            open={Boolean(item)}
            width={960}
            onCancel={onClose}
            destroyOnHidden
            footer={
                <Button type="primary" onClick={() => item && onUse(item)}>
                    使用模板
                </Button>
            }
        >
            {item && (
                <div className="space-y-4">
                    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <Tag className="!m-0">{categoryLabel(item.category)}</Tag>
                        <span>{videoTemplateMeta(item)}</span>
                    </div>
                    {item.description && <p className="text-sm text-muted-foreground">{item.description}</p>}
                    {example ? (
                        <video key={example.video} src={example.video} poster={example.poster || item.coverUrl} controls playsInline preload="none" className="max-h-[56vh] w-full rounded-lg bg-black object-contain" />
                    ) : (
                        item.coverUrl && <img src={item.coverUrl} alt="" className="max-h-[56vh] w-full rounded-lg object-contain" />
                    )}
                    {shots.length > 0 && (
                        <div>
                            <div className="mb-2 text-sm font-medium">
                                分镜脚本{" "}
                                <span className="text-xs font-normal text-muted-foreground">
                                    · {shots.length} 镜 · {storyboardSeconds(shots)} 秒
                                </span>
                            </div>
                            <Table size="small" pagination={false} scroll={{ x: 720 }} columns={shotColumns} dataSource={shots.map((shot, index) => ({ ...shot, key: index }))} />
                        </div>
                    )}
                    {item.credit && <div className="text-xs text-muted-foreground">示例来源：{item.credit}</div>}
                </div>
            )}
        </Modal>
    );
}
