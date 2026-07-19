/**
 * /audit — paginated audit log with filters + export.
 *
 * Per WS-21: live tail mode (websocket stream) is explicitly out of
 * scope for MVP. Manual refresh via the standard query refetch path.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AuditTable } from "@/features/audit/components/AuditTable";
import type { AuditFilters } from "@/features/audit/api";

export const Route = createFileRoute("/audit/")({
  component: AuditPage,
});

function AuditPage() {
  const { t } = useTranslation();
  const [filters, setFilters] = useState<AuditFilters>({});

  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("audit.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("audit.subtitle")}</p>
      </header>
      <AuditTable filters={filters} setFilters={setFilters} />
    </div>
  );
}
