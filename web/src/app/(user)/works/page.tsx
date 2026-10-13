"use client";

import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Checkbox, Empty, Image, Result, Segmented, Spin, Tag } from "antd";
import { saveAs } from "file-saver";
import { Download, RotateCcw, Trash2, X } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { retentionDaysLeft, workStatusGroup } from "@/lib/works";
import { createZip } from "@/lib/zip";
import { cancelGenerationJob, deleteWork, fetchWorks, retryGenerationJob, type GenerationJob, type WorksKind, type WorksStatus } from "@/services/api/jobs";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

const kindOptions: { label: string; value: WorksKind | "" }[] = [
    { label: "全部", value: "" },
    { label: "图片", value: "image" },
    { label: "套图", value: "set" },
    { label: "应用", value: "tool" },
];

const statusText: Record<GenerationJob["status"], string> = { queued: "排队中", running: "生成中", succeeded: "已完成", failed: "失败", cancelled: "已取消" };
const statusColor: Record<GenerationJob["status"], string> = { queued: "default", running: "processing", succeeded: "success", failed: "error", cancelled: "default" };

function imageExtension(type: string) {
    if (type === "image/jpeg") return "jpg";
    if (type === "image/webp") return "webp";
    return "png";
}

export default function WorksPage() {
    const { message, modal } = App.useApp();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const works = useConfigStore((state) => state.publicSettings?.works);
    const [kind, setKind] = useState<WorksKind | "">("");
    const [status, setStatus] = useState<WorksStatus | "">("");
    const [selected, setSelected] = useState<string[]>([]);
    const [busy, setBusy] = useState(false);
    const query = useInfiniteQuery({
        queryKey: ["works", token, kind, status],
        queryFn: ({ pageParam }) => fetchWorks(token!, { cursor: pageParam, kind, status }),
        initialPageParam: "",
        getNextPageParam: (last) => last.nextCursor || undefined,
        enabled: Boolean(token),
        refetchInterval: (current) => (current.state.data?.pages.some((page) => page.items.some((item) => workStatusGroup(item.status) === "running")) ? 5000 : false),
    });
    const items = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data]);
    const counts = query.data?.pages[0]?.counts;

    if (!token) return <Result status="info" title="请先登录" extra={<Link href="/login?redirect=/works">去登录</Link>} />;

    const run = async (action: () => Promise<unknown>, success: string) => {
        setBusy(true);
        try {
            await action();
            message.success(success);
            await queryClient.invalidateQueries({ queryKey: ["works"] });
        } catch (error) {
            message.error(error instanceof Error ? error.message : "操作失败");
        } finally {
            setBusy(false);
        }
    };

    const download = (targets: GenerationJob[]) =>
        run(async () => {
            const ready = targets.filter((item) => item.outputUrl);
            if (!ready.length) throw new Error("所选作品还没有可下载的图片");
            const files = await Promise.all(
                ready.map(async (item, index) => {
                    const response = await fetch(item.outputUrl);
                    if (!response.ok) throw new Error(`下载失败（${response.status}）`);
                    const blob = await response.blob();
                    return { name: `work-${index + 1}.${imageExtension(blob.type)}`, data: blob };
                }),
            );
            if (files.length === 1) saveAs(files[0].data, files[0].name);
            else saveAs(await createZip(files), `works-${Date.now()}.zip`);
        }, "已开始下载");

    const remove = (ids: string[]) =>
        modal.confirm({
            title: `删除 ${ids.length} 个作品`,
            content: "删除后图片文件会被清除，无法恢复。",
            okText: "删除",
            okButtonProps: { danger: true },
            onOk: () =>
                run(async () => {
                    await Promise.all(ids.map((id) => deleteWork(token, id)));
                    setSelected((value) => value.filter((id) => !ids.includes(id)));
                }, "已删除"),
        });

    const toggle = (id: string, checked: boolean) => setSelected((value) => (checked ? [...value, id] : value.filter((item) => item !== id)));

    return (
        <main className="h-full overflow-y-auto px-6 py-6">
            <div className="mx-auto max-w-7xl space-y-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <h1 className="text-xl font-semibold">我的作品</h1>
                    <Segmented value={kind} options={kindOptions} onChange={(value) => { setKind(value as WorksKind | ""); setSelected([]); }} />
                </div>
                <div className="text-sm text-stone-500">
                    作品在服务器保存 {works?.retentionDays ?? 7} 天（会员 {works?.memberRetentionDays ?? 90} 天），到期自动删除，请及时下载。失败的生成不扣积分。
                </div>
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <Segmented
                        value={status}
                        onChange={(value) => { setStatus(value as WorksStatus | ""); setSelected([]); }}
                        options={[
                            { label: "全部", value: "" },
                            { label: `已完成 ${counts?.succeeded ?? 0}`, value: "succeeded" },
                            { label: `生成中 ${counts?.running ?? 0}`, value: "running" },
                            { label: `失败 ${counts?.failed ?? 0}`, value: "failed" },
                        ]}
                    />
                    <div className="flex items-center gap-2">
                        <Checkbox
                            checked={items.length > 0 && selected.length === items.length}
                            indeterminate={selected.length > 0 && selected.length < items.length}
                            onChange={(event) => setSelected(event.target.checked ? items.map((item) => item.id) : [])}
                        >
                            全选
                        </Checkbox>
                        <Button icon={<Download className="size-4" />} disabled={!selected.length} loading={busy} onClick={() => download(items.filter((item) => selected.includes(item.id)))}>
                            批量下载
                        </Button>
                        <Button danger icon={<Trash2 className="size-4" />} disabled={!selected.length} onClick={() => remove(selected)}>
                            批量删除
                        </Button>
                    </div>
                </div>
                {query.isLoading ? (
                    <div className="flex justify-center py-20"><Spin /></div>
                ) : !items.length ? (
                    <Empty className="py-20" description="还没有作品，去图片制作生成第一张吧" />
                ) : (
                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
                        {items.map((item) => {
                            const group = workStatusGroup(item.status);
                            const daysLeft = retentionDaysLeft(item.expiresAt);
                            return (
                                <div key={item.id} className="overflow-hidden rounded-lg border border-stone-200 dark:border-stone-800">
                                    <div className="relative aspect-square bg-stone-100 dark:bg-stone-900">
                                        <Checkbox className="!absolute left-2 top-2 z-10" checked={selected.includes(item.id)} onChange={(event) => toggle(item.id, event.target.checked)} aria-label="选择作品" />
                                        {item.outputUrl ? (
                                            <Image src={item.outputUrl} alt={item.prompt || "作品"} rootClassName="!block h-full w-full" className="!h-full !w-full object-cover" />
                                        ) : (
                                            <div className="flex h-full items-center justify-center text-sm text-stone-500">{group === "running" ? <Spin /> : statusText[item.status]}</div>
                                        )}
                                    </div>
                                    <div className="space-y-1 p-2 text-xs">
                                        <div className="line-clamp-2 min-h-8" title={item.prompt}>{item.prompt || "无提示词"}</div>
                                        {group === "failed" && item.error && <div className="line-clamp-2 text-red-500" title={item.error}>{item.error}</div>}
                                        <div className="flex items-center justify-between">
                                            <Tag color={statusColor[item.status]}>{statusText[item.status]}</Tag>
                                            <span className={daysLeft <= 2 ? "text-red-500" : "text-stone-500"}>{daysLeft} 天后过期</span>
                                        </div>
                                        <div className="flex gap-1">
                                            {item.outputUrl && <Button size="small" icon={<Download className="size-3" />} onClick={() => download([item])} aria-label="下载" />}
                                            {group === "failed" && <Button size="small" icon={<RotateCcw className="size-3" />} onClick={() => run(() => retryGenerationJob(token, item.id), "已重新排队")} aria-label="重试" />}
                                            {group === "running" && <Button size="small" icon={<X className="size-3" />} onClick={() => run(() => cancelGenerationJob(token, item.id), "已取消")} aria-label="取消" />}
                                            <Button size="small" danger icon={<Trash2 className="size-3" />} onClick={() => remove([item.id])} aria-label="删除" />
                                        </div>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}
                {query.hasNextPage && (
                    <div className="flex justify-center">
                        <Button loading={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多</Button>
                    </div>
                )}
            </div>
        </main>
    );
}
