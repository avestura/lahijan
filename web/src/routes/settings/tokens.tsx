/**
 * /settings/tokens — personal access tokens list + create + revoke.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { PersonalAccessTokensCard } from "@/features/settings/components/PersonalAccessTokensCard";

export const Route = createFileRoute("/settings/tokens")({
  component: SettingsTokensPage,
});

function SettingsTokensPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4" data-testid="page-settings-tokens">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("settings.tokens.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("settings.tokens.subtitle")}</p>
      </header>
      <PersonalAccessTokensCard />
    </div>
  );
}
