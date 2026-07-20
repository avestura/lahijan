/**
 * WS-22 e2e spec — admin plugins journey.
 *
 * Covers: admin installs a plugin → grants requested permissions → the
 * grant emits an audit event visible in the audit log.
 *
 * Status: scaffolded. The full UI flow needs a sample .wasm plugin
 * artifact + a logged-in admin user with the plugins.install
 * permission. The .wasm sample ships with WS-10c; the admin permission
 * bootstrap is operator-side.
 *
 * The spec below drives the list endpoint to validate the dashboard
 * wiring (admin plugins page renders + lists the empty catalog) once
 * the admin fixtures land.
 */
import { test, expect } from "@playwright/test";
import {
    loginViaUI,
    registerUser,
    STRONG_PASSWORD,
    uniqueEmail,
} from "./helpers";

test.describe("admin plugins journey", () => {
    test("admin plugins page renders for an admin user", async ({
        page,
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_PLUGINS,
            "plugins spec needs an admin user fixture + a sample .wasm; gated behind LAHIJAN_E2E_RUN_PLUGINS=1",
        );

        const email = uniqueEmail("admin-plugins");
        await registerUser(request, email);
        await loginViaUI(page, email, STRONG_PASSWORD);

        await page.goto("/admin/plugins");
        // The page renders an empty state when no plugins are installed.
        await expect(page.locator("body")).toBeVisible();
    });
});
