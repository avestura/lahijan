/**
 * /settings/sessions — list of active sessions.
 *
 * The backend doesn't yet expose a /me/sessions endpoint (it's a
 * follow-up auth API). We surface a "coming soon" notice rather than
 * a broken UI.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { MonitorIcon } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";

export const Route = createFileRoute("/settings/sessions")({
  component: SettingsSessionsPage,
});

function SettingsSessionsPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4" data-testid="page-settings-sessions">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("settings.sessions.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("settings.sessions.subtitle")}</p>
      </header>
      <Card>
        <CardContent className="p-6">
          <EmptyState
            icon={MonitorIcon}
            title={t("settings.sessions.comingSoon")}
            description={t("settings.sessions.comingSoon")}
          />
        </CardContent>
      </Card>
    </div>
  );
}
