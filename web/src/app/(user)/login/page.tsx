"use client";

import { LockOutlined, MailOutlined, SafetyOutlined, UserOutlined } from "@ant-design/icons";
import { App, Button, Form, Input, Result, Segmented, Space, Spin } from "antd";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";

import { TurnstileWidget } from "@/components/auth/turnstile-widget";
import { safeRedirect } from "@/lib/safe-redirect";
import { fetchCurrentUser } from "@/services/api/auth";
import {
    finishNewapiLogin,
    getNewapiAuthConfig,
    getNewapiStatus,
    newapiLogin,
    newapiRegister,
    newapiSendEmailCode,
    newapiVerifyLogin,
    type LoginChallenge,
    type NewapiAuthConfig,
    type NewapiStatus,
} from "@/services/api/newapi-auth";
import { useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";

type LoginFormValues = {
    username: string;
    password: string;
    confirmPassword?: string;
};

export default function LoginPage() {
    return (
        <Suspense fallback={null}>
            <LoginContent />
        </Suspense>
    );
}

function LoginContent() {
    const { message } = App.useApp();
    const router = useRouter();
    const searchParams = useSearchParams();
    const login = useUserStore((state) => state.login);
    const register = useUserStore((state) => state.register);
    const setSession = useUserStore((state) => state.setSession);
    const isLoading = useUserStore((state) => state.isLoading);
    const linuxDoEnabled = useConfigStore((state) => state.publicSettings?.auth?.linuxDo?.enabled === true);
    const allowRegister = useConfigStore((state) => state.publicSettings?.auth?.allowRegister !== false);
    const [mode, setMode] = useState<"login" | "register">("login");
    const redirect = safeRedirect(searchParams.get("redirect"));
    const [ssoConfig, setSsoConfig] = useState<NewapiAuthConfig | null>(null);
    const [localAdmin, setLocalAdmin] = useState(false);
    useEffect(() => {
        getNewapiAuthConfig()
            .then(setSsoConfig)
            .catch(() => setSsoConfig({ enabled: false }));
    }, []);

    useEffect(() => {
        const token = searchParams.get("token");
        const error = searchParams.get("error");
        if (error) message.error(error);
        if (!token) return;
        void fetchCurrentUser(token).then((user) => {
            setSession(token, user);
            message.success("登录成功");
            router.replace(redirect);
            router.refresh();
        });
    }, [message, redirect, router, searchParams, setSession]);

    useEffect(() => {
        if (!allowRegister && mode === "register") setMode("login");
    }, [allowRegister, mode]);

    const submit = async (values: LoginFormValues) => {
        try {
            if (mode === "register" && !allowRegister) {
                message.error("当前未开放注册");
                return;
            }
            if (mode === "register" && values.password !== values.confirmPassword) {
                message.error("两次输入的密码不一致");
                return;
            }
            const action = mode === "register" ? register : login;
            const user = await action({ username: values.username, password: values.password });
            message.success(mode === "register" ? "注册成功" : "登录成功");
            router.replace(redirect);
            router.refresh();
            if (user.role !== "admin") router.replace("/");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "登录失败");
        }
    };

    if (ssoConfig === null) return null;
    if (ssoConfig.enabled && !localAdmin) {
        return (
            <main className="flex h-full min-h-0 items-center justify-center overflow-y-auto bg-background px-6 py-10">
                <section className="w-full max-w-[420px]">
                    <h1 className="mb-7 text-center text-3xl font-semibold text-stone-950 dark:text-stone-100">账号登录</h1>
                    <NewapiLoginForm config={ssoConfig} redirect={redirect} />
                    <button type="button" className="mt-6 w-full text-center text-sm text-stone-400 hover:text-stone-600" onClick={() => setLocalAdmin(true)}>
                        管理员本地登录
                    </button>
                </section>
            </main>
        );
    }

    return (
        <main className="flex h-full min-h-0 items-center justify-center overflow-y-auto bg-background bg-[radial-gradient(#e5e7eb_1px,transparent_1px)] px-6 py-10 [background-size:16px_16px] dark:bg-[radial-gradient(rgba(245,245,244,.16)_1px,transparent_1px)]">
            <section className="w-full max-w-[420px]">
                <div className="mb-7 text-center">
                    <span
                        className="mx-auto mb-4 block size-12 bg-stone-950 dark:bg-stone-100"
                        style={{
                            mask: "url(/logo.svg) center / contain no-repeat",
                            WebkitMask: "url(/logo.svg) center / contain no-repeat",
                        }}
                        aria-label="无限画布"
                    />
                    <h1 className="text-3xl font-semibold tracking-normal text-stone-950 dark:text-stone-100">账号登录</h1>
                    <p className="mt-3 text-base leading-7 text-stone-500 dark:text-stone-400">支持账号密码和 Linux.do 登录。</p>
                </div>

                <Form<LoginFormValues> layout="vertical" size="large" requiredMark={false} onFinish={submit}>
                    <Form.Item>
                        <Segmented
                            block
                            value={mode}
                            onChange={(value) => setMode(value as "login" | "register")}
                            options={allowRegister ? [{ label: "登录", value: "login" }, { label: "注册", value: "register" }] : [{ label: "登录", value: "login" }]}
                        />
                    </Form.Item>
                    <Form.Item name="username" label={<span className="font-medium text-stone-800 dark:text-stone-200">用户名</span>} rules={[{ required: true, message: "请输入用户名" }]}>
                        <Input prefix={<UserOutlined />} autoComplete="username" />
                    </Form.Item>
                    <Form.Item name="password" label={<span className="font-medium text-stone-800 dark:text-stone-200">密码</span>} rules={[{ required: true, message: "请输入密码" }]}>
                        <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
                    </Form.Item>
                    {mode === "register" ? (
                        <Form.Item name="confirmPassword" label={<span className="font-medium text-stone-800 dark:text-stone-200">确认密码</span>} rules={[{ required: true, message: "请再次输入密码" }]}>
                            <Input.Password prefix={<LockOutlined />} autoComplete="new-password" />
                        </Form.Item>
                    ) : null}
                    <Space orientation="vertical" size={12} style={{ width: "100%" }}>
                        <Button block type="primary" htmlType="submit" loading={isLoading}>
                            {mode === "register" ? "注册" : "登录"}
                        </Button>
                        {linuxDoEnabled ? (
                            <Button block href={`/api/auth/linux-do/authorize?redirect=${encodeURIComponent(redirect)}`} icon={<img src="/icons/linuxdo.svg" alt="" width={18} height={18} />}>
                                使用 Linux.do 登录
                            </Button>
                        ) : null}
                    </Space>
                </Form>
            </section>
        </main>
    );
}

