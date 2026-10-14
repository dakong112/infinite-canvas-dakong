"use client";

import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { Popover } from "radix-ui";
import { ChevronDown, Cpu, Layers, Plus, Search } from "lucide-react";

import { useAutoDLWorkflowNames } from "@/hooks/use-autodl-workflow";
import { catalogPriceLabel, catalogVendors, formatRatio, groupRatio, isPlatformChannelId, platformGroupOf, ratioTone } from "@/lib/model-catalog";
import { isWorkflowProtocol } from "@/lib/model-channel";
import { cn } from "@/lib/utils";
import type { WorkflowRef, WorkflowSummary } from "@/lib/workflow-channel";
import { useAccountStore } from "@/stores/use-account-store";
import { filterModelsByCapability, normalizeLocalChannels, useConfigStore, useModelCatalog, type AiConfig, type ModelCapabilities, type ModelCapability } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

type ModelPickerProps = {
    config: AiConfig;
    value?: string;
    channelId?: string;
    capability?: ModelCapability;
    onChange: (model: string, channelId?: string) => void;
    workflowRef?: WorkflowRef;
    onWorkflowChange?: (ref?: WorkflowRef) => void;
    className?: string;
    fullWidth?: boolean;
    placeholder?: string;
    onMissingConfig?: () => void;
};

type PickerBase = { key: string; channelId?: string; channelName: string; protocol?: string; baseUrl?: string };
type PickerOption = (PickerBase & { model: string }) | (PickerBase & { model: ""; workflowRef: WorkflowRef; label: string });
type PickerSection = "platform" | "own" | "remote";
type PickerChannel = { id: string; name: string; section: PickerSection; options: PickerOption[] };
type SourceChannel = { id: string; protocol: string; name: string; baseUrl?: string; models: string[]; modelCapabilities?: ModelCapabilities; workflows: WorkflowSummary[]; own: boolean };

const sections: Array<{ key: PickerSection; title: string }> = [
    { key: "platform", title: "平台渠道" },
    { key: "own", title: "我的渠道" },
    { key: "remote", title: "云端渠道" },
];

// antd's reset.css sets `button { color: inherit }` outside any layer, so colours on buttons need `!`.
const ratioToneClass = {
    low: "bg-emerald-500/15 !text-emerald-600 dark:!text-emerald-400",
    normal: "bg-muted !text-muted-foreground",
    high: "bg-orange-500/15 !text-orange-600 dark:!text-orange-400",
};

const workflowKey = (ref: WorkflowRef) => `workflow:${JSON.stringify([ref.scope, ref.channelId, ref.kind, ref.workflowId])}`;

