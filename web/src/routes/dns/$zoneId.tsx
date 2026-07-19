/**
 * /dns/$zoneId — zone detail with Records / Settings / Templates tabs.
 *
 * DNSSEC toggle lives in the Settings tab (DNSSECCard). Records tab
 * embeds DNSRecordList (inline create/edit + bulk-delete + filter).
 */
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { useTenant } from "@/hooks/useTenant";
import { useDNSZone } from "@/features/dns/api";
import { DNSRecordList } from "@/features/dns/components/DNSRecordList";
import { DNSSECCard } from "@/features/dns/components/DNSSECCard";
import { DNSTemplatesList } from "@/features/dns/components/DNSTemplatesList";
import { RECORD_TYPES } from "@/features/dns/schemas";

type DNSZone = components["schemas"]["DNSZone"];

export const Route = createFileRoute("/dns/$zoneId")({
  component: DNSZoneDetailPage,
});

const routeApi = getRouteApi("/dns/$zoneId");

/**
 * useZoneId — typed wrapper around the route's useParams().
 *
 * See compute/$id.tsx for the rationale: eslint's typed-rules program
 * can't see the routeTree.gen.ts augmentation, so we cast through
 * `unknown` to the concrete shape.
 */
function useZoneId(): string {
  const params = routeApi.useParams() as unknown as { zoneId: string };
  return params.zoneId;
}

// Tab identifiers used internally; localized labels come from
// `dns.detail.tabs.<id>` and `dns.templates.title`.
const DETAIL_TABS = ["records", "templates", "settings"] as const;
type DetailTab = (typeof DETAIL_TABS)[number];

// Type-filter dropdown options. The "all" sentinel is rendered via
// `dns.records.filter.type.all`.
const TYPE_FILTERS = ["all", ...RECORD_TYPES] as const;
type TypeFilter = (typeof TYPE_FILTERS)[number];

function DNSZoneDetailPage() {
  const { t } = useTranslation();
  const zoneId = useZoneId();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;

  const query = useDNSZone(tenantId, zoneId);

  const [nameFilter, setNameFilter] = useState("");
  const [typeFilter, setTypeFilter] = useState<TypeFilter>("all");

  if (query.isLoading) return <LoadingState rows={4} />;
  if (query.error) {
    return (
      <ErrorState
        message={t("dns.detail.notFound")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  }
  const zone: DNSZone | undefined = query.data;
  if (!zone) return null;

  const renderTab = (tab: DetailTab) => {
    switch (tab) {
      case "records":
        return (
          <div className="space-y-4">
            <RecordFilters
              nameFilter={nameFilter}
              setNameFilter={setNameFilter}
              typeFilter={typeFilter}
              setTypeFilter={setTypeFilter}
            />
            <DNSRecordList
              tenantId={tenantId}
              zoneId={zoneId}
              nameFilter={nameFilter}
              typeFilter={typeFilter}
            />
          </div>
        );
      case "templates":
        return <DNSTemplatesList tenantId={tenantId} zoneId={zoneId} />;
      case "settings":
        return (
          <div className="space-y-4">
            <DNSSECCard tenantId={tenantId} zone={zone} />
            <ZoneMetadataCard zone={zone} />
          </div>
        );
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <Button asChild variant="ghost" size="sm" className="mb-2">
          <Link to="/dns">
            <ArrowLeftIcon className="h-4 w-4" />
            {t("common.back")}
          </Link>
        </Button>
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold">{zone.name}</h1>
          <p className="text-sm text-muted-foreground">
            {zone.description ?? t("dns.detail.noDescription")}
          </p>
        </header>
      </div>

      <Tabs defaultValue={DETAIL_TABS[0]}>
        <TabsList>
          {DETAIL_TABS.map((tab) => (
            <TabsTrigger key={tab} value={tab}>
              {tab === "templates" ? t("dns.templates.title") : t(`dns.detail.tabs.${tab}`)}
            </TabsTrigger>
          ))}
        </TabsList>

        {DETAIL_TABS.map((tab) => (
          <TabsContent key={tab} value={tab}>
            {renderTab(tab)}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}

function RecordFilters({
  nameFilter,
  setNameFilter,
  typeFilter,
  setTypeFilter,
}: {
  nameFilter: string;
  setNameFilter: (v: string) => void;
  typeFilter: TypeFilter;
  setTypeFilter: (v: TypeFilter) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        value={nameFilter}
        onChange={(e) => setNameFilter(e.target.value)}
        placeholder={t("dns.records.filter.placeholder")}
        className="max-w-sm"
        aria-label={t("common.search")}
      />
      <Select value={typeFilter} onValueChange={(v) => setTypeFilter(v as TypeFilter)}>
        <SelectTrigger className="w-[160px]" aria-label={t("dns.records.filter.type.label")}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {TYPE_FILTERS.map((rt) => (
            <SelectItem key={rt} value={rt}>
              {rt === "all" ? t("dns.records.filter.type.all") : rt}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function ZoneMetadataCard({ zone }: { zone: DNSZone }) {
  const { t } = useTranslation();
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("dns.detail.title")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <DetailRow label={t("dns.detail.id")} value={zone.id} />
        <DetailRow label={t("dns.detail.canonical")} value={zone.canonicalId} />
        <DetailRow label={t("dns.detail.kind")} value={t(`dns.kinds.${zone.kind}`)} />
        <DetailRow label={t("common.created")} value={new Date(zone.createdAt).toLocaleString()} />
      </CardContent>
    </Card>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[120px_1fr] items-center gap-2">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className="break-all font-mono text-xs">{value}</span>
    </div>
  );
}
