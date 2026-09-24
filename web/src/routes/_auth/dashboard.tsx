/**
 * `/dashboard` — landing page for authenticated users.
 *
 * Composes a resource overview row + four widgets (instance status donut,
 * storage usage bars, billing balance + recent ledger, recent audit
 * activity). Each widget fires its own queries and degrades gracefully
 * when the backing module is disabled (501) so the dashboard never goes
 * blank.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { useSessionStore } from "@/lib/stores/session-store";
import { ResourceOverviewCards } from "@/features/dashboard/components/ResourceOverviewCards";
import { InstanceStatusChart } from "@/features/dashboard/components/InstanceStatusChart";
import { StorageUsageChart } from "@/features/dashboard/components/StorageUsageChart";
import { BillingBalanceCard } from "@/features/dashboard/components/BillingBalanceCard";
import { RecentActivityCard } from "@/features/dashboard/components/RecentActivityCard";

export const Route = createFileRoute("/_auth/dashboard")({
  component: DashboardPage,
});

function DashboardPage() {
  const { t } = useTranslation();
  const user = useSessionStore((s) => s.user);

  return (
    <div className="space-y-6" data-testid="page-dashboard">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">
          {t("dashboard.welcome", {
            name: user?.displayName ?? user?.email ?? "",
          })}
        </h1>
        <p className="text-sm text-muted-foreground">{t("dashboard.subtitle")}</p>
      </header>

      <ResourceOverviewCards />

      {/* Collapsed grid: panels share 1px rules instead of floating. */}
      <div className="grid grid-cols-1 gap-px border border-line bg-line lg:grid-cols-2 [&>*]:border-0">
        <InstanceStatusChart />
        <StorageUsageChart />
        <BillingBalanceCard />
        <RecentActivityCard />
      </div>
    </div>
  );
}
