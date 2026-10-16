"use client";

import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Crown, Users } from "lucide-react";
import Link from "next/link";
import { App, Dropdown, Tooltip } from "antd";

import { CreditSymbol } from "@/constant/credits";
import { useOpenAccountCenter } from "@/hooks/use-open-account-center";
import { rememberSpace } from "@/lib/space";
import { fetchSpaces, NEWAPI_TEAM_PAGE, switchSpace } from "@/services/api/account";
import { useAccountStore } from "@/stores/use-account-store";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

export function formatExpiry(expiresAt: number) {
    return new Date(expiresAt * 1000).toLocaleDateString("zh-CN");
}

/** Personal / team space switcher; "团队管理" opens new-api's team page signed in. */
function SpaceSwitcher() {
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    const spaceId = useUserStore((state) => state.user?.spaceId ?? "");
    const openAccountCenter = useOpenAccountCenter();
    const { data } = useQuery({ queryKey: ["spaces", token], queryFn: () => fetchSpaces(token), enabled: Boolean(token), staleTime: 60_000 });
    const spaces = data?.spaces ?? [];
    const current = spaces.find((space) => space.id === spaceId);

    const switchTo = async (teamId: number) => {
        try {
            const list = await switchSpace(token, teamId);
            // Projects, materials and works belong to the space: reload so every cache follows.
            rememberSpace(list.current);
            window.location.reload();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "切换空间失败");
        }
    };

    return (
        <Dropdown
            trigger={["click"]}
            menu={{
                selectedKeys: [spaceId || "personal"],
                items: [
                    ...spaces.map((space) => ({ key: space.id || "personal", label: space.name, onClick: () => space.id !== spaceId && void switchTo(space.teamId) })),
                    { type: "divider" as const },
                    { key: "manage", label: "团队管理", onClick: () => void openAccountCenter(NEWAPI_TEAM_PAGE) },
                ],
            }}
        >
            <button type="button" className="flex cursor-pointer items-center gap-1 !text-stone-500 hover:!text-stone-950 dark:!text-stone-400 dark:hover:!text-stone-100">
                <Users className="size-4" />
                <span className="max-w-28 truncate">{current?.name ?? (spaceId ? "团队空间" : "个人空间")}</span>
                <ChevronDown className="size-3" />
            </button>
        </Dropdown>
    );
}

/** Space, balance, recharge and membership shortcuts shown at the top right in SSO mode. */
export function AccountActions() {
    const ssoMode = useConfigStore((state) => state.publicSettings !== null && state.publicSettings.auth?.newapiSso !== false);
    const user = useUserStore((state) => state.user);
    const summary = useAccountStore((state) => state.summary);

    if (!ssoMode || !user) return null;
    const member = summary?.member;
    const team = summary?.team;
    const remaining = team?.unlimited ? "不限" : team?.remaining.toLocaleString();
    return (
        <div className="flex items-center gap-3 text-sm">
            <SpaceSwitcher />
            {team ? (
                <Tooltip title={`团队「${team.name}」余额，生成从团队钱包扣费；我的剩余额度：${remaining}`}>
                    <span className="flex items-center gap-1 font-medium text-amber-600 dark:text-amber-500">
                        <CreditSymbol />
                        {team.balance.toLocaleString()}
                        <span className="font-normal text-stone-500 dark:text-stone-400">· 我的剩余额度 {remaining}</span>
                    </span>
                </Tooltip>
            ) : (
                <>
                    <Link href="/recharge" className="flex items-center gap-1 font-medium !text-amber-600 hover:!text-amber-500 dark:!text-amber-500 dark:hover:!text-amber-400">
                        <CreditSymbol />
                        {summary ? summary.credits.toLocaleString() : "--"}
                    </Link>
                    <Link href="/recharge" className="!text-stone-500 hover:!text-stone-950 dark:!text-stone-400 dark:hover:!text-stone-100">
                        充值
                    </Link>
                </>
            )}
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
