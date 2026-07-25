/**
 * WS-22 e2e spec — object storage journey.
 *
 * Covers the primary UI journey: open the "New bucket" dialog → fill
 * the slug → submit → the bucket appears in the list. Then mints S3
 * credentials via the API and deletes the bucket for cleanup.
 *
 * Gated behind LAHIJAN_E2E_RUN_STORAGE: needs the SeaweedFS container
 * from the test compose. Enable in the full `make test-e2e` stack.
 */
import { test, expect } from "@playwright/test";
import { API_BASE_URL, navigateViaSidebar, registerAndLogin } from "./helpers";

test.describe("storage journey", () => {
    test("create a bucket via the dialog, mint creds, clean up", async ({
        page,
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_STORAGE,
            "storage spec needs the SeaweedFS container; " +
                "gated behind LAHIJAN_E2E_RUN_STORAGE=1",
        );

        await registerAndLogin(page, request, "s3");
        await navigateViaSidebar(page, "nav-storage", "storage");
        await expect(page.getByTestId("page-storage")).toBeVisible();

        // Open the create dialog + fill the slug.
        await page.getByTestId("new-bucket-button").click();
        await expect(page.getByTestId("create-bucket-dialog")).toBeVisible();

        const slug = `e2e-${Date.now()}`;
        await page.getByTestId("create-bucket-slug").fill(slug);
        await page.getByTestId("create-bucket-submit").click();

        // The dialog closes + the list refreshes; the slug shows up.
        await expect(page.getByTestId("create-bucket-dialog")).toBeHidden({
            timeout: 15_000,
        });
        await expect(page.getByTestId("page-storage")).toContainText(slug);

        // Look up the created bucket via the API, mint a credential, delete.
        const bucketsResp = await request.get(
            `${API_BASE_URL}/api/v1/storage/buckets`,
        );
        expect(bucketsResp.status()).toBe(200);
        const buckets = (await bucketsResp.json()) as Array<{
            id: string;
            slug: string;
        }>;
        const bucket = buckets.find((b) => b.slug === slug);
        expect(bucket, "created bucket is listable via the API").toBeTruthy();
        const bucketId = bucket?.id ?? "";

        const mintCreds = await request.post(
            `${API_BASE_URL}/api/v1/storage/buckets/${bucketId}/credentials`,
            { data: { label: "e2e" } },
        );
        expect(mintCreds.status()).toBe(201);
        const creds = await mintCreds.json();
        expect(creds.access_key_id).toBeTruthy();
        expect(creds.secret_access_key).toBeTruthy();

        const del = await request.delete(
            `${API_BASE_URL}/api/v1/storage/buckets/${bucketId}`,
        );
        expect(del.status()).toBe(204);
    });
});
