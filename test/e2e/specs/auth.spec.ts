/**
 * WS-22 e2e spec — auth journey.
 *
 * Covers: register → (email verification skipped via
 * LAHIJAN_AUTH_EMAILVERIFICATIONREQUIRED=false in the e2e env) → login.
 *
 * The register-then-login round-trip is the smallest user journey that
 * exercises the full stack (browser → dashboard → API → DB → auth
 * subsystem → cookie session). If this spec passes, the harness wiring
 * (sandbox stack, fakes, app, dashboard preview) is sound; the other
 * specs build on top.
 */
import { test, expect } from "@playwright/test";
import {
    loginViaUI,
    registerUser,
    STRONG_PASSWORD,
    uniqueEmail,
} from "./helpers";

test.describe("auth journey", () => {
    test("register then login via the dashboard", async ({ page, request }) => {
        const email = uniqueEmail("auth");

        // Register via the API (the dashboard doesn't ship a /register
        // page yet — the WS-18 dashboard assumes the operator-provisioned
        // flow for MVP). This still exercises the full backend: WS-06 auth
        // pipeline, password hashing, session cookie issuance, audit row.
        await registerUser(request, email);

        // Now log in via the UI — the actual user-facing journey.
        await loginViaUI(page, email, STRONG_PASSWORD);

        // Sanity: the dashboard shell renders with the user authenticated.
        // The presence of a session cookie + the URL change is the proof.
        const cookies = await page.context().cookies();
        const sessionCookie = cookies.find((c) => c.name === "lahijan_session");
        expect(sessionCookie, "session cookie set after login").toBeTruthy();
    });

    test("login with wrong password surfaces the localized error", async ({
        page,
        request,
    }) => {
        const email = uniqueEmail("badpw");
        await registerUser(request, email);

        await page.goto("/login");
        await page.locator('input[name="email"]').fill(email);
        await page
            .locator('input[name="password"]')
            .fill("DefinitelyWrong!456");
        await page.locator('button[type="submit"]').click();

        // The form's role="alert" error paragraph is the WS-06 + WS-18
        // contract for surfacing credential failures.
        const alert = page.locator('[role="alert"]');
        await expect(alert).toBeVisible({ timeout: 10_000 });
        // URL must NOT have changed (still on /login).
        await expect(page).toHaveURL(/\/login/);
    });
});
