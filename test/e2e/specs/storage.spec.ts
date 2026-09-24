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
import {
    API_BASE_URL,
    navigateViaSidebar,
    registerAndLogin,
    tenantHeaders,
} from "./helpers";

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
        const headers = await tenantHeaders(request);
        const bucketsResp = await request.get(
            `${API_BASE_URL}/api/v1/storage/buckets`,
            { headers },
        );
        expect(bucketsResp.status()).toBe(200);
        const buckets = (
            (await bucketsResp.json()) as {
                items: Array<{ id: string; slug: string }>;
            }
        ).items;
        const bucket = buckets.find((b) => b.slug === slug);
        expect(bucket, "created bucket is listable via the API").toBeTruthy();
        const bucketId = bucket?.id ?? "";

        const mintCreds = await request.post(
            `${API_BASE_URL}/api/v1/storage/buckets/${bucketId}/credentials`,
            {
                headers,
                data: { label: "e2e", actions: ["Read", "Write", "List"] },
            },
        );
        expect(mintCreds.status()).toBe(201);
        const creds = (await mintCreds.json()) as {
            credential: { accessKeyId: string };
            secretKey: string;
        };
        expect(creds.credential.accessKeyId).toBeTruthy();
        expect(creds.secretKey).toBeTruthy();

        const del = await request.delete(
            `${API_BASE_URL}/api/v1/storage/buckets/${bucketId}`,
            { headers },
        );
        expect(del.status()).toBe(204);
    });
});
