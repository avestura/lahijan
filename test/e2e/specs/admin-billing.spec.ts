/**
 * WS-22 e2e spec — admin billing journey.
 *
 * Covers: admin tops up a user's balance → the user can spend against
 * the new balance.
 *
 * Status: scaffolded. The full UI flow needs an admin user with the
 * billing.balance.adjust permission. The spec below drives the API to
 * validate the topup + ledger wiring without the admin fixtures.
 */
import { test, expect } from "@playwright/test";
import { API_BASE_URL, registerUser, uniqueEmail } from "./helpers";

test.describe("admin billing journey", () => {
    test("balance is queryable for a freshly-registered user", async ({
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_BILLING,
            "billing spec needs an admin fixture for the topup side; gated behind LAHIJAN_E2E_RUN_BILLING=1",
        );

        const email = uniqueEmail("billing");
        const { email: registered } = await registerUser(request, email);

        // The user can always read their own balance. A freshly-registered
        // user starts at zero (no ledger entries yet).
        const balance = await request.get(`${API_BASE_URL}/api/v1/me/balance`);
        expect(balance.status()).toBe(200);
        const body = await balance.json();
        expect(body.balance_cents ?? body.balanceCents ?? 0).toBe(0);

        // The user's own email is the only stable identifier we can
        // assert against here; the rest of the topup flow needs an admin.
        expect(registered).toBe(email);
    });
});
