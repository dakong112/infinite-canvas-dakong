const SPACE_KEY = "infinite-canvas:space";

/** The space this browser works in ("" personal, "team:<id>"); local caches are kept per space. */
export function currentSpaceId() {
    try {
        return typeof window === "undefined" ? "" : localStorage.getItem(SPACE_KEY) || "";
    } catch {
        return "";
    }
}

/** Local cache key of one space: the personal space keeps the original key, so existing local data stays personal. */
export function spaceStorageKey(base: string, spaceId = currentSpaceId()) {
    return spaceId ? `${base}:${spaceId}` : base;
}

/** X-Space-Id value: the server refuses a /api/v1 request made for another space than the account's. */
export function spaceHeader(spaceId = currentSpaceId()) {
    return spaceId || "personal";
}

/** Remembers the account's space; true when it changed, and the page must reload to load that space's caches. */
export function rememberSpace(spaceId: string) {
    if (currentSpaceId() === spaceId) return false;
    try {
        localStorage.setItem(SPACE_KEY, spaceId);
        return true;
    } catch {
        return false;
    }
}
