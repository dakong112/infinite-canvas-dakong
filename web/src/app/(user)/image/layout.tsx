"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

const imageTools = [
    { href: "/image/set", label: "商品套图" },
    { href: "/image/detail", label: "详情图 / A+" },
    { href: "/image/replicate", label: "图片复刻" },
    { href: "/image/remove-bg", label: "去除背景" },
    { href: "/image/tryon", label: "AI 换装" },
    { href: "/image", label: "自由生图" },
];

export default function ImageLayout({ children }: { children: ReactNode }) {
    const pathname = usePathname();
    return (
        <div className="flex h-full min-h-0 flex-col bg-stone-50 dark:bg-stone-950">
            <nav aria-label="图片工具" className="thin-scrollbar flex shrink-0 gap-1 overflow-x-auto border-b border-stone-200 px-4 dark:border-stone-800">
                {imageTools.map((tool) => {
                    const active = pathname === tool.href;
                    return (
                        <Link
                            key={tool.href}
                            href={tool.href}
                            aria-current={active ? "page" : undefined}
                            className={`whitespace-nowrap border-b-2 px-3 py-2 text-sm transition ${active ? "border-stone-900 font-medium !text-stone-950 dark:border-stone-100 dark:!text-stone-100" : "border-transparent !text-stone-500 hover:!text-stone-900 dark:hover:!text-stone-200"}`}
                        >
                            {tool.label}
                        </Link>
                    );
                })}
            </nav>
            <div className="flex min-h-0 flex-1 flex-col">{children}</div>
        </div>
    );
}
