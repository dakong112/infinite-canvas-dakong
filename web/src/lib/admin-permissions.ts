/** Permission codes new-api grants for this application; local admins hold all of them. */
export const PERMISSIONS = {
    templateRead: "canvas.template.read",
    templateWrite: "canvas.template.write",
    templatePublish: "canvas.template.publish",
    userRead: "canvas.user.read",
    orderRead: "canvas.order.read",
    settingWrite: "canvas.setting.write",
    promptWrite: "canvas.prompt.write",
    assetWrite: "canvas.asset.write",
    academyRead: "canvas.academy.read",
    academyWrite: "canvas.academy.write",
} as const;

type PermissionHolder = { permissions?: string[]; isLocalAdmin?: boolean } | null | undefined;

/** The code each admin page needs; it mirrors the RequirePermission groups in router/router.go. */
export const ADMIN_PAGE_PERMISSIONS: Record<string, string> = {
    "/admin/users": PERMISSIONS.userRead,
    "/admin/credit-logs": PERMISSIONS.orderRead,
    "/admin/ai-logs": PERMISSIONS.settingWrite,
    "/admin/prompts": PERMISSIONS.promptWrite,
    "/admin/skills": PERMISSIONS.promptWrite,
    "/admin/assets": PERMISSIONS.assetWrite,
    "/admin/templates": PERMISSIONS.templateRead,
    "/admin/academy": PERMISSIONS.academyRead,
    "/admin/settings": PERMISSIONS.settingWrite,
};

export function hasPermission(user: PermissionHolder, code: string) {
    return Boolean(user?.isLocalAdmin || user?.permissions?.includes(code));
}

export function canEnterAdmin(user: PermissionHolder) {
    return Boolean(user?.isLocalAdmin || user?.permissions?.some((code) => code.startsWith("canvas.")));
}

/** Keeps the menu entries whose page the user may open, in menu order. */
export function allowedAdminMenus<T extends { key: string }>(menus: T[], user: PermissionHolder) {
    return menus.filter((menu) => hasPermission(user, ADMIN_PAGE_PERMISSIONS[menu.key] ?? ""));
}
