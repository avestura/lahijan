/**
 * /settings — redirects to /settings/profile.
 *
 * Profile used to live at the bare /settings index, which made every
 * /settings/* sub-page substring-match the "Profile" nav entry. The
 * index is now a pure redirect to the profile sub-path.
 */
import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/settings/")({
  beforeLoad: () => {
    // TanStack Router resolves a beforeLoad by catching the thrown
    // redirect; the throw is mandatory (a plain return is ignored).
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ to: "/settings/profile" });
  },
});
