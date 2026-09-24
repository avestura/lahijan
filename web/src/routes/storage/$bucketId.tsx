/**
 * /storage/$bucketId — bucket detail with Overview / Credentials /
 * Presign / Quota tabs.
 */
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { useTenant } from "@/hooks/useTenant";
import { useStorageBucket } from "@/features/storage/api";
import { StorageUsageCard } from "@/features/storage/components/StorageUsageCard";
import { StorageCredentialsCard } from "@/features/storage/components/StorageCredentialsCard";
import { StoragePresignCard } from "@/features/storage/components/StoragePresignCard";
import { StorageQuotaCard } from "@/features/storage/components/StorageQuotaCard";
import { StorageObjectsCard } from "@/features/storage/components/StorageObjectsCard";
import { StorageConnectionCard } from "@/features/storage/components/StorageConnectionCard";
import { formatBytes } from "@/features/storage/format";

export const Route = createFileRoute("/storage/$bucketId")({
  component: StorageBucketDetailPage,
});

const routeApi = getRouteApi("/storage/$bucketId");

function useBucketId(): string {
  const params = routeApi.useParams() as unknown as { bucketId: string };
  return params.bucketId;
}

const DETAIL_TABS = [
  "objects",
  "overview",
  "connection",
  "credentials",
  "presign",
  "quota",
] as const;
type DetailTab = (typeof DETAIL_TABS)[number];

function StorageBucketDetailPage() {
  const { t } = useTranslation();
  const bucketId = useBucketId();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const query = useStorageBucket(tenantId, bucketId);

  if (query.isLoading) return <LoadingState rows={4} />;
  if (query.error) {
    return (
      <ErrorState
        message={t("storage.detail.notFound")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  }
  const bucket = query.data;
  if (!bucket) return null;

  const renderTab = (tab: DetailTab) => {
    switch (tab) {
      case "objects":
        return <StorageObjectsCard bucketId={bucketId} />;
      case "overview":
        return (
          <div className="space-y-4">
            <StorageUsageCard tenantId={tenantId} bucket={bucket} />
            <Card>
              <CardHeader>
                <CardTitle className="text-base">{t("storage.detail.id")}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <DetailRow label={t("storage.detail.name")} value={bucket.name} />
                <DetailRow label={t("storage.detail.slug")} value={bucket.slug} />
                <DetailRow label={t("storage.detail.owner")} value={bucket.ownerId} />
                <DetailRow
                  label={t("storage.detail.label")}
                  value={bucket.label ?? t("common.none")}
                />
                <DetailRow
                  label={t("storage.detail.description")}
                  value={bucket.description ?? t("storage.detail.noDescription")}
                />
                <DetailRow
                  label={t("storage.detail.bytesUsed")}
                  value={formatBytes(bucket.bytesUsed)}
                />
                <DetailRow
                  label={t("storage.detail.objectsUsed")}
                  value={bucket.objectsUsed.toLocaleString()}
                />
              </CardContent>
            </Card>
          </div>
        );
      case "connection":
        return <StorageConnectionCard bucket={bucket} />;
      case "credentials":
        return <StorageCredentialsCard tenantId={tenantId} bucketId={bucketId} />;
      case "presign":
        return <StoragePresignCard bucketId={bucketId} />;
      case "quota":
        return <StorageQuotaCard tenantId={tenantId} bucket={bucket} />;
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <Button asChild variant="ghost" size="sm" className="mb-2">
          <Link to="/storage">
            <ArrowLeftIcon className="h-4 w-4" />
            {t("common.back")}
          </Link>
        </Button>
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold">{bucket.name}</h1>
          <p className="text-sm text-muted-foreground">
            {bucket.description ?? t("storage.detail.noDescription")}
          </p>
        </header>
      </div>

      <Tabs defaultValue={DETAIL_TABS[0]}>
        <TabsList>
          {DETAIL_TABS.map((tab) => (
            <TabsTrigger key={tab} value={tab}>
              {t(`storage.detail.tabs.${tab}`)}
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

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[140px_1fr] items-center gap-2">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className="break-all font-mono text-xs">{value}</span>
    </div>
  );
}
