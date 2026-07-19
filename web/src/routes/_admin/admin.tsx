/**
 * `/admin` — admin landing page.
 *
 * Mounted under the `_admin` layout so it requires platform.admin. WS-18
 * only ships a placeholder so the routing + guards are exercised; the real
 * admin UI lands with WS-21.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { Card, CardContent, CardDescription, CardTitle } from "@/components/ui/card";

export const Route = createFileRoute("/_admin/admin")({
  component: AdminPage,
});

function AdminPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">{t("nav.admin.label")}</h1>
      <Card className="max-w-md">
        <CardContent className="space-y-2 p-6">
          <CardTitle>{t("nav.admin.label")}</CardTitle>
          <CardDescription>{t("dashboard.emptyState.body")}</CardDescription>
        </CardContent>
      </Card>
    </div>
  );
}
