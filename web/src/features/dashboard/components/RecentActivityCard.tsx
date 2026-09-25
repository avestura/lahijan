/**
 * RecentActivityCard — compact timeline of the last few audit events in
 * this tenant. Each row shows the action slug, the actor type, the
 * status, and when it happened. Links to the full /audit view.
 */
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { ActivityIcon } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/layout/EmptyState";
import { useAuditEvents } from "@/features/audit/api";

export function RecentActivityCard() {
  const { t } = useTranslation();
  const query = useAuditEvents({}, 0, 6);
  const items = query.data?.items ?? [];

  let body: React.ReactNode;
  if (query.isLoading) {
    body = <Skeleton className="h-32 w-full" />;
  } else if (items.length === 0) {
    body = (
      <EmptyState
        icon={ActivityIcon}
        title={t("audit.list.empty.title")}
        description={t("audit.list.empty.body")}
      />
    );
  } else {
    body = (
      <ul className="space-y-1">
        {items.map((e) => (
          <li key={e.id} className="flex items-center gap-3 px-2 py-2 hover:bg-surface-sunken">
            <span
              className={
                "h-2 w-2 shrink-0 " +
                (e.status === "success"
                  ? "bg-success"
                  : e.status === "failure"
                    ? "bg-destructive"
                    : "bg-warning")
              }
              aria-hidden="true"
            />
            <div className="min-w-0 flex-1">
              <p className="truncate font-mono text-xs">{e.action}</p>
              <p className="truncate text-xs text-muted-foreground">
                {new Date(e.createdAt).toLocaleString()}
              </p>
            </div>
            <Badge variant="outline" className="shrink-0 text-xs">
              {t(`audit.filters.actorType.${e.actorType}`)}
            </Badge>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{t("dashboard.charts.activity")}</CardTitle>
        <Link
          to="/audit"
          className="text-xs font-medium text-primary hover:underline"
          viewTransition
        >
          {t("dashboard.charts.viewAll")}
        </Link>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );
}
