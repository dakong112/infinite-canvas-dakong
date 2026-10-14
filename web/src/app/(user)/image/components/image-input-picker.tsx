"use client";

import { App, Button, Image } from "antd";
import { FolderOpen, Plus, X } from "lucide-react";
import { useRef, useState } from "react";

import { AssetPickerModal, type InsertAssetPayload } from "@/app/(user)/canvas/components/asset-picker-modal";
import { ensureServerFileId } from "@/services/api/jobs";

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

/** Upload, or pick from the asset library, up to `max` JPG/PNG images (≤10MB) as server file IDs. */
export function ImageInputPicker({ label, max, value, onChange }: { label: string; max: number; value: PickedImage[]; onChange: (images: PickedImage[]) => void }) {
    const { message } = App.useApp();
    const inputRef = useRef<HTMLInputElement>(null);
    const [busy, setBusy] = useState(false);
    const [pickerOpen, setPickerOpen] = useState(false);

    const add = async (sources: Array<{ source: string | Blob; storageKey?: string }>) => {
        const room = max - value.length;
        if (room <= 0) {
            message.warning(`${label}最多 ${max} 张`);
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

    return (
        <div>
            <div className="mb-2 text-sm font-medium">
                {label}
                <span className="ml-1 text-xs font-normal text-stone-500">
                    （{value.length}/{max}，JPG/PNG ≤10MB）
                </span>
            </div>
            <div className="flex flex-wrap gap-2">
                {value.map((image, index) => (
                    <div key={image.fileId} className="relative size-20 shrink-0 overflow-hidden rounded-md border border-stone-200 dark:border-stone-800">
                        <Image src={image.url} alt={`${label} ${index + 1}`} className="size-full object-cover" style={{ width: "100%", height: "100%", objectFit: "cover" }} />
                        <button
                            type="button"
                            aria-label="移除图片"
                            className="absolute right-1 top-1 z-10 flex size-5 items-center justify-center rounded bg-black/60 text-white"
                            onClick={() => onChange(value.filter((item) => item.fileId !== image.fileId))}
                        >
                            <X className="size-3" />
                        </button>
                    </div>
                ))}
                {value.length < max && (
                    <div className="flex flex-col gap-1">
                        <Button size="small" icon={<Plus className="size-3" />} loading={busy} onClick={() => inputRef.current?.click()}>
                            上传
                        </Button>
                        <Button size="small" icon={<FolderOpen className="size-3" />} disabled={busy} onClick={() => setPickerOpen(true)}>
                            资产库
                        </Button>
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
