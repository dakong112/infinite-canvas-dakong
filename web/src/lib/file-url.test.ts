import { test } from "node:test";
import assert from "node:assert/strict";

import { staleFileUrl, unsignedFileId } from "@/lib/file-url";

test("unsigned stored-file URLs give their ID", () => {
    assert.equal(unsignedFileId("/api/files/a-1/content"), "a-1");
    assert.equal(unsignedFileId("/api/files/a-1/content?direct=1"), "a-1");
    assert.equal(unsignedFileId("/api/files/a-1/content?exp=1&sig=x"), "");
    assert.equal(unsignedFileId("https://cdn.example.com/a.png"), "");
});

test("stored-file URLs without a signature or close to expiry are stale", () => {
    const now = 1_000_000_000_000;
    const exp = (ms: number) => `/api/files/a/content?exp=${Math.floor((now + ms) / 1000)}&sig=x`;
    assert.equal(staleFileUrl("/api/files/a/content", now), true);
    assert.equal(staleFileUrl(exp(30 * 60 * 1000), now), true);
    assert.equal(staleFileUrl(exp(24 * 60 * 60 * 1000), now), false);
    assert.equal(staleFileUrl("https://cdn.example.com/a.png", now), false);
});
