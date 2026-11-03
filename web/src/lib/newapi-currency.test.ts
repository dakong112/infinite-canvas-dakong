import { test } from "node:test";
import assert from "node:assert/strict";

import { displayToUsd, formatNewapiAmount, formatUsdAmount, usdToDisplay, type BillingDisplay } from "@/lib/newapi-currency";

const usd: BillingDisplay = { mode: "USD", symbol: "$", rate: 1, quotaPerUnit: 500000 };
const cny: BillingDisplay = { mode: "CNY", symbol: "¥", rate: 7.3, quotaPerUnit: 500000 };
const custom: BillingDisplay = { mode: "CUSTOM", symbol: "点", rate: 10, quotaPerUnit: 500000 };
const tokens: BillingDisplay = { mode: "TOKENS", symbol: "", rate: 1, quotaPerUnit: 500000 };

test("USD: 2 decimals from 1 up, 4 below, thousands grouped", () => {
    assert.equal(formatNewapiAmount(5000000, usd), "$10");
    assert.equal(formatUsdAmount(1234.567, usd), "$1,234.57");
    assert.equal(formatUsdAmount(0.12345, usd), "$0.1235");
    assert.equal(formatUsdAmount(0, usd), "$0");
    assert.equal(formatUsdAmount(-2.5, usd), "-$2.5");
});

test("CNY converts with the exchange rate", () => {
    assert.equal(formatNewapiAmount(5000000, cny), "¥73");
    assert.equal(formatUsdAmount(0.04, cny), "¥0.292");
});

test("CUSTOM puts the symbol before the number with a space", () => {
    assert.equal(formatNewapiAmount(1500000, custom), "点 30");
});

test("TOKENS shows raw quota, abbreviated from 1000", () => {
    assert.equal(formatNewapiAmount(1500000, tokens), "1500k");
    assert.equal(formatNewapiAmount(1250, tokens), "1.3k");
    assert.equal(formatNewapiAmount(800, tokens), "800");
    assert.equal(formatUsdAmount(0.002, tokens), "1k");
});

test("tiny positive amounts read as below the smallest shown unit", () => {
    assert.equal(formatUsdAmount(0.00001, usd), "<$0.0001");
    assert.equal(formatUsdAmount(0.000001, custom), "<点 0.0001");
});

test("admin inputs convert to new-api units and back", () => {
    assert.equal(usdToDisplay(2, cny), 14.6);
    assert.equal(displayToUsd(14.6, cny), 2);
    assert.equal(usdToDisplay(0.01, tokens), 5000);
});
