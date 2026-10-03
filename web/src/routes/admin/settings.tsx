/**
 * /admin/settings — platform settings an administrator changes at runtime.
 */
import { createFileRoute } from "@tanstack/react-router";

import { PlatformSettingsPanel } from "@/features/admin/components/PlatformSettingsPanel";

export const Route = createFileRoute("/admin/settings")({
  component: PlatformSettingsPanel,
});
