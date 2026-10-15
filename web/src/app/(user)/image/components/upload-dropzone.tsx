"use client";

import { useQuery } from "@tanstack/react-query";
import { App, Button, Image, Spin } from "antd";
import { CloudUpload, FolderOpen, Plus, X } from "lucide-react";
import { useRef, useState, type DragEvent } from "react";

import { AssetPickerModal, type InsertAssetPayload } from "@/app/(user)/canvas/components/asset-picker-modal";
import { unsignedFileId } from "@/lib/file-url";
import { cn } from "@/lib/utils";
import { ensureServerFileId } from "@/services/api/jobs";
import { signFileUrls } from "@/services/api/storage";
import { useUserStore } from "@/stores/use-user-store";

export type PickedImage = { fileId: string; url: string };

const MAX_BYTES = 10 * 1024 * 1024;
const ACCEPTED_TYPES = ["image/jpeg", "image/png"];

export const pickedImageUrl = (fileId: string) => `/api/files/${encodeURIComponent(fileId)}/content`;

/** Returns why an image can't be used (R-SET-1: JPG/PNG ≤10MB), or "" when it can; unknown type/size passes (the server re-checks). */
function imageProblem(name: string, type?: string, bytes?: number) {
    if (type && !ACCEPTED_TYPES.includes(type)) return `${name} 不是 JPG/PNG 图片`;
    if (bytes && bytes > MAX_BYTES) return `${name} 超过 10MB`;
    return "";
}

const tile = "flex size-[72px] shrink-0 items-center justify-center rounded-lg border border-dashed border-border text-muted-foreground transition hover:border-foreground/40 hover:text-foreground";

/**
 * Drag-drop, upload or pick from the asset library up to `max` JPG/PNG images (≤10MB) as server file IDs.
 * Picked images show as 72px thumbnails inside the zone; `compact` skips the large empty prompt.
 */
