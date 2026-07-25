/**
 * WS-22 e2e spec — auth journey.
 *
 * Covers the critical-path auth flows that run on every e2e invocation
 * (no LAHIJAN_E2E_RUN_* flag needed — these only exercise the auth
 * subsystem + the dashboard shell, no provider fakes required):
 *
 *   - register (API) → login (UI) → session cookie issued
 *   - wrong password → localized error, stays on /login
 *   - logged-in user visiting /login → redirected to /dashboard
 *   - logout → session cleared, bounced to /login
 *   - anonymous root `/` → redirected to /login
 *
 * If this spec passes, the harness wiring (sandbox stack, fakes, app,
 * dashboard preview) is sound; the other specs build on top.
 */
import { test, expect } from "@playwright/test";
import {
    loginViaUI,
    logoutViaUI,
    registerAndLogin,
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
        await page.getByTestId("login-email").fill(email);
        await page.getByTestId("login-password").fill("DefinitelyWrong!456");
        await page.getByTestId("login-submit").click();

        // The form's role="alert" error paragraph is the WS-06 + WS-18
        // contract for surfacing credential failures.
        await expect(page.getByTestId("login-error")).toBeVisible({
            timeout: 10_000,
        });
        // URL must NOT have changed (still on /login).
        await expect(page).toHaveURL(/\/login/);
    });

    test("a logged-in user visiting /login is redirected to /dashboard", async ({
        page,
        request,
    }) => {
        await registerAndLogin(page, request, "redir");

        // The login route's beforeLoad redirects authenticated users away.
        await page.goto("/login");
        await page.waitForURL("**/dashboard", { timeout: 15_000 });
        await expect(page).toHaveURL(/\/dashboard/);
    });

    test("logout clears the session and returns to /login", async ({
        page,
        request,
    }) => {
        await registerAndLogin(page, request, "logout");

        await logoutViaUI(page);
        await expect(page).toHaveURL(/\/login/);

        // The session cookie must be gone (or emptied) after logout.
        const cookies = await page.context().cookies();
        const session = cookies.find((c) => c.name === "lahijan_session");
        expect(session?.value ?? "", "session cookie cleared on logout").toBe(
            "",
        );
    });

    test("anonymous root `/` redirects to /login", async ({ page }) => {
        // No login, no cookie. The root index route's beforeLoad bounces
        // unauthenticated users to /login before paint.
        await page.goto("/");
        await page.waitForURL("**/login", { timeout: 15_000 });
        await expect(page).toHaveURL(/\/login/);
    });
});
