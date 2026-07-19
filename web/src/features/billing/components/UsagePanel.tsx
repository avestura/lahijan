/**
 * UsagePanel — usage events for the active window.
 *
 * The chart is a simple per-resource bar list (no external chart lib
 * — keeps the bundle small). Per the WS-21 open question default we
 * would have used recharts/visx; an ADR is not warranted because we
 * don't need anything a CSS bar chart can't do for the MVP surface.
 */
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { BarChart3Icon } from "lucide-react";
import { useMyUsage } from "../api";
import { USAGE_WINDOWS, type UsageWindow } from "../schemas";

const WINDOW_MS: Record<UsageWindow, number> = {
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

export function UsagePanel() {
  const { t } = useTranslation();
  const [window, setWindow] = useState<UsageWindow>("7d");

  const from = useMemo(() => {
    return new Date(Date.now() - WINDOW_MS[window]).toISOString();
  }, [window]);
  const to = useMemo(() => new Date().toISOString(), []);

  const query = useMyUsage({ from, to });

  // Aggregate by resourceType for the simple bar view.
  const aggregated = useMemo(() => {
    const map = new Map<string, number>();
    for (const ev of query.data ?? []) {
      map.set(ev.resourceType, (map.get(ev.resourceType) ?? 0) + ev.qty);
    }
    const max = Math.max(1, ...Array.from(map.values()));
    return Array.from(map.entries())
      .map(([resourceType, qty]) => ({ resourceType, qty, pct: (qty / max) * 100 }))
      .sort((a, b) => b.qty - a.qty);
  }, [query.data]);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{t("billing.user.usage.title")}</CardTitle>
        <Select value={window} onValueChange={(v) => setWindow(v as UsageWindow)}>
          <SelectTrigger className="w-[160px]" aria-label={t("billing.user.usage.window.label")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {USAGE_WINDOWS.map((w) => (
              <SelectItem key={w} value={w}>
                {t(`billing.user.usage.window.${w}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-xs text-muted-foreground">{t("billing.user.usage.subtitle")}</p>
        {query.isLoading ? (
          <LoadingState rows={3} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : aggregated.length === 0 ? (
          <EmptyState
            icon={BarChart3Icon}
            title={t("billing.user.usage.empty.title")}
            description={t("billing.user.usage.empty.body")}
          />
        ) : (
          <ul className="space-y-2">
            {aggregated.map((row) => (
              <li key={row.resourceType} className="space-y-1">
                <div className="flex items-center justify-between text-sm">
                  <span className="font-mono text-xs">{row.resourceType}</span>
                  <span className="font-mono text-xs text-muted-foreground">
                    {row.qty.toLocaleString()}
                  </span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-2 rounded-full bg-primary transition-all"
                    style={{ width: `${Math.max(2, row.pct)}%` }}
                  />
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
