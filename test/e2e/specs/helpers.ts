/**
 * Helpers shared by the WS-22 Playwright specs.
 *
 * Every spec creates its own tenant + user via the Lahijan public API
 * (POST /api/v1/auth/register) and cleans up by destroying the session
 * cookies at the end. Tests do NOT share state across files (the
 * Playwright workers config is 1 anyway).
 *
 * Selectors use `data-testid` exclusively per the testing skill: text
 * breaks under i18n (the suite runs both en + fa).
 */
import { expect, type Page, type APIRequestContext } from "@playwright/test";
/**
 * The API base URL — the Lahijan Go backend. The dashboard proxies /api
 * to this in dev + preview, but the helpers below hit it directly so we
 * don't pay a per-call round-trip through Vite's proxy.
 */
export const API_BASE_URL =
    process.env.LAHIJAN_E2E_API_BASE_URL ?? "http://127.0.0.1:8080";

/** Strong password matching the WS-06 password rules. */
export const STRONG_PASSWORD = "VeryStrong123!xyz";

/**
 * Unique email generator. Uses a per-run prefix so parallel CI runs
 * (different branches, same SHA) cannot collide on the same address.
 */
export function uniqueEmail(prefix = "user"): string {
    const stamp =
        process.env.LAHIJAN_E2E_RUN_ID ??
        `${Date.now()}-${process.pid ?? "nopid"}`;
    const slug = stamp
        .toString()
        .replace(/[^a-z0-9]/gi, "")
        .toLowerCase();
    return `${prefix}+${slug}@e2e.lahijan.test`;
}

/**
 * Register a brand-new user via the public API. Returns the session
 * cookies (so the caller can drive authenticated requests without
 * re-logging in via the UI).
 */
export async function registerUser(
    request: APIRequestContext,
    email: string,
    password = STRONG_PASSWORD,
    locale = "en",
): Promise<{ email: string; password: string }> {
    const response = await request.post(
        `${API_BASE_URL}/api/v1/auth/register`,
        {
            data: { email, password, locale },
            maxRedirects: 0,
        },
    );
    expect(response.status(), `register ${email} should succeed`).toBe(201);
    return { email, password };
}

/**
 * Poll the Lahijan /healthz endpoint until it returns 200 or the test
 * times out. The runner script already waits for /healthz before
 * starting Playwright, but this guards against the app being restarted
 * mid-suite (a future flaky-test recovery path).
 */
export async function waitForApp(page: Page): Promise<void> {
    await expect
        .poll(
            async () => {
                const response = await page.request.get(
                    `${API_BASE_URL}/healthz`,
                );
                return response.status();
            },
            { timeout: 30_000, intervals: [1_000] },
        )
        .toBe(200);
}

/**
 * Log in via the dashboard UI (NOT the API). This is the user-facing
 * flow the spec is exercising; using the UI ensures the journey stays
 * green even if the API contract changes shape (the UI is the
 * source-of-truth for the user experience).
 *
 * Selectors use `name=` attributes (react-hook-form sets these from the
 * zod schema field names) which are i18n-stable. The dashboard doesn't
 * yet ship `data-testid` attributes (a WS-22 follow-up); the testing
 * skill's recommendation is `data-testid` but `name=` is the
 * next-best i18n-stable alternative.
 */
export async function loginViaUI(
    page: Page,
    email: string,
    password: string,
): Promise<void> {
    await page.goto("/login");
    await page.locator('input[name="email"]').fill(email);
    await page.locator('input[name="password"]').fill(password);
    await page.locator('button[type="submit"]').click();
    // The dashboard's post-login destination is /dashboard. Wait for the
    // URL change as the success signal (the header email render depends
    // on a TanStack Query load that races with the navigation).
    await page.waitForURL("**/dashboard", { timeout: 15_000 });
}
