"use client";

import { App } from "antd";

import { createNewapiLoginUrl } from "@/services/api/account";
import { useUserStore } from "@/stores/use-user-store";

/** Returns a click handler that opens the new-api account center in a new tab, already signed in. */
export function useOpenAccountCenter() {
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    return async (to: "/wallet" | "/" = "/") => {
        // Open synchronously so the popup blocker treats it as a user gesture; navigate after the request.
        const win = window.open("about:blank");
        if (!win) {
            message.error("浏览器拦截了弹窗，请允许后重试");
            return;
        }
        win.opener = null;
        try {
            const { loginUrl } = await createNewapiLoginUrl(token!, to);
            if (!/^https?:\/\//i.test(loginUrl)) throw new Error("账户中心地址无效");
            win.location.href = loginUrl;
        } catch (error) {
            win.close();
            message.error(error instanceof Error ? error.message : "打开账户中心失败");
        }
    };
}