export function ModelPicker({ config, value, channelId, capability, onChange, workflowRef, onWorkflowChange, className, fullWidth = false, placeholder = "选择模型", onMissingConfig }: ModelPickerProps) {
    const pickerId = useId();
    const contentRef = useRef<HTMLDivElement>(null);
    const [open, setOpen] = useState(false);
    const [activeId, setActiveId] = useState("");
    const [search, setSearch] = useState("");
    const [vendorId, setVendorId] = useState<number | null>(null);
    const token = useUserStore((state) => state.token);
    const userReady = useUserStore((state) => state.isReady);
    const publicSettings = useConfigStore((state) => state.publicSettings);
    const openConfigDialog = useConfigStore((state) => state.openConfigDialog);
    const creditName = useAccountStore((state) => state.summary?.creditName || "积分");
    const catalog = useModelCatalog();
    const workflowEnabled = Boolean(onWorkflowChange && capability && capability !== "text");
    const channels = useMemo<PickerChannel[]>(() => {
        const remote = config.channelMode === "remote";
        const source: SourceChannel[] = remote
            ? config.publicChannels.map((channel) => ({ id: channel.id || "", protocol: channel.protocol || "", name: channel.name || "云端渠道", baseUrl: channel.baseUrl, models: channel.models || [], modelCapabilities: channel.modelCapabilities, workflows: channel.workflows || [], own: Boolean(channel.local) }))
            : normalizeLocalChannels(config).map((channel) => ({ id: channel.id, protocol: channel.protocol, name: channel.name || "本地渠道", baseUrl: channel.baseUrl, models: channel.models, modelCapabilities: channel.modelCapabilities, workflows: channel.workflowSummaries || [], own: true }));
        const scope = remote ? "system" : "personal";
        return source
            .map((channel) => {
                const base = { channelId: channel.id, channelName: channel.name, protocol: channel.protocol, baseUrl: channel.baseUrl };
                const options: PickerOption[] = isWorkflowProtocol(channel.protocol)
                    ? workflowEnabled && token
                        ? channel.workflows
                              .filter((entry) => entry.enabled && entry.capability === capability && entry.provider === channel.protocol)
                              .map((entry) => {
                                  const ref: WorkflowRef = { scope, channelId: channel.id, kind: entry.kind, workflowId: entry.workflowId };
                                  return { ...base, key: workflowKey(ref), model: "" as const, workflowRef: ref, label: entry.title || entry.workflowId };
                              })
                        : []
                    : filterModelsByCapability(channel.models, capability, channel.protocol, channel.modelCapabilities).map((model) => ({ ...base, key: `${channel.id}::${model}`, model }));
                const section: PickerSection = isPlatformChannelId(channel.id) ? "platform" : channel.own ? "own" : "remote";
                return { id: channel.id, name: channel.name, section, options };
            })
            .filter((channel) => channel.section === "platform" || channel.options.length);
    }, [capability, config, token, workflowEnabled]);
    const options = useMemo(() => channels.flatMap((channel) => channel.options), [channels]);
    const modelLabel = useAutoDLWorkflowNames(options);
    const currentOption = useMemo<PickerOption | undefined>(() => {
        if (workflowRef && workflowEnabled) return options.find((item) => "workflowRef" in item && item.key === workflowKey(workflowRef));
        if (!value) return undefined;
        return options.find((item) => item.model === value && item.channelId === channelId) || options.find((item) => item.model === value);
    }, [channelId, options, value, workflowEnabled, workflowRef]);
    const current = workflowRef && workflowEnabled ? (currentOption && "label" in currentOption ? currentOption.label : "") : value || "";

    useEffect(() => {
        if (workflowRef && workflowEnabled) {
            const workflowOptionsReady = workflowRef.scope === "system" ? publicSettings !== null : config.workflowSyncTouched === true;
            if (token && userReady && workflowOptionsReady && !currentOption && options.some((item) => !("workflowRef" in item))) onWorkflowChange?.(undefined);
            return;
        }
        if (value && currentOption?.channelId && !("workflowRef" in currentOption) && channelId !== currentOption.channelId) onChange(value, currentOption.channelId);
    }, [channelId, options, config.workflowSyncTouched, currentOption, onChange, onWorkflowChange, publicSettings, token, userReady, value, workflowEnabled, workflowRef]);

    useEffect(() => {
        const closeOtherPicker = (event: Event) => {
            if ((event as CustomEvent<string>).detail !== pickerId) setOpen(false);
        };
        window.addEventListener("model-picker-open", closeOtherPicker);
        return () => window.removeEventListener("model-picker-open", closeOtherPicker);
    }, [pickerId]);

    // Centre the current channel and model in their columns; scrollIntoView would also scroll the canvas/page.
    useEffect(() => {
        if (!open) return;
        const frame = requestAnimationFrame(() =>
            contentRef.current?.querySelectorAll<HTMLElement>("[data-current]").forEach((element) => {
                const box = element.closest<HTMLElement>("[data-scroll]");
                if (box) box.scrollTop = element.offsetTop - (box.clientHeight - element.offsetHeight) / 2;
            }),
        );
        return () => cancelAnimationFrame(frame);
    }, [open]);

    const activeChannel = channels.find((channel) => channel.id === activeId) || channels[0];
    const platformActive = activeChannel?.section === "platform";
    const activeGroup = platformGroupOf(activeChannel?.id);
    const vendors = catalog && platformActive ? catalogVendors(catalog, activeChannel.options.map((option) => option.model)) : [];
    const keyword = search.trim().toLowerCase();
    const vendorOf = (model: string) => (catalog ? catalog.vendors.find((vendor) => vendor.id === catalog.models.find((item) => item.name === model)?.vendorId)?.name || "其他" : "");
    const rows = (activeChannel?.options || []).filter((option) => {
        const label = "workflowRef" in option ? option.label : modelLabel(option.model, option);
        if (keyword && !`${label} ${option.model}`.toLowerCase().includes(keyword)) return false;
        return vendorId === null || !platformActive || catalog?.models.find((model) => model.name === option.model)?.vendorId === vendorId;
    });
    const defaultModel = capability && catalog ? catalog.platform.defaults[capability] : "";
    const triggerGroup = platformGroupOf(currentOption?.channelId);
    const showAddChannel = config.channelMode === "local" || publicSettings?.modelChannel?.allowCustomChannel === true;
    const emptyText = !channels.length ? (config.channelMode === "remote" ? "暂无可用模型" : "请先到配置里拉取模型列表") : keyword || vendorId !== null ? "没有匹配的模型" : "该渠道暂无此类模型";

    const handleOpenChange = (nextOpen: boolean) => {
        if (nextOpen && !options.length && config.channelMode === "local") {
            onMissingConfig?.();
            return;
        }
        if (nextOpen) {
            window.dispatchEvent(new CustomEvent("model-picker-open", { detail: pickerId }));
            setActiveId(currentOption?.channelId || channelId || channels.find((channel) => channel.options.length)?.id || "");
            setSearch("");
            setVendorId(null);
        }
        setOpen(nextOpen);
    };
    const select = (option: PickerOption) => {
        if ("workflowRef" in option) onWorkflowChange?.(option.workflowRef);
        else {
            onChange(option.model, option.channelId);
            if (workflowRef) onWorkflowChange?.(undefined);
        }
        setOpen(false);
    };
    const channelButton = (channel: PickerChannel, label: ReactNode, trailing: ReactNode, title?: string) => {
        const active = channel.id === activeChannel?.id;
        return (
            <button
                key={channel.id}
                type="button"
                data-current={active || undefined}
                onClick={() => {
                    setActiveId(channel.id);
                    setVendorId(null);
                }}
                title={title || channel.name}
                aria-pressed={active}
                className={cn("flex w-full items-center gap-1.5 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent/70", active ? "bg-accent font-medium !text-accent-foreground" : !channel.options.length && "opacity-50")}
            >
                <span className="min-w-0 flex-1 truncate">{label}</span>
                {trailing}
            </button>
        );
    };

    return (
        <Popover.Root open={open} onOpenChange={handleOpenChange}>
            <Popover.Trigger asChild>
                <button
                    type="button"
                    className={cn(
                        "canvas-composer-model-picker flex h-8 w-fit max-w-full items-center gap-2 rounded-full border border-input bg-transparent px-3 text-sm font-normal whitespace-nowrap shadow-sm transition-colors outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 dark:hover:bg-input/50",
                        fullWidth ? "w-full min-w-0 justify-start" : "min-w-[9rem] justify-start",
                        "data-[state=open]:border-ring data-[state=open]:ring-2 data-[state=open]:ring-ring/20",
                        className,
                    )}
                    onMouseDown={(event) => event.stopPropagation()}
                    onPointerDown={(event) => event.stopPropagation()}
                    title={workflowRef && workflowEnabled && !currentOption ? "工作流已停用、未公开或删除，请重新选择" : [current || placeholder, triggerGroup && `分组 ${triggerGroup}`].filter(Boolean).join(" · ")}
                >
                    <ModelIcon model={current} />
                    <span className="canvas-model-picker-text min-w-0 flex-1 truncate text-left">{workflowRef && workflowEnabled ? current || "工作流已停用、未公开或删除，请重新选择" : modelLabel(current, currentOption) || placeholder}</span>
                    {/* canvas-model-picker-text: hidden with the name when the canvas composer collapses the picker to an icon */}
                    {catalog && triggerGroup ? <RatioBadge ratio={groupRatio(catalog, triggerGroup)} label={triggerGroup} className="canvas-model-picker-text max-w-28 truncate" /> : null}
                    <ChevronDown className="canvas-select-chevron size-4 shrink-0 text-muted-foreground" />
                </button>
            </Popover.Trigger>
            <Popover.Portal>
                <Popover.Content
                    ref={contentRef}
                    data-canvas-no-zoom
                    side="bottom"
                    align="start"
                    sideOffset={6}
                    collisionPadding={12}
                    className="z-[1200] flex h-[min(420px,var(--radix-popover-content-available-height))] w-[560px] max-w-[calc(100vw-24px)] flex-col overflow-hidden rounded-xl border border-border/70 bg-popover text-sm text-popover-foreground shadow-xl outline-none"
                    onPointerDown={(event) => event.stopPropagation()}
                    onMouseDown={(event) => event.stopPropagation()}
                    onWheel={(event) => event.stopPropagation()}
                >
                    <div className="space-y-2 border-b border-border/60 p-2">
                        <label className="flex h-8 items-center gap-2 rounded-lg border border-input bg-muted/40 px-2.5 transition-colors focus-within:border-ring focus-within:bg-transparent">
                            <Search className="size-4 shrink-0 text-muted-foreground" />
                            <input
                                autoFocus
                                value={search}
                                onChange={(event) => setSearch(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === "Enter" && !event.nativeEvent.isComposing && rows[0]) {
                                        event.preventDefault();
                                        select(rows[0]);
                                    }
                                }}
                                placeholder="搜索模型，回车选第一个"
                                aria-label="搜索模型"
                                className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
                            />
                        </label>
                        {vendors.length > 1 ? (
                            <div className="flex gap-1 overflow-x-auto text-xs [scrollbar-width:none]">
                                <Chip active={vendorId === null} onClick={() => setVendorId(null)}>
                                    全部
                                </Chip>
                                {vendors.map((vendor) => (
                                    <Chip key={vendor.id} active={vendorId === vendor.id} onClick={() => setVendorId(vendor.id)}>
                                        <ModelIcon model={vendor.name} className="size-3.5" />
                                        {vendor.name}
                                    </Chip>
                                ))}
                            </div>
                        ) : null}
                    </div>
                    <div className="flex min-h-0 flex-1">
                        <div data-scroll className="relative w-[140px] shrink-0 overflow-y-auto border-r border-border/60 bg-muted/30 p-1.5 sm:w-[180px]">
                            {sections.map(({ key, title }) => {
                                const items = channels.filter((channel) => channel.section === key);
                                const addHere = key === "own" && showAddChannel;
                                if (!items.length && !addHere) return null;
                                return (
                                    <div key={key} className="mb-2 last:mb-0">
                                        <div className="px-2 pt-1 pb-1 text-[11px] text-muted-foreground">{title}</div>
                                        {key === "platform" ? (
                                            <>
                                                <div className="flex items-center gap-1.5 px-2 py-1 font-medium" title={catalog?.platform.name}>
                                                    <Layers className="size-3.5 shrink-0 text-muted-foreground" />
                                                    <span className="truncate">{catalog?.platform.name || "平台"}</span>
                                                </div>
                                                <div className="ml-3 space-y-0.5 border-l border-dashed border-border pl-1.5 text-[13px]">
                                                    {items.map((channel) => {
                                                        const group = catalog?.groups.find((item) => item.name === platformGroupOf(channel.id));
                                                        return channelButton(channel, group?.name || channel.name, group ? <RatioBadge ratio={group.ratio} /> : null, group?.desc || undefined);
                                                    })}
                                                </div>
                                            </>
                                        ) : (
                                            <div className="space-y-0.5">
                                                {items.map((channel) => channelButton(channel, channel.name, <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{channel.options.length}</span>))}
                                                {addHere ? (
                                                    <button
                                                        type="button"
                                                        onClick={() => {
                                                            setOpen(false);
                                                            openConfigDialog();
                                                        }}
                                                        className="flex w-full items-center gap-1.5 rounded-lg px-2 py-1.5 text-left !text-muted-foreground transition-colors hover:bg-accent/70 hover:!text-foreground"
                                                    >
                                                        <Plus className="size-3.5 shrink-0" />
                                                        <span className="truncate">添加渠道</span>
                                                    </button>
                                                ) : null}
                                            </div>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                        <div data-scroll className="relative min-w-0 flex-1 space-y-0.5 overflow-y-auto p-1.5" role="listbox" aria-label="模型">
                            {rows.length ? (
                                rows.map((option) => {
                                    const workflow = "workflowRef" in option;
                                    const label = workflow ? `工作流 · ${option.label}` : modelLabel(option.model, option);
                                    const vendorName = platformActive && option.model ? vendorOf(option.model) : option.channelName;
                                    const price = platformActive && catalog && option.model ? catalogPriceLabel(catalog, option.model, activeGroup, creditName) : null;
                                    const selected = currentOption?.key === option.key;
                                    return (
                                        <button
                                            key={option.key}
                                            type="button"
                                            role="option"
                                            aria-selected={selected}
                                            data-current={selected || undefined}
                                            onClick={() => select(option)}
                                            className={cn("flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent/70", selected && "bg-accent ring-1 ring-border ring-inset")}
                                        >
                                            <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted">
                                                <ModelIcon model={workflow ? option.workflowRef.workflowId : platformActive ? `${option.model} ${vendorName}` : option.model} />
                                            </span>
                                            <span className="min-w-0 flex-1">
                                                <span className="flex min-w-0 items-center gap-1">
                                                    <span className="truncate font-medium" title={option.model || label}>
                                                        {label}
                                                    </span>
                                                    {platformActive && option.model && option.model === defaultModel ? <span className="shrink-0 rounded bg-primary/10 px-1 text-[10px] leading-4 text-primary">默认</span> : null}
                                                </span>
                                                <span className="block truncate text-[11px] text-muted-foreground">{vendorName}</span>
                                            </span>
                                            {price ? (
                                                <span className="shrink-0 text-right tabular-nums">
                                                    <span className={cn("block text-xs", price.main.startsWith("≈") ? "font-medium" : "text-muted-foreground")}>{price.main}</span>
                                                    {price.base ? <span className="block text-[10px] text-muted-foreground/80">{price.base}</span> : null}
                                                </span>
                                            ) : null}
                                        </button>
                                    );
                                })
                            ) : (
                                <div className="px-2 py-10 text-center text-xs text-muted-foreground">{emptyText}</div>
                            )}
                        </div>
                    </div>
                </Popover.Content>
            </Popover.Portal>
        </Popover.Root>
    );
}

function RatioBadge({ ratio, label, className }: { ratio: number; label?: string; className?: string }) {
    return <span className={cn("inline-block shrink-0 rounded px-1 text-[10px] leading-4 tabular-nums", ratioToneClass[ratioTone(ratio)], className)}>{label ? `${label} ${formatRatio(ratio)}` : formatRatio(ratio)}</span>;
}

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
    return (
        <button type="button" onClick={onClick} aria-pressed={active} className={cn("flex h-6 shrink-0 items-center gap-1 rounded-full border px-2.5 transition-colors", active ? "border-foreground bg-foreground !text-background" : "border-border !text-muted-foreground hover:bg-accent/70 hover:!text-foreground")}>
            {children}
        </button>
    );
}

function ModelIcon({ model, className }: { model: string; className?: string }) {
    const icon = resolveModelIcon(model);
    return icon ? <img src={icon} alt="" className={cn("size-4 shrink-0 dark:invert", className)} /> : <Cpu className={cn("size-4 shrink-0 opacity-70", className)} />;
}

function resolveModelIcon(model: string) {
    const name = model.toLowerCase();
    if (name.includes("claude") || name.includes("anthropic")) return "/icons/claude.svg";
    if (name.includes("gemini") || name.includes("google")) return "/icons/gemini.svg";
    if (name.includes("gpt") || name.includes("openai")) return "/icons/openai.svg";
    if (name.includes("grok")) return "/icons/grok.svg";
    if (name.includes("deepseek")) return "/icons/deepseek.svg";
    if (name.includes("glm") || name.includes("zhipu")) return "/icons/glm.svg";
    return "";
}
