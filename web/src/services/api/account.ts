import { apiGet, apiPost } from "@/services/api/request";

export type AccountMember = { active: boolean; planId: number; planTitle: string; expiresAt: number; remainingCredits: number; unlimited: boolean };

export type AccountSummary = {
    credits: number;
    usedCredits: number;
    group: string;
    creditName: string;
    member: AccountMember;
};

export type AccountPlan = {
    id: number;
    title: string;
    subtitle: string;
    priceAmount: number;
    currency: string;
    durationUnit: string;
    durationValue: number;
    allowBalancePay: boolean;
    includedCredits: number;
    unlimited: boolean;
};

export type PayMethod = { name: string; type: string; color?: string };

export type AccountTopupInfo = {
    enableOnlineTopup: boolean;
    enableRedemption: boolean;
    payMethods: PayMethod[];
    minTopup: number;
    amountOptions: number[];
    topupLink: string;
};

export type EpayOrder = { url: string; params: Record<string, string> };

export type ModelPrice = { quotaType: number; modelPrice: number };

export type AccountPricing = { groupRatio: number; creditsPerUnit: number; models: Record<string, ModelPrice> };

export const fetchAccountSummary = (token: string) => apiGet<AccountSummary>("/api/account/summary", undefined, token);
export const fetchAccountPlans = (token: string) => apiGet<AccountPlan[]>("/api/account/plans", undefined, token);
/** Resolves to null when the write succeeded but the summary refresh failed; callers should then refresh(). */
export const subscribeWithBalance = (token: string, planId: number) => apiPost<AccountSummary | null>("/api/account/subscribe", { planId }, token);
export const fetchTopupInfo = (token: string) => apiGet<AccountTopupInfo>("/api/account/topup", undefined, token);
/** Resolves to null when the write succeeded but the summary refresh failed; callers should then refresh(). */
export const redeemTopupCode = (token: string, key: string) => apiPost<AccountSummary | null>("/api/account/topup/redeem", { key }, token);
export const createEpayOrder = (token: string, amount: number, paymentMethod: string) =>
    apiPost<EpayOrder>("/api/account/topup/epay", { amount, paymentMethod }, token);
export const fetchAccountPricing = (token: string) => apiGet<AccountPricing>("/api/account/pricing", undefined, token);

/** Sends the browser to the Epay gateway with a POSTed form, as new-api's own wallet does. */
export function submitEpayOrder(order: EpayOrder) {
    if (!/^https?:\/\//i.test(order.url)) throw new Error("支付地址无效");
    const form = document.createElement("form");
    form.method = "POST";
    form.action = order.url;
    for (const [name, value] of Object.entries(order.params)) {
        const input = document.createElement("input");
        input.type = "hidden";
        input.name = name;
        input.value = value;
        form.appendChild(input);
    }
    document.body.appendChild(form);
    form.submit();
}
