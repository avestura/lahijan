/**
 * /settings/profile — profile form (display name, email, locale, ...).
 *
 * Profile lives at its own path (not the bare /settings index) so the
 * Sidebar's active-link matching no longer highlights "Profile" while
 * the user is on every other settings sub-page. /settings itself now
 * redirects here.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { ProfileForm } from "@/features/settings/components/ProfileForm";

export const Route = createFileRoute("/settings/profile")({
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
