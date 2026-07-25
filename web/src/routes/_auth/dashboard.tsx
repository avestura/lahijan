/**
 * `/dashboard` — landing page for authenticated users.
 *
 * Calls /api/v1/ping via TanStack Query to prove the API pipeline is wired
 * end to end (typed client + i18n + design tokens). Lists the welcome
 * banner + a small "ping" card that the e2e suite in WS-22 will lean on.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { Card, CardContent, CardDescription, CardTitle } from "@/components/ui/card";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { useSessionStore } from "@/lib/stores/session-store";

export const Route = createFileRoute("/_auth/dashboard")({
  component: DashboardPage,
});

function DashboardPage() {
  const { t } = useTranslation();
  const user = useSessionStore((s) => s.user);

  const ping = useQuery({
    queryKey: queryKeys.ping(),
    queryFn: async () => {
      const { data, error } = await apiClient.GET("/api/v1/ping");
      if (error || !data) throw new Error("ping failed");
      return data;
    },
    staleTime: 30_000,
  });

  const pingBody = ping.data
    ? t("dashboard.ping.success", { ts: String(ping.data.pong ?? "") })
    : ping.isLoading
      ? t("dashboard.ping.loading")
      : t("dashboard.ping.error");

  return (
    <div className="space-y-6" data-testid="page-dashboard">
      <h1 className="text-2xl font-semibold">
        {t("dashboard.welcome", {
          name: user?.displayName ?? user?.email ?? "",
        })}
      </h1>

      <Card className="max-w-md">
        <CardContent className="space-y-2 p-6">
          <CardTitle>{t("nav.dashboard")}</CardTitle>
          <CardDescription>{pingBody}</CardDescription>
        </CardContent>
      </Card>
    </div>
  );
}
