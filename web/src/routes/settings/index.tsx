/**
 * /settings — profile sub-page.
 *
 * The settings area is split into 5 pages (profile / security / tokens
 * / identities / sessions), all linked from the Sidebar's Settings
 * subsection. The default `/settings` route renders the profile form.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { ProfileForm } from "@/features/settings/components/ProfileForm";

export const Route = createFileRoute("/settings/")({
  component: SettingsProfilePage,
});

function SettingsProfilePage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4" data-testid="page-settings-profile">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("settings.profile.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("settings.profile.subtitle")}</p>
      </header>
      <ProfileForm />
    </div>
  );
}
