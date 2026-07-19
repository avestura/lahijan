/**
 * InstanceAudit — the Audit tab on the instance detail page.
 *
 * Calls /api/v1/audit filtered by resourceType=compute_instance +
 * the current instance id (via the metadata field). Requires
 * audit.read; if the user lacks it we show a "permission denied"
 * notice rather than an empty list (the API would 403 anyway).
 */
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { components } from "@api-schema";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { apiClient } from "@/lib/api/client";
import { usePerm } from "@/lib/perm";
import { ScrollTextIcon } from "lucide-react";

type AuditEvent = components["schemas"]["AuditEvent"];

interface Props {
  instanceId: string;
}

const STATUS_TONE: Record<string, "success" | "destructive" | "warning"> = {
  success: "success",
  failure: "destructive",
  pending: "warning",
};

export function InstanceAudit({ instanceId }: Props) {
  const { t } = useTranslation();
  const { hasPerm } = usePerm("audit.read");

  const query = useQuery({
    queryKey: ["audit", "instance", instanceId],
    enabled: hasPerm,
    staleTime: 30_000,
    queryFn: async (): Promise<AuditEvent[]> => {
      // The audit list endpoint filters by action / resourceType but
      // not by resource id directly today; we filter client-side.
      const { data, error, response } = await apiClient.GET("/api/v1/audit", {
        params: { query: { limit: 100, offset: 0, resourceType: "compute_instance" } },
      });
      if (error || !data) {
        throw new Error(`audit.list: ${response?.status ?? "network"}`);
      }
      const items = data.items ?? [];
      return items.filter((e) => e.resourceId === instanceId);
    },
  });

  if (!hasPerm) {
    return (
      <EmptyState
        icon={ScrollTextIcon}
        title={t("permission.denied")}
        description={t("permission.denied")}
      />
    );
  }
  if (query.isLoading) return <LoadingState rows={4} />;
  if (query.error) {
    return (
      <ErrorState
        message={t("common.error")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  }
  const rows = query.data ?? [];
  if (rows.length === 0) {
    return (
      <EmptyState
        icon={ScrollTextIcon}
        title={t("common.none")}
        description={t("common.none")}
      />
    );
  }

  return (
    <div className="rounded-md border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("audit.columns.action")}</TableHead>
            <TableHead>{t("audit.columns.status")}</TableHead>
            <TableHead>{t("audit.columns.created")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const tone = STATUS_TONE[row.status] ?? "secondary";
            return (
              <TableRow key={row.id}>
                <TableCell className="font-mono text-xs">{row.action}</TableCell>
                <TableCell>
                  <Badge variant={tone}>{row.status}</Badge>
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {row.createdAt ? new Date(row.createdAt).toLocaleString() : ""}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
