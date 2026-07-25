/**
 * WS-22 e2e spec — i18n + theme toggles (runs by default).
 *
 * Pillar 12 mandates full i18n (en + fa, RTL) from day one. This spec
 * guards the two user-facing toggles that live in the header:
 *
 *   - locale toggle flips `<html lang>` + `<html dir>` (fa → RTL)
 *   - theme toggle flips the `dark` class on `<html>`
 *
 * Only needs the dashboard shell (no provider fakes), so it is not
 * gated behind a LAHIJAN_E2E_RUN_* flag.
 */
import { test, expect } from "@playwright/test";
import {
    registerAndLogin,
    resetPrefs,
    setLocaleViaUI,
    setThemeViaUI,
} from "./helpers";

async function htmlHasDarkClass(
    page: import("@playwright/test").Page,
): Promise<boolean> {
    return page.evaluate(() =>
        document.documentElement.classList.contains("dark"),
    );
}

test.describe("i18n + theme toggles", () => {
    test.beforeEach(async ({ page, request }) => {
        await registerAndLogin(page, request, "i18n");
        // Deterministic starting point: en + light.
        await resetPrefs(page);
    });

    test("switching to Persian flips the document to RTL", async ({ page }) => {
        await setLocaleViaUI(page, "fa");

        await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
        await expect(page.locator("html")).toHaveAttribute("lang", "fa");

        // And switching back restores LTR.
        await setLocaleViaUI(page, "en");
        await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
        await expect(page.locator("html")).toHaveAttribute("lang", "en");
    });

    test("the dark theme applies the `dark` class to <html>", async ({
        page,
    }) => {
        // Start from light (resetPrefs). Assert baseline has no dark class.
        await expect
            .poll(htmlHasDarkClass.bind(null, page), {
                message: "light baseline",
            })
            .toBe(false);

        await setThemeViaUI(page, "dark");
        await expect
            .poll(htmlHasDarkClass.bind(null, page), {
                message: "dark applied",
            })
            .toBe(true);

        await setThemeViaUI(page, "light");
        await expect
            .poll(htmlHasDarkClass.bind(null, page), {
                message: "dark removed",
            })
            .toBe(false);
    });
});
