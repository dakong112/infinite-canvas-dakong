"use client";

import { useQuery } from "@tanstack/react-query";

import { estimateCredits } from "@/lib/credit-estimate";
import { fetchAccountPricing } from "@/services/api/account";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

/** Returns the estimated credits for a generation, or null when no fixed price applies. */
export function useCreditEstimate(model: string, count: number) {
    const token = useUserStore((state) => state.token);
    const ssoMode = useConfigStore((state) => state.publicSettings !== null && state.publicSettings.auth?.newapiSso !== false);
    const { data } = useQuery({
        queryKey: ["account-pricing", token],
        queryFn: () => fetchAccountPricing(token!),
        enabled: Boolean(token) && ssoMode,
        staleTime: 5 * 60 * 1000,
    });
    return { enabled: ssoMode && Boolean(token), credits: estimateCredits(data, model, count) };
}
