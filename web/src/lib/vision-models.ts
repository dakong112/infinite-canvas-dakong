/** Model families known to read images (shown as "支持识图" when admins pick feature models). */
const visionModelPatterns = [/gpt-4o/, /gpt-4\.1/, /gpt-5/, /(^|\/)o\d(-|$)/, /gemini/, /claude/, /qwen[\w.-]*-?vl/, /qvq/, /glm-4(\.\d+)?v/, /doubao[\w.-]*vision/, /seed[\w.-]*vision/, /(kimi|moonshot)[\w.-]*(vision|vl)/, /internvl/, /llava/];

export function isVisionModel(name: string) {
    const value = name.trim().toLowerCase();
    return visionModelPatterns.some((pattern) => pattern.test(value));
}
