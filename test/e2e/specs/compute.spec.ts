/**
 * WS-22 e2e spec — compute journey.
 *
 * Covers the primary UI journey: open the create wizard → pick image →
 * set size → review → submit → land on the instance detail page. The
 * lifecycle (start/stop/delete) is driven through the API afterwards so
 * the spec also cleans up after itself.
 *
 * Gated behind LAHIJAN_E2E_RUN_COMPUTE: needs the Incus fake (wired by
 * the harness) + a non-empty image catalog. Enable in the full
 * `make test-e2e` stack.
 */
import { test, expect } from "@playwright/test";
import {
    API_BASE_URL,
    navigateViaSidebar,
    registerAndLogin,
    tenantHeaders,
} from "./helpers";

test.describe("compute journey", () => {
    test("create an instance via the wizard, then clean up via the API", async ({
        page,
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_COMPUTE,
            "compute spec needs the Incus fake + an image catalog; " +
                "gated behind LAHIJAN_E2E_RUN_COMPUTE=1",
        );

        // Instance create waits on a real image download against a live daemon.
        test.setTimeout(300_000);
        await registerAndLogin(page, request, "compute");
        await navigateViaSidebar(page, "nav-compute", "compute");
        await expect(page.getByTestId("page-compute")).toBeVisible();

        // Open the create wizard.
        await page.getByTestId("new-instance-button").click();
        await page.waitForURL("**/compute/new", { timeout: 15_000 });

        // Step 1 — image: the field is a free-text alias input (the catalog
        // dropdown and stream browser are shortcuts); alpine/3.22 is in the
        // featured catalog every tenant is seeded with.
        await page.getByTestId("create-instance-image").fill("alpine/3.22");
        await page.getByTestId("create-instance-next").click();

        // Step 2 — size: name + defaults are fine; just advance.
        const name = `e2e-${Date.now()}`;
        await page.getByTestId("create-instance-name").fill(name);
        await page.getByTestId("create-instance-next").click();

        // Step 3 — review: submit.
        await page.getByTestId("create-instance-submit").click();

        // On success the wizard navigates to the new instance's detail page.
        // A real daemon may download the image first; the fake answers at once.
        await page.waitForURL(/\/compute\/[0-9a-fA-F-]+/, { timeout: 240_000 });
        expect(page.url(), "navigated to the new instance detail").toMatch(
            /\/compute\/[0-9a-fA-F-]+/,
        );

        // Clean up + sanity-check the lifecycle via the API. The instance
        // id is the last path segment.
        const instanceId = page.url().split("/").pop() ?? "";
        expect(instanceId, "parsed instance id from URL").toBeTruthy();

        const headers = await tenantHeaders(request);
        const start = await request.post(
            `${API_BASE_URL}/api/v1/compute/instances/${instanceId}/start`,
            { headers },
        );
        expect(start.status()).toBe(200);

        const stop = await request.post(
            `${API_BASE_URL}/api/v1/compute/instances/${instanceId}/stop`,
            { headers },
        );
        expect(stop.status()).toBe(200);

        const del = await request.delete(
            `${API_BASE_URL}/api/v1/compute/instances/${instanceId}`,
            { headers },
        );
        expect(del.status()).toBe(204);
    });
});
