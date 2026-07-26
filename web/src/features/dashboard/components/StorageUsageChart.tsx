/**
 * StorageUsageChart — horizontal bar chart of bytes used per bucket (top N).
 *
 * Aggregates from the tenant's bucket list (each row carries a cached
 * `bytesUsed`). Degrades gracefully when storage is disabled or empty.
 */
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { FeatureDisabledState } from "@/components/layout/FeatureDisabledState";
import { LoadingState } from "@/components/layout/LoadingState";
import { DatabaseIcon } from "lucide-react";
import { useTenant } from "@/hooks/useTenant";
import { useStorageBuckets } from "@/features/storage/api";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { formatBytes } from "@/features/storage/format";
import { chartPrimary } from "@/features/dashboard/charts";

const MAX_BARS = 6;

export function StorageUsageChart() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const query = useStorageBuckets(tenant.currentTenantId);

  const data = useMemo(() => {
    const rows = (query.data ?? [])
      .map((b) => ({ name: b.label ?? b.slug, bytes: Number(b.bytesUsed ?? 0) }))
      .sort((a, b) => b.bytes - a.bytes)
      .slice(0, MAX_BARS);
    return rows;
  }, [query.data]);

  let body: React.ReactNode;
  if (query.isLoading) {
    body = <LoadingState rows={4} />;
  } else if (isFeatureDisabledError(query.error)) {
    body = (
      <FeatureDisabledState
        title={t("common.featureDisabled.title")}
        description={t("common.featureDisabled.description")}
      />
    );
  } else if (data.length === 0) {
    body = (
      <EmptyState
        icon={DatabaseIcon}
        title={t("storage.list.empty.title")}
        description={t("storage.list.empty.body")}
      />
    );
  } else {
    body = (
      <div className="h-[260px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} layout="vertical" margin={{ left: 8, right: 16, top: 4, bottom: 4 }}>
            <CartesianGrid
              strokeDasharray="3 3"
              horizontal={false}
              stroke="hsl(var(--color-border))"
            />
            <XAxis
              type="number"
              tickFormatter={(v: number) => formatBytes(v)}
              stroke="hsl(var(--color-muted-fg))"
              fontSize={11}
              tickLine={false}
              axisLine={false}
            />
            <YAxis
              type="category"
              dataKey="name"
              width={120}
              stroke="hsl(var(--color-muted-fg))"
              fontSize={11}
              tickLine={false}
              axisLine={false}
            />
            <Tooltip
              formatter={(v: number) => [formatBytes(v), t("storage.usage.size")]}
              cursor={{ fill: "hsl(var(--color-muted) / 0.4)" }}
              contentStyle={{
                background: "hsl(var(--color-card))",
                border: "1px solid hsl(var(--color-border))",
                borderRadius: "0.5rem",
                color: "hsl(var(--color-card-fg))",
              }}
            />
            <Bar dataKey="bytes" fill={chartPrimary()} radius={[0, 4, 4, 0]} />
          </BarChart>
        </ResponsiveContainer>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("dashboard.charts.storage")}</CardTitle>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );
}
