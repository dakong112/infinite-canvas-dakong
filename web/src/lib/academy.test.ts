import { test } from "node:test";
import assert from "node:assert/strict";

import { courseDates, coursePrice, courseProblem, courseTags, lessonVideoProblem, seatsLabel } from "@/lib/academy";

test("course tags show 会员专属 and the closed states only", () => {
    assert.deepEqual(courseTags({ memberOnly: true, state: "open" }), [{ label: "会员专属", tone: "gold" }]);
    assert.deepEqual(courseTags({ memberOnly: false, state: "ended" }), [{ label: "已结束", tone: "default" }]);
    assert.deepEqual(
        courseTags({ memberOnly: true, state: "full" }).map((tag) => tag.label),
        ["会员专属", "已满"],
    );
    assert.deepEqual(courseTags({ memberOnly: false }), []);
});

test("seat labels follow the benchmark wording", () => {
    assert.equal(seatsLabel({ enrolledCount: 195, capacity: 200 }), "195人报名 / 限200人（97.5%）");
    assert.equal(seatsLabel({ enrolledCount: 100, capacity: 100 }), "100人报名 / 限100人（满员）");
    assert.equal(seatsLabel({ enrolledCount: 7, capacity: 0 }), "7人报名");
});

test("price display waives member-free courses for members only", () => {
    assert.deepEqual(coursePrice({ feeCredits: 0, memberFree: false }, false), { label: "免费", free: true });
    assert.deepEqual(coursePrice({ feeCredits: 99, memberFree: true }, true), { label: "会员免费", free: true });
    assert.deepEqual(coursePrice({ feeCredits: 99, memberFree: true }, false, "积分"), { label: "≈99 积分（会员免费）", free: false });
    assert.deepEqual(coursePrice({ feeCredits: 12.5, memberFree: false }, true, "点"), { label: "≈12.5 点", free: false });
});

test("date ranges and admin validation", () => {
    assert.equal(courseDates({ startsAt: "2026-10-12T04:00:00Z", endsAt: "2026-10-25T04:00:00Z" }), "10月12号-10月25号");
    assert.equal(courseDates({ startsAt: "2026-10-12T04:00:00Z", endsAt: "2026-10-12T08:00:00Z" }), "10月12号");
    assert.equal(courseDates({ startsAt: null, endsAt: null }), "");
    assert.equal(courseProblem({ slug: "ok-course", title: "标题", category: "AI技能提升" }), "");
    assert.match(courseProblem({ slug: "Bad", title: "t", category: "c" }), /标识/);
    assert.match(courseProblem({ slug: "ok", title: "t", category: "c", lessons: [{ title: " ", videoUrl: "" }] }), /课时/);
});

test("lesson videos accept MP4 / WebM up to 500MB", () => {
    assert.equal(lessonVideoProblem({ name: "a.mp4", type: "video/mp4", size: 500 * 1024 * 1024 }), "");
    assert.equal(lessonVideoProblem({ name: "a.webm", type: "", size: 1 }), "");
    assert.equal(lessonVideoProblem({ name: "a.mov", type: "video/quicktime", size: 1 }), "课时视频只支持 MP4 / WebM");
    assert.equal(lessonVideoProblem({ name: "a.mp4", type: "video/mp4", size: 500 * 1024 * 1024 + 1 }), "课时视频不能超过 500MB");
});
