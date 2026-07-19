/**
 * StorageUsageCard — small usage banner showing bytesUsed / quotaBytes
 * and objectsUsed / quotaObjects as a progress bar.
 */
import { useTranslation } from "react-i18next";
import type { components } from "@api-schema";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useStorageBucketUsage } from "../api";
import { formatBytes } from "../format";

type StorageBucket = components["schemas"]["StorageBucket"];

interface Props {
  tenantId: string | null;
  bucket: StorageBucket;
}

export function StorageUsageCard({ tenantId, bucket }: Props) {
  const { t } = useTranslation();
  const usage = useStorageBucketUsage(tenantId, bucket.id);

  const bytesUsed = usage.data?.bytesUsed ?? bucket.bytesUsed ?? 0;
  const objectsUsed = usage.data?.objectsUsed ?? bucket.objectsUsed ?? 0;
  const quotaBytes = usage.data?.quotaBytes ?? bucket.quotaBytes ?? 0;
  const quotaObjects = usage.data?.quotaObjects ?? bucket.quotaObjects ?? 0;

  const pctBytes = quotaBytes > 0 ? Math.min(100, (bytesUsed / quotaBytes) * 100) : 0;
  const pctObjs = quotaObjects > 0 ? Math.min(100, (objectsUsed / quotaObjects) * 100) : 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("storage.usage.title")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <UsageBar
          label={t("storage.usage.size")}
          used={formatBytes(bytesUsed)}
          quota={quotaBytes > 0 ? formatBytes(quotaBytes) : t("storage.usage.unlimited")}
          pct={pctBytes}
        />
        <UsageBar
          label={t("storage.usage.objects")}
          used={objectsUsed.toLocaleString()}
          quota={quotaObjects > 0 ? quotaObjects.toLocaleString() : t("storage.usage.unlimited")}
          pct={pctObjs}
        />
      </CardContent>
    </Card>
  );
}

function UsageBar({
  label,
  used,
  quota,
  pct,
}: {
  label: string;
  used: string;
  quota: string;
  pct: number;
}) {
  const { t } = useTranslation();
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-sm">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-mono text-xs">{t("storage.usage.bar", { used, quota })}</span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
        <div
          className="h-2 rounded-full bg-primary transition-all"
          style={{ width: `${Math.max(2, pct)}%` }}
          aria-label={`${label}: ${Math.round(pct)}%`}
        />
      </div>
    </div>
  );
}
