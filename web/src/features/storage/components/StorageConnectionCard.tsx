/**
 * StorageConnectionCard — "how to connect to this bucket from an external
 * S3 client".
 *
 * Shows the bucket canonical name + the S3 endpoint this deployment
 * signs presigned URLs against. The endpoint is discovered by probing a
 * throwaway presign call once (cached via TanStack Query); we extract the
 * URL's origin because that is exactly the host an external S3 client
 * must target.
 *
 * Note: in deployments where the operator configured an internal hostname
 * (e.g. `http://seaweedfs:8333` in the dev compose stack), the host is
 * only reachable from inside the docker network. The card surfaces the
 * raw configured endpoint so the user knows what to substitute.
 */
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useState } from "react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useToast } from "@/hooks/useToast";
import { apiClient } from "@/lib/api/client";
import type { components } from "@api-schema";

type StorageBucket = components["schemas"]["StorageBucket"];

interface Props {
  bucket: StorageBucket;
}

const PROBE_KEY = (bucketId: string) => ["storage", "connection", bucketId] as const;

export function StorageConnectionCard({ bucket }: Props) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const [copied, setCopied] = useState<string | null>(null);

  // One-shot presign of a probe key to discover the S3 endpoint origin.
  // Disabled until the user clicks "reveal" so we don't mint an audit row
  // (and a network call) for every bucket detail view.
  const [probed, setProbed] = useState(false);
  const probe = useQuery({
    queryKey: PROBE_KEY(bucket.id),
    enabled: probed,
    staleTime: Infinity,
    queryFn: async (): Promise<string> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/storage/buckets/{bucketId}/presign",
        {
          params: { path: { bucketId: bucket.id } },
          body: { method: "GET", key: ".lahijan-endpoint-probe", expiresInSeconds: 1 },
        },
      );
      if (error || !data?.url) {
        throw new Error(`storage.connection.probe: ${response?.status ?? "network"}`);
      }
      try {
        return new URL(data.url).origin;
      } catch {
        return data.url;
      }
    },
  });

  const endpoint = probe.data ?? "";

  const s3cmd = `[default]
host_base = ${endpoint}
host_bucket = ${endpoint}
bucket_location = us-east-1
use_path_style = true`;

  const awsCli = `endpoint_url = ${endpoint}
# bucket: ${bucket.name}`;

  const copy = async (which: string, text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(which);
      toast({ title: t("common.copied"), variant: "success" });
    } catch {
      /* clipboard blocked in some sandboxed contexts */
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("storage.connection.title")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <Row label={t("storage.detail.name")} value={bucket.name} mono />
        <Row label={t("storage.detail.slug")} value={bucket.slug} mono />

        <div className="space-y-1">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">
            {t("storage.connection.endpoint")}
          </p>
          {probed ? (
            probe.isLoading ? (
              <Skeleton className="h-5 w-48" />
            ) : probe.error ? (
              <p className="text-xs text-destructive">{t("storage.connection.endpointError")}</p>
            ) : (
              <p className="break-all font-mono text-xs" data-testid="s3-endpoint">
                {endpoint}
              </p>
            )
          ) : (
            <Button size="sm" variant="outline" onClick={() => setProbed(true)}>
              {t("storage.connection.reveal")}
            </Button>
          )}
        </div>

        <p className="text-xs text-muted-foreground">{t("storage.connection.hint")}</p>

        <ConfigBlock
          label={t("storage.connection.s3cmd")}
          code={s3cmd}
          copied={copied === "s3cmd"}
          onCopy={() => copy("s3cmd", s3cmd)}
        />
        <ConfigBlock
          label={t("storage.connection.awsCli")}
          code={awsCli}
          copied={copied === "aws"}
          onCopy={() => copy("aws", awsCli)}
        />
      </CardContent>
    </Card>
  );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[120px_1fr] items-center gap-2">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className={mono ? "break-all font-mono text-xs" : "text-xs"}>{value}</span>
    </div>
  );
}

function ConfigBlock({
  label,
  code,
  copied,
  onCopy,
}: {
  label: string;
  code: string;
  copied: boolean;
  onCopy: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium">{label}</p>
        <Button size="sm" variant="ghost" onClick={onCopy}>
          {copied ? t("common.copied") : t("common.copy")}
        </Button>
      </div>
      <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-xs">
        {code}
      </pre>
    </div>
  );
}
