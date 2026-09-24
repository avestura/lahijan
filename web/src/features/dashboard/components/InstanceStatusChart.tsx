/**
 * InstanceStatusChart — the tenant's instances grouped by the same status
 * buckets the compute list's filter uses (running / stopped / frozen /
 * other), drawn as a Boxy instrument: a mono total, one square stacked bar
 * and a legend of 8px squares with counts (color is never the only signal).
 * Degrades to an empty-state card when the compute module is disabled or
 * there are no instances.
 */
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { FeatureDisabledState } from "@/components/layout/FeatureDisabledState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ServerIcon } from "lucide-react";
import { useTenant } from "@/hooks/useTenant";
import { useComputeInstances, classifyStatus } from "@/features/compute/api";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { cn } from "@/lib/utils";

const BUCKETS = ["running", "stopped", "frozen", "other"] as const;
type Bucket = (typeof BUCKETS)[number];

// Same tones as InstanceStatusBadge's squares.
const FILL: Record<Bucket, string> = {
  running: "bg-success",
  stopped: "bg-ink-faint",
  frozen: "bg-surface-inverse",
  other: "bg-warning",
};

export function InstanceStatusChart() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const query = useComputeInstances(tenant.currentTenantId);

  const data = useMemo(() => {
    const counts: Record<Bucket, number> = { running: 0, stopped: 0, frozen: 0, other: 0 };
    for (const inst of query.data ?? []) {
      counts[classifyStatus(inst.status)] += 1;
    }
    return BUCKETS.map((name) => ({
      name,
      label: t(`compute.list.filter.status.${name}`),
      value: counts[name],
    })).filter((d) => d.value > 0);
  }, [query.data, t]);

  const total = data.reduce((sum, d) => sum + d.value, 0);

  let body: React.ReactNode;
  if (query.isLoading) {
    body = <LoadingState rows={3} />;
  } else if (isFeatureDisabledError(query.error)) {
    body = (
      <FeatureDisabledState
        title={t("common.featureDisabled.title")}
        description={t("common.featureDisabled.description")}
      />
    );
  } else if (total === 0) {
    body = (
      <EmptyState
        icon={ServerIcon}
        title={t("compute.list.empty.title")}
        description={t("compute.list.empty.body")}
      />
    );
  } else {
    body = (
      <div className="space-y-4" data-testid="instance-status-chart">
        <div className="flex items-baseline gap-2">
          <span className="font-mono text-4xl font-medium tabular-nums tracking-[-0.02em]">
            {total}
          </span>
          <span className="label-mono">{t("nav.compute")}</span>
        </div>
        <div className="flex h-4 w-full gap-px border border-line bg-line" aria-hidden="true">
          {data.map((d) => (
            <div
              key={d.name}
              className={cn("h-full", FILL[d.name])}
              style={{ width: `${(d.value / total) * 100}%` }}
            />
          ))}
        </div>
        <ul className="divide-y divide-line-subtle border border-line">
          {data.map((d) => (
            <li key={d.name} className="flex items-center justify-between bg-card px-3 py-2">
              <span className="flex items-center gap-2 font-mono text-label uppercase tracking-[0.08em] text-ink-muted">
                <span className={cn("h-2 w-2", FILL[d.name])} aria-hidden="true" />
                {d.label}
              </span>
              <span className="font-mono text-sm tabular-nums">{d.value}</span>
            </li>
          ))}
        </ul>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("dashboard.charts.instances")}</CardTitle>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );
}
