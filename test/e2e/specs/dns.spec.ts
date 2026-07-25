/**
 * WS-22 e2e spec — DNS journey.
 *
 * Covers the primary UI journey: open the "New zone" dialog → fill the
 * zone name → submit → the zone appears in the list. Then adds an A
 * record via the API and deletes the zone for cleanup.
 *
 * Gated behind LAHIJAN_E2E_RUN_DNS: needs the PowerDNS fake (wired by
 * the harness). Enable in the full `make test-e2e` stack.
 */
import { test, expect } from "@playwright/test";
import { API_BASE_URL, navigateViaSidebar, registerAndLogin } from "./helpers";

test.describe("dns journey", () => {
    test("create a zone via the dialog, add a record, clean up", async ({
        page,
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_DNS,
            "dns spec needs the PowerDNS fake; gated behind LAHIJAN_E2E_RUN_DNS=1",
        );

        await registerAndLogin(page, request, "dns");
        await navigateViaSidebar(page, "nav-dns", "dns");
        await expect(page.getByTestId("page-dns")).toBeVisible();

        // Open the create dialog + fill the zone name.
        await page.getByTestId("new-zone-button").click();
        await expect(page.getByTestId("create-zone-dialog")).toBeVisible();

        const zoneName = `e2e-${Date.now()}.test`;
        await page.getByTestId("create-zone-name").fill(zoneName);
        await page.getByTestId("create-zone-submit").click();

        // The dialog closes + the list refreshes; the zone name shows up.
        await expect(page.getByTestId("create-zone-dialog")).toBeHidden({
            timeout: 15_000,
        });
        await expect(page.getByTestId("page-dns")).toContainText(zoneName);

        // Look up the created zone via the API (the UI doesn't expose the
        // id on the row in a stable way yet), add an A record, then delete.
        const zonesResp = await request.get(`${API_BASE_URL}/api/v1/dns/zones`);
        expect(zonesResp.status()).toBe(200);
        const zones = (await zonesResp.json()) as Array<{
            id: string;
            name: string;
        }>;
        const zone = zones.find((z) => z.name === zoneName);
        expect(zone, "created zone is listable via the API").toBeTruthy();
        const zoneId = zone?.id ?? "";

        const createRecord = await request.post(
            `${API_BASE_URL}/api/v1/dns/zones/${zoneId}/records`,
            {
                data: {
                    name: `www.${zoneName}`,
                    type: "A",
                    content: "203.0.113.42",
                    ttl: 300,
                },
            },
        );
        expect(createRecord.status()).toBe(201);

        const del = await request.delete(
            `${API_BASE_URL}/api/v1/dns/zones/${zoneId}`,
        );
        expect(del.status()).toBe(204);
    });
});
