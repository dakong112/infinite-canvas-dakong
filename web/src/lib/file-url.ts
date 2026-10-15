const STORED_FILE = /^\/api\/files\/([A-Za-z0-9-]+)\/content(?:\?(.*))?$/;

/** ID of a stored-file URL (/api/files/<id>/content) that carries no signature, else "". */
export function unsignedFileId(url: string) {
    const match = STORED_FILE.exec(url);
    return match && !new URLSearchParams(match[2] ?? "").has("sig") ? match[1] : "";
}

/** True for a stored-file URL whose signature is missing or expires within the hour (public files carry none). */
export function staleFileUrl(url: string, now = Date.now()) {
    const match = STORED_FILE.exec(url);
    if (!match) return false;
    const exp = Number(new URLSearchParams(match[2] ?? "").get("exp"));
    return !exp || exp * 1000 < now + 60 * 60 * 1000;
}
