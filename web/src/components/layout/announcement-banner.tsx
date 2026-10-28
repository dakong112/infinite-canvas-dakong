"use client";

import { useEffect, useState } from "react";
import Link from "next/link";

import { useConfigStore } from "@/stores/use-config-store";

export function AnnouncementBanner() {
    const announcement = useConfigStore((state) => state.publicSettings?.announcement);
    const [now, setNow] = useState(() => Date.now());

    useEffect(() => {
        if (!announcement?.enabled || !announcement.endAt) return;
        const timer = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(timer);
    }, [announcement?.enabled, announcement?.endAt]);

    if (announcement?.enabled && announcement.text) {
        const remaining = announcement.endAt ? new Date(announcement.endAt).getTime() - now : Number.POSITIVE_INFINITY;
        if (remaining <= 0) return null;
        return (
            <div className="flex h-9 shrink-0 items-center justify-center gap-3 bg-amber-500 px-4 text-sm font-medium text-stone-950">
                {Number.isFinite(remaining) ? <span className="font-mono">{formatCountdown(remaining)}</span> : null}
                {/^(https:\/\/|\/(?!\/|\\))/.test(announcement.link) ? (
                    announcement.link.startsWith("/") ? (
                        <Link href={announcement.link} className="!text-stone-950 underline-offset-2 hover:underline">{announcement.text}</Link>
                    ) : (
                        <a href={announcement.link} className="!text-stone-950 underline-offset-2 hover:underline">{announcement.text}</a>
                    )
                ) : (
                    <span>{announcement.text}</span>
                )}
            </div>
        );
    }

    return null;
}

function formatCountdown(ms: number) {
    const total = Math.floor(ms / 1000);
    const days = Math.floor(total / 86400);
    const pad = (value: number) => String(value).padStart(2, "0");
    return `${days > 0 ? `${days}天 ` : ""}${pad(Math.floor((total % 86400) / 3600))}:${pad(Math.floor((total % 3600) / 60))}:${pad(total % 60)}`;
}
