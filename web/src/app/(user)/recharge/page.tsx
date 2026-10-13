"use client";

import { App, Button, Card, Empty, Input, Radio, Result, Space } from "antd";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useState } from "react";

import { CreditSymbol } from "@/constant/credits";
import { useNewapiConfig } from "@/hooks/use-newapi-config";
import { createEpayOrder, fetchTopupInfo, redeemTopupCode, submitEpayOrder } from "@/services/api/account";
import { useAccountStore } from "@/stores/use-account-store";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

export default function RechargePage() {
    const { message } = App.useApp();
    const token = useUserStore((state) => state.token);
    const ssoMode = useConfigStore((state) => state.publicSettings !== null && state.publicSettings.auth?.newapiSso !== false);
    const summary = useAccountStore((state) => state.summary);
    const setSummary = useAccountStore((state) => state.setSummary);
    const [code, setCode] = useState("");
    const [amount, setAmount] = useState<number>();
    const [method, setMethod] = useState<string>();
    const [busy, setBusy] = useState(false);
    const { data: info } = useQuery({
        queryKey: ["topup-info", token],
        queryFn: () => fetchTopupInfo(token!),
        enabled: Boolean(token) && ssoMode,
    });

    // Coming back from the payment gateway via bfcache keeps the old busy state.
    useEffect(() => {
        const reset = () => setBusy(false);
        window.addEventListener("pageshow", reset);
        return () => window.removeEventListener("pageshow", reset);
    }, []);

    const { data: newapiConfig } = useNewapiConfig(Boolean(token) && ssoMode);
    const accountCenterUrl = newapiConfig?.enabled ? newapiConfig.newapiUrl : "";
    const shopLink = /^https?:\/\//i.test(info?.topupLink ?? "") ? info!.topupLink : "";

    if (!token) return <Result status="info" title="请先登录" extra={<Link href="/login?redirect=/recharge">去登录</Link>} />;
    if (!ssoMode) return <Result status="info" title="当前站点未开启统一账号充值" />;

    const redeem = async () => {
        if (busy || !code.trim()) return;
        setBusy(true);
        try {
            const next = await redeemTopupCode(token, code.trim());
            if (next) setSummary(next);
            else await useAccountStore.getState().refresh();
            setCode("");
            message.success("兑换成功");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "兑换失败");
        } finally {
            setBusy(false);
        }
    };

    const pay = async () => {
        if (busy) return;
        if (!amount || !method) {
            message.warning("请选择充值金额和支付方式");
            return;
        }
        setBusy(true);
        try {
            submitEpayOrder(await createEpayOrder(token, amount, method));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "下单失败");
            setBusy(false);
        }
    };

    return (
        <main className="h-full overflow-y-auto px-6 py-8">
            <div className="mx-auto max-w-3xl space-y-6">
                <Card>
                    <div className="text-sm text-stone-500">当前余额</div>
                    <div className="mt-2 flex items-center gap-2 text-3xl font-semibold text-amber-600 dark:text-amber-500">
                        <CreditSymbol />
                        {summary ? summary.credits.toLocaleString() : "--"}
                        <span className="text-base font-normal text-stone-500">{summary?.creditName || "积分"}</span>
                    </div>
                </Card>

                {info?.enableRedemption === false ? null : (
                    <Card title="兑换码充值">
                        <Space.Compact className="w-full">
                            <Input value={code} onChange={(event) => setCode(event.target.value)} placeholder="输入兑换码" onPressEnter={redeem} />
                            <Button type="primary" loading={busy} onClick={redeem}>兑换</Button>
                        </Space.Compact>
                        {shopLink ? (
                            <Button className="mt-3" href={shopLink} target="_blank" rel="noopener noreferrer">购买兑换码</Button>
                        ) : (
                            <p className="mt-3 text-xs text-stone-500">兑换码可向客服购买</p>
                        )}
                    </Card>
                )}

                <Card title="在线充值">
                    {info?.enableOnlineTopup && info.payMethods.length ? (
                        <div className="space-y-4">
                            <Radio.Group value={amount} onChange={(event) => setAmount(event.target.value)} optionType="button" buttonStyle="solid"
                                options={info.amountOptions.filter((value) => value >= info.minTopup).map((value) => ({ label: `${value}`, value }))} />
                            <Radio.Group value={method} onChange={(event) => setMethod(event.target.value)}
                                options={info.payMethods.map((item) => ({ label: item.name, value: item.type }))} />
                            <Button type="primary" size="large" loading={busy} onClick={pay}>去支付</Button>
                            <p className="text-xs text-stone-500">支付完成后返回本页刷新即可看到最新余额。</p>
                        </div>
                    ) : (
                        <Empty description={info?.enableRedemption === false ? "暂未开放充值" : "暂未开放在线充值，请使用兑换码"} image={Empty.PRESENTED_IMAGE_SIMPLE} />
                    )}
                </Card>

                {accountCenterUrl ? (
                    <div className="text-center text-sm">
                        <a href={`${accountCenterUrl}/wallet`} target="_blank" rel="noopener noreferrer">前往账户中心充值 / 查看充值记录</a>
                        <p className="mt-1 text-xs text-stone-500">需在账户中心重新登录</p>
                    </div>
                ) : null}
            </div>
        </main>
    );
}
