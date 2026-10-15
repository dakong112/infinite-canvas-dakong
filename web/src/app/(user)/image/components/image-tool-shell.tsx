"use client";

import { Button, Image, Tooltip, type ButtonProps } from "antd";
import { ChevronLeft, Copy, Eraser, Images, Minus, PanelsTopLeft, Plus, Shirt, Sparkles, WandSparkles, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";

import { imageTool, toolExamples, type ImageToolKey } from "@/lib/image-tool-examples";
import { catalogTotal, type TypicalUsage } from "@/lib/model-catalog";
import { cn } from "@/lib/utils";
import { useAccountStore } from "@/stores/use-account-store";
import { useConfigStore, useModelCatalog } from "@/stores/use-config-store";

export const toolIcons: Record<ImageToolKey, LucideIcon> = { set: Images, detail: PanelsTopLeft, replicate: Copy, "remove-bg": Eraser, tryon: Shirt, free: WandSparkles };

/** Prices image-tool steps in the account group (where e-commerce jobs bill); null outside SSO or when a step can't be priced. */
export function useImagePrice() {
    const catalog = useModelCatalog();
    return (steps: Array<{ model: string; count?: number; usage?: TypicalUsage }>) => (catalog ? catalogTotal(catalog, catalog.userGroup, steps) : null);
}

/** "≈3.9 积分"; renders nothing when there is no estimate. */
export function PriceText({ credits, prefix, className }: { credits: number | null; prefix?: string; className?: string }) {
    const creditName = useAccountStore((state) => state.summary?.creditName || "积分");
    if (credits === null) return null;
    return (
        <Tooltip title="预估值，按实际用量结算">
            <span className={cn("ml-1 tabular-nums", className)}>
                {prefix}
                {prefix ? " " : ""}≈{credits > 0 ? credits.toLocaleString() : "<0.01"} {creditName}
            </span>
        </Tooltip>
    );
}

/** Outlined pill for secondary AI actions: "✦ 一键解析 · ≈0.2 积分". */
export function AiPillButton({ credits, children, ...props }: ButtonProps & { credits: number | null }) {
    return (
        <Button shape="round" icon={<Sparkles className="size-3.5" />} {...props}>
            {children}
            <PriceText credits={credits} prefix="·" className="opacity-70" />
        </Button>
    );
}

/** Flat form group: small heading, optional action on the right, no card border. */
export function FormSection({ title, extra, children }: { title: string; extra?: ReactNode; children?: ReactNode }) {
    return (
        <section className="space-y-3">
            <div className="flex min-h-8 items-center justify-between gap-2">
                <h3 className="text-sm font-semibold">{title}</h3>
                {extra}
            </div>
            {children}
        </section>
    );
}

export function Stepper({ value, onChange, min = 0, max }: { value: number; onChange: (value: number) => void; min?: number; max: number }) {
    const button = "flex size-7 items-center justify-center text-muted-foreground transition hover:text-foreground disabled:pointer-events-none disabled:opacity-30";
    return (
        <span className="inline-flex shrink-0 items-center rounded-md border border-border">
            <button type="button" aria-label="减少" className={button} disabled={value <= min} onClick={() => onChange(value - 1)}>
                <Minus className="size-3" />
            </button>
            <span className="w-6 text-center text-xs tabular-nums">{value}</span>
            <button type="button" aria-label="增加" className={button} disabled={value >= max} onClick={() => onChange(value + 1)}>
                <Plus className="size-3" />
            </button>
        </span>
    );
}

export type ShellTab = { key: "result" | "history" | "examples"; label: string; content: ReactNode; extra?: ReactNode; footer?: ReactNode };

/**
 * Tool page frame: back bar with the centered title, a flat 440px form column with the main action pinned
 * at the bottom, and a large result / history / examples panel. Opens on "结果" when there is one, else "示例".
 */
export function ImageToolShell({ title, headerExtra, form, action, hint, tabs }: { title: string; headerExtra?: ReactNode; form: ReactNode; action: ReactNode; hint?: ReactNode; tabs: ShellTab[] }) {
    const hasResult = tabs.some((tab) => tab.key === "result");
    const [picked, setPicked] = useState<ShellTab["key"]>();
    useEffect(() => {
        if (hasResult) setPicked("result");
    }, [hasResult]);
    const current = tabs.find((tab) => tab.key === picked) ?? tabs.find((tab) => tab.key === (hasResult ? "result" : "examples")) ?? tabs[0];

    return (
        <div className="flex h-full min-h-0 flex-col bg-stone-50 text-foreground dark:bg-background">
            <header className="relative flex h-12 shrink-0 items-center justify-between gap-2 border-b border-border px-4">
                <Link href="/image" className="flex items-center gap-1 text-sm !text-muted-foreground transition hover:!text-foreground">
                    <ChevronLeft className="size-4" />
                    返回图片创作
                </Link>
                <h1 className="pointer-events-none absolute left-1/2 max-w-[45%] -translate-x-1/2 truncate text-base font-semibold">{title}</h1>
                <div>{headerExtra}</div>
            </header>
            <div className="thin-scrollbar flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4 lg:flex-row lg:overflow-hidden">
                <aside className="flex shrink-0 flex-col rounded-xl border border-border bg-card lg:min-h-0 lg:w-[380px] xl:w-[440px]">
                    <div className="thin-scrollbar min-h-0 flex-1 space-y-6 px-5 py-5 lg:overflow-y-auto">{form}</div>
                    <div className="sticky bottom-0 z-10 shrink-0 rounded-b-xl border-t border-border bg-card px-5 py-4">
                        {action}
                        {hint && <div className="mt-2 text-center text-xs text-muted-foreground">{hint}</div>}
                    </div>
                </aside>
                <main className="flex min-h-[520px] min-w-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card lg:min-h-0">
                    <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 px-5 pt-4">
                        <div role="tablist" className="inline-flex rounded-lg bg-muted p-1">
                            {tabs.map((tab) => (
                                <button
                                    key={tab.key}
                                    type="button"
                                    role="tab"
                                    aria-selected={tab === current}
                                    className={cn("rounded-md px-4 py-1 text-sm transition", tab === current ? "bg-foreground font-medium !text-background" : "!text-muted-foreground hover:!text-foreground")}
                                    onClick={() => setPicked(tab.key)}
                                >
                                    {tab.label}
                                </button>
                            ))}
                        </div>
                        {current.extra}
                    </div>
                    <div className="thin-scrollbar min-h-0 flex-1 overflow-y-auto p-5">{current.content}</div>
                    {current.footer && <div className="shrink-0 border-t border-border px-5 py-3">{current.footer}</div>}
                </main>
            </div>
        </div>
    );
}

/** What a tool produces: numbered outputs or before / after, from admin-uploaded images or gradient placeholders. */
export function ToolExamples({ tool }: { tool: ImageToolKey }) {
    const setting = useConfigStore((state) => state.publicSettings?.imageExamples);
    const config = imageTool(tool);
    const { items } = toolExamples(tool, setting);
    const compare = config.layout === "compare";
    const Icon = toolIcons[tool];
    return (
        <div className="mx-auto max-w-4xl py-4">
            <div className="mb-8 text-center">
                <h2 className="text-2xl font-bold tracking-tight">{config.headline}</h2>
                <p className="mx-auto mt-2 max-w-xl text-sm leading-6 text-muted-foreground">{config.description}</p>
            </div>
            <Image.PreviewGroup>
                <div className={compare ? "grid grid-cols-2 gap-4" : "grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4"}>
                    {items.map((item, index) => (
                        <div key={`${item.label}-${index}`} className={cn("relative overflow-hidden rounded-xl border border-border bg-linear-to-br", config.tone, compare ? "aspect-[3/4]" : "aspect-square")}>
                            {item.url ? (
                                <Image src={item.url} alt={item.label} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-cover" />
                            ) : (
                                <div className="flex h-full flex-col items-center justify-center gap-2 p-3 text-center text-foreground/45">
                                    <Icon className="size-9" strokeWidth={1.25} />
                                    {!compare && <span className="text-xs">{item.label}</span>}
                                </div>
                            )}
                            <span className="pointer-events-none absolute left-2 top-2 rounded-md bg-black/65 px-2 py-0.5 text-xs font-medium text-white">
                                {compare ? item.label : `${String(index + 1).padStart(2, "0")} ${item.label}`}
                            </span>
                        </div>
                    ))}
                </div>
            </Image.PreviewGroup>
        </div>
    );
}
