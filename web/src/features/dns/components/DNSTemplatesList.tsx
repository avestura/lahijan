/**
 * DNSTemplatesList — table of predefined templates with an "Apply"
 * button per row.
 */
import { useTranslation } from "react-i18next";
import { LayoutTemplateIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import { useApplyDNSTemplate, useDNSTemplates } from "../api";

interface Props {
  tenantId: string | null;
  zoneId: string | undefined;
}

export function DNSTemplatesList({ tenantId, zoneId }: Props) {
  const { t } = useTranslation();
  const query = useDNSTemplates();
  const apply = useApplyDNSTemplate(tenantId, zoneId);
  const { hasPerm } = usePerm("dns.record.create");

  if (query.isLoading) return <LoadingState rows={3} />;
  if (query.error) {
    return (
      <ErrorState
        message={t("common.error")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  }
  if ((query.data ?? []).length === 0) {
    return (
      <EmptyState
        icon={LayoutTemplateIcon}
        title={t("dns.templates.empty.title")}
        description={t("dns.templates.empty.body")}
      />
    );
  }

  return (
    <div className="border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("dns.templates.columns.name")}</TableHead>
            <TableHead>{t("dns.templates.columns.description")}</TableHead>
            <TableHead className="text-end">{t("dns.templates.columns.actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(query.data ?? []).map((tpl) => (
            <TableRow key={tpl.id}>
              <TableCell className="font-medium">{tpl.name}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{tpl.description}</TableCell>
              <TableCell className="text-end">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!hasPerm || apply.isPending}
                  onClick={() => apply.mutate({ templateId: tpl.id })}
                >
                  {apply.isPending ? t("dns.templates.applying") : t("dns.templates.apply")}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
