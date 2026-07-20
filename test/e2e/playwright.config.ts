/**
 * Playwright config for the Lahijan WS-22 e2e suite.
 *
 * Driven by scripts/run-e2e.sh, which brings up the test sandbox
 * (Postgres + SeaweedFS) + the in-process Incus + PowerDNS fakes + the
 * Lahijan app + the dashboard preview, then runs this config against the
 * dashboard URL.
 *
 * Flaky-rate policy (WS-22 DoD item):
 *   - CI (forced) retries: 2 on flake, 0 by default locally
 *   - The on-failure trace + video is the flaky-rate monitor. Inspect
 *     playwright-report/index.html after any CI run; a test that fails
 *     once and passes on retry is recorded as flaky in the report.
 *   - The expectation is zero flakies per WS; a regression is a P1.
 */
import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.LAHIJAN_E2E_BASE_URL ?? "http://127.0.0.1:4173";
const isCI = !!process.env.CI;

export default defineConfig({
    testDir: "./specs",
    timeout: 60_000, // 60s per test (dashboard loads can be slow on cold CI)
    expect: {
        timeout: 10_000, // 10s for `expect(locator).toBeVisible()` etc.
    },
    fullyParallel: false, // the sandbox is a single fixture; serial keeps it simple
    forbidOnly: !!isCI,
    retries: isCI ? 2 : 0,
    workers: 1, // single worker: one browser, one session, one fixture
    reporter: [
        ["html", { outputFolder: "playwright-report", open: "never" }],
        ["list"],
    ],
    use: {
        baseURL,
        actionTimeout: 10_000,
        navigationTimeout: 15_000,
        trace: "retain-on-failure", // keep trace for failures (the flaky-rate signal)
        video: "retain-on-failure",
        screenshot: "only-on-failure",
        storageState: undefined, // each test logs in fresh
    },
    projects: [
        {
            name: "chromium",
            use: { ...devices["Desktop Chrome"] },
        },
    ],
});
