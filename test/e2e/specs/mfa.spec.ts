/**
 * WS-22 e2e spec — MFA journey.
 *
 * Covers: the security page renders + enrolling TOTP via the API
 * surfaces a secret + QR URL.
 *
 * Status: the full "scan QR → type 6-digit code → confirm" loop needs a
 * TOTP code computed from the returned secret (a TOTP lib the e2e deps
 * don't ship). That loop is tracked as a follow-up in the WS-22e admin
 * + security e2e workstream. This spec still exercises the backend
 * pipeline + the security page every CI run once enabled.
 *
 * Gated behind LAHIJAN_E2E_RUN_MFA. Enable in the full `make test-e2e`
 * stack.
 */
import { test, expect } from "@playwright/test";
import { API_BASE_URL, navigateViaSidebar, registerAndLogin } from "./helpers";

test.describe("mfa journey", () => {
    test("the security page renders and TOTP enrollment returns a secret", async ({
        page,
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_MFA,
            "mfa spec needs the TOTP endpoints; gated behind LAHIJAN_E2E_RUN_MFA=1",
        );

        await registerAndLogin(page, request, "mfa");

        // The security page mounts the TOTP / WebAuthn / recovery cards.
        await navigateViaSidebar(
            page,
            "nav-settings-security",
            "settings/security",
        );
        await expect(page.getByTestId("page-settings-security")).toBeVisible();

        // Enroll via the API; the session cookie from login is carried
        // forward automatically by Playwright's APIRequestContext.
        const enroll = await request.post(
            `${API_BASE_URL}/api/v1/me/mfa/totp/enroll`,
        );
        expect(enroll.status()).toBe(200);
        const body = await enroll.json();
        expect(body.secret).toBeTruthy();
        expect(body.qr_url).toBeTruthy();
    });
});
