/**
 * InstanceStatusChart — donut of the tenant's instances grouped by the
 * same status buckets the compute list's filter dropdown uses (running /
 * stopped / frozen / other). Degrades to an empty-state card when the
 * compute module is disabled or there are no instances.
 */
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { FeatureDisabledState } from "@/components/layout/FeatureDisabledState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ServerIcon } from "lucide-react";
import { useTenant } from "@/hooks/useTenant";
import { useComputeInstances, classifyStatus } from "@/features/compute/api";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { chartPalette } from "@/features/dashboard/charts";

const BUCKETS = ["running", "stopped", "frozen", "other"] as const;
type Bucket = (typeof BUCKETS)[number];

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

  const palette = chartPalette();
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
      <div className="relative h-[220px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={data}
              dataKey="value"
              nameKey="label"
              innerRadius={58}
              outerRadius={84}
              paddingAngle={2}
              stroke="hsl(var(--color-card))"
              strokeWidth={2}
            >
              {data.map((d, i) => (
                <Cell key={d.name} fill={palette[i % palette.length]} />
              ))}
            </Pie>
            <Tooltip
              formatter={(v: number, n: string) => [String(v), n]}
              contentStyle={{
                background: "hsl(var(--color-card))",
                border: "1px solid hsl(var(--color-border))",
                borderRadius: "0.5rem",
                color: "hsl(var(--color-card-fg))",
              }}
            />
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-3xl font-semibold tabular-nums">{total}</span>
          <span className="text-xs uppercase tracking-wider text-muted-foreground">
            {t("nav.compute")}
          </span>
        </div>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("dashboard.charts.instances")}</CardTitle>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );
}
