/**
 * /admin/marketplace — browse available plugins.
 *
 * Platform-admin only. Mirrors the plugins list page but reads the
 * marketplace index and offers install + upgrade.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { MarketplaceList } from "@/features/plugins/components/MarketplaceList";

export const Route = createFileRoute("/admin/marketplace")({
  component: MarketplacePage,
});

function MarketplacePage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("plugins.marketplace.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("plugins.marketplace.subtitle")}</p>
      </header>
      <MarketplaceList />
    </div>
  );
}
