"use client";

import { DEFAULT_BILLING_DISPLAY, displayToUsd, displayUnitLabel, formatNewapiAmount, formatUsdAmount, usdToDisplay } from "@/lib/newapi-currency";
import { useConfigStore } from "@/stores/use-config-store";

/**
 * Money formatting for the current site. SSO mode shows everything in new-api's display unit (all charges happen there);
 * local-credit sites keep showing 积分. `credits` are infinite's internal unit (estimates, template / course fees).
 */
export function useMoney() {
    const settings = useConfigStore((state) => state.publicSettings);
    const sso = settings !== null && settings.auth?.newapiSso !== false;
    const display = sso ? settings?.billing?.display || DEFAULT_BILLING_DISPLAY : null;
    const perUnit = settings?.billing?.creditsPerUnit || 1;
    return {
        /** null outside SSO mode. */
        display,
        /** new-api raw quota (balances, plan quota). */
        quota: (quota: number) => formatNewapiAmount(quota, display || DEFAULT_BILLING_DISPLAY),
        /** Amounts in new-api units (USD), e.g. plan prices. */
        usd: (usd: number) => formatUsdAmount(usd, display || DEFAULT_BILLING_DISPLAY),
        credits: (credits: number) => (display ? formatUsdAmount(credits / perUnit, display) : `${Math.round(credits * 100) / 100} 积分`),
        /** Unit admins type fees in. */
        unit: display ? displayUnitLabel(display) : "积分",
        /** Form.Item props letting admins type a fee stored in credits in the display unit (SSO mode); empty otherwise. */
        feeField: display
            ? {
                  getValueProps: (credits?: number | null) => ({ value: typeof credits === "number" ? Math.round(usdToDisplay(credits / perUnit, display) * 1e4) / 1e4 : credits }),
                  normalize: (value?: number | null) => (typeof value === "number" ? displayToUsd(value, display) * perUnit : value),
              }
            : {},
    };
}
