/**
 * /compute/new — the 3-step Create Instance wizard.
 *
 * Wrapped in the `_auth` layout (mounted via the file router). The
 * route is pathless w.r.t. the auth guard; the auth layout runs the
 * session check.
 */
import { createFileRoute } from "@tanstack/react-router";

import { CreateInstanceWizard } from "@/features/compute/components/CreateInstanceWizard";

export const Route = createFileRoute("/compute/new")({
  component: NewInstanceRoute,
});

function NewInstanceRoute() {
  return <CreateInstanceWizard />;
}
