/** new-api's money display unit (public settings billing.display): amount = quota / quotaPerUnit × rate; TOKENS shows raw quota. */
export type BillingDisplay = { mode: "USD" | "CNY" | "TOKENS" | "CUSTOM"; symbol: string; rate: number; quotaPerUnit: number };

/** new-api web's own defaults (DEFAULT_CURRENCY_CONFIG), used until the real settings load. */
export const DEFAULT_BILLING_DISPLAY: BillingDisplay = { mode: "USD", symbol: "$", rate: 1, quotaPerUnit: 500000 };

const trimZeros = (text: string) => (text.includes(".") ? text.replace(/(\.\d*?)0+$/, "$1").replace(/\.$/, "") : text);

/**
 * Formats an amount of new-api units (USD) like new-api web's formatCurrencyFromUSD (lib/currency.ts): 2 decimals from 1 up,
 * 4 below, "¤ 1.5" for custom symbols, and "1.5k" style token counts in TOKENS mode. Positive amounts too small to show read "<¥0.0001".
 */
export function formatUsdAmount(usd: number, display: BillingDisplay) {
    if (display.mode === "TOKENS") {
        const tokens = usd * display.quotaPerUnit;
        if (Math.abs(tokens) >= 1000) return `${trimZeros((tokens / 1000).toFixed(1))}k`;
        return trimZeros(tokens.toFixed(Math.abs(tokens) >= 1 ? 0 : 4));
    }
    const value = usd * display.rate;
    const digits = Math.abs(value) >= 1 ? 2 : 4;
    const symbol = display.mode === "CUSTOM" ? `${display.symbol} ` : display.symbol;
    if (value > 0 && value < 10 ** -digits) return `<${symbol}${10 ** -digits}`;
    return `${value < 0 ? "-" : ""}${symbol}${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: digits }).format(Math.abs(value))}`;
}

/** Formats new-api raw quota (balances, plan quota) in its display unit. */
export function formatNewapiAmount(quota: number, display: BillingDisplay) {
    return formatUsdAmount(quota / display.quotaPerUnit, display);
}

/** Number an admin types for an amount of new-api units, and back; TOKENS mode uses raw quota. */
export const usdToDisplay = (usd: number, display: BillingDisplay) => usd * (display.mode === "TOKENS" ? display.quotaPerUnit : display.rate);
export const displayToUsd = (value: number, display: BillingDisplay) => value / (display.mode === "TOKENS" ? display.quotaPerUnit : display.rate);
export const displayUnitLabel = (display: BillingDisplay) => (display.mode === "TOKENS" ? "额度" : display.symbol);
