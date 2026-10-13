import { test } from "node:test";
import assert from "node:assert/strict";

import { canvasTaskStatus, isGenerationJobId, retentionDaysLeft, workStatusGroup } from "@/lib/works";

test("job statuses group into the three works counters", () => {
    assert.equal(workStatusGroup("succeeded"), "succeeded");
    assert.equal(workStatusGroup("queued"), "running");
    assert.equal(workStatusGroup("running"), "running");
    assert.equal(workStatusGroup("failed"), "failed");
    assert.equal(workStatusGroup("cancelled"), "failed");
});

test("job statuses map onto the image workbench task statuses", () => {
    assert.equal(canvasTaskStatus("succeeded"), "completed");
    assert.equal(canvasTaskStatus("running"), "processing");
    assert.equal(canvasTaskStatus("queued"), "queued");
    assert.equal(canvasTaskStatus("cancelled"), "failed");
});

test("retention days round up and never go negative", () => {
    const now = Date.parse("2026-10-14T00:00:00Z");
    assert.equal(retentionDaysLeft("2026-10-20T12:00:00Z", now), 7);
    assert.equal(retentionDaysLeft("2026-10-14T00:00:01Z", now), 1);
    assert.equal(retentionDaysLeft("2026-10-13T00:00:00Z", now), 0);
    assert.equal(retentionDaysLeft("not-a-date", now), 0);
});

test("generation job ids are recognised by prefix", () => {
    assert.equal(isGenerationJobId("genjob-1f2e"), true);
    assert.equal(isGenerationJobId("client_image_task_abc"), false);
});
