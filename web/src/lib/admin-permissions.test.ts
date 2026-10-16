import { test } from "node:test";
import assert from "node:assert/strict";

import { allowedAdminMenus, canEnterAdmin, hasPermission, PERMISSIONS } from "@/lib/admin-permissions";

const menus = ["/admin/users", "/admin/credit-logs", "/admin/templates", "/admin/settings", "/admin/unknown"].map((key) => ({ key }));

test("local admins see every admin page", () => {
    assert.equal(allowedAdminMenus(menus, { isLocalAdmin: true, permissions: [] }).length, menus.length);
});

test("SSO staff see only the pages their codes open", () => {
    const support = { permissions: [PERMISSIONS.userRead, PERMISSIONS.orderRead] };
    assert.deepEqual(allowedAdminMenus(menus, support).map((menu) => menu.key), ["/admin/users", "/admin/credit-logs"]);
    // A page without a mapped code stays hidden from staff.
    assert.deepEqual(allowedAdminMenus(menus, { permissions: [PERMISSIONS.templateWrite] }).map((menu) => menu.key), []);
    assert.equal(hasPermission(support, PERMISSIONS.templatePublish), false);
});

test("the admin console opens for any canvas code and never for a bare role", () => {
    assert.equal(canEnterAdmin({ permissions: ["canvas.order.read"] }), true);
    assert.equal(canEnterAdmin({ permissions: ["user.read"] }), false);
    assert.equal(canEnterAdmin({ permissions: [] }), false);
    assert.equal(canEnterAdmin(null), false);
});
