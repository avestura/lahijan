/**
 * WS-22 e2e spec — settings area.
 *
 * Covers: every settings sub-page (profile, security, tokens, identities,
 * sessions) is reachable from the sidebar and renders its page root.
 *
 * Deep interactions (create/revoke a personal access token, enroll TOTP
 * from the UI, unlink an identity) need more `data-testid` coverage on
 * the settings feature cards and are tracked as a follow-up. This spec
 * guards the navigation contract + the page shells every CI run once
 * enabled.
 *
 * Gated behind LAHIJAN_E2E_RUN_SETTINGS. Enable in the full
 * `make test-e2e` stack.
 */
import { test, expect } from "@playwright/test";
import { navigateViaSidebar, registerAndLogin } from "./helpers";

test.describe("settings area", () => {
    test("every settings sub-page renders", async ({ page, request }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_SETTINGS,
            "settings spec needs the auth + profile endpoints; " +
                "gated behind LAHIJAN_E2E_RUN_SETTINGS=1",
        );

        await registerAndLogin(page, request, "settings");

        for (const [navId, urlPart, pageId] of [
            ["nav-settings", "settings", "page-settings-profile"],
            [
                "nav-settings-security",
                "settings/security",
                "page-settings-security",
            ],
            ["nav-settings-tokens", "settings/tokens", "page-settings-tokens"],
            [
                "nav-settings-identities",
                "settings/identities",
                "page-settings-identities",
            ],
            [
                "nav-settings-sessions",
                "settings/sessions",
                "page-settings-sessions",
            ],
        ] as const) {
            await navigateViaSidebar(page, navId, urlPart);
            await expect(page.getByTestId(pageId)).toBeVisible();
        }
    });
});
