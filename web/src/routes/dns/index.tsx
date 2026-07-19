/**
 * /dns — zones list page.
 *
 * Renders the DNSZoneList with text filter + create button. The
 * "New zone" button is gated by `dns.zone.create`.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { PlusIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { usePerm } from "@/lib/perm";
import { useTenant } from "@/hooks/useTenant";
import { useDNSZones } from "@/features/dns/api";
import { DNSZoneList } from "@/features/dns/components/DNSZoneList";
import { CreateDNSZoneDialog } from "@/features/dns/components/CreateDNSZoneDialog";

export const Route = createFileRoute("/dns/")({
  component: DNSListPage,
});

function DNSListPage() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const { hasPerm } = usePerm("dns.zone.create");

  const query = useDNSZones(tenantId);

  const [filter, setFilter] = useState("");
  const [createOpen, setCreateOpen] = useState(false);

  const filtered = useMemo(() => {
    const list = query.data ?? [];
    const lower = filter.trim().toLowerCase();
    return list.filter(
      (z) =>
        !lower ||
        z.name.toLowerCase().includes(lower) ||
        (z.description ?? "").toLowerCase().includes(lower),
    );
  }, [query.data, filter]);

  return (
    <div className="space-y-4">
      <header className="flex items-center justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("dns.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("dns.subtitle")}</p>
        </div>
        {hasPerm && (
          <Button onClick={() => setCreateOpen(true)}>
            <PlusIcon className="h-4 w-4" />
            {t("dns.list.new")}
          </Button>
        )}
      </header>

      <Input
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder={t("dns.list.filter.placeholder")}
        className="max-w-sm"
        aria-label={t("common.search")}
      />

      <DNSZoneList
        zones={filtered}
        isLoading={query.isLoading}
        error={query.error}
        onRetry={() => void query.refetch()}
      />

      <CreateDNSZoneDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        tenantId={tenantId}
      />
    </div>
  );
}
