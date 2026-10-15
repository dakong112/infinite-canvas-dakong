"use client";

import { useQuery } from "@tanstack/react-query";
import { Input } from "antd";
import { Clapperboard, LayoutTemplate, Search, ShoppingBag, Video } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { videoTemplateCategoryLabels } from "@/lib/product-video";
import { fetchTemplates } from "@/services/api/templates";
import { CoverCard, SectionTitle } from "../image/components/image-tool-shell";

const videoTools = [
    {
        key: "product",
        href: "/video/product",
        title: "带货视频",
        summary: "上传产品图，AI 写分镜、逐镜生成并合成带配音字幕的成片",
        icon: ShoppingBag,
        tone: "from-rose-100 via-orange-50 to-amber-100 dark:from-rose-500/20 dark:via-orange-500/5 dark:to-amber-500/15",
    },
    {
        key: "free",
        href: "/video/free",
        title: "自由视频",
        summary: "文生视频、图生视频，自由选择模型与参数",
        icon: Clapperboard,
        tone: "from-sky-100 via-indigo-50 to-violet-100 dark:from-sky-500/20 dark:via-indigo-500/5 dark:to-violet-500/15",
    },
];

const matches = (keyword: string, ...texts: string[]) => !keyword || texts.some((text) => text.toLowerCase().includes(keyword));

export default function VideoHubPage() {
    const [keyword, setKeyword] = useState("");
    const search = keyword.trim().toLowerCase();
    // ponytail: no video templates yet (or the endpoint is missing) just hides the section.
    const { data } = useQuery({ queryKey: ["templates", "video"], queryFn: () => fetchTemplates("", "video"), retry: false, staleTime: 5 * 60 * 1000 });
    const tools = videoTools.filter((tool) => matches(search, tool.title, tool.summary));
    const templates = (data ?? []).filter((item) => matches(search, item.title, item.description));

    return (
        <div className="thin-scrollbar h-full overflow-y-auto bg-stone-50 text-foreground dark:bg-background">
            <div className="mx-auto max-w-[1600px] space-y-8 px-4 py-5 md:px-6">
                <nav aria-label="视频工具" className="flex items-center gap-2 rounded-xl border border-border bg-card p-1.5">
                    <div className="thin-scrollbar flex min-w-0 flex-1 gap-1 overflow-x-auto">
                        <span className="flex shrink-0 items-center gap-1.5 rounded-lg bg-foreground px-3 py-1.5 text-sm font-medium text-background">
                            <Video className="size-4" />
                            视频制作
                        </span>
                        {videoTools.map((tool) => (
                            <Link key={tool.key} href={tool.href} className="flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm !text-muted-foreground transition hover:bg-muted hover:!text-foreground">
                                <tool.icon className="size-4" />
                                {tool.title}
                            </Link>
                        ))}
                    </div>
                    <Input
                        allowClear
                        variant="borderless"
                        className="!w-36 shrink-0 sm:!w-52"
                        prefix={<Search className="size-4 text-muted-foreground" />}
                        placeholder="搜索工具或模板"
                        value={keyword}
                        onChange={(event) => setKeyword(event.target.value)}
                    />
                </nav>

                {tools.length > 0 && (
                    <section>
                        <SectionTitle>精品推荐</SectionTitle>
                        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
                            {tools.map((tool) => (
                                <CoverCard key={tool.key} href={tool.href} tone={tool.tone} icon={<tool.icon className="size-20" strokeWidth={1} />} title={tool.title} summary={tool.summary} />
                            ))}
                        </div>
                    </section>
                )}

                {templates.length > 0 && (
                    <section>
                        <SectionTitle>视频模板</SectionTitle>
                        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
                            {templates.map((item) => (
                                <CoverCard
                                    key={item.slug}
                                    href={`/video/product?template=${encodeURIComponent(item.slug)}`}
                                    cover={item.coverUrl || undefined}
                                    tone="from-rose-100 via-amber-50 to-orange-100 dark:from-rose-500/20 dark:via-amber-500/5 dark:to-orange-500/15"
                                    icon={<LayoutTemplate className="size-16" strokeWidth={1} />}
                                    title={item.title}
                                    summary={`${videoTemplateCategoryLabels[item.category as keyof typeof videoTemplateCategoryLabels] ?? ""} · ${item.description}`}
                                />
                            ))}
                        </div>
                    </section>
                )}
            </div>
        </div>
    );
}
