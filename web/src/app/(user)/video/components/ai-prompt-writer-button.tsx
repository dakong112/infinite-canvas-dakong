"use client";

import { App } from "antd";
import { useState } from "react";

import { AiPillButton, useImagePrice } from "@/app/(user)/image/components/image-tool-shell";
import { cleanVideoPrompt, videoPromptMessages, videoPromptUsage } from "@/lib/video-prompt";
import { writeVideoPrompt } from "@/services/api/video";
import { useConfigStore } from "@/stores/use-config-store";

type NamedAsset = { name: string };

/** "AI 代写": turns the short description in the prompt box (plus the referenced asset names) into a full video prompt with the platform script model. */
export function AiPromptWriterButton({ prompt, images, videos, audios, onPromptChange, small }: { prompt: string; images?: NamedAsset[]; videos?: NamedAsset[]; audios?: NamedAsset[]; onPromptChange: (value: string) => void; small?: boolean }) {
    const { message } = App.useApp();
    const model = useConfigStore((state) => state.publicSettings?.video?.scriptModel || "");
    const sso = useConfigStore((state) => state.publicSettings?.auth?.newapiSso !== false);
    const price = useImagePrice();
    const [loading, setLoading] = useState(false);
    if (!model) return null;
    const write = async () => {
        if (!prompt.trim()) {
            message.warning("先写一句简短描述，再让 AI 代写");
            return;
        }
        setLoading(true);
        try {
            onPromptChange(cleanVideoPrompt(await writeVideoPrompt(model, videoPromptMessages(prompt, { images, videos, audios }), sso)));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "代写失败");
        } finally {
            setLoading(false);
        }
    };
    return (
        <AiPillButton size={small ? "small" : undefined} credits={price([{ model, usage: videoPromptUsage }])} loading={loading} onClick={() => void write()}>
            AI 代写
        </AiPillButton>
    );
}
