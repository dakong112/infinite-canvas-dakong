// Ported from new-api web/src/features/auth/lib/password-encryption.ts (AGPL-3.0, QuantumNous).
// Only the Web Crypto path is kept: production runs on HTTPS.

export type EncryptedPassword = { password_encrypted: string; encryption_key_id: string };

export async function encryptPassword(newapiUrl: string, password: string): Promise<EncryptedPassword> {
    const response = await fetch(`${newapiUrl}/api/user/login/encryption-key`);
    const body = (await response.json()) as { success: boolean; data?: { kid: string; public_key: string } };
    if (!body.success || !body.data?.kid || !body.data.public_key) {
        throw new Error("登录加密密钥不可用");
    }
    const { kid, public_key: publicKeyPEM } = body.data;
    if (typeof globalThis.crypto?.subtle === "undefined") {
        throw new Error("当前环境不支持加密登录，请使用 HTTPS 访问");
    }
    const publicKey = await crypto.subtle.importKey("spki", pemToDER(publicKeyPEM), { name: "RSA-OAEP", hash: "SHA-256" }, false, ["encrypt"]);
    const plaintext = new TextEncoder().encode(password);
    const algorithm = publicKey.algorithm as RsaHashedKeyAlgorithm;
    if (plaintext.byteLength <= algorithm.modulusLength / 8 - 66) {
        const ciphertext = await crypto.subtle.encrypt({ name: "RSA-OAEP" }, publicKey, plaintext);
        return { password_encrypted: toBase64(ciphertext), encryption_key_id: kid };
    }
    const secret = crypto.getRandomValues(new Uint8Array(32));
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const aesKey = await crypto.subtle.importKey("raw", secret, "AES-GCM", false, ["encrypt"]);
    const [wrappedKey, ciphertext] = await Promise.all([
        crypto.subtle.encrypt({ name: "RSA-OAEP", label: new TextEncoder().encode("password-v2") }, publicKey, secret),
        crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce, additionalData: new TextEncoder().encode(`password-v2:${kid}`) }, aesKey, plaintext),
    ]);
    return {
        password_encrypted: ["v2", toBase64(wrappedKey), toBase64(nonce.buffer), toBase64(ciphertext)].join("."),
        encryption_key_id: kid,
    };
}

function pemToDER(pem: string): ArrayBuffer {
    const body = pem.replace("-----BEGIN PUBLIC KEY-----", "").replace("-----END PUBLIC KEY-----", "").replaceAll(/\s+/g, "");
    const binary = atob(body);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
    return bytes.buffer;
}

function toBase64(buffer: ArrayBuffer): string {
    let binary = "";
    for (const byte of new Uint8Array(buffer)) binary += String.fromCharCode(byte);
    return btoa(binary);
}
