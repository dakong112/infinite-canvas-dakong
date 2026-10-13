"use client";

import { App, Button, Card, Empty, Result, Tag } from "antd";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Crown } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { formatExpiry } from "@/components/layout/account-actions";
import { fetchAccountPlans, subscribeWithBalance, type AccountPlan } from "@/services/api/account";
import { useAccountStore } from "@/stores/use-account-store";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

const durationLabel: Record<string, string> = { year: "年", month: "个月", day: "天", hour: "小时" };

function formatPrice(plan: AccountPlan) {
    const symbol = plan.currency === "CNY" ? "¥" : plan.currency === "USD" ? "$" : `${plan.currency} `;
    return `${symbol}${plan.priceAmount}`;
}

const benefits = ["模型调用会员折扣", "会员模板免费使用", "查看全部锁定提示词", "作品保存时间更长"];

export default function MemberPage() {
    const { message, modal } = App.useApp();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const ssoMode = useConfigStore((state) => state.publicSettings !== null && state.publicSettings.auth?.newapiSso !== false);
    const summary = useAccountStore((state) => state.summary);
    const setSummary = useAccountStore((state) => state.setSummary);
    const [buyingId, setBuyingId] = useState<number>();
    const { data: plans, isLoading } = useQuery({
        queryKey: ["account-plans", token],
        queryFn: () => fetchAccountPlans(token!),
        enabled: Boolean(token) && ssoMode,
    });

    if (!token) return <Result status="info" title="请先登录" extra={<Link href="/login?redirect=/member">去登录</Link>} />;
    if (!ssoMode) return <Result status="info" title="当前站点未开启会员服务" />;

    const buy = (plan: AccountPlan) => {
        modal.confirm({
            title: `开通 ${plan.title}`,
            content: `将从账户余额扣除 ${formatPrice(plan)} 等值${summary?.creditName || "积分"}，确认开通？`,
            okText: "确认开通",
            onOk: async () => {
                setBuyingId(plan.id);
                try {
                    const next = await subscribeWithBalance(token, plan.id);
                    if (next) setSummary(next);
                    else await useAccountStore.getState().refresh();
                    void queryClient.invalidateQueries({ queryKey: ["account-pricing"] });
                    message.success("开通成功");
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "开通失败，请检查余额");
                } finally {
                    setBuyingId(undefined);
                }
            },
        });
    };

    const member = summary?.member;
    return (
        <main className="h-full overflow-y-auto px-6 py-8">
            <div className="mx-auto max-w-4xl space-y-6">
                <Card>
                    <div className="flex items-center gap-3">
                        <Crown className="size-6 text-amber-600 dark:text-amber-500" />
                        {member?.active ? (
                            <div>
                                <div className="text-lg font-semibold">{member.planTitle || "会员"}</div>
                                <div className="text-sm text-stone-500">{formatExpiry(member.expiresAt)} 到期</div>
                            </div>
                        ) : (
                            <div className="text-lg font-semibold">尚未开通会员</div>
                        )}
                    </div>
                    <div className="mt-4 flex flex-wrap gap-2">
                        {benefits.map((item) => <Tag key={item} color="gold">{item}</Tag>)}
                    </div>
                </Card>

                {!isLoading && !plans?.length ? <Empty description="暂无可购买的会员套餐" /> : null}
                <div className="grid gap-4 sm:grid-cols-2">
                    {plans?.map((plan) => (
                        <Card key={plan.id} title={plan.title}>
                            {plan.subtitle ? <p className="mb-3 text-sm text-stone-500">{plan.subtitle}</p> : null}
                            <div className="text-3xl font-semibold">
                                {formatPrice(plan)}
                                <span className="ml-1 text-sm font-normal text-stone-500">/ {plan.durationValue}{durationLabel[plan.durationUnit] || plan.durationUnit}</span>
                            </div>
                            <Button className="mt-4" type="primary" block disabled={!plan.allowBalancePay || buyingId !== undefined} loading={buyingId === plan.id} onClick={() => buy(plan)}>
                                {plan.allowBalancePay ? (member?.active ? "续费" : "余额开通") : "暂不支持余额开通"}
                            </Button>
                        </Card>
                    ))}
                </div>
            </div>
        </main>
    );
}
