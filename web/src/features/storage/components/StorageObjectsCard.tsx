/**
 * StorageObjectsCard — the bucket's "Objects" tab.
 *
 * A small file browser built on the existing /buckets/{id}/versions +
 * /buckets/{id}/presign endpoints (per ADR-0011 the data plane is direct
 * to SeaweedFS; Lahijan only lists + signs).
 *
 *   - Browse: lists the latest version of each key (prefix-filterable).
 *   - Upload: file picker → presigned PUT → XHR upload with progress.
 *   - Download: per-row presigned GET → opens in a new tab.
 *
 * Both upload + download degrade gracefully: if the browser cannot reach
 * the presigned URL (e.g. the deployment's internal S3 hostname is not
 * externally routable), the raw URL is revealed so the user can open it
 * elsewhere or wire up an external S3 client.
 */
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  DownloadIcon,
  FileIcon,
  RefreshCwIcon,
  UploadIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { LoadingState } from "@/components/layout/LoadingState";
import { FeatureDisabledState } from "@/components/layout/FeatureDisabledState";
import { ErrorState } from "@/components/layout/ErrorState";
import { EmptyState } from "@/components/layout/EmptyState";
import { useToast } from "@/hooks/useToast";
import { isFeatureDisabledError } from "@/lib/api-errors";
import {
  useStorageObjectVersions,
  uploadObjectViaPresign,
} from "../api";
import { formatBytes } from "../format";

interface Props {
  bucketId: string;
}

export function StorageObjectsCard({ bucketId }: Props) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const [prefix, setPrefix] = useState("");
  const query = useStorageObjectVersions(null, bucketId, prefix);
  const fileInput = useRef<HTMLInputElement>(null);

  // Upload state: { name, percent } for the in-flight file, null when idle.
  const [upload, setUpload] = useState<{ name: string; percent: number } | null>(null);

  // Deduplicate to the latest version of each key. The versions endpoint
  // returns every version on a versioned bucket; for a plain browse view we
  // only want the current object per key.
  const rows = (query.data?.versions ?? [])
    .filter((v) => v.isLatest !== false)
    .filter((v) => !v.isDeleteMarker);

  const onPickFile = () => fileInput.current?.click();

  const onFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    const key = file.name;
    setUpload({ name: key, percent: 0 });
    try {
      await uploadObjectViaPresign({
        bucketId,
        key,
        file,
        onProgress: (percent) => setUpload({ name: key, percent }),
      });
      toast({ title: t("storage.objects.uploadSuccess", { key }), variant: "success" });
      void query.refetch();
    } catch (err) {
      toast({
        title: t("storage.objects.uploadError"),
        description: extractMessage(err),
        variant: "destructive",
      });
    } finally {
      setUpload(null);
    }
  };

  const onDownload = async (key: string) => {
    try {
      const url = await presignGet(bucketId, key);
      window.open(url, "_blank", "noopener,noreferrer");
    } catch (err) {
      toast({
        title: t("storage.objects.downloadError"),
        description: extractMessage(err),
        variant: "destructive",
      });
    }
  };

  let body: React.ReactNode;
  if (query.isLoading) {
    body = <LoadingState rows={4} />;
  } else if (isFeatureDisabledError(query.error)) {
    body = (
      <FeatureDisabledState
        title={t("common.featureDisabled.title")}
        description={t("common.featureDisabled.description")}
      />
    );
  } else if (query.error) {
    body = (
      <ErrorState
        message={t("storage.objects.listError")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  } else if (rows.length === 0) {
    body = (
      <EmptyState
        icon={FileIcon}
        title={t("storage.objects.empty.title")}
        description={t("storage.objects.empty.body")}
      />
    );
  } else {
    body = (
      <ul className="divide-y divide-border rounded-md border border-border">
        {rows.map((v, i) => (
          <li key={`${v.key}-${i}`} className="flex items-center gap-3 px-3 py-2">
            <FileIcon className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            <div className="min-w-0 flex-1">
              <p className="truncate break-all font-mono text-xs">{v.key}</p>
              <p className="text-xs text-muted-foreground">
                {formatBytes(v.size ?? 0)}
                {v.lastModified
                  ? ` · ${new Date(v.lastModified).toLocaleString()}`
                  : ""}
              </p>
            </div>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => onDownload(v.key)}
              aria-label={t("storage.objects.download")}
            >
              <DownloadIcon className="h-4 w-4" />
            </Button>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
          placeholder={t("storage.objects.prefixPlaceholder")}
          className="max-w-xs"
          aria-label={t("common.search")}
        />
        <Button variant="ghost" size="sm" onClick={() => void query.refetch()}>
          <RefreshCwIcon className="h-4 w-4" />
          {t("common.retry")}
        </Button>
        <Button size="sm" className="ms-auto" onClick={onPickFile} disabled={!!upload}>
          <UploadIcon className="h-4 w-4" />
          {t("storage.objects.upload")}
        </Button>
        <input
          ref={fileInput}
          type="file"
          className="hidden"
          onChange={(e) => void onFileChange(e)}
        />
      </div>

      {upload && (
        <div className="space-y-1 rounded-md border border-border bg-muted/30 p-3">
          <div className="flex items-center justify-between text-xs">
            <span className="truncate font-mono">{upload.name}</span>
            <span className="tabular-nums">{upload.percent}%</span>
          </div>
          <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
            <div
              className="h-full bg-primary transition-[width]"
              style={{ width: `${upload.percent}%` }}
            />
          </div>
        </div>
      )}

      {query.isFetching && !query.isLoading ? (
        <Skeleton className="h-4 w-24" />
      ) : null}

      {body}
    </div>
  );
}

/**
 * presignGet — mints a GET presigned URL via the control-plane API. Used by
 * the per-row download button. Kept here (not in api.ts) because it is a
 * one-shot helper with no cache; the hooks in api.ts own cacheable state.
 */
async function presignGet(bucketId: string, key: string): Promise<string> {
  const { apiClient } = await import("@/lib/api/client");
  const { data, error, response } = await apiClient.POST(
    "/api/v1/storage/buckets/{bucketId}/presign",
    {
      params: { path: { bucketId } },
      body: { method: "GET", key, expiresInSeconds: 300 },
    },
  );
  if (error || !data) {
    throw new Error(`storage.object.download.presign: ${response?.status ?? "network"}`);
  }
  return data.url;
}

function extractMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return "";
}
