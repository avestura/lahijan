/**
 * WS-22 e2e spec — dashboard shell + navigation (runs by default).
 *
 * Exercises the cross-cutting chrome that every authenticated page
 * shares: the sidebar nav, the header, and the global Command+K palette.
 * These only need the app + a logged-in user (no provider fakes), so
 * the spec is NOT gated behind a LAHIJAN_E2E_RUN_* flag.
 *
 * Assertions are URL + `data-testid` based so they survive the en/fa
 * locale runs and don't depend on any list API returning data.
 */
import { test, expect } from "@playwright/test";
import {
    navigateViaSidebar,
    openCommandPalette,
    registerAndLogin,
} from "./helpers";

test.describe("dashboard shell + navigation", () => {
    test("sidebar navigates between the primary areas", async ({
        page,
        request,
    }) => {
        await registerAndLogin(page, request, "nav");

        // Dashboard is the post-login landing.
        await expect(page.getByTestId("page-dashboard")).toBeVisible();
        await expect(page).toHaveURL(/\/dashboard/);

        // Each sidebar entry should change the URL + render its page root.
        for (const [testId, urlPart, pageTestId] of [
            ["nav-compute", "compute", "page-compute"],
            ["nav-dns", "dns", "page-dns"],
            ["nav-storage", "storage", "page-storage"],
            ["nav-settings", "settings", "page-settings-profile"],
            ["nav-dashboard", "dashboard", "page-dashboard"],
        ] as const) {
            await navigateViaSidebar(page, testId, urlPart);
            await expect(page.getByTestId(pageTestId)).toBeVisible();
        }
    });

    test("the command palette jumps to an area", async ({ page, request }) => {
        await registerAndLogin(page, request, "cmdk");

        await openCommandPalette(page);
        await page.getByTestId("command-palette-input").fill("dns");
        // cmdk filters to the matching destination; click it.
        await page.getByTestId("command-palette-item-dns").click();
        await page.waitForURL("**/dns", { timeout: 15_000 });
        await expect(page).toHaveURL(/\/dns/);
    });

    test("the header renders for an authenticated user", async ({
        page,
        request,
    }) => {
        await registerAndLogin(page, request, "header");
        await expect(page.getByTestId("app-header")).toBeVisible();
        await expect(page.getByTestId("command-palette-trigger")).toBeVisible();
        // The user menu trigger is the avatar button.
        await expect(page.getByTestId("user-menu-trigger")).toBeVisible();
    });
});
