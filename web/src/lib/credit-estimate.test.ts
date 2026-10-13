import { test } from "node:test";
import assert from "node:assert/strict";

import { estimateCredits } from "@/lib/credit-estimate";

const pricing = {
    groupRatio: 0.65,
    creditsPerUnit: 10,
    models: {
        "gpt-image-1": { quotaType: 1, modelPrice: 0.2 },
        "gpt-4o": { quotaType: 0, modelPrice: 0 },
    },
};

test("fixed-price model: price × group ratio × credits per unit × count", () => {
    // 0.2 × 0.65 × 10 × 3 = 3.9
    assert.equal(estimateCredits(pricing, "gpt-image-1", 3), 3.9);
});

test("token-billed and unknown models have no fixed estimate", () => {
    assert.equal(estimateCredits(pricing, "gpt-4o", 1), null);
    assert.equal(estimateCredits(pricing, "missing", 1), null);
    assert.equal(estimateCredits(undefined, "gpt-image-1", 1), null);
});

test("count is clamped to at least 1 and results round to 2 decimals", () => {
    assert.equal(estimateCredits(pricing, "gpt-image-1", 0), 1.3);
    assert.equal(estimateCredits({ ...pricing, groupRatio: 1, creditsPerUnit: 1, models: { m: { quotaType: 1, modelPrice: 0.0333 } } }, "m", 1), 0.03);
});
