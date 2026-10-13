import type { AccountPricing } from "@/services/api/account";

/** Credits for `count` calls of a fixed-price model, or null when the model is billed by usage or unknown. */
export function estimateCredits(pricing: AccountPricing | undefined, model: string, count: number): number | null {
    const price = pricing?.models[model];
    if (!pricing || !price || price.quotaType !== 1 || price.modelPrice <= 0) return null;
    const calls = Math.max(1, Math.floor(count) || 1);
    return Math.round(price.modelPrice * pricing.groupRatio * pricing.creditsPerUnit * calls * 100) / 100;
}
