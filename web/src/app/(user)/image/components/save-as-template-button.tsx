"use client";

import { useMutation } from "@tanstack/react-query";
import { App, Button } from "antd";
import { LayoutTemplate } from "lucide-react";
import { useState } from "react";

import { TemplateEditor } from "@/app/(admin)/admin/templates/components/template-editor";
import { hasPermission, PERMISSIONS } from "@/lib/admin-permissions";
import { templatePrefill, templateProblem } from "@/lib/image-templates";
import { ensureServerFileId } from "@/services/api/jobs";
import { saveAdminTemplate, type ImageTemplate } from "@/services/api/templates";
import { useUserStore } from "@/stores/use-user-store";
import { pickedImageUrl } from "./upload-dropzone";

type WorkImage = { source: string; storageKey?: string };
/** A finished work: prompt, output and reference images (uploaded to server storage when only local) and pixel size. */
export type SaveAsTemplateWork = { prompt: string; output: WorkImage; references: WorkImage[]; width?: number; height?: number };

/** Admin-only "存为模板": opens a new draft template prefilled from the work; renders nothing for other users. */
export function SaveAsTemplateButton({ work, block }: { work: SaveAsTemplateWork; block?: boolean }) {
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    const isAdmin = useUserStore((state) => hasPermission(state.user, PERMISSIONS.templateWrite));
    const [item, setItem] = useState<Partial<ImageTemplate> | null>(null);
    const [busy, setBusy] = useState(false);
    const save = useMutation({
        mutationFn: (value: Partial<ImageTemplate>) => saveAdminTemplate(token, value),
        onSuccess: () => {
            message.success("已存为草稿模板，可在后台模板库发布");
            setItem(null);
        },
        onError: (error: Error) => message.error(error.message || "保存失败"),
    });
    if (!isAdmin) return null;

    const open = async () => {
        setBusy(true);
        try {
            const [output, ...references] = await Promise.all([work.output, ...work.references].map((image) => ensureServerFileId(image.source, image.storageKey)));
            setItem(templatePrefill({ prompt: work.prompt, output: pickedImageUrl(output), references: references.map(pickedImageUrl), width: work.width, height: work.height }));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "图片上传失败");
        } finally {
            setBusy(false);
        }
    };

    return (
        <>
            {block ? (
                <Button block className="!h-11 !rounded-lg" icon={<LayoutTemplate className="size-4" />} loading={busy} onClick={() => void open()}>
                    存为模板
                </Button>
            ) : (
                <Button size="small" title="存为模板" aria-label="存为模板" icon={<LayoutTemplate className="size-3.5" />} loading={busy} onClick={() => void open()} />
            )}
            <TemplateEditor
                item={item}
                saving={save.isPending}
                onSave={(value) => {
                    const problem = templateProblem(value);
                    if (problem) message.error(problem);
                    else save.mutate(value);
                }}
                onClose={() => setItem(null)}
            />
        </>
    );
}
