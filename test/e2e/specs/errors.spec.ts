/**
 * WS-22 e2e spec — error + edge states.
 *
 *   - unknown route → the NotFound component renders (runs by default;
 *     only needs the shell, no provider fakes)
 *   - HTTP 501 "feature disabled" → the friendly FeatureDisabledState
 *     renders instead of the generic error (gated; needs a stack where
 *     a module is intentionally disabled, e.g. compute without Incus)
 */
import { test, expect } from "@playwright/test";
import { registerAndLogin } from "./helpers";

test.describe("error + edge states", () => {
    test("an unknown route renders the not-found state", async ({
        page,
        request,
    }) => {
        await registerAndLogin(page, request, "404");

        await page.goto("/this-route-does-not-exist");
        // The root route's notFoundComponent (NotFoundState) renders.
        await expect(page.getByTestId("not-found")).toBeVisible({
            timeout: 15_000,
        });
        // The "back to dashboard" link is part of the not-found body.
        await expect(page.getByTestId("not-found")).toContainText(/./);
    });

    test("a disabled module shows the feature-disabled state, not a crash", async ({
        page,
        request,
    }) => {
        test.skip(
            !process.env.LAHIJAN_E2E_RUN_FEATURE_DISABLED,
            "needs a stack with a module returning 501 (e.g. compute without Incus); " +
                "gated behind LAHIJAN_E2E_RUN_FEATURE_DISABLED=1",
        );

        await registerAndLogin(page, request, "featoff");
        await page.goto("/compute");

        // The list page must surface the friendly disabled state rather
        // than the generic "Something went wrong" error.
        await expect(page.getByTestId("feature-disabled")).toBeVisible({
            timeout: 15_000,
        });
    });
});