export function UploadDropzone({ label, max, value, onChange, compact }: { label?: string; max: number; value: PickedImage[]; onChange: (images: PickedImage[]) => void; compact?: boolean }) {
    const { message } = App.useApp();
    const inputRef = useRef<HTMLInputElement>(null);
    const [busy, setBusy] = useState(false);
    const [dragging, setDragging] = useState(false);
    const [pickerOpen, setPickerOpen] = useState(false);
    const full = value.length >= max;
    // Values keep the plain /api/files/<id>/content URL (what gets saved); thumbnails show it signed.
    const token = useUserStore((state) => state.token);
    const unsignedIds = [...new Set(value.map((image) => unsignedFileId(image.url)).filter(Boolean))];
    const signed = useQuery({ queryKey: ["file-urls", token, unsignedIds], queryFn: () => signFileUrls(token, unsignedIds), enabled: Boolean(token && unsignedIds.length), staleTime: 60 * 60 * 1000 }).data;

    const add = async (sources: Array<{ source: string | Blob; storageKey?: string }>) => {
        const room = max - value.length;
        if (room <= 0) {
            message.warning(`${label || "图片"}最多 ${max} 张`);
            return;
        }
        setBusy(true);
        try {
            const ids = await Promise.all(sources.slice(0, room).map((item) => ensureServerFileId(item.source, item.storageKey)));
            onChange([...value, ...ids.map((fileId) => ({ fileId, url: pickedImageUrl(fileId) }))]);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "图片上传失败");
        } finally {
            setBusy(false);
        }
    };

    const addFiles = (files: FileList | null) => {
        const accepted = Array.from(files || []).filter((file) => {
            const problem = imageProblem(file.name, file.type || "unknown", file.size);
            if (problem) message.error(problem);
            return !problem;
        });
        if (accepted.length) void add(accepted.map((file) => ({ source: file })));
    };

    const addAsset = (payload: InsertAssetPayload) => {
        setPickerOpen(false);
        if (payload.kind !== "image") {
            message.error("请选择图片素材");
            return;
        }
        const problem = imageProblem(payload.title, payload.mimeType, payload.bytes);
        if (problem) {
            message.error(problem);
            return;
        }
        void add([{ source: payload.dataUrl, storageKey: payload.storageKey }]);
    };

    const openFiles = () => !busy && inputRef.current?.click();
    const drop = (event: DragEvent) => {
        event.preventDefault();
        setDragging(false);
        if (!busy && !full) addFiles(event.dataTransfer.files);
    };

    return (
        <div>
            {label && (
                <div className="mb-2 flex items-center justify-between text-sm font-medium">
                    <span>{label}</span>
                    <span className="text-xs font-normal tabular-nums text-muted-foreground">
                        {value.length}/{max}
                    </span>
                </div>
            )}
            <div
                className={cn("relative rounded-[18px] border-2 border-dashed border-border transition", dragging && !full && "border-foreground/40 bg-muted/60")}
                onDragOver={(event) => {
                    event.preventDefault();
                    setDragging(true);
                }}
                onDragLeave={() => setDragging(false)}
                onDrop={drop}
            >
                {!value.length && !compact ? (
                    <div className="flex cursor-pointer flex-col items-center px-4 py-6 text-center" onClick={openFiles}>
                        <CloudUpload className="mb-2 size-7 text-muted-foreground" strokeWidth={1.5} />
                        <div className="text-sm font-medium">点击或拖拽上传图片</div>
                        <div className="mt-1 text-xs text-muted-foreground">支持 JPG、PNG，单张不超过 10MB{max > 1 ? `，最多 ${max} 张` : ""}</div>
                        <div className="mt-4 flex gap-2" onClick={(event) => event.stopPropagation()}>
                            <Button className="!rounded-lg" icon={<Plus className="size-3.5" />} loading={busy} onClick={openFiles}>
                                选择文件
                            </Button>
                            <Button className="!rounded-lg" icon={<FolderOpen className="size-3.5" />} disabled={busy} onClick={() => setPickerOpen(true)}>
                                从资产库选择
                            </Button>
                        </div>
                    </div>
                ) : (
                    <div className="flex flex-wrap gap-2 p-3">
                        {value.map((image, index) => (
                            <div key={image.fileId} className="group relative size-[72px] shrink-0 overflow-hidden rounded-lg border border-border bg-muted">
                                <Image src={signed?.[unsignedFileId(image.url)] ?? image.url} alt={`${label || "图片"} ${index + 1}`} rootClassName="!block !h-full !w-full" className="!h-full !w-full object-cover" />
                                <button
                                    type="button"
                                    aria-label="移除图片"
                                    className="absolute right-1 top-1 z-10 flex size-5 items-center justify-center rounded-full bg-black/60 text-white opacity-90 transition hover:bg-black/80"
                                    onClick={() => onChange(value.filter((item) => item.fileId !== image.fileId))}
                                >
                                    <X className="size-3" />
                                </button>
                            </div>
                        ))}
                        {!full && (
                            <>
                                <button type="button" className={tile} title="选择文件" aria-label="选择文件" disabled={busy} onClick={openFiles}>
                                    {busy ? <Spin size="small" /> : <Plus className="size-5" />}
                                </button>
                                <button type="button" className={tile} title="从资产库选择" aria-label="从资产库选择" disabled={busy} onClick={() => setPickerOpen(true)}>
                                    <FolderOpen className="size-5" />
                                </button>
                            </>
                        )}
                    </div>
                )}
            </div>
            <input
                ref={inputRef}
                type="file"
                accept="image/png,image/jpeg"
                multiple={max > 1}
                hidden
                onChange={(event) => {
                    addFiles(event.target.files);
                    event.target.value = "";
                }}
            />
            <AssetPickerModal open={pickerOpen} defaultTab="my-assets" onInsert={addAsset} onClose={() => setPickerOpen(false)} />
        </div>
    );
}
