import { apiDelete, apiGet, apiPost } from "@/services/api/request";
import type { UserWebDAVStorageProvider } from "@/services/image-storage";

export type RegisteredStorageObject = {
    url: string;
    storageKey: string;
    bytes: number;
    mimeType: string;
};

export type StorageObjectInfo = {
    id: string;
    objectKey: string;
    publicUrl: string;
    /** Content URL, signed when the signed-in user owns the file. */
    url: string;
    mimeType: string;
    bytes: number;
    direct: boolean;
};

export function getStorageObjectInfo(id: string) {
    return apiGet<StorageObjectInfo>(`/api/files/${encodeURIComponent(id)}`);
}

/** Content URLs (id -> url) of stored files, signed for the ones the user may read. */
export function signFileUrls(token: string, ids: string[]) {
    return apiPost<Record<string, string>>("/api/files/sign", { ids }, token);
}

export function registerDirectStorageObject(
    token: string,
    payload: { provider: UserWebDAVStorageProvider; objectKey: string; mimeType: string; bytes: number },
) {
    return apiPost<RegisteredStorageObject>("/api/v1/files/direct", payload, token);
}

export function deleteDirectStorageObjectRecord(token: string, id: string) {
    return apiDelete<boolean>(`/api/v1/files/${encodeURIComponent(id)}/record`, token);
}
