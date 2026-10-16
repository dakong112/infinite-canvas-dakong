import { apiDelete, apiGet, apiPost, apiPut, compactApiParams } from "@/services/api/request";

export type AcademyLesson = { title: string; videoUrl: string };
/** Derived by the server: 报名中 / 已满 / 已结束. */
export type AcademyState = "open" | "full" | "ended";
export type AcademyCourse = {
    id: number;
    slug: string;
    kind: "camp" | "course";
    category: string;
    title: string;
    summary: string;
    coverUrl: string;
    /** Markdown; empty in the list. */
    body: string;
    /** null in the list; videoUrl is "" while locked. */
    lessons: AcademyLesson[] | null;
    startsAt: string | null;
    endsAt: string | null;
    enrollEndsAt: string | null;
    /** 0 = unlimited. */
    capacity: number;
    enrolledCount: number;
    memberOnly: boolean;
    feeCredits: number;
    memberFree: boolean;
    status: "draft" | "published";
    sort: number;
    state?: AcademyState;
    enrolled: boolean;
    locked: boolean;
    createdAt: string;
    updatedAt: string;
};
export type AcademyEnrollment = { id: number; courseId: number; userId: string; feeQuota: number; feeStatus: "free" | "charged"; createdAt: string; username?: string };

export const fetchAcademyCourses = (category = "") => apiGet<AcademyCourse[]>("/api/academy/courses", compactApiParams({ category }));
export const fetchAcademyCourse = (slug: string, token?: string) => apiGet<AcademyCourse>(`/api/academy/courses/${encodeURIComponent(slug)}`, undefined, token);
export const enrollAcademyCourse = (token: string, slug: string) => apiPost<AcademyEnrollment>(`/api/v1/academy/courses/${encodeURIComponent(slug)}/enroll`, {}, token);
export const fetchAcademyEnrollments = (token: string) => apiGet<AcademyEnrollment[]>("/api/v1/academy/enrollments", undefined, token);

export const fetchAdminAcademyCourses = (token: string) => apiGet<AcademyCourse[]>("/api/admin/academy/courses", undefined, token);
export const saveAdminAcademyCourse = (token: string, item: Partial<AcademyCourse>) =>
    item.id ? apiPut<AcademyCourse>(`/api/admin/academy/courses/${item.id}`, item, token) : apiPost<AcademyCourse>("/api/admin/academy/courses", item, token);
export const deleteAdminAcademyCourse = (token: string, id: number) => apiDelete<boolean>(`/api/admin/academy/courses/${id}`, token);
export const fetchAdminAcademyEnrollments = (token: string, id: number) => apiGet<AcademyEnrollment[]>(`/api/admin/academy/courses/${id}/enrollments`, undefined, token);
