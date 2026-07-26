/**
 * ResourceOverviewCards — the top "at a glance" row on the dashboard.
 *
 * Four tiles: running instances, DNS zones, storage buckets, and the
 * current billing balance. Each tile fires its own query and degrades
 * independently: a 501 (feature disabled) collapses the tile to a muted
 * "—" so the row still renders.
 */
import { useTranslation } from "react-i18next";
import { CloudIcon, DatabaseIcon, DollarSignIcon } from "lucide-react";
import { Link } from "@tanstack/react-router";

import { CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useTenant } from "@/hooks/useTenant";
import { useComputeInstances } from "@/features/compute/api";
import { useDNSZones } from "@/features/dns/api";
import { useStorageBuckets } from "@/features/storage/api";
import { useMyBalance } from "@/features/billing/api";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { formatCurrency } from "@/features/dashboard/format";
import { Globe } from "@/features/dashboard/icons";

export function ResourceOverviewCards() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;

  const instances = useComputeInstances(tenantId);
  const zones = useDNSZones(tenantId);
  const buckets = useStorageBuckets(tenantId);
  const balance = useMyBalance();

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Tile
        icon={<CloudIcon className="h-5 w-5" />}
        label={t("dashboard.overview.instances")}
        to="/compute"
        loading={instances.isLoading}
        disabled={isFeatureDisabledError(instances.error)}
        value={String(instances.data?.length ?? 0)}
      />
      <Tile
        icon={<Globe className="h-5 w-5" />}
        label={t("dashboard.overview.zones")}
        to="/dns"
        loading={zones.isLoading}
        disabled={isFeatureDisabledError(zones.error)}
        value={String(zones.data?.length ?? 0)}
      />
      <Tile
        icon={<DatabaseIcon className="h-5 w-5" />}
        label={t("dashboard.overview.buckets")}
        to="/storage"
        loading={buckets.isLoading}
        disabled={isFeatureDisabledError(buckets.error)}
        value={String(buckets.data?.length ?? 0)}
      />
      <Tile
        icon={<DollarSignIcon className="h-5 w-5" />}
        label={t("dashboard.overview.balance")}
        to="/billing"
        loading={balance.isLoading}
        disabled={isFeatureDisabledError(balance.error)}
        value={
          balance.data
            ? formatCurrency(balance.data.balanceCents, balance.data.currency)
            : "—"
        }
      />
    </div>
  );
}

interface TileProps {
  icon: React.ReactNode;
  label: string;
  value: string;
  to: string;
  loading?: boolean;
  disabled?: boolean;
}

function Tile({ icon, label, value, to, loading, disabled }: TileProps) {
  const body = (
    <CardContent className="flex items-center gap-4 p-5">
      <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        {icon}
      </span>
      <div className="min-w-0 space-y-0.5">
        <p className="truncate text-xs uppercase tracking-wider text-muted-foreground">
          {label}
        </p>
        {loading ? (
          <Skeleton className="h-7 w-16" />
        ) : (
          <p className="truncate text-2xl font-semibold tabular-nums" data-value={value}>
            {disabled ? "—" : value}
          </p>
        )}
      </div>
    </CardContent>
  );
  // Client-side navigation via TanStack Link keeps the SPA mounted (a raw
  // <a href> would force a full reload on every tile click).
  return (
    <Link
      to={to}
      data-testid={`dashboard-tile-${to.split("/").pop()}`}
      className="block rounded-lg border border-border bg-card text-card-fg shadow-sm transition-colors hover:border-primary/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      aria-label={label}
    >
      {body}
    </Link>
  );
}
