import { apiGet, apiPost, apiPut } from "@/services/api/request";

export type AccountMember = { active: boolean; planId: number; planTitle: string; expiresAt: number; remainingCredits: number; unlimited: boolean };

/** The team wallet seen by one member: remaining is "我的剩余额度" (ignored when unlimited). */
export type AccountTeam = { id: number; name: string; role: string; balance: number; remaining: number; unlimited: boolean };

export type AccountSummary = {
    credits: number;
    usedCredits: number;
    group: string;
    creditName: string;
    member: AccountMember;
    /** Present in a team space. */
    team?: AccountTeam;
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

/** new-api SSO applications the user may open (infinite itself excluded). */
export type AccountApp = { clientId: string; name: string; launchUrl: string };
export const fetchAccountApps = (token: string) => apiGet<AccountApp[]>("/api/account/apps", undefined, token);

export type ModelCatalogDefaults = { group: string; image: string; text: string; video: string; audio: string };
export type ModelCatalogModel = { name: string; vendorId: number; kind: "image" | "video" | "text" | "audio" | ""; groups: string[]; quotaType: number; modelPrice: number; modelRatio: number; completionRatio?: number };
/** new-api groups the user may use and the models of each (SSO mode only). */
export type ModelCatalog = {
    platform: { name: string; defaults: ModelCatalogDefaults };
    userGroup: string;
    groups: Array<{ name: string; desc: string; ratio: number }>;
    vendors: Array<{ id: number; name: string; icon?: string }>;
    models: ModelCatalogModel[];
    creditsPerUnit: number;
    /** new-api quota per currency unit; 0 when unknown. */
    quotaPerUnit?: number;
};

export const fetchModelCatalog = (token: string) => apiGet<ModelCatalog>("/api/account/models", undefined, token);

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

/** new-api's team management page (keep in sync with service.NewapiTeamPage). */
export const NEWAPI_TEAM_PAGE = "/console/team";
export type NewapiPage = "/wallet" | "/" | typeof NEWAPI_TEAM_PAGE;

/** Asks the backend for a one-time new-api login URL for one of the allowed pages. */
export const createNewapiLoginUrl = (token: string, to: NewapiPage) => apiPost<{ loginUrl: string }>("/api/account/newapi-login", { to }, token);

/** id is "" for the personal space, "team:<id>" for a team. */
export type Space = { id: string; teamId: number; name: string; role: string };
export type SpaceList = { current: string; spaces: Space[] };

export const fetchSpaces = (token: string) => apiGet<SpaceList>("/api/spaces", undefined, token);
export const switchSpace = (token: string, teamId: number) => apiPut<SpaceList>("/api/space", { teamId }, token);
