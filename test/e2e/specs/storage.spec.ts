/**
 * WS-22 e2e spec — object storage journey.
 *
 * Covers: create bucket → mint credentials → upload via aws-cli (in test
 * container).
 *
 * Status: scaffolded. The SeaweedFS container from docker-compose.test.yml
 * serves the S3 data plane; the Lahijan app talks to the Filer HTTP API
 * for control. The aws-cli upload portion runs against the published
 * SEAWEEDFS_S3_PORT (default 8334 in the test stack). The spec drives
 * the Lahijan API for the control plane; the data-plane upload is a
 * follow-up that needs an aws-cli container sidecar (tracked in the
 * WS-22 resolution notes).
 */
import { test, expect } from "@playwright/test";
import {
    API_BASE_URL,
    loginViaUI,
    registerUser,
    STRONG_PASSWORD,
    uniqueEmail,
} from "./helpers";

test.describe("storage journey", () => {
    test("create bucket + mint credentials via the API (aws-cli upload is a follow-up)", async ({
        page,
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_STORAGE,
            "storage spec needs the SeaweedFS container; gated behind LAHIJAN_E2E_RUN_STORAGE=1",
        );

        const email = uniqueEmail("s3");
        await registerUser(request, email);
        await loginViaUI(page, email, STRONG_PASSWORD);

        const createBucket = await request.post(
            `${API_BASE_URL}/api/v1/storage/buckets`,
            {
                data: { slug: `e2e-${Date.now()}` },
            },
        );
        expect(createBucket.status()).toBe(201);
        const bucket = await createBucket.json();

        const mintCreds = await request.post(
            `${API_BASE_URL}/api/v1/storage/buckets/${bucket.id}/credentials`,
            { data: { label: "e2e" } },
        );
        expect(mintCreds.status()).toBe(201);
        const creds = await mintCreds.json();
        expect(creds.access_key_id).toBeTruthy();
        expect(creds.secret_access_key).toBeTruthy();
    });
});
