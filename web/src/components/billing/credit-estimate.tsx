"use client";

import { CreditSymbol } from "@/constant/credits";
import { useCreditEstimate } from "@/hooks/use-credit-estimate";
import { useAccountStore } from "@/stores/use-account-store";

/** Inline "⚡ 3.9 积分" label; renders nothing outside SSO mode. */
export function CreditEstimate({ model, count = 1, channelId, className }: { model: string; count?: number; channelId?: string; className?: string }) {
    const { enabled, credits } = useCreditEstimate(model, count, channelId);
    const creditName = useAccountStore((state) => state.summary?.creditName || "积分");
    if (!enabled) return null;
    return (
        <span className={className}>
            {credits === null ? "按量计费" : (
                <>
                    <CreditSymbol />
                    {credits > 0 ? credits.toLocaleString() : "<0.01"} {creditName}
                </>
            )}
        </span>
    );
}
