"use client";

import { useQuery } from "@tanstack/react-query";
import { Empty, Spin, Tag } from "antd";
import { Clock, GraduationCap, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { courseDates, courseTags, seatsLabel } from "@/lib/academy";
import { cn } from "@/lib/utils";
import { fetchAcademyCourses, fetchAcademyEnrollments, type AcademyCourse } from "@/services/api/academy";
import { useUserStore } from "@/stores/use-user-store";

type View = "all" | "mine" | "past";

function CourseCard({ course }: { course: AcademyCourse }) {
    const href = `/academy/${encodeURIComponent(course.slug)}`;
    const dates = courseDates(course);
    return (
        <div className="flex flex-col gap-3 rounded-xl border border-border bg-card p-4">
            <div className="flex gap-4">
                <Link href={href} className="size-24 shrink-0 overflow-hidden rounded-lg bg-muted">
                    {course.coverUrl ? (
                        <img src={course.coverUrl} alt="" className="size-full object-cover" />
                    ) : (
                        <div className="flex size-full items-center justify-center bg-gradient-to-br from-amber-100 to-rose-100 text-amber-700 dark:from-amber-500/20 dark:to-rose-500/15 dark:text-amber-300">
                            <GraduationCap className="size-10" strokeWidth={1.25} />
                        </div>
                    )}
                </Link>
                <div className="min-w-0 flex-1">
                    <div className="flex items-start gap-2">
                        <Link href={href} className="min-w-0 flex-1 truncate text-base font-semibold !text-foreground">
                            {course.title}
                        </Link>
                        <div className="flex shrink-0 gap-1">
                            {courseTags(course).map((tag) => (
                                <Tag key={tag.label} color={tag.tone === "gold" ? "gold" : undefined} className="!m-0">
                                    {tag.label}
                                </Tag>
                            ))}
                        </div>
                    </div>
                    <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{course.summary}</p>
                    {dates && <p className="mt-1 text-xs text-muted-foreground">{course.kind === "camp" ? "开营时间" : "开课时间"}：{dates}</p>}
                </div>
            </div>
            <div className="flex items-center justify-between gap-2">
                <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Users className="size-3.5" />
                    {seatsLabel(course)}
                </span>
                <Link href={href} className="rounded-full border border-border px-4 py-1 text-sm font-medium !text-foreground transition hover:bg-muted">
                    查看详情
                </Link>
            </div>
        </div>
    );
}

export default function AcademyPage() {
    const router = useRouter();
    const token = useUserStore((state) => state.token);
    const [view, setView] = useState<View>("all");
    const [category, setCategory] = useState("");
    const { data: courses = [], isLoading } = useQuery({ queryKey: ["academy", "courses"], queryFn: () => fetchAcademyCourses(), retry: false });
    const { data: enrollments = [] } = useQuery({ queryKey: ["academy", "enrollments", token], queryFn: () => fetchAcademyEnrollments(token), enabled: Boolean(token) && view === "mine", retry: false });
    const mine = new Set(enrollments.map((item) => item.courseId));
    const categories = [...new Set(courses.map((course) => course.category))];
    const shown = courses.filter((course) => (!category || course.category === category) && (view === "all" || (view === "past" ? course.state === "ended" : mine.has(course.id))));

    const toggle = (next: View) => {
        if (next === "mine" && !token) return router.push("/login?redirect=/academy");
        setView(view === next ? "all" : next);
    };

    return (
        <div className="thin-scrollbar h-full overflow-y-auto bg-stone-50 text-foreground dark:bg-background">
            <div className="mx-auto max-w-[1200px] space-y-5 px-4 py-5 md:px-6">
                <section className="relative overflow-hidden rounded-2xl bg-gradient-to-br from-stone-900 via-indigo-950 to-sky-900 px-6 py-10 text-white md:px-12 md:py-14">
                    <div className="absolute -right-16 -top-20 size-72 rounded-full bg-sky-400/20 blur-3xl" />
                    <div className="absolute bottom-0 right-24 size-48 rounded-full bg-amber-400/20 blur-3xl" />
                    <div className="relative max-w-xl space-y-3">
                        <p className="text-sm tracking-widest text-sky-200">无限画布 · 学院</p>
                        <h1 className="text-3xl font-bold leading-tight md:text-4xl">
                            把 <span className="text-sky-300">AI</span> 学会，把<span className="text-amber-300">作品</span>做成
                        </h1>
                        <p className="text-sm text-white/75 md:text-base">训练营带练 · 随到随学课程 · 学完就能用在画布里</p>
                    </div>
                </section>

                <div className="grid grid-cols-2 gap-3">
                    {(
                        [
                            ["mine", "我参与的", Users],
                            ["past", "往期活动", Clock],
                        ] as const
                    ).map(([key, label, Icon]) => (
                        <button
                            key={key}
                            type="button"
                            onClick={() => toggle(key)}
                            className={cn("flex h-12 items-center justify-center gap-2 rounded-xl border text-sm font-medium transition", view === key ? "border-foreground bg-foreground !text-background" : "border-border bg-card text-foreground hover:bg-muted")}
                        >
                            <Icon className="size-4" />
                            {label}
                        </button>
                    ))}
                </div>

                <div className="space-y-3">
                    <h2 className="text-lg font-semibold">{view === "mine" ? "我参与的活动" : view === "past" ? "往期活动" : "学院活动"}</h2>
                    <div className="thin-scrollbar flex gap-2 overflow-x-auto">
                        {["", ...categories].map((value) => (
                            <button
                                key={value || "all"}
                                type="button"
                                onClick={() => setCategory(value)}
                                className={cn("shrink-0 rounded-full px-4 py-1 text-sm transition", category === value ? "bg-foreground font-medium !text-background" : "text-muted-foreground hover:bg-muted hover:text-foreground")}
                            >
                                {value || "全部"}
                            </button>
                        ))}
                    </div>
                </div>

                {isLoading ? (
                    <Spin className="!mt-16 !block" />
                ) : shown.length ? (
                    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                        {shown.map((course) => (
                            <CourseCard key={course.id} course={course} />
                        ))}
                    </div>
                ) : (
                    <Empty className="!mt-16" description={view === "mine" ? "还没有报名任何活动" : "暂无活动"} />
                )}
            </div>
        </div>
    );
}
