"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Empty, Spin, Tag } from "antd";
import { ArrowLeft, CalendarDays, ExternalLink, GraduationCap, Lock, PlayCircle, Users } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { courseDates, coursePrice, courseTags, seatsLabel } from "@/lib/academy";
import { enrollAcademyCourse, fetchAcademyCourse, type AcademyCourse } from "@/services/api/academy";
import { useAccountStore } from "@/stores/use-account-store";
import { useUserStore } from "@/stores/use-user-store";

const isStoredFile = (url: string) => url.startsWith("/api/files/") || /\.(mp4|webm|mov)(\?|$)/i.test(url);

function EnrollButton({ course }: { course: AcademyCourse }) {
    const { message, modal } = App.useApp();
    const router = useRouter();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const summary = useAccountStore((state) => state.summary);
    const member = Boolean(summary?.member?.active);
    const price = coursePrice(course, member, summary?.creditName);
    const enroll = useMutation({
        mutationFn: () => enrollAcademyCourse(token, course.slug),
        onSuccess: () => {
            message.success("报名成功");
            void queryClient.invalidateQueries({ queryKey: ["academy"] });
            void useAccountStore.getState().refresh();
        },
        onError: (error: Error) => message.error(error.message || "报名失败"),
    });
    const button = (label: string, props: Parameters<typeof Button>[0] = {}) => (
        <Button type="primary" size="large" className="!min-w-48" {...props}>
            {label}
        </Button>
    );

    if (course.enrolled) return button("已报名", { disabled: true });
    if (course.state === "ended") return button("报名已结束", { disabled: true });
    if (course.state === "full") return button("名额已满", { disabled: true });
    if (!token) return button("登录后报名", { onClick: () => router.push(`/login?redirect=${encodeURIComponent(`/academy/${course.slug}`)}`) });
    if (course.memberOnly && summary && !member) return button("开通会员后报名", { onClick: () => router.push("/member") });
    return button(`立即报名 · ${price.label}`, {
        loading: enroll.isPending,
        onClick: () =>
            price.free
                ? enroll.mutate()
                : modal.confirm({ title: "确认报名", content: `报名将从个人账户扣除 ${course.feeCredits} ${summary?.creditName || "积分"}，确认报名「${course.title}」？`, okText: "确认报名", cancelText: "取消", onOk: () => enroll.mutateAsync() }),
    });
}

export default function AcademyCoursePage() {
    const { slug } = useParams<{ slug: string }>();
    const token = useUserStore((state) => state.token);
    const { data: course, isLoading } = useQuery({ queryKey: ["academy", "course", slug, token], queryFn: () => fetchAcademyCourse(slug, token || undefined), retry: false });
    if (isLoading) return <Spin className="!mt-24 !block" />;
    if (!course) return <Empty className="!mt-24" description="课程不存在或未发布" />;
    const dates = courseDates(course);
    const lessons = course.lessons ?? [];

    return (
        <div className="thin-scrollbar h-full overflow-y-auto bg-stone-50 text-foreground dark:bg-background">
            <div className="mx-auto max-w-[1000px] space-y-5 px-4 py-5 md:px-6">
                <Link href="/academy" className="inline-flex items-center gap-1 text-sm !text-muted-foreground hover:!text-foreground">
                    <ArrowLeft className="size-4" />
                    返回学院
                </Link>
                <section className="flex flex-col gap-5 rounded-2xl border border-border bg-card p-5 md:flex-row">
                    <div className="aspect-square w-full shrink-0 overflow-hidden rounded-xl bg-muted md:w-56">
                        {course.coverUrl ? (
                            <img src={course.coverUrl} alt="" className="size-full object-cover" />
                        ) : (
                            <div className="flex size-full items-center justify-center bg-gradient-to-br from-amber-100 to-rose-100 text-amber-700 dark:from-amber-500/20 dark:to-rose-500/15 dark:text-amber-300">
                                <GraduationCap className="size-16" strokeWidth={1} />
                            </div>
                        )}
                    </div>
                    <div className="flex min-w-0 flex-1 flex-col gap-3">
                        <div className="flex flex-wrap items-center gap-2">
                            <Tag className="!m-0">{course.kind === "camp" ? "训练营" : "课程"}</Tag>
                            <Tag className="!m-0">{course.category}</Tag>
                            {courseTags(course).map((tag) => (
                                <Tag key={tag.label} color={tag.tone === "gold" ? "gold" : undefined} className="!m-0">
                                    {tag.label}
                                </Tag>
                            ))}
                        </div>
                        <h1 className="text-2xl font-bold">{course.title}</h1>
                        <p className="text-sm text-muted-foreground">{course.summary}</p>
                        <div className="flex flex-wrap gap-x-5 gap-y-1 text-sm text-muted-foreground">
                            {dates && (
                                <span className="flex items-center gap-1.5">
                                    <CalendarDays className="size-4" />
                                    {course.kind === "camp" ? "开营时间" : "开课时间"}：{dates}
                                </span>
                            )}
                            <span className="flex items-center gap-1.5">
                                <Users className="size-4" />
                                {seatsLabel(course)}
                            </span>
                        </div>
                        <div className="mt-auto pt-2">
                            <EnrollButton course={course} />
                        </div>
                    </div>
                </section>

                {course.body && (
                    <section className="rounded-2xl border border-border bg-card p-5 text-sm leading-7 [&_h2]:mb-2 [&_h2]:mt-4 [&_h2]:text-base [&_h2]:font-semibold [&_li]:ml-5 [&_li]:list-disc [&_p]:my-2">
                        <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml>
                            {course.body}
                        </ReactMarkdown>
                    </section>
                )}

                {lessons.length > 0 && (
                    <section className="space-y-3 rounded-2xl border border-border bg-card p-5">
                        <h2 className="flex items-center justify-between text-base font-semibold">
                            课时（{lessons.length}）{course.locked && <span className="text-xs font-normal text-muted-foreground">报名后解锁全部视频</span>}
                        </h2>
                        <ol className="space-y-3">
                            {lessons.map((lesson, index) => (
                                <li key={index} className="rounded-lg border border-border p-3">
                                    <div className="flex items-center gap-2 text-sm">
                                        {course.locked ? <Lock className="size-4 text-muted-foreground" /> : <PlayCircle className="size-4 text-amber-600" />}
                                        <span className="min-w-0 flex-1 truncate">
                                            {index + 1}. {lesson.title}
                                        </span>
                                        {lesson.videoUrl && !isStoredFile(lesson.videoUrl) && (
                                            <a href={lesson.videoUrl} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-xs">
                                                观看
                                                <ExternalLink className="size-3" />
                                            </a>
                                        )}
                                        {!course.locked && !lesson.videoUrl && <span className="text-xs text-muted-foreground">视频待更新</span>}
                                    </div>
                                    {lesson.videoUrl && isStoredFile(lesson.videoUrl) && <video src={lesson.videoUrl} controls preload="none" className="mt-3 aspect-video w-full rounded-md bg-black" />}
                                </li>
                            ))}
                        </ol>
                    </section>
                )}
            </div>
        </div>
    );
}
