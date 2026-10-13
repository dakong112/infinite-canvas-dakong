"use client";

import { useEffect, useRef } from "react";

type TurnstileApi = {
    render: (element: HTMLElement, options: { sitekey: string; callback: (token: string) => void; "expired-callback": () => void; "error-callback": () => void }) => string;
    reset: (widgetId: string) => void;
    remove: (widgetId: string) => void;
};

declare global {
    interface Window {
        turnstile?: TurnstileApi;
    }
}

const SCRIPT_URL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

function loadTurnstile(): Promise<TurnstileApi> {
    if (window.turnstile) return Promise.resolve(window.turnstile);
    return new Promise((resolve, reject) => {
        const existing = document.querySelector<HTMLScriptElement>(`script[src="${SCRIPT_URL}"]`);
        const script = existing ?? Object.assign(document.createElement("script"), { src: SCRIPT_URL, async: true });
        script.addEventListener("load", () => (window.turnstile ? resolve(window.turnstile) : reject(new Error("Turnstile 加载失败"))));
        script.addEventListener("error", () => {
            script.remove(); // let a later attempt reload it
            reject(new Error("Turnstile 加载失败"));
        });
        if (!existing) document.head.appendChild(script);
    });
}

/** Renders a Cloudflare Turnstile challenge; `resetKey` changes force a fresh token after each attempt. */
export function TurnstileWidget({ siteKey, onToken, onError, resetKey }: { siteKey: string; onToken: (token: string) => void; onError?: () => void; resetKey: number }) {
    const containerRef = useRef<HTMLDivElement>(null);
    const widgetRef = useRef<string>("");

    useEffect(() => {
        let cancelled = false;
        loadTurnstile().then((api) => {
            if (cancelled || !containerRef.current) return;
            widgetRef.current = api.render(containerRef.current, { sitekey: siteKey, callback: onToken, "expired-callback": () => onToken(""), "error-callback": () => { onToken(""); onError?.(); } });
        }).catch(() => { onToken(""); onError?.(); });
        return () => {
            cancelled = true;
            if (widgetRef.current) window.turnstile?.remove(widgetRef.current);
            widgetRef.current = "";
        };
    }, [siteKey, onToken, onError]);

    useEffect(() => {
        if (resetKey && widgetRef.current) {
            onToken("");
            window.turnstile?.reset(widgetRef.current);
        }
    }, [resetKey, onToken]);

    return <div ref={containerRef} className="flex justify-center" />;
}
