"use client";

import { useQuery } from "@tanstack/react-query";
import { Drawer } from "antd";
import { ExternalLink } from "lucide-react";
import Link from "next/link";

import { isNavigationActive, navigationTools } from "@/constant/navigation-tools";
import { cn } from "@/lib/utils";
import { fetchAccountApps } from "@/services/api/account";
import { useUserStore } from "@/stores/use-user-store";

type MobileNavDrawerProps = {
    open: boolean;
    pathname: string;
    onClose: () => void;
};

export function MobileNavDrawer({ open, pathname, onClose }: MobileNavDrawerProps) {
    const token = useUserStore((state) => state.token);
    // Same query (and cache) as the desktop sidebar's 更多应用.
    const { data: apps = [] } = useQuery({ queryKey: ["account", "apps", token], queryFn: () => fetchAccountApps(token), enabled: Boolean(token), retry: false, staleTime: 60_000 });
    return (
        <Drawer title="导航" placement="left" size={280} open={open} onClose={onClose} className="md:hidden">
            <div className="space-y-1">
                {navigationTools.map((tool) => {
                    const Icon = tool.icon;
                    const active = isNavigationActive(pathname, tool.href);
                    return (
                        <Link
                            key={tool.key}
                            href={tool.href}
                            onClick={onClose}
                            className={cn(
                                "flex items-center gap-3 rounded-lg px-3 py-3 text-base transition",
                                active ? "bg-stone-100 font-medium text-stone-950 dark:bg-stone-800 dark:text-stone-100" : "text-stone-600 hover:bg-stone-100 hover:text-stone-950 dark:text-stone-300 dark:hover:bg-stone-800 dark:hover:text-stone-100",
                            )}
                        >
                            <Icon className="size-5" />
                            <span>{tool.label}</span>
                        </Link>
                    );
                })}
                {apps.length > 0 && (
                    <>
                        <div className="px-3 pb-1 pt-4 text-xs text-stone-400 dark:text-stone-500">更多应用</div>
                        {apps.map((app) => (
                            <a
                                key={app.clientId}
                                href={app.launchUrl}
                                target="_blank"
                                rel="noreferrer"
                                onClick={onClose}
                                className="flex items-center gap-3 rounded-lg px-3 py-3 text-base text-stone-600 transition hover:bg-stone-100 hover:text-stone-950 dark:text-stone-300 dark:hover:bg-stone-800 dark:hover:text-stone-100"
                            >
                                <ExternalLink className="size-5" />
                                <span className="truncate">{app.name}</span>
                            </a>
                        ))}
                    </>
                )}
            </div>
        </Drawer>
    );
}
