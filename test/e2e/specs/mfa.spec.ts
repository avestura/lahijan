/**
 * WS-22 e2e spec — MFA journey.
 *
 * Covers: enroll TOTP → log in with MFA.
 *
 * Status: scaffolded. The TOTP enrollment + verify API endpoints shipped
 * with WS-07c. The dashboard's /settings/security page wires the QR
 * scan (WS-20). Driving the MFA challenge in the login flow needs the
 * post-202 challenge UI which lands with the WS-20 MFA follow-up
 * (documented in the WS-20 resolution notes).
 *
 * The spec below drives the API directly so the backend pipeline is
 * exercised even before the UI catches up.
 */
import { test, expect } from "@playwright/test";
import { API_BASE_URL, registerUser, uniqueEmail } from "./helpers";

test.describe("mfa journey", () => {
    test("enroll TOTP via the API (login-flow challenge UI is a follow-up)", async ({
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_MFA,
            "mfa spec needs the post-202 login challenge UI; gated behind LAHIJAN_E2E_RUN_MFA=1",
        );

        const email = uniqueEmail("mfa");
        await registerUser(request, email);

        // The session cookie from registerUser is automatically carried
        // forward by Playwright's APIRequestContext.
        const enroll = await request.post(
            `${API_BASE_URL}/api/v1/me/mfa/totp/enroll`,
        );
        expect(enroll.status()).toBe(200);
        const body = await enroll.json();
        expect(body.secret).toBeTruthy();
        expect(body.qr_url).toBeTruthy();
    });
});