type NewapiFormValues = { username: string; email: string; code: string; totp: string; password: string; confirmPassword: string; affCode?: string };

function NewapiLoginForm({ config, redirect }: { config: NewapiAuthConfig; redirect: string }) {
    const { message } = App.useApp();
    const [status, setStatus] = useState<NewapiStatus | null>(null);
    const [mode, setMode] = useState<"login" | "register">("login");
    const [challenge, setChallenge] = useState<LoginChallenge | null>(null);
    const [submitting, setSubmitting] = useState(false);
    const [turnstileToken, setTurnstileToken] = useState("");
    const [turnstileReset, setTurnstileReset] = useState(0);
    const onTurnstileToken = useCallback((token: string) => setTurnstileToken(token), []);
    const [form] = Form.useForm<NewapiFormValues>();
    const base = config.newapiUrl!;

    useEffect(() => {
        getNewapiStatus(base)
            .then(setStatus)
            .catch(() => message.error("统一账号服务不可用"));
    }, [base, message]);

    if (!status) return <Spin />;
    const accountCenter = (subTitle: string, back?: boolean) => (
        <Result
            status="info"
            title="请在账号中心登录"
            subTitle={subTitle}
            extra={[
                <Button key="go" type="primary" href={`${base}/login`}>前往账号中心</Button>,
                back ? <Button key="back" onClick={() => setChallenge(null)}>返回</Button> : null,
            ]}
        />
    );
    const needTurnstile = Boolean(status.turnstile_check && status.turnstile_site_key);
    const turnstile = needTurnstile ? turnstileToken : undefined;
    if (status.turnstile_check && !status.turnstile_site_key) return accountCenter("当前账号服务开启了人机验证，请先登录账号中心，再点击“应用”进入本平台。");
    if (status.password_login_enabled === false) return accountCenter("当前账号服务未开启密码登录，请先登录账号中心，再点击“应用”进入本平台。");
    if (challenge && !challenge.methods?.some((item) => item.method === "2fa" && item.available !== false)) {
        return accountCenter("该账号需要的验证方式暂不支持，请先登录账号中心，再点击“应用”进入本平台。", true);
    }
    const canRegister = status.register_enabled && status.password_register_enabled;

    const run = async (action: () => Promise<void>) => {
        setSubmitting(true);
        try {
            await action();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "操作失败");
        } finally {
            setSubmitting(false);
            // A Turnstile token is single-use.
            setTurnstileToken("");
            setTurnstileReset((value) => value + 1);
        }
    };

    const signIn = (username: string, password: string) =>
        run(async () => {
            const result = await newapiLogin(base, status, username, password, turnstile);
            if (typeof result === "string") {
                await finishNewapiLogin(config, result, redirect);
                return;
            }
            setChallenge(result);
        });

    const submit = (values: NewapiFormValues) => {
        if (challenge) {
            return run(async () => {
                const token = await newapiVerifyLogin(base, challenge.flow_token, values.totp);
                await finishNewapiLogin(config, token, redirect);
            });
        }
        if (mode === "login") return signIn(values.username, values.password);
        if (values.password !== values.confirmPassword) {
            message.error("两次输入的密码不一致");
            return;
        }
        return run(async () => {
            await newapiRegister(base, {
                username: values.username,
                password: values.password,
                email: values.email,
                verification_code: values.code ?? "",
                aff_code: values.affCode,
            }, turnstile);
            if (needTurnstile) {
                message.success("注册成功，请完成人机验证后登录");
                setMode("login");
                return;
            }
            message.success("注册成功，正在登录");
            const result = await newapiLogin(base, status, values.username, values.password);
            if (typeof result === "string") await finishNewapiLogin(config, result, redirect);
            else setChallenge(result);
        });
    };

    const sendCode = () =>
        run(async () => {
            const email = form.getFieldValue("email");
            if (!email) throw new Error("请先填写邮箱");
            await newapiSendEmailCode(base, email, turnstile);
            message.success("验证码已发送");
        });

    return (
        <Form<NewapiFormValues> form={form} layout="vertical" size="large" requiredMark={false} onFinish={submit}>
            {challenge ? (
                <>
                    <Form.Item name="totp" label="两步验证码" rules={[{ required: true, message: "请输入验证码" }]}>
                        <Input prefix={<SafetyOutlined />} autoComplete="one-time-code" />
                    </Form.Item>
                    <Button block className="mb-3" onClick={() => setChallenge(null)}>返回</Button>
                </>
            ) : (
                <>
                    {canRegister ? (
                        <Form.Item>
                            <Segmented block value={mode} onChange={(value) => setMode(value as "login" | "register")} options={[{ label: "登录", value: "login" }, { label: "注册", value: "register" }]} />
                        </Form.Item>
                    ) : null}
                    <Form.Item name="username" label={mode === "login" ? "用户名或邮箱" : "用户名"} rules={[{ required: true, message: "请输入用户名" }]}>
                        <Input prefix={<UserOutlined />} autoComplete="username" />
                    </Form.Item>
                    {mode === "register" ? (
                        <>
                            <Form.Item name="email" label="邮箱" rules={[{ required: true, type: "email", message: "请输入有效邮箱" }]}>
                                <Input prefix={<MailOutlined />} autoComplete="email" />
                            </Form.Item>
                            {status.email_verification ? (
                                <Form.Item name="code" label="邮箱验证码" rules={[{ required: true, message: "请输入验证码" }]}>
                                    <Input suffix={<Button type="link" size="small" onClick={sendCode}>发送验证码</Button>} />
                                </Form.Item>
                            ) : null}
                        </>
                    ) : null}
                    <Form.Item name="password" label="密码" rules={[{ required: true, message: "请输入密码" }]}>
                        <Input.Password prefix={<LockOutlined />} autoComplete={mode === "login" ? "current-password" : "new-password"} />
                    </Form.Item>
                    {mode === "register" ? (
                        <>
                            <Form.Item name="confirmPassword" label="确认密码" rules={[{ required: true, message: "请再次输入密码" }]}>
                                <Input.Password prefix={<LockOutlined />} autoComplete="new-password" />
                            </Form.Item>
                            <Form.Item name="affCode" label="邀请码（选填）">
                                <Input />
                            </Form.Item>
                        </>
                    ) : null}
                </>
            )}
            {needTurnstile && !challenge ? (
                <Form.Item>
                    <TurnstileWidget siteKey={status.turnstile_site_key} onToken={onTurnstileToken} resetKey={turnstileReset} />
                </Form.Item>
            ) : null}
            <Button block type="primary" htmlType="submit" loading={submitting} disabled={needTurnstile && !challenge && !turnstileToken}>
                {challenge ? "验证" : mode === "register" ? "注册" : "登录"}
            </Button>
        </Form>
    );
}
