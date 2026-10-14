import { test } from "node:test";
import assert from "node:assert/strict";

import { defaultCustomCounts, setSlotEstimate, slotTypes, structureError, structureTotal } from "@/lib/ecommerce-options";

test("the eight standard image types are offered and the default custom layout is valid", () => {
    assert.equal(slotTypes.length, 8);
    assert.equal(structureTotal(defaultCustomCounts), 8);
    assert.equal(structureError(defaultCustomCounts), "");
});

test("custom structures need 7 to 15 images", () => {
    assert.equal(structureError({ white: 1, brand: 1 }), "至少 7 张，当前 2 张");
    assert.equal(structureError({ white: 5, brand: 5, scene: 5, craft: 1 }), "最多 15 张，当前 16 张");
    assert.equal(structureTotal({ white: -2, brand: 2.7, unknown: 9 }), 2);
});

test("slot estimate follows the preset, structure and SKU count", () => {
    assert.equal(setSlotEstimate("detail", "smart", defaultCustomCounts, 3), 7);
    assert.equal(setSlotEstimate("set", "smart", defaultCustomCounts, 2), 11);
    assert.equal(setSlotEstimate("set", "custom", { white: 1, brand: 6 }, 0), 7);
});
