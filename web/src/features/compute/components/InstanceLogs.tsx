/**
 * InstanceLogs — the Logs tab on the instance detail page.
 *
 * Lists the log files the compute backend keeps for the instance (the
 * console output buffer plus the runtime log) and shows the selected one.
 * Large logs are tailed server-side to the last 1 MiB.
 */
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCwIcon, ScrollTextIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { useInstanceLog, useInstanceLogs } from "../api";
import { RuntimeSection } from "./RuntimeSection";

interface Props {
  instanceId: string;
  tenantId: string | null;
}

export function InstanceLogs({ instanceId, tenantId }: Props) {
  const { t } = useTranslation();
  const logs = useInstanceLogs(tenantId, instanceId);
  const [file, setFile] = useState<string | null>(null);

  // Default to the first offered log (the console buffer).
  useEffect(() => {
    if (!file && logs.data && logs.data.length > 0) setFile(logs.data[0] ?? null);
  }, [file, logs.data]);

  const log = useInstanceLog(tenantId, instanceId, file);

  if (logs.isLoading) return <LoadingState rows={3} />;
  if (logs.error) {
    return (
      <ErrorState
        message={t("common.error")}
        retryLabel={t("common.retry")}
        onRetry={() => void logs.refetch()}
      />
    );
  }
  const names = logs.data ?? [];
  if (names.length === 0) {
    return <EmptyState icon={ScrollTextIcon} title={t("compute.logs.none")} />;
  }

  return (
    <RuntimeSection
      title={t("compute.logs.title")}
      description={t("compute.logs.hint")}
      actions={
        <>
          <Select value={file ?? undefined} onValueChange={setFile}>
            <SelectTrigger className="h-8 w-48" aria-label={t("compute.logs.file")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {names.map((n) => (
                <SelectItem key={n} value={n}>
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void log.refetch()}
            disabled={log.isFetching}
          >
            <RefreshCwIcon className="h-4 w-4" />
            {t("compute.logs.refresh")}
          </Button>
        </>
      }
    >
      {log.isLoading ? (
        <LoadingState rows={6} />
      ) : log.error ? (
        <ErrorState
          message={t("common.error")}
          retryLabel={t("common.retry")}
          onRetry={() => void log.refetch()}
        />
      ) : (
        <div>
          {log.data?.truncated && (
            <p className="border-b border-border px-3 py-2 text-xs text-muted-foreground">
              {t("compute.logs.truncated")}
            </p>
          )}
          <pre
            data-testid="instance-log-content"
            dir="ltr"
            className="max-h-[32rem] overflow-auto bg-black p-3 font-mono text-xs leading-5 text-neutral-200"
          >
            {log.data?.content.trim() ? log.data.content : t("compute.logs.empty")}
          </pre>
        </div>
      )}
    </RuntimeSection>
  );
}
