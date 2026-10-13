"use client";

import { Menu } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { AccountActions } from "@/components/layout/account-actions";
import { AppConfigModal } from "@/components/layout/app-config-modal";
import { AnnouncementBanner } from "@/components/layout/announcement-banner";
import { MobileNavDrawer } from "@/components/layout/mobile-nav-drawer";
import { UserStatusActions } from "@/components/layout/user-status-actions";
import { useState } from "react";

export function AppTopNav() {
    const pathname = usePathname();
    const [mobileNavOpen, setMobileNavOpen] = useState(false);
    const hideHeader = /^\/canvas\/[^/]+/.test(pathname);

    return (
        <>
            {!hideHeader && <AnnouncementBanner />}
            {!hideHeader ? (
                <header className="sticky top-0 z-20 h-16 shrink-0 border-b border-stone-200 bg-background/90 backdrop-blur-xl dark:border-stone-800">
                    <div className="flex h-full w-full items-stretch justify-between gap-5 px-6">
                        <div className="flex min-w-0 items-center">
                            <Link href="/" className="flex h-full shrink-0 items-center gap-2 md:hidden text-sm font-semibold leading-none tracking-tight text-stone-950 transition hover:text-stone-600 dark:text-stone-100 dark:hover:text-stone-300">
                                <span
                                    className="size-5 shrink-0 bg-current"
                                    style={{
                                        mask: "url(/logo.svg) center / contain no-repeat",
                                        WebkitMask: "url(/logo.svg) center / contain no-repeat",
                                    }}
                                />
                                <span className="text-base font-medium">无限画布</span>
                            </Link>

                            <button
                                type="button"
                                className="ml-3 inline-flex size-8 shrink-0 items-center justify-center text-stone-600 transition hover:text-stone-950 md:hidden dark:text-stone-300 dark:hover:text-white"
                                onClick={() => setMobileNavOpen(true)}
                                aria-label="打开导航菜单"
                                title="导航菜单"
                            >
                                <Menu className="size-5" />
                            </button>
                        </div>

                        <div className="my-auto flex h-9 min-w-0 items-center justify-end gap-4 justify-self-end whitespace-nowrap">
                            <AccountActions />
                            <UserStatusActions />
                        </div>
                    </div>
                </header>
            ) : null}

            <MobileNavDrawer open={mobileNavOpen} pathname={pathname} onClose={() => setMobileNavOpen(false)} />
            <AppConfigModal />
        </>
    );
}
