import type { TypicalUsage } from "@/lib/model-catalog";

type NamedAsset = { name: string };

/** Typical tokens of one "AI 代写" call, for the ≈credits label of usage-billed script models. */
export const videoPromptUsage: TypicalUsage = { input: 800, output: 400 };

/** Chat messages asking the script model to expand a short idea into a video prompt; referenced assets are listed by order and name. */
export function videoPromptMessages(idea: string, assets: { images?: NamedAsset[]; videos?: NamedAsset[]; audios?: NamedAsset[] } = {}) {
    const lines = (["图片", "视频", "音频"] as const).flatMap((kind, index) =>
        ([assets.images, assets.videos, assets.audios][index] || []).map((asset, order) => `${kind}${order + 1}：${asset.name}`),
    );
    return [
        {
            role: "system" as const,
            content:
                "你是 AI 视频提示词专家。把用户的简短描述扩写成一段可直接用于视频生成模型的中文提示词：写清主体与动作、场景与氛围、镜头运动与景别、光线色调和画面风格；用户列出参考素材时，用“图片1”“视频1”这样的名字指代它们。只输出提示词正文，不要标题、解释或引号，不超过 200 字。",
        },
        { role: "user" as const, content: lines.length ? `${idea.trim()}\n\n参考素材：\n${lines.join("\n")}` : idea.trim() },
    ];
}

/** The reply as a bare prompt: drops a code fence, a leading "提示词：" label and wrapping quotes. */
export function cleanVideoPrompt(answer: string) {
    return answer
        .trim()
        .replace(/^```[\w-]*\n?|\n?```$/g, "")
        .trim()
        .replace(/^(视频)?提示词[:：]\s*/, "")
        .replace(/^["“'「]([\s\S]*)["”'」]$/, "$1")
        .trim();
}
