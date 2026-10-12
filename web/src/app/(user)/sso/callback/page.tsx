"use client";

import { App, Result, Spin } from "antd";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useRef, useState } from "react";

import { safeRedirect } from "@/lib/safe-redirect";
import { exchangeNewapiCode, LOGIN_REDIRECT_KEY } from "@/services/api/newapi-auth";
import { useUserStore } from "@/stores/use-user-store";

export default function SSOCallbackPage() {
    return (
        <Suspense fallback={null}>
            <SSOCallbackContent />
        </Suspense>
    );
}

function SSOCallbackContent() {
    const { message } = App.useApp();
    const router = useRouter();
    const searchParams = useSearchParams();
    const setSession = useUserStore((state) => state.setSession);
    const started = useRef(false);
    const [error, setError] = useState("");

    useEffect(() => {
        if (started.current) return;
        started.current = true;
        const code = searchParams.get("code") ?? "";
        const state = searchParams.get("state") ?? "";
        const redirect = safeRedirect(sessionStorage.getItem(LOGIN_REDIRECT_KEY));
        sessionStorage.removeItem(LOGIN_REDIRECT_KEY);
        exchangeNewapiCode(code, state)
            .then((session) => {
                setSession(session.token, session.user);
                message.success("登录成功");
                router.replace(redirect);
                router.refresh();
            })
            .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "登录失败"));
    }, [message, router, searchParams, setSession]);

    if (error) {
        return <Result status="error" title="登录失败" subTitle={error} extra={<a href="/login">返回登录</a>} />;
    }
    return (
        <main className="flex h-full items-center justify-center">
            <Spin tip="正在登录…" />
        </main>
    );
}
