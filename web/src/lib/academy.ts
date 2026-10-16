import type { AcademyCourse } from "@/services/api/academy";

export const academyStateLabels = { open: "报名中", full: "已满", ended: "已结束" } as const;

/** Card tags in display order: 会员专属, then 已满 / 已结束 (报名中 shows no tag). */
export function courseTags(course: Pick<AcademyCourse, "memberOnly" | "state">) {
    const tags: { label: string; tone: "gold" | "default" }[] = [];
    if (course.memberOnly) tags.push({ label: "会员专属", tone: "gold" });
    if (course.state && course.state !== "open") tags.push({ label: academyStateLabels[course.state], tone: "default" });
    return tags;
}

/** "x人报名 / 限y人（z%）"; unlimited courses show only the count. */
export function seatsLabel(course: Pick<AcademyCourse, "enrolledCount" | "capacity">) {
    if (course.capacity <= 0) return `${course.enrolledCount}人报名`;
    if (course.enrolledCount >= course.capacity) return `${course.enrolledCount}人报名 / 限${course.capacity}人（满员）`;
    return `${course.enrolledCount}人报名 / 限${course.capacity}人（${Math.round((course.enrolledCount / course.capacity) * 1000) / 10}%）`;
}

/** Price shown on the enroll button: 免费, 会员免费, or "≈N 积分" (members of member-free courses pay nothing). */
export function coursePrice(course: Pick<AcademyCourse, "feeCredits" | "memberFree">, member: boolean, creditName = "积分") {
    if (course.feeCredits <= 0) return { label: "免费", free: true };
    if (course.memberFree && member) return { label: "会员免费", free: true };
    return { label: `≈${course.feeCredits} ${creditName}${course.memberFree ? "（会员免费）" : ""}`, free: false };
}

/** "10月12号-10月25号" style date range of a course, "" when it has no start date. */
export function courseDates(course: Pick<AcademyCourse, "startsAt" | "endsAt">) {
    const day = (value: string) => {
        const date = new Date(value);
        return `${date.getMonth() + 1}月${date.getDate()}号`;
    };
    if (!course.startsAt) return "";
    return course.endsAt && day(course.endsAt) !== day(course.startsAt) ? `${day(course.startsAt)}-${day(course.endsAt)}` : day(course.startsAt);
}

/** Why the admin course can't be saved, or "" (mirrors service/academy.go normalizeAcademyCourse). */
export function courseProblem(item: Partial<AcademyCourse>) {
    const runes = (value?: string) => [...(value ?? "").trim()].length;
    if (!/^[a-z0-9-]{2,64}$/.test((item.slug ?? "").trim())) return "标识只能使用 2-64 位小写字母、数字和 -";
    if (!runes(item.title) || runes(item.title) > 100) return "标题需为 1-100 字";
    if (!runes(item.category) || runes(item.category) > 16) return "分类需为 1-16 字";
    if (runes(item.summary) > 300) return "简介不能超过 300 字";
    if (item.startsAt && item.endsAt && item.endsAt < item.startsAt) return "结束时间不能早于开始时间";
    if ((item.lessons ?? []).some((lesson) => !runes(lesson?.title) || runes(lesson?.title) > 100)) return "课时标题需为 1-100 字";
    return "";
}
