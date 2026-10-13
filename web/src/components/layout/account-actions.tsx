"use client";

import { Crown } from "lucide-react";
import Link from "next/link";
import { Tooltip } from "antd";

import { CreditSymbol } from "@/constant/credits";
import { useAccountStore } from "@/stores/use-account-store";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

export function formatExpiry(expiresAt: number) {
    return new Date(expiresAt * 1000).toLocaleDateString("zh-CN");
}

/** Balance, recharge and membership shortcuts shown at the top right in SSO mode. */
export function AccountActions() {
    const ssoMode = useConfigStore((state) => state.publicSettings !== null && state.publicSettings.auth?.newapiSso !== false);
    const user = useUserStore((state) => state.user);
    const summary = useAccountStore((state) => state.summary);

    if (!ssoMode || !user) return null;
    const member = summary?.member;
    return (
        <div className="flex items-center gap-3 text-sm">
            <Link href="/recharge" className="flex items-center gap-1 font-medium !text-amber-600 hover:!text-amber-500 dark:!text-amber-500 dark:hover:!text-amber-400">
                <CreditSymbol />
                {summary ? summary.credits.toLocaleString() : "--"}
            </Link>
            <Link href="/recharge" className="!text-stone-500 hover:!text-stone-950 dark:!text-stone-400 dark:hover:!text-stone-100">
                充值
            </Link>
            <Tooltip title={member?.active ? `${member.planTitle || "会员"} · ${formatExpiry(member.expiresAt)} 到期` : "开通会员享模型折扣与会员模板"}>
                <Link
                    href="/member"
                    className={member?.active ? "flex items-center gap-1 font-medium !text-amber-600 dark:!text-amber-500" : "flex items-center gap-1 !text-stone-500 hover:!text-amber-500 dark:!text-stone-400"}
                >
                    <Crown className="size-4" />
                    会员
                </Link>
            </Tooltip>
        </div>
    );
}
