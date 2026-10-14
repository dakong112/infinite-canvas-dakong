"use client";

import { useQuery } from "@tanstack/react-query";
import { Input } from "antd";
import { ChevronRight, ImagePlus, LayoutTemplate, Search } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";

import { imageTools, toolExamples } from "@/lib/image-tool-examples";
import { cn } from "@/lib/utils";
import { fetchTemplates, type ImageTemplateCategory } from "@/services/api/templates";
import { useConfigStore } from "@/stores/use-config-store";
import { toolIcons } from "./components/image-tool-shell";

const templateSections: { category: ImageTemplateCategory; title: string }[] = [
    { category: "ecommerce", title: "电商专区" },
    { category: "creative", title: "创意应用" },
];

const matches = (keyword: string, ...texts: string[]) => !keyword || texts.some((text) => text.toLowerCase().includes(keyword));

/** Cover card shared by tools and templates: image or gradient placeholder, title, one line and an arrow. */
function CoverCard({ href, cover, tone, icon, title, summary }: { href: string; cover?: string; tone: string; icon: ReactNode; title: string; summary: string }) {
    return (
        <Link href={href} className="group relative block aspect-[16/10] overflow-hidden rounded-xl border border-border bg-card">
            {cover ? (
                <img src={cover} alt="" className="absolute inset-0 size-full object-cover transition duration-500 group-hover:scale-105" />
            ) : (
                <div className={cn("absolute inset-0 flex items-start justify-end bg-linear-to-br p-5 text-foreground/15 transition duration-500 group-hover:text-foreground/25", tone)}>{icon}</div>
            )}
            <div className="absolute inset-x-0 bottom-0 bg-linear-to-t from-black/80 via-black/40 to-transparent px-4 pb-3 pt-12">
                <div className="truncate text-[15px] font-semibold !text-white">{title}</div>
                <div className="mt-0.5 flex items-center gap-2 text-xs !text-white/75">
                    <span className="min-w-0 flex-1 truncate">{summary}</span>
                    <ChevronRight className="size-4 shrink-0 transition group-hover:translate-x-0.5" />
                </div>
            </div>
        </Link>
    );
}

function SectionTitle({ children }: { children: ReactNode }) {
    return (
        <h2 className="mb-3 flex items-center gap-2 text-base font-semibold">
            <span className="h-4 w-1 rounded-full bg-foreground/70" />
            {children}
        </h2>
    );
}

function TemplateSection({ category, title, keyword }: { category: ImageTemplateCategory; title: string; keyword: string }) {
    // ponytail: no templates yet (or the endpoint is missing) just hides the section.
    const { data } = useQuery({ queryKey: ["templates", category], queryFn: () => fetchTemplates(category), retry: false, staleTime: 5 * 60 * 1000 });
    const items = (data ?? []).filter((item) => matches(keyword, item.title, item.description));
    if (!items.length) return null;
    return (
        <section>
            <SectionTitle>{title}</SectionTitle>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
                {items.map((item) => (
                    <CoverCard
                        key={item.slug}
                        href={`/image/app/${encodeURIComponent(item.slug)}`}
                        cover={item.coverUrl || undefined}
                        tone={category === "ecommerce" ? "from-orange-100 via-amber-50 to-rose-100 dark:from-orange-500/20 dark:via-amber-500/5 dark:to-rose-500/15" : "from-indigo-100 via-sky-50 to-violet-100 dark:from-indigo-500/20 dark:via-sky-500/5 dark:to-violet-500/15"}
                        icon={<LayoutTemplate className="size-16" strokeWidth={1} />}
                        title={item.title}
                        summary={item.description}
                    />
                ))}
            </div>
        </section>
    );
}

export default function ImageHubPage() {
    const examples = useConfigStore((state) => state.publicSettings?.imageExamples);
    const [keyword, setKeyword] = useState("");
    const search = keyword.trim().toLowerCase();
    const tools = imageTools.filter((tool) => matches(search, tool.title, tool.summary));

    return (
        <div className="thin-scrollbar h-full overflow-y-auto bg-stone-50 text-foreground dark:bg-background">
            <div className="mx-auto max-w-[1600px] space-y-8 px-4 py-5 md:px-6">
                <nav aria-label="图片工具" className="flex items-center gap-2 rounded-xl border border-border bg-card p-1.5">
                    <div className="thin-scrollbar flex min-w-0 flex-1 gap-1 overflow-x-auto">
                        <span className="flex shrink-0 items-center gap-1.5 rounded-lg bg-foreground px-3 py-1.5 text-sm font-medium text-background">
                            <ImagePlus className="size-4" />
                            图片制作
                        </span>
                        {imageTools.map((tool) => {
                            const Icon = toolIcons[tool.key];
                            return (
                                <Link key={tool.key} href={tool.href} className="flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm !text-muted-foreground transition hover:bg-muted hover:!text-foreground">
                                    <Icon className="size-4" />
                                    {tool.title}
                                </Link>
                            );
                        })}
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
                            {tools.map((tool) => {
                                const Icon = toolIcons[tool.key];
                                return <CoverCard key={tool.key} href={tool.href} cover={toolExamples(tool.key, examples).cover} tone={tool.tone} icon={<Icon className="size-20" strokeWidth={1} />} title={tool.title} summary={tool.summary} />;
                            })}
                        </div>
                    </section>
                )}

                {templateSections.map((section) => (
                    <TemplateSection key={section.category} {...section} keyword={search} />
                ))}
            </div>
        </div>
    );
}
