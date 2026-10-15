"use client";

import { Button, Image, Modal } from "antd";
import { Shuffle } from "lucide-react";

export type WorkDetail = { image: string; references: string[]; prompt: string; apply: () => void };

/** Big image on the left; app name, reference images, prompt and "一键同款" on the right. Stacks below lg. */
export function WorkDetailModal({ appName, work, onClose }: { appName: string; work: WorkDetail | null; onClose: () => void }) {
    const block = "rounded-lg border border-border bg-muted/40 p-3";
    return (
        <Modal open={Boolean(work)} onCancel={onClose} footer={null} width={1100} centered destroyOnHidden styles={{ container: { padding: 0, overflow: "hidden" } }}>
            {work && (
                <div className="flex max-h-[85vh] flex-col lg:h-[640px] lg:flex-row">
                    <div className="flex min-h-[280px] min-w-0 flex-1 items-center justify-center bg-black">
                        <Image src={work.image} alt={appName} rootClassName="!block !max-h-full" className="!max-h-[85vh] object-contain lg:!max-h-[640px]" />
                    </div>
                    <aside className="thin-scrollbar flex w-full shrink-0 flex-col gap-3 overflow-y-auto p-5 lg:w-80">
                        <h2 className="pr-8 text-base font-semibold">作品详情</h2>
                        <div className={block}>
                            <div className="text-xs text-muted-foreground">应用名称</div>
                            <div className="mt-1 text-sm font-semibold">{appName}</div>
                        </div>
                        <div className={block}>
                            <div className="mb-2 text-xs text-muted-foreground">参考素材</div>
                            {work.references.length ? (
                                <Image.PreviewGroup>
                                    <div className="flex flex-wrap gap-2">
                                        {work.references.map((url) => (
                                            <div key={url} className="size-16 overflow-hidden rounded-md bg-muted">
                                                <Image src={url} alt="参考素材" rootClassName="!block !h-full !w-full" className="!h-full !w-full object-cover" />
                                            </div>
                                        ))}
                                    </div>
                                </Image.PreviewGroup>
                            ) : (
                                <div className="py-4 text-center text-xs text-muted-foreground">暂无参考素材</div>
                            )}
                        </div>
                        <div className={block}>
                            <div className="mb-1 text-xs text-muted-foreground">提示词</div>
                            <p className="whitespace-pre-wrap break-words text-sm leading-6">{work.prompt || "未填写提示词"}</p>
                        </div>
                        <Button
                            block
                            className="!mt-auto !h-11 !rounded-lg"
                            icon={<Shuffle className="size-4" />}
                            onClick={() => {
                                work.apply();
                                onClose();
                            }}
                        >
                            一键同款
                        </Button>
                    </aside>
                </div>
            )}
        </Modal>
    );
}
