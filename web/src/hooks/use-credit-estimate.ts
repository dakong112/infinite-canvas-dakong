"use client";

import { useQuery } from "@tanstack/react-query";

import { estimateCredits } from "@/lib/credit-estimate";
import { catalogCredits, isPlatformChannelId, platformGroupOf } from "@/lib/model-catalog";
import { fetchAccountPricing } from "@/services/api/account";
import { channelIdForActiveModel, isOwnChannel, useConfigStore, useEffectiveConfig, useModelCatalog } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

/** Returns the estimated credits for a generation, or null when no fixed price applies. Own channels are not billed by new-api. */
export function useCreditEstimate(model: string, count: number) {
    const token = useUserStore((state) => state.token);
    const ssoMode = useConfigStore((state) => state.publicSettings !== null && state.publicSettings.auth?.newapiSso !== false);
    const catalog = useModelCatalog();
    const config = useEffectiveConfig();
    const { data } = useQuery({
        queryKey: ["account-pricing", token],
        queryFn: () => fetchAccountPricing(token!),
        enabled: Boolean(token) && ssoMode,
        staleTime: 5 * 60 * 1000,
    });
    const enabled = ssoMode && Boolean(token);
    // ponytail: the channel comes from the global per-capability selection; pass a channelId in if nodes ever pick their own channel.
    const channelId = channelIdForActiveModel({ ...config, model });
    if (catalog && isPlatformChannelId(channelId)) return { enabled, credits: catalogCredits(catalog, model, platformGroupOf(channelId), count) };
    if (channelId && isOwnChannel(config, channelId)) return { enabled: false, credits: null };
    return { enabled, credits: estimateCredits(data, model, count) };
}
