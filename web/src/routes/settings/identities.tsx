/**
 * /settings/identities — OAuth / OIDC / SAML link list + unlink.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { IdentitiesCard } from "@/features/settings/components/IdentitiesCard";

export const Route = createFileRoute("/settings/identities")({
  component: SettingsIdentitiesPage,
});

function SettingsIdentitiesPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4" data-testid="page-settings-identities">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("settings.identities.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("settings.identities.subtitle")}</p>
      </header>
      <IdentitiesCard />
    </div>
  );
}
