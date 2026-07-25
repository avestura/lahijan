/**
 * /compute — instances list page.
 *
 * Renders the InstanceList with filtering + bulk actions. The "New
 * instance" button is gated by compute.instance.create. Polling is
 * driven by the useComputeInstances hook (every 5s).
 */
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { PlusIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { usePerm } from "@/lib/perm";
import { useTenant } from "@/hooks/useTenant";
import { useComputeInstances, classifyStatus, type StatusBucket } from "@/features/compute/api";
import { InstanceList } from "@/features/compute/components/InstanceList";

export const Route = createFileRoute("/compute/")({
  component: ComputeListPage,
});

// Status filter dropdown options. These are codes used by the
// filtering logic; the localized label is fetched via the
// `compute.list.filter.status.<code>` i18n key.
const STATUS_OPTIONS = ["all", "running", "stopped", "frozen", "other"] as const;

function ComputeListPage() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const { hasPerm } = usePerm("compute.instance.create");

  const query = useComputeInstances(tenantId);

  const [filter, setFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusBucket | "all">("all");

  const filtered = useMemo(() => {
    const list = query.data ?? [];
    const lower = filter.trim().toLowerCase();
    return list.filter((inst) => {
      const matchesText =
        !lower ||
        inst.name.toLowerCase().includes(lower) ||
        (inst.description ?? "").toLowerCase().includes(lower) ||
        inst.imageAlias.toLowerCase().includes(lower);
      const matchesStatus = statusFilter === "all" || classifyStatus(inst.status) === statusFilter;
      return matchesText && matchesStatus;
    });
  }, [query.data, filter, statusFilter]);

  return (
    <div className="space-y-4" data-testid="page-compute">
      <header className="flex items-center justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("compute.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("compute.subtitle")}</p>
        </div>
        {hasPerm && (
          <Button asChild data-testid="new-instance-button">
            <Link to="/compute/new">
              <PlusIcon className="h-4 w-4" />
              {t("compute.list.new")}
            </Link>
          </Button>
        )}
      </header>

      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder={t("compute.list.filter.placeholder")}
          className="max-w-sm"
          aria-label={t("common.search")}
          data-testid="compute-filter-input"
        />
        <Select
          value={statusFilter}
          onValueChange={(v) => setStatusFilter(v as StatusBucket | "all")}
        >
          <SelectTrigger className="w-[180px]" aria-label={t("common.status")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STATUS_OPTIONS.map((opt) => (
              <SelectItem key={opt} value={opt}>
                {t(`compute.list.filter.status.${opt}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <InstanceList
        instances={filtered}
        isLoading={query.isLoading}
        error={query.error}
        onRetry={() => void query.refetch()}
      />
    </div>
  );
}
