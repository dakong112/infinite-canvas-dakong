"use client";

import { Avatar } from "antd";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { CreditSymbol } from "@/constant/credits";
import { isNavigationActive, navigationTools } from "@/constant/navigation-tools";
import { cn } from "@/lib/utils";
import { useAccountStore } from "@/stores/use-account-store";
import { useUserStore } from "@/stores/use-user-store";

/** Desktop left navigation with the signed-in user card at the bottom. */
export function AppSidebar() {
    const pathname = usePathname();
    const user = useUserStore((state) => state.user);
    const summary = useAccountStore((state) => state.summary);

    return (
        <aside className="hidden w-56 shrink-0 flex-col border-r border-stone-200 bg-background md:flex dark:border-stone-800">
            <Link href="/" className="flex h-16 items-center gap-2 px-5 text-base font-medium !text-stone-950 dark:!text-stone-100">
                <span
                    className="size-6 shrink-0 bg-current"
                    style={{ mask: "url(/logo.svg) center / contain no-repeat", WebkitMask: "url(/logo.svg) center / contain no-repeat" }}
                />
                无限画布
            </Link>
            <nav className="flex-1 space-y-1 overflow-y-auto px-3 py-2">
                {navigationTools.map((tool) => {
                    const Icon = tool.icon;
                    const active = isNavigationActive(pathname, tool.href);
                    return (
                        <Link
                            key={tool.key}
                            href={tool.href}
                            className={cn(
                                "flex h-11 items-center gap-3 rounded-lg px-3 text-sm transition",
                                active
                                    ? "!bg-stone-100 font-medium !text-stone-950 dark:!bg-stone-800 dark:!text-stone-50"
                                    : "!text-stone-500 hover:!bg-stone-50 hover:!text-stone-950 dark:!text-stone-400 dark:hover:!bg-stone-900 dark:hover:!text-stone-100",
                            )}
                        >
                            <Icon className="size-4" />
                            {tool.label}
                        </Link>
                    );
                })}
            </nav>
            {user ? (
                <div className="flex items-center gap-3 border-t border-stone-200 p-4 dark:border-stone-800">
                    <Avatar src={user.avatarUrl || undefined}>{(user.displayName || user.username).slice(0, 1).toUpperCase()}</Avatar>
                    <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium text-stone-900 dark:text-stone-100">{user.displayName || user.username}</div>
                        {summary ? (
                            <div className="flex items-center gap-1 text-xs text-amber-600 dark:text-amber-500">
                                <CreditSymbol />
                                {summary.credits.toLocaleString()} {summary.creditName}
                            </div>
                        ) : null}
                    </div>
                    <Link href="/recharge" className="rounded-md !bg-amber-500 px-2.5 py-1 text-xs font-medium !text-stone-950 hover:!bg-amber-400">
                        充值
                    </Link>
                </div>
            ) : (
                <Link href="/login" className="m-4 rounded-lg border border-stone-200 py-2 text-center text-sm dark:border-stone-700">
                    登录 / 注册
                </Link>
            )}
        </aside>
    );
}
