/**
 * Helpers shared by the WS-22 Playwright specs.
 *
 * Every spec creates its own tenant + user via the Lahijan public API
 * (POST /api/v1/auth/register) and cleans up by destroying the session
 * cookies at the end. Tests do NOT share state across files (the
 * Playwright workers config is 1 anyway).
 *
 * Selectors use `data-testid` exclusively per the testing skill: text
 * breaks under i18n (the suite runs both en + fa). The matching
 * `data-testid` coverage on the dashboard ships with the WS-22b
 * follow-up.
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

/** The dashboard base URL — the Vite preview server. */
export const WEB_BASE_URL =
    process.env.LAHIJAN_E2E_BASE_URL ?? "http://127.0.0.1:4173";

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
 * Selectors target the `data-testid` attributes added in WS-22b, which
 * are i18n-stable across the en + fa runs.
 */
export async function loginViaUI(
    page: Page,
    email: string,
    password: string,
): Promise<void> {
    await page.goto("/login");
    await page.getByTestId("login-email").fill(email);
    await page.getByTestId("login-password").fill(password);
    await page.getByTestId("login-submit").click();
    // The dashboard's post-login destination is /dashboard. Wait for the
    // URL change as the success signal (the header email render depends
    // on a TanStack Query load that races with the navigation).
    await page.waitForURL("**/dashboard", { timeout: 15_000 });
}

/**
 * Register a fresh user via the API and immediately log in through the
 * dashboard UI. The combination most specs want as a starting point.
 */
export async function registerAndLogin(
    page: Page,
    request: APIRequestContext,
    prefix = "user",
): Promise<{ email: string; password: string }> {
    const email = uniqueEmail(prefix);
    const creds = await registerUser(request, email);
    await loginViaUI(page, creds.email, creds.password);
    return creds;
}

/**
 * Sign out through the header user menu. Waits for the redirect back to
 * /login as the success signal.
 */
export async function logoutViaUI(page: Page): Promise<void> {
    await page.getByTestId("user-menu-trigger").click();
    await page.getByTestId("user-menu-logout").click();
    await page.waitForURL("**/login", { timeout: 15_000 });
}

/**
 * Click a sidebar nav entry by its `nav-*` data-testid and wait for the
 * resulting URL to settle. `navTestId` is the full testid, e.g.
 * `nav-compute` or `nav-settings-security`.
 */
export async function navigateViaSidebar(
    page: Page,
    navTestId: string,
    expectedUrlPart: string,
): Promise<void> {
    await page.getByTestId(navTestId).click();
    await page.waitForURL(`**/${expectedUrlPart}**`, { timeout: 15_000 });
}

/** Open the global Command+K palette via its header trigger. */
export async function openCommandPalette(page: Page): Promise<void> {
    await page.getByTestId("command-palette-trigger").click();
    await expect(page.getByTestId("command-palette-input")).toBeVisible();
}

/**
 * Flip the UI locale through the header toggle. Asserts the `<html dir>`
 * flips so the caller gets an RTL/LTR signal for free.
 */
export async function setLocaleViaUI(
    page: Page,
    code: "en" | "fa",
): Promise<void> {
    await page.getByTestId("locale-toggle").click();
    await page.getByTestId(`locale-option-${code}`).click();
    await expect(page.locator("html")).toHaveAttribute(
        "dir",
        code === "fa" ? "rtl" : "ltr",
    );
}

/** Pick a theme through the header toggle. */
export async function setThemeViaUI(
    page: Page,
    theme: "light" | "dark" | "system",
): Promise<void> {
    await page.getByTestId("theme-toggle").click();
    await page.getByTestId(`theme-option-${theme}`).click();
}

/** Reset the locale + theme to deterministic defaults for a spec. */
export async function resetPrefs(page: Page): Promise<void> {
    await setLocaleViaUI(page, "en").catch(() => undefined);
    await setThemeViaUI(page, "light").catch(() => undefined);
}
