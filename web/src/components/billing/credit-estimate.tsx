"use client";

import { CreditSymbol } from "@/constant/credits";
import { useCreditEstimate } from "@/hooks/use-credit-estimate";
import { useMoney } from "@/hooks/use-money";

/** Inline "⚡ ≈¥0.35" label in new-api's money unit; renders nothing outside SSO mode or when the model has no estimate. */
export function CreditEstimate({ model, count = 1, channelId, className }: { model: string; count?: number; channelId?: string; className?: string }) {
    const { enabled, credits } = useCreditEstimate(model, count, channelId);
    const money = useMoney();
    if (!enabled || credits === null) return null;
    return (
        <span className={className}>
            <CreditSymbol />≈{money.credits(credits)}
        </span>
    );
}
